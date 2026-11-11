package scoring_test

import (
	"encoding/json"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// booleanAllCriterion builds a boolean_all criterion of weight 10 over the
// given questions.
func booleanAllCriterion(t *testing.T, questionIDs ...string) domain.Criterion {
	t.Helper()
	return domain.Criterion{
		ID:     "c1",
		Label:  "Eligibility",
		Kind:   domain.CriterionKindBooleanAll,
		Weight: 10,
		Config: mustConfig(t, map[string]any{"question_ids": questionIDs}),
	}
}

func TestBooleanAll_RequiresAll(t *testing.T) {
	t.Parallel()

	c := booleanAllCriterion(t, "q1", "q2", "q3")
	tests := []struct {
		name    string
		answers []domain.Answer
		detail  string
	}{
		{
			name:    "two of three true",
			answers: []domain.Answer{answer("q1", "true"), answer("q2", "true"), answer("q3", "false")},
			detail:  "2 of 3 conditions met",
		},
		{
			name:    "none true",
			answers: []domain.Answer{answer("q1", "false"), answer("q2", "false"), answer("q3", "false")},
			detail:  "0 of 3 conditions met",
		},
		{
			// Not a boolean at all: not met, and not an error.
			name:    "unreadable answer",
			answers: []domain.Answer{answer("q1", "true"), answer("q2", "true"), answer("q3", "yes")},
			detail:  "2 of 3 conditions met",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, tt.answers...)
			if got.Earned != 0 || got.Available != 10 {
				t.Errorf("Earned/Available = %v/%v, want 0/10", got.Earned, got.Available)
			}
			if got.Detail != tt.detail {
				t.Errorf("Detail = %q, want %q", got.Detail, tt.detail)
			}
		})
	}
}

func TestBooleanAll_AllTrue(t *testing.T) {
	t.Parallel()

	got := scoreOne(t, booleanAllCriterion(t, "q1", "q2", "q3"),
		answer("q1", "true"), answer("q2", "true"), answer("q3", " true "))

	if got.Earned != 10 {
		t.Errorf("Earned = %v, want 10", got.Earned)
	}
	if got.Detail != "3 of 3 conditions met" {
		t.Errorf("Detail = %q, want %q", got.Detail, "3 of 3 conditions met")
	}
	// The score rests on three answers, so it names none of them.
	if got.EvidenceAnswerID != "" {
		t.Errorf("EvidenceAnswerID = %q, want empty", got.EvidenceAnswerID)
	}
}

func TestBooleanAll_MissingAnswer(t *testing.T) {
	t.Parallel()

	c := booleanAllCriterion(t, "q1", "q2", "q3")

	t.Run("one missing", func(t *testing.T) {
		t.Parallel()
		got := scoreOne(t, c, answer("q1", "true"), answer("q2", "true"))
		if got.Earned != 0 {
			t.Errorf("Earned = %v, want 0", got.Earned)
		}
		if want := "2 of 3 conditions met (1 unanswered)"; got.Detail != want {
			t.Errorf("Detail = %q, want %q", got.Detail, want)
		}
	})

	t.Run("all missing", func(t *testing.T) {
		t.Parallel()
		got := scoreOne(t, c)
		if got.Earned != 0 {
			t.Errorf("Earned = %v, want 0", got.Earned)
		}
		if want := "0 of 3 conditions met (3 unanswered)"; got.Detail != want {
			t.Errorf("Detail = %q, want %q", got.Detail, want)
		}
	})
}

func TestBooleanAll_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config     string
		questionID string
	}{
		"empty list":       {config: `{"question_ids":[]}`},
		"absent list":      {config: `{}`},
		"empty ID":         {config: `{"question_ids":["q1",""]}`},
		"duplicate ID":     {config: `{"question_ids":["q1","q1"]}`},
		"question_id too":  {config: `{"question_ids":["q1"]}`, questionID: "q1"},
		"misspelt key":     {config: `{"question_id":["q1"]}`},
		"string, not list": {config: `{"question_ids":"q1"}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := domain.Criterion{
				ID: "c1", Kind: domain.CriterionKindBooleanAll, Weight: 10,
				QuestionID: tt.questionID, Config: json.RawMessage(tt.config),
			}
			_, err := scoring.Score(nil, []domain.Criterion{c})
			wantCriterionError(t, err, "c1", scoring.ErrInvalidConfig)
		})
	}
}
