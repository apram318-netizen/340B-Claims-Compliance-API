package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// loadEnv loads .env once at startup. Safe to call multiple times — godotenv
// skips keys that are already set in the environment, so real env vars win.
func loadEnv() {
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, relying on environment variables")
	}
}

func connectDB() *pgxpool.Pool {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is not set")
		os.Exit(1)
	}
	if !strings.Contains(dbURL, "sslmode=") {
		slog.Warn("DATABASE_URL does not specify sslmode; connections may be unencrypted — set sslmode=require for production")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("failed to create connection pool", "error", err)
		os.Exit(1)
	}

	if err := pool.Ping(ctx); err != nil {
		slog.Error("database ping failed", "error", err)
		os.Exit(1)
	}

	slog.Info("database connected")
	return pool
}
