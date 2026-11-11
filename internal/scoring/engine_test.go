package scoring_test

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// approxEqual compares scores, which are products of float64 fractions and
// weights and so may differ from the expected value in the last bits.
func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// mustConfig encodes a config the way it arrives from the jsonb column.
func mustConfig(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding config: %v", err)
	}
	return raw
}

// answer builds an answer to question q, with an ID derived from it.
func answer(q, text string) domain.Answer {
	return domain.Answer{ID: "a-" + q, QuestionID: q, Text: text}
}

// scoreOne scores a single criterion and returns its breakdown entry,
// failing the test if the rubric is refused.
func scoreOne(t *testing.T, c domain.Criterion, answers ...domain.Answer) scoring.CriterionScore {
	t.Helper()
	res, err := scoring.Score(answers, []domain.Criterion{c})
	if err != nil {
		t.Fatalf("Score: unexpected error: %v", err)
	}
	if len(res.Breakdown) != 1 {
		t.Fatalf("Score: got %d breakdown entries, want 1", len(res.Breakdown))
	}
	return res.Breakdown[0]
}

// wantCriterionError checks that err is a *CriterionError for criterion id
// that wraps target.
func wantCriterionError(t *testing.T, err error, id string, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want one wrapping %v", err, target)
	}
	var ce *scoring.CriterionError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *scoring.CriterionError", err)
	}
	if ce.CriterionID != id {
		t.Errorf("CriterionError.CriterionID = %q, want %q", ce.CriterionID, id)
	}
}

// borderlessWorkRubric is the Borderless Work rubric: six criteria whose
// weights sum to 100. The labels, kinds and weights are the brief's; the
// keyword lists and thresholds are chosen for a backend Go role.
func borderlessWorkRubric(t *testing.T) []domain.Criterion {
	t.Helper()
	return []domain.Criterion{
		{
			ID: "go", Label: "Go knowledge", Kind: domain.CriterionKindKeywordMatch, Weight: 30, QuestionID: "q-go",
			Config: mustConfig(t, map[string]any{"keywords": []string{"go", "goroutine", "channel", "interface", "context"}}),
		},
		{
			ID: "db", Label: "Database skill", Kind: domain.CriterionKindKeywordMatch, Weight: 20, QuestionID: "q-db",
			Config: mustConfig(t, map[string]any{"keywords": []string{"postgres", "index", "transaction", "migration"}}),
		},
		{
			ID: "obs", Label: "Observability", Kind: domain.CriterionKindKeywordMatch, Weight: 15, QuestionID: "q-obs",
			Config: mustConfig(t, map[string]any{"keywords": []string{"prometheus", "grafana", "tracing"}}),
		},
		{
			ID: "depth", Label: "Answer depth", Kind: domain.CriterionKindAnswerDepth, Weight: 10, QuestionID: "q-project",
			Config: mustConfig(t, map[string]any{"min_chars": 50, "target_chars": 400}),
		},
		{
			ID: "years", Label: "Experience", Kind: domain.CriterionKindExperienceYears, Weight: 15, QuestionID: "q-years",
			Config: mustConfig(t, map[string]any{"min": 0, "target": 4}),
		},
		{
			ID: "remote", Label: "Remote readiness", Kind: domain.CriterionKindBooleanAll, Weight: 10,
			Config: mustConfig(t, map[string]any{"question_ids": []string{"q-overlap", "q-internet", "q-workspace"}}),
		},
	}
}

// borderlessWorkAnswers is one candidate's application to that rubric. The
// expected score of every answer is worked out in the comments of
// TestScore_BorderlessWorkRubric and in docs/notes/06-2.
func borderlessWorkAnswers() []domain.Answer {
	return []domain.Answer{
		answer("q-go", "I have written Go for three years. Each request gets a context with a "+
			"deadline, work fans out to goroutines, and results come back over a channel."),
		answer("q-db", "I run Postgres in production: every slow query gets an index, and "+
			"money only moves inside a transaction."),
		answer("q-obs", "I export Prometheus metrics from every service, and I add tracing to "+
			"each handler rather than relying on logs alone."),
		answer("q-project", "I rebuilt our invoicing service in Go. The old one timed out under "+
			"load, so I profiled it, found an N+1 query, and replaced it with one join. I added "+
			"tests for the slow paths first, then shipped behind a flag. p99 latency fell from "+
			"2.1s to 180ms within a week."),
		answer("q-years", "3"),
		answer("q-overlap", "true"),
		answer("q-internet", "true"),
		answer("q-workspace", "true"),
	}
}

func TestScore_BorderlessWorkRubric(t *testing.T) {
	t.Parallel()

	criteria := borderlessWorkRubric(t)
	if err := scoring.ValidateCriteria(criteria); err != nil {
		t.Fatalf("the rubric itself should be valid: %v", err)
	}

	res, err := scoring.Score(borderlessWorkAnswers(), criteria)
	if err != nil {
		t.Fatalf("Score: unexpected error: %v", err)
	}

	want := []struct {
		id     string
		earned float64
		detail string
	}{
		// 3 of 5 keywords: "goroutines" is not "goroutine", and no "interface". 3/5 x 30 = 18.
		{"go", 18, "matched 3 of 5 keywords: go, channel, context"},
		// 3 of 4: no "migration". 3/4 x 20 = 15.
		{"db", 15, "matched 3 of 4 keywords: postgres, index, transaction"},
		// 2 of 3: no "grafana". 2/3 x 15 = 10.
		{"obs", 10, "matched 2 of 3 keywords: prometheus, tracing"},
		// (260 - 50) / (400 - 50) = 0.6. 0.6 x 10 = 6.
		{"depth", 6, "260 characters, 60% of the way from 50 to the target of 400"},
		// (3 - 0) / (4 - 0) = 0.75. 0.75 x 15 = 11.25.
		{"years", 11.25, "3 years, 75% of the way from 0 to the target of 4"},
		// All three true. 1 x 10 = 10.
		{"remote", 10, "3 of 3 conditions met"},
	}

	if len(res.Breakdown) != len(want) {
		t.Fatalf("got %d breakdown entries, want %d", len(res.Breakdown), len(want))
	}
	for i, w := range want {
		got := res.Breakdown[i]
		if got.CriterionID != w.id {
			t.Errorf("Breakdown[%d] is %q, want %q (rubric order)", i, got.CriterionID, w.id)
		}
		if !approxEqual(got.Earned, w.earned) {
			t.Errorf("%s: Earned = %v, want %v (%s)", w.id, got.Earned, w.earned, got.Detail)
		}
		if got.Detail != w.detail {
			t.Errorf("%s: Detail = %q, want %q", w.id, got.Detail, w.detail)
		}
	}

	// 18 + 15 + 10 + 6 + 11.25 + 10 = 70.25 out of 100.
	if !approxEqual(res.TotalEarned, 70.25) {
		t.Errorf("TotalEarned = %v, want 70.25", res.TotalEarned)
	}
	if res.TotalAvailable != 100 {
		t.Errorf("TotalAvailable = %v, want 100", res.TotalAvailable)
	}
	if !approxEqual(res.Percentage, 70.25) {
		t.Errorf("Percentage = %v, want 70.25", res.Percentage)
	}
}

func TestScore_MissingAnswer(t *testing.T) {
	t.Parallel()

	criteria := borderlessWorkRubric(t)

	// Drop the answer to the project question and to one remote question.
	var answers []domain.Answer
	for _, a := range borderlessWorkAnswers() {
		if a.QuestionID != "q-project" && a.QuestionID != "q-internet" {
			answers = append(answers, a)
		}
	}

	res, err := scoring.Score(answers, criteria)
	if err != nil {
		t.Fatalf("Score: a missing answer must not be an error, got %v", err)
	}

	depth, remote := res.Breakdown[3], res.Breakdown[5]
	if depth.Earned != 0 || depth.Available != 10 || depth.Detail != scoring.NoAnswerDetail {
		t.Errorf("depth = %+v, want 0 of 10 with %q", depth, scoring.NoAnswerDetail)
	}
	if depth.EvidenceAnswerID != "" {
		t.Errorf("depth.EvidenceAnswerID = %q, want empty: there is no answer to point at", depth.EvidenceAnswerID)
	}
	if remote.Earned != 0 || remote.Detail != "2 of 3 conditions met (1 unanswered)" {
		t.Errorf("remote = %+v, want 0 with one unanswered", remote)
	}

	// Everything else scores as before: 70.25 - 6 (depth) - 10 (remote).
	if !approxEqual(res.TotalEarned, 54.25) || res.TotalAvailable != 100 {
		t.Errorf("totals = %v/%v, want 54.25/100", res.TotalEarned, res.TotalAvailable)
	}

	t.Run("no answers at all", func(t *testing.T) {
		t.Parallel()
		res, err := scoring.Score(nil, criteria)
		if err != nil {
			t.Fatalf("Score: unexpected error: %v", err)
		}
		if res.TotalEarned != 0 || res.TotalAvailable != 100 || res.Percentage != 0 {
			t.Errorf("totals = %v/%v (%v%%), want 0/100 (0%%)", res.TotalEarned, res.TotalAvailable, res.Percentage)
		}
	})
}

func TestScore_EmptyRubric(t *testing.T) {
	t.Parallel()

	for name, criteria := range map[string][]domain.Criterion{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res, err := scoring.Score(borderlessWorkAnswers(), criteria)
			if err != nil {
				t.Fatalf("Score: unexpected error: %v", err)
			}
			if res.TotalEarned != 0 || res.TotalAvailable != 0 || res.Percentage != 0 {
				t.Errorf("Score = %+v, want all totals 0", res)
			}
			if math.IsNaN(res.Percentage) {
				t.Error("Percentage is NaN: the empty rubric was divided by zero")
			}
			if res.Breakdown == nil || len(res.Breakdown) != 0 {
				t.Errorf("Breakdown = %#v, want an empty, non-nil slice", res.Breakdown)
			}
		})
	}

	// Zero weight parks a criterion; a rubric of only parked criteria has
	// nothing available, and must not divide by it either.
	t.Run("all weights zero", func(t *testing.T) {
		t.Parallel()
		c := domain.Criterion{
			ID: "c1", Kind: domain.CriterionKindKeywordMatch, Weight: 0, QuestionID: "q1",
			Config: mustConfig(t, map[string]any{"keywords": []string{"go"}}),
		}
		res, err := scoring.Score([]domain.Answer{answer("q1", "go")}, []domain.Criterion{c})
		if err != nil {
			t.Fatalf("Score: unexpected error: %v", err)
		}
		if res.Percentage != 0 || math.IsNaN(res.Percentage) {
			t.Errorf("Percentage = %v, want 0", res.Percentage)
		}
	})
}

func TestScore_UnknownKindReturnsError(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"astrology", "", "Keyword_Match"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			// A valid rubric with one bad criterion in the middle: nothing is
			// scored, not even the valid criteria.
			criteria := borderlessWorkRubric(t)
			criteria[2].Kind = kind

			res, err := scoring.Score(borderlessWorkAnswers(), criteria)
			wantCriterionError(t, err, "obs", scoring.ErrUnknownKind)
			if !reflect.DeepEqual(res, scoring.Result{}) {
				t.Errorf("Result = %+v, want the zero Result alongside the error", res)
			}
		})
	}
}

func TestScore_EvidenceIsSet(t *testing.T) {
	t.Parallel()

	res, err := scoring.Score(borderlessWorkAnswers(), borderlessWorkRubric(t))
	if err != nil {
		t.Fatalf("Score: unexpected error: %v", err)
	}

	// Each single-answer criterion points at the answer it read; boolean_all
	// reads three, so it points at none.
	want := map[string]string{
		"go":     "a-q-go",
		"db":     "a-q-db",
		"obs":    "a-q-obs",
		"depth":  "a-q-project",
		"years":  "a-q-years",
		"remote": "",
	}
	for _, got := range res.Breakdown {
		if got.EvidenceAnswerID != want[got.CriterionID] {
			t.Errorf("%s: EvidenceAnswerID = %q, want %q", got.CriterionID, got.EvidenceAnswerID, want[got.CriterionID])
		}
	}

	// Evidence is set even when the answer earned nothing: a reviewer must be
	// able to see what the candidate wrote.
	t.Run("zero score still has evidence", func(t *testing.T) {
		t.Parallel()
		c := domain.Criterion{
			ID: "c1", Kind: domain.CriterionKindKeywordMatch, Weight: 10, QuestionID: "q1",
			Config: mustConfig(t, map[string]any{"keywords": []string{"go"}}),
		}
		got := scoreOne(t, c, answer("q1", "I write Python"))
		if got.Earned != 0 || got.EvidenceAnswerID != "a-q1" {
			t.Errorf("Earned = %v, EvidenceAnswerID = %q; want 0 and a-q1", got.Earned, got.EvidenceAnswerID)
		}
	})
}

func TestScore_PercentageMath(t *testing.T) {
	t.Parallel()

	// keyword criterion of weight w reading question q, with a single keyword
	// "go", so it earns all of w or none of it.
	keyword := func(id, q string, w float64) domain.Criterion {
		return domain.Criterion{
			ID: id, Kind: domain.CriterionKindKeywordMatch, Weight: w, QuestionID: q,
			Config: mustConfig(t, map[string]any{"keywords": []string{"go"}}),
		}
	}

	tests := []struct {
		name           string
		criteria       []domain.Criterion
		answers        []domain.Answer
		wantEarned     float64
		wantAvailable  float64
		wantPercentage float64
	}{
		{
			name:           "full marks",
			criteria:       []domain.Criterion{keyword("a", "q1", 60), keyword("b", "q2", 40)},
			answers:        []domain.Answer{answer("q1", "go"), answer("q2", "go")},
			wantEarned:     100,
			wantAvailable:  100,
			wantPercentage: 100,
		},
		{
			name:           "nothing earned",
			criteria:       []domain.Criterion{keyword("a", "q1", 60), keyword("b", "q2", 40)},
			answers:        []domain.Answer{answer("q1", "rust"), answer("q2", "rust")},
			wantEarned:     0,
			wantAvailable:  100,
			wantPercentage: 0,
		},
		{
			// The percentage is of what was available, not of 100: a rubric
			// may be scored before it has been validated to sum to 100.
			name:           "weights not summing to 100",
			criteria:       []domain.Criterion{keyword("a", "q1", 3), keyword("b", "q2", 1)},
			answers:        []domain.Answer{answer("q1", "go"), answer("q2", "rust")},
			wantEarned:     3,
			wantAvailable:  4,
			wantPercentage: 75,
		},
		{
			// 1/3 has no exact float64 form, so this is compared approximately.
			name:           "a third",
			criteria:       []domain.Criterion{keyword("a", "q1", 10), keyword("b", "q2", 10), keyword("c", "q3", 10)},
			answers:        []domain.Answer{answer("q1", "go")},
			wantEarned:     10,
			wantAvailable:  30,
			wantPercentage: 100.0 / 3,
		},
		{
			// A parked criterion adds nothing to either side.
			name:           "zero-weight criterion",
			criteria:       []domain.Criterion{keyword("a", "q1", 50), keyword("b", "q2", 0)},
			answers:        []domain.Answer{answer("q1", "go"), answer("q2", "go")},
			wantEarned:     50,
			wantAvailable:  50,
			wantPercentage: 100,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := scoring.Score(tt.answers, tt.criteria)
			if err != nil {
				t.Fatalf("Score: unexpected error: %v", err)
			}
			if !approxEqual(res.TotalEarned, tt.wantEarned) || !approxEqual(res.TotalAvailable, tt.wantAvailable) {
				t.Errorf("totals = %v/%v, want %v/%v", res.TotalEarned, res.TotalAvailable, tt.wantEarned, tt.wantAvailable)
			}
			if !approxEqual(res.Percentage, tt.wantPercentage) {
				t.Errorf("Percentage = %v, want %v", res.Percentage, tt.wantPercentage)
			}
		})
	}
}

func TestScore_DeterministicAcrossRuns(t *testing.T) {
	t.Parallel()

	criteria := borderlessWorkRubric(t)
	answers := borderlessWorkAnswers()

	first, err := scoring.Score(answers, criteria)
	if err != nil {
		t.Fatalf("Score: unexpected error: %v", err)
	}

	// Go randomises map iteration order on every range, and the engine keeps
	// both the registry and the answers in maps. Many runs give that a chance
	// to leak into the result if anything depended on it.
	for run := range 50 {
		again, runErr := scoring.Score(answers, criteria)
		if runErr != nil {
			t.Fatalf("run %d: unexpected error: %v", run, runErr)
		}
		// DeepEqual, not approxEqual: identical input must give bit-identical
		// output, not merely close output.
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differs:\nfirst: %+v\nagain: %+v", run, first, again)
		}
	}

	// The order answers arrive in must not matter either.
	reversed := slices.Clone(answers)
	slices.Reverse(reversed)
	if got, _ := scoring.Score(reversed, criteria); !reflect.DeepEqual(first, got) {
		t.Errorf("reversed answers differ:\nfirst: %+v\ngot:   %+v", first, got)
	}

	// A compiled rubric reused across applications carries nothing from one
	// to the next.
	r, err := scoring.Compile(criteria)
	if err != nil {
		t.Fatalf("Compile: unexpected error: %v", err)
	}
	r.Score([]domain.Answer{answer("q-go", "nothing relevant")})
	if got := r.Score(answers); !reflect.DeepEqual(first, got) {
		t.Errorf("compiled rubric differs after scoring another application:\nfirst: %+v\ngot:   %+v", first, got)
	}
}

func TestScore_InvalidWeight(t *testing.T) {
	t.Parallel()

	for name, weight := range map[string]float64{
		"negative": -1,
		"NaN":      math.NaN(),
		"infinite": math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := domain.Criterion{
				ID: "c1", Kind: domain.CriterionKindKeywordMatch, Weight: weight, QuestionID: "q1",
				Config: json.RawMessage(`{"keywords":["go"]}`),
			}
			_, err := scoring.Score(nil, []domain.Criterion{c})
			wantCriterionError(t, err, "c1", scoring.ErrInvalidConfig)
		})
	}
}

func TestScore_MissingQuestion(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		domain.CriterionKindKeywordMatch,
		domain.CriterionKindExperienceYears,
		domain.CriterionKindAnswerDepth,
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			criteria := borderlessWorkRubric(t)
			for i := range criteria {
				if criteria[i].Kind == kind {
					criteria[i].QuestionID = ""
					_, err := scoring.Score(borderlessWorkAnswers(), criteria)
					wantCriterionError(t, err, criteria[i].ID, scoring.ErrMissingQuestion)
					return
				}
			}
			t.Fatalf("the rubric has no %s criterion", kind)
		})
	}
}

func TestScore_MalformedConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"not JSON":       `{"keywords":`,
		"wrong type":     `{"keywords":"go"}`,
		"unknown key":    `{"keywords":["go"],"keyword":["rust"]}`,
		"trailing value": `{"keywords":["go"]} {}`,
		"not an object":  `["go"]`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := domain.Criterion{
				ID: "c1", Kind: domain.CriterionKindKeywordMatch, Weight: 10, QuestionID: "q1",
				Config: json.RawMessage(raw),
			}
			_, err := scoring.Score(nil, []domain.Criterion{c})
			wantCriterionError(t, err, "c1", scoring.ErrInvalidConfig)
		})
	}
}

func TestCriterionError_Message(t *testing.T) {
	t.Parallel()

	// The message is what lands in a log when a rubric is refused, so it must
	// name the criterion, its kind and the reason.
	c := domain.Criterion{
		ID: "c1", Kind: domain.CriterionKindExperienceYears, Weight: 10, QuestionID: "q1",
		Config: json.RawMessage(`{"min":0}`),
	}
	_, err := scoring.Score(nil, []domain.Criterion{c})
	if err == nil {
		t.Fatal("Score: want an error for a config with no target")
	}
	if want := "criterion c1 (experience_years): invalid criterion config: target is required"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}
