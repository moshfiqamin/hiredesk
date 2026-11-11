// Package scoring turns a candidate's answers and a job post's rubric into a
// score.
//
// The package is pure: it reads no database, makes no HTTP call, never looks
// at the clock and uses no randomness. The same answers and criteria always
// produce the same Result, so every rule here is tested with plain values.
//
// Edge cases, each deliberate and covered by a test:
//
//   - An empty criteria list returns a zero Result with an empty Breakdown.
//     Percentage is 0, not NaN: it is only divided out when TotalAvailable
//     is above zero, which also covers a rubric whose weights are all zero.
//   - A criterion of unknown kind makes Score return an error wrapping
//     ErrUnknownKind. A rubric the engine cannot apply in full is not scored
//     in part.
//   - A config the kind cannot use, or a negative or non-finite weight,
//     makes Score return an error wrapping ErrInvalidConfig. A kind that
//     reads one answer but names no question returns ErrMissingQuestion.
//   - A criterion whose question has no answer scores 0 out of its weight,
//     with Detail "no answer provided". This is not an error: an unanswered
//     optional question is an ordinary application.
//
// Score compiles the rubric on every call. To score many applications
// against one rubric, call Compile once and reuse the *Rubric.
//
// Compile accepts any rubric it can apply. ValidateCriteria adds the rules
// for saving one, such as weights summing to 100, and reports every problem
// at once.
package scoring

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// NoAnswerDetail is the Detail of a criterion whose question was not answered.
const NoAnswerDetail = "no answer provided"

// Errors returned by Score and Compile, always inside a *CriterionError.
var (
	// ErrUnknownKind means a criterion's kind has no scorer.
	ErrUnknownKind = errors.New("unknown criterion kind")

	// ErrInvalidConfig means a criterion's config or weight cannot be used
	// by its kind.
	ErrInvalidConfig = errors.New("invalid criterion config")

	// ErrMissingQuestion means a criterion of a kind that reads one answer
	// names no question to read it from.
	ErrMissingQuestion = errors.New("criterion has no question")
)

// CriterionError says which criterion made a rubric unusable.
type CriterionError struct {
	CriterionID string
	Kind        string
	Err         error
}

// Error names the criterion and the problem.
func (e *CriterionError) Error() string {
	return "criterion " + e.CriterionID + " (" + e.Kind + "): " + e.Err.Error()
}

// Unwrap makes errors.Is work through the wrapper for each of the errors
// above.
func (e *CriterionError) Unwrap() error {
	return e.Err
}

// Result is the score of one application against one rubric.
type Result struct {
	TotalEarned    float64
	TotalAvailable float64

	// Percentage is TotalEarned as a share of TotalAvailable, from 0 to 100.
	Percentage float64

	// Breakdown has one entry per criterion, in rubric order.
	Breakdown []CriterionScore
}

// CriterionScore is what one criterion contributed, and why.
type CriterionScore struct {
	CriterionID string
	Label       string
	Kind        string
	Earned      float64
	Available   float64

	// Detail explains the number in words, for the reviewer's scorecard.
	Detail string

	// EvidenceAnswerID is the answer the score was based on. Empty when no
	// single answer was: the question was unanswered, or the kind reads
	// several answers.
	EvidenceAnswerID string
}

// answerIndex finds an application's answer to a question by question ID.
type answerIndex map[string]domain.Answer

// outcome is what a scorer reports. The engine, not the scorer, turns the
// fraction into points, so weighting and clamping happen in one place.
type outcome struct {
	fraction         float64 // share of the weight earned, 0 to 1
	detail           string
	evidenceAnswerID string
}

// scorer scores one compiled criterion against one application. It cannot
// fail: everything that could be wrong with the criterion was checked when it
// was compiled.
type scorer func(answers answerIndex) outcome

// newScorer checks one criterion's config and compiles it into a scorer.
type newScorer func(c domain.Criterion) (scorer, error)

// kindSpec is what the engine knows about one kind.
type kindSpec struct {
	build newScorer

	// needsQuestion is true for kinds that read the answer to the
	// criterion's own QuestionID.
	needsQuestion bool
}

// registry maps each kind to its spec. Read-only after package
// initialisation, so sharing it between goroutines is safe.
var registry = map[string]kindSpec{
	domain.CriterionKindKeywordMatch:    {build: newKeywordMatch, needsQuestion: true},
	domain.CriterionKindExperienceYears: {build: newExperienceYears, needsQuestion: true},
	domain.CriterionKindAnswerDepth:     {build: newAnswerDepth, needsQuestion: true},
	domain.CriterionKindBooleanAll:      {build: newBooleanAll},
}

// Rubric is a list of criteria whose configs have been checked and compiled.
// It is immutable, so one Rubric may score any number of applications,
// concurrently if need be.
type Rubric struct {
	criteria []compiledCriterion
}

type compiledCriterion struct {
	criterion domain.Criterion
	score     scorer
}

// Compile checks every criterion and prepares it for scoring. It returns a
// *CriterionError for the first problem it finds.
func Compile(criteria []domain.Criterion) (*Rubric, error) {
	r := &Rubric{criteria: make([]compiledCriterion, 0, len(criteria))}
	for _, c := range criteria {
		score, problems := checkCriterion(c)
		if len(problems) > 0 {
			return nil, &CriterionError{CriterionID: c.ID, Kind: c.Kind, Err: problems[0].err}
		}
		r.criteria = append(r.criteria, compiledCriterion{criterion: c, score: score})
	}
	return r, nil
}

// criterionProblem is one thing wrong with one criterion.
type criterionProblem struct {
	field   string // the criterion's JSON field: weight, kind, question_id, config
	message string // safe to show a client
	err     error  // wraps one of the package's sentinel errors
}

// checkCriterion runs every per-criterion check and, if all pass, returns the
// compiled scorer. Compile stops at the first problem; ValidateCriteria
// reports them all, so both share this one list of rules.
func checkCriterion(c domain.Criterion) (scorer, []criterionProblem) {
	var problems []criterionProblem

	// A negative weight would subtract from the total, and NaN would poison
	// every sum it touches. JSON cannot carry NaN, but a Criterion built in
	// Go can.
	if c.Weight < 0 || math.IsNaN(c.Weight) || math.IsInf(c.Weight, 0) {
		const msg = "must be a finite number of at least 0"
		problems = append(problems, criterionProblem{field: "weight", message: msg, err: invalidConfig("weight " + msg)})
	}

	spec, ok := registry[c.Kind]
	if !ok {
		// The remaining checks depend on the kind, so they cannot run.
		problems = append(problems, criterionProblem{
			field: "kind", message: "must be one of " + strings.Join(kindNames(), ", "), err: ErrUnknownKind,
		})
		return nil, problems
	}

	if spec.needsQuestion && c.QuestionID == "" {
		problems = append(problems, criterionProblem{
			field: "question_id", message: "is required for kind " + c.Kind, err: ErrMissingQuestion,
		})
	}

	score, err := spec.build(c)
	if err != nil {
		problems = append(problems, criterionProblem{field: "config", message: configReason(err), err: err})
	}

	if len(problems) > 0 {
		return nil, problems
	}
	return score, nil
}

// kindNames lists the kinds with a scorer, sorted so messages are stable.
func kindNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Score scores one application's answers against the rubric.
func (r *Rubric) Score(answers []domain.Answer) Result {
	index := indexAnswers(answers)

	// Non-nil even when empty, so it encodes as [] rather than null.
	res := Result{Breakdown: make([]CriterionScore, 0, len(r.criteria))}

	for _, cc := range r.criteria {
		o := cc.score(index)
		c := cc.criterion
		earned := clampFraction(o.fraction) * c.Weight

		res.Breakdown = append(res.Breakdown, CriterionScore{
			CriterionID:      c.ID,
			Label:            c.Label,
			Kind:             c.Kind,
			Earned:           earned,
			Available:        c.Weight,
			Detail:           o.detail,
			EvidenceAnswerID: o.evidenceAnswerID,
		})
		res.TotalEarned += earned
		res.TotalAvailable += c.Weight
	}

	// The only division in the package, and the only place a zero
	// denominator could arise.
	if res.TotalAvailable > 0 {
		res.Percentage = res.TotalEarned / res.TotalAvailable * 100
	}
	return res
}

// Score compiles criteria and scores answers against them. It returns an
// error only when the rubric itself is unusable; see the package comment.
func Score(answers []domain.Answer, criteria []domain.Criterion) (Result, error) {
	r, err := Compile(criteria)
	if err != nil {
		return Result{}, err
	}
	return r.Score(answers), nil
}

// indexAnswers keys answers by question. Submission refuses two answers to
// one question, so a repeat should not occur; if one does, the first wins,
// which keeps the result independent of anything but input order.
func indexAnswers(answers []domain.Answer) answerIndex {
	index := make(answerIndex, len(answers))
	for _, a := range answers {
		if _, seen := index[a.QuestionID]; !seen {
			index[a.QuestionID] = a
		}
	}
	return index
}

// singleAnswer adapts a strategy that reads one answer to the scorer
// signature. It owns the "no answer" rule and the evidence link, so no
// single-answer strategy has to repeat either.
func singleAnswer(questionID string, score func(a domain.Answer) outcome) scorer {
	return func(answers answerIndex) outcome {
		a, ok := answers[questionID]
		if !ok {
			return outcome{detail: NoAnswerDetail}
		}
		o := score(a)
		o.evidenceAnswerID = a.ID
		return o
	}
}

// clampFraction keeps a scorer's fraction within 0 to 1, so no criterion can
// earn more than its weight or less than nothing.
func clampFraction(f float64) float64 {
	switch {
	case !(f > 0): // also true for NaN, which fails every comparison
		return 0
	case f > 1:
		return 1
	}
	return f
}

// configError is an ErrInvalidConfig with a reason a client may be shown.
type configError struct {
	reason string
}

// Error prefixes the reason with the category.
func (e *configError) Error() string {
	return ErrInvalidConfig.Error() + ": " + e.reason
}

// Unwrap makes errors.Is(err, ErrInvalidConfig) true.
func (e *configError) Unwrap() error {
	return ErrInvalidConfig
}

// invalidConfig returns an error wrapping ErrInvalidConfig with a reason.
func invalidConfig(reason string) error {
	return &configError{reason: reason}
}

// configReason extracts the client-facing reason from a constructor's error.
func configReason(err error) string {
	var ce *configError
	if errors.As(err, &ce) {
		return ce.reason
	}
	return err.Error()
}

// decodeConfig decodes a criterion's config into dst, refusing unknown keys
// so a misspelt parameter is an error rather than a silent default. An empty
// config is treated as {}.
func decodeConfig(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return invalidConfig(decodeReason(err))
	}
	// Decode stops after the first value; anything after it is malformed.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return invalidConfig("unexpected data after the config object")
	}
	return nil
}

// decodeReason rewrites a decoding error for a client. The decoder's own
// messages name Go types such as "keywordConfig", which are internals.
func decodeReason(err error) string {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr) && typeErr.Field == "":
		return "must be a JSON object"
	case errors.As(err, &typeErr):
		return strconv.Quote(typeErr.Field) + " has the wrong type: got " + typeErr.Value
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		return "is not valid JSON"
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		// The decoder has no typed error for this one; the field name it
		// quotes is the client's own input.
		return strings.TrimPrefix(err.Error(), "json: ")
	}
	return "is not a valid config object"
}

// plural picks the singular or plural form of a noun for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
