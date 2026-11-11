package scoring

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// booleanAllConfig is the config of a boolean_all criterion:
// {"question_ids": ["…", "…"]}.
type booleanAllConfig struct {
	QuestionIDs []string `json:"question_ids"`
}

// newBooleanAll compiles a boolean_all criterion: the full weight when every
// listed question was answered true, nothing otherwise.
//
// Unlike the other kinds it reads several answers, so it implements scorer
// directly instead of going through singleAnswer.
func newBooleanAll(c domain.Criterion) (scorer, error) {
	var cfg booleanAllConfig
	if err := decodeConfig(c.Config, &cfg); err != nil {
		return nil, err
	}
	// The questions come from the config. A question_id as well would be a
	// second, ignored source of truth.
	if c.QuestionID != "" {
		return nil, invalidConfig("boolean_all reads config.question_ids; question_id must be empty")
	}
	// "Every one of no conditions is met" is vacuously true and would hand
	// every candidate the full weight.
	if len(cfg.QuestionIDs) == 0 {
		return nil, invalidConfig("question_ids must list at least one question")
	}

	ids := make([]string, 0, len(cfg.QuestionIDs))
	seen := make(map[string]bool, len(cfg.QuestionIDs))
	for _, id := range cfg.QuestionIDs {
		if id == "" {
			return nil, invalidConfig("question_ids must not contain an empty ID")
		}
		if seen[id] {
			return nil, invalidConfig("question " + strconv.Quote(id) + " is listed twice")
		}
		seen[id] = true
		ids = append(ids, id)
	}

	return func(answers answerIndex) outcome {
		met, unanswered := 0, 0
		for _, id := range ids {
			a, ok := answers[id]
			if !ok {
				unanswered++
				continue
			}
			// Anything ParseBool rejects counts as not met, never as an error.
			if v, err := strconv.ParseBool(strings.TrimSpace(a.Text)); err == nil && v {
				met++
			}
		}

		detail := fmt.Sprintf("%d of %d %s met", met, len(ids), plural(len(ids), "condition", "conditions"))
		if unanswered > 0 {
			detail += fmt.Sprintf(" (%d unanswered)", unanswered)
		}

		// All or nothing, so partial agreement earns 0.
		var fraction float64
		if met == len(ids) {
			fraction = 1
		}
		// No evidence answer: the score rests on several.
		return outcome{fraction: fraction, detail: detail}
	}, nil
}
