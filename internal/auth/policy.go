package auth

import "github.com/moshfiqamin/hiredesk/internal/domain"

// The policy functions below decide what a user may do to one application.
//
// Each is pure: the answer depends only on its arguments, it reads no clock,
// database or request, and it changes nothing. The caller gathers the facts
// (the user, the application, whether an assignment row exists) and these
// functions only judge them. Every one denies by default: a role not named
// here, including an empty or misspelt one, gets false.
//
// assigned must say whether an interviewer_assignments row links u to app. It
// is a parameter rather than a lookup so that this file never touches the
// database.

// CanViewApplication reports whether u may read app.
//
//   - An admin may view any application.
//   - A candidate may view only their own.
//   - An interviewer may view only one they are assigned to.
func CanViewApplication(u domain.User, app domain.Application, assigned bool) bool {
	switch u.Role {
	case domain.RoleAdmin:
		return true
	case domain.RoleCandidate:
		return owns(u, app)
	case domain.RoleInterviewer:
		return assigned
	default:
		return false
	}
}

// CanReviewApplication reports whether u may file or edit a review of app.
//
//   - Only an interviewer assigned to app may review it.
//   - Never their own application, even if assigned to it.
//   - Admins do not review: they decide the outcome, and the marks they read
//     to decide it come from someone else.
//   - Candidates never review, including their own application.
func CanReviewApplication(u domain.User, app domain.Application, assigned bool) bool {
	switch u.Role {
	case domain.RoleInterviewer:
		return assigned && !owns(u, app)
	default:
		return false
	}
}

// CanChangeApplicationStatus reports whether u may move app to another stage
// of the pipeline, such as from shortlisted to offer.
//
//   - Only an admin may.
//   - An interviewer recommends through a review; the decision is not theirs,
//     assigned or not.
//   - A candidate may not move their own application, which would let them
//     make themselves an offer.
func CanChangeApplicationStatus(u domain.User, _ domain.Application, _ bool) bool {
	// The application and assignment are ignored today. They stay in the
	// signature so that all three functions are called the same way, and so
	// a future rule, such as "not once an offer is accepted", needs no change
	// at any call site.
	return u.Role == domain.RoleAdmin
}

// owns reports whether u is the candidate who submitted app.
func owns(u domain.User, app domain.Application) bool {
	// The empty check matters: a zero-value User and a zero-value Application
	// both have an ID of "", and "" == "" would otherwise grant access.
	return u.ID != "" && app.CandidateID == u.ID
}
