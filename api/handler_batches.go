package main

import (
	"claims-system/internal/database"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	amqp091 "github.com/rabbitmq/amqp091-go"
)

func (apiCfg *apiConfig) handlerCreateBatch(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	type requestBody struct {
		FileName string                   `json:"file_name"`
		Rows     []map[string]interface{} `json:"rows"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	if body.FileName == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "file_name", Message: "is required"},
		})
		return
	}
	if len(body.Rows) == 0 {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "rows", Message: "must not be empty"},
		})
		return
	}

	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	batch, err := apiCfg.DB.CreateUploadBatches(r.Context(), database.CreateUploadBatchesParams{
		OrgID:      user.OrgID,
		UploadedBy: userID,
		FileName:   body.FileName,
		RowCount:   int32(len(body.Rows)),
		Status:     "uploaded",
	})
	if err != nil {
		slog.Error("create batch failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create batch")
		return
	}

	for i, row := range body.Rows {
		rawJSON, err := json.Marshal(row)
		if err != nil {
			slog.Error("marshal row failed", "row", i+1, "error", err)
			respondWithError(w, http.StatusInternalServerError, "failed to encode row data")
			return
		}

		if _, err = apiCfg.DB.CreateUploadRow(r.Context(), database.CreateUploadRowParams{
			BatchID:   batch.ID,
			RowNumber: int32(i + 1),
			Data:      rawJSON,
		}); err != nil {
			slog.Error("save row failed", "row", i+1, "error", err)
			respondWithError(w, http.StatusInternalServerError, "failed to save row")
			return
		}
	}

	if _, err = apiCfg.DB.CreateAuditEvent(r.Context(), database.CreateAuditEventParams{
		EventType:  "batch_uploaded",
		EntityType: "batch",
		EntityID:   batch.ID.String(),
		ActorID:    pgtype.UUID{Bytes: userID, Valid: true},
		Payload:    nil,
	}); err != nil {
		// Audit failure is logged but does not fail the request
		slog.Error("audit event failed", "batch_id", batch.ID, "error", err)
	}

	if err = apiCfg.Queue.PublishWithContext(
		r.Context(), "", "batch_validation", false, false,
		amqp091.Publishing{
			ContentType:  "text/plain",
			Body:         []byte(batch.ID.String()),
			DeliveryMode: amqp091.Persistent,
		},
	); err != nil {
		slog.Error("queue publish failed", "batch_id", batch.ID, "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to queue batch for processing")
		return
	}

	respondWithJSON(w, http.StatusCreated, databaseBatchToBatch(batch))
}

func (apiCfg *apiConfig) handlerGetBatch(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid batch id")
		return
	}

	batch, err := apiCfg.DB.GetUploadBatches(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "batch not found")
			return
		}
		slog.Error("get batch failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Tenant isolation: only the owning org may read this batch
	userID, _ := getUserId(r)
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil || user.OrgID != batch.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	respondWithJSON(w, http.StatusOK, databaseBatchToBatch(batch))
}
