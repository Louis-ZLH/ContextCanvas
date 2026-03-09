package infra

import (
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ 封装了 AMQP 连接和 Channel，提供发布消息能力。
type RabbitMQ struct {
	url             string
	Conn            *amqp.Connection
	PubChannel      *amqp.Channel
	SubChannel      *amqp.Channel
	notifyConnClose chan *amqp.Error // 用于监听连接断开的 channel
	mu              sync.RWMutex    // 保护 PubChannel、SubChannel、ReconnectCh 的并发访问
	ReconnectCh     chan struct{}    // 重连成功后 close 此 channel 进行广播
	done            chan struct{}    // 通知 watchConnection goroutine 退出
	closeOnce       sync.Once       // 保护 close(done) 不被重复调用导致 panic
	closed          bool            // 标记是否已 Close，防止 handleReconnect 在 Close 后继续重连
}

// NewRabbitMQ 初始化并启动重连守护协程。首次连接失败时返回 error。
func NewRabbitMQ(url string) (*RabbitMQ, error) {
	rmq := &RabbitMQ{
		url:         url,
		ReconnectCh: make(chan struct{}),
		done:        make(chan struct{}),
	}
	// 1. 首次连接（失败立即返回 error）
	if err := rmq.connect(); err != nil {
		return nil, err
	}

	// 2. 开启后台协程，专门盯着连接状态
	go rmq.watchConnection()

	return rmq, nil
}

// declareTopologyOn 在指定 Channel 上声明所有 exchange（首次连接和重连复用）
func declareTopologyOn(ch *amqp.Channel) error {
	// 已有：ai_exchange
	if err := ch.ExchangeDeclare("ai_exchange", "topic", true, false, false, false, nil); err != nil {
		return err
	}
	// 新增：email_exchange
	if err := ch.ExchangeDeclare("email_exchange", "topic", true, false, false, false, nil); err != nil {
		return err
	}
	return nil
}

// connect 尝试建立一次连接，失败直接返回 error
func (r *RabbitMQ) connect() error {
	conn, err := amqp.Dial(r.url)
	if err != nil {
		return err
	}

	pubCh, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}

	subCh, err := conn.Channel()
	if err != nil {
		pubCh.Close()
		conn.Close()
		return err
	}

	// 先声明拓扑（使用局部变量，失败时直接 close 局部变量即可）
	if err := declareTopologyOn(pubCh); err != nil {
		subCh.Close()
		pubCh.Close()
		conn.Close()
		return err
	}

	// 拓扑成功后再赋值到 struct
	r.Conn = conn
	r.PubChannel = pubCh
	r.SubChannel = subCh

	r.notifyConnClose = make(chan *amqp.Error, 1)
	r.Conn.NotifyClose(r.notifyConnClose)

	log.Println("RabbitMQ 连接成功并已建立 Channels")
	return nil
}

// GetPubChannel 线程安全地获取 PubChannel
func (r *RabbitMQ) GetPubChannel() *amqp.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.PubChannel
}

// GetSubChannel 线程安全地获取 SubChannel
func (r *RabbitMQ) GetSubChannel() *amqp.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.SubChannel
}

// GetReconnectCh 线程安全地获取 ReconnectCh
func (r *RabbitMQ) GetReconnectCh() <-chan struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ReconnectCh
}

// NewChannel 从当前连接创建一个新的独立 Channel（线程安全）
func (r *RabbitMQ) NewChannel() (*amqp.Channel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.Conn == nil || r.Conn.IsClosed() {
		return nil, fmt.Errorf("rabbitmq connection is not available")
	}
	return r.Conn.Channel()
}

// watchConnection 负责监听掉线事件并触发重连
func (r *RabbitMQ) watchConnection() {
	for {
		select {
		case <-r.done:
			return
		case err := <-r.notifyConnClose:
			log.Printf("RabbitMQ 连接断开: %v. 开始重连...", err)
			r.handleReconnect()
		}
	}
}

// handleReconnect 包含具体的连接和 Channel 重建逻辑
func (r *RabbitMQ) handleReconnect() {
	for {
		// 每轮重连循环开头检查是否已关闭
		r.mu.RLock()
		if r.closed {
			r.mu.RUnlock()
			return
		}
		r.mu.RUnlock()

		log.Println("尝试连接 RabbitMQ...")
		newConn, err := amqp.Dial(r.url)
		if err != nil {
			log.Printf("连接失败: %v. 3秒后重试...", err)
			time.Sleep(3 * time.Second)
			continue
		}

		// 连接成功后，建立收发分离的 Channel
		newPubCh, err := newConn.Channel()
		if err != nil {
			log.Printf("创建 Pub Channel 失败: %v", err)
			newConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		newSubCh, err := newConn.Channel()
		if err != nil {
			log.Printf("创建 Sub Channel 失败: %v", err)
			newPubCh.Close()
			newConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		// 先用局部变量声明拓扑（此时尚未赋值到 struct，不需要加锁）
		if err := declareTopologyOn(newPubCh); err != nil {
			log.Printf("重连后声明拓扑失败: %v. 3秒后重试...", err)
			newSubCh.Close()
			newPubCh.Close()
			newConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		// 拓扑声明成功后，再加锁赋值给结构体
		r.mu.Lock()
		if r.closed {
			// Close() 在重连期间被调用，放弃本次重连
			r.mu.Unlock()
			newSubCh.Close()
			newPubCh.Close()
			newConn.Close()
			return
		}
		r.Conn = newConn
		r.PubChannel = newPubCh
		r.SubChannel = newSubCh
		close(r.ReconnectCh)                // 广播：所有监听者同时收到信号
		r.ReconnectCh = make(chan struct{}) // 重置，为下次重连做准备
		r.mu.Unlock()

		// 重新注册断线监听 Channel（在锁外，因为只有 watchConnection 单协程访问）
		r.notifyConnClose = make(chan *amqp.Error, 1)
		r.Conn.NotifyClose(r.notifyConnClose)

		log.Println("RabbitMQ 重连成功并已重建 Channels")
		break
	}
}

// DeclareQueue 声明一个持久化队列（幂等操作）。
func (r *RabbitMQ) DeclareQueue(name string) (amqp.Queue, error) {
	return r.GetSubChannel().QueueDeclare(
		name,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	)
}

// Close 按序关闭 Channel 和 Connection。
func (r *RabbitMQ) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true

	if r.SubChannel != nil {
		_ = r.SubChannel.Close()
	}
	if r.PubChannel != nil {
		_ = r.PubChannel.Close()
	}
	if r.Conn != nil {
		_ = r.Conn.Close()
	}
	r.mu.Unlock()

	r.closeOnce.Do(func() { close(r.done) })
}
