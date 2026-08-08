package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"

	"nimbus/internal/platform/metrics"
)

const (
	// healthTTL = 3 missed probes at probeInterval — TTL expiry doubles as
	// the down signal for anything reading Redis directly, without a
	// separate "mark down" write that might never happen
	// (docs/07-distributed-architecture.md §2).
	healthTTL     = 6 * time.Second
	probeInterval = 2 * time.Second
	probeTimeout  = 500 * time.Millisecond
	// HealthChangesChannel carries "node-id:status" messages on every
	// health transition — exported because internal/live relays it to
	// browsers over SSE (backlog #12).
	HealthChangesChannel = "nimbus:health:changes"
	// latencyEWMAAlpha weights each new probe RTT against the running
	// average — high enough that a node degrading mid-run is reflected
	// within a few 2s probe ticks, low enough that one slow tick doesn't
	// itself flip a node's tier.
	latencyEWMAAlpha = 0.3
)

// Router owns the hash ring, per-node circuit breakers, and the
// in-memory health view Resolve reads. It's the type described in
// docs/04-lld.md §1.
type Router struct {
	mu       sync.RWMutex
	ring     *Ring
	nodes    map[NodeID]StorageNode
	breakers map[NodeID]*breaker
	health   map[NodeID]NodeStatus
	// latencyEWMA is the rolling average successful-probe RTT per node —
	// Resolve reads it (via isSlow) to sink a technically-alive-but-slow
	// node behind faster ones instead of treating every healthy node
	// identically (audit §02). Absent until a node's first successful
	// probe, at which point it seeds from that first measurement.
	latencyEWMA map[NodeID]time.Duration

	repo          *Repository
	redis         *redis.Client
	httpClient    *http.Client
	logger        *slog.Logger
	slowThreshold time.Duration

	// internalMinio is used for admin calls nimbus-api makes itself
	// (bucket creation); publicMinio is used only to build presigned URLs
	// for external clients. See StorageNode.PublicEndpoint.
	internalMinio map[NodeID]*minio.Client
	publicMinio   map[NodeID]*minio.Client
}

// NewRouter builds a Router and its per-node MinIO clients. Returns an
// error if any node's endpoint can't be parsed into a MinIO client.
// slowThreshold is the latency-tier cutoff Resolve uses (0 disables the
// distinction — every healthy node is first-tier).
func NewRouter(repo *Repository, redisClient *redis.Client, nodes []StorageNode, minioAccessKey, minioSecretKey string, logger *slog.Logger, slowThreshold time.Duration) (*Router, error) {
	ids := make([]NodeID, 0, len(nodes))
	nodeMap := make(map[NodeID]StorageNode, len(nodes))
	breakers := make(map[NodeID]*breaker, len(nodes))
	health := make(map[NodeID]NodeStatus, len(nodes))
	latencyEWMA := make(map[NodeID]time.Duration, len(nodes))
	internalMinio := make(map[NodeID]*minio.Client, len(nodes))
	publicMinio := make(map[NodeID]*minio.Client, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
		nodeMap[n.ID] = n
		breakers[n.ID] = newBreaker()
		health[n.ID] = StatusHealthy // optimistic until the first probe says otherwise

		internalCli, err := buildMinIOClient(n.Endpoint, minioAccessKey, minioSecretKey)
		if err != nil {
			return nil, fmt.Errorf("node %s internal client: %w", n.ID, err)
		}
		internalMinio[n.ID] = internalCli

		publicCli, err := buildMinIOClient(n.PublicEndpoint, minioAccessKey, minioSecretKey)
		if err != nil {
			return nil, fmt.Errorf("node %s public client: %w", n.ID, err)
		}
		publicMinio[n.ID] = publicCli
	}
	return &Router{
		ring:          BuildRing(ids),
		nodes:         nodeMap,
		breakers:      breakers,
		health:        health,
		latencyEWMA:   latencyEWMA,
		repo:          repo,
		redis:         redisClient,
		httpClient:    &http.Client{Timeout: probeTimeout},
		logger:        logger,
		slowThreshold: slowThreshold,
		internalMinio: internalMinio,
		publicMinio:   publicMinio,
	}, nil
}

// Bootstrap upserts the configured node set into Postgres so
// storage_nodes/chunk_locations FKs and the admin view have rows to
// reference before the first health probe even runs.
func (rt *Router) Bootstrap(ctx context.Context) error {
	rt.mu.RLock()
	nodes := make([]StorageNode, 0, len(rt.nodes))
	for _, n := range rt.nodes {
		nodes = append(nodes, n)
	}
	rt.mu.RUnlock()

	for _, n := range nodes {
		if err := rt.repo.UpsertNode(ctx, string(n.ID), n.Endpoint); err != nil {
			return fmt.Errorf("bootstrap node %s: %w", n.ID, err)
		}
	}
	return nil
}

// Resolve returns n distinct healthy node IDs for chunkHash, in preference
// order. It only touches in-memory state — no network I/O — which is what
// keeps failover fast: the hot path never waits on a health probe
// (docs/04-lld.md §5).
//
// Placement is latency-tiered, not purely ring-order: a first pass
// considers only healthy nodes whose rolling probe-latency EWMA is under
// slowThreshold ("fast"). Only if that pass can't fill n does a second pass
// fall back to every healthy node regardless of latency — so a
// technically-alive-but-slow node is used to complete a quorum that
// genuinely needs it, but never preferred over a faster node at the same
// ring distance (audit §02: "no capacity/latency signal folds into
// placement" was the named gap; slowThreshold 0 disables the distinction,
// reproducing the pre-fix single-pass behavior).
func (rt *Router) Resolve(chunkHash string, n int) ([]NodeID, error) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	if rt.slowThreshold > 0 {
		fast, err := rt.ring.SelectReplicas(chunkHash, n, func(id NodeID) bool {
			return rt.health[id] == StatusHealthy && rt.latencyEWMA[id] < rt.slowThreshold
		})
		if err == nil {
			return fast, nil
		}
	}

	ids, err := rt.ring.SelectReplicas(chunkHash, n, func(id NodeID) bool {
		return rt.health[id] == StatusHealthy
	})
	if err != nil {
		metrics.StoragePlacementFailuresTotal.Inc()
	}
	return ids, err
}

// Candidates returns every configured node in ring preference order for
// key, ignoring health — for locating an already-stored object whose exact
// node isn't recorded anywhere (e.g. a worker-placed thumbnail): the writer
// stored it on the first node of this same list that was healthy at write
// time, so a reader should try each in turn.
func (rt *Router) Candidates(key string) ([]NodeID, error) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.ring.SelectReplicas(key, len(rt.nodes), func(NodeID) bool { return true })
}

// RingSnapshot returns the current ring's vnode table (see Ring.VNodes) —
// the admin ring view's layout data.
func (rt *Router) RingSnapshot() []RingVNode {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.ring.VNodes()
}

// IsHealthy reports whether id is currently marked healthy — used by
// callers (e.g. file's download-plan) that need to order already-recorded
// replica locations rather than make a fresh placement decision.
func (rt *Router) IsHealthy(id NodeID) bool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.health[id] == StatusHealthy
}

// HealthCheckLoop runs until ctx is cancelled, probing every configured
// node every probeInterval (docs/02-system-design.md §2.5).
func (rt *Router) HealthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rt.probeAll(ctx)
		}
	}
}

func (rt *Router) probeAll(ctx context.Context) {
	rt.mu.RLock()
	nodes := make([]StorageNode, 0, len(rt.nodes))
	for _, n := range rt.nodes {
		nodes = append(nodes, n)
	}
	rt.mu.RUnlock()

	var wg sync.WaitGroup
	for _, node := range nodes {
		wg.Add(1)
		go func(n StorageNode) {
			defer wg.Done()
			rt.probeOne(ctx, n)
		}(node)
	}
	wg.Wait()
}

func (rt *Router) probeOne(ctx context.Context, node StorageNode) {
	ok, rtt := rt.ping(ctx, node)
	if ok {
		rt.recordLatency(node.ID, rtt)
	}

	rt.mu.RLock()
	b := rt.breakers[node.ID]
	rt.mu.RUnlock()

	var changed bool
	if ok {
		changed = b.recordSuccess()
	} else {
		changed = b.recordFailure()
	}

	newStatus := StatusHealthy
	if b.isOpen() {
		newStatus = StatusDown
	}

	rt.mu.Lock()
	rt.health[node.ID] = newStatus
	rt.mu.Unlock()

	healthValue := 0.0
	if newStatus == StatusHealthy {
		healthValue = 1.0
	}
	metrics.StorageNodeHealthy.WithLabelValues(string(node.ID)).Set(healthValue)

	if ok {
		now := time.Now()
		rt.redis.Set(ctx, statusRedisKey(node.ID), "healthy", healthTTL)
		rt.redis.Set(ctx, heartbeatRedisKey(node.ID), now.Format(time.RFC3339), 0)
		if err := rt.repo.SetHealthy(ctx, string(node.ID), now); err != nil {
			rt.logger.Error("failed to persist node health", "node", node.ID, "error", err)
		}
	} else if changed {
		if err := rt.repo.SetDown(ctx, string(node.ID)); err != nil {
			rt.logger.Error("failed to persist node health", "node", node.ID, "error", err)
		}
	}

	if changed {
		rt.logger.Info("storage node health transition", "node", node.ID, "status", newStatus.String())
		rt.redis.Publish(ctx, HealthChangesChannel, fmt.Sprintf("%s:%s", node.ID, newStatus.String()))
	}
}

// ping reports liveness and, only when ok, how long the round trip took —
// a failed/timed-out probe's "latency" (the full probeTimeout) would only
// pollute the rolling average with a number that means "unreachable," not
// "slow," so callers must ignore rtt when ok is false.
func (rt *Router) ping(ctx context.Context, node StorageNode) (ok bool, rtt time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint+"/minio/health/live", nil)
	if err != nil {
		return false, 0
	}
	start := time.Now()
	resp, err := rt.httpClient.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, time.Since(start)
}

// recordLatency updates node's rolling probe-latency EWMA from a fresh
// successful measurement, seeding it on the node's first-ever successful
// probe rather than easing in from zero (which would make a node look
// artificially fast until the average caught up).
func (rt *Router) recordLatency(id NodeID, rtt time.Duration) {
	rt.mu.Lock()
	prev, seen := rt.latencyEWMA[id]
	if !seen {
		rt.latencyEWMA[id] = rtt
	} else {
		rt.latencyEWMA[id] = time.Duration(latencyEWMAAlpha*float64(rtt) + (1-latencyEWMAAlpha)*float64(prev))
	}
	current := rt.latencyEWMA[id]
	rt.mu.Unlock()

	metrics.StorageNodeLatencyMS.WithLabelValues(string(id)).Set(float64(current.Microseconds()) / 1000.0)
}

func statusRedisKey(id NodeID) string    { return "nimbus:node:" + string(id) + ":status" }
func heartbeatRedisKey(id NodeID) string { return "nimbus:node:" + string(id) + ":last_heartbeat" }
