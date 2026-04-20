package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"claims-system/internal/envx"

	amqp "github.com/rabbitmq/amqp091-go"
)

type QueuePublisher interface {
	PublishWithContext(
		ctx context.Context,
		exchange, key string,
		mandatory, immediate bool,
		msg amqp.Publishing,
	) error
}

// connectQueue opens a connection to RabbitMQ and creates a channel.
// A connection is the TCP link to RabbitMQ.
// A channel is a lightweight session inside that connection.
// You do all your publishing and consuming through the channel.
func connectQueue() (*amqp.Connection, *amqp.Channel) {
	amqpURL := os.Getenv("AMQP_URL")

	if amqpURL == "" {
		slog.Error("AMQP_URL is not set")
		os.Exit(1)
	}

	if !strings.HasPrefix(amqpURL, "amqps://") {
		if envx.RequireAMQPS() {
			slog.Error("AMQP_URL must use amqps:// when REQUIRE_AMQPS=true or ENVIRONMENT=production")
			os.Exit(1)
		}
		slog.Warn("AMQP_URL does not use TLS (amqps://); credentials and messages are transmitted in plaintext — not suitable for production")
	}

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		slog.Error("failed to connect to RabbitMQ", "error", err)
		os.Exit(1)
	}

	ch, err := conn.Channel()
	if err != nil {
		slog.Error("failed to open RabbitMQ channel", "error", err)
		os.Exit(1)
	}

	if err := declareQueueTopology(ch); err != nil {
		slog.Error("failed to declare queue topology", "error", err)
		os.Exit(1)
	}

	slog.Info("RabbitMQ connected")
	return conn, ch
}

func declareQueueTopology(ch *amqp.Channel) error {
	const dlxName = "claims.dlx"

	if err := ch.ExchangeDeclare(dlxName, "direct", true, false, false, false, nil); err != nil {
		return err
	}

	for _, q := range []string{"batch_validation", "batch_normalization", "batch_reconciliation", "report_generation"} {
		dlqName := q + ".dlq"
		if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
			return err
		}
		if err := ch.QueueBind(dlqName, q, dlxName, false, nil); err != nil {
			return err
		}
		if _, err := ch.QueueDeclare(q, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange":    dlxName,
			"x-dead-letter-routing-key": q,
		}); err != nil {
			return err
		}
	}
	return nil
}
