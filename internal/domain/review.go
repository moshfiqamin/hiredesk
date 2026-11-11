package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The application_events types recorded by assignment and review.
const (
	EventApplicationAssigned      = "application.assigned"
	EventApplicationUnassigned    = "application.unassigned"
	EventApplicationReviewed      = "application.reviewed"
	EventApplicationReviewUpdated = "application.review_updated"
)

// ReviewEventTypes are the event types that describe a review. An event of
// one of these types is visible exactly when the review it describes is: its
// payload carries the mark.
var ReviewEventTypes = []string{EventApplicationReviewed, EventApplicationReviewUpdated}

// The range of a review's mark. The reviews_mark_check constraint in the
// database states the same range.
const (
	MinMark = 1
	MaxMark = 10
)

// MaxReviewNotesChars is the longest review notes accepted, in characters:
// several paragraphs, which is a review, not an essay.
const MaxReviewNotesChars = 5000

var (
	// ErrAlreadyReviewed means an interviewer tried to file a second review
	// of the same application. It also matches ErrDuplicate.
	ErrAlreadyReviewed = fmt.Errorf("already reviewed this application: %w", ErrDuplicate)

	// ErrNoReview means an interviewer tried to change their review of an
	// application they have not reviewed. It also matches ErrNotFound.
	ErrNoReview = fmt.Errorf("no review of this application by this interviewer: %w", ErrNotFound)
)

// Assignment is one interviewer assigned to one application.
type Assignment struct {
	ApplicationID    string
	InterviewerID    string
	InterviewerName  string
	InterviewerEmail string

	// AssignedBy and AssignedByName are nil when the admin who made the
	// assignment is unknown or their account has been deleted.
	AssignedBy     *string
	AssignedByName *string

	AssignedAt time.Time

	// Reviewed reports whether this interviewer has filed a review of the
	// application.
	Reviewed bool
}

// Review is one interviewer's mark and notes on one application.
type Review struct {
	ID              string
	ApplicationID   string
	InterviewerID   string
	InterviewerName string
	Mark            int

	// Notes is nil when the interviewer wrote none.
	Notes *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReviewPatch is a change to a review. A nil field is left as it is. Notes
// pointing at "" removes the notes.
type ReviewPatch struct {
	Mark  *int
	Notes *string
}

// CheckMark returns a field message if mark is outside MinMark to MaxMark,
// and "" if it is a valid mark.
func CheckMark(mark int) string {
	if mark < MinMark || mark > MaxMark {
		return MarkProblem
	}
	return ""
}

// MarkProblem is the message for a mark that is not a whole number in range,
// whether it was out of range, fractional, or not a number at all.
const MarkProblem = "must be a whole number from 1 to 10"

// NormalizeReviewNotes trims notes and reports what is wrong with them, if
// anything. Notes that are empty once trimmed become nil: a review with
// blank notes has no notes.
func NormalizeReviewNotes(notes string) (*string, string) {
	notes = strings.TrimSpace(notes)
	switch {
	case notes == "":
		return nil, ""
	case strings.ContainsRune(notes, 0):
		// JSON can carry "\u0000", and PostgreSQL text cannot store it: left
		// unchecked, it would be a 500.
		return nil, "must not contain a NUL character"
	case utf8.RuneCountInString(notes) > MaxReviewNotesChars:
		return nil, "must be at most " + strconv.Itoa(MaxReviewNotesChars) + " characters"
	}
	return &notes, ""
}

// ReviewScope is which reviews of an application a viewer may see.
type ReviewScope int

// The review scopes, from least to most.
const (
	// ReviewsNone: no reviews, and no review events.
	ReviewsNone ReviewScope = iota

	// ReviewsOwn: only the viewer's own review, and the events about it.
	ReviewsOwn

	// ReviewsAll: every review.
	ReviewsAll
)

// ApplicationView is what one viewer may see of an application's reviews and
// history, once they are allowed to see the application at all. It is
// decided once, by auth.ApplicationViewFor, and enforced in the SQL that
// reads reviews and events, so rows outside it never leave the database.
type ApplicationView struct {
	// ViewerID is whose review ReviewsOwn means.
	ViewerID string

	Reviews ReviewScope

	// EventTypes, when not nil, limits the events to these types. Nil means
	// every type, subject to Reviews.
	EventTypes []string
}

// ApplicationHistory is what an application accumulates after submission,
// as one viewer may see it.
type ApplicationHistory struct {
	// Answers is in form order.
	Answers []Answer

	// Events is oldest first, limited by the viewer's ApplicationView.
	Events []ApplicationEvent

	// Reviews is in the order they were filed, limited by the viewer's
	// ApplicationView. Nil means the viewer may not see reviews at all, as
	// opposed to empty: there are none they may see.
	Reviews []Review
}

// TimelineFilter selects one page of an application's timeline, newest
// first.
type TimelineFilter struct {
	Limit int

	// After is the (created_at, id) of the last event on the previous page,
	// or nil for the first page.
	After *ListingPosition
}
