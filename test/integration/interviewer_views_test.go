//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// TestOpenJobListingHidesEverythingButOpenPosts pins the public post page:
// an open post comes back with its form; a draft, a closed post and a
// missing one are all ErrNotFound, indistinguishably.
func TestOpenJobListingHidesEverythingButOpenPosts(t *testing.T) {
	pool := startPostgres(t)
	seed(t, pool)
	ctx := context.Background()
	posts := store.NewJobPostStore(pool)

	d, err := posts.GetOpenJobListing(ctx, postID)
	if err != nil {
		t.Fatalf("open post: %v", err)
	}
	if d.ID != postID || len(d.Questions) != 1 || d.Questions[0].Prompt != "Years of Go?" {
		t.Errorf("open post = %+v", d)
	}

	for name, id := range map[string]string{"draft": draftPostID, "closed": closedPostID, "missing": missingPost} {
		if _, err := posts.GetOpenJobListing(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
	}
}

// TestInterviewerDirectoryAndOwnReviewFlag pins the two reads the
// interviewer screens rely on: the directory lists interviewers only, with
// their load; and an interviewer's list says whether they themselves have
// reviewed, turning true once they file.
func TestInterviewerDirectoryAndOwnReviewFlag(t *testing.T) {
	pool := startPostgres(t)
	seed(t, pool)
	ctx := context.Background()
	reviews := store.NewReviewStore(pool)
	apps := store.NewApplicationStore(pool)

	loads := func() map[string]domain.InterviewerLoad {
		t.Helper()
		list, err := reviews.ListInterviewers(ctx)
		if err != nil {
			t.Fatalf("ListInterviewers: %v", err)
		}
		out := map[string]domain.InterviewerLoad{}
		for _, l := range list {
			out[l.ID] = l
		}
		return out
	}

	before := loads()
	if len(before) != 2 {
		t.Fatalf("directory has %d rows, want the 2 interviewers only: %v", len(before), before)
	}
	for _, notStaff := range []string{adminID, candidateID, otherCandidateID} {
		if _, ok := before[notStaff]; ok {
			t.Errorf("directory lists non-interviewer %s", notStaff)
		}
	}
	if l := before[interviewerID]; l.OpenAssignments != 1 || l.PendingReviews != 1 {
		t.Errorf("assigned interviewer load = %+v, want 1 open, 1 pending", l)
	}
	if l := before[outsideInterviewerID]; l.OpenAssignments != 0 || l.PendingReviews != 0 {
		t.Errorf("unassigned interviewer load = %+v, want zeros", l)
	}

	interviewer := domain.User{ID: interviewerID, Role: domain.RoleInterviewer}
	reviewedFlag := func() bool {
		t.Helper()
		list, _, _, err := apps.ListApplications(ctx, interviewer, domain.ApplicationFilter{Limit: 10})
		if err != nil || len(list) != 1 || list[0].ReviewedByMe == nil {
			t.Fatalf("interviewer list = %+v, %v; want one row with the flag set", list, err)
		}
		return *list[0].ReviewedByMe
	}
	if reviewedFlag() {
		t.Error("reviewed_by_me true before any review")
	}

	if _, err := reviews.CreateReview(ctx, appA, interviewerID, 8, nil); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if !reviewedFlag() {
		t.Error("reviewed_by_me false after the review")
	}
	if l := loads()[interviewerID]; l.OpenAssignments != 1 || l.PendingReviews != 0 {
		t.Errorf("after review load = %+v, want 1 open, 0 pending", l)
	}

	// An admin's list never carries the flag.
	admin := domain.User{ID: adminID, Role: domain.RoleAdmin}
	list, _, _, err := apps.ListApplications(ctx, admin, domain.ApplicationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("admin list: %v", err)
	}
	for _, a := range list {
		if a.ReviewedByMe != nil {
			t.Errorf("admin row %s has ReviewedByMe", a.ID)
		}
	}
}
