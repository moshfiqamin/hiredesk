// Package store owns the Postgres connection pool and, from the next module
// on, the queries that run against it.
//
// Nothing here reads the environment: every setting arrives in a *config.Config
// so the pool's behaviour is visible at the call site rather than hidden in
// package state.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moshfiqamin/hiredesk/internal/config"
)

// New opens a Postgres connection pool configured from cfg and verifies it is
// usable before returning.
//
// The returned pool is safe for concurrent use by many goroutines and is the
// single pool for the process — callers share it rather than creating their
// own. The caller owns it and must Close it at shutdown.
//
// A failure here is fatal by design: a wrong DATABASE_URL should stop the
// process at startup, not surface as a 500 on the first request that needs a
// database.
func New(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	// ParseConfig rather than pgxpool.New(ctx, url): it yields a struct we can
	// adjust before any connection is opened. Passing the URL directly would
	// leave every pool setting at its library default.
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		// %w so a caller can errors.As this back to pgx's own parse error.
		// The URL is not echoed — it contains the database password.
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdleTime

	// Spreads retirement over a window instead of letting every connection
	// opened at boot reach MaxConnLifetime in the same second. Without it, a
	// pool created at startup reconnects as one burst, and the request unlucky
	// enough to arrive then waits for a whole new handshake.
	poolCfg.MaxConnLifetimeJitter = cfg.DBMaxConnLifetime / 10

	// How often the pool prunes idle connections and tops back up to
	// MinConns. Left at pgx's one-minute default would mean an idle-time of
	// less than a minute is only honoured to the nearest minute.
	poolCfg.HealthCheckPeriod = cfg.DBMaxConnIdleTime / 2

	// NewWithConfig does not connect — it returns as soon as the pool is
	// built, and the first real connection is made lazily. That is why the
	// Ping below is necessary: without it, a bad host is not noticed here.
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	// Bounded separately from ctx: ctx lives for the process, so pinging on it
	// alone would hang indefinitely against a host that accepts the TCP
	// connection and then never answers.
	pingCtx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		// The pool already holds goroutines and possibly a socket, so it must
		// be closed on this path. Returning the error without closing leaks
		// both for the lifetime of the process.
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
