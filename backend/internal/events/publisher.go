package events

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"nimbus/internal/platform/tracing"
)

type Publisher struct {
	js jetstream.JetStream
}

func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// PublishUploadCompleted injects the calling request's trace context
// (internal/platform/httpserver.Tracing put it on ctx) into the message
// headers before publishing — see tracing.go's headerCarrier and
// Subscribe's matching extraction — so the worker's processing span joins
// the same trace as the HTTP request that triggered it (roadmap #14).
func (p *Publisher) PublishUploadCompleted(ctx context.Context, evt UploadCompleted) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	header := nats.Header{}
	tracing.Propagator().Inject(ctx, headerCarrier(header))
	_, err = p.js.PublishMsg(ctx, &nats.Msg{Subject: UploadCompletedSubject, Data: data, Header: header})
	return err
}

// PublishRaw republishes an already-serialized payload to its original
// subject — the DLQ retry path, which must not need to understand every
// payload type a subject might carry.
func (p *Publisher) PublishRaw(ctx context.Context, subject string, payload []byte) error {
	_, err := p.js.Publish(ctx, subject, payload)
	return err
}
