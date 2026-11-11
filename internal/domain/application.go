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
}
