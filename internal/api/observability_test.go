package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/platform"
)

// captureSlog sends the default logger's output, as JSON, to the returned
// buffer for the rest of the test. Tests that use it must not run in
// parallel with each other: the default logger is process-wide.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(platform.NewLogger(&buf, slog.LevelDebug, platform.FormatJSON))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &buf
}

// logLines parses every JSON line in buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var lines []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	// Stack traces make for long lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, scanner.Bytes())
		}
		lines = append(lines, line)
	}
	return lines
}

// accessLine returns the access-log line for path, failing the test if
// there is not exactly one.
func accessLine(t *testing.T, buf *bytes.Buffer, path string) map[string]any {
	t.Helper()

	var found []map[string]any
	for _, line := range logLines(t, buf) {
		if line["msg"] == "request" && line["path"] == path {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d access-log lines for %s, want 1:\n%s", len(found), path, buf)
	}
	return found[0]
}

// middlewareEngine is an engine with the production middleware chain and the
// given extra routes, for testing the chain without the whole API.
func middlewareEngine(inFlight *InFlight, routes func(*gin.Engine)) *gin.Engine {
	engine := gin.New()
	engine.Use(RequestID(), AccessLog(inFlight), Recovery(), CORS(nil), BodyLimit(maxBodyBytes))
	routes(engine)
	return engine
}

func TestRequestIDIsGeneratedEchoedAndInTheContext(t *testing.T) {
	var seen string
	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.GET("/x", func(c *gin.Context) {
			seen = platform.RequestID(c.Request.Context())
			c.Status(http.StatusNoContent)
		})
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	got := rec.Header().Get(RequestIDHeader)
	if len(got) != 26 {
		t.Errorf("generated ID = %q, want 26 characters of base32", got)
	}
	if seen != got {
		t.Errorf("context ID = %q, header ID = %q; want the same", seen, got)
	}

	second := httptest.NewRecorder()
	engine.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/x", nil))
	if second.Header().Get(RequestIDHeader) == got {
		t.Error("two requests were given the same generated ID")
	}
}

func TestInboundRequestID(t *testing.T) {
	tests := []struct {
		name    string
		inbound string
		kept    bool
	}{
		{name: "a UUID", inbound: "3f1c9a52-7d3e-4b8e-9f20-6a1d2c3b4e5f", kept: true},
		{name: "a prefixed trace ID", inbound: "web:checkout.v2_0001", kept: true},
		{name: "128 characters", inbound: strings.Repeat("a", 128), kept: true},
		{name: "129 characters", inbound: strings.Repeat("a", 129)},
		{name: "a newline, forging a log line", inbound: "abc\nlevel=ERROR msg=forged"},
		{name: "a quote", inbound: `abc"def`},
		{name: "a space", inbound: "abc def"},
		{name: "non-ASCII", inbound: "café"},
	}

	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			// Header values are set directly, bypassing http.Header's own
			// validation, as a raw client could send them.
			req.Header[http.CanonicalHeaderKey(RequestIDHeader)] = []string{tt.inbound}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			got := rec.Header().Get(RequestIDHeader)
			if tt.kept && got != tt.inbound {
				t.Errorf("ID = %q, want the inbound %q kept", got, tt.inbound)
			}
			if !tt.kept && (got == tt.inbound || len(got) != 26) {
				t.Errorf("ID = %q, want the inbound replaced by a generated one", got)
			}
		})
	}
}

func TestErrorBodyCarriesTheRequestID(t *testing.T) {
	engine := middlewareEngine(NewInFlight(), func(*gin.Engine) {})
	engine.NoRoute(NoRoute)

	req := httptest.NewRequest(http.MethodGet, "/nowhere", nil)
	req.Header.Set(RequestIDHeader, "bug-report-42")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if got := decode(t, rec.Body.Bytes()).RequestID; got != "bug-report-42" {
		t.Errorf("request_id in body = %q, want bug-report-42", got)
	}
}

func TestAccessLogRecordsEveryField(t *testing.T) {
	logged := captureSlog(t)

	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.GET("/v1/things/:id", func(c *gin.Context) {
			// Stands in for RequireAuth.
			c.Request = c.Request.WithContext(platform.WithUserID(c.Request.Context(), "user-7"))
			c.String(http.StatusTeapot, "short and stout")
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/things/42?refresh_token=leaky", nil)
	req.Header.Set(RequestIDHeader, "req-abc")
	req.Header.Set("Authorization", "Bearer header-secret")
	engine.ServeHTTP(httptest.NewRecorder(), req)

	line := accessLine(t, logged, "/v1/things/42")
	want := map[string]any{
		"level":      "INFO",
		"method":     "GET",
		"route":      "/v1/things/:id",
		"status":     float64(http.StatusTeapot),
		"bytes":      float64(len("short and stout")),
		"client_ip":  "192.0.2.1",
		"request_id": "req-abc",
		"user_id":    "user-7",
	}
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s = %v, want %v", key, line[key], value)
		}
	}
	if _, ok := line["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms = %v, want a number", line["duration_ms"])
	}

	for _, leaked := range []string{"leaky", "header-secret"} {
		if strings.Contains(logged.String(), leaked) {
			t.Errorf("log contains %q:\n%s", leaked, logged)
		}
	}
}

func TestAccessLogLevelForServerErrors(t *testing.T) {
	logged := captureSlog(t)

	engine := middlewareEngine(NewInFlight(), func(e *gin.Engine) {
		e.GET("/fail", func(c *gin.Context) { c.Status(http.StatusBadGateway) })
	})
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fail", nil))

	if got := accessLine(t, logged, "/fail")["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR for a 5xx", got)
	}
}

func TestInFlightTracksRunningRequests(t *testing.T) {
	inFlight := NewInFlight()
	release := make(chan struct{})
	entered := make(chan struct{})

	engine := middlewareEngine(inFlight, func(e *gin.Engine) {
		e.GET("/slow", func(c *gin.Context) {
			close(entered)
			<-release
			c.Status(http.StatusNoContent)
		})
	})

	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/slow", nil)
		req.Header.Set(RequestIDHeader, "slow-one")
		engine.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()

	<-entered
	snapshot := inFlight.Snapshot()
	if len(snapshot) != 1 || snapshot[0].RequestID != "slow-one" || snapshot[0].Path != "/slow" {
		t.Errorf("snapshot while running = %+v, want the one slow request", snapshot)
	}

	close(release)
	<-done
	if n := inFlight.Count(); n != 0 {
		t.Errorf("count after completion = %d, want 0", n)
	}
}

// TestLoginNeverLogsCredentials drives a real registration, login, refresh,
// authenticated call and logout through the production router, with the
// logger at debug level, and checks that no password, token or Authorization
// header reaches the log.
func TestLoginNeverLogsCredentials(t *testing.T) {
	logged := captureSlog(t)
	s := newTestServer(t)

	const password = "correct horse"
	s.mustRegister(t)
	pair := s.login(t, "alice@example.com")
	rotated := s.mustRefresh(t, pair.RefreshToken)

	if rec := s.get("/v1/me", "Bearer "+rotated.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("/v1/me: status = %d", rec.Code)
	}
	// A failed login, whose wrong password must not be logged either.
	s.post("/v1/auth/login", `{"email":"alice@example.com","password":"wrong-guess-123"}`)
	// A replayed refresh token: the security event is logged, the token is not.
	s.refreshWith(pair.RefreshToken)
	s.post("/v1/auth/logout", `{"refresh_token":"`+rotated.RefreshToken+`"}`)

	secrets := map[string]string{
		"password":              password,
		"wrong password":        "wrong-guess-123",
		"access token":          pair.AccessToken,
		"rotated access token":  rotated.AccessToken,
		"refresh token":         pair.RefreshToken,
		"rotated refresh token": rotated.RefreshToken,
		"JWT secret":            testJWTSecret,
		"Authorization header":  "Bearer ",
	}
	for name, secret := range secrets {
		if strings.Contains(logged.String(), secret) {
			t.Errorf("the log contains the %s:\n%s", name, logged)
		}
	}

	// The log is not simply empty: every request was recorded.
	if n := strings.Count(logged.String(), `"msg":"request"`); n < 6 {
		t.Errorf("%d access-log lines, want at least 6:\n%s", n, logged)
	}
}
