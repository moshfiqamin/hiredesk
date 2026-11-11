package domain

import (
	"errors"
	"slices"
	"testing"
)

// Every pair of statuses, plus values that are not statuses, against
// CanTransition. Listing all of them, not just the legal ones, is the point:
// a lifecycle bug is almost always a move that should have been refused.
func TestCanTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from, to string
		want     bool
	}{
		{JobStatusDraft, JobStatusOpen, true},
		{JobStatusOpen, JobStatusClosed, true},

		// Skipping a step.
		{JobStatusDraft, JobStatusClosed, false},

		// Going back.
		{JobStatusOpen, JobStatusDraft, false},
		{JobStatusClosed, JobStatusOpen, false},
		{JobStatusClosed, JobStatusDraft, false},

		// Staying put is not a transition.
		{JobStatusDraft, JobStatusDraft, false},
		{JobStatusOpen, JobStatusOpen, false},
		{JobStatusClosed, JobStatusClosed, false},

		// Values that are not statuses, on either side.
		{"", JobStatusOpen, false},
		{JobStatusDraft, "", false},
		{"Draft", JobStatusOpen, false}, // compared exactly, as the database does
		{JobStatusDraft, "Open", false},
		{"archived", JobStatusOpen, false},
		{JobStatusOpen, "archived", false},
	}

	for _, tt := range tests {
		t.Run(tt.from+" → "+tt.to, func(t *testing.T) {
			t.Parallel()

			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}

			// CheckTransition must agree with CanTransition in every case.
			err := CheckTransition(tt.from, tt.to)
			if tt.want && err != nil {
				t.Errorf("CheckTransition(%q, %q) = %v, want nil", tt.from, tt.to, err)
			}
			if !tt.want && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("CheckTransition(%q, %q) = %v, want ErrInvalidTransition", tt.from, tt.to, err)
			}
		})
	}
}

// The table above names three statuses. This fails if the lifecycle gains or
// loses one without the table being revisited.
func TestLifecycleCoversEveryStatus(t *testing.T) {
	t.Parallel()

	got := make([]string, 0, len(jobPostTransitions))
	for status := range jobPostTransitions {
		got = append(got, status)
	}
	slices.Sort(got)

	want := []string{JobStatusClosed, JobStatusDraft, JobStatusOpen}
	if !slices.Equal(got, want) {
		t.Errorf("lifecycle statuses = %v, want %v", got, want)
	}

	// Every destination must itself be a status, or a post could be moved
	// somewhere it can never leave and the database would refuse.
	for from, next := range jobPostTransitions {
		for _, to := range next {
			if _, ok := jobPostTransitions[to]; !ok {
				t.Errorf("%s may move to %q, which is not in the lifecycle", from, to)
			}
		}
	}
}

func TestAllowedTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from string
		want []string
	}{
		{JobStatusDraft, []string{JobStatusOpen}},
		{JobStatusOpen, []string{JobStatusClosed}},
		{JobStatusClosed, []string{}},
		{"archived", []string{}},
	}
	for _, tt := range tests {
		got := AllowedTransitions(tt.from)
		if got == nil || !slices.Equal(got, tt.want) {
			t.Errorf("AllowedTransitions(%q) = %#v, want %#v", tt.from, got, tt.want)
		}
	}

	t.Run("the result is a copy", func(t *testing.T) {
		t.Parallel()

		got := AllowedTransitions(JobStatusDraft)
		got[0] = JobStatusClosed

		if !CanTransition(JobStatusDraft, JobStatusOpen) || CanTransition(JobStatusDraft, JobStatusClosed) {
			t.Errorf("changing the returned slice changed the lifecycle")
		}
	})
}

func TestTransitionErrorDetails(t *testing.T) {
	t.Parallel()

	err := CheckTransition(JobStatusClosed, JobStatusOpen)

	var refused *TransitionError
	if !errors.As(err, &refused) {
		t.Fatalf("CheckTransition = %v, want a *TransitionError", err)
	}

	details := refused.Details()
	if details["current_status"] != JobStatusClosed || details["requested_status"] != JobStatusOpen {
		t.Errorf("details = %v", details)
	}
	allowed, ok := details["allowed_next_statuses"].([]string)
	if !ok || allowed == nil || len(allowed) != 0 {
		t.Errorf("allowed_next_statuses = %#v, want an empty, non-nil []string", details["allowed_next_statuses"])
	}
}

func TestCheckReadyToOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   OpenReadiness
		want []string // the keys reported missing; empty means ready
	}{
		{"questions and a weighted rubric", OpenReadiness{Questions: 1, Criteria: 1, WeightedCriteria: 1}, nil},
		{"some criteria weighted zero", OpenReadiness{Questions: 3, Criteria: 4, WeightedCriteria: 1}, nil},

		{"nothing at all", OpenReadiness{}, []string{"questions", "rubric"}},
		{"no questions", OpenReadiness{Criteria: 1, WeightedCriteria: 1}, []string{"questions"}},
		{"no rubric", OpenReadiness{Questions: 2}, []string{"rubric"}},
		{"every criterion weighted zero", OpenReadiness{Questions: 2, Criteria: 3}, []string{"rubric"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckReadyToOpen(tt.in)
			if len(tt.want) == 0 {
				if err != nil {
					t.Errorf("CheckReadyToOpen(%+v) = %v, want nil", tt.in, err)
				}
				return
			}

			var notReady *NotReadyError
			if !errors.As(err, &notReady) || !errors.Is(err, ErrNotReady) {
				t.Fatalf("CheckReadyToOpen(%+v) = %v, want a *NotReadyError", tt.in, err)
			}
			if len(notReady.Missing) != len(tt.want) {
				t.Errorf("missing = %v, want exactly %v", notReady.Missing, tt.want)
			}
			for _, key := range tt.want {
				if notReady.Missing[key] == "" {
					t.Errorf("no message for %q; missing = %v", key, notReady.Missing)
				}
			}
		})
	}

	t.Run("a zero-weight rubric is not reported as absent", func(t *testing.T) {
		t.Parallel()

		var absent, unweighted *NotReadyError
		errors.As(CheckReadyToOpen(OpenReadiness{Questions: 1}), &absent)
		errors.As(CheckReadyToOpen(OpenReadiness{Questions: 1, Criteria: 2}), &unweighted)

		if absent == nil || unweighted == nil || absent.Missing["rubric"] == unweighted.Missing["rubric"] {
			t.Errorf("both cases give the same message, so the admin cannot tell which to fix")
		}
	})
}

// The messages go to logs, so they must name the facts and be stable.
func TestLifecycleErrorMessages(t *testing.T) {
	t.Parallel()

	if got, want := CheckTransition(JobStatusClosed, JobStatusOpen).Error(), "status change not allowed: closed to open"; got != want {
		t.Errorf("TransitionError.Error() = %q, want %q", got, want)
	}
	if got, want := CheckReadyToOpen(OpenReadiness{}).Error(), "not ready for this status: missing questions rubric"; got != want {
		t.Errorf("NotReadyError.Error() = %q, want %q", got, want)
	}

	var notReady *NotReadyError
	if !errors.As(CheckReadyToOpen(OpenReadiness{Questions: 1}), &notReady) {
		t.Fatal("want a *NotReadyError")
	}
	if details := notReady.Details(); len(details) != 1 || details["rubric"] != notReady.Missing["rubric"] {
		t.Errorf("Details() = %v, want it to mirror Missing %v", details, notReady.Missing)
	}
}
