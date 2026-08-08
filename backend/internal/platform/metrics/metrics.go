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
	// them apart. A Grafana alert (deploy/observability/grafana/provisioning/
	// alerting/rules.yml) compares the two processes' series for the same
	// node and fires if they've disagreed for a sustained period — the two
	// independent gauges are a deliberate design choice (docs/04-lld.md §5),
	// but nothing previously surfaced a genuine, lasting disagreement
	// between them to an operator (audit §02).
	StorageNodeHealthy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nimbus_storage_node_healthy",
		Help: "1 if this process currently considers the storage node healthy, 0 if down.",
	}, []string{"node"})

	// StorageNodeLatencyMS is the rolling EWMA of successful health-probe
	// round-trip time, in milliseconds — Router.Resolve reads the same
	// underlying value (in-process, not this exported gauge) to sink a
	// technically-alive-but-slow node behind faster ones in the preference
	// order (docs/04-lld.md §1, audit §02: "no capacity/latency signal
	// folds into placement" was the named gap).
	StorageNodeLatencyMS = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nimbus_storage_node_latency_ms",
		Help: "Rolling EWMA of this process's health-probe round-trip time to the node, in milliseconds.",
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

	// Storage repair counters are recorded by Router.RepairNode, the
	// manual-trigger POST /v1/admin/nodes/{nodeId}/repair pass (audit §02:
	// "recovery restores routing but not necessarily the replica count").
	StorageRepairChunksCheckedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_storage_repair_chunks_checked_total",
		Help: "Chunks a repair pass re-verified as physically present on the target node.",
	})
	StorageRepairChunksRestoredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_storage_repair_chunks_restored_total",
		Help: "Chunks a repair pass found missing and successfully re-copied from a surviving replica.",
	})
	StorageRepairChunksUnrepairableTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nimbus_storage_repair_chunks_unrepairable_total",
		Help: "Chunks a repair pass found missing with no surviving replica to copy from, or whose copy attempt itself failed.",
	})
)
