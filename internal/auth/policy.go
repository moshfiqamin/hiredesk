package auth

import "github.com/moshfiqamin/hiredesk/internal/domain"

// The policy functions below decide what a user may see and do.
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

// CanViewScore reports whether u may read the score breakdown of app: every
// criterion, the points it earned, and the evidence behind them.
//
//   - An admin may view any application's score.
//   - An interviewer may view the score of an application they are assigned
//     to, as part of reviewing it.
//   - A candidate may not, not even of their own application. The breakdown
//     shows the rubric at work: which keywords were looked for and how each
//     answer was weighed. See docs/notes/07-3-score-breakdown.md.
//
// It is narrower than CanViewApplication, never wider: anyone it allows may
// also view the application.
func CanViewScore(u domain.User, _ domain.Application, assigned bool) bool {
	switch u.Role {
	case domain.RoleAdmin:
		return true
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

// CanViewTimeline reports whether u may read the full event timeline of app.
//
//   - An admin may read any application's timeline.
//   - An interviewer may read the timeline of an application they are
//     assigned to.
//   - A candidate may not, not even of their own application: the timeline
//     records who reviews them and how they were rescored. The few events a
//     candidate may see come with the application itself.
//
// Like CanViewScore, it is narrower than CanViewApplication, never wider.
func CanViewTimeline(u domain.User, _ domain.Application, assigned bool) bool {
	switch u.Role {
	case domain.RoleAdmin:
		return true
	case domain.RoleInterviewer:
		return assigned
	default:
		return false
	}
}

// ApplicationViewFor returns what u may see of the reviews and events of an
// application they are already allowed to view. It is the one place this is
// decided; the store applies the result in SQL.
//
//   - An admin sees every review and every event.
//   - An interviewer sees only their own review, and of the review events
//     only those about it. Every other event is shown. An interviewer must
//     not learn another interviewer's mark before giving their own; see
//     docs/notes/09-2-reviews.md.
//   - A candidate sees no reviews, and of the events only their submission
//     and the status changes, which they can see in the status anyway. The
//     others reveal who reviews them, the marks, and score totals, which
//     CanViewScore already keeps from them.
//   - Any other role sees no reviews and no events.
func ApplicationViewFor(u domain.User) domain.ApplicationView {
	switch u.Role {
	case domain.RoleAdmin:
		return domain.ApplicationView{ViewerID: u.ID, Reviews: domain.ReviewsAll}
	case domain.RoleInterviewer:
		return domain.ApplicationView{ViewerID: u.ID, Reviews: domain.ReviewsOwn}
	case domain.RoleCandidate:
		return domain.ApplicationView{
			ViewerID:   u.ID,
			Reviews:    domain.ReviewsNone,
			EventTypes: []string{domain.EventApplicationSubmitted, domain.EventApplicationStatusChanged},
		}
	default:
		// An empty, non-nil list: no event type at all, rather than nil,
		// which would mean every type.
		return domain.ApplicationView{Reviews: domain.ReviewsNone, EventTypes: []string{}}
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

// CanViewDraftJobPosts reports whether u may see job posts that have not been
// published. Only an admin may: a draft is unfinished work, and to everyone
// else it does not exist yet.
//
// Open and closed posts need no rule here, since every signed-in user may
// read them.
func CanViewDraftJobPosts(u domain.User) bool {
	return u.Role == domain.RoleAdmin
}

// owns reports whether u is the candidate who submitted app.
func owns(u domain.User, app domain.Application) bool {
	// The empty check matters: a zero-value User and a zero-value Application
	// both have an ID of "", and "" == "" would otherwise grant access.
	return u.ID != "" && app.CandidateID == u.ID
}
