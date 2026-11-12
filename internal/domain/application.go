package domain

import "time"

// The stages an application moves through. They mirror the
// applications_status_check constraint in the database.
const (
	StatusSubmitted   = "submitted"
	StatusShortlisted = "shortlisted"
	StatusInterview   = "interview"
	StatusOffer       = "offer"
	StatusRejected    = "rejected"
)

// EventApplicationSubmitted is the application_events type recorded when a
// candidate submits an application.
const EventApplicationSubmitted = "application.submitted"

// Application is one row of the applications table: a candidate's
// application to one job post.
type Application struct {
	ID          string
	JobPostID   string
	CandidateID string
	Status      string
	CVURL       string
	SubmittedAt time.Time
	UpdatedAt   time.Time

	// Answers is what the candidate said on the form. Only SubmitApplication
	// loads it; the list and single reads leave it nil.
	Answers []Answer

	// RubricVersion is the rubric version the application was scored against
	// when it was submitted, or nil if its post had no rubric. Only
	// SubmitApplication sets it.
	RubricVersion *int

	// The fields below are set only by ListApplications, for the admin
	// applications table, so one page needs no follow-up request per row.

	// CandidateName and JobPostTitle name the two IDs above.
	CandidateName string
	JobPostTitle  string

	// Score is nil when the viewer may not see scores (a candidate). When
	// set, its Percentage is nil if the application has no score yet.
	Score *ListedScore

	// Assignees is nil unless the viewer is an admin: who is assigned to
	// review the application is not the candidate's or another
	// interviewer's business.
	Assignees []Assignee

	// ReviewedByMe is nil unless the viewer is an interviewer: whether that
	// interviewer has filed their review of this application.
	ReviewedByMe *bool
}

// ListedScore is an application's latest total as a percentage, as the
// applications list shows it.
type ListedScore struct {
	// Percentage is nil for an application with no stored score.
	Percentage *float64
}

// Assignee is an interviewer assigned to an application, by ID and name.
type Assignee struct {
	ID   string
	Name string
}

// Answer is one row of the application_answers table.
//
// Text holds every kind of answer in its canonical text form: "42" for a
// number, "true" for a boolean, the option exactly as the form spells it for
// a choice.
type Answer struct {
	ID            string
	ApplicationID string
	QuestionID    string
	Text          string
	CreatedAt     time.Time
}

// NewApplication is what a candidate submits. The ID, status and timestamps
// are absent because the database assigns them.
type NewApplication struct {
	JobPostID   string
	CandidateID string
	CVURL       string
	Answers     []NewAnswer
}

// NewAnswer is one answer as the candidate sent it, before it has been
// checked against the form.
type NewAnswer struct {
	QuestionID string
	Answer     string
}
