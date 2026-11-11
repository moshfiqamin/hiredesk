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

// applicationColumns is the column list every query here returns, in the
// order scanApplication reads them. Qualified with the alias a, which every
// query gives the applications table, so the same list works in a join.
const applicationColumns = `a.id::text, a.job_post_id::text, a.candidate_id::text, a.status, a.cv_url, a.submitted_at, a.updated_at`

// applicationOrder puts the newest first. id breaks ties between rows
// submitted in the same microsecond, so the order is stable across calls.
const applicationOrder = `ORDER BY a.submitted_at DESC, a.id`

// ApplicationStore reads the applications table.
type ApplicationStore struct {
	db *pgxpool.Pool
}

// NewApplicationStore returns an ApplicationStore that runs its queries on db.
func NewApplicationStore(db *pgxpool.Pool) *ApplicationStore {
	return &ApplicationStore{db: db}
}

// ListApplications returns the applications viewer may see, newest first:
//
//   - an admin, every application;
//   - a candidate, the ones they submitted;
//   - an interviewer, the ones they are assigned to.
//
// The restriction is in the SQL, so rows the viewer may not see never leave
// the database. Any other role gets an error matching domain.ErrForbidden.
//
// These three queries and auth.CanViewApplication state the same rule twice,
// once for many rows and once for one. TestAuthorizationMatrix checks that
// they agree.
func (s *ApplicationStore) ListApplications(ctx context.Context, viewer domain.User) ([]domain.Application, error) {
	var (
		query string
		args  []any
	)

	switch viewer.Role {
	case domain.RoleAdmin:
		query = `
			SELECT ` + applicationColumns + `
			FROM applications a
			` + applicationOrder

	case domain.RoleCandidate:
		// Served by applications_candidate_id_idx.
		query = `
			SELECT ` + applicationColumns + `
			FROM applications a
			WHERE a.candidate_id = $1::text::uuid
			` + applicationOrder
		args = []any{viewer.ID}

	case domain.RoleInterviewer:
		// An inner join keeps only applications with a matching assignment
		// row. interviewer_assignments is unique on (application_id,
		// interviewer_id), so no application can appear twice. The WHERE is
		// served by interviewer_assignments_interviewer_id_idx.
		query = `
			SELECT ` + applicationColumns + `
			FROM applications a
			JOIN interviewer_assignments ia ON ia.application_id = a.id
			WHERE ia.interviewer_id = $1::text::uuid
			` + applicationOrder
		args = []any{viewer.ID}

	default:
		// Deny by default, as the policy does: a role nobody wrote a query
		// for sees nothing, rather than falling through to the admin query.
		return nil, fmt.Errorf("list applications: role %q: %w", viewer.Role, domain.ErrForbidden)
	}

	// args... spreads the slice into Query's variadic parameter; for the
	// admin query it is empty, matching the query's lack of placeholders.
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, translate("list applications", err)
	}

	// CollectRows reads every row through scanApplication, closes rows, and
	// returns rows.Err(), which is where an error part-way through the result
	// set is reported. A hand-written rows.Next loop must remember all three.
	apps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Application, error) {
		// scanErr, not err: the closure can see the outer err, and reusing the
		// name would shadow it.
		app, scanErr := scanApplication(row)
		if scanErr != nil {
			return domain.Application{}, scanErr
		}
		return *app, nil
	})
	if err != nil {
		return nil, translate("list applications", err)
	}

	// CollectRows returns a nil slice for no rows. An empty slice encodes as
	// [] rather than null, so a client gets one shape either way.
	if apps == nil {
		apps = []domain.Application{}
	}
	return apps, nil
}

// GetApplication returns the application with this ID, and whether viewerID
// is assigned to it as an interviewer. It returns an error matching
// domain.ErrNotFound if there is no such application, including when id is
// not a UUID.
//
// It does not decide whether the viewer may see the application; that is
// auth.CanViewApplication, which needs exactly the two facts returned here.
func (s *ApplicationStore) GetApplication(ctx context.Context, id, viewerID string) (*domain.Application, bool, error) {
	// Both facts in one round trip, so a refused request and a missing one do
	// the same work. With a second query only when the row exists, the time
	// taken would tell an outsider which IDs are real.
	//
	// EXISTS stops at the first matching row and is answered from the unique
	// index on (application_id, interviewer_id).
	const query = `
		SELECT ` + applicationColumns + `,
			EXISTS (
				SELECT 1
				FROM interviewer_assignments ia
				WHERE ia.application_id = a.id
				  AND ia.interviewer_id = $2::text::uuid
			)
		FROM applications a
		WHERE a.id = $1::text::uuid`

	var (
		app      domain.Application
		assigned bool
	)
	err := s.db.QueryRow(ctx, query, id, viewerID).Scan(
		&app.ID, &app.JobPostID, &app.CandidateID, &app.Status, &app.CVURL, &app.SubmittedAt, &app.UpdatedAt,
		&assigned,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == codeInvalidTextRepresentation {
			// No application can have an ID that is not a UUID.
			return nil, false, fmt.Errorf("get application: %w", domain.ErrNotFound)
		}
		return nil, false, translate("get application", err)
	}
	return &app, assigned, nil
}

// scanApplication reads one row in applicationColumns order.
func scanApplication(row pgx.Row) (*domain.Application, error) {
	var a domain.Application

	err := row.Scan(&a.ID, &a.JobPostID, &a.CandidateID, &a.Status, &a.CVURL, &a.SubmittedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
