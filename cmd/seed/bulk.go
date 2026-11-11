package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// bulkUnscored is how many of the bulk applications have their scores
// removed afterwards, so the ranking has unscored rows to put last.
const bulkUnscored = 10

// unusableHash is stored as the bulk candidates' password hash. It has the
// shape of a bcrypt hash but is not one, so bcrypt rejects every password
// against it and these accounts can never sign in. Hashing 500 real
// passwords at cost 12 would take minutes and serve no purpose.
const unusableHash = "$2a$12$bulk.seed.accounts.cannot.sign.in.xxxxxxxxxxxxxxxxxxx"

// bulkWords are phrases an answer is built from. Which ones an answer gets
// depends on the applicant's number, so the keyword scores vary.
var bulkWords = []string{
	"I built a REST API in Go", "using Gin for routing", "with goroutines for background work",
	"and PostgreSQL behind it", "deployed on Kubernetes", "with integration tests",
	"and a CI pipeline", "behind a feature flag", "measured with pprof",
}

// seedBulk creates an open job post with a form and a rubric, and n
// applications to it from n new candidates, each submitted through
// ApplicationStore.SubmitApplication, so every score is real. The last
// bulkUnscored applications then lose their scores. It prints progress, the
// post's ID and how to measure the ranking on out, a *log.Logger for the same
// reason as seed's.
//
// Each run makes a new post and new candidates, so it can be repeated.
func seedBulk(ctx context.Context, pool *pgxpool.Pool, n int, out *log.Logger) error {
	users := store.NewUserStore(pool)
	admin, err := users.GetUserByEmail(ctx, "admin@hiredesk.test")
	if err != nil {
		return fmt.Errorf("bulk: find the admin: %w", err)
	}

	posts := store.NewJobPostStore(pool)
	post, err := posts.CreateJobPost(ctx, domain.NewJobPost{
		Title: "Backend Engineer (bulk ranking demo, " + time.Now().UTC().Format("2006-01-02 15:04:05") + ")", Vacancies: 3, CreatedBy: admin.ID,
	})
	if err != nil {
		return fmt.Errorf("bulk: create post: %w", err)
	}

	questions := store.NewQuestionStore(pool)
	var ids [4]string
	for i, q := range []domain.NewQuestion{
		{Prompt: "Describe a project you built", Kind: domain.QuestionKindText, Required: true},
		{Prompt: "Years of Go?", Kind: domain.QuestionKindNumber, Required: true},
		{Prompt: "Do you have power backup?", Kind: domain.QuestionKindBoolean, Required: true},
		{Prompt: "Stable internet?", Kind: domain.QuestionKindBoolean, Required: true},
	} {
		created, qErr := questions.CreateQuestion(ctx, post.ID, q)
		if qErr != nil {
			return fmt.Errorf("bulk: add question: %w", qErr)
		}
		ids[i] = created.ID
	}

	criteria := []domain.Criterion{
		{Label: "Go knowledge", Kind: domain.CriterionKindKeywordMatch, Weight: 30, QuestionID: ids[0],
			Config: json.RawMessage(`{"keywords":["go","gin","goroutines","postgresql","kubernetes"]}`)},
		{Label: "Experience", Kind: domain.CriterionKindExperienceYears, Weight: 40, QuestionID: ids[1],
			Config: json.RawMessage(`{"min":0,"target":6}`)},
		{Label: "Remote readiness", Kind: domain.CriterionKindBooleanAll, Weight: 10,
			Config: json.RawMessage(`{"question_ids":["` + ids[2] + `","` + ids[3] + `"]}`)},
		{Label: "Depth", Kind: domain.CriterionKindAnswerDepth, Weight: 20, QuestionID: ids[0],
			Config: json.RawMessage(`{"min_chars":50,"target_chars":300}`)},
	}
	if err = scoring.ValidateCriteria(criteria); err != nil {
		return fmt.Errorf("bulk: rubric: %w", err)
	}
	if _, err = store.NewRubricStore(pool).ReplaceRubric(ctx, post.ID, admin.ID, criteria); err != nil {
		return fmt.Errorf("bulk: rubric: %w", err)
	}
	if _, err = posts.TransitionJobPost(ctx, post.ID, domain.JobStatusOpen, admin.ID); err != nil {
		return fmt.Errorf("bulk: open post: %w", err)
	}

	apps := store.NewApplicationStore(pool)
	// The post ID's first block keeps this run's addresses apart from any
	// earlier run's.
	prefix := "bulk-" + post.ID[:8] + "-"
	appIDs := make([]string, 0, n)
	started := time.Now()
	for i := range n {
		candidate, cErr := users.CreateUser(ctx, domain.NewUser{
			Email:        fmt.Sprintf("%s%04d@hiredesk.test", prefix, i),
			PasswordHash: unusableHash,
			FullName:     fmt.Sprintf("Bulk Candidate %04d", i),
			Role:         domain.RoleCandidate,
		})
		if cErr != nil {
			return fmt.Errorf("bulk: candidate %d: %w", i, cErr)
		}

		app, sErr := apps.SubmitApplication(ctx, domain.NewApplication{
			JobPostID:   post.ID,
			CandidateID: candidate.ID,
			CVURL:       "https://cv.example/" + prefix + strconv.Itoa(i) + ".pdf",
			Answers: []domain.NewAnswer{
				{QuestionID: ids[0], Answer: bulkAnswer(i)},
				{QuestionID: ids[1], Answer: strconv.Itoa(i % 9)},
				{QuestionID: ids[2], Answer: strconv.FormatBool(i%5 != 0)},
				{QuestionID: ids[3], Answer: "true"},
			},
		})
		if sErr != nil {
			return fmt.Errorf("bulk: application %d: %w", i, sErr)
		}
		appIDs = append(appIDs, app.ID)

		if (i+1)%100 == 0 {
			out.Printf("  %d applications", i+1)
		}
	}

	// Remove the scores of the last few, so the ranking has applications
	// with no score to place at the bottom.
	if strip := min(bulkUnscored, len(appIDs)); strip > 0 {
		if _, err = pool.Exec(ctx, `DELETE FROM scores WHERE application_id = ANY($1::text[]::uuid[])`,
			appIDs[len(appIDs)-strip:]); err != nil {
			return fmt.Errorf("bulk: unscore: %w", err)
		}
	}

	out.Printf("\ncreated job post %s with %d applications (%d unscored) in %v",
		post.ID, n, min(bulkUnscored, n), time.Since(started).Round(time.Millisecond))
	out.Printf("measure it: JOB=%s, then see docs/notes/07-5-ranking.md", post.ID)
	return nil
}

// bulkAnswer builds applicant i's project answer from a varying number of
// phrases, so both keyword matches and answer length differ between
// applicants.
func bulkAnswer(i int) string {
	n := 1 + i%len(bulkWords)
	parts := make([]string, 0, n)
	for j := range n {
		parts = append(parts, bulkWords[(i+j)%len(bulkWords)])
	}
	return strings.Join(parts, ", ") + "."
}
