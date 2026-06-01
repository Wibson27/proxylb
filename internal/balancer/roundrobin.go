package balancer

import (
	"sync/atomic"

	"github.com/Wibson27/proxylb/internal/backend"
)

// RoundRobin distributes requests evenly across backends by cycling
// through them in order. Each call to Pick advances the counter.
//
// The counter is an atomic uint64 — no mutex needed. On overflow
// (after 2^64 requests), it wraps to 0. Since we take modulo the
// backend count, the distribution stays even.
//
// Thread safety: atomic.Uint64.Add is a single LOCK XADD instruction
// on x86-64. Multiple goroutines can call Pick concurrently without
// any contention beyond the cache line bounce on the counter.
type RoundRobin struct {
	counter atomic.Uint64
}

// NewRoundRobin creates a round-robin balancer.
func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

// Pick selects the next backend in round-robin order.
func (rr *RoundRobin) Pick(backends []*backend.Backend) *backend.Backend {
	n := uint64(len(backends))
	idx := rr.counter.Add(1) - 1 // Add returns new value, subtract 1 for 0-based
	return backends[idx%n]
}
