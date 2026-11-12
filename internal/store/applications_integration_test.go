//go:build integration

package store

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// appFixture is two candidates, two interviewers, one post and two
// applications, created in the shared database and removed when the test
// ends. Interviewer "assigned" is assigned to the first candidate's
// application only.
type appFixture struct {
	store *ApplicationStore

	admin, candidate, otherCandidate, assigned, outsider domain.User
	mine, theirs, postID                                 string
}

func newAppFixture(t *testing.T) *appFixture {
	t.Helper()
	ctx := context.Background()

	users := newTestStore(t)
	f := &appFixture{store: NewApplicationStore(users.db)}

	// A suffix unique to this run, so parallel runs and leftovers cannot
	// collide with these rows.
	suffix := strings.ToLower(rand.Text())

	create := func(name, role string) domain.User {
		u, err := users.CreateUser(ctx, domain.NewUser{
			Email:        name + "-" + suffix + "@example.test",
			PasswordHash: "unused",
			FullName:     name,
			Role:         role,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return *u
	}
	f.admin = create("admin", domain.RoleAdmin)
	f.candidate = create("candidate", domain.RoleCandidate)
	f.otherCandidate = create("other", domain.RoleCandidate)
	f.assigned = create("assigned", domain.RoleInterviewer)
	f.outsider = create("outsider", domain.RoleInterviewer)

	var postID string
	if err := users.db.QueryRow(ctx,
		`INSERT INTO job_posts (title, status) VALUES ('Store test', 'open') RETURNING id::text`,
	).Scan(&postID); err != nil {
		t.Fatalf("create post: %v", err)
	}
	f.postID = postID

	insertApp := func(candidateID string) string {
		var id string
		if err := users.db.QueryRow(ctx,
			`INSERT INTO applications (job_post_id, candidate_id, cv_url)
			 VALUES ($1::text::uuid, $2::text::uuid, 'https://cv.test/x.pdf') RETURNING id::text`,
			postID, candidateID,
		).Scan(&id); err != nil {
			t.Fatalf("create application: %v", err)
		}
		return id
	}
	f.mine = insertApp(f.candidate.ID)
	f.theirs = insertApp(f.otherCandidate.ID)

	if _, err := users.db.Exec(ctx,
		`INSERT INTO interviewer_assignments (application_id, interviewer_id) VALUES ($1::text::uuid, $2::text::uuid)`,
		f.mine, f.assigned.ID,
	); err != nil {
		t.Fatalf("assign: %v", err)
	}

	t.Cleanup(func() {
		// The post cascades to its applications and their assignments; the
		// users go after them, since applications reference users.
		if _, err := users.db.Exec(ctx, `DELETE FROM job_posts WHERE id = $1::text::uuid`, postID); err != nil {
			t.Errorf("clean up post: %v", err)
		}
		if _, err := users.db.Exec(ctx, `DELETE FROM users WHERE email LIKE '%-' || $1 || '@example.test'`, suffix); err != nil {
			t.Errorf("clean up users: %v", err)
		}
	})

	return f
}

func ids(apps []domain.Application) []string {
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.ID)
	}
	return out
}

func TestListApplicationsByRole(t *testing.T) {
	f := newAppFixture(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		viewer domain.User
		want   []string
	}{
		{"candidate sees only their own", f.candidate, []string{f.mine}},
		{"other candidate sees only theirs", f.otherCandidate, []string{f.theirs}},
		{"assigned interviewer sees only the assigned one", f.assigned, []string{f.mine}},
		{"unassigned interviewer sees none", f.outsider, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps, _, _, err := f.store.ListApplications(ctx, tt.viewer, domain.ApplicationFilter{Limit: 100})
			if err != nil {
				t.Fatalf("ListApplications: %v", err)
			}
			if apps == nil {
				t.Fatal("nil slice, want an empty one")
			}
			if got := ids(apps); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	// The shared database may hold other applications, so the admin's list is
	// checked for containing both, not for being exactly these two.
	t.Run("admin sees every application", func(t *testing.T) {
		// Narrowed to the fixture's post: the shared database may hold more
		// applications than one page.
		apps, _, _, err := f.store.ListApplications(ctx, f.admin, domain.ApplicationFilter{JobPostID: f.postID, Limit: 100})
		if err != nil {
			t.Fatalf("ListApplications: %v", err)
		}
		got := ids(apps)
		if !slices.Contains(got, f.mine) || !slices.Contains(got, f.theirs) {
			t.Errorf("admin list %v is missing one of %s, %s", got, f.mine, f.theirs)
		}
	})
}

// The branch no HTTP request can reach, because users_role_check keeps such a
// role out of the database. It must still deny rather than fall through.
func TestListApplicationsRefusesUnknownRoles(t *testing.T) {
	f := newAppFixture(t)

	for _, role := range []string{"", "Admin", "superuser"} {
		viewer := f.admin
		viewer.Role = role

		apps, _, _, err := f.store.ListApplications(context.Background(), viewer, domain.ApplicationFilter{Limit: 100})
		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("role %q: err = %v, want ErrForbidden", role, err)
		}
		if apps != nil {
			t.Errorf("role %q: returned %d applications", role, len(apps))
		}
	}
}

func TestGetApplication(t *testing.T) {
	f := newAppFixture(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		id, viewerID string
		wantAssigned bool
	}{
		{"assigned interviewer", f.mine, f.assigned.ID, true},
		{"unassigned interviewer", f.mine, f.outsider.ID, false},
		{"assigned elsewhere", f.theirs, f.assigned.ID, false},
		{"the candidate is not an assignee", f.mine, f.candidate.ID, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, assigned, err := f.store.GetApplication(ctx, tt.id, tt.viewerID)
			if err != nil {
				t.Fatalf("GetApplication: %v", err)
			}
			if app.ID != tt.id || assigned != tt.wantAssigned {
				t.Errorf("got %s assigned=%v, want %s assigned=%v", app.ID, assigned, tt.id, tt.wantAssigned)
			}
		})
	}

	for _, id := range []string{"00000000-0000-4000-8000-0000000000ff", "not-a-uuid", ""} {
		if _, _, err := f.store.GetApplication(ctx, id, f.admin.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("GetApplication(%q): err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestListApplicationsByScore(t *testing.T) {
	ctx := context.Background()
	f := newAppFixture(t)
	db := f.store.db
	users := NewUserStore(db)
	suffix := strings.ToLower(rand.Text())

	// Two more applications, so the post has four: mine, theirs, extra1, extra2.
	addApp := func(name string, submittedOffset string) string {
		t.Helper()
		u, err := users.CreateUser(ctx, domain.NewUser{
			Email: name + "-" + suffix + "@example.test", PasswordHash: "unused", FullName: name, Role: domain.RoleCandidate,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		var id string
		if err = db.QueryRow(ctx, `
			INSERT INTO applications (job_post_id, candidate_id, cv_url, submitted_at)
			VALUES ($1::text::uuid, $2::text::uuid, 'https://cv.test/x.pdf', now() + $3::interval) RETURNING id::text`,
			f.postID, u.ID, submittedOffset,
		).Scan(&id); err != nil {
			t.Fatalf("create application: %v", err)
		}
		return id
	}
	tieOlder := addApp("tie-older", "1 minute")
	tieNewer := addApp("tie-newer", "2 minutes")

	criterion := func(version int) string {
		t.Helper()
		var id string
		if err := db.QueryRow(ctx, `
			INSERT INTO rubric_criteria (job_post_id, label, kind, weight, config, rubric_version, position)
			VALUES ($1::text::uuid, 'c', 'boolean_all', 100, '{}'::jsonb, $2, 1) RETURNING id::text`, f.postID, version,
		).Scan(&id); err != nil {
			t.Fatalf("add criterion: %v", err)
		}
		return id
	}
	v1, v2 := criterion(1), criterion(2)
	score := func(appID, criterionID string, version int, earned float64) {
		t.Helper()
		if _, err := db.Exec(ctx, `
			INSERT INTO scores (application_id, criterion_id, earned, available, detail, rubric_version)
			VALUES ($1::text::uuid, $2::text::uuid, $3, 100, 'test', $4)`, appID, criterionID, earned, version,
		); err != nil {
			t.Fatalf("add score: %v", err)
		}
	}
	// mine: 90 under the old version, 40 under the newest, so 40 counts.
	score(f.mine, v1, 1, 90)
	score(f.mine, v2, 2, 40)
	// Two at 80, told apart by submission time. theirs has no score.
	score(tieOlder, v2, 2, 80)
	score(tieNewer, v2, 2, 80)

	// Scores reference criteria with ON DELETE RESTRICT, so they go before
	// the fixture deletes the post. Cleanups run last-registered first.
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DELETE FROM scores WHERE application_id IN (SELECT id FROM applications WHERE job_post_id = $1::text::uuid)`, f.postID); err != nil {
			t.Errorf("clean up scores: %v", err)
		}
		if _, err := db.Exec(ctx, `DELETE FROM applications WHERE job_post_id = $1::text::uuid`, f.postID); err != nil {
			t.Errorf("clean up applications: %v", err)
		}
		if _, err := db.Exec(ctx, `DELETE FROM users WHERE email LIKE '%-' || $1 || '@example.test'`, suffix); err != nil {
			t.Errorf("clean up users: %v", err)
		}
	})

	filter := domain.ApplicationFilter{JobPostID: f.postID, Sort: domain.ApplicationSortScore, Limit: 2}

	first, total, more, err := f.store.ListApplications(ctx, f.admin, filter)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if total != 4 || !more {
		t.Fatalf("total = %d, more = %v; want 4, true", total, more)
	}
	if got, want := ids(first), []string{tieNewer, tieOlder}; !slices.Equal(got, want) {
		t.Fatalf("first page = %v, want the two 80s, newer first: %v", got, want)
	}

	// The cursor, as the handler builds it from the last row.
	last := first[len(first)-1]
	s := *last.Score.Percentage
	filter.After = &domain.ListingPosition{CreatedAt: last.SubmittedAt, ID: last.ID, Score: &s}
	second, _, more, err := f.store.ListApplications(ctx, f.admin, filter)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if got, want := ids(second), []string{f.mine, f.theirs}; !slices.Equal(got, want) || more {
		t.Fatalf("second page = %v (more %v), want 40 then the unscored one: %v", got, more, want)
	}
	if p := second[0].Score.Percentage; p == nil || *p != 40 {
		t.Errorf("mine scored %v, want 40: only the newest rubric version counts", p)
	}
	if second[1].Score == nil || second[1].Score.Percentage != nil {
		t.Errorf("unscored application: Score = %+v, want visible with no percentage", second[1].Score)
	}

	// What each role is given, row by row.
	byID := func(apps []domain.Application, id string) domain.Application {
		for _, a := range apps {
			if a.ID == id {
				return a
			}
		}
		t.Fatalf("%s not listed", id)
		return domain.Application{}
	}
	adminView := byID(second, f.mine)
	if adminView.CandidateName != "candidate" || adminView.JobPostTitle != "Store test" {
		t.Errorf("names = %q, %q", adminView.CandidateName, adminView.JobPostTitle)
	}
	if len(adminView.Assignees) != 1 || adminView.Assignees[0].ID != f.assigned.ID || adminView.Assignees[0].Name != "assigned" {
		t.Errorf("admin sees assignees %+v, want the assigned interviewer", adminView.Assignees)
	}

	interviewer, _, _, err := f.store.ListApplications(ctx, f.assigned, domain.ApplicationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("interviewer list: %v", err)
	}
	if mine := byID(interviewer, f.mine); mine.Score == nil || mine.Assignees != nil {
		t.Errorf("interviewer sees Score %+v, Assignees %+v; want the score and no assignees", mine.Score, mine.Assignees)
	}

	candidate, _, _, err := f.store.ListApplications(ctx, f.candidate, domain.ApplicationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("candidate list: %v", err)
	}
	if mine := byID(candidate, f.mine); mine.Score != nil || mine.Assignees != nil {
		t.Errorf("candidate sees Score %+v, Assignees %+v; want neither", mine.Score, mine.Assignees)
	}
}
