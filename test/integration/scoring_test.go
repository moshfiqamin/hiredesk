//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// IDs for the scoring tests, on top of seedSubmission's candidate, post and
// two questions (subQWhy, a text question; subQYears, a number).
const (
	scoringAdminID = "00000000-0000-4000-8000-0000000000a1"
	subQBackup     = "55555555-0000-4000-8000-000000000003"
	subQInternet   = "55555555-0000-4000-8000-000000000004"
)

// projectAnswer is long enough that an excerpt must cut it, with the
// keywords well past the first 120 characters.
var projectAnswer = strings.Repeat("I care about clear naming and small functions. ", 6) +
	"Last year I built a REST API in Go using Gin, with a goroutine per job handling the slow work. " +
	strings.Repeat("It served real traffic for a year and never paged anyone. ", 8)

// seedScoring adds an admin, two yes/no questions, and a rubric of one
// criterion of each kind, stored through RubricStore as the API would.
func seedScoring(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	seedSubmission(t, pool)

	const fixtures = `
		INSERT INTO users (id, email, password_hash, full_name, role)
		VALUES ('` + scoringAdminID + `', 'scoring-admin@test', 'unused', 'Admin', 'admin');

		INSERT INTO questions (id, job_post_id, prompt, kind, required, position) VALUES
			('` + subQBackup + `',   '` + subOpenPostID + `', 'Do you have power backup?', 'boolean', false, 3),
			('` + subQInternet + `', '` + subOpenPostID + `', 'Stable internet?',          'boolean', false, 4);`
	if _, err := pool.Exec(ctx, fixtures); err != nil {
		t.Fatalf("seed scoring: %v", err)
	}

	criteria := []domain.Criterion{
		{Label: "Go knowledge", Kind: domain.CriterionKindKeywordMatch, Weight: 30, QuestionID: subQWhy,
			Config: json.RawMessage(`{"keywords":["go","gin","goroutine","kubernetes"]}`)},
		{Label: "Experience", Kind: domain.CriterionKindExperienceYears, Weight: 40, QuestionID: subQYears,
			Config: json.RawMessage(`{"min":0,"target":8}`)},
		{Label: "Remote readiness", Kind: domain.CriterionKindBooleanAll, Weight: 10,
			Config: json.RawMessage(`{"question_ids":["` + subQBackup + `","` + subQInternet + `"]}`)},
		{Label: "Depth", Kind: domain.CriterionKindAnswerDepth, Weight: 20, QuestionID: subQWhy,
			Config: json.RawMessage(`{"min_chars":50,"target_chars":400}`)},
	}
	if err := scoring.ValidateCriteria(criteria); err != nil {
		t.Fatalf("the fixture rubric is invalid: %v", err)
	}
	if _, err := store.NewRubricStore(pool).ReplaceRubric(ctx, subOpenPostID, scoringAdminID, criteria); err != nil {
		t.Fatalf("store rubric: %v", err)
	}
}

// scoredApplication answers every question of the scoring post.
func scoredApplication() domain.NewApplication {
	in := validApplication()
	in.Answers = []domain.NewAnswer{
		{QuestionID: subQWhy, Answer: projectAnswer},
		{QuestionID: subQYears, Answer: "4"},
		{QuestionID: subQBackup, Answer: "true"},
		{QuestionID: subQInternet, Answer: "true"},
	}
	return in
}

// storedScore is one scores row, with the question its evidence answers.
type storedScore struct {
	label, detail      string
	earned, available  float64
	version            int
	evidenceQuestionID *string
}

func readScores(t *testing.T, pool *pgxpool.Pool, appID string) []storedScore {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT rc.label, s.detail, s.earned::float8, s.available::float8, s.rubric_version, aa.question_id::text
		FROM scores s
		JOIN rubric_criteria rc ON rc.id = s.criterion_id
		LEFT JOIN application_answers aa ON aa.id = s.evidence_answer_id
		WHERE s.application_id = $1::text::uuid
		ORDER BY rc.position`, appID)
	if err != nil {
		t.Fatalf("read scores: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedScore, error) {
		var s storedScore
		scanErr := row.Scan(&s.label, &s.detail, &s.earned, &s.available, &s.version, &s.evidenceQuestionID)
		return s, scanErr
	})
	if err != nil {
		t.Fatalf("read scores: %v", err)
	}
	return out
}

// TestSubmitScoresTheApplication submits through the store and checks the
// score rows the same transaction wrote: one per criterion, each pointing at
// the answer that produced it.
func TestSubmitScoresTheApplication(t *testing.T) {
	pool := startPostgres(t)
	seedScoring(t, pool)
	ctx := context.Background()

	app, err := store.NewApplicationStore(pool).SubmitApplication(ctx, scoredApplication())
	if err != nil {
		t.Fatalf("SubmitApplication: %v", err)
	}
	if app.RubricVersion == nil || *app.RubricVersion != 1 {
		t.Fatalf("RubricVersion = %v, want 1", app.RubricVersion)
	}

	got := readScores(t, pool, app.ID)
	if len(got) != 4 {
		t.Fatalf("got %d score rows, want one per criterion (4): %+v", len(got), got)
	}

	// Worked by hand. Go knowledge: 3 of 4 keywords, 0.75 × 30. Experience:
	// 4 of a ramp from 0 to 8, 0.5 × 40. Remote: both true, all 10. Depth:
	// the answer is over 400 characters, all 20.
	want := []struct {
		label     string
		earned    float64
		available float64
		evidence  string // the question whose answer is the evidence; "" for none
	}{
		{"Go knowledge", 22.5, 30, subQWhy},
		{"Experience", 20, 40, subQYears},
		{"Remote readiness", 10, 10, ""},
		{"Depth", 20, 20, subQWhy},
	}
	for i, w := range want {
		s := got[i]
		if s.label != w.label || s.earned != w.earned || s.available != w.available || s.version != 1 {
			t.Errorf("row %d = %+v, want %s %v of %v at version 1", i, s, w.label, w.earned, w.available)
		}
		gotEvidence := ""
		if s.evidenceQuestionID != nil {
			gotEvidence = *s.evidenceQuestionID
		}
		if gotEvidence != w.evidence {
			t.Errorf("%s: evidence answers question %q, want %q", w.label, gotEvidence, w.evidence)
		}
		if s.detail == "" {
			t.Errorf("%s: empty detail", w.label)
		}
	}

	// The event says which rubric version scored it.
	var version int
	if err := pool.QueryRow(ctx,
		`SELECT (payload->>'rubric_version')::int FROM application_events WHERE application_id = $1::text::uuid`,
		app.ID).Scan(&version); err != nil || version != 1 {
		t.Errorf("event rubric_version = %d, %v; want 1", version, err)
	}
}

// TestSubmitRollsBackWhenScoringFails breaks the stored rubric behind the
// API's back, which is the only way a rubric the engine refuses can exist,
// and checks that the submission leaves nothing behind.
func TestSubmitRollsBackWhenScoringFails(t *testing.T) {
	pool := startPostgres(t)
	seedScoring(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`UPDATE rubric_criteria SET config = '{"keywords": []}' WHERE label = 'Go knowledge'`); err != nil {
		t.Fatalf("break rubric: %v", err)
	}

	_, err := store.NewApplicationStore(pool).SubmitApplication(ctx, scoredApplication())
	if !errors.Is(err, scoring.ErrInvalidConfig) {
		t.Fatalf("err = %v, want one wrapping scoring.ErrInvalidConfig", err)
	}

	// The application and answers had been inserted when scoring failed.
	// None of it, and no score, may survive.
	if a, ans, ev := countRows(t, pool); a != 0 || ans != 0 || ev != 0 {
		t.Errorf("after the failed submit: %d applications, %d answers, %d events; want none", a, ans, ev)
	}
	var scores int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scores`).Scan(&scores); err != nil || scores != 0 {
		t.Errorf("scores = %d, %v; want 0", scores, err)
	}
}

// TestSubmitWithoutRubricIsKeptUnscored covers the decision for a post that
// somehow has no rubric: the application is stored, with no scores, and its
// event says it was not scored.
func TestSubmitWithoutRubricIsKeptUnscored(t *testing.T) {
	pool := startPostgres(t)
	seedSubmission(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	app, err := apps.SubmitApplication(ctx, validApplication())
	if err != nil {
		t.Fatalf("SubmitApplication: %v", err)
	}
	if app.RubricVersion != nil {
		t.Errorf("RubricVersion = %d, want nil", *app.RubricVersion)
	}
	if got := readScores(t, pool, app.ID); len(got) != 0 {
		t.Errorf("got %d score rows, want none", len(got))
	}

	var isNull bool
	if err := pool.QueryRow(ctx,
		`SELECT payload->'rubric_version' = 'null'::jsonb FROM application_events WHERE application_id = $1::text::uuid`,
		app.ID).Scan(&isNull); err != nil || !isNull {
		t.Errorf("event rubric_version is JSON null = %v, %v; want true", isNull, err)
	}

	if _, err := apps.ScoreCard(ctx, app.ID); !errors.Is(err, domain.ErrNotScored) {
		t.Errorf("ScoreCard = %v, want ErrNotScored", err)
	}
}

// TestOldCriteriaOutliveARubricChange replaces the rubric after an
// application was scored, and checks that the old score still reads as it
// was given: against version 1, with version 1's labels.
func TestOldCriteriaOutliveARubricChange(t *testing.T) {
	pool := startPostgres(t)
	seedScoring(t, pool)
	ctx := context.Background()
	apps := store.NewApplicationStore(pool)

	app, err := apps.SubmitApplication(ctx, scoredApplication())
	if err != nil {
		t.Fatalf("SubmitApplication: %v", err)
	}

	_, err = store.NewRubricStore(pool).ReplaceRubric(ctx, subOpenPostID, scoringAdminID, []domain.Criterion{
		{Label: "Only depth now", Kind: domain.CriterionKindAnswerDepth, Weight: 100, QuestionID: subQWhy,
			Config: json.RawMessage(`{"target_chars":100}`)},
	})
	if err != nil {
		t.Fatalf("ReplaceRubric: %v", err)
	}

	card, err := apps.ScoreCard(ctx, app.ID)
	if err != nil {
		t.Fatalf("ScoreCard: %v", err)
	}
	if card.RubricVersion != 1 || len(card.Entries) != 4 || card.Entries[0].Criterion.Label != "Go knowledge" {
		t.Errorf("card = version %d, %d entries, first %q; want version 1's four criteria",
			card.RubricVersion, len(card.Entries), card.Entries[0].Criterion.Label)
	}

	// And the database will not let version 1 go while a score points at it:
	// scores.criterion_id is ON DELETE RESTRICT.
	_, err = pool.Exec(ctx, `DELETE FROM rubric_criteria WHERE job_post_id = $1::text::uuid AND rubric_version = 1`, subOpenPostID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("deleting version 1 = %v, want a foreign key violation (23503)", err)
	}
}

// queryCounter is a pgx tracer that counts the statements a pool runs.
type queryCounter struct{ n atomic.Int64 }

func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.n.Add(1)
	return ctx
}

func (q *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TestScoreEndpoint calls GET /v1/applications/:id/score through the real
// router, checks the body against the overview's shape, and counts the
// statements it took: two, whatever the number of criteria.
func TestScoreEndpoint(t *testing.T) {
	plain := startPostgres(t)
	seedScoring(t, plain)
	ctx := context.Background()

	app, err := store.NewApplicationStore(plain).SubmitApplication(ctx, scoredApplication())
	if err != nil {
		t.Fatalf("SubmitApplication: %v", err)
	}

	// A second pool on the same database, with the tracer attached.
	cfg := plain.Config()
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("traced pool: %v", err)
	}
	t.Cleanup(traced.Close)

	tokens := auth.NewTokenManager(testJWTSecret)
	router := newRouter(t, traced, tokens)
	token, err := tokens.NewAccessToken(scoringAdminID, domain.RoleAdmin)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/applications/"+app.ID+"/score", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(rec, req)
		return rec
	}

	// RequireAuth loads the user: one statement before the handler runs.
	before := counter.n.Load()
	rec := get()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
	}
	if n := counter.n.Load() - before; n != 3 {
		t.Errorf("the request ran %d statements, want 3: the user, the application, the score card", n)
	}

	var body struct {
		ApplicationID  string  `json:"application_id"`
		TotalEarned    float64 `json:"total_earned"`
		TotalAvailable float64 `json:"total_available"`
		Percentage     float64 `json:"percentage"`
		RubricVersion  int     `json:"rubric_version"`
		Breakdown      []struct {
			Criterion string  `json:"criterion"`
			Kind      string  `json:"kind"`
			Earned    float64 `json:"earned"`
			Available float64 `json:"available"`
			Detail    string  `json:"detail"`
			Evidence  *struct {
				Question string `json:"question"`
				AnswerID string `json:"answer_id"`
				Excerpt  string `json:"excerpt"`
			} `json:"evidence"`
		} `json:"breakdown"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body)
	}

	if body.ApplicationID != app.ID || body.RubricVersion != 1 || len(body.Breakdown) != 4 {
		t.Fatalf("body = %s", rec.Body)
	}
	if body.TotalEarned != 72.5 || body.TotalAvailable != 100 || body.Percentage != 72.5 {
		t.Errorf("totals = %v of %v (%v%%), want 72.5 of 100 (72.5%%)", body.TotalEarned, body.TotalAvailable, body.Percentage)
	}

	goKnowledge := body.Breakdown[0]
	if goKnowledge.Evidence == nil || goKnowledge.Evidence.Question != "Why this role?" {
		t.Fatalf("Go knowledge evidence = %+v", goKnowledge.Evidence)
	}
	if ex := goKnowledge.Evidence.Excerpt; !strings.Contains(ex, "REST API in Go using Gin") || !strings.HasPrefix(ex, "...") || len(ex) > 200 {
		t.Errorf("excerpt = %q, want about 120 characters around the first keyword", ex)
	}
	if strings.Contains(rec.Body.String(), "never paged anyone") {
		t.Errorf("the body contains the end of the answer: the whole answer was sent")
	}

	// The evidence ID is a real answer row, to the question it names.
	var question string
	if err = plain.QueryRow(ctx, `
		SELECT q.prompt FROM application_answers aa JOIN questions q ON q.id = aa.question_id
		WHERE aa.id = $1::text::uuid`, goKnowledge.Evidence.AnswerID).Scan(&question); err != nil || question != "Why this role?" {
		t.Errorf("evidence answer resolves to %q, %v", question, err)
	}

	if remote := body.Breakdown[2]; remote.Kind != domain.CriterionKindBooleanAll || remote.Evidence != nil {
		t.Errorf("boolean_all entry = %+v, want null evidence", remote)
	}

	// Twice the criteria, the same number of statements: no query per
	// criterion. The new rubric is applied by a second application.
	doubled := make([]domain.Criterion, 0, 8)
	for i := range 8 {
		doubled = append(doubled, domain.Criterion{
			Label: "Depth " + string(rune('A'+i)), Kind: domain.CriterionKindAnswerDepth, Weight: 12.5,
			QuestionID: subQWhy, Config: json.RawMessage(`{"target_chars":100}`),
		})
	}
	if _, err = store.NewRubricStore(plain).ReplaceRubric(ctx, subOpenPostID, scoringAdminID, doubled); err != nil {
		t.Fatalf("ReplaceRubric: %v", err)
	}
	if _, err = plain.Exec(ctx, `DELETE FROM applications WHERE id = $1::text::uuid`, app.ID); err != nil {
		t.Fatalf("delete first application: %v", err)
	}
	app, err = store.NewApplicationStore(plain).SubmitApplication(ctx, scoredApplication())
	if err != nil {
		t.Fatalf("SubmitApplication again: %v", err)
	}

	before = counter.n.Load()
	if rec := get(); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Depth H") {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
	}
	if n := counter.n.Load() - before; n != 3 {
		t.Errorf("with 8 criteria the request ran %d statements, want the same 3", n)
	}
}
