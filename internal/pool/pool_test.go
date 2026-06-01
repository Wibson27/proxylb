package pool

import (
	"testing"

	"github.com/Wibson27/proxylb/internal/config"
)

func TestNew(t *testing.T) {
	cfgs := []config.BackendConfig{
		{Name: "a", URL: "http://localhost:3001", Weight: 1, HealthURL: "http://localhost:3001/health"},
		{Name: "b", URL: "http://localhost:3002", Weight: 2, HealthURL: "http://localhost:3002/health"},
	}

	p, err := New(cfgs)
	if err != nil {
		t.Fatal(err)
	}

	all := p.AllBackends()
	if len(all) != 2 {
		t.Fatalf("AllBackends len = %d, want 2", len(all))
	}
	if all[0].Name != "a" || all[1].Name != "b" {
		t.Errorf("backends = [%s, %s], want [a, b]", all[0].Name, all[1].Name)
	}
}

func TestNew_InvalidURL(t *testing.T) {
	cfgs := []config.BackendConfig{
		{Name: "bad", URL: "://invalid", Weight: 1, HealthURL: ""},
	}

	_, err := New(cfgs)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestHealthyBackends_AllHealthy(t *testing.T) {
	cfgs := []config.BackendConfig{
		{Name: "a", URL: "http://localhost:3001", Weight: 1, HealthURL: ""},
		{Name: "b", URL: "http://localhost:3002", Weight: 1, HealthURL: ""},
	}

	p, _ := New(cfgs)
	healthy := p.HealthyBackends()

	if len(healthy) != 2 {
		t.Errorf("HealthyBackends len = %d, want 2", len(healthy))
	}
}

func TestHealthyBackends_OneUnhealthy(t *testing.T) {
	cfgs := []config.BackendConfig{
		{Name: "a", URL: "http://localhost:3001", Weight: 1, HealthURL: ""},
		{Name: "b", URL: "http://localhost:3002", Weight: 1, HealthURL: ""},
		{Name: "c", URL: "http://localhost:3003", Weight: 1, HealthURL: ""},
	}

	p, _ := New(cfgs)

	// Mark b as unhealthy.
	all := p.AllBackends()
	all[1].SetHealthy(false)

	healthy := p.HealthyBackends()
	if len(healthy) != 2 {
		t.Fatalf("HealthyBackends len = %d, want 2", len(healthy))
	}
	for _, b := range healthy {
		if b.Name == "b" {
			t.Error("unhealthy backend b should not be in HealthyBackends")
		}
	}
}

func TestHealthyBackends_AllUnhealthy(t *testing.T) {
	cfgs := []config.BackendConfig{
		{Name: "a", URL: "http://localhost:3001", Weight: 1, HealthURL: ""},
		{Name: "b", URL: "http://localhost:3002", Weight: 1, HealthURL: ""},
	}

	p, _ := New(cfgs)
	all := p.AllBackends()
	all[0].SetHealthy(false)
	all[1].SetHealthy(false)

	healthy := p.HealthyBackends()
	if len(healthy) != 0 {
		t.Errorf("HealthyBackends len = %d, want 0 (all unhealthy)", len(healthy))
	}
}

func TestAllBackends_ReturnsCopy(t *testing.T) {
	cfgs := []config.BackendConfig{
		{Name: "a", URL: "http://localhost:3001", Weight: 1, HealthURL: ""},
	}

	p, _ := New(cfgs)

	copy1 := p.AllBackends()
	copy2 := p.AllBackends()

	// Modifying one copy should not affect the other.
	copy1[0] = nil

	if copy2[0] == nil {
		t.Error("AllBackends returned the same slice, not a copy")
	}
}
