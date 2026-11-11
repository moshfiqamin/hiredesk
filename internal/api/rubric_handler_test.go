package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// fakeRubrics is an in-memory RubricStore. A post exists if it has an entry
// in rubrics, even one with no criteria. It does not run the form checks,
// which need the database and are tested against PostgreSQL.
type fakeRubrics struct {
	mu sync.Mutex

	rubrics map[string]*domain.Rubric

	// replaceCalls counts ReplaceRubric calls; lastActor and lastCriteria are
	// what the most recent one received.
	replaceCalls int
	lastActor    string
	lastCriteria []domain.Criterion

	// replaceErr, when set, is what ReplaceRubric returns; toRescore is the
	// ApplicationsToRescore it reports otherwise.
	replaceErr error
	toRescore  int
}

func newFakeRubrics() *fakeRubrics {
	return &fakeRubrics{rubrics: make(map[string]*domain.Rubric)}
}

func (f *fakeRubrics) GetRubricByJobPost(_ context.Context, jobPostID string) (*domain.Rubric, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	r, ok := f.rubrics[jobPostID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return r, nil
}

func (f *fakeRubrics) ReplaceRubric(_ context.Context, jobPostID, actorID string, criteria []domain.Criterion) (*domain.RubricReplacement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.replaceCalls++
	f.lastActor = actorID
	f.lastCriteria = criteria
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	old, ok := f.rubrics[jobPostID]
	if !ok {
		return nil, domain.ErrNotFound
	}

	next := &domain.Rubric{JobPostID: jobPostID, Version: old.Version + 1}
	for i, c := range criteria {
		c.ID = "crit-" + strconv.Itoa(next.Version) + "-" + strconv.Itoa(i)
		c.RubricVersion = next.Version
		next.Criteria = append(next.Criteria, c)
	}
	f.rubrics[jobPostID] = next
	return &domain.RubricReplacement{Rubric: *next, PreviousVersion: old.Version, ApplicationsToRescore: f.toRescore}, nil
}

// rubricFixture is a test server with an admin, a candidate, and two posts:
// one with a rubric at version 2, one with none.
type rubricFixture struct {
	*testServer

	admin, candidate string // Authorization headers
	adminID          string
}

const (
	scoredPostID        = "bbbbbbbb-0000-4000-8000-000000000001"
	unscoredPostID      = "bbbbbbbb-0000-4000-8000-000000000002"
	missingRubricPostID = "bbbbbbbb-0000-4000-8000-00000000ffff"
	textQuestionID      = "cccccccc-0000-4000-8000-000000000001"
)

func newRubricFixture(t *testing.T) *rubricFixture {
	t.Helper()

	f := &rubricFixture{testServer: newTestServer(t)}
	admin := mustCreateUser(t, f.users, "admin@example.com", domain.RoleAdmin)
	candidate := mustCreateUser(t, f.users, "candidate@example.com", domain.RoleCandidate)
	f.adminID = admin.ID
	f.admin = bearerFor(t, f.tokens, admin.ID, domain.RoleAdmin)
	f.candidate = bearerFor(t, f.tokens, candidate.ID, domain.RoleCandidate)

	f.rubrics.rubrics[scoredPostID] = &domain.Rubric{
		JobPostID: scoredPostID, Version: 2,
		Criteria: []domain.Criterion{{
			ID: "crit-2-0", Label: "Go knowledge", Kind: domain.CriterionKindKeywordMatch, Weight: 100,
			Config: json.RawMessage(`{"keywords": ["go"]}`), QuestionID: textQuestionID, RubricVersion: 2,
		}},
	}
	f.rubrics.rubrics[unscoredPostID] = &domain.Rubric{JobPostID: unscoredPostID, Criteria: []domain.Criterion{}}
	return f
}

func rubricPath(postID string) string {
	return "/v1/job-posts/" + postID + "/rubric"
}

func TestGetRubric(t *testing.T) {
	t.Parallel()

	f := newRubricFixture(t)

	t.Run("the current version, in order", func(t *testing.T) {
		t.Parallel()

		rec := f.get(rubricPath(scoredPostID), f.admin)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		var got rubricResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Version == nil || *got.Version != 2 || len(got.Criteria) != 1 {
			t.Fatalf("got %+v, want version 2 with one criterion", got)
		}
		c := got.Criteria[0]
		if c.QuestionID == nil || *c.QuestionID != textQuestionID || string(c.Config) != `{"keywords":["go"]}` {
			t.Errorf("criterion = %+v, config %s", c, c.Config)
		}
	})

	t.Run("a post with no rubric has a null version and an empty list", func(t *testing.T) {
		t.Parallel()

		rec := f.get(rubricPath(unscoredPostID), f.admin)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		keys := keysOf(t, rec.Body.Bytes())
		if string(keys["version"]) != "null" || string(keys["criteria"]) != "[]" {
			t.Errorf("version = %s, criteria = %s; want null and []", keys["version"], keys["criteria"])
		}
	})

	for _, tt := range []struct {
		name, path, header string
		want               int
	}{
		{"missing post", rubricPath(missingRubricPostID), f.admin, http.StatusNotFound},
		{"malformed ID", rubricPath("not-a-uuid"), f.admin, http.StatusBadRequest},
		{"candidate", rubricPath(scoredPostID), f.candidate, http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if rec := f.get(tt.path, tt.header); rec.Code != tt.want {
				t.Errorf("status = %d, want %d; body = %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestReplaceRubric(t *testing.T) {
	t.Parallel()

	// validBody is one keyword criterion worth the whole 100. The question ID
	// is uppercase and the label padded, to check both are normalised.
	validBody := `{"criteria":[{"label":"  Go knowledge ","kind":"keyword_match","weight":100,
		"question_id":"` + strings.ToUpper(textQuestionID) + `","config":{"keywords":["go","gin"]}}]}`

	t.Run("stores the next version and reports what is left to rescore", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		f.rubrics.toRescore = 3

		rec := f.send(http.MethodPut, rubricPath(scoredPostID), f.admin, validBody)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		var got replacedRubricResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Version == nil || *got.Version != 3 || got.PreviousVersion == nil || *got.PreviousVersion != 2 {
			t.Errorf("version %v from %v, want 3 from 2", got.Version, got.PreviousVersion)
		}
		if got.ApplicationsToRescore != 3 {
			t.Errorf("applications_to_rescore = %d, want 3", got.ApplicationsToRescore)
		}

		if f.rubrics.lastActor != f.adminID {
			t.Errorf("actor = %q, want the admin %q", f.rubrics.lastActor, f.adminID)
		}
		c := f.rubrics.lastCriteria[0]
		if c.Label != "Go knowledge" || c.QuestionID != textQuestionID {
			t.Errorf("stored label %q, question %q; want them trimmed and lowercased", c.Label, c.QuestionID)
		}
	})

	t.Run("a first rubric has a null previous version", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		rec := f.send(http.MethodPut, rubricPath(unscoredPostID), f.admin, validBody)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if keys := keysOf(t, rec.Body.Bytes()); string(keys["previous_version"]) != "null" || string(keys["version"]) != "1" {
			t.Errorf("previous_version = %s, version = %s; want null and 1", keys["previous_version"], keys["version"])
		}
	})

	t.Run("every problem in one 422, and nothing stored", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		body := `{"criteria":[
			{"label":"","kind":"keyword_match","weight":40,"question_id":"` + textQuestionID + `","config":{"keywords":["go"]}},
			{"label":"Years","kind":"experience_years","weight":30,"question_id":"q-7","config":{"target":2}},
			{"label":"Depth","kind":"answer_depth","weight":20,"question_id":"` + textQuestionID + `","config":[1,2]},
			{"label":"Typo","kind":"keyword_match","weight":5,"question_id":"` + textQuestionID + `","config":{"keyword":["go"]}},
			{"label":"Mystery","kind":"vibes","weight":1.234}
		]}`

		rec := f.send(http.MethodPut, rubricPath(scoredPostID), f.admin, body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		got := decode(t, rec.Body.Bytes()).stringDetails()
		want := map[string]string{
			"criteria[0].label":       "is required",
			"criteria[1].question_id": "must be a UUID",
			"criteria[2].config":      "must be a JSON object",
			"criteria[3].config":      `unknown field "keyword"`,
			"criteria[4].kind":        "must be one of answer_depth, boolean_all, experience_years, keyword_match",
			"criteria[4].weight":      "must have at most two decimal places",
			"criteria":                "weights must sum to 100; they sum to 96.23",
		}
		for field, problem := range want {
			if got[field] != problem {
				t.Errorf("%s = %q, want %q", field, got[field], problem)
			}
		}
		if len(got) != len(want) {
			t.Errorf("got %d problems, want %d: %v", len(got), len(want), got)
		}
		if f.rubrics.replaceCalls != 0 {
			t.Errorf("store called %d times for an invalid rubric", f.rubrics.replaceCalls)
		}
	})

	t.Run("an absent or null config is an empty object", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		body := `{"criteria":[{"label":"Remote","kind":"boolean_all","weight":100,"config":null}]}`

		rec := f.send(http.MethodPut, rubricPath(scoredPostID), f.admin, body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		// The engine judged {}, not null: it complains about the missing
		// field, not about the type.
		if got := decode(t, rec.Body.Bytes()).detail("criteria[0].config"); got != "question_ids must list at least one question" {
			t.Errorf("config problem = %q", got)
		}
	})

	t.Run("an empty list", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		rec := f.send(http.MethodPut, rubricPath(scoredPostID), f.admin, `{"criteria":[]}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		if got := decode(t, rec.Body.Bytes()).detail("criteria"); got != "must contain at least one criterion" {
			t.Errorf("criteria problem = %q", got)
		}
	})

	t.Run("too many criteria is refused before any is checked", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		items := make([]string, domain.MaxRubricCriteria+1)
		for i := range items {
			items[i] = `{"label":"","kind":"nope"}`
		}
		rec := f.send(http.MethodPut, rubricPath(scoredPostID), f.admin, `{"criteria":[`+strings.Join(items, ",")+`]}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		if got := decode(t, rec.Body.Bytes()).stringDetails(); len(got) != 1 || got["criteria"] == "" {
			t.Errorf("details = %v, want only the length problem", got)
		}
	})

	t.Run("the store's verdicts", func(t *testing.T) {
		t.Parallel()

		var formProblem domain.ValidationError
		formProblem.Add("criteria[0].question_id", "is not a question on this job post")

		for _, tt := range []struct {
			name   string
			postID string
			err    error
			want   int
		}{
			{"a question from another post", scoredPostID, formProblem.Err(), http.StatusUnprocessableEntity},
			{"no such post", missingRubricPostID, nil, http.StatusNotFound},
			{"an unexpected failure", scoredPostID, errors.New("connection reset"), http.StatusInternalServerError},
		} {
			t.Run(tt.name, func(t *testing.T) {
				f := newRubricFixture(t)
				f.rubrics.replaceErr = tt.err

				rec := f.send(http.MethodPut, rubricPath(tt.postID), f.admin, validBody)
				if rec.Code != tt.want {
					t.Errorf("status = %d, want %d; body = %s", rec.Code, tt.want, rec.Body)
				}
			})
		}
	})

	t.Run("a candidate is refused before the body is read", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		rec := f.send(http.MethodPut, rubricPath(scoredPostID), f.candidate, validBody)
		if rec.Code != http.StatusForbidden || f.rubrics.replaceCalls != 0 {
			t.Errorf("status = %d, store calls = %d; want 403 and none", rec.Code, f.rubrics.replaceCalls)
		}
	})

	t.Run("a body that is not JSON", func(t *testing.T) {
		t.Parallel()

		f := newRubricFixture(t)
		if rec := f.send(http.MethodPut, rubricPath(scoredPostID), f.admin, `{"criteria":`); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}
