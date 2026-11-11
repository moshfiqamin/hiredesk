package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// The trigger recorded in an application.rescored event: whether one
// application was rescored on its own, or as part of its whole post.
const (
	rescoreTriggerApplication = "application"
	rescoreTriggerJobPost     = "job_post"
)

// RescoreApplication replaces the scores of the application with this ID by
// scoring its stored answers against its post's current rubric, and records
// an application.rescored event naming actorID, with the totals and rubric
// versions before and after. All of it happens in one transaction.
//
// It fails with:
//
//   - an error matching domain.ErrNotFound if there is no such application;
//   - an error matching domain.ErrNoRubric if its post has no rubric, in
//     which case the existing scores are left alone;
//   - an error wrapping a *scoring.CriterionError if the rubric cannot be
//     applied.
func (s *ApplicationStore) RescoreApplication(ctx context.Context, id, actorID string) (*domain.RescoreResult, error) {
	const op = "rescore application"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	// FOR UPDATE on the application: two rescores of it at once would each
	// delete the rows they can see and insert their own, leaving both sets.
	// See lockApplications for why the post is not locked.
	const lockApplication = `SELECT a.job_post_id::text FROM applications a WHERE a.id = $1::text::uuid FOR UPDATE`

	var postID string
	if err = tx.QueryRow(ctx, lockApplication, id).Scan(&postID); err != nil {
		return nil, translateByID(op, err)
	}

	_, results, err := rescore(ctx, tx, postID, []string{id}, actorID, rescoreTriggerApplication)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, translate(op+": commit", err)
	}
	return &results[0], nil
}

// RescoreJobPost rescores every application to the job post against its
// current rubric, in one transaction, and records an application.rescored
// event for each. Either every application is rescored or none is.
//
// The work is set-based: a fixed number of statements however many
// applications there are, so five hundred cost about what five do.
//
// It fails with an error matching domain.ErrNotFound if there is no such
// post, domain.ErrNoRubric if it has no rubric, or one wrapping a
// *scoring.CriterionError if the rubric cannot be applied. A post with no
// applications is not an error: the result has no entries.
func (s *ApplicationStore) RescoreJobPost(ctx context.Context, jobPostID, actorID string) (*domain.JobPostRescore, error) {
	const op = "rescore job post"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	var postID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM job_posts WHERE id = $1::text::uuid`, jobPostID).Scan(&postID); err != nil {
		return nil, translateByID(op, err)
	}

	ids, err := lockApplications(ctx, tx, postID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	version, results, err := rescore(ctx, tx, postID, ids, actorID, rescoreTriggerJobPost)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, translate(op+": commit", err)
	}
	return &domain.JobPostRescore{JobPostID: postID, RubricVersion: version, Results: results}, nil
}

// lockApplications locks every application to the post, FOR UPDATE, and
// returns their IDs in ID order.
//
// ORDER BY id makes every bulk rescore take its locks in the same order, so
// two of them on one post queue rather than deadlock.
//
// The post itself is deliberately not locked. TransitionApplication locks an
// application and then, for an offer, the post; a rescore that locked the
// post and then the applications would take the same two locks in the other
// order, and the pair could deadlock. Without the lock a rubric replacement
// may commit while a rescore runs; the rescore then writes scores labelled
// with the version it read, which is accurate, and the replacement's
// applications_to_rescore counts them.
func lockApplications(ctx context.Context, tx pgx.Tx, jobPostID string) ([]string, error) {
	const query = `
		SELECT a.id::text FROM applications a
		WHERE a.job_post_id = $1::text::uuid
		ORDER BY a.id
		FOR UPDATE`

	rows, err := tx.Query(ctx, query, jobPostID)
	if err != nil {
		return nil, translate("lock applications", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, translate("lock applications", err)
	}
	return ids, nil
}

// rescore scores the applications ids, all to the post, against the post's
// current rubric, replaces their scores, and records an event for each. The
// caller must hold a lock on every application in ids.
//
// It returns the rubric version used and one result per ID, in the order
// given.
func rescore(ctx context.Context, tx pgx.Tx, jobPostID string, ids []string, actorID, trigger string) (int, []domain.RescoreResult, error) {
	rubric, err := loadRubric(ctx, tx, jobPostID)
	if err != nil {
		return 0, nil, err
	}
	// Checked before anything is deleted: rescoring against nothing would
	// throw the old scores away and put none in their place.
	if len(rubric.Criteria) == 0 {
		return 0, nil, domain.ErrNoRubric
	}

	// Compiled once for every application, rather than once each by
	// scoring.Score: the regexes of a keyword criterion are built one time.
	compiled, err := scoring.Compile(rubric.Criteria)
	if err != nil {
		return 0, nil, fmt.Errorf("compile rubric version %d: %w", rubric.Version, err)
	}

	results := make([]domain.RescoreResult, 0, len(ids))
	if len(ids) == 0 {
		return rubric.Version, results, nil
	}

	previous, err := currentTotals(ctx, tx, ids)
	if err != nil {
		return 0, nil, err
	}
	answers, err := answersOf(ctx, tx, ids)
	if err != nil {
		return 0, nil, err
	}

	scored := make([]scoredApplication, 0, len(ids))
	for _, id := range ids {
		// The engine sees the stored answers, with their IDs, exactly as it
		// saw the submitted ones, so the evidence links are rebuilt too.
		res := compiled.Score(answers[id])
		scored = append(scored, scoredApplication{ID: id, Breakdown: res.Breakdown})

		result := domain.RescoreResult{ApplicationID: id, Current: totalsOf(rubric.Version, res.Breakdown)}
		if p, ok := previous[id]; ok {
			result.Previous = &p
		}
		results = append(results, result)
	}

	// Delete, then insert: the new rubric may have more criteria, fewer, or
	// different ones, so the old rows cannot be updated into the new ones.
	// Both statements are in this transaction, so no reader ever sees the
	// application with no scores.
	const deleteScores = `DELETE FROM scores WHERE application_id = ANY($1::text[]::uuid[])`
	if _, err = tx.Exec(ctx, deleteScores, ids); err != nil {
		return 0, nil, translate("delete scores", err)
	}
	if err = insertScores(ctx, tx, rubric.Version, scored); err != nil {
		return 0, nil, err
	}
	if err = insertRescoreEvents(ctx, tx, actorID, trigger, results); err != nil {
		return 0, nil, err
	}
	return rubric.Version, results, nil
}

// currentTotals returns, for each of the applications that has scores, the
// totals of its latest rubric version. An unscored application has no
// entry.
func currentTotals(ctx context.Context, tx pgx.Tx, ids []string) (map[string]domain.ScoreTotals, error) {
	// = ANY($1) with one array parameter: one statement for every
	// application, not one each. The subquery keeps to the latest version,
	// as ScoreCard does.
	const query = `
		SELECT s.application_id::text, s.rubric_version, sum(s.earned)::float8, sum(s.available)::float8
		FROM scores s
		WHERE s.application_id = ANY($1::text[]::uuid[])
		  AND s.rubric_version = (
			SELECT max(latest.rubric_version) FROM scores latest
			WHERE latest.application_id = s.application_id)
		GROUP BY s.application_id, s.rubric_version`

	rows, err := tx.Query(ctx, query, ids)
	if err != nil {
		return nil, translate("current totals", err)
	}
	totals := make(map[string]domain.ScoreTotals, len(ids))
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var (
			id string
			t  domain.ScoreTotals
		)
		scanErr := row.Scan(&id, &t.RubricVersion, &t.Earned, &t.Available)
		totals[id] = t
		return struct{}{}, scanErr
	})
	if err != nil {
		return nil, translate("current totals", err)
	}
	return totals, nil
}

// answersOf returns the stored answers of each application, keyed by
// application ID. An application with no answers has no entry, which the
// engine treats as every question unanswered.
func answersOf(ctx context.Context, tx pgx.Tx, ids []string) (map[string][]domain.Answer, error) {
	const query = `
		SELECT aa.id::text, aa.application_id::text, aa.question_id::text, aa.answer_text, aa.created_at
		FROM application_answers aa
		WHERE aa.application_id = ANY($1::text[]::uuid[])`

	rows, err := tx.Query(ctx, query, ids)
	if err != nil {
		return nil, translate("load answers", err)
	}
	answers := make(map[string][]domain.Answer, len(ids))
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var a domain.Answer
		scanErr := row.Scan(&a.ID, &a.ApplicationID, &a.QuestionID, &a.Text, &a.CreatedAt)
		answers[a.ApplicationID] = append(answers[a.ApplicationID], a)
		return struct{}{}, scanErr
	})
	if err != nil {
		return nil, translate("load answers", err)
	}
	return answers, nil
}

// insertRescoreEvents records one application.rescored event per result, in
// one statement.
//
// The payload keeps both totals and both versions, because the score rows
// that held the old ones have just been deleted: after this transaction the
// event is the only record that the application ever scored differently.
func insertRescoreEvents(ctx context.Context, tx pgx.Tx, actorID, trigger string, results []domain.RescoreResult) error {
	n := len(results)
	ids := make([]string, n)
	// Pointers, so an application with no previous score sends SQL NULLs,
	// which jsonb_build_object writes as JSON null.
	oldVersions := make([]*int, n)
	oldEarned := make([]*float64, n)
	oldAvailable := make([]*float64, n)
	newEarned := make([]float64, n)
	newAvailable := make([]float64, n)
	var newVersion int
	for i, r := range results {
		ids[i] = r.ApplicationID
		if r.Previous != nil {
			// Copies, so each pointer has its own value rather than all of
			// them pointing into one reused variable.
			v, e, a := r.Previous.RubricVersion, r.Previous.Earned, r.Previous.Available
			oldVersions[i], oldEarned[i], oldAvailable[i] = &v, &e, &a
		}
		newEarned[i], newAvailable[i] = r.Current.Earned, r.Current.Available
		newVersion = r.Current.RubricVersion
	}

	const query = `
		INSERT INTO application_events (application_id, actor_id, type, payload)
		SELECT t.application_id::uuid, $1::text::uuid, $2,
		       jsonb_build_object(
		           'trigger',             $3::text,
		           'old_rubric_version',  t.old_version,
		           'old_total_earned',    t.old_earned,
		           'old_total_available', t.old_available,
		           'new_rubric_version',  $4::int,
		           'new_total_earned',    t.new_earned,
		           'new_total_available', t.new_available)
		FROM unnest($5::text[], $6::int[], $7::float8[], $8::float8[], $9::float8[], $10::float8[])
			AS t(application_id, old_version, old_earned, old_available, new_earned, new_available)`

	if _, err := tx.Exec(ctx, query, actorID, domain.EventApplicationRescored, trigger, newVersion,
		ids, oldVersions, oldEarned, oldAvailable, newEarned, newAvailable); err != nil {
		return translate("record rescore events", err)
	}
	return nil
}
