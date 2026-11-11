package scoring

import (
	"math"
	"strconv"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// RequiredWeightTotal is what a rubric's weights must add up to, so that a
// score's total reads directly as a percentage.
const RequiredWeightTotal = 100

// weightTolerance is how far a sum of weights may stray from
// RequiredWeightTotal and still count as equal. Adding float64 weights leaves
// errors around 1e-14; weights are stored with two decimals, so a real
// difference is at least 0.01. 1e-6 sits far from both.
const weightTolerance = 1e-6

// ValidateCriteria checks a rubric before it is saved. It returns nil, or a
// *domain.ValidationError naming every problem found, keyed by field such as
// "criteria[2].config".
//
// It applies every check Compile does, and adds rules for a rubric as a
// whole: at least one criterion, weights with at most two decimals (what the
// numeric(6, 2) column holds), and weights summing to RequiredWeightTotal.
func ValidateCriteria(criteria []domain.Criterion) error {
	var invalid domain.ValidationError

	if len(criteria) == 0 {
		invalid.Add("criteria", "must contain at least one criterion")
		return invalid.Err()
	}

	var sum float64
	sumIsMeaningful := true
	for i, c := range criteria {
		prefix := "criteria[" + strconv.Itoa(i) + "]."

		_, problems := checkCriterion(c)
		for _, p := range problems {
			invalid.Add(prefix+p.field, p.message)
		}

		// A NaN or infinite weight is already reported above, and would make
		// the sum say nothing useful.
		if math.IsNaN(c.Weight) || math.IsInf(c.Weight, 0) {
			sumIsMeaningful = false
			continue
		}
		sum += c.Weight

		// Only checked when the weight passed the checks above, so it does
		// not replace the more basic complaint about the same field.
		if c.Weight >= 0 && !hasAtMostTwoDecimals(c.Weight) {
			invalid.Add(prefix+"weight", "must have at most two decimal places")
		}
	}

	// Compared with a tolerance, never ==: see weightTolerance.
	if sumIsMeaningful && math.Abs(sum-RequiredWeightTotal) > weightTolerance {
		invalid.Add("criteria", "weights must sum to "+strconv.Itoa(RequiredWeightTotal)+
			"; they sum to "+formatWeight(sum))
	}

	return invalid.Err()
}

// hasAtMostTwoDecimals reports whether w is a whole number of hundredths.
// 0.1 has no exact float64 form, so w*100 is compared to the nearest whole
// number with a tolerance rather than tested for an exact integer.
func hasAtMostTwoDecimals(w float64) bool {
	hundredths := w * 100
	return math.Abs(hundredths-math.Round(hundredths)) < weightTolerance
}

// formatWeight prints a sum of weights to two decimals, dropping trailing
// zeros: 99, 100.5, 99.99.
func formatWeight(w float64) string {
	return strconv.FormatFloat(math.Round(w*100)/100, 'f', -1, 64)
}
