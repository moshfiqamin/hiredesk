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

	postID = "11111111-0000-4000-8000-000000000001"

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

		INSERT INTO job_posts (id, title, status, created_by)
		VALUES ('` + postID + `', 'Backend Engineer', 'open', '` + adminID + `');

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
	name    string
	method  string
	route   string // the pattern registered with Gin, e.g. /v1/applications/:id
	path    string
	appID   string // for single-application endpoints: which one is requested
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
		unau = http.StatusUnauthorized
		nf   = http.StatusNotFound
	)

	// The matrix. Read across: what one caller gets from every endpoint.
	matrix := []callerRow{
		{caller: "anonymous", me: unau, list: unau, getA: unau, getB: unau, getMissing: unau, getMalformed: unau},
		{caller: "ghost (token for a deleted user)", userID: ghostID, role: domain.RoleAdmin,
			me: unau, list: unau, getA: unau, getB: unau, getMissing: unau, getMalformed: unau},

		{caller: "admin", userID: adminID, role: domain.RoleAdmin,
			me: ok, list: ok, getA: ok, getB: ok, getMissing: nf, getMalformed: nf,
			listed: []string{appA, appB}},
		{caller: "interviewer assigned to A", userID: interviewerID, role: domain.RoleInterviewer,
			me: ok, list: ok, getA: ok, getB: nf, getMissing: nf, getMalformed: nf,
			listed: []string{appA}},
		{caller: "interviewer assigned to nothing", userID: outsideInterviewerID, role: domain.RoleInterviewer,
			me: ok, list: ok, getA: nf, getB: nf, getMissing: nf, getMalformed: nf,
			listed: []string{}},
		{caller: "candidate who owns A", userID: candidateID, role: domain.RoleCandidate,
			me: ok, list: ok, getA: ok, getB: nf, getMissing: nf, getMalformed: nf,
			listed: []string{appA}},
		{caller: "candidate who owns B", userID: otherCandidateID, role: domain.RoleCandidate,
			me: ok, list: ok, getA: nf, getB: ok, getMissing: nf, getMalformed: nf,
			listed: []string{appB}},

		// The token claims admin; the database says candidate. The database
		// wins on every endpoint, including the list's choice of query.
		{caller: "candidate with a forged admin claim", userID: candidateID, role: domain.RoleAdmin,
			me: ok, list: ok, getA: ok, getB: nf, getMissing: nf, getMalformed: nf,
			listed: []string{appA}},
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
	}

	assertEveryProtectedRouteIsInTheMatrix(t, router, endpoints)

	send := func(method, path, header string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
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
		absent := send(http.MethodGet, "/v1/applications/"+missingApp, header)

		for _, ep := range endpoints {
			want := ep.cellFor(row)

			t.Run(row.caller+" → "+ep.name, func(t *testing.T) {
				rec := send(ep.method, ep.path, header)

				if rec.Code != want {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body)
				}

				switch {
				case want == unau:
					if got := errorCode(t, rec.Body.Bytes()); got != "unauthenticated" {
						t.Errorf("code = %q, want unauthenticated", got)
					}

				case want == nf:
					// docs/decisions.md D1: not yours and not there look the same.
					if rec.Body.String() != absent.Body.String() {
						t.Errorf("refusal differs from absence:\n got:    %s\n absent: %s", rec.Body, absent.Body)
					}

				case ep.isList:
					if got := listedIDs(t, rec.Body.Bytes()); !sameSet(got, row.listed) {
						t.Errorf("listed %v, want exactly %v", got, row.listed)
					}

				case ep.appID != "":
					if got := singleID(t, rec.Body.Bytes()); got != ep.appID {
						t.Errorf("returned application %q, want %q", got, ep.appID)
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
					opens := send(http.MethodGet, "/v1/applications/"+id, header).Code == ok
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
		t.Fatalf("body is not an application: %v\n%s", err, body)
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
