//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// reviewOf files asID's review of appID and returns its ID.
func (f *reviewFixture) reviewOf(t *testing.T, appID, asID, body string) string {
	t.Helper()

	rec := f.call(http.MethodPost, "/v1/applications/"+appID+"/reviews", asID, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("review by %s: %d %s", asID, rec.Code, rec.Body)
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode review: %v", err)
	}
	return r.ID
}

// TestReviews_InterviewerSeesOnlyTheirOwn is the anchoring rule. Omar
// reviews first; Ivy, about to review, must not be able to learn his mark,
// his notes, or even his review's ID from any response she can get.
// The checks are on the raw response bytes, not on decoded fields, so a
// leak through any key, the events included, fails the test.
func TestReviews_InterviewerSeesOnlyTheirOwn(t *testing.T) {
	f := newReviewFixture(t)
	f.assign(t, f.app, ivyID)
	f.assign(t, f.app, omarID)

	// Distinctive values, so a leak is unmistakable in the bytes.
	omarReview := f.reviewOf(t, f.app, omarID, `{"mark":9,"notes":"omar-secret-notes"}`)
	omarSecrets := []string{omarReview, "omar-secret-notes"}

	detailPath := "/v1/applications/" + f.app
	timelinePath := detailPath + "/events"

	// Before Ivy reviews: her detail has an empty reviews list, and neither
	// it nor her timeline holds anything of Omar's review.
	detail := f.call(http.MethodGet, detailPath, ivyID, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"reviews":[]`) {
		t.Fatalf("Ivy's detail before reviewing: %d %s", detail.Code, detail.Body)
	}
	timeline := f.call(http.MethodGet, timelinePath, ivyID, "")
	if timeline.Code != http.StatusOK {
		t.Fatalf("Ivy's timeline: %d", timeline.Code)
	}
	for _, body := range []string{detail.Body.String(), timeline.Body.String()} {
		for _, secret := range omarSecrets {
			if strings.Contains(body, secret) {
				t.Errorf("Ivy can see %q before reviewing:\n%s", secret, body)
			}
		}
		if strings.Contains(body, domain.EventApplicationReviewed) {
			t.Errorf("Ivy can see that a review was filed:\n%s", body)
		}
	}

	// After Ivy reviews: she sees exactly her own, still nothing of Omar's.
	ivyReview := f.reviewOf(t, f.app, ivyID, `{"mark":4,"notes":"ivy-notes"}`)
	detail = f.call(http.MethodGet, detailPath, ivyID, "")
	var mine struct {
		Reviews []struct {
			ID   string `json:"id"`
			Mark int    `json:"mark"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &mine); err != nil || len(mine.Reviews) != 1 ||
		mine.Reviews[0].ID != ivyReview || mine.Reviews[0].Mark != 4 {
		t.Errorf("Ivy's reviews = %+v, want only her own mark 4", mine.Reviews)
	}
	timeline = f.call(http.MethodGet, timelinePath, ivyID, "")
	for _, body := range []string{detail.Body.String(), timeline.Body.String()} {
		for _, secret := range omarSecrets {
			if strings.Contains(body, secret) {
				t.Errorf("Ivy can see %q after reviewing:\n%s", secret, body)
			}
		}
	}
	if !strings.Contains(timeline.Body.String(), ivyReview) {
		t.Errorf("Ivy's timeline lacks her own review event:\n%s", timeline.Body)
	}

	// The admin sees both reviews and both review events.
	adminDetail := f.call(http.MethodGet, detailPath, scoringAdminID, "").Body.String()
	adminTimeline := f.call(http.MethodGet, timelinePath, scoringAdminID, "").Body.String()
	for _, want := range []string{omarReview, ivyReview, "omar-secret-notes", "ivy-notes"} {
		if !strings.Contains(adminDetail, want) {
			t.Errorf("admin detail lacks %q", want)
		}
	}
	if strings.Count(adminTimeline, domain.EventApplicationReviewed) != 2 {
		t.Errorf("admin timeline should hold both review events:\n%s", adminTimeline)
	}

	// The candidate: no reviews key at all, no review or assignment events,
	// and no rescore totals, which CanViewScore keeps from them.
	candidate := f.call(http.MethodGet, detailPath, subCandidateID, "")
	var body map[string]json.RawMessage
	if err := json.Unmarshal(candidate.Body.Bytes(), &body); err != nil || candidate.Code != http.StatusOK {
		t.Fatalf("candidate detail: %d %s", candidate.Code, candidate.Body)
	}
	if _, present := body["reviews"]; present {
		t.Errorf("candidate got a reviews key: %s", body["reviews"])
	}
	for _, leaked := range []string{omarReview, ivyReview, "secret", "ivy-notes", "application.review", "application.assigned", ivyID, omarID} {
		if strings.Contains(candidate.Body.String(), leaked) {
			t.Errorf("candidate can see %q:\n%s", leaked, candidate.Body)
		}
	}
}

// TestReviews_ValidationAndOnePerInterviewer checks the mark rule against
// the real stack, the 409 on a second review, and the event written.
func TestReviews_ValidationAndOnePerInterviewer(t *testing.T) {
	f := newReviewFixture(t)
	f.assign(t, f.app, ivyID)
	path := "/v1/applications/" + f.app + "/reviews"

	for _, body := range []string{`{"mark":0}`, `{"mark":11}`, `{"mark":"eight"}`, `{"mark":7.5}`, `{"notes":"no mark"}`} {
		if rec := f.call(http.MethodPost, path, ivyID, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422; %s", body, rec.Code, rec.Body)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM reviews`); n != 0 {
		t.Fatalf("%d reviews stored by invalid requests", n)
	}

	reviewID := f.reviewOf(t, f.app, ivyID, `{"mark":7,"notes":"solid"}`)

	rec := f.call(http.MethodPost, path, ivyID, `{"mark":3}`)
	if rec.Code != http.StatusConflict || errorCode(t, rec.Body.Bytes()) != "already_reviewed" {
		t.Errorf("second review: %d %s, want 409 already_reviewed", rec.Code, rec.Body)
	}

	// Five more at once: the unique constraint refuses every one.
	var wg sync.WaitGroup
	codes := make([]int, 5)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = f.call(http.MethodPost, path, ivyID, `{"mark":2}`).Code
		}()
	}
	wg.Wait()
	for _, code := range codes {
		if code != http.StatusConflict {
			t.Errorf("a concurrent second review answered %d, want 409", code)
		}
	}

	var mark int
	var payload map[string]any
	if err := f.pool.QueryRow(t.Context(), `SELECT r.mark, e.payload FROM reviews r
		JOIN application_events e ON e.application_id = r.application_id AND e.type = 'application.reviewed'
		WHERE r.id = $1::text::uuid`, reviewID).Scan(&mark, &payload); err != nil {
		t.Fatalf("read review and event: %v", err)
	}
	if mark != 7 || payload["mark"] != float64(7) || payload["review_id"] != reviewID || payload["interviewer_id"] != ivyID {
		t.Errorf("mark %d, event payload %v", mark, payload)
	}
	if n := f.eventCount(t, f.app, domain.EventApplicationReviewed); n != 1 {
		t.Errorf("%d reviewed events, want 1", n)
	}

	// Omar is not assigned: the same 404 as an application that does not
	// exist, so he learns nothing.
	if rec := f.call(http.MethodPost, path, omarID, `{"mark":5}`); rec.Code != http.StatusNotFound {
		t.Errorf("unassigned interviewer: %d, want 404", rec.Code)
	}
}

// TestReviews_Correction checks PATCH .../reviews/mine: the change, the
// event with both marks, and that a correction changing nothing records
// nothing.
func TestReviews_Correction(t *testing.T) {
	f := newReviewFixture(t)
	f.assign(t, f.app, ivyID)
	f.assign(t, f.app, omarID)
	path := "/v1/applications/" + f.app + "/reviews/mine"

	// Omar has not reviewed, so there is nothing of his to correct.
	if rec := f.call(http.MethodPatch, path, omarID, `{"mark":5}`); rec.Code != http.StatusNotFound ||
		errorCode(t, rec.Body.Bytes()) != "no_review" {
		t.Errorf("correcting a review never filed: %d %s", rec.Code, rec.Body)
	}

	f.reviewOf(t, f.app, ivyID, `{"mark":7,"notes":"first thoughts"}`)

	rec := f.call(http.MethodPatch, path, ivyID, `{"mark":8}`)
	var got struct {
		Mark  int     `json:"mark"`
		Notes *string `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK ||
		got.Mark != 8 || got.Notes == nil || *got.Notes != "first thoughts" {
		t.Fatalf("correct the mark: %d %s", rec.Code, rec.Body)
	}

	// The same correction again changes nothing, so records nothing.
	if rec := f.call(http.MethodPatch, path, ivyID, `{"mark":8}`); rec.Code != http.StatusOK {
		t.Fatalf("repeat: %d", rec.Code)
	}
	if n := f.eventCount(t, f.app, domain.EventApplicationReviewUpdated); n != 1 {
		t.Errorf("%d review_updated events after a change and a no-op, want 1", n)
	}

	// Clearing the notes is a change.
	if rec := f.call(http.MethodPatch, path, ivyID, `{"notes":""}`); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"notes":null`) {
		t.Errorf("clear notes: %d %s", rec.Code, rec.Body)
	}

	rows, err := f.pool.Query(t.Context(), `SELECT payload FROM application_events
		WHERE application_id = $1::text::uuid AND type = 'application.review_updated' ORDER BY created_at, id`, f.app)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var payloads []map[string]any
	for rows.Next() {
		var p map[string]any
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		payloads = append(payloads, p)
	}
	if len(payloads) != 2 {
		t.Fatalf("review_updated events = %v, want 2", payloads)
	}
	if p := payloads[0]; p["from_mark"] != float64(7) || p["to_mark"] != float64(8) || p["notes_changed"] != false {
		t.Errorf("mark correction payload = %v", p)
	}
	if p := payloads[1]; p["from_mark"] != float64(8) || p["to_mark"] != float64(8) || p["notes_changed"] != true {
		t.Errorf("notes correction payload = %v", p)
	}
}
