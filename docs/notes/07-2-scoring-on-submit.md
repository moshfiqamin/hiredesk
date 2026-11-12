# Module 7, part 2 — Scoring inside the submission

The moment an application is stored, it is scored, in the same transaction.
This note covers the decision for a post with **no rubric** (section 1);
the hook into the transaction from [05-2](05-2-submit-transaction.md) and
why a scoring failure rolls everything back (sections 2–3); **how
`evidence_answer_id` gets set, traced from the engine to the row**
(section 4); **why the engine takes domain types, not pgx rows**
(section 5); storing the numbers (section 6); **whether scoring belongs in
the request or a background job, and at what scale that changes**
(section 7); and what was run (section 8).

Part 1 is [07-1-rubric-endpoints.md](07-1-rubric-endpoints.md).

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-a-post-with-no-rubric-the-decision) | A post with no rubric: the decision | [`applications.go:886`](../../internal/store/applications.go#L886) |
| [2](#2-the-hook) | The hook | [`applications.go:837`](../../internal/store/applications.go#L837) |
| [3](#3-why-a-scoring-failure-fails-the-submission) | Why a scoring failure fails the submission | [`applications.go:892`](../../internal/store/applications.go#L892) |
| [4](#4-tracing-evidence_answer_id) | **Tracing `evidence_answer_id`** | [`engine.go:312`](../../internal/scoring/engine.go#L312) |
| [5](#5-why-the-engine-takes-domain-types) | **Why the engine takes domain types** | [`applications.go:873`](../../internal/store/applications.go#L873) |
| [6](#6-storing-the-numbers) | Storing the numbers | [`rubric.go:281`](../../internal/store/rubric.go#L281) |
| [7](#7-in-the-request-or-in-a-background-job) | **In the request, or in a background job?** | — |
| [8](#8-verification) | Verification | — |

---

## 1. A post with no rubric: the decision

**📍 Open:** [`internal/store/applications.go:886`](../../internal/store/applications.go#L886)

**Decision: accept the application, store no scores, and record that it was
not scored.** The `application.submitted` event carries
`"rubric_version": null`, `SubmitApplication` returns a nil
`RubricVersion`, and `GET /score` answers 404 with the code `not_scored`.

First, how "somehow" could happen. Opening a post requires a weighted rubric
(`CheckReadyToOpen`, [04-2](04-2-job-post-lifecycle.md)), and `PUT /rubric`
cannot store an empty one (`ValidateCriteria` requires at least one
criterion). So an open post with no criteria means someone changed rows by
hand. The options:

| Option | What the candidate sees | What the admin sees |
|---|---|---|
| **Refuse the submission** (409 or 500) | "Try again later", for as long as the data stays broken | applications silently stop arriving |
| **Accept, unscored** (built) | a normal confirmation | the application, at the bottom of the ranking, marked not scored |
| Accept with a zero score | a normal confirmation | a fake "0 of 0" that looks like a real result |

Refusing punishes the candidate for an admin's data problem, and the
candidate is the one person who cannot fix it. A zero score is a lie in the
data — it is indistinguishable from a real result. "Not scored" is an honest
state the rest of the design already expects: the ranking query planned for
module 7 uses a `LEFT JOIN` precisely so unscored applications still appear,
and the rescore endpoint can score them once the rubric is repaired.

**Why that is not "log and continue".** The project error convention
forbids swallowing errors.
No error is swallowed here: an empty rubric is not an error from any call,
it is a state, and the state is recorded durably in the event row rather
than in a log line nobody reads.

**Contrast with section 3.** A *missing* rubric means there is nothing to
score against. A *broken* rubric — one the engine refuses — means scoring
was attempted and failed. The first is a state to record; the second is an
error, and it rolls back.

## 2. The hook

**📍 Open:** [`internal/store/applications.go:837`](../../internal/store/applications.go#L837)

One line in `SubmitApplication`, between the answers and the event:

```text
lock post (FOR SHARE) → validate → insert application → insert answers
  → scoreApplication → insert event → COMMIT
```

That is the order of statements, not a command. `scoreApplication`
([line 619](../../internal/store/applications.go#L726)) does three things
on the same `tx`:

1. `loadRubric(ctx, tx, …)` — the current version, through the same query
   `GET /rubric` uses, but inside this transaction. `loadRubric` takes the
   small `queryer` interface rather than a pool or a transaction, so both
   callers pass what they have.
2. `scoring.Score(app.Answers, rubric.Criteria)` — pure, no database.
3. `insertScores` — one statement for every criterion.

The rubric cannot change underneath: `ReplaceRubric` takes
`FOR NO KEY UPDATE` on the post, which waits for the `FOR SHARE` this
transaction already holds ([07-1 section 5](07-1-rubric-endpoints.md#5-the-lock-and-the-race-it-closes)).

The event now records the version used, so the timeline says which rubric
judged the application, even after the scores are rescored.

## 3. Why a scoring failure fails the submission

**📍 Open:** [`internal/store/applications.go:892`](../../internal/store/applications.go#L892)

`Score` returns an error only for a rubric it cannot apply: an unknown kind
or an unusable config. Neither can arrive through `PUT /rubric`
([07-1 section 8](07-1-rubric-endpoints.md#8-validation-at-the-boundary)),
so either means a hand-edited row. The error is returned, `defer
tx.Rollback` runs, and the application, its answers and its partial scores
all vanish. The candidate gets a 500 and can retry once it is fixed.

Why not keep the application and skip scoring, as in section 1? Because the
post *claims* to have a rubric. An application stored unscored against a
rubric that exists would look identical to one submitted before the rubric
existed, and the breakage would be invisible. Failing loudly gets it
noticed and fixed. It is also what the brief asked for.

`TestSubmitRollsBackWhenScoringFails`
([`scoring_test.go:183`](../../test/integration/scoring_test.go#L183))
breaks the rubric with `UPDATE … SET config = '{"keywords": []}'`, submits,
and checks: the error wraps `scoring.ErrInvalidConfig` (so `errors.Is` sees
through both `%w` layers), and zero applications, answers, events and scores
remain — though the application and answers *had* been inserted when the
engine refused.

## 4. Tracing `evidence_answer_id`

**📍 Open:** [`internal/scoring/engine.go:312`](../../internal/scoring/engine.go#L312)

The value is born in PostgreSQL, travels through the engine untouched, and
goes back into PostgreSQL. Follow one answer, the candidate's reply to
"Describe a project":

**1. The database creates the ID.** `insertAnswers` inserts the answers and
returns them with `RETURNING id::text, …`
([`applications.go:923`](../../internal/store/applications.go#L923)). Each
`domain.Answer` now has `ID = "a188f925-…"`, `QuestionID = "<describe>"`.
These go into `app.Answers` ([line 578](../../internal/store/applications.go#L685)).

**2. The engine indexes answers by question.** `Score` builds
`answerIndex`, a `map[questionID]domain.Answer`. The whole `Answer` is
stored, ID included.

**3. A single-answer scorer finds its answer and copies the ID.**
`singleAnswer` ([`engine.go:305`](../../internal/scoring/engine.go#L305))
looks up the criterion's `QuestionID`, runs the strategy, then sets
`o.evidenceAnswerID = a.ID` (line 312). The strategy itself never sees IDs —
the adapter owns the evidence link, so no strategy can forget it.

**4. The engine copies it into the result.** `Rubric.Score` writes
`EvidenceAnswerID: o.evidenceAnswerID` into the `CriterionScore`
([line 265](../../internal/scoring/engine.go#L265)).

**5. The store turns it into a column.** `insertScores` appends each
`cs.EvidenceAnswerID` to a `[]string`
([`rubric.go:296`](../../internal/store/rubric.go#L296)), sent as a
`text[]`, and the SQL turns each element back into a uuid with
`NULLIF(t.evidence_answer_id, '')::uuid`
([line 306](../../internal/store/rubric.go#L306)).

**6. The foreign key checks it.** `scores.evidence_answer_id REFERENCES
application_answers (id)`. The answer row was inserted earlier *in the same
transaction*, and a transaction sees its own uncommitted rows, so the
check passes.

When there is no single answer — the question was unanswered, or the kind
is `boolean_all`, which reads several — the engine leaves the string empty,
`NULLIF` turns `''` into `NULL`, and the column is `NULL`. The empty string
is the Go side's "none", `NULL` is the SQL side's, and `NULLIF` is the
translation.

`TestSubmitScoresTheApplication`
([`scoring_test.go:122`](../../test/integration/scoring_test.go#L122))
joins each score to its evidence answer and checks it belongs to the right
question: the keyword and depth scores to the text question, the experience
score to the number, `boolean_all` to nothing.

**A conflict between the project documents.** The overview's example
response shows a `boolean_all` entry *with* evidence, and the workflow's
check says a NULL evidence on a score above zero "is a bug". Migration 000005
and the engine say `boolean_all` has no single evidence answer. This module
follows the schema and the engine; changing it is a decision for the
engine, not for this endpoint (see "What I just built" for the options).

## 5. Why the engine takes domain types

**📍 Open:** [`internal/store/applications.go:873`](../../internal/store/applications.go#L873)

`scoring.Score(answers []domain.Answer, criteria []domain.Criterion)`. The
store converts rows to domain values — `scanCriterion` for criteria,
`insertAnswers`' scan for answers — and the engine never sees SQL. Handing
it `pgx.Rows` instead would break, later, in four concrete places:

- **Tests.** Every engine test today builds a `domain.Answer` literal. With
  `pgx.Rows` in the signature, each would need a fake implementing `Next`,
  `Scan`, `Values`, `Err`, `Close`, `CommandTag`, `FieldDescriptions`,
  `RawValues` and `Conn` — or a real database. The table-driven cases
  in [06-1](06-1-scoring-engine.md) would stop being one-liners.
- **The rescore endpoint, next.** It must score answers that are *already
  stored*, read by a different query. A `pgx.Rows`-shaped engine would need
  that query to return columns in exactly the order the engine scans —
  coupling every caller's SQL to the engine.
- **The excerpt, in part 3.** `scoring.Excerpt` takes a `domain.Criterion`
  that came out of the breakdown query, a third query. Domain types are
  what let three unrelated queries feed one engine.
- **Purity.** [06-2 section 12](06-2-depth-and-validation.md) proved with
  `go list -deps` that `scoring` imports no driver. A `pgx` parameter would
  pull the driver into the package and end that guarantee, and the engine
  would gain a way to fail — a scan error mid-score — that it has no
  business having.

The arrow points one way: `store → scoring → domain`. The store knows about
the engine (it calls `Score`); the engine knows nothing about the store.

## 6. Storing the numbers

**📍 Open:** [`internal/store/rubric.go:281`](../../internal/store/rubric.go#L281)

One statement for all scores, with `unnest` over parallel arrays — one
application at submission, or five hundred in a rescore
([07-4](07-4-rescore.md)). Two
details:

**Rounded in Go.** A criterion matching 1 of 3 keywords at weight 10 earns
`3.3333333333333335` in float64. `numeric(6, 2)` would round that on insert anyway, but
`roundCents` ([line 330](../../internal/store/rubric.go#L330)) does it
first, so the value stored is exactly the value this code decided, and the
column's sum is the total the breakdown shows.

**`rubric_version` is copied, not joined.** The criterion row knows its
version too, but 000005 copies it onto the score so "which rubric produced
this number?" is answered by the score row alone.

## 7. In the request, or in a background job?

**For this project, in the request is right.** The costs, measured on this
stack:

| Step | Cost |
|---|---|
| Load the rubric | one indexed query, sub-millisecond |
| Score | pure CPU: compile a few regexes, match a few kB of text — microseconds ([06-1 section 10](06-1-scoring-engine.md#10-what-precompiling-saves) measured it) |
| Insert the scores | one statement |

Against that, what in-request buys:

- **Atomicity.** An application exists if and only if its scores do. No
  "scoring pending" state, no job that fails silently, no reconciler.
- **Read-your-writes.** The admin who refreshes the ranking sees the new
  application scored.
- **No infrastructure.** No queue, worker, retry policy or dead-letter
  handling to build, deploy and monitor.

**When the answer changes.** Not at a number of applications per day — it
is driven by *what scoring costs per application*:

1. **Slow or remote scoring.** If a criterion ever calls a CV parser or an
   LLM (hundreds of milliseconds to seconds, with its own failure modes),
   scoring inside the transaction holds the post's `FOR SHARE` lock and a
   pool connection for that whole time, and an outage of the remote service
   becomes an outage of applying. That is the point to move it out.
2. **Bursts.** A post shared widely might take thousands of submissions in
   minutes. Each is a short transaction today; at a few milliseconds each,
   PostgreSQL handles that comfortably. It stops being comfortable when
   scoring time × arrival rate approaches the connection pool's size
   (`DB_MAX_CONNS`), because requests start queueing for connections.
3. **Bulk rescoring.** Re-running a changed rubric over 50,000 stored
   applications is the first job that genuinely belongs in the background —
   that is a batch, not a request.

**How it would move, without losing atomicity.** The transactional outbox
from [05-2 section 5](05-2-submit-transaction.md#5-why-the-event-is-inside-the-transaction): the submit transaction
writes the application and an `application.submitted` event (it already
does), and a worker scores applications whose event has no matching scores.
Because the event and the application commit together, no application can
be missed. Because the engine takes domain types (section 5), the worker
calls exactly the same `scoring.Score`.

## 8. Verification

| Command | Result |
|---|---|
| `make test-integration` | pass, including the four tests below |
| `TestSubmitScoresTheApplication` | 4 rows; 22.5/30, 20/40, 10/10, 20/20 as worked by hand; evidence on the right questions; event `rubric_version` 1 |
| `TestSubmitRollsBackWhenScoringFails` | error wraps `scoring.ErrInvalidConfig`; 0 applications, answers, events, scores |
| `TestSubmitWithoutRubricIsKeptUnscored` | stored, no scores, event `rubric_version` is JSON null, `ScoreCard` → `ErrNotScored` |
| `TestSubmitRollback` (05-2, unchanged) | still pass: its post has no rubric, so it exercises section 1's path |
| live: candidate applies to the module-7 post | 201 |
| live: `psql` evidence join | 4 rows; three join to answers, `boolean_all` is NULL |
| `make fmt && make lint && make test && go build ./...` | clean, 0 issues, all pass, builds |

**Checking the evidence yourself.** With the stack up, in the VS Code
terminal, replacing `<APP>` with an application's ID:

```bash
docker compose exec postgres psql -U hiredesk -d hiredesk -c "SELECT s.earned, aa.id AS answer_id, left(aa.answer_text, 40) FROM scores s LEFT JOIN application_answers aa ON aa.id = s.evidence_answer_id WHERE s.application_id = '<APP>';"
```

Lists each score beside the answer it points at; every row except a
`boolean_all` should show an answer.
