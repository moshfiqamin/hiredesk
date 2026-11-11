# Module 2 — Database wiring

The pgx connection pool, a `/readyz` that actually pings, migrations that run
before the API starts, graceful shutdown, and Adminer for reading the schema in
a browser.

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-a-connection-pool-is) | What a connection pool is | [`db.go:28`](../../internal/store/db.go#L28) |
| [2](#2-each-pool-setting-and-how-it-fails) | Each pool setting, and how it fails | [`config.go:80`](../../internal/config/config.go#L80) |
| [3](#3-parseconfig-not-pgxpoolnew) | `ParseConfig`, not `pgxpool.New` | [`db.go:32`](../../internal/store/db.go#L32) |
| [4](#4-why-the-startup-ping-is-mandatory) | Why the startup Ping is mandatory | [`db.go:66`](../../internal/store/db.go#L66) |
| [5](#5-healthz-versus-readyz) | `/healthz` versus `/readyz` | [`health.go:21`](../../internal/api/health.go#L21) |
| [6](#6-what-contextwithtimeout-is-doing) | What `context.WithTimeout` does | [`health.go:34`](../../internal/api/health.go#L34) |
| [7](#7-graceful-shutdown-and-why-run-exists) | Graceful shutdown, and why `run` exists | [`main.go:51`](../../cmd/api/main.go#L51) |
| [8](#8-migrations-as-a-compose-dependency) | Migrations as a Compose dependency | [`compose:38`](../../docker-compose.yml#L38) |
| [9](#9-adminer-seeing-the-schema-in-a-browser) | Adminer, in a browser | [`compose:112`](../../docker-compose.yml#L112) |

---

## 1. What a connection pool is

**📍 Open:** [`internal/store/db.go:28`](../../internal/store/db.go#L28)

A Postgres connection is not a cheap object. Opening one costs a TCP handshake,
optionally a TLS negotiation, then SASL authentication, then — and this is the
part people miss — **the server forks a dedicated backend process** for it.
Postgres is process-per-connection, not thread-per-connection. Each backend
carries its own memory for work areas and caches, measured in megabytes.

So "open a connection per request" means, on every request: three or more
network round trips before a single byte of SQL is sent, a `fork()` on the
database server, and a process teardown afterwards. At any real request rate
the database spends more effort creating and destroying backends than answering
queries, and a traffic spike turns into a fork storm that can take the server
down. `max_connections` (100 by default) is a hard ceiling, so a burst of 200
concurrent requests does not queue — the extra ones are **refused**.

A pool keeps a set of already-authenticated connections and lends them out. A
request borrows one, runs its query, and returns it. The handshake and the fork
happen once per connection, not once per request, and the number of database
backends stops tracking request concurrency and starts tracking pool size —
a number you choose.

`*pgxpool.Pool` is safe for concurrent use by many goroutines, so there is
exactly one for the process and it is passed explicitly to whatever needs it.
Creating a second pool per package would quietly double the connection count.

Verified: with `DB_MIN_CONNS=2` the server showed idle `client backend`
connections for the API while no requests were in flight — the pool holding
them open rather than reconnecting.

---

## 2. Each pool setting, and how it fails

**📍 Open:** [`internal/config/config.go:80`](../../internal/config/config.go#L80)

All four are set explicitly. pgx's own defaults are `MaxConns = max(4, NumCPU)`
and `MinConns = 0` — a pool whose size depends on which machine the container
lands on, which is not a property you want varying between your laptop and
production.

### `MaxConns` — the ceiling

The one that matters. Budget it as:

```
(number of API instances x DB_MAX_CONNS) + room for psql/admin <= max_connections
```

**Too high** and you exhaust the server's connection slots. The failure is not
graceful: new connections are refused with `FATAL: sorry, too many clients
already`, and that includes *your* `psql` when you try to log in and find out
what is wrong. Worse, Postgres does not get faster with more concurrent
backends — past the point where active queries exceed roughly the core count,
throughput *falls* as the server thrashes between processes, so a too-large
pool makes the database slower while using more memory.

**Too low** and requests queue inside the application waiting to borrow a
connection. The database looks idle and healthy while your p99 latency climbs,
because the time is spent in `pool.Acquire`, not in the query. This is the
harder one to diagnose: every database metric looks fine.

10 is a deliberate starting point for a single instance against the default
`max_connections = 100`: ten instances still fit with room to spare.

### `MinConns` — the floor

Idle connections kept open even when nothing is happening, so the first request
after a quiet spell does not pay for TCP, TLS and authentication. Set to 0
(pgx's default), a service with bursty traffic pays full connection setup on
the first request of every burst — visible as a slow first request after each
lull. Set too high, you hold database backends and their memory hostage while
doing nothing.

### `MaxConnLifetime` — retire connections on age

Caps how long any connection may live, however healthy. Without it, a
connection opened at boot lives until something breaks it. Three reasons to
retire them anyway: server-side memory and prepared-statement caches creep up
over a long-lived session; a connection pinned to an old primary after a
failover never notices the new one; and a leaked server-side resource is
bounded rather than permanent.

[`db.go:48`](../../internal/store/db.go#L48) also sets
`MaxConnLifetimeJitter` to a tenth of the lifetime. Without jitter, every
connection opened in the same second at boot reaches its maximum age in the
same second an hour later and reconnects as one burst — a small thundering
herd, and the request that arrives then waits for a full handshake.

### `MaxConnIdleTime` — retire connections on disuse

Closes a connection unused for this long, returning memory on the database
server. Distinct from lifetime: a busy connection is never idle-reaped, and an
idle one is reaped long before it is old.

[`db.go:53`](../../internal/store/db.go#L53) sets `HealthCheckPeriod` to half
the idle time, because the pool only prunes and tops up when that sweep runs.
Left at pgx's one-minute default, an idle timeout shorter than a minute would
only be honoured to the nearest minute.

### Validation

[`config.go:308`](../../internal/config/config.go#L308) and
[`:336`](../../internal/config/config.go#L336) parse these with bounds, and
[`:204`](../../internal/config/config.go#L204) adds the cross-field check that
`DB_MIN_CONNS <= DB_MAX_CONNS` — pgx would accept the inversion and then simply
never honour `MinConns`, which is a silent misconfiguration rather than an
error. Durations use Go syntax, so a bare `30` is rejected as ambiguous between
seconds and minutes rather than guessed at.

Every one of these failure paths was exercised; see
[Verified](#verified-in-this-module).

---

## 3. `ParseConfig`, not `pgxpool.New`

**📍 Open:** [`internal/store/db.go:32`](../../internal/store/db.go#L32)

`pgxpool.New(ctx, url)` is the one-liner, and it leaves every pool setting at
its library default. `ParseConfig` returns a struct to adjust *before* any
connection is opened, which is the only way to apply the values above.

The error is wrapped with `%w` so a caller can `errors.As` it back to pgx's own
parse error — the convention from module 0. The URL itself is never echoed into
the error, because it contains the database password. pgx is careful about this
too: a failed connection reports ``user=hiredesk database=hiredesk`` with no
password, which the wrong-password test confirmed.

---

## 4. Why the startup Ping is mandatory

**📍 Open:** [`internal/store/db.go:66`](../../internal/store/db.go#L66)

**`pgxpool.NewWithConfig` does not connect.** It builds the pool and returns;
the first real connection is made lazily, on first use. So without a Ping, a
completely wrong `DATABASE_URL` produces a process that starts, logs "listening
on :8080", passes its liveness check, and only fails when the first request
that needs data arrives — as a 500, to a user.

The Ping turns that into a startup failure, which is the right place for it:
the deployment fails visibly, the old version keeps running, and nobody sees an
error. Verified for a bad host, a malformed URL and a wrong password — each
exits before the listener opens:

```
fatal: database: ping database: failed to connect to `user=hiredesk database=hiredesk`:
       ... password authentication failed for user "hiredesk" (SQLSTATE 28P01)
```

The Ping gets its own bounded context rather than using `ctx` directly, since
`ctx` lives for the whole process: pinging on it alone would hang forever
against a host that accepts the TCP connection and then never answers.

On failure the pool is **closed before returning the error**. It already holds
goroutines and possibly a socket; returning without closing leaks both for the
process's lifetime. This is the easy bug in this function.

---

## 5. `/healthz` versus `/readyz`

**📍 Open:** [`internal/api/health.go:21`](../../internal/api/health.go#L21)

They answer different questions and have different consequences, which is why
one endpoint cannot serve both.

| | `/healthz` — liveness | `/readyz` — readiness |
|---|---|---|
| Question | "Is this process able to answer HTTP?" | "Should I receive traffic?" |
| Checks | nothing external | the database |
| On failure | **restart me** | **stop sending me requests** |
| Touches the DB | never | yes |

**A load balancer needs both because the two failures need opposite
responses.** If the database goes down and readiness is all you have, every
instance is pulled from rotation — correct — but nothing is restarted, also
correct, because restarting would not help. If liveness is all you have and it
checks the database, then a database outage makes every API process fail its
liveness probe, and the orchestrator kills and restarts all of them,
simultaneously, in a loop. You have converted a recoverable dependency outage
into a total outage of your own service, and the restart storm adds connection
churn to a database already in trouble.

That is why `/healthz` must **never** touch the database. It is deliberately
trivial: if the HTTP server can answer at all, the process is alive.

Verified end to end. With Postgres stopped:

```
/healthz -> {"status":"ok"}                                       HTTP 200
/readyz  -> {"database":"unreachable","status":"degraded"}        HTTP 503
```

and after restarting Postgres, `/readyz` returned 200 again with
`restarts=0` on the container — the process was never killed, exactly as
intended.

**503, not 500**, is the detail that makes it work: 503 Service Unavailable
says the instance is functioning but its dependency is not, which is what tells
a load balancer to stop routing rather than an orchestrator to restart.

The failure detail is **logged, not returned**. A health endpoint is usually
unauthenticated, and the driver's error names the host, port and user.

---

## 6. What `context.WithTimeout` is doing

**📍 Open:** [`internal/api/health.go:34`](../../internal/api/health.go#L34)

A `context.Context` carries a deadline and a cancellation signal down a call
chain. `context.WithTimeout(parent, d)` returns a child context that is
cancelled when `d` elapses **or** when the parent is cancelled, whichever comes
first, plus a `cancel` function that must always be called — hence the
`defer cancel()`. Skipping it leaks the timer and the goroutine watching it
until the deadline passes.

The parent here is `c.Request.Context()`, not `context.Background()`, and that
choice does real work: Gin cancels the request context when the client
disconnects, so a probe that gives up takes the query down with it instead of
leaving it running against an already-struggling database.

**What the deadline protects against is a database that is reachable but
stalled** — not one that is down. A stopped Postgres fails fast (DNS or
connection refused), which needs no timeout. A database that accepts the TCP
connection and then never answers is the dangerous case: without a deadline,
`Ping` waits indefinitely, the health check never responds, the load balancer's
own probe times out, and the instance is marked unhealthy for an unbounded
period — or the probe goroutines pile up.

This was tested properly rather than assumed, by proxying Postgres through a
container and then **freezing** it, so packets went unanswered with no
connection refused:

| | |
|---|---|
| `/readyz`, proxy alive | `{"status":"ok"}` HTTP 200 in **0.0018 s** |
| `/healthz`, proxy frozen | `{"status":"ok"}` HTTP 200 in **0.0013 s** |
| `/readyz`, proxy frozen | `{"status":"degraded"}` HTTP 503 in **2.0079 s** |
| logged reason | `readiness check failed: timeout: context deadline exceeded` |

2.0079 s against a `DB_PING_TIMEOUT` of 2 s: the deadline fired, not the TCP
stack. And `/healthz` stayed fast throughout, because it never touches the
database.

The two budgets are independent on purpose —
[`DB_CONNECT_TIMEOUT`](../../internal/config/config.go#L37) (5 s) is a one-off
at startup where waiting is fine, while `DB_PING_TIMEOUT` (2 s) runs on every
probe and should finish before the load balancer's own probe timeout.

---

## 7. Graceful shutdown, and why `run` exists

**📍 Open:** [`cmd/api/main.go:51`](../../cmd/api/main.go#L51)

`main` does almost nothing: it loads config and calls `run`. That split exists
for one reason — **`log.Fatalf` calls `os.Exit`, and `os.Exit` does not run
deferred functions.** A `defer pool.Close()` written in `main` alongside any
`log.Fatalf` would silently never execute. Putting the work in a function that
*returns an error* lets every defer run on the way out, and `main` does the
exiting.

The shutdown path:

1. [`signal.NotifyContext`](../../cmd/api/main.go#L55) gives a context cancelled
   on SIGINT (Ctrl-C) or SIGTERM — what `docker stop` and Kubernetes send.
2. `ListenAndServe` blocks, so it runs in a goroutine and reports failures back
   over a **buffered** channel ([`:109`](../../cmd/api/main.go#L109)). Buffered
   with room for one value so that goroutine can exit even if nobody is reading
   — an unbuffered channel would leak it.
3. `ErrServerClosed` is filtered out at [`:116`](../../cmd/api/main.go#L116):
   it is what a deliberate `Shutdown` returns, a normal outcome rather than a
   failure.
4. `srv.Shutdown` stops accepting new connections and waits for in-flight
   requests.
5. `defer pool.Close()` then waits for borrowed connections to be returned and
   closes them.

**The shutdown context at [`:134`](../../cmd/api/main.go#L134) is deliberately
NOT derived from `ctx`.** `ctx` is already cancelled — that is why we are
shutting down — and a cancelled parent would make `Shutdown` give up
immediately, defeating the entire grace period. This is the subtle bug the
comment there exists to prevent.

`stop_grace_period: 15s` in Compose must exceed `SHUTDOWN_TIMEOUT` (10 s), or
Docker's SIGKILL arrives mid-drain and the graceful shutdown is pointless.

Verified: `docker compose stop api` produced

```
shutdown signal received, draining connections
shutdown complete
```

and the database's count of API connections went from 1 to **0** — the pool
closed cleanly rather than leaving backends for the server to reap.

`ReadHeaderTimeout` at [`:103`](../../cmd/api/main.go#L103) is unrelated to
shutdown but belongs on every `http.Server`: without it a client that opens a
connection and dribbles header bytes holds it open forever, which is the
Slowloris attack.

---

## 8. Migrations as a Compose dependency

**📍 Open:** [`docker-compose.yml:38`](../../docker-compose.yml#L38)

The migration runner is a **service that exits**, not a long-running one. The
API then waits on it:

```yaml
depends_on:
  postgres:
    condition: service_healthy
  migrate:
    condition: service_completed_successfully
```

`service_completed_successfully` waits for exit code 0. If a migration fails,
the API is **never started** — far better than booting against a half-applied
schema and failing on whichever query hits the missing column first.

Observed ordering on `make up`:

```
postgres Starting → Healthy → migrate Starting → Exited → api Starting
```

`restart: "no"` is required. With any restart policy, Compose would treat the
clean exit as a crash and run the migrator forever.

Re-running with nothing pending prints "no change" and exits 0, which is what
keeps `make up` idempotent.

The alternative — running migrations from inside the API process at startup —
was rejected because it couples schema changes to application rollout: with
several replicas, each one races the others to apply the same migration, and
golang-migrate's lock turns that into a startup stall for all but one.

The image is pinned to `migrate/migrate:v4.20.1`, matching the CLI version, so
the automatic run and `make migrate-create` cannot disagree about file naming.

**The Makefile targets run in the same container**
([`Makefile:29`](../../Makefile#L29)), which means nothing to install locally
and — usefully — immunity to whatever is listening on the host's 5432. A local
Homebrew Postgres bound to `127.0.0.1:5432` wins over Docker's `0.0.0.0`
binding, so a host-side `migrate` would reach the wrong server entirely and
fail with `role "hiredesk" does not exist`.

`make migrate-create` passes `--user $(id -u):$(id -g)`, without which a
container writing to a bind mount leaves the new files owned by root.

---

## 9. Adminer: seeing the schema in a browser

**📍 Open:** [`docker-compose.yml:112`](../../docker-compose.yml#L112)

A REST API has nothing to look at until it has endpoints, but the *database*
does. Adminer is a single-file database client in one small container: the
tables, their columns, indexes, foreign keys with their delete actions, and the
`CHECK` constraints are all browsable at `http://localhost:8081`.

Two deliberate choices:

- **Published on `127.0.0.1` only.** Without the host part, Docker publishes on
  `0.0.0.0` and anyone on the same network reaches a database console. This is
  a development tool and must never be exposed.
- **The entry point is a pre-filled link**, printed by `make up` and
  `make adminer`:
  `/?pgsql=postgres&username=hiredesk&db=hiredesk`. `ADMINER_DEFAULT_SERVER`
  alone does not prefill the form — Adminer still opens on MySQL with an empty
  server box — so the query string is what makes it one field instead of five.

---

## Verified in this module

| Check | Result |
|---|---|
| `make up` ordering | `postgres Healthy → migrate Exited(0) → api Starting` |
| Migrations applied automatically | `1/u` … `6/u`, version `6`, `dirty = f`, 12 tables |
| Startup log | `database pool ready (max=10 min=2 lifetime=1h0m0s idle=30m0s)` |
| `GET /healthz` | `{"status":"ok"}` 200 |
| `GET /readyz`, database up | `{"status":"ok"}` 200 |
| `GET /readyz`, database **stopped** | `{"status":"degraded","database":"unreachable"}` **503** |
| `GET /healthz`, database stopped | still `{"status":"ok"}` 200 |
| Recovery after database restart | `/readyz` 200 again, `restarts=0` |
| `/readyz` against a **frozen** proxy | 503 in **2.0079 s**, `context deadline exceeded` |
| `/healthz` against a frozen proxy | 200 in 0.0013 s |
| Idle pool connections | server shows idle `client backend` rows for the API |
| Bad host / malformed URL / wrong password | all three **fail at startup**, before the listener opens |
| Password in error output | redacted by pgx — `user=hiredesk database=hiredesk` |
| `DB_MAX_CONNS` = `0` / `9999` / `abc` | each rejected with a bounded, named message |
| `DB_MIN_CONNS=20` with max 10 | `must not exceed DB_MAX_CONNS (10)` |
| `DB_PING_TIMEOUT` = `30` / `1ms` | `missing unit in duration` / `must be between 1s and 24h0m0s` |
| `docker compose stop api` | `draining connections` → `shutdown complete`, connections 1 → **0** |
| `make migrate-version` / `down` / `up` | `6` → `5` (12 → 10 tables) → `6` (12 tables) |
| `make migrate-create NAME=…` | `000007_*.{up,down}.sql`, owned by the host user |
| Adminer in a browser | all 12 tables; `applications` shows its FKs, indexes and CHECK |
| `gofmt -l .` / `go build` / `go vet` | clean / OK / OK |
| `go test -race -cover ./...` | passes |
| `golangci-lint run ./...` | **0 issues** |
| `go mod tidy -diff` | clean — pgx is now a direct dependency |

## Still open

- **No tests anywhere.** Coverage is 0.0% across all three packages, and
  `internal/config` is the most testable code in the project: `Load` is a pure
  function of the environment, and every validation path above was checked by
  hand through a container when a table-driven test would do it in CI.
- **`make` prints `Makefile` instead of target names.** Pre-existing: `include
  .env` adds `.env` to `MAKEFILE_LIST`, so `grep` searches two files and
  prefixes each match with the filename, which `awk` then reads as `$1`. Fixed
  by grepping `$(firstword $(MAKEFILE_LIST))`.
- **The schema gaps** from [`01-schema.md`](01-schema.md#still-open) are
  unchanged: salary currency, append-only enforcement, seven unindexed foreign
  keys, `updated_at`, and `earned <= available`.

## Next

**Module 3** — the first real resources: `POST /auth/register`, `POST
/auth/login` and the job-post CRUD endpoints. That is where `23505` becomes a
409 through `errors.As`, `numeric` meets `pgtype.Numeric`, and `jsonb` meets a
discriminated union keyed on `kind`.
