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
	mine, theirs                                         string
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
			apps, err := f.store.ListApplications(ctx, tt.viewer)
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
		apps, err := f.store.ListApplications(ctx, f.admin)
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

		apps, err := f.store.ListApplications(context.Background(), viewer)
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
