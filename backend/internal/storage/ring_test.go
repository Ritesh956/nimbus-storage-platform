package storage

import (
	"fmt"
	"testing"
)

func allHealthy(ids ...NodeID) func(NodeID) bool {
	set := make(map[NodeID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return func(id NodeID) bool { return set[id] }
}

func TestSelectReplicas_ReturnsDistinctNodes(t *testing.T) {
	ring := BuildRing([]NodeID{"a", "b", "c"})
	ids, err := ring.SelectReplicas("some-chunk-hash", 2, allHealthy("a", "b", "c"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 replicas, got %d (%v)", len(ids), ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("replicas must be distinct, got %v", ids)
	}
}

func TestSelectReplicas_SkipsUnhealthyNodes(t *testing.T) {
	ring := BuildRing([]NodeID{"a", "b", "c"})
	ids, err := ring.SelectReplicas("some-chunk-hash", 2, allHealthy("b", "c")) // "a" is down
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, id := range ids {
		if id == "a" {
			t.Fatalf("unhealthy node 'a' should never be selected, got %v", ids)
		}
	}
}

func TestSelectReplicas_ErrorsWhenNotEnoughHealthyNodes(t *testing.T) {
	ring := BuildRing([]NodeID{"a", "b", "c"})
	ids, err := ring.SelectReplicas("some-chunk-hash", 2, allHealthy("a")) // only 1 healthy
	if err != ErrInsufficientHealthyNodes {
		t.Fatalf("want ErrInsufficientHealthyNodes, got %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("want partial result of length 1, got %d (%v)", len(ids), ids)
	}
}

func TestSelectReplicas_DeterministicForSameHash(t *testing.T) {
	ring := BuildRing([]NodeID{"a", "b", "c"})
	healthy := allHealthy("a", "b", "c")
	first, err := ring.SelectReplicas("fixed-chunk-hash", 2, healthy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ring.SelectReplicas("fixed-chunk-hash", 2, healthy)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("same chunk hash must resolve to the same replicas: %v != %v", first, second)
	}
}

func TestSelectReplicas_ReasonablyBalancedAcrossNodes(t *testing.T) {
	ring := BuildRing([]NodeID{"a", "b", "c"})
	healthy := allHealthy("a", "b", "c")

	const trials = 3000
	counts := map[NodeID]int{}
	for i := 0; i < trials; i++ {
		ids, err := ring.SelectReplicas(fmt.Sprintf("chunk-%d", i), 1, healthy)
		if err != nil {
			t.Fatal(err)
		}
		counts[ids[0]]++
	}

	// With 128 vnodes/node and random-ish SHA-1 input, expect roughly even
	// distribution (~1000 each) — a generous +/-30% band avoids a flaky
	// test while still catching a badly broken hash/placement.
	const want = trials / 3
	for _, id := range []NodeID{"a", "b", "c"} {
		if got := counts[id]; got < want*7/10 || got > want*13/10 {
			t.Errorf("node %s got %d/%d placements, want roughly %d", id, got, trials, want)
		}
	}
}

func TestSelectReplicas_NoNodesConfigured(t *testing.T) {
	ring := BuildRing(nil)
	if _, err := ring.SelectReplicas("x", 1, allHealthy()); err != ErrNoNodes {
		t.Fatalf("want ErrNoNodes, got %v", err)
	}
}
