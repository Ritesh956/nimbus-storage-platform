// Package activity owns the org activity feed — see docs/03-hld.md §1.
//
// Most events are written synchronously by nimbus-api right where they
// happen (e.g. upload completion) rather than exclusively through the
// worker's NATS consumer as docs/02-system-design.md §6 originally sketched:
// a cheap, always-relevant fact like "file uploaded" shouldn't be at the
// mercy of async processing being up. The worker (Day 9) still owns
// events that are genuinely derived from async work, like "thumbnail
// ready" — this is a refinement, not a contradiction, of that doc.
package activity

import "time"

const (
	VerbUploaded           = "uploaded"
	VerbThumbnailGenerated = "thumbnail_generated" // written by nimbus-worker, actor_user_id nil (system-generated)

	TargetTypeFile = "file"
)

type Event struct {
	ID          int64
	OrgID       string
	ActorUserID *string
	Verb        string
	TargetType  string
	TargetID    string
	CreatedAt   time.Time
}
