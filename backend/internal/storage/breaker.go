package storage

import "sync"

// failureThreshold consecutive failed probes marks a node down
// (docs/02-system-design.md §2.5).
const failureThreshold = 3

type breakerState int

const (
	bClosed breakerState = iota
	bOpen
)

// breaker is a minimal 2-state circuit breaker, not the full
// closed/open/half-open sketched in docs/04-lld.md §1.3. A half-open state
// earns its keep when you need to throttle retry *frequency* against a
// struggling dependency; our health-check loop already probes every node
// on a fixed 2s cadence regardless of state, so there's no separate retry
// rate to throttle — the next scheduled probe *is* the half-open trial.
// Simplified accordingly; noted here since it's a deliberate deviation
// from the LLD sketch, not an oversight.
type breaker struct {
	mu       sync.Mutex
	state    breakerState
	failures int
}

func newBreaker() *breaker {
	return &breaker{state: bClosed}
}

// recordSuccess resets the failure count and closes the breaker. Returns
// whether this changed the state (i.e. it was open before).
func (b *breaker) recordSuccess() (changed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	was := b.state
	b.failures = 0
	b.state = bClosed
	return was != bClosed
}

// recordFailure increments the consecutive-failure count and opens the
// breaker once failureThreshold is reached. Returns whether this changed
// the state.
func (b *breaker) recordFailure() (changed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	was := b.state
	b.failures++
	if b.failures >= failureThreshold {
		b.state = bOpen
	}
	return was != b.state
}

func (b *breaker) isOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == bOpen
}
