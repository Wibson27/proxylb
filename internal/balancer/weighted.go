package balancer

import (
	"sync"

	"github.com/Wibson27/proxylb/internal/backend"
)

// WeightedRoundRobin implements Nginx's smooth weighted round-robin.
//
// The algorithm distributes requests proportionally to weights while
// spreading them evenly across rounds (no bursts to one backend).
//
// How it works with weights [5, 1, 1] (total = 7):
//
//   Each backend has a "current weight" starting at 0.
//   On each request:
//     1. Add each backend's weight to its current weight
//     2. Pick the backend with the highest current weight
//     3. Subtract total weight from the picked backend's current weight
//
//   Round-by-round:
//     Step  | Add weights     | Current weights | Pick  | After subtract
//     1     | [5, 1, 1]      | [5, 1, 1]      | A(5)  | [-2, 1, 1]
//     2     | [5, 1, 1]      | [3, 2, 2]      | A(3)  | [-4, 2, 2]
//     3     | [5, 1, 1]      | [1, 3, 3]      | B(3)  | [1, -4, 3]
//     4     | [5, 1, 1]      | [6, -3, 4]     | A(6)  | [-1, -3, 4]
//     5     | [5, 1, 1]      | [4, -2, 5]     | C(5)  | [4, -2, -2]
//     6     | [5, 1, 1]      | [9, -1, -1]    | A(9)  | [2, -1, -1]
//     7     | [5, 1, 1]      | [7, 0, 0]      | A(7)  | [0, 0, 0]
//
//   Result: A A B A C A A — smooth distribution, not A A A A A B C.
//   After 7 requests (sum of weights), all current weights return to 0.
//
// Why a mutex instead of atomics? The algorithm reads and updates
// ALL backends' current weights in a single pick operation. This is
// a multi-variable read-modify-write — atomics only protect single
// variables. The mutex holds for ~100ns (a few additions), which is
// negligible compared to the HTTP round-trip.
type WeightedRoundRobin struct {
	mu             sync.Mutex
	currentWeights map[string]int // keyed by backend name
}

// NewWeightedRoundRobin creates a weighted round-robin balancer.
func NewWeightedRoundRobin() *WeightedRoundRobin {
	return &WeightedRoundRobin{
		currentWeights: make(map[string]int),
	}
}

// Pick selects the backend with the highest effective weight.
func (w *WeightedRoundRobin) Pick(backends []*backend.Backend) *backend.Backend {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Calculate total weight.
	totalWeight := 0
	for _, b := range backends {
		totalWeight += b.Weight
	}

	// Step 1: add each backend's weight to its current weight.
	// Step 2: find the backend with the highest current weight.
	var best *backend.Backend
	bestWeight := -1 << 31 // min int

	for _, b := range backends {
		cw := w.currentWeights[b.Name] + b.Weight
		w.currentWeights[b.Name] = cw

		if cw > bestWeight {
			bestWeight = cw
			best = b
		}
	}

	// Step 3: subtract total weight from the selected backend.
	w.currentWeights[best.Name] -= totalWeight

	return best
}
