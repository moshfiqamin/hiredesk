//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

const (
	pipeAdminID       = "00000000-0000-4000-8000-0000000000a1"
	pipeInterviewerID = "00000000-0000-4000-8000-0000000000a2"
)

// pipelineFixture is a job post with a number of applications, each from its
// own candidate, all in a container of their own.
type pipelineFixture struct {
	pool *pgxpool.Pool
	apps *store.ApplicationStore
	ctx  context.Context
}

func newPipelineFixture(t *testing.T) *pipelineFixture {
	t.Helper()

	pool := startPostgres(t)
	f := &pipelineFixture{pool: pool, apps: store.NewApplicationStore(pool), ctx: context.Background()}

	f.exec(t, `INSERT INTO users (id, email, password_hash, full_name, role) VALUES
		('`+pipeAdminID+`',       'admin@pipe',       'unused', 'Admin',       'admin'),
		('`+pipeInterviewerID+`', 'interviewer@pipe', 'unused', 'Interviewer', 'interviewer')`)
	return f
}

func (f *pipelineFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		t.Fatalf("exec: %v\n%s", err, sql)
	}
}

// post creates an open post with this many vacancies and returns its ID.
func (f *pipelineFixture) post(t *testing.T, title string, vacancies int) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx,
		`INSERT INTO job_posts (title, status, vacancies) VALUES ($1, 'open', $2) RETURNING id::text`,
		title, vacancies).Scan(&id); err != nil {
		t.Fatalf("create post: %v", err)
	}
	return id
}

// application creates a candidate and their application to postID, in
// status, submitted at the given time, and returns the application's ID.
func (f *pipelineFixture) application(t *testing.T, postID, status string, submittedAt time.Time) (appID, candidateID string) {
	t.Helper()
	err := f.pool.QueryRow(f.ctx, `
		WITH c AS (
			INSERT INTO users (email, password_hash, full_name, role)
			VALUES (gen_random_uuid()::text || '@pipe', 'unused', 'Candidate', 'candidate')
			RETURNING id
		)
		INSERT INTO applications (job_post_id, candidate_id, cv_url, status, submitted_at)
		SELECT $1::text::uuid, c.id, 'https://cv.test/x.pdf', $2, $3 FROM c
		RETURNING id::text, candidate_id::text`,
		postID, status, submittedAt).Scan(&appID, &candidateID)
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	return appID, candidateID
}

func (f *pipelineFixture) status(t *testing.T, appID string) string {
	t.Helper()
	var s string
	if err := f.pool.QueryRow(f.ctx, `SELECT status FROM applications WHERE id = $1::text::uuid`, appID).Scan(&s); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return s
}

// statusEvents returns the from → to pairs recorded for an application, in
// order, read through ApplicationHistory.
func (f *pipelineFixture) statusEvents(t *testing.T, appID string) []string {
	t.Helper()
	_, events, err := f.apps.ApplicationHistory(f.ctx, appID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var out []string
	for _, e := range events {
		if e.Type != domain.EventApplicationStatusChanged {
			continue
		}
		if e.ActorID == nil || *e.ActorID != pipeAdminID {
			t.Errorf("event %s has actor %v, want the admin", e.ID, e.ActorID)
		}
		out = append(out, fmt.Sprintf("%v→%v", e.Payload["from"], e.Payload["to"]))
	}
	return out
}

var base = time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)

func TestApplicationPipeline(t *testing.T) {
	f := newPipelineFixture(t)
	postID := f.post(t, "One vacancy", 1)
	first, _ := f.application(t, postID, domain.StatusSubmitted, base)
	second, _ := f.application(t, postID, domain.StatusInterview, base.Add(time.Minute))

	move := func(id, to string) error {
		_, err := f.apps.TransitionApplication(f.ctx, id, to, pipeAdminID)
		return err
	}

	t.Run("a skipped stage is refused and writes nothing", func(t *testing.T) {
		err := move(first, domain.StatusInterview)
		var te *domain.TransitionError
		if !errors.As(err, &te) || te.From != domain.StatusSubmitted {
			t.Fatalf("err = %v, want a *TransitionError from submitted", err)
		}
		if got := f.status(t, first); got != domain.StatusSubmitted {
			t.Errorf("status = %s after a refused move", got)
		}
		if got := f.statusEvents(t, first); len(got) != 0 {
			t.Errorf("events %v after a refused move", got)
		}
	})

	t.Run("each step forward is recorded with both statuses", func(t *testing.T) {
		for _, to := range []string{domain.StatusShortlisted, domain.StatusInterview, domain.StatusOffer} {
			if err := move(first, to); err != nil {
				t.Fatalf("move to %s: %v", to, err)
			}
		}
		want := []string{"submitted→shortlisted", "shortlisted→interview", "interview→offer"}
		if got := f.statusEvents(t, first); !slices.Equal(got, want) {
			t.Errorf("events = %v, want %v", got, want)
		}
	})

	t.Run("a second offer on a one-vacancy post is refused", func(t *testing.T) {
		err := move(second, domain.StatusOffer)
		var nv *domain.NoVacanciesError
		if !errors.As(err, &nv) || nv.Vacancies != 1 || nv.Offers != 1 {
			t.Fatalf("err = %v, want a *NoVacanciesError with 1 vacancy and 1 offer", err)
		}
		if got := f.status(t, second); got != domain.StatusInterview {
			t.Errorf("status = %s after a refused offer", got)
		}
	})

	t.Run("rejecting the first offer frees the vacancy", func(t *testing.T) {
		if err := move(first, domain.StatusRejected); err != nil {
			t.Fatalf("reject the offer: %v", err)
		}
		if err := move(second, domain.StatusOffer); err != nil {
			t.Fatalf("offer to the second: %v", err)
		}
	})

	t.Run("rejected is final", func(t *testing.T) {
		for _, to := range []string{domain.StatusSubmitted, domain.StatusShortlisted, domain.StatusOffer, domain.StatusRejected} {
			if err := move(first, to); !errors.Is(err, domain.ErrInvalidTransition) {
				t.Errorf("rejected → %s: err = %v, want ErrInvalidTransition", to, err)
			}
		}
	})

	t.Run("an application that does not exist", func(t *testing.T) {
		for _, id := range []string{"22222222-0000-4000-8000-0000000000ff", "not-a-uuid"} {
			if err := move(id, domain.StatusRejected); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("%q: err = %v, want ErrNotFound", id, err)
			}
		}
	})
}

// Five admins each make an offer for the last vacancy at the same moment.
// Without the lock on the post, every transaction would count zero offers and
// all five would succeed.
func TestConcurrentOffersForLastVacancy(t *testing.T) {
	f := newPipelineFixture(t)
	postID := f.post(t, "Last seat", 1)

	const n = 5
	ids := make([]string, n)
	for i := range n {
		ids[i], _ = f.application(t, postID, domain.StatusInterview, base.Add(time.Duration(i)*time.Second))
	}

	// Each offer's UPDATE sleeps for 200 ms after the vacancy check has passed
	// and before the transaction commits. Without it the transactions are
	// short enough to finish one after another by luck, and the test would
	// pass whether the code serialised them or not. With it, they overlap
	// every time unless the code makes them wait.
	f.exec(t, `
		CREATE FUNCTION slow_offer() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_sleep(0.2);
			RETURN NEW;
		END $$;

		CREATE TRIGGER slow_offer BEFORE UPDATE ON applications
			FOR EACH ROW WHEN (NEW.status = 'offer') EXECUTE FUNCTION slow_offer();`)

	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = f.apps.TransitionApplication(f.ctx, ids[i], domain.StatusOffer, pipeAdminID)
		}()
	}
	close(start)
	wg.Wait()

	var offered, refused int
	for i, err := range errs {
		switch {
		case err == nil:
			offered++
		case errors.Is(err, domain.ErrNoVacancies):
			refused++
		default:
			t.Errorf("offer %d: unexpected error %v", i, err)
		}
	}
	if offered != 1 || refused != n-1 {
		t.Errorf("offered %d, refused %d; want 1 and %d", offered, refused, n-1)
	}

	var offers int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM applications WHERE job_post_id = $1::text::uuid AND status = 'offer'`, postID,
	).Scan(&offers); err != nil {
		t.Fatalf("count: %v", err)
	}
	if offers != 1 {
		t.Errorf("%d offers stored for one vacancy", offers)
	}
}

func TestListApplicationsFilteredAndPaged(t *testing.T) {
	f := newPipelineFixture(t)
	postA := f.post(t, "A", 3)
	postB := f.post(t, "B", 3)

	// Post A: five applications, newest last. The middle two share a
	// submitted_at, so the id tie-breaker is exercised across a page edge.
	statuses := []string{domain.StatusSubmitted, domain.StatusInterview, domain.StatusInterview, domain.StatusSubmitted, domain.StatusRejected}
	times := []time.Time{base, base.Add(time.Hour), base.Add(2 * time.Hour), base.Add(2 * time.Hour), base.Add(3 * time.Hour)}
	var aIDs []string
	var aCandidate string
	for i := range statuses {
		id, candidate := f.application(t, postA, statuses[i], times[i])
		aIDs = append(aIDs, id)
		if i == 0 {
			aCandidate = candidate
		}
	}
	bID, _ := f.application(t, postB, domain.StatusSubmitted, base.Add(4*time.Hour))

	// The interviewer is assigned to one application on each post.
	f.exec(t, `INSERT INTO interviewer_assignments (application_id, interviewer_id) VALUES
		($1::text::uuid, $3::text::uuid), ($2::text::uuid, $3::text::uuid)`, aIDs[1], bID, pipeInterviewerID)

	admin := domain.User{ID: pipeAdminID, Role: domain.RoleAdmin}
	interviewer := domain.User{ID: pipeInterviewerID, Role: domain.RoleInterviewer}
	candidate := domain.User{ID: aCandidate, Role: domain.RoleCandidate}

	// walk follows next cursors to the end and returns every ID in order,
	// the totals reported on each page, and how many pages it took.
	walk := func(t *testing.T, viewer domain.User, filter domain.ApplicationFilter) (ids []string, totals []int, pages int) {
		t.Helper()
		for {
			page, total, more, err := f.apps.ListApplications(f.ctx, viewer, filter)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			pages++
			totals = append(totals, total)
			for _, a := range page {
				ids = append(ids, a.ID)
			}
			if !more {
				return ids, totals, pages
			}
			last := page[len(page)-1]
			filter.After = &domain.ListingPosition{CreatedAt: last.SubmittedAt, ID: last.ID}
		}
	}

	t.Run("pages through one post newest first, with no repeats or gaps", func(t *testing.T) {
		ids, totals, pages := walk(t, admin, domain.ApplicationFilter{JobPostID: postA, Limit: 2})

		if pages != 3 {
			t.Errorf("pages = %d, want 3", pages)
		}
		if !slices.Equal(totals, []int{5, 5, 5}) {
			t.Errorf("totals = %v, want 5 on every page", totals)
		}
		// Newest first; the two at the same time in descending id order.
		want := []string{aIDs[4], max(aIDs[2], aIDs[3]), min(aIDs[2], aIDs[3]), aIDs[1], aIDs[0]}
		if !slices.Equal(ids, want) {
			t.Errorf("order = %v\n want  %v", ids, want)
		}
	})

	t.Run("filters compose with each other and with the role scope", func(t *testing.T) {
		tests := []struct {
			name   string
			viewer domain.User
			filter domain.ApplicationFilter
			want   []string
		}{
			{"admin, no filter", admin, domain.ApplicationFilter{}, append(slices.Clone(aIDs), bID)},
			{"admin, status", admin, domain.ApplicationFilter{Status: domain.StatusInterview}, []string{aIDs[1], aIDs[2]}},
			{"admin, post and status", admin, domain.ApplicationFilter{JobPostID: postA, Status: domain.StatusSubmitted}, []string{aIDs[0], aIDs[3]}},
			{"admin, post B", admin, domain.ApplicationFilter{JobPostID: postB}, []string{bID}},
			{"interviewer, no filter", interviewer, domain.ApplicationFilter{}, []string{aIDs[1], bID}},
			{"interviewer, post A", interviewer, domain.ApplicationFilter{JobPostID: postA}, []string{aIDs[1]}},
			{"interviewer, a status none of theirs has", interviewer, domain.ApplicationFilter{Status: domain.StatusRejected}, nil},
			{"candidate, no filter", candidate, domain.ApplicationFilter{}, []string{aIDs[0]}},
			{"candidate, another post", candidate, domain.ApplicationFilter{JobPostID: postB}, nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tt.filter.Limit = 2
				ids, totals, _ := walk(t, tt.viewer, tt.filter)

				if !sameSet(ids, tt.want) {
					t.Errorf("ids = %v, want %v", ids, tt.want)
				}
				for _, total := range totals {
					if total != len(tt.want) {
						t.Errorf("total = %d, want %d", total, len(tt.want))
					}
				}
			})
		}
	})

	t.Run("a filter value is data, never SQL", func(t *testing.T) {
		page, total, _, err := f.apps.ListApplications(f.ctx, admin,
			domain.ApplicationFilter{Status: "submitted' OR '1'='1", Limit: 10})
		if err != nil || total != 0 || len(page) != 0 {
			t.Errorf("got %d rows, total %d, err %v; want nothing", len(page), total, err)
		}
	})
}

// History comes back in order: answers in form order, events oldest first.
func TestApplicationHistory(t *testing.T) {
	pool := startPostgres(t)
	seedSubmission(t, pool)
	apps := store.NewApplicationStore(pool)
	ctx := context.Background()

	in := validApplication()
	// Sent in reverse; read back in form order.
	in.Answers[0], in.Answers[1] = in.Answers[1], in.Answers[0]
	app, err := apps.SubmitApplication(ctx, in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, full_name, role)
		VALUES ('`+pipeAdminID+`', 'admin@history', 'unused', 'Admin', 'admin')`); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if _, err = apps.TransitionApplication(ctx, app.ID, domain.StatusShortlisted, pipeAdminID); err != nil {
		t.Fatalf("shortlist: %v", err)
	}

	answers, events, err := apps.ApplicationHistory(ctx, app.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(answers) != 2 || answers[0].QuestionID != subQWhy || answers[1].QuestionID != subQYears {
		t.Errorf("answers = %+v, want why then years", answers)
	}
	if len(events) != 2 || events[0].Type != domain.EventApplicationSubmitted || events[1].Type != domain.EventApplicationStatusChanged {
		t.Fatalf("events = %+v", events)
	}
	if *events[0].ActorID != subCandidateID || *events[1].ActorID != pipeAdminID {
		t.Errorf("actors = %s, %s", *events[0].ActorID, *events[1].ActorID)
	}
	if events[1].Payload["from"] != domain.StatusSubmitted || events[1].Payload["to"] != domain.StatusShortlisted {
		t.Errorf("payload = %v", events[1].Payload)
	}

	// A well-formed ID with nothing behind it: empty, not nil, not an error.
	answers, events, err = apps.ApplicationHistory(ctx, "22222222-0000-4000-8000-0000000000ff")
	if err != nil || answers == nil || events == nil || len(answers)+len(events) != 0 {
		t.Errorf("missing application: %v, %v, %v", answers, events, err)
	}
}
