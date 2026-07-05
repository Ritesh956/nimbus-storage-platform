// Package ratelimit is the per-caller Redis token bucket designed in
// docs/04-lld.md §4 (middleware chain: requestID → recover → logger →
// rateLimiter → auth). Buckets live in Redis so the limit holds across
// nimbus-api replicas; the refill math runs in a Lua script so concurrent
// requests can't race the read-modify-write.
package ratelimit

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"nimbus/internal/platform/httpserver"
)

// script implements a classic token bucket: refill by elapsed time × rate
// (capped at burst), then spend one token if available. State is a hash
// {tokens, ts(ms)} with a TTL of two full refill windows, so idle buckets
// clean themselves up.
var script = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local state = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(state[1])
local ts = tonumber(state[2])
if tokens == nil or ts == nil then
	tokens = burst
	ts = now
end

tokens = math.min(burst, tokens + math.max(0, now - ts) / 1000 * rate)
local allowed = 0
if tokens >= 1 then
	tokens = tokens - 1
	allowed = 1
end

redis.call('HSET', key, 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', key, math.ceil(burst / rate * 2000))
return allowed
`)

type Limiter struct {
	rdb    *redis.Client
	rate   float64 // tokens refilled per second
	burst  int     // bucket capacity
	logger *slog.Logger
}

func NewLimiter(rdb *redis.Client, ratePerSecond, burst int, logger *slog.Logger) *Limiter {
	return &Limiter{rdb: rdb, rate: float64(ratePerSecond), burst: burst, logger: logger}
}

// Allow spends one token from key's bucket, reporting whether the request
// may proceed.
func (l *Limiter) Allow(ctx context.Context, key string) (bool, error) {
	res, err := script.Run(ctx, l.rdb, []string{"nimbus:rl:" + key},
		l.rate, l.burst, time.Now().UnixMilli()).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

// Middleware enforces the limit per keyFn(r), skipping the operational
// endpoints (health probes and Prometheus scrapes poll on fixed intervals
// from one address and must never eat a caller's budget). Redis failure
// fails open — for this system, degraded abuse protection beats turning a
// Redis blip into a full API outage.
func (l *Limiter) Middleware(keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/healthz", "/readyz", "/metrics":
				next.ServeHTTP(w, r)
				return
			}
			allowed, err := l.Allow(r.Context(), keyFn(r))
			if err != nil {
				l.logger.Warn("rate limiter unavailable, failing open", "error", err)
				next.ServeHTTP(w, r)
				return
			}
			if !allowed {
				w.Header().Set("Retry-After", "1")
				httpserver.WriteError(w, r, httpserver.ErrRateLimited, "too many requests, slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP is the fallback bucket key for unauthenticated callers: first
// X-Forwarded-For hop when a reverse proxy is in front (the go-public plan
// puts Caddy there), else the connection's remote host.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
