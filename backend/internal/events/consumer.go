package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	thumbnailConsumerName = "thumbnail-worker"
	maxDeliver            = 5
)

// Subscribe creates (or attaches to) the durable "thumbnail-worker"
// consumer and invokes handler for each delivered UploadCompleted message —
// ack on success, nak (redelivery, up to maxDeliver with the backoff
// schedule below) on error (docs/07-distributed-architecture.md §3).
//
// A dead-letter subject for permanently-failed messages
// (nimbus.uploads.completed.dlq) is documented but not built — a real gap,
// not silently dropped: NATS's own max-deliveries advisory would be the
// natural hook for it, deferred as a roadmap item since it doesn't block
// the core pipeline this exists to prove works.
func Subscribe(ctx context.Context, js jetstream.JetStream, handler func(context.Context, UploadCompleted) error) error {
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       thumbnailConsumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    maxDeliver,
		FilterSubject: UploadCompletedSubject,
		BackOff:       []time.Duration{1 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second},
	})
	if err != nil {
		return err
	}

	_, err = cons.Consume(func(msg jetstream.Msg) {
		var evt UploadCompleted
		if err := json.Unmarshal(msg.Data(), &evt); err != nil {
			_ = msg.Term() // malformed payload will never succeed on redelivery — terminate rather than retry
			return
		}
		if err := handler(ctx, evt); err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	return err
}
