# Module 4, part 2 — The job post lifecycle

This note covers moving a job post from draft to open to closed: what a state
machine is and why the rules are a map (sections 1–3), the two routes and the
transaction behind them (sections 4–5), why an illegal move is a 409
(section 6), why a post cannot open without questions and a rubric
(section 7), where the history lives (section 8), the error envelope's new
`details` type (section 9), and the Go idioms and tests that are new here
(sections 10–11).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-a-state-machine-is) | What a state machine is | [`job_post.go:26`](../../internal/domain/job_post.go#L26) |
| [2](#2-why-a-map-beats-a-chain-of-if-statements) | Why a map beats a chain of `if` statements | [`job_post.go:35`](../../internal/domain/job_post.go#L35) |
| [3](#3-the-rest-of-the-lifecycle-api) | The rest of the lifecycle API | [`job_post.go:43`](../../internal/domain/job_post.go#L43) |
| [4](#4-one-route-per-transition) | One route per transition | [`router.go:129`](../../internal/api/router.go#L129) |
| [5](#5-check-and-write-under-one-lock) | Check and write under one lock | [`job_posts.go:205`](../../internal/store/job_posts.go#L205) |
| [6](#6-why-409-and-not-400-or-422) | Why 409, and not 400 or 422 | [`errors.go:82`](../../internal/api/errors.go#L82) |
| [7](#7-why-a-post-cannot-open-without-a-rubric) | Why a post cannot open without a rubric | [`job_post.go:110`](../../internal/domain/job_post.go#L110) |
| [8](#8-where-job-post-history-lives) | Where job post history lives | [`000007_job_post_events.up.sql:11`](../../migrations/000007_job_post_events.up.sql#L11) |
| [9](#9-details-becomes-mapstringany) | `details` becomes `map[string]any` | [`errors.go:53`](../../internal/api/errors.go#L53) |
| [10](#10-go-idioms-appearing-for-the-first-time) | Go idioms appearing for the first time | [`errors.go:130`](../../internal/api/errors.go#L130) |
| [11](#11-the-tests) | The tests | [`job_post_lifecycle_test.go:12`](../../internal/domain/job_post_lifecycle_test.go#L12) |

---

## 1. What a state machine is

**📍 Open:** [`internal/domain/job_post.go:26`](../../internal/domain/job_post.go#L26)

A **state machine** is a thing that is always in exactly one of a fixed set of
states, plus a list of the moves allowed between them. Nothing else is
allowed: no state outside the set, no move outside the list.

A job post's is small:

```
draft ──open──▶ open ──close──▶ closed
```

Three states and two moves. Every move not drawn is refused: draft straight
to closed, back from open to draft, anything out of closed. "Closed is final"
is a rule, not an accident of nobody having written the reopen code.

The whole of it, in Go:

```go
var jobPostTransitions = map[string][]string{
	JobStatusDraft:  {JobStatusOpen},
	JobStatusOpen:   {JobStatusClosed},
	JobStatusClosed: {},
}
```

Read a row as "a post in this state may go to these". `closed` maps to an
empty list instead of being left out, so the finality is written down rather
than implied by a missing key.

The database has half of this already: `job_posts_status_check` allows only
the three values. It cannot express which *moves* are legal, because a
`CHECK` sees one row at one moment and has no idea what the value was before.
The moves live in Go.

---

## 2. Why a map beats a chain of `if` statements

**📍 Open:** [`internal/domain/job_post.go:35`](../../internal/domain/job_post.go#L35)

The alternative is code, usually spread across the handlers that move things:

```go
// in the close handler
if post.Status == "draft" {
	return errors.New("cannot close a draft")
}
if post.Status == "closed" {
	return errors.New("already closed")
}
// otherwise allow
```

*Illustration only, not in the repo.*

With three states that works. It breaks down as the states grow, in specific
ways.

**It fails open.** The chain above lists what to *refuse* and allows the rest.
Add a fourth state, `paused`, and every handler written before it lets a
paused post through, because no `if` mentions it. Nobody decided that paused
posts may close; it is just what happened when the new value fell past the
end of the chain. In the map, a status nobody wrote down is a missing key, a
missing key yields a nil slice, and `slices.Contains` on a nil slice is false.
**A move nobody wrote is refused** — the same deny-by-default rule as the
policy functions in module 3.

**The rule has no single home.** The `if`s for closing live in the close
handler, those for opening in the open handler, and a future "archive" or a
background job that auto-closes past `closes_at` grows its own copy. Each copy
drifts. To answer "can an open post go back to draft?" you read every one. In
the map, the answer is one row.

**The number of cases grows with the square of the states.** With *n* states
there are *n × n* (from, to) pairs. Three states is 9 pairs; five is 25; eight
is 64. An `if` chain covers them implicitly, so you cannot tell by reading it
whether all 64 were thought about. The map lists every legal pair explicitly,
and `TestCanTransition` checks every pair, legal or not.

**The answer cannot be reused.** A refused request should say what *would*
have been allowed (section 6), and a front end wants to know which buttons to
show. With `if`s, "what is allowed from here" is not stored anywhere, so you
would write it a second time and keep the two in step by hand. With the map,
`AllowedTransitions` reads the same row `CanTransition` does.

**It cannot be tested as data.**
[`TestLifecycleCoversEveryStatus`](../../internal/domain/job_post_lifecycle_test.go#L66)
ranges over the map itself and fails if a state is added without the test
being revisited, or if any move points at a state that does not exist. You
cannot range over an `if` chain.

**Where the map stops being enough.** A map says *whether* a move is legal by
status alone. Some rules need more — "may not open without a rubric" depends
on the post's contents. Those stay as code, but as a named check called
*after* the map approves the move (section 7), not mixed into it. When rules
like that multiply, the next step is a map from status to a list of
`{to, guard func}` pairs. Three states do not need it.

---

## 3. The rest of the lifecycle API

**📍 Open:** [`internal/domain/job_post.go:43`](../../internal/domain/job_post.go#L43)

| Function | Returns |
|---|---|
| [`CanTransition(from, to)`](../../internal/domain/job_post.go#L35) | `bool` — the question asked |
| [`AllowedTransitions(from)`](../../internal/domain/job_post.go#L43) | a **copy** of the row, never nil |
| [`CheckTransition(from, to)`](../../internal/domain/job_post.go#L56) | `nil`, or a `*TransitionError` carrying the allowed list |

`AllowedTransitions` copies at
[`job_post.go:50`](../../internal/domain/job_post.go#L50). A slice is a view
onto a shared array. Returning the map's own slice would let any caller write
`allowed[0] = "closed"` and change the lifecycle for every request after it,
in a package-level variable shared by all goroutines. `TestAllowedTransitions`
does exactly that to a returned slice and checks the lifecycle is unchanged.

`make([]string, len(next))` also guarantees a non-nil result, so a closed
post's allowed list encodes as `[]`, not `null`.

**Staying put is not a transition.** `open` → `open` is refused like any
other illegal move. That makes `POST …/open` on an open post a 409, not a
quiet 200. The trade-off: a client that retries a successful request after a
timeout sees a 409 the second time. It is still safe — nothing changes, and
the details say the post is already `open` — but a client must read the
details rather than treat every 409 as failure. The alternative, returning 200
for a no-op, would also write a second "draft to open" event for a move that
did not happen, or need a special case to skip it.

---

## 4. One route per transition

**📍 Open:** [`internal/api/router.go:129`](../../internal/api/router.go#L129)

```
POST /v1/job-posts/:id/open
POST /v1/job-posts/:id/close
```

Both go to [`transition`](../../internal/api/job_post_handler.go#L311), which
takes the target status from the route, never from the body.

**Rejected:** a writable `status` field on `PATCH`. It looks simpler and is
not:

- `PATCH` would mean two different things, editing fields and changing state,
  with different rules, locks and side effects (an event row).
- A body of `{"status":"open","title":"x"}` would have to succeed or fail as
  one, and say which half failed.
- The access log would show `PATCH /v1/job-posts/…` for a rename and for
  closing a vacancy alike.
- Each transition will gain rules of its own — opening checks readiness, and
  closing will one day reject pending applications. A route per transition
  gives each rule an obvious home.

`PATCH` still ignores `"status"` in its body, as it did in part 1.

Both routes are on the `admin` group, so the chain is `RequireAuth →
RequireRole → handler` and a non-admin is refused before the ID is read.

---

## 5. Check and write under one lock

**📍 Open:** [`internal/store/job_posts.go:205`](../../internal/store/job_posts.go#L205)

`TransitionJobPost` runs one transaction:

1. `SELECT … FOR UPDATE` — read the post and lock it.
2. [`CheckTransition`](../../internal/store/job_posts.go#L231) — is the move
   legal from the status just read?
3. If opening: [`openReadiness`](../../internal/store/job_posts.go#L284), then
   [`CheckReadyToOpen`](../../internal/store/job_posts.go#L240).
4. `UPDATE` the status.
5. `INSERT` the event.
6. Commit.

**Why the lock.** Two admins press "open" at the same moment. Without it, both
read `draft`, both pass step 2, both write `open`, and the log records two
"draft to open" events for one change. With it, the second waits at step 1,
then reads `open` and is refused at step 2.
[`TestJobPostStoreTransitionConcurrently`](../../internal/store/job_post_lifecycle_integration_test.go#L226)
races two opens twenty times; with `FOR UPDATE` removed it fails on the first
round.

**The lock also protects the counts.** Inserting a question or a rubric
criterion that references this post makes PostgreSQL take a `FOR KEY SHARE`
lock on the post's row, to be sure it is not deleted mid-insert. `FOR KEY
SHARE` waits for `FOR UPDATE`. So while the transition holds its lock, nobody
can add a question between the count and the status change — the comment at
[`job_posts.go:217`](../../internal/store/job_posts.go#L217) records why. A
*delete* of a question takes no lock on the post, which is listed under
*Still open*; no endpoint deletes questions yet.

**Why the event is in the same transaction.** At
[`job_posts.go:260`](../../internal/store/job_posts.go#L260). An audit log
written separately can disagree with the data: a crash after the `UPDATE`
commits and before the `INSERT` leaves a status change with no record of it,
and the reverse order leaves a record of a change that never happened. Inside
one transaction, both commit or neither does.

`jsonb_build_object('from', $3::text, 'to', $4::text)` builds the payload in
SQL from typed parameters, so no JSON text is assembled by concatenating
strings in Go.

**Why the domain decides and the store enforces.** The rules are pure
functions in `domain`; the store gathers the facts (the status, the counts)
and calls them while holding the lock. That is the module 3 pattern of
`CanViewApplication`, and the part 1 pattern of `Validate` after `Apply`.

---

## 6. Why 409, and not 400 or 422

**📍 Open:** [`internal/api/errors.go:82`](../../internal/api/errors.go#L82)

Each status answers a different question about *who must change what* before
the request can succeed:

| Status | Means | Here? |
|---|---|---|
| **400 Bad Request** | The request itself is malformed — unparseable JSON, an ID that is not a UUID. It will fail however many times it is sent, against any resource. | No. `POST …/close` has no body at all; there is nothing malformed to report. A bad ID *is* a 400, and is checked first. |
| **422 Unprocessable Content** | The request is well-formed and its *content* breaks a rule — a title of 300 characters, `vacancies: 0`. It would be wrong against any post. | No. "Close this post" is a perfectly good request. Sent to an open post, it succeeds. |
| **409 Conflict** | The request is fine; **the resource's current state** does not allow it. | Yes. Whether `close` succeeds depends only on what the post is right now. |

The test is: *would the identical request succeed against a different
resource, or the same one at another time?* For a validation error, no — the
client must change the request. For an illegal transition, yes — the same
`POST …/close` succeeds once the post is open. That is the definition of a
conflict with current state: RFC 9110 describes 409 as for a request that
conflicts with the target resource's current state, where the user might be
able to resolve the conflict and resubmit.

What each tells a client to do next:

- **400 / 422:** fix the request. Retrying it unchanged is pointless.
- **409:** re-read the resource, decide whether the move still makes sense,
  and maybe do something else first. The `details` say exactly what:

```json
{"error":{"code":"invalid_transition","message":"…",
  "details":{"current_status":"closed","requested_status":"open","allowed_next_statuses":[]}}}
```

The not-ready refusal is a 409 too, for the same reason: `open` on a draft
with no rubric fails today and succeeds the moment a rubric is added, with
the request unchanged. Its `details` name what is missing.

**Never a 500.** A refused move is an expected outcome, the API working as
designed. A 500 says the server is broken: it is logged as an internal error,
can page whoever is on call, and many HTTP clients retry 5xx automatically,
repeating a request that cannot succeed. Every refusal path returns a typed
error that `Respond` maps — `TransitionError` and `NotReadyError` unwrap to
sentinels in the mapping table — and the matrix asserts the `not_ready` code
against a real database.

Two distinct codes, `invalid_transition` and `not_ready`, rather than one
generic `conflict`: the fixes differ (choose another action versus add a
rubric), and a client branches on `code`.

---

## 7. Why a post cannot open without a rubric

**📍 Open:** [`internal/domain/job_post.go:110`](../../internal/domain/job_post.go#L110)

Opening is the one-way door. A draft is private and can be anything; an open
post takes applications from real people, and every application is judged
against the rubric and answers the questions as they stood when it arrived.
So the moment of opening is the last cheap moment to insist they exist.

**What breaks without a rubric.**

- **Nothing can be scored.** A score row is "points earned on one criterion",
  and `scores.criterion_id` is `NOT NULL`. With no criteria there is nothing to
  write: every application sits unscored, so ranking and shortlisting by score
  — the point of the pipeline — produce nothing.
- **Adding a rubric later is unfair, not just late.** Rubrics are versioned so
  that a score stays tied to the rubric that produced it. Applications that
  arrived before any rubric existed were never matched to one; scoring them
  now applies rules written after reading them. Two candidates for one post
  would be judged differently depending on when they applied.
- **Criteria point at questions.** An `answer_depth` criterion scores the
  answer to one `question_id`. Written after applications arrive, it may
  depend on a question early applicants never saw.

**What breaks without questions.** Candidates submit an empty form. Add
questions later and every earlier applicant is missing those answers, with no
way to tell a skipped optional question from one that did not exist when they
applied.

**Why "weight above zero" too.** A rubric whose every weight is 0 exists but
cannot score: a weighted total divides by the sum of the weights, so the
answer would be 0 ÷ 0. No scorer exists yet, which is exactly why the rule
belongs at the door rather than in the scorer. `CheckReadyToOpen` reports it separately at
[`job_post.go:120`](../../internal/domain/job_post.go#L120), with a different
message, so the admin knows which of the two to fix.

**Only the current rubric counts.**
[`openReadiness`](../../internal/store/job_posts.go#L284) counts the rows at
the post's highest `rubric_version`, the one new applications will be scored
against. A weighted version 1 does not rescue an all-zero version 2.

**Everything missing is reported at once**, like validation errors: an admin
who lacks both questions and a rubric learns both from one request.

The rule is a function of four counts (`OpenReadiness`), so it is tested with
no database, and the store only supplies the numbers.

---

## 8. Where job post history lives

**📍 Open:** [`migrations/000007_job_post_events.up.sql:11`](../../migrations/000007_job_post_events.up.sql#L11)

The brief asked for an `application_events`-style entry, or to say if that
table is for applications only. **It is for applications only:**
`application_events.application_id` is `NOT NULL` and references
`applications (id)`. An event about a post has no application to point at.

**The options:**

| Option | Verdict |
|---|---|
| Reuse `application_events`: make `application_id` nullable, add a nullable `job_post_id`, and a `CHECK` that exactly one is set | Rejected. Every reader branches on which column is set, the index on `(application_id, created_at)` no longer serves post history, and a row with both or neither is one bug away. |
| One generic `events` table with `subject_type` and `subject_id` | Rejected. `subject_id` cannot have a foreign key, because it points at different tables, so the database can no longer guarantee the subject exists. |
| **A sibling table, `job_post_events`, with the same shape** | **Chosen.** A real foreign key, its own index, and the same conventions: nullable `actor_id` with `SET NULL`, an unfenced `type`, a `jsonb` payload, no `updated_at`. |

This module adds that migration. The event written is `status_changed` with
`{"from":"draft","to":"open"}` and the acting admin. If you prefer another
option, the table is new and nothing else depends on it yet.

`make up` applies the migration automatically; on a running stack, `make
migrate-up` does. `make migrate-down` reverses it, and both were run.

Nothing reads the table yet; see *Still open*.

---

## 9. `details` becomes `map[string]any`

**📍 Open:** [`internal/api/errors.go:53`](../../internal/api/errors.go#L53)

Until now `details` was `map[string]string`: field name to problem. The
allowed statuses are a **list**, and `["open"]` is not a string. The options
were to join them (`"open,closed"`, which every client must split, and which
is `""` for a closed post) or to widen the type. It is now
`map[string]any`, which still always encodes as a JSON object, so the
envelope's shape promise holds.

`Respond` no longer looks for `*ValidationError` specifically. It looks for
anything with a `Details() map[string]any` method — the
[`detailedError`](../../internal/api/errors.go#L53) interface — so
`ValidationError`, `TransitionError` and `NotReadyError` all supply their own
details, and the next error type that needs some only has to add the method.

The tests that compared `details` against strings now go through two small
helpers in `errors_test.go`, `detail(name)` and `stringDetails()`. A plain
`got.Details["x"] == ""` would have compiled and quietly changed meaning: an
absent key is `nil`, and `nil == ""` is false, so a check for "no problem
reported" would always pass.

---

## 10. Go idioms appearing for the first time

**📍 Open:** [`internal/api/errors.go:130`](../../internal/api/errors.go#L130)

**`errors.As` with an interface target.** Module 3 used `errors.As(err,
&invalid)` where `invalid` is a `*domain.ValidationError`: "find an error of
this concrete type". The target can also be a pointer to an **interface**:

```go
var detailed detailedError
if errors.As(err, &detailed) { … }
```

This means "find the first error in the chain that has these methods,
whatever its type". The API layer stays ignorant of how many error types
carry details.

**A map literal with slice values, types elided.** In
`map[string][]string{JobStatusDraft: {JobStatusOpen}}`, the inner
`{JobStatusOpen}` has no type written. Inside a composite literal, Go infers
the element type — here `[]string` — so it need not be repeated per row.

**`any`.** An alias for `interface{}`, the type every value satisfies. It
appears here because one `details` map must hold strings and lists. Reading a
value back out needs a type assertion, `v.(string)`, which is why the test
helper uses the two-value form `s, ok := v.(string)`.

**An empty slice versus a nil slice.** `[]string{}` and `[]string(nil)` both
have length 0 and both work with `range` and `len`, but `encoding/json`
writes the first as `[]` and the second as `null`. `AllowedTransitions` and
`TransitionError.Details` both make sure the client never sees `null`.

**`count(*) FILTER (WHERE …)`.** Not Go, but new here: at
[`job_posts.go:292`](../../internal/store/job_posts.go#L292) one pass over the
rubric rows yields both "how many criteria" and "how many with a positive
weight". An aggregate with no `GROUP BY` also always returns exactly one row,
so a post with no rubric gets counts of 0 rather than `pgx.ErrNoRows`.

---

## 11. The tests

**📍 Open:** [`internal/domain/job_post_lifecycle_test.go:12`](../../internal/domain/job_post_lifecycle_test.go#L12)

| Test | What it pins |
|---|---|
| [`TestCanTransition`](../../internal/domain/job_post_lifecycle_test.go#L12) | All nine (from, to) pairs of real statuses, plus unknown and wrongly-cased values on both sides; and that `CheckTransition` agrees with `CanTransition` in every case |
| [`TestLifecycleCoversEveryStatus`](../../internal/domain/job_post_lifecycle_test.go#L66) | The map has exactly the three statuses, and every destination is itself a status |
| [`TestAllowedTransitions`](../../internal/domain/job_post_lifecycle_test.go#L91) | The lists, non-nil for `closed`, and that mutating a result does not change the lifecycle |
| [`TestCheckReadyToOpen`](../../internal/domain/job_post_lifecycle_test.go#L142) | Each combination of missing questions, missing rubric and zero weights |
| [`TestJobPostTransitions`](../../internal/api/job_post_handler_test.go#L854) | Every route against every state: 200s, 409 `invalid_transition` with the exact allowed list, 404 |
| [`TestOpenJobPostRefusesAnUnreadyPost`](../../internal/api/job_post_handler_test.go#L915) | 409 `not_ready`, with exactly the missing keys in `details` |
| [`TestJobPostTransitionsAreAdminOnly`](../../internal/api/job_post_handler_test.go#L964) | 403 / 401, and 400 for a malformed ID, all before the store |
| [`TestJobPostStoreLifecycle`](../../internal/store/job_post_lifecycle_integration_test.go#L91) | Real SQL: the full lifecycle with one event per move and none per refusal; readiness including rubric versions |
| [`TestJobPostStoreTransitionConcurrently`](../../internal/store/job_post_lifecycle_integration_test.go#L226) | Section 5's race |
| [`TestAuthorizationMatrix`](../../test/integration/authz_test.go#L282) | Three new columns: open a ready draft, open an unready one, close an open post, for every caller |

---

## Verified

| Check | Result |
|---|---|
| `gofmt -l .` | no files |
| `go vet ./...`, with and without `-tags integration` | clean |
| `golangci-lint run ./...` | 0 issues |
| `go test -race -cover ./...` | pass — `api` **91.0%**, `auth` **95.2%**, `domain` **94.4%** |
| `make migrate-up` / `make migrate-down` / `make migrate-up` | version 7 / 6 with `job_post_events` gone / 7 |
| `make test-integration` | store tests and the matrix pass |
| **Deliberate mistakes** | |
| `closed` allowed to go to `open` in the map | domain tests and `TestJobPostTransitions` fail |
| `FOR UPDATE` removed from `TransitionJobPost` | `TestJobPostStoreTransitionConcurrently` fails |
| Readiness check skipped | `TestJobPostStoreLifecycle` fails, and the matrix cell for opening an unready draft |
| **Compose stack, as the seeded admin and candidate** | |
| Open a new draft | 409 `not_ready`, both `questions` and `rubric` named |
| …after adding a question | 409, only `rubric` named |
| …after adding a criterion of weight 0 | 409, the zero-weight message |
| Close the draft | 409 `invalid_transition`, `allowed_next_statuses: ["open"]` |
| Open, as the candidate | 403 |
| Open, with the weight raised to 2 | 200, `status: open` |
| Open again | 409, `allowed_next_statuses: ["closed"]` |
| Close | 200, `status: closed` |
| Reopen the closed post | 409, `allowed_next_statuses: []` |
| Candidate reads the closed post | 200 |
| `job_post_events` for the post | two `status_changed` rows, `draft→open` and `open→closed`, actor set |
| `internal error` lines in the API log during all of the above | 0 |
| Rows created by these checks | removed afterwards |
| Every `#L` link in this note | resolves to the line described; anchors in notes 03 and 04 that this module's code moved were updated |

## Still open

- **No API can make a post ready to open.** Part 3 added the question
  endpoints; rubric criteria still have none, so a rubric row must be
  inserted by hand before a post can open.
- **An open or closed post can still be edited** through `PATCH`, including
  its title and salary after applications have arrived. Whether to freeze
  fields on opening is a product decision worth making before applications
  exist.
- ~~Deleting a question does not wait for the transition lock.~~ Part 3's
  delete takes the post's lock and is refused once the post is open.
- **Nothing reads `job_post_events`.** A `GET /v1/job-posts/:id/events` for
  admins would be the natural first reader.
- **Neither events table is protected from `UPDATE` or `DELETE`.** The
  append-only rule is a convention. A trigger that rejects `UPDATE` would
  enforce it; rejecting `DELETE` would also block the `ON DELETE CASCADE` from
  the parent, so it needs more thought.
- **Edits through `PATCH` create no event.** A post's history records status
  changes only.
- **A retried transition is a 409** (section 3).
- **`closes_at` is not acted on.** Nothing closes a post automatically when
  its deadline passes; when something does, it should call
  `TransitionJobPost` with a null actor, so the rules and the event stay in
  one place.
- **Closing does not touch applications.** Pending applications on a closed
  post stay as they are.

## Next

Endpoints to add questions and rubric criteria to a draft, so a post can be
made ready and opened through the API; then submitting an application to an
open post.
