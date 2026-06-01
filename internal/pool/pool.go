// Package pool manages the set of upstream backends and provides
// filtered views (healthy backends only) to the proxy.
//
// Thread safety: a sync.RWMutex protects the backends slice.
// The proxy reads (RLock) on every request, the health checker
// writes (Lock) when marking backends up/down. RWMutex allows
// concurrent proxy requests to read without blocking each other.
package pool

import (
	"fmt"
	"sync"

	"github.com/Wibson27/proxylb/internal/backend"
	"github.com/Wibson27/proxylb/internal/config"
)

// Pool manages a set of backends.
type Pool struct {
	mu       sync.RWMutex
	backends []*backend.Backend
}

// New creates a Pool from the config backend list.
func New(cfgs []config.BackendConfig) (*Pool, error) {
	backends := make([]*backend.Backend, 0, len(cfgs))

	for _, cfg := range cfgs {
		b, err := backend.New(cfg.Name, cfg.URL, cfg.Weight, cfg.HealthURL)
		if err != nil {
			return nil, fmt.Errorf("backend %q: %w", cfg.Name, err)
		}
		backends = append(backends, b)
	}

	return &Pool{backends: backends}, nil
}

// HealthyBackends returns a snapshot of all backends currently marked
// healthy. This is the method the proxy calls on every request.
//
// Returns a new slice — the caller can iterate it without holding
// the lock. The backend pointers are shared, but their atomic fields
// (activeConns, healthy) are safe to read concurrently.
func (p *Pool) HealthyBackends() []*backend.Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	healthy := make([]*backend.Backend, 0, len(p.backends))
	for _, b := range p.backends {
		if b.IsHealthy() {
			healthy = append(healthy, b)
		}
	}
	return healthy
}

// AllBackends returns all backends regardless of health status.
// Used by the admin API and health checker.
func (p *Pool) AllBackends() []*backend.Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]*backend.Backend, len(p.backends))
	copy(result, p.backends)
	return result
}
