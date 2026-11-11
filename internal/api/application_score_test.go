package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// longAnswer is an answer far longer than an excerpt, with its keywords in
// the middle, past where the start of the answer would be cut.
var longAnswer = strings.Repeat("I care about tests and readable code. ", 20) +
	"Last year I built a REST API in Go using Gin, with goroutines handling the background jobs. " +
	strings.Repeat("It ran in production for a year without an outage. ", 20)

// scoreCardFor is a stored score with one criterion of each evidence shape:
// a keyword match on a long answer, a number, and a boolean_all with none.
func scoreCardFor(appID string) *domain.ScoreCard {
	return &domain.ScoreCard{
		ApplicationID: appID,
		RubricVersion: 2,
		Entries: []domain.ScoreEntry{
			{
				Criterion: domain.Criterion{
					Label: "Go knowledge", Kind: domain.CriterionKindKeywordMatch,
					Config: json.RawMessage(`{"keywords": ["go", "gin", "goroutine", "kubernetes"]}`),
				},
				Earned: 22.5, Available: 30, Detail: "matched 3 of 4 keywords: go, gin, goroutine",
				Evidence: &domain.ScoreEvidence{AnswerID: "e91c", Question: "Describe a project you built", AnswerText: longAnswer},
			},
			{
				Criterion: domain.Criterion{Label: "Experience", Kind: domain.CriterionKindExperienceYears},
				Earned:    0.1, Available: 60, Detail: "0.1 years of 2 targeted",
				Evidence: &domain.ScoreEvidence{AnswerID: "c4a2", Question: "Years of Go?", AnswerText: "0.1"},
			},
			{
				Criterion: domain.Criterion{Label: "Remote readiness", Kind: domain.CriterionKindBooleanAll},
				Earned:    10, Available: 10.2, Detail: "3 of 3 conditions met",
			},
		},
	}
}

func TestApplicationScore(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	f.apps.scoreCards[f.mine.ID] = scoreCardFor(f.mine.ID)
	f.apps.scoreCards[f.theirs.ID] = scoreCardFor(f.theirs.ID)
	const missing = "aaaaaaaa-0000-4000-8000-00000000ffff"

	tests := []struct {
		caller     string
		id         string
		wantStatus int
	}{
		{"admin", f.mine.ID, http.StatusOK},
		{"admin", f.theirs.ID, http.StatusOK},
		{"admin", missing, http.StatusNotFound},

		{"assigned interviewer", f.mine.ID, http.StatusOK},
		{"assigned interviewer", f.theirs.ID, http.StatusNotFound},
		{"unassigned interviewer", f.mine.ID, http.StatusNotFound},

		// Their own application, which they know exists: refused, not hidden.
		{"candidate", f.mine.ID, http.StatusForbidden},
		// Someone else's: indistinguishable from absent.
		{"candidate", f.theirs.ID, http.StatusNotFound},
		{"candidate", missing, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.caller+" → "+tt.id[len(tt.id)-4:], func(t *testing.T) {
			t.Parallel()

			rec := f.get("/v1/applications/"+tt.id+"/score", f.headers[tt.caller])
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusNotFound {
				absent := f.get("/v1/applications/"+missing+"/score", f.headers[tt.caller])
				if rec.Body.String() != absent.Body.String() {
					t.Errorf("refusal differs from absence:\n got:    %s\n absent: %s", rec.Body, absent.Body)
				}
			}
		})
	}
}

// The score card is loaded only for a caller allowed to read it, so a
// refused request does no more work than one for a missing application.
func TestApplicationScoreLoadsNothingForARefusedCaller(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	f.apps.scoreCards[f.mine.ID] = scoreCardFor(f.mine.ID)

	for _, caller := range []string{"candidate", "unassigned interviewer", "other candidate"} {
		f.get("/v1/applications/"+f.mine.ID+"/score", f.headers[caller])
	}
	if len(f.apps.scoreCalls) != 0 {
		t.Errorf("ScoreCard called for %v, want no calls", f.apps.scoreCalls)
	}
}

func TestApplicationScoreBody(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)
	f.apps.scoreCards[f.mine.ID] = scoreCardFor(f.mine.ID)

	rec := f.get("/v1/applications/"+f.mine.ID+"/score", f.headers["admin"])
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
	}

	// The exact top-level keys of the overview's shape, and no others.
	keys := keysOf(t, rec.Body.Bytes())
	for _, k := range []string{"application_id", "total_earned", "total_available", "percentage", "rubric_version", "breakdown"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if len(keys) != 6 {
		t.Errorf("got %d top-level keys, want 6: %s", len(keys), rec.Body)
	}

	var got scoreResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 22.5 + 0.1 + 10 is 32.6 and 30 + 60 + 10.2 is 100.2; 32.6 / 100.2 is
	// 32.5349…%, which the response rounds to two places.
	if got.TotalEarned != 32.6 || got.TotalAvailable != 100.2 || got.Percentage != 32.53 {
		t.Errorf("totals = %v of %v (%v%%), want 32.6 of 100.2 (32.53%%)", got.TotalEarned, got.TotalAvailable, got.Percentage)
	}
	if got.ApplicationID != f.mine.ID || got.RubricVersion != 2 || len(got.Breakdown) != 3 {
		t.Fatalf("got %+v", got)
	}

	keyword := got.Breakdown[0]
	if keyword.Criterion != "Go knowledge" || keyword.Kind != "keyword_match" || keyword.Evidence == nil {
		t.Fatalf("first entry = %+v", keyword)
	}
	ex := keyword.Evidence.Excerpt
	if keyword.Evidence.AnswerID != "e91c" || keyword.Evidence.Question != "Describe a project you built" {
		t.Errorf("evidence = %+v", keyword.Evidence)
	}
	if !strings.HasPrefix(ex, "...") || !strings.HasSuffix(ex, "...") || !strings.Contains(ex, "REST API in Go") {
		t.Errorf("excerpt = %q, want a window around the first keyword, cut on both sides", ex)
	}
	if n := utf8.RuneCountInString(ex); n > scoring.ExcerptWidth+2*len("...") {
		t.Errorf("excerpt is %d characters, want at most about %d", n, scoring.ExcerptWidth)
	}

	if short := got.Breakdown[1].Evidence; short == nil || short.Excerpt != "0.1" {
		t.Errorf("short answer evidence = %+v, want the whole answer", short)
	}

	// No single answer: evidence is JSON null, not an empty object.
	var raw struct {
		Breakdown []map[string]json.RawMessage `json:"breakdown"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if string(raw.Breakdown[2]["evidence"]) != "null" {
		t.Errorf("boolean_all evidence = %s, want null", raw.Breakdown[2]["evidence"])
	}

	if strings.Contains(rec.Body.String(), "without an outage") {
		t.Errorf("the response contains text from outside the excerpt: the whole answer leaked")
	}
}

func TestApplicationScoreNotScored(t *testing.T) {
	t.Parallel()

	f := newAppFixture(t)

	rec := f.get("/v1/applications/"+f.mine.ID+"/score", f.headers["admin"])
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body)
	}
	if got := decode(t, rec.Body.Bytes()).Code; got != "not_scored" {
		t.Errorf("code = %q, want not_scored", got)
	}
}
