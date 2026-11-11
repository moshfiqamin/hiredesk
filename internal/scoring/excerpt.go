package scoring

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// ExcerptWidth is about how many characters of an answer an excerpt shows,
// not counting the ellipses.
const ExcerptWidth = 120

// ellipsis marks the side of an excerpt where the answer was cut.
const ellipsis = "..."

// wordSnap is the furthest an excerpt's edge moves to avoid cutting a word in
// half. Past that the word is cut: one very long "word" must not empty the
// excerpt.
const wordSnap = 15

// Excerpt returns the part of answer worth showing beside c's score: about
// ExcerptWidth characters, with "..." on each side where the answer was cut.
//
// For a keyword_match criterion the window is centred on the first place, in
// reading order, that any of its keywords occurs, found by the same
// whole-word rule the scorer uses, so the excerpt shows what actually earned
// the points. For other kinds, or when no keyword occurs, it is the start of
// the answer.
//
// Runs of whitespace, newlines included, become one space first. Width is
// counted in characters (runes), never bytes, so a cut never splits a
// multi-byte character.
func Excerpt(c domain.Criterion, answer string) string {
	text := strings.Join(strings.Fields(answer), " ")
	runes := []rune(text)
	if len(runes) <= ExcerptWidth {
		return text
	}

	// The match in rune offsets. With no match both stay 0, which anchors
	// the window at the start of the answer.
	var matchStart, matchEnd int
	if start, end, ok := firstKeywordMatch(c, text); ok {
		// FindStringSubmatchIndex reports byte offsets; the window is cut
		// from runes, so each is converted by counting the runes before it.
		matchStart = utf8.RuneCountInString(text[:start])
		matchEnd = matchStart + utf8.RuneCountInString(text[start:end])
	}

	// Centre the match, then slide the window back inside the text if
	// centring pushed either edge past an end.
	start := max(0, matchStart-(ExcerptWidth-(matchEnd-matchStart))/2)
	end := min(len(runes), start+ExcerptWidth)
	start = max(0, end-ExcerptWidth)

	// Move each cut edge to the nearest space, but never across the match.
	if hi := min(start+wordSnap, matchStart); start > 0 && hi > start {
		if i := slices.Index(runes[start:hi], ' '); i >= 0 {
			start += i + 1
		}
	}
	if lo := max(end-wordSnap, matchEnd); end < len(runes) && lo < end {
		if i := lastIndexRune(runes[lo:end], ' '); i >= 0 {
			end = lo + i
		}
	}

	out := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		out = ellipsis + out
	}
	if end < len(runes) {
		out += ellipsis
	}
	return out
}

// firstKeywordMatch returns the byte offsets of the earliest keyword of a
// keyword_match criterion in text. ok is false for any other kind, for a
// config that does not decode, and when no keyword occurs.
func firstKeywordMatch(c domain.Criterion, text string) (start, end int, ok bool) {
	if c.Kind != domain.CriterionKindKeywordMatch {
		return 0, 0, false
	}
	var cfg keywordConfig
	if decodeConfig(c.Config, &cfg) != nil {
		return 0, 0, false
	}

	start = -1
	for _, kw := range cfg.Keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		re, err := compileKeyword(kw)
		if err != nil {
			continue
		}
		// Indexes 2 and 3 are capture group 1: the keyword without the
		// edge characters around it.
		loc := re.FindStringSubmatchIndex(text)
		if loc != nil && (start < 0 || loc[2] < start) {
			start, end = loc[2], loc[3]
		}
	}
	return start, end, start >= 0
}

// lastIndexRune returns the index of the last r in s, or -1. The slices
// package has Index but no LastIndex.
func lastIndexRune(s []rune, r rune) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == r {
			return i
		}
	}
	return -1
}
