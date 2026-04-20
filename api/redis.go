package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// connectRedis parses REDIS_URL and returns a connected client.
// Returns nil (with a warning) if REDIS_URL is unset so the application
// can fall back to in-memory rate limiting during local development.
func connectRedis() *redis.Client {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		slog.Warn("REDIS_URL not set — falling back to in-memory rate limiter; not suitable for multi-instance deployments")
		return nil
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		slog.Error("invalid REDIS_URL", "error", err)
		os.Exit(1)
	}

	client := redis.NewClient(opts)
	client.Options().DialTimeout = 5 * time.Second
	client.Options().ReadTimeout = 3 * time.Second
	client.Options().WriteTimeout = 3 * time.Second

	slog.Info("Redis connected", "addr", opts.Addr)
	return client
}
