package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/auth"
)

// downPinger is a database that cannot be reached.
type downPinger struct{}

func (downPinger) Ping(context.Context) error {
	return errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")
}

func TestRoutes(t *testing.T) {
	t.Parallel()

	s := newTestServer(t)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"liveness is outside /v1", http.MethodGet, "/healthz", http.StatusOK},
		{"readiness is outside /v1", http.MethodGet, "/readyz", http.StatusOK},

		// 400 rather than 404 proves the route exists: the request reached the
		// handler, which then objected to the empty body.
		{"register is under /v1", http.MethodPost, "/v1/auth/register", http.StatusBadRequest},
		{"login is under /v1", http.MethodPost, "/v1/auth/login", http.StatusBadRequest},
		{"refresh is under /v1", http.MethodPost, "/v1/auth/refresh", http.StatusBadRequest},
		{"logout is under /v1", http.MethodPost, "/v1/auth/logout", http.StatusBadRequest},

		// 401 proves both that the route exists and that it is guarded.
		{"me is under /v1 and protected", http.MethodGet, "/v1/me", http.StatusUnauthorized},
		{"me without the version", http.MethodGet, "/me", http.StatusNotFound},

		{"job post is under /v1 and protected", http.MethodGet, "/v1/job-posts/not-checked-before-auth", http.StatusUnauthorized},
		{"creating a job post is protected", http.MethodPost, "/v1/job-posts", http.StatusUnauthorized},
		{"updating a job post is protected", http.MethodPatch, "/v1/job-posts/not-checked-before-auth", http.StatusUnauthorized},
		{"PUT on a PATCH route", http.MethodPut, "/v1/job-posts/not-checked-before-auth", http.StatusMethodNotAllowed},

		{"register without the version", http.MethodPost, "/auth/register", http.StatusNotFound},
		{"a version that does not exist", http.MethodPost, "/v2/auth/register", http.StatusNotFound},
		{"probes are not under /v1", http.MethodGet, "/v1/healthz", http.StatusNotFound},
		{"GET on a POST route", http.MethodGet, "/v1/auth/login", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.engine.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("%s %s: status = %d, want %d; body = %s", tt.method, tt.path, rec.Code, tt.wantStatus, rec.Body)
			}
			if rec.Code >= http.StatusBadRequest {
				decode(t, rec.Body.Bytes())
			}
		})
	}
}

func TestReadyzReportsAnUnreachableDatabase(t *testing.T) {
	logged := captureLog(t)

	users := newFakeUsers()
	engine, err := NewRouter(Deps{
		Auth:         NewAuthHandler(users, nil, nil),
		Applications: NewApplicationHandler(newFakeApplications()),
		JobPosts:     NewJobPostHandler(newFakeJobPosts()),
		Tokens:       auth.NewTokenManager(testJWTSecret),
		Users:        users,
		DB:           downPinger{},
		PingTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewRouter() unexpected error: %v", err)
	}

	ready := httptest.NewRecorder()
	engine.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if ready.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz status = %d, want %d", ready.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(ready.Body.String(), "10.0.0.5") {
		t.Errorf("/readyz body leaks the database address: %s", ready.Body)
	}
	if !strings.Contains(logged.String(), "10.0.0.5") {
		t.Errorf("the ping failure was not logged: %s", logged)
	}

	// Liveness must be unaffected by the same outage.
	live := httptest.NewRecorder()
	engine.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if live.Code != http.StatusOK {
		t.Errorf("/healthz status = %d, want %d while the database is down", live.Code, http.StatusOK)
	}
}
