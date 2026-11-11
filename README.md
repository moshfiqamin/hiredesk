# HireDesk

[![CI](https://github.com/moshfiqamin/hiredesk/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/moshfiqamin/hiredesk/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/moshfiqamin/hiredesk)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A hiring pipeline REST API in Go — jobs, candidates, applications and stage
transitions — built with Gin and Postgres, where every point of a candidate's
score can be traced to the answer that earned it.

## Quick start

Requires only Docker. No Go toolchain, no local Postgres.

```bash
git clone https://github.com/moshfiqamin/hiredesk.git && cd hiredesk && make up
```

That generates `.env` with a random `JWT_SECRET`, builds the image, starts
Postgres, waits for it to pass its healthcheck, **applies all eleven schema
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

## What a score looks like

Every application is scored the moment it is submitted, inside the same
transaction, against a rubric stored as data. The breakdown names the answer
behind each number and quotes the part of it that mattered
(`GET /v1/applications/:id/score`, abridged):

```json
{
  "application_id": "d5f1a1b8-bbe5-457e-b0ac-78734c0fea26",
  "total_earned": 66.8,
  "total_available": 100,
  "percentage": 66.8,
  "rubric_version": 1,
  "breakdown": [
    {
      "criterion": "Go knowledge",
      "kind": "keyword_match",
      "earned": 12,
      "available": 30,
      "detail": "matched 2 of 5 keywords: go, gin",
      "evidence": {
        "question": "Describe a project you built",
        "answer_id": "a188f925-702d-4f6a-a839-61844fc66603",
        "excerpt": "...services for a few years. Last year I built a REST API in Go using Gin, with goroutines handling the background jobs..."
      }
    },
    {
      "criterion": "Experience",
      "kind": "experience_years",
      "earned": 30,
      "available": 40,
      "detail": "3 years, 75% of the way from 0 to the target of 4",
      "evidence": { "question": "Years of Go?", "answer_id": "6858469c-bd7b-40eb-83b7-220672afec44", "excerpt": "3" }
    }
  ]
}
```

A rubric is never edited in place: replacing it adds a new version, and a
score keeps pointing at the exact criteria that produced it.

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
| `GET` | `/v1/applications` | The applications the caller may see: all of them for an admin, their own for a candidate, assigned ones for an interviewer. Filters `?status=` and `?job_post_id=`; newest first, keyset-paginated with `?limit=20&after=<next_cursor>` (`limit` capped at 100). Body: `{"data":[…],"next_cursor":"…","total":42}`, where `total` counts every match across all pages. |
| `GET` | `/v1/applications/:id` | One application with its `answers`, in form order, and its `events`, oldest first. An application the caller may not see is a 404, identical to one that does not exist — see [`docs/decisions.md`](docs/decisions.md). |
| `GET` | `/v1/applications/:id/score` | **Admins, and the interviewer assigned to the application.** The stored score: `total_earned`, `total_available`, `percentage`, `rubric_version`, and a `breakdown` with each criterion's `earned`, `available`, `detail` and `evidence` (`question`, `answer_id`, and an `excerpt` of about 120 characters around the first keyword match; `null` when no single answer was the evidence). The candidate who applied gets a 403; anyone who may not see the application gets the usual 404. An application with no score is a 404 `not_scored`. Built in one query, whatever the number of criteria. |
| `POST` | `/v1/applications/:id/rescore` | **Admin only.** Replace the application's scores by scoring its stored answers against the post's current rubric, in one transaction. Returns `previous` (null if it had none) and `current` totals and versions, and `changed`. Records an `application.rescored` event with both totals and both rubric versions. A post with no rubric is a 409 `no_rubric`, and the old scores are kept. |
| `PATCH` | `/v1/applications/:id/status` | **Admin only.** Body: `status`. One step at a time through `submitted → shortlisted → interview → offer`; `rejected` from any stage, and final. Any other move is a 409 `invalid_transition` listing `allowed_next_statuses`. An offer when every vacancy already has one is a 409 `no_vacancies`. Each change records an `application.status_changed` event with `from` and `to`. |
| `GET` | `/v1/job-posts` | **Public, no token needed.** Open posts, newest first: `id`, `title`, `location`, `employment_type`, `skill_tags`, `remaining_vacancies`. Keyset-paginated: `?limit=20&after=<next_cursor>`, `limit` capped at 100. Body: `{"data":[…],"next_cursor":"…"}`, `null` on the last page. |
| `POST` | `/v1/job-posts` | **Admin only.** Create a job post, always as a draft. Body: `title` (1–200 characters), and optionally `description`, `employment_type`, `location`, `vacancies` (default 1), `salary_min`, `salary_max`, `closes_at`. Returns 201 and the post. |
| `GET` | `/v1/job-posts/:id` | One job post with its questions. A draft is visible to admins only; to anyone else it is a 404. |
| `PATCH` | `/v1/job-posts/:id` | **Admin only.** Partial update: only the fields present in the body change, and a field sent as `""` is stored as empty. `null` is treated as not sent. The rules are checked against the post as it would be after the change. |
| `POST` | `/v1/job-posts/:id/open` | **Admin only.** Draft to open. Refused with 409 `not_ready` unless the post has at least one question and a rubric with a positive weight; `details` names what is missing. |
| `POST` | `/v1/job-posts/:id/close` | **Admin only.** Open to closed, which is final. Any move the lifecycle does not allow is a 409 `invalid_transition` listing `allowed_next_statuses`. |
| `GET` | `/v1/job-posts/:id/rubric` | **Admin only.** The current version of the post's scoring rubric: `version` (`null` before the first) and `criteria` in order, each with `label`, `kind`, `weight`, `question_id` and `config`. |
| `PUT` | `/v1/job-posts/:id/rubric` | **Admin only, any status.** Body: `criteria`, the whole rubric; it becomes the next version, and earlier versions are kept for the scores that use them. Kinds: `keyword_match` `{"keywords":[…]}`, `experience_years` `{"min":0,"target":2}`, `answer_depth` `{"min_chars":50,"target_chars":400}`, `boolean_all` `{"question_ids":[…]}`. Weights must sum to 100. Every problem comes back in one 422 keyed like `criteria[2].config`. Nothing is rescored: the response's `applications_to_rescore` says how many applications still carry an older score. |
| `POST` | `/v1/job-posts/:id/rescore-all` | **Admin only.** Rescore every application to the post against its current rubric, atomically, in a fixed number of statements (500 applications take about 50 ms). Returns `rubric_version`, `rescored` and `changed`. |
| `GET` | `/v1/job-posts/:id/questions` | **Admin only.** The post's application form in order, whatever its status. Everyone else gets the form inside `GET /v1/job-posts/:id`. |
| `POST` | `/v1/job-posts/:id/questions` | **Admin only, draft only.** Add a question. Body: `prompt`, `kind` (`text`, `number`, `boolean`, `choice`), `choices` (required for `choice`, absent otherwise), `min_length` (text only), `required` (default true), `position` (default: after the last). |
| `PATCH` | `/v1/job-posts/:id/questions/:questionID` | **Admin only, draft only.** Partial update. Send `"choices":[]` to clear choices when changing the kind. |
| `DELETE` | `/v1/job-posts/:id/questions/:questionID` | **Admin only, draft only.** Returns 204. |
| `POST` | `/v1/job-posts/:id/applications` | **Candidate only.** Apply to an open post. Body: `cv_url` (absolute `http`/`https`), `answers`: `[{"question_id","answer"}]`, where `answer` may be a string, number or boolean. Every problem comes back in one 422, under `details.cv_url` and `details.answers` (each with `question_id`, `code` and, for `too_short`, `min_length` and `got`). A closed post is a 409 `not_accepting_applications`, a second application a 409 `already_applied`, also when several arrive at once. Returns 201, the application and its answers; the application, answers, one score per rubric criterion and an `application.submitted` event are written in one transaction, and a rubric that cannot be applied fails the whole submission. |
| `PUT` | `/v1/job-posts/:id/questions/order` | **Admin only, draft only.** Body: `question_ids`, every question of the post exactly once, in the new order. Applied in one transaction. |

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

Browse the open jobs, with no account at all:

```bash
curl -s "localhost:8080/v1/job-posts?limit=20"
```

Register a candidate of your own:

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
| `make test-integration` | Database tests against the running stack, and the authorization matrix in its own PostgreSQL container (needs `make up` and Docker) |
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
internal/scoring/     pure scoring engine: rubric validation and four strategies
internal/store/       Postgres connection pool and hand-written SQL queries
migrations/           golang-migrate pairs, applied on container start
test/integration/     tests against a real PostgreSQL started by testcontainers-go
docs/notes/           design notes, one per module
docs/decisions.md     decision records
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
always an object: it names each bad field when validation fails, and lists the
allowed next statuses when a status change is refused.

| Status | `code` | Meaning |
|---|---|---|
| 400 | `malformed_request` | The body is not a JSON object |
| 400 | `invalid_id` | The ID in the URL is not a UUID |
| 400 | `invalid_cursor` | The `after` parameter is not a cursor this API issued; start from the first page |
| 401 | `invalid_credentials` | Login failed |
| 401 | `token_expired` | The access token has expired; refresh and retry |
| 401 | `unauthenticated` | No usable access or refresh token |
| 403 | `forbidden` | Authenticated, not permitted |
| 404 | `not_found` | The record does not exist |
| 404 | `not_scored` | The application exists but has no stored score |
| 404 | `route_not_found` | No such endpoint |
| 405 | `method_not_allowed` | The endpoint exists under another method |
| 409 | `duplicate` | A record with the same unique key exists |
| 409 | `already_applied` | The candidate has already applied to this job post |
| 409 | `not_accepting_applications` | The job post is closed |
| 409 | `no_vacancies` | Every vacancy on the job post already has an offer; see `details` |
| 409 | `invalid_transition` | That status change is not allowed from the current status |
| 409 | `no_rubric` | The job post has no rubric to score against |
| 409 | `not_ready` | The resource lacks something the new status needs; see `details` |
| 409 | `form_locked` | The job post is no longer a draft, so its questions cannot change |
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

Twelve tables across nine migrations, applied automatically when the stack
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
| `application_events` | Append-only audit trail of each application |
| `job_post_events` | Append-only audit trail of each job post, such as status changes |
| `interviewer_assignments` | Who is reviewing what |
| `reviews` | Marks (1–10) and notes |

The reasoning behind every constraint, index and delete action is in
[`docs/notes/01-schema.md`](docs/notes/01-schema.md).

## Engineering notes

- **Multi-stage Docker build.** The runtime image is 43MB and contains only two
  statically linked binaries (the API and the development seeder), CA
  certificates and an unprivileged user — no Go toolchain, no source. A
  single-stage build is over 679MB.
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
- **Roles come from the database, not the token.** Every authenticated
  request reloads the user, so a demotion or a deleted account takes effect on
  the next request rather than when the token expires.
- **Authorization is filtered in SQL.** Each role gets its own query, so rows
  a caller may not see never leave the database. A record the caller may not
  see is a 404, indistinguishable from one that does not exist.
- **An authorization matrix runs in CI.** Every caller against every protected
  endpoint, on a real PostgreSQL started by testcontainers-go; adding a route
  without a row for it fails the build.
- **Rotation is race-safe.** The token row is locked with `FOR UPDATE` inside a
  transaction, so two concurrent refreshes cannot both succeed.
- **A pure scoring engine.** Scoring imports only the standard library and
  the domain types — no database, HTTP, clock or randomness — so the same
  answers and rubric always give the same, explainable score. Keywords match
  whole words only (`go` never matches `google`), answer length counts
  characters rather than bytes, and a fuzz test checks the matcher's
  boundaries against generated input.
- **One error shape.** Every failure — validation, unknown route, panic —
  returns the same JSON envelope, and internal causes are logged, never sent.
- **CI on every push and PR.** Build, vet, race-detector tests, a `go.mod`
  tidiness gate, `golangci-lint`, and the authorization matrix.

Design decisions and the reasoning behind them are written up per module in
[`docs/notes/`](docs/notes/).

## License

MIT — see [LICENSE](LICENSE).
