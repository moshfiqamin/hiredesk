package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// The question methods of fakeJobPosts. The form lives in each post's
// Questions slice, kept in position order, and the same domain checks run as
// in the real store: the form lock first, then the rules, then uniqueness of
// position.

func (f *fakeJobPosts) editableForm(jobPostID string) (domain.JobPost, error) {
	post, ok := f.byID[jobPostID]
	if !ok {
		return domain.JobPost{}, domain.ErrNotFound
	}
	if err := domain.CheckFormEditable(post.Status); err != nil {
		return domain.JobPost{}, err
	}
	return post, nil
}

func (f *fakeJobPosts) CreateQuestion(_ context.Context, jobPostID string, in domain.NewQuestion) (*domain.Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.failWith != nil {
		return nil, f.failWith
	}
	post, err := f.editableForm(jobPostID)
	if err != nil {
		return nil, err
	}

	position := in.Position
	if position == 0 {
		position = 1
		for _, q := range post.Questions {
			position = max(position, q.Position+1)
		}
	}
	for _, q := range post.Questions {
		if q.Position == position {
			var invalid domain.ValidationError
			invalid.Add("position", "is already used by another question in this post")
			return nil, invalid.Err()
		}
	}

	f.next++
	q := domain.Question{
		ID: fmt.Sprintf("dddddddd-0000-4000-8000-%012d", f.next), JobPostID: jobPostID,
		Prompt: in.Prompt, Kind: in.Kind, Choices: in.Choices, Required: in.Required,
		MinLength: in.MinLength, Position: position,
	}
	post.Questions = append(slices.Clone(post.Questions), q)
	slices.SortFunc(post.Questions, func(a, b domain.Question) int { return a.Position - b.Position })
	f.byID[jobPostID] = post
	return &q, nil
}

func (f *fakeJobPosts) ListQuestionsByJobPost(_ context.Context, jobPostID string) ([]domain.Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.failWith != nil {
		return nil, f.failWith
	}
	post, ok := f.byID[jobPostID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return slices.Clone(post.Questions), nil
}

func (f *fakeJobPosts) UpdateQuestion(_ context.Context, jobPostID, questionID string, patch domain.QuestionPatch) (*domain.Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.lastQuestionPatch = patch
	if f.failWith != nil {
		return nil, f.failWith
	}
	post, err := f.editableForm(jobPostID)
	if err != nil {
		return nil, err
	}

	i := slices.IndexFunc(post.Questions, func(q domain.Question) bool { return q.ID == questionID })
	if i < 0 {
		return nil, domain.ErrNotFound
	}
	q := post.Questions[i]
	q.Apply(patch)
	if err := q.Validate(); err != nil {
		return nil, err
	}
	for j, other := range post.Questions {
		if j != i && other.Position == q.Position {
			var invalid domain.ValidationError
			invalid.Add("position", "is already used by another question in this post")
			return nil, invalid.Err()
		}
	}

	post.Questions = slices.Clone(post.Questions)
	post.Questions[i] = q
	f.byID[jobPostID] = post
	return &q, nil
}

func (f *fakeJobPosts) DeleteQuestion(_ context.Context, jobPostID, questionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.failWith != nil {
		return f.failWith
	}
	post, err := f.editableForm(jobPostID)
	if err != nil {
		return err
	}

	i := slices.IndexFunc(post.Questions, func(q domain.Question) bool { return q.ID == questionID })
	if i < 0 {
		return domain.ErrNotFound
	}
	post.Questions = slices.Delete(slices.Clone(post.Questions), i, i+1)
	f.byID[jobPostID] = post
	return nil
}

func (f *fakeJobPosts) ReorderQuestions(_ context.Context, jobPostID string, order []string) ([]domain.Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.failWith != nil {
		return nil, f.failWith
	}
	post, err := f.editableForm(jobPostID)
	if err != nil {
		return nil, err
	}

	byID := map[string]domain.Question{}
	for _, q := range post.Questions {
		byID[q.ID] = q
	}
	if len(order) != len(byID) {
		return nil, reorderMismatch()
	}

	reordered := make([]domain.Question, 0, len(order))
	for i, id := range order {
		q, ok := byID[id]
		if !ok {
			return nil, reorderMismatch()
		}
		delete(byID, id)
		q.Position = i + 1
		reordered = append(reordered, q)
	}
	post.Questions = reordered
	f.byID[jobPostID] = post
	return slices.Clone(reordered), nil
}

func reorderMismatch() error {
	var invalid domain.ValidationError
	invalid.Add("question_ids", "must list each of this post's questions exactly once")
	return invalid.Err()
}

// The fixture's posts, as far as questions go: f.draft has none, and f.open
// has qOne and qTwo.

func TestCreateQuestion(t *testing.T) {
	t.Parallel()

	t.Run("defaults: required, no minimum, after the last question", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		path := "/v1/job-posts/" + draftPostID + "/questions"

		first := f.send(http.MethodPost, path, f.headers[domain.RoleAdmin], `{"prompt":"  Why this role?  ","kind":"text"}`)
		if first.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", first.Code, first.Body)
		}
		var got questionResponse
		if err := json.Unmarshal(first.Body.Bytes(), &got); err != nil {
			t.Fatalf("body: %v", err)
		}
		if got.Prompt != "Why this role?" || !got.Required || got.MinLength != 0 || got.Position != 1 {
			t.Errorf("question = %+v", got)
		}
		if got.Choices == nil || len(got.Choices) != 0 {
			t.Errorf("choices = %#v, want []", got.Choices)
		}

		second := f.send(http.MethodPost, path, f.headers[domain.RoleAdmin],
			`{"prompt":"Office","kind":"choice","choices":[" Dhaka ","Remote"],"required":false}`)
		if second.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", second.Code, second.Body)
		}
		if err := json.Unmarshal(second.Body.Bytes(), &got); err != nil {
			t.Fatalf("body: %v", err)
		}
		if got.Position != 2 || got.Required || !slices.Equal(got.Choices, []string{"Dhaka", "Remote"}) {
			t.Errorf("question = %+v, want position 2, not required, trimmed choices", got)
		}
	})

	t.Run("an explicit free position is used", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.send(http.MethodPost, "/v1/job-posts/"+draftPostID+"/questions", f.headers[domain.RoleAdmin],
			`{"prompt":"Years of Go","kind":"number","position":7}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		if got := f.stored(draftPostID).Questions; len(got) != 1 || got[0].Position != 7 {
			t.Errorf("stored = %+v", got)
		}
	})
}

func TestCreateQuestionValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantFields []string
	}{
		{"no prompt", `{"kind":"text"}`, []string{"prompt"}},
		{"a prompt of spaces", `{"prompt":"   ","kind":"text"}`, []string{"prompt"}},
		{"an unknown kind", `{"prompt":"x","kind":"date"}`, []string{"kind"}},
		{"choice without choices", `{"prompt":"x","kind":"choice"}`, []string{"choices"}},
		{"choices on a number", `{"prompt":"x","kind":"number","choices":["1"]}`, []string{"choices"}},
		{"an option of spaces", `{"prompt":"x","kind":"choice","choices":["a","  "]}`, []string{"choices"}},
		{"a repeated option after trimming", `{"prompt":"x","kind":"choice","choices":["Remote"," remote"]}`, []string{"choices"}},
		{"min_length on a boolean", `{"prompt":"x","kind":"boolean","min_length":3}`, []string{"min_length"}},

		// An explicit 0 must not silently become "after the last question".
		{"position 0", `{"prompt":"x","kind":"text","position":0}`, []string{"position"}},
		{"a negative position", `{"prompt":"x","kind":"text","position":-3}`, []string{"position"}},
		{"several at once", `{"kind":"date","position":0}`, []string{"prompt", "kind", "position"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newPostFixture(t)
			rec := f.send(http.MethodPost, "/v1/job-posts/"+draftPostID+"/questions", f.headers[domain.RoleAdmin], tt.body)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
			}
			got := decode(t, rec.Body.Bytes())
			if len(got.Details) != len(tt.wantFields) {
				t.Errorf("details = %v, want exactly %v", got.Details, tt.wantFields)
			}
			for _, field := range tt.wantFields {
				if got.detail(field) == "" {
					t.Errorf("no problem reported for %q; details = %v", field, got.Details)
				}
			}
			if n := f.posts.callCount(); n != 0 {
				t.Errorf("store called %d times for invalid input, want 0", n)
			}
		})
	}

	t.Run("a taken position is a 422 on position", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		path := "/v1/job-posts/" + draftPostID + "/questions"
		f.send(http.MethodPost, path, f.headers[domain.RoleAdmin], `{"prompt":"a","kind":"text","position":1}`)

		rec := f.send(http.MethodPost, path, f.headers[domain.RoleAdmin], `{"prompt":"b","kind":"text","position":1}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		if got := decode(t, rec.Body.Bytes()); got.detail("position") == "" {
			t.Errorf("details = %v, want a problem with position", got.Details)
		}
	})
}

// The critical rule: once a post leaves draft, every write to its form is a
// 409 form_locked, whatever the request, and nothing changes.
func TestQuestionWritesAreLockedOutsideDraft(t *testing.T) {
	t.Parallel()

	writes := func(postID string) []struct{ method, path, body string } {
		base := "/v1/job-posts/" + postID + "/questions"
		return []struct{ method, path, body string }{
			{http.MethodPost, base, `{"prompt":"New","kind":"text"}`},
			{http.MethodPatch, base + "/" + qOne, `{"prompt":"Changed"}`},
			{http.MethodDelete, base + "/" + qOne, ``},
			{http.MethodPut, base + "/order", `{"question_ids":["` + qTwo + `","` + qOne + `"]}`},
		}
	}

	for _, status := range []string{domain.JobStatusOpen, domain.JobStatusClosed} {
		for _, req := range writes(openPostID) {
			t.Run(status+" "+req.method+" "+req.path[len(req.path)-10:], func(t *testing.T) {
				t.Parallel()

				f := newPostFixture(t)
				post := f.stored(openPostID)
				post.Status = status
				f.posts.byID[openPostID] = post

				rec := f.send(req.method, req.path, f.headers[domain.RoleAdmin], req.body)

				if rec.Code != http.StatusConflict {
					t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body)
				}
				got := decode(t, rec.Body.Bytes())
				if got.Code != "form_locked" {
					t.Errorf("code = %q, want form_locked", got.Code)
				}
				if !strings.Contains(got.Message, "answered") {
					t.Errorf("message = %q, want it to say candidates may have answered", got.Message)
				}
				if got.detail("current_status") != status || got.detail("reason") == "" {
					t.Errorf("details = %v", got.Details)
				}

				after := f.stored(openPostID).Questions
				if len(after) != 2 || after[0].Prompt != "Why this role?" || after[0].Position != 1 {
					t.Errorf("a locked form was changed: %+v", after)
				}
			})
		}
	}

	t.Run("reading a locked form is still allowed", func(t *testing.T) {
		t.Parallel()

		f := newPostFixture(t)
		rec := f.get("/v1/job-posts/"+openPostID+"/questions", f.headers[domain.RoleAdmin])
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
	})
}

// IDs of the open fixture post's two questions: qOne is a text question at
// position 1, qTwo a choice question at position 2.
const (
	qOne = "eeeeeeee-0000-4000-8000-000000000001"
	qTwo = "eeeeeeee-0000-4000-8000-000000000002"
)

func TestUpdateQuestion(t *testing.T) {
	t.Parallel()

	draftWithForm := func(t *testing.T) *postFixture {
		t.Helper()

		f := newPostFixture(t)
		post := f.stored(openPostID)
		post.Status = domain.JobStatusDraft
		f.posts.byID[openPostID] = post
		return f
	}
	path := "/v1/job-posts/" + openPostID + "/questions/"

	t.Run("only the fields sent change", func(t *testing.T) {
		t.Parallel()

		f := draftWithForm(t)
		rec := f.send(http.MethodPatch, path+qOne, f.headers[domain.RoleAdmin], `{"prompt":"  Why us?  "}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		patch := f.posts.lastQuestionPatch
		if patch.Prompt == nil || *patch.Prompt != "Why us?" || patch.Kind != nil || patch.Choices != nil ||
			patch.Required != nil || patch.MinLength != nil || patch.Position != nil {
			t.Errorf("patch = %+v, want only a trimmed prompt", patch)
		}
		q := f.stored(openPostID).Questions[0]
		if q.Prompt != "Why us?" || q.Kind != "text" || q.MinLength != 50 || !q.Required {
			t.Errorf("stored = %+v", q)
		}
	})

	t.Run("choices: absent, empty and set are three different requests", func(t *testing.T) {
		t.Parallel()

		f := draftWithForm(t)

		// q-2 is a choice question. Turning it into text needs its choices
		// cleared, and an empty list is how to say so.
		keep := f.send(http.MethodPatch, path+qTwo, f.headers[domain.RoleAdmin], `{"kind":"text"}`)
		if keep.Code != http.StatusUnprocessableEntity {
			t.Errorf("kind change keeping choices: status = %d, want 422", keep.Code)
		}

		clear := f.send(http.MethodPatch, path+qTwo, f.headers[domain.RoleAdmin], `{"kind":"text","choices":[]}`)
		if clear.Code != http.StatusOK {
			t.Fatalf("kind change clearing choices: status = %d; body = %s", clear.Code, clear.Body)
		}
		if p := f.posts.lastQuestionPatch.Choices; p == nil || len(*p) != 0 {
			t.Errorf("patch.Choices = %v, want a pointer to an empty slice", p)
		}

		set := f.send(http.MethodPatch, path+qTwo, f.headers[domain.RoleAdmin], `{"kind":"choice","choices":["A","B"]}`)
		if set.Code != http.StatusOK {
			t.Fatalf("back to choice: status = %d; body = %s", set.Code, set.Body)
		}
		if got := f.stored(openPostID).Questions[1].Choices; !slices.Equal(got, []string{"A", "B"}) {
			t.Errorf("choices = %v", got)
		}
	})

	t.Run("moving to a taken position is refused", func(t *testing.T) {
		t.Parallel()

		f := draftWithForm(t)
		rec := f.send(http.MethodPatch, path+qOne, f.headers[domain.RoleAdmin], `{"position":2}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
		}
		if got := decode(t, rec.Body.Bytes()); got.detail("position") == "" {
			t.Errorf("details = %v", got.Details)
		}
	})

	t.Run("a question of another post is not found", func(t *testing.T) {
		t.Parallel()

		f := draftWithForm(t)
		rec := f.send(http.MethodPatch, "/v1/job-posts/"+draftPostID+"/questions/"+qOne, f.headers[domain.RoleAdmin], `{"prompt":"x"}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404; body = %s", rec.Code, rec.Body)
		}
	})
}

func TestDeleteQuestion(t *testing.T) {
	t.Parallel()

	f := newPostFixture(t)
	post := f.stored(openPostID)
	post.Status = domain.JobStatusDraft
	f.posts.byID[openPostID] = post
	path := "/v1/job-posts/" + openPostID + "/questions/" + qOne

	rec := f.send(http.MethodDelete, path, f.headers[domain.RoleAdmin], "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q; want 204 and no body", rec.Code, rec.Body)
	}
	if got := f.stored(openPostID).Questions; len(got) != 1 || got[0].ID != qTwo {
		t.Errorf("remaining = %+v, want only q-2", got)
	}

	again := f.send(http.MethodDelete, path, f.headers[domain.RoleAdmin], "")
	if again.Code != http.StatusNotFound {
		t.Errorf("second delete: status = %d, want 404", again.Code)
	}
}

func TestReorderQuestions(t *testing.T) {
	t.Parallel()

	draftWithForm := func(t *testing.T) *postFixture {
		t.Helper()

		f := newPostFixture(t)
		post := f.stored(openPostID)
		post.Status = domain.JobStatusDraft
		f.posts.byID[openPostID] = post
		return f
	}
	path := "/v1/job-posts/" + openPostID + "/questions/order"

	t.Run("swap", func(t *testing.T) {
		t.Parallel()

		f := draftWithForm(t)
		// Upper case on purpose: the handler must lower it to match.
		rec := f.send(http.MethodPut, path, f.headers[domain.RoleAdmin],
			`{"question_ids":["`+strings.ToUpper(qTwo)+`","`+qOne+`"]}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body)
		}
		var body listResponse[questionResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if len(body.Data) != 2 || body.Data[0].ID != qTwo || body.Data[0].Position != 1 ||
			body.Data[1].ID != qOne || body.Data[1].Position != 2 {
			t.Errorf("data = %+v, want q-2 then q-1 at 1 and 2", body.Data)
		}
	})

	invalid := []struct{ name, body string }{
		{"no question_ids", `{}`},
		{"not a UUID", `{"question_ids":["` + qOne + `","nope"]}`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := draftWithForm(t)
			rec := f.send(http.MethodPut, path, f.headers[domain.RoleAdmin], tt.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
			}
			if n := f.posts.callCount(); n != 0 {
				t.Errorf("store called %d times, want 0", n)
			}
		})
	}

	// These pass the handler and are refused by the store, which alone knows
	// the post's questions.
	mismatched := []struct{ name, ids string }{
		{"one missing", `["` + qOne + `"]`},
		{"one repeated", `["` + qOne + `","` + qOne + `"]`},
		{"one from elsewhere", `["` + qOne + `","eeeeeeee-0000-4000-8000-0000000000ff"]`},
		{"one extra", `["` + qOne + `","` + qTwo + `","eeeeeeee-0000-4000-8000-0000000000ff"]`},
	}
	for _, tt := range mismatched {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := draftWithForm(t)
			rec := f.send(http.MethodPut, path, f.headers[domain.RoleAdmin], `{"question_ids":`+tt.ids+`}`)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body)
			}
			if got := decode(t, rec.Body.Bytes()); got.detail("question_ids") == "" {
				t.Errorf("details = %v", got.Details)
			}
			if got := f.stored(openPostID).Questions; got[0].ID != qOne || got[0].Position != 1 {
				t.Errorf("a refused reorder changed the form: %+v", got)
			}
		})
	}
}

func TestListQuestions(t *testing.T) {
	t.Parallel()

	f := newPostFixture(t)

	rec := f.get("/v1/job-posts/"+openPostID+"/questions", f.headers[domain.RoleAdmin])
	var body listResponse[questionResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d, err = %v; body = %s", rec.Code, err, rec.Body)
	}
	if len(body.Data) != 2 || body.Data[0].ID != qOne {
		t.Errorf("data = %+v", body.Data)
	}

	empty := f.get("/v1/job-posts/"+draftPostID+"/questions", f.headers[domain.RoleAdmin])
	if empty.Code != http.StatusOK || empty.Body.String() != `{"data":[]}` {
		t.Errorf("empty form: %d %s, want 200 {\"data\":[]}", empty.Code, empty.Body)
	}

	missing := f.get("/v1/job-posts/"+missingPostID+"/questions", f.headers[domain.RoleAdmin])
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing post: status = %d, want 404", missing.Code)
	}
}

func TestQuestionRoutesAreAdminOnly(t *testing.T) {
	t.Parallel()

	base := "/v1/job-posts/" + draftPostID + "/questions"
	requests := []struct{ method, path, body string }{
		{http.MethodGet, base, ``},
		{http.MethodPost, base, `{"prompt":"x","kind":"text"}`},
		{http.MethodPatch, base + "/" + qOne, `{"prompt":"x"}`},
		{http.MethodDelete, base + "/" + qOne, ``},
		{http.MethodPut, base + "/order", `{"question_ids":[]}`},
	}

	for _, req := range requests {
		for role, wantStatus := range map[string]int{
			domain.RoleInterviewer: http.StatusForbidden,
			domain.RoleCandidate:   http.StatusForbidden,
			"":                     http.StatusUnauthorized,
		} {
			t.Run(req.method+" "+req.path+" as "+role, func(t *testing.T) {
				t.Parallel()

				f := newPostFixture(t)
				rec := f.send(req.method, req.path, f.headers[role], req.body)
				if rec.Code != wantStatus {
					t.Errorf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body)
				}
				if n := f.posts.callCount(); n != 0 {
					t.Errorf("store called %d times, want 0", n)
				}
			})
		}
	}
}

func TestQuestionRoutesCheckBothIDs(t *testing.T) {
	t.Parallel()

	f := newPostFixture(t)
	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/v1/job-posts/nope/questions"},
		{http.MethodPost, "/v1/job-posts/nope/questions"},
		{http.MethodPut, "/v1/job-posts/nope/questions/order"},
		{http.MethodPatch, "/v1/job-posts/" + draftPostID + "/questions/nope"},
		{http.MethodDelete, "/v1/job-posts/" + draftPostID + "/questions/nope"},
		{http.MethodDelete, "/v1/job-posts/nope/questions/" + qOne},
	} {
		rec := f.send(req.method, req.path, f.headers[domain.RoleAdmin], `{}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s: status = %d, want 400", req.method, req.path, rec.Code)
		}
	}
	if n := f.posts.callCount(); n != 0 {
		t.Errorf("store called %d times, want 0", n)
	}
}

// Not using t.Parallel: captureLog swaps the global logger.
func TestQuestionStoreFailures(t *testing.T) {
	captureLog(t)

	f := newPostFixture(t)
	f.posts.failWith = fmt.Errorf("connection reset")
	base := "/v1/job-posts/" + draftPostID + "/questions"

	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, base, ``},
		{http.MethodPost, base, `{"prompt":"x","kind":"text"}`},
		{http.MethodPatch, base + "/" + qOne, `{"prompt":"x"}`},
		{http.MethodDelete, base + "/" + qOne, ``},
		{http.MethodPut, base + "/order", `{"question_ids":[]}`},
	} {
		rec := f.send(req.method, req.path, f.headers[domain.RoleAdmin], req.body)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: status = %d, want 500", req.method, req.path, rec.Code)
		}
	}
}
