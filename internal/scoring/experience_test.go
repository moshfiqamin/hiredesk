package scoring_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// experienceCriterion builds an experience_years criterion of weight 10
// reading question q1.
func experienceCriterion(t *testing.T, minYears, target float64) domain.Criterion {
	t.Helper()
	return domain.Criterion{
		ID:         "c1",
		Label:      "Go experience",
		Kind:       domain.CriterionKindExperienceYears,
		Weight:     10,
		QuestionID: "q1",
		Config:     mustConfig(t, map[string]float64{"min": minYears, "target": target}),
	}
}

func TestExperienceYears_LinearRamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text       string
		wantEarned float64
	}{
		{"0", 0},
		{"0.5", 2.5},
		{"1", 5}, // half the target, half the weight
		{"1.5", 7.5},
		{"2", 10},
		{" 1 ", 5}, // surrounding space is not a reason to fail
		{"3", 10},
	}

	c := experienceCriterion(t, 0, 2)
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", tt.text))
			if !approxEqual(got.Earned, tt.wantEarned) {
				t.Errorf("%q years: Earned = %v, want %v (%s)", tt.text, got.Earned, tt.wantEarned, got.Detail)
			}
			if got.EvidenceAnswerID != "a-q1" {
				t.Errorf("EvidenceAnswerID = %q, want a-q1", got.EvidenceAnswerID)
			}
		})
	}

	// The ramp starts at min, not at zero.
	t.Run("non-zero min", func(t *testing.T) {
		t.Parallel()
		shifted := experienceCriterion(t, 2, 6)
		for text, want := range map[string]float64{"1": 0, "2": 0, "3": 2.5, "4": 5, "6": 10} {
			if got := scoreOne(t, shifted, answer("q1", text)); !approxEqual(got.Earned, want) {
				t.Errorf("min 2, target 6, %s years: Earned = %v, want %v", text, got.Earned, want)
			}
		}
	})

	t.Run("detail", func(t *testing.T) {
		t.Parallel()
		got := scoreOne(t, c, answer("q1", "1"))
		if want := "1 years, 50% of the way from 0 to the target of 2"; got.Detail != want {
			t.Errorf("Detail = %q, want %q", got.Detail, want)
		}
	})
}

func TestExperienceYears_Unparseable(t *testing.T) {
	t.Parallel()

	c := experienceCriterion(t, 0, 2)
	for _, text := range []string{
		"a few", "", "two", "2 years", "2,5", "NaN", "Inf", "-Inf", "1e400",
		strings.Repeat("many ", 100),
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", text)) // fails the test on error; a panic fails it too
			if got.Earned != 0 || got.Available != 10 {
				t.Errorf("Earned/Available = %v/%v, want 0/10", got.Earned, got.Available)
			}
			if !strings.HasPrefix(got.Detail, "could not read ") {
				t.Errorf("Detail = %q, want it to say the answer could not be read", got.Detail)
			}
			if len(got.Detail) > 100 {
				t.Errorf("Detail is %d bytes; a long answer should be shortened", len(got.Detail))
			}
		})
	}
}

func TestExperienceYears_Negative(t *testing.T) {
	t.Parallel()

	c := experienceCriterion(t, 0, 2)
	for _, text := range []string{"-1", "-0.5", "-100"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", text))
			if got.Earned != 0 {
				t.Errorf("Earned = %v, want 0", got.Earned)
			}
			if !strings.Contains(got.Detail, "negative") {
				t.Errorf("Detail = %q, want it to say the answer is negative", got.Detail)
			}
		})
	}
}

func TestExperienceYears_AboveTarget(t *testing.T) {
	t.Parallel()

	res, err := scoring.Score(
		[]domain.Answer{answer("q1", "5")},
		[]domain.Criterion{experienceCriterion(t, 0, 2)},
	)
	if err != nil {
		t.Fatalf("Score: unexpected error: %v", err)
	}
	// Unclamped, (5 - 0) / (2 - 0) = 2.5: 25 points out of 10, 250%.
	if res.TotalEarned != 10 || res.Percentage != 100 {
		t.Errorf("Earned = %v, Percentage = %v; want 10 and 100", res.TotalEarned, res.Percentage)
	}
	if want := "5 years, at or above the target of 2"; res.Breakdown[0].Detail != want {
		t.Errorf("Detail = %q, want %q", res.Breakdown[0].Detail, want)
	}
}

func TestExperienceYears_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing target":   `{"min":0}`,
		"target equal min": `{"min":2,"target":2}`,
		"target below min": `{"min":3,"target":2}`,
		"negative min":     `{"min":-1,"target":2}`,
		"string target":    `{"target":"2"}`,
		"misspelt key":     `{"min":0,"traget":2}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := domain.Criterion{
				ID: "c1", Kind: domain.CriterionKindExperienceYears, Weight: 10, QuestionID: "q1",
				Config: json.RawMessage(raw),
			}
			_, err := scoring.Score(nil, []domain.Criterion{c})
			wantCriterionError(t, err, "c1", scoring.ErrInvalidConfig)
		})
	}
}
