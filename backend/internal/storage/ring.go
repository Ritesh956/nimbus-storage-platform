package storage

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"sort"
)

// 128 virtual nodes per physical node — enough for reasonably even
// distribution across a handful of nodes without the vnode table being
// unwieldy to reason about (docs/07-distributed-architecture.md §4).
const virtualNodesPerPhysical = 128

type vnode struct {
	hash uint32
	node NodeID
}

// Ring is immutable once built; a membership change rebuilds and swaps the
// whole ring rather than mutating it in place (docs/04-lld.md §1.2) — that
// separation from live health state is what keeps failover from requiring
// a rebuild.
type Ring struct {
	vnodes []vnode
}

func BuildRing(nodeIDs []NodeID) *Ring {
	vnodes := make([]vnode, 0, len(nodeIDs)*virtualNodesPerPhysical)
	for _, id := range nodeIDs {
		for i := 0; i < virtualNodesPerPhysical; i++ {
			vnodes = append(vnodes, vnode{hash: hashKey(fmt.Sprintf("%s#%d", id, i)), node: id})
		}
	}
	sort.Slice(vnodes, func(i, j int) bool { return vnodes[i].hash < vnodes[j].hash })
	return &Ring{vnodes: vnodes}
}

// hashKey is SHA-1 truncated to uint32 — simple and not a bottleneck at
// this node count (docs/07-distributed-architecture.md §4).
func hashKey(s string) uint32 {
	sum := sha1.Sum([]byte(s))
	return binary.BigEndian.Uint32(sum[:4])
}

// RingVNode is one virtual node in a ring snapshot — the read model behind
// GET /v1/admin/ring (backlog #13). Position is the vnode's point on the
// uint32 ring; the admin UI maps it to an angle.
type RingVNode struct {
	Position uint32
	Node     NodeID
}

// VNodes returns the ring's vnode table in position order. The ring is
// immutable once built, so handing out a copy is race-free by construction.
func (r *Ring) VNodes() []RingVNode {
	out := make([]RingVNode, len(r.vnodes))
	for i, vn := range r.vnodes {
		out[i] = RingVNode{Position: vn.hash, Node: vn.node}
	}
	return out
}

// PositionOf exposes the ring's key-hashing (SHA-1 → uint32) so the admin
// ring view can plot where a chunk hash lands — must stay the same function
// placement uses, or the diagram would lie.
func PositionOf(key string) uint32 {
	return hashKey(key)
}

// search returns the index of the first vnode at or after h on the ring,
// wrapping to 0 (O(log V), binary search over the sorted vnode slice).
func (r *Ring) search(h uint32) int {
	idx := sort.Search(len(r.vnodes), func(i int) bool { return r.vnodes[i].hash >= h })
	if idx == len(r.vnodes) {
		idx = 0
	}
	return idx
}

// SelectReplicas walks the ring clockwise from chunkHash's position,
// collecting the first n distinct physical nodes for which isHealthy
// returns true — the Dynamo-style preference-list placement from
// docs/02-system-design.md §2.2. If fewer than n healthy nodes are found
// after a full walk, it returns whatever it found alongside
// ErrInsufficientHealthyNodes.
func (r *Ring) SelectReplicas(chunkHash string, n int, isHealthy func(NodeID) bool) ([]NodeID, error) {
	if len(r.vnodes) == 0 {
		return nil, ErrNoNodes
	}
	start := r.search(hashKey(chunkHash))

	seen := make(map[NodeID]bool, n)
	var out []NodeID
	for i := 0; i < len(r.vnodes) && len(out) < n; i++ {
		vn := r.vnodes[(start+i)%len(r.vnodes)]
		if seen[vn.node] {
			continue
		}
		seen[vn.node] = true
		if isHealthy(vn.node) {
			out = append(out, vn.node)
		}
	}
	if len(out) < n {
		return out, ErrInsufficientHealthyNodes
	}
	return out, nil
}
