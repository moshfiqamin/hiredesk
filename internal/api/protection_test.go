package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// limitedEngine is an engine whose one route, POST /login, is guarded by
// limiter, behind the production middleware chain.
func limitedEngine(limiter *RateLimiter) *gin.Engine {
	return middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.POST("/login", limiter.Middleware(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	})
}

func postFrom(engine *gin.Engine, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = ip + ":40000"
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitAllowsTenThenRefuses(t *testing.T) {
	clock := time.Date(2026, time.November, 12, 9, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(10, time.Minute, 100)
	limiter.now = func() time.Time { return clock }
	engine := limitedEngine(limiter)

	for i := 1; i <= 10; i++ {
		if rec := postFrom(engine, "203.0.113.7"); rec.Code != http.StatusNoContent {
			t.Fatalf("attempt %d: status = %d, want 204", i, rec.Code)
		}
	}

	refused := postFrom(engine, "203.0.113.7")
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 11: status = %d, want 429", refused.Code)
	}
	// One token every 6 seconds, and none has refilled on a frozen clock.
	if got := refused.Header().Get("Retry-After"); got != "6" {
		t.Errorf("Retry-After = %q, want 6", got)
	}
	body := decode(t, refused.Body.Bytes())
	if body.Code != codeRateLimited || body.RequestID == "" {
		t.Errorf("body = %+v, want code rate_limited and a request_id", body)
	}

	// Another address has its own bucket.
	if rec := postFrom(engine, "198.51.100.1"); rec.Code != http.StatusNoContent {
		t.Errorf("another IP: status = %d, want 204", rec.Code)
	}

	// Refused attempts do not dig the hole deeper: six seconds later exactly
	// one more is allowed.
	clock = clock.Add(6 * time.Second)
	if rec := postFrom(engine, "203.0.113.7"); rec.Code != http.StatusNoContent {
		t.Errorf("after 6s: status = %d, want 204", rec.Code)
	}
	if rec := postFrom(engine, "203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("immediately after: status = %d, want 429", rec.Code)
	}
}

func TestRateLimitSweepsIdleBuckets(t *testing.T) {
	clock := time.Date(2026, time.November, 12, 9, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(10, time.Minute, 100_000)
	limiter.now = func() time.Time { return clock }

	// 5,000 addresses, each seen once.
	for i := range 5000 {
		limiter.allow("10.0." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256))
	}
	if got := limiter.size(); got != 5000 {
		t.Fatalf("size = %d, want 5000", got)
	}

	// A window later, every one of those buckets is full again, so they are
	// indistinguishable from new ones and the next request sweeps them out.
	clock = clock.Add(time.Minute)
	limiter.allow("192.0.2.200")
	if got := limiter.size(); got != 1 {
		t.Errorf("size after a window = %d, want 1", got)
	}
}

func TestRateLimitNeverExceedsItsCap(t *testing.T) {
	limiter := NewRateLimiter(10, time.Minute, 50)

	// 1,000 distinct addresses inside one window: more than the cap, none
	// idle long enough to sweep.
	for i := range 1000 {
		limiter.allow("10.1." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256))
		if got := limiter.size(); got > 50 {
			t.Fatalf("size = %d after %d addresses, want at most 50", got, i+1)
		}
	}
}

func TestRateLimitGroupsIPv6ByNetwork(t *testing.T) {
	limiter := NewRateLimiter(10, time.Minute, 100)
	engine := limitedEngine(limiter)

	// Ten different addresses inside one /64 share the one allowance.
	for i := range 10 {
		postFrom(engine, "[2001:db8:1:2::"+strconv.Itoa(i+1)+"]")
	}
	if rec := postFrom(engine, "[2001:db8:1:2:ffff::1]"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("11th request from the same /64: status = %d, want 429", rec.Code)
	}
	if rec := postFrom(engine, "[2001:db8:1:3::1]"); rec.Code != http.StatusNoContent {
		t.Errorf("a different /64: status = %d, want 204", rec.Code)
	}
}

func TestRateLimitIsSafeConcurrently(t *testing.T) {
	limiter := NewRateLimiter(10, time.Minute, 100)
	engine := limitedEngine(limiter)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if postFrom(engine, "203.0.113.9").Code == http.StatusNoContent {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Exactly the burst, however the 50 interleave. Run under -race, this
	// also proves the map is never touched without the lock.
	if allowed != 10 {
		t.Errorf("%d of 50 concurrent requests allowed, want exactly 10", allowed)
	}
}

func TestAuthRoutesAreRateLimited(t *testing.T) {
	s := newTestServer(t)
	// Replace the generous test limiter with the production allowance.
	deps := s.deps
	deps.AuthLimiter = NewRateLimiter(10, time.Minute, 100)
	engine, err := NewRouter(deps)
	if err != nil {
		t.Fatalf("NewRouter() unexpected error: %v", err)
	}
	s.engine = engine

	for i := 1; i <= 10; i++ {
		s.post("/v1/auth/login", `{"email":"nobody@example.com","password":"x"}`)
	}
	rec := s.post("/v1/auth/login", `{"email":"nobody@example.com","password":"x"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("11th login: status = %d, Retry-After = %q; want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}

	// The allowance is shared by every /v1/auth route...
	if rec := s.post("/v1/auth/register", `{}`); rec.Code != http.StatusTooManyRequests {
		t.Errorf("register after the limit: status = %d, want 429", rec.Code)
	}
	// ...and by nothing else.
	if rec := s.get("/v1/job-posts", ""); rec.Code != http.StatusOK {
		t.Errorf("public listing after the auth limit: status = %d, want 200", rec.Code)
	}
}

func TestCORS(t *testing.T) {
	engine := gin.New()
	engine.Use(RequestID(), CORS([]string{"https://app.example.com"}))
	engine.GET("/v1/me", func(c *gin.Context) { Respond(c, domain.ErrUnauthenticated) })
	engine.HandleMethodNotAllowed = true
	engine.NoMethod(NoMethod)

	t.Run("an allowed origin is named, with credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		h := rec.Header()
		if h.Get("Access-Control-Allow-Origin") != "https://app.example.com" || h.Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("headers = %v, want the origin echoed with credentials", h)
		}
		// Even on an error: a browser hides a 401 from the script without it.
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want the handler's 401", rec.Code)
		}
		if !strings.Contains(h.Get("Access-Control-Expose-Headers"), RequestIDHeader) {
			t.Errorf("Expose-Headers = %q, want X-Request-ID readable by scripts", h.Get("Access-Control-Expose-Headers"))
		}
		if h.Get("Vary") != "Origin" {
			t.Errorf("Vary = %q, want Origin", h.Get("Vary"))
		}
	})

	t.Run("an unlisted origin gets no CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials"} {
			if got := rec.Header().Get(header); got != "" {
				t.Errorf("%s = %q, want absent", header, got)
			}
		}
	})

	t.Run("a preflight is answered in full", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/v1/me", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPatch)
		req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		h := rec.Header()
		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", rec.Code)
		}
		if !strings.Contains(h.Get("Access-Control-Allow-Methods"), "PATCH") ||
			!strings.Contains(h.Get("Access-Control-Allow-Headers"), "Authorization") ||
			h.Get("Access-Control-Max-Age") != "600" {
			t.Errorf("preflight headers = %v", h)
		}
	})

	t.Run("a preflight from an unlisted origin is not approved", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/v1/me", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", http.MethodDelete)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Methods") != "" || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("headers = %v, want no approval", rec.Header())
		}
	})

	t.Run("no origins configured means no cross-origin access", func(t *testing.T) {
		closed := gin.New()
		closed.Use(CORS(nil))
		closed.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec := httptest.NewRecorder()
		closed.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, want absent", got)
		}
	})
}

func TestBodyLimit(t *testing.T) {
	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.POST("/echo", func(c *gin.Context) {
			var body map[string]any
			if !decodeJSON(c, &body) {
				return
			}
			c.Status(http.StatusNoContent)
		})
	})

	// A JSON object of exactly n bytes: {"a":"xxx…"}.
	object := func(n int) string { return `{"a":"` + strings.Repeat("x", n-8) + `"}` }

	t.Run("at the limit", func(t *testing.T) {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(object(maxBodyBytes))))
		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", rec.Code)
		}
	})

	t.Run("declared too large", func(t *testing.T) {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(object(maxBodyBytes+1))))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", rec.Code)
		}
		if body := decode(t, rec.Body.Bytes()); body.Code != codePayloadTooLarge {
			t.Errorf("code = %q, want payload_too_large", body.Code)
		}
	})

	t.Run("undeclared and too large", func(t *testing.T) {
		// No Content-Length, as with a chunked upload: only the read that
		// crosses the limit can notice.
		req := httptest.NewRequest(http.MethodPost, "/echo", io.NopCloser(strings.NewReader(object(maxBodyBytes+1))))
		req.ContentLength = -1
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", rec.Code)
		}
	})
}

func TestRecoveryAnswersWithTheEnvelopeAndLogsTheStack(t *testing.T) {
	logged := captureSlog(t)

	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.GET("/panic", func(*gin.Context) { panic("something impossible happened") })
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set(RequestIDHeader, "panic-1")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decode(t, rec.Body.Bytes())
	if body.Code != codeInternal || body.RequestID != "panic-1" {
		t.Errorf("body = %+v, want internal_error with request_id panic-1", body)
	}
	if strings.Contains(rec.Body.String(), "impossible") {
		t.Errorf("the panic value reached the client: %s", rec.Body)
	}

	var panicLine map[string]any
	for _, line := range logLines(t, logged) {
		if line["msg"] == "panic recovered" {
			panicLine = line
		}
	}
	if panicLine == nil {
		t.Fatalf("no panic log line:\n%s", logged)
	}
	if panicLine["request_id"] != "panic-1" || panicLine["panic"] != "something impossible happened" {
		t.Errorf("panic line = %v", panicLine)
	}
	if stack, _ := panicLine["stack"].(string); !strings.Contains(stack, "protection_test.go") {
		t.Errorf("stack does not lead to the panicking handler: %q", stack)
	}
	// And the access log saw the 500, because Recovery runs inside it.
	if got := accessLine(t, logged, "/panic")["status"]; got != float64(http.StatusInternalServerError) {
		t.Errorf("access-log status = %v, want 500", got)
	}
}

// TestPanicDoesNotDisturbOtherRequests runs a slow request, panics in a
// second one while the first is mid-flight, and checks the first still
// completes normally, served by the same handler chain.
func TestPanicDoesNotDisturbOtherRequests(t *testing.T) {
	captureSlog(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.GET("/slow", func(c *gin.Context) {
			close(entered)
			<-release
			c.JSON(http.StatusOK, gin.H{"finished": true})
		})
		e.GET("/panic", func(*gin.Context) { panic("boom") })
	})

	slow := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		engine.ServeHTTP(slow, httptest.NewRequest(http.MethodGet, "/slow", nil))
		close(done)
	}()
	<-entered

	boom := httptest.NewRecorder()
	engine.ServeHTTP(boom, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if boom.Code != http.StatusInternalServerError {
		t.Fatalf("panicking request: status = %d, want 500", boom.Code)
	}

	close(release)
	<-done
	var body map[string]bool
	if slow.Code != http.StatusOK || json.Unmarshal(slow.Body.Bytes(), &body) != nil || !body["finished"] {
		t.Errorf("the in-flight request: status = %d, body = %s; want it to finish untouched", slow.Code, slow.Body)
	}
}

func TestRecoveryLeavesAbortHandlerToTheServer(t *testing.T) {
	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.GET("/abort", func(*gin.Context) { panic(http.ErrAbortHandler) })
	})

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler { //nolint:errorlint // the sentinel is panicked as is
			t.Errorf("recovered %v, want http.ErrAbortHandler passed through", rec)
		}
	}()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
	t.Error("ServeHTTP returned; want the panic to propagate to net/http")
}
