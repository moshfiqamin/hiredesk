// Command reindex rebuilds every application's search document from its
// stored answers, rewriting only the ones that have drifted, and reports how
// many that was.
//
// It is the recovery for full-text search: safe to run at any time, in any
// environment, as often as wanted. On a healthy database it rewrites
// nothing. Run it with `make reindex-search`.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/moshfiqamin/hiredesk/internal/config"
	"github.com/moshfiqamin/hiredesk/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("reindex: %v", err)
	}
}

func run() error {
	// The same configuration the API reads, so it connects to the same
	// database with the same pool settings. Unlike cmd/seed it does not
	// refuse production: repairing production is what it is for.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	// Ctrl-C cancels the context, which cancels the statement on the server.
	// The rebuild is one statement, so a cancelled run changes nothing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	rewritten, total, err := store.NewApplicationStore(pool).RebuildSearchDocuments(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("search documents: %d of %d rewritten\n", rewritten, total)
	if rewritten > 0 {
		// Not an error, since the repair succeeded, but worth a second look:
		// something wrote a document that disagrees with its answers.
		fmt.Println("documents had drifted from their answers; find the write path that caused it")
	}
	return nil
}
