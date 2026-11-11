package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// jobPostColumns is the column list every query here returns, in the order
// scanJobPost reads them.
//
// The salaries are numeric(12, 2) in the table and whole minor units in Go.
// Multiplying by 100 in numeric arithmetic is exact, so the cast to bigint
// never rounds, and no float is involved in either direction.
const jobPostColumns = `id::text, title, description, employment_type, location, vacancies,
	(salary_min * 100)::bigint, (salary_max * 100)::bigint,
	status, closes_at, created_by::text, created_at, updated_at`

// questionColumns is the column list listQuestions returns, in the order it
// scans them.
const questionColumns = `id::text, job_post_id::text, prompt, kind, choices, required, min_length, position`

// queryer is the one method listQuestions needs. Both *pgxpool.Pool and
// pgx.Tx have it, so the same function runs on the pool or inside a
// transaction.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// JobPostStore reads and writes the job_posts table, and reads questions.
type JobPostStore struct {
	db *pgxpool.Pool
}

// NewJobPostStore returns a JobPostStore that runs its queries on db.
func NewJobPostStore(db *pgxpool.Pool) *JobPostStore {
	return &JobPostStore{db: db}
}

// CreateJobPost inserts a draft job post and returns the stored row,
// including the ID, status and timestamps the database assigned.
//
// It does not validate in; the caller is expected to have called in.Validate.
// The table's own constraints remain the final guard.
func (s *JobPostStore) CreateJobPost(ctx context.Context, in domain.NewJobPost) (*domain.JobPost, error) {
	// status is not in the column list, so the column default applies and
	// every post starts as a draft, whatever the caller intended.
	//
	// A nil *Money arrives as NULL, and NULL / 100 is NULL, so an unset
	// salary stays unset without a branch here.
	const query = `
		INSERT INTO job_posts (
			title, description, employment_type, location, vacancies,
			salary_min, salary_max, closes_at, created_by
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6::bigint::numeric / 100, $7::bigint::numeric / 100, $8, $9::text::uuid
		)
		RETURNING ` + jobPostColumns

	post, err := scanJobPost(s.db.QueryRow(ctx, query,
		in.Title, in.Description, in.EmploymentType, in.Location, in.Vacancies,
		in.SalaryMin, in.SalaryMax, in.ClosesAt, in.CreatedBy,
	))
	if err != nil {
		return nil, translate("create job post", err)
	}

	// A post that has just been created has no questions. Set to an empty
	// slice so every function that returns a single post returns the same
	// shape.
	post.Questions = []domain.Question{}
	return post, nil
}

// GetJobPostByID returns the job post with this ID and its questions in
// display order, or an error matching domain.ErrNotFound. An ID that is not
// a UUID is reported as not found.
//
// With includeDrafts false a draft is reported as not found too. The check is
// in the SQL, so a hidden draft and a missing post run the same query and
// return the same nothing.
func (s *JobPostStore) GetJobPostByID(ctx context.Context, id string, includeDrafts bool) (*domain.JobPost, error) {
	// $2 is a boolean. When it is true the OR is satisfied for every row;
	// when it is false only the status comparison can satisfy it.
	const query = `
		SELECT ` + jobPostColumns + `
		FROM job_posts
		WHERE id = $1::text::uuid
		  AND ($2 OR status <> 'draft')`

	post, err := scanJobPost(s.db.QueryRow(ctx, query, id, includeDrafts))
	if err != nil {
		return nil, translateByID("get job post", err)
	}

	post.Questions, err = listQuestions(ctx, s.db, post.ID)
	if err != nil {
		return nil, fmt.Errorf("get job post: %w", err)
	}
	return post, nil
}

// UpdateJobPost applies patch to the job post with this ID and returns the
// post as stored, with its questions. Fields patch leaves nil keep their
// stored values.
//
// It fails with:
//
//   - an error matching domain.ErrNotFound if there is no such post;
//   - a *domain.ValidationError if the post would break a rule once the patch
//     is applied.
//
// In neither case is anything written.
func (s *JobPostStore) UpdateJobPost(ctx context.Context, id string, patch domain.JobPostPatch) (*domain.JobPost, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("update job post: begin: %w", err)
	}
	// Undoes everything on an early return. After a successful Commit it
	// does nothing and returns ErrTxClosed, so its error is of no use.
	defer tx.Rollback(ctx) //nolint:errcheck // See above.

	// FOR UPDATE holds the row until the transaction ends. The rules are
	// checked against the row as read here, so nothing may change it between
	// this read and the write below. Without the lock, two requests could
	// each raise one end of the salary range past the other, and both would
	// pass a check made against the old row.
	const selectQuery = `
		SELECT ` + jobPostColumns + `
		FROM job_posts
		WHERE id = $1::text::uuid
		FOR UPDATE`

	post, err := scanJobPost(tx.QueryRow(ctx, selectQuery, id))
	if err != nil {
		return nil, translateByID("update job post", err)
	}

	post.Apply(patch)
	if err = post.Validate(); err != nil {
		return nil, fmt.Errorf("update job post: %w", err)
	}

	// An empty patch writes nothing, so updated_at does not move for a
	// request that changed nothing.
	if !patch.IsEmpty() {
		// Every editable column is written from the merged post, so one
		// statement serves any combination of fields. The alternative is to
		// build the SET list from the patch, which means assembling SQL text
		// at run time.
		const updateQuery = `
			UPDATE job_posts
			SET title = $2, description = $3, employment_type = $4, location = $5,
				vacancies = $6,
				salary_min = $7::bigint::numeric / 100,
				salary_max = $8::bigint::numeric / 100,
				closes_at = $9,
				updated_at = now()
			WHERE id = $1::text::uuid
			RETURNING ` + jobPostColumns

		post, err = scanJobPost(tx.QueryRow(ctx, updateQuery,
			post.ID, post.Title, post.Description, post.EmploymentType, post.Location,
			post.Vacancies, post.SalaryMin, post.SalaryMax, post.ClosesAt,
		))
		if err != nil {
			return nil, translate("update job post", err)
		}
	}

	// On tx, not s.db: the pool would hand out a second connection while
	// this one is still held.
	post.Questions, err = listQuestions(ctx, tx, post.ID)
	if err != nil {
		return nil, fmt.Errorf("update job post: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("update job post: commit: %w", err)
	}
	return post, nil
}

// TransitionJobPost moves the job post with this ID to status to, records
// who did it in job_post_events, and returns the post as stored, with its
// questions.
//
// It fails with:
//
//   - an error matching domain.ErrNotFound if there is no such post;
//   - a *domain.TransitionError if the lifecycle does not allow the move;
//   - a *domain.NotReadyError if to is open and the post has no questions or
//     no usable rubric.
//
// In none of those cases is anything written. When it succeeds, the status
// change and its event are committed together or not at all.
func (s *JobPostStore) TransitionJobPost(ctx context.Context, id, to, actorID string) (*domain.JobPost, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("transition job post: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	// The lock serialises status changes to one post. Two admins pressing
	// "open" at once both read draft without it, and both would pass the
	// check and record an event.
	//
	// It also holds off new questions and criteria: inserting a row that
	// references this post takes a FOR KEY SHARE lock on it, which waits for
	// FOR UPDATE. The counts below cannot be overtaken by an insert.
	const selectQuery = `
		SELECT ` + jobPostColumns + `
		FROM job_posts
		WHERE id = $1::text::uuid
		FOR UPDATE`

	post, err := scanJobPost(tx.QueryRow(ctx, selectQuery, id))
	if err != nil {
		return nil, translateByID("transition job post", err)
	}
	from := post.Status

	if err = domain.CheckTransition(from, to); err != nil {
		return nil, fmt.Errorf("transition job post: %w", err)
	}

	if to == domain.JobStatusOpen {
		readiness, readErr := openReadiness(ctx, tx, post.ID)
		if readErr != nil {
			return nil, fmt.Errorf("transition job post: %w", readErr)
		}
		if err = domain.CheckReadyToOpen(readiness); err != nil {
			return nil, fmt.Errorf("transition job post: %w", err)
		}
	}

	const updateQuery = `
		UPDATE job_posts
		SET status = $2, updated_at = now()
		WHERE id = $1::text::uuid
		RETURNING ` + jobPostColumns

	post, err = scanJobPost(tx.QueryRow(ctx, updateQuery, post.ID, to))
	if err != nil {
		return nil, translate("transition job post", err)
	}

	// In the same transaction as the UPDATE, so the log can never show a
	// change that did not happen, nor miss one that did. jsonb_build_object
	// assembles the payload in SQL from typed parameters, so no JSON is built
	// by string concatenation.
	const eventQuery = `
		INSERT INTO job_post_events (job_post_id, actor_id, type, payload)
		VALUES ($1::text::uuid, $2::text::uuid, 'status_changed',
		        jsonb_build_object('from', $3::text, 'to', $4::text))`

	if _, err = tx.Exec(ctx, eventQuery, post.ID, actorID, from, to); err != nil {
		return nil, translate("transition job post: record event", err)
	}

	post.Questions, err = listQuestions(ctx, tx, post.ID)
	if err != nil {
		return nil, fmt.Errorf("transition job post: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("transition job post: commit: %w", err)
	}
	return post, nil
}

// openReadiness counts what opening the post depends on, in one round trip.
//
// The rubric counted is the current one: the rows at the post's highest
// rubric_version. Earlier versions belong to applications already scored.
func openReadiness(ctx context.Context, tx pgx.Tx, jobPostID string) (domain.OpenReadiness, error) {
	// An aggregate with no GROUP BY returns exactly one row even when no row
	// matches, so a post with no rubric gets counts of 0 rather than
	// pgx.ErrNoRows. FILTER restricts one aggregate without a second scan.
	const query = `
		SELECT
			(SELECT count(*) FROM questions q WHERE q.job_post_id = $1::text::uuid),
			count(*),
			count(*) FILTER (WHERE rc.weight > 0)
		FROM rubric_criteria rc
		WHERE rc.job_post_id = $1::text::uuid
		  AND rc.rubric_version = (
			SELECT max(rubric_version) FROM rubric_criteria WHERE job_post_id = $1::text::uuid
		  )`

	var r domain.OpenReadiness
	if err := tx.QueryRow(ctx, query, jobPostID).Scan(&r.Questions, &r.Criteria, &r.WeightedCriteria); err != nil {
		return domain.OpenReadiness{}, translate("count questions and rubric", err)
	}
	return r, nil
}

// ListJobPosts returns job posts, newest first, without their questions.
// With includeDrafts false, drafts are left out by the query itself.
func (s *JobPostStore) ListJobPosts(ctx context.Context, includeDrafts bool) ([]domain.JobPost, error) {
	// id breaks ties between posts created in the same microsecond, so the
	// order is stable across calls.
	const query = `
		SELECT ` + jobPostColumns + `
		FROM job_posts
		WHERE ($1 OR status <> 'draft')
		ORDER BY created_at DESC, id`

	rows, err := s.db.Query(ctx, query, includeDrafts)
	if err != nil {
		return nil, translate("list job posts", err)
	}

	posts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.JobPost, error) {
		post, scanErr := scanJobPost(row)
		if scanErr != nil {
			return domain.JobPost{}, scanErr
		}
		return *post, nil
	})
	if err != nil {
		return nil, translate("list job posts", err)
	}

	// An empty slice encodes as [] where a nil one encodes as null.
	if posts == nil {
		posts = []domain.JobPost{}
	}
	return posts, nil
}

// listQuestions returns the questions of one job post in display order. It
// returns an empty slice, never nil, for a post with none.
func listQuestions(ctx context.Context, db queryer, jobPostID string) ([]domain.Question, error) {
	// Served by questions_job_post_id_position_key, which is already sorted
	// by position within a post.
	const query = `
		SELECT ` + questionColumns + `
		FROM questions
		WHERE job_post_id = $1::text::uuid
		ORDER BY position`

	rows, err := db.Query(ctx, query, jobPostID)
	if err != nil {
		return nil, translate("list questions", err)
	}

	questions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Question, error) {
		var q domain.Question

		// choices is a nullable text[]: pgx leaves the slice nil for NULL.
		scanErr := row.Scan(&q.ID, &q.JobPostID, &q.Prompt, &q.Kind, &q.Choices, &q.Required, &q.MinLength, &q.Position)
		return q, scanErr
	})
	if err != nil {
		return nil, translate("list questions", err)
	}

	if questions == nil {
		questions = []domain.Question{}
	}
	return questions, nil
}

// scanJobPost reads one row in jobPostColumns order.
func scanJobPost(row pgx.Row) (*domain.JobPost, error) {
	var p domain.JobPost

	// The address of a pointer field is a pointer to a pointer. pgx sets the
	// inner pointer to nil for NULL and to a new value otherwise.
	err := row.Scan(
		&p.ID, &p.Title, &p.Description, &p.EmploymentType, &p.Location, &p.Vacancies,
		&p.SalaryMin, &p.SalaryMax,
		&p.Status, &p.ClosesAt, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// translateByID is translate for a query that looks a row up by its ID. An ID
// PostgreSQL cannot parse as a uuid is reported as not found: no row can have
// it.
func translateByID(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == codeInvalidTextRepresentation {
		return fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}
	return translate(op, err)
}
