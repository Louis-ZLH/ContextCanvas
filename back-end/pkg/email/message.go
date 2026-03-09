package email

// Message 是 RabbitMQ 中邮件任务的消息体，Publisher 和 Consumer 共用。
type Message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}
