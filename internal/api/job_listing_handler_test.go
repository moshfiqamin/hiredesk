package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// GetOpenJobListing on the fake finds an open post by ID; anything else is
// not found, as the SQL's status filter makes it.
func (f *fakeJobPosts) GetOpenJobListing(_ context.Context, id string) (*domain.JobListingDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.byID[id]
	if !ok || p.Status != domain.JobStatusOpen {
		return nil, domain.ErrNotFound
	}
	d := &domain.JobListingDetail{
		JobListing: domain.JobListing{ID: p.ID, Title: p.Title, Location: p.Location, EmploymentType: p.EmploymentType,
			RemainingVacancies: p.Vacancies, CreatedAt: p.CreatedAt},
		Description: p.Description, Questions: p.Questions,
	}
	if extra, ok := f.listings[p.ID]; ok {
		d.SkillTags, d.RemainingVacancies = extra.SkillTags, extra.RemainingVacancies
	}
	return d, nil
}

// ListOpenJobPosts on the fake applies the same keyset rule as the SQL, over
// the fake's open posts, so the handler's paging can be tested end to end.
// Remaining vacancies and tags come from the listings field when set.
func (f *fakeJobPosts) ListOpenJobPosts(_ context.Context, after *domain.ListingPosition, limit int) ([]domain.JobListing, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.lastLimit, f.lastAfter = limit, after
	if f.failWith != nil {
		return nil, false, f.failWith
	}

	var open []domain.JobListing
	for _, p := range f.byID {
		if p.Status != domain.JobStatusOpen {
			continue
		}
		l := domain.JobListing{
			ID: p.ID, Title: p.Title, Location: p.Location, EmploymentType: p.EmploymentType,
			RemainingVacancies: p.Vacancies, CreatedAt: p.CreatedAt,
		}
		if extra, ok := f.listings[p.ID]; ok {
			l.SkillTags, l.RemainingVacancies = extra.SkillTags, extra.RemainingVacancies
		}
		open = append(open, l)
	}

	// created_at DESC, id DESC, as the SQL orders.
	slices.SortFunc(open, func(a, b domain.JobListing) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})

	var page []domain.JobListing
	for _, l := range open {
		if after == nil || sortsAfter(l, *after) {
			page = append(page, l)
		}
	}

	more := len(page) > limit
	if more {
		page = page[:limit]
	}
	return page, more, nil
}

// sortsAfter is the SQL's (created_at, id) < ($1, $2): true when l comes
// later than p in a newest-first listing.
func sortsAfter(l domain.JobListing, p domain.ListingPosition) bool {
	if l.CreatedAt.Equal(p.CreatedAt) {
		return l.ID < p.ID
	}
	return l.CreatedAt.Before(p.CreatedAt)
}

// listingFixture is a test server whose fake holds n open posts, a draft and
// a closed post. Open post i has created_at base minus i minutes, so post 0
// is the newest.
func newListingFixture(t *testing.T, n int) *postFixture {
	t.Helper()

	f := newPostFixture(t)
	f.posts.byID = map[string]domain.JobPost{}

	base := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("ffffffff-0000-4000-8000-%012d", i)
		f.posts.byID[id] = domain.JobPost{
			ID: id, Title: fmt.Sprintf("Open %d", i), Vacancies: 2, Status: domain.JobStatusOpen,
			CreatedAt: base.Add(-time.Duration(i) * time.Minute),
		}
	}
	// Newer than every open post, so a leak would be at the top of page 1.
	for _, status := range []string{domain.JobStatusDraft, domain.JobStatusClosed} {
		id := "ffffffff-1111-4000-8000-0000000000" + status[:2]
		f.posts.byID[id] = domain.JobPost{ID: id, Title: "Hidden " + status, Vacancies: 1, Status: status, CreatedAt: base.Add(time.Hour)}
	}
	return f
}

// listingPage requests the listing with no Authorization header.
func (f *postFixture) listingPage(t *testing.T, query string) (jobListingPage, int, string) {
	t.Helper()

	rec := f.send(http.MethodGet, "/v1/job-posts"+query, "", "")
	var page jobListingPage
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("body: %v\n%s", err, rec.Body)
		}
	}
	return page, rec.Code, rec.Body.String()
}

func TestJobListingIsPublic(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t, 3)

	for name, header := range map[string]string{
		"no header":       "",
		"a valid token":   f.headers[domain.RoleCandidate],
		"a garbage token": "Bearer not-a-jwt",
	} {
		rec := f.send(http.MethodGet, "/v1/job-posts", header, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200; body = %s", name, rec.Code, rec.Body)
		}
		if rec.Header().Get("WWW-Authenticate") != "" {
			t.Errorf("%s: WWW-Authenticate set on a public route", name)
		}
	}
}

func TestJobListingEnvelopeAndFields(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t, 2)
	location, kind := "Dhaka", "full-time"
	post := f.posts.byID["ffffffff-0000-4000-8000-000000000000"]
	post.Location, post.EmploymentType = &location, &kind
	f.posts.byID[post.ID] = post
	f.posts.listings = map[string]domain.JobListing{post.ID: {SkillTags: []string{"go", "postgres"}, RemainingVacancies: 1}}

	rec := f.send(http.MethodGet, "/v1/job-posts", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
	}

	top := keysOf(t, rec.Body.Bytes())
	if len(top) != 2 || top["data"] == nil || string(top["next_cursor"]) != "null" {
		t.Errorf("envelope = %s, want exactly data and next_cursor: null", rec.Body)
	}

	var raw struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(raw.Data) != 2 {
		t.Fatalf("data has %d items, want 2", len(raw.Data))
	}

	// Exactly these keys: nothing from the admin view (status, salaries,
	// created_by, questions) may appear in the public one.
	first := raw.Data[0]
	want := []string{"employment_type", "id", "location", "remaining_vacancies", "skill_tags", "title"}
	got := make([]string, 0, len(first))
	for k := range first {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if string(first["skill_tags"]) != `["go","postgres"]` || string(first["remaining_vacancies"]) != "1" ||
		string(first["location"]) != `"Dhaka"` || string(first["employment_type"]) != `"full-time"` {
		t.Errorf("first = %v", first)
	}

	// A post with no tags sends [], and unset fields are null.
	second := raw.Data[1]
	if string(second["skill_tags"]) != "[]" || string(second["location"]) != "null" {
		t.Errorf("second = %v", second)
	}
}

func TestJobListingShowsOnlyOpenPosts(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t, 3)
	page, code, body := f.listingPage(t, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", code, body)
	}
	if strings.Contains(body, "Hidden") || len(page.Data) != 3 {
		t.Errorf("listing = %s, want the 3 open posts only", body)
	}
}

func TestJobListingPagesThroughEverythingOnce(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t, 7)

	var (
		seen   []string
		cursor string
		pages  int
	)
	for {
		query := "?limit=3"
		if cursor != "" {
			query += "&after=" + cursor
		}
		page, code, body := f.listingPage(t, query)
		if code != http.StatusOK {
			t.Fatalf("page %d: status = %d; body = %s", pages+1, code, body)
		}
		pages++
		for _, item := range page.Data {
			seen = append(seen, item.Title)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
		if pages > 10 {
			t.Fatal("never reached the last page")
		}
	}

	want := []string{"Open 0", "Open 1", "Open 2", "Open 3", "Open 4", "Open 5", "Open 6"}
	if !slices.Equal(seen, want) {
		t.Errorf("seen = %v, want %v", seen, want)
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3 (3 + 3 + 1)", pages)
	}
}

func TestJobListingNoCursorWhenExactlyFull(t *testing.T) {
	t.Parallel()

	// Six posts in pages of three: the second page is full and last. It must
	// not offer a cursor to a third, empty page.
	f := newListingFixture(t, 6)
	first, _, _ := f.listingPage(t, "?limit=3")
	if first.NextCursor == nil {
		t.Fatal("first page has no cursor")
	}
	second, _, _ := f.listingPage(t, "?limit=3&after="+*first.NextCursor)
	if len(second.Data) != 3 || second.NextCursor != nil {
		t.Errorf("second page: %d items, cursor %v; want 3 and null", len(second.Data), second.NextCursor)
	}
}

func TestJobListingLimit(t *testing.T) {
	t.Parallel()

	accepted := []struct {
		query string
		want  int
	}{
		{"", domain.DefaultListingLimit},
		{"?limit=1", 1},
		{"?limit=100", 100},
		{"?limit=101", domain.MaxListingLimit},
		{"?limit=1000000", domain.MaxListingLimit},
		{"?after=", domain.DefaultListingLimit},
	}
	for _, tt := range accepted {
		f := newListingFixture(t, 1)
		if _, code, body := f.listingPage(t, tt.query); code != http.StatusOK {
			t.Errorf("%q: status = %d; body = %s", tt.query, code, body)
			continue
		}
		if f.posts.lastLimit != tt.want {
			t.Errorf("%q: store asked for %d, want %d", tt.query, f.posts.lastLimit, tt.want)
		}
	}

	for _, query := range []string{"?limit=0", "?limit=-5", "?limit=abc", "?limit=1.5", "?limit=", "?limit=99999999999999999999"} {
		f := newListingFixture(t, 1)
		rec := f.send(http.MethodGet, "/v1/job-posts"+query, "", "")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%q: status = %d, want 422; body = %s", query, rec.Code, rec.Body)
			continue
		}
		if got := decode(t, rec.Body.Bytes()); got.detail("limit") == "" {
			t.Errorf("%q: details = %v", query, got.Details)
		}
		if f.posts.callCount() != 0 {
			t.Errorf("%q: store was called", query)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	// Microseconds, as PostgreSQL stores them. Losing them would put the
	// cursor before rows created in the same second.
	want := domain.ListingPosition{
		CreatedAt: time.Date(2026, time.November, 11, 9, 43, 11, 203416000, time.UTC),
		ID:        "ca8bdf91-ec6e-4717-887a-90a2f18cca00",
	}
	cursor := encodeCursor(want)

	if strings.ContainsAny(cursor, "+/=") {
		t.Errorf("cursor %q needs escaping in a URL", cursor)
	}
	got, err := decodeCursor(cursor)
	if err != nil || !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Errorf("decodeCursor = %+v, %v; want %+v", got, err, want)
	}

	// A time in another zone is written in UTC and read back as the same
	// instant.
	dhaka := time.FixedZone("BST", 6*60*60)
	got, err = decodeCursor(encodeCursor(domain.ListingPosition{CreatedAt: want.CreatedAt.In(dhaka), ID: want.ID}))
	if err != nil || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("other zone: %+v, %v", got, err)
	}
}

func TestInvalidCursors(t *testing.T) {
	t.Parallel()

	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	good := `{"v":1,"t":"2026-11-11T09:43:11.203416Z","id":"ca8bdf91-ec6e-4717-887a-90a2f18cca00"}`

	bad := map[string]string{
		"not base64":        "!!!",
		"padded base64":     base64.URLEncoding.EncodeToString([]byte(good)) + "==",
		"not JSON":          enc("hello"),
		"wrong version":     enc(strings.Replace(good, `"v":1`, `"v":2`, 1)),
		"no version":        enc(`{"t":"2026-11-11T09:43:11Z","id":"ca8bdf91-ec6e-4717-887a-90a2f18cca00"}`),
		"bad time":          enc(strings.Replace(good, "2026-11-11T09:43:11.203416Z", "yesterday", 1)),
		"bad id":            enc(strings.Replace(good, "ca8bdf91-ec6e-4717-887a-90a2f18cca00", "1 OR 1=1", 1)),
		"an unknown key":    enc(strings.Replace(good, `"v":1`, `"v":1,"offset":10000`, 1)),
		"far too long":      strings.Repeat("A", maxCursorLength+1),
		"a JSON array":      enc(`[1,2,3]`),
		"trailing garbage":  enc(good) + "*",
		"standard alphabet": base64.StdEncoding.EncodeToString([]byte(good + "??>>")),
	}

	for name, cursor := range bad {
		if _, err := decodeCursor(cursor); err == nil {
			t.Errorf("%s: decodeCursor(%q) succeeded", name, cursor)
		}
	}

	f := newListingFixture(t, 1)
	rec := f.send(http.MethodGet, "/v1/job-posts?after="+enc("hello"), "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body)
	}
	if got := decode(t, rec.Body.Bytes()); got.Code != codeInvalidCursor {
		t.Errorf("code = %q, want %q", got.Code, codeInvalidCursor)
	}
	if f.posts.callCount() != 0 {
		t.Error("the store was called with an invalid cursor")
	}
}

func TestJobListingPassesTheCursorThrough(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t, 5)
	first, _, _ := f.listingPage(t, "?limit=2")
	if first.NextCursor == nil {
		t.Fatal("no cursor")
	}
	f.listingPage(t, "?limit=2&after="+*first.NextCursor)

	after := f.posts.lastAfter
	if after == nil || after.ID != "ffffffff-0000-4000-8000-000000000001" {
		t.Errorf("store got after = %+v, want the second post's position", after)
	}
}

// Not using t.Parallel: captureLog swaps the global logger.
func TestJobListingStoreFailure(t *testing.T) {
	captureLog(t)

	f := newListingFixture(t, 1)
	f.posts.failWith = fmt.Errorf("connection reset")
	rec := f.send(http.MethodGet, "/v1/job-posts", "", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestJobListingDetail(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t, 1)
	const id = "ffffffff-0000-4000-8000-000000000000"
	post := f.posts.byID[id]
	post.Questions = []domain.Question{{ID: "q1", Prompt: "Years of Go?", Kind: "number", Required: true}}
	f.posts.byID[id] = post

	// No Authorization header: the page is for visitors.
	rec := f.send(http.MethodGet, "/v1/job-posts/"+id+"/listing", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; body %s", rec.Code, rec.Body)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"id", "title", "skill_tags", "remaining_vacancies", "description", "salary_min", "closes_at", "questions"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing %q in %s", key, rec.Body)
		}
	}
	if !strings.Contains(string(got["questions"]), `"Years of Go?"`) || string(got["skill_tags"]) != "[]" {
		t.Errorf("questions = %s, skill_tags = %s", got["questions"], got["skill_tags"])
	}
	// Nothing from the admin view, and never the rubric.
	for _, key := range []string{"status", "created_by", "vacancies", "rubric"} {
		if _, ok := got[key]; ok {
			t.Errorf("public detail leaks %q", key)
		}
	}

	// A draft and a closed post with well-formed IDs, so the 404 below
	// comes from the status rule and not from the UUID check.
	const draftID, closedID = "ffffffff-2222-4000-8000-000000000001", "ffffffff-2222-4000-8000-000000000002"
	f.posts.byID[draftID] = domain.JobPost{ID: draftID, Title: "Draft", Status: domain.JobStatusDraft}
	f.posts.byID[closedID] = domain.JobPost{ID: closedID, Title: "Closed", Status: domain.JobStatusClosed}

	for name, tc := range map[string]struct {
		path string
		want int
	}{
		"draft":     {"/v1/job-posts/" + draftID + "/listing", http.StatusNotFound},
		"closed":    {"/v1/job-posts/" + closedID + "/listing", http.StatusNotFound},
		"missing":   {"/v1/job-posts/ffffffff-9999-4000-8000-000000000000/listing", http.StatusNotFound},
		"malformed": {"/v1/job-posts/nope/listing", http.StatusBadRequest},
	} {
		if rec := f.send(http.MethodGet, tc.path, "", ""); rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, tc.want)
		}
	}
}
