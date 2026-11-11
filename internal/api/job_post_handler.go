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

// problemMoney is what a client is told about a salary ParseMoney rejects.
const problemMoney = "must be a non-negative amount with at most two decimal places, such as 85000 or 85000.50"

// JobPostStore is the part of the job post storage the handlers need.
// *store.JobPostStore satisfies it.
type JobPostStore interface {
	CreateJobPost(ctx context.Context, in domain.NewJobPost) (*domain.JobPost, error)
	GetJobPostByID(ctx context.Context, id string, includeDrafts bool) (*domain.JobPost, error)
	UpdateJobPost(ctx context.Context, id string, patch domain.JobPostPatch) (*domain.JobPost, error)
}

// JobPostHandler serves the job post endpoints. Every route it serves must be
// registered behind RequireAuth, and Create and Update behind
// RequireRole(domain.RoleAdmin) as well.
type JobPostHandler struct {
	posts JobPostStore
}

// NewJobPostHandler returns a JobPostHandler that reads and writes posts.
func NewJobPostHandler(posts JobPostStore) *JobPostHandler {
	return &JobPostHandler{posts: posts}
}

// createJobPostRequest is the body of POST /v1/job-posts.
//
// Title is a plain string because it is required: a missing title and an
// empty one are the same mistake and get the same answer. Every optional
// field is a pointer, so "not sent" (nil) stays distinct from a value.
// Vacancies shows why that matters: absent means "use the default of 1",
// while an explicit 0 is an error, and an int could not tell them apart.
type createJobPostRequest struct {
	Title          string  `json:"title"`
	Description    *string `json:"description"`
	EmploymentType *string `json:"employment_type"`
	Location       *string `json:"location"`
	Vacancies      *int    `json:"vacancies"`

	// json.Number keeps the digits exactly as the client wrote them. Decoded
	// into a float64, 85000.10 would already have been rounded to the nearest
	// binary fraction before any code here saw it.
	SalaryMin *json.Number `json:"salary_min"`
	SalaryMax *json.Number `json:"salary_max"`

	// Read as RFC 3339, e.g. "2026-12-31T23:59:59Z".
	ClosesAt *time.Time `json:"closes_at"`
}

// updateJobPostRequest is the body of PATCH /v1/job-posts/:id.
//
// Every field is a pointer. After decoding, nil means the key was not in the
// body and the stored value must be kept; non-nil means it was, even when it
// points at "" or 0. A JSON null also leaves the pointer nil, so null is
// treated as "not sent" rather than as "clear this field".
type updateJobPostRequest struct {
	Title          *string      `json:"title"`
	Description    *string      `json:"description"`
	EmploymentType *string      `json:"employment_type"`
	Location       *string      `json:"location"`
	Vacancies      *int         `json:"vacancies"`
	SalaryMin      *json.Number `json:"salary_min"`
	SalaryMax      *json.Number `json:"salary_max"`
	ClosesAt       *time.Time   `json:"closes_at"`
}

// jobPostResponse is a job post as a client sees one. The nullable fields
// are pointers so that an unset one is sent as null rather than as "" or 0.
type jobPostResponse struct {
	ID             string             `json:"id"`
	Title          string             `json:"title"`
	Description    *string            `json:"description"`
	EmploymentType *string            `json:"employment_type"`
	Location       *string            `json:"location"`
	Vacancies      int                `json:"vacancies"`
	SalaryMin      *json.Number       `json:"salary_min"`
	SalaryMax      *json.Number       `json:"salary_max"`
	Status         string             `json:"status"`
	ClosesAt       *time.Time         `json:"closes_at"`
	CreatedBy      *string            `json:"created_by"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	Questions      []questionResponse `json:"questions"`
}

// questionResponse is one field of a job post's application form.
type questionResponse struct {
	ID        string   `json:"id"`
	Prompt    string   `json:"prompt"`
	Kind      string   `json:"kind"`
	Choices   []string `json:"choices"`
	Required  bool     `json:"required"`
	MinLength int      `json:"min_length"`
	Position  int      `json:"position"`
}

func newJobPostResponse(p *domain.JobPost) jobPostResponse {
	out := jobPostResponse{
		ID:             p.ID,
		Title:          p.Title,
		Description:    p.Description,
		EmploymentType: p.EmploymentType,
		Location:       p.Location,
		Vacancies:      p.Vacancies,
		SalaryMin:      moneyJSON(p.SalaryMin),
		SalaryMax:      moneyJSON(p.SalaryMax),
		Status:         p.Status,
		CreatedBy:      p.CreatedBy,
		CreatedAt:      p.CreatedAt.UTC(),
		UpdatedAt:      p.UpdatedAt.UTC(),

		// Length 0, not nil: a post with no questions sends [] and not null.
		Questions: make([]questionResponse, 0, len(p.Questions)),
	}

	if p.ClosesAt != nil {
		// A new variable, so the response does not share a pointer with p.
		closesAt := p.ClosesAt.UTC()
		out.ClosesAt = &closesAt
	}

	for i := range p.Questions {
		q := &p.Questions[i]

		choices := q.Choices
		if choices == nil {
			choices = []string{}
		}
		out.Questions = append(out.Questions, questionResponse{
			ID:        q.ID,
			Prompt:    q.Prompt,
			Kind:      q.Kind,
			Choices:   choices,
			Required:  q.Required,
			MinLength: q.MinLength,
			Position:  q.Position,
		})
	}

	return out
}

// moneyJSON renders m as a JSON number with two decimal places, or as null.
func moneyJSON(m *domain.Money) *json.Number {
	if m == nil {
		return nil
	}
	// json.Number is a string that the encoder writes without quotes, so the
	// client receives 85000.50, digits intact.
	n := json.Number(m.String())
	return &n
}

// parseMoney converts an optional amount from a request. A nil n is "not
// sent" and stays nil. An amount ParseMoney rejects is recorded against field
// in invalid, and nil is returned.
func parseMoney(invalid *domain.ValidationError, field string, n *json.Number) *domain.Money {
	if n == nil {
		return nil
	}

	amount, err := domain.ParseMoney(n.String())
	if err != nil {
		invalid.Add(field, problemMoney)
		return nil
	}
	return &amount
}

// Create handles POST /v1/job-posts. The post is always created as a draft.
func (h *JobPostHandler) Create(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("create job post: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	var req createJobPostRequest
	if !decodeJSON(c, &req) {
		return
	}

	// Problems are collected rather than returned one at a time, so a client
	// that got three fields wrong learns about all three from one request.
	var invalid domain.ValidationError

	in := domain.NewJobPost{
		// Trimmed first, so a title of only spaces counts as empty.
		Title:          strings.TrimSpace(req.Title),
		Description:    req.Description,
		EmploymentType: req.EmploymentType,
		Location:       req.Location,
		Vacancies:      1,
		SalaryMin:      parseMoney(&invalid, "salary_min", req.SalaryMin),
		SalaryMax:      parseMoney(&invalid, "salary_max", req.SalaryMax),
		ClosesAt:       req.ClosesAt,
		CreatedBy:      user.ID,
	}
	if req.Vacancies != nil {
		in.Vacancies = *req.Vacancies
	}

	// The domain's findings are merged into the same set as the parse
	// failures above. errors.As is false when Validate returned nil.
	var broken *domain.ValidationError
	if errors.As(in.Validate(), &broken) {
		for field, problem := range broken.Fields {
			invalid.Add(field, problem)
		}
	}
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	post, err := h.posts.CreateJobPost(c.Request.Context(), in)
	if err != nil {
		Respond(c, fmt.Errorf("create job post: %w", err))
		return
	}

	// A 201 names the new resource, so a client can fetch it without building
	// the URL itself.
	c.Header("Location", "/v1/job-posts/"+post.ID)
	c.JSON(http.StatusCreated, newJobPostResponse(post))
}

// Get handles GET /v1/job-posts/:id. It returns the post with its questions.
//
// A draft is visible to admins only. To anyone else it is reported exactly
// as a post that does not exist. See docs/decisions.md.
func (h *JobPostHandler) Get(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("get job post: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	post, err := h.posts.GetJobPostByID(c.Request.Context(), id, auth.CanViewDraftJobPosts(user))
	if err != nil {
		Respond(c, fmt.Errorf("get job post: %w", err))
		return
	}

	c.JSON(http.StatusOK, newJobPostResponse(post))
}

// Update handles PATCH /v1/job-posts/:id. Only the fields present in the body
// change; the response is the whole post as it now stands.
func (h *JobPostHandler) Update(c *gin.Context) {
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req updateJobPostRequest
	if !decodeJSON(c, &req) {
		return
	}

	var invalid domain.ValidationError

	patch := domain.JobPostPatch{
		Description:    req.Description,
		EmploymentType: req.EmploymentType,
		Location:       req.Location,
		Vacancies:      req.Vacancies,
		SalaryMin:      parseMoney(&invalid, "salary_min", req.SalaryMin),
		SalaryMax:      parseMoney(&invalid, "salary_max", req.SalaryMax),
		ClosesAt:       req.ClosesAt,
	}
	if req.Title != nil {
		// A pointer to a new string: req.Title is left as the client sent it.
		title := strings.TrimSpace(*req.Title)
		patch.Title = &title
	}

	// Only what can be judged without the stored row is rejected here. The
	// rules themselves need the row with the patch applied, so the store runs
	// them inside the transaction that holds it.
	if err := invalid.Err(); err != nil {
		Respond(c, err)
		return
	}

	post, err := h.posts.UpdateJobPost(c.Request.Context(), id, patch)
	if err != nil {
		Respond(c, fmt.Errorf("update job post: %w", err))
		return
	}

	c.JSON(http.StatusOK, newJobPostResponse(post))
}
