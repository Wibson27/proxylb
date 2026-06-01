// Package middleware provides HTTP middleware for the proxy.
//
// Middleware is implemented as functions that wrap an http.Handler
// and return a new http.Handler — the standard Go middleware pattern.
// They compose: RequestID(Logging(proxy)) applies RequestID first,
// then Logging, then the proxy handler.
package middleware

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// RequestID adds an X-Request-ID header to each request if one is
// not already present. This provides a correlation ID for tracing
// a request across the proxy and backend logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-ID") == "" {
			r.Header.Set("X-Request-ID", newRequestID())
		}
		w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
		next.ServeHTTP(w, r)
	})
}

// Logging logs each request with method, path, status, and duration.
// Uses a responseWriter wrapper to capture the status code.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
			"request_id", r.Header.Get("X-Request-ID"),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
// WriteHeader is called at most once per response — we record the
// code and delegate to the underlying writer.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.status = code
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

// Write calls WriteHeader(200) implicitly if not already called,
// matching http.ResponseWriter's contract.
func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	return sw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher if the underlying writer supports it.
// Required for streaming responses (SSE, chunked transfer) to work
// through the proxy.
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// newRequestID generates a short unique request ID.
// 8 random bytes = 16 hex chars. Not a full UUID — shorter is better
// for log readability. 2^64 space is more than enough for request IDs.
func newRequestID() string {
	var b [8]byte
	rand.Read(b[:])
	return fmt.Sprintf("%x", b[:])
}
