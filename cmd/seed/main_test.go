package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/moshfiqamin/hiredesk/internal/domain"
)

// memoryUsers is an in-memory userStore with unique emails.
type memoryUsers struct {
	byEmail map[string]domain.NewUser

	// raceOn names an email that GetUserByEmail reports as absent although
	// CreateUser will find it taken, as if another run had just inserted it.
	raceOn string
}

func (m *memoryUsers) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	if _, ok := m.byEmail[email]; ok && email != m.raceOn {
		return &domain.User{Email: email}, nil
	}
	return nil, domain.ErrNotFound
}

func (m *memoryUsers) CreateUser(_ context.Context, in domain.NewUser) (*domain.User, error) {
	if _, ok := m.byEmail[in.Email]; ok {
		return nil, domain.ErrDuplicate
	}
	m.byEmail[in.Email] = in
	return &domain.User{Email: in.Email}, nil
}

// fakeHash marks its input instead of spending a third of a second on bcrypt.
func fakeHash(plain string) (string, error) { return "hashed:" + plain, nil }

func TestSeedCreatesOneOfEachRoleAndIsIdempotent(t *testing.T) {
	t.Parallel()

	users := &memoryUsers{byEmail: make(map[string]domain.NewUser)}

	var first bytes.Buffer
	created, err := seed(context.Background(), users, fakeHash, log.New(&first, "", 0))
	if err != nil {
		t.Fatalf("seed() unexpected error: %v", err)
	}
	if created != 4 {
		t.Errorf("created = %d, want 4", created)
	}

	roles := make(map[string]int)
	for email, user := range users.byEmail {
		roles[user.Role]++

		if !strings.HasSuffix(email, "@hiredesk.test") {
			t.Errorf("%s is not under the reserved .test domain", email)
		}
		if !strings.HasPrefix(user.PasswordHash, "hashed:") {
			t.Errorf("%s was stored with %q, want the output of the hash function", email, user.PasswordHash)
		}
	}
	if roles[domain.RoleAdmin] != 1 || roles[domain.RoleInterviewer] != 1 || roles[domain.RoleCandidate] != 2 {
		t.Errorf("roles = %v, want 1 admin, 1 interviewer, 2 candidates", roles)
	}
	if admin, ok := users.byEmail["admin@hiredesk.test"]; !ok || admin.Role != domain.RoleAdmin {
		t.Errorf("admin@hiredesk.test = %+v, want an admin", admin)
	}

	// Second run: nothing created, nothing changed, no error.
	before := len(users.byEmail)

	var second bytes.Buffer
	created, err = seed(context.Background(), users, func(string) (string, error) {
		t.Errorf("the second run hashed a password, want every account skipped")
		return "", nil
	}, log.New(&second, "", 0))
	if err != nil {
		t.Fatalf("second seed() unexpected error: %v", err)
	}
	if created != 0 {
		t.Errorf("second run created %d, want 0", created)
	}
	if len(users.byEmail) != before {
		t.Errorf("second run changed the number of users from %d to %d", before, len(users.byEmail))
	}
	if got := strings.Count(second.String(), "exists"); got != 4 {
		t.Errorf("second run reported %d existing accounts, want 4:\n%s", got, &second)
	}
}

// Two runs at once: the lookup says "absent" and the insert says "taken".
func TestSeedSurvivesALostRace(t *testing.T) {
	t.Parallel()

	users := &memoryUsers{
		byEmail: map[string]domain.NewUser{"admin@hiredesk.test": {Email: "admin@hiredesk.test"}},
		raceOn:  "admin@hiredesk.test",
	}

	var out bytes.Buffer
	created, err := seed(context.Background(), users, fakeHash, log.New(&out, "", 0))
	if err != nil {
		t.Fatalf("seed() unexpected error: %v", err)
	}
	if created != 3 {
		t.Errorf("created = %d, want the 3 that were really absent", created)
	}
}

func TestSeedStopsOnADatabaseError(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection refused")

	_, err := seed(context.Background(), failingUsers{boom}, fakeHash, log.New(&bytes.Buffer{}, "", 0))
	if !errors.Is(err, boom) {
		t.Errorf("seed() error = %v, want it to wrap %v", err, boom)
	}
}

// failingUsers is a database that is down.
type failingUsers struct{ err error }

func (f failingUsers) GetUserByEmail(context.Context, string) (*domain.User, error) {
	return nil, f.err
}

func (f failingUsers) CreateUser(context.Context, domain.NewUser) (*domain.User, error) {
	return nil, f.err
}
