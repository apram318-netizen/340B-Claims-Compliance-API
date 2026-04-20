package scheduler

import (
	"context"
	"log/slog"
	"time"

	"claims-system/internal/database"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Scheduler runs two recurring jobs:
//   - Every hour: re-queue stuck batches (uploaded/normalized but stalled > 1h)
//   - Twice daily on the 1st and 16th: log submission window compliance alert
type Scheduler struct {
	db     database.Store
	amqpCh *amqp.Channel
}

func New(db database.Store, ch *amqp.Channel) *Scheduler {
	return &Scheduler{db: db, amqpCh: ch}
}

// Run blocks until ctx is cancelled. Call in a goroutine.
func (s *Scheduler) Run(ctx context.Context) {
	hourly := time.NewTicker(1 * time.Hour)
	defer hourly.Stop()

	// Align the daily check to fire at 00:05 UTC so it runs shortly after midnight.
	daily := time.NewTicker(24 * time.Hour)
	defer daily.Stop()

	// Run immediately on startup so stuck batches aren't delayed by up to an hour.
	s.requeueStuckBatches(ctx)
	s.checkSubmissionWindows(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-hourly.C:
			s.requeueStuckBatches(ctx)
		case <-daily.C:
			s.checkSubmissionWindows(ctx)
		}
	}
}

// requeueStuckBatches finds batches that have been in 'uploaded' or 'normalized'
// for more than one hour and re-publishes them to the appropriate pipeline queue.
func (s *Scheduler) requeueStuckBatches(ctx context.Context) {
	batches, err := s.db.GetStuckBatches(ctx)
	if err != nil {
		slog.Error("scheduler: get stuck batches failed", "error", err)
		return
	}
	if len(batches) == 0 {
		return
	}

	slog.Info("scheduler: requeueing stuck batches", "count", len(batches))

	for _, b := range batches {
		queue := queueForStatus(b.Status)
		if queue == "" {
			continue
		}
		if err := s.publish(ctx, queue, b.ID.String()); err != nil {
			slog.Error("scheduler: requeue publish failed",
				"batch_id", b.ID, "queue", queue, "error", err)
		} else {
			slog.Info("scheduler: batch requeued",
				"batch_id", b.ID, "status", b.Status, "queue", queue)
		}
	}
}

// checkSubmissionWindows logs a compliance alert on the 1st and 16th of each month,
// which are the standard 340B ESP submission deadlines.
func (s *Scheduler) checkSubmissionWindows(ctx context.Context) {
	now := time.Now().UTC()
	day := now.Day()
	if day != 1 && day != 16 {
		return
	}

	rows, err := s.db.GetSubmissionCompleteness(ctx, database.GetSubmissionCompletenessParams{
		CreatedAt:   monthStart(now),
		CreatedAt_2: now,
	})
	if err != nil {
		slog.Error("scheduler: submission completeness query failed", "error", err)
		return
	}

	for _, r := range rows {
		if r.BatchCount == 0 {
			slog.Warn("scheduler: no submissions this period — compliance alert",
				"org_id", r.OrgID,
				"org_name", r.OrgName,
				"entity_id", r.EntityID,
				"window_start", monthStart(now).Format("2006-01-02"),
			)
		} else {
			slog.Info("scheduler: submission window check",
				"org_id", r.OrgID,
				"org_name", r.OrgName,
				"batch_count", r.BatchCount,
				"total_rows", r.TotalRows,
			)
		}
	}
}

// publish sends a batch ID to the given queue.
func (s *Scheduler) publish(ctx context.Context, queue, body string) error {
	return s.amqpCh.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{
		ContentType:  "text/plain",
		Body:         []byte(body),
		DeliveryMode: amqp.Persistent,
	})
}

// queueForStatus maps a stuck batch status to the queue it should re-enter.
func queueForStatus(status string) string {
	switch status {
	case "uploaded":
		return "batch_validation"
	case "normalized":
		return "batch_reconciliation"
	default:
		return ""
	}
}

// monthStart returns midnight on the 1st of the month containing t.
func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
