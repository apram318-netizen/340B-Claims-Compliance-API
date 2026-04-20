package main

import (
	"claims-system/internal/crypto"
	"claims-system/internal/database"
	"claims-system/internal/feature"
	"claims-system/internal/envx"
	"claims-system/internal/policy"
	"claims-system/internal/reconciliation"
	"claims-system/internal/reporting"
	"claims-system/internal/scheduler"
	"claims-system/internal/storage"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultAMQPReconnectInitialBackoff = 2 * time.Second
	defaultAMQPReconnectMaxBackoff     = 60 * time.Second
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, relying on environment variables")
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	startWorkerMetricsServer()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL not set")
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		slog.Error("db pool failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	db := database.New(pool)
	policyEngine := policy.NewEngine(db)
	reconciliationEngine := reconciliation.NewEngine(db, policyEngine)

	var exportStorage storage.ExportStorage
	if bucket := os.Getenv("S3_EXPORT_BUCKET"); bucket != "" {
		s3Store, err := storage.NewS3Storage(context.Background(), bucket, os.Getenv("S3_EXPORT_PREFIX"))
		if err != nil {
			slog.Error("failed to init S3 storage", "error", err)
			os.Exit(1)
		}
		exportStorage = s3Store
	} else {
		exportDir := os.Getenv("EXPORT_DIR")
		if exportDir == "" {
			exportDir = "./exports"
		}
		ls, err := storage.NewLocalStorage(exportDir)
		if err != nil {
			slog.Error("failed to init local export storage", "error", err)
			os.Exit(1)
		}
		exportStorage = ls
	}
	reportGenerator := reporting.NewGenerator(db, exportStorage, feature.NewResolver(db))

	amqpURL := os.Getenv("AMQP_URL")
	if amqpURL == "" {
		slog.Error("AMQP_URL not set")
		os.Exit(1)
	}
	if !strings.HasPrefix(amqpURL, "amqps://") {
		if envx.RequireAMQPS() {
			slog.Error("AMQP_URL must use amqps:// when REQUIRE_AMQPS=true or ENVIRONMENT=production")
			os.Exit(1)
		}
		slog.Warn("AMQP_URL does not use TLS (amqps://); credentials and messages are transmitted in plaintext — not suitable for production")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		cancel()
	}()

	slog.Info("worker started, waiting for messages")
	runWorkerLoop(ctx, db, reconciliationEngine, reportGenerator, amqpURL)
	workerReady.Store(false)
	slog.Info("worker shutting down cleanly")
}

func runWorkerLoop(
	ctx context.Context,
	db *database.Queries,
	reconciliationEngine *reconciliation.Engine,
	reportGenerator *reporting.Generator,
	amqpURL string,
) {
	backoff := initialReconnectBackoff()
	for {
		err := runWorkerSession(ctx, db, reconciliationEngine, reportGenerator, amqpURL)
		if ctx.Err() != nil {
			return
		}
		workerReady.Store(false)
		slog.Warn("worker session ended; reconnecting with backoff", "error", err, "backoff", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = nextReconnectBackoff(backoff)
	}
}

func runWorkerSession(
	ctx context.Context,
	db *database.Queries,
	reconciliationEngine *reconciliation.Engine,
	reportGenerator *reporting.Generator,
	amqpURL string,
) error {
	conn, err := dialAMQPWithRetry(ctx, amqpURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	// One channel per consumer — RabbitMQ best practice
	validationCh, err := conn.Channel()
	if err != nil {
		return err
	}
	normalizationCh, err := conn.Channel()
	if err != nil {
		return err
	}
	reconciliationCh, err := conn.Channel()
	if err != nil {
		return err
	}
	reportCh, err := conn.Channel()
	if err != nil {
		return err
	}
	schedulerCh, err := conn.Channel()
	if err != nil {
		return err
	}
	defer validationCh.Close()
	defer normalizationCh.Close()
	defer reconciliationCh.Close()
	defer reportCh.Close()
	defer schedulerCh.Close()

	if err := validationCh.Qos(1, 0, false); err != nil {
		return err
	}
	if err := normalizationCh.Qos(1, 0, false); err != nil {
		return err
	}
	if err := reconciliationCh.Qos(1, 0, false); err != nil {
		return err
	}
	if err := reportCh.Qos(1, 0, false); err != nil {
		return err
	}

	if err := declareQueues(validationCh); err != nil {
		return err
	}
	validationMsgs, err := validationCh.Consume("batch_validation", "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	normalizationMsgs, err := normalizationCh.Consume("batch_normalization", "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	reconciliationMsgs, err := reconciliationCh.Consume("batch_reconciliation", "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	reportMsgs, err := reportCh.Consume("report_generation", "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	workerReady.Store(true)

	// ── Scheduler ─────────────────────────────────────────────────────────────
	sched := scheduler.New(db, schedulerCh)
	go sched.Run(sessionCtx)

	// ── Validation worker ─────────────────────────────────────────────────────
	go func() {
		for msg := range validationMsgs {
			start := time.Now()
			workerMetrics.incReceived("validation")
			workerInFlight.Add(1)
			batchID, err := uuid.Parse(string(msg.Body))
			if err != nil {
				slog.Error("invalid batch id", "value", string(msg.Body))
				workerMetrics.incFailure("validation", "invalid_message")
				workerMetrics.addProcessingMS("validation", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			if err := processBatch(sessionCtx, db, batchID, normalizationCh); err != nil {
				slog.Error("validation failed", "batch_id", batchID, "error", err)
				workerMetrics.incFailure("validation", "processing_error")
				workerMetrics.addProcessingMS("validation", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			slog.Info("batch validated", "batch_id", batchID)
			workerMetrics.incSuccess("validation")
			workerMetrics.addProcessingMS("validation", time.Since(start).Milliseconds())
			workerInFlight.Add(-1)
			msg.Ack(false)
		}
	}()

	// ── Normalization worker ──────────────────────────────────────────────────
	go func() {
		for msg := range normalizationMsgs {
			start := time.Now()
			workerMetrics.incReceived("normalization")
			workerInFlight.Add(1)
			batchID, err := uuid.Parse(string(msg.Body))
			if err != nil {
				slog.Error("invalid batch id", "value", string(msg.Body))
				workerMetrics.incFailure("normalization", "invalid_message")
				workerMetrics.addProcessingMS("normalization", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			if err := normalizeBatch(sessionCtx, db, batchID, reconciliationCh); err != nil {
				slog.Error("normalization failed", "batch_id", batchID, "error", err)
				workerMetrics.incFailure("normalization", "processing_error")
				workerMetrics.addProcessingMS("normalization", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			slog.Info("batch normalized", "batch_id", batchID)
			workerMetrics.incSuccess("normalization")
			workerMetrics.addProcessingMS("normalization", time.Since(start).Milliseconds())
			workerInFlight.Add(-1)
			msg.Ack(false)
		}
	}()

	// ── Reconciliation worker ─────────────────────────────────────────────────
	go func() {
		for msg := range reconciliationMsgs {
			start := time.Now()
			workerMetrics.incReceived("reconciliation")
			workerInFlight.Add(1)
			batchID, err := uuid.Parse(string(msg.Body))
			if err != nil {
				slog.Error("invalid batch id", "value", string(msg.Body))
				workerMetrics.incFailure("reconciliation", "invalid_message")
				workerMetrics.addProcessingMS("reconciliation", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			if err := reconciliationEngine.ReconcileBatch(sessionCtx, batchID); err != nil {
				slog.Error("reconciliation failed", "batch_id", batchID, "error", err)
				workerMetrics.incFailure("reconciliation", "processing_error")
				workerMetrics.addProcessingMS("reconciliation", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			slog.Info("batch reconciled", "batch_id", batchID)
			workerMetrics.incSuccess("reconciliation")
			workerMetrics.addProcessingMS("reconciliation", time.Since(start).Milliseconds())
			workerInFlight.Add(-1)
			msg.Ack(false)
		}
	}()

	// ── Report generation worker ──────────────────────────────────────────────
	go func() {
		for msg := range reportMsgs {
			start := time.Now()
			workerMetrics.incReceived("report_generation")
			workerInFlight.Add(1)
			runID, err := uuid.Parse(string(msg.Body))
			if err != nil {
				slog.Error("invalid export run id", "value", string(msg.Body))
				workerMetrics.incFailure("report_generation", "invalid_message")
				workerMetrics.addProcessingMS("report_generation", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			run, err := db.GetExportRunByID(sessionCtx, runID)
			if err != nil {
				slog.Error("get export run failed", "run_id", runID, "error", err)
				workerMetrics.incFailure("report_generation", "load_export_run")
				workerMetrics.addProcessingMS("report_generation", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			if err := reportGenerator.GenerateReport(sessionCtx, run); err != nil {
				slog.Error("report generation failed", "run_id", runID, "error", err)
				workerMetrics.incFailure("report_generation", "processing_error")
				workerMetrics.addProcessingMS("report_generation", time.Since(start).Milliseconds())
				workerInFlight.Add(-1)
				msg.Nack(false, false)
				continue
			}
			slog.Info("report generated", "run_id", runID)
			workerMetrics.incSuccess("report_generation")
			workerMetrics.addProcessingMS("report_generation", time.Since(start).Milliseconds())
			workerInFlight.Add(-1)
			msg.Ack(false)
		}
	}()

	connClosed := conn.NotifyClose(make(chan *amqp.Error, 1))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-connClosed:
		if err == nil {
			return errors.New("rabbitmq connection closed")
		}
		return err
	}
}

func dialAMQPWithRetry(ctx context.Context, amqpURL string) (*amqp.Connection, error) {
	backoff := initialReconnectBackoff()
	for {
		conn, err := amqp.Dial(amqpURL)
		if err == nil {
			return conn, nil
		}
		slog.Warn("rabbitmq connect failed; retrying", "error", err, "backoff", backoff.String())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff = nextReconnectBackoff(backoff)
	}
}

func initialReconnectBackoff() time.Duration {
	return defaultAMQPReconnectInitialBackoff
}

func nextReconnectBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > defaultAMQPReconnectMaxBackoff {
		return defaultAMQPReconnectMaxBackoff
	}
	return next
}

// ─── Queue setup ──────────────────────────────────────────────────────────────

func declareQueues(ch *amqp.Channel) error {
	const dlxName = "claims.dlx"

	if err := ch.ExchangeDeclare(dlxName, "direct", true, false, false, false, nil); err != nil {
		return err
	}

	for _, q := range []string{"batch_validation", "batch_normalization", "batch_reconciliation", "report_generation"} {
		dlqName := q + ".dlq"
		if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
			return err
		}
		if err := ch.QueueBind(dlqName, q, dlxName, false, nil); err != nil {
			return err
		}
		if _, err := ch.QueueDeclare(q, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange":    dlxName,
			"x-dead-letter-routing-key": q,
		}); err != nil {
			return err
		}
	}
	return nil
}

func publish(ch *amqp.Channel, ctx context.Context, queue string, batchID uuid.UUID) error {
	return ch.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{
		ContentType:  "text/plain",
		Body:         []byte(batchID.String()),
		DeliveryMode: amqp.Persistent,
	})
}

// ─── Validation pipeline ──────────────────────────────────────────────────────

func processBatch(ctx context.Context, db *database.Queries, batchID uuid.UUID, nextCh *amqp.Channel) error {
	if err := db.UpdateBatchStatus(ctx, database.UpdateBatchStatusParams{
		ID:     batchID,
		Status: "validating",
	}); err != nil {
		return err
	}

	uploadRows, err := db.GetUploadRowsByBatch(ctx, batchID)
	if err != nil {
		return err
	}

	allValid := true
	for _, row := range uploadRows {
		var data map[string]interface{}
		if err := json.Unmarshal(row.Data, &data); err != nil {
			saveValidationResult(ctx, db, row.ID, batchID, false, []string{"invalid JSON data"})
			allValid = false
			continue
		}
		errs := validateRow(data)
		isValid := len(errs) == 0
		if !isValid {
			allValid = false
		}
		saveValidationResult(ctx, db, row.ID, batchID, isValid, errs)
	}

	if allValid {
		if err := db.UpdateBatchStatus(ctx, database.UpdateBatchStatusParams{ID: batchID, Status: "validated"}); err != nil {
			return err
		}
		return publish(nextCh, ctx, "batch_normalization", batchID)
	}

	return db.UpdateBatchStatus(ctx, database.UpdateBatchStatusParams{ID: batchID, Status: "validated_with_errors"})
}

func saveValidationResult(ctx context.Context, db *database.Queries, rowID, batchID uuid.UUID, isValid bool, errs []string) {
	errJSON, _ := json.Marshal(errs)
	if _, err := db.CreateValidationResult(ctx, database.CreateValidationResultParams{
		RowID:   rowID,
		BatchID: batchID,
		IsValid: isValid,
		Errors:  errJSON,
	}); err != nil {
		slog.Error("save validation result failed", "row_id", rowID, "error", err)
	}
}

func validateRow(data map[string]interface{}) []string {
	var errs []string
	for _, field := range []string{"ndc", "pharmacy_npi", "service_date", "quantity"} {
		val, exists := data[field]
		if !exists || val == nil || val == "" {
			errs = append(errs, field+" is required")
		}
	}
	if ndc, ok := data["ndc"].(string); ok && len(ndc) < 10 {
		errs = append(errs, "ndc must be at least 10 characters")
	}
	switch v := data["quantity"].(type) {
	case float64:
		if v <= 0 {
			errs = append(errs, "quantity must be positive")
		}
	case string:
		q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			errs = append(errs, "quantity must be numeric")
		} else if q <= 0 {
			errs = append(errs, "quantity must be positive")
		}
	default:
		errs = append(errs, "quantity must be numeric")
	}
	return errs
}

// ─── Normalization pipeline ───────────────────────────────────────────────────

func normalizeBatch(ctx context.Context, db *database.Queries, batchID uuid.UUID, nextCh *amqp.Channel) error {
	batch, err := db.GetUploadBatches(ctx, batchID)
	if err != nil {
		return err
	}

	// Idempotency guard
	existing, err := db.GetClaimsByBatch(ctx, batchID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if len(existing) > 0 {
		slog.Info("batch already normalized, skipping", "batch_id", batchID)
		return publish(nextCh, ctx, "batch_reconciliation", batchID)
	}

	if err := db.UpdateBatchStatus(ctx, database.UpdateBatchStatusParams{ID: batchID, Status: "normalizing"}); err != nil {
		return err
	}

	rows, err := db.GetUploadRowsByBatchAndValidation(ctx, database.GetUploadRowsByBatchAndValidationParams{
		BatchID: batchID,
		IsValid: true,
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return db.UpdateBatchStatus(ctx, database.UpdateBatchStatusParams{ID: batchID, Status: "normalization_failed"})
	}

	var rowErrs []string
	for _, row := range rows {
		var raw map[string]interface{}
		if err := json.Unmarshal(row.Data, &raw); err != nil {
			rowErrs = append(rowErrs, "row "+row.ID.String()+": "+err.Error())
			continue
		}
		ndc := strings.TrimSpace(stringField(raw, "ndc"))
		pharmacyNPI := strings.TrimSpace(stringField(raw, "pharmacy_npi"))
		serviceDateRaw := strings.TrimSpace(stringField(raw, "service_date"))
		hashedRxKey := strings.TrimSpace(stringField(raw, "hashed_rx_key"))
		if hashedRxKey != "" && crypto.IsEnabled() {
			h, err := crypto.HMAC(hashedRxKey)
			if err != nil {
				rowErrs = append(rowErrs, "row "+row.ID.String()+": PHI HMAC failed: "+err.Error())
				continue
			}
			hashedRxKey = h
		}
		payerType := strings.TrimSpace(stringField(raw, "payer_type"))
		qty, err := parseQuantity(raw["quantity"])
		if err != nil {
			rowErrs = append(rowErrs, "row "+row.ID.String()+": "+err.Error())
			continue
		}

		serviceDate, err := time.Parse("2006-01-02", serviceDateRaw)
		if err != nil {
			rowErrs = append(rowErrs, "row "+row.ID.String()+": invalid service_date")
			continue
		}
		if _, err = db.CreateClaim(ctx, database.CreateClaimParams{
			BatchID:     batchID,
			RowID:       row.ID,
			OrgID:       batch.OrgID,
			Ndc:         ndc,
			PharmacyNpi: pharmacyNPI,
			ServiceDate: pgtype.Date{Time: serviceDate, Valid: true},
			Quantity:    qty,
			HashedRxKey: pgtype.Text{String: hashedRxKey, Valid: hashedRxKey != ""},
			PayerType:   pgtype.Text{String: payerType, Valid: payerType != ""},
		}); err != nil {
			rowErrs = append(rowErrs, "row "+row.ID.String()+": "+err.Error())
		}
	}

	status := "normalized"
	if len(rowErrs) > 0 {
		slog.Warn("partial normalization errors", "batch_id", batchID, "count", len(rowErrs))
		status = "normalized_with_errors"
	}

	if err := db.UpdateBatchStatus(ctx, database.UpdateBatchStatusParams{ID: batchID, Status: status}); err != nil {
		return err
	}

	// Hand off to reconciliation pipeline
	return publish(nextCh, ctx, "batch_reconciliation", batchID)
}

func stringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

func parseQuantity(v interface{}) (int32, error) {
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return 0, errors.New("quantity must be positive")
		}
		return int32(t), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, errors.New("quantity must be numeric")
		}
		if f <= 0 {
			return 0, errors.New("quantity must be positive")
		}
		return int32(f), nil
	default:
		return 0, errors.New("quantity must be numeric")
	}
}
