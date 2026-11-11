package domain

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxCVURLLength is the longest CV link accepted, in bytes. Browsers and CDNs
// commonly stop at around 2,000 characters, so a longer link would not open.
const MaxCVURLLength = 2048

// The codes an AnswerProblem may carry. Clients branch on these, so they are
// part of the API and must not be renamed.
const (
	AnswerRequired        = "required"
	AnswerUnknownQuestion = "unknown_question"
	AnswerDuplicate       = "duplicate_answer"
	AnswerInvalidNumber   = "invalid_number"
	AnswerInvalidBoolean  = "invalid_boolean"
	AnswerInvalidChoice   = "invalid_choice"
	AnswerTooShort        = "too_short"
)

// NoIndex is the AnswerProblem.Index of a problem that no submitted answer
// caused, such as a required question nobody answered.
const NoIndex = -1

// CheckAcceptingApplications returns nil if a job post in this status takes
// applications, and an error otherwise:
//
//   - for a draft, one matching ErrNotFound. A candidate cannot see a draft,
//     so being told it exists but is closed to them would reveal it.
//   - for any other status, a *NotAcceptingError.
//
// Call it before ValidateApplication, and stop if it fails: the answers are
// judged against the form, and a post nobody may apply to has no form to
// judge them against.
func CheckAcceptingApplications(status string) error {
	switch status {
	case JobStatusOpen:
		return nil
	case JobStatusDraft:
		return ErrNotFound
	default:
		return &NotAcceptingError{Status: status}
	}
}

// NotAcceptingError is an ErrNotAcceptingApplications that says what status
// the post is in.
type NotAcceptingError struct {
	Status string
}

// Error names the status.
func (e *NotAcceptingError) Error() string {
	return ErrNotAcceptingApplications.Error() + ": job post is " + e.Status
}

// Unwrap makes errors.Is(err, ErrNotAcceptingApplications) true.
func (e *NotAcceptingError) Unwrap() error {
	return ErrNotAcceptingApplications
}

// Details tells the client why the application was refused.
func (e *NotAcceptingError) Details() map[string]any {
	return map[string]any{
		"current_status": e.Status,
		"reason":         "Applications are accepted only while a job post is open.",
	}
}

// AnswerProblem is one thing wrong with the answers.
type AnswerProblem struct {
	// QuestionID is the question the problem is about, as the client sent it
	// or as the form has it.
	QuestionID string

	// Index is the position in the submitted answers of the answer at fault,
	// or NoIndex when the problem is an answer that is missing.
	Index int

	// Code is one of the Answer* constants; Message says the same for people.
	Code    string
	Message string

	// MinLength and Got are set for AnswerTooShort only: the fewest
	// characters the question accepts, and how many the answer had.
	MinLength int
	Got       int

	// Choices is set for AnswerInvalidChoice only: the options the form offers.
	Choices []string
}

// SubmissionError is an ErrValidation for an application. It reports every
// problem at once, so a candidate can fix the whole form in one round trip.
type SubmissionError struct {
	// CVURL is what is wrong with the CV link, or "" if nothing is.
	CVURL string

	// Answers is every problem with the answers, in the order the rules are
	// checked: missing required answers, then answers to questions not on
	// the form, then answers of the wrong kind, then text that is too short.
	Answers []AnswerProblem
}

// Error counts the problems. The detail is left to Details: this string is
// for logs, and may not contain the candidate's answers.
func (e *SubmissionError) Error() string {
	out := ErrValidation.Error() + ": " + strconv.Itoa(len(e.Answers)) + " answer problem(s)"
	if e.CVURL != "" {
		out += ", cv_url"
	}
	return out
}

// Unwrap makes errors.Is(err, ErrValidation) true, so the error maps to 422
// without the api package knowing this type.
func (e *SubmissionError) Unwrap() error {
	return ErrValidation
}

// Details returns the problems as a client sees them:
//
//	{"cv_url": "...", "answers": [{"question_id": "...", "code": "...", ...}]}
//
// A key is present only if it has a problem, as with ValidationError.
func (e *SubmissionError) Details() map[string]any {
	out := map[string]any{}
	if e.CVURL != "" {
		out["cv_url"] = e.CVURL
	}
	if len(e.Answers) == 0 {
		return out
	}

	answers := make([]map[string]any, 0, len(e.Answers))
	for _, p := range e.Answers {
		item := map[string]any{
			"question_id": p.QuestionID,
			"code":        p.Code,
			"message":     p.Message,
		}
		// Built key by key rather than from a struct with omitempty, which
		// would drop a legitimate 0 such as index 0.
		if p.Index != NoIndex {
			item["index"] = p.Index
		}
		if p.Code == AnswerTooShort {
			item["min_length"] = p.MinLength
			item["got"] = p.Got
		}
		if p.Code == AnswerInvalidChoice {
			item["choices"] = p.Choices
		}
		answers = append(answers, item)
	}
	out["answers"] = answers
	return out
}

// err returns e if it holds any problem, and nil otherwise. For the reason,
// see ValidationError.Err.
func (e *SubmissionError) err() error {
	if e.CVURL == "" && len(e.Answers) == 0 {
		return nil
	}
	return e
}

// ValidateApplication checks in against form, the questions of the job post
// it is for, and returns in with its answers in canonical form, ready to
// store. On failure it returns a *SubmissionError holding every problem
// found, not only the first.
//
// It assumes CheckAcceptingApplications has passed.
//
// A blank answer counts as no answer: it fails a required question and is
// dropped for an optional one, so "" never reaches the database.
func ValidateApplication(form []Question, in NewApplication) (NewApplication, error) {
	byID := make(map[string]Question, len(form))
	for _, q := range form {
		byID[q.ID] = q
	}

	// One slice per rule, joined at the end, so problems come out in rule
	// order whatever order the answers arrived in.
	var unknown, wrongKind, tooShort []AnswerProblem

	answered := make(map[string]bool, len(in.Answers))
	clean := make([]NewAnswer, 0, len(in.Answers))

	for i, a := range in.Answers {
		q, onForm := byID[a.QuestionID]
		if !onForm {
			unknown = append(unknown, AnswerProblem{
				QuestionID: a.QuestionID, Index: i, Code: AnswerUnknownQuestion,
				Message: "is not a question on this job post",
			})
			continue
		}

		// The schema allows one answer per question; refusing here gives a
		// 422 naming the answer, rather than a unique violation at insert.
		if answered[q.ID] {
			unknown = append(unknown, AnswerProblem{
				QuestionID: q.ID, Index: i, Code: AnswerDuplicate,
				Message: "answers a question that has already been answered",
			})
			continue
		}

		text := strings.TrimSpace(a.Answer)
		if text == "" {
			continue
		}
		answered[q.ID] = true

		canonical, problem := checkAnswer(q, text)
		if problem != nil {
			problem.Index = i
			if problem.Code == AnswerTooShort {
				tooShort = append(tooShort, *problem)
			} else {
				wrongKind = append(wrongKind, *problem)
			}
			continue
		}
		clean = append(clean, NewAnswer{QuestionID: q.ID, Answer: canonical})
	}

	// In form order, so the list reads top to bottom like the form does.
	var missing []AnswerProblem
	for _, q := range form {
		if q.Required && !answered[q.ID] {
			missing = append(missing, AnswerProblem{
				QuestionID: q.ID, Index: NoIndex, Code: AnswerRequired,
				Message: "is required",
			})
		}
	}

	invalid := SubmissionError{CVURL: checkCVURL(in.CVURL)}
	for _, rule := range [][]AnswerProblem{missing, unknown, wrongKind, tooShort} {
		invalid.Answers = append(invalid.Answers, rule...)
	}
	if err := invalid.err(); err != nil {
		return NewApplication{}, err
	}

	out := in
	out.CVURL = strings.TrimSpace(in.CVURL)
	out.Answers = clean
	return out, nil
}

// checkAnswer checks a non-blank, trimmed answer against its question's kind
// and returns it in canonical form, or the problem with it. The problem's
// Index is left for the caller to fill in.
func checkAnswer(q Question, text string) (string, *AnswerProblem) {
	problem := func(code, message string) *AnswerProblem {
		return &AnswerProblem{QuestionID: q.ID, Code: code, Message: message}
	}

	switch q.Kind {
	case QuestionKindNumber:
		// ParseFloat also accepts "NaN" and "Inf", which no scorer can
		// compare against a threshold, so those are refused separately.
		n, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return "", problem(AnswerInvalidNumber, "must be a number")
		}
		return text, nil

	case QuestionKindBoolean:
		// Only the two words, unlike strconv.ParseBool, which also takes
		// "1", "t" and "F": every stored boolean then reads the same way.
		switch strings.ToLower(text) {
		case "true":
			return "true", nil
		case "false":
			return "false", nil
		}
		return "", problem(AnswerInvalidBoolean, "must be true or false")

	case QuestionKindChoice:
		// Case-insensitive, because a form never holds two options that
		// differ only in case. The stored text is the form's own spelling.
		for _, choice := range q.Choices {
			if strings.EqualFold(text, choice) {
				return choice, nil
			}
		}
		p := problem(AnswerInvalidChoice, "must be one of the question's choices")
		p.Choices = q.Choices
		return "", p

	default:
		// text, and any kind added to the schema before it is added here:
		// stored as written rather than refused.
		if got := utf8.RuneCountInString(text); got < q.MinLength {
			p := problem(AnswerTooShort, "must be at least "+strconv.Itoa(q.MinLength)+" characters")
			p.MinLength = q.MinLength
			p.Got = got
			return "", p
		}
		return text, nil
	}
}

// checkCVURL returns what is wrong with a CV link, or "" if nothing is.
func checkCVURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "is required"
	}
	if len(raw) > MaxCVURLLength {
		return "must be at most " + strconv.Itoa(MaxCVURLLength) + " characters"
	}

	// url.Parse is lenient: "cv.pdf" and "/files/cv.pdf" parse as relative
	// references without error. The scheme and host checks are what make
	// the link absolute. Parse lowercases the scheme, so "HTTPS" passes.
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "must be an absolute http or https URL"
	}
	return ""
}
