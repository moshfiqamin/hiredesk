//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// timelineEvent is one entry of GET /v1/applications/:id/events.
type timelineEvent struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Actor *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"actor"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

// TestTimeline_EveryEventTypeIsWritten drives one application through every
// write path the API has, through the HTTP routes, and checks that the
// timeline then holds all seven application event types, newest first,
// each with its actor resolved to a name, and that paging walks it without
// a gap or a repeat.
func TestTimeline_EveryEventTypeIsWritten(t *testing.T) {
	f := newReviewFixture(t)
	app := "/v1/applications/" + f.app

	// application.submitted was written by newReviewFixture's submit.
	steps := []struct {
		method, path, asID, body string
		want                     int
	}{
		{http.MethodPost, app + "/assign", scoringAdminID, `{"interviewer_id":"` + ivyID + `"}`, http.StatusCreated},
		{http.MethodPost, app + "/assign", scoringAdminID, `{"interviewer_id":"` + omarID + `"}`, http.StatusCreated},
		{http.MethodDelete, app + "/assign/" + omarID, scoringAdminID, "", http.StatusNoContent},
		{http.MethodPost, app + "/reviews", ivyID, `{"mark":6}`, http.StatusCreated},
		{http.MethodPatch, app + "/reviews/mine", ivyID, `{"mark":7}`, http.StatusOK},
		{http.MethodPost, app + "/rescore", scoringAdminID, "", http.StatusOK},
		{http.MethodPatch, app + "/status", scoringAdminID, `{"status":"shortlisted"}`, http.StatusOK},
	}
	for _, s := range steps {
		if rec := f.call(s.method, s.path, s.asID, s.body); rec.Code != s.want {
			t.Fatalf("%s %s: %d, want %d; %s", s.method, s.path, rec.Code, s.want, rec.Body)
		}
	}

	// Walk the whole timeline two at a time.
	var all []timelineEvent
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("paging does not end")
		}
		path := app + "/events?limit=2"
		if cursor != "" {
			path += "&after=" + url.QueryEscape(cursor)
		}
		rec := f.call(http.MethodGet, path, scoringAdminID, "")
		var page struct {
			Data       []timelineEvent `json:"data"`
			NextCursor *string         `json:"next_cursor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s", pages, rec.Code, rec.Body)
		}
		all = append(all, page.Data...)
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}

	// 1 submitted + 2 assigned + 1 unassigned + reviewed + review_updated +
	// rescored + status_changed.
	if len(all) != 8 {
		t.Fatalf("walked %d events, want 8", len(all))
	}

	seen := map[string]bool{}
	ids := map[string]bool{}
	for i, e := range all {
		if ids[e.ID] {
			t.Errorf("event %s appears twice across pages", e.ID)
		}
		ids[e.ID] = true
		seen[e.Type] = true

		if e.Actor == nil || e.Actor.Name == "" || e.Actor.Role == "" {
			t.Errorf("%s has no resolved actor: %+v", e.Type, e.Actor)
		}
		if i > 0 && e.CreatedAt.After(all[i-1].CreatedAt) {
			t.Errorf("event %d (%s) is newer than the one before it", i, e.Type)
		}
	}
	for _, want := range []string{
		domain.EventApplicationSubmitted, domain.EventApplicationStatusChanged, domain.EventApplicationRescored,
		domain.EventApplicationAssigned, domain.EventApplicationUnassigned,
		domain.EventApplicationReviewed, domain.EventApplicationReviewUpdated,
	} {
		if !seen[want] {
			t.Errorf("no %s event in the timeline", want)
		}
	}

	// Newest first: the status change was the last thing done.
	if all[0].Type != domain.EventApplicationStatusChanged || all[0].Actor.Name != "Admin" {
		t.Errorf("newest = %s by %+v", all[0].Type, all[0].Actor)
	}
	if last := all[len(all)-1]; last.Type != domain.EventApplicationSubmitted || last.Actor.ID != subCandidateID ||
		last.Actor.Name != "Applicant" || last.Actor.Role != domain.RoleCandidate {
		t.Errorf("oldest = %s by %+v", last.Type, last.Actor)
	}

	// The audit the brief asked for, straight from the table: no event
	// anywhere lacks an actor.
	if n := f.count(t, `SELECT count(*) FROM application_events WHERE actor_id IS NULL`); n != 0 {
		t.Errorf("%d events have no actor", n)
	}

	// The actor's name is read when the timeline is, so a renamed user shows
	// under their current name, on events written before the rename.
	if _, err := f.pool.Exec(t.Context(), `UPDATE users SET full_name = 'Ivy Renamed' WHERE id = $1::text::uuid`, ivyID); err != nil {
		t.Fatalf("rename: %v", err)
	}
	rec := f.call(http.MethodGet, app+"/events?limit=100", scoringAdminID, "")
	var page struct {
		Data []timelineEvent `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, e := range page.Data {
		if e.Type == domain.EventApplicationReviewed && e.Actor.Name != "Ivy Renamed" {
			t.Errorf("review event actor = %q, want the current name", e.Actor.Name)
		}
	}
}
