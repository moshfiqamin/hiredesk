package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// ListOpenJobPosts returns up to limit open job posts, newest first, starting
// after the position given, or from the newest when after is nil. more
// reports whether at least one further open post follows the last one
// returned.
//
// This is keyset pagination: the query names the sort key of the last row
// seen and asks for what comes after it, rather than counting rows to skip.
// Its cost is one page whether the client is on the first page or the
// thousandth, and posts opened or closed between requests cannot make a row
// appear twice or be skipped.
//
// Each listing carries its remaining vacancies and skill tags, both computed
// in SQL for the rows on the page only.
func (s *JobPostStore) ListOpenJobPosts(ctx context.Context, after *domain.ListingPosition, limit int) (listings []domain.JobListing, more bool, err error) {
	// Passed as typed nulls when there is no cursor, so the one statement
	// serves the first page and every later one.
	var (
		afterCreatedAt any
		afterID        any
	)
	if after != nil {
		afterCreatedAt, afterID = after.CreatedAt, after.ID
	}

	// The query in three steps:
	//
	// page: the open posts after the cursor, in order, one more than asked
	//   for. The row comparison (created_at, id) < ($1, $2) is true for every
	//   row that sorts after the cursor in a DESC, DESC order, and it matches
	//   job_posts_open_created_at_id_idx, so PostgreSQL starts the index scan
	//   at the cursor and stops after limit + 1 rows.
	//
	// offers: for each post on the page, how many applications are at the
	//   offer stage, from applications_job_post_id_status_idx.
	//
	// tags: for each post on the page, the keywords of the keyword_match
	//   criteria in its current rubric version.
	//
	// The two LATERAL subqueries run once per row of page, after the LIMIT,
	// so a page of 20 does 20 small lookups of each, never one per open post.
	const query = `
		WITH page AS (
			SELECT id, title, location, employment_type, vacancies, created_at
			FROM job_posts
			WHERE status = 'open'
			  AND ($1::timestamptz IS NULL OR (created_at, id) < ($1::timestamptz, $2::text::uuid))
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		)
		SELECT
			p.id::text,
			p.title,
			p.location,
			p.employment_type,
			coalesce(t.tags, '{}'),
			-- Two offers for one vacancy is possible (one may be declined),
			-- so the difference can go negative; a listing shows zero.
			greatest(p.vacancies - o.offers, 0),
			p.created_at
		FROM page p
		CROSS JOIN LATERAL (
			-- count(*) with no GROUP BY always returns one row, 0 when no
			-- application matches, so no coalesce is needed here.
			SELECT count(*)::int AS offers
			FROM applications a
			WHERE a.job_post_id = p.id AND a.status = 'offer'
		) o
		LEFT JOIN LATERAL (
			SELECT array_agg(DISTINCT n.tag ORDER BY n.tag) AS tags
			FROM rubric_criteria rc
			-- config is free-form jsonb, so a criterion whose "keywords" is
			-- missing or not an array is read as having none, rather than
			-- making jsonb_array_elements_text raise an error and turn the
			-- public listing into a 500.
			CROSS JOIN LATERAL jsonb_array_elements_text(
				CASE WHEN jsonb_typeof(rc.config -> 'keywords') = 'array'
				     THEN rc.config -> 'keywords'
				     ELSE '[]'::jsonb
				END
			) AS k(raw)
			CROSS JOIN LATERAL (SELECT lower(btrim(k.raw)) AS tag) n
			WHERE rc.job_post_id = p.id
			  AND rc.kind = 'keyword_match'
			  AND n.tag <> ''
			  AND rc.rubric_version = (
				SELECT max(rubric_version) FROM rubric_criteria WHERE job_post_id = p.id
			  )
		) t ON true
		ORDER BY p.created_at DESC, p.id DESC`

	// One extra row is the cheapest way to know whether another page exists:
	// it costs one more index entry, where a count(*) would visit them all.
	rows, err := s.db.Query(ctx, query, afterCreatedAt, afterID, limit+1)
	if err != nil {
		return nil, false, translate("list open job posts", err)
	}

	listings, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.JobListing, error) {
		var l domain.JobListing
		scanErr := row.Scan(&l.ID, &l.Title, &l.Location, &l.EmploymentType, &l.SkillTags, &l.RemainingVacancies, &l.CreatedAt)
		return l, scanErr
	})
	if err != nil {
		return nil, false, translate("list open job posts", err)
	}

	if len(listings) > limit {
		listings, more = listings[:limit], true
	}
	if listings == nil {
		listings = []domain.JobListing{}
	}
	return listings, more, nil
}
