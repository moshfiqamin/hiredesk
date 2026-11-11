//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// TestSubmitApplicationOutcomes runs SubmitApplication against a real
// database: what a success stores, and that every refusal stores nothing.
func TestSubmitApplicationOutcomes(t *testing.T) {
	pool := startPostgres(t)
	seedSubmission(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	const (
		draftPost  = "44444444-0000-4000-8000-000000000002"
		closedPost = "44444444-0000-4000-8000-000000000003"
		nowhere    = "44444444-0000-4000-8000-0000000000ff"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO job_posts (id, title, status) VALUES
			('`+draftPost+`',  'Draft',  'draft'),
			('`+closedPost+`', 'Closed', 'closed')`); err != nil {
		t.Fatalf("seed posts: %v", err)
	}

	// Run in order: the refusals first, while there is nothing in the
	// tables, so "nothing was written" is a count of zero; then the success;
	// then the duplicate, which needs the success to exist.
	refusals := []struct {
		name  string
		edit  func(in *domain.NewApplication)
		check func(err error) bool
	}{
		{"a closed post", func(in *domain.NewApplication) { in.JobPostID = closedPost }, func(err error) bool {
			var e *domain.NotAcceptingError
			return errors.As(err, &e) && e.Status == domain.JobStatusClosed
		}},
		{"a draft post", func(in *domain.NewApplication) { in.JobPostID = draftPost }, func(err error) bool {
			return errors.Is(err, domain.ErrNotFound)
		}},
		{"a post that does not exist", func(in *domain.NewApplication) { in.JobPostID = nowhere }, func(err error) bool {
			return errors.Is(err, domain.ErrNotFound)
		}},
		{"invalid answers and CV link", func(in *domain.NewApplication) {
			in.CVURL = "cv.pdf"
			in.Answers = []domain.NewAnswer{{QuestionID: subQWhy, Answer: "short"}}
		}, func(err error) bool {
			var e *domain.SubmissionError
			// too short, and the number missing
			return errors.As(err, &e) && len(e.Answers) == 2 && e.CVURL != ""
		}},
	}
	for _, tt := range refusals {
		t.Run("refused: "+tt.name, func(t *testing.T) {
			in := validApplication()
			tt.edit(&in)

			_, err := apps.SubmitApplication(ctx, in)
			if !tt.check(err) {
				t.Fatalf("err = %v (%T)", err, err)
			}
			var total int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM applications`).Scan(&total); err != nil {
				t.Fatalf("count: %v", err)
			}
			if total != 0 {
				t.Errorf("%d applications stored after a refusal", total)
			}
		})
	}

	t.Run("a valid submission stores all three parts", func(t *testing.T) {
		in := validApplication()
		// Submitted in reverse form order: the answers come back as sent.
		in.Answers[0], in.Answers[1] = in.Answers[1], in.Answers[0]

		app, err := apps.SubmitApplication(ctx, in)
		if err != nil {
			t.Fatalf("SubmitApplication: %v", err)
		}
		if app.ID == "" || app.Status != domain.StatusSubmitted || app.CandidateID != subCandidateID || app.JobPostID != subOpenPostID {
			t.Errorf("application = %+v", app)
		}
		if len(app.Answers) != 2 || app.Answers[0].QuestionID != subQYears || app.Answers[1].QuestionID != subQWhy {
			t.Fatalf("answers = %+v, want years then why", app.Answers)
		}
		for _, a := range app.Answers {
			if a.ID == "" || a.ApplicationID != app.ID {
				t.Errorf("answer = %+v", a)
			}
		}

		var (
			actor, eventType, payloadPost string
			payloadAnswers                int
		)
		err = pool.QueryRow(ctx, `
			SELECT actor_id::text, type, payload->>'job_post_id', (payload->>'answers')::int
			FROM application_events WHERE application_id = $1::text::uuid`, app.ID,
		).Scan(&actor, &eventType, &payloadPost, &payloadAnswers)
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		if actor != subCandidateID || eventType != domain.EventApplicationSubmitted ||
			payloadPost != subOpenPostID || payloadAnswers != 2 {
			t.Errorf("event = actor %s, type %s, payload post %s, answers %d", actor, eventType, payloadPost, payloadAnswers)
		}
	})

	t.Run("a second submission is a duplicate", func(t *testing.T) {
		_, err := apps.SubmitApplication(ctx, validApplication())
		if !errors.Is(err, domain.ErrAlreadyApplied) || !errors.Is(err, domain.ErrDuplicate) {
			t.Fatalf("err = %v, want ErrAlreadyApplied, which also matches ErrDuplicate", err)
		}
		if a, ans, ev := countRows(t, pool); a != 1 || ans != 2 || ev != 1 {
			t.Errorf("%d applications, %d answers, %d events; want the first submission's 1, 2, 1", a, ans, ev)
		}
	})
}
