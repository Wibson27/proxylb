// Package health implements active health checking for upstream backends.
//
// Architecture: one goroutine per backend, each with an independent ticker.
// Each goroutine periodically GETs the backend's HealthURL. The result
// updates the backend's atomic health state via IncrFailures/ResetFailures
// and SetHealthy.
//
// Why one goroutine per backend instead of a single loop?
// - Independent intervals: if a check to a slow backend takes 2.9s
//   (near the 3s timeout), it doesn't delay checks to other backends.
// - Simple shutdown: cancel the context, all goroutines exit.
// - Each goroutine's stack is ~2-8KB (Go initial goroutine stack size),
//   so even 100 backends = under 1MB of stack memory.
//
// Circuit breaker pattern: consecutive failures are tracked per backend.
// After UnhealthyThreshold consecutive failures, the backend is marked
// unhealthy. The pool's HealthyBackends() filter excludes it from
// selection, so the proxy stops sending traffic. A single successful
// check resets the failure counter and marks the backend healthy again —
// this is the recovery path.
//
// No mutex needed: the checker writes to atomic fields (healthy,
// consecutiveFailures) on the backend. The proxy reads them on every
// request via IsHealthy() and ActiveConns(). Atomics are sufficient
// because each field is independent — there's no multi-variable
// invariant to maintain.
package health

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Wibson27/proxylb/internal/backend"
)

// Checker performs periodic health checks on backends.
type Checker struct {
	backends  []*backend.Backend
	interval  time.Duration
	timeout   time.Duration
	threshold int

	wg sync.WaitGroup
}

// New creates a Checker for the given backends.
//
// Parameters:
//   - backends: all backends to check (healthy and unhealthy)
//   - interval: how often to check each backend
//   - timeout: HTTP GET timeout per check
//   - threshold: consecutive failures before marking unhealthy
func New(
	backends []*backend.Backend,
	interval time.Duration,
	timeout time.Duration,
	threshold int,
) *Checker {
	return &Checker{
		backends:  backends,
		interval:  interval,
		timeout:   timeout,
		threshold: threshold,
	}
}

// Start launches a health-check goroutine for each backend.
// All goroutines exit when ctx is cancelled. Call Wait() after
// cancellation to block until all goroutines finish.
func (c *Checker) Start(ctx context.Context) {
	for _, b := range c.backends {
		c.wg.Add(1)
		go c.checkLoop(ctx, b)
	}

	slog.Info("health checker started",
		"backends", len(c.backends),
		"interval", c.interval,
		"timeout", c.timeout,
		"threshold", c.threshold,
	)
}

// Wait blocks until all health-check goroutines have exited.
// Call this after cancelling the context passed to Start.
func (c *Checker) Wait() {
	c.wg.Wait()
}

// checkLoop runs periodic health checks for a single backend.
//
// Flow per tick:
//  1. HTTP GET to backend's HealthURL with timeout
//  2. If 2xx: ResetFailures, mark healthy (log recovery if was unhealthy)
//  3. If error or non-2xx: IncrFailures, if >= threshold mark unhealthy
//
// The first check runs immediately (before the first tick) so that an
// unhealthy backend discovered at startup doesn't get traffic for a
// full interval before being detected.
func (c *Checker) checkLoop(ctx context.Context, b *backend.Backend) {
	defer c.wg.Done()

	// Dedicated HTTP client per backend. No connection pooling across
	// backends — each client maintains its own idle connections to its
	// single backend. The timeout covers the entire request lifecycle:
	// DNS lookup + TCP connect + TLS handshake + request + response.
	client := &http.Client{
		Timeout: c.timeout,
	}

	// Immediate first check — don't wait a full interval.
	c.check(ctx, client, b)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.check(ctx, client, b)
		}
	}
}

// check performs a single health check against one backend.
//
// The request uses the passed context as the parent so that
// in-flight checks are cancelled on shutdown. We create a child
// context with the timeout to bound the request duration.
func (c *Checker) check(ctx context.Context, client *http.Client, b *backend.Backend) {
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, b.HealthURL, nil)
	if err != nil {
		// This only fails if the URL is fundamentally broken (which
		// we validated at startup), or if the context is already
		// cancelled. Either way, treat as a failure.
		c.recordFailure(b, err.Error())
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		// Network error, timeout, DNS failure, connection refused, etc.
		// On context cancellation (shutdown), this also fires — but
		// we're exiting the loop anyway, so the state change is harmless.
		c.recordFailure(b, err.Error())
		return
	}
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.recordFailure(b, "unexpected status: "+resp.Status)
		return
	}

	// Success — the backend responded with 2xx.
	c.recordSuccess(b)
}

// recordFailure increments the backend's failure counter and marks
// it unhealthy if the threshold is reached.
func (c *Checker) recordFailure(b *backend.Backend, reason string) {
	failures := b.IncrFailures()
	wasHealthy := b.IsHealthy()

	if failures >= int64(c.threshold) {
		b.SetHealthy(false)

		// Only log the transition, not every single failure after.
		// This prevents log spam when a backend is down for a long time.
		if wasHealthy {
			slog.Warn("backend marked unhealthy",
				"backend", b.Name,
				"consecutive_failures", failures,
				"threshold", c.threshold,
				"reason", reason,
			)
		}
	} else {
		slog.Debug("health check failed",
			"backend", b.Name,
			"consecutive_failures", failures,
			"threshold", c.threshold,
			"reason", reason,
		)
	}
}

// recordSuccess resets the failure counter and marks the backend healthy.
// Logs recovery when a previously unhealthy backend comes back.
func (c *Checker) recordSuccess(b *backend.Backend) {
	wasHealthy := b.IsHealthy()
	b.ResetFailures()
	b.SetHealthy(true)

	if !wasHealthy {
		slog.Info("backend recovered",
			"backend", b.Name,
		)
	}
}
