package store

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// RankApplications returns the applications to the job post, best total
// score first, at most limit of them. Applications with no score come last.
// Ties are broken by who applied first, then by ID, so the order is stable.
//
// It returns an error matching domain.ErrNotFound if there is no such post.
//
// THIS IS THE N+1 VERSION, written so on purpose to be measured: one query
// for the applications, then one more query per application for its total.
// See docs/notes/07-5-ranking.md. It is replaced by a single query in the
// next commit.
func (s *ApplicationStore) RankApplications(ctx context.Context, jobPostID string, limit int) ([]domain.RankedApplication, error) {
	const op = "rank applications"

	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_posts WHERE id = $1::text::uuid)`, jobPostID).Scan(&exists); err != nil {
		return nil, translateByID(op, err)
	}
	if !exists {
		return nil, fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}

	// Query 1: every application to the post, with its candidate's name.
	// Every one is fetched, not just the first limit: the totals that decide
	// the order are not known yet.
	const appsQuery = `
		SELECT a.id::text, a.candidate_id::text, u.full_name, a.status, a.submitted_at
		FROM applications a
		JOIN users u ON u.id = a.candidate_id
		WHERE a.job_post_id = $1::text::uuid`

	rows, err := s.db.Query(ctx, appsQuery, jobPostID)
	if err != nil {
		return nil, translate(op, err)
	}
	ranked, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.RankedApplication, error) {
		var r domain.RankedApplication
		scanErr := row.Scan(&r.ApplicationID, &r.CandidateID, &r.CandidateName, &r.Status, &r.SubmittedAt)
		return r, scanErr
	})
	if err != nil {
		return nil, translate(op, err)
	}

	// Queries 2 … N+1: one round trip per application. Each is fast on its
	// own, served by scores_application_id_idx; it is their number that
	// costs.
	const totalQuery = `
		SELECT sum(earned)::float8, sum(available)::float8, max(rubric_version)
		FROM scores
		WHERE application_id = $1::text::uuid`

	for i := range ranked {
		// sum and max over no rows are NULL, not 0, so an unscored
		// application scans as nil pointers.
		var (
			earned, available *float64
			version           *int
		)
		if err = s.db.QueryRow(ctx, totalQuery, ranked[i].ApplicationID).Scan(&earned, &available, &version); err != nil {
			return nil, translate(op+": total", err)
		}
		if earned != nil && available != nil && version != nil {
			ranked[i].Totals = &domain.ScoreTotals{RubricVersion: *version, Earned: *earned, Available: *available}
		}
	}

	// The ordering, in Go: by total descending, unscored last, then earliest
	// submission, then ID.
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		switch {
		case a.Totals == nil && b.Totals != nil:
			return false
		case a.Totals != nil && b.Totals == nil:
			return true
		case a.Totals != nil && a.Totals.Earned != b.Totals.Earned:
			return a.Totals.Earned > b.Totals.Earned
		case !a.SubmittedAt.Equal(b.SubmittedAt):
			return a.SubmittedAt.Before(b.SubmittedAt)
		}
		return a.ApplicationID < b.ApplicationID
	})

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	for i := range ranked {
		ranked[i].Rank = i + 1
	}
	if ranked == nil {
		ranked = []domain.RankedApplication{}
	}
	return ranked, nil
}
