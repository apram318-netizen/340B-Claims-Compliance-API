package main

import (
	"context"

	"github.com/google/uuid"
)

type traceIDKey struct{}

// WithTraceID attaches a trace id for logging and error responses.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, id)
}

func traceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey{}).(string)
	return v
}

func newTraceID() string {
	return uuid.New().String()
}
