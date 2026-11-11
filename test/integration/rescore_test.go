//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// replaceWithTwoCriteria swaps the scoring post's four-criterion rubric for
// a two-criterion one, as version 2, so a rescore has fewer rows to write
// than there are to delete.
func replaceWithTwoCriteria(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := store.NewRubricStore(pool).ReplaceRubric(context.Background(), subOpenPostID, scoringAdminID, []domain.Criterion{
		{Label: "Depth only", Kind: domain.CriterionKindAnswerDepth, Weight: 60, QuestionID: subQWhy,
			Config: json.RawMessage(`{"target_chars":100}`)},
		{Label: "Senior", Kind: domain.CriterionKindExperienceYears, Weight: 40, QuestionID: subQYears,
			Config: json.RawMessage(`{"min":2,"target":10}`)},
	})
	if err != nil {
		t.Fatalf("ReplaceRubric: %v", err)
	}
}

// rescoreEvents returns the payloads of the application's rescore events,
// oldest first.
func rescoreEvents(t *testing.T, pool *pgxpool.Pool, appID string) []map[string]any {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT payload FROM application_events
		WHERE application_id = $1::text::uuid AND type = $2
		ORDER BY created_at, id`, appID, domain.EventApplicationRescored)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var p map[string]any
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		out = append(out, p)
	}
	return out
}

// TestRescoreApplication scores an application against version 1 (four
// criteria), replaces the rubric with version 2 (two criteria), rescores,
// and checks the rows, the evidence and the event.
func TestRescoreApplication(t *testing.T) {
	pool := startPostgres(t)
	seedScoring(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	app, err := apps.SubmitApplication(ctx, scoredApplication())
	if err != nil {
		t.Fatalf("SubmitApplication: %v", err)
	}
	replaceWithTwoCriteria(t, pool)

	result, err := apps.RescoreApplication(ctx, app.ID, scoringAdminID)
	if err != nil {
		t.Fatalf("RescoreApplication: %v", err)
	}

	// Version 1 earned 72.5 of 100 (see TestSubmitScoresTheApplication).
	// Version 2: the answer is over 100 characters, all 60; 4 years on a
	// ramp from 2 to 10 is a quarter of 40, so 10. 70 of 100.
	wantPrevious := domain.ScoreTotals{RubricVersion: 1, Earned: 72.5, Available: 100}
	wantCurrent := domain.ScoreTotals{RubricVersion: 2, Earned: 70, Available: 100}
	if result.Previous == nil || *result.Previous != wantPrevious || result.Current != wantCurrent {
		t.Fatalf("result = %+v (previous %+v), want %+v then %+v", result, result.Previous, wantPrevious, wantCurrent)
	}

	// Four rows went, two came: exactly version 2's criteria, nothing left
	// of version 1.
	got := readScores(t, pool, app.ID)
	if len(got) != 2 || got[0].label != "Depth only" || got[1].label != "Senior" {
		t.Fatalf("scores after rescore = %+v, want version 2's two criteria", got)
	}
	for _, s := range got {
		if s.version != 2 || s.evidenceQuestionID == nil {
			t.Errorf("%s: version %d, evidence %v; want version 2 with evidence", s.label, s.version, s.evidenceQuestionID)
		}
	}

	events := rescoreEvents(t, pool, app.ID)
	if len(events) != 1 {
		t.Fatalf("got %d rescore events, want 1", len(events))
	}
	want := map[string]any{
		"trigger":             "application",
		"old_rubric_version":  float64(1),
		"old_total_earned":    72.5,
		"old_total_available": float64(100),
		"new_rubric_version":  float64(2),
		"new_total_earned":    float64(70),
		"new_total_available": float64(100),
	}
	for k, v := range want {
		if events[0][k] != v {
			t.Errorf("payload[%s] = %v, want %v", k, events[0][k], v)
		}
	}

	// The breakdown now reads version 2.
	card, err := apps.ScoreCard(ctx, app.ID)
	if err != nil || card.RubricVersion != 2 || len(card.Entries) != 2 {
		t.Errorf("ScoreCard = %+v, %v; want version 2 with two entries", card, err)
	}
}

// TestRescoreWithoutRubricKeepsNothingBroken rescores an unscored
// application on a post with no rubric: refused, and nothing written.
func TestRescoreWithoutRubricKeepsNothingBroken(t *testing.T) {
	pool := startPostgres(t)
	seedSubmission(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	app, err := apps.SubmitApplication(ctx, validApplication())
	if err != nil {
		t.Fatalf("SubmitApplication: %v", err)
	}
	if _, err := apps.RescoreApplication(ctx, app.ID, subCandidateID); !errors.Is(err, domain.ErrNoRubric) {
		t.Fatalf("RescoreApplication = %v, want ErrNoRubric", err)
	}
	if n := len(rescoreEvents(t, pool, app.ID)); n != 0 {
		t.Errorf("got %d rescore events after a refused rescore, want 0", n)
	}
	if _, err := apps.RescoreApplication(ctx, "22222222-0000-4000-8000-0000000000ee", subCandidateID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing application: %v, want ErrNotFound", err)
	}
}

// addCandidates submits n more applications to the scoring post, each from
// a new candidate, and returns their IDs.
func addCandidates(t *testing.T, pool *pgxpool.Pool, n int) []string {
	t.Helper()
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	ids := make([]string, 0, n)
	for i := range n {
		var candidateID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, full_name, role)
			VALUES ($1, 'unused', 'Bulk', 'candidate') RETURNING id::text`,
			fmt.Sprintf("bulk-%d@test", i)).Scan(&candidateID); err != nil {
			t.Fatalf("create candidate %d: %v", i, err)
		}
		in := scoredApplication()
		in.CandidateID = candidateID
		in.Answers[1].Answer = fmt.Sprint(i % 9) // years vary, so totals do
		app, err := apps.SubmitApplication(ctx, in)
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		ids = append(ids, app.ID)
	}
	return ids
}

// TestRescoreJobPost rescores every application to the post in one call.
func TestRescoreJobPost(t *testing.T) {
	pool := startPostgres(t)
	seedScoring(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	ids := addCandidates(t, pool, 12)
	replaceWithTwoCriteria(t, pool)

	result, err := apps.RescoreJobPost(ctx, subOpenPostID, scoringAdminID)
	if err != nil {
		t.Fatalf("RescoreJobPost: %v", err)
	}
	if result.RubricVersion != 2 || len(result.Results) != len(ids) {
		t.Fatalf("result: version %d, %d results; want 2 and %d", result.RubricVersion, len(result.Results), len(ids))
	}

	var rows, versions, events int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM scores),
		       (SELECT count(DISTINCT rubric_version) FROM scores),
		       (SELECT count(*) FROM application_events WHERE type = $1 AND payload->>'trigger' = 'job_post')`,
		domain.EventApplicationRescored).Scan(&rows, &versions, &events); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 2*len(ids) || versions != 1 || events != len(ids) {
		t.Errorf("%d score rows in %d versions, %d events; want %d rows, 1 version, %d events",
			rows, versions, events, 2*len(ids), len(ids))
	}

	// A post with no applications is a successful rescore of nothing, and a
	// missing post is not found.
	if _, err := pool.Exec(ctx, `DELETE FROM applications`); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if r, err := apps.RescoreJobPost(ctx, subOpenPostID, scoringAdminID); err != nil || len(r.Results) != 0 {
		t.Errorf("empty post: %+v, %v", r, err)
	}
	if _, err := apps.RescoreJobPost(ctx, "44444444-0000-4000-8000-0000000000ee", scoringAdminID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing post: %v, want ErrNotFound", err)
	}
}

// TestConcurrentRescores runs single and whole-post rescores of the same
// applications at once. Without the row locks, two deletes could each miss
// the other's freshly inserted rows and leave a doubled set behind; with
// them, every application ends with exactly one set of scores.
func TestConcurrentRescores(t *testing.T) {
	pool := startPostgres(t)
	seedScoring(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	ids := addCandidates(t, pool, 5)
	replaceWithTwoCriteria(t, pool)

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make(chan error, 20)
	)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var err error
			if i%4 == 0 {
				_, err = apps.RescoreJobPost(ctx, subOpenPostID, scoringAdminID)
			} else {
				_, err = apps.RescoreApplication(ctx, ids[i%len(ids)], scoringAdminID)
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("rescore: %v", err)
		}
	}

	var doubled int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT application_id FROM scores GROUP BY application_id HAVING count(*) <> 2
		) AS wrong`).Scan(&doubled); err != nil {
		t.Fatalf("count: %v", err)
	}
	if doubled != 0 {
		t.Errorf("%d applications do not have exactly 2 score rows", doubled)
	}
}
