package scoring

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// keywordConfig is the config of a keyword_match criterion:
// {"keywords": ["go", "golang", "goroutine"]}.
type keywordConfig struct {
	Keywords []string `json:"keywords"`
}

// keywordPattern is one keyword and the regexp that finds it as a whole word.
type keywordPattern struct {
	keyword string
	re      *regexp.Regexp
}

// wordChars is what counts as part of a word: any letter or digit in any
// script, and underscore. RE2's \b knows only ASCII letters, so it would
// split "café" after "caf".
const wordChars = `\p{L}\p{N}_`

// newKeywordMatch compiles a keyword_match criterion. The score is the share
// of keywords that appear in the answer as whole words, case-insensitively.
func newKeywordMatch(c domain.Criterion) (scorer, error) {
	var cfg keywordConfig
	if err := decodeConfig(c.Config, &cfg); err != nil {
		return nil, err
	}
	// With no keywords the fraction would be 0/0. That is a broken rubric,
	// not a candidate who scored nothing.
	if len(cfg.Keywords) == 0 {
		return nil, invalidConfig("keywords must list at least one keyword")
	}

	patterns := make([]keywordPattern, 0, len(cfg.Keywords))
	seen := make(map[string]bool, len(cfg.Keywords))
	for _, kw := range cfg.Keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			return nil, invalidConfig("keywords must not contain an empty keyword")
		}
		// A repeat would count twice and skew the fraction.
		key := strings.ToLower(kw)
		if seen[key] {
			return nil, invalidConfig("keyword " + strconv.Quote(kw) + " is listed twice")
		}
		seen[key] = true

		re, err := compileKeyword(kw)
		if err != nil {
			return nil, invalidConfig("keyword " + strconv.Quote(kw) + ": " + err.Error())
		}
		patterns = append(patterns, keywordPattern{keyword: kw, re: re})
	}

	return singleAnswer(c.QuestionID, func(a domain.Answer) outcome {
		return matchKeywords(patterns, a.Text)
	}), nil
}

// compileKeyword builds a case-insensitive pattern matching kw as a whole
// word. The edges are "start of text or a non-word character" and "a
// non-word character or end of text", rather than \b, because \b needs a word
// character on one side: it can never match after the last "+" of "c++".
// RE2 has no lookaround, so the edge characters are part of the match. The
// keyword itself is capture group 1, which is where Excerpt finds it.
func compileKeyword(kw string) (*regexp.Regexp, error) {
	return regexp.Compile(`(?i)(?:^|[^` + wordChars + `])(` +
		regexp.QuoteMeta(kw) +
		`)(?:[^` + wordChars + `]|$)`)
}

// matchKeywords reports which keywords appear in text, in config order.
func matchKeywords(patterns []keywordPattern, text string) outcome {
	matched := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if p.re.MatchString(text) {
			matched = append(matched, p.keyword)
		}
	}

	detail := fmt.Sprintf("matched %d of %d %s",
		len(matched), len(patterns), plural(len(patterns), "keyword", "keywords"))
	if len(matched) > 0 {
		detail += ": " + strings.Join(matched, ", ")
	}

	return outcome{
		fraction: float64(len(matched)) / float64(len(patterns)),
		detail:   detail,
	}
}
