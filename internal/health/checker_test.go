package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibson27/proxylb/internal/backend"
)

// testBackend creates a backend pointed at a test server.
// The handler can be swapped via the returned pointer.
func testBackend(t *testing.T, name string, handler *atomic.Value) *backend.Backend {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		h := handler.Load().(http.HandlerFunc)
		h(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	b, err := backend.New(name, srv.URL, 1, srv.URL+"/health")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// healthy200 is a handler that always returns 200 OK.
func healthy200() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

// unhealthy500 is a handler that always returns 500.
func unhealthy500() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func TestChecker_HealthyBackendStaysHealthy(t *testing.T) {
	var handler atomic.Value
	handler.Store(healthy200())

	b := testBackend(t, "healthy", &handler)

	checker := New(
		[]*backend.Backend{b},
		50*time.Millisecond,  // fast interval for testing
		1*time.Second,        // generous timeout
		3,                    // threshold
	)

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	// Wait for a few check cycles.
	time.Sleep(200 * time.Millisecond)
	cancel()
	checker.Wait()

	if !b.IsHealthy() {
		t.Error("healthy backend should remain healthy")
	}
	if b.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", b.ConsecutiveFailures())
	}
}

func TestChecker_UnhealthyAfterThreshold(t *testing.T) {
	var handler atomic.Value
	handler.Store(unhealthy500())

	b := testBackend(t, "unhealthy", &handler)

	checker := New(
		[]*backend.Backend{b},
		50*time.Millisecond,
		1*time.Second,
		3, // need 3 consecutive failures
	)

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	// Wait for enough checks to exceed threshold.
	// 50ms interval, threshold 3 → ~150ms minimum + immediate first check.
	time.Sleep(300 * time.Millisecond)
	cancel()
	checker.Wait()

	if b.IsHealthy() {
		t.Error("backend should be unhealthy after threshold failures")
	}
	if b.ConsecutiveFailures() < 3 {
		t.Errorf("ConsecutiveFailures = %d, want >= 3", b.ConsecutiveFailures())
	}
}

func TestChecker_RecoveryAfterFailure(t *testing.T) {
	var handler atomic.Value
	handler.Store(unhealthy500())

	b := testBackend(t, "recoverable", &handler)

	checker := New(
		[]*backend.Backend{b},
		50*time.Millisecond,
		1*time.Second,
		3,
	)

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	// Wait for the backend to become unhealthy.
	// Poll instead of sleep to avoid timing fragility.
	deadline := time.After(2 * time.Second)
	for b.IsHealthy() {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for backend to become unhealthy")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Switch to healthy response.
	handler.Store(healthy200())

	// Wait for recovery.
	deadline = time.After(2 * time.Second)
	for !b.IsHealthy() {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for backend to recover")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	checker.Wait()

	if !b.IsHealthy() {
		t.Error("backend should have recovered")
	}
	if b.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0 after recovery", b.ConsecutiveFailures())
	}
}

func TestChecker_BelowThresholdStaysHealthy(t *testing.T) {
	// A single failure should NOT mark the backend unhealthy
	// when the threshold is 3.
	var handler atomic.Value
	handler.Store(unhealthy500())

	b := testBackend(t, "flaky", &handler)

	checker := New(
		[]*backend.Backend{b},
		50*time.Millisecond,
		1*time.Second,
		3,
	)

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	// Wait for just 1-2 failures (immediate check + maybe one tick).
	time.Sleep(80 * time.Millisecond)

	// Switch back to healthy before threshold is reached.
	handler.Store(healthy200())
	time.Sleep(100 * time.Millisecond)

	cancel()
	checker.Wait()

	if !b.IsHealthy() {
		t.Error("backend should still be healthy — failures reset before threshold")
	}
}

func TestChecker_MultipleBackends(t *testing.T) {
	var handler1, handler2 atomic.Value
	handler1.Store(healthy200())
	handler2.Store(unhealthy500())

	b1 := testBackend(t, "up", &handler1)
	b2 := testBackend(t, "down", &handler2)

	checker := New(
		[]*backend.Backend{b1, b2},
		50*time.Millisecond,
		1*time.Second,
		3,
	)

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	time.Sleep(300 * time.Millisecond)
	cancel()
	checker.Wait()

	if !b1.IsHealthy() {
		t.Error("b1 should be healthy")
	}
	if b2.IsHealthy() {
		t.Error("b2 should be unhealthy")
	}
}

func TestChecker_GracefulShutdown(t *testing.T) {
	var handler atomic.Value
	handler.Store(healthy200())

	b := testBackend(t, "shutdown-test", &handler)

	checker := New(
		[]*backend.Backend{b},
		50*time.Millisecond,
		1*time.Second,
		3,
	)

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	// Cancel immediately — goroutines should exit cleanly.
	cancel()

	// Wait should return promptly (not hang).
	done := make(chan struct{})
	go func() {
		checker.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success — goroutines exited.
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return after context cancellation — goroutine leak")
	}
}
