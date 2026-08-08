package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"nimbus/internal/platform/tracing"
)

const (
	// ThumbnailConsumerName is exported so the worker can label the
	// nimbus_nats_consumer_pending gauge without this package leaking its
	// jetstream.Consumer handle.
	ThumbnailConsumerName = "thumbnail-worker"
	maxDeliver            = 5
)

// Subscribe creates (or attaches to) the durable "thumbnail-worker"
// consumer and invokes handler for each delivered UploadCompleted message —
// ack on success, nak (redelivery, up to maxDeliver with the backoff
// schedule below) on error (docs/07-distributed-architecture.md §3).
//
// A message failing its final (maxDeliver'th) delivery is dead-lettered:
// recorded in the dead_events table via dead, then Term'd. Detecting the
// final delivery inline (msg.Metadata().NumDelivered) was chosen over the
// NATS max-deliveries advisory the original design sketched — same
// guarantee, no second subscription, and the failure error is still in
// hand to store alongside the payload (backlog #9).
func Subscribe(ctx context.Context, js jetstream.JetStream, dead *Repository, handler func(context.Context, UploadCompleted) error) (jetstream.Consumer, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       ThumbnailConsumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    maxDeliver,
		FilterSubject: UploadCompletedSubject,
		BackOff:       []time.Duration{1 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second},
	})
	if err != nil {
		return nil, err
	}

	tracer := tracing.Tracer("nimbus/internal/events")
	_, err = cons.Consume(func(msg jetstream.Msg) {
		var evt UploadCompleted
		if err := json.Unmarshal(msg.Data(), &evt); err != nil {
			_ = msg.Term() // malformed payload will never succeed on redelivery — terminate rather than retry
			return
		}

		// Extracts the publishing request's trace context from the message
		// headers (Publisher.PublishUploadCompleted injected it) — this span
		// joins that trace instead of starting an unrelated one, so
		// "upload-complete → thumbnail-generated" shows up as one trace in
		// Tempo, not two disconnected ones either side of the NATS hop
		// (roadmap #14, the audit's own suggested minimum scope). ctx (not a
		// per-message derivative) still governs cancellation, matching the
		// pre-existing handler(ctx, evt) call this replaces.
		msgCtx := tracing.Propagator().Extract(ctx, headerCarrier(msg.Headers()))
		msgCtx, span := tracer.Start(msgCtx, "process "+msg.Subject(), trace.WithSpanKind(trace.SpanKindConsumer))
		err := handler(msgCtx, evt)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
		if err != nil {
			if md, mdErr := msg.Metadata(); mdErr == nil && md.NumDelivered >= maxDeliver {
				// Final attempt failed — dead-letter and stop redelivery. If
				// the insert itself fails there's nothing left to do but log:
				// delivery attempts are exhausted either way.
				if insErr := dead.InsertDeadEvent(ctx, msg.Subject(), msg.Data(), err.Error(), int(md.NumDelivered)); insErr != nil {
					slog.Default().Error("failed to record dead event — event is lost", "error", insErr, "event_id", evt.EventID, "handler_error", err)
				} else {
					slog.Default().Warn("event dead-lettered after exhausting redeliveries", "event_id", evt.EventID, "deliveries", md.NumDelivered, "error", err)
				}
				_ = msg.Term()
				return
			}
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		return nil, err
	}
	return cons, nil
}
