package scoring

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// answerDepthConfig is the config of an answer_depth criterion:
// {"min_chars": 50, "target_chars": 400}.
type answerDepthConfig struct {
	// MinChars defaults to 0 when absent.
	MinChars int `json:"min_chars"`

	// TargetChars is a pointer so an absent target is an error rather than a
	// silent 0.
	TargetChars *int `json:"target_chars"`
}

// newAnswerDepth compiles an answer_depth criterion. The score ramps
// linearly from nothing at min_chars to the full weight at target_chars.
func newAnswerDepth(c domain.Criterion) (scorer, error) {
	var cfg answerDepthConfig
	if err := decodeConfig(c.Config, &cfg); err != nil {
		return nil, err
	}
	if cfg.TargetChars == nil {
		return nil, invalidConfig("target_chars is required")
	}
	minChars, target := cfg.MinChars, *cfg.TargetChars

	if minChars < 0 {
		return nil, invalidConfig("min_chars must not be negative")
	}
	// target == min would make the ramp a division by zero.
	if target <= minChars {
		return nil, invalidConfig("target_chars must be greater than min_chars")
	}

	return singleAnswer(c.QuestionID, func(a domain.Answer) outcome {
		return rampDepth(countChars(a.Text), minChars, target)
	}), nil
}

// countChars counts the characters a reader would count: runes, not bytes,
// so a Bengali letter (three bytes in UTF-8) counts once, as an English one
// does. Whitespace is collapsed first, so padding an answer with spaces or
// blank lines earns nothing.
func countChars(text string) int {
	return utf8.RuneCountInString(strings.Join(strings.Fields(text), " "))
}

// rampDepth scores a character count against the ramp.
func rampDepth(n, minChars, target int) outcome {
	chars := strconv.Itoa(n) + " " + plural(n, "character", "characters")
	switch {
	case n <= minChars:
		return outcome{detail: fmt.Sprintf("%s, at or below the minimum of %d", chars, minChars)}
	case n >= target:
		// Clamped for the same reason as experience_years: length beyond the
		// target earns the full weight and no more.
		return outcome{fraction: 1, detail: fmt.Sprintf("%s, at or above the target of %d", chars, target)}
	}

	fraction := float64(n-minChars) / float64(target-minChars)
	return outcome{
		fraction: fraction,
		detail:   fmt.Sprintf("%s, %.0f%% of the way from %d to the target of %d", chars, fraction*100, minChars, target),
	}
}
