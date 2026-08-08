package events

import (
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/propagation"
)

// headerCarrier adapts nats.Header (identical shape to http.Header —
// map[string][]string, with the same Get/Set methods) to
// propagation.TextMapCarrier, so the same W3C tracecontext propagator the
// HTTP middleware uses (internal/platform/tracing.Propagator) can inject a
// span's trace ID into an outgoing NATS message's headers, and the worker
// can extract it back out on the consuming side — carrying a trace across
// the publish/consume boundary the same way an inbound HTTP request's
// traceparent header would.
type headerCarrier nats.Header

func (c headerCarrier) Get(key string) string { return nats.Header(c).Get(key) }
func (c headerCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

var _ propagation.TextMapCarrier = headerCarrier{}
