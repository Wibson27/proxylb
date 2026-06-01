package balancer

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Wibson27/proxylb/internal/backend"
)

// makeBackends creates test backends with the given names and weights.
func makeBackends(names []string, weights []int) []*backend.Backend {
	backends := make([]*backend.Backend, len(names))
	for i, name := range names {
		port := 3001 + i
		url := fmt.Sprintf("http://localhost:%d", port)
		b, _ := backend.New(name, url, weights[i], "")
		backends[i] = b
	}
	return backends
}

// --- Round-robin tests ---

func TestRoundRobin_CyclesInOrder(t *testing.T) {
	rr := NewRoundRobin()
	backends := makeBackends([]string{"a", "b", "c"}, []int{1, 1, 1})

	// Two full cycles.
	want := []string{"a", "b", "c", "a", "b", "c"}
	for i, expected := range want {
		got := rr.Pick(backends)
		if got.Name != expected {
			t.Errorf("pick %d: got %q, want %q", i, got.Name, expected)
		}
	}
}

func TestRoundRobin_SingleBackend(t *testing.T) {
	rr := NewRoundRobin()
	backends := makeBackends([]string{"only"}, []int{1})

	for range 5 {
		got := rr.Pick(backends)
		if got.Name != "only" {
			t.Errorf("got %q, want only", got.Name)
		}
	}
}

func TestRoundRobin_Concurrent(t *testing.T) {
	rr := NewRoundRobin()
	backends := makeBackends([]string{"a", "b", "c"}, []int{1, 1, 1})

	const requests = 900
	counts := make(map[string]int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(requests)

	for range requests {
		go func() {
			defer wg.Done()
			b := rr.Pick(backends)
			mu.Lock()
			counts[b.Name]++
			mu.Unlock()
		}()
	}

	wg.Wait()

	// With round-robin and 900 requests across 3 backends,
	// each should get exactly 300.
	for _, name := range []string{"a", "b", "c"} {
		if counts[name] != 300 {
			t.Errorf("%s got %d requests, want 300", name, counts[name])
		}
	}
}

// --- Weighted round-robin tests ---

func TestWeightedRoundRobin_Distribution(t *testing.T) {
	wrr := NewWeightedRoundRobin()
	// Weights 5:1:1, total = 7. Over 7 requests: A=5, B=1, C=1.
	backends := makeBackends([]string{"a", "b", "c"}, []int{5, 1, 1})

	counts := map[string]int{}
	for range 7 {
		b := wrr.Pick(backends)
		counts[b.Name]++
	}

	if counts["a"] != 5 {
		t.Errorf("a got %d, want 5", counts["a"])
	}
	if counts["b"] != 1 {
		t.Errorf("b got %d, want 1", counts["b"])
	}
	if counts["c"] != 1 {
		t.Errorf("c got %d, want 1", counts["c"])
	}
}

func TestWeightedRoundRobin_SmoothDistribution(t *testing.T) {
	wrr := NewWeightedRoundRobin()
	// With weights 5:1:1 over 7 requests, the Nginx smooth algorithm
	// should NOT produce "a a a a a b c". It should interleave.
	backends := makeBackends([]string{"a", "b", "c"}, []int{5, 1, 1})

	picks := make([]string, 7)
	for i := range 7 {
		picks[i] = wrr.Pick(backends).Name
	}

	// Check that "a" doesn't appear 5 times consecutively.
	consecutive := 1
	for i := 1; i < len(picks); i++ {
		if picks[i] == picks[i-1] {
			consecutive++
			if consecutive >= 4 {
				t.Errorf("4+ consecutive picks of %q — not smooth: %v", picks[i], picks)
			}
		} else {
			consecutive = 1
		}
	}
}

func TestWeightedRoundRobin_EqualWeights(t *testing.T) {
	wrr := NewWeightedRoundRobin()
	backends := makeBackends([]string{"a", "b"}, []int{1, 1})

	counts := map[string]int{}
	for range 10 {
		counts[wrr.Pick(backends).Name]++
	}

	if counts["a"] != 5 || counts["b"] != 5 {
		t.Errorf("equal weights should give equal distribution: a=%d, b=%d",
			counts["a"], counts["b"])
	}
}

func TestWeightedRoundRobin_Cycle(t *testing.T) {
	wrr := NewWeightedRoundRobin()
	// Weights 3:1:1, total = 5. After 5 picks, current weights return to 0.
	// A second cycle of 5 should produce the same distribution.
	backends := makeBackends([]string{"a", "b", "c"}, []int{3, 1, 1})

	cycle1 := make([]string, 5)
	for i := range 5 {
		cycle1[i] = wrr.Pick(backends).Name
	}

	cycle2 := make([]string, 5)
	for i := range 5 {
		cycle2[i] = wrr.Pick(backends).Name
	}

	for i := range 5 {
		if cycle1[i] != cycle2[i] {
			t.Errorf("cycle1 != cycle2 at position %d: %v vs %v", i, cycle1, cycle2)
			break
		}
	}
}

// --- Least-connections tests ---

func TestLeastConnections_PicksLowest(t *testing.T) {
	lc := NewLeastConnections()
	backends := makeBackends([]string{"a", "b", "c"}, []int{1, 1, 1})

	// a=0, b=0, c=0 → picks first (a).
	got := lc.Pick(backends)
	if got.Name != "a" {
		t.Errorf("all zero: got %q, want a (first)", got.Name)
	}

	// Give a and b some connections, c stays at 0.
	backends[0].IncrConns() // a=1
	backends[0].IncrConns() // a=2
	backends[1].IncrConns() // b=1

	got = lc.Pick(backends)
	if got.Name != "c" {
		t.Errorf("a=2,b=1,c=0: got %q, want c", got.Name)
	}
}

func TestLeastConnections_TieBreaking(t *testing.T) {
	lc := NewLeastConnections()
	backends := makeBackends([]string{"a", "b", "c"}, []int{1, 1, 1})

	// All equal — should pick the first one encountered.
	got := lc.Pick(backends)
	if got.Name != "a" {
		t.Errorf("tie: got %q, want a (first)", got.Name)
	}
}

func TestLeastConnections_DynamicRebalance(t *testing.T) {
	lc := NewLeastConnections()
	backends := makeBackends([]string{"a", "b"}, []int{1, 1})

	// Simulate a getting loaded, b stays empty.
	backends[0].IncrConns()
	backends[0].IncrConns()

	// Should consistently pick b (0 conns) over a (2 conns).
	for range 5 {
		got := lc.Pick(backends)
		if got.Name != "b" {
			t.Errorf("a=2,b=0: got %q, want b", got.Name)
		}
	}
}
