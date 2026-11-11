package scoring_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// depthCriterion builds an answer_depth criterion of weight 10 reading q1.
func depthCriterion(t *testing.T, minChars, target int) domain.Criterion {
	t.Helper()
	return domain.Criterion{
		ID:         "c1",
		Label:      "Answer depth",
		Kind:       domain.CriterionKindAnswerDepth,
		Weight:     10,
		QuestionID: "q1",
		Config:     mustConfig(t, map[string]int{"min_chars": minChars, "target_chars": target}),
	}
}

func TestAnswerDepth_BelowMinimum(t *testing.T) {
	t.Parallel()

	c := depthCriterion(t, 50, 400)
	for name, text := range map[string]string{
		"40 chars":   strings.Repeat("a", 40),
		"exactly 50": strings.Repeat("a", 50), // the ramp starts at min, so min itself earns 0
		"empty":      "",
		"only space": "     \n\n\t   ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", text))
			if got.Earned != 0 || got.Available != 10 {
				t.Errorf("Earned/Available = %v/%v, want 0/10 (%s)", got.Earned, got.Available, got.Detail)
			}
			if got.EvidenceAnswerID != "a-q1" {
				t.Errorf("EvidenceAnswerID = %q, want a-q1", got.EvidenceAnswerID)
			}
		})
	}

	t.Run("detail", func(t *testing.T) {
		t.Parallel()
		got := scoreOne(t, c, answer("q1", strings.Repeat("a", 40)))
		if want := "40 characters, at or below the minimum of 50"; got.Detail != want {
			t.Errorf("Detail = %q, want %q", got.Detail, want)
		}
	})
}

func TestAnswerDepth_Proportional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		chars      int
		wantEarned float64
	}{
		{50, 0},
		{120, 2},   // (120 - 50) / 350 = 0.2
		{225, 5},   // halfway
		{330, 8},   // 0.8
		{400, 10},  // the target itself
		{5000, 10}, // clamped: length past the target earns no more
	}

	c := depthCriterion(t, 50, 400)
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.chars), func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", strings.Repeat("a", tt.chars)))
			if !approxEqual(got.Earned, tt.wantEarned) {
				t.Errorf("%d chars: Earned = %v, want %v (%s)", tt.chars, got.Earned, tt.wantEarned, got.Detail)
			}
		})
	}

	t.Run("detail", func(t *testing.T) {
		t.Parallel()
		got := scoreOne(t, c, answer("q1", strings.Repeat("a", 225)))
		if want := "225 characters, 50% of the way from 50 to the target of 400"; got.Detail != want {
			t.Errorf("Detail = %q, want %q", got.Detail, want)
		}
	})

	// Runs of whitespace count as one character, so padding earns nothing.
	t.Run("whitespace collapsed", func(t *testing.T) {
		t.Parallel()
		padded := "   " + strings.Repeat("word ", 20) + strings.Repeat(" ", 300) + "\n\n\n"
		got := scoreOne(t, c, answer("q1", padded))
		// 20 words of 4 letters with 19 single spaces between them = 99.
		if want := "99 characters, 14% of the way from 50 to the target of 400"; got.Detail != want {
			t.Errorf("Detail = %q, want %q", got.Detail, want)
		}
	})
}

func TestAnswerDepth_CountsRunesNotBytes(t *testing.T) {
	t.Parallel()

	// Each sample is the same number of characters a reader sees, but many
	// more bytes in UTF-8. Counting bytes would give these candidates two to
	// four times the credit, or full marks for half an answer.
	tests := []struct {
		name  string
		text  string
		runes int
		bytes int
	}{
		// "আমি" ("I") is three Bengali code points of three bytes each.
		{"Bengali", strings.Repeat("আমি", 40), 120, 360},
		// "বাংলা" ("Bangla"): ব া ং ল া, five code points.
		{"Bengali word", strings.Repeat("বাংলা", 24), 120, 360},
		{"emoji", strings.Repeat("👋", 120), 120, 480},
		{"accented Latin", strings.Repeat("é", 120), 120, 240},
	}

	// With min 0 and target 240, 120 characters is exactly half the weight.
	c := depthCriterion(t, 0, 240)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Check the sample is what the test claims, so a typo in it cannot
			// make the test pass for the wrong reason.
			if n := utf8.RuneCountInString(tt.text); n != tt.runes {
				t.Fatalf("sample has %d runes, want %d", n, tt.runes)
			}
			if n := len(tt.text); n != tt.bytes {
				t.Fatalf("sample has %d bytes, want %d", n, tt.bytes)
			}

			got := scoreOne(t, c, answer("q1", tt.text))
			if !approxEqual(got.Earned, 5) {
				t.Errorf("Earned = %v, want 5: %d characters of a 240 target (%s)", got.Earned, tt.runes, got.Detail)
			}
			if !strings.HasPrefix(got.Detail, "120 characters,") {
				t.Errorf("Detail = %q, want it to count 120 characters", got.Detail)
			}
		})
	}
}

func TestAnswerDepth_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing target":     `{"min_chars":50}`,
		"target equal min":   `{"min_chars":50,"target_chars":50}`,
		"target below min":   `{"min_chars":400,"target_chars":50}`,
		"negative min":       `{"min_chars":-1,"target_chars":50}`,
		"fractional target":  `{"min_chars":0,"target_chars":50.5}`,
		"misspelt key":       `{"min_chars":0,"target":400}`,
		"string, not number": `{"target_chars":"400"}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := domain.Criterion{
				ID: "c1", Kind: domain.CriterionKindAnswerDepth, Weight: 10, QuestionID: "q1",
				Config: json.RawMessage(raw),
			}
			_, err := scoring.Score(nil, []domain.Criterion{c})
			wantCriterionError(t, err, "c1", scoring.ErrInvalidConfig)
		})
	}
}
