package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTemp creates a temporary YAML file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Defaults(t *testing.T) {
	yaml := `
backends:
  - name: svc
    url: http://localhost:3000
`
	cfg, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Algorithm != "round-robin" {
		t.Errorf("Algorithm = %q, want round-robin", cfg.Algorithm)
	}
	if cfg.HealthCheckInterval != 10*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 10s", cfg.HealthCheckInterval)
	}
	if cfg.HealthCheckTimeout != 3*time.Second {
		t.Errorf("HealthCheckTimeout = %v, want 3s", cfg.HealthCheckTimeout)
	}
	if cfg.UnhealthyThreshold != 3 {
		t.Errorf("UnhealthyThreshold = %d, want 3", cfg.UnhealthyThreshold)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("PROXYLB_PORT", "9090")
	t.Setenv("PROXYLB_ALGORITHM", "weighted")
	t.Setenv("PROXYLB_HEALTH_INTERVAL", "5s")
	t.Setenv("PROXYLB_HEALTH_TIMEOUT", "2s")
	t.Setenv("PROXYLB_UNHEALTHY_THRESHOLD", "5")
	t.Setenv("PROXYLB_SHUTDOWN_TIMEOUT", "30s")

	yaml := `
backends:
  - name: svc
    url: http://localhost:3000
`
	cfg, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.Algorithm != "weighted" {
		t.Errorf("Algorithm = %q, want weighted", cfg.Algorithm)
	}
	if cfg.HealthCheckInterval != 5*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 5s", cfg.HealthCheckInterval)
	}
	if cfg.HealthCheckTimeout != 2*time.Second {
		t.Errorf("HealthCheckTimeout = %v, want 2s", cfg.HealthCheckTimeout)
	}
	if cfg.UnhealthyThreshold != 5 {
		t.Errorf("UnhealthyThreshold = %d, want 5", cfg.UnhealthyThreshold)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 30s", cfg.ShutdownTimeout)
	}
}

func TestLoad_InvalidEnvFallback(t *testing.T) {
	t.Setenv("PROXYLB_PORT", "not-a-number")
	t.Setenv("PROXYLB_HEALTH_INTERVAL", "bad-duration")

	yaml := `
backends:
  - name: svc
    url: http://localhost:3000
`
	cfg, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080 (fallback)", cfg.Port)
	}
	if cfg.HealthCheckInterval != 10*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 10s (fallback)", cfg.HealthCheckInterval)
	}
}

func TestLoad_YAMLFullConfig(t *testing.T) {
	yaml := `
backends:
  - name: app-1
    url: http://localhost:3001
    weight: 5
    health_url: http://localhost:3001/ping
  - name: app-2
    url: http://localhost:3002
    weight: 2
`
	cfg, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Backends) != 2 {
		t.Fatalf("len(Backends) = %d, want 2", len(cfg.Backends))
	}

	b1 := cfg.Backends[0]
	if b1.Name != "app-1" {
		t.Errorf("Backends[0].Name = %q, want app-1", b1.Name)
	}
	if b1.Weight != 5 {
		t.Errorf("Backends[0].Weight = %d, want 5", b1.Weight)
	}
	if b1.HealthURL != "http://localhost:3001/ping" {
		t.Errorf("Backends[0].HealthURL = %q, want custom", b1.HealthURL)
	}

	b2 := cfg.Backends[1]
	if b2.Weight != 2 {
		t.Errorf("Backends[1].Weight = %d, want 2", b2.Weight)
	}
	if b2.HealthURL != "http://localhost:3002/health" {
		t.Errorf("Backends[1].HealthURL = %q, want default /health", b2.HealthURL)
	}
}

func TestLoad_DefaultWeight(t *testing.T) {
	yaml := `
backends:
  - name: svc
    url: http://localhost:3000
`
	cfg, err := Load(writeTemp(t, yaml))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Backends[0].Weight != 1 {
		t.Errorf("Weight = %d, want 1 (default)", cfg.Backends[0].Weight)
	}
}

func TestLoad_MissingName(t *testing.T) {
	yaml := `
backends:
  - url: http://localhost:3000
`
	_, err := Load(writeTemp(t, yaml))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoad_MissingURL(t *testing.T) {
	yaml := `
backends:
  - name: svc
`
	_, err := Load(writeTemp(t, yaml))
	if err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_EmptyConfigPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Backends) != 0 {
		t.Errorf("len(Backends) = %d, want 0 for empty config path", len(cfg.Backends))
	}
}

func TestEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		fallback int
		want     int
	}{
		{"set", "42", 0, 42},
		{"empty", "", 99, 99},
		{"invalid", "abc", 99, 99},
		{"negative", "-1", 0, -1},
		{"zero", "0", 99, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_ENV_INT_" + tt.name
			if tt.envVal != "" {
				t.Setenv(key, tt.envVal)
			}
			got := envInt(key, tt.fallback)
			if got != tt.want {
				t.Errorf("envInt(%q) = %d, want %d", tt.envVal, got, tt.want)
			}
		})
	}
}

func TestEnvDuration(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		fallback time.Duration
		want     time.Duration
	}{
		{"set", "5s", 0, 5 * time.Second},
		{"empty", "", 10 * time.Second, 10 * time.Second},
		{"invalid", "bad", 10 * time.Second, 10 * time.Second},
		{"milliseconds", "500ms", 0, 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_ENV_DUR_" + tt.name
			if tt.envVal != "" {
				t.Setenv(key, tt.envVal)
			}
			got := envDuration(key, tt.fallback)
			if got != tt.want {
				t.Errorf("envDuration(%q) = %v, want %v", tt.envVal, got, tt.want)
			}
		})
	}
}

func TestEnvString(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		t.Setenv("TEST_STR", "hello")
		if got := envString("TEST_STR", "default"); got != "hello" {
			t.Errorf("got %q, want hello", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := envString("TEST_STR_MISSING", "default"); got != "default" {
			t.Errorf("got %q, want default", got)
		}
	})
}
