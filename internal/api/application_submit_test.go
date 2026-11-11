package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

const (
	submitPostID = "dddddddd-0000-4000-8000-000000000001"
	submitQ1     = "eeeeeeee-0000-4000-8000-000000000001"
	submitQ2     = "eeeeeeee-0000-4000-8000-000000000002"
)

func submitPath(postID string) string {
	return "/v1/job-posts/" + postID + "/applications"
}

// storedSubmission is what the fake store hands back for a successful
// submission by candidateID.
func storedSubmission(candidateID string) *domain.Application {
	at := time.Date(2026, time.November, 12, 10, 0, 0, 0, time.UTC)
	return &domain.Application{
		ID: "aaaaaaaa-0000-4000-8000-0000000000aa", JobPostID: submitPostID, CandidateID: candidateID,
		Status: domain.StatusSubmitted, CVURL: "https://cv.example/me.pdf", SubmittedAt: at, UpdatedAt: at,
		Answers: []domain.Answer{
			{ID: "ans-1", QuestionID: submitQ1, Text: "4", CreatedAt: at},
			{ID: "ans-2", QuestionID: submitQ2, Text: "true", CreatedAt: at},
		},
	}
}

func TestSubmitApplication(t *testing.T) {
	t.Parallel()

	t.Run("a candidate's submission reaches the store and comes back as 201", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		candidate := f.users["candidate"]
		f.apps.submitResult = storedSubmission(candidate.ID)

		// A number and a boolean sent as JSON literals, and a question ID in
		// capitals.
		body := `{"cv_url":"https://cv.example/me.pdf","answers":[
			{"question_id":"EEEEEEEE-0000-4000-8000-000000000001","answer":4.50},
			{"question_id":"` + submitQ2 + `","answer":true},
			{"question_id":"` + submitQ2 + `","answer":"text"}]}`
		rec := f.send(http.MethodPost, submitPath(submitPostID), f.headers["candidate"], body)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body)
		}
		if got := rec.Header().Get("Location"); got != "/v1/applications/aaaaaaaa-0000-4000-8000-0000000000aa" {
			t.Errorf("Location = %q", got)
		}

		f.apps.mu.Lock()
		got := f.apps.lastSubmitted
		f.apps.mu.Unlock()
		want := &domain.NewApplication{
			JobPostID:   submitPostID,
			CandidateID: candidate.ID,
			CVURL:       "https://cv.example/me.pdf",
			// Passed through unvalidated, duplicates included: the rules are
			// the store's to apply.
			Answers: []domain.NewAnswer{
				{QuestionID: submitQ1, Answer: "4.50"},
				{QuestionID: submitQ2, Answer: "true"},
				{QuestionID: submitQ2, Answer: "text"},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("store got  %+v\n     want %+v", got, want)
		}

		var resp submittedApplicationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("body: %v", err)
		}
		if resp.ID != "aaaaaaaa-0000-4000-8000-0000000000aa" || resp.Status != domain.StatusSubmitted {
			t.Errorf("application = %+v", resp.applicationResponse)
		}
		if len(resp.Answers) != 2 || resp.Answers[0].Answer != "4" || resp.Answers[1].QuestionID != submitQ2 {
			t.Errorf("answers = %+v", resp.Answers)
		}

		// One flat object: the embedded struct's fields sit beside answers.
		keys := keysOf(t, rec.Body.Bytes())
		for _, k := range []string{"id", "job_post_id", "candidate_id", "status", "cv_url", "answers"} {
			if _, ok := keys[k]; !ok {
				t.Errorf("response has no %q key: %s", k, rec.Body)
			}
		}
	})

	t.Run("an empty answers list is sent as empty, never nil", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.submitResult = storedSubmission(f.users["candidate"].ID)
		rec := f.send(http.MethodPost, submitPath(submitPostID), f.headers["candidate"], `{"cv_url":"https://x.example/cv"}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		f.apps.mu.Lock()
		defer f.apps.mu.Unlock()
		if f.apps.lastSubmitted.Answers == nil || len(f.apps.lastSubmitted.Answers) != 0 {
			t.Errorf("answers = %#v, want an empty slice", f.apps.lastSubmitted.Answers)
		}
	})

	// Refused before the store is asked.
	refusals := []struct {
		name     string
		caller   string
		path     string
		body     string
		status   int
		wantCode string
	}{
		{"an admin", "admin", submitPath(submitPostID), `{}`, http.StatusForbidden, "forbidden"},
		{"an interviewer", "assigned interviewer", submitPath(submitPostID), `{}`, http.StatusForbidden, "forbidden"},
		{"nobody signed in", "", submitPath(submitPostID), `{}`, http.StatusUnauthorized, "unauthenticated"},
		{"a post ID that is not a UUID", "candidate", submitPath("42"), `{}`, http.StatusBadRequest, "invalid_id"},
		{"a body that is not JSON", "candidate", submitPath(submitPostID), `cv=x`, http.StatusBadRequest, "malformed_request"},
		{"an answer that is an object", "candidate", submitPath(submitPostID),
			`{"cv_url":"https://x.example","answers":[{"question_id":"` + submitQ1 + `","answer":{"a":1}}]}`,
			http.StatusBadRequest, "malformed_request"},
		{"an answer that is a list", "candidate", submitPath(submitPostID),
			`{"cv_url":"https://x.example","answers":[{"question_id":"` + submitQ1 + `","answer":["a"]}]}`,
			http.StatusBadRequest, "malformed_request"},
		{"an answer that is null", "candidate", submitPath(submitPostID),
			`{"cv_url":"https://x.example","answers":[{"question_id":"` + submitQ1 + `","answer":null}]}`,
			http.StatusBadRequest, "malformed_request"},
	}
	for _, tt := range refusals {
		t.Run("refused: "+tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			rec := f.send(http.MethodPost, tt.path, f.headers[tt.caller], tt.body)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.status, rec.Body)
			}
			if got := decode(t, rec.Body.Bytes()).Code; got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
			f.apps.mu.Lock()
			defer f.apps.mu.Unlock()
			if f.apps.lastSubmitted != nil {
				t.Errorf("store was called with %+v", f.apps.lastSubmitted)
			}
		})
	}

	// What each of the store's failures becomes on the wire.
	failures := []struct {
		name     string
		err      error
		status   int
		wantCode string
	}{
		{"a closed post", &domain.NotAcceptingError{Status: domain.JobStatusClosed},
			http.StatusConflict, "not_accepting_applications"},
		{"a missing or draft post", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"a second application", domain.ErrAlreadyApplied, http.StatusConflict, "already_applied"},
		{"invalid input", &domain.SubmissionError{
			CVURL: "must be an absolute http or https URL",
			Answers: []domain.AnswerProblem{
				{QuestionID: submitQ1, Index: domain.NoIndex, Code: domain.AnswerRequired, Message: "is required"},
				{QuestionID: submitQ2, Index: 0, Code: domain.AnswerTooShort, Message: "must be at least 50 characters", MinLength: 50, Got: 3},
			},
		}, http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, tt := range failures {
		t.Run("store failure: "+tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			f.apps.submitErr = tt.err
			rec := f.send(http.MethodPost, submitPath(submitPostID), f.headers["candidate"], `{"cv_url":"x","answers":[]}`)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.status, rec.Body)
			}
			if got := decode(t, rec.Body.Bytes()).Code; got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}

	t.Run("a 422 carries every problem in its details", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.submitErr = failures[3].err
		rec := f.send(http.MethodPost, submitPath(submitPostID), f.headers["candidate"], `{"cv_url":"x"}`)

		var body struct {
			Error struct {
				Details struct {
					CVURL   string           `json:"cv_url"`
					Answers []map[string]any `json:"answers"`
				} `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		d := body.Error.Details
		if d.CVURL == "" || len(d.Answers) != 2 {
			t.Fatalf("details = %s", rec.Body)
		}
		// JSON numbers decode into any as float64.
		short := d.Answers[1]
		if short["question_id"] != submitQ2 || short["min_length"] != 50.0 || short["got"] != 3.0 || short["index"] != 0.0 {
			t.Errorf("too_short problem = %v", short)
		}
	})
}
