package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// ReviewStore is the part of the review storage the handlers need.
// *store.ReviewStore satisfies it.
type ReviewStore interface {
	AssignInterviewer(ctx context.Context, appID, interviewerID, actorID string) (*domain.Assignment, bool, error)
	UnassignInterviewer(ctx context.Context, appID, interviewerID, actorID string) (bool, error)
	ListAssignments(ctx context.Context, appID string) ([]domain.Assignment, error)
	CreateReview(ctx context.Context, appID, interviewerID string, mark int, notes *string) (*domain.Review, error)
	UpdateReview(ctx context.Context, appID, interviewerID string, patch domain.ReviewPatch) (*domain.Review, error)
	ListInterviewers(ctx context.Context) ([]domain.InterviewerLoad, error)
}

// ApplicationGetter loads one application and whether the viewer is assigned
// to it. *store.ApplicationStore satisfies it.
type ApplicationGetter interface {
	GetApplication(ctx context.Context, id, viewerID string) (app *domain.Application, assigned bool, err error)
}

// ReviewHandler serves interviewer assignment and review. Every route it
// serves must be registered behind RequireAuth.
type ReviewHandler struct {
	apps    ApplicationGetter
	reviews ReviewStore
}

// NewReviewHandler returns a ReviewHandler that checks access through apps
// and stores through reviews.
func NewReviewHandler(apps ApplicationGetter, reviews ReviewStore) *ReviewHandler {
	return &ReviewHandler{apps: apps, reviews: reviews}
}

// assignmentResponse is one assignment as a client sees it.
type assignmentResponse struct {
	ApplicationID    string    `json:"application_id"`
	InterviewerID    string    `json:"interviewer_id"`
	InterviewerName  string    `json:"interviewer_name"`
	InterviewerEmail string    `json:"interviewer_email"`
	AssignedBy       *string   `json:"assigned_by"`
	AssignedByName   *string   `json:"assigned_by_name"`
	AssignedAt       time.Time `json:"assigned_at"`
	Reviewed         bool      `json:"reviewed"`
}

func newAssignmentResponse(a domain.Assignment) assignmentResponse {
	return assignmentResponse{
		ApplicationID: a.ApplicationID, InterviewerID: a.InterviewerID,
		InterviewerName: a.InterviewerName, InterviewerEmail: a.InterviewerEmail,
		AssignedBy: a.AssignedBy, AssignedByName: a.AssignedByName,
		AssignedAt: a.AssignedAt.UTC(), Reviewed: a.Reviewed,
	}
}

// reviewResponse is one review as a client sees it.
type reviewResponse struct {
	ID              string    `json:"id"`
	ApplicationID   string    `json:"application_id"`
	InterviewerID   string    `json:"interviewer_id"`
	InterviewerName string    `json:"interviewer_name"`
	Mark            int       `json:"mark"`
	Notes           *string   `json:"notes"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func newReviewResponse(r domain.Review) reviewResponse {
	return reviewResponse{
		ID: r.ID, ApplicationID: r.ApplicationID, InterviewerID: r.InterviewerID,
		InterviewerName: r.InterviewerName, Mark: r.Mark, Notes: r.Notes,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// assignRequest is the body of POST /v1/applications/:id/assign.
type assignRequest struct {
	InterviewerID string `json:"interviewer_id"`
}

// Assign handles POST /v1/applications/:id/assign: an admin assigns an
// interviewer to review the application. The route must be behind
// RequireRole(domain.RoleAdmin).
//
// It is idempotent: the first call creates the assignment and answers 201;
// a repeat, including a client's retry, answers 200 with the same body and
// writes nothing. A target who is not an interviewer is a 422, never an
// assignment: see docs/notes/09-1-assignment.md.
func (h *ReviewHandler) Assign(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("assign interviewer: no authenticated user; is the route behind RequireAuth?"))
		return
	}
	appID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req assignRequest
	if !decodeJSON(c, &req) {
		return
	}
	// The shape is checked here; whether the user is an interviewer only the
	// database can say, so the store checks that.
	var invalid domain.ValidationError
	switch {
	case req.InterviewerID == "":
		invalid.Add("interviewer_id", "is required")
	case !isUUID(req.InterviewerID):
		invalid.Add("interviewer_id", "must be a UUID")
	}
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	assignment, created, err := h.reviews.AssignInterviewer(c.Request.Context(), appID, strings.ToLower(req.InterviewerID), user.ID)
	if err != nil {
		Respond(c, fmt.Errorf("assign interviewer: %w", err))
		return
	}

	// Both are successes, so a retrying client stops either way. The status
	// still tells a careful client whether this call changed anything.
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, newAssignmentResponse(*assignment))
}

// Unassign handles DELETE /v1/applications/:id/assign/:interviewerID: an
// admin removes an interviewer from the application. The route must be
// behind RequireRole(domain.RoleAdmin).
//
// Idempotent, as DELETE is meant to be: 204 whether or not the interviewer
// was assigned, and an event only when they were. A missing application is
// a 404.
func (h *ReviewHandler) Unassign(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("unassign interviewer: no authenticated user; is the route behind RequireAuth?"))
		return
	}
	appID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	interviewerID, ok := pathUUID(c, "interviewerID")
	if !ok {
		return
	}

	if _, err := h.reviews.UnassignInterviewer(c.Request.Context(), appID, strings.ToLower(interviewerID), user.ID); err != nil {
		Respond(c, fmt.Errorf("unassign interviewer: %w", err))
		return
	}
	c.Status(http.StatusNoContent)
}

// Assignments handles GET /v1/applications/:id/assignments: who is assigned
// to the application, and whether each has reviewed it. The route must be
// behind RequireRole(domain.RoleAdmin).
func (h *ReviewHandler) Assignments(c *gin.Context) {
	appID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	assignments, err := h.reviews.ListAssignments(c.Request.Context(), appID)
	if err != nil {
		Respond(c, fmt.Errorf("list assignments: %w", err))
		return
	}

	out := listResponse[assignmentResponse]{Data: make([]assignmentResponse, 0, len(assignments))}
	for _, a := range assignments {
		out.Data = append(out.Data, newAssignmentResponse(a))
	}
	c.JSON(http.StatusOK, out)
}

// interviewerResponse is one row of GET /v1/admin/interviewers.
type interviewerResponse struct {
	ID              string `json:"id"`
	FullName        string `json:"full_name"`
	Email           string `json:"email"`
	OpenAssignments int    `json:"open_assignments"`
	PendingReviews  int    `json:"pending_reviews"`
}

// Interviewers handles GET /v1/admin/interviewers: every interviewer with
// their current load, for the assignment picker. The route must be behind
// RequireRole(domain.RoleAdmin).
func (h *ReviewHandler) Interviewers(c *gin.Context) {
	loads, err := h.reviews.ListInterviewers(c.Request.Context())
	if err != nil {
		Respond(c, fmt.Errorf("list interviewers: %w", err))
		return
	}

	out := listResponse[interviewerResponse]{Data: make([]interviewerResponse, 0, len(loads))}
	for _, l := range loads {
		out.Data = append(out.Data, interviewerResponse{
			ID: l.ID, FullName: l.Name, Email: l.Email,
			OpenAssignments: l.OpenAssignments, PendingReviews: l.PendingReviews,
		})
	}
	c.JSON(http.StatusOK, out)
}

// markValue is a review's mark as sent. It accepts any JSON value without
// failing the decode, and records whether that value is a whole number, so
// that "eight" or 8.5 is reported as a 422 on the mark field rather than as
// a malformed body.
type markValue struct {
	// present is false when the field was absent or null.
	present bool

	// whole is true when the value is an integer literal, and n is its value.
	whole bool
	n     int
}

// UnmarshalJSON implements json.Unmarshaler. It never returns an error: the
// outer decoder has already checked the JSON is well formed, and every
// wrong value becomes a field error later.
func (m *markValue) UnmarshalJSON(data []byte) error {
	// encoding/json calls this with "null" for an explicit null. Treated as
	// absent, as every PATCH in this API treats null.
	if string(data) == "null" {
		return nil
	}
	m.present = true

	// UseNumber keeps a number as its literal text (json.Number) instead of
	// a float64, so 8.0 and 8 stay distinguishable and a huge number does
	// not round.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil //nolint:nilerr // Unreachable for well-formed JSON; left as "not a whole number".
	}

	num, isNumber := v.(json.Number)
	if !isNumber {
		// A string ("eight", or even "8"), a boolean, an array or an object.
		return nil
	}
	// Atoi accepts only an optional sign and digits, so it refuses "8.0",
	// "8.5", "8e0" and anything beyond the int range.
	n, err := strconv.Atoi(num.String())
	if err != nil {
		return nil //nolint:nilerr // Not a whole number: reported by the caller as a field error.
	}
	m.whole, m.n = true, n
	return nil
}

// check adds a problem with the mark to invalid, if there is one. A whole
// number out of range and a value that is no number at all get the same
// message, which says what is accepted.
func (m markValue) check(invalid *domain.ValidationError) {
	if !m.whole {
		invalid.Add("mark", domain.MarkProblem)
		return
	}
	if problem := domain.CheckMark(m.n); problem != "" {
		invalid.Add("mark", problem)
	}
}

// reviewRequest is the body of POST /v1/applications/:id/reviews, and of
// PATCH /v1/applications/:id/reviews/mine, where both fields are optional.
type reviewRequest struct {
	Mark markValue `json:"mark"`

	// Notes is nil when absent or null. In a PATCH, "" removes the notes.
	Notes *string `json:"notes"`
}

// reviewer loads the application in the URL and checks that the caller may
// review it. On failure it writes the response and returns false.
//
// An application the caller may not see is a 404, identical to one that
// does not exist. One they may see but not review is a 403, which today no
// interviewer can reach, since seeing and reviewing both mean "assigned".
func (h *ReviewHandler) reviewer(c *gin.Context, op string) (domain.User, *domain.Application, bool) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, fmt.Errorf("%s: no authenticated user; is the route behind RequireAuth?", op))
		return domain.User{}, nil, false
	}

	app, assigned, err := h.apps.GetApplication(c.Request.Context(), c.Param("id"), user.ID)
	if errors.Is(err, domain.ErrNotFound) {
		Respond(c, domain.ErrNotFound)
		return domain.User{}, nil, false
	}
	if err != nil {
		Respond(c, fmt.Errorf("%s: %w", op, err))
		return domain.User{}, nil, false
	}
	if !auth.CanViewApplication(user, *app, assigned) {
		Respond(c, domain.ErrNotFound)
		return domain.User{}, nil, false
	}
	if !auth.CanReviewApplication(user, *app, assigned) {
		Respond(c, domain.ErrForbidden)
		return domain.User{}, nil, false
	}
	return user, app, true
}

// CreateReview handles POST /v1/applications/:id/reviews: an assigned
// interviewer files their review. The route must be behind
// RequireRole(domain.RoleInterviewer).
//
// mark must be a whole number from 1 to 10 and is required; notes is
// optional, at most domain.MaxReviewNotesChars characters. A second review
// by the same interviewer is a 409 already_reviewed: corrections go through
// PATCH .../reviews/mine.
func (h *ReviewHandler) CreateReview(c *gin.Context) {
	const op = "create review"
	user, app, ok := h.reviewer(c, op)
	if !ok {
		return
	}

	var req reviewRequest
	if !decodeJSON(c, &req) {
		return
	}

	var invalid domain.ValidationError
	if !req.Mark.present {
		invalid.Add("mark", "is required")
	} else {
		req.Mark.check(&invalid)
	}
	var notes *string
	if req.Notes != nil {
		var problem string
		if notes, problem = domain.NormalizeReviewNotes(*req.Notes); problem != "" {
			invalid.Add("notes", problem)
		}
	}
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	review, err := h.reviews.CreateReview(c.Request.Context(), app.ID, user.ID, req.Mark.n, notes)
	if err != nil {
		Respond(c, fmt.Errorf("%s: %w", op, err))
		return
	}
	c.JSON(http.StatusCreated, newReviewResponse(*review))
}

// UpdateMyReview handles PATCH /v1/applications/:id/reviews/mine: an
// assigned interviewer corrects their own review. The route must be behind
// RequireRole(domain.RoleInterviewer).
//
// Only the fields sent change; at least one must be. "notes": "" removes
// the notes. Correcting a review not yet filed is a 404 no_review. "mine"
// is in the URL rather than a review ID, so there is no ID to change to
// reach somebody else's review.
func (h *ReviewHandler) UpdateMyReview(c *gin.Context) {
	const op = "update review"
	user, app, ok := h.reviewer(c, op)
	if !ok {
		return
	}

	var req reviewRequest
	if !decodeJSON(c, &req) {
		return
	}

	var (
		invalid domain.ValidationError
		patch   domain.ReviewPatch
	)
	if !req.Mark.present && req.Notes == nil {
		invalid.Add("mark", "give mark, notes, or both")
	}
	if req.Mark.present {
		req.Mark.check(&invalid)
		patch.Mark = &req.Mark.n
	}
	if req.Notes != nil {
		normalized, problem := domain.NormalizeReviewNotes(*req.Notes)
		if problem != "" {
			invalid.Add("notes", problem)
		}
		// Blank notes mean "remove them", which the store reads as "".
		cleared := ""
		patch.Notes = &cleared
		if normalized != nil {
			patch.Notes = normalized
		}
	}
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	review, err := h.reviews.UpdateReview(c.Request.Context(), app.ID, user.ID, patch)
	if err != nil {
		Respond(c, fmt.Errorf("%s: %w", op, err))
		return
	}
	c.JSON(http.StatusOK, newReviewResponse(*review))
}
