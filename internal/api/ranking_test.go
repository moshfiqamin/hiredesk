package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

func TestRanking(t *testing.T) {
	t.Parallel()

	const postID = "bbbbbbbb-0000-4000-8000-000000000001"
	path := "/v1/job-posts/" + postID + "/ranking"
	when := time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)

	t.Run("scored and unscored entries", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.ranking = []domain.RankedApplication{
			{Rank: 1, ApplicationID: "a1", CandidateName: "Ada", Status: "submitted", SubmittedAt: when,
				Totals: &domain.ScoreTotals{RubricVersion: 2, Earned: 80, Available: 100}},
			{Rank: 2, ApplicationID: "a2", CandidateName: "Ben", Status: "submitted", SubmittedAt: when},
		}

		rec := f.get(path, f.headers["admin"])
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		var got struct {
			JobPostID string                       `json:"job_post_id"`
			Data      []map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.JobPostID != postID || len(got.Data) != 2 {
			t.Fatalf("body = %s", rec.Body)
		}
		if string(got.Data[0]["percentage"]) != "80" || string(got.Data[0]["rank"]) != "1" {
			t.Errorf("first = %v", got.Data[0])
		}
		for _, k := range []string{"total_earned", "total_available", "percentage", "rubric_version"} {
			if string(got.Data[1][k]) != "null" {
				t.Errorf("unscored %s = %s, want null", k, got.Data[1][k])
			}
		}
		if f.apps.lastRankLimit != domain.DefaultRankingLimit {
			t.Errorf("limit = %d, want the default", f.apps.lastRankLimit)
		}
	})

	t.Run("limit is capped and validated", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		if rec := f.get(path+"?limit=100000", f.headers["admin"]); rec.Code != http.StatusOK || f.apps.lastRankLimit != domain.MaxRankingLimit {
			t.Errorf("status %d, limit %d; want 200 and the cap", rec.Code, f.apps.lastRankLimit)
		}
		if rec := f.get(path+"?limit=0", f.headers["admin"]); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("limit=0: status %d, want 422", rec.Code)
		}
	})

	for _, tt := range []struct {
		name, caller, path string
		want               int
	}{
		{"missing post", "admin", "/v1/job-posts/bbbbbbbb-0000-4000-8000-00000000ffff/ranking", http.StatusNotFound},
		{"malformed ID", "admin", "/v1/job-posts/nope/ranking", http.StatusBadRequest},
		{"interviewer", "assigned interviewer", path, http.StatusForbidden},
		{"candidate", "candidate", path, http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			if rec := f.get(tt.path, f.headers[tt.caller]); rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
