package org

import (
	"testing"
	"time"
)

// Audit §14: org had zero automated tests of any kind. membershipCache is
// pure in-memory state with no DB dependency, so it's tested white-box here
// rather than via an integration test — matches the house style set by
// storage/ring_test.go (plain package, table-driven where it helps).

func TestMembershipCache_MissOnUnknownKey(t *testing.T) {
	c := newMembershipCache()
	if _, ok := c.get("org-1", "user-1"); ok {
		t.Fatal("expected a miss for a key that was never set")
	}
}

func TestMembershipCache_HitBeforeExpiry(t *testing.T) {
	c := newMembershipCache()
	want := Member{OrgID: "org-1", UserID: "user-1", Role: RoleAdmin}
	c.set("org-1", "user-1", want)

	got, ok := c.get("org-1", "user-1")
	if !ok {
		t.Fatal("expected a hit immediately after set")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMembershipCache_MissAfterTTLExpires(t *testing.T) {
	c := newMembershipCache()
	// Directly construct an already-expired entry rather than sleeping
	// membershipCacheTTL (5s) in a unit test — same intent, no wall-clock
	// cost.
	c.mu.Lock()
	c.entries["org-1:user-1"] = membershipCacheEntry{
		member:    Member{OrgID: "org-1", UserID: "user-1", Role: RoleOwner},
		expiresAt: time.Now().Add(-1 * time.Second),
	}
	c.mu.Unlock()

	if _, ok := c.get("org-1", "user-1"); ok {
		t.Fatal("expected a miss for an entry past its TTL")
	}
}

func TestMembershipCache_KeysAreScopedPerOrgAndUser(t *testing.T) {
	c := newMembershipCache()
	c.set("org-1", "user-1", Member{OrgID: "org-1", UserID: "user-1", Role: RoleMember})
	c.set("org-2", "user-1", Member{OrgID: "org-2", UserID: "user-1", Role: RoleOwner})

	m1, ok := c.get("org-1", "user-1")
	if !ok || m1.Role != RoleMember {
		t.Fatalf("org-1 membership got %+v, ok=%v", m1, ok)
	}
	m2, ok := c.get("org-2", "user-1")
	if !ok || m2.Role != RoleOwner {
		t.Fatalf("org-2 membership got %+v, ok=%v", m2, ok)
	}
}

func TestMembershipCache_SetSweepsExpiredEntries(t *testing.T) {
	c := newMembershipCache()
	c.mu.Lock()
	c.entries["stale:entry"] = membershipCacheEntry{
		member:    Member{OrgID: "stale", UserID: "entry"},
		expiresAt: time.Now().Add(-time.Minute),
	}
	c.mu.Unlock()

	// Any set() call opportunistically sweeps expired entries (see the
	// comment on membershipCache.set) — this asserts that behavior actually
	// happens rather than just trusting the comment.
	c.set("org-3", "user-3", Member{OrgID: "org-3", UserID: "user-3", Role: RoleMember})

	c.mu.Lock()
	_, stillThere := c.entries["stale:entry"]
	c.mu.Unlock()
	if stillThere {
		t.Fatal("expected the expired entry to be swept on the next set()")
	}
}
