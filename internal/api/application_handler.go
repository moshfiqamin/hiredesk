package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// ApplicationStore is the part of the application storage the handlers need.
// *store.ApplicationStore satisfies it.
type ApplicationStore interface {
	ListApplications(ctx context.Context, viewer domain.User) ([]domain.Application, error)
	GetApplication(ctx context.Context, id, viewerID string) (app *domain.Application, assigned bool, err error)
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

// listResponse wraps a list in an object rather than sending a bare array, so
// fields such as a pagination cursor can be added later without breaking
// clients.
type listResponse[T any] struct {
	Data []T `json:"data"`
}

// List handles GET /v1/applications. It returns the applications the caller
// may see; which ones is decided by the query the store runs for their role.
func (h *ApplicationHandler) List(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		Respond(c, errors.New("list applications: no authenticated user; is the route behind RequireAuth?"))
		return
	}

	apps, err := h.apps.ListApplications(c.Request.Context(), user)
	if err != nil {
		Respond(c, fmt.Errorf("list applications: %w", err))
		return
	}

	// make with a length of 0 and a capacity of len(apps): one allocation,
	// and an empty result still encodes as [] rather than null.
	out := make([]applicationResponse, 0, len(apps))
	for i := range apps {
		// &apps[i] points into the slice. Taking the address of a range
		// variable copy would also work since Go 1.22, but this avoids the
		// copy entirely.
		out = append(out, newApplicationResponse(&apps[i]))
	}

	c.JSON(http.StatusOK, listResponse[applicationResponse]{Data: out})
}

// Get handles GET /v1/applications/:id.
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

	c.JSON(http.StatusOK, newApplicationResponse(app))
}
