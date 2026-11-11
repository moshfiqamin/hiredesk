package domain

import "encoding/json"

// The kinds of scoring criterion. They mirror the rubric_criteria_kind_check
// constraint in the database.
const (
	CriterionKindKeywordMatch    = "keyword_match"
	CriterionKindExperienceYears = "experience_years"
	CriterionKindBooleanAll      = "boolean_all"
	CriterionKindAnswerDepth     = "answer_depth"
)

// Criterion is one row of the rubric_criteria table: one rule a job post's
// applications are scored against.
type Criterion struct {
	ID    string
	Label string
	Kind  string

	// Weight is the most this criterion can contribute to the total. The
	// column is numeric(6, 2); float64 is exact enough for scoring, and the
	// store rounds back to two places when it writes a score.
	Weight float64

	// Config holds the kind-specific parameters exactly as stored in the
	// jsonb column. Each scorer decodes it into its own typed struct.
	Config json.RawMessage

	// QuestionID is the question whose answer this criterion reads. Empty
	// means the column is NULL: the criterion is not tied to one answer.
	QuestionID string

	RubricVersion int
}
