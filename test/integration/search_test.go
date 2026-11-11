//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// searchFixture is the scoring post, a router over it, and an admin's
// Authorization header.
type searchFixture struct {
	pool   *pgxpool.Pool
	router *gin.Engine
	admin  string
	apps   *store.ApplicationStore
	n      int // candidates created so far, for unique e-mail addresses
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()

	pool := startPostgres(t)
	seedScoring(t, pool)

	tokens := auth.NewTokenManager(testJWTSecret)
	token, err := tokens.NewAccessToken(scoringAdminID, domain.RoleAdmin)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return &searchFixture{
		pool:   pool,
		router: newRouter(t, pool, tokens),
		admin:  "Bearer " + token,
		apps:   store.NewApplicationStore(pool),
	}
}

// apply submits an application from a new candidate called name, through the
// real submit path, with why as the answer to the post's text question. The
// other answers are scoredApplication's, so the application is scored.
func (f *searchFixture) apply(t *testing.T, name, why string) string {
	t.Helper()
	ctx := context.Background()
	f.n++

	var candidateID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, full_name, role)
		VALUES ($1, 'unused', $2, 'candidate') RETURNING id::text`,
		fmt.Sprintf("search-%d@test", f.n), name).Scan(&candidateID); err != nil {
		t.Fatalf("create candidate %s: %v", name, err)
	}

	in := scoredApplication()
	in.CandidateID = candidateID
	in.Answers[0].Answer = why
	app, err := f.apps.SubmitApplication(ctx, in)
	if err != nil {
		t.Fatalf("submit for %s: %v", name, err)
	}
	return app.ID
}

// searchPage is the part of the search response the tests read.
type searchPage struct {
	Total      int  `json:"total"`
	NextOffset *int `json:"next_offset"`
	Data       []struct {
		ApplicationID string   `json:"application_id"`
		CandidateName string   `json:"candidate_name"`
		Relevance     float64  `json:"relevance"`
		TotalEarned   *float64 `json:"total_earned"`
		Percentage    *float64 `json:"percentage"`
	} `json:"data"`
}

// search sends GET .../applications/search with rawQuery as the query string,
// exactly as given, so a test can send bytes url.Values would re-encode.
func (f *searchFixture) search(rawQuery string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/job-posts/"+subOpenPostID+"/applications/search?"+rawQuery, nil)
	req.Header.Set("Authorization", f.admin)
	f.router.ServeHTTP(rec, req)
	return rec
}

// searchOK searches for q, requires a 200, and decodes the page.
func (f *searchFixture) searchOK(t *testing.T, q string) searchPage {
	t.Helper()

	rec := f.search("q=" + url.QueryEscape(q))
	if rec.Code != http.StatusOK {
		t.Fatalf("search %q: status %d; body = %s", q, rec.Code, rec.Body)
	}
	var page searchPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("search %q: decode: %v", q, err)
	}
	return page
}

func (p searchPage) ids() []string {
	out := make([]string, 0, len(p.Data))
	for _, d := range p.Data {
		out = append(out, d.ApplicationID)
	}
	return out
}

// TestSearch_WholeWordOnly proves that full-text search matches words, not
// substrings: "go" finds the candidate who writes Go, and does not find the
// one who searched Google. ILIKE '%go%', run on the same rows, gets it wrong.
func TestSearch_WholeWordOnly(t *testing.T) {
	f := newSearchFixture(t)

	google := f.apply(t, "Gina", "I searched Google for the answer")
	gopher := f.apply(t, "Gus", "I write Go every day at work")

	page := f.searchOK(t, "go")
	if got := page.ids(); len(got) != 1 || got[0] != gopher {
		t.Fatalf("search go = %v, want only %s (Gus); Gina's %s must not match", got, gopher, google)
	}

	// The substring match this endpoint deliberately does not use: it
	// matches both, because "Google" contains the letters g-o.
	var substringMatches int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM applications
		WHERE job_post_id = $1::text::uuid AND search_document ILIKE '%go%'`,
		subOpenPostID).Scan(&substringMatches); err != nil {
		t.Fatalf("ILIKE: %v", err)
	}
	if substringMatches != 2 {
		t.Errorf("ILIKE '%%go%%' matched %d, expected 2 to show the contrast", substringMatches)
	}
}

// TestSearch_Stemming proves that a search matches other forms of the same
// word: "testing" and "tested" both reduce to the lexeme "test".
func TestSearch_Stemming(t *testing.T) {
	f := newSearchFixture(t)

	tested := f.apply(t, "Tess", "I tested the payment service thoroughly")
	f.apply(t, "Una", "I designed the database schema carefully")

	page := f.searchOK(t, "testing")
	if got := page.ids(); len(got) != 1 || got[0] != tested {
		t.Fatalf("search testing = %v, want only %s", got, tested)
	}
}

// TestSearch_HostileInput sends every awkward query there is through the
// real router to real PostgreSQL. None may produce a 500: each is either a
// search (200) or a refusal naming q (422).
func TestSearch_HostileInput(t *testing.T) {
	f := newSearchFixture(t)

	both := f.apply(t, "Pat", "I run Go services on Postgres in production")
	f.apply(t, "Quinn", "I only write Go, never touch databases")
	unicode := f.apply(t, "Zoë", "Mon résumé: 東京 での経験, Go et Postgres")

	var usersBefore int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&usersBefore); err != nil {
		t.Fatalf("count users: %v", err)
	}

	for _, tt := range []struct {
		name     string
		rawQuery string // sent exactly as written
		want     int
		matches  []string // for a 200: exactly these, in any order; nil means none
	}{
		{"empty", "q=", http.StatusUnprocessableEntity, nil},
		{"missing", "", http.StatusUnprocessableEntity, nil},
		{"whitespace only", "q=" + url.QueryEscape("   \t  "), http.StatusUnprocessableEntity, nil},
		{"punctuation only", "q=" + url.QueryEscape(`!!! ??? ()&|:*'"\`), http.StatusOK, nil},
		{"stop words only", "q=" + url.QueryEscape("the and a"), http.StatusOK, nil},
		{"tsquery operators", "q=" + url.QueryEscape("go & postgres"), http.StatusOK, []string{both, unicode}},
		{"broken tsquery syntax", "q=" + url.QueryEscape("go & & (postgres"), http.StatusOK, []string{both, unicode}},
		{"too long", "q=" + strings.Repeat("go+", domain.MaxSearchQueryChars), http.StatusUnprocessableEntity, nil},
		{"unicode", "q=" + url.QueryEscape("résumé"), http.StatusOK, []string{unicode}},
		{"CJK", "q=" + url.QueryEscape("東京"), http.StatusOK, []string{unicode}},
		{"SQL injection", "q=" + url.QueryEscape("'; DROP TABLE users; --"), http.StatusOK, nil},
		{"invalid UTF-8", "q=%FF%FEgo", http.StatusUnprocessableEntity, nil},
		{"NUL byte", "q=go%00postgres", http.StatusUnprocessableEntity, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := f.search(tt.rawQuery)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.want, rec.Body)
			}
			if tt.want != http.StatusOK {
				if got := errorCode(t, rec.Body.Bytes()); got != "validation_failed" {
					t.Errorf("code = %q, want validation_failed", got)
				}
				return
			}
			var page searchPage
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := page.ids(); !sameSet(got, tt.matches) || page.Total != len(tt.matches) {
				t.Errorf("matched %v (total %d), want %v", got, page.Total, tt.matches)
			}
		})
	}

	// The injection was searched for as three words, not run.
	var usersAfter int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&usersAfter); err != nil {
		t.Fatalf("users table after the injection attempt: %v", err)
	}
	if usersAfter != usersBefore {
		t.Errorf("users: %d before, %d after", usersBefore, usersAfter)
	}
}

// TestSearch_RankedPagedAndScored checks the order, the paging and the
// score totals in each result.
func TestSearch_RankedPagedAndScored(t *testing.T) {
	f := newSearchFixture(t)

	// Postgres three times beats Postgres once, whoever applied first.
	once := f.apply(t, "Olu", "I have used Postgres for reporting jobs")
	thrice := f.apply(t, "Thea", "Postgres replication, Postgres tuning and Postgres backups are my daily work")
	f.apply(t, "Nobody", "I prefer document databases for everything")

	page := f.searchOK(t, "postgres")
	if got := page.ids(); len(got) != 2 || got[0] != thrice || got[1] != once {
		t.Fatalf("order = %v, want [%s %s]", got, thrice, once)
	}
	if page.Data[0].Relevance <= page.Data[1].Relevance {
		t.Errorf("relevance %v then %v: not descending", page.Data[0].Relevance, page.Data[1].Relevance)
	}
	for _, d := range page.Data {
		if d.TotalEarned == nil || d.Percentage == nil {
			t.Errorf("%s (%s) has no score totals", d.ApplicationID, d.CandidateName)
		}
	}

	rec := f.search("q=postgres&limit=1")
	var first searchPage
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("page 1: %d %s", rec.Code, rec.Body)
	}
	if first.Total != 2 || len(first.Data) != 1 || first.NextOffset == nil || *first.NextOffset != 1 {
		t.Fatalf("page 1 = %s", rec.Body)
	}
	rec = f.search("q=postgres&limit=1&offset=1")
	var second searchPage
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("page 2: %d %s", rec.Code, rec.Body)
	}
	if len(second.Data) != 1 || second.Data[0].ApplicationID != once || second.NextOffset != nil {
		t.Errorf("page 2 = %s", rec.Body)
	}

	missing := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/job-posts/"+missingPost+"/applications/search?q=go", nil)
	req.Header.Set("Authorization", f.admin)
	f.router.ServeHTTP(missing, req)
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing post: status %d, want 404", missing.Code)
	}
}

// searchDocumentOf reads the stored search document of one application.
func searchDocumentOf(t *testing.T, pool *pgxpool.Pool, appID string) string {
	t.Helper()

	var doc string
	if err := pool.QueryRow(context.Background(),
		`SELECT search_document FROM applications WHERE id = $1::text::uuid`, appID).Scan(&doc); err != nil {
		t.Fatalf("read search document: %v", err)
	}
	return doc
}

// TestSearchDocumentSurvivesEveryWrite submits an application, checks the
// document submission built, then runs every other write path an
// application has and checks that none of them changes it.
func TestSearchDocumentSurvivesEveryWrite(t *testing.T) {
	f := newSearchFixture(t)
	ctx := context.Background()

	id := f.apply(t, "Wren", "I built a REST API in Go and tested it")

	// Only the text answer: the number and boolean answers are left out.
	const want = "I built a REST API in Go and tested it"
	if got := searchDocumentOf(t, f.pool, id); got != want {
		t.Fatalf("after submit: %q, want %q", got, want)
	}

	if _, err := f.apps.RescoreApplication(ctx, id, scoringAdminID); err != nil {
		t.Fatalf("rescore: %v", err)
	}
	if got := searchDocumentOf(t, f.pool, id); got != want {
		t.Errorf("after rescore: %q", got)
	}

	if _, err := f.apps.RescoreJobPost(ctx, subOpenPostID, scoringAdminID); err != nil {
		t.Fatalf("rescore all: %v", err)
	}
	if got := searchDocumentOf(t, f.pool, id); got != want {
		t.Errorf("after rescore-all: %q", got)
	}

	if _, err := f.apps.TransitionApplication(ctx, id, domain.StatusShortlisted, scoringAdminID); err != nil {
		t.Fatalf("shortlist: %v", err)
	}
	if got := searchDocumentOf(t, f.pool, id); got != want {
		t.Errorf("after a status change: %q", got)
	}

	if got := f.searchOK(t, "tested").ids(); len(got) != 1 || got[0] != id {
		t.Errorf("still found after every write: %v", got)
	}
}

// TestRebuildSearchDocumentsAgreesWithSubmit holds the Go rule and the SQL
// rule together: on documents that submission built, the repair finds
// nothing to rewrite. Then it damages two documents and checks that the
// repair restores exactly those, and that search finds them again.
func TestRebuildSearchDocumentsAgreesWithSubmit(t *testing.T) {
	f := newSearchFixture(t)
	ctx := context.Background()

	healthy := f.apply(t, "Hana", "Kubernetes operators written in Go")
	blanked := f.apply(t, "Bo", "Event sourcing with Postgres and Go")
	garbled := f.apply(t, "Gil", "Observability with OpenTelemetry")
	// Long enough to be cut, in multi-byte characters, so the two rules must
	// agree on where the cut falls.
	long := f.apply(t, "Lan", strings.Repeat("経験 ", domain.MaxSearchDocumentChars))

	rewritten, total, err := f.apps.RebuildSearchDocuments(ctx)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rewritten != 0 || total != 4 {
		t.Fatalf("rebuild of healthy documents: %d of %d rewritten, want 0 of 4", rewritten, total)
	}
	if got := len([]rune(searchDocumentOf(t, f.pool, long))); got != domain.MaxSearchDocumentChars {
		t.Errorf("long document is %d characters, want %d", got, domain.MaxSearchDocumentChars)
	}

	// The drift the repair exists for.
	if _, err = f.pool.Exec(ctx, `UPDATE applications SET search_document = '' WHERE id = $1::text::uuid`, blanked); err != nil {
		t.Fatalf("blank: %v", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE applications SET search_document = 'nonsense' WHERE id = $1::text::uuid`, garbled); err != nil {
		t.Fatalf("garble: %v", err)
	}
	if got := f.searchOK(t, "sourcing").ids(); len(got) != 0 {
		t.Fatalf("a blanked document is still found: %v", got)
	}

	rewritten, _, err = f.apps.RebuildSearchDocuments(ctx)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rewritten != 2 {
		t.Errorf("rewrote %d, want the 2 damaged documents", rewritten)
	}
	if got := f.searchOK(t, "sourcing").ids(); len(got) != 1 || got[0] != blanked {
		t.Errorf("after repair, sourcing = %v, want %s", got, blanked)
	}
	if got := searchDocumentOf(t, f.pool, garbled); got != "Observability with OpenTelemetry" {
		t.Errorf("garbled document after repair: %q", got)
	}
	if got := searchDocumentOf(t, f.pool, healthy); got != "Kubernetes operators written in Go" {
		t.Errorf("healthy document changed: %q", got)
	}
}
