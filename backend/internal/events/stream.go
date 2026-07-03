package events

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

// EnsureStream creates the UPLOADS JetStream stream if it doesn't already
// exist. Idempotent — safe to call on every startup
// (docs/07-distributed-architecture.md §3).
func EnsureStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{StreamSubjects},
	})
	return err
}
