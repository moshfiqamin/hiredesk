//go:build integration

package store

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// questionFixture is a draft post, owned by postFixture's admin, and a
// QuestionStore on the same pool.
type questionFixture struct {
	*postFixture
	questions *QuestionStore
}

func newQuestionFixture(t *testing.T) *questionFixture {
	t.Helper()

	f := newPostFixture(t)
	return &questionFixture{postFixture: f, questions: NewQuestionStore(f.store.db)}
}

// add creates a text question on postID at position (0 for the next slot).
func (f *questionFixture) add(t *testing.T, postID, prompt string, position int) *domain.Question {
	t.Helper()

	q, err := f.questions.CreateQuestion(context.Background(), postID, domain.NewQuestion{
		Prompt: prompt, Kind: domain.QuestionKindText, Required: true, Position: position,
	})
	if err != nil {
		t.Fatalf("CreateQuestion(%q): %v", prompt, err)
	}
	return q
}

// order returns the post's question IDs and positions as stored.
func (f *questionFixture) order(t *testing.T, postID string) ([]string, []int) {
	t.Helper()

	qs, err := f.questions.ListQuestionsByJobPost(context.Background(), postID)
	if err != nil {
		t.Fatalf("ListQuestionsByJobPost: %v", err)
	}
	ids, positions := make([]string, 0, len(qs)), make([]int, 0, len(qs))
	for _, q := range qs {
		ids = append(ids, q.ID)
		positions = append(positions, q.Position)
	}
	return ids, positions
}

func TestQuestionStoreCreate(t *testing.T) {
	ctx := context.Background()
	f := newQuestionFixture(t)
	post := f.create(t, "Form", nil, nil)

	first := f.add(t, post.ID, "First", 0)
	second := f.add(t, post.ID, "Second", 0)
	if first.Position != 1 || second.Position != 2 {
		t.Errorf("positions = %d, %d; want 1, 2", first.Position, second.Position)
	}

	t.Run("every field round-trips, and no choices is NULL", func(t *testing.T) {
		q, err := f.questions.CreateQuestion(ctx, post.ID, domain.NewQuestion{
			Prompt: "Office", Kind: domain.QuestionKindChoice, Choices: []string{"Dhaka", "Remote"}, Position: 10,
		})
		if err != nil {
			t.Fatalf("CreateQuestion: %v", err)
		}
		if q.Kind != domain.QuestionKindChoice || !slices.Equal(q.Choices, []string{"Dhaka", "Remote"}) ||
			q.Required || q.Position != 10 || q.JobPostID != post.ID {
			t.Errorf("question = %+v", q)
		}

		var nullChoices bool
		if err := f.store.db.QueryRow(ctx,
			`SELECT choices IS NULL FROM questions WHERE id = $1::text::uuid`, first.ID,
		).Scan(&nullChoices); err != nil || !nullChoices {
			t.Errorf("text question's choices IS NULL = %v, err = %v; want true", nullChoices, err)
		}
	})

	t.Run("the next slot follows the highest position, not the count", func(t *testing.T) {
		if q := f.add(t, post.ID, "After the gap", 0); q.Position != 11 {
			t.Errorf("position = %d, want 11", q.Position)
		}
	})

	t.Run("a taken position is a validation error on position", func(t *testing.T) {
		_, err := f.questions.CreateQuestion(ctx, post.ID, domain.NewQuestion{Prompt: "Dup", Kind: "text", Position: 1})

		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) || invalid.Fields["position"] == "" {
			t.Errorf("err = %v, want a ValidationError on position", err)
		}
	})

	t.Run("positions are per post", func(t *testing.T) {
		other := f.create(t, "Other form", nil, nil)
		if q := f.add(t, other.ID, "Also first", 1); q.Position != 1 {
			t.Errorf("position = %d, want 1", q.Position)
		}
	})

	t.Run("a missing or malformed post is not found", func(t *testing.T) {
		for _, id := range []string{"00000000-0000-4000-8000-000000000000", "nope"} {
			_, err := f.questions.CreateQuestion(ctx, id, domain.NewQuestion{Prompt: "x", Kind: "text"})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("CreateQuestion on %q: err = %v, want ErrNotFound", id, err)
			}
		}
	})
}

func TestQuestionStoreUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	f := newQuestionFixture(t)
	post := f.create(t, "Form", nil, nil)
	a := f.add(t, post.ID, "A", 0)
	b := f.add(t, post.ID, "B", 0)

	t.Run("a partial update", func(t *testing.T) {
		got, err := f.questions.UpdateQuestion(ctx, post.ID, a.ID, domain.QuestionPatch{
			Prompt: ptr("A, reworded"), MinLength: ptr(20),
		})
		if err != nil {
			t.Fatalf("UpdateQuestion: %v", err)
		}
		if got.Prompt != "A, reworded" || got.MinLength != 20 || got.Kind != "text" || !got.Required || got.Position != 1 {
			t.Errorf("question = %+v", got)
		}
	})

	t.Run("text to choice and back, choices stored then cleared", func(t *testing.T) {
		got, err := f.questions.UpdateQuestion(ctx, post.ID, b.ID, domain.QuestionPatch{
			Kind: ptr(domain.QuestionKindChoice), Choices: ptr([]string{"Yes", "No"}),
		})
		if err != nil || !slices.Equal(got.Choices, []string{"Yes", "No"}) {
			t.Fatalf("to choice: %+v, %v", got, err)
		}

		got, err = f.questions.UpdateQuestion(ctx, post.ID, b.ID, domain.QuestionPatch{
			Kind: ptr(domain.QuestionKindBoolean), Choices: ptr([]string{}),
		})
		if err != nil || got.Choices != nil {
			t.Fatalf("to boolean: choices = %#v, err = %v; want nil (NULL)", got.Choices, err)
		}
	})

	t.Run("refused patches write nothing", func(t *testing.T) {
		patches := map[string]domain.QuestionPatch{
			"a taken position":            {Position: ptr(2)},
			"min_length on a non-text":    {Kind: ptr(domain.QuestionKindNumber)}, // A has min_length 20
			"choice without choices":      {Kind: ptr(domain.QuestionKindChoice)},
			"a good field beside bad one": {Prompt: ptr("Changed"), Position: ptr(0)},
		}
		for name, patch := range patches {
			var invalid *domain.ValidationError
			if _, err := f.questions.UpdateQuestion(ctx, post.ID, a.ID, patch); !errors.As(err, &invalid) {
				t.Errorf("%s: err = %v, want a ValidationError", name, err)
			}
		}

		qs, _ := f.questions.ListQuestionsByJobPost(ctx, post.ID)
		if qs[0].Prompt != "A, reworded" || qs[0].Position != 1 || qs[0].Kind != "text" {
			t.Errorf("a refused patch was written: %+v", qs[0])
		}
	})

	t.Run("moving to a free position", func(t *testing.T) {
		if got, err := f.questions.UpdateQuestion(ctx, post.ID, a.ID, domain.QuestionPatch{Position: ptr(5)}); err != nil || got.Position != 5 {
			t.Fatalf("move: %+v, %v", got, err)
		}
		ids, _ := f.order(t, post.ID)
		if !slices.Equal(ids, []string{b.ID, a.ID}) {
			t.Errorf("order = %v, want B then A", ids)
		}
	})

	t.Run("a question of another post is not found", func(t *testing.T) {
		other := f.create(t, "Other", nil, nil)
		if _, err := f.questions.UpdateQuestion(ctx, other.ID, a.ID, domain.QuestionPatch{Prompt: ptr("x")}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("update via another post: err = %v, want ErrNotFound", err)
		}
		if err := f.questions.DeleteQuestion(ctx, other.ID, a.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("delete via another post: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("delete, then delete again", func(t *testing.T) {
		if err := f.questions.DeleteQuestion(ctx, post.ID, b.ID); err != nil {
			t.Fatalf("DeleteQuestion: %v", err)
		}
		if err := f.questions.DeleteQuestion(ctx, post.ID, b.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("second delete: err = %v, want ErrNotFound", err)
		}
		ids, _ := f.order(t, post.ID)
		if !slices.Equal(ids, []string{a.ID}) {
			t.Errorf("remaining = %v, want only A", ids)
		}
	})

	t.Run("deleting a question deletes the criteria that score it", func(t *testing.T) {
		q := f.add(t, post.ID, "Scored", 0)
		if _, err := f.store.db.Exec(ctx,
			`INSERT INTO rubric_criteria (job_post_id, label, kind, weight, question_id, position)
			 VALUES ($1::text::uuid, 'Depth', 'answer_depth', 1, $2::text::uuid, 1)`, post.ID, q.ID,
		); err != nil {
			t.Fatalf("add criterion: %v", err)
		}
		if err := f.questions.DeleteQuestion(ctx, post.ID, q.ID); err != nil {
			t.Fatalf("DeleteQuestion: %v", err)
		}
		var n int
		if err := f.store.db.QueryRow(ctx,
			`SELECT count(*) FROM rubric_criteria WHERE question_id = $1::text::uuid`, q.ID,
		).Scan(&n); err != nil || n != 0 {
			t.Errorf("criteria left = %d, err = %v; want 0", n, err)
		}
	})
}

func TestQuestionStoreReorder(t *testing.T) {
	ctx := context.Background()
	f := newQuestionFixture(t)
	post := f.create(t, "Form", nil, nil)

	var ids []string
	for _, prompt := range []string{"A", "B", "C", "D", "E"} {
		ids = append(ids, f.add(t, post.ID, prompt, 0).ID)
	}

	// Every one of these moves at least one question into a position another
	// still holds at that instant. Without the deferred constraint each fails.
	orders := map[string][]string{
		"reverse":         {ids[4], ids[3], ids[2], ids[1], ids[0]},
		"swap first two":  {ids[1], ids[0], ids[2], ids[3], ids[4]},
		"rotate":          {ids[1], ids[2], ids[3], ids[4], ids[0]},
		"back to start":   ids,
		"last to first":   {ids[4], ids[0], ids[1], ids[2], ids[3]},
		"unchanged again": {ids[4], ids[0], ids[1], ids[2], ids[3]},
	}
	for _, name := range []string{"reverse", "swap first two", "rotate", "back to start", "last to first", "unchanged again"} {
		want := orders[name]
		t.Run(name, func(t *testing.T) {
			got, err := f.questions.ReorderQuestions(ctx, post.ID, want)
			if err != nil {
				t.Fatalf("ReorderQuestions: %v", err)
			}
			gotIDs := make([]string, len(got))
			for i, q := range got {
				gotIDs[i] = q.ID
				if q.Position != i+1 {
					t.Errorf("question %d has position %d, want %d", i, q.Position, i+1)
				}
			}
			if !slices.Equal(gotIDs, want) {
				t.Errorf("returned order = %v, want %v", gotIDs, want)
			}
			if stored, positions := f.order(t, post.ID); !slices.Equal(stored, want) || !slices.Equal(positions, []int{1, 2, 3, 4, 5}) {
				t.Errorf("stored = %v at %v", stored, positions)
			}
		})
	}

	t.Run("gaps are closed", func(t *testing.T) {
		gappy := f.create(t, "Gappy", nil, nil)
		x := f.add(t, gappy.ID, "X", 10)
		y := f.add(t, gappy.ID, "Y", 40)

		if _, err := f.questions.ReorderQuestions(ctx, gappy.ID, []string{y.ID, x.ID}); err != nil {
			t.Fatalf("ReorderQuestions: %v", err)
		}
		if _, positions := f.order(t, gappy.ID); !slices.Equal(positions, []int{1, 2}) {
			t.Errorf("positions = %v, want [1 2]", positions)
		}
	})

	t.Run("anything but every question exactly once is refused, and nothing moves", func(t *testing.T) {
		before, _ := f.order(t, post.ID)
		bad := map[string][]string{
			"empty":          {},
			"one missing":    ids[:4],
			"one repeated":   {ids[0], ids[0], ids[1], ids[2], ids[3]},
			"one unknown":    {ids[0], ids[1], ids[2], ids[3], "00000000-0000-4000-8000-000000000000"},
			"one extra":      append(slices.Clone(ids), "00000000-0000-4000-8000-000000000000"),
			"a malformed id": {ids[0], ids[1], ids[2], ids[3], "nope"},
		}
		for name, order := range bad {
			var invalid *domain.ValidationError
			if _, err := f.questions.ReorderQuestions(ctx, post.ID, order); !errors.As(err, &invalid) || invalid.Fields["question_ids"] == "" {
				t.Errorf("%s: err = %v, want a ValidationError on question_ids", name, err)
			}
		}
		if after, _ := f.order(t, post.ID); !slices.Equal(after, before) {
			t.Errorf("order changed from %v to %v", before, after)
		}
	})

	t.Run("an empty form reorders to nothing", func(t *testing.T) {
		empty := f.create(t, "Empty", nil, nil)
		got, err := f.questions.ReorderQuestions(ctx, empty.ID, []string{})
		if err != nil || len(got) != 0 {
			t.Errorf("got %v, err = %v; want an empty form", got, err)
		}
	})
}

// Two admins reorder the same form at once, in opposite directions. The
// post's lock runs them one after the other, so the form ends in exactly one
// of the two orders, with positions 1 to n, and neither fails.
func TestQuestionStoreReorderConcurrently(t *testing.T) {
	ctx := context.Background()
	f := newQuestionFixture(t)

	const rounds = 20
	for round := 0; round < rounds; round++ {
		post := f.create(t, "Raced form", nil, nil)
		var ids []string
		for _, prompt := range []string{"A", "B", "C", "D"} {
			ids = append(ids, f.add(t, post.ID, prompt, 0).ID)
		}
		forward := slices.Clone(ids)
		backward := slices.Clone(ids)
		slices.Reverse(backward)

		var (
			wg   sync.WaitGroup
			errs = make([]error, 2)
		)
		for i, order := range [][]string{forward, backward} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = f.questions.ReorderQuestions(ctx, post.ID, order)
			}()
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: reorder %d: %v", round, i, err)
			}
		}
		got, positions := f.order(t, post.ID)
		if !slices.Equal(positions, []int{1, 2, 3, 4}) || (!slices.Equal(got, forward) && !slices.Equal(got, backward)) {
			t.Fatalf("round %d: order %v at %v, want one of the two requested", round, got, positions)
		}
	}
}

// The critical rule, against PostgreSQL: once a post is open or closed, every
// write is a FormLockedError and nothing changes.
func TestQuestionStoreLockedOutsideDraft(t *testing.T) {
	ctx := context.Background()
	f := newQuestionFixture(t)

	for _, status := range []string{domain.JobStatusOpen, domain.JobStatusClosed} {
		t.Run(status, func(t *testing.T) {
			post := f.create(t, "Locked "+status, nil, nil)
			a := f.add(t, post.ID, "A", 0)
			b := f.add(t, post.ID, "B", 0)
			if _, err := f.store.db.Exec(ctx,
				`UPDATE job_posts SET status = $2 WHERE id = $1::text::uuid`, post.ID, status,
			); err != nil {
				t.Fatalf("set status: %v", err)
			}

			attempts := map[string]error{}
			_, attempts["create"] = f.questions.CreateQuestion(ctx, post.ID, domain.NewQuestion{Prompt: "New", Kind: "text"})
			_, attempts["update"] = f.questions.UpdateQuestion(ctx, post.ID, a.ID, domain.QuestionPatch{Prompt: ptr("Changed")})
			attempts["delete"] = f.questions.DeleteQuestion(ctx, post.ID, a.ID)
			_, attempts["reorder"] = f.questions.ReorderQuestions(ctx, post.ID, []string{b.ID, a.ID})

			// The lock is checked before the question is looked up, so even a
			// question that does not exist gets the lock error, not a 404.
			attempts["delete missing"] = f.questions.DeleteQuestion(ctx, post.ID, "00000000-0000-4000-8000-000000000000")

			for name, err := range attempts {
				var locked *domain.FormLockedError
				if !errors.As(err, &locked) || locked.Status != status {
					t.Errorf("%s: err = %v, want a FormLockedError for %s", name, err, status)
				}
			}

			qs, err := f.questions.ListQuestionsByJobPost(ctx, post.ID)
			if err != nil || len(qs) != 2 || qs[0].ID != a.ID || qs[0].Prompt != "A" || qs[0].Position != 1 {
				t.Errorf("form = %+v, err = %v; want it unchanged", qs, err)
			}
		})
	}
}

// An admin opens the post while another edits its form. The opening is
// simulated by hand, so the test controls the timing exactly: a transaction
// takes the post's lock and sets it to open, and does not commit until each
// form edit has been started. Every edit must wait for that transaction, and
// then be refused, because the form it would change is now open.
//
// Without the lock in lockEditableForm, an edit reads the old committed
// status, draft, and goes ahead; an update or delete even finishes before the
// opening commits.
func TestQuestionEditWaitsForAnOpeningInProgress(t *testing.T) {
	ctx := context.Background()
	f := newQuestionFixture(t)

	edits := map[string]func(postID, questionID string) error{
		"create": func(postID, _ string) error {
			_, err := f.questions.CreateQuestion(ctx, postID, domain.NewQuestion{Prompt: "Late", Kind: "text"})
			return err
		},
		"update": func(postID, questionID string) error {
			_, err := f.questions.UpdateQuestion(ctx, postID, questionID, domain.QuestionPatch{Prompt: ptr("Late")})
			return err
		},
		"delete": func(postID, questionID string) error {
			return f.questions.DeleteQuestion(ctx, postID, questionID)
		},
		"reorder": func(postID, questionID string) error {
			_, err := f.questions.ReorderQuestions(ctx, postID, []string{questionID})
			return err
		},
	}

	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			post := f.readyPost(t, "Opening under "+name)
			ids, _ := f.order(t, post.ID)

			tx, err := f.store.db.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer tx.Rollback(ctx)

			if _, err := tx.Exec(ctx, `SELECT 1 FROM job_posts WHERE id = $1::text::uuid FOR UPDATE`, post.ID); err != nil {
				t.Fatalf("lock: %v", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE job_posts SET status = 'open' WHERE id = $1::text::uuid`, post.ID); err != nil {
				t.Fatalf("open: %v", err)
			}

			done := make(chan error, 1)
			go func() { done <- edit(post.ID, ids[0]) }()

			// The edit must still be waiting on the lock.
			select {
			case err := <-done:
				t.Fatalf("the edit finished while the opening was uncommitted: err = %v", err)
			case <-time.After(300 * time.Millisecond):
			}

			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit: %v", err)
			}

			select {
			case err := <-done:
				var locked *domain.FormLockedError
				if !errors.As(err, &locked) || locked.Status != domain.JobStatusOpen {
					t.Errorf("err = %v, want a FormLockedError for open", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the edit never finished")
			}

			if after, _ := f.order(t, post.ID); !slices.Equal(after, ids) {
				t.Errorf("form changed from %v to %v", ids, after)
			}
		})
	}
}
