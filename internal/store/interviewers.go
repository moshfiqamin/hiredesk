package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// ListInterviewers returns every user whose role is interviewer, by name,
// each with their current load. Only interviewers: the assignment picker
// built on this can never offer a candidate or an admin.
func (s *ReviewStore) ListInterviewers(ctx context.Context) ([]domain.InterviewerLoad, error) {
	const op = "list interviewers"

	// One statement for the whole list. The LEFT JOINs keep an interviewer
	// with no assignments (a load of zero, the person an admin most wants to
	// see). FILTER counts only the assignments to applications still in
	// progress; the reviews join is on the same (application, interviewer)
	// pair, so r.id IS NULL means "assigned here and not reviewed yet".
	const query = `
		SELECT u.id::text, u.full_name, u.email,
			count(*) FILTER (WHERE a.status IN ('submitted', 'shortlisted', 'interview'))::int,
			count(*) FILTER (WHERE a.status IN ('submitted', 'shortlisted', 'interview') AND r.id IS NULL)::int
		FROM users u
		LEFT JOIN interviewer_assignments ia ON ia.interviewer_id = u.id
		LEFT JOIN applications a ON a.id = ia.application_id
		LEFT JOIN reviews r ON r.application_id = ia.application_id AND r.interviewer_id = u.id
		WHERE u.role = 'interviewer'
		GROUP BY u.id, u.full_name, u.email
		ORDER BY u.full_name, u.id`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, translate(op, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.InterviewerLoad, error) {
		var l domain.InterviewerLoad
		scanErr := row.Scan(&l.ID, &l.Name, &l.Email, &l.OpenAssignments, &l.PendingReviews)
		return l, scanErr
	})
	if err != nil {
		return nil, translate(op, err)
	}
	if out == nil {
		out = []domain.InterviewerLoad{}
	}
	return out, nil
}
