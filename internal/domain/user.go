package domain

import "time"

// The roles a user can hold. They mirror the users_role_check constraint in
// the database, which remains the final guard.
const (
	RoleAdmin       = "admin"
	RoleInterviewer = "interviewer"
	RoleCandidate   = "candidate"
)

// User is one row of the users table, complete.
//
// It has no json tags on purpose: it is never written to a response. The api
// package copies the fields a client may see into its own type.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	FullName     string
	Role         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewUser is what the caller supplies to create a user. The ID and both
// timestamps are absent because the database assigns them.
type NewUser struct {
	Email        string
	PasswordHash string
	FullName     string
	Role         string
}
