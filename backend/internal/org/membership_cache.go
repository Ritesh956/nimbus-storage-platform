package org

import (
	"sync"
	"time"
)

// membershipCacheTTL bounds how long RequireRole trusts a cached membership
// lookup (audit §05: "every write endpoint re-derives the caller's
// membership/role from the DB on each request... but there's no short-lived
// cache for it"). Deliberately tiny — long enough to absorb a chatty page's
// burst of mutating calls through the same gated route, short enough that a
// just-removed member's access can't meaningfully outlive their removal.
// There is no explicit invalidation on membership change (AddMember,
// RemoveMember): the TTL alone bounds staleness, which the audit itself
// frames as an acceptable trade at this project's scale ("fine at current
// scale, a real latency tax if org membership tables grow").
const membershipCacheTTL = 5 * time.Second

type membershipCacheEntry struct {
	member    Member
	expiresAt time.Time
}

// membershipCache is deliberately in-process rather than Redis-backed: each
// API replica caching independently for a few seconds is a fine trade for
// not adding a new cross-replica invalidation path, for a gap this minor.
type membershipCache struct {
	mu      sync.Mutex
	entries map[string]membershipCacheEntry
}

func newMembershipCache() *membershipCache {
	return &membershipCache{entries: make(map[string]membershipCacheEntry)}
}

func (c *membershipCache) get(orgID, userID string) (Member, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[orgID+":"+userID]
	if !ok || time.Now().After(e.expiresAt) {
		return Member{}, false
	}
	return e.member, true
}

func (c *membershipCache) set(orgID, userID string, m Member) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	// Opportunistic sweep of expired entries — cheap, and keeps a
	// long-running process from accumulating one entry per user/org pair
	// ever seen instead of just the currently-active ones.
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
	c.entries[orgID+":"+userID] = membershipCacheEntry{member: m, expiresAt: now.Add(membershipCacheTTL)}
}
