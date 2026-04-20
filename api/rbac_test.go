package main

import (
	"bytes"
	"claims-system/internal/database"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAdminOnlyEndpointsForbiddenForMember(t *testing.T) {
	memberID := uuid.New()
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
	token, err := createJWT(memberID, apiCfg.JwtSecret)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/manufacturers", `{"name":"Mfg","labeler_code":"12345"}`},
		{http.MethodPost, "/v1/rebate-records", `{"manufacturer_id":"` + uuid.NewString() + `","org_id":"` + orgID.String() + `","ndc":"12345","pharmacy_npi":"9999999999","service_date":"2026-01-01","quantity":1}`},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		testRouter(apiCfg).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s expected 403, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}
