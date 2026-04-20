package main

import (
	"bytes"
	"claims-system/internal/database"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/xuri/excelize/v2"
)

func TestUploadBatchCSVSuccess(t *testing.T) {
	userID := uuid.New()
	orgID := uuid.New()
	batchID := uuid.New()

	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, orgID, "member"), nil
		},
		CreateUploadBatchesFn: func(_ context.Context, arg database.CreateUploadBatchesParams) (database.UploadBatch, error) {
			return database.UploadBatch{
				ID:         batchID,
				OrgID:      arg.OrgID,
				UploadedBy: arg.UploadedBy,
				FileName:   arg.FileName,
				RowCount:   arg.RowCount,
				Status:     arg.Status,
				CreatedAt:  time.Now(),
			}, nil
		},
		CreateUploadRowFn: func(_ context.Context, arg database.CreateUploadRowParams) (database.UploadRow, error) {
			return database.UploadRow{
				ID:        uuid.New(),
				BatchID:   arg.BatchID,
				RowNumber: arg.RowNumber,
				Data:      arg.Data,
				CreatedAt: time.Now(),
			}, nil
		},
		CreateAuditEventFn: func(_ context.Context, arg database.CreateAuditEventParams) (database.AuditEvent, error) {
			return database.AuditEvent{
				ID:         uuid.New(),
				EventType:  arg.EventType,
				EntityType: arg.EntityType,
				EntityID:   arg.EntityID,
				ActorID:    arg.ActorID,
				Payload:    arg.Payload,
				CreatedAt:  time.Now(),
			}, nil
		},
	}
	queue := &mockQueue{}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     queue,
		JwtSecret: "test-secret-test-secret-test-secret",
	}

	body, contentType := makeMultipartFile(t, "claims.csv", []byte(
		"ndc,pharmacy_npi,service_date,quantity,hashed_rx_key,payer_type\n"+
			"12345,9999999999,2026-01-01,1,abc,commercial\n",
	))
	req := httptest.NewRequest(http.MethodPost, "/v1/batches/upload", body)
	req.Header.Set("Content-Type", contentType)
	token, err := createJWT(userID, apiCfg.JwtSecret)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	testRouter(apiCfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(queue.published) != 1 {
		t.Fatalf("expected one queue publish, got %d", len(queue.published))
	}
}

func TestUploadBatchExcelSuccess(t *testing.T) {
	userID := uuid.New()
	orgID := uuid.New()

	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, orgID, "member"), nil
		},
		CreateUploadBatchesFn: func(_ context.Context, arg database.CreateUploadBatchesParams) (database.UploadBatch, error) {
			return database.UploadBatch{ID: uuid.New(), OrgID: arg.OrgID, UploadedBy: arg.UploadedBy, FileName: arg.FileName, RowCount: arg.RowCount, Status: arg.Status}, nil
		},
		CreateUploadRowFn: func(_ context.Context, arg database.CreateUploadRowParams) (database.UploadRow, error) {
			return database.UploadRow{ID: uuid.New(), BatchID: arg.BatchID, RowNumber: arg.RowNumber, Data: arg.Data}, nil
		},
		CreateAuditEventFn: func(_ context.Context, _ database.CreateAuditEventParams) (database.AuditEvent, error) {
			return database.AuditEvent{ID: uuid.New(), ActorID: pgtype.UUID{Valid: true}}, nil
		},
	}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
	}

	file := excelize.NewFile()
	sheet := file.GetSheetName(0)
	file.SetSheetRow(sheet, "A1", &[]interface{}{"ndc", "pharmacy_npi", "service_date", "quantity"})
	file.SetSheetRow(sheet, "A2", &[]interface{}{"12345", "9999999999", "2026-01-01", "2"})
	var xlsx bytes.Buffer
	if err := file.Write(&xlsx); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}

	body, contentType := makeMultipartFile(t, "claims.xlsx", xlsx.Bytes())
	req := httptest.NewRequest(http.MethodPost, "/v1/batches/upload", body)
	req.Header.Set("Content-Type", contentType)
	token, _ := createJWT(userID, apiCfg.JwtSecret)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	testRouter(apiCfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadBatchRejectsMissingHeaders(t *testing.T) {
	userID := uuid.New()
	orgID := uuid.New()
	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, orgID, "member"), nil
		},
	}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
	}

	body, contentType := makeMultipartFile(t, "claims.csv", []byte("ndc,quantity\n123,1\n"))
	req := httptest.NewRequest(http.MethodPost, "/v1/batches/upload", body)
	req.Header.Set("Content-Type", contentType)
	token, _ := createJWT(userID, apiCfg.JwtSecret)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	testRouter(apiCfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(out["error"], "missing required columns") {
		t.Fatalf("unexpected error message: %s", out["error"])
	}
}

func makeMultipartFile(t *testing.T, filename string, data []byte) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return &body, writer.FormDataContentType()
}
