# Module 5, part 4 — The application pipeline

This note covers moving applications through the hiring pipeline and reading
them back: the status lifecycle (section 1), why each event stores both the
old and new status (section 2), refusing an offer with no vacancy left and
the snapshot bug the first version had (sections 3–4), filters that compose
with keyset pagination without concatenating values into SQL (section 5),
why a total count is expensive and the alternatives (section 6), the detail
endpoint (section 7), and the tests (section 8).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-the-lifecycle) | The lifecycle | [`application_status.go:21`](../../internal/domain/application_status.go#L21) |
| [2](#2-why-the-event-stores-from-and-to) | Why the event stores `from` and `to` | [`applications.go:307`](../../internal/store/applications.go#L307) |
| [3](#3-no-offer-without-a-vacancy) | No offer without a vacancy | [`applications.go:336`](../../internal/store/applications.go#L336) |
| [4](#4-the-snapshot-bug-and-how-the-test-found-it) | The snapshot bug, and how the test found it | [`pipeline_test.go:193`](../../test/integration/pipeline_test.go#L193) |
| [5](#5-filters-without-string-concatenating-values) | Filters without string-concatenating values | [`applications.go:177`](../../internal/store/applications.go#L177) |
| [6](#6-the-total-count-and-what-it-costs) | The total count, and what it costs | [`applications.go:106`](../../internal/store/applications.go#L106) |
| [7](#7-the-detail-endpoint) | The detail endpoint | [`application_handler.go:261`](../../internal/api/application_handler.go#L261) |
| [8](#8-the-tests) | The tests | [`pipeline_test.go:116`](../../test/integration/pipeline_test.go#L116) |

---

## 1. The lifecycle

**📍 Open:** [`internal/domain/application_status.go:21`](../../internal/domain/application_status.go#L21)

```go
var applicationTransitions = map[string][]string{
    StatusSubmitted:   {StatusShortlisted, StatusRejected},
    StatusShortlisted: {StatusInterview, StatusRejected},
    StatusInterview:   {StatusOffer, StatusRejected},
    StatusOffer:       {StatusRejected},
    StatusRejected:    {},
}
```

The same shape as `jobPostTransitions` (4.2): one map is the entire
lifecycle, and `CheckApplicationTransition` is the only code that reads it.
Anything not listed is refused, including staying put, skipping a stage,
going backwards, and any status the map has never heard of. A refusal reuses
`*domain.TransitionError`, so the 409 `invalid_transition` body has the same
`current_status`, `requested_status` and `allowed_next_statuses` as a job
post's.

Decisions inside it:

- **`offer → rejected` is allowed.** "Rejected reachable from any state"
  includes an offer that is withdrawn or declined. It also matters for
  section 3: rejecting an offer is how a vacancy is freed.
- **`rejected` is final.** Reopening a rejected candidate would be a new
  decision with its own audit trail; it is not modelled yet.
- **One route, `PATCH /v1/applications/:id/status`, not one per move.** Job
  posts got `/open` and `/close` because there are two moves. Here there are
  seven legal moves to five targets; a route each would be noise, and the
  map already says what is legal.
  ([`router.go:136`](../../internal/api/router.go#L136))
- **An unknown status is 422, a known but illegal one is 409.**
  `{"status":"hired"}` is a malformed request: no state could accept it.
  `{"status":"offer"}` on a submitted application is a fine request refused
  by the resource's current state.

In the store ([`applications.go:262`](../../internal/store/applications.go#L262))
the application's row is read `FOR UPDATE` first, so two admins moving the
same application at once take turns: the second reads the first one's
result, not the status both started from.

## 2. Why the event stores `from` and `to`

**📍 Open:** [`internal/store/applications.go:307`](../../internal/store/applications.go#L307)

```sql
INSERT INTO application_events (application_id, actor_id, type, payload)
VALUES ($1, $2, $3, jsonb_build_object('from', $4::text, 'to', $5::text))
```

Storing only `{"to": "interview"}` looks sufficient, since the previous event
says what came before. It is not:

- **An event must be readable alone.** "Show me every rejection that
  happened after an interview" is
  `WHERE payload->>'from' = 'interview' AND payload->>'to' = 'rejected'`.
  With only `to`, it needs a window function over each application's whole
  history, ordered correctly, to reconstruct what each row's predecessor was.
- **The chain can have gaps.** Events written before this module, events
  from a data fix, a row whose predecessor was in a table that has since been
  archived: any missing link makes every later "from" unknowable. With both
  values, each row is a complete statement of fact.
- **It records what the actor actually saw.** `from` is the status this
  transaction read under its row lock. That is evidence that the admin moved
  the application from interview, not from some state a concurrent change
  had already put it in. No query run later can recover that.
- **It makes inconsistencies visible.** If one event says `→ shortlisted`
  and the next says `interview →`, something wrote to `applications` without
  going through `TransitionApplication`. With only `to`, that silently looks
  like a normal history.
- **It is cheap.** One more short string per row, in an append-only table.

`jsonb_build_object` assembles the JSON in SQL from typed parameters, so no
JSON is ever built by concatenating strings in Go.

## 3. No offer without a vacancy

**📍 Open:** [`internal/store/applications.go:336`](../../internal/store/applications.go#L336)

Remaining vacancies are defined as in the public listing (4.4): the post's
`vacancies` minus its applications at `offer`. The rule is in the domain,
[`CheckVacancyForOffer`](../../internal/domain/application_status.go#L63),
and the store supplies the two numbers:

```go
const lockPost = `SELECT vacancies FROM job_posts WHERE id = $1::text::uuid FOR NO KEY UPDATE`
…
const countOffers = `SELECT count(*)::int FROM applications
                     WHERE job_post_id = $1::text::uuid AND status = 'offer'`
```

The refusal is a 409 `no_vacancies` whose message tells the admin what to
do ("reject or withdraw an existing offer, or raise the number of
vacancies"), with `vacancies`, `offers_made` and `remaining_vacancies` in
`details`.

**Why lock the post.** Two admins offering the last seat to two different
candidates at once each lock only their own application, so nothing stops
both counting "0 offers of 1" and both writing. Locking the post's row makes
offers on one post take turns. `FOR NO KEY UPDATE` is the weakest lock two
offers cannot share; unlike `FOR UPDATE` it does not block rows that merely
reference the post. It does block a submission's `FOR SHARE` for the few
milliseconds an offer takes, which is acceptable.

**Why the lock and the count are two statements** is section 4.

## 4. The snapshot bug, and how the test found it

**📍 Open:** [`test/integration/pipeline_test.go:193`](../../test/integration/pipeline_test.go#L193)

The first version did both in one statement:

```sql
SELECT p.vacancies,
       (SELECT count(*) FROM applications a WHERE a.job_post_id = p.id AND a.status = 'offer')
FROM job_posts p WHERE p.id = $1 FOR NO KEY UPDATE OF p
```

It looks right: the lock is taken, the count is read. It is wrong. At
PostgreSQL's default isolation, READ COMMITTED, **each statement reads from a
snapshot taken when that statement starts.** The sequence for two offers:

| Time | Offer A | Offer B |
|---|---|---|
| t1 | statement starts; snapshot: 0 offers | |
| t2 | locks the post; counts 0; ok | |
| t3 | | statement starts; snapshot: 0 offers |
| t4 | | tries to lock the post; **waits** for A |
| t5 | `UPDATE … SET status = 'offer'`; `COMMIT` | |
| t6 | | gets the lock; counts from its **t3** snapshot: 0 offers; ok |
| t7 | | `UPDATE`; `COMMIT` → **two offers, one vacancy** |

B waited for the lock, but it was the same statement, so it kept the
snapshot from before the wait. (PostgreSQL does re-read the *locked row*
if it changed, but A never changed the post's row, only an application.)
With two statements, B's count starts at t6, after A committed, so it sees
A's offer and refuses.

**How it surfaced.** The first run of `TestConcurrentOffersForLastVacancy`
passed, and so did a run with the lock removed altogether. A test that
passes with and without the fix proves nothing. The transactions were simply
short enough to finish one after another by luck. The test now installs a
trigger that sleeps 200 ms inside each offer's `UPDATE`, after the check and
before the commit
([`pipeline_test.go:209`](../../test/integration/pipeline_test.go#L209)),
so the transactions really do overlap. Results:

| Version | Result |
|---|---|
| No lock | FAIL: `offered 5, refused 0`; `5 offers stored for one vacancy` |
| Lock and count in one statement | FAIL: `offered 5, refused 0`; `5 offers stored for one vacancy` |
| Lock, then count in a second statement | ok, three runs under `-race` |

Two lessons worth keeping: a concurrency test must be shown to fail without
the protection before its passing means anything, and under READ COMMITTED,
"lock then read" must be two statements.

## 5. Filters without string-concatenating values

**📍 Open:** [`internal/store/applications.go:177`](../../internal/store/applications.go#L177)

`GET /v1/applications` combines three things in one `WHERE`: the role scope
from 3.3, the optional filters `status` and `job_post_id`, and the keyset
cursor. The query text depends on which filters were sent. There are three
ways to build that.

**Concatenating values — the injection bug.**

```go
q := "SELECT … WHERE a.status = '" + c.Query("status") + "'"
```

`?status=x' OR '1'='1` becomes part of the SQL and returns every row. Never.

**One fixed query with `IS NULL` switches.**

```sql
WHERE ($2::text IS NULL OR a.status = $2)
  AND ($3::uuid IS NULL OR a.job_post_id = $3)
```

Safe: every value is a parameter. The public listing (4.4) uses this for its
single optional cursor. Its weakness is the planner: once PostgreSQL decides
to use a generic plan for a prepared statement, it must plan for "filter on
job_post_id" and "don't" with one plan, and often can't use the index for
either.

**Fixed fragments, values as parameters — what was built.**

```go
type conditions struct {
    parts []string
    args  []any
}

// arg records v as the next parameter and returns its placeholder.
func (c *conditions) arg(v any) string {
    c.args = append(c.args, v)
    return "$" + strconv.Itoa(len(c.args))
}
```

Used like this ([`applications.go:98`](../../internal/store/applications.go#L98)):

```go
if filter.Status != "" {
    where.add("a.status = " + where.arg(filter.Status))
}
```

For `?status=interview` that produces the text `a.status = $2` and appends
`"interview"` to `args`. **The only things ever concatenated are fragments
written in this file and placeholders that `arg` generates.** The value
travels separately, in the protocol's parameter slot, and PostgreSQL never
parses it as SQL. The test sends `status = "submitted' OR '1'='1"` and gets
zero rows, because it is simply a status nothing has.

The planner now sees exactly the conditions that apply. A request with
`job_post_id` gets `a.job_post_id = $3` and uses
`applications_job_post_id_status_idx`; a request without it has no such
condition at all. There are only 3 scopes × 4 filter combinations × 2
(cursor or not) = 24 distinct query texts, so pgx's per-connection statement
cache handles them comfortably.

**The cursor composes the same way**
([`applications.go:116`](../../internal/store/applications.go#L116)): it is
one more condition, `(a.submitted_at, a.id) < ($n, $m)`, added to a clone of
the conditions. The count query is built from the conditions *before* the
cursor is added, so it counts every match rather than what remains.

**The order changed.** The list used to sort by `submitted_at DESC, id`,
which is `id ASC`. A row comparison `(a, b) < (x, y)` only expresses "after
the cursor" when both columns run the same direction, so it is now
`submitted_at DESC, id DESC`
([`applications.go:34`](../../internal/store/applications.go#L34)), with a
matching index in migration 000010 for the unfiltered admin view.

**The interviewer scope became `EXISTS`** instead of a `JOIN`, so the
`FROM applications a` is identical for every role and only the condition
differs.

## 6. The total count, and what it costs

**📍 Open:** [`internal/store/applications.go:106`](../../internal/store/applications.go#L106)

Keyset pagination makes a page cost the same at row 20 and at row 2,000,000:
the index takes the scan straight to the cursor and it stops after one page.
`count(*)` has no such shortcut. **It must visit every matching row.**

Why PostgreSQL cannot simply keep a running total:

- **MVCC.** Each transaction sees its own snapshot. A row inserted by an
  uncommitted transaction is in the table but invisible to you; a row
  deleted by a committed one is gone for you but still on the page. "How
  many rows are there" has a different answer for every concurrent
  transaction, so there is no single number to cache. Each count checks the
  visibility of each candidate row.
- **Filters and scopes.** Even a cached total for the table would not answer
  "how many interview-stage applications on post X that this interviewer is
  assigned to".
- **An index helps, but not to zero.** With a suitable index PostgreSQL can
  count using an index-only scan, but it still touches every matching index
  entry, and has to visit the heap for any page not marked all-visible.

So the total is O(matching rows) on every request, including every page
after the first, while the page itself is O(page size). On a table of a few
thousand applications this is unnoticeable. At millions it dominates the
request.

**What was built.** The count and the page run in one read-only
`REPEATABLE READ` transaction
([`applications.go:127`](../../internal/store/applications.go#L127)), so
both read the same snapshot and the total cannot disagree with the page
because an application arrived between the two statements.

**The alternatives, for when it gets big:**

| Approach | Cost | Trade-off |
|---|---|---|
| Exact count every page (built) | O(matches) per request | Simple and exact; fine at this scale |
| Count on the first page only | O(matches) once per listing | Client keeps the number; it goes stale as rows arrive |
| Capped count: `SELECT count(*) FROM (SELECT 1 … LIMIT 1001) s` | At most 1001 rows | Shows "1000+"; what most search UIs do |
| Estimate: `pg_class.reltuples`, or the row estimate from `EXPLAIN` | Constant | Approximate, and only reasonable for unfiltered or simply filtered queries |
| A counter table kept up to date by triggers, per post and status | Constant read, extra write per change | Exact, but every insert and status change pays, and hot counters contend |
| No total; only `next_cursor` | Nothing | What infinite-scroll APIs do; a UI cannot show "page 3 of 9" |

The response keeps `total` as a separate field, so switching to a capped or
estimated count later is a change in the store, not to the API's shape.

## 7. The detail endpoint

**📍 Open:** [`internal/api/application_handler.go:261`](../../internal/api/application_handler.go#L261)

`GET /v1/applications/:id` now returns `answers` (in form order) and
`events` (oldest first) beside the application. The order of work is the
point:

1. `GetApplication` loads the application and whether the viewer is assigned
   to it, in one query, for every caller.
2. `auth.CanViewApplication` decides. A refusal is the same 404 as a missing
   record (D1).
3. **Only then** `ApplicationHistory` loads answers and events
   ([`application_handler.go:287`](../../internal/api/application_handler.go#L287)).

Loading the history before the check would make a refused request do more
work than a missing one, and the difference in response time would tell an
outsider which IDs are real. A handler test checks that a refused viewer
never causes the history to be loaded.

Events are ordered by `created_at, id`. `id` is a tie-breaker because
`now()` is the time the *transaction* started, so events written in one
transaction share a timestamp.

## 8. The tests

**📍 Open:** [`test/integration/pipeline_test.go:116`](../../test/integration/pipeline_test.go#L116)

| Test | Proves |
|---|---|
| [`TestApplicationTransitions`](../../internal/domain/application_status_test.go#L12) | All 49 pairs of 5 statuses plus 2 unknown ones: the 7 legal moves pass, the other 42 are refused with the right `Allowed` |
| [`TestCheckVacancyForOffer`](../../internal/domain/application_status_test.go#L101) | Room, exactly full, over-full after vacancies were lowered |
| [`TestUpdateApplicationStatus`](../../internal/api/application_pipeline_test.go#L13) | Admin only; 422 for a non-status; 409 codes for an illegal move and a full post; the admin is the actor |
| [`TestListApplicationsParameters`](../../internal/api/application_pipeline_test.go#L111) | Query parameters reach the store; both bad filters reported together; the cursor comes from the last row |
| [`TestGetApplicationDetail`](../../internal/api/application_pipeline_test.go#L215) | Answers and events in the body; history never loaded for a refused viewer |
| [`TestApplicationPipeline`](../../test/integration/pipeline_test.go#L116) | A skipped stage writes nothing; each step records `from→to`; a second offer on a one-vacancy post is refused; rejecting the first frees the seat; rejected is final |
| [`TestConcurrentOffersForLastVacancy`](../../test/integration/pipeline_test.go#L193) | Five simultaneous offers for one seat: exactly one is made (section 4) |
| [`TestListApplicationsFilteredAndPaged`](../../test/integration/pipeline_test.go#L258) | Three pages of 2, 2, 1 with no repeats or gaps across a tied timestamp; total 5 on every page; filters × role scopes; an injection attempt returns nothing |
| [`TestApplicationHistory`](../../test/integration/pipeline_test.go#L368) | Answers in form order, events oldest first with the right actors and payload |
| `TestAuthorizationMatrix` | A new column: only an admin may change a status |

---

## Verified

| Command | Result |
|---|---|
| `go test -race -count=1 -cover ./...` | all ok; `domain` 95.9 %, `api` 92.8 % |
| `go test -race -count=1 -tags integration ./test/...` | ok: matrix, rollback, both concurrency tests, pipeline, paging, history |
| `TestConcurrentOffersForLastVacancy` with the lock removed, and with lock and count in one statement | FAIL in both: 5 offers for one vacancy |
| `TestConcurrentOffersForLastVacancy` as committed, `-race -count=3` | ok |
| `make migrate-up && make test-integration` | migration 000010 applied; store and container suites ok |
| `golangci-lint run ./...` | 0 issues |

## Still open

- **Interviewer assignment** has no endpoint yet; assignments exist only in
  seed data and tests.
- **The candidate sees every event**, including who in the hiring team moved
  their application. If internal events are added later (notes, scores),
  the detail endpoint will need to filter them by role.
- **Lowering `vacancies` below the offers already made** is allowed by the
  job post PATCH; the offer check then refuses new offers, and the listing
  shows zero remaining, but nothing warns the admin at the moment of the edit.

## Next

Module 6: scoring an application against the post's rubric.
