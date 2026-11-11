//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// IDs for the assignment, review and timeline tests, on top of the scoring
// post from seedScoring and its admin.
const (
	ivyID   = "00000000-0000-4000-8000-0000000000c1" // interviewer
	omarID  = "00000000-0000-4000-8000-0000000000c2" // interviewer
	rivalID = "00000000-0000-4000-8000-0000000000c3" // a second candidate
)

// reviewFixture is the scoring post with two applications, two interviewers
// and a router, plus a signed token per person.
type reviewFixture struct {
	pool   *pgxpool.Pool
	router *gin.Engine
	tokens map[string]string // user ID → Authorization header

	// app is subCandidateID's application, rivalApp the rival's.
	app, rivalApp string
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	ctx := context.Background()

	pool := startPostgres(t)
	seedScoring(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, full_name, role) VALUES
			('`+ivyID+`',   'ivy@test',   'unused', 'Ivy Interviewer',  'interviewer'),
			('`+omarID+`',  'omar@test',  'unused', 'Omar Interviewer', 'interviewer'),
			('`+rivalID+`', 'rival@test', 'unused', 'Rival Candidate',  'candidate')`); err != nil {
		t.Fatalf("seed people: %v", err)
	}

	apps := store.NewApplicationStore(pool)
	mine, err := apps.SubmitApplication(ctx, scoredApplication())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	rival := scoredApplication()
	rival.CandidateID = rivalID
	theirs, err := apps.SubmitApplication(ctx, rival)
	if err != nil {
		t.Fatalf("submit rival: %v", err)
	}

	manager := auth.NewTokenManager(testJWTSecret)
	tokens := map[string]string{}
	for id, role := range map[string]string{
		scoringAdminID: domain.RoleAdmin, ivyID: domain.RoleInterviewer, omarID: domain.RoleInterviewer,
		subCandidateID: domain.RoleCandidate, rivalID: domain.RoleCandidate,
	} {
		token, err := manager.NewAccessToken(id, role)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		tokens[id] = "Bearer " + token
	}

	return &reviewFixture{pool: pool, router: newRouter(t, pool, manager), tokens: tokens, app: mine.ID, rivalApp: theirs.ID}
}

// call sends a request as the user asID, with body as JSON when not empty.
func (f *reviewFixture) call(method, path, asID, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", f.tokens[asID])
	f.router.ServeHTTP(rec, req)
	return rec
}

// assign assigns interviewerID to appID as the admin and returns the status.
func (f *reviewFixture) assign(t *testing.T, appID, interviewerID string) int {
	t.Helper()
	return f.call(http.MethodPost, "/v1/applications/"+appID+"/assign", scoringAdminID,
		`{"interviewer_id":"`+interviewerID+`"}`).Code
}

// count runs a count(*) query with args.
func (f *reviewFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// eventCount counts the events of one type on one application.
func (f *reviewFixture) eventCount(t *testing.T, appID, eventType string) int {
	t.Helper()
	return f.count(t, `SELECT count(*) FROM application_events WHERE application_id = $1::text::uuid AND type = $2`, appID, eventType)
}

// TestAssign_RefusesNonInterviewers is the privilege-escalation guard. A
// candidate assigned to a rival's application would be able to read it, so
// every non-interviewer target is a 422, and nothing at all is written.
func TestAssign_RefusesNonInterviewers(t *testing.T) {
	f := newReviewFixture(t)

	for _, tt := range []struct{ name, target string }{
		{"the rival candidate", rivalID},
		{"the applicant themself", subCandidateID},
		{"an admin", scoringAdminID},
		{"a user who does not exist", "00000000-0000-4000-8000-0000000000ff"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := f.call(http.MethodPost, "/v1/applications/"+f.app+"/assign", scoringAdminID, `{"interviewer_id":"`+tt.target+`"}`)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422; body %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), "interviewer_id") {
				t.Errorf("422 does not name interviewer_id: %s", rec.Body)
			}
		})
	}

	if n := f.count(t, `SELECT count(*) FROM interviewer_assignments`); n != 0 {
		t.Errorf("%d assignments written, want 0", n)
	}
	if n := f.eventCount(t, f.app, domain.EventApplicationAssigned); n != 0 {
		t.Errorf("%d assigned events written, want 0", n)
	}

	// And the point of it all: the rival still cannot read the application.
	if rec := f.call(http.MethodGet, "/v1/applications/"+f.app, rivalID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("rival reading the application: status %d, want 404", rec.Code)
	}

	// The second line: even SQL that skips the store cannot do it.
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO interviewer_assignments (application_id, interviewer_id) VALUES ($1::text::uuid, $2::text::uuid)`,
		f.app, rivalID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "interviewer_assignments_interviewer_is_interviewer_fkey" {
		t.Errorf("direct insert of a candidate: %v, want the composite foreign key to refuse it", err)
	}
}

// TestAssign_Idempotent assigns the same interviewer twice in a row, then
// ten times at once, and checks that there is one assignment and one event.
func TestAssign_Idempotent(t *testing.T) {
	f := newReviewFixture(t)

	if code := f.assign(t, f.app, ivyID); code != http.StatusCreated {
		t.Fatalf("first: %d, want 201", code)
	}
	if code := f.assign(t, f.app, strings.ToUpper(ivyID)); code != http.StatusOK {
		t.Fatalf("repeat, in upper case: %d, want 200", code)
	}

	// Ten identical requests at once, as a client retrying on a timeout
	// might send. Exactly one may create the assignment.
	const n = 10
	codes := make([]int, n)
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes[i] = f.assign(t, f.rivalApp, ivyID)
		}()
	}
	close(start)
	wg.Wait()

	created := 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Errorf("a concurrent assign answered %d", code)
		}
	}
	if created != 1 {
		t.Errorf("%d requests created the assignment, want exactly 1 (codes %v)", created, codes)
	}

	for _, appID := range []string{f.app, f.rivalApp} {
		if rows := f.count(t, `SELECT count(*) FROM interviewer_assignments WHERE application_id = $1::text::uuid`, appID); rows != 1 {
			t.Errorf("application %s: %d assignments, want 1", appID, rows)
		}
		if events := f.eventCount(t, appID, domain.EventApplicationAssigned); events != 1 {
			t.Errorf("application %s: %d assigned events, want 1", appID, events)
		}
	}
}

// TestUnassignAndList covers the list, revoking access, and that a review
// outlives the assignment.
func TestUnassignAndList(t *testing.T) {
	f := newReviewFixture(t)

	f.assign(t, f.app, ivyID)
	f.assign(t, f.app, omarID)
	if rec := f.call(http.MethodPost, "/v1/applications/"+f.app+"/reviews", ivyID, `{"mark":6}`); rec.Code != http.StatusCreated {
		t.Fatalf("review: %d %s", rec.Code, rec.Body)
	}

	rec := f.call(http.MethodGet, "/v1/applications/"+f.app+"/assignments", scoringAdminID, "")
	var list struct {
		Data []struct {
			InterviewerID   string `json:"interviewer_id"`
			InterviewerName string `json:"interviewer_name"`
			AssignedByName  string `json:"assigned_by_name"`
			Reviewed        bool   `json:"reviewed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK || len(list.Data) != 2 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if d := list.Data[0]; d.InterviewerID != ivyID || d.InterviewerName != "Ivy Interviewer" || d.AssignedByName != "Admin" || !d.Reviewed {
		t.Errorf("first assignment = %+v", d)
	}
	if list.Data[1].Reviewed {
		t.Error("Omar has not reviewed")
	}

	path := "/v1/applications/" + f.app + "/assign/" + ivyID
	if code := f.call(http.MethodDelete, path, scoringAdminID, "").Code; code != http.StatusNoContent {
		t.Fatalf("unassign: %d", code)
	}
	if code := f.call(http.MethodDelete, path, scoringAdminID, "").Code; code != http.StatusNoContent {
		t.Errorf("repeat unassign: %d, want 204", code)
	}
	if n := f.eventCount(t, f.app, domain.EventApplicationUnassigned); n != 1 {
		t.Errorf("%d unassigned events, want 1: the repeat changed nothing", n)
	}

	// Access goes with the assignment, at once.
	if code := f.call(http.MethodGet, "/v1/applications/"+f.app, ivyID, "").Code; code != http.StatusNotFound {
		t.Errorf("unassigned interviewer reading the application: %d, want 404", code)
	}
	// The review stays: it is evidence, and the admin still sees it.
	detail := f.call(http.MethodGet, "/v1/applications/"+f.app, scoringAdminID, "")
	if !strings.Contains(detail.Body.String(), `"interviewer_id":"`+ivyID+`"`) {
		t.Errorf("Ivy's review is gone from the admin's view: %s", detail.Body)
	}

	if code := f.call(http.MethodDelete, "/v1/applications/"+missingApp+"/assign/"+ivyID, scoringAdminID, "").Code; code != http.StatusNotFound {
		t.Errorf("unassign on a missing application: %d, want 404", code)
	}
}
