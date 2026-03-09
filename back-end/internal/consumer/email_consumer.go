package consumer

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/luhao/contextGraph/internal/infra"
	"github.com/luhao/contextGraph/pkg/email"
)

type EmailConsumer struct {
	mq     *infra.RabbitMQ
	sender *email.Sender
	done   chan struct{}
	wg     sync.WaitGroup
}

func NewEmailConsumer(mq *infra.RabbitMQ, sender *email.Sender) *EmailConsumer {
	return &EmailConsumer{
		mq:     mq,
		sender: sender,
		done:   make(chan struct{}),
	}
}

func (c *EmailConsumer) Start() error {
	go c.run()
	return nil
}

func (c *EmailConsumer) Stop() {
	close(c.done)
	c.wg.Wait()
}

func (c *EmailConsumer) run() {
	for {
		if err := c.consumeLoop(); err != nil {
			log.Printf("email consumer: consume error: %v", err)
		}

		select {
		case <-c.done:
			return
		case <-c.mq.GetReconnectCh():
			log.Println("email consumer: reconnected, rebuilding...")
			continue
		case <-time.After(5 * time.Second):
			log.Println("email consumer: retry timeout, attempting to rebuild...")
			continue
		}
	}
}

func (c *EmailConsumer) consumeLoop() error {
	ch, err := c.mq.NewChannel()
	if err != nil {
		return err
	}
	defer ch.Close()

	_, err = ch.QueueDeclare("email_queue", true, false, false, false, nil)
	if err != nil {
		return err
	}
	err = ch.QueueBind("email_queue", "email.verification", "email_exchange", false, nil)
	if err != nil {
		return err
	}

	deliveries, err := ch.Consume("email_queue", "email-consumer", false, false, false, false, nil)
	if err != nil {
		return err
	}

	closeCh := make(chan *amqp.Error, 1)
	ch.NotifyClose(closeCh)

	for {
		select {
		case <-c.done:
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				c.handleDelivery(delivery)
			}()
		case <-closeCh:
			log.Println("email consumer: channel closed, waiting for reconnect...")
			return nil
		}
	}
}

func (c *EmailConsumer) handleDelivery(delivery amqp.Delivery) {
	var msg email.Message
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		log.Printf("email consumer: unmarshal failed: %v", err)
		delivery.Ack(false)
		return
	}

	const maxRetries = 3
	for i := 0; i < maxRetries; i++ {
		if err := c.sender.Send(msg.To, msg.Subject, msg.Body); err != nil {
			log.Printf("email send attempt %d/%d failed: to=%s, err=%v", i+1, maxRetries, msg.To, err)
			if i < maxRetries-1 {
				backoff := time.Duration(4<<i) * time.Second
				select {
				case <-time.After(backoff):
					continue
				case <-c.done:
					delivery.Nack(false, true)
					return
				}
			}
			log.Printf("email send failed after %d retries: to=%s", maxRetries, msg.To)
			delivery.Ack(false)
			return
		}
		delivery.Ack(false)
		return
	}
}
