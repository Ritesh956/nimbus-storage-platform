// Package events holds NATS JetStream subjects/payloads shared between
// nimbus-api (publisher) and nimbus-worker (consumer, Day 9) — see
// docs/07-distributed-architecture.md §3 and docs/08-folder-structure.md.
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
