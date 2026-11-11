package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

func TestSearch(t *testing.T) {
	t.Parallel()

	const postID = "bbbbbbbb-0000-4000-8000-000000000001"
	base := "/v1/job-posts/" + postID + "/applications/search"
	when := time.Date(2026, time.March, 1, 9, 0, 0, 0, time.UTC)

	t.Run("results, totals and paging", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.searchTotal = 3
		f.apps.searchResults = []domain.SearchResult{
			{ApplicationID: "a1", CandidateName: "Ada", Status: "submitted", SubmittedAt: when, Relevance: 0.6,
				Totals: &domain.ScoreTotals{RubricVersion: 1, Earned: 75, Available: 100}},
			{ApplicationID: "a2", CandidateName: "Ben", Status: "submitted", SubmittedAt: when, Relevance: 0.3},
		}

		rec := f.get(base+"?q=%20%20go%20&limit=2", f.headers["admin"])
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		var got struct {
			Query      string                       `json:"query"`
			Total      int                          `json:"total"`
			NextOffset *int                         `json:"next_offset"`
			Data       []map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Query != "go" || got.Total != 3 || len(got.Data) != 2 {
			t.Fatalf("body = %s", rec.Body)
		}
		if got.NextOffset == nil || *got.NextOffset != 2 {
			t.Errorf("next_offset = %v, want 2", got.NextOffset)
		}
		if string(got.Data[0]["percentage"]) != "75" || string(got.Data[1]["percentage"]) != "null" {
			t.Errorf("percentages = %s, %s", got.Data[0]["percentage"], got.Data[1]["percentage"])
		}
		if want := (searchCall{query: "go", limit: 2, offset: 0}); *f.apps.lastSearch != want {
			t.Errorf("store got %+v, want %+v", *f.apps.lastSearch, want)
		}
	})

	t.Run("last page has no next offset", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		f.apps.searchTotal = 1
		f.apps.searchResults = []domain.SearchResult{{ApplicationID: "a1", SubmittedAt: when}}
		rec := f.get(base+"?q=go", f.headers["admin"])
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"next_offset":null`) {
			t.Errorf("status %d, body %s", rec.Code, rec.Body)
		}
	})

	t.Run("limit is capped", func(t *testing.T) {
		t.Parallel()

		f := newAppFixture(t)
		if rec := f.get(base+"?q=go&limit=100000", f.headers["admin"]); rec.Code != http.StatusOK || f.apps.lastSearch.limit != domain.MaxSearchLimit {
			t.Errorf("status %d, store got %+v; want 200 and the cap", rec.Code, f.apps.lastSearch)
		}
	})

	// Every input here is refused before the store is called, with a 422
	// naming the parameter, never a 500.
	for _, tt := range []struct {
		name, query, field string
	}{
		{"missing q", "", "q"},
		{"empty q", "q=", "q"},
		{"whitespace q", "q=" + url.QueryEscape(" \t\n "), "q"},
		{"invalid UTF-8", "q=%FF%FE", "q"},
		{"NUL byte", "q=go%00postgres", "q"},
		{"too long", "q=" + strings.Repeat("a", domain.MaxSearchQueryChars+1), "q"},
		{"limit zero", "q=go&limit=0", "limit"},
		{"limit not a number", "q=go&limit=ten", "limit"},
		{"negative offset", "q=go&offset=-1", "offset"},
		{"offset too deep", "q=go&offset=10001", "offset"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAppFixture(t)
			path := base
			if tt.query != "" {
				path += "?" + tt.query
			}
			rec := f.get(path, f.headers["admin"])
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), `"`+tt.field+`"`) {
				t.Errorf("body does not name %s: %s", tt.field, rec.Body)
			}
			if f.apps.lastSearch != nil {
				t.Errorf("the store was called with %+v", *f.apps.lastSearch)
			}
		})
	}

	t.Run("200 characters of multi-byte text is not too long", func(t *testing.T) {
		t.Parallel()

		// 200 characters but 600 bytes: the limit counts characters.
		f := newAppFixture(t)
		q := strings.Repeat("語", domain.MaxSearchQueryChars)
		if rec := f.get(base+"?q="+url.QueryEscape(q), f.headers["admin"]); rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
	})

	for _, tt := range []struct {
		name, caller, path string
		want               int
	}{
		{"missing post", "admin", "/v1/job-posts/bbbbbbbb-0000-4000-8000-00000000ffff/applications/search?q=go", http.StatusNotFound},
		{"malformed ID", "admin", "/v1/job-posts/nope/applications/search?q=go", http.StatusBadRequest},
		{"interviewer", "assigned interviewer", base + "?q=go", http.StatusForbidden},
		{"candidate", "candidate", base + "?q=go", http.StatusForbidden},
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
