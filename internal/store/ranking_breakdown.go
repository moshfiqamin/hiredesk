package store

import (
	"context"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// CriterionPoints returns, for each of the applications with these IDs, the
// points of every criterion of its latest score, in rubric order. An
// application with no score has no key in the map.
//
// It is one query whatever the number of IDs, so a ranking page pays one
// extra round trip for its breakdown bars, not one per row.
func (s *ApplicationStore) CriterionPoints(ctx context.Context, ids []string) (map[string][]domain.CriterionPoints, error) {
	const op = "criterion points"

	out := make(map[string][]domain.CriterionPoints, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	// ANY($1) takes the whole list as one array parameter, served by
	// scores_application_id_idx. The correlated subquery keeps each
	// application to its own latest rubric version, as ScoreCard does.
	const query = `
		SELECT s.application_id::text, rc.label, s.earned::float8, s.available::float8
		FROM scores s
		JOIN rubric_criteria rc ON rc.id = s.criterion_id
		WHERE s.application_id = ANY($1::text[]::uuid[])
		  AND s.rubric_version = (
			SELECT max(latest.rubric_version) FROM scores latest
			WHERE latest.application_id = s.application_id)
		ORDER BY s.application_id, rc.position`

	rows, err := s.db.Query(ctx, query, ids)
	if err != nil {
		return nil, translate(op, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			appID string
			p     domain.CriterionPoints
		)
		if err = rows.Scan(&appID, &p.Label, &p.Earned, &p.Available); err != nil {
			return nil, translate(op, err)
		}
		out[appID] = append(out[appID], p)
	}
	if err = rows.Err(); err != nil {
		return nil, translate(op, err)
	}
	return out, nil
}
