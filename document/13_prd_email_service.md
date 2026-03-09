# 邮件服务：RabbitMQ 异步 + SMTP

## 背景

`authService.SendCode` 中生成验证码后直接 `log.Println` 打印，需要实现真正的邮件发送。

SMTP 发送涉及 TCP 连接 + TLS 握手 + 多轮协议交互，通常耗时 200ms ~ 2s。为了不阻塞接口响应，采用 RabbitMQ 异步发送：`SendCode` 只负责 publish 消息到队列，同进程内单独起一个 goroutine 消费队列并通过 SMTP 发邮件。

## 架构设计

```
用户请求 SendCode
       │
       ▼
 AuthService.SendCode()
       │
       │  1. 生成验证码、存 Redis
       │  2. publish 邮件任务到 RabbitMQ
       │     ├─ 成功 → 立即返回
       │     └─ 失败 → fallback 同步发送邮件 → 返回
       ▼
   RabbitMQ
   email_exchange (topic, 在 infra 层统一声明)
   routing key: email.verification
       │
       ▼
   email_queue
       │
       ▼
 EmailConsumer (同进程 goroutine，支持 MQ 重连后自动重建)
       │
       │  消费消息 → 开启子协程调用 SMTP Sender 发邮件
       │  成功 → Ack
       │  失败 → 本地重试（指数退避 4s/8s，最多 3 次）
       │         全部失败 → Ack 丢弃 + 结构化日志告警
       ▼
   Gmail SMTP (smtp.gmail.com:587)
```

**关键决策：同进程 goroutine，不另起服务。** 验证码邮件量不大，没必要增加部署复杂度。

---

## 后端改动

### 1. 添加 SMTP 配置 — `config/config.go`

新增字段，通过环境变量注入：

```go
// SMTP 配置
SMTPHost     string  // 环境变量 SMTP_HOST，默认 "smtp.gmail.com"
SMTPPort     string  // 环境变量 SMTP_PORT，默认 "587"
SMTPUser     string  // 环境变量 SMTP_USER，无默认值（发件人邮箱）
SMTPPassword string  // 环境变量 SMTP_PASSWORD，无默认值（Gmail 应用专用密码）
SMTPFromName string  // 环境变量 SMTP_FROM_NAME，默认 "ContextGraph"（显示名，地址自动取 SMTPUser）
```

`Sender` 内部通过 `mail.Address{Name: cfg.SMTPFromName, Address: cfg.SMTPUser}` 生成符合 RFC 5322 的 From 头，无需用户手动拼接 `"Name <addr>"` 格式字符串。

#### 本地开发环境变量加载

引入 `github.com/joho/godotenv`，在 `config.Load()` 最开头调用 `godotenv.Load()`，自动从项目根目录 `.env` 文件加载环境变量。该库的行为是**不覆盖已存在的环境变量**，因此 Docker / CI 中通过 `environment:` 注入的变量不受影响。

```go
import "github.com/joho/godotenv"

func Load() *Config {
    // 本地开发时从 .env 加载，生产环境中 .env 不存在则静默跳过
    _ = godotenv.Load()

    // ... 其余不变
}
```

项目根目录新增 `.env.example` 作为模板（提交到 Git），`.env` 加入 `.gitignore`（已有则确认包含）：

```
# .env.example — 复制为 .env 并填入实际值
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=
SMTP_PASSWORD=
SMTP_FROM_NAME=ContextGraph
```

**SMTP 配置校验：** `SMTPUser` 和 `SMTPPassword` 无默认值，若未配置则 `Sender.Send()` 会在运行时失败。不在启动时强制校验（避免本地开发不需要邮件功能时无法启动服务），但在 `Sender.Send()` 入口检查空值并返回明确错误信息：

```go
func (s *Sender) Send(to, subject, body string) error {
    if s.User == "" || s.Password == "" {
        return fmt.Errorf("SMTP credentials not configured (SMTP_USER / SMTP_PASSWORD)")
    }
    // ...
}
```

### 2. 新建 SMTP 发送封装 — `pkg/email/sender.go`（新文件）

纯粹的 SMTP 客户端，不关心 RabbitMQ：

```go
type Sender struct {
    Host     string
    Port     string
    User     string
    Password string
    FromName string
}

func (s *Sender) Send(to, subject, body string) error
```

- 使用 `github.com/wneessen/go-mail`（标准库 `net/smtp` 功能有限且维护消极，go-mail 对 STARTTLS、MIME multipart、附件等支持更完善，避免后续迁移）
- 构造标准 MIME 邮件头（支持 HTML body）
- `From` 字段内部通过 `mail.Address{Name: s.FromName, Address: s.User}` 生成符合 RFC 5322 的格式

### 3. 修改 RabbitMQ 基础设施 — `internal/infra/rabbitmq.go`

#### 3a. Exchange Topology 统一声明

将 `email_exchange` 的声明收敛到基础设施层，与 `ai_exchange` 在同一处统一管理，避免 Publisher / Consumer 两侧重复声明：

将现有 `declareTopology()` 方法重构为 `declareTopologyOn(ch *amqp.Channel)` 包级函数，接受 Channel 参数而非依赖 `r.PubChannel` 字段（原因见 3b 改动 4），并新增 `email_exchange` 声明：

```go
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
```

`connect()` 和 `handleReconnect()` 中均调用 `declareTopologyOn(pubCh)`（使用局部变量），这样 MQ 连接建立（含重连）时自动声明所有 exchange，Publisher 和 Consumer 无需各自重复声明。

**`connect()` 中 topology 声明须在赋值之前：** 现有代码先 `r.PubChannel = pubCh` 赋值到 struct，再调用 `r.declareTopology()`。改为 `declareTopologyOn(pubCh)` 后，必须将调用位置上移到赋值语句之前（与 `handleReconnect()` 保持一致）。否则 topology 声明失败时 struct 字段已被赋值但随后 close，留下无效指针。正确顺序：

```go
func (r *RabbitMQ) connect() error {
    conn, err := amqp.Dial(r.url)
    // ... 创建 pubCh, subCh ...

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
    // ...
}
```

#### 3b. 新增并发保护 + 重连广播机制

**问题 1：** 现有 `handleReconnect()` 重建 SubChannel 后，没有任何机制通知外部消费者。如果 Consumer 在感知 channel 断开后盲等固定时间（如 `time.Sleep(3s)`），重连可能尚未完成（网络抖动、多轮重试），导致 Consumer 在旧 channel 上操作而失败。

**问题 2：** 现有 `handleReconnect()` 直接赋值 `r.PubChannel = pubCh` / `r.SubChannel = subCh`，而 Publisher 和 Consumer 在其他 goroutine 中读取这些字段，构成 data race（`go run -race` 会报错）。新增 EmailConsumer 后这个问题更加突出。

**方案：**
- 使用 `sync.RWMutex` 保护 `PubChannel`、`SubChannel`、`ReconnectCh` 的并发读写
- 利用 Go 的 `close(channel)` 一对多广播模式通知重连——关闭一个 channel 会让所有正在 `<-ch` 的 goroutine 同时收到零值，天然适合"重连成功通知所有消费者"这个场景

**改动 1 — RabbitMQ struct 新增字段：**

```go
type RabbitMQ struct {
    // ... 现有字段
    mu          sync.RWMutex     // 保护 PubChannel、SubChannel、ReconnectCh 的并发访问
    ReconnectCh chan struct{}    // 重连成功后 close 此 channel 进行广播
    done        chan struct{}    // 通知 watchConnection goroutine 退出
    closeOnce   sync.Once       // 保护 close(done) 不被重复调用导致 panic
    closed      bool            // 标记是否已 Close，防止 handleReconnect 在 Close 后继续重连
}
```

**改动 2 — 新增线程安全的 Channel 访问方法：**

```go
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
```

所有外部代码（Publisher、Consumer）通过这些方法访问，不再直接读取字段。

**TOCTOU 说明：** getter 返回的是指针副本，在 `RUnlock()` 之后、实际调用 `PublishWithContext()` 之前，`handleReconnect()` 可能已替换 Channel 并关闭旧的，导致 `Publish` 返回 error。这是预期行为，由上层 error handling 兜底：`emailRepo.PublishEmailTask` 失败后走 fallback 同步发送（见第 5 节），`fileRepo.PublishFileConvert` 失败后文件转换任务可由用户重试。无需在 getter 层面额外处理。

**改动 3 — 迁移现有直接访问点：**

引入 getter 方法后，必须同步修改所有现有的 `r.mq.PubChannel` / `r.mq.SubChannel` / `r.SubChannel` 直接访问，否则 data race 修了一半反而更危险。需要迁移的已知位置：

- `internal/repo/fileRepo.go`：`r.mq.PubChannel.PublishWithContext(...)` → `r.mq.GetPubChannel().PublishWithContext(...)`
- `internal/infra/rabbitmq.go` 的 `DeclareQueue()` 方法：`r.SubChannel.QueueDeclare(...)` → `r.GetSubChannel().QueueDeclare(...)`

全局搜索 `.PubChannel.` 和 `.SubChannel.`（排除 `rabbitmq.go` 内部的 `connect()`/`handleReconnect()` 赋值语句），确保无遗漏。

**改动 4 — `NewRabbitMQ()` 初始化：**

```go
rmq := &RabbitMQ{
    url:         url,
    ReconnectCh: make(chan struct{}),
    done:        make(chan struct{}),
}
```

**`connect()` 方法不需要加锁：** 首次调用 `connect()` 时 `watchConnection` goroutine 尚未启动，不存在并发访问，因此 `connect()` 内部直接赋值 `r.PubChannel = pubCh` 是安全的。只有 `handleReconnect()` 运行时才有多 goroutine 并发读写，需要加锁保护。

**改动 5 — `handleReconnect()` 加锁赋值 + 广播 + 检查 closed 标志：**

在 `handleReconnect()` 中，**先检查 closed 标志，再用局部变量完成 topology 声明，最后加锁赋值**。这样避免在锁内调用 `declareTopology()` 导致持锁时间过长或死锁风险，同时防止 `Close()` 后仍继续重连。

**局部变量统一使用 `new` 前缀**（`newConn`、`newPubCh`、`newSubCh`）以明确区分"尚未赋值到 struct 的新连接"与"struct 上的现有字段"，避免在代码审查时产生混淆：

```go
// 每轮重连循环开头检查是否已关闭
r.mu.RLock()
if r.closed {
    r.mu.RUnlock()
    return
}
r.mu.RUnlock()

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
```

**相应地，`declareTopology()` 改为接受 Channel 参数的包级函数或独立方法**，使其不再依赖 `r.PubChannel` 字段：

```go
// declareTopologyOn 在指定 Channel 上声明所有 exchange（首次连接和重连复用）
func declareTopologyOn(ch *amqp.Channel) error {
    if err := ch.ExchangeDeclare("ai_exchange", "topic", true, false, false, false, nil); err != nil {
        return err
    }
    if err := ch.ExchangeDeclare("email_exchange", "topic", true, false, false, false, nil); err != nil {
        return err
    }
    return nil
}
```

`connect()` 方法中同样改为调用 `declareTopologyOn(pubCh)`（`connect()` 中无并发问题，局部变量无需加 `new` 前缀）。这样 topology 声明始终使用明确的局部 Channel 变量，不依赖 struct 字段，消除了锁相关的复杂性。

**为什么用 `sync.RWMutex` 而非 `sync.Mutex`：** 读操作（Publisher 发消息、Consumer 获取 channel）频率远高于写操作（仅重连时），`RWMutex` 允许多个读操作并发执行，不影响正常吞吐。

**改动 6 — `Close()` 方法加锁 + 设置 closed 标志 + 通知 watchConnection 退出：**

现有 `Close()` 直接访问 `r.SubChannel`、`r.PubChannel`、`r.Conn`，如果与 `handleReconnect()` 并发执行会产生 data race。需要加写锁保护，并设置 `closed` 标志防止 `handleReconnect()` 在 `Close()` 后继续重连建立新连接：

```go
func (r *RabbitMQ) Close() {
    r.mu.Lock()
    if r.closed {
        r.mu.Unlock()
        return // 防止重复调用
    }
    r.closed = true

    // 在同一把锁内完成所有操作，消除 closed 标志和资源释放之间的窗口期，
    // 防止 handleReconnect() 在两次加锁之间完成赋值导致资源泄漏。
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

    // 通知 watchConnection goroutine 退出（放在锁外，因为 close(done) 不涉及被锁保护的字段，
    // 且 watchConnection 的 handleReconnect 会在循环开头检查 closed 标志后退出）
    // 使用 sync.Once 保护，即使 closed 标志逻辑有误也不会 panic
    r.closeOnce.Do(func() { close(r.done) })
}
```

相应地，`watchConnection` 中的 `for` 循环需监听 `r.done`，收到信号后退出：

```go
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
```

### 4. 新建邮件消息定义 — `pkg/email/message.go`（新文件）

Publisher（`emailRepo`）和 Consumer（`email_consumer`）共享同一个消息结构体，避免两边各自定义导致 JSON 字段不一致、反序列化失败：

```go
// pkg/email/message.go
package email

// Message 是 RabbitMQ 中邮件任务的消息体，Publisher 和 Consumer 共用。
type Message struct {
    To      string `json:"to"`
    Subject string `json:"subject"`
    Body    string `json:"body"`
}
```

### 5. 新建邮件 Publisher — `internal/repo/emailRepo.go`（新文件）

publish 邮件任务到已在 infra 层声明好的 `email_exchange`，routing key `email.verification`：

```go
type EmailRepo struct {
    mq *infra.RabbitMQ
}

func NewEmailRepo(mq *infra.RabbitMQ) *EmailRepo

func (r *EmailRepo) PublishEmailTask(ctx context.Context, to, subject, htmlBody string) error
```

- 消息体使用 `email.Message{To: to, Subject: subject, Body: htmlBody}` 构造（引用 `pkg/email/message.go`）
- 通过 `r.mq.GetPubChannel()` 线程安全地获取 PubChannel，publish 到 `email_exchange` + routing key `email.verification`
- JSON 序列化、`DeliveryMode: amqp.Persistent`
- 注意参数命名 `htmlBody` 避免与 `json.Marshal` 产出的 `payload` 变量名冲突

### 6. 修改 AuthService — `internal/service/authService.go`

**注意：现有代码顺序需要调换。** 当前 `SendCode` 中是先 `log.Println`（line 77）再 `StoreVerificationCode`（line 80），改动后需要：
1. 删除 `log.Println("email: ", email, " code: ", code)` 这一行
2. 将 `StoreVerificationCode` **上移**到生成验证码之后立即执行
3. 在 `StoreVerificationCode` 之后追加 publish 调用 + fallback 同步发送

**操作顺序至关重要：** 必须先存 Redis 再发邮件。如果反过来（先发邮件再存 Redis），用户收到邮件后极速输入验证码时 Redis 可能还没存好，导致验证失败。现有代码的"先 log 再存 Redis"顺序本身就有隐患，这里一并修正。

**注意包名冲突：** `SendCode` 的参数名 `email string` 会遮蔽 `pkg/email` 的包名，导致 `email.BuildVerificationEmailHTML(...)` 被解析为对 `string` 类型调用方法而编译报错。解决方式：import 时使用别名 `emailpkg "项目/pkg/email"`，调用处改为 `emailpkg.BuildVerificationEmailHTML(...)`。

**验证码过期时间常量：** Redis TTL 和邮件模板中的过期提示必须同步，定义常量避免分散硬编码：

```go
const codeExpiryMinutes = 1 // 验证码有效期（分钟），同时用于 Redis TTL 和邮件模板显示
```

`StoreVerificationCode` 调用处改为 `time.Duration(codeExpiryMinutes) * time.Minute`，`BuildVerificationEmailHTML` 调用处传入 `codeExpiryMinutes`。后续调整过期时间只需改一处。

改动后的 `SendCode` 关键部分：

```go
code := idgen.RandomNumbers(6)

// 1. 先存 Redis（必须在发邮件之前）
err = s.userRepo.StoreVerificationCode(ctx, email, code, reqType)
if err != nil {
    return err
}

// 2. 再异步发邮件
subject := "ContextGraph 验证码"
htmlBody := emailpkg.BuildVerificationEmailHTML(code, codeExpiryMinutes)
err = s.emailRepo.PublishEmailTask(ctx, email, subject, htmlBody)
if err != nil {
    // publish 失败，fallback 同步发送
    log.Printf("email publish failed, fallback to sync send: %v", err)
    if sendErr := s.emailSender.Send(email, subject, htmlBody); sendErr != nil {
        log.Printf("sync email send also failed: %v", sendErr)
        return apperr.InternalError("Failed to send verification email")
    }
}

return nil
```

**为什么 fallback 而非直接报错：** publish 失败时验证码已存入 Redis，若直接返回错误用户需重新请求。fallback 同步发送虽然会阻塞当前请求（增加 200ms~2s 延迟），但能保证用户收到邮件，体验更好。仅当 MQ 和 SMTP 同时不可用时才返回错误。

**邮件模板 `BuildVerificationEmailHTML`：**

模板渲染函数和模板文件放在 `pkg/email/` 中（与 `Sender` 同层），保持邮件相关逻辑内聚，避免与 `authService` 耦合：

```go
// pkg/email/template.go

//go:embed template/verification_email.html
var verificationTmpl string

func BuildVerificationEmailHTML(code string, expiryMinutes int) string {
    tmpl := template.Must(template.New("email").Parse(verificationTmpl))
    var buf bytes.Buffer
    tmpl.Execute(&buf, map[string]any{"Code": code, "ExpiryMinutes": expiryMinutes})
    return buf.String()
}
```

模板文件位于 `pkg/email/template/verification_email.html`，包含品牌样式和验证码展示。使用 `embed` 的好处是部署时只需分发单个二进制文件，无需关心模板文件路径。使用 `html/template`（非 `text/template`）自动转义变量，防止 XSS。

`authService.go` 中调用 `emailpkg.BuildVerificationEmailHTML(code, codeExpiryMinutes)`，`codeExpiryMinutes` 常量同时用于 Redis TTL 和模板渲染，单点维护。

**新增接口定义（在 `authService.go` 中）：**

```go
type emailPublisher interface {
    PublishEmailTask(ctx context.Context, to, subject, htmlBody string) error
}
```

遵循现有模式（如 `userRepo` 接口），在消费方定义接口，实现方无需关心。

**依赖变更：**
- `AuthService` 新增 `emailRepo` 字段（类型 `emailPublisher`）和 `emailSender` 字段（`*emailpkg.Sender`，用于 fallback）
- `NewAuthService` 签名新增 `emailPublisher` + `*emailpkg.Sender` 参数
- **注意：** 现有所有调用 `NewAuthService` 的地方都需要同步修改。已知调用点：`internal/app/app.go` 的 `wireHandlers`（见第 8 节）。全局搜索 `NewAuthService(` 确保无遗漏（如测试文件中若有构造 `AuthService` 的地方也需同步更新）

### 7. 新建邮件 Consumer — `internal/consumer/email_consumer.go`（新文件）

同进程内的 goroutine 消费者。

**每个 Consumer 创建独立的 AMQP Channel：** 不复用 `rabbitmq.go` 中的共享 `SubChannel`。原因：AMQP 规范中同一个 Channel 上的多个 `basic.consume` 调用需要唯一 consumer tag，且 Channel 级别的错误（如消费端异常）会导致整个 Channel 关闭，影响所有共享该 Channel 的消费者。每个 Consumer 从 `r.mq.Conn` 创建自己的 Channel，故障隔离更好，也为未来新增其他 Consumer（如 AI 任务回调）留出空间。

```go
type EmailConsumer struct {
    mq      *infra.RabbitMQ
    sender  *email.Sender
    done    chan struct{}
    wg      sync.WaitGroup  // 追踪 in-flight handleDelivery goroutine，优雅关闭时等待完成
}

func NewEmailConsumer(mq *infra.RabbitMQ, sender *email.Sender) *EmailConsumer

func (c *EmailConsumer) Start() error  // 创建独立 Channel、声明队列、绑定、消费循环
func (c *EmailConsumer) Stop()         // close(done) + c.wg.Wait()，等待所有 in-flight handler 完成后返回
```

**需要在 `internal/infra/rabbitmq.go` 新增方法** 以线程安全地从连接创建新 Channel：

```go
// NewChannel 从当前连接创建一个新的独立 Channel（线程安全）
func (r *RabbitMQ) NewChannel() (*amqp.Channel, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    if r.Conn == nil || r.Conn.IsClosed() {
        return nil, fmt.Errorf("rabbitmq connection is not available")
    }
    return r.Conn.Channel()
}
```

**Topology 声明（在每轮 `consumeLoop()` 中完成）：**
1. 通过 `c.mq.NewChannel()` 创建独立 Channel
2. `QueueDeclare("email_queue", durable)` — exchange 已在 infra 层声明，此处只声明队列
3. `QueueBind("email_queue", "email.verification", "email_exchange")`

**MQ 重连后 Consumer 自动重建：**

连接断开时 Consumer 持有的独立 Channel 和 delivery channel 会同时失效。Consumer 通过监听 channel close 事件 + `RabbitMQ.GetReconnectCh()` 广播信号（见 3b 节）实现精确的自动重建——收到重连信号后从新连接创建新的独立 Channel。

**采用外层 `for` 循环而非递归调用：** 递归 `consume()` 在频繁断连时会导致调用栈无限增长。改为迭代模式，断连时 `continue` 外层循环重建消费：

```go
func (c *EmailConsumer) Start() error {
    go c.run()
    return nil
}

func (c *EmailConsumer) run() {
    for {
        // 每轮循环：声明队列、绑定、消费，直到断连后重新进入循环
        if err := c.consumeLoop(); err != nil {
            log.Printf("email consumer: consume error: %v", err)
        }

        // 等待重连信号、停止信号、或超时兜底
        // 注意：如果 consumeLoop 因非断连原因失败（如 NewChannel() 在连接正常时返回错误），
        // ReconnectCh 不会被 close，此时需要 time.After 兜底防止永久阻塞。
        select {
        case <-c.done:
            return
        case <-c.mq.GetReconnectCh():
            log.Println("email consumer: reconnected, rebuilding...")
            continue  // 重新进入 for 循环，重建消费
        case <-time.After(5 * time.Second):
            log.Println("email consumer: retry timeout, attempting to rebuild...")
            continue  // 超时兜底，重试 consumeLoop
        }
    }
}

func (c *EmailConsumer) consumeLoop() error {
    // 1. 创建独立 Channel（不复用共享 SubChannel）
    ch, err := c.mq.NewChannel()
    if err != nil {
        return fmt.Errorf("create channel: %w", err)
    }
    defer ch.Close()

    // 2. 声明队列、绑定
    _, err = ch.QueueDeclare("email_queue", true, false, false, false, nil)
    if err != nil {
        return err
    }
    err = ch.QueueBind("email_queue", "email.verification", "email_exchange", false, nil)
    if err != nil {
        return err
    }

    // 3. 获取 delivery channel（显式 consumer tag 方便调试）
    deliveries, err := ch.Consume("email_queue", "email-consumer", false, false, false, false, nil)
    if err != nil {
        return err
    }

    // 4. 监听 channel close 通知
    closeCh := make(chan *amqp.Error, 1)
    ch.NotifyClose(closeCh)

    for {
        select {
        case <-c.done:
            return nil
        case delivery, ok := <-deliveries:
            if !ok {
                return nil  // delivery channel 关闭，退出等待重连
            }
            c.wg.Add(1)
            go func() {
                defer c.wg.Done()
                c.handleDelivery(delivery)
            }()
        case <-closeCh:
            log.Println("email consumer: channel closed, waiting for reconnect...")
            return nil  // 返回到 run() 的 for 循环，等待重连信号
        }
    }
}
```

**主协程只负责监听，处理交给子协程：** `consumeLoop` 的主 `for-select` 循环专注于从 delivery channel 接收消息和监听断连信号，收到消息后立即 `go c.handleDelivery(delivery)` 在新协程中处理。这样本地重试的退避等待（4s→8s）不会阻塞消息接收。`sync.WaitGroup` 追踪所有 in-flight handler，`Stop()` 中 `close(done)` 后调用 `c.wg.Wait()` 等待全部完成，确保优雅关闭不丢消息。

**为什么用 `GetReconnectCh()` 而非 `time.Sleep`：** `handleReconnect()` 可能因网络抖动经历多轮重试，耗时远超 3 秒；也可能瞬间完成。盲等无法保证时序正确性。`ReconnectCh` 利用 Go `close(channel)` 一对多广播模式，确保 Consumer 在连接就绪后才从新连接创建新 Channel 重建消费，且未来新增更多 Consumer 时同样能监听同一信号，无需额外改动。

**注意：** Consumer 通过 `c.mq.NewChannel()` 创建独立 Channel，不复用共享 `SubChannel`，确保故障隔离（见第 6 节说明）。

**`defer ch.Close()` 与 in-flight handler 的竞态：** `consumeLoop` 退出时（连接断开或收到 `done` 信号），`defer ch.Close()` 会关闭 Channel，但此时可能仍有 in-flight 的 `handleDelivery` goroutine 正在退避等待中。这些 goroutine 随后尝试 `Ack`/`Nack` 时会因 Channel 已关闭而失败。这是**预期行为且安全的**：AMQP 规范保证 Channel 关闭时服务端自动 requeue 所有未 ack 的消息，因此不会丢消息。实现时无需额外处理这个竞态，忽略 `Ack`/`Nack` 的返回错误即可。

**重连后消息可能被重复消费：** 上述竞态的延伸——旧 in-flight handler 未成功 Ack 的消息会被 RabbitMQ requeue，新 `consumeLoop` 会再次消费。结果是**同一封验证码邮件可能发送两次**。对验证码场景这是可接受的 trade-off（收到两封总比一封都没收到好）。如果未来需要严格去重，可在 `handleDelivery` 中通过 Redis `SET NX` 对 `message-id` 做幂等检查，但当前阶段不需要。

**消费逻辑（`handleDelivery` 方法）：**

采用**本地重试**而非 re-publish 回队列。Consumer 在本地最多重试 3 次，指数退避（4s → 8s），全部失败后 Ack 丢弃。

**优点：**
- Consumer 无需访问 PubChannel，职责纯粹（只消费不生产）
- 不用操心 re-publish 失败、header 计数、PubChannel 并发安全等复杂度
- 重试期间消息未 Ack，若进程崩溃 RabbitMQ 自动 requeue，比先 Ack 再 re-publish 更安全

```go
func (c *EmailConsumer) handleDelivery(delivery amqp.Delivery) {
    var msg email.Message  // 使用 pkg/email/message.go 中的共享定义
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
                // 指数退避：4s → 8s（可被优雅关闭中断）
                backoff := time.Duration(4<<i) * time.Second
                select {
                case <-time.After(backoff):
                    continue
                case <-c.done:
                    // 收到停止信号，Nack requeue 让消息回队列，下次启动继续处理
                    delivery.Nack(false, true)
                    return
                }
            }
            // 最后一次重试仍失败
            log.Printf("email send failed after %d retries: to=%s", maxRetries, msg.To)
            delivery.Ack(false)
            return
        }
        // 发送成功
        delivery.Ack(false)
        return
    }
}
```

**关键细节：**
- 优雅关闭时使用 `Nack(false, true)` 而非 `Ack`，让未发送成功的消息回到队列，下次启动继续处理
- 退避等待通过 `select` + `c.done` 实现可中断，`Stop()` 时不会长时间阻塞
- 反序列化失败直接 `Ack` 丢弃（消息格式错误，重试无意义）

### 8. 修改 App 接线 — `internal/app/app.go`

```go
// wireHandlers 中：
emailRepo := repo.NewEmailRepo(mq)
authService := service.NewAuthService(userRepo, emailRepo, emailSender)

// New() 中：
emailSender := &email.Sender{
    Host:     cfg.SMTPHost,
    Port:     cfg.SMTPPort,
    User:     cfg.SMTPUser,
    Password: cfg.SMTPPassword,
    FromName: cfg.SMTPFromName,
}
emailConsumer := consumer.NewEmailConsumer(mq, emailSender)
if err := emailConsumer.Start(); err != nil {
    // cleanup...
    return nil, err
}

// App struct 新增 EmailConsumer 字段

// Close() 中（注意顺序：先停 consumer 再关 MQ 连接）：
if a.EmailConsumer != nil {
    a.EmailConsumer.Stop()  // 1. 先停消费
}
// ... 然后才 a.MQ.Close()  // 2. 再关连接
```

`wireHandlers` 签名需要新增 `emailSender` 参数（AuthService fallback 需要），`mq` 已传入但需额外给 `EmailRepo` 使用。

---

## 邮件模板

模板文件 `pkg/email/template/verification_email.html`，使用 Go `embed` 嵌入（见第 5 节 `pkg/email/template.go`）：

```html
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
</head>
<body style="margin: 0; padding: 0; background-color: #f9f9f9;">
    <div style="max-width: 480px; margin: 0 auto; padding: 32px 24px; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; color: #333;">
        <h2 style="margin: 0 0 16px 0; font-size: 20px; color: #111;">ContextGraph 验证码</h2>
        <p style="margin: 0 0 16px 0; font-size: 14px; line-height: 1.5;">您的验证码为：</p>
        <div style="font-size: 32px; font-weight: bold; letter-spacing: 8px; color: #333; background: #f0f0f0; padding: 16px; text-align: center; border-radius: 8px;">{{.Code}}</div>
        <p style="margin: 16px 0 0 0; font-size: 14px; line-height: 1.5; color: #666;">验证码有效期为 {{.ExpiryMinutes}} 分钟，请勿将验证码透露给他人。</p>
    </div>
</body>
</html>
```

**注意事项：**
- 所有样式均使用 inline `style` 属性，确保在 Gmail、Outlook、Apple Mail 等主流客户端中正确渲染（许多邮件客户端会剥离 `<style>` 标签）
- 使用 `html/template`（非 `text/template`）自动转义变量，防止 XSS
- 模板变量 `{{.Code}}` 和 `{{.ExpiryMinutes}}`，过期时间与 Redis TTL 保持一致（由调用方传入），避免硬编码导致二者不同步。后续如需扩展（如用户名、链接）可继续添加字段

---

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `go.mod` / `go.sum` | 修改 | 新增 `github.com/wneessen/go-mail` 和 `github.com/joho/godotenv` 依赖 |
| `config/config.go` | 修改 | 新增 SMTP 配置字段 + `godotenv.Load()` 加载 `.env` |
| `.env.example` | **新建** | 环境变量模板（提交到 Git） |
| `.gitignore` | 修改 | 确认包含 `.env` |
| `pkg/email/sender.go` | **新建** | SMTP 发送封装（使用 `github.com/wneessen/go-mail`），入口校验 SMTP 凭据 |
| `pkg/email/message.go` | **新建** | `email.Message` 结构体，Publisher 和 Consumer 共用 |
| `pkg/email/template.go` | **新建** | 邮件模板渲染函数 + `embed` 嵌入 |
| `pkg/email/template/verification_email.html` | **新建** | 验证码邮件 HTML 模板（inline style，兼容 Outlook） |
| `internal/infra/rabbitmq.go` | 修改 | `declareTopology()` 重构为 `declareTopologyOn(ch)` 并新增 `email_exchange` 声明；`connect()` 中 topology 声明移到赋值之前；新增 `sync.RWMutex` + `ReconnectCh` 广播机制 + `done` channel + `closeOnce` + `closed` 标志 + Channel getter 方法 + `NewChannel()` 方法；`Close()` 加锁保护 + `sync.Once` 保护 `close(done)` + 通知 `watchConnection` 退出；`DeclareQueue()` 迁移为使用 `GetSubChannel()`；`handleReconnect()` 检查 `closed` 标志防止 Close 后继续重连 |
| `internal/repo/emailRepo.go` | **新建** | publish 邮件任务到 RabbitMQ（通过 `GetPubChannel()` 访问，使用 `email.Message`） |
| `internal/repo/fileRepo.go` | 修改 | `r.mq.PubChannel` → `r.mq.GetPubChannel()`，消除 data race |
| `internal/consumer/email_consumer.go` | **新建** | consume 队列 + 调 SMTP 发送，独立 Channel + 迭代式重连重建，使用 `email.Message` 反序列化 |
| `internal/service/authService.go` | 修改 | 新增 `codeExpiryMinutes` 常量；TODO 替换为 publish 调用 + fallback 同步发送；新增 `emailPublisher` 接口及依赖 |
| `internal/app/app.go` | 修改 | 初始化 Sender、EmailConsumer，接线 |

---

## 新增依赖

```bash
go get github.com/wneessen/go-mail
go get github.com/joho/godotenv
```

- `go-mail`：SMTP 客户端，替代标准库 `net/smtp`，支持 STARTTLS、MIME multipart、附件等
- `godotenv`：本地开发从 `.env` 文件加载环境变量，生产环境无 `.env` 时静默跳过

---

## 部署配置同步

Docker Compose / K8s 部署文件中需新增以下环境变量（与 `.env.example` 对应）：

```yaml
environment:
  SMTP_HOST: smtp.gmail.com
  SMTP_PORT: "587"
  SMTP_USER: ${SMTP_USER}
  SMTP_PASSWORD: ${SMTP_PASSWORD}
  SMTP_FROM_NAME: ContextGraph
```

---

## 测试注意事项

- `NewAuthService` 签名变更后，所有现有测试中构造 `AuthService` 的地方都会编译失败，需同步更新（新增 mock `emailPublisher` 和 `*email.Sender`）
- `emailPublisher` 接口可通过 mock 实现测试 `SendCode` 的 publish 成功 / 失败 / fallback 三条路径
- `email.Sender` 在单测中建议 mock（不依赖真实 SMTP 服务器），可将 `Send` 方法抽为接口后 mock

---

## Gmail SMTP 配置说明

使用前需要：
1. Google 账号开启两步验证
2. 生成「应用专用密码」：Google 账号 → 安全性 → 应用专用密码
3. 环境变量配置示例：
   ```
   SMTP_HOST=smtp.gmail.com
   SMTP_PORT=587
   SMTP_USER=your-email@gmail.com
   SMTP_PASSWORD=xxxx xxxx xxxx xxxx   # 应用专用密码（16位）
   SMTP_FROM_NAME=ContextGraph          # 显示名，地址自动取 SMTP_USER
   ```

---

## 验证方式

1. 启动服务，调用 `POST /auth/send-code` 接口
2. 查看 RabbitMQ 管理面板，确认消息进入 `email_queue` 并被消费
3. 检查收件箱收到验证码邮件
4. 测试 fallback：临时停止 RabbitMQ，调用接口确认邮件仍能同步发送
5. 测试失败重试：临时配错 SMTP 密码，观察本地重试日志（`attempt 1/3`、`attempt 2/3`、`attempt 3/3`，退避间隔 4s→8s），3 次后 Ack 丢弃并输出告警日志
6. 测试 Consumer 重连：重启 RabbitMQ，观察日志依次输出 "channel closed, waiting for reconnect..." → "reconnected, rebuilding..."，确认 Consumer 通过 `ReconnectCh` 信号（而非盲等）自动重建消费
