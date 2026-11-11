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
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/scoring"
)

// RubricStore is the part of the rubric storage the handlers need.
// *store.RubricStore satisfies it.
type RubricStore interface {
	GetRubricByJobPost(ctx context.Context, jobPostID string) (*domain.Rubric, error)
	ReplaceRubric(ctx context.Context, jobPostID, actorID string, criteria []domain.Criterion) (*domain.RubricReplacement, error)
}

// RubricHandler serves the rubric endpoints. Every route it serves must be
// registered behind RequireAuth and RequireRole(domain.RoleAdmin).
type RubricHandler struct {
	rubrics RubricStore
}

// NewRubricHandler returns a RubricHandler that reads and writes rubrics.
func NewRubricHandler(rubrics RubricStore) *RubricHandler {
	return &RubricHandler{rubrics: rubrics}
}

// replaceRubricRequest is the body of PUT /v1/job-posts/:id/rubric: the
// whole rubric, every time.
type replaceRubricRequest struct {
	Criteria []criterionRequest `json:"criteria"`
}

// criterionRequest is one criterion as a client sends it.
type criterionRequest struct {
	Label  string  `json:"label"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"weight"`

	// QuestionID is a pointer so null and absent both mean "no question",
	// which is what boolean_all needs.
	QuestionID *string `json:"question_id"`

	// Config is kept as raw JSON. Only the scoring package knows the shape
	// each kind expects, and it is the one to decode it.
	Config json.RawMessage `json:"config"`
}

// criterionResponse is one criterion as a client sees it.
type criterionResponse struct {
	ID         string          `json:"id"`
	Label      string          `json:"label"`
	Kind       string          `json:"kind"`
	Weight     float64         `json:"weight"`
	QuestionID *string         `json:"question_id"`
	Config     json.RawMessage `json:"config"`
}

// rubricResponse is the body of GET /v1/job-posts/:id/rubric.
type rubricResponse struct {
	JobPostID string `json:"job_post_id"`

	// Version is null while the post has no rubric.
	Version  *int                `json:"version"`
	Criteria []criterionResponse `json:"criteria"`
}

// replacedRubricResponse is the body of PUT /v1/job-posts/:id/rubric: the
// new rubric, flattened as submittedApplicationResponse is, plus what the
// replacement left behind.
type replacedRubricResponse struct {
	rubricResponse

	// PreviousVersion is null if the post had no rubric before.
	PreviousVersion *int `json:"previous_version"`

	// ApplicationsToRescore counts the applications still carrying a score
	// from an earlier version, or none. Nothing is rescored automatically.
	ApplicationsToRescore int `json:"applications_to_rescore"`
}

// newRubricResponse converts a rubric for a response.
func newRubricResponse(r *domain.Rubric) rubricResponse {
	out := rubricResponse{
		JobPostID: r.JobPostID,
		Version:   versionOrNull(r.Version),
		Criteria:  make([]criterionResponse, 0, len(r.Criteria)),
	}
	for _, c := range r.Criteria {
		var questionID *string
		if c.QuestionID != "" {
			// A copy, so the pointer does not alias the loop variable's field.
			id := c.QuestionID
			questionID = &id
		}
		out.Criteria = append(out.Criteria, criterionResponse{
			ID: c.ID, Label: c.Label, Kind: c.Kind, Weight: c.Weight, QuestionID: questionID, Config: c.Config,
		})
	}
	return out
}

// versionOrNull turns the domain's "0 means none" into a JSON null.
func versionOrNull(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// Get handles GET /v1/job-posts/:id/rubric: the current version of the
// post's rubric, in any status. A post with no rubric gives a null version
// and an empty list, not a 404: the post exists, and so does the empty
// rubric an admin is about to fill.
func (h *RubricHandler) Get(c *gin.Context) {
	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	rubric, err := h.rubrics.GetRubricByJobPost(c.Request.Context(), postID)
	if err != nil {
		Respond(c, fmt.Errorf("get rubric: %w", err))
		return
	}
	c.JSON(http.StatusOK, newRubricResponse(rubric))
}

// Replace handles PUT /v1/job-posts/:id/rubric: the body is the whole new
// rubric, which becomes the next version. Allowed in any status of the post;
// scores already stored are kept, with the version that produced them.
//
// Every problem with the body is reported in one 422: the rules each
// criterion and the rubric as a whole must meet. Problems that need the
// post's form, such as a question from another post, are found by the store
// and come back as a second 422, once the first kind are fixed.
func (h *RubricHandler) Replace(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("replace rubric: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	postID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req replaceRubricRequest
	if !decodeJSON(c, &req) {
		return
	}

	criteria, err := criteriaFromRequest(req.Criteria)
	if err != nil {
		Respond(c, err)
		return
	}

	replaced, err := h.rubrics.ReplaceRubric(c.Request.Context(), postID, user.ID, criteria)
	if err != nil {
		Respond(c, fmt.Errorf("replace rubric: %w", err))
		return
	}

	c.JSON(http.StatusOK, replacedRubricResponse{
		rubricResponse:        newRubricResponse(&replaced.Rubric),
		PreviousVersion:       versionOrNull(replaced.PreviousVersion),
		ApplicationsToRescore: replaced.ApplicationsToRescore,
	})
}

// criteriaFromRequest converts the request's criteria to domain values and
// checks every one, against the rules of the API and of the scoring engine.
// It returns a *domain.ValidationError naming every problem, or nil.
//
// This is the boundary that keeps a malformed config out of the database:
// scoring.ValidateCriteria decodes each config into its kind's own type,
// with unknown keys refused, exactly as the engine will when it scores.
func criteriaFromRequest(in []criterionRequest) ([]domain.Criterion, error) {
	var invalid domain.ValidationError

	// Checked before anything else, so a huge list is refused without
	// validating every entry of it.
	if len(in) > domain.MaxRubricCriteria {
		invalid.Add("criteria", "must contain at most "+strconv.Itoa(domain.MaxRubricCriteria)+" criteria")
		return nil, invalid.Err()
	}

	criteria := make([]domain.Criterion, 0, len(in))
	for i, r := range in {
		prefix := "criteria[" + strconv.Itoa(i) + "]."

		label := strings.TrimSpace(r.Label)
		switch {
		case label == "":
			invalid.Add(prefix+"label", "is required")
		case utf8.RuneCountInString(label) > domain.MaxCriterionLabelLength:
			invalid.Add(prefix+"label", "must be at most "+strconv.Itoa(domain.MaxCriterionLabelLength)+" characters")
		}

		var questionID string
		if r.QuestionID != nil {
			// Lowercased because the form is matched by string comparison,
			// and the database returns IDs in lowercase.
			questionID = strings.ToLower(strings.TrimSpace(*r.QuestionID))
			if questionID != "" && !isUUID(questionID) {
				invalid.Add(prefix+"question_id", "must be a UUID")
			}
		}

		config, problem := normaliseConfig(r.Config)
		if problem != "" {
			invalid.Add(prefix+"config", problem)
		}

		criteria = append(criteria, domain.Criterion{
			Label:      label,
			Kind:       strings.TrimSpace(r.Kind),
			Weight:     r.Weight,
			Config:     config,
			QuestionID: questionID,
		})
	}

	// The engine's own rules: the kind exists, its config decodes, a kind
	// that reads one answer names a question, weights sum to 100. Added after
	// the checks above, so a problem both find is reported in the engine's
	// words, which are the more specific.
	mergeValidation(&invalid, scoring.ValidateCriteria(criteria))

	if err := invalid.Err(); err != nil {
		return nil, err
	}
	return criteria, nil
}

// normaliseConfig returns the config to store for raw, or a problem to
// report. Absent and null both become {}, so the column never holds a JSON
// null; anything that is not a JSON object is refused here, whatever the
// kind, so the message does not depend on whether the kind was spelt right.
func normaliseConfig(raw json.RawMessage) (json.RawMessage, string) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{}`), ""
	}
	// The decoder has already checked that raw is valid JSON, so its first
	// byte says what kind of value it is. The value is still returned, so
	// that the engine, given the same input, reaches the same verdict
	// rather than a misleading one about a config it never saw.
	if trimmed[0] != '{' {
		return trimmed, "must be a JSON object"
	}
	return trimmed, ""
}
