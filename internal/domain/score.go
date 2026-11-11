package domain

import "errors"

// ErrNotScored means an application exists and the caller may see it, but no
// score has been stored for it: its post had no rubric when it was
// submitted.
var ErrNotScored = errors.New("application has not been scored")

// ScoreCard is the stored score of one application: one entry per criterion
// of the rubric version that scored it.
type ScoreCard struct {
	ApplicationID string
	RubricVersion int

	// Entries is in rubric order. Never empty: an application with no stored
	// scores has no ScoreCard, and ErrNotScored is returned instead.
	Entries []ScoreEntry
}

// ScoreEntry is one stored score, joined to the criterion that produced it.
type ScoreEntry struct {
	// Criterion is the criterion as it was in the version that scored the
	// application, which may no longer be the current one.
	Criterion Criterion

	Earned    float64
	Available float64
	Detail    string

	// Evidence is the answer the score rests on, or nil when the criterion
	// did not score a single answer.
	Evidence *ScoreEvidence
}

// ScoreEvidence is the answer behind a score, and the question it answered.
type ScoreEvidence struct {
	AnswerID string

	// Question is the prompt of the question, as the candidate was asked it.
	Question string

	// AnswerText is the full answer. It is for the server to cut an excerpt
	// from, and is not meant to be sent to a client whole.
	AnswerText string
}

// EventApplicationRescored is the application_events type recorded when an
// application's scores are replaced by a rescore.
const EventApplicationRescored = "application.rescored"

// ErrNoRubric means scoring was asked for against a job post that has no
// rubric, so there is nothing to score with.
var ErrNoRubric = errors.New("job post has no rubric")

// ScoreTotals is an application's score summed over one rubric version.
type ScoreTotals struct {
	RubricVersion int
	Earned        float64
	Available     float64
}

// RescoreResult is what a rescore changed for one application.
type RescoreResult struct {
	ApplicationID string

	// Previous is nil when the application had no score before.
	Previous *ScoreTotals
	Current  ScoreTotals
}

// Changed reports whether the rescore gave a different version or total.
func (r RescoreResult) Changed() bool {
	return r.Previous == nil || *r.Previous != r.Current
}

// JobPostRescore is the outcome of rescoring every application to a post.
type JobPostRescore struct {
	JobPostID     string
	RubricVersion int

	// Results has one entry per application, in application ID order.
	Results []RescoreResult
}
