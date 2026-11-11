package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ptr returns a pointer to v. Go has no syntax for the address of a literal
// such as &"text" or &3, so tests that fill pointer fields need this.
func ptr[T any](v T) *T {
	return &v
}

func TestNewJobPostValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   NewJobPost

		// wantFields is the exact set of fields reported; empty means valid.
		wantFields []string
	}{
		{"title and one vacancy", NewJobPost{Title: "Backend Engineer", Vacancies: 1}, nil},
		{"title of exactly 200 characters", NewJobPost{Title: strings.Repeat("a", 200), Vacancies: 1}, nil},
		{"200 characters that are 400 bytes", NewJobPost{Title: strings.Repeat("é", 200), Vacancies: 1}, nil},
		{"only a minimum salary", NewJobPost{Title: "x", Vacancies: 1, SalaryMin: ptr(Money(100))}, nil},
		{"only a maximum salary", NewJobPost{Title: "x", Vacancies: 1, SalaryMax: ptr(Money(100))}, nil},
		{"minimum equal to maximum", NewJobPost{Title: "x", Vacancies: 1, SalaryMin: ptr(Money(100)), SalaryMax: ptr(Money(100))}, nil},
		{"minimum below maximum", NewJobPost{Title: "x", Vacancies: 1, SalaryMin: ptr(Money(99)), SalaryMax: ptr(Money(100))}, nil},

		{"empty title", NewJobPost{Vacancies: 1}, []string{"title"}},
		{"title of 201 characters", NewJobPost{Title: strings.Repeat("a", 201), Vacancies: 1}, []string{"title"}},
		{"zero vacancies", NewJobPost{Title: "x", Vacancies: 0}, []string{"vacancies"}},
		{"negative vacancies", NewJobPost{Title: "x", Vacancies: -1}, []string{"vacancies"}},
		{"more vacancies than the column holds", NewJobPost{Title: "x", Vacancies: 1 << 31}, []string{"vacancies"}},
		{"minimum above maximum", NewJobPost{Title: "x", Vacancies: 1, SalaryMin: ptr(Money(101)), SalaryMax: ptr(Money(100))}, []string{"salary_min"}},
		{"everything wrong at once", NewJobPost{SalaryMin: ptr(Money(2)), SalaryMax: ptr(Money(1))}, []string{"salary_min", "title", "vacancies"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.in.Validate()

			if len(tt.wantFields) == 0 {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}

			var invalid *ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("Validate() = %v, want a *ValidationError", err)
			}
			if len(invalid.Fields) != len(tt.wantFields) {
				t.Errorf("fields = %v, want exactly %v", invalid.Fields, tt.wantFields)
			}
			for _, field := range tt.wantFields {
				if invalid.Fields[field] == "" {
					t.Errorf("no problem reported for %q; got %v", field, invalid.Fields)
				}
			}
		})
	}
}

func TestJobPostPatchIsEmpty(t *testing.T) {
	t.Parallel()

	if !(JobPostPatch{}).IsEmpty() {
		t.Errorf("zero patch: IsEmpty() = false, want true")
	}

	// A pointer to a zero value is still a field that was set.
	nonEmpty := map[string]JobPostPatch{
		"title set to empty":     {Title: ptr("")},
		"vacancies set to zero":  {Vacancies: ptr(0)},
		"salary set to zero":     {SalaryMin: ptr(Money(0))},
		"closes_at set to zero":  {ClosesAt: ptr(time.Time{})},
		"description set":        {Description: ptr("text")},
		"employment type set":    {EmploymentType: ptr("full-time")},
		"location set":           {Location: ptr("Dhaka")},
		"maximum salary set":     {SalaryMax: ptr(Money(1))},
		"two fields set at once": {Title: ptr("x"), Location: ptr("y")},
	}
	for name, patch := range nonEmpty {
		if patch.IsEmpty() {
			t.Errorf("%s: IsEmpty() = true, want false", name)
		}
	}
}

func TestJobPostApply(t *testing.T) {
	t.Parallel()

	closes := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)

	original := func() JobPost {
		return JobPost{
			ID:          "post-1",
			Title:       "Backend Engineer",
			Description: ptr("Builds the API."),
			Location:    ptr("Dhaka"),
			Vacancies:   2,
			SalaryMin:   ptr(Money(5000000)),
			SalaryMax:   ptr(Money(9000000)),
			Status:      JobStatusDraft,
			ClosesAt:    &closes,
		}
	}

	t.Run("an empty patch changes nothing", func(t *testing.T) {
		t.Parallel()

		post := original()
		post.Apply(JobPostPatch{})

		want := original()
		if post.Title != want.Title || *post.Description != *want.Description || *post.Location != *want.Location ||
			post.Vacancies != want.Vacancies || *post.SalaryMin != *want.SalaryMin || *post.SalaryMax != *want.SalaryMax ||
			!post.ClosesAt.Equal(*want.ClosesAt) || post.EmploymentType != nil {
			t.Errorf("post changed: %+v", post)
		}
	})

	t.Run("only the fields set are written", func(t *testing.T) {
		t.Parallel()

		post := original()
		post.Apply(JobPostPatch{Title: ptr("Senior Backend Engineer"), EmploymentType: ptr("full-time")})

		if post.Title != "Senior Backend Engineer" {
			t.Errorf("Title = %q, want the patched value", post.Title)
		}
		if post.EmploymentType == nil || *post.EmploymentType != "full-time" {
			t.Errorf("EmploymentType = %v, want full-time", post.EmploymentType)
		}
		if *post.Description != "Builds the API." || post.Vacancies != 2 || *post.SalaryMin != 5000000 {
			t.Errorf("fields the patch did not set were changed: %+v", post)
		}
	})

	// The case a plain string field cannot express.
	t.Run("a field set to empty is written as empty", func(t *testing.T) {
		t.Parallel()

		post := original()
		post.Apply(JobPostPatch{Description: ptr(""), Vacancies: ptr(0), SalaryMin: ptr(Money(0))})

		if post.Description == nil || *post.Description != "" {
			t.Errorf("Description = %v, want a pointer to the empty string", post.Description)
		}
		if post.Vacancies != 0 {
			t.Errorf("Vacancies = %d, want 0", post.Vacancies)
		}
		if post.SalaryMin == nil || *post.SalaryMin != 0 {
			t.Errorf("SalaryMin = %v, want a pointer to 0", post.SalaryMin)
		}
		if *post.Location != "Dhaka" {
			t.Errorf("Location = %q, want it untouched", *post.Location)
		}
	})

	t.Run("the salary rule is judged on the merged post", func(t *testing.T) {
		t.Parallel()

		// The patch alone is unobjectionable; it is only wrong next to the
		// stored maximum of 90000.
		post := original()
		post.Apply(JobPostPatch{SalaryMin: ptr(Money(9500000))})

		var invalid *ValidationError
		if err := post.Validate(); !errors.As(err, &invalid) || invalid.Fields["salary_min"] == "" {
			t.Errorf("Validate() = %v, want a problem with salary_min", err)
		}
	})
}
