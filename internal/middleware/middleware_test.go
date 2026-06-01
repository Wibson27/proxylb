package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request has a generated ID.
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			t.Error("X-Request-ID not set on request")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify response also carries the ID.
	respID := rec.Header().Get("X-Request-ID")
	if respID == "" {
		t.Error("X-Request-ID not set in response")
	}
	if len(respID) != 16 {
		t.Errorf("X-Request-ID length = %d, want 16 (8 bytes hex)", len(respID))
	}
}

func TestRequestID_PreservesExisting(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Request-ID"); got != "custom-id-123" {
			t.Errorf("X-Request-ID = %q, want custom-id-123", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "custom-id-123" {
		t.Errorf("response X-Request-ID = %q, want custom-id-123", got)
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	var ids []string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.Header.Get("X-Request-ID"))
		w.WriteHeader(http.StatusOK)
	}))

	for range 100 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate request ID: %s", id)
		}
		seen[id] = true
	}
}

func TestLogging_CapturesStatus(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := Logging(inner)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLogging_DefaultStatus(t *testing.T) {
	// Handler writes body without calling WriteHeader — should default to 200.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	handler := Logging(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestStatusWriter_FlushSupport(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	// Flush should not panic even though rec implements http.Flusher.
	sw.Flush()

	if !rec.Flushed {
		t.Error("expected underlying writer to be flushed")
	}
}

func TestStatusWriter_WriteHeaderOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	sw.WriteHeader(http.StatusCreated)
	sw.WriteHeader(http.StatusNotFound) // second call — should be ignored

	if sw.status != http.StatusCreated {
		t.Errorf("status = %d, want 201 (first call wins)", sw.status)
	}
}
