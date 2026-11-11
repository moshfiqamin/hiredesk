# Module 7, part 4 — Rescoring

A rubric can change after applications are scored ([07-1, D2](07-1-rubric-endpoints.md#1-draft-only-or-editable-later-the-decision)).
This part adds the deliberate step that brings old scores up to date: one
application, or every application to a post. It covers **why the rows are
deleted and reinserted rather than updated** (section 1); the single rescore
and its lock (section 2); **why the event records both totals** (section 3);
**rescoring 500 applications: one transaction, batches, or the background**
(section 4) and how the set-based version works (section 5); the lock order
that avoids deadlocks (section 6); **a rescore running while someone reads
the breakdown, and whether that matters here** (section 7); and what was
run (section 8).

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-delete-and-reinsert-not-update) | **Delete and reinsert, not update** | [`rescore.go:190`](../../internal/store/rescore.go#L190) |
| [2](#2-rescoring-one-application) | Rescoring one application | [`rescore.go:32`](../../internal/store/rescore.go#L32) |
| [3](#3-why-the-event-records-both-totals) | **Why the event records both totals** | [`rescore.go:271`](../../internal/store/rescore.go#L271) |
| [4](#4-500-applications-the-decision) | **500 applications: the decision** | [`rescore.go:73`](../../internal/store/rescore.go#L73) |
| [5](#5-set-based-a-fixed-number-of-statements) | Set-based: a fixed number of statements | [`rescore.go:140`](../../internal/store/rescore.go#L140) |
| [6](#6-lock-order-and-the-deadlock-that-is-not-there) | Lock order, and the deadlock that is not there | [`rescore.go:116`](../../internal/store/rescore.go#L116) |
| [7](#7-a-rescore-while-someone-reads-the-breakdown) | **A rescore while someone reads the breakdown** | — |
| [8](#8-verification) | Verification | — |

---

## 1. Delete and reinsert, not update

**📍 Open:** [`internal/store/rescore.go:190`](../../internal/store/rescore.go#L190)

A score row is keyed, in meaning, by *(application, criterion)*, and
`criterion_id` points at one immutable row of one rubric version
([07-1 section 2](07-1-rubric-endpoints.md#2-why-version-instead-of-overwrite)).
A rescore moves the application from version 1's criteria to version 2's,
and those are **different rows with different IDs**. So there is nothing to
"update in place":

| Version 1 → version 2 | What an UPDATE would have to do |
|---|---|
| 4 criteria → 2 | update 2 rows to point at new criteria, **and delete 2** |
| 2 criteria → 4 | update 2 rows, **and insert 2** |
| same count, different criteria | rewrite every column, including `criterion_id` — a new row in all but name |
| same criteria, new weights | still new criterion rows (a new version), so still a new `criterion_id` |

Every case ends up needing deletes or inserts anyway, plus a matching step
("which old row becomes which new one?") that has no right answer: row 3 of
version 1 has no natural partner in version 2. An UPDATE would also have to
change `criterion_id` on a row whose identity *is* that criterion, and would
keep the old `id` and `computed_at`, so the row would claim to be the old
score while holding the new one.

Delete-and-reinsert has none of that: the new rows are exactly what
`insertScores` writes at submission, through the same function. `TestRescoreApplication`
([`rescore_test.go:64`](../../test/integration/rescore_test.go#L64))
replaces a four-criterion rubric with a two-criterion one and checks that
exactly two rows, of version 2, remain.

**Why the gap is invisible.** Between the `DELETE` and the `INSERT` the
application has no scores. Both are in one transaction, and other sessions
see only committed data, so no reader ever observes that state (section 7).

## 2. Rescoring one application

**📍 Open:** [`internal/store/rescore.go:32`](../../internal/store/rescore.go#L32)

In one transaction:

1. **Lock the application** with `FOR UPDATE`
   ([line 44](../../internal/store/rescore.go#L44)), and learn its post.
2. **Load the current rubric**, and refuse with `ErrNoRubric` (409
   `no_rubric`) if there is none ([line 148](../../internal/store/rescore.go#L148))
   — *before* anything is deleted. Rescoring against nothing would throw away
   the old scores and put nothing in their place.
3. **Read the old totals and the stored answers.**
4. **Score**, delete, insert, record the event.

The stored answers carry their IDs, so the engine rebuilds the evidence
links exactly as at submission ([07-2 section 4](07-2-scoring-on-submit.md#4-tracing-evidence_answer_id)).

**Why the application lock matters.** Two rescores of one application at
once, with no lock, at READ COMMITTED:

```text
T1: DELETE old rows                  T2: DELETE old rows  → waits on T1's row locks
T1: INSERT new rows (set A)
T1: COMMIT                           T2: (wakes) old rows are gone; skips them
                                     T2: INSERT new rows (set B)
                                     T2: COMMIT  → the application has A and B
```

That is a timeline, not something to run. T2's `DELETE` took its snapshot
before T1 committed, so it never saw set A, and both sets survive.
`TestConcurrentRescores`
([`rescore_test.go:227`](../../test/integration/rescore_test.go#L227)) runs 20
single and whole-post rescores at once. **With both `FOR UPDATE`s removed,
it failed 3 runs out of 3, with every application holding a doubled set.**
With them, every application has exactly one.

The response is a summary — previous and current totals, and `changed`.
The full breakdown is one `GET …/score` away; repeating it here would be a
second copy of that endpoint's shape to keep in step.

## 3. Why the event records both totals

**📍 Open:** [`internal/store/rescore.go:271`](../../internal/store/rescore.go#L271)

```text
{"trigger": "application",
 "old_rubric_version": 1, "old_total_earned": 66.8, "old_total_available": 100,
 "new_rubric_version": 2, "new_total_earned": 100,  "new_total_available": 100}
```

That is a payload read from the live database, not a command. Three
reasons both sides are kept:

- **The old one is gone everywhere else.** The `DELETE` in the same
  transaction removed the rows that held it. After the commit, this event is
  the only record that the application ever scored 66.8. Without it, "this
  candidate was shortlisted at 66.8%" could no longer be checked against
  anything.
- **The new one is a snapshot.** The current rows hold it today, but the
  next rescore deletes them too. Each event records its own after-state, so
  the timeline reads as a chain: 66.8 → 100 → … without consulting any
  other table.
- **Both versions say why it moved.** A jump with the same version means
  the *answers* were scored differently (an engine change); a jump with a
  new version means the *rubric* changed. "Old 66.8 at v1, new 100 at v2"
  explains itself.

`earned` and `available` are both stored, not only a percentage: available
changes when weights change, and 50% of 80 is not the same claim as 50% of
100. Old values are JSON `null` for an application that had no score before.
The `trigger` says whether one application or the whole post was rescored.

## 4. 500 applications: the decision

**📍 Open:** [`internal/store/rescore.go:73`](../../internal/store/rescore.go#L73)

The three options, for this project:

| | One transaction | Batches (e.g. 50 per transaction) | Background job |
|---|---|---|---|
| All-or-nothing | **yes** | no — a failure leaves some rescored | no |
| Ranking ever mixes versions mid-way | **never** (readers see before or after) | yes, until the last batch commits | yes, until it finishes |
| Locks held | all 500 rows, for the whole run | 50 at a time, briefly | as batching |
| Retry after a failure | rerun everything | rerun; it is idempotent | the queue retries |
| Client waits | for the whole run | for the whole run | no — gets a job ID |
| New infrastructure | none | none | queue, worker, job status endpoint |

**The cost that decides it is the run time, and that depends on how the
work is written.** Done naively — `RescoreApplication` in a loop — 500
applications is 500 × (lock, rubric, answers, totals, delete, insert,
event) ≈ 3,500 statements, a second or more, with all 500 rows locked
throughout. That would argue for batches.

Done set-based (section 5), it is a fixed eight statements whatever N is.
**Measured: 500 applications (1,000 score rows deleted and reinserted, 500
events) in 45–57 ms over five runs, with one outlier at 138 ms**, against a
fresh PostgreSQL 16 container on a laptop.

**Recommended and built: one transaction, set-based, in the request.**
At 50 ms the locks are held for less time than a single slow page load; a
status change to one of those applications waits for at most that long.
In exchange, the post is rescored atomically — the ranking shows the old
version or the new, never a mixture — and there is no job system to build.

**When to change it:**

- **Tens of thousands of applications** to one post, where the transaction
  would run for seconds: batch it, keyed on application ID (`WHERE id >
  $last ORDER BY id LIMIT 500`), each batch its own transaction. Batching is
  safe to retry because a rescore against the same version is idempotent.
- **A slow scorer** (an LLM, a remote parser): move to the background, with
  a job row the admin can poll, because no request should wait minutes.
- Either way the engine is untouched: it takes domain values
  ([07-2 section 5](07-2-scoring-on-submit.md#5-why-the-engine-takes-domain-types)).

## 5. Set-based: a fixed number of statements

**📍 Open:** [`internal/store/rescore.go:140`](../../internal/store/rescore.go#L140)

`rescore` is shared by both endpoints — the single rescore passes a list of
one ID. For N applications it runs:

| # | Statement | Rows |
|---|---|---|
| 1 | lock the applications, `ORDER BY id FOR UPDATE` | N |
| 2 | load the rubric | criteria |
| 3 | old totals, `WHERE application_id = ANY($1)`, `GROUP BY` | N |
| 4 | every answer, `WHERE application_id = ANY($1)` | all answers |
| 5 | `DELETE … WHERE application_id = ANY($1)` | old scores |
| 6 | one `INSERT … SELECT FROM unnest(…)` for every score row | N × criteria |
| 7 | one `INSERT … SELECT FROM unnest(…)` for every event | N |

Plus the post's existence check. Two ideas carry it:

- **`= ANY($1)` with one array parameter** replaces "one query per
  application" — the same N+1 avoidance as the breakdown
  ([07-3 section 5](07-3-score-breakdown.md#5-how-the-n1-was-avoided)), on
  the write side. `$1::text[]::uuid[]` sends the IDs as text and lets
  PostgreSQL cast the array.
- **The rubric is compiled once** ([line 153](../../internal/store/rescore.go#L153))
  and the `*scoring.Rubric` scores every application. `scoring.Score`
  would recompile the keyword regexes 500 times. The package was built for
  this ([06-1 section 5](06-1-scoring-engine.md)).

The scoring itself happens in Go between statements 4 and 5: pure CPU,
microseconds per application.

`insertScores` changed to take many applications at once, and submission
now calls it with a list of one ([`rubric.go:281`](../../internal/store/rubric.go#L281)).
The events use `[]*int` and `[]*float64` for the old values: a nil element
is sent as an SQL `NULL` inside the array, which `jsonb_build_object` writes
as JSON `null`.

## 6. Lock order, and the deadlock that is not there

**📍 Open:** [`internal/store/rescore.go:116`](../../internal/store/rescore.go#L116)

A deadlock is two transactions each holding a lock the other is waiting
for. PostgreSQL detects it after `deadlock_timeout` (one second) and aborts
one with SQLSTATE 40P01 — the user sees a 500. The defence is that every
transaction takes shared locks in one global order.

**Between bulk rescores:** `ORDER BY a.id FOR UPDATE` locks rows in ID
order, so two rescore-alls on one post queue behind each other instead of
each grabbing half.

**Against an offer.** The first design also locked the post (`FOR SHARE`),
so that a rubric replacement could not commit mid-rescore. That creates a
cycle with `TransitionApplication`
([`applications.go:262`](../../internal/store/applications.go#L262)), which
locks an application and then, for an offer, the post:

```text
rescore-all:  post (SHARE) ……… then application A (UPDATE) → waits for the offer
offer on A:   application A (UPDATE) ……… then post (NO KEY UPDATE) → waits for rescore-all
```

That is a timeline, not something to run. So the post is **not** locked. What
that gives up: a rubric replacement can commit while a rescore is running.
The rescore read one complete version in one statement, writes scores
labelled with that version, and the replacement's `applications_to_rescore`
counts them — accurate, just already one version behind, which is the same
state as any score before a rescore.

## 7. A rescore while someone reads the breakdown

What could go wrong, in principle:

- **Reading the gap.** If the delete and the insert were separate
  transactions, a read between them would show no scores — a 404
  `not_scored` for an application that has them. They are one transaction,
  so this cannot happen: an uncommitted delete is invisible to everyone
  else.
- **Reading a mixture.** Could one read see some old rows and some new?
  No. The breakdown is a **single statement**, and in PostgreSQL every
  statement reads from one snapshot, taken when it starts. It sees the
  database either entirely before the rescore committed or entirely after.
- **Two reads disagreeing.** The handler runs two statements:
  `GetApplication`, then `ScoreCard`. A rescore can commit between them.
  That is harmless: the first only checks access, and the card comes from
  the second, alone.
- **Old score shown just after a rescore.** A read that started a moment
  before the commit returns the old card. It was true when the read
  began; the next read shows the new one. That is not an inconsistency, just
  ordering.

**Does it matter at this scale?** No: MVCC already gives every guarantee
above for free, with no extra locking, at any scale. Where it *would* start
to matter is a screen that combines several reads — the ranking, then each
card — expecting them to agree; that needs one `REPEATABLE READ`
transaction, as the paginated list uses ([05-4](05-4-application-pipeline.md)).
And a bulk rescore split into batches (section 4) would let a reader see
half the post rescored, which is exactly why it is one transaction here.

## 8. Verification

| Command | Result |
|---|---|
| `go test -race ./internal/api/ -run Rescore` | pass: totals and percentages, null previous, 409 `no_rubric`, 404, 400, 403 for interviewer and candidate with no store call, bulk summary counting `changed` |
| `make test-integration` | pass, including the four tests below and 16 new matrix cells |
| `TestRescoreApplication` | 72.5/100 at v1 → 70/100 at v2; 4 rows replaced by 2; evidence kept; event payload with both totals and versions |
| `TestRescoreWithoutRubricKeepsNothingBroken` | `ErrNoRubric`, no event; missing application → `ErrNotFound` |
| `TestRescoreJobPost` | 12 applications, 24 rows, one version, 12 `job_post` events; empty post → 0 results; missing post → not found |
| `TestConcurrentRescores` with both `FOR UPDATE`s removed, `-count=3` | 3 of 3 fail, every application doubled; with them, pass |
| throwaway timing test, 500 applications | 45, 51, 50, 57 ms, one run at 138 ms |
| live: rescore after the rubric change | 66.8 at v1 → 100 at v2, `changed: true`; again → `changed: false` |
| live: rescore-all | `rescored: 1, changed: 0` |
| `make fmt && make lint && make test && go build ./...` | clean, 0 issues, all pass, builds |

**Trying it yourself.** With the stack up, in the VS Code terminal, with
`$ADMIN` an admin token and `$APP` an application ID:

```bash
curl -s -X POST localhost:8080/v1/applications/$APP/rescore -H "Authorization: Bearer $ADMIN" | jq
```

Rescores one application and prints its totals before and after; run it
twice, and the second shows `"changed": false`.
