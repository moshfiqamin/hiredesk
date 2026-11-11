package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// Machine-readable code for a cursor this API did not issue.
const codeInvalidCursor = "invalid_cursor"

// JobListingStore is the part of the job post storage the public listing
// needs. *store.JobPostStore satisfies it.
type JobListingStore interface {
	ListOpenJobPosts(ctx context.Context, after *domain.ListingPosition, limit int) (listings []domain.JobListing, more bool, err error)
}

// JobListingHandler serves the public job listing. Its route needs no
// authentication, so nothing it returns may depend on, or reveal anything
// beyond, what any visitor may see.
type JobListingHandler struct {
	listings JobListingStore
}

// NewJobListingHandler returns a JobListingHandler that reads from listings.
func NewJobListingHandler(listings JobListingStore) *JobListingHandler {
	return &JobListingHandler{listings: listings}
}

// jobListingResponse is one open post in the listing. It is a separate type
// from jobPostResponse so that adding a field to the admin view can never
// publish it here by accident.
type jobListingResponse struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Location           *string  `json:"location"`
	EmploymentType     *string  `json:"employment_type"`
	SkillTags          []string `json:"skill_tags"`
	RemainingVacancies int      `json:"remaining_vacancies"`
}

// jobListingPage is the envelope of GET /v1/job-posts:
//
//	{"data":[…], "next_cursor":"…"}
//
// NextCursor is a pointer so that the last page sends null, a value a client
// can test for, rather than an empty string. The key is always present.
type jobListingPage struct {
	Data       []jobListingResponse `json:"data"`
	NextCursor *string              `json:"next_cursor"`
}

// List handles GET /v1/job-posts?limit=20&after=<cursor>: open job posts,
// newest first, one page at a time.
//
// limit defaults to 20 and is capped at 100: a larger value is served as 100.
// after is the next_cursor of the previous page; without it the listing
// starts from the newest post.
func (h *JobListingHandler) List(c *gin.Context) {
	limit, ok := listingLimit(c)
	if !ok {
		return
	}

	var after *domain.ListingPosition
	// An empty after is the same as none, so a client can always send the
	// parameter and leave it blank for the first page.
	if raw := c.Query("after"); raw != "" {
		position, err := decodeCursor(raw)
		if err != nil {
			// 400, not 422: the value is not a cursor at all, and only a
			// fresh start from the first page can fix it.
			write(c, http.StatusBadRequest, codeInvalidCursor,
				"The after parameter is not a cursor this API issued. Start again from the first page.", nil)
			return
		}
		after = &position
	}

	listings, more, err := h.listings.ListOpenJobPosts(c.Request.Context(), after, limit)
	if err != nil {
		Respond(c, fmt.Errorf("list open job posts: %w", err))
		return
	}

	page := jobListingPage{Data: make([]jobListingResponse, 0, len(listings))}
	for i := range listings {
		l := &listings[i]

		tags := l.SkillTags
		if tags == nil {
			tags = []string{}
		}
		page.Data = append(page.Data, jobListingResponse{
			ID:                 l.ID,
			Title:              l.Title,
			Location:           l.Location,
			EmploymentType:     l.EmploymentType,
			SkillTags:          tags,
			RemainingVacancies: l.RemainingVacancies,
		})
	}

	// The cursor is the sort key of the last row on this page. Only issued
	// when the store saw a further row, so a client stops on null instead of
	// requesting one more, empty, page.
	if more && len(listings) > 0 {
		last := listings[len(listings)-1]
		cursor := encodeCursor(domain.ListingPosition{CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = &cursor
	}

	c.JSON(http.StatusOK, page)
}

// listingLimit reads the limit query parameter. It writes a 422 and returns
// false for anything that is not a whole number of at least 1. A number above
// the cap is not an error; it is lowered to the cap.
func listingLimit(c *gin.Context) (int, bool) {
	raw, present := c.GetQuery("limit")
	if !present {
		return domain.DefaultListingLimit, true
	}

	// Atoi refuses "1.5", "20abc", "" and anything beyond the int range, so
	// a value too large to parse is refused rather than wrapping around.
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		var invalid domain.ValidationError
		invalid.Add("limit", "must be a whole number of at least 1; at most "+strconv.Itoa(domain.MaxListingLimit)+" are returned")
		Respond(c, invalid.Err())
		return 0, false
	}

	return min(limit, domain.MaxListingLimit), true
}
