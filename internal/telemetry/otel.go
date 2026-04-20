// Package telemetry configures OpenTelemetry tracing for the API process.
package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init configures the global tracer provider when OTEL_EXPORTER_OTLP_ENDPOINT or
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT is set (standard OTLP env vars).
func Init(ctx context.Context) (shutdown func(context.Context) error) {
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" &&
		strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")) == "" {
		slog.Info("opentelemetry: OTLP endpoint not set; tracing disabled")
		return func(context.Context) error { return nil }
	}

	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		slog.Error("opentelemetry: failed to create OTLP exporter", "error", err)
		return func(context.Context) error { return nil }
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(semconv.ServiceName(serviceName())),
	)
	if err != nil {
		slog.Error("opentelemetry: resource", "error", err)
		_ = exp.Shutdown(ctx)
		return func(context.Context) error { return nil }
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info("opentelemetry: tracing enabled", "service", serviceName())

	return func(shutdownCtx context.Context) error {
		ctx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}
}

func serviceName() string {
	s := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if s == "" {
		return "claims-system-api"
	}
	return s
}
