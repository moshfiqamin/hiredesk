package domain

import (
	"errors"
	"reflect"
	"testing"
)

// Every pair of statuses, 5 × 5, plus unknown ones on either side. A pair not
// listed in allowed must be refused; that is the deny-by-default half of the
// table, and the half that matters most.
func TestApplicationTransitions(t *testing.T) {
	t.Parallel()

	allowed := map[[2]string]bool{
		{StatusSubmitted, StatusShortlisted}: true,
		{StatusShortlisted, StatusInterview}: true,
		{StatusInterview, StatusOffer}:       true,
		{StatusSubmitted, StatusRejected}:    true,
		{StatusShortlisted, StatusRejected}:  true,
		{StatusInterview, StatusRejected}:    true,
		{StatusOffer, StatusRejected}:        true,
	}

	statuses := []string{StatusSubmitted, StatusShortlisted, StatusInterview, StatusOffer, StatusRejected, "hired", ""}

	for _, from := range statuses {
		for _, to := range statuses {
			want := allowed[[2]string{from, to}]
			t.Run(from+" → "+to, func(t *testing.T) {
				t.Parallel()

				if got := CanTransitionApplication(from, to); got != want {
					t.Fatalf("CanTransitionApplication = %v, want %v", got, want)
				}

				err := CheckApplicationTransition(from, to)
				if want {
					if err != nil {
						t.Errorf("CheckApplicationTransition = %v, want nil", err)
					}
					return
				}

				var te *TransitionError
				if !errors.As(err, &te) || !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("CheckApplicationTransition = %v, want a *TransitionError", err)
				}
				if te.From != from || te.To != to {
					t.Errorf("error names %s → %s, want %s → %s", te.From, te.To, from, to)
				}
				if !reflect.DeepEqual(te.Allowed, AllowedApplicationTransitions(from)) {
					t.Errorf("Allowed = %v, want %v", te.Allowed, AllowedApplicationTransitions(from))
				}
			})
		}
	}
}

func TestAllowedApplicationTransitions(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		StatusSubmitted:   {StatusShortlisted, StatusRejected},
		StatusShortlisted: {StatusInterview, StatusRejected},
		StatusInterview:   {StatusOffer, StatusRejected},
		StatusOffer:       {StatusRejected},
		StatusRejected:    {},
		"unknown":         {},
	}
	for from, want := range tests {
		got := AllowedApplicationTransitions(from)
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Errorf("AllowedApplicationTransitions(%q) = %#v, want %#v", from, got, want)
		}
	}

	// The result is a copy: changing it must not change the lifecycle.
	got := AllowedApplicationTransitions(StatusSubmitted)
	got[0] = StatusOffer
	if CanTransitionApplication(StatusSubmitted, StatusOffer) {
		t.Error("editing the returned slice changed the lifecycle")
	}
}

func TestIsApplicationStatus(t *testing.T) {
	t.Parallel()

	for _, s := range []string{StatusSubmitted, StatusShortlisted, StatusInterview, StatusOffer, StatusRejected} {
		if !IsApplicationStatus(s) {
			t.Errorf("IsApplicationStatus(%q) = false", s)
		}
	}
	for _, s := range []string{"", "Submitted", "hired", "offer "} {
		if IsApplicationStatus(s) {
			t.Errorf("IsApplicationStatus(%q) = true", s)
		}
	}
}

func TestCheckVacancyForOffer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		vacancies, offers int
		wantRefused       bool
		wantRemaining     int
	}{
		{"no offers yet", 1, 0, false, 0},
		{"one of three used", 3, 1, false, 0},
		{"the last vacancy", 3, 2, false, 0},
		{"every vacancy has an offer", 3, 3, true, 0},
		{"more offers than vacancies, after vacancies were lowered", 2, 3, true, 0},
		{"a single vacancy, taken", 1, 1, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckVacancyForOffer(tt.vacancies, tt.offers)
			if (err != nil) != tt.wantRefused {
				t.Fatalf("err = %v, want refused: %v", err, tt.wantRefused)
			}
			if !tt.wantRefused {
				return
			}

			var nv *NoVacanciesError
			if !errors.As(err, &nv) || !errors.Is(err, ErrNoVacancies) {
				t.Fatalf("err = %v, want a *NoVacanciesError", err)
			}
			want := map[string]any{"vacancies": tt.vacancies, "offers_made": tt.offers, "remaining_vacancies": tt.wantRemaining}
			if got := nv.Details(); !reflect.DeepEqual(got, want) {
				t.Errorf("Details = %v, want %v", got, want)
			}
		})
	}
}
