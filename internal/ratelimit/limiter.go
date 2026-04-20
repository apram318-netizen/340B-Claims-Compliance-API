// Package ratelimit provides a per-key sliding-window rate limiter with both
// Redis-backed (suitable for multi-instance / Kubernetes) and in-memory
// (single-process, development only) implementations.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is the interface for a per-key rate limiter.
type Limiter interface {
	// Allow returns true when the key is within its rate window.
	// It should fail open (return true) on transient errors to avoid
	// blocking legitimate traffic when the backing store is unavailable.
	Allow(ctx context.Context, key string) bool
}

// NewRedisLimiter creates a Redis-backed sliding window rate limiter.
// max is the maximum number of allowed requests per window duration.
func NewRedisLimiter(client *redis.Client, max int, window time.Duration) Limiter {
	return &redisLimiter{client: client, max: max, window: window}
}

type redisLimiter struct {
	client *redis.Client
	max    int
	window time.Duration
}

// Allow uses INCR + EXPIRE in a pipeline for an atomic count-and-expire.
// The key expires automatically at the end of the window.
func (l *redisLimiter) Allow(ctx context.Context, key string) bool {
	rkey := fmt.Sprintf("rl:%s", key)

	pipe := l.client.Pipeline()
	incr := pipe.Incr(ctx, rkey)
	pipe.Expire(ctx, rkey, l.window)

	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("redis rate limit error — failing open", "error", err)
		return true
	}
	return incr.Val() <= int64(l.max)
}

// NewInMemoryLimiter creates a process-local rate limiter using sync.Map.
// WARNING: in a multi-instance deployment each pod maintains its own counters.
// Use only for single-process scenarios or when Redis is unavailable.
func NewInMemoryLimiter(max int, window time.Duration) Limiter {
	return &inMemoryLimiter{max: max, window: window}
}

type inMemEntry struct {
	mu        sync.Mutex
	count     int
	windowEnd time.Time
}

type inMemoryLimiter struct {
	entries sync.Map
	max     int
	window  time.Duration
}

func (l *inMemoryLimiter) Allow(_ context.Context, key string) bool {
	v, _ := l.entries.LoadOrStore(key, &inMemEntry{windowEnd: time.Now().Add(l.window)})
	e := v.(*inMemEntry)

	e.mu.Lock()
	defer e.mu.Unlock()

	if time.Now().After(e.windowEnd) {
		e.count = 0
		e.windowEnd = time.Now().Add(l.window)
	}
	e.count++
	return e.count <= l.max
}
