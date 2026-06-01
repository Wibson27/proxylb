// Package config loads proxy settings from a YAML config file
// and environment variables.
//
// Backend definitions come from YAML — they're structured and
// variable-length. Server settings use PROXYLB_ env vars.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all application settings.
type Config struct {
	// Port is the proxy listener port (receives client traffic).
	Port int

	// Algorithm selects the load balancing strategy.
	// Valid values: "round-robin", "weighted", "least-connections".
	Algorithm string

	// HealthCheckInterval is how often to check backend health.
	HealthCheckInterval time.Duration

	// HealthCheckTimeout is the per-check HTTP timeout.
	HealthCheckTimeout time.Duration

	// UnhealthyThreshold is consecutive failures before marking down.
	UnhealthyThreshold int

	// ShutdownTimeout is the grace period for draining connections.
	ShutdownTimeout time.Duration

	// Backends is the list of upstream servers.
	Backends []BackendConfig
}

// BackendConfig defines a single upstream server.
type BackendConfig struct {
	// Name is a human-readable identifier (e.g. "app-1").
	Name string `yaml:"name"`

	// URL is the backend's base URL (e.g. "http://localhost:3001").
	URL string `yaml:"url"`

	// Weight controls traffic distribution in weighted round-robin.
	// Higher weight = more traffic. Default: 1.
	Weight int `yaml:"weight"`

	// HealthURL is the endpoint to GET for health checks.
	// If empty, defaults to URL + "/health".
	HealthURL string `yaml:"health_url"`
}

// configFile is the intermediate YAML structure.
type configFile struct {
	Backends []BackendConfig `yaml:"backends"`
}

// Load reads server settings from env vars and backend definitions
// from the given YAML file.
func Load(configPath string) (Config, error) {
	cfg := Config{
		Port:                envInt("PROXYLB_PORT", 8080),
		Algorithm:           envString("PROXYLB_ALGORITHM", "round-robin"),
		HealthCheckInterval: envDuration("PROXYLB_HEALTH_INTERVAL", 10*time.Second),
		HealthCheckTimeout:  envDuration("PROXYLB_HEALTH_TIMEOUT", 3*time.Second),
		UnhealthyThreshold:  envInt("PROXYLB_UNHEALTHY_THRESHOLD", 3),
		ShutdownTimeout:     envDuration("PROXYLB_SHUTDOWN_TIMEOUT", 15*time.Second),
	}

	if configPath == "" {
		return cfg, nil
	}

	backends, err := loadBackends(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("load backends config: %w", err)
	}
	cfg.Backends = backends

	return cfg, nil
}

// loadBackends reads and parses the YAML config file.
func loadBackends(path string) ([]BackendConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	for i := range file.Backends {
		b := &file.Backends[i]
		if b.Name == "" {
			return nil, fmt.Errorf("backend %d: name is required", i)
		}
		if b.URL == "" {
			return nil, fmt.Errorf("backend %q: url is required", b.Name)
		}
		if b.Weight <= 0 {
			b.Weight = 1
		}
		if b.HealthURL == "" {
			b.HealthURL = b.URL + "/health"
		}
	}

	return file.Backends, nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
