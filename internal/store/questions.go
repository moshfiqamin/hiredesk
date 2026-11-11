package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// questionPositionConstraint is the unique constraint on (job_post_id,
// position). 000008 made it DEFERRABLE, which ReorderQuestions relies on.
const questionPositionConstraint = "questions_job_post_id_position_key"

// QuestionStore reads and writes the questions of job posts.
//
// Every method that changes a question first locks the post and checks that
// it is a draft, inside the same transaction as the change. See
// lockEditableForm.
type QuestionStore struct {
	db *pgxpool.Pool
}

// NewQuestionStore returns a QuestionStore that runs its queries on db.
func NewQuestionStore(db *pgxpool.Pool) *QuestionStore {
	return &QuestionStore{db: db}
}

// CreateQuestion adds a question to the job post's form and returns it as
// stored. A Position of 0 places it after the current last question.
//
// It fails with:
//
//   - an error matching domain.ErrNotFound if there is no such post;
//   - a *domain.FormLockedError if the post is not a draft;
//   - a *domain.ValidationError if the requested position is taken.
//
// It does not validate in's fields; the caller is expected to have called
// in.Validate.
func (s *QuestionStore) CreateQuestion(ctx context.Context, jobPostID string, in domain.NewQuestion) (*domain.Question, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("create question: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	if err = lockEditableForm(ctx, tx, jobPostID, "create question"); err != nil {
		return nil, err
	}

	position := in.Position
	if position == 0 {
		// The post's row is locked, so no other edit to this form can run
		// between reading the maximum and inserting at the slot after it.
		const nextQuery = `SELECT coalesce(max(position), 0) + 1 FROM questions WHERE job_post_id = $1::text::uuid`
		if err = tx.QueryRow(ctx, nextQuery, jobPostID).Scan(&position); err != nil {
			return nil, translate("create question: next position", err)
		}
	} else if err = checkPositionFree(ctx, tx, jobPostID, position, ""); err != nil {
		return nil, fmt.Errorf("create question: %w", err)
	}

	const insertQuery = `
		INSERT INTO questions (job_post_id, prompt, kind, choices, required, min_length, position)
		VALUES ($1::text::uuid, $2, $3, $4, $5, $6, $7)
		RETURNING ` + questionColumns

	q, err := scanQuestion(tx.QueryRow(ctx, insertQuery,
		jobPostID, in.Prompt, in.Kind, nilIfEmpty(in.Choices), in.Required, in.MinLength, position,
	))
	if err != nil {
		return nil, translate("create question", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create question: commit: %w", err)
	}
	return q, nil
}

// ListQuestionsByJobPost returns the questions of the job post in display
// order, whatever its status, or an error matching domain.ErrNotFound if
// there is no such post. A post with no questions gives an empty slice.
//
// It is for admins. What other users may see of a form arrives with the post
// itself, from JobPostStore.GetJobPostByID, which hides drafts.
func (s *QuestionStore) ListQuestionsByJobPost(ctx context.Context, jobPostID string) ([]domain.Question, error) {
	// Checked first, because "no questions" and "no such post" would
	// otherwise both be an empty list.
	var exists bool
	const existsQuery = `SELECT EXISTS (SELECT 1 FROM job_posts WHERE id = $1::text::uuid)`
	if err := s.db.QueryRow(ctx, existsQuery, jobPostID).Scan(&exists); err != nil {
		return nil, translateByID("list questions", err)
	}
	if !exists {
		return nil, fmt.Errorf("list questions: %w", domain.ErrNotFound)
	}

	return listQuestions(ctx, s.db, jobPostID)
}

// UpdateQuestion applies patch to one question of the job post and returns
// it as stored. A question that belongs to a different post is reported as
// not found.
//
// It fails with:
//
//   - an error matching domain.ErrNotFound if the post or question is missing;
//   - a *domain.FormLockedError if the post is not a draft;
//   - a *domain.ValidationError if the question would break a rule once the
//     patch is applied, including moving to a position that is taken.
//
// In none of those cases is anything written.
func (s *QuestionStore) UpdateQuestion(ctx context.Context, jobPostID, questionID string, patch domain.QuestionPatch) (*domain.Question, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("update question: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	if err = lockEditableForm(ctx, tx, jobPostID, "update question"); err != nil {
		return nil, err
	}

	// No FOR UPDATE on the question: every change to this form takes the
	// post's lock first, so holding that one is enough.
	const selectQuery = `
		SELECT ` + questionColumns + `
		FROM questions
		WHERE id = $1::text::uuid AND job_post_id = $2::text::uuid`

	q, err := scanQuestion(tx.QueryRow(ctx, selectQuery, questionID, jobPostID))
	if err != nil {
		return nil, translateByID("update question", err)
	}
	oldPosition := q.Position

	// The kind decides which of choices and min_length are allowed, so the
	// rules are judged on the merged question, as for a job post.
	q.Apply(patch)
	if err = q.Validate(); err != nil {
		return nil, fmt.Errorf("update question: %w", err)
	}

	if q.Position != oldPosition {
		if err = checkPositionFree(ctx, tx, jobPostID, q.Position, q.ID); err != nil {
			return nil, fmt.Errorf("update question: %w", err)
		}
	}

	if !patch.IsEmpty() {
		const updateQuery = `
			UPDATE questions
			SET prompt = $2, kind = $3, choices = $4, required = $5, min_length = $6, position = $7
			WHERE id = $1::text::uuid
			RETURNING ` + questionColumns

		q, err = scanQuestion(tx.QueryRow(ctx, updateQuery,
			q.ID, q.Prompt, q.Kind, nilIfEmpty(q.Choices), q.Required, q.MinLength, q.Position,
		))
		if err != nil {
			return nil, translate("update question", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("update question: commit: %w", err)
	}
	return q, nil
}

// DeleteQuestion removes one question from the job post's form. The other
// questions keep their positions, so a gap may remain; display order is
// unchanged, since it only sorts by position.
//
// Rubric criteria that score this question are deleted with it, by the
// ON DELETE CASCADE on rubric_criteria.question_id.
//
// It fails with an error matching domain.ErrNotFound if the post or question
// is missing, and a *domain.FormLockedError if the post is not a draft.
func (s *QuestionStore) DeleteQuestion(ctx context.Context, jobPostID, questionID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("delete question: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	if err = lockEditableForm(ctx, tx, jobPostID, "delete question"); err != nil {
		return err
	}

	const deleteQuery = `DELETE FROM questions WHERE id = $1::text::uuid AND job_post_id = $2::text::uuid`

	// Exec returns a command tag, which reports how many rows the statement
	// touched. Zero means there was no such question on this post.
	tag, err := tx.Exec(ctx, deleteQuery, questionID, jobPostID)
	if err != nil {
		return translateByID("delete question", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete question: %w", domain.ErrNotFound)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("delete question: commit: %w", err)
	}
	return nil
}

// ReorderQuestions puts the job post's questions in the order given, as
// positions 1, 2, 3 and so on, and returns them in their new order.
//
// order must name every question of the post exactly once. It fails with:
//
//   - an error matching domain.ErrNotFound if there is no such post;
//   - a *domain.FormLockedError if the post is not a draft;
//   - a *domain.ValidationError if order is not exactly the post's questions.
//
// Either every question moves or none does.
func (s *QuestionStore) ReorderQuestions(ctx context.Context, jobPostID string, order []string) ([]domain.Question, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("reorder questions: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	if err = lockEditableForm(ctx, tx, jobPostID, "reorder questions"); err != nil {
		return nil, err
	}

	current, err := listQuestions(ctx, tx, jobPostID)
	if err != nil {
		return nil, fmt.Errorf("reorder questions: %w", err)
	}
	if !sameQuestions(current, order) {
		var invalid domain.ValidationError
		invalid.Add("question_ids", fmt.Sprintf("must list each of this post's %d questions exactly once", len(current)))
		return nil, fmt.Errorf("reorder questions: %w", invalid.Err())
	}

	// What makes the UPDATE below possible is 000008: the constraint is
	// DEFERRABLE, so it is checked when the statement finishes, not after
	// each row. As 000002 created it, the first row moved into a position
	// another row still holds was refused on the spot.
	//
	// DEFERRED pushes the check further, to COMMIT, so the reorder stays
	// correct if it is ever done in more than one statement. It lasts for
	// this transaction only; outside one, PostgreSQL ignores it with a
	// warning.
	if _, err = tx.Exec(ctx, "SET CONSTRAINTS "+questionPositionConstraint+" DEFERRED"); err != nil {
		return nil, fmt.Errorf("reorder questions: defer constraint: %w", err)
	}

	// unnest turns the array into rows, and WITH ORDINALITY numbers them from
	// 1 in array order, so each ID is joined to its new position in a single
	// statement.
	const reorderQuery = `
		UPDATE questions q
		SET position = o.position
		FROM unnest($2::text[]) WITH ORDINALITY AS o(id, position)
		WHERE q.id = o.id::uuid
		  AND q.job_post_id = $1::text::uuid`

	if _, err = tx.Exec(ctx, reorderQuery, jobPostID, order); err != nil {
		return nil, translate("reorder questions", err)
	}

	reordered, err := listQuestions(ctx, tx, jobPostID)
	if err != nil {
		return nil, fmt.Errorf("reorder questions: %w", err)
	}

	// The deferred check runs here. A collision would surface as a unique
	// violation from Commit, which translate maps to domain.ErrDuplicate.
	// sameQuestions above makes one impossible, so this is the backstop.
	if err = tx.Commit(ctx); err != nil {
		return nil, translate("reorder questions: commit", err)
	}
	return reordered, nil
}

// lockEditableForm locks the job post's row until the transaction ends and
// returns an error unless the post is a draft: one matching domain.ErrNotFound
// if there is no such post, or a *domain.FormLockedError.
//
// The lock is what makes the draft check mean anything. Without it, an admin
// could open the post between this check and the change it guards, and a
// question would be edited on a form candidates can already see. With it,
// TransitionJobPost, which takes the same lock, waits for this transaction,
// and every edit to one form runs one at a time.
func lockEditableForm(ctx context.Context, tx pgx.Tx, jobPostID, op string) error {
	const query = `SELECT status FROM job_posts WHERE id = $1::text::uuid FOR UPDATE`

	var status string
	if err := tx.QueryRow(ctx, query, jobPostID).Scan(&status); err != nil {
		return translateByID(op, err)
	}
	if err := domain.CheckFormEditable(status); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// checkPositionFree returns a *domain.ValidationError if another question of
// the post already holds position. exceptID is the question being moved,
// which may keep its own position, or "" when creating.
//
// The unique constraint would refuse the write anyway; checking first gives a
// 422 that names the field, rather than a 409 that does not.
func checkPositionFree(ctx context.Context, tx pgx.Tx, jobPostID string, position int, exceptID string) error {
	// NULLIF turns "" into NULL, and IS DISTINCT FROM is true for every id
	// when compared with NULL, where <> would be NULL and match nothing.
	const query = `
		SELECT EXISTS (
			SELECT 1 FROM questions
			WHERE job_post_id = $1::text::uuid
			  AND position = $2
			  AND id IS DISTINCT FROM NULLIF($3, '')::uuid
		)`

	var taken bool
	if err := tx.QueryRow(ctx, query, jobPostID, position, exceptID).Scan(&taken); err != nil {
		return translate("check position", err)
	}
	if taken {
		var invalid domain.ValidationError
		invalid.Add("position", "is already used by another question in this post")
		return invalid.Err()
	}
	return nil
}

// sameQuestions reports whether order names every question in current
// exactly once, and nothing else.
func sameQuestions(current []domain.Question, order []string) bool {
	if len(order) != len(current) {
		return false
	}

	want := make(map[string]bool, len(current))
	for _, q := range current {
		want[q.ID] = true
	}

	for _, id := range order {
		// Deleting each match means a repeated ID is not found the second
		// time, so duplicates are caught without a separate pass.
		if !want[id] {
			return false
		}
		delete(want, id)
	}
	return true
}

// scanQuestion reads one row in questionColumns order.
func scanQuestion(row pgx.Row) (*domain.Question, error) {
	var q domain.Question

	// choices is a nullable text[]: pgx leaves the slice nil for NULL.
	if err := row.Scan(&q.ID, &q.JobPostID, &q.Prompt, &q.Kind, &q.Choices, &q.Required, &q.MinLength, &q.Position); err != nil {
		return nil, err
	}
	return &q, nil
}

// nilIfEmpty returns nil for an empty slice, which pgx sends as NULL. The
// column stores "no choices" as NULL, never as an empty array, so readers
// have one kind of emptiness to handle.
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
