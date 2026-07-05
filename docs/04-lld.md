# Low-Level Design — Nimbus Storage Platform

Status: DRAFT
Version: 0.1
Depends on: [03-hld.md](03-hld.md)

Scope: interfaces, key types, concurrency model, and algorithms for the modules where "how exactly does this work" isn't obvious from the HLD — mainly `storage`, `upload`, `auth`. Straightforward CRUD modules (`org`, `folder`, `search`, `activity`) aren't detailed here; they're standard repository-pattern Postgres access and don't need it.

## 1. `storage` module — hash ring, health, placement

### 1.1 Core types

```go
type NodeID string

type Replica struct {
    Node     NodeID
    Endpoint string // MinIO endpoint for this node
}

type NodeStatus int
const (
    StatusHealthy NodeStatus = iota
    StatusDown
)

// Ring is immutable once built; rebuilt (not mutated) when node set changes.
type Ring struct {
    vnodes []vnode // sorted by hash, 128 vnodes per physical node
}

type vnode struct {
    hash uint32
    node NodeID
}

type Router struct {
    mu      sync.RWMutex
    ring    *Ring              // swapped atomically on membership change
    health  map[NodeID]NodeStatus
    breaker map[NodeID]*breaker
    redis   *redis.Client       // source of truth for health across replicas
}
```

### 1.2 Placement algorithm

```go
// Resolve returns N distinct healthy replicas for a chunk hash, in
// preference order (first = primary for reads).
func (r *Router) Resolve(chunkHash string, n int) ([]Replica, error) {
    r.mu.RLock()
    ring := r.ring
    r.mu.RUnlock()

    pos := ring.search(hash32(chunkHash))
    seen := map[NodeID]bool{}
    var out []Replica
    for i := 0; i < len(ring.vnodes) && len(out) < n; i++ {
        vn := ring.vnodes[(pos+i)%len(ring.vnodes)]
        if seen[vn.node] {
            continue
        }
        seen[vn.node] = true
        if r.isHealthy(vn.node) { // reads r.health, populated from Redis
            out = append(out, r.replicaFor(vn.node))
        }
    }
    if len(out) < n {
        return out, ErrInsufficientHealthyNodes // caller decides: degrade or fail
    }
    return out, nil
}
```
`ring.search` is a binary search over the sorted vnode hash slice (`sort.Search`), O(log(V·N)). Ring is rebuilt (new slice, atomically swapped via the mutex) only when a node is added/removed permanently — **not** on every health flap, since health is a separate, fast-changing overlay (`r.health`), not part of ring structure. This separation is why failover doesn't require rebuilding the ring.

### 1.3 Health check loop

```go
func (r *Router) HealthCheckLoop(ctx context.Context) {
    ticker := time.NewTicker(2 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            var wg sync.WaitGroup
            for _, node := range r.allNodes() {
                wg.Add(1)
                go func(n NodeID) {
                    defer wg.Done()
                    r.probe(n) // updates r.breaker[n], writes health to Redis on state change
                }(node)
            }
            wg.Wait()
        }
    }
}
```
- Probes run concurrently (one goroutine per node, bounded by node count which is small — no worker pool needed at this scale).
- **Corrected from this doc's original sketch**: each node has a `breaker` (`internal/storage/breaker.go`), but it's **2-state (closed/open), not 3-state with half-open** — a deliberate simplification made while implementing (Day 4), not an oversight. A half-open state earns its keep when you need to throttle retry *frequency*; the health-check loop already probes every node on a fixed 2s cadence regardless of state, so there's no separate retry rate to throttle — the next scheduled probe *is* the half-open trial. 3 consecutive failures opens the breaker; the next successful probe closes it immediately.
- **Also corrected**: `Router.Resolve`/`IsHealthy` read `rt.health`, an in-memory map updated *directly* by the same process's health-check loop — not refreshed from Redis at all. Redis (TTL keys + pub/sub) is written by the health loop for cross-process visibility (the admin endpoint reads Postgres, not Redis; a hypothetical second `nimbus-api` replica could read Redis) but is never read on the hot path. This is what keeps `Resolve` fast — no Redis round-trip per chunk resolution, not even an indirect one via a poll.

## 2. `upload` module — chunk state machine

```go
type ChunkState int
const (
    ChunkPending ChunkState = iota
    ChunkPresigned
    ChunkCommitted
)
```

State transitions are enforced server-side (not trusted from the client):
`Pending --InitChunk--> Presigned --CommitChunk(etags)--> Committed`

```go
func (s *UploadService) CommitChunk(ctx context.Context, uploadID string, hash string, etags map[NodeID]string) error {
    rec, err := s.repo.GetChunkAttempt(ctx, uploadID, hash)
    if err != nil { return err }
    if rec.State != ChunkPresigned {
        return ErrInvalidState // rejects replays/out-of-order commits
    }
    for node, etag := range etags {
        if !s.verifyETag(hash, etag) {
            return ErrChecksumMismatch // NFR-5: must catch injected corruption
        }
        _ = node
    }
    return s.repo.MarkCommitted(ctx, uploadID, hash, etags) // idempotent: re-commit of same hash is a no-op (SRS/HLD idempotency note)
}
```
`CompleteUpload` (called once all chunks committed) requires a client-supplied `Idempotency-Key` header; the handler upserts on that key so a retried "complete" call can't create two `FileVersion` rows for one logical upload.

## 3. `auth` module — token mechanics

- Access token: JWT, HS256 (single-service, no need for asymmetric keys yet — noted as a roadmap item if `auth` is ever split into its own service, where RS256 + JWKS would replace this), 15 min TTL, claims `{sub, org_id, role, exp, iat, jti}`.
- Refresh token: opaque random 256-bit token, stored hashed (SHA-256) in Postgres with `family_id`, `used_at`, `expires_at`.
- **Rotation on use**: every refresh call issues a new refresh token in the same `family_id` and marks the old one `used_at`. If a refresh token with a non-null `used_at` is presented again, the entire `family_id` is revoked — this is the standard reuse-detection pattern for stolen refresh tokens, worth calling out explicitly since it's a common interview follow-up ("what if a refresh token is stolen and replayed?").
- Access-token revocation before natural expiry (e.g. logout) is handled by a short Redis blacklist keyed on `jti`, TTL'd to the token's remaining lifetime — bounded set size since entries expire with the token anyway.

## 4. Middleware chain (HTTP layer, applied in order)

`requestID → recover(panic→500) → structuredLogger → rateLimiter → authMiddleware (skip for public routes) → orgMembershipCheck (route-scoped) → handler`

Rate limiter and auth run *before* handler-level validation so abuse is rejected as cheaply as possible.

The rate limiter (built in the Tier 2 backlog session — `internal/platform/ratelimit`) is a Redis token bucket run as a Lua script (atomic refill+spend, shared across api replicas). Bucket key: user ID when the request carries a signature-valid access token (`auth.Service.PeekUserID` — signature check only, no blacklist round-trip; real authorization still happens in authMiddleware), else client IP (first X-Forwarded-For hop, for the eventual reverse-proxy deployment). Defaults 25 rps / burst 50 (`NIMBUS_RATE_LIMIT_RPS`/`_BURST`, RPS 0 disables). `/healthz`, `/readyz`, `/metrics` are exempt — fixed-interval pollers must not eat a caller's budget. Redis failure fails open: degraded abuse protection beats turning a Redis blip into an API outage.

## 5. Concurrency notes worth stating explicitly

- `Router.Resolve` is called on the hot path (every chunk init/read) and must not block on network I/O — it only ever touches in-memory state (`r.ring`, `r.health`), both kept current by background goroutines. This is the key design property that makes failover fast: the hot path never waits on a health probe.
- Chunk upload verification (`CommitChunk`) for multiple chunks of one file can run concurrently from the client (parallel upload, per SRS FR-6); server-side, `MarkCommitted` is a single-row upsert keyed on `(upload_id, chunk_hash)` — Postgres handles the concurrency, no application-level locking needed.
- Worker's NATS consumer (`events.Subscribe`, Day 9) uses JetStream's `Consume` callback with explicit ack — corrected from this doc's original sketch of "a bounded goroutine pool (e.g. 4 workers)": the actual implementation doesn't configure explicit concurrency, it processes deliveries via the callback as JetStream invokes it. Acks only after chunk reassembly + thumbnail storage + `SetThumbnailKey` all succeed, so a crash mid-processing results in redelivery (up to 5 attempts, `[1s,5s,15s,30s,60s]` backoff) rather than a silently-lost thumbnail.

## 6. Resolved decisions

- **HS256 JWT** (not RS256/JWKS) — confirmed and implemented (`internal/auth/jwt.go`). Revisit if auth is ever split into its own service.
