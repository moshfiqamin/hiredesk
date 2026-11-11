//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// Fixed IDs for the submission tests. Each test starts its own container, so
// these cannot collide with the matrix's rows.
const (
	subCandidateID = "00000000-0000-4000-8000-0000000000d1"
	subOpenPostID  = "44444444-0000-4000-8000-000000000001"
	subQWhy        = "55555555-0000-4000-8000-000000000001"
	subQYears      = "55555555-0000-4000-8000-000000000002"
)

// seedSubmission inserts one candidate and one open post whose form has two
// required questions: a text answer of at least 10 characters, and a number.
func seedSubmission(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	const fixtures = `
		INSERT INTO users (id, email, password_hash, full_name, role)
		VALUES ('` + subCandidateID + `', 'applicant@test', 'unused', 'Applicant', 'candidate');

		INSERT INTO job_posts (id, title, status)
		VALUES ('` + subOpenPostID + `', 'Go Engineer', 'open');

		INSERT INTO questions (id, job_post_id, prompt, kind, required, min_length, position) VALUES
			('` + subQWhy + `',   '` + subOpenPostID + `', 'Why this role?',    'text',   true, 10, 1),
			('` + subQYears + `', '` + subOpenPostID + `', 'Years of Go?',      'number', true, 0,  2);`

	if _, err := pool.Exec(context.Background(), fixtures); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// validApplication answers both questions of the seeded post.
func validApplication() domain.NewApplication {
	return domain.NewApplication{
		JobPostID:   subOpenPostID,
		CandidateID: subCandidateID,
		CVURL:       "https://cv.test/applicant.pdf",
		Answers: []domain.NewAnswer{
			{QuestionID: subQWhy, Answer: "I enjoy building reliable APIs."},
			{QuestionID: subQYears, Answer: "4"},
		},
	}
}

// countRows returns how many rows of the application, its answers and its
// events exist for the seeded candidate and post.
func countRows(t *testing.T, pool *pgxpool.Pool) (apps, answers, events int) {
	t.Helper()

	// Answers and events are counted through any application at all, not
	// through the candidate's: a row orphaned by a half-finished write would
	// have no application to join to, and must still be counted.
	const query = `
		SELECT
			(SELECT count(*) FROM applications
			 WHERE job_post_id = $1::text::uuid AND candidate_id = $2::text::uuid),
			(SELECT count(*) FROM application_answers),
			(SELECT count(*) FROM application_events)`

	if err := pool.QueryRow(context.Background(), query, subOpenPostID, subCandidateID).
		Scan(&apps, &answers, &events); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return apps, answers, events
}

// TestSubmitRollback makes the last statement of SubmitApplication's
// transaction fail, after the application and both answers have been
// inserted, and checks that none of them survives.
//
// The failure comes from a trigger on application_events, so the code under
// test is the production code with no hook added for testing. The trigger's
// message reports how many answers it could see, which proves the earlier
// inserts really had happened inside the transaction before it was undone.
func TestSubmitRollback(t *testing.T) {
	pool := startPostgres(t)
	seedSubmission(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	// BEFORE INSERT runs as part of the INSERT statement, so raising here
	// fails that statement exactly as a constraint violation would.
	const installTrigger = `
		CREATE FUNCTION fail_application_event() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'forced failure: % answers already inserted',
				(SELECT count(*) FROM application_answers WHERE application_id = NEW.application_id);
		END
		$$;

		CREATE TRIGGER fail_application_event
			BEFORE INSERT ON application_events
			FOR EACH ROW EXECUTE FUNCTION fail_application_event();`

	if _, err := pool.Exec(ctx, installTrigger); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	app, err := apps.SubmitApplication(ctx, validApplication())
	if err == nil {
		t.Fatalf("SubmitApplication succeeded with %+v, want the forced failure", app)
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("err = %v, want the trigger's *pgconn.PgError in the chain", err)
	}
	if want := "forced failure: 2 answers already inserted"; pgErr.Message != want {
		t.Fatalf("trigger said %q, want %q: the failure must come after both answers", pgErr.Message, want)
	}

	if a, ans, ev := countRows(t, pool); a != 0 || ans != 0 || ev != 0 {
		t.Fatalf("after the failed submit: %d applications, %d answers, %d events; want none of any", a, ans, ev)
	}

	// Without the trigger the same submission goes through. Had the failed
	// attempt left its application behind, this would be refused as a
	// duplicate by the unique constraint on (job_post_id, candidate_id).
	if _, err := pool.Exec(ctx, `DROP TRIGGER fail_application_event ON application_events`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if _, err := apps.SubmitApplication(ctx, validApplication()); err != nil {
		t.Fatalf("SubmitApplication after removing the trigger: %v", err)
	}
	if a, ans, ev := countRows(t, pool); a != 1 || ans != 2 || ev != 1 {
		t.Errorf("after the retry: %d applications, %d answers, %d events; want 1, 2, 1", a, ans, ev)
	}
}
