// proxylb is a reverse proxy and load balancer.
//
// Architecture:
//   Client → middleware (Request-ID, logging)
//          → proxy handler (pick backend, track connections)
//          → httputil.ReverseProxy → backend
//
// Health checker goroutines run independently, marking backends
// unhealthy after consecutive failures. The proxy's HealthyBackends()
// filter automatically excludes them from selection.
//
// Admin routes under /_lb/ are handled directly.
// All other paths are forwarded to upstream backends.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Wibson27/proxylb/internal/balancer"
	"github.com/Wibson27/proxylb/internal/config"
	"github.com/Wibson27/proxylb/internal/health"
	"github.com/Wibson27/proxylb/internal/middleware"
	"github.com/Wibson27/proxylb/internal/pool"
	"github.com/Wibson27/proxylb/internal/proxy"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	configPath := flag.String("config", "config.yaml", "path to backends config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	slog.Info("starting proxylb",
		"port", cfg.Port,
		"algorithm", cfg.Algorithm,
		"backends", len(cfg.Backends),
	)

	// --- Pool ---

	backendPool, err := pool.New(cfg.Backends)
	if err != nil {
		slog.Error("failed to create backend pool", "error", err)
		os.Exit(1)
	}

	// --- Health checker ---
	//
	// The health checker needs all backends (not just healthy ones) so
	// it can detect recovery. It writes to each backend's atomic fields;
	// the pool reads them on every proxy request.

	healthChecker := health.New(
		backendPool.AllBackends(),
		cfg.HealthCheckInterval,
		cfg.HealthCheckTimeout,
		cfg.UnhealthyThreshold,
	)

	// --- Balancer ---

	var bal balancer.Balancer
	switch cfg.Algorithm {
	case "round-robin":
		bal = balancer.NewRoundRobin()
	case "weighted":
		bal = balancer.NewWeightedRoundRobin()
	case "least-connections":
		bal = balancer.NewLeastConnections()
	default:
		slog.Error("unknown algorithm, use: round-robin, weighted, least-connections",
			"algorithm", cfg.Algorithm)
		os.Exit(1)
	}

	// --- Proxy ---

	proxyHandler := proxy.New(backendPool, bal)

	// --- HTTP server ---
	//
	// Admin routes use the /_lb/ prefix to avoid collisions with
	// proxied paths. Go 1.22+ ServeMux matches most-specific first,
	// so /_lb/backends matches before the catch-all "/".

	mux := http.NewServeMux()
	mux.HandleFunc("GET /_lb/health", handleHealth)
	mux.HandleFunc("GET /_lb/backends", handleBackends(backendPool))
	mux.Handle("/", proxyHandler)

	// Apply middleware: RequestID first, then Logging, then the mux.
	handler := middleware.RequestID(middleware.Logging(mux))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: handler,
	}

	// --- Graceful shutdown ---

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Start health checker goroutines. They use the signal context,
	// so they'll exit when shutdown begins.
	healthChecker.Start(ctx)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("proxy listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			slog.Error("server error", "error", err)
		}
	}

	// Shutdown order:
	// 1. HTTP server (stop accepting, drain in-flight requests)
	// 2. Health checker (stop sending checks to backends)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	healthChecker.Wait()

	slog.Info("proxylb stopped")
}

// handleHealth returns the load balancer's own health status.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// backendInfo is the JSON response for the admin backends API.
type backendInfo struct {
	Name                string `json:"name"`
	URL                 string `json:"url"`
	Weight              int    `json:"weight"`
	Healthy             bool   `json:"healthy"`
	ActiveConns         int64  `json:"active_connections"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
}

// handleBackends returns status of all backends.
func handleBackends(p *pool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backends := p.AllBackends()

		infos := make([]backendInfo, 0, len(backends))
		for _, b := range backends {
			infos = append(infos, backendInfo{
				Name:                b.Name,
				URL:                 b.URL.String(),
				Weight:              b.Weight,
				Healthy:             b.IsHealthy(),
				ActiveConns:         b.ActiveConns(),
				ConsecutiveFailures: b.ConsecutiveFailures(),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(infos)
	}
}
