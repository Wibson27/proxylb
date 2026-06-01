package balancer

import (
	"github.com/Wibson27/proxylb/internal/backend"
)

// LeastConnections selects the backend with the fewest active
// connections. This adapts to uneven backend performance — a slow
// backend accumulates connections and receives fewer new ones.
//
// The algorithm reads each backend's atomic activeConns counter.
// No mutex needed — each Load is a single atomic MOV instruction
// and we're just finding the minimum.
//
// Tie-breaking: when multiple backends have the same connection count,
// the first one encountered wins. This gives a slight bias toward
// earlier backends in the list, which is acceptable — under real
// load, ties are rare and transient.
type LeastConnections struct{}

// NewLeastConnections creates a least-connections balancer.
func NewLeastConnections() *LeastConnections {
	return &LeastConnections{}
}

// Pick selects the backend with the fewest active connections.
func (lc *LeastConnections) Pick(backends []*backend.Backend) *backend.Backend {
	best := backends[0]
	bestConns := best.ActiveConns()

	for _, b := range backends[1:] {
		conns := b.ActiveConns()
		if conns < bestConns {
			best = b
			bestConns = conns
		}
	}

	return best
}
