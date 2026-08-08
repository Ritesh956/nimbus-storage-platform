// Package events holds NATS JetStream subjects/payloads shared between
// nimbus-api (publisher) and nimbus-worker (consumer, Day 9) — see
// docs/07-distributed-architecture.md §3 and docs/08-folder-structure.md.
// It also owns the dead-letter queue (dlq.go, dlq_handler.go) and so is one
// of the two packages the /v1/admin/* cluster-ops routes actually live in —
// internal/admin was sketched in early docs but never materialized; see
// docs/00-project-state.md "Known issues" for why that split is now
// considered permanent rather than a gap. The other is internal/storage
// (nodes, ring).
package events

const (
	StreamName             = "UPLOADS"
	StreamSubjects         = "nimbus.uploads.*"
	UploadCompletedSubject = "nimbus.uploads.completed"
)

// UploadCompleted is published once per successful upload completion.
// RequestID carries the originating HTTP request's correlation ID
// (docs/03-hld.md §2) so a redelivered/DLQ'd message can be traced back to
// the request that produced it.
type UploadCompleted struct {
	EventID   string `json:"event_id"`
	FileID    string `json:"file_id"`
	VersionID string `json:"version_id"`
	OrgID     string `json:"org_id"`
	RequestID string `json:"request_id"`
}
