// Command rehash-rx-keys recomputes claims.hashed_rx_key from the original CSV value
// stored in upload_rows.data JSON (hashed_rx_key field), using the current PHI_ENCRYPTION_KEY.
//
// Use after rotating PHI_ENCRYPTION_KEY: ingest used the key at claim-creation time; this
// backfills stored HMACs so reconciliation still matches. Rebate records and any rows
// without upload source must be handled separately.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"claims-system/internal/crypto"
	"claims-system/internal/database"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	dryRun := flag.Bool("dry-run", false, "log actions without updating the database")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if !crypto.IsEnabled() {
		slog.Error("PHI_ENCRYPTION_KEY must be set (32-byte secret, base64-encoded)")
		os.Exit(1)
	}

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
	rows, err := q.ListClaimsForRxRehash(ctx)
	if err != nil {
		slog.Error("list claims for rehash failed", "error", err)
		os.Exit(1)
	}

	var applied, dryRunWouldChange, skipped, failed int
	for _, row := range rows {
		var raw map[string]any
		if err := json.Unmarshal(row.Data, &raw); err != nil {
			slog.Warn("skip claim: invalid upload row JSON", "claim_id", row.ID, "error", err)
			skipped++
			continue
		}
		s, _ := raw["hashed_rx_key"].(string)
		s = strings.TrimSpace(s)
		if s == "" {
			skipped++
			continue
		}
		newH, err := crypto.HMAC(s)
		if err != nil {
			slog.Error("HMAC failed", "claim_id", row.ID, "error", err)
			failed++
			continue
		}
		cur := ""
		if row.HashedRxKey.Valid {
			cur = row.HashedRxKey.String
		}
		if cur == newH {
			skipped++
			continue
		}
		if *dryRun {
			dryRunWouldChange++
			slog.Info("rehash_rx_dry_run", "claim_id", row.ID)
			continue
		}
		if err := q.UpdateClaimHashedRxKey(ctx, database.UpdateClaimHashedRxKeyParams{
			ID: row.ID,
			HashedRxKey: pgtype.Text{String: newH, Valid: true},
		}); err != nil {
			slog.Error("update claim failed", "claim_id", row.ID, "error", err)
			failed++
			continue
		}
		applied++
	}

	slog.Info("rehash_rx_keys_completed",
		"event", "rehash_rx_keys",
		"rows_considered", len(rows),
		"rows_updated", applied,
		"rows_would_update", dryRunWouldChange,
		"rows_skipped", skipped,
		"rows_failed", failed,
		"dry_run", *dryRun,
	)
	if failed > 0 {
		os.Exit(1)
	}
}
