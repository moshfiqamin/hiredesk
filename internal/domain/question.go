package domain

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The kinds of question a form may contain. They mirror the
// questions_kind_check constraint in the database.
const (
	QuestionKindText    = "text"
	QuestionKindNumber  = "number"
	QuestionKindBoolean = "boolean"
	QuestionKindChoice  = "choice"
)

// Limits on a question. None is in the schema; they keep one form from being
// large enough to be a problem to render or to store answers for.
const (
	MaxQuestionPromptLength = 1000
	MaxQuestionChoices      = 50
	MaxChoiceLength         = 200
	MaxQuestionMinLength    = 10000
)

// maxQuestionPosition is the largest value the int column can hold.
const maxQuestionPosition = math.MaxInt32

// IsQuestionKind reports whether kind is one of the kinds above.
func IsQuestionKind(kind string) bool {
	switch kind {
	case QuestionKindText, QuestionKindNumber, QuestionKindBoolean, QuestionKindChoice:
		return true
	}
	return false
}

// CheckFormEditable returns a *FormLockedError unless the questions of a job
// post in this status may be added, changed or removed. Only a draft's may.
//
// This is a rule about fairness, not storage: once a post is open, candidates
// are answering the form as it stands, and changing it would mean that people
// applying for one job were asked different things.
func CheckFormEditable(status string) error {
	if status == JobStatusDraft {
		return nil
	}
	return &FormLockedError{Status: status}
}

// FormLockedError is an ErrFormLocked that says what status locked the form.
type FormLockedError struct {
	Status string
}

// Error names the status.
func (e *FormLockedError) Error() string {
	return ErrFormLocked.Error() + ": job post is " + e.Status
}

// Unwrap makes errors.Is(err, ErrFormLocked) true.
func (e *FormLockedError) Unwrap() error {
	return ErrFormLocked
}

// Details tells the client why the form cannot change.
func (e *FormLockedError) Details() map[string]any {
	return map[string]any{
		"current_status": e.Status,
		"reason":         "Questions can only be changed while the job post is a draft. Once it is open, candidates may already have answered them, and every candidate must be asked the same questions.",
	}
}

// NewQuestion is what the caller supplies to add a question to a form.
type NewQuestion struct {
	Prompt string
	Kind   string

	// Choices is the options of a "choice" question. Empty for other kinds.
	Choices []string

	Required bool

	// MinLength is the fewest characters a text answer may have. 0 means no
	// minimum, and is the only value allowed for other kinds.
	MinLength int

	// Position is where the question goes in the form. 0 asks for the slot
	// after the current last question.
	Position int
}

// Validate returns a *ValidationError naming each field that breaks a rule,
// or nil. Whether Position is free is not checked here: that depends on the
// other questions, which only the store can see.
func (in NewQuestion) Validate() error {
	var invalid ValidationError
	checkQuestionFields(&invalid, in.Prompt, in.Kind, in.Choices, in.MinLength)

	// 0 is a request for the next free slot, not a position, so only a value
	// that was actually given is checked.
	if in.Position != 0 {
		checkQuestionPosition(&invalid, in.Position)
	}
	return invalid.Err()
}

// QuestionPatch is a partial update to a question. A nil field is left as it
// is; a non-nil field is written.
//
// Choices is a pointer to a slice, not a slice, for the reason every field
// here is a pointer: a nil slice and an empty one both decode from JSON in
// ways that look like "nothing", and only the pointer says whether the key
// was sent. A pointer to an empty slice clears the choices, which is how a
// client changes a choice question into another kind.
type QuestionPatch struct {
	Prompt    *string
	Kind      *string
	Choices   *[]string
	Required  *bool
	MinLength *int
	Position  *int
}

// IsEmpty reports whether p changes nothing.
func (p QuestionPatch) IsEmpty() bool {
	return p == QuestionPatch{}
}

// Apply copies the fields patch sets onto q and leaves the others alone.
func (q *Question) Apply(patch QuestionPatch) {
	if patch.Prompt != nil {
		q.Prompt = *patch.Prompt
	}
	if patch.Kind != nil {
		q.Kind = *patch.Kind
	}
	if patch.Choices != nil {
		q.Choices = *patch.Choices
	}
	if patch.Required != nil {
		q.Required = *patch.Required
	}
	if patch.MinLength != nil {
		q.MinLength = *patch.MinLength
	}
	if patch.Position != nil {
		q.Position = *patch.Position
	}
}

// Validate returns a *ValidationError naming each field that breaks a rule,
// or nil. Call it after Apply: the rules for choices and min_length depend on
// the kind, which the patch may or may not have changed.
func (q *Question) Validate() error {
	var invalid ValidationError
	checkQuestionFields(&invalid, q.Prompt, q.Kind, q.Choices, q.MinLength)
	checkQuestionPosition(&invalid, q.Position)
	return invalid.Err()
}

// checkQuestionFields holds the rules shared by a new question and a patched
// one, so a question cannot be edited into a state it could not have been
// created in.
func checkQuestionFields(invalid *ValidationError, prompt, kind string, choices []string, minLength int) {
	switch n := utf8.RuneCountInString(prompt); {
	case n == 0:
		invalid.Add("prompt", "is required")
	case n > MaxQuestionPromptLength:
		invalid.Add("prompt", "must be at most "+strconv.Itoa(MaxQuestionPromptLength)+" characters")
	}

	if !IsQuestionKind(kind) {
		invalid.Add("kind", "must be one of text, number, boolean, choice")
	}

	// Choices belong to choice questions and only to them. An empty list
	// counts as absent, so a client can clear the choices while changing the
	// kind away from choice; a JSON null cannot do that (see QuestionPatch).
	if kind == QuestionKindChoice {
		checkChoices(invalid, choices)
	} else if len(choices) > 0 {
		invalid.Add("choices", "must be absent unless kind is choice")
	}

	// 0 is "no minimum" for every kind, so it is always accepted. Any other
	// value means something only for a text answer.
	switch {
	case minLength < 0:
		invalid.Add("min_length", "must not be negative")
	case minLength > MaxQuestionMinLength:
		invalid.Add("min_length", "must be at most "+strconv.Itoa(MaxQuestionMinLength))
	case minLength > 0 && kind != QuestionKindText:
		invalid.Add("min_length", "applies only to text questions; omit it or send 0")
	}
}

// checkChoices checks the options of a choice question.
func checkChoices(invalid *ValidationError, choices []string) {
	if len(choices) == 0 {
		invalid.Add("choices", "must list at least one option when kind is choice")
		return
	}
	if len(choices) > MaxQuestionChoices {
		invalid.Add("choices", "must list at most "+strconv.Itoa(MaxQuestionChoices)+" options")
		return
	}

	// An answer to a choice question is stored as the chosen text. Two
	// options that differ only in case could not be told apart in an answer
	// a reviewer reads, so duplicates are compared case-insensitively.
	seen := make(map[string]bool, len(choices))
	for _, choice := range choices {
		switch n := utf8.RuneCountInString(choice); {
		case n == 0:
			invalid.Add("choices", "must not contain an empty option")
			return
		case n > MaxChoiceLength:
			invalid.Add("choices", "options must be at most "+strconv.Itoa(MaxChoiceLength)+" characters")
			return
		}

		key := strings.ToLower(choice)
		if seen[key] {
			invalid.Add("choices", "must not contain the same option twice")
			return
		}
		seen[key] = true
	}
}

// checkQuestionPosition checks the range of a position. Uniqueness within the
// post is the store's to check.
func checkQuestionPosition(invalid *ValidationError, position int) {
	switch {
	case position < 1:
		invalid.Add("position", "must be at least 1")
	case position > maxQuestionPosition:
		invalid.Add("position", "must be at most "+strconv.Itoa(maxQuestionPosition))
	}
}
