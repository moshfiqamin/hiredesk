# HireDesk

[![CI](https://github.com/moshfiqamin/hiredesk/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/moshfiqamin/hiredesk/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/moshfiqamin/hiredesk)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A hiring pipeline REST API in Go — jobs, candidates, applications and stage
transitions — built with Gin and Postgres.

## Quick start

Requires only Docker. No Go toolchain, no local Postgres.

```bash
git clone https://github.com/moshfiqamin/hiredesk.git && cd hiredesk && make up
```

That generates `.env` with a random `JWT_SECRET`, builds the image, starts
Postgres, waits for it to pass its healthcheck, **applies all six schema
migrations**, then starts the API.

```bash
curl -s localhost:8080/readyz
```

Returns `{"status":"ok"}` with HTTP 200 once the database is reachable, and
`{"status":"degraded","database":"unreachable"}` with HTTP 503 when it is not.

### See it in a browser

| What | Where |
|---|---|
| **The database** — tables, columns, indexes, foreign keys, constraints | <http://localhost:8081/?pgsql=postgres&username=hiredesk&db=hiredesk> |
| Liveness | <http://localhost:8080/healthz> |
| Readiness | <http://localhost:8080/readyz> |

The database link opens [Adminer](https://www.adminer.org/) with everything
pre-filled except the password — `POSTGRES_PASSWORD` from your `.env`, which
defaults to `hiredesk_dev_password`. It is bound to `127.0.0.1` only and is a
development tool: never expose it.

Run `make` at any time to list every command.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness. Never touches the database, so a database outage cannot cause a restart loop. |
| `GET` | `/readyz` | Readiness. Fails when a dependency is unavailable, removing the instance from the load balancer without restarting it. |

## Commands

| Command | Does |
|---|---|
| `make up` | Start the stack in the background |
| `make down` | Stop it; database data is kept |
| `make logs` | Follow logs from all services |
| `make ps` | Container status and health |
| `make psql` | Open a `psql` shell in the database |
| `make adminer` | Print the pre-filled database-browser link |
| `make migrate-up` | Apply every pending migration |
| `make migrate-down` | Roll back exactly one migration |
| `make migrate-version` | Show the applied version and dirty flag |
| `make migrate-create` | New migration pair: `make migrate-create NAME=add_foo` |
| `make test` | Tests with the race detector and coverage |
| `make lint` | `golangci-lint` (13 linters) |
| `make fmt` | Format code and tidy `go.mod` |
| `make build` | Compile to `./bin` without Docker |
| `make reset` | Destroy the database volume and start fresh |

## Layout

```
cmd/api/              main package — the server binary
internal/config/      environment config, loaded once into a struct
internal/store/       Postgres connection pool (and, later, queries)
migrations/           golang-migrate pairs, applied on container start
docs/notes/           design notes, one per module
.github/workflows/    CI pipeline
```

`internal/` is enforced by the Go compiler: nothing outside this module can
import it, so those packages stay free to change without breaking anyone.

## Configuration

All settings are read once at startup into a single struct
([`internal/config/config.go`](internal/config/config.go)) and passed
explicitly from there. No `os.Getenv` anywhere else in the codebase.

A missing required variable stops the process before it serves a request, and
every missing variable is reported at once rather than one per run:

```
configuration error: missing required environment variable(s): DATABASE_URL, JWT_SECRET
```

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | yes | — | Postgres connection string |
| `JWT_SECRET` | yes | — | Token signing key; no default by design |
| `PORT` | no | `8080` | Listen port, validated as 1–65535 |
| `ENV` | no | `development` | `production` switches Gin to release mode |
| `DB_MAX_CONNS` | no | `10` | Pool ceiling, 1–500 |
| `DB_MIN_CONNS` | no | `2` | Idle connections kept open; must be ≤ `DB_MAX_CONNS` |
| `DB_MAX_CONN_LIFETIME` | no | `1h` | Maximum age of any connection |
| `DB_MAX_CONN_IDLE_TIME` | no | `30m` | Idle connections older than this are closed |
| `DB_CONNECT_TIMEOUT` | no | `5s` | Budget for the startup ping |
| `DB_PING_TIMEOUT` | no | `2s` | Budget for the `/readyz` ping |
| `SHUTDOWN_TIMEOUT` | no | `10s` | Grace period for in-flight requests |
| `ADMINER_PORT` | no | `8081` | Database browser, bound to `127.0.0.1` |

Durations use Go syntax (`30s`, `1h30m`); a bare number is rejected at startup
as ambiguous.

See [`.env.example`](.env.example) for the full template.

## Running without Docker

```bash
make build && DATABASE_URL="postgres://localhost:5432/hiredesk?sslmode=disable" JWT_SECRET="$(openssl rand -hex 32)" ./bin/hiredesk-api
```

## Schema

Eleven tables across six migrations, applied automatically when the stack
starts. Browse them at the Adminer link above, or:

```bash
make psql
```

| Table | Holds |
|---|---|
| `users` | Accounts and roles (`admin`, `interviewer`, `candidate`) |
| `refresh_tokens` | Session tokens, stored as SHA-256 hashes |
| `job_posts` | Vacancies, with status `draft` / `open` / `closed` |
| `questions` | A post's application form, ordered by `position` |
| `rubric_criteria` | Versioned scoring rubric, parameters in `jsonb` |
| `applications` | One per candidate per post — enforced by a unique constraint |
| `application_answers` | Submitted answers, one per question per application |
| `scores` | Per-criterion scores with the evidence behind them |
| `application_events` | Append-only audit trail |
| `interviewer_assignments` | Who is reviewing what |
| `reviews` | Marks (1–10) and notes |

The reasoning behind every constraint, index and delete action is in
[`docs/notes/01-schema.md`](docs/notes/01-schema.md).

## Engineering notes

- **Multi-stage Docker build.** The runtime image is 22MB and contains only a
  statically linked binary, CA certificates and an unprivileged user — no Go
  toolchain, no source. A single-stage build of the same code is 679MB.
- **Runs as non-root.** UID 10001, no shell, no home directory.
- **Startup ordering is explicit.** The API waits on Postgres's `pg_isready`
  healthcheck rather than on container creation, so a cold start cannot race
  `initdb`.
- **Fail fast.** Invalid configuration — and an unreachable database — stops
  the process at startup, not on the first request that needs it.
- **A tuned connection pool.** `MaxConns`, `MinConns`, lifetime and idle time
  are set explicitly rather than left to the driver's CPU-derived defaults, and
  validated at startup.
- **Liveness and readiness are separate.** `/healthz` never touches the
  database, so a database outage removes instances from the load balancer
  without triggering a restart loop.
- **Migrations gate the API.** The server container does not start until the
  migration container has exited successfully.
- **Graceful shutdown.** SIGTERM drains in-flight requests, then closes the
  pool.
- **CI on every push and PR.** Build, vet, race-detector tests, a `go.mod`
  tidiness gate, and `golangci-lint`.

Design decisions and the reasoning behind them are written up per module in
[`docs/notes/`](docs/notes/).

## License

MIT — see [LICENSE](LICENSE).
