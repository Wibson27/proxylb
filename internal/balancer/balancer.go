// Package balancer defines the load balancing algorithm interface
// and provides three implementations: round-robin, weighted
// round-robin, and least connections.
//
// The interface is a single method — Pick — that selects a backend
// from a slice of healthy backends. The pool (caller) is responsible
// for filtering out unhealthy backends before calling Pick.
//
// Why an interface? The algorithm is selected at startup via config
// and never changes. A simple function type would work too, but an
// interface allows each algorithm to carry its own state (e.g. the
// round-robin counter or weighted current weights).
package balancer

import (
	"github.com/Wibson27/proxylb/internal/backend"
)

// Balancer selects a backend from a list of healthy backends.
// The slice is guaranteed to have at least one element.
type Balancer interface {
	Pick(backends []*backend.Backend) *backend.Backend
}
