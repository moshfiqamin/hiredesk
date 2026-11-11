package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// ApplicationStore is the part of the application storage the handlers need.
// *store.ApplicationStore satisfies it.
type ApplicationStore interface {
	ListApplications(ctx context.Context, viewer domain.User, filter domain.ApplicationFilter) (apps []domain.Application, total int, more bool, err error)
	GetApplication(ctx context.Context, id, viewerID string) (app *domain.Application, assigned bool, err error)
	ApplicationHistory(ctx context.Context, id string) ([]domain.Answer, []domain.ApplicationEvent, error)
	SubmitApplication(ctx context.Context, in domain.NewApplication) (*domain.Application, error)
	TransitionApplication(ctx context.Context, id, to, actorID string) (*domain.Application, error)
}

// ApplicationHandler serves the application endpoints. Every route it serves
// must be registered behind RequireAuth.
type ApplicationHandler struct {
	apps ApplicationStore
}

// NewApplicationHandler returns an ApplicationHandler that reads from apps.
func NewApplicationHandler(apps ApplicationStore) *ApplicationHandler {
	return &ApplicationHandler{apps: apps}
}

// applicationResponse is an application as a client sees one.
type applicationResponse struct {
	ID          string    `json:"id"`
	JobPostID   string    `json:"job_post_id"`
	CandidateID string    `json:"candidate_id"`
	Status      string    `json:"status"`
	CVURL       string    `json:"cv_url"`
	SubmittedAt time.Time `json:"submitted_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newApplicationResponse(a *domain.Application) applicationResponse {
	return applicationResponse{
		ID:          a.ID,
		JobPostID:   a.JobPostID,
		CandidateID: a.CandidateID,
		Status:      a.Status,
		CVURL:       a.CVURL,
		SubmittedAt: a.SubmittedAt.UTC(),
		UpdatedAt:   a.UpdatedAt.UTC(),
	}
}

// submitApplicationRequest is the body of POST
// /v1/job-posts/:id/applications.
type submitApplicationRequest struct {
	CVURL   string          `json:"cv_url"`
	Answers []answerRequest `json:"answers"`
}

// answerRequest is one answer in a submission.
type answerRequest struct {
	QuestionID string      `json:"question_id"`
	Answer     answerValue `json:"answer"`
}

// answerValue is an answer in its text form. It accepts a JSON string,
// number or boolean, so a client may send "answer": 4 or "answer": "4" alike;
// whether the text suits the question is checked later, against the form.
type answerValue string

// UnmarshalJSON implements json.Unmarshaler. An object, an array or null is
// refused, which the decoder reports as a malformed body, as it does a wrong
// type in any other field.
func (v *answerValue) UnmarshalJSON(data []byte) error {
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	// A type switch: in each case, s has the type that case names.
	switch s := decoded.(type) {
	case string:
		*v = answerValue(s)
	case float64, bool:
		// The literal exactly as sent, so 4.50 stays "4.50" rather than
		// going through a float64 and coming back as "4.5".
		*v = answerValue(data)
	default:
		return errors.New("answer must be a string, number or boolean")
	}
	return nil
}

// answerResponse is a stored answer as a client sees it.
type answerResponse struct {
	ID         string    `json:"id"`
	QuestionID string    `json:"question_id"`
	Answer     string    `json:"answer"`
	CreatedAt  time.Time `json:"created_at"`
}

// submittedApplicationResponse is an application together with its answers.
//
// Embedding applicationResponse promotes its fields, and encoding/json
// writes them at the top level, so the body is one flat object with an extra
// "answers" key rather than {"application": {...}, "answers": [...]}.
type submittedApplicationResponse struct {
	applicationResponse
	Answers []answerResponse `json:"answers"`
}

// listResponse wraps a list in an object rather than sending a bare array, so
// fields such as a pagination cursor can be added later without breaking
// clients.
type listResponse[T any] struct {
	Data []T `json:"data"`
}

// applicationPage is the envelope of GET /v1/applications:
//
//	{"data":[…], "next_cursor":"…", "total":42}
//
// total counts every application matching the filters, across all pages.
// next_cursor is null on the last page.
type applicationPage struct {
	Data       []applicationResponse `json:"data"`
	NextCursor *string               `json:"next_cursor"`
	Total      int                   `json:"total"`
}

// eventResponse is one entry of an application's history.
type eventResponse struct {
	ID string `json:"id"`

	// ActorID is null when the system acted, or the actor's account is gone.
	ActorID   *string        `json:"actor_id"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

// applicationDetailResponse is one application with its answers and its
// history, flattened into one object as submittedApplicationResponse is.
type applicationDetailResponse struct {
	applicationResponse
	Answers []answerResponse `json:"answers"`
	Events  []eventResponse  `json:"events"`
}

// updateStatusRequest is the body of PATCH /v1/applications/:id/status.
type updateStatusRequest struct {
	Status string `json:"status"`
}

// newAnswerList converts stored answers for a response, never nil.
func newAnswerList(answers []domain.Answer) []answerResponse {
	out := make([]answerResponse, 0, len(answers))
	for _, a := range answers {
		out = append(out, answerResponse{
			ID:         a.ID,
			QuestionID: a.QuestionID,
			Answer:     a.Text,
			CreatedAt:  a.CreatedAt.UTC(),
		})
	}
	return out
}

// List handles GET /v1/applications?status=&job_post_id=&limit=&after=.
//
// Which applications the caller may see is decided by the store's query for
// their role. status and job_post_id narrow that further; limit and after
// page through it newest first, as GET /v1/job-posts does.
func (h *ApplicationHandler) List(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("list applications: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	limit, ok := listingLimit(c)
	if !ok {
		return
	}
	filter := domain.ApplicationFilter{Limit: limit}

	// Both filters are checked before the store sees them, and both problems
	// are reported together if both are wrong.
	var invalid domain.ValidationError
	if status := c.Query("status"); status != "" {
		if !domain.IsApplicationStatus(status) {
			invalid.Add("status", "must be one of submitted, shortlisted, interview, offer, rejected")
		}
		filter.Status = status
	}
	if postID := c.Query("job_post_id"); postID != "" {
		if !isUUID(postID) {
			invalid.Add("job_post_id", "must be a UUID")
		}
		filter.JobPostID = strings.ToLower(postID)
	}
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	if raw := c.Query("after"); raw != "" {
		position, err := decodeCursor(raw)
		if err != nil {
			write(c, http.StatusBadRequest, codeInvalidCursor,
				"The after parameter is not a cursor this API issued. Start again from the first page.", nil)
			return
		}
		filter.After = &position
	}

	apps, total, more, err := h.apps.ListApplications(c.Request.Context(), user, filter)
	if err != nil {
		Respond(c, fmt.Errorf("list applications: %w", err))
		return
	}

	// make with a length of 0 and a capacity of len(apps): one allocation,
	// and an empty result still encodes as [] rather than null.
	page := applicationPage{Data: make([]applicationResponse, 0, len(apps)), Total: total}
	for i := range apps {
		// &apps[i] points into the slice rather than copying each element.
		page.Data = append(page.Data, newApplicationResponse(&apps[i]))
	}

	// The cursor is the sort key of the last row on this page, issued only
	// when the store saw a further row.
	if more && len(apps) > 0 {
		last := apps[len(apps)-1]
		cursor := encodeCursor(domain.ListingPosition{CreatedAt: last.SubmittedAt, ID: last.ID})
		page.NextCursor = &cursor
	}

	c.JSON(http.StatusOK, page)
}

// Get handles GET /v1/applications/:id: the application, its answers and its
// history.
//
// An application the caller may not view is reported exactly as one that does
// not exist: the same 404, the same body. See docs/decisions.md.
func (h *ApplicationHandler) Get(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("get application: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	app, assigned, err := h.apps.GetApplication(c.Request.Context(), c.Param("id"), user.ID)
	if errors.Is(err, domain.ErrNotFound) {
		Respond(c, domain.ErrNotFound)
		return
	}
	if err != nil {
		Respond(c, fmt.Errorf("get application: %w", err))
		return
	}

	if !auth.CanViewApplication(user, *app, assigned) {
		// Not ErrForbidden: a 403 here would confirm that the ID is real.
		Respond(c, domain.ErrNotFound)
		return
	}

	// Only now, after the policy has allowed it. A refused request and a
	// missing one stop above having done the same work, so neither the
	// answers nor the time taken to load them can tell an outsider anything.
	answers, events, err := h.apps.ApplicationHistory(c.Request.Context(), app.ID)
	if err != nil {
		Respond(c, fmt.Errorf("get application: %w", err))
		return
	}

	history := make([]eventResponse, 0, len(events))
	for _, e := range events {
		payload := e.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		history = append(history, eventResponse{
			ID: e.ID, ActorID: e.ActorID, Type: e.Type, Payload: payload, CreatedAt: e.CreatedAt.UTC(),
		})
	}

	c.JSON(http.StatusOK, applicationDetailResponse{
		applicationResponse: newApplicationResponse(app),
		Answers:             newAnswerList(answers),
		Events:              history,
	})
}

// UpdateStatus handles PATCH /v1/applications/:id/status: an admin moves the
// application one step through the pipeline, or rejects it. The route must be
// behind RequireRole(domain.RoleAdmin).
//
// A move the lifecycle does not allow is a 409 listing the statuses that
// would be allowed; an offer on a post with no vacancy left is a 409 saying
// how full it is.
func (h *ApplicationHandler) UpdateStatus(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("update application status: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req updateStatusRequest
	if !decodeJSON(c, &req) {
		return
	}

	// 422 for a word that is not a status at all; 409 is kept for a real
	// status the application cannot move to from where it is.
	if !domain.IsApplicationStatus(req.Status) {
		var invalid domain.ValidationError
		invalid.Add("status", "must be one of submitted, shortlisted, interview, offer, rejected")
		Respond(c, invalid.Err())
		return
	}

	app, err := h.apps.TransitionApplication(c.Request.Context(), id, req.Status, user.ID)
	if err != nil {
		Respond(c, fmt.Errorf("update application status: %w", err))
		return
	}

	c.JSON(http.StatusOK, newApplicationResponse(app))
}

// Submit handles POST /v1/job-posts/:id/applications: the caller applies to
// the job post. The route must be behind RequireRole(domain.RoleCandidate).
//
// Every rule is checked by the store, inside the transaction that writes the
// application, so the post cannot close between the check and the write.
// Problems with the answers and the CV link come back together, as one 422.
func (h *ApplicationHandler) Submit(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("submit application: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req submitApplicationRequest
	if !decodeJSON(c, &req) {
		return
	}

	in := domain.NewApplication{
		JobPostID:   postID,
		CandidateID: user.ID,
		CVURL:       req.CVURL,
		Answers:     make([]domain.NewAnswer, 0, len(req.Answers)),
	}
	for _, a := range req.Answers {
		in.Answers = append(in.Answers, domain.NewAnswer{
			// The store returns IDs in lowercase, and the form is matched by
			// string comparison, so an uppercase UUID would not be found.
			QuestionID: strings.ToLower(strings.TrimSpace(a.QuestionID)),
			Answer:     string(a.Answer),
		})
	}

	app, err := h.apps.SubmitApplication(c.Request.Context(), in)
	if err != nil {
		Respond(c, fmt.Errorf("submit application: %w", err))
		return
	}

	c.Header("Location", "/v1/applications/"+app.ID)
	c.JSON(http.StatusCreated, submittedApplicationResponse{
		applicationResponse: newApplicationResponse(app),
		Answers:             newAnswerList(app.Answers),
	})
}
