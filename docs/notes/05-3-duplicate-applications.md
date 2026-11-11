# Module 5, part 3 — Duplicate applications under concurrency

This note covers making a second application to the same post impossible,
and proving it while ten copies arrive at once: the race a check-then-insert
would have (section 1), why the unique constraint is the only reliable
guard (section 2), how the violation becomes a 409 (section 3), why a 500
would be a bug (section 4), and the test, with what goroutines,
`sync.WaitGroup` and `-race` are (sections 5–7).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-the-race-a-check-then-insert-would-have) | The race a check-then-insert would have | — |
| [2](#2-why-the-constraint-is-the-only-reliable-guard) | Why the constraint is the only reliable guard | [`000004_applications.up.sql:39`](../../migrations/000004_applications.up.sql#L39) |
| [3](#3-from-sqlstate-23505-to-a-409) | From SQLSTATE 23505 to a 409 | [`applications.go:571`](../../internal/store/applications.go#L571) |
| [4](#4-why-a-500-here-would-be-a-bug) | Why a 500 here would be a bug | [`errors.go:79`](../../internal/api/errors.go#L79) |
| [5](#5-goroutines) | Goroutines | [`concurrency_test.go:57`](../../test/integration/concurrency_test.go#L57) |
| [6](#6-syncwaitgroup-and-the-start-gate) | `sync.WaitGroup` and the start gate | [`concurrency_test.go:50`](../../test/integration/concurrency_test.go#L50) |
| [7](#7-what--race-detects-and-what-it-does-not) | What `-race` detects, and what it does not | [`concurrency_test.go:79`](../../test/integration/concurrency_test.go#L79) |

---

## 1. The race a check-then-insert would have

The tempting version of "one application per candidate per post":

```go
var exists bool
tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM applications
                  WHERE job_post_id = $1 AND candidate_id = $2)`, post, cand).Scan(&exists)
if exists {
    return ErrAlreadyApplied
}
tx.Exec(ctx, `INSERT INTO applications (job_post_id, candidate_id, cv_url) VALUES ($1, $2, $3)`, …)
```

It reads correctly. It fails as soon as two requests overlap, which happens
whenever a candidate double-clicks "Submit", a phone retries on a flaky
connection, or a frontend fires its request twice. Two requests, A and B,
each on its own connection, without the constraint:

| Time | Request A | Request B | Rows in the table, as A and B see them |
|---|---|---|---|
| t1 | `BEGIN` | | 0 |
| t2 | | `BEGIN` | 0 |
| t3 | `SELECT EXISTS …` → **false** | | 0 |
| t4 | | `SELECT EXISTS …` → **false** | 0 — A has inserted nothing yet |
| t5 | `if exists` → no, carry on | | |
| t6 | | `if exists` → no, carry on | |
| t7 | `INSERT …` → ok | | A sees 1; B still sees 0 |
| t8 | | `INSERT …` → ok | each sees its own row only |
| t9 | `COMMIT` | | 1 |
| t10 | | `COMMIT` | **2** |

Both checks were true when they ran. That is the problem: at t4, B's
`SELECT` answered a question about the past. Between B's check and B's
insert there is a gap, and A's insert happens inside it. Nothing connects
B's "no row exists" to B's "so I may insert": they are two separate
statements, and the world changes between them. This shape of bug is
called **time-of-check to time-of-use** (TOCTOU).

It does not need the precise interleaving above. Any overlap between A's
check and A's commit with B's check and B's commit gives the same result.
Even with A's `INSERT` before B's `SELECT` (swap t4 and t7), B still sees
nothing: A's row is uncommitted, and uncommitted rows are invisible to every
other transaction (part 2, section 1).

**Measured, not argued.** The test in section 5 was run once against a
database with the constraint dropped. All ten submissions succeeded, storing
10 applications, 20 answers and 10 events for one candidate on one post. The
code has no check-then-insert, and the result is the same as one would have,
because without the constraint nothing at all stops the second insert.

### Things that look like fixes and are not

- **`SELECT … FOR UPDATE` on the check.** `FOR UPDATE` locks rows that
  exist. The row we are worried about does not exist yet, so there is
  nothing to lock; both transactions lock zero rows and carry on. (A row
  that appears between two reads is a *phantom*, and row locks cannot see
  phantoms.)
- **Locking something else**, such as the job post's row with
  `FOR UPDATE`. It works, but it makes every candidate applying to a popular
  post queue behind each other, and it only protects code paths that
  remember to take that lock.
- **`SERIALIZABLE` isolation.** PostgreSQL would detect the conflict and
  abort one transaction with SQLSTATE `40001`, which the code then has to
  retry. Correct, but heavier, and every writer to the table must run at
  that level for it to hold.
- **A mutex in the Go process.** It serialises requests in one process. Run
  two replicas behind a load balancer and the two requests land in
  different processes with different mutexes.

## 2. Why the constraint is the only reliable guard

**📍 Open:** [`migrations/000004_applications.up.sql:39`](../../migrations/000004_applications.up.sql#L39)

```sql
CONSTRAINT applications_job_post_id_candidate_id_key
    UNIQUE (job_post_id, candidate_id)
```

A unique constraint is enforced by a unique index, and the check happens
**inside the index insertion itself**, not before it. There is no gap
between "is it there?" and "put it there"; they are one operation on the
index page, under the index's own short-lived lock.

What happens to A and B with the constraint in place:

| Time | Request A | Request B |
|---|---|---|
| t7 | `INSERT` → index entry added, uncommitted | |
| t8 | | `INSERT` → finds A's uncommitted entry for the same key. It cannot know yet whether A will commit, so it **waits** for A. |
| t9 | `COMMIT` | (still waiting) |
| t9+ | | A committed: the key is taken. B fails with **SQLSTATE 23505**, unique violation. |

Had A rolled back instead, B's insert would have gone ahead: the key would
be free. Either way exactly one row exists for the pair, whatever the
timing.

Why "only reliable" and not just "a good idea":

- **It cannot be bypassed.** Every write path — this handler, a future bulk
  import, a migration script, someone in `psql` — goes through the same
  index. A check in Go only guards the Go code that remembers to run it.
- **It holds across processes and machines.** The guard lives with the data,
  not in any one copy of the API.
- **It needs no extra locking or round trip.** The insert we had to do anyway
  is the check.
- **It is atomic by construction.** Application-level checks are only atomic
  if every caller gets the locking exactly right; the index gets it right
  once, for everyone.

The application code's job is not to *prevent* the duplicate, which it
cannot do reliably, but to *recognise* the database's refusal and report it
well.

## 3. From SQLSTATE 23505 to a 409

**📍 Open:** [`internal/store/applications.go:571`](../../internal/store/applications.go#L571)

```go
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation &&
    pgErr.ConstraintName == constraintOneApplicationPerPost {
    return nil, fmt.Errorf("%s: %w: %w", op, domain.ErrAlreadyApplied, err)
}
```

- **`errors.As`** walks the error chain looking for a `*pgconn.PgError`, the
  type pgx uses for anything the server reported, and copies it into
  `pgErr`. String-matching `err.Error()` for "duplicate key" would break the
  first time the message, the locale or the driver changed. SQLSTATE codes
  are part of the SQL standard and never change; `codeUniqueViolation` is
  `"23505"`.
- **The constraint name is checked too.** Any unique index on the table
  raises 23505. If a future migration adds one, a violation of it must not be
  reported to the candidate as "you have already applied". The name is a
  constant ([`applications.go:28`](../../internal/store/applications.go#L28))
  so a typo is one place to fix.
- **`%w` twice.** The domain sentinel goes in so the api layer can map it;
  the original `*pgconn.PgError` goes in so the log, if anything logs it,
  still has the constraint name and detail.
- **`ErrAlreadyApplied` itself wraps `ErrDuplicate`**
  ([`domain/errors.go:64`](../../internal/domain/errors.go#L64)), so code
  that only cares about "some unique key clashed" keeps working.

The api mapping row ([`api/errors.go:79`](../../internal/api/errors.go#L79))
sits **above** the generic `ErrDuplicate` row, because the table is read
top to bottom and the first match wins. The candidate gets:

```json
{"error":{"code":"already_applied",
  "message":"You have already applied to this job post. Each candidate can apply once per post; your existing application is under /v1/applications.",
  "details":{}}}
```

It says what happened, that it is a rule rather than a fault, and where to
find what they already submitted. A frontend can branch on the stable
`already_applied` code to show "View your application" instead of an error.

## 4. Why a 500 here would be a bug

**📍 Open:** [`internal/api/errors.go:79`](../../internal/api/errors.go#L79)

If the insert's error were returned as it came from pgx — no `errors.As`,
no `translate` — the `*pgconn.PgError` would reach `Respond`, match no
mapping, and become a 500. (`translate` has turned 23505 into a generic 409
since module 3; this part makes it specific.) A 500 is not merely an ugly
response here. It is wrong in ways that cause damage:

- **It states the wrong fact.** 5xx means "the server failed; the request may
  have been fine". 4xx means "the request cannot succeed as sent". Nothing
  failed here: the server worked exactly as designed and refused a request
  that breaks a rule.
- **Clients retry 5xx.** HTTP libraries, mobile SDKs, API gateways and load
  balancers commonly retry a 500 automatically, treating it as transient.
  Every retry hits the same constraint. A double-click becomes a small
  retry storm, and the candidate waits through the backoff to be told
  "something went wrong".
- **The candidate is told to do the wrong thing.** The 500 message is "try
  again". Trying again cannot work, ever. A 409 `already_applied` tells
  them they are done.
- **It pages someone.** Alerting and SLOs are built on the 5xx rate. A
  burst of candidates double-clicking would look like an outage, wake the
  on-call engineer, and spend the error budget on user behaviour.
- **It hides real failures.** Every one of these lands in the log as
  `internal error: …`, next to the genuine database outages, and trains
  people to skim past that line.
- **It leaks less, but explains nothing.** The fixed 500 body is safe, but
  a client cannot tell "you already applied" from "the database is down",
  so it cannot build the right behaviour for either.

The rule this project follows: an outcome the code anticipates is never a
500. A 500 is reserved for "we did not expect this", and then it means it.

## 5. Goroutines

**📍 Open:** [`test/integration/concurrency_test.go:57`](../../test/integration/concurrency_test.go#L57)

```go
for i := range attempts {
    go func() {
        …
        router.ServeHTTP(rec, req)
        statuses[i] = rec.Code
    }()
}
```

A **goroutine** is a function running concurrently with the rest of the
program. `go f()` starts `f` and returns immediately, without waiting for it.
Goroutines are not operating-system threads: the Go runtime multiplexes many
of them onto a few threads, and each starts with a stack of a few kilobytes,
so starting ten — or ten thousand — is cheap. The HTTP server already runs
every request in its own goroutine; this test does by hand what production
does for real.

Two details in that loop:

- **`i` is per iteration.** Since Go 1.22 each iteration of a `for` loop has
  its own `i`, so each goroutine sees its own index. Before 1.22 all ten
  closures shared one variable and usually all wrote to the last slot — the
  most famous Go bug there was.
- **Each goroutine writes only its own element** of `statuses` and `bodies`.
  Different elements of a slice are different memory locations, so ten
  goroutines writing ten different elements do not race, and no mutex is
  needed. Ten goroutines appending to one slice *would* race, because
  `append` reads and writes the shared length.

## 6. `sync.WaitGroup` and the start gate

**📍 Open:** [`test/integration/concurrency_test.go:50`](../../test/integration/concurrency_test.go#L50)

A `sync.WaitGroup` is a counter that one goroutine can wait on until it
reaches zero:

| Call | Does |
|---|---|
| `wg.Add(n)` | Add `n` to the counter. Call before starting the goroutines, so `Wait` cannot run before they are counted. |
| `wg.Done()` | Subtract one. Called by each goroutine as it finishes, here with `defer` so it runs on every path out. |
| `wg.Wait()` | Block until the counter is zero. |

The test uses two of them and a channel, because "launched in a loop" is not
"simultaneous": the first goroutine could finish its request before the
tenth is even created.

1. Each goroutine builds its request, then calls `ready.Done()` and blocks on
   `<-start`, a receive from a channel nobody sends on.
2. The test calls `ready.Wait()`, which returns once all ten are parked at
   the gate.
3. `close(start)`: receiving from a **closed** channel returns immediately,
   in every waiting goroutine at once. All ten requests leave together.
4. `done.Wait()` blocks until all ten have finished and called `Done`.

`Wait` does one more thing that matters: it **orders memory**. Everything a
goroutine wrote before its `Done` is guaranteed visible to the code after
`Wait` returns. Without that guarantee, reading `statuses` after the loop
could see stale values, and the race detector would flag it. With it, the
read is safe.

## 7. What `-race` detects, and what it does not

**📍 Open:** [`test/integration/concurrency_test.go:79`](../../test/integration/concurrency_test.go#L79)

`go test -race` builds the program with ThreadSanitizer instrumentation:
every memory read and write records which goroutine made it, and every
synchronisation point (a mutex, a channel operation, `WaitGroup.Wait`)
records a happens-before edge. A **data race** is two goroutines touching the
same memory, at least one writing, with no happens-before edge between them.
When it sees one, the test fails with both stack traces.

Here it checks:

- that the test's own bookkeeping is safe (the slices and the gate);
- that the **production code** under test — the router, middleware,
  handlers, the store, pgx's pool — shares no memory unsafely when ten
  requests run through it at once. A handler that stashed something in a
  package-level variable would be caught here.

What it does **not** detect, and this is the important distinction:

- **The database race in section 1.** That is a *race condition* in the
  logic: two transactions in another process interleaving badly. No Go
  memory is shared, so the race detector sees nothing. Only the assertion
  "exactly one row" catches it, which is why the test counts rows rather
  than trusting the status codes.
- **Races in code that did not run.** It instruments what executes. A race
  on a path the test never took is invisible.

It costs memory and roughly an order of magnitude in speed, so it runs in
tests and CI (`make test`, the CI workflow), not in production builds.

The test passed three runs in a row under `-race`. Run against a database
without the constraint, it failed with `created 10, refused 0` and
`stored 10 applications, 20 answers, 10 events`, so it really does tell a
protected table from an unprotected one.

---

## Verified

| Command | Result |
|---|---|
| `go test -race -count=3 -tags integration -run 'TestConcurrentDuplicateSubmission\|TestSubmit' ./test/integration/` | ok, three runs |
| The same test against a copy with the constraint dropped (throwaway test file, deleted afterwards) | FAIL: `created 10, refused 0`; `stored 10 applications, 20 answers, 10 events` |
| `go test -race ./internal/api/` | ok; a repeated submission maps to 409 `already_applied` |
| `golangci-lint run ./...` | 0 issues |

## Next

[Part 4](05-4-application-pipeline.md): moving an application through the
pipeline, and listing applications with filters and pages.
