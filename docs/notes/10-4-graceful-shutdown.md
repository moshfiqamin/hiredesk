# Module 10, part 4: Graceful shutdown

On SIGTERM or SIGINT: stop accepting connections, give in-flight requests up
to 10 seconds, close the pool, and exit 0. Past the deadline, log what was
abandoned and exit 1.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | What SIGTERM is, and who sends it | [`cmd/api/main.go:78`](../../cmd/api/main.go#L78) |
| 2 | A user mid-request, without graceful shutdown | — |
| 3 | `signal.NotifyContext`, not a raw channel | [`cmd/api/main.go:78`](../../cmd/api/main.go#L78) |
| 4 | `Shutdown` versus `Close` | [`cmd/api/main.go:202`](../../cmd/api/main.go#L202) |
| 5 | Why the pool closes after the server | [`cmd/api/main.go:90`](../../cmd/api/main.go#L90) |
| 6 | When time runs out | [`cmd/api/main.go:205`](../../cmd/api/main.go#L205) |
| 7 | Flushing the logger, honestly | [`cmd/api/main.go:69`](../../cmd/api/main.go#L69) |
| 8 | Proving it: the two-terminal procedure | — |
| 9 | Verification | — |

Most of the shape already existed from module 2 (`signal.NotifyContext`,
`Shutdown`, a deferred `pool.Close`). This part adds the in-flight report,
the forced close, the exit codes, structured logs for each phase, a second
signal that kills immediately, and the proof.

---

## 1. What SIGTERM is, and who sends it

**📍 Open:** [`cmd/api/main.go:78`](../../cmd/api/main.go#L78)

A signal is a small integer the operating system delivers to a process.
**SIGTERM** (15) means "please terminate": a process may catch it and clean
up first. **SIGINT** (2) is the same request from a terminal's Ctrl-C.
**SIGKILL** (9) cannot be caught. The kernel removes the process, with no
cleanup at all.

Every orchestrator stops a container the same way: SIGTERM, a grace period,
then SIGKILL.

| Who | Sends SIGTERM when | Grace before SIGKILL |
|---|---|---|
| `docker stop` / `docker compose down` | you stop the stack | 10 s by default; `stop_grace_period: 15s` here ([`docker-compose.yml:114`](../../docker-compose.yml#L114)) |
| Kubernetes | a rolling deploy, scale-down, node drain, eviction | `terminationGracePeriodSeconds`, 30 s by default; after any `preStop` hook |
| Fly.io | a deploy, a restart, a machine stop | `kill_timeout`, 5 s by default (`kill_signal` can change the signal) |

So **every deploy** sends this signal to every old instance. Graceful
shutdown is not an edge case. It runs on every release.

The signal only reaches the Go process because the Dockerfile uses exec-form
`ENTRYPOINT ["/usr/local/bin/hiredesk-api"]`
([`Dockerfile:66`](../../Dockerfile#L66)), making it PID 1. The shell form
(`ENTRYPOINT /usr/local/bin/hiredesk-api`) runs it under `/bin/sh -c`, which
does not forward SIGTERM. Docker then waits out the grace period and
SIGKILLs it: every stop takes 10 seconds and is never graceful.

`SHUTDOWN_TIMEOUT` (10 s) is deliberately shorter than the grace period
(15 s), so the server gives up and exits on its own terms, with logs, before
the kill arrives.

## 2. A user mid-request, without graceful shutdown

Say a candidate has pressed "Submit application" and the request is inside
its transaction when the deploy starts. If the process exits on the signal
(Go's default for an uncaught SIGTERM) or is SIGKILLed:

1. The process vanishes mid-handler. Its TCP connections are closed by the
   kernel, often with a reset.
2. **The client gets no HTTP response.** curl reports `Empty reply from
   server` or `Connection reset by peer`. A browser shows a network error,
   and the frontend cannot tell "failed" from "succeeded but the answer was
   lost".
3. **The database rolls back** the open transaction, because its
   connection died. Nothing half-written is left: the transaction design
   (module 5) protects the data. But the candidate does not know that.
4. **The candidate retries.** If the first attempt had committed just
   before the kill, the retry hits `409 already_applied` for an application
   they were told failed. If it had not, they must refill the form.
5. Multiply by every in-flight request on every instance, on every deploy.

With graceful shutdown, the same candidate's request finishes, they get
their `201`, and only *new* connections go elsewhere: to the new instances
the orchestrator has already started.

## 3. `signal.NotifyContext`, not a raw channel

**📍 Open:** [`cmd/api/main.go:78`](../../cmd/api/main.go#L78)

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

The older form is:

```go
sigs := make(chan os.Signal, 1)
signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
<-sigs
```

Why the context form:

- **It plugs into everything that already takes a context.** `store.New(ctx,
  cfg)` uses it, so a Ctrl-C during the startup ping aborts the ping instead
  of waiting out its timeout. Anything else given `ctx` inherits shutdown for
  free. A channel can be received by one goroutine, once. A context's
  `Done()` can be watched by any number of goroutines, and its `Err()` read
  forever after.
- **It composes.** A child of `ctx` with a timeout is cancelled by
  *whichever* comes first, the signal or the deadline. Doing that with
  channels means a `select` at every site.
- **`stop` restores default behaviour.** Called right after the first signal
  ([`:186`](../../cmd/api/main.go#L186)), it unregisters the handler, so a
  *second* Ctrl-C or SIGTERM kills the process at once. That gives an
  operator an escape hatch from a stuck drain. With a raw channel the second
  signal sits in a buffer, or is dropped, and nothing happens.
- **No buffer-size trap.** `signal.Notify` with an unbuffered channel can
  miss a signal (the runtime does not block to deliver one), a classic bug
  `go vet` warns about. `NotifyContext` handles that internally.

## 4. `Shutdown` versus `Close`

**📍 Open:** [`cmd/api/main.go:202`](../../cmd/api/main.go#L202)

| | `srv.Shutdown(ctx)` | `srv.Close()` |
|---|---|---|
| Listener | closed: new connections are refused | closed |
| Idle keep-alive connections | closed | closed |
| **Active connections** (a request in progress) | **left alone**; waits for each to go idle | **closed immediately**, mid-response |
| Returns | when all are idle (`nil`), or when `ctx` expires (`ctx.Err()`) | at once |
| `ListenAndServe` returns | `ErrServerClosed` | `ErrServerClosed` |

`Shutdown` is the graceful one. `Close` is the hard stop, used here only
after `Shutdown` has run out of time ([`:218`](../../cmd/api/main.go#L218)).
Neither stops a running handler goroutine. `Close` closes its connection,
which cancels the request's context, which aborts any pgx query waiting on
that context. That is how the abandoned handlers let go of their database
connections.

The context passed to `Shutdown` is a **fresh** `context.Background()` with
the timeout ([`:196`](../../cmd/api/main.go#L196)), deliberately not derived
from the signal context. That one is already cancelled, and a cancelled
parent would make `Shutdown` give up immediately, with no grace at all.

## 5. Why the pool closes after the server

**📍 Open:** [`cmd/api/main.go:90`](../../cmd/api/main.go#L90)

`pool.Close()` is deferred right after the pool is created, and `run` returns
only after `Shutdown` has. Go runs defers in last-in-first-out order when the
function returns, so the pool closes **after** the server has drained.

If it ran the other way round, pool first:

1. The in-flight request (the candidate's submit) is still running. Its next
   `pool.Begin` or `QueryRow` fails at once: the pool is closed.
2. The handler gets an error it has no reason to expect, maps it to a 500,
   and the candidate gets `internal_error`. That is worse than the
   connection reset in section 2, because it is a *confident* failure for a
   request that would have succeeded.
3. The server then "gracefully" waits for requests that have all failed.

The order follows the dependency: **stop the work that uses a resource
before closing the resource.** The server depends on the pool, so the pool
outlives it. `pgxpool.Close` itself also waits for acquired connections to be
released, so it is graceful too. It just must not start while requests still
need it.

## 6. When time runs out

**📍 Open:** [`cmd/api/main.go:205`](../../cmd/api/main.go#L205)

If `Shutdown` returns `context.DeadlineExceeded`:

1. **Say what is being abandoned.** `InFlight` (part 2) knows every request
   still running. Each one is logged with its `request_id`, method, path and
   how long it had been running, so whoever reads the logs after a deploy
   can find the users affected.
2. `srv.Close()` drops those connections.
3. `run` returns an error. `main` logs it and exits **1**. A non-zero exit
   tells the orchestrator, and anyone watching, that this stop was not
   clean.

The clean path exits **0**.

## 7. Flushing the logger, honestly

**📍 Open:** [`cmd/api/main.go:69`](../../cmd/api/main.go#L69)

The requirement was "flush the logger". There is nothing to flush.
`slog.NewJSONHandler(os.Stdout, …)` formats each record into a buffer and
writes it with **one** `Write` call to `os.Stdout`, an unbuffered
`*os.File`. By the time `slog.Info` returns, the bytes are in the kernel,
and they survive the exit. Calling `os.Stdout.Sync()` would `fsync` a pipe,
which fails with `EINVAL` and means nothing.

What *does* matter is order, and it is kept: `run`'s deferred `pool.Close` and
its log line finish, then the final line, then `os.Exit`. `os.Exit` must never
run earlier: it skips every pending `defer`, which is why `main` calls it
only after `run` has returned. If the handler were ever made buffered or
asynchronous (some logging libraries batch writes), its `Flush` would go on
the line before `os.Exit`, and the comment there says so.

## 8. Proving it: the two-terminal procedure

The stack must be up (`make up`) and seeded (`make seed`). The procedure
needs a request that takes several seconds without any test-only code. curl's
`-T -` uploads from stdin as a **chunked** stream, so a body can be sent in
two halves with a pause between. While the server waits for the second half,
the request is in flight.

**Terminal 1** (VS Code: Terminal → New Terminal), from the repository root:

```bash
docker compose logs -f api
```

Follows the API's logs, so you watch the shutdown happen.

**Terminal 2** (split the terminal with the ⊞ icon), from the repository root:

```bash
(printf '{"email":"admin@hiredesk.test",'; sleep 6; printf '"password":"admin-password"}') | curl -s -w '\nHTTP %{http_code} after %{time_total}s\n' -X POST -T - -H 'Content-Type: application/json' -H 'X-Request-ID: survives-shutdown' localhost:8080/v1/auth/login & sleep 2; docker compose kill -s SIGTERM api; wait
```

Starts a login whose body arrives over six seconds, sends SIGTERM two seconds
in, then waits for curl to finish.

**What to look for, in this order, in Terminal 1:**

1. `msg="shutdown signal received, draining" timeout=10s in_flight=1`. The
   signal arrived and one request is being waited for.
2. About four seconds later:
   `msg=request method=POST path=/v1/auth/login … status=200 duration_ms=6…
   request_id=survives-shutdown`. The in-flight request completed after the
   signal.
3. `msg="http server drained; every in-flight request finished"`
4. `msg="database pool closed"`. After the server, not before.
5. `msg="shutdown complete"`

**In Terminal 2:** the login's JSON (`access_token`, `refresh_token`, `user`),
then `HTTP 200 after 6.…s`. The request survived.

**Exit code**, in Terminal 2 afterwards:

```bash
docker inspect hiredesk-api --format '{{.State.ExitCode}}'
```

Prints the API container's exit code: `0` for a clean drain.

**New connections are refused.** During the drain, `curl localhost:8080/healthz`
from a third terminal fails to connect: the listener is closed.

**The failing case.** Bring the API back with `make up`, then repeat Terminal
2's command with `sleep 14` instead of `sleep 6`, so the request outlives the
10-second budget. Terminal 1 shows `level=WARN msg="request still running at
shutdown deadline" request_id=… method=POST path=/v1/auth/login
running_for=11.6s`, then `database pool closed`, then `level=ERROR msg="server
stopped with an error" error="graceful shutdown timed out after 10s: context
deadline exceeded"`. curl reports an empty reply, and the exit code is `1`.

Finish with `make up` to restart the API.

## 9. Verification

Run exactly as in section 8, against the container built from this commit:

| Run | Observed |
|---|---|
| `sleep 5` body, SIGTERM at 1.5 s | `draining … in_flight=1` at 04:10:43.451; login `status=200 duration_ms=5391.592` at 04:10:47.074; `http server drained` at .214; `database pool closed`; `shutdown complete`. curl `200 after 5.40s`. Exit code `0`, restart count `0` |
| `curl /healthz` during that drain | connection refused (curl exit 56) |
| `sleep 14` body, SIGTERM at 1.5 s | `draining … in_flight=1` at 04:11:02.761; exactly 10 s later `request still running at shutdown deadline request_id=too-slow-to-drain … running_for=11.629s`; `database pool closed`; `server stopped with an error … context deadline exceeded`. curl empty reply (exit 52). Exit code `1` |
