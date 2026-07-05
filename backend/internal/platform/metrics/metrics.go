// Package metrics holds the process-wide Prometheus collectors shared across
// nimbus-api and nimbus-worker (docs/02-system-design.md §7, Day 11). Kept
// as a single small package, rather than one file per domain module, so
// metric names stay consistent and discoverable in one place.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTPRequestDuration is recorded by httpserver.Metrics middleware,
	// labeled by the *pattern* a request matched (e.g. "GET /v1/files/{fileId}"),
	// not the literal path — keeps cardinality bounded regardless of how
	// many distinct IDs are ever requested.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nimbus_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by method, route pattern, and status.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	// UploadChunksCommittedTotal and UploadBytesCommittedTotal are the
	// upload-throughput signal, recorded once per successfully verified
	// chunk commit (upload.Service.CommitChunk) — the point bytes are
	// confirmed durably written to their replicas.
	UploadChunksCommittedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_upload_chunks_committed_total",
		Help: "Chunks that passed cross-replica ETag verification and were committed.",
	})
	UploadBytesCommittedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_upload_bytes_committed_total",
		Help: "Total bytes across all committed chunks.",
	})

	// StoragePlacementFailuresTotal counts Router.Resolve calls that
	// couldn't find enough healthy replicas for a chunk (upload writes and
	// worker thumbnail writes both go through Resolve, so instrumenting it
	// there covers both call sites).
	StoragePlacementFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_storage_placement_failures_total",
		Help: "Chunk placements that failed because too few storage nodes were healthy.",
	})

	// StorageNodeHealthy is set by Router.probeOne on every health-check
	// tick. nimbus-api and nimbus-worker each run their own Router/probe
	// loop (health state is in-process, not shared — docs/04-lld.md §5), so
	// this series exists on both processes; the Prometheus job label tells
	// them apart.
	StorageNodeHealthy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nimbus_storage_node_healthy",
		Help: "1 if this process currently considers the storage node healthy, 0 if down.",
	}, []string{"node"})

	// NATSConsumerPending is nimbus-worker's consumer-lag gauge: pending
	// (undelivered/unacked) messages on the durable "thumbnail-worker"
	// consumer.
	NATSConsumerPending = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nimbus_nats_consumer_pending",
		Help: "Pending (undelivered or unacked) message count for a JetStream consumer.",
	}, []string{"consumer"})

	// GC counters are recorded by nimbus-worker's chunk sweeper
	// (internal/gc). Doomed counts mark-phase transitions; reaped/reclaimed
	// count only chunks whose objects and row were actually deleted.
	GCChunksDoomedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_gc_chunks_doomed_total",
		Help: "Chunks marked doomed (unreferenced past the grace window) by the GC mark phase.",
	})
	GCChunksReapedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_gc_chunks_reaped_total",
		Help: "Doomed chunks physically deleted (MinIO objects + chunks row) by the GC sweep phase.",
	})
	GCBytesReclaimedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_gc_bytes_reclaimed_total",
		Help: "Logical bytes freed by reaped chunks (chunk size, not multiplied by replica count).",
	})
	GCSweepFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_gc_sweep_failures_total",
		Help: "Sweep attempts aborted mid-chunk (e.g. a replica's node unreachable); the chunk stays doomed and is retried next tick.",
	})
)
