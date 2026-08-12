package app

import (
	"bytes"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Client-side request idempotency (PRD F.1 C-10). A client may attach
// `client-request-id` to a state-changing request; the server deduplicates by
// (method, path, client-request-id) so a retried request returns the first
// response instead of executing twice. Keys expire after a short TTL and the
// store is capped so memory stays bounded.
const (
	idempotencyTTL        = 5 * time.Minute
	idempotencyMaxEntries = 4096
)

type idempotencyEntry struct {
	status  int
	header  http.Header
	body    []byte
	expires time.Time
}

type idempotencyStore struct {
	mu    sync.Mutex
	items map[string]idempotencyEntry
}

func newIdempotencyStore() *idempotencyStore {
	return &idempotencyStore{items: make(map[string]idempotencyEntry)}
}

func (s *idempotencyStore) lookup(key string, now time.Time) (idempotencyEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[key]
	if !ok {
		return idempotencyEntry{}, false
	}
	if now.After(e.expires) {
		delete(s.items, key)
		return idempotencyEntry{}, false
	}
	return e, true
}

func (s *idempotencyStore) store(key string, e idempotencyEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) >= idempotencyMaxEntries {
		// Evict expired entries before dropping new ones under pressure.
		now := time.Now()
		for k, v := range s.items {
			if now.After(v.expires) {
				delete(s.items, k)
			}
		}
	}
	s.items[key] = e
}

// recordingResponse captures the status, headers, and body so a duplicate
// request can be replayed verbatim.
type recordingResponse struct {
	http.ResponseWriter
	status int
	header http.Header
	body   bytes.Buffer
}

func (r *recordingResponse) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recordingResponse) Write(b []byte) (int, error) {
	_, _ = r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// useIdempotency mounts the client_request_id dedup middleware for
// state-changing methods.
func (app *App) useIdempotency(router chi.Router) {
	store := newIdempotencyStore()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				next.ServeHTTP(w, r)
				return
			}
			id := r.Header.Get("client-request-id")
			if id == "" || len(id) > 128 {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Method + " " + r.URL.Path + " " + id
			if e, ok := store.lookup(key, time.Now()); ok {
				for k, vs := range e.header {
					for _, v := range vs {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(e.status)
				_, _ = w.Write(e.body)
				return
			}
			rec := &recordingResponse{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if rec.status >= 200 && rec.status < 300 {
				store.store(key, idempotencyEntry{
					status:  rec.status,
					header:  rec.header,
					body:    append([]byte(nil), rec.body.Bytes()...),
					expires: time.Now().Add(idempotencyTTL),
				})
			}
		})
	})
}
