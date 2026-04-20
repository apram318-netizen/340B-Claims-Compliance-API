package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicEndpoints(t *testing.T) {
	apiCfg := apiConfig{
		JwtSecret: "test-secret-test-secret-test-secret",
	}
	router := testRouter(apiCfg)

	t.Run("health", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
	})

	t.Run("register invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/register", bytes.NewBufferString("{"))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("login missing fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/login", bytes.NewBufferString(`{"email":""}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestProtectedEndpointsRequireAuth(t *testing.T) {
	apiCfg := apiConfig{
		JwtSecret: "test-secret-test-secret-test-secret",
	}
	router := testRouter(apiCfg)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/organizations", `{}`},
		{http.MethodGet, "/v1/organizations/00000000-0000-0000-0000-000000000001", ""},
		{http.MethodPost, "/v1/batches", `{}`},
		{http.MethodPost, "/v1/batches/upload", ""},
		{http.MethodGet, "/v1/batches/00000000-0000-0000-0000-000000000001", ""},
		{http.MethodGet, "/v1/batches/00000000-0000-0000-0000-000000000001/claims", ""},
		{http.MethodGet, "/v1/batches/00000000-0000-0000-0000-000000000001/reconciliation-job", ""},
		{http.MethodGet, "/v1/claims/00000000-0000-0000-0000-000000000001", ""},
		{http.MethodGet, "/v1/claims/00000000-0000-0000-0000-000000000001/decision", ""},
		{http.MethodPost, "/v1/manufacturers", `{}`},
		{http.MethodGet, "/v1/manufacturers", ""},
		{http.MethodGet, "/v1/manufacturers/00000000-0000-0000-0000-000000000001", ""},
		{http.MethodPost, "/v1/manufacturers/00000000-0000-0000-0000-000000000001/products", `{}`},
		{http.MethodGet, "/v1/manufacturers/00000000-0000-0000-0000-000000000001/products", ""},
		{http.MethodPost, "/v1/manufacturers/00000000-0000-0000-0000-000000000001/policies", `{}`},
		{http.MethodGet, "/v1/manufacturers/00000000-0000-0000-0000-000000000001/policies", ""},
		{http.MethodPost, "/v1/policies/00000000-0000-0000-0000-000000000001/versions", `{}`},
		{http.MethodPost, "/v1/policy-versions/00000000-0000-0000-0000-000000000001/rules", `{}`},
		{http.MethodGet, "/v1/policy-versions/00000000-0000-0000-0000-000000000001/rules", ""},
		{http.MethodPost, "/v1/rebate-records", `{}`},
		{http.MethodGet, "/v1/reconciliation-jobs/00000000-0000-0000-0000-000000000001", ""},
		{http.MethodGet, "/v1/reconciliation-jobs/00000000-0000-0000-0000-000000000001/decisions", ""},
		{http.MethodPost, "/v1/exports", `{}`},
		{http.MethodGet, "/v1/exports/00000000-0000-0000-0000-000000000001", ""},
		{http.MethodGet, "/v1/exports/00000000-0000-0000-0000-000000000001/download", ""},
		{http.MethodPost, "/v1/exports/00000000-0000-0000-0000-000000000001/retry", ""},
		{http.MethodPost, "/v1/claims/00000000-0000-0000-0000-000000000001/override", `{}`},
		{http.MethodGet, "/v1/audit-events?entity_type=batch&entity_id=1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
		})
	}
}
