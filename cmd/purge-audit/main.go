// Command purge-audit deletes audit_events rows past HIPAA-style retention (expires_at).
// Run on a schedule (e.g. weekly CronJob) with DATABASE_URL set.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"claims-system/internal/database"

	"github.com/joho/godotenv"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	q := database.New(pool)
	n, err := q.PurgeExpiredAuditEvents(ctx)
	if err != nil {
		slog.Error("purge failed", "error", err)
		os.Exit(1)
	}
	slog.Info("audit_purge_completed", "event", "audit_purge", "rows_deleted", n)
}
