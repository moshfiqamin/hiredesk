package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
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

	// lastViewer and lastFilter are what the most recent ListApplications
	// call received; listTotal and listMore are what it returns beside list.
	lastViewer domain.User
	lastFilter domain.ApplicationFilter
	listTotal  int
	listMore   bool

	// answers and events are what ApplicationHistory returns, by application
	// ID; historyCalls records every ID it was asked for.
	answers      map[string][]domain.Answer
	events       map[string][]domain.ApplicationEvent
	historyCalls []string

	// reviews is every review, by application ID, which ApplicationHistory
	// filters by the view. lastView and lastTimeline are what the latest
	// history or timeline call received.
	reviews      map[string][]domain.Review
	lastView     *domain.ApplicationView
	lastTimeline *domain.TimelineFilter

	// lastTransition is {id, to, actorID} of the most recent
	// TransitionApplication call; transitionErr, when set, is its result.
	lastTransition *[3]string
	transitionErr  error

	// failWith, when set, is returned by every call.
	failWith error

	// lastSubmitted is what the most recent SubmitApplication received;
	// submitResult and submitErr are what it returns.
	lastSubmitted *domain.NewApplication
	submitResult  *domain.Application
	submitErr     error

	// scoreCards is what ScoreCard returns, by application ID; an ID with no
	// card gets domain.ErrNotScored. scoreCalls records every ID asked for.
	scoreCards map[string]*domain.ScoreCard
	scoreCalls []string

	// rescoreResult and rescoreAllResult are what the rescore calls return,
	// rescoreErr their error; lastRescore is {id, actorID} of the latest.
	rescoreResult    *domain.RescoreResult
	rescoreAllResult *domain.JobPostRescore
	rescoreErr       error
	lastRescore      *[2]string

	// ranking is what RankApplications returns; lastRankLimit the limit it
	// was given.
	ranking       []domain.RankedApplication
	lastRankLimit int

	// points is what CriterionPoints returns; lastPointsIDs the IDs it was
	// given.
	points        map[string][]domain.CriterionPoints
	lastPointsIDs []string

	// searchResults and searchTotal are what SearchApplications returns;
	// lastSearch is the query, limit and offset it was given, nil until then.
	searchResults []domain.SearchResult
	searchTotal   int
	lastSearch    *searchCall
}

// searchCall is one SearchApplications call, as the fake received it.
type searchCall struct {
	query         string
	limit, offset int
}

func (f *fakeApplications) SearchApplications(_ context.Context, jobPostID, query string, limit, offset int) ([]domain.SearchResult, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastSearch = &searchCall{query: query, limit: limit, offset: offset}
	if f.failWith != nil {
		return nil, 0, f.failWith
	}
	if jobPostID == "bbbbbbbb-0000-4000-8000-00000000ffff" {
		return nil, 0, domain.ErrNotFound
	}
	return f.searchResults, f.searchTotal, nil
}

func (f *fakeApplications) RankApplications(_ context.Context, jobPostID string, limit int) ([]domain.RankedApplication, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastRankLimit = limit
	if f.failWith != nil {
		return nil, f.failWith
	}
	if jobPostID == "bbbbbbbb-0000-4000-8000-00000000ffff" {
		return nil, domain.ErrNotFound
	}
	return f.ranking, nil
}

func (f *fakeApplications) CriterionPoints(_ context.Context, ids []string) (map[string][]domain.CriterionPoints, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastPointsIDs = ids
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.points, nil
}

func (f *fakeApplications) RescoreApplication(_ context.Context, id, actorID string) (*domain.RescoreResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastRescore = &[2]string{id, actorID}
	if f.rescoreErr != nil {
		return nil, f.rescoreErr
	}
	return f.rescoreResult, nil
}

func (f *fakeApplications) RescoreJobPost(_ context.Context, jobPostID, actorID string) (*domain.JobPostRescore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastRescore = &[2]string{jobPostID, actorID}
	if f.rescoreErr != nil {
		return nil, f.rescoreErr
	}
	return f.rescoreAllResult, nil
}

func newFakeApplications() *fakeApplications {
	return &fakeApplications{
		byID:        make(map[string]domain.Application),
		assignments: make(map[[2]string]bool),
		scoreCards:  make(map[string]*domain.ScoreCard),
	}
}

func (f *fakeApplications) ScoreCard(_ context.Context, id string) (*domain.ScoreCard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.scoreCalls = append(f.scoreCalls, id)
	if f.failWith != nil {
		return nil, f.failWith
	}
	card, ok := f.scoreCards[id]
	if !ok {
		return nil, domain.ErrNotScored
	}
	return card, nil
}

func (f *fakeApplications) ListApplications(_ context.Context, viewer domain.User, filter domain.ApplicationFilter) ([]domain.Application, int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastViewer = viewer
	f.lastFilter = filter
	if f.failWith != nil {
		return nil, 0, false, f.failWith
	}
	return f.list, f.listTotal, f.listMore, nil
}

// ApplicationHistory returns the configured answers and events, and reviews
// filtered by the view the way the SQL filters them, so a handler test can
// check that the view it passes is the right one. It records each view.
func (f *fakeApplications) ApplicationHistory(_ context.Context, id string, view domain.ApplicationView) (*domain.ApplicationHistory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.historyCalls = append(f.historyCalls, id)
	f.lastView = &view
	if f.failWith != nil {
		return nil, f.failWith
	}

	history := &domain.ApplicationHistory{Answers: f.answers[id], Events: f.events[id]}
	switch view.Reviews {
	case domain.ReviewsAll:
		history.Reviews = append([]domain.Review{}, f.reviews[id]...)
	case domain.ReviewsOwn:
		history.Reviews = []domain.Review{}
		for _, r := range f.reviews[id] {
			if r.InterviewerID == view.ViewerID {
				history.Reviews = append(history.Reviews, r)
			}
		}
	}
	return history, nil
}

// Timeline returns the configured events for id, one page of them, and
// records the view and filter it was given.
func (f *fakeApplications) Timeline(_ context.Context, id string, view domain.ApplicationView, filter domain.TimelineFilter) ([]domain.ApplicationEvent, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastView = &view
	f.lastTimeline = &filter
	if f.failWith != nil {
		return nil, false, f.failWith
	}
	events := f.events[id]
	if len(events) > filter.Limit {
		return events[:filter.Limit], true, nil
	}
	return events, false, nil
}

func (f *fakeApplications) TransitionApplication(_ context.Context, id, to, actorID string) (*domain.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastTransition = &[3]string{id, to, actorID}
	if f.failWith != nil {
		return nil, f.failWith
	}
	if f.transitionErr != nil {
		return nil, f.transitionErr
	}
	app, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	app.Status = to
	return &app, nil
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

// SubmitApplication records what it was given and returns submitResult or
// submitErr. Validation is the store's and the domain's, and is tested there.
func (f *fakeApplications) SubmitApplication(_ context.Context, in domain.NewApplication) (*domain.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastSubmitted = &in
	if f.failWith != nil {
		return nil, f.failWith
	}
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	return f.submitResult, nil
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
				var got applicationDetailResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.ID != tt.id {
					t.Errorf("body = %s, want application %s", rec.Body, tt.id)
				}
				// The moves offered must be exactly the ones PATCH accepts.
				want := domain.AllowedApplicationTransitions(got.Status)
				if !slices.Equal(got.AllowedNextStatuses, want) {
					t.Errorf("allowed_next_statuses = %v, want %v", got.AllowedNextStatuses, want)
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

		want := `{"data":[],"next_cursor":null,"total":0}`
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("status = %d, body = %s; want 200 %s", rec.Code, rec.Body, want)
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

// reviewed_by_me is the interviewer's own flag: present when the store set
// it, absent otherwise, so no other role's list ever carries the key.
func TestListReviewedByMe(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	yes := true
	mine := f.mine
	mine.ReviewedByMe = &yes
	f.apps.list = []domain.Application{mine}

	rec := f.get("/v1/applications", f.headers["assigned interviewer"])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reviewed_by_me":true`) {
		t.Errorf("interviewer: status %d; body %s", rec.Code, rec.Body)
	}

	f.apps.list = []domain.Application{f.mine}
	if rec := f.get("/v1/applications", f.headers["admin"]); strings.Contains(rec.Body.String(), "reviewed_by_me") {
		t.Errorf("admin list carries reviewed_by_me: %s", rec.Body)
	}
}
