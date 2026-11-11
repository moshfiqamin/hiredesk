//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// addQuestion gives the post one question at position.
func (f *postFixture) addQuestion(t *testing.T, postID string, position int) {
	t.Helper()

	if _, err := f.store.db.Exec(context.Background(),
		`INSERT INTO questions (job_post_id, prompt, kind, position) VALUES ($1::text::uuid, 'Why?', 'text', $2)`,
		postID, position,
	); err != nil {
		t.Fatalf("add question: %v", err)
	}
}

// addCriterion gives the post one rubric criterion in version with weight.
func (f *postFixture) addCriterion(t *testing.T, postID string, version int, weight string) {
	t.Helper()

	if _, err := f.store.db.Exec(context.Background(),
		`INSERT INTO rubric_criteria (job_post_id, label, kind, weight, rubric_version, position)
		 VALUES ($1::text::uuid, 'Go experience', 'experience_years', $2::numeric, $3,
		         (SELECT coalesce(max(position), 0) + 1 FROM rubric_criteria
		          WHERE job_post_id = $1::text::uuid AND rubric_version = $3))`,
		postID, weight, version,
	); err != nil {
		t.Fatalf("add criterion: %v", err)
	}
}

// readyPost creates a draft with a question and a weighted criterion.
func (f *postFixture) readyPost(t *testing.T, title string) *domain.JobPost {
	t.Helper()

	post := f.create(t, title, nil, nil)
	f.addQuestion(t, post.ID, 1)
	f.addCriterion(t, post.ID, 1, "1.00")
	return post
}

// storedEvent is one row of job_post_events, as the tests read it back.
type storedEvent struct {
	actorID string
	typ     string
	payload map[string]string
}

func (f *postFixture) events(t *testing.T, postID string) []storedEvent {
	t.Helper()

	rows, err := f.store.db.Query(context.Background(),
		`SELECT coalesce(actor_id::text, ''), type, payload FROM job_post_events
		 WHERE job_post_id = $1::text::uuid ORDER BY created_at, id`, postID)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	defer rows.Close()

	var out []storedEvent
	for rows.Next() {
		var (
			e   storedEvent
			raw []byte
		)
		if err := rows.Scan(&e.actorID, &e.typ, &raw); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		if err := json.Unmarshal(raw, &e.payload); err != nil {
			t.Fatalf("event payload %s: %v", raw, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read events: %v", err)
	}
	return out
}

func TestJobPostStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)

	t.Run("draft to open to closed, with an event for each", func(t *testing.T) {
		post := f.readyPost(t, "Lifecycle")

		opened, err := f.store.TransitionJobPost(ctx, post.ID, domain.JobStatusOpen, f.admin.ID)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if opened.Status != domain.JobStatusOpen || len(opened.Questions) != 1 || !opened.UpdatedAt.After(post.UpdatedAt) {
			t.Errorf("opened = %+v", opened)
		}

		closed, err := f.store.TransitionJobPost(ctx, post.ID, domain.JobStatusClosed, f.admin.ID)
		if err != nil {
			t.Fatalf("close: %v", err)
		}
		if closed.Status != domain.JobStatusClosed {
			t.Errorf("status = %q, want closed", closed.Status)
		}

		events := f.events(t, post.ID)
		want := []map[string]string{{"from": "draft", "to": "open"}, {"from": "open", "to": "closed"}}
		if len(events) != len(want) {
			t.Fatalf("events = %+v, want %d", events, len(want))
		}
		for i, e := range events {
			if e.typ != "status_changed" || e.actorID != f.admin.ID ||
				e.payload["from"] != want[i]["from"] || e.payload["to"] != want[i]["to"] || len(e.payload) != 2 {
				t.Errorf("event %d = %+v, want status_changed by the admin with %v", i, e, want[i])
			}
		}
	})

	t.Run("illegal moves are refused and write nothing", func(t *testing.T) {
		draft := f.readyPost(t, "Refusals")

		check := func(id, to string, wantAllowed []string) {
			t.Helper()

			_, err := f.store.TransitionJobPost(ctx, id, to, f.admin.ID)
			var refused *domain.TransitionError
			if !errors.As(err, &refused) {
				t.Fatalf("%s: err = %v, want a *domain.TransitionError", to, err)
			}
			if !slices.Equal(refused.Allowed, wantAllowed) {
				t.Errorf("%s: allowed = %v, want %v", to, refused.Allowed, wantAllowed)
			}
		}

		check(draft.ID, domain.JobStatusClosed, []string{domain.JobStatusOpen})
		check(draft.ID, domain.JobStatusDraft, []string{domain.JobStatusOpen})

		if _, err := f.store.TransitionJobPost(ctx, draft.ID, domain.JobStatusOpen, f.admin.ID); err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := f.store.TransitionJobPost(ctx, draft.ID, domain.JobStatusClosed, f.admin.ID); err != nil {
			t.Fatalf("close: %v", err)
		}
		check(draft.ID, domain.JobStatusOpen, []string{})

		// Two successful moves, two events; the four refusals added none.
		if n := len(f.events(t, draft.ID)); n != 2 {
			t.Errorf("%d events, want 2", n)
		}
	})

	t.Run("opening needs questions and a weighted rubric", func(t *testing.T) {
		tests := []struct {
			name        string
			setup       func(id string)
			wantMissing []string
		}{
			{"nothing", func(string) {}, []string{"questions", "rubric"}},
			{"a rubric but no questions", func(id string) { f.addCriterion(t, id, 1, "2.00") }, []string{"questions"}},
			{"questions but no rubric", func(id string) { f.addQuestion(t, id, 1) }, []string{"rubric"}},
			{"a rubric weighted zero", func(id string) {
				f.addQuestion(t, id, 1)
				f.addCriterion(t, id, 1, "0")
			}, []string{"rubric"}},
			// Only the current version counts: a weighted version 1 does not
			// rescue an unweighted version 2.
			{"an older version weighted, the current one not", func(id string) {
				f.addQuestion(t, id, 1)
				f.addCriterion(t, id, 1, "3.00")
				f.addCriterion(t, id, 2, "0")
			}, []string{"rubric"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				post := f.create(t, "Unready", nil, nil)
				tt.setup(post.ID)

				_, err := f.store.TransitionJobPost(ctx, post.ID, domain.JobStatusOpen, f.admin.ID)

				var notReady *domain.NotReadyError
				if !errors.As(err, &notReady) {
					t.Fatalf("err = %v, want a *domain.NotReadyError", err)
				}
				if len(notReady.Missing) != len(tt.wantMissing) {
					t.Errorf("missing = %v, want exactly %v", notReady.Missing, tt.wantMissing)
				}
				for _, key := range tt.wantMissing {
					if notReady.Missing[key] == "" {
						t.Errorf("no message for %q; missing = %v", key, notReady.Missing)
					}
				}

				stored, getErr := f.store.GetJobPostByID(ctx, post.ID, true)
				if getErr != nil || stored.Status != domain.JobStatusDraft {
					t.Errorf("status = %q, err = %v; want it still a draft", stored.Status, getErr)
				}
				if n := len(f.events(t, post.ID)); n != 0 {
					t.Errorf("%d events for a refused open, want 0", n)
				}
			})
		}
	})

	t.Run("unknown and malformed IDs are not found", func(t *testing.T) {
		for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
			_, err := f.store.TransitionJobPost(ctx, id, domain.JobStatusOpen, f.admin.ID)
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("TransitionJobPost(%q): err = %v, want ErrNotFound", id, err)
			}
		}
	})
}

// Two admins open the same draft at the same moment. The row lock makes the
// second wait and then see an open post, so exactly one succeeds and exactly
// one event is recorded.
func TestJobPostStoreTransitionConcurrently(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)

	const rounds = 20
	for round := 0; round < rounds; round++ {
		post := f.readyPost(t, "Raced open")

		var (
			wg   sync.WaitGroup
			errs = make([]error, 2)
		)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = f.store.TransitionJobPost(ctx, post.ID, domain.JobStatusOpen, f.admin.ID)
			}()
		}
		wg.Wait()

		succeeded := 0
		for _, err := range errs {
			switch {
			case err == nil:
				succeeded++
			case !errors.Is(err, domain.ErrInvalidTransition):
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("round %d: %d opens succeeded, want exactly 1", round, succeeded)
		}
		if n := len(f.events(t, post.ID)); n != 1 {
			t.Fatalf("round %d: %d events, want 1", round, n)
		}
	}
}
