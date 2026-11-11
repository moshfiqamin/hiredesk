package scoring_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

func TestCheckAgainstForm(t *testing.T) {
	t.Parallel()

	form := []domain.Question{
		{ID: "q-text", Kind: domain.QuestionKindText},
		{ID: "q-choice", Kind: domain.QuestionKindChoice},
		{ID: "q-number", Kind: domain.QuestionKindNumber},
		{ID: "q-yes-1", Kind: domain.QuestionKindBoolean},
		{ID: "q-yes-2", Kind: domain.QuestionKindBoolean},
	}

	keyword := func(q string) domain.Criterion {
		return domain.Criterion{Kind: domain.CriterionKindKeywordMatch, QuestionID: q, Config: json.RawMessage(`{"keywords":["go"]}`)}
	}
	years := func(q string) domain.Criterion {
		return domain.Criterion{Kind: domain.CriterionKindExperienceYears, QuestionID: q, Config: json.RawMessage(`{"target":2}`)}
	}
	depth := func(q string) domain.Criterion {
		return domain.Criterion{Kind: domain.CriterionKindAnswerDepth, QuestionID: q, Config: json.RawMessage(`{"target_chars":400}`)}
	}
	all := func(config string) domain.Criterion {
		return domain.Criterion{Kind: domain.CriterionKindBooleanAll, Config: json.RawMessage(config)}
	}

	tests := []struct {
		name     string
		criteria []domain.Criterion
		want     map[string]string // nil: valid
	}{
		{
			name: "every kind reading a question it can score",
			criteria: []domain.Criterion{
				keyword("q-text"), keyword("q-choice"), years("q-number"), depth("q-text"),
				all(`{"question_ids":["q-yes-1","q-yes-2"]}`),
			},
		},
		{
			name:     "a question from another post",
			criteria: []domain.Criterion{keyword("q-elsewhere")},
			want:     map[string]string{"criteria[0].question_id": "is not a question on this job post"},
		},
		{
			name:     "an uppercase ID is told to be lowercase",
			criteria: []domain.Criterion{years("Q-NUMBER")},
			want:     map[string]string{"criteria[0].question_id": "must be written in lowercase, as the API returns it"},
		},
		{
			name:     "experience_years on a text question",
			criteria: []domain.Criterion{years("q-text")},
			want: map[string]string{
				"criteria[0].question_id": "experience_years cannot score a text question; it reads number questions",
			},
		},
		{
			name:     "answer_depth on a choice question",
			criteria: []domain.Criterion{depth("q-choice")},
			want: map[string]string{
				"criteria[0].question_id": "answer_depth cannot score a choice question; it reads text questions",
			},
		},
		{
			name:     "keyword_match on a boolean",
			criteria: []domain.Criterion{keyword("q-yes-1")},
			want: map[string]string{
				"criteria[0].question_id": "keyword_match cannot score a boolean question; it reads text or choice questions",
			},
		},
		{
			name:     "boolean_all names a missing question and a non-boolean one",
			criteria: []domain.Criterion{depth("q-text"), all(`{"question_ids":["q-yes-1","q-gone","q-number"]}`)},
			want: map[string]string{
				"criteria[1].config.question_ids[1]": "is not a question on this job post",
				"criteria[1].config.question_ids[2]": "boolean_all cannot score a number question; it reads boolean questions",
			},
		},
		{
			// Already reported by ValidateCriteria; not reported twice.
			name:     "an unknown kind and an undecodable config are skipped",
			criteria: []domain.Criterion{{Kind: "vibes", QuestionID: "q-gone"}, all(`{"question_ids":"q-yes-1"}`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := scoring.CheckAgainstForm(tt.criteria, form)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("CheckAgainstForm = %v, want nil", err)
				}
				return
			}

			var invalid *domain.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("CheckAgainstForm = %v, want a *domain.ValidationError", err)
			}
			for field, problem := range tt.want {
				if invalid.Fields[field] != problem {
					t.Errorf("%s = %q, want %q", field, invalid.Fields[field], problem)
				}
			}
			if len(invalid.Fields) != len(tt.want) {
				t.Errorf("got %v, want exactly %v", invalid.Fields, tt.want)
			}
		})
	}
}
