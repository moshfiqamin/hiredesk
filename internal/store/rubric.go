package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// criterionColumns is the column list every rubric query returns, in the
// order scanCriterion reads them. A NULL question_id comes back as "", which
// is how domain.Criterion spells "not tied to one answer".
const criterionColumns = `rc.id::text, rc.label, rc.kind, rc.weight, rc.config,
	coalesce(rc.question_id::text, ''), rc.rubric_version`

// currentVersionFilter limits rubric_criteria rc to the post's current
// version: its highest. $1 is the job post's ID.
const currentVersionFilter = `rc.job_post_id = $1::text::uuid
	AND rc.rubric_version = (
		SELECT max(latest.rubric_version) FROM rubric_criteria latest
		WHERE latest.job_post_id = $1::text::uuid)`

// emptyConfig is what a criterion with no parameters stores. config is
// NOT NULL, and pgx sends a nil json.RawMessage as NULL.
var emptyConfig = json.RawMessage(`{}`)

// RubricStore reads and replaces the scoring rubrics of job posts.
//
// A rubric is never edited in place. Replacing one inserts every criterion
// again at the next rubric_version and leaves the old rows where they are,
// because the scores already stored point at them.
type RubricStore struct {
	db *pgxpool.Pool
}

// NewRubricStore returns a RubricStore that runs its queries on db.
func NewRubricStore(db *pgxpool.Pool) *RubricStore {
	return &RubricStore{db: db}
}

// GetRubricByJobPost returns the current version of the job post's rubric,
// whatever the post's status, or an error matching domain.ErrNotFound if
// there is no such post. A post with no rubric gives Version 0 and no
// criteria.
//
// It is for admins: drafts are not hidden.
func (s *RubricStore) GetRubricByJobPost(ctx context.Context, jobPostID string) (*domain.Rubric, error) {
	const op = "get rubric"

	// Checked first, because "no rubric" and "no such post" would otherwise
	// both be an empty list.
	var exists bool
	const existsQuery = `SELECT EXISTS (SELECT 1 FROM job_posts WHERE id = $1::text::uuid)`
	if err := s.db.QueryRow(ctx, existsQuery, jobPostID).Scan(&exists); err != nil {
		return nil, translateByID(op, err)
	}
	if !exists {
		return nil, fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}

	rubric, err := loadRubric(ctx, s.db, jobPostID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return rubric, nil
}

// ReplaceRubric makes criteria the job post's rubric, as version current + 1,
// in one transaction, and records a rubric_replaced event naming actorID.
// The criteria of earlier versions are kept: scores refer to them.
//
// It does not rescore anything. Applications already scored keep the score
// and the rubric_version they were given; the result says how many there are.
//
// The caller is expected to have run scoring.ValidateCriteria. ReplaceRubric
// adds the checks that need the database, under the post's lock. It fails
// with:
//
//   - an error matching domain.ErrNotFound if there is no such post;
//   - a *domain.ValidationError if a criterion names a question that is not
//     on the post's form, or one of a kind it cannot score.
//
// In neither case is anything written.
func (s *RubricStore) ReplaceRubric(ctx context.Context, jobPostID, actorID string, criteria []domain.Criterion) (*domain.RubricReplacement, error) {
	const op = "replace rubric"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	// Serialises replacements of one post's rubric. Without it, two
	// transactions could read the same highest version and both insert the
	// next one; the unique constraint from 000011 would then refuse the
	// second, but as a 409 rather than by waiting its turn.
	//
	// FOR NO KEY UPDATE, the weakest lock two replacements cannot share. It
	// waits for a submission in progress (FOR SHARE), so a submission scores
	// against one version or the other, never against a half-written one.
	const lockPost = `SELECT id::text FROM job_posts WHERE id = $1::text::uuid FOR NO KEY UPDATE`

	var postID string
	if err = tx.QueryRow(ctx, lockPost, jobPostID).Scan(&postID); err != nil {
		return nil, translateByID(op, err)
	}

	// Under the lock, so a question cannot be deleted between this check and
	// the insert that refers to it.
	form, err := listQuestions(ctx, tx, postID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if err = scoring.CheckAgainstForm(criteria, form); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var previous int
	const versionQuery = `SELECT coalesce(max(rubric_version), 0) FROM rubric_criteria WHERE job_post_id = $1::text::uuid`
	if err = tx.QueryRow(ctx, versionQuery, postID).Scan(&previous); err != nil {
		return nil, translate(op+": current version", err)
	}
	version := previous + 1

	inserted, err := insertCriteria(ctx, tx, postID, version, criteria)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Every application already on the post was scored against an earlier
	// version, or not at all. Served by applications_job_post_id_status_idx.
	var stale int
	const staleQuery = `SELECT count(*)::int FROM applications WHERE job_post_id = $1::text::uuid`
	if err = tx.QueryRow(ctx, staleQuery, postID).Scan(&stale); err != nil {
		return nil, translate(op+": count applications", err)
	}

	// Same transaction, so the trail shows a new version if and only if one
	// exists. from is 0 when there was no rubric before.
	const insertEvent = `
		INSERT INTO job_post_events (job_post_id, actor_id, type, payload)
		VALUES ($1::text::uuid, $2::text::uuid, $3,
		        jsonb_build_object('from_version', $4::int, 'to_version', $5::int, 'criteria', $6::int))`

	if _, err = tx.Exec(ctx, insertEvent, postID, actorID, domain.EventRubricReplaced,
		previous, version, len(inserted)); err != nil {
		return nil, translate(op+": record event", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, translate(op+": commit", err)
	}
	return &domain.RubricReplacement{
		Rubric:                domain.Rubric{JobPostID: postID, Version: version, Criteria: inserted},
		PreviousVersion:       previous,
		ApplicationsToRescore: stale,
	}, nil
}

// insertCriteria stores criteria as version of the post's rubric, numbered
// from position 1 in the order given, in one statement, and returns them as
// stored, in that order.
func insertCriteria(ctx context.Context, tx pgx.Tx, jobPostID string, version int, criteria []domain.Criterion) ([]domain.Criterion, error) {
	// One slice per column, because unnest takes parallel arrays. The jsonb
	// configs travel as a text[] and are cast row by row: pgx has no array
	// encoding for a slice of json.RawMessage.
	labels := make([]string, len(criteria))
	kinds := make([]string, len(criteria))
	weights := make([]float64, len(criteria))
	configs := make([]string, len(criteria))
	questionIDs := make([]string, len(criteria))
	for i, c := range criteria {
		labels[i] = c.Label
		kinds[i] = c.Kind
		weights[i] = c.Weight
		config := c.Config
		if len(config) == 0 {
			config = emptyConfig
		}
		configs[i] = string(config)
		questionIDs[i] = c.QuestionID
	}

	// WITH ORDINALITY numbers the unnested rows from 1 in array order, which
	// is the position. NULLIF turns "" into NULL for a criterion that reads
	// no single question. weight goes through float8 into numeric(6, 2),
	// which rounds it to two decimals; ValidateCriteria has already refused
	// a weight with more.
	const query = `
		INSERT INTO rubric_criteria AS rc
			(job_post_id, label, kind, weight, config, question_id, rubric_version, position)
		SELECT $1::text::uuid, t.label, t.kind, t.weight, t.config::jsonb,
		       NULLIF(t.question_id, '')::uuid, $2, t.position
		FROM unnest($3::text[], $4::text[], $5::float8[], $6::text[], $7::text[])
			WITH ORDINALITY AS t(label, kind, weight, config, question_id, position)
		RETURNING rc.position, ` + criterionColumns

	rows, err := tx.Query(ctx, query, jobPostID, version, labels, kinds, weights, configs, questionIDs)
	if err != nil {
		return nil, translate("insert criteria", err)
	}

	// RETURNING promises no order, so each row goes back to the slot its
	// position names.
	stored := make([]domain.Criterion, len(criteria))
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var position int
		var c domain.Criterion
		if scanErr := row.Scan(&position, &c.ID, &c.Label, &c.Kind, &c.Weight, &c.Config, &c.QuestionID, &c.RubricVersion); scanErr != nil {
			return struct{}{}, scanErr
		}
		stored[position-1] = c
		return struct{}{}, nil
	})
	if err != nil {
		return nil, translate("insert criteria", err)
	}
	return stored, nil
}

// loadRubric returns the current version of the post's rubric, in order. A
// post with no criteria, or no such post, gives Version 0 and no criteria.
//
// It takes a queryer so that SubmitApplication can call it inside its own
// transaction, and GetRubricByJobPost on the pool.
func loadRubric(ctx context.Context, db queryer, jobPostID string) (*domain.Rubric, error) {
	const query = `SELECT ` + criterionColumns + `
		FROM rubric_criteria rc
		WHERE ` + currentVersionFilter + `
		ORDER BY rc.position`

	rows, err := db.Query(ctx, query, jobPostID)
	if err != nil {
		return nil, translate("load rubric", err)
	}
	criteria, err := pgx.CollectRows(rows, scanCriterion)
	if err != nil {
		return nil, translate("load rubric", err)
	}

	rubric := &domain.Rubric{JobPostID: jobPostID, Criteria: criteria}
	if len(criteria) > 0 {
		rubric.Version = criteria[0].RubricVersion
	} else {
		// Non-nil, so it encodes as [] rather than null.
		rubric.Criteria = []domain.Criterion{}
	}
	return rubric, nil
}

// scanCriterion reads one row in criterionColumns order.
//
// weight is numeric, which pgx converts to a float64. config is jsonb: pgx
// strips the binary format's version byte and hands the JSON text to
// json.RawMessage's UnmarshalJSON, which keeps a copy of it unparsed.
func scanCriterion(row pgx.CollectableRow) (domain.Criterion, error) {
	var c domain.Criterion
	err := row.Scan(&c.ID, &c.Label, &c.Kind, &c.Weight, &c.Config, &c.QuestionID, &c.RubricVersion)
	return c, err
}

// scoredApplication is one application and the engine's breakdown for it.
type scoredApplication struct {
	ID        string
	Breakdown []scoring.CriterionScore
}

// insertScores stores one score row per criterion of every application in
// scored, all produced by version of the rubric, in one statement, whether
// that is one application at submission or five hundred in a rescore.
//
// The evidence link is the engine's EvidenceAnswerID: the ID of a stored
// answer row, or "" when no single answer was the evidence, which NULLIF
// stores as NULL.
func insertScores(ctx context.Context, tx pgx.Tx, version int, scored []scoredApplication) error {
	var (
		applicationIDs, criterionIDs, details, evidence []string
		earned, available                               []float64
	)
	for _, app := range scored {
		for _, cs := range app.Breakdown {
			applicationIDs = append(applicationIDs, app.ID)
			criterionIDs = append(criterionIDs, cs.CriterionID)
			// Rounded here rather than left to numeric(6, 2), so the stored
			// value is exactly what this code decided, and the column's sum
			// is the total the breakdown shows.
			earned = append(earned, roundCents(cs.Earned))
			available = append(available, roundCents(cs.Available))
			details = append(details, cs.Detail)
			evidence = append(evidence, cs.EvidenceAnswerID)
		}
	}
	if len(criterionIDs) == 0 {
		return nil
	}

	const query = `
		INSERT INTO scores (application_id, criterion_id, earned, available, detail, evidence_answer_id, rubric_version)
		SELECT t.application_id::uuid, t.criterion_id::uuid, t.earned, t.available, t.detail,
		       NULLIF(t.evidence_answer_id, '')::uuid, $1
		FROM unnest($2::text[], $3::text[], $4::float8[], $5::float8[], $6::text[], $7::text[])
			AS t(application_id, criterion_id, earned, available, detail, evidence_answer_id)`

	if _, err := tx.Exec(ctx, query, version, applicationIDs, criterionIDs, earned, available, details, evidence); err != nil {
		return translate("insert scores", err)
	}
	return nil
}

// totalsOf sums a breakdown the way the stored rows will sum: each value
// rounded to cents first, as insertScores stores it.
func totalsOf(version int, breakdown []scoring.CriterionScore) domain.ScoreTotals {
	t := domain.ScoreTotals{RubricVersion: version}
	for _, cs := range breakdown {
		t.Earned += roundCents(cs.Earned)
		t.Available += roundCents(cs.Available)
	}
	t.Earned, t.Available = roundCents(t.Earned), roundCents(t.Available)
	return t
}

// roundCents rounds to two decimal places, half away from zero, as numeric
// does.
func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}
