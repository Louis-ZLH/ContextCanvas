package email

import (
	"fmt"
	"net/mail"
	"strconv"

	gomail "github.com/wneessen/go-mail"
)

type Sender struct {
	Host     string
	Port     string
	User     string
	Password string
	FromName string
}

func (s *Sender) Send(to, subject, body string) error {
	if s.User == "" || s.Password == "" {
		return fmt.Errorf("SMTP credentials not configured (SMTP_USER / SMTP_PASSWORD)")
	}

	from := mail.Address{Name: s.FromName, Address: s.User}

	msg := gomail.NewMsg()
	if err := msg.FromFormat(from.Name, from.Address); err != nil {
		return fmt.Errorf("set from: %w", err)
	}
	if err := msg.To(to); err != nil {
		return fmt.Errorf("set to: %w", err)
	}
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextHTML, body)

	port, err := strconv.Atoi(s.Port)
	if err != nil {
		return fmt.Errorf("invalid SMTP port %q: %w", s.Port, err)
	}

	client, err := gomail.NewClient(s.Host,
		gomail.WithPort(port),
		gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
		gomail.WithUsername(s.User),
		gomail.WithPassword(s.Password),
		gomail.WithTLSPortPolicy(gomail.TLSMandatory),
	)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}

	if err := client.DialAndSend(msg); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}
