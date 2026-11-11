# Module 9, part 1 — Interviewer assignment: a privilege check, and idempotency

`POST /v1/applications/:id/assign`, `DELETE /v1/applications/:id/assign/:interviewerID`
and `GET /v1/applications/:id/assignments`, all admin only. Small endpoints
carrying two big ideas: an assignment **grants access**, so who may receive
one is a security rule; and a client will send the same request twice, so
the endpoint must not care.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-why-assignment-is-a-privilege) | Why assignment is a privilege | [`policy.go:65`](../../internal/auth/policy.go#L65) |
| [2](#2-the-check-in-the-store) | The check in the store | [`reviews.go:91`](../../internal/store/reviews.go#L91) |
| [3](#3-why-ordinary-constraints-cannot-catch-it) | **Why ordinary constraints cannot catch it — and how one can** | [`000013_assignment_role_guard.up.sql:47`](../../migrations/000013_assignment_role_guard.up.sql#L47) |
| [4](#4-what-idempotency-means) | **What idempotency means** | — |
| [5](#5-a-flaky-network-step-by-step) | **A flaky network, step by step** | — |
| [6](#6-on-conflict-do-nothing) | **`ON CONFLICT DO NOTHING`** | [`reviews.go:110`](../../internal/store/reviews.go#L110) |
| [7](#7-201-then-200-and-no-second-event) | 201, then 200, and no second event | [`reviews.go:128`](../../internal/store/reviews.go#L128) |
| [8](#8-unassignment) | Unassignment | [`reviews.go:161`](../../internal/store/reviews.go#L161) |
| [9](#9-the-tests) | The tests | [`assignment_test.go:121`](../../test/integration/assignment_test.go#L121) |
| [10](#10-verification) | Verification | — |

---

## 1. Why assignment is a privilege

**📍 Open:** [`internal/auth/policy.go:65`](../../internal/auth/policy.go#L65)

Since module 3, `CanViewApplication` lets an interviewer read exactly the
applications they are assigned to. So an `interviewer_assignments` row is
not a to-do item; it is a **grant of read access** to a candidate's answers,
CV link, score and, from part 2, the right to file a mark.

That makes "who can be assigned?" a security question. If a candidate's ID
went into that table, `GetApplication` would report `assigned = true` for
them, and every check built on it would let them read a **rival's**
application to the same post. That is privilege escalation: gaining access
through a path meant for a different role.

Note that `CanViewApplication` *itself* would not save us. Its interviewer
branch only runs for users whose role is interviewer, but the escalation
does not need that branch: once the data says "assigned", any future check
that trusts the assignment table (a query that lists "applications assigned
to me", say) is fooled. The fix belongs where the assignment is created.

## 2. The check in the store

**📍 Open:** [`internal/store/reviews.go:91`](../../internal/store/reviews.go#L91)

```sql
SELECT role FROM users WHERE id = $1 FOR SHARE
```

- **The role is read from the database**, never trusted from the request or
  the admin's client. The body carries only an ID.
- **Missing user and wrong role get the same 422**, naming
  `interviewer_id`. For an admin both mean "pick someone else", and 422 is
  the right status: the request is well formed, but a field's value breaks a
  rule. A 404 would wrongly suggest the *application* was missing.
- **It runs inside the transaction that inserts the assignment.** `FOR
  SHARE` locks the user's row until commit, so nobody can change their role
  between the check and the insert. That is the time-of-check to
  time-of-use gap from 05-3, closed with a lock.
- **The application is checked first**, so a missing application is a 404
  whatever the body says.

Why the store and not the handler? The handler can only check the *shape*
of the body (present, a UUID). Whether the ID belongs to an interviewer is a
fact only the database knows, and it must be read in the same transaction
as the write to mean anything.

## 3. Why ordinary constraints cannot catch it — and how one can

**📍 Open:** [`migrations/000013_assignment_role_guard.up.sql:47`](../../migrations/000013_assignment_role_guard.up.sql#L47)

The brief says the database cannot catch this and only the handler can. That
is true of the constraints the schema had, and it is worth understanding
exactly why. But it is not true of the database in general, and this module
adds the constraint that does catch it.

**What is special about this rule** is that it is about a property of
**another row**. "The user this row points at has role interviewer"
involves two tables. Look at what each constraint type can see:

| Constraint | What it can check | Why it misses this |
|---|---|---|
| `CHECK` | only the columns of the row being written | the assignment row has no `role` column; and PostgreSQL forbids subqueries in `CHECK`, so it cannot look the role up |
| `FOREIGN KEY (interviewer_id) REFERENCES users (id)` | that *some* users row has that `id` | a candidate's row has that id too. It proves existence, nothing about the row's contents |
| `UNIQUE` | no two rows share a value | irrelevant |
| `NOT NULL` | the value is present | irrelevant |

So with only those, the database would happily store a candidate's ID, and
the role check has to live in code that reads the other row.

**The trick that makes a foreign key check it.** A foreign key compares
*all* its columns. So copy the property into this row, pin it, and
reference the pair:

```sql
ALTER TABLE users ADD CONSTRAINT users_id_role_key UNIQUE (id, role);

ALTER TABLE interviewer_assignments
    ADD COLUMN interviewer_role text NOT NULL DEFAULT 'interviewer'
        CHECK (interviewer_role = 'interviewer');

ALTER TABLE interviewer_assignments
    ADD FOREIGN KEY (interviewer_id, interviewer_role) REFERENCES users (id, role);
```

- The `CHECK` means `interviewer_role` can only ever be `'interviewer'`.
- The foreign key means `(interviewer_id, 'interviewer')` must exist in
  `users` as an `(id, role)` pair, which is true only if that user's role
  *is* interviewer.
- A foreign key may only reference columns with a unique constraint, hence
  `UNIQUE (id, role)`. It is trivially unique, since `id` alone is; it
  exists only so it can be referenced.

I ran both escape attempts against the migrated database:

```text
INSERT … (application_id, interviewer_id) VALUES (…, <candidate>)
ERROR: violates foreign key constraint "interviewer_assignments_interviewer_is_interviewer_fkey"

INSERT … (…, interviewer_role) VALUES (…, <candidate>, 'candidate')
ERROR: violates check constraint "interviewer_assignments_interviewer_role_check"
```

**Bonus behaviour.** `ON UPDATE` is left at `NO ACTION`, so changing an
assigned interviewer's role to candidate is now **refused** while they have
assignments. A demoted user cannot keep read access to the applications
they were reviewing. Making `role` part of a unique key also makes it a
"key column" for row locking, so the `FOR KEY SHARE` lock every foreign-key
check takes blocks a concurrent role change. The race in section 2 is closed
twice.

**Why keep the store check, then?** Each layer does a different job:

- the **store check** gives the admin a useful 422 naming the field. A
  foreign-key violation would otherwise surface as a 500, though the store
  also maps that specific constraint's violation to the same 422 as a
  backstop;
- the **constraint** holds against *every* writer: a future import script,
  a migration, someone in `psql`, a new endpoint that forgets the check.

I tested each layer alone. With the store's `role != interviewer`
comparison deleted, `TestAssign_RefusesNonInterviewers` **still passed**:
the constraint caught the three users with the wrong role, and the
backstop mapping turned each violation into the same 422. (The
non-existent user is still caught by the store's "no such user" branch.) With the check
deleted *and* migration 13 removed, three of the four escalation attempts
returned **201**, and the test failed. (The fourth, an ID with no user at
all, is caught by the ordinary foreign key.)

**The general lesson** is that the statement "the database cannot enforce
X" usually means "no single built-in constraint expresses X directly". The
options, in order of preference: restructure so a declarative constraint
can express it (as here); a trigger (procedural, so it can read other rows,
but easy to disable and invisible in `\d`); or application code (flexible,
but only as strong as the discipline of every writer). Some rules really
are code-only: "an interviewer may not review a candidate they used to work
with" depends on data the schema does not model.

## 4. What idempotency means

An operation is **idempotent** if doing it twice has the same effect as
doing it once. *Effect* means the state of the system, not the response
text.

- `x = 5` is idempotent; `x = x + 1` is not.
- "Ivy is assigned to application A" is a **state**. Asserting it again
  changes nothing. "Add an assignment row" is an **action**, and doing it
  twice gives two rows, or a 409.

HTTP already defines `GET`, `PUT` and `DELETE` as idempotent and `POST` as
not. This endpoint is a `POST` (as the brief specifies) that *behaves*
idempotently, because its meaning is "make sure this interviewer is
assigned", not "create another assignment".

## 5. A flaky network, step by step

Why it is necessary: **a client can never know for certain whether a
request it sent succeeded.**

```text
 Admin's browser                    Network                     HireDesk API
 ───────────────                    ───────                     ────────────
 1. POST /assign {ivy}  ──────────────────────────────────────▶  2. insert row, insert event,
                                                                    COMMIT  ✔
                                                                 3. 201 Created ──┐
                                    4. Wi-Fi drops, response lost ✘ ◀─────────────┘
 5. after 10 s: timeout. Did it work?
    The browser cannot tell "never arrived" from "arrived, answer lost".
 6. retry: POST /assign {ivy} ────────────────────────────────▶  7. ???
```

Every client, and every HTTP library with retries, mobile apps on a bad
connection, load balancers that retry upstream, and users double-clicking,
will at some point send step 6. What happens at step 7 decides whether the
system is reliable:

| Step 7 if the endpoint is… | What the admin sees | What the data says |
|---|---|---|
| a naive INSERT, no unique key | 201 | **two assignments**; Ivy has the item twice in her queue |
| an INSERT hitting the unique key | **409 Conflict** | correct, but the client is told its request *failed*, for something that worked. The UI shows an error, the admin tries again, gets another 409 |
| **idempotent** | 200 | correct: one assignment, one event |

Only the idempotent version lets the client follow the simplest possible
rule: **if unsure, send it again.** That is why the brief asks for 200, not
409, on a repeat.

## 6. `ON CONFLICT DO NOTHING`

**📍 Open:** [`internal/store/reviews.go:110`](../../internal/store/reviews.go#L110)

```sql
INSERT INTO interviewer_assignments (application_id, interviewer_id, assigned_by)
VALUES ($1, $2, $3)
ON CONFLICT ON CONSTRAINT interviewer_assignments_application_id_interviewer_id_key DO NOTHING
RETURNING id
```

**What it does.** If inserting would violate that unique constraint, the
row is silently skipped instead of raising an error. `RETURNING` then
returns **no row**, which pgx reports as `pgx.ErrNoRows`. So one statement
answers "did I create it?": a row back means created, `ErrNoRows` means it
already existed.

**Why it is neat here:**

- **It is atomic, so race-free.** The tempting alternative is
  check-then-insert: `SELECT … WHERE app = $1 AND interviewer = $2`, insert
  if nothing. Two simultaneous requests both see nothing and both insert,
  which is the exact race from 05-3. `ON CONFLICT` has no gap: the
  decision is made inside the unique index insertion. If two transactions
  try at once, the second **waits** for the first to commit, then sees the
  conflict and does nothing. `TestAssign_Idempotent` fires ten identical
  requests at once: exactly one gets 201.
- **The unique constraint already existed** (migration 000006's comment
  says it was put there "so a double-assign is idempotent"). The schema
  already defined what a duplicate is; `ON CONFLICT` just reuses that
  definition.
- **It does not abort the transaction.** A unique violation *does*:
  in PostgreSQL every later statement in the transaction fails until
  rollback, so "catch the 23505 and carry on" is impossible without a
  savepoint. `DO NOTHING` is not an error, so the transaction carries on
  to read back the assignment.
- **Naming the constraint** (`ON CONSTRAINT …`) rather than a column list
  makes it explicit which uniqueness rule counts as "already there". A
  conflict on any *other* constraint still errors.

**The rejected alternative, `ON CONFLICT … DO UPDATE SET assigned_by =
EXCLUDED.assigned_by`** (an "upsert") would also never fail, but a repeat
would *change* the row, overwriting who made the assignment and when.
That is not idempotent, and it rewrites the record of who actually
assigned Ivy first.

## 7. 201, then 200, and no second event

**📍 Open:** [`internal/store/reviews.go:128`](../../internal/store/reviews.go#L128)

- **201 on the first call, 200 on a repeat.** Both are 2xx, so a client
  that only checks "success?" stops retrying either way. The difference is
  there for a client that cares whether *this* call did the work. The body
  is the same assignment both times.
- **The `application.assigned` event is written only when a row was
  inserted.** This is the part most often got wrong. If the event were
  appended on every call, the flaky-network retry above would leave
  "assigned Ivy" in the history **twice**, a record of something that
  happened once. Idempotency has to cover *every* side effect, the audit
  log included. The event and the row are in one transaction, so they
  cannot disagree.

## 8. Unassignment

**📍 Open:** [`internal/store/reviews.go:161`](../../internal/store/reviews.go#L161)

`DELETE …/assign/:interviewerID`, with the interviewer in the path because
a `DELETE` has no body by convention.

- **Idempotent as well**, as `DELETE` is defined to be: `DELETE … RETURNING
  id`, and no row back means "was not assigned". Either way the answer is
  204, and the state afterwards is the same: not assigned. The
  `application.unassigned` event is written only if a row was deleted.
- **A missing application is still a 404**, because that is a mistake in
  the request, not a repeat.
- **Access ends at once.** The next `GET` by that interviewer is a 404,
  because `GetApplication` reads the assignment table on every request;
  there is no cached permission to expire.
- **Their review survives.** `reviews` has its own rows (`ON DELETE
  RESTRICT` on the interviewer, from 000006); unassigning does not touch
  it, and admins still see it. A mark that informed a decision must not
  vanish because someone was later taken off the case.
- **Locking with part 2.** A review being filed holds the assignment row
  `FOR SHARE`; the `DELETE` needs a row lock that conflicts with it, so it
  waits for the review to commit. An admin cannot pull the assignment out
  from under a review mid-write.

## 9. The tests

**📍 Open:** [`test/integration/assignment_test.go:121`](../../test/integration/assignment_test.go#L121)

| Test | Proves |
|---|---|
| `TestAssign_RefusesNonInterviewers` | the rival candidate, the applicant themself, an admin and a non-existent ID each get a 422 naming `interviewer_id`; no row and no event written; the rival still gets a 404 on the application; and a direct SQL insert of a candidate is refused by the composite foreign key |
| `TestAssign_Idempotent` | 201 then 200 (the second with the ID upper-cased); ten concurrent identical requests produce exactly one 201; each application ends with one assignment and one `application.assigned` event |
| `TestUnassignAndList` | names, assigner and `reviewed` in the list; unassign 204, repeat 204, one event; access revoked at once; the review kept for the admin; a missing application is a 404 |
| `TestAuthorizationMatrix` | four new columns (see part 3) |

## 10. Verification

| Command | Result |
|---|---|
| `make migrate-up`, `make migrate-down`, `make migrate-up` | 13, 12, 13 |
| `psql`: insert a candidate as an assignee; then with `interviewer_role = 'candidate'` | refused by the composite foreign key; refused by the check constraint |
| `make test-integration` | all pass |
| delete the store's `role != interviewer` comparison | `TestAssign_RefusesNonInterviewers` still passes: the constraint and its backstop mapping still give the 422 |
| delete the check **and** remove migration 13 | three attempts return 201; three assignments and three events written; test fails |
| delete `ON CONFLICT … DO NOTHING` | `TestAssign_Idempotent` fails: `repeat, in upper case: 409, want 200` |
| `curl` on the live stack as the seeded admin | assign a candidate: 422 `validation_failed` naming `interviewer_id`; assign the seeded interviewer: 201; again: 200; exactly one `application.assigned` row in the database |
