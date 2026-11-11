package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSearchDocument(t *testing.T) {
	t.Parallel()

	// In display order, as listQuestions returns them.
	form := []Question{
		{ID: "why", Kind: QuestionKindText, Position: 1},
		{ID: "years", Kind: QuestionKindNumber, Position: 2},
		{ID: "remote", Kind: QuestionKindBoolean, Position: 3},
		{ID: "stack", Kind: QuestionKindChoice, Position: 4},
		{ID: "skipped", Kind: QuestionKindText, Position: 5},
		{ID: "extra", Kind: QuestionKindText, Position: 6},
	}

	// Submitted out of form order, with one optional question unanswered.
	answers := []NewAnswer{
		{QuestionID: "extra", Answer: "I mentor juniors."},
		{QuestionID: "stack", Answer: "Go"},
		{QuestionID: "remote", Answer: "true"},
		{QuestionID: "years", Answer: "4"},
		{QuestionID: "why", Answer: "I built a REST API in Go."},
	}

	got := SearchDocument(form, answers)
	want := "I built a REST API in Go.\nGo\nI mentor juniors."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if got := SearchDocument(form, nil); got != "" {
		t.Errorf("no answers: got %q, want empty", got)
	}
}

func TestSearchDocumentIsCutToTheLimit(t *testing.T) {
	t.Parallel()

	form := []Question{{ID: "why", Kind: QuestionKindText, Position: 1}}
	// Three bytes per character, so a byte-based cut would split one.
	long := strings.Repeat("語", MaxSearchDocumentChars+10)

	got := SearchDocument(form, []NewAnswer{{QuestionID: "why", Answer: long}})
	if n := utf8.RuneCountInString(got); n != MaxSearchDocumentChars {
		t.Errorf("got %d characters, want %d", n, MaxSearchDocumentChars)
	}
	if !utf8.ValidString(got) {
		t.Error("the cut split a multi-byte character")
	}
}

func TestTruncateChars(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   string
		n    int
		want string
	}{
		{"", 3, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"héllo", 2, "hé"},
		{"日本語", 5, "日本語"},
		{"日本語", 1, "日"},
	} {
		if got := truncateChars(tt.in, tt.n); got != tt.want {
			t.Errorf("truncateChars(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestNormalizeSearchQuery(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, in, want string
		refused        bool
	}{
		{"plain", "go postgres", "go postgres", false},
		{"trimmed", "  go  ", "go", false},
		{"operators are just text", "go & postgres", "go & postgres", false},
		{"SQL is just text", "'; DROP TABLE users; --", "'; DROP TABLE users; --", false},
		{"punctuation is just text", "!!!???", "!!!???", false},
		{"unicode", "résumé 日本語", "résumé 日本語", false},
		{"empty", "", "", true},
		{"whitespace", " \t\n", "", true},
		{"invalid UTF-8", "go\xff", "", true},
		{"NUL", "go\x00", "", true},
		{"at the limit", strings.Repeat("a", MaxSearchQueryChars), strings.Repeat("a", MaxSearchQueryChars), false},
		{"over the limit", strings.Repeat("a", MaxSearchQueryChars+1), "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, problem := NormalizeSearchQuery(tt.in)
			if (problem != "") != tt.refused {
				t.Fatalf("problem = %q, refused = %v", problem, tt.refused)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
