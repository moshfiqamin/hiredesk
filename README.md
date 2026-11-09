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
Postgres, waits for it to pass its healthcheck, then starts the API.

```bash
curl -s localhost:8080/healthz
```

```json
{"status":"ok"}
```

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
| `make test` | Tests with the race detector and coverage |
| `make lint` | `golangci-lint` (13 linters) |
| `make fmt` | Format code and tidy `go.mod` |
| `make build` | Compile to `./bin` without Docker |
| `make reset` | Destroy the database volume and start fresh |

## Layout

```
cmd/api/              main package — the server binary
internal/config/      environment config, loaded once into a struct
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

See [`.env.example`](.env.example) for the full template.

## Running without Docker

```bash
make build && DATABASE_URL="postgres://localhost:5432/hiredesk?sslmode=disable" JWT_SECRET="$(openssl rand -hex 32)" ./bin/hiredesk-api
```

## Engineering notes

- **Multi-stage Docker build.** The runtime image is 22MB and contains only a
  statically linked binary, CA certificates and an unprivileged user — no Go
  toolchain, no source. A single-stage build of the same code is 679MB.
- **Runs as non-root.** UID 10001, no shell, no home directory.
- **Startup ordering is explicit.** The API waits on Postgres's `pg_isready`
  healthcheck rather than on container creation, so a cold start cannot race
  `initdb`.
- **Fail fast.** Invalid configuration stops the process at startup, not on the
  first request that needs it.
- **CI on every push and PR.** Build, vet, race-detector tests, a `go.mod`
  tidiness gate, and `golangci-lint`.

Design decisions and the reasoning behind them are written up per module in
[`docs/notes/`](docs/notes/).

## License

MIT — see [LICENSE](LICENSE).
