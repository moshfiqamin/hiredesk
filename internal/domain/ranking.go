package domain

import "time"

// The page size of a ranking: how many applicants it returns by default, and
// at most. The most is large enough that a post's whole pool fits on one page
// at the scale this project targets.
const (
	DefaultRankingLimit = 50
	MaxRankingLimit     = 500
)

// RankedApplication is one row of a job post's ranking.
type RankedApplication struct {
	// Rank is the position in the ranking, from 1.
	Rank int

	ApplicationID string
	CandidateID   string
	CandidateName string
	Status        string
	SubmittedAt   time.Time

	// Totals is nil for an application with no stored score, which ranks
	// below every scored one.
	Totals *ScoreTotals
}
