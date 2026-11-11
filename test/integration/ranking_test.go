//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// TestRankApplications pins what the ranking returns, whichever way it is
// computed: best total first, ties by earliest submission, unscored last,
// and no more than limit rows. The N+1 version and the single query that
// replaces it must both pass it unchanged.
func TestRankApplications(t *testing.T) {
	pool := startPostgres(t)
	seedScoring(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	// Years 0 to 8 give totals from 52.5 to 92.5; i % 9 repeats, so some
	// totals tie and the tie-break is exercised.
	ids := addCandidates(t, pool, 12)

	// Two applications lose their scores: they must still be ranked, last.
	unscored := map[string]bool{ids[3]: true, ids[7]: true}
	if _, err := pool.Exec(ctx, `DELETE FROM scores WHERE application_id = ANY($1::text[]::uuid[])`,
		[]string{ids[3], ids[7]}); err != nil {
		t.Fatalf("unscore: %v", err)
	}

	ranked, err := apps.RankApplications(ctx, subOpenPostID, domain.MaxRankingLimit)
	if err != nil {
		t.Fatalf("RankApplications: %v", err)
	}
	if len(ranked) != len(ids) {
		t.Fatalf("got %d rows, want all %d applications, unscored included", len(ranked), len(ids))
	}

	for i, r := range ranked {
		if r.Rank != i+1 || r.CandidateName != "Bulk" {
			t.Errorf("row %d: rank %d, name %q", i, r.Rank, r.CandidateName)
		}
		if unscored[r.ApplicationID] != (r.Totals == nil) {
			t.Errorf("row %d (%s): totals %+v, unscored %v", i, r.ApplicationID, r.Totals, unscored[r.ApplicationID])
		}
		if i == 0 {
			continue
		}
		prev := ranked[i-1]
		switch {
		case prev.Totals == nil && r.Totals != nil:
			t.Errorf("row %d is scored but follows an unscored row", i)
		case prev.Totals != nil && r.Totals != nil && prev.Totals.Earned < r.Totals.Earned:
			t.Errorf("row %d: %v ranks below %v", i, r.Totals.Earned, prev.Totals.Earned)
		case prev.Totals != nil && r.Totals != nil && prev.Totals.Earned == r.Totals.Earned && prev.SubmittedAt.After(r.SubmittedAt):
			t.Errorf("row %d: a tie is not broken by earliest submission", i)
		}
	}
	if top := ranked[0].Totals; top == nil || top.Earned != 92.5 || top.Available != 100 || top.RubricVersion != 1 {
		t.Errorf("top = %+v, want 92.5 of 100 at version 1", top)
	}

	if short, err := apps.RankApplications(ctx, subOpenPostID, 3); err != nil || len(short) != 3 || short[0].ApplicationID != ranked[0].ApplicationID {
		t.Errorf("limit 3: %d rows, %v", len(short), err)
	}
	if _, err := apps.RankApplications(ctx, "44444444-0000-4000-8000-0000000000ee", 10); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing post: %v, want ErrNotFound", err)
	}
}
