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

// Constraints matched by name, so that a violation of any other constraint
// is not mistaken for these.
const (
	// constraintOneReviewPerInterviewer, from migration 000006, allows one
	// review per interviewer per application.
	constraintOneReviewPerInterviewer = "reviews_application_id_interviewer_id_key"

	// constraintAssigneeIsInterviewer, from migration 000013, makes it
	// impossible to assign a user whose role is not interviewer.
	constraintAssigneeIsInterviewer = "interviewer_assignments_interviewer_is_interviewer_fkey"

	// codeForeignKeyViolation is SQLSTATE 23503.
	codeForeignKeyViolation = "23503"
)

// reviewColumns is the column list every review query returns, in the order
// scanReview reads them. r is reviews and u the interviewer's users row.
const reviewColumns = `r.id::text, r.application_id::text, r.interviewer_id::text, u.full_name, r.mark, r.notes, r.created_at, r.updated_at`

// ReviewStore reads and writes interviewer assignments and reviews.
type ReviewStore struct {
	db *pgxpool.Pool
}

// NewReviewStore returns a ReviewStore that runs its queries on db.
func NewReviewStore(db *pgxpool.Pool) *ReviewStore {
	return &ReviewStore{db: db}
}

// notAnInterviewer is the 422 for an assignment target who is missing or is
// not an interviewer. One message for both: to an admin, "no such user" and
// "that user cannot review" call for the same correction.
func notAnInterviewer(op string) error {
	var invalid domain.ValidationError
	invalid.Add("interviewer_id", "must be the ID of a user whose role is interviewer")
	return fmt.Errorf("%s: %w", op, invalid.Err())
}

// AssignInterviewer assigns the user interviewerID to review the application
// appID, on behalf of the admin actorID, and returns the assignment.
//
// It is idempotent. created is true if this call made the assignment, and
// an application.assigned event is recorded only then; if the interviewer
// was already assigned, nothing is written and created is false.
//
// It fails with an error matching domain.ErrNotFound if there is no such
// application, and a *domain.ValidationError naming interviewer_id if the
// user does not exist or is not an interviewer.
func (s *ReviewStore) AssignInterviewer(ctx context.Context, appID, interviewerID, actorID string) (*domain.Assignment, bool, error) {
	const op = "assign interviewer"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	// The application first, so a missing one is a 404 whatever the body
	// says.
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM applications WHERE id = $1::text::uuid)`, appID).Scan(&exists); err != nil {
		return nil, false, translateByID(op, err)
	}
	if !exists {
		return nil, false, fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}

	// The privilege check. A candidate assigned here would gain read access
	// to every other candidate's application on the post, so the role is
	// read from the database, never taken from the request.
	//
	// FOR SHARE holds the user's row until commit, so their role cannot be
	// changed between this check and the insert. The composite foreign key
	// from migration 000013 would refuse the insert anyway; this check is
	// what gives the admin a 422 that says why.
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1::text::uuid FOR SHARE`, interviewerID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, notAnInterviewer(op)
	}
	if err != nil {
		return nil, false, translate(op+": read role", err)
	}
	if role != domain.RoleInterviewer {
		return nil, false, notAnInterviewer(op)
	}

	// ON CONFLICT … DO NOTHING is the idempotency. If the pair is already
	// assigned, the insert does nothing and RETURNING returns no row, instead
	// of failing with a unique violation. Two identical requests at the same
	// moment are serialised by the unique index: the second waits for the
	// first to commit, then finds the conflict.
	const insert = `
		INSERT INTO interviewer_assignments (application_id, interviewer_id, assigned_by)
		VALUES ($1::text::uuid, $2::text::uuid, $3::text::uuid)
		ON CONFLICT ON CONSTRAINT interviewer_assignments_application_id_interviewer_id_key DO NOTHING
		RETURNING id::text`

	var assignmentID string
	err = tx.QueryRow(ctx, insert, appID, interviewerID, actorID).Scan(&assignmentID)
	created := err == nil
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Already assigned: not an error, and nothing to record.
	case err != nil:
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeForeignKeyViolation &&
			pgErr.ConstraintName == constraintAssigneeIsInterviewer {
			return nil, false, notAnInterviewer(op)
		}
		return nil, false, translate(op, err)
	}

	if created {
		// Only for an assignment that happened. A retried request changes
		// nothing, so it must not add a second "assigned" to the history.
		if err = insertApplicationEvent(ctx, tx, appID, actorID, domain.EventApplicationAssigned,
			map[string]any{"interviewer_id": interviewerID}); err != nil {
			return nil, false, fmt.Errorf("%s: %w", op, err)
		}
	}

	assignments, err := listAssignments(ctx, tx, appID, interviewerID)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", op, err)
	}
	if len(assignments) != 1 {
		return nil, false, fmt.Errorf("%s: read back %d assignments, want 1", op, len(assignments))
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, false, translate(op+": commit", err)
	}
	return &assignments[0], created, nil
}

// UnassignInterviewer removes the assignment of interviewerID to appID, on
// behalf of the admin actorID, and reports whether there was one. An
// application.unassigned event is recorded only if there was.
//
// It is idempotent: removing an assignment that does not exist changes
// nothing and is not an error. Any review the interviewer filed is kept; it
// is evidence, and admins still see it.
//
// It fails with an error matching domain.ErrNotFound if there is no such
// application.
func (s *ReviewStore) UnassignInterviewer(ctx context.Context, appID, interviewerID, actorID string) (bool, error) {
	const op = "unassign interviewer"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM applications WHERE id = $1::text::uuid)`, appID).Scan(&exists); err != nil {
		return false, translateByID(op, err)
	}
	if !exists {
		return false, fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}

	// DELETE takes the row lock that a review in progress holds FOR SHARE
	// (see CreateReview), so it waits for that review to commit rather than
	// pulling the assignment out from under it.
	const remove = `
		DELETE FROM interviewer_assignments
		WHERE application_id = $1::text::uuid AND interviewer_id = $2::text::uuid
		RETURNING id::text`

	var removedID string
	err = tx.QueryRow(ctx, remove, appID, interviewerID).Scan(&removedID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not assigned. Nothing happened, so nothing is recorded.
		return false, nil
	}
	if err != nil {
		return false, translate(op, err)
	}

	if err = insertApplicationEvent(ctx, tx, appID, actorID, domain.EventApplicationUnassigned,
		map[string]any{"interviewer_id": interviewerID}); err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, translate(op+": commit", err)
	}
	return true, nil
}

// ListAssignments returns every interviewer assigned to the application, in
// the order they were assigned. It returns an error matching
// domain.ErrNotFound if there is no such application.
func (s *ReviewStore) ListAssignments(ctx context.Context, appID string) ([]domain.Assignment, error) {
	const op = "list assignments"

	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM applications WHERE id = $1::text::uuid)`, appID).Scan(&exists); err != nil {
		return nil, translateByID(op, err)
	}
	if !exists {
		return nil, fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}

	assignments, err := listAssignments(ctx, s.db, appID, "")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return assignments, nil
}

// listAssignments returns the assignments of one application, or only the
// one of interviewerID when it is not empty, never nil.
func listAssignments(ctx context.Context, db queryer, appID, interviewerID string) ([]domain.Assignment, error) {
	var where conditions
	where.add("ia.application_id = " + where.arg(appID) + "::text::uuid")
	if interviewerID != "" {
		where.add("ia.interviewer_id = " + where.arg(interviewerID) + "::text::uuid")
	}

	// LEFT JOIN for the assigner, who may be unknown or deleted. EXISTS
	// answers "has this interviewer reviewed it?" from the unique index on
	// reviews (application_id, interviewer_id), one probe per assignment.
	query := `
		SELECT ia.application_id::text, ia.interviewer_id::text, u.full_name, u.email,
		       ia.assigned_by::text, ab.full_name, ia.created_at,
		       EXISTS (SELECT 1 FROM reviews r
		               WHERE r.application_id = ia.application_id AND r.interviewer_id = ia.interviewer_id)
		FROM interviewer_assignments ia
		JOIN users u ON u.id = ia.interviewer_id
		LEFT JOIN users ab ON ab.id = ia.assigned_by` + where.sql() + `
		ORDER BY ia.created_at, ia.interviewer_id`

	rows, err := db.Query(ctx, query, where.args...)
	if err != nil {
		return nil, translate("list assignments", err)
	}
	assignments, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Assignment, error) {
		var a domain.Assignment
		scanErr := row.Scan(&a.ApplicationID, &a.InterviewerID, &a.InterviewerName, &a.InterviewerEmail,
			&a.AssignedBy, &a.AssignedByName, &a.AssignedAt, &a.Reviewed)
		return a, scanErr
	})
	if err != nil {
		return nil, translate("list assignments", err)
	}
	if assignments == nil {
		assignments = []domain.Assignment{}
	}
	return assignments, nil
}

// lockAssignment locks the assignment of interviewerID to appID FOR SHARE
// until the transaction ends, or returns an error matching
// domain.ErrNotFound if there is none.
//
// The handler has already checked the assignment, but a request takes time:
// without this lock an admin could unassign the interviewer between that
// check and the write, and a review would be filed by someone who may no
// longer see the application. FOR SHARE makes UnassignInterviewer's DELETE
// wait, and two reviews by different interviewers do not block each other.
func lockAssignment(ctx context.Context, tx pgx.Tx, appID, interviewerID string) error {
	const query = `
		SELECT 1 FROM interviewer_assignments
		WHERE application_id = $1::text::uuid AND interviewer_id = $2::text::uuid
		FOR SHARE`

	var one int
	if err := tx.QueryRow(ctx, query, appID, interviewerID).Scan(&one); err != nil {
		return translateByID("lock assignment", err)
	}
	return nil
}

// CreateReview files interviewerID's review of appID, with mark and notes,
// records an application.reviewed event, and returns the review.
//
// It fails with an error matching domain.ErrNotFound if the interviewer is
// not assigned to the application, and domain.ErrAlreadyReviewed (and so
// ErrDuplicate) if they have already reviewed it. The mark must already
// have passed domain.CheckMark; the database refuses anything else.
func (s *ReviewStore) CreateReview(ctx context.Context, appID, interviewerID string, mark int, notes *string) (*domain.Review, error) {
	const op = "create review"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	if err = lockAssignment(ctx, tx, appID, interviewerID); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// A data-modifying CTE, so the insert and the interviewer's name come
	// back in one statement. The unique constraint, not a SELECT first, is
	// what stops a second review: a check-then-insert would let two
	// simultaneous requests both pass the check (see 05-3).
	const insert = `
		WITH r AS (
			INSERT INTO reviews (application_id, interviewer_id, mark, notes)
			VALUES ($1::text::uuid, $2::text::uuid, $3, $4)
			RETURNING *
		)
		SELECT ` + reviewColumns + ` FROM r JOIN users u ON u.id = r.interviewer_id`

	review, err := scanReview(tx.QueryRow(ctx, insert, appID, interviewerID, mark, notes))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation &&
			pgErr.ConstraintName == constraintOneReviewPerInterviewer {
			return nil, fmt.Errorf("%s: %w: %w", op, domain.ErrAlreadyReviewed, err)
		}
		return nil, translate(op, err)
	}

	// The mark goes in the event, so the log can show what was decided on.
	// Its visibility is that of the review: see auth.ApplicationViewFor.
	if err = insertApplicationEvent(ctx, tx, appID, interviewerID, domain.EventApplicationReviewed,
		map[string]any{"review_id": review.ID, "interviewer_id": interviewerID, "mark": mark}); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, translate(op+": commit", err)
	}
	return review, nil
}

// UpdateReview applies patch to interviewerID's review of appID and returns
// the review as stored. If the patch changes something, it records an
// application.review_updated event with the mark before and after;
// otherwise nothing is written.
//
// It fails with an error matching domain.ErrNotFound if the interviewer is
// not assigned to the application, and domain.ErrNoReview (which also
// matches ErrNotFound) if they have not reviewed it yet.
func (s *ReviewStore) UpdateReview(ctx context.Context, appID, interviewerID string, patch domain.ReviewPatch) (*domain.Review, error) {
	const op = "update review"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Its error carries nothing after Commit or on an early return.

	if err = lockAssignment(ctx, tx, appID, interviewerID); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// FOR UPDATE OF r locks the review, not the users row it is joined to,
	// so two corrections by the same interviewer are applied one at a time
	// and the event's "from" is always the mark the second one replaced.
	const lockReview = `
		SELECT ` + reviewColumns + `
		FROM reviews r JOIN users u ON u.id = r.interviewer_id
		WHERE r.application_id = $1::text::uuid AND r.interviewer_id = $2::text::uuid
		FOR UPDATE OF r`

	current, err := scanReview(tx.QueryRow(ctx, lockReview, appID, interviewerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", op, domain.ErrNoReview)
	}
	if err != nil {
		return nil, translate(op, err)
	}

	mark, notes := current.Mark, current.Notes
	if patch.Mark != nil {
		mark = *patch.Mark
	}
	if patch.Notes != nil {
		notes = patch.Notes
		if *patch.Notes == "" {
			notes = nil
		}
	}

	// A correction that corrects nothing is not an event: the history
	// records changes, and a repeated PATCH must not look like several.
	notesChanged := !sameText(notes, current.Notes)
	if mark == current.Mark && !notesChanged {
		if err = tx.Commit(ctx); err != nil {
			return nil, translate(op+": commit", err)
		}
		return current, nil
	}

	const update = `
		WITH r AS (
			UPDATE reviews
			SET mark = $2, notes = $3, updated_at = now()
			WHERE id = $1::text::uuid
			RETURNING *
		)
		SELECT ` + reviewColumns + ` FROM r JOIN users u ON u.id = r.interviewer_id`

	updated, err := scanReview(tx.QueryRow(ctx, update, current.ID, mark, notes))
	if err != nil {
		return nil, translate(op, err)
	}

	// Both marks, because "from" is what this transaction read under the
	// lock, which no later query could recover. The notes text is not
	// copied: it can be long, and whether it changed is what a reader of the
	// history needs.
	if err = insertApplicationEvent(ctx, tx, appID, interviewerID, domain.EventApplicationReviewUpdated,
		map[string]any{
			"review_id": current.ID, "interviewer_id": interviewerID,
			"from_mark": current.Mark, "to_mark": mark, "notes_changed": notesChanged,
		}); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, translate(op+": commit", err)
	}
	return updated, nil
}

// sameText reports whether two optional texts are equal, nil equal only to
// nil.
func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// scanReview reads one row in reviewColumns order.
func scanReview(row pgx.Row) (*domain.Review, error) {
	var r domain.Review
	if err := row.Scan(&r.ID, &r.ApplicationID, &r.InterviewerID, &r.InterviewerName,
		&r.Mark, &r.Notes, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// insertApplicationEvent appends one event to an application's history,
// inside tx. pgx encodes payload as JSON for the jsonb column.
func insertApplicationEvent(ctx context.Context, tx pgx.Tx, appID, actorID, eventType string, payload map[string]any) error {
	const query = `
		INSERT INTO application_events (application_id, actor_id, type, payload)
		VALUES ($1::text::uuid, $2::text::uuid, $3, $4)`

	if _, err := tx.Exec(ctx, query, appID, actorID, eventType, payload); err != nil {
		return translate("record "+eventType+" event", err)
	}
	return nil
}
