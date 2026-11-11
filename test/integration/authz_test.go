//go:build integration

// Package integration holds tests that run the real router against a real
// PostgreSQL, started in a throwaway container. They need Docker, and run with
// `make test-integration`.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/moshfiqamin/hiredesk/internal/api"
	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// postgresImage matches docker-compose.yml, so the tests run on the server
// version the application does.
const postgresImage = "postgres:16-alpine"

const testJWTSecret = "integration-test-signing-key-0123456789-abcdef"

// Fixed IDs, so a failure message names a recognisable row.
const (
	adminID              = "00000000-0000-4000-8000-00000000000a"
	interviewerID        = "00000000-0000-4000-8000-00000000000b"
	outsideInterviewerID = "00000000-0000-4000-8000-00000000000c"
	candidateID          = "00000000-0000-4000-8000-00000000000d"
	otherCandidateID     = "00000000-0000-4000-8000-00000000000e"
	ghostID              = "00000000-0000-4000-8000-00000000000f" // valid token, no row

	// postID is open, and the applications below are for it. draftPostID is
	// a draft, which only an admin may see.
	postID      = "11111111-0000-4000-8000-000000000001"
	draftPostID = "11111111-0000-4000-8000-000000000002"

	// readyDraftID is a draft with a question and a rubric, which the admin
	// opens. closablePostID is open, and the admin closes it. Each is touched
	// by one cell only, so the order the cells run in does not matter.
	readyDraftID   = "11111111-0000-4000-8000-000000000003"
	closablePostID = "11111111-0000-4000-8000-000000000004"

	// formPostID is a draft with three questions, which the admin edits,
	// reorders and then deletes from, in that order.
	formPostID = "11111111-0000-4000-8000-000000000005"
	formQ1     = "33333333-0000-4000-8000-000000000001"
	formQ2     = "33333333-0000-4000-8000-000000000002"
	formQ3     = "33333333-0000-4000-8000-000000000003"

	// missingPost is a well-formed UUID that no row has.
	missingPost = "11111111-0000-4000-8000-0000000000ff"

	// appA is the candidate's own application, and the interviewer is
	// assigned to it. appB belongs to the other candidate, and nobody is
	// assigned to it.
	appA = "22222222-0000-4000-8000-00000000000a"
	appB = "22222222-0000-4000-8000-00000000000b"

	// missingApp is a well-formed UUID that no row has.
	missingApp = "22222222-0000-4000-8000-0000000000ff"
)

// startPostgres runs a PostgreSQL container with every migration applied, and
// returns a pool connected to it. The container is removed when the test ends.
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	// The up migrations, in order. Their names are zero-padded, so sorting the
	// strings sorts them numerically.
	migrations, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatalf("find migrations: %v (found %d)", err, len(migrations))
	}
	sort.Strings(migrations)

	ctr, err := postgres.Run(ctx, postgresImage,
		postgres.WithDatabase("hiredesk"),
		postgres.WithUsername("hiredesk"),
		postgres.WithPassword("hiredesk"),
		// Copied into /docker-entrypoint-initdb.d, which the image runs with
		// psql, in order, the first time the data directory is created.
		postgres.WithOrderedInitScripts(migrations...),
		// Waits for the server's second "ready" log line (the image restarts
		// it once after running the init scripts) and for the port to accept
		// connections from this host.
		postgres.BasicWaitStrategies(),
	)
	// Registered before the error check, as testcontainers documents: a
	// container that started and then failed its wait strategy still exists
	// and must still be removed.
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// seed inserts the people and applications the matrix refers to.
func seed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	// One batch of statements in one round trip; Exec with no arguments uses
	// PostgreSQL's simple protocol, which accepts several statements at once.
	const fixtures = `
		INSERT INTO users (id, email, password_hash, full_name, role) VALUES
			('` + adminID + `',              'admin@test',              'unused', 'Admin',               'admin'),
			('` + interviewerID + `',        'interviewer@test',        'unused', 'Assigned Interviewer', 'interviewer'),
			('` + outsideInterviewerID + `', 'outside@test',            'unused', 'Outside Interviewer',  'interviewer'),
			('` + candidateID + `',          'candidate@test',          'unused', 'Candidate',            'candidate'),
			('` + otherCandidateID + `',     'other@test',              'unused', 'Other Candidate',      'candidate');

		INSERT INTO job_posts (id, title, status, created_by) VALUES
			('` + postID + `',      'Backend Engineer', 'open',  '` + adminID + `'),
			('` + draftPostID + `', 'Data Analyst',     'draft', '` + adminID + `'),
			('` + readyDraftID + `', 'Ready to Open',   'draft', '` + adminID + `'),
			('` + closablePostID + `', 'To Be Closed',  'open',  '` + adminID + `'),
			('` + formPostID + `',     'Form Building', 'draft', '` + adminID + `');

		INSERT INTO questions (id, job_post_id, prompt, kind, position) VALUES
			('` + formQ1 + `', '` + formPostID + `', 'First',  'text', 1),
			('` + formQ2 + `', '` + formPostID + `', 'Second', 'text', 2),
			('` + formQ3 + `', '` + formPostID + `', 'Third',  'text', 3);

		INSERT INTO questions (job_post_id, prompt, kind, position)
		VALUES ('` + readyDraftID + `', 'Why this role?', 'text', 1);

		INSERT INTO rubric_criteria (job_post_id, label, kind, weight)
		VALUES ('` + readyDraftID + `', 'Go experience', 'experience_years', 1);

		INSERT INTO applications (id, job_post_id, candidate_id, cv_url, submitted_at) VALUES
			('` + appA + `', '` + postID + `', '` + candidateID + `',      'https://cv.test/a.pdf', '2026-03-02T09:00:00Z'),
			('` + appB + `', '` + postID + `', '` + otherCandidateID + `', 'https://cv.test/b.pdf', '2026-03-01T09:00:00Z');

		INSERT INTO interviewer_assignments (application_id, interviewer_id, assigned_by)
		VALUES ('` + appA + `', '` + interviewerID + `', '` + adminID + `');`

	if _, err := pool.Exec(context.Background(), fixtures); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// newRouter builds the production router over real stores on pool.
func newRouter(t *testing.T, pool *pgxpool.Pool, tokens *auth.TokenManager) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)

	users := store.NewUserStore(pool)
	router, err := api.NewRouter(api.Deps{
		Auth:         api.NewAuthHandler(users, store.NewTokenStore(pool), tokens),
		Applications: api.NewApplicationHandler(store.NewApplicationStore(pool)),
		JobPosts:     api.NewJobPostHandler(store.NewJobPostStore(pool)),
		Questions:    api.NewQuestionHandler(store.NewQuestionStore(pool)),
		Listings:     api.NewJobListingHandler(store.NewJobPostStore(pool)),
		Tokens:       tokens,
		Users:        users,
		DB:           pool,
		PingTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	return router
}

// endpoint is one protected route, with a concrete path to request.
type endpoint struct {
	name   string
	method string
	route  string // the pattern registered with Gin, e.g. /v1/applications/:id
	path   string
	body   string // sent as JSON when not empty
	appID  string // for single-application endpoints: which one is requested
	postID string // for single-post endpoints that answer with the post

	// conflictCode is the error code a 409 from this endpoint must carry.
	conflictCode string

	// public marks an endpoint whose 200 must be byte-for-byte what an
	// anonymous caller gets: who is asking must change nothing.
	public  bool
	isList  bool
	cellFor func(callerRow) int
}

// callerRow is one row of the matrix: who is calling, and the status each
// endpoint must answer them with.
type callerRow struct {
	caller string
	userID string // empty: no Authorization header
	role   string // the role put in the token

	me, list, getA, getB, getMissing, getMalformed int

	// The job post endpoints. Reading an open post is allowed to every role
	// and a draft to admins only; writing is for admins only, and is refused
	// before the ID is looked at.
	postOpen, postDraft, postMissing, postMalformed int
	postCreate, postPatch, postPatchMalformed       int

	// The lifecycle routes: opening a ready draft, opening a draft with no
	// questions or rubric, and closing an open post.
	postOpenReady, postOpenUnready, postClose int

	// The application form: list, add, add to an open post (locked), edit,
	// reorder and delete.
	qList, qCreate, qCreateLocked, qUpdate, qReorder, qDelete int

	// The public listing, which every caller gets, identically.
	listing int

	// listed is exactly which applications GET /v1/applications must return,
	// for callers that get a 200.
	listed []string
}

// TestAuthorizationMatrix sends every caller to every protected endpoint
// against a real database, and checks the status, that a refusal is
// indistinguishable from a missing record, that lists contain exactly the
// right rows, and that the list and single-record rules agree.
func TestAuthorizationMatrix(t *testing.T) {
	pool := startPostgres(t)
	seed(t, pool)

	tokens := auth.NewTokenManager(testJWTSecret)
	router := newRouter(t, pool, tokens)

	const (
		ok   = http.StatusOK
		made = http.StatusCreated
		bad  = http.StatusBadRequest
		unau = http.StatusUnauthorized
		forb = http.StatusForbidden
		conf = http.StatusConflict
		gone = http.StatusNoContent
		nf   = http.StatusNotFound
	)

	// The matrix. Read across: what one caller gets from every endpoint.
	matrix := []callerRow{
		{caller: "anonymous", me: unau, list: unau, getA: unau, getB: unau, getMissing: unau, getMalformed: unau,
			postOpen: unau, postDraft: unau, postMissing: unau, postMalformed: unau,
			postCreate: unau, postPatch: unau, postPatchMalformed: unau,
			postOpenReady: unau, postOpenUnready: unau, postClose: unau,
			qList: unau, qCreate: unau, qCreateLocked: unau, qUpdate: unau, qReorder: unau, qDelete: unau,
			listing: ok},
		{caller: "ghost (token for a deleted user)", userID: ghostID, role: domain.RoleAdmin,
			me: unau, list: unau, getA: unau, getB: unau, getMissing: unau, getMalformed: unau,
			postOpen: unau, postDraft: unau, postMissing: unau, postMalformed: unau,
			postCreate: unau, postPatch: unau, postPatchMalformed: unau,
			postOpenReady: unau, postOpenUnready: unau, postClose: unau,
			qList: unau, qCreate: unau, qCreateLocked: unau, qUpdate: unau, qReorder: unau, qDelete: unau,
			listing: ok},

		{caller: "admin", userID: adminID, role: domain.RoleAdmin,
			me: ok, list: ok, getA: ok, getB: ok, getMissing: nf, getMalformed: nf,
			postOpen: ok, postDraft: ok, postMissing: nf, postMalformed: bad,
			postCreate: made, postPatch: ok, postPatchMalformed: bad,
			postOpenReady: ok, postOpenUnready: conf, postClose: ok,
			qList: ok, qCreate: made, qCreateLocked: conf, qUpdate: ok, qReorder: ok, qDelete: gone,
			listing: ok,
			listed:  []string{appA, appB}},
		{caller: "interviewer assigned to A", userID: interviewerID, role: domain.RoleInterviewer,
			me: ok, list: ok, getA: ok, getB: nf, getMissing: nf, getMalformed: nf,
			postOpen: ok, postDraft: nf, postMissing: nf, postMalformed: bad,
			postCreate: forb, postPatch: forb, postPatchMalformed: forb,
			postOpenReady: forb, postOpenUnready: forb, postClose: forb,
			qList: forb, qCreate: forb, qCreateLocked: forb, qUpdate: forb, qReorder: forb, qDelete: forb,
			listing: ok,
			listed:  []string{appA}},
		{caller: "interviewer assigned to nothing", userID: outsideInterviewerID, role: domain.RoleInterviewer,
			me: ok, list: ok, getA: nf, getB: nf, getMissing: nf, getMalformed: nf,
			postOpen: ok, postDraft: nf, postMissing: nf, postMalformed: bad,
			postCreate: forb, postPatch: forb, postPatchMalformed: forb,
			postOpenReady: forb, postOpenUnready: forb, postClose: forb,
			qList: forb, qCreate: forb, qCreateLocked: forb, qUpdate: forb, qReorder: forb, qDelete: forb,
			listing: ok,
			listed:  []string{}},
		{caller: "candidate who owns A", userID: candidateID, role: domain.RoleCandidate,
			me: ok, list: ok, getA: ok, getB: nf, getMissing: nf, getMalformed: nf,
			postOpen: ok, postDraft: nf, postMissing: nf, postMalformed: bad,
			postCreate: forb, postPatch: forb, postPatchMalformed: forb,
			postOpenReady: forb, postOpenUnready: forb, postClose: forb,
			qList: forb, qCreate: forb, qCreateLocked: forb, qUpdate: forb, qReorder: forb, qDelete: forb,
			listing: ok,
			listed:  []string{appA}},
		{caller: "candidate who owns B", userID: otherCandidateID, role: domain.RoleCandidate,
			me: ok, list: ok, getA: nf, getB: ok, getMissing: nf, getMalformed: nf,
			postOpen: ok, postDraft: nf, postMissing: nf, postMalformed: bad,
			postCreate: forb, postPatch: forb, postPatchMalformed: forb,
			postOpenReady: forb, postOpenUnready: forb, postClose: forb,
			qList: forb, qCreate: forb, qCreateLocked: forb, qUpdate: forb, qReorder: forb, qDelete: forb,
			listing: ok,
			listed:  []string{appB}},

		// The token claims admin; the database says candidate. The database
		// wins on every endpoint, including the list's choice of query.
		{caller: "candidate with a forged admin claim", userID: candidateID, role: domain.RoleAdmin,
			me: ok, list: ok, getA: ok, getB: nf, getMissing: nf, getMalformed: nf,
			postOpen: ok, postDraft: nf, postMissing: nf, postMalformed: bad,
			postCreate: forb, postPatch: forb, postPatchMalformed: forb,
			postOpenReady: forb, postOpenUnready: forb, postClose: forb,
			qList: forb, qCreate: forb, qCreateLocked: forb, qUpdate: forb, qReorder: forb, qDelete: forb,
			listing: ok,
			listed:  []string{appA}},
	}

	endpoints := []endpoint{
		{name: "GET /v1/me", method: http.MethodGet, route: "/v1/me", path: "/v1/me",
			cellFor: func(r callerRow) int { return r.me }},
		{name: "GET /v1/applications", method: http.MethodGet, route: "/v1/applications", path: "/v1/applications",
			isList: true, cellFor: func(r callerRow) int { return r.list }},
		{name: "GET application A", method: http.MethodGet, route: "/v1/applications/:id", path: "/v1/applications/" + appA,
			appID: appA, cellFor: func(r callerRow) int { return r.getA }},
		{name: "GET application B", method: http.MethodGet, route: "/v1/applications/:id", path: "/v1/applications/" + appB,
			appID: appB, cellFor: func(r callerRow) int { return r.getB }},
		{name: "GET missing application", method: http.MethodGet, route: "/v1/applications/:id", path: "/v1/applications/" + missingApp,
			appID: missingApp, cellFor: func(r callerRow) int { return r.getMissing }},
		{name: "GET malformed ID", method: http.MethodGet, route: "/v1/applications/:id", path: "/v1/applications/not-a-uuid",
			appID: "not-a-uuid", cellFor: func(r callerRow) int { return r.getMalformed }},

		{name: "GET open job post", method: http.MethodGet, route: "/v1/job-posts/:id", path: "/v1/job-posts/" + postID,
			postID: postID, cellFor: func(r callerRow) int { return r.postOpen }},
		{name: "GET draft job post", method: http.MethodGet, route: "/v1/job-posts/:id", path: "/v1/job-posts/" + draftPostID,
			postID: draftPostID, cellFor: func(r callerRow) int { return r.postDraft }},
		{name: "GET missing job post", method: http.MethodGet, route: "/v1/job-posts/:id", path: "/v1/job-posts/" + missingPost,
			cellFor: func(r callerRow) int { return r.postMissing }},
		{name: "GET malformed job post ID", method: http.MethodGet, route: "/v1/job-posts/:id", path: "/v1/job-posts/not-a-uuid",
			cellFor: func(r callerRow) int { return r.postMalformed }},
		{name: "POST job post", method: http.MethodPost, route: "/v1/job-posts", path: "/v1/job-posts",
			body: `{"title":"Created by the matrix"}`, cellFor: func(r callerRow) int { return r.postCreate }},
		// The patch sets the description, which nothing else in the matrix
		// reads, so the order the cells run in does not matter.
		{name: "PATCH job post", method: http.MethodPatch, route: "/v1/job-posts/:id", path: "/v1/job-posts/" + draftPostID,
			body: `{"description":"Patched by the matrix"}`, postID: draftPostID, cellFor: func(r callerRow) int { return r.postPatch }},
		{name: "PATCH malformed job post ID", method: http.MethodPatch, route: "/v1/job-posts/:id", path: "/v1/job-posts/not-a-uuid",
			body: `{"description":"x"}`, cellFor: func(r callerRow) int { return r.postPatchMalformed }},

		{name: "POST open a ready draft", method: http.MethodPost, route: "/v1/job-posts/:id/open", path: "/v1/job-posts/" + readyDraftID + "/open",
			postID: readyDraftID, cellFor: func(r callerRow) int { return r.postOpenReady }},
		{name: "POST open an unready draft", method: http.MethodPost, route: "/v1/job-posts/:id/open", path: "/v1/job-posts/" + draftPostID + "/open",
			conflictCode: "not_ready", cellFor: func(r callerRow) int { return r.postOpenUnready }},
		{name: "POST close an open post", method: http.MethodPost, route: "/v1/job-posts/:id/close", path: "/v1/job-posts/" + closablePostID + "/close",
			postID: closablePostID, cellFor: func(r callerRow) int { return r.postClose }},

		// Each response is compared with an anonymous request made at the
		// same moment, so the admin's cells opening and closing posts do not
		// disturb it.
		{name: "GET public listing", method: http.MethodGet, route: "/v1/job-posts", path: "/v1/job-posts?limit=100",
			public: true, cellFor: func(r callerRow) int { return r.listing }},

		// The form endpoints run in this order for the admin: the reorder
		// must name all three questions, so it comes before the delete.
		{name: "GET questions", method: http.MethodGet, route: "/v1/job-posts/:id/questions", path: "/v1/job-posts/" + formPostID + "/questions",
			cellFor: func(r callerRow) int { return r.qList }},
		{name: "POST question to a draft", method: http.MethodPost, route: "/v1/job-posts/:id/questions", path: "/v1/job-posts/" + draftPostID + "/questions",
			body: `{"prompt":"Added by the matrix","kind":"text"}`, cellFor: func(r callerRow) int { return r.qCreate }},
		{name: "POST question to an open post", method: http.MethodPost, route: "/v1/job-posts/:id/questions", path: "/v1/job-posts/" + postID + "/questions",
			body: `{"prompt":"Too late","kind":"text"}`, conflictCode: "form_locked", cellFor: func(r callerRow) int { return r.qCreateLocked }},
		{name: "PATCH question", method: http.MethodPatch, route: "/v1/job-posts/:id/questions/:questionID", path: "/v1/job-posts/" + formPostID + "/questions/" + formQ1,
			body: `{"prompt":"First, reworded"}`, cellFor: func(r callerRow) int { return r.qUpdate }},
		{name: "PUT question order", method: http.MethodPut, route: "/v1/job-posts/:id/questions/order", path: "/v1/job-posts/" + formPostID + "/questions/order",
			body: `{"question_ids":["` + formQ3 + `","` + formQ1 + `","` + formQ2 + `"]}`, cellFor: func(r callerRow) int { return r.qReorder }},
		{name: "DELETE question", method: http.MethodDelete, route: "/v1/job-posts/:id/questions/:questionID", path: "/v1/job-posts/" + formPostID + "/questions/" + formQ3,
			cellFor: func(r callerRow) int { return r.qDelete }},
	}

	assertEveryProtectedRouteIsInTheMatrix(t, router, endpoints)

	send := func(method, path, header, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		router.ServeHTTP(rec, req)
		return rec
	}

	for _, row := range matrix {
		header := ""
		if row.userID != "" {
			token, err := tokens.NewAccessToken(row.userID, row.role)
			if err != nil {
				t.Fatalf("sign token for %s: %v", row.caller, err)
			}
			header = "Bearer " + token
		}

		// What this caller gets for an ID that certainly does not exist. Every
		// refusal below must be byte-for-byte the same.
		absent := send(http.MethodGet, "/v1/applications/"+missingApp, header, "")

		for _, ep := range endpoints {
			want := ep.cellFor(row)

			t.Run(row.caller+" → "+ep.name, func(t *testing.T) {
				rec := send(ep.method, ep.path, header, ep.body)

				if rec.Code != want {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body)
				}

				switch {
				case want == unau:
					if got := errorCode(t, rec.Body.Bytes()); got != "unauthenticated" {
						t.Errorf("code = %q, want unauthenticated", got)
					}

				case want == forb:
					if got := errorCode(t, rec.Body.Bytes()); got != "forbidden" {
						t.Errorf("code = %q, want forbidden", got)
					}

				case want == bad:
					if got := errorCode(t, rec.Body.Bytes()); got != "invalid_id" {
						t.Errorf("code = %q, want invalid_id", got)
					}

				case want == conf:
					// Refused by a domain rule, never a 500.
					if got := errorCode(t, rec.Body.Bytes()); got != ep.conflictCode {
						t.Errorf("code = %q, want %q", got, ep.conflictCode)
					}

				case want == gone:
					if rec.Body.Len() != 0 {
						t.Errorf("204 with a body: %s", rec.Body)
					}

				case want == nf:
					// docs/decisions.md D1: not yours and not there look the same.
					if rec.Body.String() != absent.Body.String() {
						t.Errorf("refusal differs from absence:\n got:    %s\n absent: %s", rec.Body, absent.Body)
					}

				case ep.public:
					anonymous := send(ep.method, ep.path, "", "")
					if rec.Body.String() != anonymous.Body.String() {
						t.Errorf("differs from the anonymous response:\n got:       %s\n anonymous: %s", rec.Body, anonymous.Body)
					}
					// Drafts no cell ever opens. readyDraftID is left out: the
					// admin's row opens it, after which it belongs here.
					for _, hidden := range []string{draftPostID, formPostID} {
						if strings.Contains(rec.Body.String(), hidden) {
							t.Errorf("the public listing shows draft %s", hidden)
						}
					}

				case ep.isList:
					if got := listedIDs(t, rec.Body.Bytes()); !sameSet(got, row.listed) {
						t.Errorf("listed %v, want exactly %v", got, row.listed)
					}

				case ep.appID != "":
					if got := singleID(t, rec.Body.Bytes()); got != ep.appID {
						t.Errorf("returned application %q, want %q", got, ep.appID)
					}

				case ep.postID != "":
					if got := singleID(t, rec.Body.Bytes()); got != ep.postID {
						t.Errorf("returned job post %q, want %q", got, ep.postID)
					}
				}
			})
		}

		// The list query and CanViewApplication are two encodings of one rule.
		// If they drift, a caller sees a row in the list they cannot open, or
		// can open one the list hides.
		if row.list == ok {
			t.Run(row.caller+" → list agrees with single fetch", func(t *testing.T) {
				for _, id := range []string{appA, appB} {
					inList := slices.Contains(row.listed, id)
					opens := send(http.MethodGet, "/v1/applications/"+id, header, "").Code == ok
					if inList != opens {
						t.Errorf("application %s: in list = %v, opens = %v", id, inList, opens)
					}
				}
			})
		}
	}
}

// assertEveryProtectedRouteIsInTheMatrix fails if the router has a /v1 route,
// outside the public /v1/auth group, that no endpoint in the matrix requests.
// Adding an endpoint without adding it here turns this test red.
func assertEveryProtectedRouteIsInTheMatrix(t *testing.T, router *gin.Engine, endpoints []endpoint) {
	t.Helper()

	covered := map[string]bool{}
	for _, ep := range endpoints {
		covered[ep.method+" "+ep.route] = true
	}

	for _, r := range router.Routes() {
		if !strings.HasPrefix(r.Path, "/v1/") || strings.HasPrefix(r.Path, "/v1/auth/") {
			continue
		}
		if !covered[r.Method+" "+r.Path] {
			t.Errorf("%s %s is not in TestAuthorizationMatrix; add a column for it", r.Method, r.Path)
		}
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("body is not the error envelope: %v\n%s", err, body)
	}
	return envelope.Error.Code
}

func listedIDs(t *testing.T, body []byte) []string {
	t.Helper()

	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("body is not a list: %v\n%s", err, body)
	}
	if list.Data == nil {
		t.Fatalf("data is null or missing, want an array: %s", body)
	}

	ids := make([]string, 0, len(list.Data))
	for _, item := range list.Data {
		ids = append(ids, item.ID)
	}
	return ids
}

func singleID(t *testing.T, body []byte) string {
	t.Helper()

	var app struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &app); err != nil {
		t.Fatalf("body is not a single record: %v\n%s", err, body)
	}
	return app.ID
}

// sameSet reports whether a and b hold the same IDs, ignoring order, and with
// no ID repeated in a. A duplicate in the list would be a join bug.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA, sortedB := slices.Clone(a), slices.Clone(b)
	slices.Sort(sortedA)
	slices.Sort(sortedB)
	return slices.Equal(sortedA, sortedB) && len(slices.Compact(sortedA)) == len(a)
}
