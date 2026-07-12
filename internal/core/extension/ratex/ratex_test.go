package ratex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRedis starts an in-memory Redis and returns a client pointed at it.
func newRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

// okHandler is the downstream handler; it records whether it was reached.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func TestLimit_BurstDefaultsToRate(t *testing.T) {
	assert.Equal(t, 5, Limit(5, time.Minute, 0).Burst, "zero burst defaults to rate")
	assert.Equal(t, 2, Limit(5, time.Minute, 2).Burst)
}

func TestIPKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	assert.Equal(t, "203.0.113.9", IPKey(r))

	r.RemoteAddr = "203.0.113.9" // no port
	assert.Equal(t, "203.0.113.9", IPKey(r))
}

func TestHandler_IPLimit(t *testing.T) {
	rdb, _ := newRedis(t)
	// rate 2 / minute, burst 2 → third request in the same minute is blocked.
	lim := New(rdb, "rl", Dimension{Name: "ip", Key: IPKey, Limit: Limit(2, time.Minute, 2)})

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:1000"
		return r
	}

	for i := 1; i <= 2; i++ {
		var reached bool
		rr := serve(lim.Handler(okHandler(&reached)), req())
		require.True(t, reached, "request %d should pass", i)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "2", rr.Header().Get("X-RateLimit-Limit"))
	}

	// Third request is limited.
	var reached bool
	rr := serve(lim.Handler(okHandler(&reached)), req())
	assert.False(t, reached, "third request must be blocked")
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("Retry-After"))

	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
	assert.Equal(t, http.StatusTooManyRequests, env.Code)
	assert.NotEmpty(t, env.Message)
	assert.JSONEq(t, "null", string(env.Data))
}

func TestHandler_PerKeyIsolation(t *testing.T) {
	rdb, _ := newRedis(t)
	lim := New(rdb, "rl", Dimension{Name: "ip", Key: IPKey, Limit: Limit(1, time.Minute, 1)})

	mk := func(ip string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = ip + ":1000"
		return r
	}

	// First IP exhausts its bucket.
	var a1 bool
	serve(lim.Handler(okHandler(&a1)), mk("10.0.0.1"))
	var a2 bool
	rrA := serve(lim.Handler(okHandler(&a2)), mk("10.0.0.1"))
	assert.Equal(t, http.StatusTooManyRequests, rrA.Code)

	// A different IP still has its own bucket.
	var b bool
	rrB := serve(lim.Handler(okHandler(&b)), mk("10.0.0.2"))
	assert.True(t, b)
	assert.Equal(t, http.StatusOK, rrB.Code)
}

func TestHandler_AccountDimension(t *testing.T) {
	rdb, _ := newRedis(t)
	lim := New(rdb, "rl", Dimension{Name: "account", Key: HeaderKey("X-Account-Id"), Limit: Limit(1, time.Minute, 1)})

	withAcct := func(id string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if id != "" {
			r.Header.Set("X-Account-Id", id)
		}
		return r
	}

	// Account "acme" allowed once, then blocked.
	var r1 bool
	serve(lim.Handler(okHandler(&r1)), withAcct("acme"))
	var r2 bool
	rr := serve(lim.Handler(okHandler(&r2)), withAcct("acme"))
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	// A different account is independent.
	var r3 bool
	rrOther := serve(lim.Handler(okHandler(&r3)), withAcct("other"))
	assert.True(t, r3)
	assert.Equal(t, http.StatusOK, rrOther.Code)
}

func TestHandler_AnonymousSkipsAccountDimension(t *testing.T) {
	rdb, _ := newRedis(t)
	// Account limit of 1 with no header present: the dimension is skipped, so
	// repeated anonymous requests are never blocked by it.
	lim := New(rdb, "rl", Dimension{Name: "account", Key: HeaderKey("X-Account-Id"), Limit: Limit(1, time.Minute, 1)})

	for i := 0; i < 5; i++ {
		var reached bool
		rr := serve(lim.Handler(okHandler(&reached)), httptest.NewRequest(http.MethodGet, "/", nil))
		require.True(t, reached, "anonymous request %d should pass", i)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestHandler_FailsOpenWhenRedisDown(t *testing.T) {
	rdb, mr := newRedis(t)
	mr.Close() // take Redis down before any request

	lim := New(rdb, "rl", Dimension{Name: "ip", Key: IPKey, Limit: Limit(1, time.Minute, 1)})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1000"
	var reached bool
	rr := serve(lim.Handler(okHandler(&reached)), r)

	assert.True(t, reached, "request must be allowed when redis is unavailable (fail-open)")
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHandler_MultiDimensionBothEnforced(t *testing.T) {
	rdb, _ := newRedis(t)
	lim := New(rdb, "rl",
		Dimension{Name: "ip", Key: IPKey, Limit: Limit(10, time.Minute, 10)},
		Dimension{Name: "account", Key: HeaderKey("X-Account-Id"), Limit: Limit(1, time.Minute, 1)},
	)

	mk := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:1000"
		r.Header.Set("X-Account-Id", "acme")
		return r
	}

	var a bool
	serve(lim.Handler(okHandler(&a)), mk())
	// IP still has budget, but the account dimension is exhausted → blocked.
	var b bool
	rr := serve(lim.Handler(okHandler(&b)), mk())
	assert.False(t, b)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
}
