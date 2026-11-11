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

// ptr returns a pointer to v, for filling pointer fields from literals.
func ptr[T any](v T) *T {
	return &v
}

// postFixture is a JobPostStore and an admin to create posts as. Every post
// the admin created is removed, and then the admin, when the test ends.
type postFixture struct {
	store *JobPostStore
	admin domain.User
}

func newPostFixture(t *testing.T) *postFixture {
	t.Helper()
	ctx := context.Background()

	users := newTestStore(t)

	in := newCandidate(t, users)
	in.Role = domain.RoleAdmin
	admin, err := users.CreateUser(ctx, in)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	// Cleanups run last-registered first, so this runs before newCandidate's
	// removal of the admin. Deleting the admin first would only null
	// created_by and leave the posts behind.
	t.Cleanup(func() {
		if _, err := users.db.Exec(ctx, `DELETE FROM job_posts WHERE created_by = $1::text::uuid`, admin.ID); err != nil {
			t.Errorf("clean up posts: %v", err)
		}
	})

	return &postFixture{store: NewJobPostStore(users.db), admin: *admin}
}

// create stores a post with the given title and salary range.
func (f *postFixture) create(t *testing.T, title string, salaryMin, salaryMax *domain.Money) *domain.JobPost {
	t.Helper()

	post, err := f.store.CreateJobPost(context.Background(), domain.NewJobPost{
		Title: title, Vacancies: 1, SalaryMin: salaryMin, SalaryMax: salaryMax, CreatedBy: f.admin.ID,
	})
	if err != nil {
		t.Fatalf("CreateJobPost(%q): %v", title, err)
	}
	return post
}

// publish moves a post out of draft, which no store method does yet.
func (f *postFixture) publish(t *testing.T, id string) {
	t.Helper()

	if _, err := f.store.db.Exec(context.Background(),
		`UPDATE job_posts SET status = 'open' WHERE id = $1::text::uuid`, id,
	); err != nil {
		t.Fatalf("publish %s: %v", id, err)
	}
}

func TestJobPostStoreCreate(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)

	t.Run("a title alone", func(t *testing.T) {
		post := f.create(t, "Minimal", nil, nil)

		if post.ID == "" || post.CreatedAt.IsZero() || post.UpdatedAt.IsZero() {
			t.Errorf("database-assigned fields are missing: %+v", post)
		}
		if post.Status != domain.JobStatusDraft {
			t.Errorf("Status = %q, want draft", post.Status)
		}
		if post.Description != nil || post.EmploymentType != nil || post.Location != nil ||
			post.SalaryMin != nil || post.SalaryMax != nil || post.ClosesAt != nil {
			t.Errorf("unset fields are not nil: %+v", post)
		}
		if post.CreatedBy == nil || *post.CreatedBy != f.admin.ID {
			t.Errorf("CreatedBy = %v, want %s", post.CreatedBy, f.admin.ID)
		}
		if post.Questions == nil || len(post.Questions) != 0 {
			t.Errorf("Questions = %v, want an empty, non-nil slice", post.Questions)
		}
	})

	t.Run("every field survives the round trip", func(t *testing.T) {
		closes := time.Date(2026, time.December, 31, 12, 0, 0, 0, time.UTC)
		in := domain.NewJobPost{
			Title:          "Complete",
			Description:    ptr("Runs the platform."),
			EmploymentType: ptr("full-time"),
			Location:       ptr("Remote"),
			Vacancies:      3,
			// 85000.10 is the kind of amount a float64 cannot hold exactly.
			SalaryMin: ptr(domain.Money(8500010)),
			SalaryMax: ptr(domain.Money(999999999999)),
			ClosesAt:  &closes,
			CreatedBy: f.admin.ID,
		}

		created, err := f.store.CreateJobPost(ctx, in)
		if err != nil {
			t.Fatalf("CreateJobPost: %v", err)
		}
		got, err := f.store.GetJobPostByID(ctx, created.ID, true)
		if err != nil {
			t.Fatalf("GetJobPostByID: %v", err)
		}

		if got.Title != in.Title || *got.Description != *in.Description || *got.EmploymentType != *in.EmploymentType ||
			*got.Location != *in.Location || got.Vacancies != in.Vacancies {
			t.Errorf("text fields: got %+v", got)
		}
		if *got.SalaryMin != *in.SalaryMin || *got.SalaryMax != *in.SalaryMax {
			t.Errorf("salaries = %d, %d; want %d, %d", *got.SalaryMin, *got.SalaryMax, *in.SalaryMin, *in.SalaryMax)
		}
		if !got.ClosesAt.Equal(closes) {
			t.Errorf("ClosesAt = %v, want %v", got.ClosesAt, closes)
		}

		// And what the column itself holds, read without this package's
		// conversion.
		var stored string
		if err := f.store.db.QueryRow(ctx,
			`SELECT salary_min::text FROM job_posts WHERE id = $1::text::uuid`, created.ID,
		).Scan(&stored); err != nil {
			t.Fatalf("read salary_min: %v", err)
		}
		if stored != "85000.10" {
			t.Errorf("salary_min column = %q, want 85000.10", stored)
		}
	})
}

func TestJobPostStoreGet(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)

	draft := f.create(t, "Draft", nil, nil)
	open := f.create(t, "Open", nil, nil)
	f.publish(t, open.ID)

	t.Run("drafts are hidden unless asked for", func(t *testing.T) {
		if _, err := f.store.GetJobPostByID(ctx, draft.ID, true); err != nil {
			t.Errorf("draft with includeDrafts: %v", err)
		}
		if _, err := f.store.GetJobPostByID(ctx, draft.ID, false); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("draft without includeDrafts: err = %v, want ErrNotFound", err)
		}
		for _, includeDrafts := range []bool{true, false} {
			if _, err := f.store.GetJobPostByID(ctx, open.ID, includeDrafts); err != nil {
				t.Errorf("open post, includeDrafts=%v: %v", includeDrafts, err)
			}
		}
	})

	t.Run("unknown and malformed IDs are not found", func(t *testing.T) {
		for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid", ""} {
			if _, err := f.store.GetJobPostByID(ctx, id, true); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("GetJobPostByID(%q): err = %v, want ErrNotFound", id, err)
			}
		}
	})

	t.Run("questions come back in position order", func(t *testing.T) {
		// Inserted out of order, so the ORDER BY is what sorts them.
		if _, err := f.store.db.Exec(ctx, `
			INSERT INTO questions (job_post_id, prompt, kind, choices, required, min_length, position) VALUES
				($1::text::uuid, 'Preferred office', 'choice', ARRAY['Dhaka', 'Remote'], false, 0,  2),
				($1::text::uuid, 'Why this role?',   'text',   NULL,                     true,  50, 1)`,
			open.ID,
		); err != nil {
			t.Fatalf("insert questions: %v", err)
		}

		got, err := f.store.GetJobPostByID(ctx, open.ID, false)
		if err != nil {
			t.Fatalf("GetJobPostByID: %v", err)
		}
		if len(got.Questions) != 2 {
			t.Fatalf("len(Questions) = %d, want 2", len(got.Questions))
		}

		first, second := got.Questions[0], got.Questions[1]
		if first.Position != 1 || first.Prompt != "Why this role?" || first.Kind != "text" ||
			!first.Required || first.MinLength != 50 || first.Choices != nil || first.JobPostID != open.ID {
			t.Errorf("first question = %+v", first)
		}
		if second.Position != 2 || second.Required || !slices.Equal(second.Choices, []string{"Dhaka", "Remote"}) {
			t.Errorf("second question = %+v", second)
		}

		// The other post's form is its own.
		other, err := f.store.GetJobPostByID(ctx, draft.ID, true)
		if err != nil || len(other.Questions) != 0 {
			t.Errorf("draft: %d questions, err = %v; want none", len(other.Questions), err)
		}
	})
}

func TestJobPostStoreUpdate(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)

	t.Run("only the fields in the patch change", func(t *testing.T) {
		before := f.create(t, "Before", ptr(domain.Money(5000000)), ptr(domain.Money(9000000)))

		after, err := f.store.UpdateJobPost(ctx, before.ID, domain.JobPostPatch{
			Title:    ptr("After"),
			Location: ptr("Dhaka"),
		})
		if err != nil {
			t.Fatalf("UpdateJobPost: %v", err)
		}

		if after.Title != "After" || after.Location == nil || *after.Location != "Dhaka" {
			t.Errorf("patched fields: %+v", after)
		}
		if after.Vacancies != 1 || *after.SalaryMin != 5000000 || *after.SalaryMax != 9000000 ||
			after.Description != nil || after.Status != domain.JobStatusDraft {
			t.Errorf("fields the patch did not set were changed: %+v", after)
		}
		if !after.CreatedAt.Equal(before.CreatedAt) {
			t.Errorf("CreatedAt moved from %v to %v", before.CreatedAt, after.CreatedAt)
		}
		if !after.UpdatedAt.After(before.UpdatedAt) {
			t.Errorf("UpdatedAt = %v, want later than %v", after.UpdatedAt, before.UpdatedAt)
		}

		// What was returned is what was stored.
		stored, err := f.store.GetJobPostByID(ctx, before.ID, true)
		if err != nil || stored.Title != "After" || *stored.Location != "Dhaka" {
			t.Errorf("stored = %+v, err = %v", stored, err)
		}
	})

	t.Run("a field set to empty is stored as empty, not as NULL", func(t *testing.T) {
		post, err := f.store.CreateJobPost(ctx, domain.NewJobPost{
			Title: "Described", Description: ptr("Some text."), Vacancies: 1, CreatedBy: f.admin.ID,
		})
		if err != nil {
			t.Fatalf("CreateJobPost: %v", err)
		}

		after, err := f.store.UpdateJobPost(ctx, post.ID, domain.JobPostPatch{
			Description: ptr(""),
			SalaryMin:   ptr(domain.Money(0)),
		})
		if err != nil {
			t.Fatalf("UpdateJobPost: %v", err)
		}
		if after.Description == nil || *after.Description != "" {
			t.Errorf("Description = %v, want a pointer to the empty string", after.Description)
		}
		if after.SalaryMin == nil || *after.SalaryMin != 0 {
			t.Errorf("SalaryMin = %v, want a pointer to 0", after.SalaryMin)
		}
	})

	t.Run("an empty patch writes nothing", func(t *testing.T) {
		before := f.create(t, "Untouched", nil, nil)

		after, err := f.store.UpdateJobPost(ctx, before.ID, domain.JobPostPatch{})
		if err != nil {
			t.Fatalf("UpdateJobPost: %v", err)
		}
		if after.Title != "Untouched" || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("post changed: title %q, updated_at %v → %v", after.Title, before.UpdatedAt, after.UpdatedAt)
		}
	})

	t.Run("the rules are checked against the stored row, and a refusal writes nothing", func(t *testing.T) {
		post := f.create(t, "Ranged", ptr(domain.Money(5000000)), ptr(domain.Money(9000000)))

		patches := map[string]domain.JobPostPatch{
			"minimum above the stored maximum": {SalaryMin: ptr(domain.Money(9000001))},
			"maximum below the stored minimum": {SalaryMax: ptr(domain.Money(4999999))},
			"empty title":                      {Title: ptr("")},
			"zero vacancies":                   {Vacancies: ptr(0)},
			// One good field beside a bad one: neither may be written.
			"a good field beside a bad one": {Location: ptr("Remote"), Vacancies: ptr(-1)},
		}
		for name, patch := range patches {
			_, err := f.store.UpdateJobPost(ctx, post.ID, patch)

			var invalid *domain.ValidationError
			if !errors.As(err, &invalid) {
				t.Errorf("%s: err = %v, want a *domain.ValidationError", name, err)
			}
		}

		stored, err := f.store.GetJobPostByID(ctx, post.ID, true)
		if err != nil {
			t.Fatalf("GetJobPostByID: %v", err)
		}
		if stored.Title != "Ranged" || stored.Vacancies != 1 || stored.Location != nil ||
			*stored.SalaryMin != 5000000 || *stored.SalaryMax != 9000000 || !stored.UpdatedAt.Equal(post.UpdatedAt) {
			t.Errorf("a refused patch was written: %+v", stored)
		}
	})

	t.Run("unknown and malformed IDs are not found", func(t *testing.T) {
		for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
			_, err := f.store.UpdateJobPost(ctx, id, domain.JobPostPatch{Title: ptr("x")})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("UpdateJobPost(%q): err = %v, want ErrNotFound", id, err)
			}
		}
	})
}

// Two patches that are each valid against the stored row and invalid
// together. The row lock makes the second wait and see the first's result,
// so exactly one is applied.
func TestJobPostStoreUpdateConcurrently(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)

	const rounds = 20
	for round := 0; round < rounds; round++ {
		post := f.create(t, "Raced", ptr(domain.Money(5000000)), ptr(domain.Money(10000000)))

		patches := []domain.JobPostPatch{
			{SalaryMin: ptr(domain.Money(8000000))}, // fine while the maximum is 100000
			{SalaryMax: ptr(domain.Money(7000000))}, // fine while the minimum is 50000
		}

		var (
			wg   sync.WaitGroup
			errs = make([]error, len(patches))
		)
		for i, patch := range patches {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = f.store.UpdateJobPost(ctx, post.ID, patch)
			}()
		}
		wg.Wait()

		succeeded := 0
		for _, err := range errs {
			var invalid *domain.ValidationError
			switch {
			case err == nil:
				succeeded++
			case !errors.As(err, &invalid):
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("round %d: %d updates succeeded, want exactly 1", round, succeeded)
		}

		stored, err := f.store.GetJobPostByID(ctx, post.ID, true)
		if err != nil {
			t.Fatalf("GetJobPostByID: %v", err)
		}
		if *stored.SalaryMin > *stored.SalaryMax {
			t.Fatalf("round %d: stored minimum %d is above maximum %d", round, *stored.SalaryMin, *stored.SalaryMax)
		}
	}
}

func TestJobPostStoreList(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)

	oldest := f.create(t, "Oldest", nil, nil)
	draft := f.create(t, "Draft", nil, nil)
	newest := f.create(t, "Newest", nil, nil)
	f.publish(t, oldest.ID)
	f.publish(t, newest.ID)

	// The database is shared with other tests and the seed, so the result is
	// narrowed to this test's own posts before it is compared.
	mine := func(posts []domain.JobPost) []string {
		var ids []string
		for _, p := range posts {
			if p.CreatedBy != nil && *p.CreatedBy == f.admin.ID {
				ids = append(ids, p.ID)
			}
		}
		return ids
	}

	all, err := f.store.ListJobPosts(ctx, true)
	if err != nil {
		t.Fatalf("ListJobPosts(true): %v", err)
	}
	if got, want := mine(all), []string{newest.ID, draft.ID, oldest.ID}; !slices.Equal(got, want) {
		t.Errorf("with drafts: %v, want newest first %v", got, want)
	}

	published, err := f.store.ListJobPosts(ctx, false)
	if err != nil {
		t.Fatalf("ListJobPosts(false): %v", err)
	}
	if got, want := mine(published), []string{newest.ID, oldest.ID}; !slices.Equal(got, want) {
		t.Errorf("without drafts: %v, want %v", got, want)
	}
	for _, p := range published {
		if p.Status == domain.JobStatusDraft {
			t.Errorf("a draft was listed without includeDrafts: %s", p.ID)
		}
	}
}
