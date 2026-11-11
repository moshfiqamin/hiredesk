package domain

import (
	"strings"
	"testing"
)

func TestCheckMark(t *testing.T) {
	t.Parallel()

	for mark := -1; mark <= 12; mark++ {
		valid := mark >= MinMark && mark <= MaxMark
		if got := CheckMark(mark); (got == "") != valid {
			t.Errorf("CheckMark(%d) = %q, valid %v", mark, got, valid)
		}
	}
}

func TestNormalizeReviewNotes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, in string
		want     *string
		refused  bool
	}{
		{"plain", "Strong answers.", ptr("Strong answers."), false},
		{"trimmed", "  Strong.  \n", ptr("Strong."), false},
		{"blank is none", "   ", nil, false},
		{"empty is none", "", nil, false},
		{"NUL", "bad\x00notes", nil, true},
		{"at the limit, multi-byte", strings.Repeat("ব", MaxReviewNotesChars), ptr(strings.Repeat("ব", MaxReviewNotesChars)), false},
		{"over the limit", strings.Repeat("a", MaxReviewNotesChars+1), nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, problem := NormalizeReviewNotes(tt.in)
			if (problem != "") != tt.refused {
				t.Fatalf("problem = %q", problem)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
