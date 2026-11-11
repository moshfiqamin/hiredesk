package scoring

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// experienceConfig is the config of an experience_years criterion:
// {"min": 0, "target": 2}.
type experienceConfig struct {
	// Min defaults to 0 when absent.
	Min float64 `json:"min"`

	// Target is a pointer so an absent target is an error rather than a
	// silent 0.
	Target *float64 `json:"target"`
}

// maxQuotedAnswer bounds how much of an unreadable answer is quoted back in
// a Detail, which is stored with every score.
const maxQuotedAnswer = 40

// newExperienceYears compiles an experience_years criterion. The answer is a
// number of years; the score ramps linearly from nothing at min to the full
// weight at target.
func newExperienceYears(c domain.Criterion) (scorer, error) {
	var cfg experienceConfig
	if err := decodeConfig(c.Config, &cfg); err != nil {
		return nil, err
	}
	if cfg.Target == nil {
		return nil, invalidConfig("target is required")
	}
	minYears, target := cfg.Min, *cfg.Target

	if minYears < 0 {
		return nil, invalidConfig("min must not be negative")
	}
	// target == min would make the ramp a division by zero.
	if target <= minYears {
		return nil, invalidConfig("target must be greater than min")
	}

	return singleAnswer(c.QuestionID, func(a domain.Answer) outcome {
		return rampYears(a.Text, minYears, target)
	}), nil
}

// rampYears scores one answer. It never fails: an answer it cannot read
// scores 0 and says why.
func rampYears(text string, minYears, target float64) outcome {
	years, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	// ParseFloat accepts "NaN" and "Inf", and returns ±Inf with an error for
	// out-of-range input; none of them is a number of years.
	if err != nil || math.IsNaN(years) || math.IsInf(years, 0) {
		return outcome{detail: "could not read " + strconv.Quote(excerpt(text)) + " as a number of years"}
	}

	y, lo, hi := formatYears(years), formatYears(minYears), formatYears(target)
	switch {
	case years < 0:
		return outcome{detail: y + " years is negative"}
	case years <= minYears:
		return outcome{detail: y + " years, at or below the minimum of " + lo}
	case years >= target:
		// Clamped: experience beyond the target earns the full weight and no
		// more, or one criterion could outweigh the rest of the rubric.
		return outcome{fraction: 1, detail: y + " years, at or above the target of " + hi}
	}

	fraction := (years - minYears) / (target - minYears)
	return outcome{
		fraction: fraction,
		detail:   fmt.Sprintf("%s years, %.0f%% of the way from %s to the target of %s", y, fraction*100, lo, hi),
	}
}

// formatYears prints a number with as many decimals as it needs: 2, 1.5.
func formatYears(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// excerpt shortens text to maxQuotedAnswer characters, counting runes so a
// multi-byte character is never cut in half.
func excerpt(text string) string {
	if utf8.RuneCountInString(text) <= maxQuotedAnswer {
		return text
	}
	return string([]rune(text)[:maxQuotedAnswer]) + "…"
}
