package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// guarded is an engine with one route behind the given middleware, and a
// counter of how many requests reached the handler at the end of the chain.
type guarded struct {
	engine  *gin.Engine
	reached *atomic.Int32

	// seen is the user the handler read with CurrentUser on the last request
	// that reached it.
	seen atomic.Pointer[domain.User]
}

func newGuarded(middleware ...gin.HandlerFunc) *guarded {
	g := &guarded{reached: new(atomic.Int32)}

	g.engine = gin.New()
	handlers := append(middleware, func(c *gin.Context) {
		g.reached.Add(1)
		if user, ok := CurrentUser(c); ok {
			g.seen.Store(&user)
		}
		c.String(http.StatusOK, "handler ran")
	})
	g.engine.GET("/guarded", handlers...)

	return g
}

func (g *guarded) get(header string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	g.engine.ServeHTTP(rec, req)
	return rec
}

// mustCreateUser stores a user with the given role and returns it.
func mustCreateUser(t *testing.T, users *fakeUsers, email, role string) *domain.User {
	t.Helper()

	user, err := users.CreateUser(context.Background(), domain.NewUser{
		Email: email, PasswordHash: "unused", FullName: "Test User", Role: role,
	})
	if err != nil {
		t.Fatalf("create %s: %v", email, err)
	}
	return user
}

// bearerFor signs an access token for user carrying role, which need not be
// the role the user actually holds.
func bearerFor(t *testing.T, tokens *auth.TokenManager, userID, role string) string {
	t.Helper()

	token, err := tokens.NewAccessToken(userID, role)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return "Bearer " + token
}

func TestRequireAuthNeverRunsTheHandlerWithoutAValidCredential(t *testing.T) {
	t.Parallel()

	tokens := auth.NewTokenManager(testJWTSecret)
	users := newFakeUsers()
	alice := mustCreateUser(t, users, "alice@example.com", domain.RoleCandidate)

	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		Role: domain.RoleCandidate,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   alice.ID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	tests := []struct {
		name     string
		header   string
		wantCode string
	}{
		{"no Authorization header", "", "unauthenticated"},
		{"wrong scheme", "Basic YWxpY2U6cGFzc3dvcmQ=", "unauthenticated"},
		{"scheme with no token", "Bearer ", "unauthenticated"},
		{"not a token", "Bearer not-a-jwt", "unauthenticated"},
		{"expired", "Bearer " + expired, "token_expired"},
		{"valid token for a user who does not exist", bearerFor(t, tokens, "00000000-0000-4000-8000-999999999999", domain.RoleAdmin), "unauthenticated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A fresh engine per case, so the counter belongs to this case.
			g := newGuarded(RequireAuth(tokens, users))
			rec := g.get(tt.header)

			if n := g.reached.Load(); n != 0 {
				t.Fatalf("handler ran %d time(s); it must never run without a valid credential", n)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if got := decode(t, rec.Body.Bytes()); got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
			}
			// Proves the handler's own output was not appended after the
			// envelope, which is what a missing abort produces.
			if strings.Contains(rec.Body.String(), "handler ran") {
				t.Errorf("body contains the handler's output: %s", rec.Body)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
		})
	}
}

// The token says admin; the database says candidate. The database wins.
func TestRequireAuthUsesTheRoleFromTheDatabaseNotTheToken(t *testing.T) {
	t.Parallel()

	tokens := auth.NewTokenManager(testJWTSecret)
	users := newFakeUsers()
	alice := mustCreateUser(t, users, "alice@example.com", domain.RoleCandidate)

	header := bearerFor(t, tokens, alice.ID, domain.RoleAdmin)

	g := newGuarded(RequireAuth(tokens, users))
	if rec := g.get(header); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
	}
	if got := g.seen.Load(); got == nil || got.Role != domain.RoleCandidate {
		t.Fatalf("CurrentUser = %+v, want alice as a candidate", got)
	}

	admin := newGuarded(RequireAuth(tokens, users), RequireRole(domain.RoleAdmin))
	if rec := admin.get(header); rec.Code != http.StatusForbidden {
		t.Errorf("admin route with an admin claim and a candidate row: status = %d, want 403", rec.Code)
	}
	if n := admin.reached.Load(); n != 0 {
		t.Errorf("admin handler ran %d time(s)", n)
	}
}

// A role changed in the database takes effect on the next request, without
// waiting for the user's token to expire.
func TestRequireAuthSeesARoleChangeImmediately(t *testing.T) {
	t.Parallel()

	tokens := auth.NewTokenManager(testJWTSecret)
	users := newFakeUsers()
	ian := mustCreateUser(t, users, "ian@example.com", domain.RoleInterviewer)
	header := bearerFor(t, tokens, ian.ID, domain.RoleInterviewer)

	g := newGuarded(RequireAuth(tokens, users), RequireRole(domain.RoleInterviewer))
	if rec := g.get(header); rec.Code != http.StatusOK {
		t.Fatalf("before demotion: status = %d, want 200", rec.Code)
	}

	users.mu.Lock()
	ian.Role = domain.RoleCandidate
	users.mu.Unlock()

	if rec := g.get(header); rec.Code != http.StatusForbidden {
		t.Errorf("after demotion, same token: status = %d, want 403", rec.Code)
	}
	if n := g.reached.Load(); n != 1 {
		t.Errorf("handler ran %d time(s), want 1", n)
	}
}

// Not using t.Parallel: captureLog swaps the global logger.
func TestRequireAuthWhenTheDatabaseFails(t *testing.T) {
	logged := captureLog(t)

	tokens := auth.NewTokenManager(testJWTSecret)
	users := newFakeUsers()
	alice := mustCreateUser(t, users, "alice@example.com", domain.RoleCandidate)
	header := bearerFor(t, tokens, alice.ID, domain.RoleCandidate)

	users.failWith = errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")

	g := newGuarded(RequireAuth(tokens, users))
	rec := g.get(header)

	if n := g.reached.Load(); n != 0 {
		t.Fatalf("handler ran %d time(s)", n)
	}
	// 500, not 401: the credential was valid, and a 401 would make the client
	// throw its tokens away.
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("body leaks the cause: %s", rec.Body)
	}
	if !strings.Contains(logged.String(), "10.0.0.5") {
		t.Errorf("cause was not logged; log = %q", logged)
	}
}

func TestRequireRole(t *testing.T) {
	t.Parallel()

	tokens := auth.NewTokenManager(testJWTSecret)
	users := newFakeUsers()

	headers := map[string]string{}
	for _, role := range []string{domain.RoleAdmin, domain.RoleInterviewer, domain.RoleCandidate} {
		user := mustCreateUser(t, users, role+"@example.com", role)
		headers[role] = bearerFor(t, tokens, user.ID, role)
	}

	tests := []struct {
		name       string
		allowed    []string
		caller     string
		wantStatus int
	}{
		{"admin on an admin route", []string{domain.RoleAdmin}, domain.RoleAdmin, http.StatusOK},
		{"candidate on an admin route", []string{domain.RoleAdmin}, domain.RoleCandidate, http.StatusForbidden},
		{"interviewer on an admin route", []string{domain.RoleAdmin}, domain.RoleInterviewer, http.StatusForbidden},
		{"interviewer on a staff route", []string{domain.RoleAdmin, domain.RoleInterviewer}, domain.RoleInterviewer, http.StatusOK},
		{"admin on a staff route", []string{domain.RoleAdmin, domain.RoleInterviewer}, domain.RoleAdmin, http.StatusOK},
		{"candidate on a staff route", []string{domain.RoleAdmin, domain.RoleInterviewer}, domain.RoleCandidate, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newGuarded(RequireAuth(tokens, users), RequireRole(tt.allowed...))
			rec := g.get(headers[tt.caller])

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body)
			}

			wantReached := int32(0)
			if tt.wantStatus == http.StatusOK {
				wantReached = 1
			}
			if n := g.reached.Load(); n != wantReached {
				t.Errorf("handler ran %d time(s), want %d", n, wantReached)
			}

			if tt.wantStatus == http.StatusForbidden {
				if got := decode(t, rec.Body.Bytes()); got.Code != "forbidden" {
					t.Errorf("code = %q, want forbidden", got.Code)
				}
				// 403 is "known, not permitted": no challenge, since logging
				// in again would not help.
				if got := rec.Header().Get("WWW-Authenticate"); got != "" {
					t.Errorf("WWW-Authenticate = %q on a 403, want it absent", got)
				}
			}
		})
	}
}

// Not using t.Parallel: captureLog swaps the global logger.
func TestRequireRoleWithoutRequireAuthIsAServerError(t *testing.T) {
	logged := captureLog(t)

	g := newGuarded(RequireRole(domain.RoleAdmin))
	rec := g.get("")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if n := g.reached.Load(); n != 0 {
		t.Errorf("handler ran %d time(s)", n)
	}
	if !strings.Contains(logged.String(), "RequireAuth") {
		t.Errorf("log does not name the missing middleware: %q", logged)
	}
}

func TestRequireRolePanicsOnAMistakeInTheRouteTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		roles []string
	}{
		{"no roles", nil},
		{"misspelt role", []string{"admn"}},
		{"one good, one unknown", []string{domain.RoleAdmin, "superuser"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Errorf("RequireRole(%q) did not panic", tt.roles)
				}
			}()
			RequireRole(tt.roles...)
		})
	}
}

func TestRequireRoleIsUnaffectedByLaterChangesToItsArgument(t *testing.T) {
	t.Parallel()

	tokens := auth.NewTokenManager(testJWTSecret)
	users := newFakeUsers()
	candidate := mustCreateUser(t, users, "c@example.com", domain.RoleCandidate)

	roles := []string{domain.RoleAdmin}
	g := newGuarded(RequireAuth(tokens, users), RequireRole(roles...))
	roles[0] = domain.RoleCandidate

	if rec := g.get(bearerFor(t, tokens, candidate.ID, domain.RoleCandidate)); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestCurrentUser(t *testing.T) {
	t.Parallel()

	t.Run("absent on an unguarded route", func(t *testing.T) {
		t.Parallel()

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		if user, ok := CurrentUser(c); ok {
			t.Errorf("CurrentUser = %+v, true; want false", user)
		}
	})

	// The key's type is part of its identity, so a plain string with the same
	// text is a different key and cannot plant a user.
	t.Run("a string key with the same text is not the same key", func(t *testing.T) {
		t.Parallel()

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("currentUser", &domain.User{ID: "planted", Role: domain.RoleAdmin})

		if user, ok := CurrentUser(c); ok {
			t.Errorf("CurrentUser = %+v, true; want false", user)
		}
	})

	t.Run("a value of the wrong type is reported, not panicked on", func(t *testing.T) {
		t.Parallel()

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(currentUserKey, "not a user")

		if _, ok := CurrentUser(c); ok {
			t.Error("CurrentUser ok = true for a string value")
		}
	})

	t.Run("returns a copy", func(t *testing.T) {
		t.Parallel()

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(currentUserKey, &domain.User{ID: "u1", Role: domain.RoleCandidate})

		first, _ := CurrentUser(c)
		first.Role = domain.RoleAdmin

		if second, _ := CurrentUser(c); second.Role != domain.RoleCandidate {
			t.Errorf("role after a caller changed its copy to %q = %q, want candidate", first.Role, second.Role)
		}
	})
}
