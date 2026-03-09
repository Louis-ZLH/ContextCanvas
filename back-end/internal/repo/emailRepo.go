package repo

import (
	"context"
	"encoding/json"

	"github.com/luhao/contextGraph/internal/infra"
	"github.com/luhao/contextGraph/pkg/email"
	amqp "github.com/rabbitmq/amqp091-go"
)

type EmailRepo struct {
	mq *infra.RabbitMQ
}

func NewEmailRepo(mq *infra.RabbitMQ) *EmailRepo {
	return &EmailRepo{mq: mq}
}

func (r *EmailRepo) PublishEmailTask(ctx context.Context, to, subject, htmlBody string) error {
	msg := email.Message{
		To:      to,
		Subject: subject,
		Body:    htmlBody,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return r.mq.GetPubChannel().PublishWithContext(
		ctx,
		"email_exchange",
		"email.verification",
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         payload,
		},
	)
}
