package storage

import (
	"fmt"
	"testing"
	"time"
)

// newTestRouter builds a real Router (exercising the same NewRouter path
// production uses) against fake, never-dialed endpoints — buildMinIOClient
// only parses the URL and constructs a client, it doesn't connect, so this
// needs no network and no real MinIO. Tests then poke the unexported
// health/latencyEWMA maps directly (same package) rather than waiting on a
// real health-check loop, since what's under test is Resolve's placement
// logic, not the probe loop itself.
func newTestRouter(t *testing.T, ids []NodeID, slowThreshold time.Duration) *Router {
	t.Helper()
	nodes := make([]StorageNode, len(ids))
	for i, id := range ids {
		nodes[i] = StorageNode{ID: id, Endpoint: "http://" + string(id) + ":9000", PublicEndpoint: "http://" + string(id) + ":9000"}
	}
	rt, err := NewRouter(nil, nil, nodes, "access", "secret", nil, slowThreshold)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return rt
}

func allHealthyStatus(ids ...NodeID) map[NodeID]NodeStatus {
	m := make(map[NodeID]NodeStatus, len(ids))
	for _, id := range ids {
		m[id] = StatusHealthy
	}
	return m
}

// TestResolve_SinksSlowNodeBehindFasterOnes proves the audit §02 fix: with
// enough fast healthy nodes to fill the quorum, a node whose latency EWMA
// is over the threshold is never chosen, across many distinct chunk
// hashes — not just deprioritized on average, genuinely excluded from the
// first-tier pass.
func TestResolve_SinksSlowNodeBehindFasterOnes(t *testing.T) {
	rt := newTestRouter(t, []NodeID{"a", "b", "c"}, 50*time.Millisecond)
	rt.health = allHealthyStatus("a", "b", "c")
	rt.latencyEWMA = map[NodeID]time.Duration{
		"a": 2 * time.Millisecond,
		"b": 200 * time.Millisecond, // slow
		"c": 2 * time.Millisecond,
	}

	for i := 0; i < 200; i++ {
		hash := fmt.Sprintf("chunk-%d", i)
		ids, err := rt.Resolve(hash, 2)
		if err != nil {
			t.Fatalf("chunk-%d: unexpected error: %v", i, err)
		}
		for _, id := range ids {
			if id == "b" {
				t.Fatalf("chunk-%d: slow node b was chosen even though 2 fast healthy nodes exist: %v", i, ids)
			}
		}
	}
}

// TestResolve_SlowNodeStillFillsGenuineShortfall proves the "sinks, isn't
// dropped" half: when the fast tier alone can't meet the quorum, a slow
// node is still used rather than the write failing outright.
func TestResolve_SlowNodeStillFillsGenuineShortfall(t *testing.T) {
	rt := newTestRouter(t, []NodeID{"a", "b"}, 50*time.Millisecond)
	rt.health = allHealthyStatus("a", "b")
	rt.latencyEWMA = map[NodeID]time.Duration{
		"a": 2 * time.Millisecond,
		"b": 200 * time.Millisecond, // slow, but the only other node
	}

	ids, err := rt.Resolve("some-chunk-hash", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 replicas (only 2 nodes total, both needed), got %v", ids)
	}
	found := map[NodeID]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["b"] {
		t.Fatalf("slow node b should have filled the genuine shortfall, got %v", ids)
	}
}

// TestResolve_ZeroThresholdDisablesTiering confirms slowThreshold: 0 (the
// zero value, e.g. an unset config knob) reproduces the pre-fix
// single-pass behavior rather than silently misbehaving.
func TestResolve_ZeroThresholdDisablesTiering(t *testing.T) {
	rt := newTestRouter(t, []NodeID{"a", "b", "c"}, 0)
	rt.health = allHealthyStatus("a", "b", "c")
	rt.latencyEWMA = map[NodeID]time.Duration{"b": time.Hour} // absurdly slow, must not matter

	seenB := false
	for i := 0; i < 200; i++ {
		ids, err := rt.Resolve(fmt.Sprintf("chunk-%d", i), 2)
		if err != nil {
			t.Fatalf("chunk-%d: unexpected error: %v", i, err)
		}
		for _, id := range ids {
			if id == "b" {
				seenB = true
			}
		}
	}
	if !seenB {
		t.Fatal("with tiering disabled (threshold 0), node b should be selectable on ring merit like any other healthy node")
	}
}
