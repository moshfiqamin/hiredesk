package domain

import (
	"slices"
	"strconv"
	"time"
)

// EventApplicationStatusChanged is the application_events type recorded when
// an application moves from one status to another. Its payload is
// {"from": "<old status>", "to": "<new status>"}.
const EventApplicationStatusChanged = "application.status_changed"

// applicationTransitions is the application lifecycle: each status, and the
// statuses an application in it may move to next. It is the one place the
// lifecycle is written down, as jobPostTransitions is for job posts.
//
// The pipeline runs forward one step at a time. Rejection is possible from
// every stage, including after an offer, which covers an offer that is
// withdrawn or declined. rejected is final.
var applicationTransitions = map[string][]string{
	StatusSubmitted:   {StatusShortlisted, StatusRejected},
	StatusShortlisted: {StatusInterview, StatusRejected},
	StatusInterview:   {StatusOffer, StatusRejected},
	StatusOffer:       {StatusRejected},
	StatusRejected:    {},
}

// IsApplicationStatus reports whether status is one of the statuses above.
func IsApplicationStatus(status string) bool {
	_, ok := applicationTransitions[status]
	return ok
}

// CanTransitionApplication reports whether an application in status from may
// move to status to. Staying in the same status is not a transition, and is
// false. Any status not in the lifecycle, on either side, is false.
func CanTransitionApplication(from, to string) bool {
	return slices.Contains(applicationTransitions[from], to)
}

// AllowedApplicationTransitions returns the statuses an application in status
// from may move to. It returns an empty, non-nil slice for a final or unknown
// status.
func AllowedApplicationTransitions(from string) []string {
	// Appending to a new empty slice copies, so a caller cannot edit the
	// lifecycle through the result, and an empty result is [] rather than nil.
	return append([]string{}, applicationTransitions[from]...)
}

// CheckApplicationTransition returns a *TransitionError if an application in
// status from may not move to status to, and nil if it may.
func CheckApplicationTransition(from, to string) error {
	if CanTransitionApplication(from, to) {
		return nil
	}
	return &TransitionError{From: from, To: to, Allowed: AllowedApplicationTransitions(from)}
}

// CheckVacancyForOffer returns a *NoVacanciesError unless a job post with
// this many vacancies, and this many applications already at the offer
// stage, has room for one more offer.
func CheckVacancyForOffer(vacancies, offers int) error {
	if offers < vacancies {
		return nil
	}
	return &NoVacanciesError{Vacancies: vacancies, Offers: offers}
}

// NoVacanciesError is an ErrNoVacancies that says how full the post is.
type NoVacanciesError struct {
	Vacancies int
	Offers    int
}

// Error states both numbers.
func (e *NoVacanciesError) Error() string {
	return ErrNoVacancies.Error() + ": " + strconv.Itoa(e.Offers) + " offers for " + strconv.Itoa(e.Vacancies) + " vacancies"
}

// Unwrap makes errors.Is(err, ErrNoVacancies) true.
func (e *NoVacanciesError) Unwrap() error {
	return ErrNoVacancies
}

// Details gives the admin the numbers behind the refusal.
func (e *NoVacanciesError) Details() map[string]any {
	return map[string]any{
		"vacancies":           e.Vacancies,
		"offers_made":         e.Offers,
		"remaining_vacancies": max(e.Vacancies-e.Offers, 0),
	}
}

// ApplicationEvent is one row of the application_events table.
type ApplicationEvent struct {
	ID            string
	ApplicationID string

	// ActorID is who caused the event, or nil when the system did, or when
	// the actor's account has since been deleted.
	ActorID *string

	// ActorName and ActorRole are the actor's current name and role, read
	// from users when the event is read. Both are nil when ActorID is.
	ActorName *string
	ActorRole *string

	Type string

	// Payload is the event's detail. Its shape depends on Type.
	Payload map[string]any

	CreatedAt time.Time
}

// The orders a list of applications can be read in.
const (
	// ApplicationSortNewest is newest first, by submitted_at then id.
	ApplicationSortNewest = ""

	// ApplicationSortScore is highest score first, unscored last, then
	// newest first. Admins and interviewers only: a candidate cannot see
	// scores, so may not sort by them either.
	ApplicationSortScore = "score"
)

// ApplicationFilter narrows and pages a list of applications. The zero value
// means every application the viewer may see, first page, default size.
type ApplicationFilter struct {
	// Status, when not "", keeps only applications in that status.
	Status string

	// JobPostID, when not "", keeps only applications to that post.
	JobPostID string

	// Sort is ApplicationSortNewest (the zero value) or ApplicationSortScore.
	Sort string

	// After, when not nil, starts the page after this position.
	After *ListingPosition

	// Limit is the page size. The caller is expected to have bounded it.
	Limit int
}
