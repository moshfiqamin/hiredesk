//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// rubricFixture is a post with a form of one question of each kind, created
// by a postFixture admin and removed with the admin's posts.
type rubricFixture struct {
	*postFixture
	rubrics *RubricStore

	postID                         string
	qText, qNumber, qYes1, qYes2   string
	otherPostID, otherPostQuestion string
}

func newRubricFixture(t *testing.T) *rubricFixture {
	t.Helper()

	pf := newPostFixture(t)
	f := &rubricFixture{postFixture: pf, rubrics: NewRubricStore(pf.store.db)}

	f.postID = pf.create(t, "Rubric test", nil, nil).ID
	f.qText = f.question(t, f.postID, "Describe a project", "text", 1)
	f.qNumber = f.question(t, f.postID, "Years of Go?", "number", 2)
	f.qYes1 = f.question(t, f.postID, "Power backup?", "boolean", 3)
	f.qYes2 = f.question(t, f.postID, "Stable internet?", "boolean", 4)

	f.otherPostID = pf.create(t, "Another post", nil, nil).ID
	f.otherPostQuestion = f.question(t, f.otherPostID, "Elsewhere", "text", 1)
	return f
}

// question adds a question to a post and returns its ID.
func (f *rubricFixture) question(t *testing.T, postID, prompt, kind string, position int) string {
	t.Helper()

	var id string
	if err := f.store.db.QueryRow(context.Background(),
		`INSERT INTO questions (job_post_id, prompt, kind, position)
		 VALUES ($1::text::uuid, $2, $3, $4) RETURNING id::text`, postID, prompt, kind, position,
	).Scan(&id); err != nil {
		t.Fatalf("add question: %v", err)
	}
	return id
}

// fullRubric is a valid rubric over the fixture's form, one criterion of
// each kind, weights summing to 100.
func (f *rubricFixture) fullRubric() []domain.Criterion {
	return []domain.Criterion{
		{Label: "Go knowledge", Kind: domain.CriterionKindKeywordMatch, Weight: 30, QuestionID: f.qText,
			Config: json.RawMessage(`{"keywords":["go","gin"]}`)},
		{Label: "Experience", Kind: domain.CriterionKindExperienceYears, Weight: 40, QuestionID: f.qNumber,
			Config: json.RawMessage(`{"min":0,"target":2}`)},
		{Label: "Depth", Kind: domain.CriterionKindAnswerDepth, Weight: 20, QuestionID: f.qText,
			Config: json.RawMessage(`{"min_chars":50,"target_chars":400}`)},
		{Label: "Remote readiness", Kind: domain.CriterionKindBooleanAll, Weight: 10,
			Config: json.RawMessage(`{"question_ids":["` + f.qYes1 + `","` + f.qYes2 + `"]}`)},
	}
}

// replace calls ReplaceRubric as the fixture's admin and fails the test on
// an error.
func (f *rubricFixture) replace(t *testing.T, criteria []domain.Criterion) *domain.RubricReplacement {
	t.Helper()

	got, err := f.rubrics.ReplaceRubric(context.Background(), f.postID, f.admin.ID, criteria)
	if err != nil {
		t.Fatalf("ReplaceRubric: %v", err)
	}
	return got
}

// versionCounts returns how many criteria rows each rubric version of the
// post has.
func (f *rubricFixture) versionCounts(t *testing.T) map[int]int {
	t.Helper()

	rows, err := f.store.db.Query(context.Background(),
		`SELECT rubric_version, count(*)::int FROM rubric_criteria
		 WHERE job_post_id = $1::text::uuid GROUP BY 1`, f.postID)
	if err != nil {
		t.Fatalf("count criteria: %v", err)
	}
	defer rows.Close()

	counts := map[int]int{}
	for rows.Next() {
		var version, n int
		if err := rows.Scan(&version, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		counts[version] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("count criteria: %v", err)
	}
	return counts
}

// TestRubricConfigJSONB writes configs through ReplaceRubric and reads them
// back, checking what survives the trip through a jsonb column and what
// PostgreSQL rewrites.
func TestRubricConfigJSONB(t *testing.T) {
	ctx := context.Background()
	f := newRubricFixture(t)

	// Written with extra spaces, keys in an unusual order, a number with a
	// trailing zero, and a keyword outside ASCII.
	written := `{  "target" : 2.50 ,  "min":0 }`
	keywords := `{"keywords":["go", "café", "c++"]}`

	f.replace(t, []domain.Criterion{
		{Label: "Experience", Kind: domain.CriterionKindExperienceYears, Weight: 60, QuestionID: f.qNumber,
			Config: json.RawMessage(written)},
		{Label: "Keywords", Kind: domain.CriterionKindKeywordMatch, Weight: 40, QuestionID: f.qText,
			Config: json.RawMessage(keywords)},
	})

	got, err := f.rubrics.GetRubricByJobPost(ctx, f.postID)
	if err != nil {
		t.Fatalf("GetRubricByJobPost: %v", err)
	}

	// The same JSON value: decoding both sides gives equal Go values.
	for i, want := range []string{written, keywords} {
		var a, b any
		if err := json.Unmarshal([]byte(want), &a); err != nil {
			t.Fatalf("decode written: %v", err)
		}
		if err := json.Unmarshal(got.Criteria[i].Config, &b); err != nil {
			t.Fatalf("decode read back %s: %v", got.Criteria[i].Config, err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("criterion %d: wrote %s, read back %s", i, want, got.Criteria[i].Config)
		}
	}

	// Not the same bytes. jsonb stores a parsed value, so the text it gives
	// back is its own: whitespace normalised, keys sorted shorter first, but
	// the number kept exactly as written, because jsonb numbers are numeric.
	if want := `{"min": 0, "target": 2.50}`; string(got.Criteria[0].Config) != want {
		t.Errorf("stored text = %s, want %s", got.Criteria[0].Config, want)
	}
}

func TestReplaceRubricVersions(t *testing.T) {
	ctx := context.Background()
	f := newRubricFixture(t)

	// No rubric yet: version 0, an empty list, not an error.
	empty, err := f.rubrics.GetRubricByJobPost(ctx, f.postID)
	if err != nil || empty.Version != 0 || empty.Criteria == nil || len(empty.Criteria) != 0 {
		t.Fatalf("before any rubric: %+v, %v; want version 0 and an empty, non-nil list", empty, err)
	}

	first := f.replace(t, f.fullRubric())
	if first.Rubric.Version != 1 || first.PreviousVersion != 0 || len(first.Rubric.Criteria) != 4 {
		t.Fatalf("first replacement: %+v", first)
	}

	// Reversed, and one criterion fewer, so a read that ignored position or
	// mixed versions would show it.
	second := f.fullRubric()[1:]
	second[0], second[2] = second[2], second[0]
	second[0].Weight, second[1].Weight, second[2].Weight = 50, 30, 20
	replaced := f.replace(t, second)
	if replaced.Rubric.Version != 2 || replaced.PreviousVersion != 1 {
		t.Fatalf("second replacement: version %d from %d, want 2 from 1", replaced.Rubric.Version, replaced.PreviousVersion)
	}

	// Version 1 is still there, every row of it.
	if counts := f.versionCounts(t); counts[1] != 4 || counts[2] != 3 || len(counts) != 2 {
		t.Errorf("rows per version = %v, want {1:4 2:3}", counts)
	}

	current, err := f.rubrics.GetRubricByJobPost(ctx, f.postID)
	if err != nil {
		t.Fatalf("GetRubricByJobPost: %v", err)
	}
	var labels []string
	for _, c := range current.Criteria {
		labels = append(labels, c.Label)
		if c.RubricVersion != 2 {
			t.Errorf("%s has version %d, want 2", c.Label, c.RubricVersion)
		}
	}
	if want := []string{"Remote readiness", "Depth", "Experience"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("order = %v, want %v", labels, want)
	}
	if c := current.Criteria[0]; c.QuestionID != "" || c.Weight != 50 {
		t.Errorf("boolean_all read back as %+v, want no question and weight 50", c)
	}

	// The trail: one event per replacement, naming the admin and both
	// versions.
	rows, err := f.store.db.Query(ctx, `
		SELECT actor_id::text, payload->>'from_version', payload->>'to_version', payload->>'criteria'
		FROM job_post_events
		WHERE job_post_id = $1::text::uuid AND type = $2
		ORDER BY created_at, id`, f.postID, domain.EventRubricReplaced)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	defer rows.Close()
	var trail [][4]string
	for rows.Next() {
		var e [4]string
		if err := rows.Scan(&e[0], &e[1], &e[2], &e[3]); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		trail = append(trail, e)
	}
	want := [][4]string{{f.admin.ID, "0", "1", "4"}, {f.admin.ID, "1", "2", "3"}}
	if !reflect.DeepEqual(trail, want) {
		t.Errorf("events = %v, want %v", trail, want)
	}
}

func TestReplaceRubricCountsApplicationsToRescore(t *testing.T) {
	ctx := context.Background()
	f := newRubricFixture(t)
	f.replace(t, f.fullRubric())

	users := NewUserStore(f.store.db)
	for i := range 2 {
		in := newCandidate(t, users)
		c, err := users.CreateUser(ctx, in)
		if err != nil {
			t.Fatalf("create candidate %d: %v", i, err)
		}
		if _, err := f.store.db.Exec(ctx,
			`INSERT INTO applications (job_post_id, candidate_id, cv_url) VALUES ($1::text::uuid, $2::text::uuid, 'https://cv.test')`,
			f.postID, c.ID); err != nil {
			t.Fatalf("insert application: %v", err)
		}
	}

	if got := f.replace(t, f.fullRubric()).ApplicationsToRescore; got != 2 {
		t.Errorf("ApplicationsToRescore = %d, want 2", got)
	}
}

func TestReplaceRubricChecksTheForm(t *testing.T) {
	ctx := context.Background()
	f := newRubricFixture(t)
	f.replace(t, f.fullRubric())

	bad := f.fullRubric()
	bad[0].QuestionID = f.otherPostQuestion // a text question, but on another post
	bad[1].QuestionID = f.qText             // experience_years on a text question

	_, err := f.rubrics.ReplaceRubric(ctx, f.postID, f.admin.ID, bad)
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a *domain.ValidationError", err)
	}
	if invalid.Fields["criteria[0].question_id"] != "is not a question on this job post" ||
		invalid.Fields["criteria[1].question_id"] == "" || len(invalid.Fields) != 2 {
		t.Errorf("fields = %v", invalid.Fields)
	}

	// Nothing was written: still exactly version 1.
	if counts := f.versionCounts(t); len(counts) != 1 || counts[1] != 4 {
		t.Errorf("rows per version = %v, want only version 1", counts)
	}
}

func TestReplaceRubricMissingPost(t *testing.T) {
	f := newRubricFixture(t)

	for _, id := range []string{"11111111-0000-4000-8000-0000000000ee", "not-a-uuid"} {
		_, err := f.rubrics.ReplaceRubric(context.Background(), id, f.admin.ID, f.fullRubric())
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("ReplaceRubric(%q) = %v, want ErrNotFound", id, err)
		}
		if _, err := f.rubrics.GetRubricByJobPost(context.Background(), id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("GetRubricByJobPost(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

// TestReplaceRubricConcurrently starts several replacements of one post's
// rubric at the same moment. Each must get a version of its own, with only
// its own criteria in it. Without the post lock, two would read the same
// highest version and collide.
func TestReplaceRubricConcurrently(t *testing.T) {
	f := newRubricFixture(t)
	const writers = 8

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make(chan error, writers)
	)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// i criteria + 1, so each version's row count says which writer
			// made it.
			criteria := make([]domain.Criterion, i+1)
			for j := range criteria {
				criteria[j] = domain.Criterion{
					Label: "w" + strconv.Itoa(i) + "c" + strconv.Itoa(j), Kind: domain.CriterionKindExperienceYears,
					QuestionID: f.qNumber, Config: json.RawMessage(`{"target":2}`),
				}
			}
			<-start
			_, err := f.rubrics.ReplaceRubric(context.Background(), f.postID, f.admin.ID, criteria)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("ReplaceRubric: %v", err)
		}
	}

	counts := f.versionCounts(t)
	if len(counts) != writers {
		t.Fatalf("versions = %v, want %d distinct versions", counts, writers)
	}
	seen := map[int]bool{}
	for version, n := range counts {
		if version < 1 || version > writers || seen[n] {
			t.Errorf("version %d has %d rows: versions must be 1..%d with one writer's criteria each", version, n, writers)
		}
		seen[n] = true
	}
}
