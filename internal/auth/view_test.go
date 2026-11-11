package auth

import (
	"slices"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

func TestCanViewTimeline(t *testing.T) {
	t.Parallel()

	app := domain.Application{CandidateID: userID}
	for _, role := range testRoles {
		for _, assigned := range testAssigned {
			u := domain.User{ID: userID, Role: role}
			want := role == domain.RoleAdmin || (role == domain.RoleInterviewer && assigned)
			if got := CanViewTimeline(u, app, assigned); got != want {
				t.Errorf("role %q, assigned %v: got %v, want %v", role, assigned, got, want)
			}
			// Never wider than viewing the application itself.
			if CanViewTimeline(u, app, assigned) && !CanViewApplication(u, app, assigned) {
				t.Errorf("role %q, assigned %v: may see the timeline but not the application", role, assigned)
			}
		}
	}
}

func TestApplicationViewFor(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		role       string
		reviews    domain.ReviewScope
		viewer     string
		eventTypes []string // nil means every type
	}{
		{domain.RoleAdmin, domain.ReviewsAll, userID, nil},
		{domain.RoleInterviewer, domain.ReviewsOwn, userID, nil},
		{domain.RoleCandidate, domain.ReviewsNone, userID,
			[]string{domain.EventApplicationSubmitted, domain.EventApplicationStatusChanged}},
		// Anything else sees nothing: no reviews, and an empty, non-nil type
		// list, which matches no event, where nil would match them all.
		{"", domain.ReviewsNone, "", []string{}},
		{"Admin", domain.ReviewsNone, "", []string{}},
		{"superuser", domain.ReviewsNone, "", []string{}},
	} {
		t.Run(tt.role, func(t *testing.T) {
			t.Parallel()

			got := ApplicationViewFor(domain.User{ID: userID, Role: tt.role})
			if got.Reviews != tt.reviews || got.ViewerID != tt.viewer {
				t.Errorf("got %+v", got)
			}
			if (got.EventTypes == nil) != (tt.eventTypes == nil) || !slices.Equal(got.EventTypes, tt.eventTypes) {
				t.Errorf("event types = %#v, want %#v", got.EventTypes, tt.eventTypes)
			}
			// No view below admin may include the review event types
			// unfiltered: that is the whole rule.
			if tt.role != domain.RoleAdmin && got.Reviews == domain.ReviewsAll {
				t.Errorf("role %q sees every review", tt.role)
			}
		})
	}
}
