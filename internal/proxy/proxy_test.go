package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibson27/proxylb/internal/backend"
	"github.com/Wibson27/proxylb/internal/balancer"
)

// mockPool implements the Pool interface for testing.
type mockPool struct {
	backends []*backend.Backend
}

func (m *mockPool) HealthyBackends() []*backend.Backend {
	return m.backends
}

// startTestBackend creates a real HTTP test server that returns
// its name in the response body.
func startTestBackend(t *testing.T, name string) *backend.Backend {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"server":     name,
			"path":       r.URL.Path,
			"request_id": r.Header.Get("X-Request-ID"),
		})
	}))
	t.Cleanup(srv.Close)

	b, err := backend.New(name, srv.URL, 1, srv.URL+"/health")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestProxy_ForwardsToBackend(t *testing.T) {
	b := startTestBackend(t, "app-1")
	pool := &mockPool{backends: []*backend.Backend{b}}
	p := New(pool, balancer.NewRoundRobin())

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)

	if body["server"] != "app-1" {
		t.Errorf("server = %q, want app-1", body["server"])
	}
	if body["path"] != "/hello" {
		t.Errorf("path = %q, want /hello", body["path"])
	}
}

func TestProxy_RoundRobinDistribution(t *testing.T) {
	b1 := startTestBackend(t, "app-1")
	b2 := startTestBackend(t, "app-2")
	pool := &mockPool{backends: []*backend.Backend{b1, b2}}
	p := New(pool, balancer.NewRoundRobin())

	counts := map[string]int{}
	for range 6 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)

		var body map[string]string
		json.NewDecoder(rec.Body).Decode(&body)
		counts[body["server"]]++
	}

	if counts["app-1"] != 3 || counts["app-2"] != 3 {
		t.Errorf("distribution = %v, want app-1:3, app-2:3", counts)
	}
}

func TestProxy_NoHealthyBackends(t *testing.T) {
	pool := &mockPool{backends: nil} // no backends
	p := New(pool, balancer.NewRoundRobin())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestProxy_ConnectionTracking(t *testing.T) {
	b := startTestBackend(t, "app-1")
	pool := &mockPool{backends: []*backend.Backend{b}}
	p := New(pool, balancer.NewRoundRobin())

	// Before request.
	if b.ActiveConns() != 0 {
		t.Fatalf("before: ActiveConns = %d, want 0", b.ActiveConns())
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	// After request — connection should be released.
	if b.ActiveConns() != 0 {
		t.Errorf("after: ActiveConns = %d, want 0 (released)", b.ActiveConns())
	}
}

func TestProxy_BadGateway(t *testing.T) {
	// Create a backend pointing to a non-existent server.
	b, _ := backend.New("dead", "http://127.0.0.1:1", 1, "")
	pool := &mockPool{backends: []*backend.Backend{b}}
	p := New(pool, balancer.NewRoundRobin())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}

	// Connection count should be back to 0 after error.
	if b.ActiveConns() != 0 {
		t.Errorf("ActiveConns = %d, want 0 after error", b.ActiveConns())
	}
}
