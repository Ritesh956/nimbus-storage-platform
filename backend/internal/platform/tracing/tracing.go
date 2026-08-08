// Package tracing wires OpenTelemetry distributed tracing across the
// HTTP -> NATS -> worker boundary (roadmap #14, closing the audit's §09
// "no distributed tracing" gap). Grafana Tempo is the trace backend —
// added to deploy/docker-compose.yml and deploy/k8s/infra alongside the
// existing Prometheus/Grafana stack, with traces viewable in the same
// Grafana instance those dashboards already live in, not a separate UI.
//
// Optional infra, same graceful-degradation pattern as mail.NewSMTPSender:
// NIMBUS_OTEL_EXPORTER_ENDPOINT empty means Setup installs nothing further.
// The OpenTelemetry API's own default global TracerProvider is already a
// genuine no-op (near-zero overhead, no allocation beyond a no-op span), so
// every tracing.Tracer(...).Start call elsewhere in the backend is safe to
// leave in place unconditionally rather than needing its own nil-check.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// Setup installs the global TracerProvider and W3C tracecontext propagator
// for serviceName ("nimbus-api" or "nimbus-worker"). endpoint is a bare
// host:port (NIMBUS_OTEL_EXPORTER_ENDPOINT, e.g. "tempo:4318") for Tempo's
// OTLP/HTTP receiver. The returned shutdown func flushes buffered spans on
// exit — callers should defer it right after a successful Setup.
//
// An empty endpoint is not an error: it returns a no-op shutdown and skips
// exporter/provider setup entirely, leaving the SDK's default no-op
// provider in place — the same "optional, disabled by omission" shape as
// SMTPAddr/mailer above it in config.go.
func Setup(ctx context.Context, serviceName, endpoint string) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }
	if endpoint == "" {
		return noop, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		// Tempo's OTLP/HTTP receiver is plaintext on the Docker/kind
		// internal network — same trust boundary this backend already
		// extends to Postgres/Redis/NATS (docs/03-hld.md's "internal
		// services aren't independently hardened" scope note).
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return noop, fmt.Errorf("otlp trace exporter: %w", err)
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		return noop, fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}

// Tracer is the one call sites elsewhere in the backend should use — a
// thin named wrapper so those call sites read tracing.Tracer(name) instead
// of reaching into the otel package directly, matching how logging.New and
// metrics.* are the sole entry points into their respective libraries.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// Propagator is the shared W3C tracecontext (de)serializer — used both by
// the HTTP middleware (internal/platform/httpserver.Tracing) and the NATS
// publish/consume boundary (internal/events) so both hops speak the same
// wire format Setup installed above.
func Propagator() propagation.TextMapPropagator {
	return otel.GetTextMapPropagator()
}
