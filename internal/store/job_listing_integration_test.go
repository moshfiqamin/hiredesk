//go:build integration

package store

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// The listing is global, and these tests share a database with every other
// test and with the seed. Each test therefore places its posts in its own
// range of created_at far in the future, and starts reading from a cursor
// just above that range. Keyset pagination makes that possible: a position
// can be any sort key, not only one a previous page returned.

// listingRange is a run of posts for one test, newest first.
type listingRange struct {
	f   *postFixture
	top time.Time
}

func newListingRange(t *testing.T, f *postFixture, year int) *listingRange {
	t.Helper()
	return &listingRange{f: f, top: time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)}
}

// insert adds a post with the given status, minutesBelow the top of the
// range, and returns its ID.
func (r *listingRange) insert(t *testing.T, title, status string, minutesBelow, vacancies int) string {
	t.Helper()

	var id string
	if err := r.f.store.db.QueryRow(context.Background(), `
		INSERT INTO job_posts (title, status, vacancies, created_at, created_by)
		VALUES ($1, $2, $3, $4, $5::text::uuid)
		RETURNING id::text`,
		title, status, vacancies, r.top.Add(-time.Duration(minutesBelow)*time.Minute), r.f.admin.ID,
	).Scan(&id); err != nil {
		t.Fatalf("insert %s: %v", title, err)
	}
	return id
}

// start is a cursor just above the range, so a listing read from it begins
// with this range's newest post.
func (r *listingRange) start() *domain.ListingPosition {
	return &domain.ListingPosition{CreatedAt: r.top.Add(time.Hour), ID: "ffffffff-ffff-4fff-bfff-ffffffffffff"}
}

// page reads one page after position and returns the titles and the
// position of its last row.
func (r *listingRange) page(t *testing.T, after *domain.ListingPosition, limit int) ([]string, *domain.ListingPosition, bool) {
	t.Helper()

	listings, more, err := r.f.store.ListOpenJobPosts(context.Background(), after, limit)
	if err != nil {
		t.Fatalf("ListOpenJobPosts: %v", err)
	}
	titles := make([]string, len(listings))
	for i, l := range listings {
		titles[i] = l.Title
	}
	if len(listings) == 0 {
		return titles, nil, more
	}
	last := listings[len(listings)-1]
	return titles, &domain.ListingPosition{CreatedAt: last.CreatedAt, ID: last.ID}, more
}

func TestListOpenJobPostsPages(t *testing.T) {
	f := newPostFixture(t)
	r := newListingRange(t, f, 2101)
	for i := 0; i < 7; i++ {
		r.insert(t, fmt.Sprintf("P%d", i), domain.JobStatusOpen, i, 1)
	}

	one, cursor, more := r.page(t, r.start(), 3)
	two, cursor2, more2 := r.page(t, cursor, 3)
	three, _, _ := r.page(t, cursor2, 3)

	if !slices.Equal(one, []string{"P0", "P1", "P2"}) || !more {
		t.Errorf("page 1 = %v, more = %v", one, more)
	}
	if !slices.Equal(two, []string{"P3", "P4", "P5"}) || !more2 {
		t.Errorf("page 2 = %v, more = %v", two, more2)
	}
	// Other tests' posts may follow P6, since the listing is global.
	if len(three) == 0 || three[0] != "P6" {
		t.Errorf("page 3 = %v, want it to start with P6", three)
	}
}

func TestListOpenJobPostsFirstPageHasNoCursorArgument(t *testing.T) {
	f := newPostFixture(t)

	// nil after is the first page: the newest open posts in the database.
	listings, _, err := f.store.ListOpenJobPosts(context.Background(), nil, 5)
	if err != nil {
		t.Fatalf("ListOpenJobPosts(nil): %v", err)
	}
	for i := 1; i < len(listings); i++ {
		if listings[i].CreatedAt.After(listings[i-1].CreatedAt) {
			t.Errorf("row %d is newer than row %d", i, i-1)
		}
	}
}

// Five posts created in the same microsecond, read two at a time. Without id
// in the sort key and the cursor, the boundary between pages would fall
// inside the tie, and rows would be lost or repeated.
func TestListOpenJobPostsTies(t *testing.T) {
	f := newPostFixture(t)
	r := newListingRange(t, f, 2102)

	var want []string
	for i := 0; i < 5; i++ {
		want = append(want, r.insert(t, fmt.Sprintf("Tie%d", i), domain.JobStatusOpen, 0, 1))
	}
	// id DESC, as the listing orders a tie.
	slices.Sort(want)
	slices.Reverse(want)

	var got []string
	after := r.start()
	for len(got) < 5 {
		listings, _, err := f.store.ListOpenJobPosts(context.Background(), after, 2)
		if err != nil {
			t.Fatalf("ListOpenJobPosts: %v", err)
		}
		if len(listings) == 0 {
			t.Fatalf("ran out after %v", got)
		}
		for _, l := range listings {
			got = append(got, l.ID)
		}
		last := listings[len(listings)-1]
		after = &domain.ListingPosition{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	if !slices.Equal(got[:5], want) {
		t.Errorf("got %v, want %v", got[:5], want)
	}
}

func TestListOpenJobPostsOnlyOpen(t *testing.T) {
	f := newPostFixture(t)
	r := newListingRange(t, f, 2103)
	r.insert(t, "Draft", domain.JobStatusDraft, 0, 1)
	r.insert(t, "Open", domain.JobStatusOpen, 1, 1)
	r.insert(t, "Closed", domain.JobStatusClosed, 2, 1)

	titles, _, _ := r.page(t, r.start(), 3)
	if len(titles) == 0 || titles[0] != "Open" || slices.Contains(titles, "Draft") || slices.Contains(titles, "Closed") {
		t.Errorf("titles = %v, want Open first and no draft or closed post", titles)
	}
}

func TestListOpenJobPostsRemainingVacancies(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)
	r := newListingRange(t, f, 2104)
	users := NewUserStore(f.store.db)

	apply := func(postID, status string) {
		t.Helper()
		in := newCandidate(t, users)
		u, err := users.CreateUser(ctx, in)
		if err != nil {
			t.Fatalf("create candidate: %v", err)
		}
		if _, err := f.store.db.Exec(ctx, `
			INSERT INTO applications (job_post_id, candidate_id, cv_url, status)
			VALUES ($1::text::uuid, $2::text::uuid, 'https://cv.test/x.pdf', $3)`,
			postID, u.ID, status,
		); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}

	three := r.insert(t, "Three vacancies", domain.JobStatusOpen, 0, 3)
	for _, status := range []string{"offer", "offer", "interview", "rejected", "submitted", "shortlisted"} {
		apply(three, status)
	}
	oversubscribed := r.insert(t, "One vacancy, two offers", domain.JobStatusOpen, 1, 1)
	apply(oversubscribed, "offer")
	apply(oversubscribed, "offer")
	untouched := r.insert(t, "No applications", domain.JobStatusOpen, 2, 4)

	listings, _, err := f.store.ListOpenJobPosts(ctx, r.start(), 3)
	if err != nil {
		t.Fatalf("ListOpenJobPosts: %v", err)
	}
	got := map[string]int{}
	for _, l := range listings {
		got[l.ID] = l.RemainingVacancies
	}

	// Only offers count; every other stage leaves the vacancy open.
	if got[three] != 1 {
		t.Errorf("three vacancies, two offers: remaining = %d, want 1", got[three])
	}
	// Never below zero.
	if got[oversubscribed] != 0 {
		t.Errorf("one vacancy, two offers: remaining = %d, want 0", got[oversubscribed])
	}
	if got[untouched] != 4 {
		t.Errorf("no applications: remaining = %d, want 4", got[untouched])
	}
}

func TestListOpenJobPostsSkillTags(t *testing.T) {
	ctx := context.Background()
	f := newPostFixture(t)
	r := newListingRange(t, f, 2105)

	criterion := func(postID, kind string, version int, config string) {
		t.Helper()
		if _, err := f.store.db.Exec(ctx, `
			INSERT INTO rubric_criteria (job_post_id, label, kind, weight, config, rubric_version)
			VALUES ($1::text::uuid, 'c', $2, 1, $3::jsonb, $4)`, postID, kind, config, version,
		); err != nil {
			t.Fatalf("add criterion: %v", err)
		}
	}

	merged := r.insert(t, "Merged", domain.JobStatusOpen, 0, 1)
	criterion(merged, "keyword_match", 1, `{"keywords":["Go"," postgres ","go"]}`)
	criterion(merged, "keyword_match", 1, `{"keywords":["Kubernetes","  "]}`)
	criterion(merged, "experience_years", 1, `{"keywords":["not a screening keyword"],"min_years":3}`)

	versioned := r.insert(t, "Versioned", domain.JobStatusOpen, 1, 1)
	criterion(versioned, "keyword_match", 1, `{"keywords":["old"]}`)
	criterion(versioned, "keyword_match", 2, `{"keywords":["new"]}`)

	malformed := r.insert(t, "Malformed", domain.JobStatusOpen, 2, 1)
	criterion(malformed, "keyword_match", 1, `{"keywords":"go"}`)
	criterion(malformed, "keyword_match", 1, `{}`)
	criterion(malformed, "keyword_match", 1, `{"keywords":[1, true, null]}`)

	bare := r.insert(t, "No rubric", domain.JobStatusOpen, 3, 1)

	listings, _, err := f.store.ListOpenJobPosts(ctx, r.start(), 4)
	if err != nil {
		t.Fatalf("ListOpenJobPosts: %v", err)
	}
	got := map[string][]string{}
	for _, l := range listings {
		got[l.ID] = l.SkillTags
	}

	want := map[string][]string{
		merged:    {"go", "kubernetes", "postgres"}, // lower-cased, trimmed, deduplicated, sorted
		versioned: {"new"},                          // only the current rubric version
		malformed: {"1", "true"},                    // non-arrays ignored; scalars read as text, null dropped
		bare:      {},
	}
	for id, tags := range want {
		if got[id] == nil || !slices.Equal(got[id], tags) {
			t.Errorf("%s: tags = %#v, want %#v", id, got[id], tags)
		}
	}
}

// The second OFFSET bug, against real data. A client reads page 1, then the
// data changes, then it reads page 2. Keyset continues from where page 1
// ended. OFFSET counts rows from the top again, and the count is now wrong.
func TestListOpenJobPostsIsStableWhileDataChanges(t *testing.T) {
	ctx := context.Background()

	// offsetPage is the query a naive implementation would run, confined to
	// this test's range so other tests' rows do not disturb the count.
	offsetPage := func(t *testing.T, r *listingRange, offset, limit int) []string {
		t.Helper()
		rows, err := r.f.store.db.Query(ctx, `
			SELECT title FROM job_posts
			WHERE status = 'open' AND created_at BETWEEN $1 AND $2
			ORDER BY created_at DESC, id DESC
			OFFSET $3 LIMIT $4`,
			r.top.Add(-24*time.Hour), r.top.Add(time.Hour), offset, limit)
		if err != nil {
			t.Fatalf("offset query: %v", err)
		}
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var title string
			if err := rows.Scan(&title); err != nil {
				t.Fatalf("scan: %v", err)
			}
			titles = append(titles, title)
		}
		return titles
	}

	t.Run("a new post arrives between pages", func(t *testing.T) {
		f := newPostFixture(t)
		r := newListingRange(t, f, 2106)
		for i := 1; i <= 6; i++ {
			r.insert(t, fmt.Sprintf("P%d", i), domain.JobStatusOpen, i, 1)
		}

		first, cursor, _ := r.page(t, r.start(), 3)
		offsetFirst := offsetPage(t, r, 0, 3)
		if !slices.Equal(first, []string{"P1", "P2", "P3"}) || !slices.Equal(offsetFirst, first) {
			t.Fatalf("page 1: keyset %v, offset %v", first, offsetFirst)
		}

		// Newer than everything: it goes to the top and pushes every row down.
		r.insert(t, "NEW", domain.JobStatusOpen, 0, 1)

		second, _, _ := r.page(t, cursor, 3)
		if !slices.Equal(second, []string{"P4", "P5", "P6"}) {
			t.Errorf("keyset page 2 = %v, want P4 P5 P6", second)
		}
		// OFFSET 3 now lands on P3, already shown: a repeat.
		if got := offsetPage(t, r, 3, 3); !slices.Equal(got, []string{"P3", "P4", "P5"}) {
			t.Errorf("offset page 2 = %v; expected the repeat P3 P4 P5", got)
		}
	})

	t.Run("a post closes between pages", func(t *testing.T) {
		f := newPostFixture(t)
		r := newListingRange(t, f, 2107)
		ids := map[string]string{}
		for i := 1; i <= 6; i++ {
			title := fmt.Sprintf("P%d", i)
			ids[title] = r.insert(t, title, domain.JobStatusOpen, i, 1)
		}

		first, cursor, _ := r.page(t, r.start(), 3)
		if !slices.Equal(first, []string{"P1", "P2", "P3"}) {
			t.Fatalf("page 1 = %v", first)
		}

		// P2, already shown, leaves the listing: every later row moves up.
		if _, err := f.store.db.Exec(ctx, `UPDATE job_posts SET status = 'closed' WHERE id = $1::text::uuid`, ids["P2"]); err != nil {
			t.Fatalf("close: %v", err)
		}

		second, _, _ := r.page(t, cursor, 3)
		if !slices.Equal(second, []string{"P4", "P5", "P6"}) {
			t.Errorf("keyset page 2 = %v, want P4 P5 P6", second)
		}
		// OFFSET 3 now starts at P5. P4 is never shown to this client: a skip.
		if got := offsetPage(t, r, 3, 3); !slices.Equal(got, []string{"P5", "P6"}) {
			t.Errorf("offset page 2 = %v; expected the skip P5 P6", got)
		}
	})

	t.Run("the cursor's own post closes", func(t *testing.T) {
		f := newPostFixture(t)
		r := newListingRange(t, f, 2108)
		ids := map[string]string{}
		for i := 1; i <= 4; i++ {
			title := fmt.Sprintf("P%d", i)
			ids[title] = r.insert(t, title, domain.JobStatusOpen, i, 1)
		}

		_, cursor, _ := r.page(t, r.start(), 2) // ends on P2
		if _, err := f.store.db.Exec(ctx, `UPDATE job_posts SET status = 'closed' WHERE id = $1::text::uuid`, ids["P2"]); err != nil {
			t.Fatalf("close: %v", err)
		}

		// The cursor is a sort key, not a row: it still marks the same point
		// in the order after the row it came from has gone.
		second, _, _ := r.page(t, cursor, 2)
		if !slices.Equal(second, []string{"P3", "P4"}) {
			t.Errorf("page 2 = %v, want P3 P4", second)
		}
	})
}
