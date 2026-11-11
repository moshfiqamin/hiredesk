package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

func TestUpdateApplicationStatus(t *testing.T) {
	t.Parallel()

	t.Run("an admin's move reaches the store with the admin as actor", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		rec := f.send(http.MethodPatch, "/v1/applications/"+f.mine.ID+"/status", f.headers["admin"], `{"status":"shortlisted"}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		f.apps.mu.Lock()
		got := f.apps.lastTransition
		f.apps.mu.Unlock()
		if want := [3]string{f.mine.ID, domain.StatusShortlisted, f.users["admin"].ID}; got == nil || *got != want {
			t.Errorf("store got %v, want %v", got, want)
		}

		var body applicationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Status != domain.StatusShortlisted {
			t.Errorf("body = %s", rec.Body)
		}
	})

	tests := []struct {
		name     string
		caller   string
		path     string
		body     string
		storeErr error
		status   int
		code     string
		reaches  bool // whether the store is asked
	}{
		{name: "a candidate", caller: "candidate", body: `{"status":"offer"}`, status: http.StatusForbidden, code: "forbidden"},
		{name: "an interviewer", caller: "assigned interviewer", body: `{"status":"offer"}`, status: http.StatusForbidden, code: "forbidden"},
		{name: "nobody", caller: "", body: `{"status":"offer"}`, status: http.StatusUnauthorized, code: "unauthenticated"},
		{name: "an ID that is not a UUID", caller: "admin", path: "/v1/applications/7/status", body: `{"status":"offer"}`,
			status: http.StatusBadRequest, code: "invalid_id"},
		{name: "a body that is not JSON", caller: "admin", body: `status=offer`, status: http.StatusBadRequest, code: "malformed_request"},
		{name: "no status", caller: "admin", body: `{}`, status: http.StatusUnprocessableEntity, code: "validation_failed"},
		{name: "a status that does not exist", caller: "admin", body: `{"status":"hired"}`,
			status: http.StatusUnprocessableEntity, code: "validation_failed"},
		{name: "a status in the wrong case", caller: "admin", body: `{"status":"Offer"}`,
			status: http.StatusUnprocessableEntity, code: "validation_failed"},

		{name: "a move the lifecycle refuses", caller: "admin", body: `{"status":"offer"}`,
			storeErr: domain.CheckApplicationTransition(domain.StatusSubmitted, domain.StatusOffer),
			status:   http.StatusConflict, code: "invalid_transition", reaches: true},
		{name: "an offer with no vacancy left", caller: "admin", body: `{"status":"offer"}`,
			storeErr: domain.CheckVacancyForOffer(2, 2),
			status:   http.StatusConflict, code: "no_vacancies", reaches: true},
		{name: "an application that does not exist", caller: "admin", body: `{"status":"rejected"}`,
			storeErr: domain.ErrNotFound, status: http.StatusNotFound, code: "not_found", reaches: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			f.apps.transitionErr = tt.storeErr
			path := tt.path
			if path == "" {
				path = "/v1/applications/" + f.mine.ID + "/status"
			}

			rec := f.send(http.MethodPatch, path, f.headers[tt.caller], tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.status, rec.Body)
			}
			if got := decode(t, rec.Body.Bytes()).Code; got != tt.code {
				t.Errorf("code = %q, want %q", got, tt.code)
			}
			f.apps.mu.Lock()
			defer f.apps.mu.Unlock()
			if reached := f.apps.lastTransition != nil; reached != tt.reaches {
				t.Errorf("store reached = %v, want %v", reached, tt.reaches)
			}
		})
	}

	t.Run("a refused move lists what is allowed", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.transitionErr = domain.CheckApplicationTransition(domain.StatusSubmitted, domain.StatusOffer)
		rec := f.send(http.MethodPatch, "/v1/applications/"+f.mine.ID+"/status", f.headers["admin"], `{"status":"offer"}`)

		details := decode(t, rec.Body.Bytes()).Details
		if !reflect.DeepEqual(details["allowed_next_statuses"], []any{"shortlisted", "rejected"}) ||
			details["current_status"] != "submitted" {
			t.Errorf("details = %v", details)
		}
	})
}

func TestListApplicationsParameters(t *testing.T) {
	t.Parallel()

	const postID = "cccccccc-0000-4000-8000-000000000002"

	t.Run("filters, limit and cursor reach the store", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		cursorAt := time.Date(2026, time.March, 1, 9, 0, 0, 123456000, time.UTC)
		cursor := encodeCursor(domain.ListingPosition{CreatedAt: cursorAt, ID: f.theirs.ID})

		rec := f.get("/v1/applications?status=interview&job_post_id=CCCCCCCC-0000-4000-8000-000000000002&limit=5&after="+cursor,
			f.headers["admin"])
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}

		f.apps.mu.Lock()
		got := f.apps.lastFilter
		f.apps.mu.Unlock()
		want := domain.ApplicationFilter{
			Status: domain.StatusInterview, JobPostID: postID, Limit: 5,
			After: &domain.ListingPosition{CreatedAt: cursorAt, ID: f.theirs.ID},
		}
		if got.Status != want.Status || got.JobPostID != want.JobPostID || got.Limit != want.Limit ||
			got.After == nil || !got.After.CreatedAt.Equal(want.After.CreatedAt) || got.After.ID != want.After.ID {
			t.Errorf("filter = %+v, want %+v", got, want)
		}
	})

	t.Run("no parameters means the first page at the default size", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.get("/v1/applications", f.headers["candidate"])

		f.apps.mu.Lock()
		defer f.apps.mu.Unlock()
		if want := (domain.ApplicationFilter{Limit: domain.DefaultListingLimit}); !reflect.DeepEqual(f.apps.lastFilter, want) {
			t.Errorf("filter = %+v, want %+v", f.apps.lastFilter, want)
		}
	})

	t.Run("a further page gets a cursor from the last row, and the total", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.list = []domain.Application{f.mine, f.theirs}
		f.apps.listTotal, f.apps.listMore = 7, true

		rec := f.get("/v1/applications?limit=2", f.headers["admin"])
		var page applicationPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("body: %v", err)
		}
		if page.Total != 7 || len(page.Data) != 2 || page.NextCursor == nil {
			t.Fatalf("page = %s", rec.Body)
		}
		pos, err := decodeCursor(*page.NextCursor)
		if err != nil || pos.ID != f.theirs.ID || !pos.CreatedAt.Equal(f.theirs.SubmittedAt) {
			t.Errorf("cursor decodes to %+v (%v), want the last row %s", pos, err, f.theirs.ID)
		}
	})

	refusals := []struct {
		name, query string
		status      int
		code        string
		fields      []string
	}{
		{"an unknown status", "status=hired", http.StatusUnprocessableEntity, "validation_failed", []string{"status"}},
		{"a job post ID that is not a UUID", "job_post_id=42", http.StatusUnprocessableEntity, "validation_failed", []string{"job_post_id"}},
		{"both wrong, reported together", "status=x&job_post_id=y", http.StatusUnprocessableEntity, "validation_failed", []string{"status", "job_post_id"}},
		{"a limit of zero", "limit=0", http.StatusUnprocessableEntity, "validation_failed", []string{"limit"}},
		{"a cursor this API did not issue", "after=bm90LWEtY3Vyc29y", http.StatusBadRequest, "invalid_cursor", nil},
	}
	for _, tt := range refusals {
		t.Run("refused: "+tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			rec := f.get("/v1/applications?"+tt.query, f.headers["admin"])
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.status, rec.Body)
			}
			got := decode(t, rec.Body.Bytes())
			if got.Code != tt.code {
				t.Errorf("code = %q, want %q", got.Code, tt.code)
			}
			for _, field := range tt.fields {
				if _, ok := got.Details[field]; !ok {
					t.Errorf("details has no %q: %v", field, got.Details)
				}
			}
			f.apps.mu.Lock()
			defer f.apps.mu.Unlock()
			if f.apps.lastViewer.ID != "" {
				t.Errorf("store was asked despite the refusal")
			}
		})
	}
}

func TestGetApplicationDetail(t *testing.T) {
	t.Parallel()

	t.Run("includes answers and events", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		at := time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC)
		admin := f.users["admin"].ID
		f.apps.answers = map[string][]domain.Answer{
			f.mine.ID: {{ID: "ans-1", QuestionID: "q-1", Text: "4", CreatedAt: at}},
		}
		f.apps.events = map[string][]domain.ApplicationEvent{
			f.mine.ID: {
				{ID: "ev-1", ActorID: &f.mine.CandidateID, Type: domain.EventApplicationSubmitted,
					Payload: map[string]any{"answers": 1.0}, CreatedAt: at},
				{ID: "ev-2", ActorID: &admin, Type: domain.EventApplicationStatusChanged,
					Payload: map[string]any{"from": "submitted", "to": "shortlisted"}, CreatedAt: at},
				{ID: "ev-3", Type: "system.note", CreatedAt: at},
			},
		}

		rec := f.get("/v1/applications/"+f.mine.ID, f.headers["candidate"])
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}

		var body applicationDetailResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if body.ID != f.mine.ID || len(body.Answers) != 1 || body.Answers[0].Answer != "4" {
			t.Errorf("application or answers wrong: %s", rec.Body)
		}
		if len(body.Events) != 3 || body.Events[1].Payload["from"] != "submitted" || body.Events[1].Payload["to"] != "shortlisted" {
			t.Errorf("events = %+v", body.Events)
		}
		// No actor, and no payload: null and {} rather than missing.
		if body.Events[2].ActorID != nil || body.Events[2].Payload == nil {
			t.Errorf("system event = %+v, want a null actor and an empty payload", body.Events[2])
		}
	})

	t.Run("a refused viewer never causes the history to be loaded", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		refused := f.get("/v1/applications/"+f.theirs.ID, f.headers["candidate"])
		if refused.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", refused.Code)
		}
		f.apps.mu.Lock()
		defer f.apps.mu.Unlock()
		if len(f.apps.historyCalls) != 0 {
			t.Errorf("history loaded for %v", f.apps.historyCalls)
		}
	})

	t.Run("an empty history is two empty arrays", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		rec := f.get("/v1/applications/"+f.mine.ID, f.headers["admin"])
		keys := keysOf(t, rec.Body.Bytes())
		if string(keys["answers"]) != "[]" || string(keys["events"]) != "[]" {
			t.Errorf("answers = %s, events = %s; want [] and []", keys["answers"], keys["events"])
		}
	})
}
