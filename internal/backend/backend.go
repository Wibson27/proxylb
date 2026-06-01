// Package backend defines the upstream server model with atomic
// connection tracking and health state.
//
// Atomic operations are used for activeConns and healthy because
// these fields are read by the balancer on every request and written
// by the health checker and proxy. Using sync/atomic avoids taking
// a mutex on the hot path (every proxied request).
//
// On x86-64, atomic.Int64.Load compiles to a single MOV instruction
// with aligned access — the CPU guarantees atomicity for aligned
// 64-bit reads without any locking:
//
//   MOV rax, [rdi+offset]   ; atomic because aligned 8-byte load
//
// atomic.Int64.Add compiles to LOCK XADD — an atomic read-modify-write
// that asserts the cache line lock:
//
//   LOCK XADD [rdi+offset], rax   ; atomic increment
//
// This is ~10ns vs ~50-100ns for an uncontended mutex Lock/Unlock pair.
// On the proxy hot path where every request reads activeConns, this
// matters.
package backend

import (
	"net/url"
	"sync/atomic"
)

// Backend represents an upstream server.
type Backend struct {
	// Name is a human-readable identifier.
	Name string

	// URL is the parsed backend URL.
	URL *url.URL

	// Weight controls traffic distribution in weighted round-robin.
	Weight int

	// HealthURL is the endpoint for health checks.
	HealthURL string

	// activeConns tracks in-flight requests to this backend.
	// Read by least-connections balancer, incremented/decremented
	// by the proxy handler.
	activeConns atomic.Int64

	// healthy indicates whether the backend is available.
	// Set by the health checker, read by the balancer.
	healthy atomic.Bool

	// consecutiveFailures tracks sequential health check failures
	// for the circuit breaker. Reset to zero on a successful check.
	consecutiveFailures atomic.Int64
}

// New creates a Backend from a URL string. Returns an error if the
// URL is unparseable.
func New(name, rawURL string, weight int, healthURL string) (*Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	b := &Backend{
		Name:      name,
		URL:       u,
		Weight:    weight,
		HealthURL: healthURL,
	}
	b.healthy.Store(true) // assume healthy until proven otherwise

	return b, nil
}

// ActiveConns returns the current number of in-flight requests.
func (b *Backend) ActiveConns() int64 {
	return b.activeConns.Load()
}

// IncrConns atomically increments the active connection count.
// Called when a request starts being proxied.
func (b *Backend) IncrConns() {
	b.activeConns.Add(1)
}

// DecrConns atomically decrements the active connection count.
// Called when a proxied request completes (in a defer).
func (b *Backend) DecrConns() {
	b.activeConns.Add(-1)
}

// IsHealthy returns whether the backend is considered healthy.
func (b *Backend) IsHealthy() bool {
	return b.healthy.Load()
}

// SetHealthy updates the health status.
func (b *Backend) SetHealthy(v bool) {
	b.healthy.Store(v)
}

// ConsecutiveFailures returns the current failure count.
func (b *Backend) ConsecutiveFailures() int64 {
	return b.consecutiveFailures.Load()
}

// IncrFailures atomically increments the consecutive failure count.
func (b *Backend) IncrFailures() int64 {
	return b.consecutiveFailures.Add(1)
}

// ResetFailures sets the consecutive failure count to zero.
func (b *Backend) ResetFailures() {
	b.consecutiveFailures.Store(0)
}
