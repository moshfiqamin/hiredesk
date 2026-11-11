# Module 0 — Foundation

Go module, config loading, Gin server, Docker, Compose, Makefile, CI.

**How to read this.** Every section names the exact file and line to open. Click
the link, read the code, then come back. The code carries short comments; the
full reasoning lives here.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-gomod-and-the-module-path) | `go.mod` and the module path | [`go.mod`](../../go.mod) |
| [2](#2-why-internal-is-special) | Why `internal/` is special | [`internal/`](../../internal) |
| [3](#3-config-in-a-struct-not-osgetenv) | Config in a struct, not `os.Getenv` | [`config.go:62`](../../internal/config/config.go#L62) |
| [4](#4-reporting-every-missing-variable-at-once) | Reporting every missing variable at once | [`config.go:157`](../../internal/config/config.go#L157) |
| [5](#5-the-string-pointer) | The `*[]string` pointer | [`config.go:263`](../../internal/config/config.go#L263) |
| [6](#6-named-error-types-and-errorsas) | Named error types and `errors.As` | [`config.go:109`](../../internal/config/config.go#L109) |
| [7](#7-healthz-vs-readyz) | `/healthz` vs `/readyz` | [`health.go:21`](../../internal/api/health.go#L21) |
| [8](#8-gin-modes-and-trusted-proxies) | Gin modes and trusted proxies | [`main.go:73`](../../cmd/api/main.go#L73) |
| [9](#9-the-multi-stage-dockerfile) | The multi-stage Dockerfile | [`Dockerfile:4`](../../Dockerfile#L4) |
| [10](#10-cgo_enabled0-and-static-linking) | `CGO_ENABLED=0` and static linking | [`Dockerfile:20`](../../Dockerfile#L20) |
| [11](#11-docker-layer-cache-ordering) | Docker layer cache ordering | [`Dockerfile:12`](../../Dockerfile#L12) |
| [12](#12-what-non-root-actually-buys-you) | What non-root actually buys you | [`Dockerfile:50`](../../Dockerfile#L50) |
| [13](#13-depends_on-condition-service_healthy) | `condition: service_healthy` | [`compose:46`](../../docker-compose.yml#L46) |
| [14](#14-named-volumes) | Named volumes | [`compose:24`](../../docker-compose.yml#L24) |
| [15](#15-container-to-container-dns) | Container-to-container DNS | [`compose:52`](../../docker-compose.yml#L52) |
| [16](#16-makefile-mechanics) | Makefile mechanics | [`Makefile:153`](../../Makefile#L153) |
| [17](#17-what-ci-is-actually-for) | What CI is actually for | [`ci.yml`](../../.github/workflows/ci.yml) |
| [18](#18-the-race-detector) | The race detector | [`ci.yml:50`](../../.github/workflows/ci.yml#L50) |
| [19](#19-the-linters) | The linters | [`.golangci.yml:18`](../../.golangci.yml#L18) |

---

## 1. `go.mod` and the module path

**📍 Open:** [`go.mod`](../../go.mod)

```
module github.com/moshfiqamin/hiredesk
go 1.27.3
require github.com/gin-gonic/gin v1.11.0
```

`go.mod` turns a directory of `.go` files into a **module** — Go's unit of
versioning and dependency management.

The `module` line is not just a name. It is the **import path prefix** for
everything inside, which is why [`main.go:19`](../../cmd/api/main.go#L19)
imports `github.com/moshfiqamin/hiredesk/internal/config` rather than a
relative `./internal/config`. Go has no relative imports: every package is
addressed by module path + directory. Change the `module` line and every
internal import breaks.

It does not have to resolve to a real URL — it has to be **globally unique**,
and a GitHub path you own guarantees that for free.

`go.sum` holds cryptographic checksums of every dependency, including
transitive ones. It is the lockfile that makes today's build byte-identical to
next month's. **Commit both.**

`go mod tidy` reconciles the two with your actual imports.

---

## 2. Why `internal/` is special

**📍 Open:** [`internal/config/`](../../internal/config)

`internal/` is the one directory name the Go **compiler** treats specially. A
package under `internal/` can only be imported by code rooted at the parent of
that `internal/` directory.

So `internal/config` is importable by anything in
`github.com/moshfiqamin/hiredesk/...`, but if someone runs
`go get github.com/moshfiqamin/hiredesk` and imports it, their build **fails**.
Not a lint warning — a compile error.

**Why this matters:** Go has no package-level `public`/`private` keyword.
Capitalisation controls visibility within a module, and that is all. Anything
exported is API you have implicitly promised not to break. `internal/` is how
you say "exported so my own packages can use it, but not part of my contract
with the world" — which is why `Config.DatabaseURL` can be renamed later
without it being a breaking change.

The resulting shape:

- `cmd/<name>/` — one directory per binary, each `package main`.
  `go build ./cmd/api` yields an executable named `api`. Adding `cmd/worker/`
  later gives a second binary sharing the same internals.
- `internal/` — the implementation, protected from outside import.
- top-level packages — the deliberate public API. HireDesk has none, which is
  correct for a service.

---

## 3. Config in a struct, not `os.Getenv`

**📍 Open:** [`internal/config/config.go:62`](../../internal/config/config.go#L62)

```go
type Config struct {
	Port        int
	DatabaseURL string
	JWTSecret   string
	Env         string
}
```

Scattering `os.Getenv` across the codebase costs five things:

**1. Failures move from startup to runtime.** `os.Getenv("JWT_SECRET")` returns
`""` for an unset variable — no error, no panic. Call it inside token signing
and the server boots happily, passes `/healthz` happily, and then signs tokens
with an empty key the first time someone logs in.
[`Load()`](../../internal/config/config.go#L157) refuses to boot instead. A
server that will not start is a loud, cheap failure; a server signing tokens
with `""` is a silent, expensive one.

**2. No list of what the app needs.** With `os.Getenv` in twenty files, the only
answer to "what do I need to run this?" is grep — and grep misses anything
built dynamically. The struct *is* the answer, and cannot drift from the truth
because it is what the code reads.

**3. Strings instead of types.** `os.Getenv("PORT")` is a `string`, so every use
site re-parses it. `Config.Port` is an `int`, parsed and range-checked once.

**4. Untestable functions.** A function calling `os.Getenv` depends on
process-global mutable state. One taking `cfg *config.Config` is tested with a
struct literal. This is the deeper principle: **dependencies belong in
signatures.** `NewAuthService(cfg *config.Config)` tells you what it needs;
`NewAuthService()` that secretly reads the environment lies about it.

**5. No way to swap the source.** Add Vault or a config file later and only
`Load()` changes.

---

## 4. Reporting every missing variable at once

**📍 Open:** [`internal/config/config.go:157`](../../internal/config/config.go#L157)

```go
var missing []string

databaseURL := lookupRequired("DATABASE_URL", &missing)
jwtSecret := lookupRequired("JWT_SECRET", &missing)

if len(missing) > 0 {
	return nil, &MissingVarsError{Names: missing}
}
```

The accumulator is the whole point. Returning on the first failure would make a
fresh clone a fix-one-rerun-fix-one loop. Instead:

```
configuration error: missing required environment variable(s): DATABASE_URL, JWT_SECRET
```

Everything wrong, in one run.

Note the ordering decision at [line 163](../../internal/config/config.go#L163):
required variables are reported **before** optional ones are validated. A boot
with no configuration at all should complain about the configuration, not
about `PORT`.

---

## 5. The `*[]string` pointer

**📍 Open:** [`internal/config/config.go:263`](../../internal/config/config.go#L263)

```go
func lookupRequired(name string, missing *[]string) string {
	value, ok := lookupNonEmpty(name)
	if !ok {
		*missing = append(*missing, name)
		return ""
	}
	return value
}
```

**Why a pointer, when slices already feel like references?**

A Go slice is a small value — a pointer to a backing array, a length and a
capacity — and it is passed **by copy**. `append` may reallocate the array, and
will certainly change the length. Both changes land in the *local copy*.

Pass `missing []string` by value and the caller's slice never grows. The
function would appear to work and silently do nothing. This catches nearly
everyone learning Go.

**Related:** [`lookupNonEmpty`](../../internal/config/config.go#L292) collapses
"unset" and "set but blank" into one case. `os.LookupEnv` distinguishes them,
but that distinction is never useful for configuration — an empty secret is as
broken as a missing one. It also trims whitespace, so a stray newline from a
`.env` file never becomes part of a secret.

---

## 6. Named error types and `errors.As`

**📍 Open:** [`internal/config/config.go:109`](../../internal/config/config.go#L109)

```go
type MissingVarsError struct {
	Names []string
}

func (e *MissingVarsError) Error() string { ... }
```

**How interfaces work in Go, in one example.** Any type with an
`Error() string` method satisfies the built-in `error` interface. No
`implements` keyword, no registration — satisfaction is *structural*. That is
Go's interface model in miniature.

**Why a type instead of `fmt.Errorf`.** The type carries `Names []string` as
structured data, so the caller can recognise this specific failure:

**📍 Open:** [`cmd/api/main.go:27`](../../cmd/api/main.go#L27)

```go
var missing *config.MissingVarsError
if errors.As(err, &missing) {
	// print setup hints
}
```

With a formatted string the only way to detect the case is
`strings.Contains` on the message — which breaks the moment you reword it.

[`InvalidVarError`](../../internal/config/config.go#L122) also has
[`Unwrap()`](../../internal/config/config.go#L133), which lets `errors.Is` and
`errors.As` see through to the wrapped `strconv` failure. Wrapping preserves the
chain; `fmt.Errorf("...: %v", err)` flattens it to text. Use `%w`, not `%v`,
when you want the chain intact.

**Also note:** `JWT_SECRET` has no default, deliberately. A fallback like
`"dev-secret"` works on every machine, so it survives review, reaches
production, and signs real tokens. Required-with-no-default makes that
impossible.

---

## 7. `/healthz` vs `/readyz`

**📍 Open:** [`internal/api/health.go:21`](../../internal/api/health.go#L21)

The distinction people get wrong, and it is worth internalising **before**
[module 2](02-database.md#5-healthz-versus-readyz) wires `/readyz` to the
database.

| | Question | Failure means |
|---|---|---|
| `/healthz` | "Is this process alive?" | **Restart me** |
| `/readyz` | "Should I get traffic now?" | **Take me out of the load balancer, leave me running** |

**Why `/healthz` must never touch the database:** if it did, a brief database
blip would make Kubernetes kill *every* API pod at once — turning a recoverable
dependency hiccup into a full outage, exactly when the database can least
afford a thundering herd of reconnecting restarts.

Database checks belong in `/readyz` only. Both return the same shape today, so
the response contract does not change when
[module 2](02-database.md#5-healthz-versus-readyz) adds the real check — at
which point `/readyz` gains a 503 branch and `/healthz` stays exactly as it is.

---

## 8. Gin modes and trusted proxies

**📍 Open:** [`cmd/api/main.go:73`](../../cmd/api/main.go#L73)

```go
if cfg.IsProduction() {
	gin.SetMode(gin.ReleaseMode)
}

router, err := api.NewRouter(api.Deps{ /* ... */ })
```

**Order matters.** The engine reads the mode as it installs middleware, so
setting it afterwards leaves debug behaviour in production. The engine itself
is built in [`internal/api/router.go:47`](../../internal/api/router.go#L47),
where the routes moved in module 3.

`gin.Default()` is `gin.New()` plus two middlewares: a request logger, and a
recovery handler that converts a panic in any handler into a 500 instead of
killing the process. This project started with `gin.Default()` and now spells
the two out, because module 3 swaps the recovery handler for one that answers
in the API's error format — see
[module 3, section 24](03-auth.md#24-errors-that-never-reach-a-handler).

**📍 Open:** [`internal/api/router.go:58`](../../internal/api/router.go#L58)

```go
if err := router.SetTrustedProxies(nil); err != nil {
	log.Fatalf("failed to configure trusted proxies: %v", err)
}
```

By default Gin trusts `0.0.0.0/0` and `::/0`, meaning it believes any
`X-Forwarded-For` header a client sends and reports it from `c.ClientIP()`.
Passing `nil` trusts nobody, so `ClientIP()` returns the real TCP peer. Harmless
today, but it matters the moment rate limiting or audit logging reads client IPs.

**Why handle the error** when `nil` can never fail? Because the moment you
replace `nil` with a real CIDR the check becomes load-bearing — and `go vet`
flags discarded errors anyway.

---

## 9. The multi-stage Dockerfile

**📍 Open:** [`Dockerfile:4`](../../Dockerfile#L4) and [`Dockerfile:43`](../../Dockerfile#L43)

Two `FROM` lines means two images. The first holds the Go toolchain and is
**discarded**; the second starts empty and receives only what
[`COPY --from=builder`](../../Dockerfile#L44) names.

Measured, not estimated:

| Build | Filesystem | Compressed |
|---|---|---|
| Single-stage (`FROM golang:alpine`, build in place) | **679.5 MB** | 182.5 MB |
| Multi-stage (this one) | **22.4 MB** | 9.2 MB |

These are the module 0 figures. The image has grown since, as the binary
gained dependencies and a second one was added; the current size is in the
verification table of [`03-auth.md`](03-auth.md#verified).

A **30× reduction** — and that is against a single-stage *alpine* build. With
`FROM golang:1.27` (Debian) it would be ~1.2GB.

The 657MB difference is the compiler, the linker, the Go standard library in
source form, the build cache, the module cache, `git`, and your source tree. All
needed to *produce* the binary; none needed to *run* it. A single-stage image
ships your source code to production.

**A measurement gotcha:** `docker images` reports **33MB** for this image.
That is Docker 29's containerd store summing both the compressed blobs (9.2MB)
and the unpacked snapshot (23.9MB). For the real number:

```bash
docker run --rm --entrypoint du hiredesk-api:latest -sh /
```

---

## 10. `CGO_ENABLED=0` and static linking

**📍 Open:** [`Dockerfile:20`](../../Dockerfile#L20)

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/hiredesk-api ./cmd/api
```

**What cgo is.** Go can call C, and a few stdlib packages do when available:
`net` prefers the system resolver (`getaddrinfo` from libc), `os/user` reads
`/etc/passwd` via libc.

**What `CGO_ENABLED=0` does.** Forces the pure-Go implementations, producing a
binary with **zero dynamic dependencies** — no libc, no dynamic loader. Verify:

```bash
docker run --rm --entrypoint sh hiredesk-api:latest -c 'ldd /usr/local/bin/hiredesk-api'
```

```
/lib/ld-musl-aarch64.so.1: ... Not a valid dynamic program
```

That error *is* the proof: `ldd` has nothing dynamic to inspect.

**Why it matters here — the classic Docker-Go trap.** `golang:alpine` and
`alpine` use **musl**. `golang:1.27` (Debian) uses **glibc**. Build with cgo on
Debian, copy into alpine, and it dies with:

```
exec /usr/local/bin/hiredesk-api: no such file or directory
```

Maddening, because the file obviously exists — it is the *dynamic loader* that
is missing. `CGO_ENABLED=0` makes the binary indifferent to its base image.

**The trade-off:** pure-Go DNS ignores some `/etc/nsswitch.conf` setups and NSS
plugins such as LDAP hostname lookup. Irrelevant in a container, which is why
this is near-universal for Go services — but it is a real difference.

The other flags: `-trimpath` strips local paths like `/Users/you/...` from the
binary; `-ldflags="-s -w"` drops the symbol table and DWARF debug info, cutting
~25–30%. Stack traces keep function names; only debugger support is lost.

---

## 11. Docker layer cache ordering

**📍 Open:** [`Dockerfile:12`](../../Dockerfile#L12)

```dockerfile
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build ...
```

Docker caches each instruction and invalidates a layer when its inputs change —
then **every layer below it** too. So instruction order decides how much work an
edit costs.

The anti-pattern, for contrast:

```dockerfile
COPY . .          # depends on every file
RUN go mod download   # re-runs on every source edit
```

`COPY . .` depends on everything, so changing one character in a handler
re-downloads Gin and its 20 transitive dependencies. Every build. That step
takes **9 seconds** here, and the ordering above saves it on nearly every
rebuild.

**The principle:** order instructions from least to most frequently changing.
It is also why [`.dockerignore`](../../.dockerignore) excludes `README.md` and
`docs/` — otherwise editing a note would invalidate `COPY . .` and force a
recompile for nothing.

---

## 12. What non-root actually buys you

**📍 Open:** [`Dockerfile:50`](../../Dockerfile#L50)

```dockerfile
RUN adduser -D -H -s /sbin/nologin -u 10001 appuser
...
USER appuser
```

Verify:

```bash
docker run --rm --entrypoint id hiredesk-api:latest
```

```
uid=10001(appuser) gid=10001(appuser)
```

**Honest framing: this is not a wall, it is a second lock.** Container isolation
comes from namespaces and cgroups, which apply to root too. The benefit is
specific:

**1. It breaks the exploit chain.** Container escapes are multi-step: code
execution → escalate → escape to host. Many escape techniques *require* root
inside the container — writing to `/proc/sys`, abusing `CAP_DAC_OVERRIDE`,
mounting, exploiting a runc bug. As uid 10001 with no capabilities, most of
those doors are shut.

**2. Root in the container is root on the host.** Without user-namespace
remapping (off by default), uid 0 inside *is* uid 0 outside. Mount a host path
and a root process writes to it as root. Yours writes as 10001.

**3. The filesystem is effectively read-only.** `appuser` owns only its own
binary. It cannot install packages or drop a persistent backdoor. An attacker
gets a process, not a foothold that survives restart.

**4. Defence in depth against your own bugs.** A future path-traversal flaw
leaks world-readable files as `appuser`, rather than anything at all as root.

Two deliberate details: `USER` comes **after** the `COPY`s, because an
unprivileged user cannot write to `/usr/local/bin`; and `-u 10001` is fixed and
high, so volume ownership stays stable across rebuilds and never collides with
a real host user.

---

## 13. `depends_on: condition: service_healthy`

**📍 Open:** [`docker-compose.yml:42`](../../docker-compose.yml#L42)

```yaml
depends_on:
  postgres:
    condition: service_healthy
```

The most important three lines in the file, because the bug it prevents is
**intermittent**.

**What plain `depends_on: [postgres]` guarantees:** only that Docker *starts*
the postgres container first. "Started" means the process exists and
`docker-entrypoint.sh` has begun.

**What it does not guarantee:** that Postgres accepts connections. On first
boot that entrypoint has real work:

1. `initdb` creates the cluster on disk
2. the server starts in single-user mode
3. the `POSTGRES_USER` role is created
4. the `POSTGRES_DB` database is created
5. `/docker-entrypoint-initdb.d` scripts run
6. the server **restarts** to listen on the network

That takes 1–15 seconds. Plain `depends_on` is satisfied at the start of step 1.

**So what breaks:** your API starts, `config.Load()` succeeds (it is only
strings), then module 1's `Ping` hits a port with nothing listening:

```
dial tcp 172.20.0.2:5432: connect: connection refused
```

`log.Fatalf` kills the process; `restart: unless-stopped` restarts it; it
probably succeeds on the second or third try. **That is why this is so
pernicious** — it works on your fast laptop with a warm volume and fails in CI,
where the volume is always empty and `initdb` always runs. Classic "works on my
machine".

**With `service_healthy`,** Compose blocks the API from starting until the
healthcheck passes. You can see it in the output:

```
Container hiredesk-postgres  Waiting
Container hiredesk-postgres  Healthy
Container hiredesk-api       Starting
```

**📍 Open:** [`docker-compose.yml:26`](../../docker-compose.yml#L26) — the
healthcheck, where `-d` is load-bearing:

```yaml
test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-hiredesk} -d ${POSTGRES_DB:-hiredesk}"]
```

Without `-d`, `pg_isready` only asks "is the server up?" — and the server comes
up at step 2, before the database exists at step 4. So a healthcheck without
`-d` can pass while `DATABASE_URL` points at a nonexistent database,
reintroducing the race. `CMD-SHELL` rather than `CMD` because `${...}` needs a
shell.

**The caveat:** this protects *startup only*. If Postgres dies at 3am nothing
restarts your API — nor should it. That is what pool retries and a real
`/readyz` are for.

---

## 14. Named volumes

**📍 Open:** [`docker-compose.yml:24`](../../docker-compose.yml#L24) and [`:59`](../../docker-compose.yml#L59)

```yaml
volumes:
  - postgres_data:/var/lib/postgresql/data
```

**The mechanism.** A container's filesystem is a thin writable layer on the
image's read-only layers. That layer belongs to the container, so `docker rm`
deletes it. A volume is separate storage with its own lifecycle, untouched by
container removal.

**Without one:**

| Command | With named volume | Without |
|---|---|---|
| `docker compose restart` | kept | kept |
| `make down` then `make up` | **kept** | **gone** |
| `up --build` after a Dockerfile edit | **kept** | **gone** |

Row two is the killer, because `make down` reads like "stop" — but
`compose down` *removes* containers, and removing a container destroys its
writable layer. Every migration, every test candidate, gone on each restart.

Verified rather than asserted: insert a row, `make down`, confirm the volume
survives, `make up`, query it back → `survived the restart`.

**Why named rather than a bind mount** like `./pgdata:/var/lib/postgresql/data`:
Postgres is very sensitive to filesystem semantics, and on Docker Desktop for
macOS a bind mount crosses a virtualisation boundary — much slower `fsync`, plus
permission mismatches because the container's `postgres` user is not your macOS
UID. Named volumes live on a native filesystem inside the Linux VM.

**To wipe deliberately:** `make reset`
([`Makefile:161`](../../Makefile#L161)). You will need it after changing
`POSTGRES_USER`, `POSTGRES_PASSWORD` or `POSTGRES_DB`, since those are read
**only** while initialising an empty data directory — editing them in `.env`
appears to do nothing until the volume is destroyed.

---

## 15. Container-to-container DNS

**📍 Open:** [`docker-compose.yml:52`](../../docker-compose.yml#L52)

```
postgres://hiredesk:...@postgres:5432/hiredesk?sslmode=disable
```

**The host is `postgres`** — the service name. Compose puts both containers on a
shared network with an embedded DNS server resolving service names to container
IPs:

```
Name:    postgres
Address: 172.20.0.2
```

Inside the API container, `localhost` is the *API container itself*, so
`localhost:5432` is connection-refused. The single most common Docker
networking mistake.

**The port is `5432`, not `POSTGRES_PORT`.** Published ports exist only for host
access; container-to-container traffic goes direct and never touches the
mapping. Set `POSTGRES_PORT=5433` because Homebrew Postgres owns 5432 and the
API's URL still says `postgres:5432` — only your host-side `psql` changes.

---

## 16. Makefile mechanics

**📍 Open:** [`Makefile`](../../Makefile)

Three things that are unlike anything else in the project.

**1. Recipe lines need a literal TAB.** Spaces produce
`missing separator`. If your editor converts tabs, set
`"files.associations": {"Makefile": "makefile"}` in VS Code.

Related trap: a **TAB-indented `#` comment is handed to the shell** as a shell
comment, and Make echoes it before running it — so your explanation appears in
the terminal as if it were a command. Comments must sit at column 0. See the
note above [`test:`](../../Makefile#L73).

**2. `.PHONY` is not decoration.** Without it, `make test` would do nothing the
moment a directory named `test` existed, because Make would consider the target
up to date.

**3. Targets are files, with dependencies.**
[`Makefile:153`](../../Makefile#L153):

```make
up: .env

.env:
	@sed 's|^JWT_SECRET=.*|JWT_SECRET='"$$(openssl rand -hex 32)"'|' .env.example > .env
```

`.env` is a **real file target**, not phony, so Make runs it only when the file
is absent. `make up` on a fresh clone therefore generates `.env` with a real
secret, then starts the stack — one command, no instructions to read. This is
Make's actual purpose, rather than being a command runner.

`$$` escapes to a single `$` for the shell, because Make consumes the first one.

---

## 17. What CI is actually for

**📍 Open:** [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)

"It runs your tests automatically" undersells it. Three things are happening.

**1. It defines "broken".** Without CI, "does the build work?" is answered by
whoever last tried, on their machine, with their Go version and their
uncommitted files. CI replaces that with one reproducible answer on a clean
checkout.

The [`go mod tidy` gate](../../.github/workflows/ci.yml#L36) is the pure case:
committing an import without its `go.mod` entry works perfectly for you —
your module cache already has it — and breaks every clean build. CI catches the
class of bug that is *invisible locally by construction*.

**2. It moves the cost of a bug earlier.** Found by CI: minutes. In review: a
round trip. In staging: a redeploy. In production: an incident.

**3. It makes a branch protectable.** Once CI reports a status, GitHub can
require it before merge. `main` stops being "whatever got pushed" and becomes
"everything that passed". You cannot enforce that by asking people nicely.

**What CI is not:** proof of correctness. A green pipeline on a project with no
tests proves it compiles, vets and lints. Worth having, not the same as working.

**One design decision:** lint is [its own job](../../.github/workflows/ci.yml#L66),
not a step in `build-and-test`, so the two run in **parallel**. Sequentially, a
lint failure would abort before the tests ran, and you would fix a typo only to
find a real test failure on the next push.

---

## 18. The race detector

**📍 Open:** [`.github/workflows/ci.yml:50`](../../.github/workflows/ci.yml#L50)

```yaml
run: go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
```

**What a data race is.** Two goroutines access the same memory, at least one
writes, and nothing orders them. The result is not a wrong value — it is
undefined behaviour: a torn 64-bit read, a corrupted map, a slice header
pointing at reallocated memory.

**Why it matters for HireDesk specifically.** Gin runs **every request in its
own goroutine**. So the moment there is shared state — a cache, a rate-limit
counter — every handler is concurrent code whether you thought about it or not:

```go
var cache = map[string]Candidate{}   // no mutex — this is the bug

func getCandidate(c *gin.Context) {
	if cand, ok := cache[c.Param("id")]; ok {  // concurrent read
		c.JSON(200, cand); return
	}
	cand := loadFromDB(c.Param("id"))
	cache[c.Param("id")] = cand                // concurrent WRITE
	c.JSON(200, cand)
}
```

Single-user testing never trips it. Under real traffic:

```
fatal error: concurrent map writes
```

Not a panic — a **fatal error**. `recover()` cannot catch it, so Gin's recovery
middleware is powerless and the whole process dies, dropping every in-flight
request.

**What `-race` does.** It is ThreadSanitizer, a **runtime** tool, not static
analysis. The compiler instruments every memory access; the runtime keeps shadow
memory plus a vector clock per goroutine, tracking happens-before. Two
unordered accesses to one address with at least one write → both stack traces
printed.

**The crucial limitation:** it only finds races that *actually occur during that
run*. No proof of absence.

**Which is exactly why CI, not just local:**

- **Races are nondeterministic.** Manifestation depends on scheduling, core
  count and timing. Your M-series Mac and a 2-core runner interleave
  differently, so CI samples a genuinely different schedule.
- **Repetition is the strategy.** A race with a 1-in-50 chance per run is
  near-invisible to you and near-certain across a few hundred CI runs. CI
  accumulates attempts for free, forever.
- **You can afford it there.** `-race` is 2–20× slower and uses 5–10× the
  memory — too slow for a tight local loop, fine on a runner.

**Two flags:** `-count=1` defeats Go's test result cache so CI genuinely re-runs
rather than replaying a pass. `-covermode=atomic` is *required* with `-race`,
because the default coverage counter is a non-atomic increment — the
instrumentation would itself be a data race.

---

## 19. The linters

**📍 Open:** [`.golangci.yml:18`](../../.golangci.yml#L18)

The config uses the **v2 schema**, which is not compatible with the v1 examples
most tutorials still show: `version: "2"` is mandatory, settings moved under
`linters.settings`, formatters became their own section, and `gosimple` no
longer exists — it was merged into `staticcheck`. Validate any change with
`golangci-lint config verify`.

[`default: none`](../../.golangci.yml#L15) means nothing is enabled implicitly,
so the file is the complete answer to "what runs in CI?".

| Linter | Catches |
|---|---|
| **errcheck** | Ignored error returns — see the worked example below |
| **govet** | stdlib analysers: `printf` arg mismatches, bad struct tags, lost `CancelFunc`, impossible nil comparisons, copied mutexes |
| **ineffassign** | A value assigned then never read before being overwritten |
| **staticcheck** | Hundreds of correctness rules: deprecated APIs, impossible type assertions, `time.Sleep` with an int |
| **unused** | Dead code — unexported functions, consts, types, fields |
| **bodyclose** | Unclosed `http.Response.Body` → leaked TCP connection per call |
| **noctx** | HTTP request with no `context.Context` → no timeout |
| **rowserrcheck** | Unchecked `sql.Rows.Err()` → a mid-iteration failure looks like a short result set. You would show 3 of 50 candidates with no error |
| **sqlclosecheck** | Unclosed `sql.Rows`/`sql.Stmt` → exhausted pool, then every request blocks |
| **errorlint** | `err == ErrFoo` instead of `errors.Is` — breaks on wrapped errors |
| **nilerr** | `if err != nil { return nil }` — checks an error then swallows it |
| **misspell** | Typos in comments and strings |
| **revive** | Style and naming; the `exported` rule keeps doc comments present |

One deliberately **disabled** at [line 45](../../.golangci.yml#L45):
`fieldalignment` reorders struct fields by size to save padding. Technically
right, but it would order `Config` by byte size rather than by meaning, to save
a few bytes on a struct allocated once.

### The errcheck example worth memorising

This is the shape your candidate store will take, and the worst kind of bug —
the function reports success while losing data:

```go
func (s *CandidateStore) Create(ctx context.Context, c Candidate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, "INSERT INTO candidates (name, email) VALUES ($1, $2)", c.Name, c.Email)
	if err != nil {
		return err
	}

	tx.Commit() // ← errcheck flags this
	return nil
}
```

`tx.Commit()` returns an `error`, and committing is exactly where transactions
fail — a serialization conflict under concurrent writes, disk full, a dropped
connection, a deadlock the server resolved by killing your transaction. When it
fails the insert is rolled back and the candidate is **not saved**.

But the function returns `nil`. The handler returns `201 Created`. The recruiter
sees a success toast. The candidate is gone — no log line, no alert. You find
out weeks later when someone asks where an application went.

The fix:

```go
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit candidate: %w", err)
	}
	return nil
```

**Why this is the highest-value linter here:** Go has no exceptions. An ignored
error is not a warning you will see later — it is a code path that does the
wrong thing and says nothing.

Two non-default options widen it at
[`.golangci.yml:36`](../../.golangci.yml#L36): `check-type-assertions` flags
`v := x.(string)` (which panics where `v, ok := x.(string)` does not), and
`check-blank` flags `_ = doThing()`, so silencing an error has to be deliberate.

---

## Verified in this module

| Check | Result |
|---|---|
| `go build ./...` / `go vet ./...` | clean |
| `gofmt -l .` | no files |
| `golangci-lint run ./...` | **0 issues**, 13 linters |
| `golangci-lint config verify` | valid v2 schema |
| `actionlint` | 0 problems |
| Image filesystem | 22.4 MB (vs 679.5 MB single-stage) |
| Container identity | `uid=10001(appuser)` |
| Binary linkage | static — `ldd` reports "not a valid dynamic program" |
| Startup ordering | `postgres Waiting → Healthy → api Starting` |
| Volume persistence | row survived `make down` + `make up` |
| Both endpoints | `200` · `{"status":"ok"}` |
| Config failure path | exits 1, lists **both** missing variables |

## Next

**Module 1** — [`01-schema.md`](01-schema.md): the eleven-table schema, written
as six golang-migrate pairs.

**Module 2** — [`02-database.md`](02-database.md): the pgx connection pool and
the `/readyz` check this section anticipated.

Still open: `internal/config` has no tests, which is why coverage reads 0.0%.
It is the most testable code in the project — more so now that it parses and
validates eight further variables.
