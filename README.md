# HireDesk

[![CI](https://github.com/moshfiqamin/hiredesk/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/moshfiqamin/hiredesk/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/moshfiqamin/hiredesk)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

HireDesk is a hiring-pipeline REST API in Go that scores every job application
against a versioned rubric the moment it is submitted, and shows which answer
earned each point.

```json
{
  "application_id": "cdb5cb85-e026-40c1-b066-57ebd56d9654",
  "total_earned": 83.37,
  "total_available": 100,
  "percentage": 83.37,
  "rubric_version": 1,
  "breakdown": [
    {
      "criterion": "Go knowledge",
      "kind": "keyword_match",
      "earned": 30,
      "available": 30,
      "detail": "matched 5 of 5 keywords: go, gin, goroutines, postgres, docker",
      "evidence": {
        "question": "Describe a project you built",
        "answer_id": "e3dd2676-b12f-4bf6-b990-f71148cefe91",
        "excerpt": "...services for a few years. Last year I built a REST API in Go using Gin, with goroutines handling the background jobs..."
      }
    },
    {
      "criterion": "Experience",
      "kind": "experience_years",
      "earned": 30,
      "available": 40,
      "detail": "3 years, 75% of the way from 0 to the target of 4",
      "evidence": { "question": "Years of Go?", "answer_id": "0fc16bcc-1142-49a0-ac7c-0b20db6e0480", "excerpt": "3" }
    },
    {
      "criterion": "Answer depth",
      "kind": "answer_depth",
      "earned": 13.37,
      "available": 20,
      "detail": "284 characters, 67% of the way from 50 to the target of 400",
      "evidence": { "question": "Describe a project you built", "answer_id": "e3dd2676-b12f-4bf6-b990-f71148cefe91", "excerpt": "I have been writing backend services for a few years. Last year I built a REST API in Go using Gin, with goroutines..." }
    },
    {
      "criterion": "Eligibility",
      "kind": "boolean_all",
      "earned": 10,
      "available": 10,
      "detail": "1 of 1 condition met",
      "evidence": null
    }
  ]
}
```

That is a real response from `GET /v1/applications/:id/score`, captured while
driving the API from its Swagger UI. `evidence` is `null` when no single answer
was the evidence (here, a criterion over several yes/no questions).

## Quick start

Needs only Docker (with Compose v2), `make` and `openssl`, which macOS and most
Linux distributions already have.

```bash
git clone https://github.com/moshfiqamin/hiredesk.git && cd hiredesk && make up
```

That creates `.env` with a random `JWT_SECRET`, builds the image, starts
PostgreSQL, waits for its healthcheck, applies all 13 migrations, and starts
the API on port 8080. The first build takes a minute or two.

Then open **<http://localhost:8080/docs>**: Swagger UI, served from inside the
binary. To use it:

1. `make seed` creates four development accounts (table below).
2. In `/docs`, run `POST /v1/auth/login` with `admin@hiredesk.test` /
   `admin-password`. The example body is already filled in.
3. Copy `access_token` from the response, press **Authorize**, paste it.

Every padlocked operation now runs as that user. The whole flow (create a job
post, add questions, set a rubric, open it, sign in as a candidate, apply,
read the score) works from the browser without curl.

| Role | Email | Password |
|---|---|---|
| admin | `admin@hiredesk.test` | `admin-password` |
| interviewer | `interviewer@hiredesk.test` | `interviewer-password` |
| candidate | `candidate1@hiredesk.test` | `candidate-password` |
| candidate | `candidate2@hiredesk.test` | `candidate-password` |

These are development fixtures with published passwords. The seeder refuses
to run with `ENV=production`.

If port 8080 or 5432 is already taken on your machine, set `API_PORT` or
`POSTGRES_PORT` in `.env` and run `make down && make up`.

## Architecture

```
cmd/api ─────────────▶ internal/api ──▶ internal/auth ──┐
   │                       │    │                       │
   │                       │    └──────▶ internal/scoring ──▶ internal/domain
   │                       ▼                    ▲                ▲
   ├──────────────▶ internal/store ─────────────┘                │
   │                       └─────────────────────────────────────┘
   └──────────────▶ internal/config ──▶ internal/platform
```

Dependencies point inward, toward `internal/domain`:

- **`domain`** holds the types, rules and errors (an application's lifecycle,
  what makes a submission valid, `ErrNotFound`). It imports no other package
  in this module, and nothing that knows about HTTP or SQL.
- **`scoring`** is the scoring engine: a pure function from answers and a
  rubric to scores. It imports only `domain` and the standard library; no
  database, no clock, no randomness. That makes it deterministic, and
  testable by table and by fuzzing.
- **`auth`** (bcrypt, JWTs, refresh tokens, access policy) imports only
  `domain`.
- **`store`** is the only package that speaks SQL. It turns database errors
  into domain errors (a unique violation on the application key becomes
  `domain.ErrAlreadyApplied`), so nothing above it sees a pgx error.
- **`api`** is the only package that speaks HTTP. It decodes requests, calls
  the store, and maps domain errors to statuses in one table.
- **`cmd/api`** builds every dependency once and passes it down. It is the one
  place that shows what the server is made of. Configuration is read once, in
  `internal/config`; nothing else calls `os.Getenv`.

The rule is checked, not just stated: `go list -deps ./internal/domain
./internal/scoring` contains neither pgx, Gin nor `net/http`.

## Technical highlights

- **Refresh-token rotation with replay detection.** Each refresh issues a new token and kills the old one; presenting a used token revokes every session of that user ([`store/tokens.go`](internal/store/tokens.go), [notes](docs/notes/03-auth.md)).
- **An authorization matrix test.** Every caller (anonymous, three roles, assigned and unassigned interviewers) against every protected route on a real PostgreSQL: 334 subtests, and a route added without a row fails the build ([`authz_test.go`](test/integration/authz_test.go)).
- **A concurrent duplicate-application test.** Ten goroutines submit the same candidate's application at once, released by one start gate; exactly one succeeds and the rest get 409 `already_applied`, enforced by a unique constraint rather than a check-then-insert ([`concurrency_test.go`](test/integration/concurrency_test.go)).
- **An N+1 query, measured.** The ranking endpoint is deliberately naive today: 502 statements for 500 applicants, **53.9 ms median server time** (20 runs, API and PostgreSQL in Docker on one laptop). The single-query fix is designed but **not yet built**, so there is no "after" number yet ([notes](docs/notes/07-5-ranking.md)).
- **Whole-word full-text search on a GIN index.** A generated `tsvector` column with stemming: "testing" finds "tested", "go" does not find "google", and the query is a bound parameter ([`000012_search.up.sql`](migrations/000012_search.up.sql), [decision D7](docs/decisions.md)).
- **Graceful shutdown, proven.** On SIGTERM a request already in flight finishes with 200, new connections are refused, then the pool closes and the process exits 0. Past the 10-second budget it logs which requests it abandoned and exits 1 ([`cmd/api/main.go`](cmd/api/main.go), [notes](docs/notes/10-4-graceful-shutdown.md)).
- **Request IDs end to end.** Every response carries `X-Request-ID`, every error body carries `request_id`, and every log line written during the request carries both the ID and the user ID, read from `context.Context` rather than passed down ([notes](docs/notes/10-2-structured-logging.md)).
- **No credentials in the logs, by test.** A full register, login, refresh, replay and logout cycle runs at debug level, and the test fails if any password, token or `Authorization` value appears in the output.

## Test strategy

| Layer | What runs | How | Coverage |
|---|---|---|---|
| Pure logic | `domain`, `scoring`, `auth` | table-driven unit tests, a fuzz test on the keyword matcher | 96.0%, 97.6%, 96.0% |
| HTTP | `internal/api` | handlers and middleware against in-memory fakes, via `httptest` | 91.4% |
| Plumbing | `config`, `platform` | env parsing, log redaction, context propagation | 72.5%, 94.3% |
| SQL | `internal/store` | integration tests against the running PostgreSQL | 83.1% |
| End to end | `test/integration` | the production router over real stores, on a PostgreSQL started by testcontainers-go | (included below) |

Unit-test numbers are from `make test` (statement coverage per package). The
store figure is from the integration run, measured with `-coverpkg` across
`./internal/...`; that run alone covers **69.9%** of all statements in
`internal/`. `cmd/` is not counted: the seeder has a unit test (20.2%), and
the API's `main` is exercised by the shutdown procedure, not by a test.

What the tests deliberately prove, beyond line counts:

- **Refusal looks like absence.** The matrix compares a refused request's body
  byte for byte with the body for an ID that does not exist
  ([decision D1](docs/decisions.md)).
- **Races are real.** The duplicate-submission and vacancy tests use a start
  gate so the goroutines collide, and run under `-race`.
- **Rollback is real.** A trigger forces a failure mid-submit, and the test
  checks that nothing was written.
- **The spec matches the code.** A test fails if a route is served but missing
  from `api/openapi.yaml`, or documented but not served, or if its security
  requirement disagrees with the router.

Run them:

```bash
make test
```

Unit tests with the race detector. Needs Go 1.27.3 locally; nothing else.

```bash
make test-integration
```

Integration tests. Needs Go, and `make up` running, plus a Docker daemon for
testcontainers.

## Out of scope, deliberately

- **Email, notifications, file uploads.** A CV is a URL, not a stored file.
- **Multi-tenancy.** One organisation per deployment.
- **A frontend.** `/docs` is the UI. CORS is in place for when one exists
  (`ALLOWED_ORIGINS`).
- **Rate limiting beyond `/v1/auth/*`.** The limiter is in memory, per
  process: right for one instance, wrong for several. More than one instance
  needs a shared store (Redis) or limiting at the proxy.
- **Distributed tracing and metrics.** Request IDs and structured logs are the
  first step. There is no OpenTelemetry and no `/metrics` yet.

## What I would build next

1. **Step 2 of the ranking**: one query with `LEFT JOIN … NULLS LAST`, and the
   before/after numbers, measured the same way as above.
2. **sqlc** to generate the scan code from the existing SQL, keeping every
   hand-tuned query ([decision D6](docs/decisions.md)).
3. **The outbox pattern** for application events, so notifications can be
   added without a second write path ([notes](docs/notes/05-2-submit-transaction.md)).
4. **A candidate-facing score view** without keywords or weights, if a
   candidate is ever owed an explanation ([decision D3](docs/decisions.md)).
5. **Prometheus metrics** keyed on the same `route` field the access log uses.

---

## Reference

### Endpoints

The full contract (request bodies, response schemas, every status code, and
examples) is the OpenAPI document at [`api/openapi.yaml`](api/openapi.yaml),
rendered at `/docs`. In brief:

| Method | Path | Who |
|---|---|---|
| `GET` | `/healthz` | anyone (liveness; never touches the database) |
| `GET` | `/readyz` | anyone (readiness; pings the database) |
| `GET` | `/docs` | anyone (Swagger UI; `/docs/openapi.yaml` is the spec) |
| `POST` | `/v1/auth/register` · `/login` · `/refresh` · `/logout` | anyone, 10 per IP per minute |
| `GET` | `/v1/me` | any signed-in user |
| `GET` | `/v1/job-posts` | anyone (open posts only) |
| `POST` | `/v1/job-posts` | admin |
| `GET` | `/v1/job-posts/:id` | any signed-in user (drafts: admin only) |
| `PATCH` | `/v1/job-posts/:id` | admin |
| `POST` | `/v1/job-posts/:id/open` · `/close` | admin |
| `GET` `POST` | `/v1/job-posts/:id/questions` | admin |
| `PUT` | `/v1/job-posts/:id/questions/order` | admin |
| `PATCH` `DELETE` | `/v1/job-posts/:id/questions/:questionID` | admin |
| `GET` `PUT` | `/v1/job-posts/:id/rubric` | admin |
| `POST` | `/v1/job-posts/:id/applications` | candidate |
| `GET` | `/v1/job-posts/:id/applications/search?q=` | admin |
| `GET` | `/v1/job-posts/:id/ranking` | admin |
| `POST` | `/v1/job-posts/:id/rescore-all` | admin |
| `GET` | `/v1/applications` | any signed-in user (filtered per role, in SQL) |
| `GET` | `/v1/applications/:id` | admin, assigned interviewer, its candidate |
| `PATCH` | `/v1/applications/:id/status` | admin |
| `GET` | `/v1/applications/:id/score` | admin, assigned interviewer |
| `POST` | `/v1/applications/:id/rescore` | admin |
| `GET` | `/v1/applications/:id/events` | admin, assigned interviewer |
| `POST` | `/v1/applications/:id/assign` | admin |
| `DELETE` | `/v1/applications/:id/assign/:interviewerID` | admin |
| `GET` | `/v1/applications/:id/assignments` | admin |
| `POST` | `/v1/applications/:id/reviews` | assigned interviewer |
| `PATCH` | `/v1/applications/:id/reviews/mine` | the interviewer who filed it |

### Errors

Every error, from every endpoint, including unknown routes, rate limiting and
panics, has one shape:

```json
{"error":{"code":"validation_failed","message":"One or more fields are invalid.","details":{"email":"is not a valid address"},"request_id":"01JCQ7Z8K3M5N7P9R1T3V5X7Z9"}}
```

`code` is stable and meant for programs. `message` is for people. `details`
is always an object. `request_id` matches the `X-Request-ID` response header
and every server log line for that request, so quote it in a bug report.

| Status | `code` | Meaning |
|---|---|---|
| 400 | `malformed_request` | The body is not a JSON object |
| 400 | `invalid_id` | An ID in the URL is not a UUID |
| 400 | `invalid_cursor` | `after` is not a cursor this API issued |
| 401 | `invalid_credentials` | Login failed |
| 401 | `token_expired` | The access token has expired; refresh and retry |
| 401 | `unauthenticated` | No usable access or refresh token |
| 403 | `forbidden` | Signed in, not permitted |
| 404 | `not_found` | No such record, or one you may not see |
| 404 | `not_scored` · `no_review` | The application has no score · you have not reviewed it |
| 404 | `route_not_found` | No such endpoint |
| 405 | `method_not_allowed` | The endpoint exists under another method |
| 409 | `duplicate` · `already_applied` · `already_reviewed` | A unique rule was hit |
| 409 | `invalid_transition` · `not_ready` · `form_locked` · `not_accepting_applications` · `no_vacancies` · `no_rubric` · `conflict` | Valid, but not in the resource's current state; see `details` |
| 413 | `payload_too_large` | The body is over 1 MiB |
| 422 | `validation_failed` | One or more fields are invalid; see `details` |
| 429 | `rate_limited` | Too many `/v1/auth/*` requests; see `Retry-After` |
| 500 | `internal_error` | Unexpected; the cause is logged under the `request_id`, never returned |

### Commands

| Command | Does |
|---|---|
| `make up` | Start the stack in the background (creates `.env` on first run) |
| `make down` | Stop it; database data is kept |
| `make logs` | Follow logs from all services |
| `make ps` | Container status and health |
| `make seed` | Create the development accounts (safe to repeat) |
| `make seed-bulk` | Create one open post with 500 scored applications |
| `make test` | Unit tests with the race detector and coverage |
| `make test-integration` | Database and container tests (needs `make up` and Docker) |
| `make lint` | `golangci-lint` (13 linters) |
| `make fmt` | Format code and tidy `go.mod` |
| `make build` | Compile to `./bin` without Docker |
| `make psql` | `psql` shell in the database container |
| `make adminer` | Print the pre-filled database-browser link |
| `make migrate-up` · `migrate-down` · `migrate-version` · `migrate-create` | Migrations by hand |
| `make reindex-search` | Rebuild search documents that drifted from their answers |
| `make reset` | Destroy the database volume and start fresh |
| `make env` | Create `.env` from `.env.example` with a generated `JWT_SECRET` |

### Configuration

Read once at startup into one struct ([`internal/config/config.go`](internal/config/config.go)).
An invalid value stops the process before it serves a request, and every
missing required variable is reported at once.

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL connection string (Compose assembles it) |
| `JWT_SECRET` | yes | — | Token signing key, at least 32 bytes; no default by design |
| `PORT` | no | `8080` | Listen port inside the container |
| `ENV` | no | `development` | `production` switches Gin to release mode |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn` or `error` |
| `LOG_FORMAT` | no | `text` in development, `json` otherwise | `text` is refused in production |
| `ALLOWED_ORIGINS` | no | empty (no cross-origin access) | Comma-separated `scheme://host[:port]`; wildcards refused |
| `DB_MAX_CONNS` | no | `10` | Pool ceiling, 1–500 |
| `DB_MIN_CONNS` | no | `2` | Idle connections kept open; at most `DB_MAX_CONNS` |
| `DB_MAX_CONN_LIFETIME` | no | `1h` | Maximum age of a connection |
| `DB_MAX_CONN_IDLE_TIME` | no | `30m` | Idle connections older than this are closed |
| `DB_CONNECT_TIMEOUT` | no | `5s` | Budget for the startup ping |
| `DB_PING_TIMEOUT` | no | `2s` | Budget for the `/readyz` ping |
| `SHUTDOWN_TIMEOUT` | no | `10s` | Grace period for in-flight requests on SIGTERM |
| `API_PORT` · `POSTGRES_PORT` · `ADMINER_PORT` | no | `8080` · `5432` · `8081` | Host ports Compose publishes |

Durations use Go syntax (`30s`, `1h30m`). See [`.env.example`](.env.example)
for the full, commented template.

### Running without Docker

```bash
make build && DATABASE_URL="postgres://hiredesk:hiredesk_dev_password@localhost:5432/hiredesk?sslmode=disable" JWT_SECRET="$(openssl rand -hex 32)" ./bin/hiredesk-api
```

Needs PostgreSQL with the migrations applied; `make up` provides both.

### Layout

```
api/                  OpenAPI document and Swagger UI, embedded into the binary
cmd/api/              the server
cmd/seed/             development accounts and bulk data
cmd/reindex/          repairs full-text search documents
internal/api/         HTTP: router, middleware, handlers, error envelope
internal/auth/        bcrypt, JWTs, refresh tokens, access policy
internal/config/      environment configuration, loaded once
internal/domain/      types, rules and errors; no HTTP or SQL
internal/platform/    structured logger and request-scoped context values
internal/scoring/     the pure scoring engine
internal/store/       PostgreSQL pool and hand-written SQL
migrations/           golang-migrate pairs, applied on start
test/integration/     tests on a real PostgreSQL via testcontainers-go
docs/decisions.md     decision records
docs/notes/           design notes, one per module
```

The image is a multi-stage build: 74 MB, three static binaries (API, seeder,
reindexer), CA certificates and an unprivileged user (UID 10001), with no Go
toolchain and no source.

## Further reading

- [`docs/decisions.md`](docs/decisions.md): why Gin, why hand-written SQL,
  why PostgreSQL search, 403 versus 404, generated column versus trigger,
  why scores are stored.
- [`docs/notes/`](docs/notes/): one design note per module, each pointing at
  the lines it explains.

## License

MIT. See [LICENSE](LICENSE).
