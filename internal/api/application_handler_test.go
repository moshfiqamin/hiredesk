package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// fakeApplications is an in-memory ApplicationStore. It does not implement
// the per-role filtering, which lives in SQL and is tested against PostgreSQL
// by TestAuthorizationMatrix. It records the viewer it was given and returns
// whatever list it was told to.
type fakeApplications struct {
	mu sync.Mutex

	byID        map[string]domain.Application
	assignments map[[2]string]bool // {applicationID, interviewerID}
	list        []domain.Application

	// lastViewer is the user the most recent ListApplications call received.
	lastViewer domain.User

	// failWith, when set, is returned by every call.
	failWith error
}

func newFakeApplications() *fakeApplications {
	return &fakeApplications{
		byID:        make(map[string]domain.Application),
		assignments: make(map[[2]string]bool),
	}
}

func (f *fakeApplications) ListApplications(_ context.Context, viewer domain.User) ([]domain.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastViewer = viewer
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.list, nil
}

func (f *fakeApplications) GetApplication(_ context.Context, id, viewerID string) (*domain.Application, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return nil, false, f.failWith
	}
	app, ok := f.byID[id]
	if !ok {
		return nil, false, domain.ErrNotFound
	}
	return &app, f.assignments[[2]string{id, viewerID}], nil
}

// appFixture is a test server with one user of each role and two applications.
type appFixture struct {
	*testServer

	headers map[string]string // role name → Authorization header
	users   map[string]*domain.User
	mine    domain.Application // submitted by the "candidate" user
	theirs  domain.Application // submitted by someone else
}

func newAppFixture(t *testing.T) *appFixture {
	t.Helper()

	f := &appFixture{
		testServer: newTestServer(t),
		headers:    map[string]string{},
		users:      map[string]*domain.User{},
	}

	for _, who := range []struct{ name, role string }{
		{"admin", domain.RoleAdmin},
		{"assigned interviewer", domain.RoleInterviewer},
		{"unassigned interviewer", domain.RoleInterviewer},
		{"candidate", domain.RoleCandidate},
		{"other candidate", domain.RoleCandidate},
	} {
		user := mustCreateUser(t, f.testServer.users, who.name+"@example.com", who.role)
		f.users[who.name] = user
		f.headers[who.name] = bearerFor(t, f.tokens, user.ID, who.role)
	}

	submitted := time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)
	f.mine = domain.Application{
		ID: "aaaaaaaa-0000-4000-8000-000000000001", JobPostID: "post-1",
		CandidateID: f.users["candidate"].ID, Status: domain.StatusSubmitted,
		CVURL: "https://cv.example/mine.pdf", SubmittedAt: submitted, UpdatedAt: submitted,
	}
	f.theirs = domain.Application{
		ID: "aaaaaaaa-0000-4000-8000-000000000002", JobPostID: "post-1",
		CandidateID: f.users["other candidate"].ID, Status: domain.StatusSubmitted,
		CVURL: "https://cv.example/theirs.pdf", SubmittedAt: submitted, UpdatedAt: submitted,
	}
	f.apps.byID[f.mine.ID] = f.mine
	f.apps.byID[f.theirs.ID] = f.theirs
	f.apps.assignments[[2]string{f.mine.ID, f.users["assigned interviewer"].ID}] = true

	return f
}

func TestGetApplication(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	const missing = "aaaaaaaa-0000-4000-8000-00000000ffff"

	tests := []struct {
		caller     string
		id         string
		wantStatus int
	}{
		{"admin", f.mine.ID, http.StatusOK},
		{"admin", f.theirs.ID, http.StatusOK},
		{"admin", missing, http.StatusNotFound},

		{"candidate", f.mine.ID, http.StatusOK},
		{"candidate", f.theirs.ID, http.StatusNotFound},
		{"candidate", missing, http.StatusNotFound},

		{"assigned interviewer", f.mine.ID, http.StatusOK},
		{"assigned interviewer", f.theirs.ID, http.StatusNotFound},
		{"unassigned interviewer", f.mine.ID, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.caller+" → "+tt.id[len(tt.id)-4:], func(t *testing.T) {
			t.Parallel()

			rec := f.get("/v1/applications/"+tt.id, f.headers[tt.caller])
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusOK {
				var got applicationResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.ID != tt.id {
					t.Errorf("body = %s, want application %s", rec.Body, tt.id)
				}
			}
		})
	}
}

// The point of answering 404: a caller cannot tell "not yours" from "not
// there", by status or by body.
func TestGetApplicationRefusalIsIndistinguishableFromAbsence(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)

	refused := f.get("/v1/applications/"+f.theirs.ID, f.headers["candidate"])
	absent := f.get("/v1/applications/aaaaaaaa-0000-4000-8000-00000000ffff", f.headers["candidate"])

	if refused.Code != absent.Code {
		t.Errorf("status: refused %d, absent %d", refused.Code, absent.Code)
	}
	if refused.Body.String() != absent.Body.String() {
		t.Errorf("bodies differ:\nrefused: %s\nabsent:  %s", refused.Body, absent.Body)
	}
	for name, h := range map[string]http.Header{"refused": refused.Header(), "absent": absent.Header()} {
		if h.Get("WWW-Authenticate") != "" {
			t.Errorf("%s: WWW-Authenticate set on a 404", name)
		}
	}
}

func TestListApplications(t *testing.T) {
	t.Parallel()

	t.Run("passes the user from the database to the store", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.list = []domain.Application{f.mine}

		// The token claims admin; the store must be asked as the candidate the
		// database says this user is.
		header := bearerFor(t, f.tokens, f.users["candidate"].ID, domain.RoleAdmin)
		rec := f.get("/v1/applications", header)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		f.apps.mu.Lock()
		viewer := f.apps.lastViewer
		f.apps.mu.Unlock()
		if viewer.ID != f.users["candidate"].ID || viewer.Role != domain.RoleCandidate {
			t.Errorf("store was asked as %+v, want the candidate", viewer)
		}

		var body listResponse[applicationResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if len(body.Data) != 1 || body.Data[0].ID != f.mine.ID {
			t.Errorf("data = %+v, want only %s", body.Data, f.mine.ID)
		}
	})

	t.Run("an empty result is an empty array", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		rec := f.get("/v1/applications", f.headers["candidate"])

		if rec.Code != http.StatusOK || rec.Body.String() != `{"data":[]}` {
			t.Errorf("status = %d, body = %s; want 200 {\"data\":[]}", rec.Code, rec.Body)
		}
	})

	t.Run("a role the store refuses is a 403", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.failWith = domain.ErrForbidden
		rec := f.get("/v1/applications", f.headers["candidate"])

		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("without a token the store is never asked", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		rec := f.get("/v1/applications", "")

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		f.apps.mu.Lock()
		defer f.apps.mu.Unlock()
		if f.apps.lastViewer.ID != "" {
			t.Errorf("store was called with %+v", f.apps.lastViewer)
		}
	})
}

// Not using t.Parallel: captureLog swaps the global logger.
func TestApplicationStoreFailures(t *testing.T) {
	logged := captureLog(t)

	f := newAppFixture(t)
	f.apps.failWith = errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")

	for _, path := range []string{"/v1/applications", "/v1/applications/" + f.mine.ID} {
		rec := f.get(path, f.headers["admin"])
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", path, rec.Code)
		}
		if got := decode(t, rec.Body.Bytes()); got.Code != "internal_error" {
			t.Errorf("%s: code = %q", path, got.Code)
		}
	}
	if n := strings.Count(logged.String(), "10.0.0.5"); n != 2 {
		t.Errorf("cause logged %d times, want 2; log = %q", n, logged)
	}
}
