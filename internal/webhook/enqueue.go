package webhook

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"claims-system/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// EnqueueForOrg creates delivery rows for every active subscription matching eventType.
func EnqueueForOrg(ctx context.Context, db database.Store, orgID uuid.UUID, eventType string, payload any) {
	subs, err := db.ListWebhookSubscriptionsByOrg(ctx, orgID)
	if err != nil {
		slog.Error("webhook list subscriptions", "error", err, "org_id", orgID)
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook marshal payload", "error", err)
		return
	}
	for _, s := range subs {
		if !s.Active {
			continue
		}
		if !matchEvent(s.EventTypes, eventType) {
			continue
		}
		_, err := db.CreateWebhookDelivery(ctx, database.CreateWebhookDeliveryParams{
			SubscriptionID: s.ID,
			EventType:      eventType,
			Payload:        raw,
			Status:         "pending",
			NextRetryAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		if err != nil {
			slog.Error("webhook create delivery", "error", err, "subscription_id", s.ID)
		}
	}
}

func matchEvent(types []string, event string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if t == event || t == "*" {
			return true
		}
	}
	return false
}

// EventExportCompleted is the event_type for successful exports.
const EventExportCompleted = "export.completed"

// ExportPayload is the JSON envelope for export webhooks.
type ExportPayload struct {
	ExportID   uuid.UUID `json:"export_id"`
	ReportType string    `json:"report_type"`
	OrgID      string    `json:"org_id,omitempty"`
	Status     string    `json:"status"`
}
