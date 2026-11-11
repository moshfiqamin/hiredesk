package domain

// MaxRubricCriteria caps how many criteria one rubric may hold. Weights must
// sum to 100 with at most two decimals, so a longer list would be mostly
// criteria worth a fraction of a point.
const MaxRubricCriteria = 50

// MaxCriterionLabelLength is the longest criterion label accepted, in
// characters. A label is a heading on a scorecard, not a description.
const MaxCriterionLabelLength = 120

// EventRubricReplaced is the job_post_events type recorded when a job post's
// rubric is replaced by a new version.
const EventRubricReplaced = "rubric_replaced"

// Rubric is the current version of a job post's scoring rubric.
type Rubric struct {
	JobPostID string

	// Version is the rubric_version every criterion below shares. 0 means
	// the post has no rubric yet: stored versions start at 1.
	Version int

	// Criteria is in the order the admin wrote them. Never nil.
	Criteria []Criterion
}

// RubricReplacement is the outcome of replacing a job post's rubric.
type RubricReplacement struct {
	Rubric Rubric

	// PreviousVersion is the version that was current before, or 0 if the
	// post had no rubric.
	PreviousVersion int

	// ApplicationsToRescore is how many applications to the post were scored
	// against an earlier version, or not scored at all, and keep that score
	// until someone rescores them.
	ApplicationsToRescore int
}
