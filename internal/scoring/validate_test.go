package scoring_test

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// validationFields returns the field problems of a *domain.ValidationError,
// failing the test if err is anything else.
func validationFields(t *testing.T, err error) map[string]string {
	t.Helper()
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error = %v, want one wrapping domain.ErrValidation", err)
	}
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("error = %T, want *domain.ValidationError", err)
	}
	return v.Fields
}

// weighted returns keyword criteria with the given weights, each reading its
// own question, so only the weights can be wrong.
func weighted(t *testing.T, weights ...float64) []domain.Criterion {
	t.Helper()
	criteria := make([]domain.Criterion, 0, len(weights))
	for i, w := range weights {
		q := "q" + string(rune('a'+i))
		criteria = append(criteria, domain.Criterion{
			ID: "c-" + q, Kind: domain.CriterionKindKeywordMatch, Weight: w, QuestionID: q,
			Config: mustConfig(t, map[string]any{"keywords": []string{"go"}}),
		})
	}
	return criteria
}

func TestValidateCriteria_WeightsMustSum(t *testing.T) {
	t.Parallel()

	rejected := []struct {
		name    string
		weights []float64
		message string
	}{
		{"99", []float64{50, 49}, "weights must sum to 100; they sum to 99"},
		{"101", []float64{50, 51}, "weights must sum to 100; they sum to 101"},
		{"one hundredth short", []float64{50, 49.99}, "weights must sum to 100; they sum to 99.99"},
		{"far over", []float64{100, 100}, "weights must sum to 100; they sum to 200"},
		{"all zero", []float64{0, 0}, "weights must sum to 100; they sum to 0"},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fields := validationFields(t, scoring.ValidateCriteria(weighted(t, tt.weights...)))
			if fields["criteria"] != tt.message {
				t.Errorf(`Fields["criteria"] = %q, want %q`, fields["criteria"], tt.message)
			}
		})
	}

	accepted := []struct {
		name    string
		weights []float64
	}{
		{"100 exactly", []float64{60, 40}},
		{"single criterion", []float64{100}},
		{"two decimals", []float64{33.33, 33.33, 33.34}},
		{"the Borderless Work weights", []float64{30, 20, 15, 10, 15, 10}},
		// In float64 these add up to 99.999999999999986, not 100: none of the
		// three has an exact binary form. == would reject a correct rubric.
		{"float rounding", []float64{5.6, 66.6, 27.8}},
		{"a zero weight among others", []float64{100, 0}},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := scoring.ValidateCriteria(weighted(t, tt.weights...)); err != nil {
				t.Errorf("ValidateCriteria(%v) = %v, want nil", tt.weights, err)
			}
		})
	}

	// Proof the float rounding case really is a rounding case, so the test
	// above cannot pass for a reason other than the tolerance.
	t.Run("the rounding case is not exactly 100", func(t *testing.T) {
		t.Parallel()
		var sum float64
		for _, w := range []float64{5.6, 66.6, 27.8} {
			sum += w
		}
		if sum == 100 {
			t.Fatalf("5.6 + 66.6 + 27.8 == 100 in float64; pick another example")
		}
	})
}

func TestValidateCriteria_UnknownKind(t *testing.T) {
	t.Parallel()

	criteria := borderlessWorkRubric(t)
	criteria[1].Kind = "astrology"

	fields := validationFields(t, scoring.ValidateCriteria(criteria))
	want := "must be one of answer_depth, boolean_all, experience_years, keyword_match"
	if fields["criteria[1].kind"] != want {
		t.Errorf(`Fields["criteria[1].kind"] = %q, want %q`, fields["criteria[1].kind"], want)
	}
	// The kind decides what config means, so the config is not judged.
	if _, ok := fields["criteria[1].config"]; ok {
		t.Errorf("config was checked although the kind is unknown: %v", fields)
	}
	if len(fields) != 1 {
		t.Errorf("Fields = %v, want only the kind problem", fields)
	}
}

func TestValidateCriteria_MalformedConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		config  string
		message string
	}{
		{"truncated JSON", domain.CriterionKindKeywordMatch, `{"keywords":`, "is not valid JSON"},
		{"not JSON", domain.CriterionKindKeywordMatch, `keywords`, "is not valid JSON"},
		{"not an object", domain.CriterionKindKeywordMatch, `["go"]`, "must be a JSON object"},
		{"wrong type", domain.CriterionKindKeywordMatch, `{"keywords":"go"}`, `"keywords" has the wrong type: got string`},
		{"misspelt key", domain.CriterionKindExperienceYears, `{"min":0,"traget":2}`, `unknown field "traget"`},
		{"trailing data", domain.CriterionKindKeywordMatch, `{"keywords":["go"]} {}`, "unexpected data after the config object"},

		// Well-formed JSON, missing what the kind requires.
		{"keyword_match without keywords", domain.CriterionKindKeywordMatch, `{}`, "keywords must list at least one keyword"},
		{"experience_years without target", domain.CriterionKindExperienceYears, `{"min":0}`, "target is required"},
		{"answer_depth without target", domain.CriterionKindAnswerDepth, `{"min_chars":50}`, "target_chars is required"},
		{"boolean_all without questions", domain.CriterionKindBooleanAll, `{}`, "question_ids must list at least one question"},
		{"empty config", domain.CriterionKindAnswerDepth, ``, "target_chars is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := domain.Criterion{
				ID: "c1", Kind: tt.kind, Weight: 100, Config: json.RawMessage(tt.config),
			}
			if tt.kind != domain.CriterionKindBooleanAll {
				c.QuestionID = "q1"
			}

			fields := validationFields(t, scoring.ValidateCriteria([]domain.Criterion{c}))
			if fields["criteria[0].config"] != tt.message {
				t.Errorf(`Fields["criteria[0].config"] = %q, want %q`, fields["criteria[0].config"], tt.message)
			}
			if len(fields) != 1 {
				t.Errorf("Fields = %v, want only the config problem", fields)
			}
		})
	}
}

func TestValidateCriteria_CollectsAllErrors(t *testing.T) {
	t.Parallel()

	// Start from the valid rubric and break five criteria in five ways.
	criteria := borderlessWorkRubric(t)
	criteria[0].Weight = -30                                // negative weight
	criteria[1].Kind = "astrology"                          // unknown kind
	criteria[2].Config = json.RawMessage(`{"keywords":[]}`) // config missing its keywords
	criteria[3].QuestionID = ""                             // answer_depth with no question
	criteria[4].Weight = 15.005                             // more decimals than the column holds

	fields := validationFields(t, scoring.ValidateCriteria(criteria))

	want := map[string]string{
		"criteria[0].weight":      "must be a finite number of at least 0",
		"criteria[1].kind":        "must be one of answer_depth, boolean_all, experience_years, keyword_match",
		"criteria[2].config":      "keywords must list at least one keyword",
		"criteria[3].question_id": "is required for kind answer_depth",
		"criteria[4].weight":      "must have at most two decimal places",
		// -30 + 20 + 15 + 10 + 15.005 + 10 = 40.005, shown to two places.
		"criteria": "weights must sum to 100; they sum to 40.01",
	}
	for field, message := range want {
		if fields[field] != message {
			t.Errorf("Fields[%q] = %q, want %q", field, fields[field], message)
		}
	}
	if len(fields) != len(want) {
		got := make([]string, 0, len(fields))
		for f := range fields {
			got = append(got, f)
		}
		slices.Sort(got)
		t.Errorf("got %d problems %v, want %d", len(fields), got, len(want))
	}

	// One criterion with several problems reports each under its own field.
	t.Run("several problems in one criterion", func(t *testing.T) {
		t.Parallel()
		c := domain.Criterion{
			ID: "c1", Kind: domain.CriterionKindAnswerDepth, Weight: -1,
			Config: json.RawMessage(`{"min_chars":50}`),
		}
		fields := validationFields(t, scoring.ValidateCriteria([]domain.Criterion{c}))
		for _, f := range []string{"criteria[0].weight", "criteria[0].question_id", "criteria[0].config", "criteria"} {
			if _, ok := fields[f]; !ok {
				t.Errorf("no problem reported for %s; got %v", f, fields)
			}
		}
	})
}

func TestValidateCriteria_Empty(t *testing.T) {
	t.Parallel()

	for name, criteria := range map[string][]domain.Criterion{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fields := validationFields(t, scoring.ValidateCriteria(criteria))
			if want := "must contain at least one criterion"; fields["criteria"] != want {
				t.Errorf(`Fields["criteria"] = %q, want %q`, fields["criteria"], want)
			}
		})
	}
}

func TestValidateCriteria_NonFiniteWeight(t *testing.T) {
	t.Parallel()

	for name, w := range map[string]float64{"NaN": math.NaN(), "infinite": math.Inf(1)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fields := validationFields(t, scoring.ValidateCriteria(weighted(t, w, 100)))
			if fields["criteria[0].weight"] == "" {
				t.Errorf("no weight problem reported; got %v", fields)
			}
			// A sum containing NaN says nothing, so it is not reported.
			if _, ok := fields["criteria"]; ok {
				t.Errorf("sum reported alongside a non-finite weight: %v", fields)
			}
		})
	}
}

func TestValidateCriteria_ValidRubric(t *testing.T) {
	t.Parallel()

	if err := scoring.ValidateCriteria(borderlessWorkRubric(t)); err != nil {
		t.Errorf("ValidateCriteria = %v, want nil", err)
	}
}
