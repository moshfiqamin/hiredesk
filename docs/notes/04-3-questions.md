# Module 4, part 3 — Application questions

This note covers building a job post's application form: the endpoints
(section 1), the rule that the form is locked once a post leaves draft, and
why that is a domain decision (sections 2–4), the validation rules
(section 5), reordering and the unique-constraint collision it has to get
past (sections 6–9), and the Go and SQL idioms and tests that are new here
(sections 10–11).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-the-endpoints) | The endpoints | [`router.go:212`](../../internal/api/router.go#L212) |
| [2](#2-the-form-locks-when-the-post-opens) | The form locks when the post opens | [`question.go:46`](../../internal/domain/question.go#L46) |
| [3](#3-why-the-lock-is-a-domain-decision) | Why the lock is a domain decision | [`question.go:46`](../../internal/domain/question.go#L46) |
| [4](#4-making-the-lock-hold-under-concurrency) | Making the lock hold under concurrency | [`questions.go:293`](../../internal/store/questions.go#L293) |
| [5](#5-validation) | Validation | [`question.go:167`](../../internal/domain/question.go#L167) |
| [6](#6-the-collision-walked-through) | The collision, walked through | [`000002_job_posts.up.sql:89`](../../migrations/000002_job_posts.up.sql#L89) |
| [7](#7-the-fix-a-deferrable-constraint) | The fix: a deferrable constraint | [`000008_questions_position_deferrable.up.sql:26`](../../migrations/000008_questions_position_deferrable.up.sql#L26) |
| [8](#8-why-the-reorder-needs-a-transaction) | Why the reorder needs a transaction | [`questions.go:222`](../../internal/store/questions.go#L222) |
| [9](#9-what-the-experiments-showed) | What the experiments showed | [`questions.go:252`](../../internal/store/questions.go#L252) |
| [10](#10-go-and-sql-idioms-appearing-for-the-first-time) | Go and SQL idioms appearing for the first time | [`question_handler.go:62`](../../internal/api/question_handler.go#L62) |
| [11](#11-the-tests) | The tests | [`questions_integration_test.go:410`](../../internal/store/questions_integration_test.go#L410) |

---

## 1. The endpoints

**📍 Open:** [`internal/api/router.go:212`](../../internal/api/router.go#L212)

| Route | Does | Answers |
|---|---|---|
| `GET /v1/job-posts/:id/questions` | The form, in order, whatever the post's status | 200 `{"data":[…]}` |
| `POST /v1/job-posts/:id/questions` | Add a question; with no `position`, after the last | 201 and the question |
| `PATCH /v1/job-posts/:id/questions/:questionID` | Partial update | 200 and the question |
| `DELETE /v1/job-posts/:id/questions/:questionID` | Remove one | 204, no body |
| `PUT /v1/job-posts/:id/questions/order` | Put every question in the order given | 200 and the form |

All five are on the `admin` group, so the chain is `RequireAuth → RequireRole
→ handler`, and both IDs in the path go through `pathUUID` before the store is
called.

**"Available publicly as part of the job post detail."** The form already
comes back inside `GET /v1/job-posts/:id`, which every signed-in role may call
and which hides drafts from non-admins. This module did not change that
route, and read "publicly" as "to every role", not "without signing in".
Making it unauthenticated is a one-line move in the router, and a decision
about whether job adverts should be readable before registering.

**Why reorder is its own route.** Moving one question with `PATCH` works only
into a free slot. Swapping two needs both to move at once, which no
single-question request can express. `PUT …/order` takes the whole order:
`PUT` because sending it twice leaves the form as sending it once did.

A question that belongs to another post is a 404, not a 403: the store looks
it up `WHERE id = $1 AND job_post_id = $2`, so the wrong post simply has no
such question.

---

## 2. The form locks when the post opens

**📍 Open:** [`internal/domain/question.go:46`](../../internal/domain/question.go#L46)

```go
func CheckFormEditable(status string) error {
	if status == JobStatusDraft {
		return nil
	}
	return &FormLockedError{Status: status}
}
```

Every write — create, update, delete, reorder — calls it first. An open or
closed post answers:

```json
{"error":{"code":"form_locked",
  "message":"The application form is locked because the job post is no longer a draft. Candidates may already have answered these questions.",
  "details":{"current_status":"open","reason":"Questions can only be changed while the job post is a draft. …"}}}
```

It is a **409** for the reason part 2 gave: the request is valid, and the
post's current state refuses it. The same `PATCH` succeeded a minute ago, while
the post was a draft.

It is written as "allowed only in draft", not "refused when open or closed".
A fourth status added to the lifecycle later is therefore locked until someone
decides otherwise — deny by default, as in the transition map.
`TestCheckFormEditable` includes `""` and `"Draft"` to pin that.

The check runs before the question is looked up, so editing a question that
does not exist on an open post is a 409, not a 404. The form is locked; what
is in it does not matter.

Reading the form is never locked.

---

## 3. Why the lock is a domain decision

**📍 Open:** [`internal/domain/question.go:46`](../../internal/domain/question.go#L46)

**The database would allow every one of these edits.** A technical rule
protects data from becoming *invalid*. The schema already has one:
`application_answers.question_id` is `ON DELETE RESTRICT`
([`000004_applications.up.sql:65`](../../migrations/000004_applications.up.sql#L65)),
so a question someone has answered cannot be deleted. Beyond that, PostgreSQL
is content. Rewording a prompt, raising `min_length`, removing a choice,
adding a required question, changing `text` to `number` — every resulting row
is valid, every reference intact.

**The rule exists because the result would be unfair, not invalid.** Hiring
compares candidates. A comparison is only fair if everyone was asked the same
thing. Each kind of edit breaks that in its own way:

| Edit after opening | What happens to candidates |
|---|---|
| Reword a prompt | "Describe a project you led" becomes "Describe a Go project you led". Early and late answers sit in the same column and are read side by side, as if they answered one question. |
| Add a required question | Early applicants never saw it. Their application looks incomplete, and a rubric criterion scoring that answer gives them nothing. |
| Raise `min_length` | Early answers were accepted under a rule later answers must beat. |
| Remove a choice | An early candidate's answer is now an option that does not exist. |
| Add a choice | Later candidates could pick something earlier ones could not. |
| Change the kind | A stored text answer cannot be read as a number. |
| Delete a question | Answers to it are orphaned in meaning, even where RESTRICT keeps the rows. |
| Reorder | The mildest: the questions are the same. Locked anyway, so that the form a candidate saw can be reproduced exactly if a decision is ever challenged, and because "the form is frozen" is a rule an admin can predict, where "some edits are allowed" is not. |

The last row is a judgement call; the others are not.

**Why it locks at opening, not at the first application.** Locking only once
someone has applied sounds gentler, and it is not:

- A candidate may have loaded the form and be halfway through it. An edit then
  changes the form under them, with no application yet to show it happened.
- The admin cannot predict when the lock falls; it depends on strangers.
- `open` already means "candidates can see this". The lifecycle has a state
  for "still being written", and it is `draft`.

Closing does not unlock it either. A closed post still holds its applications,
and they must stay comparable.

**So what makes it a *domain* decision** is that it is stated in the terms of
the business — candidates, fairness, what was asked — and would hold however
the data was stored. Kept in a spreadsheet, the same rule would apply. That is
why it lives in `domain`, next to the lifecycle, as a pure function of the
status; the store only decides *when* it runs (section 4). A technical
constraint answers "can this row exist?" This answers "should anyone be
allowed to do this?"

**What an admin does instead.** Close the post and create a new one. There is
no copy endpoint yet; it is listed under *Still open*.

---

## 4. Making the lock hold under concurrency

**📍 Open:** [`internal/store/questions.go:293`](../../internal/store/questions.go#L293)

A check is only as good as the moment it describes. `lockEditableForm` reads
the status with `FOR UPDATE`:

```sql
SELECT status FROM job_posts WHERE id = $1::text::uuid FOR UPDATE
```

Without `FOR UPDATE`, an edit reads the last committed status. If an admin
is opening the post at that instant, the edit sees `draft`, goes ahead, and
changes a form that is open by the time anyone looks.
`TransitionJobPost` takes the same lock, so with it, one of the two waits for
the other:

- the edit commits first, then the post opens with the edited form; or
- the post opens first, and the edit then reads `open` and is refused.

[`TestQuestionEditWaitsForAnOpeningInProgress`](../../internal/store/questions_integration_test.go#L410)
proves it without relying on luck. It opens the post by hand in a transaction
that holds the lock, starts each kind of edit, checks the edit is still
waiting after 300 ms, commits, and checks the edit was refused. With
`FOR UPDATE` removed, the update and delete finish before the opening commits,
and the create adds a question to the opened form.

The same lock serialises every edit to one form, which the other parts rely
on: "next free position" reads `max(position)` and inserts after it, and a
reorder reads the set of questions and rewrites all their positions. Neither
may be overtaken.

This also closes an item part 2 left open: deleting a question did not wait
for the transition lock. Every delete now takes it.

---

## 5. Validation

**📍 Open:** [`internal/domain/question.go:167`](../../internal/domain/question.go#L167)

| Field | Rule |
|---|---|
| `prompt` | Trimmed; 1 to 1000 characters |
| `kind` | Exactly one of `text`, `number`, `boolean`, `choice` |
| `choices` | Required and non-empty for `choice`; **absent otherwise**. Each trimmed, non-empty, at most 200 characters; at most 50; no two the same, ignoring case |
| `min_length` | 0 to 10,000; above 0 only for `text` |
| `position` | At least 1, and not held by another question of the post |
| `required` | Defaults to true, the column's default |

**"Absent" includes an empty list.** At
[`question.go:182`](../../internal/domain/question.go#L182), a non-choice
question is refused only if it has *some* choices. `[]` is accepted as "none".
That is deliberate, and it comes from part 1's lesson about `null`: to turn a
choice question into a text one, a client must clear the choices, and `null`
decodes to the same nil as an absent key, so it cannot. An empty list can,
because `Choices` in the patch is `*[]string` (section 10). A client sends
`{"kind":"text","choices":[]}`. In the database, no choices is always `NULL`,
never `{}`: [`nilIfEmpty`](../../internal/store/questions.go#L372).

**`min_length` 0 is allowed on every kind**, for the same reason: 0 is the
column's "no minimum", and a client changing a text question with a minimum
into a number question sends `"min_length":0` alongside the new kind.

**Rules on the merged question.** As with a job post, a patch is judged after
`Apply`: changing only the kind of a choice question is refused, because the
merged question would be a text question with choices.

**Duplicate choices ignoring case** at
[`question.go:214`](../../internal/domain/question.go#L214): an answer stores
the chosen text, and a reviewer reading "remote" cannot tell which of "Remote"
and "remote" was meant.

**An explicit `position: 0` is an error.** Inside the domain, `NewQuestion`
uses 0 to mean "after the last". The request field is `*int`, and the handler
refuses a sent 0 at
[`question_handler.go:167`](../../internal/api/question_handler.go#L167)
before it can quietly become the default — absent and zero again, as in
part 1.

**A taken position is a 422, not a 409.** The unique constraint would refuse
it as a duplicate, which `translate` maps to a 409 with no field named. The
store checks first, under the post's lock, and reports it against
`position` like any other field problem:
[`checkPositionFree`](../../internal/store/questions.go#L312). The constraint
stays as the backstop.

---

## 6. The collision, walked through

**📍 Open:** [`migrations/000002_job_posts.up.sql:89`](../../migrations/000002_job_posts.up.sql#L89)

The constraint:

```sql
CONSTRAINT questions_job_post_id_position_key UNIQUE (job_post_id, position)
```

No two questions of one post may share a position. That is what makes
`ORDER BY position` a total order, so the form renders the same way every
time. It is also what makes reordering hard.

Take a form with two questions, and swap them:

| | before | wanted |
|---|---|---|
| A | 1 | 2 |
| B | 2 | 1 |

**Attempt 1: two statements.**

```sql
UPDATE questions SET position = 2 WHERE id = 'A';
-- ERROR: duplicate key value violates unique constraint
--        "questions_job_post_id_position_key"
-- DETAIL: Key (job_post_id, position)=(…, 2) already exists.
```

B still holds 2. Doing B first fails the same way, at 1. A swap is a cycle:
every question wants a slot another still holds, so **no order of
single-row updates works**. Any reorder that is not just moving into empty
slots contains such a cycle — a rotation of five questions is one big one.

**Attempt 2: one statement.**

```sql
UPDATE questions SET position = 3 - position WHERE job_post_id = '…';
```

The SQL standard says a constraint is checked when the statement finishes, so
this ought to work. In PostgreSQL it does not, for an ordinary `UNIQUE`
constraint: PostgreSQL checks a non-deferrable unique constraint **row by
row, as each new row version is written into the index**. The first row it
reaches moves into a position the second row has not left yet. The psql
experiment in section 9 shows the error. Whether it fails can even depend on
the physical order of the rows on disk, which is worse than failing every
time.

**Attempt 3: park the rows somewhere free first.** The classic workaround:

```sql
UPDATE questions SET position = -position WHERE job_post_id = '…';  -- A→-1, B→-2
UPDATE questions SET position = 2 WHERE id = 'A';
UPDATE questions SET position = 1 WHERE id = 'B';
```

Every statement succeeds. And **without a transaction** this is where things
go wrong, in four ways:

1. **Readers see the middle.** Between the statements, a candidate loading the
   post gets positions −1 and −2: the form in reverse order, from numbers no
   real form has.
2. **A failure strands it.** If the connection drops, the server restarts or a
   statement times out after the first `UPDATE`, nothing undoes it. The form
   stays at negative positions until someone notices and repairs it by hand.
3. **Two reorders interleave.** Two admins reordering at once each park and
   then place rows. Their statements interleave, and the result is either a
   collision error half way through one of them, or an order neither of them
   asked for.
4. **Anything that reads positions is fooled.** "Add a question after the
   last" computes `max(position) + 1`. Run in the middle, `max` is −1, and the
   new question is placed at position 0.

---

## 7. The fix: a deferrable constraint

**📍 Open:** [`migrations/000008_questions_position_deferrable.up.sql:26`](../../migrations/000008_questions_position_deferrable.up.sql#L26)

PostgreSQL can be told when to check a unique constraint:

| Declared as | Checked |
|---|---|
| `UNIQUE (…)` (the default) | After **each row** |
| `UNIQUE (…) DEFERRABLE INITIALLY IMMEDIATE` | At the end of **each statement**; a transaction may defer it to commit |
| `UNIQUE (…) DEFERRABLE INITIALLY DEFERRED` | At **commit**, always |

Migration 8 chooses the middle one. Everything else keeps behaving as before
— an `INSERT` into a taken position is refused at the end of its statement —
while a reorder can move rows through each other.

Module 1 anticipated this. The comment in 000002 says the constraint was
declared as a table constraint, not a bare unique index, "so it can later be
made DEFERRABLE, which bulk reordering needs". A unique *index* cannot be
deferrable; a constraint can.

PostgreSQL's `ALTER CONSTRAINT` can change deferrability only on foreign
keys, so the migration drops the constraint and adds it again. golang-migrate
sends the file as one multi-statement query, which PostgreSQL runs as one
implicit transaction, so no session ever sees the table without it.

**What deferrable costs.** A deferrable unique constraint cannot be the
target of `ON CONFLICT`, cannot be referenced by a foreign key, and the
planner cannot use it to prove a join returns at most one row. Nothing in this
schema needs any of those for questions.

With it in place, the reorder is one statement at
[`questions.go:262`](../../internal/store/questions.go#L262):

```sql
UPDATE questions q
SET position = o.position
FROM unnest($2::text[]) WITH ORDINALITY AS o(id, position)
WHERE q.id = o.id::uuid AND q.job_post_id = $1::text::uuid
```

`unnest` turns the array of IDs into rows; `WITH ORDINALITY` numbers them 1,
2, 3 in array order. Every question gets its new position in one pass, and
the constraint looks at the finished result.

---

## 8. Why the reorder needs a transaction

**📍 Open:** [`internal/store/questions.go:222`](../../internal/store/questions.go#L222)

With a deferrable constraint, the `UPDATE` alone no longer collides. The
transaction is still essential, for what surrounds it:

```
BEGIN
  SELECT status … FOR UPDATE          -- lock the post; is it a draft?
  SELECT the post's questions          -- what exists right now
  compare with the order sent          -- every question, exactly once?
  SET CONSTRAINTS … DEFERRED
  UPDATE … unnest … WITH ORDINALITY    -- move them all
  SELECT the questions again           -- the answer
COMMIT                                 -- the constraint is checked here
```

1. **The order is checked against the questions that exist.** If the read and
   the update were separate, an admin could add a question between them. The
   reorder would rewrite positions 1–5 and leave the new one at 6 — or, if it
   had been added at a position the reorder then assigns, collide. Under one
   transaction, holding the post's lock, nothing can be added in between.
2. **The draft check must still be true at the write.** Section 4.
3. **All or nothing.** If anything fails — a collision at commit, a lost
   connection, a timeout — every question keeps its old position. No form is
   ever left half reordered.
4. **No one sees the middle.** Until `COMMIT`, other sessions keep reading the
   old positions. The psql experiment showed two rows sharing position 1
   *inside* a deferred transaction; nobody outside it can see that state.
5. **`SET CONSTRAINTS … DEFERRED` exists only inside one.** Outside a
   transaction PostgreSQL ignores it, with the warning "SET CONSTRAINTS can
   only be used in transaction blocks".

[`sameQuestions`](../../internal/store/questions.go#L337) checks point 1: the
same number of IDs, every one belonging to the post, none twice. It deletes
each match from a set, so a repeated ID is simply not found the second time.

[`TestQuestionStoreReorderConcurrently`](../../internal/store/questions_integration_test.go#L319)
has two admins reorder one form in opposite directions, twenty times. Both
always succeed — the post's lock runs them one after the other — and the form
always ends in exactly one of the two orders, numbered 1 to 4.

---

## 9. What the experiments showed

**📍 Open:** [`internal/store/questions.go:252`](../../internal/store/questions.go#L252)

Run in psql against the Compose database, on throwaway tables:

| Experiment | Result |
|---|---|
| Plain `UNIQUE`, one `UPDATE` swapping two rows | `ERROR: duplicate key value … Key (pos)=(2) already exists` |
| `DEFERRABLE INITIALLY IMMEDIATE`, the same `UPDATE` | succeeds: B at 1, A at 2 |
| Deferrable, one row per statement, not deferred | first statement fails |
| Deferrable, `SET CONSTRAINTS … DEFERRED`, one row per statement | inside the transaction both rows sit at position 1; `COMMIT` succeeds with A at 1, B at 2 |
| Deferred, but left colliding at `COMMIT` | `COMMIT` fails with the duplicate-key error; nothing is saved |
| `SET CONSTRAINTS … DEFERRED` outside a transaction | warning; the next colliding statement fails |
| The real constraint after migration 8 | `condeferrable = t`, `condeferred = f` |

And against the store, with deliberate mistakes:

| Change | Result |
|---|---|
| Migration 8 rolled back, code unchanged | reorder fails: "constraint … is not deferrable" |
| Migration 8 rolled back and `SET CONSTRAINTS` removed | reorder fails: duplicate key, on reverse, swap and rotate |
| **Only `SET CONSTRAINTS` removed**, migration 8 in place | **all reorder tests still pass** |

The last row is worth being honest about. Because the reorder is a single
statement, and the constraint is already checked at the end of each
statement, `SET CONSTRAINTS … DEFERRED` is not what makes it work today — the
deferrable declaration is. The line is kept because it states the intent and
keeps the reorder correct if it ever grows a second statement (moving a
question and inserting one at its old position, say). The comment at
[`questions.go:243`](../../internal/store/questions.go#L243) says exactly this,
so no reader mistakes it for the load-bearing part.

---

## 10. Go and SQL idioms appearing for the first time

**📍 Open:** [`internal/api/question_handler.go:62`](../../internal/api/question_handler.go#L62)

**A pointer to a slice, `*[]string`.** A slice already behaves like a
reference, so why point to one? Because a slice has only two "empty" values
— `nil` and `[]string{}` — and JSON decoding cannot be relied on to keep them
apart for you. With `*[]string`:

| Body | `Choices` |
|---|---|
| no `choices` key, or `null` | `nil` — keep the stored choices |
| `"choices": []` | pointer to an empty slice — clear them |
| `"choices": ["A","B"]` | pointer to `["A","B"]` — replace them |

The same three-way reading as every other `PATCH` field, applied to a field
whose value is itself a collection.

**The command tag, `tag.RowsAffected()`.** `Exec` returns a
`pgconn.CommandTag` describing what the statement did. `DeleteQuestion` reads
it at the delete: zero rows means there was no such question on this post,
which becomes a 404. One statement, instead of a `SELECT` to check and then a
`DELETE`.

**`break` inside a `for` inside an `if`.** In `Reorder`, `break` sits inside
an `if` inside a `for`. `break` leaves the innermost `for`, `switch` or
`select` — `if` does not count — so it stops the loop at the first bad ID.
(Inside a `switch` inside a `for`, the same `break` would leave only the
`switch`, a classic Go surprise.)

**`c.Status(http.StatusNoContent)`** at
[`question_handler.go:250`](../../internal/api/question_handler.go#L250). A 204
must have no body. `c.JSON(204, x)` would try to write one; `c.Status` writes
only the status line and headers.

**`IS DISTINCT FROM`** at
[`questions.go:320`](../../internal/store/questions.go#L320). In SQL,
`id <> NULL` is not true or false but NULL, and a `WHERE` treats NULL as
false — so it would match nothing. `IS DISTINCT FROM` treats NULL as an
ordinary value: `id IS DISTINCT FROM NULL` is true for every real id. With
`NULLIF($3, '')`, one query serves both "any question" (when creating) and
"any question but this one" (when moving).

**`unnest … WITH ORDINALITY`**, section 7.

---

## 11. The tests

**📍 Open:** [`internal/store/questions_integration_test.go:410`](../../internal/store/questions_integration_test.go#L410)

| Test | What it pins |
|---|---|
| [`TestNewQuestionValidate`](../../internal/domain/question_test.go#L9) | Every rule in section 5, with boundaries |
| [`TestQuestionApplyAndValidate`](../../internal/domain/question_test.go#L101) | Kind changes: choices must be cleared or supplied, a stored `min_length` must be reset |
| [`TestCheckFormEditable`](../../internal/domain/question_test.go#L161) | Only `draft` is editable, including against bad values |
| [`TestQuestionWritesAreLockedOutsideDraft`](../../internal/api/question_handler_test.go#L309) | Each write against open and closed posts: 409 `form_locked`, the message mentions answers, the form is untouched |
| [`TestUpdateQuestion`](../../internal/api/question_handler_test.go#L374) | Absent, empty and set choices are three different requests |
| [`TestReorderQuestions`](../../internal/api/question_handler_test.go#L484) | A swap, upper-case IDs, and every way an order can fail to be "each question exactly once" |
| [`TestQuestionRoutesAreAdminOnly`](../../internal/api/question_handler_test.go#L590) | 403 / 401 on all five routes, store never called |
| [`TestQuestionStoreReorder`](../../internal/store/questions_integration_test.go#L230) | Reverse, swap, rotate and more against PostgreSQL; gaps closed; refused orders move nothing |
| [`TestQuestionStoreReorderConcurrently`](../../internal/store/questions_integration_test.go#L319) | Section 8 |
| [`TestQuestionStoreLockedOutsideDraft`](../../internal/store/questions_integration_test.go#L361) | The lock against real SQL, including a missing question on a locked form |
| [`TestQuestionEditWaitsForAnOpeningInProgress`](../../internal/store/questions_integration_test.go#L410) | Section 4 |
| [`TestAuthorizationMatrix`](../../test/integration/authz_test.go#L309) | Six new columns: list, create, create on an open post (409 `form_locked`), update, reorder, delete (204) |

---

## Verified

| Check | Result |
|---|---|
| `gofmt -l .` | no files |
| `go vet ./...`, with and without `-tags integration` | clean |
| `golangci-lint run ./...` | 0 issues |
| `go test -race -cover ./...` | pass — `api` **92.1%**, `auth` **95.2%**, `domain` **94.8%** |
| `make migrate-up` / `migrate-down` / `migrate-up` | 8 / 7 / 8 |
| `make test-integration` | store tests and the matrix pass |
| **Deliberate mistakes** | |
| `FOR UPDATE` removed from `lockEditableForm` | `TestQuestionEditWaitsForAnOpeningInProgress` fails: update and delete finish early, create edits the open form |
| `CheckFormEditable` allows open posts | domain, handler and store lock tests fail |
| Reorder without migration 8, and without deferral | section 9 |
| **Compose stack, as the seeded admin and candidate** | |
| Add a text question with `min_length` 50, a choice question with `" Remote "`, a number question | 201 each; positions 1, 2, 3; choices trimmed; `required: false` kept |
| Add with empty prompt, `choice` with no choices, `min_length` on a choice, `position: 0` | 422 naming all four fields |
| Add at a taken position | 422 on `position` |
| Reorder to number, text, choice | 200; positions 1, 2, 3 in that order |
| Reorder naming two of three | 422 on `question_ids` |
| Change the choice question to text without clearing choices / with `"choices":[]` | 422 / 200 |
| Delete one | 204; the others keep positions 2 and 3 |
| Add as the candidate | 403 |
| Open the post (after adding a rubric row by hand), then add, edit, delete, reorder | 409 `form_locked` for each, with the message and `current_status: open` |
| Candidate reads the open post | the two remaining questions, in order |
| `internal error` lines in the API log | 0 |
| Rows created by these checks | removed afterwards |
| Every `#L` link in this note | resolves to the line described; anchors in notes 00 and 02–05 that this module's code moved were updated |

## Still open

- **A post still cannot be opened through the API alone**, because rubric
  criteria have no endpoint yet. Questions do now. The rubric is next.
- **No way to start again once a post is open.** An admin who finds a mistake
  in a live form must close the post and build a new one from scratch. A
  "copy this post as a new draft" endpoint would make the lock painless.
- **Deleting a question silently deletes the rubric criteria that score it**,
  through `ON DELETE CASCADE`. On a draft that is defensible, but the response
  does not say it happened.
- **Positions are not compacted after a delete.** Display order is right; the
  numbers have gaps until the next reorder.
- **The form is visible to signed-in users only**, as before (section 1).
- **Choices are matched as text.** Renaming an option on a draft is fine;
  there is no stable ID per option for answers to point at, should one ever be
  needed.
- **No single-question `GET`.** Create returns no `Location`, since there is
  nothing for it to point at.

## Next

Rubric criteria endpoints, under the same draft-only lock, so a post can be
made ready and opened entirely through the API; then submitting an
application to an open post.
