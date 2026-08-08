package httpserver

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"nimbus/internal/platform/tracing"
)

// Tracing starts a server span per request, named by the registered mux
// pattern (e.g. "GET /v1/files/{fileId}") rather than the literal path —
// same cardinality reasoning as Metrics, and it takes the *http.ServeMux
// for the same reason. Extracts an inbound traceparent header first (so a
// request already part of a caller's trace joins it instead of starting a
// new one — harmless no-op when there isn't one), and stores the resulting
// context so downstream code in the same request (notably
// events.Publisher, which injects it into outgoing NATS message headers)
// can attach child spans. A no-op when tracing.Setup was never given an
// endpoint — see that package's doc comment.
func Tracing(mux *http.ServeMux) func(http.Handler) http.Handler {
	tracer := tracing.Tracer("nimbus-api/http")
	propagator := tracing.Propagator()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			_, pattern := mux.Handler(r)
			if pattern == "" {
				pattern = "unmatched"
			}
			ctx, span := tracer.Start(ctx, pattern, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))

			span.SetAttributes(
				attribute.String("http.method", r.Method),
				attribute.Int("http.status_code", sw.status),
				// RequestID runs upstream of Tracing in the middleware chain,
				// so the header's already set — ties the two correlation
				// mechanisms (docs/03-hld.md §2's request-ID log correlation,
				// and this span's own trace ID) to the same value.
				attribute.String("nimbus.request_id", w.Header().Get("X-Request-ID")),
			)
			if sw.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(sw.status))
			}
		})
	}
}
