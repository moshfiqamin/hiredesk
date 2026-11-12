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

## Contents

1. [Getting started, step by step](#getting-started-step-by-step)
2. [Troubleshooting](#troubleshooting)
3. [How to read this repository](#how-to-read-this-repository)
4. [Architecture](#architecture)
5. [Technical highlights](#technical-highlights)
6. [Test strategy](#test-strategy)
7. [Out of scope, deliberately](#out-of-scope-deliberately)
8. [What I would build next](#what-i-would-build-next)
9. [Reference](#reference): endpoints, errors, commands, configuration, layout
10. [Contributing and security](#contributing-and-security)

## Getting started, step by step

Every step says what to type, where, and what you should see. All commands are
run in a terminal, from the repository folder unless a step says otherwise.

### Step 1. Install the prerequisites

| Tool | Needed for | Check it with |
|---|---|---|
| Docker with Compose v2 | running everything | `docker compose version` |
| Git | getting the code | `git --version` |
| GNU Make and `openssl` | the `make` commands (preinstalled on macOS and most Linux) | `make --version` and `openssl version` |
| Go 1.27.3 | **only** for running the Go tests (step 8) | `go version` |
| Node.js 22 | **only** for the browser tests (step 8) | `node --version` |

Docker must be running. On macOS and Windows that means Docker Desktop is open.

### Step 2. Get the code

```bash
git clone https://github.com/moshfiqamin/hiredesk.git
cd hiredesk
```

### Step 3. Start the stack

```bash
make up
```

This one command:

1. creates `.env` from `.env.example`, with a freshly generated random `JWT_SECRET`;
2. builds the API and frontend images (the first build takes a few minutes);
3. starts PostgreSQL and waits for its health check;
4. applies all 13 database migrations;
5. starts the API on port **8080**, the frontend on port **3000**, and a
   database browser on port **8081**.

> **Do not create `.env` by copying `.env.example`.** Its `JWT_SECRET` is a
> placeholder that is deliberately too short, so the API refuses to start.
> If you already copied it, run `rm .env && make up` and a proper one is
> generated.

### Step 4. Check that it is running

```bash
curl -s localhost:8080/healthz
```

You should see `{"status":"ok"}`. Then check the database connection:

```bash
curl -s localhost:8080/readyz
```

Also `{"status":"ok"}`. If either fails, see [Troubleshooting](#troubleshooting).

### Step 5. Create the demo accounts

```bash
make seed
```

Creates four accounts. It is safe to run twice: existing accounts are left alone.

| Role | Email | Password |
|---|---|---|
| admin | `admin@hiredesk.test` | `admin-password` |
| interviewer | `interviewer@hiredesk.test` | `interviewer-password` |
| candidate | `candidate1@hiredesk.test` | `candidate-password` |
| candidate | `candidate2@hiredesk.test` | `candidate-password` |

These are local development fixtures with published passwords. The seeder
refuses to run when `ENV=production`.

### Step 6. Walk through the product

Open **<http://localhost:3000>**. The whole hiring flow takes about five
minutes. Use a private browser window for each new person, so that two
sign-ins do not overwrite each other.

**As the admin** (`admin@hiredesk.test`):

1. Sign in, then choose **Create role**. Enter a title, a location and the
   number of vacancies, then **Create draft**.
2. Open **Questions**, use **Add question** twice: one *Text* question such as
   "Describe a Go project you built", and one *Number* question such as
   "Years of Go?". Then **Save form**.
3. Open **Rubric**, use **Add criterion** to build the scoring rules, for
   example "Go knowledge" (weight 60, keywords `go`, `goroutines`, `postgres`)
   reading the first question, and a second criterion worth 40. Save becomes
   available once the weights add up to exactly 100. Then **Save rubric**.
4. Choose **Open for applications**.

**As a candidate**, in a new private window:

5. Go to **<http://localhost:3000/roles>** without signing in, open the role,
   and choose **Create an account to apply** (or sign in as
   `candidate1@hiredesk.test`).
6. Fill in the form, which is built from the role's questions, and choose
   **Send application**. The application appears under **My applications**
   with its status.

**Back as the admin:**

7. Open the role, then **Ranking**. Choose the candidate to see the score
   breakdown: each criterion's points, and the words in the answer that earned
   them.
8. Under **Interviewers**, choose **Manage** and assign the seeded interviewer.

**As the interviewer** (`interviewer@hiredesk.test`):

9. Sign in. The candidate is on **My assignments**. Open it, choose a mark
   from 1 to 10, write notes and **Submit review**. The admin now sees the
   review on the application.

**Just want to see scores?** `make seed-bulk` creates an open role with 500
applications, each scored through the real code path.

### Step 7. Explore the API in Swagger UI

Open **<http://localhost:8080/docs>**: interactive API documentation served
from inside the binary.

1. Expand `POST /v1/auth/login` and choose **Try it out**. The example body
   already holds the admin's credentials; choose **Execute**.
2. Copy `access_token` from the response.
3. Choose **Authorize** at the top of the page, paste the token, and confirm.

Every padlocked operation now runs as the admin.

### Step 8. Run the tests

```bash
make test
```

Unit tests with the race detector. Needs Go; no database and no Docker.

```bash
make test-integration
```

SQL tests against the running database, plus the authorization matrix on a
throwaway PostgreSQL. Needs Go, `make up` running, and Docker.

```bash
make e2e
```

The full hiring flow in a real browser. Needs Node, plus `make up` and
`make seed`. Playwright downloads Chromium the first time.

```bash
make lint
```

`golangci-lint` with 13 linters. Needs `golangci-lint` installed.

### Step 9. Stop or reset

```bash
make down
```

Stops everything. The database is kept for next time.

```bash
make reset
```

Deletes the database and starts fresh. It asks for confirmation first.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `curl` says connection refused | The API is still starting or is restarting | `make ps` shows its state; `make logs` shows why |
| Logs say `JWT_SECRET is too short` | `.env` was copied from `.env.example` | `rm .env && make up` |
| `port is already allocated` | Another program uses 8080, 3000 or 5432 | Set `API_PORT`, `WEB_PORT` or `POSTGRES_PORT` in `.env`, then `make down && make up` |
| `role "hiredesk" does not exist` | A local PostgreSQL answers on 5432 instead of the container | Set `POSTGRES_PORT=5433` in `.env`, then `make down && make up` |
| `-race requires cgo` on Linux | The race detector needs a C compiler | Install `gcc` |
| Sign-in returns 429 | The API allows 10 sign-ins a minute per address | Wait a minute |

**Known issue:** in the rubric builder, changing a criterion's kind after
typing its name and weight can lose those values. Choose the kind first, then
fill in the fields. The cause and the fix are described in
[`docs/notes/14-3-responsive-accessible.md`](docs/notes/14-3-responsive-accessible.md).

## How to read this repository

For a reviewer with limited time, in this order:

1. **[`internal/scoring/engine.go`](internal/scoring/engine.go)**: the core
   idea, a pure function from answers and a rubric to an auditable score.
2. **[`test/integration/authz_test.go`](test/integration/authz_test.go)**:
   every caller against every protected route, on a real PostgreSQL.
3. **[`internal/store/tokens.go`](internal/store/tokens.go)**: refresh-token
   rotation with replay detection, in SQL.
4. **[`docs/decisions.md`](docs/decisions.md)**: the major decisions, what
   was rejected, and what each one costs.
5. **[`docs/notes/`](docs/notes/)**: one design note per module, in build
   order, each linking to the lines it explains.
6. **[`web/src/components/score/score-breakdown.tsx`](web/src/components/score/score-breakdown.tsx)**:
   the score shown on screen, with its evidence highlighted.

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
- **An authorization matrix test.** Every caller (anonymous, three roles, assigned and unassigned interviewers) against every protected route on a real PostgreSQL: 358 subtests, and a route added without a row fails the build ([`authz_test.go`](test/integration/authz_test.go)).
- **A concurrent duplicate-application test.** Ten goroutines submit the same candidate's application at once, released by one start gate; exactly one succeeds and the rest get 409 `already_applied`, enforced by a unique constraint rather than a check-then-insert ([`concurrency_test.go`](test/integration/concurrency_test.go)).
- **An N+1 query, measured.** The ranking endpoint is deliberately naive today: 502 statements for 500 applicants, **53.9 ms median server time** (20 runs, API and PostgreSQL in Docker on one laptop). The single-query fix is designed but **not yet built**, so there is no "after" number yet ([notes](docs/notes/07-5-ranking.md)).
- **A score you can audit, on screen.** The admin frontend draws each criterion as earned against available on one zero-based axis, highlights the words that matched in the answer (XSS-safe: segments rendered as text, never `dangerouslySetInnerHTML`), and says in a sentence why any criterion scored zero ([`score-breakdown.tsx`](web/src/components/score/score-breakdown.tsx), [notes](docs/notes/13-2-evidence.md)).
- **Whole-word full-text search on a GIN index.** A generated `tsvector` column with stemming: "testing" finds "tested", "go" does not find "google", and the query is a bound parameter ([`000012_search.up.sql`](migrations/000012_search.up.sql), [decision D7](docs/decisions.md)).
- **Graceful shutdown, proven.** On SIGTERM a request already in flight finishes with 200, new connections are refused, then the pool closes and the process exits 0. Past the 10-second budget it logs which requests it abandoned and exits 1 ([`cmd/api/main.go`](cmd/api/main.go), [notes](docs/notes/10-4-graceful-shutdown.md)).
- **Request IDs end to end.** Every response carries `X-Request-ID`, every error body carries `request_id`, and every log line written during the request carries both the ID and the user ID, read from `context.Context` rather than passed down ([notes](docs/notes/10-2-structured-logging.md)).
- **Accessible and responsive, by test.** An axe audit covers 17 pages at 375, 768 and 1280px in both themes (102 page checks: zero WCAG 2.2 AA violations, no sideways scrolling, one `h1` each, a visible focus indicator on everything Tab reaches), and a contrast script measures every token pair. One Playwright test walks the product end to end through roles and labels only ([notes](docs/notes/14-3-responsive-accessible.md)).
- **No credentials in the logs, by test.** A full register, login, refresh, replay and logout cycle runs at debug level, and the test fails if any password, token or `Authorization` value appears in the output.

## Test strategy

| Layer | What runs | How | Coverage |
|---|---|---|---|
| Pure logic | `domain`, `scoring`, `auth` | table-driven unit tests, a fuzz test on the keyword matcher | 96.0%, 97.6%, 96.0% |
| HTTP | `internal/api` | handlers and middleware against in-memory fakes, via `httptest` | 91.2% |
| Plumbing | `config`, `platform` | env parsing, log redaction, context propagation | 72.5%, 94.3% |
| SQL | `internal/store` | integration tests against the running PostgreSQL | 83.1% |
| End to end | `test/integration` | the production router over real stores, on a PostgreSQL started by testcontainers-go | (included below) |
| Browser | `web/e2e/full-flow.spec.ts` | Playwright against the running stack: admin builds and opens a role, a candidate registers and applies, the admin assigns, an interviewer reviews | one flow, 4 steps |
| Accessibility | `web/e2e/audit`, `web/scripts/contrast-report.mjs` | axe-core on every page at three widths in both themes, plus sideways-scroll, heading and focus sweeps; WCAG ratios for every token pair | 102 page checks |

Unit-test numbers are statement coverage per package from
`go test -race -cover ./...`, exactly as CI runs it, with no `.env` present.
`make test` can report a higher figure for `config` (81.7% on one machine),
because the Makefile exports every variable in `.env` into the test process.
The store figure is from the integration run, measured with `-coverpkg`
across `./internal/...`; that run alone covers **69.9%** of all statements in
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

Unit tests with the race detector. Needs Go 1.27.3 locally, but no database
and no Docker. On Linux, Go's race detector also needs a C compiler; if it
reports `-race requires cgo`, install `gcc`.

```bash
make test-integration
```

Integration tests. Needs Go, and `make up` running, plus a Docker daemon for
testcontainers.

```bash
make e2e
```

The end-to-end flow in Chromium. Needs Node, and `make up && make seed`
running; Playwright's browser downloads on first use.

```bash
make a11y
```

The contrast report, then the accessibility and responsive audit. Same needs
as `make e2e`. Each of the two signs in a few times, and the API allows ten
sign-ins a minute per address, so run them a minute apart.

## Out of scope, deliberately

- **Email, notifications, file uploads.** A CV is a URL, not a stored file.
- **Multi-tenancy.** One organisation per deployment.
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
| `GET` | `/v1/admin/job-posts` | admin (every post, with application counts) |
| `GET` | `/v1/admin/interviewers` | admin (interviewers only, with their open and pending load) |
| `POST` | `/v1/job-posts` | admin |
| `GET` | `/v1/job-posts/:id/listing` | anyone, no token (open posts only; with the questions, never the rubric) |
| `GET` | `/v1/job-posts/:id` | any signed-in user (drafts: admin only) |
| `PATCH` | `/v1/job-posts/:id` | admin |
| `POST` | `/v1/job-posts/:id/open` · `/close` | admin |
| `GET` `POST` | `/v1/job-posts/:id/questions` | admin |
| `PUT` | `/v1/job-posts/:id/questions/order` | admin |
| `PATCH` `DELETE` | `/v1/job-posts/:id/questions/:questionID` | admin |
| `GET` `PUT` | `/v1/job-posts/:id/rubric` | admin |
| `POST` | `/v1/job-posts/:id/applications` | candidate |
| `GET` | `/v1/job-posts/:id/applications/search?q=` | admin |
| `GET` | `/v1/job-posts/:id/ranking` | admin (with each row's per-criterion points) |
| `POST` | `/v1/job-posts/:id/rescore-all` | admin |
| `GET` | `/v1/applications?sort=score` | any signed-in user (filtered per role, in SQL); score order for admins and interviewers; `reviewed_by_me` for interviewers |
| `GET` | `/v1/applications/:id` | admin, assigned interviewer, its candidate (with `allowed_next_statuses`) |
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
| `make e2e` | The end-to-end flow in a browser (needs `make up && make seed`) |
| `make a11y` | Contrast report and the accessibility/responsive audit of every page |
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
| `ALLOWED_ORIGINS` | no | empty in the binary; Compose sets `http://localhost:<WEB_PORT>` | Comma-separated `scheme://host[:port]`; wildcards refused |
| `DB_MAX_CONNS` | no | `10` | Pool ceiling, 1–500 |
| `DB_MIN_CONNS` | no | `2` | Idle connections kept open; at most `DB_MAX_CONNS` |
| `DB_MAX_CONN_LIFETIME` | no | `1h` | Maximum age of a connection |
| `DB_MAX_CONN_IDLE_TIME` | no | `30m` | Idle connections older than this are closed |
| `DB_CONNECT_TIMEOUT` | no | `5s` | Budget for the startup ping |
| `DB_PING_TIMEOUT` | no | `2s` | Budget for the `/readyz` ping |
| `SHUTDOWN_TIMEOUT` | no | `10s` | Grace period for in-flight requests on SIGTERM |
| `API_PORT` · `WEB_PORT` · `POSTGRES_PORT` · `ADMINER_PORT` | no | `8080` · `3000` · `5432` · `8081` | Host ports Compose publishes |
| `NEXT_PUBLIC_API_URL` | no | `http://localhost:<API_PORT>` | The API URL as the browser sees it; baked into the frontend at build time |

Durations use Go syntax (`30s`, `1h30m`). See [`.env.example`](.env.example)
for the full, commented template.

### Running without Docker

```bash
make build && DATABASE_URL="postgres://hiredesk:hiredesk_dev_password@localhost:5432/hiredesk?sslmode=disable" JWT_SECRET="$(openssl rand -hex 32)" ./bin/hiredesk-api
```

Needs PostgreSQL with the migrations applied; `make up` provides both.

### Layout

```
.github/              CI workflow, Dependabot, pull request template
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
web/                  Next.js frontend (App Router, Tailwind, shadcn/ui)
docs/decisions.md     decision records
docs/design/          the interactive design prototype
docs/notes/           design notes, one per module
CONTRIBUTING.md       conventions and the change workflow
SECURITY.md           how to report a vulnerability
.gitleaks.toml        secret-scan configuration used by CI
```

The image is a multi-stage build: 74 MB, three static binaries (API, seeder,
reindexer), CA certificates and an unprivileged user (UID 10001), with no Go
toolchain and no source.

## Contributing and security

- [`CONTRIBUTING.md`](CONTRIBUTING.md): conventions, the step-by-step change
  workflow, adding a migration or a route, and the pull request checklist.
- [`SECURITY.md`](SECURITY.md): how to report a vulnerability privately.
- [`docs/decisions.md`](docs/decisions.md): why Gin, why hand-written SQL,
  why PostgreSQL search, 403 versus 404, generated column versus trigger,
  why scores are stored.
- [`docs/notes/`](docs/notes/): one design note per module, each pointing at
  the lines it explains.

## License

MIT. See [LICENSE](LICENSE).
