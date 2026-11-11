package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// QuestionStore is the part of the question storage the handlers need.
// *store.QuestionStore satisfies it.
type QuestionStore interface {
	CreateQuestion(ctx context.Context, jobPostID string, in domain.NewQuestion) (*domain.Question, error)
	ListQuestionsByJobPost(ctx context.Context, jobPostID string) ([]domain.Question, error)
	UpdateQuestion(ctx context.Context, jobPostID, questionID string, patch domain.QuestionPatch) (*domain.Question, error)
	DeleteQuestion(ctx context.Context, jobPostID, questionID string) error
	ReorderQuestions(ctx context.Context, jobPostID string, order []string) ([]domain.Question, error)
}

// QuestionHandler serves the endpoints that manage a job post's application
// form. Every route it serves must be registered behind RequireAuth and
// RequireRole(domain.RoleAdmin).
type QuestionHandler struct {
	questions QuestionStore
}

// NewQuestionHandler returns a QuestionHandler that reads and writes
// questions.
func NewQuestionHandler(questions QuestionStore) *QuestionHandler {
	return &QuestionHandler{questions: questions}
}

// createQuestionRequest is the body of POST /v1/job-posts/:id/questions.
//
// Required, MinLength and Position are pointers because "not sent" must be
// told apart from the zero value: an absent required means true, an absent
// position means "after the last question", and an explicit position of 0 is
// an error rather than a request for the default.
type createQuestionRequest struct {
	Prompt    string   `json:"prompt"`
	Kind      string   `json:"kind"`
	Choices   []string `json:"choices"`
	Required  *bool    `json:"required"`
	MinLength *int     `json:"min_length"`
	Position  *int     `json:"position"`
}

// updateQuestionRequest is the body of PATCH
// /v1/job-posts/:id/questions/:questionID. A field absent from the body, or
// null, keeps its stored value.
//
// Choices is *[]string so that "choices": [] can be told from no choices key
// at all. The empty list clears them, which a client needs when changing a
// choice question into another kind.
type updateQuestionRequest struct {
	Prompt    *string   `json:"prompt"`
	Kind      *string   `json:"kind"`
	Choices   *[]string `json:"choices"`
	Required  *bool     `json:"required"`
	MinLength *int      `json:"min_length"`
	Position  *int      `json:"position"`
}

// reorderQuestionsRequest is the body of PUT
// /v1/job-posts/:id/questions/order: every question ID of the post, in the
// order the form should show them.
type reorderQuestionsRequest struct {
	QuestionIDs []string `json:"question_ids"`
}

// newQuestionResponse is a question as a client sees it.
func newQuestionResponse(q *domain.Question) questionResponse {
	// A non-choice question sends [] rather than null, so a client reads one
	// shape for every kind.
	choices := q.Choices
	if choices == nil {
		choices = []string{}
	}

	return questionResponse{
		ID:        q.ID,
		Prompt:    q.Prompt,
		Kind:      q.Kind,
		Choices:   choices,
		Required:  q.Required,
		MinLength: q.MinLength,
		Position:  q.Position,
	}
}

// newQuestionList converts a form for a response, never nil.
func newQuestionList(questions []domain.Question) []questionResponse {
	out := make([]questionResponse, 0, len(questions))
	for i := range questions {
		out = append(out, newQuestionResponse(&questions[i]))
	}
	return out
}

// trimChoices returns choices with surrounding spaces removed from each, so
// " Yes" and "Yes" are the same option. nil stays nil.
func trimChoices(choices []string) []string {
	if choices == nil {
		return nil
	}
	out := make([]string, len(choices))
	for i, choice := range choices {
		out[i] = strings.TrimSpace(choice)
	}
	return out
}

// List handles GET /v1/job-posts/:id/questions: the post's form in display
// order, whatever the post's status.
func (h *QuestionHandler) List(c *gin.Context) {
	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	questions, err := h.questions.ListQuestionsByJobPost(c.Request.Context(), postID)
	if err != nil {
		Respond(c, fmt.Errorf("list questions: %w", err))
		return
	}

	c.JSON(http.StatusOK, listResponse[questionResponse]{Data: newQuestionList(questions)})
}

// Create handles POST /v1/job-posts/:id/questions. The post must be a draft.
func (h *QuestionHandler) Create(c *gin.Context) {
	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req createQuestionRequest
	if !decodeJSON(c, &req) {
		return
	}

	in := domain.NewQuestion{
		Prompt:  strings.TrimSpace(req.Prompt),
		Kind:    req.Kind,
		Choices: trimChoices(req.Choices),

		// The column's default, applied here so the store always writes an
		// explicit value.
		Required: true,
	}
	if req.Required != nil {
		in.Required = *req.Required
	}
	if req.MinLength != nil {
		in.MinLength = *req.MinLength
	}

	var invalid domain.ValidationError
	if req.Position != nil {
		// NewQuestion uses 0 to mean "after the last question", so a client
		// that sends 0 must be refused here, before it reaches the domain and
		// silently becomes the default.
		if *req.Position < 1 {
			invalid.Add("position", "must be at least 1")
		} else {
			in.Position = *req.Position
		}
	}
	mergeValidation(&invalid, in.Validate())
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	q, err := h.questions.CreateQuestion(c.Request.Context(), postID, in)
	if err != nil {
		Respond(c, fmt.Errorf("create question: %w", err))
		return
	}

	c.JSON(http.StatusCreated, newQuestionResponse(q))
}

// Update handles PATCH /v1/job-posts/:id/questions/:questionID. Only the
// fields present in the body change. The post must be a draft.
func (h *QuestionHandler) Update(c *gin.Context) {
	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	questionID, ok := pathUUID(c, "questionID")
	if !ok {
		return
	}

	var req updateQuestionRequest
	if !decodeJSON(c, &req) {
		return
	}

	patch := domain.QuestionPatch{
		Kind:      req.Kind,
		Required:  req.Required,
		MinLength: req.MinLength,
		Position:  req.Position,
	}
	if req.Prompt != nil {
		prompt := strings.TrimSpace(*req.Prompt)
		patch.Prompt = &prompt
	}
	if req.Choices != nil {
		choices := trimChoices(*req.Choices)
		patch.Choices = &choices
	}

	// The rules are judged by the store on the merged question, inside the
	// transaction that holds the post's lock.
	q, err := h.questions.UpdateQuestion(c.Request.Context(), postID, questionID, patch)
	if err != nil {
		Respond(c, fmt.Errorf("update question: %w", err))
		return
	}

	c.JSON(http.StatusOK, newQuestionResponse(q))
}

// Delete handles DELETE /v1/job-posts/:id/questions/:questionID. The post
// must be a draft. It answers 204 with no body.
func (h *QuestionHandler) Delete(c *gin.Context) {
	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	questionID, ok := pathUUID(c, "questionID")
	if !ok {
		return
	}

	if err := h.questions.DeleteQuestion(c.Request.Context(), postID, questionID); err != nil {
		Respond(c, fmt.Errorf("delete question: %w", err))
		return
	}

	// c.Status alone writes no body; c.JSON with 204 would try to write one,
	// which a 204 response must not have.
	c.Status(http.StatusNoContent)
}

// Reorder handles PUT /v1/job-posts/:id/questions/order. The body lists every
// question of the post in its new order; the response is the form as it now
// stands. The post must be a draft.
//
// PUT, not PATCH: the body is the whole order, and sending it twice leaves
// the form exactly as sending it once did.
func (h *QuestionHandler) Reorder(c *gin.Context) {
	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req reorderQuestionsRequest
	if !decodeJSON(c, &req) {
		return
	}

	var invalid domain.ValidationError
	order := make([]string, 0, len(req.QuestionIDs))
	// nil means the key was absent or null. An empty list is accepted: it is
	// the correct order for a form with no questions.
	if req.QuestionIDs == nil {
		invalid.Add("question_ids", "is required")
	}
	for _, id := range req.QuestionIDs {
		if !isUUID(id) {
			invalid.Add("question_ids", "must contain only question IDs")
			break
		}
		// The database writes UUIDs in lower case, and the store compares IDs
		// as text, so an upper-case ID must be lowered to match.
		order = append(order, strings.ToLower(id))
	}
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	questions, err := h.questions.ReorderQuestions(c.Request.Context(), postID, order)
	if err != nil {
		Respond(c, fmt.Errorf("reorder questions: %w", err))
		return
	}

	c.JSON(http.StatusOK, listResponse[questionResponse]{Data: newQuestionList(questions)})
}
