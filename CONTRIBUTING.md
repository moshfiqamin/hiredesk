# Contributing to HireDesk

This file describes how the code is organised and the rules every change
follows. The README explains how to run the project; this explains how to
change it without breaking what it promises.

## Contents

1. [Set up a working copy](#1-set-up-a-working-copy)
2. [Project conventions](#2-project-conventions)
3. [Making a change, step by step](#3-making-a-change-step-by-step)
4. [Common tasks](#4-common-tasks)
5. [Commit messages](#5-commit-messages)
6. [Pull request checklist](#6-pull-request-checklist)

---

## 1. Set up a working copy

| Tool | Version | Needed for |
|---|---|---|
| Docker with Compose v2 | any recent | running the stack, integration tests |
| GNU Make, `openssl` | preinstalled on macOS and most Linux distributions | every `make` target |
| Go | 1.27.3 (read from `go.mod`) | unit tests, lint, building outside Docker |
| golangci-lint | v2.15 (the version CI pins) | `make lint` |
| Node.js | 22 or later | the frontend, Playwright tests |

```bash
git clone https://github.com/moshfiqamin/hiredesk.git
cd hiredesk
make up
```

`make up` creates `.env` with a freshly generated `JWT_SECRET`. `.env` is
gitignored and must never be committed.

## 2. Project conventions

### Layout

- Runnable binaries live in `cmd/<name>/`; everything else is a private package
  in `internal/`, which no other module can import.
- Dependencies point inward, toward `internal/domain`. `domain` and `scoring`
  import neither pgx, Gin nor `net/http`; this is checked with
  `go list -deps ./internal/domain ./internal/scoring`.
- `internal/store` is the only package that speaks SQL; `internal/api` is the
  only package that speaks HTTP.

### Configuration

- Read once at startup in `internal/config` into a `Config` struct, then passed
  explicitly. **No `os.Getenv` anywhere else.**
- Fail fast: a misconfigured server refuses to boot rather than breaking on its
  first request. Every missing required variable is reported at once.
- A new variable is added in three places together: `internal/config`,
  `.env.example` (with a comment), and the configuration table in the README.

### Errors

- Return errors; do not log and continue.
- Wrap with `%w` so `errors.Is` and `errors.As` keep working.
- Use a named error type when a caller needs to inspect the detail.
- Domain errors are mapped to HTTP statuses in one table in `internal/api`;
  a raw database or driver error never reaches a client.

### Secrets

- Never defaulted, never logged, never committed.
- A secret that was committed, even once, is treated as compromised and
  rotated. Deleting it in a later commit does not remove it from history.
- CI runs a secret scan over the full history on every push.

### Health endpoints

- `/healthz` is liveness and must not touch the database.
- `/readyz` is readiness and may.

### Comments

- Doc comments on every exported identifier, starting with its name (enforced
  by `revive`).
- Otherwise, comment the *why* where it is not obvious from the line itself.
  Longer explanations belong in `docs/notes/`, not in the code.

### Design notes

Each module has a note in `docs/notes/NN-topic.md` covering what was built,
why, the rejected alternative, and how it was verified. Sections link to the
exact line they explain:

```markdown
**📍 Open:** [`internal/config/config.go:76`](../../internal/config/config.go#L76)
```

Line links break when code moves, so write or refresh a note after the code is
final, and check its anchors with `grep -n`.

## 3. Making a change, step by step

1. **Branch from `main`.**
   ```bash
   git switch -c feat/short-description
   ```
2. **Write the test first** where the behaviour is testable: table-driven for
   pure logic, `httptest` against fakes for handlers, the integration suite for
   SQL.
3. **Make the change.**
4. **Run the gates**, in this order. All four must pass before you push:
   ```bash
   make fmt
   make lint
   make test
   go build ./...
   ```
5. **Run the integration tests** if you touched SQL, a migration or a route:
   ```bash
   make test-integration
   ```
6. **Update the docs:** the README tables (endpoints, commands,
   configuration) and the module's note in `docs/notes/`.
7. **Commit** using the format in [section 5](#5-commit-messages), then push and
   open a pull request. CI runs the same gates plus the secret scan.

## 4. Common tasks

### Add a migration

```bash
make migrate-create NAME=add_interview_slots
```

Creates a numbered `up`/`down` pair in `migrations/`. Write both: every `up`
needs a `down` that undoes it exactly. Then apply it:

```bash
make migrate-up
```

Prefer additive migrations (a new nullable column, a new table, an index built
`CONCURRENTLY`). Renaming or dropping a column that running code still reads
needs two releases: stop reading it first, remove it second.

### Add a protected route

1. Register it in `internal/api/router.go` behind the right middleware.
2. Add it to `api/openapi.yaml`. A test fails if the router and the spec
   disagree.
3. Add its column to the authorization matrix in
   `test/integration/authz_test.go`. A route without a row fails the build.
4. Run `make test-integration`.

### Reset the local database

```bash
make reset
```

Destroys the database volume and starts fresh. It asks for confirmation.

## 5. Commit messages

[Conventional Commits](https://www.conventionalcommits.org/), scoped to the
area touched:

```
feat(applications): server-paged table with score sort
fix(auth): revoke every session on refresh-token reuse
docs(readme): step-by-step getting started
ci: scan the full history for secrets
```

The body says what changed and how it was verified.

## 6. Pull request checklist

- [ ] `make fmt && make lint && make test && go build ./...` pass locally
- [ ] `make test-integration` passes if SQL, a migration or a route changed
- [ ] New configuration is in `internal/config`, `.env.example` and the README
- [ ] No secret, credential or local path in the diff
- [ ] README tables and the relevant `docs/notes/` entry are up to date
