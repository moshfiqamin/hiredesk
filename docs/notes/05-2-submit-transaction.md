# Module 5, part 2 — Submitting an application atomically

This note covers writing a submission to the database: what a transaction
is in terms of what PostgreSQL does (section 1), the `defer tx.Rollback`
pattern (section 2), what the tables would hold without one (section 3),
the order of work inside `SubmitApplication` (section 4), why the event row
is inside the transaction (section 5), inserting many answers in one
statement and when `CopyFrom` would be better (section 6), and the test that
forces the last insert to fail (section 7).

The rules checked before the write are [part 1](05-1-application-validation.md).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-a-transaction-is-in-postgresql) | What a transaction is, in PostgreSQL | [`applications.go:774`](../../internal/store/applications.go#L774) |
| [2](#2-defer-txrollback) | `defer tx.Rollback` | [`applications.go:780`](../../internal/store/applications.go#L780) |
| [3](#3-what-the-tables-would-hold-without-a-transaction) | What the tables would hold without a transaction | [`applications.go:814`](../../internal/store/applications.go#L814) |
| [4](#4-the-order-of-work) | The order of work | [`applications.go:771`](../../internal/store/applications.go#L771) |
| [5](#5-why-the-event-is-inside-the-transaction) | Why the event is inside the transaction | [`applications.go:845`](../../internal/store/applications.go#L845) |
| [6](#6-many-answers-one-statement) | Many answers, one statement | [`applications.go:909`](../../internal/store/applications.go#L909) |
| [7](#7-testsubmitrollback) | `TestSubmitRollback` | [`submit_rollback_test.go:90`](../../test/integration/submit_rollback_test.go#L90) |

---

## 1. What a transaction is, in PostgreSQL

**📍 Open:** [`internal/store/applications.go:774`](../../internal/store/applications.go#L774)

`s.db.Begin(ctx)` takes one connection out of the pool and sends `BEGIN` on
it. Every statement sent through the returned `tx` goes down that same
connection. That is the first thing to understand: **a transaction belongs
to one connection.** A query sent through `s.db` instead of `tx` in the
middle would borrow a different connection and run outside the transaction
entirely, which is why `listQuestions` takes the small `queryer` interface
and is handed `tx`.

What PostgreSQL does from there:

1. **It assigns the transaction an ID** (an `xid`) when it first writes.
2. **Every row it inserts is written to the table straight away**, stamped
   with that `xid` as the row's creator (`xmin`). The application row, the
   answer rows and the event row are physically on disk pages, and in the
   write-ahead log, before `COMMIT` is sent.
3. **No other session can see those rows.** Each row version carries the ID
   of the transaction that made it, and every reader checks that ID against
   its snapshot: rows made by a transaction that has not committed are
   skipped. This is MVCC, multi-version concurrency control. Readers never
   wait for writers; they simply do not see uncommitted versions.
4. **`COMMIT` writes one small record** to the write-ahead log saying "`xid`
   committed", waits for it to reach disk, and marks the `xid` committed in
   the commit log (`pg_xact`). From that instant every row stamped with it
   becomes visible, all at once. There is no moment where the application is
   visible and its answers are not.
5. **`ROLLBACK` marks the `xid` aborted** instead. The rows stay on the
   pages, but every reader now skips them forever, and `VACUUM` later reclaims
   the space. Nothing is "undone" row by row; one flag flips.

Two more facts that matter for the code:

- **An error inside a transaction poisons it.** After any failed statement,
  PostgreSQL refuses everything except `ROLLBACK` with *"current transaction
  is aborted, commands ignored until end of transaction block"*. The only way
  out is to end it.
- **Row locks taken inside a transaction last until it ends.** That is what
  makes the `FOR SHARE` in section 4 mean anything.

## 2. `defer tx.Rollback`

**📍 Open:** [`internal/store/applications.go:780`](../../internal/store/applications.go#L780)

```go
tx, err := s.db.Begin(ctx)
if err != nil { … }
defer tx.Rollback(ctx) //nolint:errcheck

// … every statement, each with an early return on error …

if err = tx.Commit(ctx); err != nil { … }
return app, nil
```

`defer` schedules the call to run when the function returns, by **any**
path: any of the nine `return nil, …` lines after it, the final
`return app, nil`, or a panic unwinding the stack. One line, written next to
`Begin`, covers every exit, including exits added later by someone who
never thought about the transaction.

**Why calling Rollback after a successful Commit is safe.** pgx's `Tx`
records whether it has ended. `Commit` sets that state; a later `Rollback`
sees it, sends nothing to the server, and returns `pgx.ErrTxClosed`. The
error is discarded, which is why the line carries `//nolint:errcheck` with
its reason: after a commit the error is always `ErrTxClosed`, and on an early
return the function is already reporting the error that caused it.

**What breaks without it.** Every early return would have to call
`tx.Rollback` itself; the first one forgotten leaves the connection inside
an open, possibly aborted, transaction. `pgxpool` will not hand such a
connection to the next request — it closes it instead — so the cost is a
destroyed connection and a fresh handshake, plus any row locks held until
the server notices. A forgotten rollback on a panic path is nearly
impossible to spot in review. The deferred call makes forgetting impossible.

**The rejected alternative** was a helper taking a closure,
`withTx(ctx, func(tx pgx.Tx) error)`, which commits if the closure returns
nil. It is tidier at scale, but every store method so far uses the explicit
form, and the explicit form shows the pattern this note is about.

## 3. What the tables would hold without a transaction

**📍 Open:** [`internal/store/applications.go:814`](../../internal/store/applications.go#L814)

Without `BEGIN`, PostgreSQL runs each statement in its own transaction and
commits it the moment it finishes ("autocommit"). Suppose the answers insert
failed — a dropped connection, a deploy restarting the process, a constraint
nobody expected:

| Table | Rows |
|---|---|
| `applications` | **1**, status `submitted`, committed |
| `application_answers` | **0** (or, with one insert per answer, however many ran before the failure) |
| `application_events` | **0** |

And then:

- **The candidate is stuck.** They see an error and press submit again. The
  `UNIQUE (job_post_id, candidate_id)` constraint from module 1 now refuses
  them with a 409 `already_applied`: they have "already applied", with an
  application that holds none of their answers. Nothing in the API lets them
  fix it.
- **The recruiter sees an application with an empty form**, and cannot tell
  a candidate who skipped every question from a write that broke.
- **The scorer in a later module** scores it against the rubric and gives it
  zero on every criterion, ranking a real candidate at the bottom.
- **The audit trail has no record** of the submission, so nothing explains
  where the row came from.

With the transaction, the same failure leaves all three tables exactly as
they were. The candidate's retry is a fresh first attempt.
`TestSubmitRollback` checks precisely this (section 7).

## 4. The order of work

**📍 Open:** [`internal/store/applications.go:771`](../../internal/store/applications.go#L771)

| Step | Statement | Why here |
|---|---|---|
| 1 | `SELECT status … FOR SHARE` | Rule 1, under a lock |
| 2 | `SELECT … FROM questions` | The form, read inside the same transaction |
| 3 | `domain.ValidateApplication` | Rules 2–6, in Go, no I/O |
| 4 | `INSERT INTO applications … RETURNING` | Needs the new `id` for steps 5 and 6 |
| 5 | `INSERT INTO application_answers … FROM unnest(…)` | Every answer, one statement |
| 6 | `INSERT INTO application_events` | Last: describes the finished write |
| 7 | `COMMIT` | |

**Why validation runs inside the transaction.** Checking first and writing
later has a gap: an admin could close the post between the check and the
insert, and an application would land on a closed post. `FOR SHARE` closes
the gap. `TransitionJobPost` takes `FOR UPDATE` on the same row, and the two
lock modes conflict, so closing the post waits until this transaction ends.
Two submissions both take `FOR SHARE`, which do **not** conflict with each
other, so candidates applying at the same time never queue behind one
another. `FOR UPDATE` here would have serialised every submission to a
popular post for no benefit.

The form needs no lock of its own: module 4.3 refuses every question edit
unless the post is a draft, and this post is open and cannot become a draft.

**Why not a pre-flight duplicate check.** Step 4 is allowed to fail on the
unique constraint, which becomes `domain.ErrAlreadyApplied` (409). [Part 3](05-3-duplicate-applications.md) walks through why.
A `SELECT` to check first would have the same gap as above, as module 1's
note explains for this exact constraint.

## 5. Why the event is inside the transaction

**📍 Open:** [`internal/store/applications.go:845`](../../internal/store/applications.go#L845)

`application_events` is the audit trail: append-only, and the record a
recruiter or a candidate's data request is answered from. It must say that
a submission happened **if and only if** one did.

Written *after* the commit, in a second statement:

- **The insert can fail**, or the process can die between the commit and the
  event — a deploy, an OOM kill, a panic. The application then exists with
  no `application.submitted` event. Anything built on the event stream (a
  notification email, a "new applications" counter, an analytics pipeline)
  silently misses it, and the history of that application starts with
  whatever happens to it next.
- **There is nothing to retry from.** The request has already returned, or
  is about to return, 201.

Written *before* the application's commit but outside its transaction, the
reverse happens: an event for an application that was then rolled back, a
history recording something that never occurred.

Inside, both outcomes are impossible. They commit together or neither does.
It costs one extra statement on a connection that is already open, and the
event row is written in the same `COMMIT` flush as everything else.

The same reasoning put the job post's status change and its event in one
transaction in module 4.2. If an event ever needs to leave the database —
to a queue, say — this is still where it is written; a separate worker then
reads committed events and publishes them (the *transactional outbox*
pattern), rather than the request trying to do two writes to two systems
and hoping both succeed.

`actor_id` is `app.CandidateID`, not the input's: the value the database
returned, in its canonical lowercase form, so the event names the user
exactly as the `users` table does.

## 6. Many answers, one statement

**📍 Open:** [`internal/store/applications.go:909`](../../internal/store/applications.go#L909)

The naive loop sends one `INSERT` per answer: ten answers, ten round trips
to the database, each waiting for the last. Inside a transaction that is
correct but slow, and the time spent holds the `FOR SHARE` lock.

### What was built: `unnest`

**📍 Open:** [`internal/store/applications.go:922`](../../internal/store/applications.go#L922)

```sql
INSERT INTO application_answers (application_id, question_id, answer_text)
SELECT $1::text::uuid, t.question_id::uuid, t.answer_text
FROM unnest($2::text[], $3::text[]) AS t(question_id, answer_text)
RETURNING id::text, application_id::text, question_id::text, answer_text, created_at
```

The Go side builds two parallel slices, `questionIDs` and `texts`, and pgx
sends each `[]string` as one PostgreSQL `text[]` parameter. `unnest` with
two arrays walks them in step, producing one row per pair; `INSERT … SELECT`
inserts every row the `SELECT` yields. Ten answers or a hundred: one
statement, one round trip, three parameters.

Why this form:

- **The SQL text never changes.** pgx prepares a statement the first time it
  sees its text and reuses the plan afterwards. A query whose text depends on
  the number of answers would be a new statement every time.
- **No parameter limit.** PostgreSQL allows 65,535 parameters per statement.
  A `VALUES` list uses three per row; arrays use three in total.
- **`RETURNING` works,** which is how the response gets each answer's `id`
  and `created_at` without a second query.

**`RETURNING` promises no order.** In practice it follows the input, but
nothing guarantees that, so the rows are put back in submission order with a
map from question ID to position
([`applications.go:938`](../../internal/store/applications.go#L938)). Each
question appears once — the validation and the unique constraint both
ensure it — so the map is unambiguous.

### The rejected alternative: a built-up `VALUES` list

```sql
INSERT INTO application_answers (application_id, question_id, answer_text)
VALUES ($1, $2, $3), ($1, $4, $5), ($1, $6, $7)
```

Also one statement and one round trip. It needs the SQL string assembled in
a loop with placeholder numbers computed by hand, the text differs for every
answer count (no plan reuse), and it hits the parameter cap at about 21,000
rows. It is what most ORMs generate. `unnest` does the same job with fixed
text.

### `pgx.CopyFrom`, and when it is the better choice

`CopyFrom` uses PostgreSQL's `COPY … FROM STDIN` protocol: instead of a
statement with parameters, the client streams rows in a compact format and
the server appends them with very little per-row work. It runs inside a
transaction like anything else:

```go
n, err := tx.CopyFrom(ctx,
    pgx.Identifier{"application_answers"},
    []string{"application_id", "question_id", "answer_text"},
    pgx.CopyFromSlice(len(answers), func(i int) ([]any, error) {
        return []any{appID, answers[i].QuestionID, answers[i].Answer}, nil
    }),
)
```

It is not used here because:

- **No `RETURNING`.** `COPY` reports a row count and nothing else. The
  response needs each answer's ID, so a second `SELECT` would be required,
  giving back the round trip it saved.
- **No `ON CONFLICT`, no expressions.** Values go in exactly as sent; casts
  such as `::uuid` must happen in Go first.
- **Its advantage only shows at volume.** For ten rows, the statement and
  `COPY` are both one round trip, and the difference is noise.

`CopyFrom` is the right choice when loading **thousands of rows or more**
and nothing needs to come back: importing a CSV of past candidates,
backfilling scores after a rubric change, seeding a load test. There it is
commonly an order of magnitude faster than even a multi-row `INSERT`.

## 7. `TestSubmitRollback`

**📍 Open:** [`test/integration/submit_rollback_test.go:90`](../../test/integration/submit_rollback_test.go#L90)

The test needs the **last** statement — the event insert — to fail after the
application and both answers have been written. Choices for causing that:

- **A test hook in the store** (a function variable it calls before the
  event, swapped in tests). Rejected: production code carrying a switch
  whose only purpose is to break it.
- **A fake `pgx.Tx`.** Rejected: it would test the fake's idea of a
  rollback, not PostgreSQL's.
- **A trigger in the test database.** Chosen. The code under test is the
  production code, unchanged, against a real server.

**📍 Open:** [`submit_rollback_test.go:98`](../../test/integration/submit_rollback_test.go#L98)

```sql
CREATE FUNCTION fail_application_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'forced failure: % answers already inserted',
        (SELECT count(*) FROM application_answers WHERE application_id = NEW.application_id);
END $$;

CREATE TRIGGER fail_application_event
    BEFORE INSERT ON application_events
    FOR EACH ROW EXECUTE FUNCTION fail_application_event();
```

A `BEFORE INSERT` trigger runs as part of the `INSERT` statement, so raising
in it fails that statement exactly as a constraint violation would.

**The message is the proof.** The trigger runs inside the submitting
transaction, so it can see that transaction's own uncommitted rows. It
counts the answers for the new application and puts the count in the error.
The test asserts the message is exactly
`forced failure: 2 answers already inserted`
([`submit_rollback_test.go:124`](../../test/integration/submit_rollback_test.go#L124)).
That shows the failure came after both earlier inserts had succeeded, not
from something earlier that would make the test pass for the wrong reason.

Then:

1. **Nothing survived.** Zero applications for the candidate, zero answers
   and zero events in the whole database. Answers and events are counted
   across every application, not through the candidate's, so an orphan whose
   parent vanished would still be caught.
2. **The retry works.** The trigger is dropped
   ([`submit_rollback_test.go:135`](../../test/integration/submit_rollback_test.go#L135))
   and the same submission is sent again. Had the failed attempt left its
   application behind, the unique constraint would refuse this one as a
   duplicate. It succeeds, and the counts are 1, 2 and 1.

`errors.As(err, &pgErr)` finds the trigger's `*pgconn.PgError` even though
the store wrapped it twice with `%w` on the way out — the same mechanism
`translate` uses to read SQLSTATE codes.

### The other outcomes

**📍 Open:** [`test/integration/submit_test.go:16`](../../test/integration/submit_test.go#L16)

`TestSubmitApplicationOutcomes` runs the refusals first, while the tables are
empty, so "nothing was written" is a count of zero: a closed post
(`*NotAcceptingError`), a draft and a missing post (`ErrNotFound`), and
invalid input (`*SubmissionError` with both answer problems and the CV
problem). Then a valid submission, sent in reverse form order, comes back
with its answers in the order sent, and its event has the candidate as
actor, the right type, and the post and answer count in its payload. A second
submission is `ErrAlreadyApplied` and leaves the counts at 1, 2 and 1.

---

## Verified

| Command | Result |
|---|---|
| `go test -race -count=1 -tags integration -run 'TestSubmit' -v ./test/...` | `TestSubmitRollback` PASS; `TestSubmitApplicationOutcomes` PASS, 6 subtests |
| `make test-integration` (store tests on the running stack, container tests on their own) | `internal/store` ok, `test/integration` ok |
| `go test -race -cover ./...` | all ok |
| `golangci-lint run ./...` | 0 issues |
| `go build ./...` | ok |

## Still open

- **No `GET` returns answers yet.** `GET /v1/applications/:id` still returns
  the application alone; answers come back only from the submit. Showing
  them to the reviewers who may see the application is the next read.
- **Withdrawing an application** is not possible, and the unique constraint
  means a candidate gets exactly one attempt per post.

## Next

Scoring a submitted application against the post's rubric.
