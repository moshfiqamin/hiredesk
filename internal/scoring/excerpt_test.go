package scoring_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// excerptCriterion is a keyword_match criterion with the given keywords.
func excerptCriterion(keywords ...string) domain.Criterion {
	cfg, _ := json.Marshal(map[string][]string{"keywords": keywords})
	return domain.Criterion{Kind: domain.CriterionKindKeywordMatch, Config: cfg, QuestionID: "q"}
}

// filler is n characters of words with no keyword in them.
func filler(n int) string {
	return strings.Repeat("lorem ipsum ", n/12+1)[:n]
}

func TestExcerpt(t *testing.T) {
	t.Parallel()

	maxLen := scoring.ExcerptWidth + 2*len("...")

	tests := []struct {
		name      string
		criterion domain.Criterion
		answer    string

		want       string // exact, when set
		contains   string
		prefix     bool // starts with "..."
		suffix     bool // ends with "..."
		notContain string
	}{
		{
			name:      "a short answer is returned whole",
			criterion: excerptCriterion("go"),
			answer:    "I write Go.",
			want:      "I write Go.",
		},
		{
			name:      "whitespace and newlines are collapsed",
			criterion: excerptCriterion("go"),
			answer:    "  I write\n\n  Go\tdaily.  ",
			want:      "I write Go daily.",
		},
		{
			name:      "a keyword in the middle is centred, cut on both sides",
			criterion: excerptCriterion("goroutine"),
			answer:    filler(400) + " each goroutine owns its channel " + filler(400),
			contains:  "each goroutine owns its channel",
			prefix:    true, suffix: true,
		},
		{
			name:      "a keyword near the start is not cut before",
			criterion: excerptCriterion("go"),
			answer:    "Go is what I use. " + filler(400),
			contains:  "Go is what I use.",
			suffix:    true,
		},
		{
			name:      "a keyword at the very end is not cut after",
			criterion: excerptCriterion("postgres"),
			answer:    filler(400) + " and finally postgres",
			contains:  "and finally postgres",
			prefix:    true,
		},
		{
			// The scorer's rule, not strings.Contains: "go" inside "google"
			// earned nothing, so it must not anchor the excerpt.
			name:      "a keyword inside a longer word is not a match",
			criterion: excerptCriterion("go"),
			answer:    "I worked at Google " + filler(300) + " then wrote Go services " + filler(300),
			contains:  "then wrote Go services",
			prefix:    true, suffix: true,
			notContain: "Google",
		},
		{
			name:      "the earliest of several keywords wins, whatever the config order",
			criterion: excerptCriterion("kubernetes", "sql"),
			answer:    filler(300) + " tuned SQL indexes " + filler(300) + " ran kubernetes " + filler(300),
			contains:  "tuned SQL indexes",
			prefix:    true, suffix: true,
			notContain: "kubernetes",
		},
		{
			name:      "no keyword at all gives the start of the answer",
			criterion: excerptCriterion("rust"),
			answer:    "First sentence here. " + filler(400),
			contains:  "First sentence here.",
			suffix:    true,
		},
		{
			name:      "other kinds give the start of the answer",
			criterion: domain.Criterion{Kind: domain.CriterionKindAnswerDepth},
			answer:    "Opening line. " + filler(400),
			contains:  "Opening line.",
			suffix:    true,
		},
		{
			name:      "an unusable config falls back to the start",
			criterion: domain.Criterion{Kind: domain.CriterionKindKeywordMatch, Config: json.RawMessage(`{"keywords": "go"}`)},
			answer:    "Opening line. " + filler(400),
			contains:  "Opening line.",
			suffix:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := scoring.Excerpt(tt.criterion, tt.answer)

			if tt.want != "" {
				if got != tt.want {
					t.Errorf("Excerpt = %q, want %q", got, tt.want)
				}
				return
			}
			if n := utf8.RuneCountInString(got); n > maxLen {
				t.Errorf("Excerpt is %d characters, want at most %d: %q", n, maxLen, got)
			}
			if !strings.Contains(got, tt.contains) {
				t.Errorf("Excerpt = %q, want it to contain %q", got, tt.contains)
			}
			if tt.notContain != "" && strings.Contains(got, tt.notContain) {
				t.Errorf("Excerpt = %q, must not contain %q", got, tt.notContain)
			}
			if strings.HasPrefix(got, "...") != tt.prefix {
				t.Errorf("Excerpt = %q, leading ellipsis = %v, want %v", got, !tt.prefix, tt.prefix)
			}
			if strings.HasSuffix(got, "...") != tt.suffix {
				t.Errorf("Excerpt = %q, trailing ellipsis = %v, want %v", got, !tt.suffix, tt.suffix)
			}
		})
	}
}

// Words are kept whole at a cut edge: neither edge of the excerpt, inside the
// ellipses, is part of a longer word in the answer.
func TestExcerptCutsBetweenWords(t *testing.T) {
	t.Parallel()

	answer := strings.Repeat("alpha beta gamma delta ", 30) + "uses Go daily " + strings.Repeat("epsilon zeta eta theta ", 30)
	got := scoring.Excerpt(excerptCriterion("go"), answer)

	inner := strings.TrimSuffix(strings.TrimPrefix(got, "..."), "...")
	for _, w := range strings.Fields(inner) {
		if !strings.Contains(" "+answer+" ", " "+w+" ") {
			t.Errorf("excerpt %q contains %q, which is not a whole word of the answer", got, w)
		}
	}
}

// Counting is in characters, so an answer in a script whose characters take
// three bytes each is neither cut short nor split inside a character.
func TestExcerptCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()

	word := "আমি " // Bengali: three runes of three bytes, and a space
	answer := strings.Repeat(word, 30) + "golang " + strings.Repeat(word, 30)

	got := scoring.Excerpt(excerptCriterion("golang"), answer)

	if !utf8.ValidString(got) {
		t.Fatalf("Excerpt split a character: %q", got)
	}
	if !strings.Contains(got, "golang") {
		t.Errorf("Excerpt = %q, want it to contain the keyword", got)
	}
	// A byte-counted window of 120 would hold only about 30 of these
	// characters; a rune-counted one holds about 120.
	if n := utf8.RuneCountInString(got); n < scoring.ExcerptWidth-20 {
		t.Errorf("Excerpt is %d characters, want about %d: counted in bytes?", n, scoring.ExcerptWidth)
	}
}
