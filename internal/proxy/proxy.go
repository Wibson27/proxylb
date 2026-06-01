// Package proxy provides the reverse proxy handler that forwards
// requests to upstream backends selected by a load balancer.
//
// Uses net/http/httputil.ReverseProxy with the Rewrite API (Go 1.20+).
// The Rewrite function receives a ProxyRequest with separate inbound
// and outbound requests — cleaner than the old Director API because
// modifications to the outbound request can't accidentally leak back.
//
// Connection tracking: the proxy increments the backend's active
// connection count before forwarding and decrements in a defer after
// ReverseProxy.ServeHTTP returns. ServeHTTP blocks until the full
// response body has been sent to the client, so the counter accurately
// reflects in-flight requests.
//
// Backend selection uses request context: Pick happens in ServeHTTP,
// the chosen backend is stored in the context, and the Rewrite
// function reads it from the outbound request's context.
package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"

	"github.com/Wibson27/proxylb/internal/backend"
	"github.com/Wibson27/proxylb/internal/balancer"
)

type contextKey string

const backendKey contextKey = "proxy-backend"

// Pool provides the list of healthy backends for selection.
type Pool interface {
	HealthyBackends() []*backend.Backend
}

// Proxy is an HTTP handler that forwards requests to backends.
type Proxy struct {
	pool     Pool
	balancer balancer.Balancer
	rp       *httputil.ReverseProxy
}

// New creates a Proxy with the given pool and balancing algorithm.
func New(pool Pool, bal balancer.Balancer) *Proxy {
	p := &Proxy{
		pool:     pool,
		balancer: bal,
	}

	p.rp = &httputil.ReverseProxy{
		Rewrite: p.rewrite,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("proxy error",
				"backend", backendFromCtx(r.Context()),
				"error", err,
			)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}

	return p
}

// ServeHTTP selects a backend, tracks the connection, and forwards.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	backends := p.pool.HealthyBackends()
	if len(backends) == 0 {
		http.Error(w, "no healthy backends available", http.StatusServiceUnavailable)
		return
	}

	b := p.balancer.Pick(backends)
	b.IncrConns()
	defer b.DecrConns()

	// Store the backend in the context for the Rewrite function.
	ctx := context.WithValue(r.Context(), backendKey, b)
	p.rp.ServeHTTP(w, r.WithContext(ctx))
}

// rewrite configures the outbound request to target the selected backend.
//
// SetURL copies the backend's scheme and host to the outbound request
// and prepends the backend's path to the request path.
// SetXForwarded adds X-Forwarded-For, X-Forwarded-Host, and
// X-Forwarded-Proto headers — standard reverse proxy headers that
// tell the backend the original client's address and protocol.
func (p *Proxy) rewrite(pr *httputil.ProxyRequest) {
	b := pr.In.Context().Value(backendKey).(*backend.Backend)
	pr.SetURL(b.URL)
	pr.SetXForwarded()
	pr.Out.Host = pr.In.Host
}

// backendFromCtx extracts the backend name from the context for logging.
func backendFromCtx(ctx context.Context) string {
	if b, ok := ctx.Value(backendKey).(*backend.Backend); ok {
		return b.Name
	}
	return "unknown"
}
