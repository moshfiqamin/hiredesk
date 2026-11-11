package scoring

import (
	"strconv"
	"strings"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// CheckAgainstForm checks a rubric against the application form it will
// score. Every question a criterion reads must be on form, and of a kind the
// criterion can score: experience_years reads a number, boolean_all reads
// yes/no answers, and so on.
//
// It returns nil, or a *domain.ValidationError keyed like ValidateCriteria's,
// such as "criteria[1].question_id" or "criteria[3].config.question_ids[0]".
//
// ValidateCriteria judges a rubric on its own; this judges it against one
// post. Call ValidateCriteria first: a criterion whose kind or config it
// refused is skipped here rather than reported twice.
func CheckAgainstForm(criteria []domain.Criterion, form []domain.Question) error {
	byID := make(map[string]domain.Question, len(form))
	for _, q := range form {
		byID[q.ID] = q
	}

	var invalid domain.ValidationError
	for i, c := range criteria {
		spec, ok := registry[c.Kind]
		if !ok {
			continue
		}
		prefix := "criteria[" + strconv.Itoa(i) + "]."

		if spec.needsQuestion && c.QuestionID != "" {
			if problem := questionProblem(byID, c.QuestionID, c.Kind, spec.questionKinds); problem != "" {
				invalid.Add(prefix+"question_id", problem)
			}
		}

		if c.Kind == domain.CriterionKindBooleanAll {
			var cfg booleanAllConfig
			if decodeConfig(c.Config, &cfg) != nil {
				continue
			}
			for j, id := range cfg.QuestionIDs {
				if problem := questionProblem(byID, id, c.Kind, spec.questionKinds); problem != "" {
					invalid.Add(prefix+"config.question_ids["+strconv.Itoa(j)+"]", problem)
				}
			}
		}
	}
	return invalid.Err()
}

// questionProblem describes what is wrong with a criterion of kind reading
// question id, or returns "" if nothing is.
func questionProblem(form map[string]domain.Question, id, kind string, allowed []string) string {
	q, ok := form[id]
	if !ok {
		// Answers are matched to criteria by exact string comparison, and
		// the API writes IDs in lowercase, so an uppercase ID would never
		// find its answer. Saying so is kinder than "not on this post".
		if _, lower := form[strings.ToLower(id)]; lower {
			return "must be written in lowercase, as the API returns it"
		}
		return "is not a question on this job post"
	}
	for _, k := range allowed {
		if q.Kind == k {
			return ""
		}
	}
	return kind + " cannot score a " + q.Kind + " question; it reads " + strings.Join(allowed, " or ") + " questions"
}
