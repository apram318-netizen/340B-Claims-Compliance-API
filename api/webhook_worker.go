package main

import (
	"context"
	"log/slog"
	"math"
	"time"

	"claims-system/internal/database"
	"claims-system/internal/webhook"

	"github.com/jackc/pgx/v5/pgtype"
)

func (apiCfg *apiConfig) runWebhookDeliveryWorker(ctx context.Context) {
	if apiCfg.DB == nil {
		return
	}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			apiCfg.processWebhookBatch(ctx)
		}
	}
}

func (apiCfg *apiConfig) processWebhookBatch(ctx context.Context) {
	rows, err := apiCfg.DB.ListWebhookDeliveriesPending(ctx, 50)
	if err != nil || len(rows) == 0 {
		return
	}
	for _, d := range rows {
		sub, err := apiCfg.DB.GetWebhookSubscriptionByID(ctx, d.SubscriptionID)
		if err != nil || !sub.Active {
			_ = apiCfg.DB.UpdateWebhookDeliveryStatus(ctx, database.UpdateWebhookDeliveryStatusParams{
				ID:           d.ID,
				Status:       "failed",
				AttemptCount: d.AttemptCount + 1,
				LastError:    pgtype.Text{String: "subscription inactive or missing", Valid: true},
				NextRetryAt:  pgtype.Timestamptz{},
			})
			appMetrics.webhookDeliveriesFailed.Add(1)
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		status, err := webhook.PostSigned(sendCtx, sub.Url, sub.SecretHmac, d.Payload)
		cancel()
		attempts := d.AttemptCount + 1
		if err == nil && status >= 200 && status < 300 {
			_ = apiCfg.DB.UpdateWebhookDeliveryStatus(ctx, database.UpdateWebhookDeliveryStatusParams{
				ID:           d.ID,
				Status:       "delivered",
				AttemptCount: attempts,
				LastError:    pgtype.Text{},
				NextRetryAt:  pgtype.Timestamptz{},
			})
			appMetrics.webhookDeliveriesSucceeded.Add(1)
			slog.Info("webhook delivered", "delivery_id", d.ID, "subscription_id", sub.ID, "status_code", status)
			continue
		}
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = "non-2xx status"
		}
		if attempts >= 8 {
			_ = apiCfg.DB.UpdateWebhookDeliveryStatus(ctx, database.UpdateWebhookDeliveryStatusParams{
				ID:           d.ID,
				Status:       "failed",
				AttemptCount: attempts,
				LastError:    pgtype.Text{String: errMsg, Valid: true},
				NextRetryAt:  pgtype.Timestamptz{},
			})
			appMetrics.webhookDeliveriesFailed.Add(1)
			slog.Warn("webhook delivery exhausted", "delivery_id", d.ID, "error", errMsg)
			continue
		}
		backoff := time.Duration(math.Min(300_000, math.Pow(2, float64(attempts))*10_000)) * time.Millisecond
		next := time.Now().UTC().Add(backoff)
		_ = apiCfg.DB.UpdateWebhookDeliveryStatus(ctx, database.UpdateWebhookDeliveryStatusParams{
			ID:           d.ID,
			Status:       "retrying",
			AttemptCount: attempts,
			LastError:    pgtype.Text{String: errMsg, Valid: true},
			NextRetryAt:  pgtype.Timestamptz{Time: next, Valid: true},
		})
		appMetrics.webhookRetries.Add(1)
	}
}
