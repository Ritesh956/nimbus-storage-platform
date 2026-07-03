// Package storage owns the distributed piece: consistent-hash ring, replica
// placement, node health, and failover — see docs/02-system-design.md §1-2
// and docs/04-lld.md §1.
package storage

import "errors"

type NodeID string

type NodeStatus int

const (
	StatusHealthy NodeStatus = iota
	StatusDown
)

func (s NodeStatus) String() string {
	if s == StatusHealthy {
		return "healthy"
	}
	return "down"
}

type StorageNode struct {
	ID NodeID
	// Endpoint is the docker-network-internal address (e.g.
	// http://minio-node-1:9000) — used for health probing and any
	// server-side MinIO admin calls (bucket creation) made from within
	// nimbus-api's own container.
	Endpoint string
	// PublicEndpoint is the externally-reachable address (e.g.
	// http://localhost:9000) used ONLY as the basis for presigned URLs.
	// S3 SigV4 signs the Host header, so a URL signed against the internal
	// hostname would fail verification when a browser/CLI outside the
	// docker network actually requests it — this split is what makes
	// presigned URLs usable by an external client at all (a real deployment
	// would use a public DNS name here instead of localhost).
	PublicEndpoint string
}

var (
	ErrNoNodes                  = errors.New("no storage nodes configured")
	ErrInsufficientHealthyNodes = errors.New("not enough healthy storage nodes to satisfy replication factor")
)
