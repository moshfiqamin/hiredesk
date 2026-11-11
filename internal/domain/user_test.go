package domain

import "testing"

func TestIsRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role string
		want bool
	}{
		{RoleAdmin, true},
		{RoleInterviewer, true},
		{RoleCandidate, true},
		{"", false},
		{"Admin", false}, // roles are compared exactly, as the database does
		{"superuser", false},
	}

	for _, tt := range tests {
		if got := IsRole(tt.role); got != tt.want {
			t.Errorf("IsRole(%q) = %v, want %v", tt.role, got, tt.want)
		}
	}
}
