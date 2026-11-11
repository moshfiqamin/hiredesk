package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// fakeJobPosts is an in-memory JobPostStore. Like the real one, UpdateJobPost
// applies the patch to the stored post and validates the result.
type fakeJobPosts struct {
	mu sync.Mutex

	byID map[string]domain.JobPost
	next int

	// readiness holds the rubric counts per post ID. Questions are counted
	// from the post itself.
	readiness map[string]domain.OpenReadiness

	// lastActor is the actor the most recent TransitionJobPost call received,
	// and lastQuestionPatch the patch the most recent UpdateQuestion did.
	lastActor         string
	lastQuestionPatch domain.QuestionPatch

	// lastPatch is the patch the most recent UpdateJobPost call received, and
	// calls counts every call to any method.
	lastPatch domain.JobPostPatch
	calls     int

	// failWith, when set, is returned by every call.
	failWith error
}

func newFakeJobPosts() *fakeJobPosts {
	return &fakeJobPosts{byID: make(map[string]domain.JobPost), readiness: make(map[string]domain.OpenReadiness)}
}

func (f *fakeJobPosts) CreateJobPost(_ context.Context, in domain.NewJobPost) (*domain.JobPost, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.failWith != nil {
		return nil, f.failWith
	}

	f.next++
	now := time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)
	post := domain.JobPost{
		ID:             fmt.Sprintf("bbbbbbbb-0000-4000-8000-%012d", f.next),
		Title:          in.Title,
		Description:    in.Description,
		EmploymentType: in.EmploymentType,
		Location:       in.Location,
		Vacancies:      in.Vacancies,
		SalaryMin:      in.SalaryMin,
		SalaryMax:      in.SalaryMax,
		Status:         domain.JobStatusDraft,
		ClosesAt:       in.ClosesAt,
		CreatedBy:      &in.CreatedBy,
		CreatedAt:      now,
		UpdatedAt:      now,
		Questions:      []domain.Question{},
	}
	f.byID[post.ID] = post
	return &post, nil
}

func (f *fakeJobPosts) GetJobPostByID(_ context.Context, id string, includeDrafts bool) (*domain.JobPost, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.failWith != nil {
		return nil, f.failWith
	}

	post, ok := f.byID[id]
	if !ok || (post.Status == domain.JobStatusDraft && !includeDrafts) {
		return nil, domain.ErrNotFound
	}
	return &post, nil
}

func (f *fakeJobPosts) UpdateJobPost(_ context.Context, id string, patch domain.JobPostPatch) (*domain.JobPost, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.lastPatch = patch
	if f.failWith != nil {
		return nil, f.failWith
	}

	post, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	post.Apply(patch)
	if err := post.Validate(); err != nil {
		return nil, err
	}
	f.byID[id] = post
	return &post, nil
}

// TransitionJobPost runs the same domain checks as the real store, in the
// same order, so the handler sees the same errors.
func (f *fakeJobPosts) TransitionJobPost(_ context.Context, id, to, actorID string) (*domain.JobPost, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.lastActor = actorID
	if f.failWith != nil {
		return nil, f.failWith
	}

	post, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if err := domain.CheckTransition(post.Status, to); err != nil {
		return nil, err
	}
	if to == domain.JobStatusOpen {
		readiness := f.readiness[id]
		readiness.Questions = len(post.Questions)
		if err := domain.CheckReadyToOpen(readiness); err != nil {
			return nil, err
		}
	}

	post.Status = to
	f.byID[id] = post
	return &post, nil
}

func (f *fakeJobPosts) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// send makes a request with an optional Authorization header and JSON body.
func (s *testServer) send(method, path, header, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	s.engine.ServeHTTP(rec, req)
	return rec
}

// postFixture is a test server with one user of each role, a draft post and
// an open post with two questions.
type postFixture struct {
	*testServer

	headers map[string]string // role → Authorization header
	adminID string
	draft   domain.JobPost
	open    domain.JobPost
}

const (
	draftPostID   = "cccccccc-0000-4000-8000-000000000001"
	openPostID    = "cccccccc-0000-4000-8000-000000000002"
	missingPostID = "cccccccc-0000-4000-8000-00000000ffff"
)

func newPostFixture(t *testing.T) *postFixture {
	t.Helper()

	f := &postFixture{testServer: newTestServer(t), headers: map[string]string{}}

	for _, role := range []string{domain.RoleAdmin, domain.RoleInterviewer, domain.RoleCandidate} {
		user := mustCreateUser(t, f.users, role+"@example.com", role)
		f.headers[role] = bearerFor(t, f.tokens, user.ID, role)
		if role == domain.RoleAdmin {
			f.adminID = user.ID
		}
	}

	created := time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)
	description, location := "Builds the API.", "Dhaka"
	salaryMin, salaryMax := domain.Money(5000000), domain.Money(9000000)

	f.draft = domain.JobPost{
		ID: draftPostID, Title: "Backend Engineer", Description: &description, Location: &location,
		Vacancies: 2, SalaryMin: &salaryMin, SalaryMax: &salaryMax,
		Status: domain.JobStatusDraft, CreatedBy: &f.adminID, CreatedAt: created, UpdatedAt: created,
		Questions: []domain.Question{},
	}
	f.open = domain.JobPost{
		ID: openPostID, Title: "Data Analyst", Vacancies: 1,
		Status: domain.JobStatusOpen, CreatedBy: &f.adminID, CreatedAt: created, UpdatedAt: created,
		Questions: []domain.Question{
			{ID: qOne, JobPostID: openPostID, Prompt: "Why this role?", Kind: "text", Required: true, MinLength: 50, Position: 1},
			{ID: qTwo, JobPostID: openPostID, Prompt: "Preferred office", Kind: "choice", Choices: []string{"Dhaka", "Remote"}, Position: 2},
		},
	}
	f.posts.byID[f.draft.ID] = f.draft
	f.posts.byID[f.open.ID] = f.open

	return f
}

// stored returns the post as the fake store now holds it.
func (f *postFixture) stored(id string) domain.JobPost {
	f.posts.mu.Lock()
	defer f.posts.mu.Unlock()
	return f.posts.byID[id]
}

func decodePost(t *testing.T, rec *httptest.ResponseRecorder) jobPostResponse {
	t.Helper()

	var post jobPostResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &post); err != nil {
		t.Fatalf("body is not a job post: %v\n%s", err, rec.Body)
	}
	return post
}

func TestCreateJobPost(t *testing.T) {
	t.Parallel()

	t.Run("a title alone creates a draft with one vacancy", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPost, "/v1/job-posts", f.headers[domain.RoleAdmin], `{"title":"  Platform Engineer  "}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body)
		}
		got := decodePost(t, rec)

		if got.Title != "Platform Engineer" {
			t.Errorf("title = %q, want it trimmed", got.Title)
		}
		if got.Status != domain.JobStatusDraft {
			t.Errorf("status = %q, want draft", got.Status)
		}
		if got.Vacancies != 1 {
			t.Errorf("vacancies = %d, want the default of 1", got.Vacancies)
		}
		if got.CreatedBy == nil || *got.CreatedBy != f.adminID {
			t.Errorf("created_by = %v, want the admin %s", got.CreatedBy, f.adminID)
		}
		if want := "/v1/job-posts/" + got.ID; rec.Header().Get("Location") != want {
			t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), want)
		}

		// Unset fields are null and an empty form is [], so a client reads
		// one shape whatever the post contains.
		keys := keysOf(t, rec.Body.Bytes())
		for _, key := range []string{"description", "employment_type", "location", "salary_min", "salary_max", "closes_at"} {
			if string(keys[key]) != "null" {
				t.Errorf("%s = %s, want null", key, keys[key])
			}
		}
		if string(keys["questions"]) != "[]" {
			t.Errorf("questions = %s, want []", keys["questions"])
		}
	})

	t.Run("every field", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPost, "/v1/job-posts", f.headers[domain.RoleAdmin], `{
			"title": "Platform Engineer",
			"description": "Runs the platform.",
			"employment_type": "full-time",
			"location": "Remote",
			"vacancies": 3,
			"salary_min": 85000,
			"salary_max": 120000.5,
			"closes_at": "2026-12-31T18:00:00+06:00"
		}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body)
		}
		got := decodePost(t, rec)

		if got.Description == nil || *got.Description != "Runs the platform." ||
			got.EmploymentType == nil || *got.EmploymentType != "full-time" ||
			got.Location == nil || *got.Location != "Remote" || got.Vacancies != 3 {
			t.Errorf("post = %+v", got)
		}
		if got.ClosesAt == nil || got.ClosesAt.Format(time.RFC3339) != "2026-12-31T12:00:00Z" {
			t.Errorf("closes_at = %v, want the same instant in UTC", got.ClosesAt)
		}

		// The amounts are checked as text: two decimal places, no quotes.
		keys := keysOf(t, rec.Body.Bytes())
		if string(keys["salary_min"]) != "85000.00" || string(keys["salary_max"]) != "120000.50" {
			t.Errorf("salary_min = %s, salary_max = %s; want 85000.00 and 120000.50", keys["salary_min"], keys["salary_max"])
		}
	})

	t.Run("a status in the body is ignored", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPost, "/v1/job-posts", f.headers[domain.RoleAdmin], `{"title":"x","status":"open"}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body)
		}
		if got := decodePost(t, rec); got.Status != domain.JobStatusDraft {
			t.Errorf("status = %q, want draft", got.Status)
		}
	})
}

func TestCreateJobPostValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantFields []string
	}{
		{"no title", `{}`, []string{"title"}},
		{"empty title", `{"title":""}`, []string{"title"}},
		{"title of only spaces", `{"title":"   "}`, []string{"title"}},
		{"title of 201 characters", `{"title":"` + strings.Repeat("a", 201) + `"}`, []string{"title"}},

		// Absent vacancies default to 1; an explicit 0 does not.
		{"zero vacancies", `{"title":"x","vacancies":0}`, []string{"vacancies"}},
		{"negative vacancies", `{"title":"x","vacancies":-2}`, []string{"vacancies"}},

		{"minimum above maximum", `{"title":"x","salary_min":90000,"salary_max":80000}`, []string{"salary_min"}},
		{"negative salary", `{"title":"x","salary_min":-1}`, []string{"salary_min"}},
		{"three decimal places", `{"title":"x","salary_max":100.005}`, []string{"salary_max"}},
		{"an exponent", `{"title":"x","salary_max":1e5}`, []string{"salary_max"}},
		{"more digits than the column holds", `{"title":"x","salary_max":12345678901}`, []string{"salary_max"}},

		{"several at once", `{"title":"","vacancies":0,"salary_min":-1}`, []string{"title", "vacancies", "salary_min"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newPostFixture(t)
			rec := f.send(http.MethodPost, "/v1/job-posts", f.headers[domain.RoleAdmin], tt.body)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
			}
			got := decode(t, rec.Body.Bytes())
			if len(got.Details) != len(tt.wantFields) {
				t.Errorf("details = %v, want exactly %v", got.Details, tt.wantFields)
			}
			for _, field := range tt.wantFields {
				if got.detail(field) == "" {
					t.Errorf("no problem reported for %q; details = %v", field, got.Details)
				}
			}
			if n := f.posts.callCount(); n != 0 {
				t.Errorf("store called %d times for invalid input, want 0", n)
			}
		})
	}
}

func TestJobPostMalformedBody(t *testing.T) {
	t.Parallel()

	bodies := map[string]string{
		"not JSON":                  `{"title":`,
		"empty body":                ``,
		"title is a number":         `{"title":7}`,
		"vacancies is a string":     `{"title":"x","vacancies":"three"}`,
		"vacancies is a fraction":   `{"title":"x","vacancies":1.5}`,
		"salary is not a number":    `{"title":"x","salary_min":"lots"}`,
		"closes_at is not RFC 3339": `{"title":"x","closes_at":"31/12/2026"}`,
	}

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newPostFixture(t)

			for _, req := range []struct{ method, path string }{
				{http.MethodPost, "/v1/job-posts"},
				{http.MethodPatch, "/v1/job-posts/" + draftPostID},
			} {
				rec := f.send(req.method, req.path, f.headers[domain.RoleAdmin], body)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("%s: status = %d, want 400; body = %s", req.method, rec.Code, rec.Body)
					continue
				}
				if got := decode(t, rec.Body.Bytes()); got.Code != codeMalformedRequest {
					t.Errorf("%s: code = %q, want %q", req.method, got.Code, codeMalformedRequest)
				}
			}
			if n := f.posts.callCount(); n != 0 {
				t.Errorf("store called %d times, want 0", n)
			}
		})
	}
}

func TestGetJobPost(t *testing.T) {
	t.Parallel()

	f := newPostFixture(t)

	tests := []struct {
		role       string
		id         string
		wantStatus int
	}{
		{domain.RoleAdmin, openPostID, http.StatusOK},
		{domain.RoleAdmin, draftPostID, http.StatusOK},
		{domain.RoleAdmin, missingPostID, http.StatusNotFound},

		{domain.RoleInterviewer, openPostID, http.StatusOK},
		{domain.RoleInterviewer, draftPostID, http.StatusNotFound},

		{domain.RoleCandidate, openPostID, http.StatusOK},
		{domain.RoleCandidate, draftPostID, http.StatusNotFound},
		{domain.RoleCandidate, missingPostID, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.role+" → "+tt.id[len(tt.id)-4:], func(t *testing.T) {
			t.Parallel()

			rec := f.get("/v1/job-posts/"+tt.id, f.headers[tt.role])
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusOK {
				if got := decodePost(t, rec); got.ID != tt.id {
					t.Errorf("returned post %q, want %q", got.ID, tt.id)
				}
			}
		})
	}

	t.Run("questions come back in order, and choices is always an array", func(t *testing.T) {
		t.Parallel()

		got := decodePost(t, f.get("/v1/job-posts/"+openPostID, f.headers[domain.RoleCandidate]))

		if len(got.Questions) != 2 || got.Questions[0].Position != 1 || got.Questions[1].Position != 2 {
			t.Fatalf("questions = %+v, want two in position order", got.Questions)
		}
		if got.Questions[0].Choices == nil || len(got.Questions[0].Choices) != 0 {
			t.Errorf("choices of a text question = %v, want []", got.Questions[0].Choices)
		}
		if len(got.Questions[1].Choices) != 2 {
			t.Errorf("choices of a choice question = %v, want two", got.Questions[1].Choices)
		}
	})

	t.Run("a hidden draft is indistinguishable from a missing post", func(t *testing.T) {
		t.Parallel()

		hidden := f.get("/v1/job-posts/"+draftPostID, f.headers[domain.RoleCandidate])
		absent := f.get("/v1/job-posts/"+missingPostID, f.headers[domain.RoleCandidate])

		if hidden.Code != absent.Code || hidden.Body.String() != absent.Body.String() {
			t.Errorf("hidden: %d %s\nabsent: %d %s", hidden.Code, hidden.Body, absent.Code, absent.Body)
		}
	})
}

func TestJobPostIDMustBeAUUID(t *testing.T) {
	t.Parallel()

	malformed := []string{
		"1",
		"not-a-uuid",
		"cccccccc-0000-4000-8000-00000000000",   // one digit short
		"cccccccc-0000-4000-8000-0000000000011", // one digit long
		"cccccccc00004000800000000000000001",    // no hyphens
		"cccccccc-0000-4000-8000_000000000001",  // wrong separator
		"gccccccc-0000-4000-8000-000000000001",  // g is not a hex digit
		"{cccccccc-0000-4000-8000-000000000001", // PostgreSQL would accept braces around it
		"cccccccc-0000-4000-8000-000000000001'", // a stray quote
	}

	for _, id := range malformed {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			f := newPostFixture(t)

			for _, req := range []struct{ method, body string }{
				{http.MethodGet, ""},
				{http.MethodPatch, `{"title":"x"}`},
			} {
				rec := f.send(req.method, "/v1/job-posts/"+id, f.headers[domain.RoleAdmin], req.body)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("%s: status = %d, want 400; body = %s", req.method, rec.Code, rec.Body)
					continue
				}
				got := decode(t, rec.Body.Bytes())
				if got.Code != codeInvalidID {
					t.Errorf("%s: code = %q, want %q", req.method, got.Code, codeInvalidID)
				}
				if strings.Contains(rec.Body.String(), id) {
					t.Errorf("%s: the response echoes the malformed ID: %s", req.method, rec.Body)
				}
			}

			// The point of the check: the store, and so the database, is
			// never asked.
			if n := f.posts.callCount(); n != 0 {
				t.Errorf("store called %d times for a malformed ID, want 0", n)
			}
		})
	}

	t.Run("upper case is accepted", func(t *testing.T) {
		t.Parallel()

		if !isUUID(strings.ToUpper(openPostID)) || !isUUID(openPostID) {
			t.Errorf("isUUID rejected a well-formed UUID")
		}
	})
}

// The heart of PATCH: a key that is absent leaves the stored value alone, and
// a key that is present is written even when its value is empty or zero.
func TestUpdateJobPostAbsentVersusEmpty(t *testing.T) {
	t.Parallel()

	t.Run("an absent field is nil in the patch and unchanged in the store", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"title":"Senior Backend Engineer"}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}

		patch := f.posts.lastPatch
		if patch.Title == nil || *patch.Title != "Senior Backend Engineer" {
			t.Errorf("patch.Title = %v, want the new title", patch.Title)
		}
		if patch.Description != nil || patch.EmploymentType != nil || patch.Location != nil ||
			patch.Vacancies != nil || patch.SalaryMin != nil || patch.SalaryMax != nil || patch.ClosesAt != nil {
			t.Errorf("fields absent from the body are set in the patch: %+v", patch)
		}

		got := f.stored(draftPostID)
		if got.Title != "Senior Backend Engineer" {
			t.Errorf("stored title = %q", got.Title)
		}
		if *got.Description != "Builds the API." || *got.Location != "Dhaka" || got.Vacancies != 2 ||
			*got.SalaryMin != 5000000 || *got.SalaryMax != 9000000 {
			t.Errorf("fields absent from the body were changed: %+v", got)
		}
	})

	t.Run("a field set to empty is non-nil in the patch and emptied in the store", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"description":"","salary_min":0}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}

		patch := f.posts.lastPatch
		if patch.Description == nil || *patch.Description != "" {
			t.Errorf("patch.Description = %v, want a pointer to the empty string", patch.Description)
		}
		if patch.SalaryMin == nil || *patch.SalaryMin != 0 {
			t.Errorf("patch.SalaryMin = %v, want a pointer to 0", patch.SalaryMin)
		}

		got := f.stored(draftPostID)
		if got.Description == nil || *got.Description != "" {
			t.Errorf("stored description = %v, want the empty string", got.Description)
		}
		if *got.Location != "Dhaka" || got.Title != "Backend Engineer" {
			t.Errorf("fields absent from the body were changed: %+v", got)
		}

		// And the client sees "" and 0.00, not null.
		keys := keysOf(t, rec.Body.Bytes())
		if string(keys["description"]) != `""` || string(keys["salary_min"]) != "0.00" {
			t.Errorf("description = %s, salary_min = %s; want \"\" and 0.00", keys["description"], keys["salary_min"])
		}
	})

	t.Run("an explicit zero is rejected where an absent field is not", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)

		absent := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"location":"Remote"}`)
		if absent.Code != http.StatusOK {
			t.Errorf("vacancies absent: status = %d, want 200; body = %s", absent.Code, absent.Body)
		}

		zero := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"vacancies":0}`)
		if zero.Code != http.StatusUnprocessableEntity {
			t.Fatalf("vacancies 0: status = %d, want 422; body = %s", zero.Code, zero.Body)
		}
		if got := decode(t, zero.Body.Bytes()); got.detail("vacancies") == "" {
			t.Errorf("details = %v, want a problem with vacancies", got.Details)
		}

		emptyTitle := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"title":"  "}`)
		if emptyTitle.Code != http.StatusUnprocessableEntity {
			t.Errorf("title of spaces: status = %d, want 422; body = %s", emptyTitle.Code, emptyTitle.Body)
		}

		if got := f.stored(draftPostID); got.Vacancies != 2 || got.Title != "Backend Engineer" {
			t.Errorf("a rejected patch was written: %+v", got)
		}
	})

	t.Run("null is treated as absent", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"description":null,"salary_max":null}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if !f.posts.lastPatch.IsEmpty() {
			t.Errorf("patch = %+v, want it empty", f.posts.lastPatch)
		}
		if got := f.stored(draftPostID); *got.Description != "Builds the API." || *got.SalaryMax != 9000000 {
			t.Errorf("null changed the stored post: %+v", got)
		}
	})

	t.Run("an empty object changes nothing", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if got := decodePost(t, rec); got.Title != "Backend Engineer" || got.Vacancies != 2 {
			t.Errorf("post = %+v, want it unchanged", got)
		}
	})
}

func TestUpdateJobPost(t *testing.T) {
	t.Parallel()

	t.Run("the salary rule is checked against the stored value", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)

		// The stored maximum is 90000, which the body does not mention.
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"salary_min":95000}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		if got := decode(t, rec.Body.Bytes()); got.detail("salary_min") == "" {
			t.Errorf("details = %v, want a problem with salary_min", got.Details)
		}

		// Raising both together is fine.
		rec = f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"salary_min":95000,"salary_max":"110000.00"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if got := f.stored(draftPostID); *got.SalaryMin != 9500000 || *got.SalaryMax != 11000000 {
			t.Errorf("stored salaries = %d, %d", *got.SalaryMin, *got.SalaryMax)
		}
	})

	t.Run("an unparseable amount never reaches the store", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"salary_min":-5}`)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		if n := f.posts.callCount(); n != 0 {
			t.Errorf("store called %d times, want 0", n)
		}
	})

	t.Run("a status in the body does not publish the post", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID, f.headers[domain.RoleAdmin], `{"status":"open"}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if got := f.stored(draftPostID); got.Status != domain.JobStatusDraft {
			t.Errorf("status = %q, want draft", got.Status)
		}
	})

	t.Run("a post that does not exist is a 404", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+missingPostID, f.headers[domain.RoleAdmin], `{"title":"x"}`)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404; body = %s", rec.Code, rec.Body)
		}
	})
}

func TestJobPostWritesAreAdminOnly(t *testing.T) {
	t.Parallel()

	requests := []struct{ method, path, body string }{
		{http.MethodPost, "/v1/job-posts", `{"title":"x"}`},
		{http.MethodPatch, "/v1/job-posts/" + draftPostID, `{"title":"x"}`},

		// Refused before the ID or the body is looked at, so neither tells a
		// non-admin anything.
		{http.MethodPatch, "/v1/job-posts/not-a-uuid", `{"title":"x"}`},
		{http.MethodPost, "/v1/job-posts", `not json`},
	}

	for _, req := range requests {
		for role, wantStatus := range map[string]int{
			domain.RoleInterviewer: http.StatusForbidden,
			domain.RoleCandidate:   http.StatusForbidden,
			"":                     http.StatusUnauthorized,
		} {
			t.Run(req.method+" "+req.path+" as "+role, func(t *testing.T) {
				t.Parallel()

				f := newPostFixture(t)
				rec := f.send(req.method, req.path, f.headers[role], req.body)

				if rec.Code != wantStatus {
					t.Errorf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body)
				}
				if n := f.posts.callCount(); n != 0 {
					t.Errorf("store called %d times, want 0", n)
				}
			})
		}
	}
}

// Not using t.Parallel: captureLog swaps the global logger.
func TestJobPostStoreFailures(t *testing.T) {
	logged := captureLog(t)

	f := newPostFixture(t)
	f.posts.failWith = errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")

	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/job-posts", `{"title":"x"}`},
		{http.MethodGet, "/v1/job-posts/" + draftPostID, ``},
		{http.MethodPatch, "/v1/job-posts/" + draftPostID, `{"title":"x"}`},
		{http.MethodPost, "/v1/job-posts/" + draftPostID + "/open", ``},
	} {
		rec := f.send(req.method, req.path, f.headers[domain.RoleAdmin], req.body)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: status = %d, want 500", req.method, req.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "10.0.0.5") {
			t.Errorf("%s %s: the cause leaked to the client: %s", req.method, req.path, rec.Body)
		}
	}
	if n := strings.Count(logged.String(), "10.0.0.5"); n != 4 {
		t.Errorf("cause logged %d times, want 4; log = %q", n, logged)
	}
}

// lifecycleFixture adds, to postFixture's draft (no questions) and open post,
// a draft that is ready to open and a closed post.
func newLifecycleFixture(t *testing.T) *postFixture {
	t.Helper()

	f := newPostFixture(t)

	ready := f.draft
	ready.ID = readyPostID
	ready.Questions = []domain.Question{{ID: "q-r", JobPostID: readyPostID, Prompt: "Why?", Kind: "text", Position: 1}}
	f.posts.byID[readyPostID] = ready
	f.posts.readiness[readyPostID] = domain.OpenReadiness{Criteria: 2, WeightedCriteria: 2}

	closed := f.open
	closed.ID = closedPostID
	closed.Status = domain.JobStatusClosed
	f.posts.byID[closedPostID] = closed

	return f
}

const (
	readyPostID  = "cccccccc-0000-4000-8000-000000000003"
	closedPostID = "cccccccc-0000-4000-8000-000000000004"
)

// allowedNext reads details.allowed_next_statuses from an error response.
func allowedNext(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()

	var body struct {
		Error struct {
			Details struct {
				Allowed *[]string `json:"allowed_next_statuses"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v\n%s", err, rec.Body)
	}
	if body.Error.Details.Allowed == nil {
		t.Fatalf("details.allowed_next_statuses is missing or null: %s", rec.Body)
	}
	return *body.Error.Details.Allowed
}

func TestJobPostTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		action     string // "open" or "close"
		id         string
		wantStatus int
		wantCode   string   // for a refusal
		wantNext   []string // allowed_next_statuses, for an invalid transition
		wantState  string   // the post's status afterwards
	}{
		{"open a ready draft", "open", readyPostID, http.StatusOK, "", nil, domain.JobStatusOpen},
		{"close an open post", "close", openPostID, http.StatusOK, "", nil, domain.JobStatusClosed},

		{"close a draft", "close", readyPostID, http.StatusConflict, "invalid_transition", []string{"open"}, domain.JobStatusDraft},
		{"open an open post", "open", openPostID, http.StatusConflict, "invalid_transition", []string{"closed"}, domain.JobStatusOpen},
		{"reopen a closed post", "open", closedPostID, http.StatusConflict, "invalid_transition", []string{}, domain.JobStatusClosed},
		{"close a closed post", "close", closedPostID, http.StatusConflict, "invalid_transition", []string{}, domain.JobStatusClosed},

		{"a post that does not exist", "open", missingPostID, http.StatusNotFound, "not_found", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newLifecycleFixture(t)
			rec := f.send(http.MethodPost, "/v1/job-posts/"+tt.id+"/"+tt.action, f.headers[domain.RoleAdmin], "")

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body)
			}

			if tt.wantStatus == http.StatusOK {
				if got := decodePost(t, rec); got.Status != tt.wantState {
					t.Errorf("response status = %q, want %q", got.Status, tt.wantState)
				}
				if f.posts.lastActor != f.adminID {
					t.Errorf("actor = %q, want the admin %q", f.posts.lastActor, f.adminID)
				}
			} else {
				if got := decode(t, rec.Body.Bytes()); got.Code != tt.wantCode {
					t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
				}
				if tt.wantNext != nil {
					if got := allowedNext(t, rec); !slices.Equal(got, tt.wantNext) {
						t.Errorf("allowed_next_statuses = %#v, want %#v", got, tt.wantNext)
					}
				}
			}

			if tt.wantState != "" {
				if got := f.stored(tt.id).Status; got != tt.wantState {
					t.Errorf("stored status = %q, want %q", got, tt.wantState)
				}
			}
		})
	}
}

func TestOpenJobPostRefusesAnUnreadyPost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		questions   int
		readiness   domain.OpenReadiness
		wantMissing []string
	}{
		{"neither questions nor rubric", 0, domain.OpenReadiness{}, []string{"questions", "rubric"}},
		{"no questions", 0, domain.OpenReadiness{Criteria: 1, WeightedCriteria: 1}, []string{"questions"}},
		{"no rubric", 1, domain.OpenReadiness{}, []string{"rubric"}},
		{"a rubric weighted zero", 1, domain.OpenReadiness{Criteria: 2}, []string{"rubric"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newLifecycleFixture(t)
			post := f.stored(readyPostID)
			post.Questions = post.Questions[:tt.questions]
			f.posts.byID[readyPostID] = post
			f.posts.readiness[readyPostID] = tt.readiness

			rec := f.send(http.MethodPost, "/v1/job-posts/"+readyPostID+"/open", f.headers[domain.RoleAdmin], "")

			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body)
			}
			got := decode(t, rec.Body.Bytes())
			if got.Code != "not_ready" {
				t.Errorf("code = %q, want not_ready", got.Code)
			}
			if len(got.Details) != len(tt.wantMissing) {
				t.Errorf("details = %v, want exactly %v", got.Details, tt.wantMissing)
			}
			for _, key := range tt.wantMissing {
				if got.detail(key) == "" {
					t.Errorf("no message for %q; details = %v", key, got.Details)
				}
			}
			if f.stored(readyPostID).Status != domain.JobStatusDraft {
				t.Errorf("an unready post was opened")
			}
		})
	}
}

func TestJobPostTransitionsAreAdminOnly(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/v1/job-posts/" + readyPostID + "/open",
		"/v1/job-posts/" + openPostID + "/close",
		"/v1/job-posts/not-a-uuid/open",
	} {
		for role, wantStatus := range map[string]int{
			domain.RoleInterviewer: http.StatusForbidden,
			domain.RoleCandidate:   http.StatusForbidden,
			"":                     http.StatusUnauthorized,
		} {
			t.Run(path+" as "+role, func(t *testing.T) {
				t.Parallel()

				f := newLifecycleFixture(t)
				rec := f.send(http.MethodPost, path, f.headers[role], "")

				if rec.Code != wantStatus {
					t.Errorf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body)
				}
				if n := f.posts.callCount(); n != 0 {
					t.Errorf("store called %d times, want 0", n)
				}
			})
		}
	}

	t.Run("a malformed ID is a 400 for an admin, before the store", func(t *testing.T) {
		t.Parallel()

		f := newLifecycleFixture(t)
		for _, action := range []string{"open", "close"} {
			rec := f.send(http.MethodPost, "/v1/job-posts/not-a-uuid/"+action, f.headers[domain.RoleAdmin], "")
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", action, rec.Code)
			}
		}
		if n := f.posts.callCount(); n != 0 {
			t.Errorf("store called %d times, want 0", n)
		}
	})
}
