package scoring_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// keywordCriterion builds a keyword_match criterion reading question q1.
func keywordCriterion(t *testing.T, weight float64, keywords ...string) domain.Criterion {
	t.Helper()
	return domain.Criterion{
		ID:         "c1",
		Label:      "Keywords",
		Kind:       domain.CriterionKindKeywordMatch,
		Weight:     weight,
		QuestionID: "q1",
		Config:     mustConfig(t, map[string]any{"keywords": keywords}),
	}
}

// keywordCase is one answer and whether the criterion's keywords should
// earn the full weight from it.
type keywordCase struct {
	name  string
	text  string
	match bool
}

// runKeywordCases scores each case against a single-keyword criterion of
// weight 1, so Earned is 1 for a match and 0 otherwise.
func runKeywordCases(t *testing.T, keyword string, tests []keywordCase) {
	t.Helper()
	c := keywordCriterion(t, 1, keyword)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", tt.text))
			want := 0.0
			if tt.match {
				want = 1
			}
			if got.Earned != want {
				t.Errorf("keyword %q in %q: Earned = %v, want %v (%s)", keyword, tt.text, got.Earned, want, got.Detail)
			}
		})
	}
}

func TestKeywordMatch_WholeWordOnly(t *testing.T) {
	t.Parallel()

	runKeywordCases(t, "go", []keywordCase{
		// Substrings that strings.Contains would wrongly accept.
		{"inside google", "I search with google", false},
		{"end of ago", "two years ago", false},
		{"start of going", "it is going well", false},
		{"start of golang", "I write golang", false},
		{"inside cargo", "shipped with cargo", false},
		{"before non-ASCII letter", "goä", false},
		{"after non-ASCII letter", "ägo", false},

		// The word itself, wherever it sits.
		{"alone", "go", true},
		{"start", "go is great", true},
		{"end", "I write go", true},
		{"before full stop", "I write go.", true},
		{"before comma", "go, rust", true},
		{"in parentheses", "backend (go) services", true},
		{"after newline", "rust\ngo", true},
		{"after tab", "rust\tgo", true},
		{"hyphenated", "a go-based service", true},
	})

	// \b in RE2 is ASCII-only and would split "café" after "caf".
	runKeywordCases(t, "caf", []keywordCase{
		{"inside café", "le café", false},
	})
}

func TestKeywordMatch_CaseInsensitive(t *testing.T) {
	t.Parallel()

	runKeywordCases(t, "golang", []keywordCase{
		{"title case", "I write Golang", true},
		{"upper case", "GOLANG all day", true},
		{"lower case", "golang", true},
		{"mixed case", "gOlAnG", true},
	})

	// The keyword's own case does not matter either.
	runKeywordCases(t, "PostgreSQL", []keywordCase{
		{"lower-case answer", "we run postgresql", true},
	})
}

func TestKeywordMatch_PartialCredit(t *testing.T) {
	t.Parallel()

	keywords := []string{"go", "sql", "docker", "kubernetes", "grpc"}
	tests := []struct {
		name       string
		text       string
		wantEarned float64
		wantDetail string
	}{
		{
			name:       "three of five",
			text:       "Go services on SQL, shipped with Docker",
			wantEarned: 0.6 * 10,
			wantDetail: "matched 3 of 5 keywords: go, sql, docker",
		},
		{
			name:       "all five",
			text:       "go sql docker kubernetes grpc",
			wantEarned: 10,
			wantDetail: "matched 5 of 5 keywords: go, sql, docker, kubernetes, grpc",
		},
		{
			name:       "none",
			text:       "I write Python",
			wantEarned: 0,
			wantDetail: "matched 0 of 5 keywords",
		},
		{
			name:       "repeated keyword counts once",
			text:       "go go go",
			wantEarned: 0.2 * 10,
			wantDetail: "matched 1 of 5 keywords: go",
		},
	}

	c := keywordCriterion(t, 10, keywords...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", tt.text))
			if !approxEqual(got.Earned, tt.wantEarned) || got.Available != 10 {
				t.Errorf("Earned/Available = %v/%v, want %v/10", got.Earned, got.Available, tt.wantEarned)
			}
			if got.Detail != tt.wantDetail {
				t.Errorf("Detail = %q, want %q", got.Detail, tt.wantDetail)
			}
			if got.EvidenceAnswerID != "a-q1" {
				t.Errorf("EvidenceAnswerID = %q, want a-q1", got.EvidenceAnswerID)
			}
		})
	}
}

func TestKeywordMatch_EmptyAnswer(t *testing.T) {
	t.Parallel()

	c := keywordCriterion(t, 10, "go", "sql", "docker")
	for _, text := range []string{"", "   ", "\n\t"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			got := scoreOne(t, c, answer("q1", text))
			if got.Earned != 0 || got.Available != 10 {
				t.Errorf("Earned/Available = %v/%v, want 0/10", got.Earned, got.Available)
			}
			// An empty answer is still an answer, unlike a missing one.
			if got.Detail != "matched 0 of 3 keywords" {
				t.Errorf("Detail = %q, want %q", got.Detail, "matched 0 of 3 keywords")
			}
		})
	}
}

func TestKeywordMatch_EmptyKeywordList(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty list":     `{"keywords":[]}`,
		"absent list":    `{}`,
		"null list":      `{"keywords":null}`,
		"empty config":   ``,
		"blank keyword":  `{"keywords":["go","  "]}`,
		"duplicate word": `{"keywords":["go","Go"]}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := domain.Criterion{
				ID: "c1", Kind: domain.CriterionKindKeywordMatch, Weight: 10, QuestionID: "q1",
				Config: json.RawMessage(raw),
			}
			_, err := scoring.Score([]domain.Answer{answer("q1", "go")}, []domain.Criterion{c})
			wantCriterionError(t, err, "c1", scoring.ErrInvalidConfig)
		})
	}
}

func TestKeywordMatch_RegexpSpecialChars(t *testing.T) {
	t.Parallel()

	// "+" ends the keyword, so \b could never match after it.
	runKeywordCases(t, "c++", []keywordCase{
		{"before space", "I write C++ and Go", true},
		{"at end", "mostly c++", true},
		{"before comma", "c++, rust", true},
		{"plain c", "I write c and go", false},
		{"c+", "c+ is not a language", false},
		{"longer word", "c++11", false},
	})

	runKeywordCases(t, "c#", []keywordCase{
		{"present", "C# and .NET", true},
		{"plain c", "c only", false},
	})

	// Starts with a non-word character.
	runKeywordCases(t, ".net", []keywordCase{
		{"present", "C# and .NET", true},
		{"no dot", "a net", false},
	})

	// Unquoted, "." would match any character.
	runKeywordCases(t, "node.js", []keywordCase{
		{"present", "Node.js backend", true},
		{"dot as wildcard", "nodexjs backend", false},
	})

	// Unquoted, these would not even compile, or would mean something else.
	runKeywordCases(t, "f(x", []keywordCase{{"unbalanced paren", "where f(x = 1", true}})
	runKeywordCases(t, "a|b", []keywordCase{
		{"literal pipe", "a|b", true},
		{"alternation", "just a", false},
	})
	runKeywordCases(t, "[go]", []keywordCase{
		{"literal brackets", "tag [go]", true},
		{"character class", "o", false},
	})
}

// benchmarkRubric and benchmarkAnswers model a realistic application: a
// paragraph-long answer and a ten-keyword criterion.
var (
	benchmarkRubric = []domain.Criterion{{
		ID: "c1", Kind: domain.CriterionKindKeywordMatch, Weight: 10, QuestionID: "q1",
		Config: json.RawMessage(`{"keywords":["go","golang","goroutine","gin","sql","postgres","docker","kubernetes","grpc","c++"]}`),
	}}
	benchmarkAnswers = []domain.Answer{answer("q1",
		"I have written Go professionally for four years, mostly HTTP services on Gin "+
			"backed by Postgres, deployed with Docker to Kubernetes. Before that, C++.")}
)

// BenchmarkScore compiles the rubric on every call, as Score does.
func BenchmarkScore(b *testing.B) {
	for b.Loop() {
		if _, err := scoring.Score(benchmarkAnswers, benchmarkRubric); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRubricScore compiles once and only scores in the loop.
func BenchmarkRubricScore(b *testing.B) {
	r, err := scoring.Compile(benchmarkRubric)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		r.Score(benchmarkAnswers)
	}
}

// FuzzKeywordMatch checks properties that must hold for any keyword and any
// answer, not only the ones a person thought to write down:
//
//   - scoring never panics, and an answer never causes an error;
//   - a single-keyword criterion earns all of its weight or none of it;
//   - the keyword surrounded by spaces always matches;
//   - a keyword made only of word characters never matches inside a longer
//     word ("z" + keyword + "z"), which is "go" versus "google" generalised.
//
// Plain go test runs only the seed corpus below. go test -fuzz runs it with
// generated inputs; see docs/notes/06-2.
func FuzzKeywordMatch(f *testing.F) {
	seeds := []struct{ keyword, text string }{
		{"go", "I search with google"},
		{"go", "I write Go."},
		{"c++", "I write C++ and Go"},
		{"caf", "le café"},
		{"node.js", "nodexjs"},
		{"আমি", "আমি গো লিখি"},
		{"", "anything"},
		{"  ", ""},
		{"\xff", "\xff"}, // invalid UTF-8
	}
	for _, s := range seeds {
		f.Add(s.keyword, s.text)
	}

	f.Fuzz(func(t *testing.T, keyword, text string) {
		// The config goes through JSON exactly as it would from the database.
		// json.Marshal replaces invalid UTF-8 with U+FFFD, so the keyword the
		// scorer sees is the round-tripped one.
		raw, err := json.Marshal(map[string][]string{"keywords": {keyword}})
		if err != nil {
			t.Skip("not encodable as a config")
		}
		var decoded struct{ Keywords []string }
		if decodeErr := json.Unmarshal(raw, &decoded); decodeErr != nil {
			t.Fatalf("round-tripping the config: %v", decodeErr)
		}
		kw := strings.TrimSpace(decoded.Keywords[0])

		c := domain.Criterion{
			ID: "c1", Kind: domain.CriterionKindKeywordMatch, Weight: 10, QuestionID: "q1", Config: raw,
		}
		score := func(text string) (scoring.CriterionScore, error) {
			res, scoreErr := scoring.Score([]domain.Answer{answer("q1", text)}, []domain.Criterion{c})
			if scoreErr != nil {
				return scoring.CriterionScore{}, scoreErr
			}
			return res.Breakdown[0], nil
		}

		got, err := score(text)
		if kw == "" {
			// The only keyword the config can reject is a blank one.
			if !errors.Is(err, scoring.ErrInvalidConfig) {
				t.Fatalf("blank keyword %q: error = %v, want ErrInvalidConfig", keyword, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("keyword %q: unexpected error: %v", kw, err)
		}
		if got.Earned != 0 && got.Earned != 10 {
			t.Fatalf("keyword %q in %q: Earned = %v, want 0 or 10", kw, text, got.Earned)
		}

		if spaced, _ := score(" " + kw + " "); spaced.Earned != 10 {
			t.Fatalf("keyword %q did not match itself between spaces (%s)", kw, spaced.Detail)
		}

		if strings.IndexFunc(kw, isNotWordRune) == -1 {
			if inside, _ := score("z" + kw + "z"); inside.Earned != 0 {
				t.Fatalf("keyword %q matched inside the word %q", kw, "z"+kw+"z")
			}
		}
	})
}

// isNotWordRune is the complement of the matcher's wordChars class.
func isNotWordRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_'
}
