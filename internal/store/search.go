package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// SearchApplications returns one page of the applications to the job post
// whose search document matches query, most relevant first, together with
// how many match in all. Ties in relevance go to whoever applied first, then
// by ID, so paging is stable.
//
// query is plain text, parsed by plainto_tsquery: every word must appear, in
// any order, after stemming. Operators and punctuation in it have no special
// meaning. A query of only stop words or punctuation matches nothing, which
// is an empty page, not an error.
//
// It returns an error matching domain.ErrNotFound if there is no such post.
func (s *ApplicationStore) SearchApplications(ctx context.Context, jobPostID, query string, limit, offset int) ([]domain.SearchResult, int, error) {
	const op = "search applications"

	// One snapshot for the existence check, the count and the page, so the
	// total cannot disagree with the rows returned.
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Read-only: there is nothing to undo, and Commit below ends it.

	// Checked separately so that a missing post is a 404 rather than an empty
	// result, which would tell the caller nothing about a mistyped ID.
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_posts WHERE id = $1::text::uuid)`, jobPostID).Scan(&exists); err != nil {
		return nil, 0, translateByID(op, err)
	}
	if !exists {
		return nil, 0, fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}

	// query arrives as $2 and is never part of the SQL text. plainto_tsquery
	// is a function in FROM, so it is evaluated once per statement, not once
	// per row, and its result is named so WHERE and ts_rank share it.
	//
	// @@ against search_vector is what applications_search_vector_idx
	// answers. The planner combines it with applications_job_post_id_status_idx
	// for the post filter when that is cheaper.
	const countQuery = `
		SELECT count(*)
		FROM applications a, plainto_tsquery('english', $2) AS tsq
		WHERE a.job_post_id = $1::text::uuid
		  AND a.search_vector @@ tsq`

	var total int
	if err = tx.QueryRow(ctx, countQuery, jobPostID, query).Scan(&total); err != nil {
		return nil, 0, translate(op+": count", err)
	}

	// The inner query ranks every match, which ordering by relevance cannot
	// avoid, and keeps one page. Only that page is then joined to users and
	// to its score totals, so a broad query costs one rank per match, not one
	// score lookup per match.
	//
	// The LATERAL subquery runs once per page row: it sums that application's
	// scores per rubric version and keeps the latest version, as ScoreCard
	// does. LEFT JOIN with ON true keeps an application with no scores, whose
	// totals come back NULL.
	//
	// ts_rank returns real (float4); it is widened only on output, so the
	// ORDER BY compares the values PostgreSQL computed.
	const pageQuery = `
		SELECT p.id::text, p.candidate_id::text, u.full_name, p.status, p.submitted_at,
		       p.relevance::float8, t.rubric_version, t.earned, t.available
		FROM (
			SELECT a.id, a.candidate_id, a.status, a.submitted_at,
			       ts_rank(a.search_vector, tsq) AS relevance
			FROM applications a, plainto_tsquery('english', $2) AS tsq
			WHERE a.job_post_id = $1::text::uuid
			  AND a.search_vector @@ tsq
			ORDER BY relevance DESC, a.submitted_at, a.id
			LIMIT $3 OFFSET $4
		) AS p
		JOIN users u ON u.id = p.candidate_id
		LEFT JOIN LATERAL (
			SELECT s.rubric_version, sum(s.earned)::float8 AS earned, sum(s.available)::float8 AS available
			FROM scores s
			WHERE s.application_id = p.id
			GROUP BY s.rubric_version
			ORDER BY s.rubric_version DESC
			LIMIT 1
		) AS t ON true
		ORDER BY p.relevance DESC, p.submitted_at, p.id`

	rows, err := tx.Query(ctx, pageQuery, jobPostID, query, limit, offset)
	if err != nil {
		return nil, 0, translate(op, err)
	}
	results, err := pgx.CollectRows(rows, scanSearchResult)
	if err != nil {
		return nil, 0, translate(op, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, 0, translate(op+": commit", err)
	}

	if results == nil {
		results = []domain.SearchResult{}
	}
	return results, total, nil
}

// scanSearchResult reads one row of the SearchApplications page query.
func scanSearchResult(row pgx.CollectableRow) (domain.SearchResult, error) {
	var (
		r domain.SearchResult
		// NULL together when the application has no scores, so pointers.
		version           *int
		earned, available *float64
	)
	err := row.Scan(&r.ApplicationID, &r.CandidateID, &r.CandidateName, &r.Status, &r.SubmittedAt,
		&r.Relevance, &version, &earned, &available)
	if err != nil {
		return domain.SearchResult{}, err
	}
	if version != nil && earned != nil && available != nil {
		r.Totals = &domain.ScoreTotals{RubricVersion: *version, Earned: *earned, Available: *available}
	}
	return r, nil
}

// RebuildSearchDocuments recomputes the search document of every application
// from its stored answers and writes it where it differs from what is
// stored. It returns how many it rewrote and how many applications there are.
//
// search_vector cannot drift from search_document, because PostgreSQL
// computes one from the other. search_document can drift from the answers:
// a bug in a future write path, a hand-edited answer, or a change to the
// rule itself. This is the recovery, and rewritten > 0 is the signal that
// something drifted.
//
// The rule is domain.SearchDocument's, in SQL: text and choice answers, in
// form order, joined by newlines, cut to domain.MaxSearchDocumentChars
// characters. TestRebuildSearchDocumentsAgreesWithSubmit holds the two
// together.
//
// It is one statement, so it is atomic, and safe to run while the API is
// serving: an application submitted meanwhile is already correct.
func (s *ApplicationStore) RebuildSearchDocuments(ctx context.Context) (rewritten, total int, err error) {
	// LEFT JOINs, so an application with no answers is still a row of doc,
	// and a stale document on it is reset to ''. FILTER drops number and
	// boolean answers from the aggregate without dropping the application;
	// string_agg over no rows is NULL, hence the coalesce.
	//
	// A data-modifying CTE: changed runs the UPDATE, and the final SELECT
	// counts what it returned. The comparison in its WHERE skips rows that
	// are already right, so a healthy database is read, not rewritten.
	const query = `
		WITH doc AS (
			SELECT a.id,
			       coalesce(left(
			           string_agg(aa.answer_text, E'\n' ORDER BY q.position)
			               FILTER (WHERE q.kind IN ('text', 'choice')),
			           $1::int), '') AS text
			FROM applications a
			LEFT JOIN application_answers aa ON aa.application_id = a.id
			LEFT JOIN questions q ON q.id = aa.question_id
			GROUP BY a.id
		), changed AS (
			UPDATE applications a
			SET search_document = doc.text
			FROM doc
			WHERE a.id = doc.id
			  AND a.search_document <> doc.text
			RETURNING a.id
		)
		SELECT (SELECT count(*) FROM changed)::int, (SELECT count(*) FROM doc)::int`

	if err = s.db.QueryRow(ctx, query, domain.MaxSearchDocumentChars).Scan(&rewritten, &total); err != nil {
		return 0, 0, translate("rebuild search documents", err)
	}
	return rewritten, total, nil
}
