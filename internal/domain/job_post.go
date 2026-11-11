package domain

import (
	"math"
	"slices"
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

// jobPostTransitions is the job post lifecycle: each status, and the statuses
// a post in it may move to next. It is the one place the lifecycle is
// written down. CanTransition and AllowedTransitions read it, and no other
// code compares statuses to decide whether a move is legal.
//
// closed maps to an empty list rather than being left out, so "closed is
// final" is stated here instead of implied by an absence.
var jobPostTransitions = map[string][]string{
	JobStatusDraft:  {JobStatusOpen},
	JobStatusOpen:   {JobStatusClosed},
	JobStatusClosed: {},
}

// CanTransition reports whether a job post in status from may move to status
// to. Staying in the same status is not a transition, and is false. Any
// status not in the lifecycle, on either side, is false.
func CanTransition(from, to string) bool {
	// A missing key yields a nil slice, which contains nothing, so an unknown
	// from needs no separate check.
	return slices.Contains(jobPostTransitions[from], to)
}

// AllowedTransitions returns the statuses a job post in status from may move
// to. It returns an empty, non-nil slice for a final or unknown status.
func AllowedTransitions(from string) []string {
	next := jobPostTransitions[from]

	// A copy, so a caller that appends to or sorts the result cannot change
	// the lifecycle for every later caller. make with the length, never nil,
	// so it encodes as [] in JSON.
	out := make([]string, len(next))
	copy(out, next)
	return out
}

// CheckTransition returns a *TransitionError if a job post in status from
// may not move to status to, and nil if it may.
func CheckTransition(from, to string) error {
	if CanTransition(from, to) {
		return nil
	}
	return &TransitionError{From: from, To: to, Allowed: AllowedTransitions(from)}
}

// TransitionError is an ErrInvalidTransition that says what was attempted
// and what would have been allowed.
type TransitionError struct {
	From    string
	To      string
	Allowed []string
}

// Error names both statuses. Neither is sensitive: the caller sent one and
// can read the other.
func (e *TransitionError) Error() string {
	return ErrInvalidTransition.Error() + ": " + e.From + " to " + e.To
}

// Unwrap makes errors.Is(err, ErrInvalidTransition) true.
func (e *TransitionError) Unwrap() error {
	return ErrInvalidTransition
}

// Details returns what a client needs to correct the request: where the post
// is, where it was asked to go, and where it could go instead.
func (e *TransitionError) Details() map[string]any {
	allowed := e.Allowed
	if allowed == nil {
		allowed = []string{}
	}
	return map[string]any{
		"current_status":        e.From,
		"requested_status":      e.To,
		"allowed_next_statuses": allowed,
	}
}

// OpenReadiness is what a job post has that opening it depends on. The
// caller counts; CheckReadyToOpen judges.
type OpenReadiness struct {
	// Questions is how many questions the post's application form has.
	Questions int

	// Criteria is how many criteria the current version of the post's rubric
	// has, and WeightedCriteria how many of those have a weight above zero.
	Criteria         int
	WeightedCriteria int
}

// CheckReadyToOpen returns a *NotReadyError naming everything a post still
// lacks before it may be opened, or nil if it lacks nothing.
func CheckReadyToOpen(r OpenReadiness) error {
	missing := map[string]string{}

	if r.Questions == 0 {
		missing["questions"] = "The post has no questions. Add at least one before opening it."
	}

	switch {
	case r.Criteria == 0:
		missing["rubric"] = "The post has no rubric. Add at least one scoring criterion before opening it."
	case r.WeightedCriteria == 0:
		// A weighted total divides by the sum of the weights, which is zero
		// here, so no application could be given a score.
		missing["rubric"] = "Every rubric criterion has a weight of 0. Give at least one a positive weight before opening the post."
	}

	if len(missing) == 0 {
		return nil
	}
	return &NotReadyError{Missing: missing}
}

// NotReadyError is an ErrNotReady that names each thing that is missing.
type NotReadyError struct {
	// Missing maps what is missing ("questions", "rubric") to a sentence for
	// the client saying what to do about it.
	Missing map[string]string
}

// Error lists what is missing, sorted so the same error always prints the
// same way.
func (e *NotReadyError) Error() string {
	names := make([]string, 0, len(e.Missing))
	for name := range e.Missing {
		names = append(names, name)
	}
	slices.Sort(names)

	out := ErrNotReady.Error() + ": missing"
	for _, name := range names {
		out += " " + name
	}
	return out
}

// Unwrap makes errors.Is(err, ErrNotReady) true.
func (e *NotReadyError) Unwrap() error {
	return ErrNotReady
}

// Details returns Missing as a client sees it.
func (e *NotReadyError) Details() map[string]any {
	out := make(map[string]any, len(e.Missing))
	for name, problem := range e.Missing {
		out[name] = problem
	}
	return out
}

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
