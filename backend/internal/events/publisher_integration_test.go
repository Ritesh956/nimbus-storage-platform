//go:build integration

// Audit §14: events' Publisher/Subscribe (the HTTP -> NATS -> worker
// backbone roadmap #14's tracing rides on) had zero automated tests. This
// proves a real publish is actually delivered to a real consumer end to end
// against real JetStream — not the redelivery/backoff/dead-letter timing
// itself (maxDeliver=5 with backoff up to 60s between attempts makes that
// genuinely too slow for a per-push CI gate, ~50s+ minimum; the DLQ
// Repository's own state machine is covered fast and directly in
// dlq_repository_integration_test.go instead). Gated behind the
// "integration" build tag, matching the house style set by
// internal/auth/refresh_integration_test.go.
package events_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"nimbus/internal/events"
)

func testNATSURL() string {
	if v := os.Getenv("NIMBUS_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://localhost:4222"
}

func newTestJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()
	url := testNATSURL()
	nc, err := nats.Connect(url, nats.Timeout(3*time.Second))
	if err != nil {
		t.Skipf("nats not reachable at %s (is `docker compose up` running?): %v", url, err)
	}
	t.Cleanup(nc.Close)

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	return js
}

func TestPublishUploadCompleted_IsDeliveredToARealSubscriber(t *testing.T) {
	js := newTestJetStream(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := events.EnsureStream(ctx, js); err != nil {
		t.Fatalf("EnsureStream: %v", err)
	}

	pub := events.NewPublisher(js)
	want := events.UploadCompleted{
		EventID:   fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		FileID:    "file-round-trip",
		VersionID: "version-round-trip",
		OrgID:     "org-round-trip",
		RequestID: "req-round-trip",
	}

	received := make(chan events.UploadCompleted, 1)
	dead := events.NewRepository(nil) // handler succeeds, so dead-lettering is never reached
	cons, err := events.Subscribe(ctx, js, dead, func(ctx context.Context, evt events.UploadCompleted) error {
		if evt.EventID == want.EventID {
			received <- evt
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = cons

	if err := pub.PublishUploadCompleted(ctx, want); err != nil {
		t.Fatalf("PublishUploadCompleted: %v", err)
	}

	select {
	case got := <-received:
		if got != want {
			t.Fatalf("received event = %+v, want %+v", got, want)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the published event to be delivered to the subscriber")
	}
}

func TestPublishRaw_RepublishesToTheOriginalSubject(t *testing.T) {
	js := newTestJetStream(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := events.EnsureStream(ctx, js); err != nil {
		t.Fatalf("EnsureStream: %v", err)
	}

	pub := events.NewPublisher(js)
	payload := []byte(fmt.Sprintf(`{"event_id":"raw-%d"}`, time.Now().UnixNano()))

	received := make(chan []byte, 1)
	dead := events.NewRepository(nil)
	_, err := events.Subscribe(ctx, js, dead, func(ctx context.Context, evt events.UploadCompleted) error {
		if evt.EventID != "" {
			received <- payload
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := pub.PublishRaw(ctx, events.UploadCompletedSubject, payload); err != nil {
		t.Fatalf("PublishRaw: %v", err)
	}

	select {
	case <-received:
		// delivered — PublishRaw put a decodable UploadCompleted-shaped
		// payload back on the same subject Subscribe listens to.
	case <-ctx.Done():
		t.Fatal("timed out waiting for the raw-republished payload to be delivered")
	}
}
