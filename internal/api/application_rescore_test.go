package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

func TestRescoreApplication(t *testing.T) {
	t.Parallel()

	t.Run("previous and current totals, and who asked", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.rescoreResult = &domain.RescoreResult{
			ApplicationID: f.mine.ID,
			Previous:      &domain.ScoreTotals{RubricVersion: 1, Earned: 66.8, Available: 100},
			Current:       domain.ScoreTotals{RubricVersion: 2, Earned: 41, Available: 80},
		}

		rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/rescore", f.headers["admin"], "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		var got rescoreResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Previous == nil || got.Previous.RubricVersion != 1 || got.Previous.Percentage != 66.8 {
			t.Errorf("previous = %+v", got.Previous)
		}
		if got.Current.RubricVersion != 2 || got.Current.Percentage != 51.25 || !got.Changed {
			t.Errorf("current = %+v, changed = %v; want version 2 at 51.25%% and changed", got.Current, got.Changed)
		}
		if f.apps.lastRescore == nil || *f.apps.lastRescore != [2]string{f.mine.ID, f.users["admin"].ID} {
			t.Errorf("store got %v, want the application and the admin", f.apps.lastRescore)
		}
	})

	t.Run("an application never scored before has a null previous", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.rescoreResult = &domain.RescoreResult{
			ApplicationID: f.mine.ID, Current: domain.ScoreTotals{RubricVersion: 1, Earned: 10, Available: 100},
		}
		rec := f.send(http.MethodPost, "/v1/applications/"+f.mine.ID+"/rescore", f.headers["admin"], "")
		if keys := keysOf(t, rec.Body.Bytes()); string(keys["previous"]) != "null" || string(keys["changed"]) != "true" {
			t.Errorf("previous = %s, changed = %s; want null and true", keys["previous"], keys["changed"])
		}
	})

	for _, tt := range []struct {
		name     string
		caller   string
		path     string
		err      error
		want     int
		wantCode string
	}{
		{"a post with no rubric", "admin", "/v1/applications/aaaaaaaa-0000-4000-8000-000000000001/rescore", domain.ErrNoRubric, http.StatusConflict, "no_rubric"},
		{"no such application", "admin", "/v1/applications/aaaaaaaa-0000-4000-8000-00000000ffff/rescore", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"a malformed ID", "admin", "/v1/applications/not-a-uuid/rescore", nil, http.StatusBadRequest, "invalid_id"},
		{"an assigned interviewer", "assigned interviewer", "/v1/applications/aaaaaaaa-0000-4000-8000-000000000001/rescore", nil, http.StatusForbidden, "forbidden"},
		{"the candidate", "candidate", "/v1/applications/aaaaaaaa-0000-4000-8000-000000000001/rescore", nil, http.StatusForbidden, "forbidden"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			f.apps.rescoreErr = tt.err
			rec := f.send(http.MethodPost, tt.path, f.headers[tt.caller], "")
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.want, rec.Body)
			}
			if got := decode(t, rec.Body.Bytes()).Code; got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
			if tt.want == http.StatusForbidden && f.apps.lastRescore != nil {
				t.Errorf("the store was called for a refused caller")
			}
		})
	}
}

func TestRescoreAll(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	const postID = "bbbbbbbb-0000-4000-8000-000000000001"
	same := domain.ScoreTotals{RubricVersion: 2, Earned: 50, Available: 100}
	f.apps.rescoreAllResult = &domain.JobPostRescore{
		JobPostID: postID, RubricVersion: 2,
		Results: []domain.RescoreResult{
			{ApplicationID: "a1", Previous: &same, Current: same},                                                             // unchanged
			{ApplicationID: "a2", Previous: &domain.ScoreTotals{RubricVersion: 1, Earned: 50, Available: 100}, Current: same}, // new version
			{ApplicationID: "a3", Current: same},                                                                              // first score
		},
	}

	rec := f.send(http.MethodPost, "/v1/job-posts/"+postID+"/rescore-all", f.headers["admin"], "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
	}
	var got rescoreAllResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := rescoreAllResponse{JobPostID: postID, RubricVersion: 2, Rescored: 3, Changed: 2}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if rec := f.send(http.MethodPost, "/v1/job-posts/"+postID+"/rescore-all", f.headers["assigned interviewer"], ""); rec.Code != http.StatusForbidden {
		t.Errorf("interviewer: status = %d, want 403", rec.Code)
	}
}
