package main

import (
	"claims-system/internal/database"
	"claims-system/internal/feature"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func randomWebhookSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// POST /v1/organizations/{id}/webhooks
func (apiCfg *apiConfig) handlerCreateWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	if apiCfg.Features == nil || !apiCfg.Features.Enabled(r.Context(), orgID, feature.Webhooks) {
		respondWithError(w, http.StatusNotFound, "feature not enabled")
		return
	}
	var body struct {
		URL        string   `json:"url"`
		EventTypes []string `json:"event_types"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{{Field: "url", Message: "is required"}})
		return
	}
	sub, err := apiCfg.DB.CreateWebhookSubscription(r.Context(), database.CreateWebhookSubscriptionParams{
		OrgID:      orgID,
		Url:        strings.TrimSpace(body.URL),
		SecretHmac: randomWebhookSecret(),
		EventTypes: body.EventTypes,
		Active:     true,
	})
	if err != nil {
		slog.Error("create webhook", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	apiCfg.recordAuditEventBestEffort(r, "webhook_created", "webhook", sub.ID.String(), mustUserID(r), map[string]string{"ip": clientIP(r)})
	respondWithJSON(w, http.StatusCreated, webhookSubscriptionToJSON(sub, true))
}

func mustUserID(r *http.Request) uuid.UUID {
	id, _ := getUserId(r)
	return id
}

func webhookSubscriptionToJSON(s database.WebhookSubscription, includeSecret bool) map[string]any {
	m := map[string]any{
		"id":          s.ID,
		"org_id":      s.OrgID,
		"url":         s.Url,
		"event_types": s.EventTypes,
		"active":      s.Active,
		"created_at":  s.CreatedAt,
		"updated_at":  s.UpdatedAt,
	}
	if includeSecret {
		m["secret"] = s.SecretHmac
	}
	return m
}

// GET /v1/organizations/{id}/webhooks
func (apiCfg *apiConfig) handlerListWebhooks(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	if apiCfg.Features == nil || !apiCfg.Features.Enabled(r.Context(), orgID, feature.Webhooks) {
		respondWithJSON(w, http.StatusOK, map[string]any{"webhooks": []any{}})
		return
	}
	subs, err := apiCfg.DB.ListWebhookSubscriptionsByOrg(r.Context(), orgID)
	if err != nil {
		slog.Error("list webhooks", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(subs))
	for _, s := range subs {
		out = append(out, webhookSubscriptionToJSON(s, false))
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"webhooks": out})
}

// PATCH /v1/organizations/{id}/webhooks/{webhookId}
func (apiCfg *apiConfig) handlerPatchWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, _ := uuid.Parse(chi.URLParam(r, "id"))
	wid, err := uuid.Parse(chi.URLParam(r, "webhookId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	var body struct {
		URL        *string  `json:"url"`
		EventTypes []string `json:"event_types"`
		Active     *bool    `json:"active"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	existing, err := apiCfg.DB.GetWebhookSubscription(r.Context(), database.GetWebhookSubscriptionParams{ID: wid, OrgID: orgID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		slog.Error("get webhook", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	url := existing.Url
	if body.URL != nil {
		url = strings.TrimSpace(*body.URL)
	}
	ev := existing.EventTypes
	if body.EventTypes != nil {
		ev = body.EventTypes
	}
	active := existing.Active
	if body.Active != nil {
		active = *body.Active
	}
	sub, err := apiCfg.DB.UpdateWebhookSubscription(r.Context(), database.UpdateWebhookSubscriptionParams{
		ID:         wid,
		OrgID:      orgID,
		Url:        url,
		EventTypes: ev,
		Active:     active,
	})
	if err != nil {
		slog.Error("update webhook", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusOK, webhookSubscriptionToJSON(sub, false))
}

// DELETE /v1/organizations/{id}/webhooks/{webhookId}
func (apiCfg *apiConfig) handlerDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, _ := uuid.Parse(chi.URLParam(r, "id"))
	wid, err := uuid.Parse(chi.URLParam(r, "webhookId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	if err := apiCfg.DB.DeleteWebhookSubscription(r.Context(), database.DeleteWebhookSubscriptionParams{ID: wid, OrgID: orgID}); err != nil {
		slog.Error("delete webhook", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/organizations/{id}/webhooks/{webhookId}/deliveries
func (apiCfg *apiConfig) handlerListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	orgID, _ := uuid.Parse(chi.URLParam(r, "id"))
	wid, err := uuid.Parse(chi.URLParam(r, "webhookId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	sub, err := apiCfg.DB.GetWebhookSubscription(r.Context(), database.GetWebhookSubscriptionParams{ID: wid, OrgID: orgID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = sub
	rows, err := apiCfg.DB.ListWebhookDeliveriesBySubscription(r.Context(), database.ListWebhookDeliveriesBySubscriptionParams{
		SubscriptionID: wid,
		Limit:            100,
	})
	if err != nil {
		slog.Error("list deliveries", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		out = append(out, map[string]any{
			"id":             d.ID,
			"event_type":     d.EventType,
			"status":         d.Status,
			"attempt_count":  d.AttemptCount,
			"last_error":     d.LastError,
			"created_at":     d.CreatedAt,
			"payload":        json.RawMessage(d.Payload),
		})
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"deliveries": out})
}
