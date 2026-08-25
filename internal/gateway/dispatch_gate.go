package gateway

import "sync"

const sessionDispatchStripeCount = 64

// sessionDispatchGate serializes the short accept/dispatch critical section
// for one session without retaining an unbounded session-keyed mutex map.
// Hash collisions only add temporary serialization; they do not affect
// correctness.
type sessionDispatchGate struct {
	stripes [sessionDispatchStripeCount]sync.Mutex
}

func (g *sessionDispatchGate) Lock(sessionID string) func() {
	mu := &g.stripes[sessionDispatchStripe(sessionID)]
	mu.Lock()
	return mu.Unlock
}

// sessionDispatchStripe uses allocation-free FNV-1a. The stripe count is a
// power of two, so the mask is equivalent to modulo without a division.
func sessionDispatchStripe(sessionID string) int {
	const (
		offset32 = uint32(2166136261)
		prime32  = uint32(16777619)
	)
	hash := offset32
	for i := 0; i < len(sessionID); i++ {
		hash ^= uint32(sessionID[i])
		hash *= prime32
	}
	return int(hash & (sessionDispatchStripeCount - 1))
}
