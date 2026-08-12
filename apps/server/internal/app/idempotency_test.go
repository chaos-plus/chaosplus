package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestIdempotencyReplaysFirstResponse verifies that a retried state-changing
// request with the same client-request-id returns the first response without
// re-executing the handler (PRD F.1 C-10).
func TestIdempotencyReplaysFirstResponse(t *testing.T) {
	router := chi.NewMux()
	app := &App{}
	app.useIdempotency(router)
	router.Post("/api/things", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	})

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/things", nil)
		r.Header.Set("client-request-id", "req-abc")
		return r
	}

	first := httptest.NewRecorder()
	router.ServeHTTP(first, req())
	if first.Code != http.StatusCreated || first.Body.String() != `{"id":1}` {
		t.Fatalf("first: got %d %q", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, req())
	if second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("replay diverged: got %d %q, want %d %q", second.Code, second.Body.String(), first.Code, first.Body.String())
	}
}

// TestIdempotencySkipsReads verifies idempotency does not cache reads.
func TestIdempotencySkipsReads(t *testing.T) {
	router := chi.NewMux()
	app := &App{}
	app.useIdempotency(router)
	calls := 0
	router.Get("/api/things", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`[]`))
	})
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/things", nil)
		r.Header.Set("client-request-id", "req-abc")
		router.ServeHTTP(httptest.NewRecorder(), r)
	}
	if calls != 2 {
		t.Fatalf("GET was cached: handler ran %d times", calls)
	}
}
