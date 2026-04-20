//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestEndToEndPipelineWithLiveInfra(t *testing.T) {
	root := repoRoot(t)
	adminURL := getenvDefault("INTEGRATION_POSTGRES_ADMIN_URL", "postgres://claims:claims@localhost:5432/postgres?sslmode=disable")
	amqpURL := getenvDefault("AMQP_URL", "amqp://guest:guest@localhost:5672/")
	port := getenvDefault("INTEGRATION_API_PORT", "18080")

	dbName := fmt.Sprintf("claims_it_%d", time.Now().UnixNano())
	dbURL, cleanupDB := createEphemeralDB(t, adminURL, dbName)
	defer cleanupDB()

	applyMigrations(t, root, dbURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	apiCmd := exec.CommandContext(ctx, "go", "run", "./api/...")
	apiCmd.Dir = root
	apiCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	apiCmd.Env = append(os.Environ(),
		"PORT="+port,
		"DATABASE_URL="+dbURL,
		"AMQP_URL="+amqpURL,
		"JWT_SECRET=integration-secret-integration-secret-12345",
		"ALLOWED_ORIGIN=http://localhost:3000",
		"PASSWORD_RESET_EXPOSE_TOKEN=true",
		"API_REDACT_SENSITIVE_FIELDS=false",
		"EXPORT_REDACT_SENSITIVE_FIELDS=false",
	)
	apiCmd.Stdout = os.Stdout
	apiCmd.Stderr = os.Stderr
	if err := apiCmd.Start(); err != nil {
		t.Fatalf("start api: %v", err)
	}
	defer killProcess(apiCmd)

	workerCmd := exec.CommandContext(ctx, "go", "run", "./worker/...")
	workerCmd.Dir = root
	workerCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	workerCmd.Env = append(os.Environ(),
		"DATABASE_URL="+dbURL,
		"AMQP_URL="+amqpURL,
		"WORKER_METRICS_PORT=19091",
	)
	workerCmd.Stdout = os.Stdout
	workerCmd.Stderr = os.Stderr
	if err := workerCmd.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	defer killProcess(workerCmd)

	baseURL := "http://localhost:" + port
	waitForHTTP(t, baseURL+"/health", 45*time.Second)

	orgID := seedOrg(t, dbURL)
	email := fmt.Sprintf("it_%d@example.com", time.Now().UnixNano())
	password := "Password1!"

	registerBody := map[string]any{
		"org_id":   orgID,
		"email":    email,
		"name":     "Integration User",
		"password": password,
	}
	resp := doJSON(t, http.MethodPost, baseURL+"/v1/register", "", registerBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register expected 201 got %d", resp.StatusCode)
	}

	loginResp := doJSON(t, http.MethodPost, baseURL+"/v1/login", "", map[string]any{
		"email": email, "password": password,
	})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login expected 200 got %d", loginResp.StatusCode)
	}
	var loginPayload struct {
		Token string `json:"token"`
	}
	decodeBody(t, loginResp.Body, &loginPayload)
	if loginPayload.Token == "" {
		t.Fatal("expected token in login response")
	}

	uploadURL := baseURL + "/v1/batches/upload"
	batchResp := doMultipartCSV(t, uploadURL, loginPayload.Token, "claims.csv",
		"ndc,pharmacy_npi,service_date,quantity,payer_type\n1234567890,9999999999,2026-01-01,1,commercial\n")
	if batchResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload expected 201 got %d", batchResp.StatusCode)
	}
	var batchPayload struct {
		ID string `json:"id"`
	}
	decodeBody(t, batchResp.Body, &batchPayload)
	if batchPayload.ID == "" {
		t.Fatal("expected batch id in upload response")
	}

	jobID := pollForBatchJob(t, baseURL, loginPayload.Token, batchPayload.ID, 60*time.Second)
	decisionsResp := doJSON(t, http.MethodGet, baseURL+"/v1/reconciliation-jobs/"+jobID+"/decisions", loginPayload.Token, nil)
	if decisionsResp.StatusCode != http.StatusOK {
		t.Fatalf("job decisions expected 200 got %d", decisionsResp.StatusCode)
	}

	exportResp := doJSON(t, http.MethodPost, baseURL+"/v1/exports", loginPayload.Token, map[string]any{
		"report_type": "exceptions",
		"from_date":   "2020-01-01",
		"to_date":     "2030-01-01",
	})
	if exportResp.StatusCode != http.StatusCreated {
		t.Fatalf("create export expected 201 got %d", exportResp.StatusCode)
	}
	var exportPayload struct {
		ID string `json:"id"`
	}
	decodeBody(t, exportResp.Body, &exportPayload)
	if exportPayload.ID == "" {
		t.Fatal("expected export id")
	}

	waitForExportCompletion(t, baseURL, loginPayload.Token, exportPayload.ID, 60*time.Second)

	downloadResp := doJSON(t, http.MethodGet, baseURL+"/v1/exports/"+exportPayload.ID+"/download", loginPayload.Token, nil)
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("download export expected 200 got %d", downloadResp.StatusCode)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	return filepath.Dir(wd)
}

func getenvDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func createEphemeralDB(t *testing.T, adminURL, dbName string) (string, func()) {
	t.Helper()
	db, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if _, err := db.Exec(`CREATE DATABASE ` + dbName); err != nil {
		t.Fatalf("create database: %v", err)
	}

	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	parsed.Path = "/" + dbName
	dbURL := parsed.String()

	cleanup := func() {
		_, _ = db.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbName)
		_, _ = db.Exec(`DROP DATABASE IF EXISTS ` + dbName)
		_ = db.Close()
	}
	return dbURL, cleanup
}

func applyMigrations(t *testing.T, root, dbURL string) {
	t.Helper()
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()

	paths, err := filepath.Glob(filepath.Join(root, "sql", "schema", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(paths)
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read migration %s: %v", p, err)
		}
		up := upSection(string(raw))
		if strings.TrimSpace(up) == "" {
			continue
		}
		if _, err := db.Exec(up); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(p), err)
		}
	}
}

func upSection(contents string) string {
	upMarker := "-- +goose Up"
	downMarker := "-- +goose Down"
	start := strings.Index(contents, upMarker)
	if start == -1 {
		return ""
	}
	body := contents[start+len(upMarker):]
	end := strings.Index(body, downMarker)
	if end == -1 {
		return strings.TrimSpace(body)
	}
	return strings.TrimSpace(body[:end])
}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	}
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
		if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = cmd.Process.Kill()
		}
		<-done
	}
}

func waitForHTTP(t *testing.T, target string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(target)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", target)
}

func seedOrg(t *testing.T, dbURL string) string {
	t.Helper()
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open db for seed: %v", err)
	}
	defer db.Close()

	id := uuid.New().String()
	entityID := fmt.Sprintf("ENT-%d", time.Now().UnixNano())
	_, err = db.Exec(`INSERT INTO organizations (id, name, entity_id) VALUES ($1, $2, $3)`, id, "Integration Org", entityID)
	if err != nil {
		t.Fatalf("seed org: %v", err)
	}
	return id
}

func doJSON(t *testing.T, method, target, token string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, target, err)
	}
	return resp
}

func doMultipartCSV(t *testing.T, target, token, filename, csvContent string) *http.Response {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write([]byte(csvContent))
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, target, &b)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do multipart request: %v", err)
	}
	return resp
}

func decodeBody(t *testing.T, rc io.ReadCloser, out any) {
	t.Helper()
	defer rc.Close()
	if err := json.NewDecoder(rc).Decode(out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func pollForBatchJob(t *testing.T, baseURL, token, batchID string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := doJSON(t, http.MethodGet, baseURL+"/v1/batches/"+batchID+"/reconciliation-job", token, nil)
		if resp.StatusCode == http.StatusOK {
			var job struct {
				ID string `json:"id"`
			}
			decodeBody(t, resp.Body, &job)
			if job.ID != "" {
				return job.ID
			}
		} else {
			_ = resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting for reconciliation job for batch %s", batchID)
	return ""
}

func waitForExportCompletion(t *testing.T, baseURL, token, exportID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := doJSON(t, http.MethodGet, baseURL+"/v1/exports/"+exportID, token, nil)
		if resp.StatusCode == http.StatusOK {
			var run struct {
				Status string `json:"status"`
			}
			decodeBody(t, resp.Body, &run)
			if run.Status == "completed" {
				return
			}
			if run.Status == "failed" {
				t.Fatalf("export %s failed", exportID)
			}
		} else {
			_ = resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting for export completion: %s", exportID)
}
