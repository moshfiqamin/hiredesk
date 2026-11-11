package auth

import (
	"fmt"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// ownership describes how the user relates to the application's candidate.
type ownership string

const (
	// owner: the user is the candidate who submitted the application.
	owner ownership = "owner"
	// other: the application belongs to someone else.
	other ownership = "other"
	// blank: both IDs are empty, as in zero-value structs. Must never count
	// as owning.
	blank ownership = "blank"
)

// The role values tested. The three real roles, plus three that must be
// treated as "anything else": empty, the right word in the wrong case, and a
// role that does not exist.
var testRoles = []string{
	domain.RoleAdmin, domain.RoleInterviewer, domain.RoleCandidate,
	"", "Admin", "superuser",
}

var (
	testOwnerships = []ownership{owner, other, blank}
	testAssigned   = []bool{true, false}
)

const (
	userID      = "11111111-1111-4111-8111-111111111111"
	someoneElse = "22222222-2222-4222-8222-222222222222"
)

// inputs builds the arguments for one combination.
func inputs(role string, own ownership) (domain.User, domain.Application) {
	u := domain.User{ID: userID, Role: role}
	app := domain.Application{ID: "app-1", JobPostID: "post-1", Status: domain.StatusSubmitted}

	switch own {
	case owner:
		app.CandidateID = userID
	case other:
		app.CandidateID = someoneElse
	case blank:
		u.ID = ""
		app.CandidateID = ""
	}
	return u, app
}

// policyCase is one combination and the three answers expected for it.
type policyCase struct {
	role     string
	own      ownership
	assigned bool

	view, review, change bool
}

func (c policyCase) String() string {
	return fmt.Sprintf("role=%q/%s/assigned=%v", c.role, c.own, c.assigned)
}

// policyTable lists every combination of testRoles × testOwnerships ×
// testAssigned, with the answers written out by hand rather than computed.
// TestPolicyTableIsComplete fails if a combination is missing or repeated.
var policyTable = []policyCase{
	// Admin: views everything, changes everything, reviews nothing.
	{domain.RoleAdmin, owner, true, true, false, true},
	{domain.RoleAdmin, owner, false, true, false, true},
	{domain.RoleAdmin, other, true, true, false, true},
	{domain.RoleAdmin, other, false, true, false, true},
	{domain.RoleAdmin, blank, true, true, false, true},
	{domain.RoleAdmin, blank, false, true, false, true},

	// Interviewer: views and reviews only when assigned, never reviews their
	// own application, never changes status.
	{domain.RoleInterviewer, owner, true, true, false, false},
	{domain.RoleInterviewer, owner, false, false, false, false},
	{domain.RoleInterviewer, other, true, true, true, false},
	{domain.RoleInterviewer, other, false, false, false, false},
	{domain.RoleInterviewer, blank, true, true, true, false},
	{domain.RoleInterviewer, blank, false, false, false, false},

	// Candidate: views only their own, whatever the assignment says; never
	// reviews, never changes status.
	{domain.RoleCandidate, owner, true, true, false, false},
	{domain.RoleCandidate, owner, false, true, false, false},
	{domain.RoleCandidate, other, true, false, false, false},
	{domain.RoleCandidate, other, false, false, false, false},
	{domain.RoleCandidate, blank, true, false, false, false},
	{domain.RoleCandidate, blank, false, false, false, false},

	// Empty role: nothing.
	{"", owner, true, false, false, false},
	{"", owner, false, false, false, false},
	{"", other, true, false, false, false},
	{"", other, false, false, false, false},
	{"", blank, true, false, false, false},
	{"", blank, false, false, false, false},

	// "Admin" with a capital A is not admin: roles compare exactly.
	{"Admin", owner, true, false, false, false},
	{"Admin", owner, false, false, false, false},
	{"Admin", other, true, false, false, false},
	{"Admin", other, false, false, false, false},
	{"Admin", blank, true, false, false, false},
	{"Admin", blank, false, false, false, false},

	// A role that does not exist: nothing.
	{"superuser", owner, true, false, false, false},
	{"superuser", owner, false, false, false, false},
	{"superuser", other, true, false, false, false},
	{"superuser", other, false, false, false, false},
	{"superuser", blank, true, false, false, false},
	{"superuser", blank, false, false, false, false},
}

func TestPolicy(t *testing.T) {
	t.Parallel()

	checks := []struct {
		name string
		fn   func(domain.User, domain.Application, bool) bool
		want func(policyCase) bool
	}{
		{"CanViewApplication", CanViewApplication, func(c policyCase) bool { return c.view }},
		{"CanReviewApplication", CanReviewApplication, func(c policyCase) bool { return c.review }},
		{"CanChangeApplicationStatus", CanChangeApplicationStatus, func(c policyCase) bool { return c.change }},
	}

	for _, tc := range policyTable {
		t.Run(tc.String(), func(t *testing.T) {
			t.Parallel()

			u, app := inputs(tc.role, tc.own)
			for _, check := range checks {
				if got, want := check.fn(u, app, tc.assigned), check.want(tc); got != want {
					t.Errorf("%s = %v, want %v", check.name, got, want)
				}
			}
		})
	}
}

// Guards the table itself: every combination present exactly once.
func TestPolicyTableIsComplete(t *testing.T) {
	t.Parallel()

	seen := make(map[string]int, len(policyTable))
	for _, tc := range policyTable {
		seen[tc.String()]++
	}

	want := 0
	for _, role := range testRoles {
		for _, own := range testOwnerships {
			for _, assigned := range testAssigned {
				want++
				key := policyCase{role: role, own: own, assigned: assigned}.String()
				switch n := seen[key]; n {
				case 1:
				case 0:
					t.Errorf("missing from policyTable: %s", key)
				default:
					t.Errorf("listed %d times in policyTable: %s", n, key)
				}
			}
		}
	}

	if len(policyTable) != want {
		t.Errorf("policyTable has %d rows, want %d (%d roles × %d ownerships × %d assignments)",
			len(policyTable), want, len(testRoles), len(testOwnerships), len(testAssigned))
	}
}

// Rules that must hold across all three functions, whatever the table says.
func TestPolicyInvariants(t *testing.T) {
	t.Parallel()

	statuses := []string{
		domain.StatusSubmitted, domain.StatusShortlisted, domain.StatusInterview,
		domain.StatusOffer, domain.StatusRejected,
	}

	for _, role := range testRoles {
		for _, own := range testOwnerships {
			for _, assigned := range testAssigned {
				u, app := inputs(role, own)
				name := policyCase{role: role, own: own, assigned: assigned}.String()

				view := CanViewApplication(u, app, assigned)
				review := CanReviewApplication(u, app, assigned)
				change := CanChangeApplicationStatus(u, app, assigned)

				// Nobody may act on an application they are not allowed to see.
				if review && !view {
					t.Errorf("%s: may review but not view", name)
				}
				if change && !view {
					t.Errorf("%s: may change status but not view", name)
				}

				// Nobody both marks an application and decides its outcome.
				if review && change {
					t.Errorf("%s: may both review and change status", name)
				}

				// The rules do not mention status, so it must not change any
				// answer. If a rule ever depends on it, this fails and the
				// table needs a status dimension.
				for _, status := range statuses {
					app.Status = status
					if CanViewApplication(u, app, assigned) != view ||
						CanReviewApplication(u, app, assigned) != review ||
						CanChangeApplicationStatus(u, app, assigned) != change {
						t.Errorf("%s: an answer changed with status %q", name, status)
					}
				}
			}
		}
	}
}

// The zero values a careless caller might pass: an unauthenticated request's
// empty user, and an application that failed to load.
func TestPolicyZeroValuesGrantNothing(t *testing.T) {
	t.Parallel()

	var (
		u   domain.User
		app domain.Application
	)
	for _, assigned := range testAssigned {
		if CanViewApplication(u, app, assigned) ||
			CanReviewApplication(u, app, assigned) ||
			CanChangeApplicationStatus(u, app, assigned) {
			t.Errorf("zero user, zero application, assigned=%v: granted something", assigned)
		}
	}
}
