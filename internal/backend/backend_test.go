package backend

import (
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	b, err := New("app-1", "http://localhost:3001", 3, "http://localhost:3001/health")
	if err != nil {
		t.Fatal(err)
	}

	if b.Name != "app-1" {
		t.Errorf("Name = %q, want app-1", b.Name)
	}
	if b.URL.String() != "http://localhost:3001" {
		t.Errorf("URL = %q, want http://localhost:3001", b.URL.String())
	}
	if b.Weight != 3 {
		t.Errorf("Weight = %d, want 3", b.Weight)
	}
	if b.HealthURL != "http://localhost:3001/health" {
		t.Errorf("HealthURL = %q", b.HealthURL)
	}
	if !b.IsHealthy() {
		t.Error("new backend should be healthy")
	}
	if b.ActiveConns() != 0 {
		t.Errorf("ActiveConns = %d, want 0", b.ActiveConns())
	}
	if b.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", b.ConsecutiveFailures())
	}
}

func TestNew_InvalidURL(t *testing.T) {
	_, err := New("bad", "://invalid", 1, "")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestActiveConns(t *testing.T) {
	b, _ := New("test", "http://localhost:3000", 1, "")

	b.IncrConns()
	b.IncrConns()
	b.IncrConns()

	if got := b.ActiveConns(); got != 3 {
		t.Errorf("after 3 incr: ActiveConns = %d, want 3", got)
	}

	b.DecrConns()

	if got := b.ActiveConns(); got != 2 {
		t.Errorf("after 1 decr: ActiveConns = %d, want 2", got)
	}
}

func TestHealthState(t *testing.T) {
	b, _ := New("test", "http://localhost:3000", 1, "")

	if !b.IsHealthy() {
		t.Error("initial state should be healthy")
	}

	b.SetHealthy(false)
	if b.IsHealthy() {
		t.Error("should be unhealthy after SetHealthy(false)")
	}

	b.SetHealthy(true)
	if !b.IsHealthy() {
		t.Error("should be healthy after SetHealthy(true)")
	}
}

func TestConsecutiveFailures(t *testing.T) {
	b, _ := New("test", "http://localhost:3000", 1, "")

	f1 := b.IncrFailures()
	if f1 != 1 {
		t.Errorf("first IncrFailures = %d, want 1", f1)
	}

	f2 := b.IncrFailures()
	if f2 != 2 {
		t.Errorf("second IncrFailures = %d, want 2", f2)
	}

	if got := b.ConsecutiveFailures(); got != 2 {
		t.Errorf("ConsecutiveFailures = %d, want 2", got)
	}

	b.ResetFailures()
	if got := b.ConsecutiveFailures(); got != 0 {
		t.Errorf("after reset: ConsecutiveFailures = %d, want 0", got)
	}
}

func TestConcurrentConns(t *testing.T) {
	b, _ := New("test", "http://localhost:3000", 1, "")

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// 100 goroutines increment, 100 decrement — net effect should be 0.
	for range goroutines {
		go func() {
			defer wg.Done()
			b.IncrConns()
		}()
		go func() {
			defer wg.Done()
			b.IncrConns()
			b.DecrConns()
		}()
	}

	wg.Wait()

	if got := b.ActiveConns(); got != goroutines {
		t.Errorf("ActiveConns = %d, want %d", got, goroutines)
	}
}
