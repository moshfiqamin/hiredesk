package domain

import "time"

// Page sizes for the public job listing.
const (
	// DefaultListingLimit is the page size when the client asks for none.
	DefaultListingLimit = 20

	// MaxListingLimit is the largest page a client may ask for. A larger
	// request is served at this size.
	MaxListingLimit = 100
)

// JobListing is an open job post as the public listing shows it: what a
// visitor needs to decide whether to look closer, and nothing an anonymous
// reader should not see.
type JobListing struct {
	ID             string
	Title          string
	Location       *string
	EmploymentType *string

	// SkillTags are the keywords the post's current rubric screens CVs for,
	// lower case, sorted, each once.
	SkillTags []string

	// RemainingVacancies is vacancies minus applications at the offer stage,
	// never below zero.
	RemainingVacancies int

	// CreatedAt is half of the listing's sort key; see ListingPosition.
	CreatedAt time.Time
}

// ListingPosition is a place in a newest-first listing: the sort key of the
// last row a client has seen. The next page is every row that sorts after it.
//
// The public job listing orders by created_at, and the applications list by
// submitted_at, each with id breaking ties. CreatedAt holds whichever time
// the listing sorts by. Both values are fixed for the life of a row, which is
// what makes a position stay valid while rows are added or changed around it.
type ListingPosition struct {
	CreatedAt time.Time
	ID        string
}
