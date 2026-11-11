# Module 7, part 1 — Rubric management

This part gives the scoring engine something to read: an admin can write a
job post's rubric, and read it back. It covers the decision about **when a
rubric may change** (section 1); why a rubric is **versioned rather than
overwritten**, and what breaks if a score points at a criterion that no
longer exists (sections 2–3); the replace transaction and its lock
(sections 4–5); a migration that gives criteria an order (section 6);
**how a jsonb config round-trips through pgx, with the encode and scan sides
shown** (section 7); validating at the API boundary so a malformed config
never reaches the database (sections 8–9); new Go idioms (section 10); and
what was run (section 11).

Part 2 is [07-2-scoring-on-submit.md](07-2-scoring-on-submit.md); part 3 is
[07-3-score-breakdown.md](07-3-score-breakdown.md).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-draft-only-or-editable-later-the-decision) | Draft only, or editable later? The decision | [`router.go:150`](../../internal/api/router.go#L150) |
| [2](#2-why-version-instead-of-overwrite) | **Why version instead of overwrite** | [`000003_rubric.up.sql:46`](../../migrations/000003_rubric.up.sql#L46) |
| [3](#3-why-the-old-rows-must-be-kept) | **Why the old rows must be kept** | [`000005_scores_events.up.sql:14`](../../migrations/000005_scores_events.up.sql#L14) |
| [4](#4-the-replace-transaction) | The replace transaction | [`rubric.go:90`](../../internal/store/rubric.go#L90) |
| [5](#5-the-lock-and-the-race-it-closes) | The lock, and the race it closes | [`rubric.go:107`](../../internal/store/rubric.go#L107) |
| [6](#6-a-migration-giving-criteria-an-order) | A migration giving criteria an order | [`000011_….up.sql:35`](../../migrations/000011_rubric_criteria_position.up.sql#L35) |
| [7](#7-jsonb-through-pgx-both-directions) | **jsonb through pgx, both directions** | [`rubric.go:262`](../../internal/store/rubric.go#L262) |
| [8](#8-validation-at-the-boundary) | Validation at the boundary | [`rubric_handler.go:190`](../../internal/api/rubric_handler.go#L190) |
| [9](#9-checks-that-need-the-form) | Checks that need the form | [`form.go:21`](../../internal/scoring/form.go#L21) |
| [10](#10-go-idioms-new-in-this-part) | Go idioms new in this part | — |
| [11](#11-verification) | Verification | — |

---

## 1. Draft only, or editable later? The decision

**📍 Open:** [`internal/api/router.go:150`](../../internal/api/router.go#L150)

The brief offered two rules: a rubric editable only while the post is a
draft, or editable later with an explicit rescore. **Recommended and built:
editable in any status, never rescored automatically.** The rescore is its
own deliberate action — `POST /v1/applications/:id/rescore`, the next prompt
in this module.

Why not draft-only, which is what questions do
([04-3-questions.md](04-3-questions.md))?

- **Versioning would be pointless.** A draft has no applications, so no
  scores, so nothing that needs an old version to stay readable. The schema
  has carried `rubric_version` since migration 000003 precisely so that a
  rubric can change *after* scores exist. Draft-only would leave that column
  permanently at 1.
- **Typos would be permanent.** An admin who misspells `kubernetes` as
  `kubernets` in a keyword list notices on the first application. Under
  draft-only, the only fix is to close the post and open a new one, losing
  every applicant.
- **The fairness argument is different from questions'.** Questions lock
  because every candidate must be *asked* the same thing; that cannot be
  repaired after the fact. A rubric is how answers are *judged*, and a
  judgement can be redone — every answer is still stored. Rescoring makes
  everyone judged by the same rule again.

What this choice risks, and how the code answers:

| Risk | Answer |
|---|---|
| A ranking that mixes applications scored by v1 and v2 | Every score row carries its `rubric_version`; the breakdown shows it ([07-3](07-3-score-breakdown.md)). |
| The admin not realising old scores are stale | `PUT` answers with `applications_to_rescore` — how many applications still carry an older score ([`rubric.go:139`](../../internal/store/rubric.go#L139)). |
| A silent, surprise rescore changing decisions already made | Nothing rescores implicitly. A rescore is an explicit, logged action. |
| Not knowing who changed the rubric, or when | A `rubric_replaced` row in `job_post_events`, in the same transaction ([`rubric.go:146`](../../internal/store/rubric.go#L146)). |

The rejected middle ground — "editable later, but the `PUT` itself rescores
everyone" — couples a cheap edit to an expensive, decision-changing
operation, and makes a typo fix silently re-rank a shortlist.

## 2. Why version instead of overwrite

**📍 Open:** [`migrations/000003_rubric.up.sql:46`](../../migrations/000003_rubric.up.sql#L46)

A score is a **record of a decision**: "on 3 March, against these rules,
this answer earned 24 of 30." Overwriting the rules changes what that record
means without changing the record.

Concretely, suppose v1 weighted *Go knowledge* at 30 and an application
earned 24. An admin overwrites the criterion in place, weight now 10:

- the score row still says `earned 24, available 30`;
- the criterion it points at now says `weight 10`;
- the breakdown would show "24 of 30" next to a criterion worth 10, or, if
  it recomputed from the criterion, a number that was never the score.

Neither is defensible to a candidate who asks why they were rejected.
Versioning keeps both halves of the record consistent: the score points at
the exact criterion row it was computed from, and that row never changes.

The two ways to model this were considered:

| | Overwrite + audit log | New rows per version (built) |
|---|---|---|
| Reading the current rubric | one `WHERE job_post_id = $1` | the highest version: a `max()` subquery |
| Explaining an old score | reconstruct the criterion from the log | join to the row the score points at |
| Foreign keys | the score's criterion is a moving target | each score points at an immutable row |

The cost of versioning is the subquery, and some duplicated rows. A rubric
is a handful of rows edited a few times in a post's life; both costs round
to nothing.

## 3. Why the old rows must be kept

**📍 Open:** [`migrations/000005_scores_events.up.sql:14`](../../migrations/000005_scores_events.up.sql#L14)

"Do not delete the old criteria" is enforced by the database, not by
discipline. `scores.criterion_id` is `ON DELETE RESTRICT`: PostgreSQL refuses
to delete a criterion while any score references it.

**What breaks if a score points at a criterion that no longer exists?** That
state cannot exist with a foreign key — which is the point. Consider the
alternatives the delete could have tried:

- **`CASCADE`** would delete the *scores* with the criterion: replacing a
  rubric would silently erase every application's score. A shortlist built
  last week would have no numbers behind it.
- **`SET NULL`** would keep scores whose criterion is unknown: "24 of 30
  for … something". The label, the kind, the keywords looked for — all of
  what makes the number explainable — would be gone.
- **No foreign key** would leave a dangling UUID: the breakdown's inner join
  would drop the row, and the totals would stop adding up.

`TestOldCriteriaOutliveARubricChange`
([`scoring_test.go:244`](../../test/integration/scoring_test.go#L244))
proves it both ways: after a rubric change the old score still reads as v1,
with v1's labels; and a direct `DELETE` of v1 fails with SQLSTATE 23503.

There is one deliberate way old criteria *do* disappear: deleting a
question cascades to criteria that read it (000003). That is only possible
on a draft, where no scores exist yet.

## 4. The replace transaction

**📍 Open:** [`internal/store/rubric.go:90`](../../internal/store/rubric.go#L90)

`ReplaceRubric`, in order, all in one transaction:

1. **Lock the post** (section 5).
2. **Check the criteria against the form** under that lock
   ([`rubric.go:120`](../../internal/store/rubric.go#L120)), so a question
   cannot be deleted between the check and the insert that refers to it.
3. **Read the current version** and add one
   ([`rubric.go:129`](../../internal/store/rubric.go#L129)).
   `coalesce(max(…), 0)` turns "no rubric yet" into 0, so the first rubric
   is version 1.
4. **Insert every criterion** in one statement with `unnest`
   ([`rubric.go:169`](../../internal/store/rubric.go#L169)) — the same
   technique as `insertAnswers` in [05-2](05-2-submit-transaction.md).
5. **Count the applications** now carrying an older score.
6. **Record the event.**

"Soft-replace" means nothing is updated or deleted: the old version stays,
the new one is added beside it, and "current" is simply the highest number.
Every read of the current rubric uses one filter,
[`currentVersionFilter`](../../internal/store/rubric.go#L24), so the
definition of "current" is written once.

If any step fails, `defer tx.Rollback` undoes the inserts, so a reader can
never observe half a version.

## 5. The lock, and the race it closes

**📍 Open:** [`internal/store/rubric.go:107`](../../internal/store/rubric.go#L107)

Step 3 is a read-then-write. Two admins saving at once would both read
`max = 2` and both insert version 3; the "current rubric" would become the
union of two lists.

`SELECT … FOR NO KEY UPDATE` on the post's row makes the second transaction
wait at step 1 until the first commits; its step 3 then reads 3 and inserts
4. `FOR NO KEY UPDATE` rather than `FOR UPDATE`, as in
[05-4](05-4-application-pipeline.md): it is the weakest lock two
replacements cannot share, and it still lets other rows reference the post.

It also conflicts with the `FOR SHARE` a submission holds, which is what
part 2 relies on: a submission scores against one complete version, never
while a replacement is half-written.

**Proving the test can fail.** `TestReplaceRubricConcurrently`
([`rubric_integration_test.go:299`](../../internal/store/rubric_integration_test.go#L299))
starts eight replacements behind a start gate. With the `FOR NO KEY UPDATE`
deleted and the test run three times, all three runs failed — 18 writers
were refused with `duplicate key value violates unique constraint
"rubric_criteria_job_post_id_rubric_version_position_key"`, and only two
versions survived. With the lock restored, eight versions, every time. The
refusal came from section 6's constraint: the backstop held, but as errors
rather than by waiting.

## 6. A migration giving criteria an order

**📍 Open:** [`migrations/000011_rubric_criteria_position.up.sql:35`](../../migrations/000011_rubric_criteria_position.up.sql#L35)

Building `GET /rubric` exposed a gap: nothing recorded the order the admin
wrote the criteria in. Every row of one version shares a `created_at`
(`now()` is the transaction's start), so a read came back in UUID order —
effectively random.

000011 adds `position`, numbered from 1 per version, with
`UNIQUE (job_post_id, rubric_version, position)`. That constraint does two
jobs:

- it makes order well-defined, and its index serves the hot query
  ("version N of this post, in order"), so 000003's two-column index
  became a redundant prefix and is dropped;
- it is the race backstop from section 5. Two transactions writing the same
  version both start at position 1, so whichever inserts second collides.

The insert numbers positions with `WITH ORDINALITY`
([`rubric.go:201`](../../internal/store/rubric.go#L201)), as the question
reorder did in [04-3](04-3-questions.md).

**Existing rows.** A `NOT NULL` column cannot be added to a table with rows
unless every row gets a value, and a unique constraint cannot be added while
duplicates exist. So the column is added nullable, backfilled with
`row_number()` in the only stable order the old rows have
([line 17](../../migrations/000011_rubric_criteria_position.up.sql#L17)),
then made `NOT NULL`, then constrained. Four test fixtures that inserted
criteria by hand now supply a position.

## 7. jsonb through pgx, both directions

**📍 Open:** [`internal/store/rubric.go:262`](../../internal/store/rubric.go#L262)

A criterion's `config` is a `json.RawMessage` in Go
([`criterion.go:28`](../../internal/domain/criterion.go#L28)) and `jsonb`
in PostgreSQL. `json.RawMessage` is `[]byte` holding JSON text that nobody
has parsed. The store never looks inside it: only the scoring package knows
each kind's shape.

### The encode side (Go → PostgreSQL)

For a single value, pgx's `JSONCodec.PlanEncode` has a case for
`json.RawMessage` specifically (pgx v5.12, `pgtype/json.go`). It **appends
the bytes as they are**, deliberately not running them through
`json.Marshal`, which would reformat them. For `jsonb` in binary format,
`JSONBCodec` prepends one byte — the jsonb format version, `1` — and then
the text. Two consequences worth knowing:

- **A nil `json.RawMessage` is sent as SQL `NULL`.** The column is
  `NOT NULL`, so an absent config must become `{}` before it reaches pgx:
  [`emptyConfig`](../../internal/store/rubric.go#L31), used at
  [`rubric.go:186`](../../internal/store/rubric.go#L186), and the handler
  normalises absent and `null` the same way (section 8).
- **There is no array encoding for `[]json.RawMessage`.** The `unnest`
  insert needs one array per column, so configs travel as a `text[]` and
  each element is cast with `t.config::jsonb`. PostgreSQL parses the text
  there, so invalid JSON would fail the statement — though the handler has
  already made that impossible.

### The scan side (PostgreSQL → Go)

`scanCriterion` scans the column straight into `&c.Config`. In
`JSONCodec.PlanScan` the target `*json.RawMessage` is not `*[]byte` (a type
switch matches exact types, and `json.RawMessage` is a distinct named type),
so pgx falls through to its generic plan: it strips the version byte and
calls `json.Unmarshal(src, &c.Config)`. `json.RawMessage` implements
`UnmarshalJSON` by **copying** the bytes — a copy matters, because pgx
reuses its read buffer for the next row.

### What survives the trip

`TestRubricConfigJSONB`
([`rubric_integration_test.go:116`](../../internal/store/rubric_integration_test.go#L116))
writes `{  "target" : 2.50 ,  "min":0 }` and reads back:

```text
{"min": 0, "target": 2.50}
```

That is output, not something to run. jsonb stores a parsed value, not
text, so what comes back is PostgreSQL's own rendering:

| Property | Survives? |
|---|---|
| The JSON value (decoded, compared with `reflect.DeepEqual`) | yes |
| Whitespace | no — normalised |
| Key order | no — shorter keys first, then bytewise |
| Duplicate keys | no — the last one wins |
| Number spelling | **yes** — jsonb numbers are `numeric`, so `2.50` stays `2.50` |
| Non-ASCII text (`café`) | yes |

So "round-trips unchanged" is true of the value and false of the bytes. No
code here compares config bytes, which is why that is fine; a test that did
`string(got) == string(written)` would fail.

## 8. Validation at the boundary

**📍 Open:** [`internal/api/rubric_handler.go:190`](../../internal/api/rubric_handler.go#L190)

The database cannot check a jsonb column's shape. A `CHECK` with
`jsonb_typeof` and `?` operators could check the top level, but would have to
restate every kind's rules in SQL, and drift from the Go that actually
scores. So the boundary is in Go, and it is the same code the engine runs.

`criteriaFromRequest`:

1. **Refuses more than 50 criteria first**
   ([line 195](../../internal/api/rubric_handler.go#L195)), so a request
   with a million entries is not validated a million times.
2. **Checks what only the API knows**: a label present and at most 120
   characters; `question_id` a UUID, lowercased; config a JSON object
   ([`normaliseConfig`](../../internal/api/rubric_handler.go#L252)).
3. **Runs `scoring.ValidateCriteria`**
   ([line 240](../../internal/api/rubric_handler.go#L240)). That is the
   heart of "validate each config against its kind": `checkCriterion` calls
   each kind's constructor, which decodes the config into that kind's own
   struct with `DisallowUnknownFields` — the exact decode the engine does
   when it scores. A config `ValidateCriteria` accepts is, by construction,
   one the engine can use.

All problems from all three go into one `ValidationError`, so the 422 lists
everything at once:

```text
{"criteria":"weights must sum to 100; they sum to 110",
 "criteria[0].config":"unknown field \"keywrods\"",
 "criteria[2].config":"must be a JSON object",
 "criteria[2].label":"is required"}
```

That is output from the live check in section 11, not a command.

**Why `null` becomes `{}`.** A `json.RawMessage` field receives the literal
bytes `null` for `"config": null`. Decoding `null` into a struct is not an
error in Go — it leaves every field zero — so a `null` config would pass a
naive check and be stored as a JSON null. Normalising to `{}` means the
engine judges `{}` and reports the missing field by name.

**Why the not-an-object check passes the raw value on.** If the handler
replaced `[1,2]` with nothing, the engine would judge `{}` and say
"target_chars is required", overwriting the true problem. Passing the
original on means both checks reach the same verdict.

## 9. Checks that need the form

**📍 Open:** [`internal/scoring/form.go:21`](../../internal/scoring/form.go#L21)

Two errors no request-only check can catch:

- **A question from another post.** The foreign key only proves the
  question exists *somewhere*. A criterion on post A reading a question of
  post B would never find an answer and silently score 0 forever.
- **A kind reading the wrong kind of question.** `experience_years` on a
  text question would fail to parse every answer.

`CheckAgainstForm` runs inside `ReplaceRubric`, under the lock. Which
question kinds each criterion kind reads is a new field on the engine's
registry, `questionKinds`
([`engine.go:138`](../../internal/scoring/engine.go#L138)), so a kind's
rules live in one place. It lives in `scoring` rather than `domain` because
it must decode `boolean_all`'s config to find its `question_ids`, and only
`scoring` knows that shape. An uppercase UUID gets its own message, because
answers are matched by exact string and an uppercase ID would never find one.

The cost of running it in the store is a second 422 round: request-shape
problems come back first, form problems once those are fixed. Loading the
form in the handler would merge them, but outside the lock — the check would
then guard nothing.

## 10. Go idioms new in this part

**A named slice type is not its underlying type to a type switch.**
`json.RawMessage` is declared `type RawMessage []byte`. `[]byte(raw)`
converts freely, but `switch v.(type) { case *[]byte: }` does not match a
`*json.RawMessage`. That is why pgx needs a dedicated encode case for it,
and why scanning takes the unmarshal path (section 7).

**Pointer fields to tell `null` from a value.** `QuestionID *string` in
`criterionRequest` ([line 52](../../internal/api/rubric_handler.go#L52))
is nil for both absent and `null`, and points at `""` for `"question_id":
""`. Here all three mean "no question", so the handler collapses them — but
the pointer is what lets it see them.

**`versionOrNull`** ([line 114](../../internal/api/rubric_handler.go#L114))
turns the domain's "0 means none" into a `*int`, which `encoding/json`
writes as `null`. The domain keeps a plain `int`; the JSON-specific
representation stays in the HTTP layer.

**`range` over an integer.** `for i := range writers` (Go 1.22) counts
0 to writers−1 — the concurrency test uses it to start goroutines.

## 11. Verification

| Command | Result |
|---|---|
| `make migrate-up` | version 11; `\d rubric_criteria` shows `position NOT NULL` and the three-column unique constraint |
| `go test -race ./internal/scoring/ ./internal/api/` | pass |
| `make test-integration` | pass, including `TestRubricConfigJSONB`, the five `TestReplaceRubric*` tests and 24 new authorization-matrix cells |
| `TestReplaceRubricConcurrently` with the lock removed, `-count=3` | 3 of 3 runs fail, 18 refused writers; with the lock, pass |
| live: `PUT` with weights totalling 95 | `422` |
| live: `PUT` with three broken criteria | one 422 listing four problems (section 8) |
| live: `experience_years` on a text question | `422`, `criteria[0].question_id` explains the kind mismatch |
| live: valid `PUT`, then `GET` | version 1, criteria in the order written, config read back |
| live: candidate `GET /rubric` | `403` |
| live: `PUT` on the now-open post | version 2 from 1, `applications_to_rescore: 1` |
| `make fmt && make lint && make test && go build ./...` | clean, 0 issues, all pass, builds |

**Trying the 422 yourself.** With the stack up (`make up`), in the VS Code
terminal, with `$ADMIN` holding an admin access token and `$JOB` and `$Q1`
a post and one of its questions:

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X PUT localhost:8080/v1/job-posts/$JOB/rubric -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' -d '{"criteria":[{"label":"Go","kind":"keyword_match","weight":95,"question_id":"'$Q1'","config":{"keywords":["go"]}}]}'
```

Sends a rubric whose weights total 95; it should print 422.
