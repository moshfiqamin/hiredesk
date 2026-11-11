package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewQuestionValidate(t *testing.T) {
	t.Parallel()

	valid := NewQuestion{Prompt: "Why this role?", Kind: QuestionKindText}

	tests := []struct {
		name       string
		edit       func(q *NewQuestion)
		wantFields []string
	}{
		{"a text question", func(*NewQuestion) {}, nil},
		{"text with a minimum length", func(q *NewQuestion) { q.MinLength = 50 }, nil},
		{"each other kind", func(q *NewQuestion) { q.Kind = QuestionKindNumber }, nil},
		{"boolean", func(q *NewQuestion) { q.Kind = QuestionKindBoolean }, nil},
		{"a choice question", func(q *NewQuestion) { q.Kind, q.Choices = QuestionKindChoice, []string{"Dhaka", "Remote"} }, nil},
		{"min_length 0 on any kind", func(q *NewQuestion) { q.Kind = QuestionKindNumber }, nil},
		{"an empty choices list on a text question", func(q *NewQuestion) { q.Choices = []string{} }, nil},
		{"an explicit position", func(q *NewQuestion) { q.Position = 3 }, nil},
		{"a prompt of exactly the limit", func(q *NewQuestion) { q.Prompt = strings.Repeat("a", MaxQuestionPromptLength) }, nil},

		{"no prompt", func(q *NewQuestion) { q.Prompt = "" }, []string{"prompt"}},
		{"a prompt over the limit", func(q *NewQuestion) { q.Prompt = strings.Repeat("a", MaxQuestionPromptLength+1) }, []string{"prompt"}},
		{"an unknown kind", func(q *NewQuestion) { q.Kind = "date" }, []string{"kind"}},
		{"kind in the wrong case", func(q *NewQuestion) { q.Kind = "Text" }, []string{"kind"}},
		{"no kind", func(q *NewQuestion) { q.Kind = "" }, []string{"kind"}},

		{"choice with no choices", func(q *NewQuestion) { q.Kind = QuestionKindChoice }, []string{"choices"}},
		{"choice with an empty list", func(q *NewQuestion) { q.Kind, q.Choices = QuestionKindChoice, []string{} }, []string{"choices"}},
		{"choices on a text question", func(q *NewQuestion) { q.Choices = []string{"a"} }, []string{"choices"}},
		{"choices on a boolean question", func(q *NewQuestion) { q.Kind, q.Choices = QuestionKindBoolean, []string{"Yes", "No"} }, []string{"choices"}},
		{"an empty option", func(q *NewQuestion) { q.Kind, q.Choices = QuestionKindChoice, []string{"a", ""} }, []string{"choices"}},
		{"a repeated option", func(q *NewQuestion) { q.Kind, q.Choices = QuestionKindChoice, []string{"Remote", "remote"} }, []string{"choices"}},
		{"an option over the limit", func(q *NewQuestion) {
			q.Kind, q.Choices = QuestionKindChoice, []string{strings.Repeat("a", MaxChoiceLength+1)}
		}, []string{"choices"}},
		{"too many options", func(q *NewQuestion) {
			q.Kind = QuestionKindChoice
			for i := 0; i <= MaxQuestionChoices; i++ {
				q.Choices = append(q.Choices, strings.Repeat("x", i+1))
			}
		}, []string{"choices"}},

		{"min_length on a number question", func(q *NewQuestion) { q.Kind, q.MinLength = QuestionKindNumber, 5 }, []string{"min_length"}},
		{"min_length on a choice question", func(q *NewQuestion) {
			q.Kind, q.Choices, q.MinLength = QuestionKindChoice, []string{"a"}, 1
		}, []string{"min_length"}},
		{"negative min_length", func(q *NewQuestion) { q.MinLength = -1 }, []string{"min_length"}},
		{"min_length over the limit", func(q *NewQuestion) { q.MinLength = MaxQuestionMinLength + 1 }, []string{"min_length"}},

		{"a negative position", func(q *NewQuestion) { q.Position = -1 }, []string{"position"}},
		{"several problems at once", func(q *NewQuestion) {
			q.Prompt, q.Kind, q.Choices = "", "date", []string{"a"}
		}, []string{"prompt", "kind", "choices"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := valid
			tt.edit(&in)
			assertFields(t, in.Validate(), tt.wantFields)
		})
	}
}

// assertFields fails unless err reports exactly wantFields, or is nil when
// wantFields is empty.
func assertFields(t *testing.T, err error, wantFields []string) {
	t.Helper()

	if len(wantFields) == 0 {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		return
	}

	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a *ValidationError", err)
	}
	if len(invalid.Fields) != len(wantFields) {
		t.Errorf("fields = %v, want exactly %v", invalid.Fields, wantFields)
	}
	for _, field := range wantFields {
		if invalid.Fields[field] == "" {
			t.Errorf("no problem reported for %q; got %v", field, invalid.Fields)
		}
	}
}

func TestQuestionApplyAndValidate(t *testing.T) {
	t.Parallel()

	choice := func() Question {
		return Question{ID: "q-1", Prompt: "Office?", Kind: QuestionKindChoice, Choices: []string{"Dhaka", "Remote"}, Required: true, Position: 2}
	}

	t.Run("an empty patch changes nothing", func(t *testing.T) {
		t.Parallel()

		q := choice()
		q.Apply(QuestionPatch{})
		if q.Prompt != "Office?" || len(q.Choices) != 2 || !q.Required || q.Position != 2 {
			t.Errorf("question changed: %+v", q)
		}
		if !(QuestionPatch{}).IsEmpty() || (QuestionPatch{Required: ptr(false)}).IsEmpty() {
			t.Errorf("IsEmpty is wrong")
		}
	})

	t.Run("changing kind away from choice requires clearing the choices", func(t *testing.T) {
		t.Parallel()

		q := choice()
		q.Apply(QuestionPatch{Kind: ptr(QuestionKindText)})
		assertFields(t, q.Validate(), []string{"choices"})

		q = choice()
		q.Apply(QuestionPatch{Kind: ptr(QuestionKindText), Choices: ptr([]string{})})
		assertFields(t, q.Validate(), nil)
	})

	t.Run("changing kind to choice requires choices", func(t *testing.T) {
		t.Parallel()

		q := Question{Prompt: "Why?", Kind: QuestionKindText, Position: 1}
		q.Apply(QuestionPatch{Kind: ptr(QuestionKindChoice)})
		assertFields(t, q.Validate(), []string{"choices"})
	})

	t.Run("a stored min_length blocks a change of kind until it is reset", func(t *testing.T) {
		t.Parallel()

		q := Question{Prompt: "Why?", Kind: QuestionKindText, MinLength: 50, Position: 1}
		q.Apply(QuestionPatch{Kind: ptr(QuestionKindNumber)})
		assertFields(t, q.Validate(), []string{"min_length"})

		q.Apply(QuestionPatch{MinLength: ptr(0)})
		assertFields(t, q.Validate(), nil)
	})

	t.Run("position 0 is invalid on a stored question", func(t *testing.T) {
		t.Parallel()

		q := choice()
		q.Apply(QuestionPatch{Position: ptr(0)})
		assertFields(t, q.Validate(), []string{"position"})
	})
}

func TestCheckFormEditable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		locked bool
	}{
		{JobStatusDraft, false},
		{JobStatusOpen, true},
		{JobStatusClosed, true},
		// Anything that is not a draft is locked, including values that
		// should never occur: the rule fails closed.
		{"", true},
		{"Draft", true},
	}

	for _, tt := range tests {
		err := CheckFormEditable(tt.status)
		if !tt.locked {
			if err != nil {
				t.Errorf("CheckFormEditable(%q) = %v, want nil", tt.status, err)
			}
			continue
		}

		var locked *FormLockedError
		if !errors.As(err, &locked) || !errors.Is(err, ErrFormLocked) {
			t.Fatalf("CheckFormEditable(%q) = %v, want a *FormLockedError", tt.status, err)
		}
		if got := locked.Details()["current_status"]; got != tt.status {
			t.Errorf("details current_status = %v, want %q", got, tt.status)
		}
		if locked.Details()["reason"] == "" {
			t.Errorf("details has no reason")
		}
		if want := "application form is locked: job post is " + tt.status; err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	}
}

func TestIsQuestionKind(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{QuestionKindText, QuestionKindNumber, QuestionKindBoolean, QuestionKindChoice} {
		if !IsQuestionKind(kind) {
			t.Errorf("IsQuestionKind(%q) = false", kind)
		}
	}
	for _, kind := range []string{"", "Text", "date", "file"} {
		if IsQuestionKind(kind) {
			t.Errorf("IsQuestionKind(%q) = true", kind)
		}
	}
}
