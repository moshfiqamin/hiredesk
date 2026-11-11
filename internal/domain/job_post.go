package domain

import (
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

// The states a job post moves through. They mirror the
// job_posts_status_check constraint in the database.
const (
	JobStatusDraft  = "draft"
	JobStatusOpen   = "open"
	JobStatusClosed = "closed"
)

// MaxJobTitleLength is the longest title a job post may have, in characters.
const MaxJobTitleLength = 200

// maxVacancies is the largest value the int column can hold.
const maxVacancies = math.MaxInt32

// JobPost is one row of the job_posts table.
//
// The pointer fields are the nullable columns: nil is NULL. A plain string
// could not tell "no description" from "an empty description".
type JobPost struct {
	ID             string
	Title          string
	Description    *string
	EmploymentType *string
	Location       *string
	Vacancies      int
	SalaryMin      *Money
	SalaryMax      *Money
	Status         string
	ClosesAt       *time.Time

	// CreatedBy is nil once the author's account has been deleted.
	CreatedBy *string

	CreatedAt time.Time
	UpdatedAt time.Time

	// Questions is the application form, in display order. Only
	// GetJobPostByID and UpdateJobPost load it; ListJobPosts leaves it nil.
	Questions []Question
}

// Question is one row of the questions table: one field of a job post's
// application form.
type Question struct {
	ID        string
	JobPostID string
	Prompt    string
	Kind      string

	// Choices is the options of a "choice" question, and nil for other kinds.
	Choices []string

	Required  bool
	MinLength int
	Position  int
}

// NewJobPost is what the caller supplies to create a job post. The ID, the
// status and both timestamps are absent because the database assigns them.
type NewJobPost struct {
	Title          string
	Description    *string
	EmploymentType *string
	Location       *string
	Vacancies      int
	SalaryMin      *Money
	SalaryMax      *Money
	ClosesAt       *time.Time

	// CreatedBy is the ID of the admin creating the post.
	CreatedBy string
}

// Validate returns a *ValidationError naming each field that breaks a rule,
// or nil.
func (in NewJobPost) Validate() error {
	return validateJobPost(in.Title, in.Vacancies, in.SalaryMin, in.SalaryMax)
}

// JobPostPatch is a partial update to a job post. A nil field is left as it
// is; a non-nil field is written, including when it points at "" or 0.
//
// Every field is a pointer for that reason alone: nil is the one value that
// cannot be confused with something the caller meant to store.
type JobPostPatch struct {
	Title          *string
	Description    *string
	EmploymentType *string
	Location       *string
	Vacancies      *int
	SalaryMin      *Money
	SalaryMax      *Money
	ClosesAt       *time.Time
}

// IsEmpty reports whether p changes nothing.
func (p JobPostPatch) IsEmpty() bool {
	// A struct whose fields are all comparable can be compared with ==. Every
	// field here is a pointer, so this is true only when all of them are nil.
	return p == JobPostPatch{}
}

// Apply copies the fields patch sets onto p and leaves the others alone.
func (p *JobPost) Apply(patch JobPostPatch) {
	if patch.Title != nil {
		p.Title = *patch.Title
	}
	if patch.Vacancies != nil {
		p.Vacancies = *patch.Vacancies
	}

	// The nullable fields take the pointer itself: p.Description must become
	// non-nil, and the patch's pointer is already one to the new value.
	if patch.Description != nil {
		p.Description = patch.Description
	}
	if patch.EmploymentType != nil {
		p.EmploymentType = patch.EmploymentType
	}
	if patch.Location != nil {
		p.Location = patch.Location
	}
	if patch.SalaryMin != nil {
		p.SalaryMin = patch.SalaryMin
	}
	if patch.SalaryMax != nil {
		p.SalaryMax = patch.SalaryMax
	}
	if patch.ClosesAt != nil {
		p.ClosesAt = patch.ClosesAt
	}
}

// Validate returns a *ValidationError naming each field that breaks a rule,
// or nil. Call it after Apply: the salary rule spans two fields, and a patch
// that sends only one of them can only be judged against the stored other.
func (p *JobPost) Validate() error {
	return validateJobPost(p.Title, p.Vacancies, p.SalaryMin, p.SalaryMax)
}

// validateJobPost holds the rules shared by a new post and a patched one, so
// a post cannot be edited into a state it could not have been created in.
func validateJobPost(title string, vacancies int, salaryMin, salaryMax *Money) error {
	var invalid ValidationError

	// Counted in runes, not bytes: len("café") is 5, and a limit in bytes
	// would allow fewer characters in some languages than in others.
	switch n := utf8.RuneCountInString(title); {
	case n == 0:
		invalid.Add("title", "is required")
	case n > MaxJobTitleLength:
		invalid.Add("title", "must be at most "+strconv.Itoa(MaxJobTitleLength)+" characters")
	}

	switch {
	case vacancies < 1:
		invalid.Add("vacancies", "must be at least 1")
	case vacancies > maxVacancies:
		invalid.Add("vacancies", "must be at most "+strconv.Itoa(maxVacancies))
	}

	// Only when both are set: a post may state a minimum alone, a maximum
	// alone, or neither.
	if salaryMin != nil && salaryMax != nil && *salaryMin > *salaryMax {
		invalid.Add("salary_min", "must not be greater than salary_max")
	}

	return invalid.Err()
}
