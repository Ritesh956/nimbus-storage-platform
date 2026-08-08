package events

import (
	"testing"

	"github.com/nats-io/nats.go"
)

// Audit §14: events had zero automated tests. headerCarrier is a pure
// adapter with no NATS/Postgres dependency, so it's unit-tested here —
// Publisher/Subscribe/the DLQ Repository all need real NATS/Postgres and
// are integration-tested in publisher_integration_test.go and
// dlq_repository_integration_test.go.

func TestHeaderCarrier_SetThenGetRoundTrips(t *testing.T) {
	c := headerCarrier{}
	c.Set("traceparent", "00-abc-def-01")
	if got := c.Get("traceparent"); got != "00-abc-def-01" {
		t.Fatalf("Get = %q, want %q", got, "00-abc-def-01")
	}
}

func TestHeaderCarrier_GetMissingKeyReturnsEmpty(t *testing.T) {
	c := headerCarrier{}
	if got := c.Get("nonexistent"); got != "" {
		t.Fatalf("Get on missing key = %q, want empty string", got)
	}
}

func TestHeaderCarrier_KeysListsEverySetKey(t *testing.T) {
	c := headerCarrier{}
	c.Set("traceparent", "00-abc-def-01")
	c.Set("tracestate", "vendor=value")

	keys := c.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys() returned %d keys, want 2", len(keys))
	}
	found := map[string]bool{}
	for _, k := range keys {
		found[k] = true
	}
	if !found["traceparent"] || !found["tracestate"] {
		t.Fatalf("Keys() = %v, want both traceparent and tracestate", keys)
	}
}

func TestHeaderCarrier_WrapsNATSHeaderShapeDirectly(t *testing.T) {
	// headerCarrier is a defined type over nats.Header, not a copy — this
	// pins that a nats.Header built independently (as Publisher does) is
	// usable as a headerCarrier via a plain conversion, with no translation
	// step that could silently drop headers.
	h := nats.Header{}
	h.Set("x-custom", "value")
	c := headerCarrier(h)
	if got := c.Get("x-custom"); got != "value" {
		t.Fatalf("Get on a converted nats.Header = %q, want %q", got, "value")
	}
}
