// Command seed creates a fixed set of development accounts, one or more per
// role, so the API can be explored without registering by hand.
//
// It is safe to run repeatedly: an account that already exists is left
// exactly as it is.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/moshfiqamin/hiredesk/internal/auth"
	"github.com/moshfiqamin/hiredesk/internal/config"
	"github.com/moshfiqamin/hiredesk/internal/domain"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

// account is one user to create.
type account struct {
	email    string
	password string
	fullName string
	role     string
}

// accounts are published in the README and exist only so a development
// database has someone to log in as. They are not secrets, which is exactly
// why this command refuses to run in production.
//
// .test is a top-level domain reserved by RFC 2606 that can never be
// registered, so none of these can collide with, or send mail to, a real
// address.
var accounts = []account{
	{"admin@hiredesk.test", "admin-password", "Ada Admin", domain.RoleAdmin},
	{"interviewer@hiredesk.test", "interviewer-password", "Ivan Interviewer", domain.RoleInterviewer},
	{"candidate1@hiredesk.test", "candidate-password", "Chitra Candidate", domain.RoleCandidate},
	{"candidate2@hiredesk.test", "candidate-password", "Carlos Candidate", domain.RoleCandidate},
}

// userStore is the part of *store.UserStore this command uses.
type userStore interface {
	CreateUser(ctx context.Context, in domain.NewUser) (*domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	// An admin account with a password printed in a public README must never
	// exist on a real deployment.
	if cfg.IsProduction() {
		return errors.New("refusing to create accounts with published passwords when ENV=production")
	}

	// Ctrl-C cancels the context, which aborts whichever query is running.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	// No prefix and no flags, so each line is printed as written, without
	// the timestamp the default logger adds.
	created, err := seed(ctx, store.NewUserStore(pool), auth.Hash, log.New(os.Stdout, "", 0))
	if err != nil {
		return err
	}

	fmt.Printf("\n%d created, %d already present\n", created, len(accounts)-created)
	return nil
}

// seed creates each account that does not exist yet and reports what it did
// on out, one line per account. It returns how many it created.
//
// out is a *log.Logger rather than an io.Writer because its Printf returns
// nothing: a progress line that fails to print is not worth an error path.
func seed(ctx context.Context, users userStore, hash func(string) (string, error), out *log.Logger) (int, error) {
	created := 0

	for _, a := range accounts {
		// Looked up first only to skip the hash, which takes a third of a
		// second each. It is not what makes this safe to repeat — the unique
		// index is, through the ErrDuplicate case below.
		_, err := users.GetUserByEmail(ctx, a.email)
		if err == nil {
			out.Printf("exists   %-12s %s", a.role, a.email)
			continue
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return created, fmt.Errorf("look up %s: %w", a.email, err)
		}

		passwordHash, err := hash(a.password)
		if err != nil {
			return created, fmt.Errorf("hash password for %s: %w", a.email, err)
		}

		_, err = users.CreateUser(ctx, domain.NewUser{
			Email:        a.email,
			PasswordHash: passwordHash,
			FullName:     a.fullName,
			Role:         a.role,
		})
		if errors.Is(err, domain.ErrDuplicate) {
			// Another run inserted it between the lookup and here.
			out.Printf("exists   %-12s %s", a.role, a.email)
			continue
		}
		if err != nil {
			return created, fmt.Errorf("create %s: %w", a.email, err)
		}

		created++
		out.Printf("created  %-12s %s  (password: %s)", a.role, a.email, a.password)
	}

	return created, nil
}
