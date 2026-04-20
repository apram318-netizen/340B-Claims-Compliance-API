package main

import (
	"claims-system/internal/database"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestListManufacturersPagination(t *testing.T) {
	userID := uuid.New()
	orgID := uuid.New()
	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, orgID, "admin"), nil
		},
		ListManufacturersFn: func(_ context.Context) ([]database.Manufacturer, error) {
			return []database.Manufacturer{
				{ID: uuid.New(), Name: "m1", LabelerCode: "11111"},
				{ID: uuid.New(), Name: "m2", LabelerCode: "22222"},
				{ID: uuid.New(), Name: "m3", LabelerCode: "33333"},
			}, nil
		},
	}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
	}
	token, err := createJWT(userID, apiCfg.JwtSecret)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/manufacturers?limit=2&offset=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	testRouter(apiCfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Total-Count") != "3" {
		t.Fatalf("expected X-Total-Count=3, got %s", rec.Header().Get("X-Total-Count"))
	}

	var out []database.Manufacturer
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 manufacturers, got %d", len(out))
	}
}
