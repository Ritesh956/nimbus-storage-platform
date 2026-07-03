package events

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go/jetstream"
)

type Publisher struct {
	js jetstream.JetStream
}

func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

func (p *Publisher) PublishUploadCompleted(ctx context.Context, evt UploadCompleted) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = p.js.Publish(ctx, UploadCompletedSubject, data)
	return err
}
