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
| `POST` | `/v1/auth/register` | Create a candidate account. Body: `email`, `password` (8–72 characters), `full_name`. Returns 201 and the user. |
| `POST` | `/v1/auth/login` | Body: `email`, `password`. Returns a 15-minute access token, a 30-day refresh token and the user. An unknown email and a wrong password get the same 401. |
| `POST` | `/v1/auth/refresh` | Body: `refresh_token`. Returns a new access token and a **new** refresh token; the one presented stops working. Presenting a token a second time ends every session of that user. |
| `POST` | `/v1/auth/logout` | Body: `refresh_token`. Revokes it. Always 204. |
| `GET` | `/v1/me` | The current user. Requires `Authorization: Bearer <access token>`. |

Create the development accounts, then try it:

```bash
make seed
```

| Role | Email | Password |
|---|---|---|
| admin | `admin@hiredesk.test` | `admin-password` |
| interviewer | `interviewer@hiredesk.test` | `interviewer-password` |
| candidate | `candidate1@hiredesk.test` | `candidate-password` |
| candidate | `candidate2@hiredesk.test` | `candidate-password` |

These are development fixtures with published passwords. The seed refuses to
run when `ENV=production`, and is safe to run more than once.

```bash
curl -s -X POST localhost:8080/v1/auth/login -d '{"email":"admin@hiredesk.test","password":"admin-password"}'
```

Or register a candidate of your own:

```bash
curl -s -X POST localhost:8080/v1/auth/register -d '{"email":"demo@example.com","password":"correct horse","full_name":"Demo User"}'
```

```bash
curl -s -X POST localhost:8080/v1/auth/login -d '{"email":"demo@example.com","password":"correct horse"}'
```

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
| `make test-integration` | Database tests against the running stack (needs `make up`) |
| `make seed` | Create the development accounts (needs `make up`; safe to repeat) |
| `make lint` | `golangci-lint` (13 linters) |
| `make fmt` | Format code and tidy `go.mod` |
| `make build` | Compile to `./bin` without Docker |
| `make reset` | Destroy the database volume and start fresh |

## Layout

```
cmd/api/              main package — the server binary
cmd/seed/             creates the development accounts
internal/api/         HTTP layer: router, handlers, validation, error envelope
internal/auth/        password hashing (bcrypt), access tokens (JWT), refresh tokens
internal/config/      environment config, loaded once into a struct
internal/domain/      domain types and errors; no HTTP or database imports
internal/store/       Postgres connection pool and hand-written SQL queries
migrations/           golang-migrate pairs, applied on container start
docs/notes/           design notes, one per module
.github/workflows/    CI pipeline
```

`internal/` is enforced by the Go compiler: nothing outside this module can
import it, so those packages stay free to change without breaking anyone.

## Errors

Every error response has the same body, including unknown routes and panics:

```json
{"error":{"code":"validation_failed","message":"One or more fields are invalid.","details":{"email":"is not a valid address"}}}
```

`code` is stable and meant for programs; `message` is for people; `details` is
always an object, and names each bad field when validation fails.

| Status | `code` | Meaning |
|---|---|---|
| 400 | `malformed_request` | The body is not a JSON object |
| 401 | `invalid_credentials` | Login failed |
| 401 | `token_expired` | The access token has expired; refresh and retry |
| 401 | `unauthenticated` | No usable access or refresh token |
| 403 | `forbidden` | Authenticated, not permitted |
| 404 | `not_found` | The record does not exist |
| 404 | `route_not_found` | No such endpoint |
| 405 | `method_not_allowed` | The endpoint exists under another method |
| 409 | `duplicate` | A record with the same unique key exists |
| 409 | `conflict` | Valid, but not in the resource's current state |
| 422 | `validation_failed` | One or more fields are invalid; see `details` |
| 500 | `internal_error` | Unexpected; the cause is logged, never returned |

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
| `JWT_SECRET` | yes | — | Token signing key, at least 32 characters; no default by design |
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
- **Passwords are bcrypt at cost 12**, verified in comparable time whether or
  not the account exists, so login does not reveal which emails are registered.
- **Short-lived access tokens, rotating refresh tokens.** Access tokens are
  15-minute HS256 JWTs with the algorithm pinned on verification. Refresh
  tokens are random, stored only as SHA-256 hashes, and replaced on every use;
  presenting a used one revokes all of that user's sessions.
- **Rotation is race-safe.** The token row is locked with `FOR UPDATE` inside a
  transaction, so two concurrent refreshes cannot both succeed.
- **One error shape.** Every failure — validation, unknown route, panic —
  returns the same JSON envelope, and internal causes are logged, never sent.
- **CI on every push and PR.** Build, vet, race-detector tests, a `go.mod`
  tidiness gate, and `golangci-lint`.

Design decisions and the reasoning behind them are written up per module in
[`docs/notes/`](docs/notes/).

## License

MIT — see [LICENSE](LICENSE).
