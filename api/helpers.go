package main

import (
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/jackc/pgx/v5/pgtype"
)

func publishMsg(body string) amqp.Publishing {
	return amqp.Publishing{
		ContentType:  "text/plain",
		Body:         []byte(body),
		DeliveryMode: amqp.Persistent,
	}
}

func pgTextFrom(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func pgNumericFrom(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	n.Scan(f)
	return n
}

// parseDateParam parses a "YYYY-MM-DD" string into a pgtype.Date.
func parseDateParam(s string) (pgtype.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}
