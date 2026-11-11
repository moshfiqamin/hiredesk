package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// fakeReviews is an in-memory ReviewStore. It returns what it was told to
// and records what it was given; the real rules are tested against
// PostgreSQL in test/integration.
type fakeReviews struct {
	mu sync.Mutex

	// assigned is what AssignInterviewer reports for "created"; assignErr,
	// when set, is its error.
	alreadyAssigned bool
	assignErr       error
	lastAssign      *[3]string // {appID, interviewerID, actorID}
	lastUnassign    *[3]string

	reviewErr  error
	lastCreate *createCall
	lastPatch  *domain.ReviewPatch
}

// createCall is one CreateReview call, as the fake received it.
type createCall struct {
	appID, interviewerID string
	mark                 int
	notes                *string
}

var reviewTime = time.Date(2026, time.March, 2, 10, 0, 0, 0, time.UTC)

func (f *fakeReviews) AssignInterviewer(_ context.Context, appID, interviewerID, actorID string) (*domain.Assignment, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastAssign = &[3]string{appID, interviewerID, actorID}
	if f.assignErr != nil {
		return nil, false, f.assignErr
	}
	return &domain.Assignment{ApplicationID: appID, InterviewerID: interviewerID, InterviewerName: "Ivan", AssignedAt: reviewTime},
		!f.alreadyAssigned, nil
}

func (f *fakeReviews) UnassignInterviewer(_ context.Context, appID, interviewerID, actorID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastUnassign = &[3]string{appID, interviewerID, actorID}
	return true, f.assignErr
}

func (f *fakeReviews) ListAssignments(_ context.Context, appID string) ([]domain.Assignment, error) {
	if appID == "aaaaaaaa-0000-4000-8000-00000000ffff" {
		return nil, domain.ErrNotFound
	}
	return []domain.Assignment{{ApplicationID: appID, InterviewerID: "i1", InterviewerName: "Ivan", AssignedAt: reviewTime, Reviewed: true}}, nil
}

func (f *fakeReviews) CreateReview(_ context.Context, appID, interviewerID string, mark int, notes *string) (*domain.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastCreate = &createCall{appID: appID, interviewerID: interviewerID, mark: mark, notes: notes}
	if f.reviewErr != nil {
		return nil, f.reviewErr
	}
	return &domain.Review{ID: "r1", ApplicationID: appID, InterviewerID: interviewerID, Mark: mark, Notes: notes,
		CreatedAt: reviewTime, UpdatedAt: reviewTime}, nil
}

func (f *fakeReviews) UpdateReview(_ context.Context, appID, interviewerID string, patch domain.ReviewPatch) (*domain.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastPatch = &patch
	if f.reviewErr != nil {
		return nil, f.reviewErr
	}
	r := &domain.Review{ID: "r1", ApplicationID: appID, InterviewerID: interviewerID, Mark: 5, CreatedAt: reviewTime, UpdatedAt: reviewTime}
	if patch.Mark != nil {
		r.Mark = *patch.Mark
	}
	return r, nil
}

func TestAssign(t *testing.T) {
	t.Parallel()

	const target = "cccccccc-0000-4000-8000-000000000001"

	t.Run("first call is 201, repeat is 200", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		path := "/v1/applications/" + f.mine.ID + "/assign"
		body := `{"interviewer_id":"` + strings.ToUpper(target) + `"}`

		if rec := f.send(http.MethodPost, path, f.headers["admin"], body); rec.Code != http.StatusCreated {
			t.Fatalf("first: status %d; body %s", rec.Code, rec.Body)
		}
		if want := [3]string{f.mine.ID, target, f.users["admin"].ID}; *f.reviews.lastAssign != want {
			t.Errorf("store got %v, want %v (ID lowercased, actor the admin)", *f.reviews.lastAssign, want)
		}

		f.reviews.alreadyAssigned = true
		if rec := f.send(http.MethodPost, path, f.headers["admin"], body); rec.Code != http.StatusOK {
			t.Errorf("repeat: status %d, want 200", rec.Code)
		}
	})

	t.Run("a non-interviewer from the store is a 422", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		var invalid domain.ValidationError
		invalid.Add("interviewer_id", "must be the ID of a user whose role is interviewer")
		f.reviews.assignErr = fmt.Errorf("assign: %w", invalid.Err())

		rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/assign", f.headers["admin"], `{"interviewer_id":"`+target+`"}`)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "interviewer_id") {
			t.Errorf("status %d; body %s", rec.Code, rec.Body)
		}
	})

	for _, tt := range []struct{ name, body string }{
		{"missing", `{}`},
		{"empty", `{"interviewer_id":""}`},
		{"not a UUID", `{"interviewer_id":"ivan"}`},
	} {
		t.Run("interviewer_id "+tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/assign", f.headers["admin"], tt.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status %d, want 422; body %s", rec.Code, rec.Body)
			}
			if f.reviews.lastAssign != nil {
				t.Error("the store was called")
			}
		})
	}

	t.Run("unassign is 204, and checks both IDs", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		if rec := f.send(http.MethodDelete, "/v1/applications/"+f.mine.ID+"/assign/"+target, f.headers["admin"], ""); rec.Code != http.StatusNoContent {
			t.Errorf("status %d, want 204", rec.Code)
		}
		if rec := f.send(http.MethodDelete, "/v1/applications/"+f.mine.ID+"/assign/ivan", f.headers["admin"], ""); rec.Code != http.StatusBadRequest {
			t.Errorf("malformed interviewer ID: status %d, want 400", rec.Code)
		}
	})

	t.Run("list", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		rec := f.get("/v1/applications/"+f.mine.ID+"/assignments", f.headers["admin"])
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reviewed":true`) {
			t.Errorf("status %d; body %s", rec.Code, rec.Body)
		}
		if rec := f.get("/v1/applications/aaaaaaaa-0000-4000-8000-00000000ffff/assignments", f.headers["admin"]); rec.Code != http.StatusNotFound {
			t.Errorf("missing application: status %d", rec.Code)
		}
	})

	for _, caller := range []string{"assigned interviewer", "candidate"} {
		t.Run(caller+" may not assign", func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			if rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/assign", f.headers[caller], `{"interviewer_id":"`+target+`"}`); rec.Code != http.StatusForbidden {
				t.Errorf("status %d, want 403", rec.Code)
			}
		})
	}
}

func TestCreateReview(t *testing.T) {
	t.Parallel()

	// Every one of these is a 422 on mark, and the store is never called.
	for _, tt := range []struct{ name, body string }{
		{"missing", `{}`},
		{"null", `{"mark":null}`},
		{"zero", `{"mark":0}`},
		{"eleven", `{"mark":11}`},
		{"negative", `{"mark":-3}`},
		{"a word", `{"mark":"eight"}`},
		{"a numeric string", `{"mark":"8"}`},
		{"a fraction", `{"mark":8.5}`},
		{"a float literal", `{"mark":8.0}`},
		{"an exponent", `{"mark":8e0}`},
		{"a boolean", `{"mark":true}`},
		{"an object", `{"mark":{"value":8}}`},
		{"huge", `{"mark":99999999999999999999999}`},
	} {
		t.Run("mark "+tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/reviews", f.headers["assigned interviewer"], tt.body)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"mark"`) {
				t.Errorf("status %d, want 422 naming mark; body %s", rec.Code, rec.Body)
			}
			if f.reviews.lastCreate != nil {
				t.Error("the store was called")
			}
		})
	}

	for _, mark := range []int{1, 10} {
		t.Run(fmt.Sprintf("mark %d is accepted", mark), func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/reviews", f.headers["assigned interviewer"],
				fmt.Sprintf(`{"mark":%d,"notes":"  Clear answers.  "}`, mark))
			if rec.Code != http.StatusCreated {
				t.Fatalf("status %d; body %s", rec.Code, rec.Body)
			}
			got := f.reviews.lastCreate
			if got.mark != mark || got.interviewerID != f.users["assigned interviewer"].ID || got.notes == nil || *got.notes != "Clear answers." {
				t.Errorf("store got %+v", got)
			}
		})
	}

	for _, tt := range []struct{ name, notes string }{
		{"too long", strings.Repeat("a", domain.MaxReviewNotesChars+1)},
		{"a NUL", `bad\u0000notes`},
	} {
		t.Run("notes "+tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			body, _ := json.Marshal(map[string]any{"mark": 7, "notes": json.RawMessage(`"` + tt.notes + `"`)})
			rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/reviews", f.headers["assigned interviewer"], string(body))
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"notes"`) {
				t.Errorf("status %d; body %s", rec.Code, rec.Body)
			}
		})
	}

	t.Run("a second review is a 409", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.reviews.reviewErr = fmt.Errorf("create review: %w", domain.ErrAlreadyReviewed)
		rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/reviews", f.headers["assigned interviewer"], `{"mark":7}`)
		if rec.Code != http.StatusConflict || errorCodeOf(t, rec.Body.Bytes()) != "already_reviewed" {
			t.Errorf("status %d; body %s", rec.Code, rec.Body)
		}
	})

	for _, tt := range []struct {
		caller string
		want   int
	}{
		// Not assigned to it, so may not see it: the same 404 as a missing one.
		{"unassigned interviewer", http.StatusNotFound},
		// Refused by RequireRole before anything is read.
		{"admin", http.StatusForbidden},
		{"candidate", http.StatusForbidden},
	} {
		t.Run(tt.caller, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			if rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/reviews", f.headers[tt.caller], `{"mark":7}`); rec.Code != tt.want {
				t.Errorf("status %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestUpdateMyReview(t *testing.T) {
	t.Parallel()

	path := func(f *appFixture) string { return "/v1/applications/" + f.mine.ID + "/reviews/mine" }

	t.Run("an empty patch is a 422", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		if rec := f.send(http.MethodPatch, path(f), f.headers["assigned interviewer"], `{}`); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status %d, want 422", rec.Code)
		}
	})

	t.Run("mark only leaves notes alone", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		rec := f.send(http.MethodPatch, path(f), f.headers["assigned interviewer"], `{"mark":9}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d; body %s", rec.Code, rec.Body)
		}
		if p := f.reviews.lastPatch; p.Mark == nil || *p.Mark != 9 || p.Notes != nil {
			t.Errorf("patch = %+v", p)
		}
	})

	t.Run("blank notes clear them", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		if rec := f.send(http.MethodPatch, path(f), f.headers["assigned interviewer"], `{"notes":"   "}`); rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		if p := f.reviews.lastPatch; p.Mark != nil || p.Notes == nil || *p.Notes != "" {
			t.Errorf("patch = %+v, want notes cleared", p)
		}
	})

	t.Run("an invalid mark is a 422", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		if rec := f.send(http.MethodPatch, path(f), f.headers["assigned interviewer"], `{"mark":"eight"}`); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status %d, want 422", rec.Code)
		}
	})

	t.Run("no review yet is a 404 no_review", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.reviews.reviewErr = fmt.Errorf("update review: %w", domain.ErrNoReview)
		rec := f.send(http.MethodPatch, path(f), f.headers["assigned interviewer"], `{"mark":4}`)
		if rec.Code != http.StatusNotFound || errorCodeOf(t, rec.Body.Bytes()) != "no_review" {
			t.Errorf("status %d; body %s", rec.Code, rec.Body)
		}
	})
}

// TestReviewsInApplicationDetail checks the view each role is given, and
// what the response then holds: every review for an admin, only their own
// for an interviewer, no reviews key at all for a candidate.
func TestReviewsInApplicationDetail(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	me := f.users["assigned interviewer"].ID
	f.apps.reviews = map[string][]domain.Review{f.mine.ID: {
		{ID: "r-mine", InterviewerID: me, Mark: 6, CreatedAt: reviewTime, UpdatedAt: reviewTime},
		{ID: "r-other", InterviewerID: "someone-else", Mark: 9, CreatedAt: reviewTime, UpdatedAt: reviewTime},
	}}
	path := "/v1/applications/" + f.mine.ID

	reviewIDs := func(t *testing.T, caller string) ([]string, bool) {
		t.Helper()
		rec := f.get(path, f.headers[caller])
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", caller, rec.Code)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		raw, present := body["reviews"]
		if !present {
			return nil, false
		}
		var reviews []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &reviews); err != nil {
			t.Fatalf("decode reviews: %v", err)
		}
		ids := make([]string, 0, len(reviews))
		for _, r := range reviews {
			ids = append(ids, r.ID)
		}
		return ids, true
	}

	if ids, present := reviewIDs(t, "admin"); !present || len(ids) != 2 {
		t.Errorf("admin: %v, want both reviews", ids)
	}
	if ids, present := reviewIDs(t, "assigned interviewer"); !present || len(ids) != 1 || ids[0] != "r-mine" {
		t.Errorf("interviewer: %v, want only their own", ids)
	}
	if v := f.apps.lastView; v.Reviews != domain.ReviewsOwn || v.ViewerID != me {
		t.Errorf("interviewer's view = %+v", v)
	}
	if ids, present := reviewIDs(t, "candidate"); present {
		t.Errorf("candidate got a reviews key: %v", ids)
	}
	if v := f.apps.lastView; v.Reviews != domain.ReviewsNone || len(v.EventTypes) != 2 {
		t.Errorf("candidate's view = %+v, want no reviews and two event types", v)
	}
}

func TestTimeline(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	admin := f.users["admin"]
	f.apps.events = map[string][]domain.ApplicationEvent{f.mine.ID: {
		{ID: "e2", Type: domain.EventApplicationStatusChanged, ActorID: &admin.ID, ActorName: &admin.FullName, ActorRole: &admin.Role,
			Payload: map[string]any{"from": "submitted", "to": "shortlisted"}, CreatedAt: reviewTime},
		{ID: "e1", Type: "application.system_note", CreatedAt: reviewTime.Add(-time.Hour)},
	}}
	path := "/v1/applications/" + f.mine.ID + "/events"

	rec := f.get(path+"?limit=1", f.headers["admin"])
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; body %s", rec.Code, rec.Body)
	}
	var page struct {
		Data []struct {
			ID      string          `json:"id"`
			Actor   json.RawMessage `json:"actor"`
			Payload map[string]any  `json:"payload"`
		} `json:"data"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Data) != 1 || page.NextCursor == nil || !strings.Contains(string(page.Data[0].Actor), `"role":"admin"`) {
		t.Errorf("page 1 = %s", rec.Body)
	}

	rec = f.get(path, f.headers["admin"])
	if !strings.Contains(rec.Body.String(), `"actor":null`) || !strings.Contains(rec.Body.String(), `"payload":{}`) {
		t.Errorf("a system event must have a null actor and an object payload: %s", rec.Body)
	}

	for _, tt := range []struct {
		caller string
		want   int
	}{
		{"assigned interviewer", http.StatusOK},
		{"candidate", http.StatusForbidden}, // may see the application, not its timeline
		{"unassigned interviewer", http.StatusNotFound},
		{"other candidate", http.StatusNotFound},
	} {
		if rec := f.get(path, f.headers[tt.caller]); rec.Code != tt.want {
			t.Errorf("%s: status %d, want %d", tt.caller, rec.Code, tt.want)
		}
	}
	if rec := f.get(path+"?after=nonsense", f.headers["admin"]); rec.Code != http.StatusBadRequest {
		t.Errorf("bad cursor: status %d", rec.Code)
	}
}

// errorCodeOf returns the error code of an error envelope.
func errorCodeOf(t *testing.T, body []byte) string {
	t.Helper()

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error body %s: %v", body, err)
	}
	return envelope.Error.Code
}

// Compile-time check that the fake satisfies the interface.
var _ ReviewStore = (*fakeReviews)(nil)
