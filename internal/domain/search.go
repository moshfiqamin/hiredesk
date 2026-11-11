package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxSearchDocumentChars is the longest search document stored, in
// characters. The tsvector built from it must stay under PostgreSQL's 1 MB
// limit, and a document of nothing but distinct short words produces a
// tsvector about twice its own length.
//
// Migration 000012 and store.RebuildSearchDocuments cut at the same length,
// with left(), which also counts characters.
const MaxSearchDocumentChars = 100_000

// The page size of a search, by default and at most.
const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 100
)

// MaxSearchOffset bounds how deep a search may page. Each page re-ranks every
// match before the offset, so an unbounded offset is unbounded work.
const MaxSearchOffset = 10_000

// MaxSearchQueryChars is the longest search query accepted, in characters.
// Nobody types more into a search box; anything longer is a mistake or an
// attempt to make the database work.
const MaxSearchQueryChars = 200

// SearchDocument returns the searchable text of an application: its answers
// to the text and choice questions of form, in form order, one per line, cut
// to MaxSearchDocumentChars characters.
//
// form must be in display order. answers should be the answers as validated,
// so each is in canonical form and empty answers are already gone.
//
// Number and boolean answers are left out: "4" and "true" are not words
// anyone searches for, and every candidate who answered yes would match a
// search for "true".
//
// store.RebuildSearchDocuments computes the same text in SQL. If the two ever
// disagree, the repair command will rewrite documents that submission built,
// which TestRebuildSearchDocumentsAgreesWithSubmit catches.
func SearchDocument(form []Question, answers []NewAnswer) string {
	byQuestion := make(map[string]string, len(answers))
	for _, a := range answers {
		byQuestion[a.QuestionID] = a.Answer
	}

	parts := make([]string, 0, len(form))
	for _, q := range form {
		if q.Kind != QuestionKindText && q.Kind != QuestionKindChoice {
			continue
		}
		// A question nobody answered contributes nothing, not an empty line:
		// string_agg in the SQL version skips it the same way, because it has
		// no application_answers row.
		if text, ok := byQuestion[q.ID]; ok {
			parts = append(parts, text)
		}
	}
	return truncateChars(strings.Join(parts, "\n"), MaxSearchDocumentChars)
}

// truncateChars returns s cut to at most n characters, never splitting a
// multi-byte character.
func truncateChars(s string, n int) string {
	// Fast path: a string of at most n bytes has at most n characters.
	if len(s) <= n {
		return s
	}
	// Ranging over a string steps one character (rune) at a time, and i is the
	// byte offset where that character starts. The n-th one starts exactly
	// where the first n characters end.
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// NormalizeSearchQuery trims q and reports what is wrong with it, if
// anything, as a field message for the q parameter. An empty message means q
// can be sent to the database.
//
// It refuses only what PostgreSQL itself would turn into an error: nothing to
// search for, invalid UTF-8, a NUL byte, and absurd length. Punctuation,
// operators and stop words are not refused here; plainto_tsquery reduces them
// to no lexemes, and a query with no lexemes simply matches nothing.
func NormalizeSearchQuery(q string) (string, string) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return "", "is required: give one or more words to search for"
	case !utf8.ValidString(q):
		// The database encoding is UTF-8 and rejects anything else with
		// SQLSTATE 22021, which would surface as a 500.
		return "", "must be valid UTF-8 text"
	case strings.ContainsRune(q, 0):
		// PostgreSQL text cannot hold a NUL byte at all.
		return "", "must not contain a NUL character"
	case utf8.RuneCountInString(q) > MaxSearchQueryChars:
		return "", "must be at most " + strconv.Itoa(MaxSearchQueryChars) + " characters"
	}
	return q, ""
}

// SearchResult is one application matching a search, with how well it
// matched and its score, so a recruiter can weigh relevance against merit.
type SearchResult struct {
	ApplicationID string
	CandidateID   string
	CandidateName string
	Status        string
	SubmittedAt   time.Time

	// Relevance is PostgreSQL's ts_rank of the application against the
	// query. It is only comparable between results of the same query.
	Relevance float64

	// Totals is nil for an application with no stored score.
	Totals *ScoreTotals
}
