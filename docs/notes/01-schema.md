# Module 1 — Schema

Six golang-migrate pairs: eleven tables, nineteen foreign keys, and the
constraints that make the hiring pipeline's rules the database's problem
rather than the handler's.

**How to read this.** Every section names the exact file and line to open.
Click the link, read the SQL, then come back. The migrations carry short
comments; the full reasoning lives here.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-why-timestamptz-and-never-timestamp) | `timestamptz`, never `timestamp` | [`0001:30`](../../migrations/000001_users.up.sql#L30) |
| [2](#2-check-constraints-instead-of-postgres-enums) | `CHECK` instead of an `ENUM` | [`0001:39`](../../migrations/000001_users.up.sql#L39) |
| [3](#3-uuid-instead-of-a-sequence) | `uuid` instead of a sequence | [`0001:12`](../../migrations/000001_users.up.sql#L12) |
| [4](#4-storing-a-hash-of-the-refresh-token) | Hashing the refresh token | [`0001:50`](../../migrations/000001_users.up.sql#L50) |
| [5](#5-the-delete-policy) | The delete policy | [`0004:1`](../../migrations/000004_applications.up.sql#L1) |
| [6](#6-the-functional-unique-index-on-loweremail) | Unique index on `lower(email)` | [`0001:48`](../../migrations/000001_users.up.sql#L48) |
| [7](#7-numeric-for-money-never-float) | `numeric` for money | [`0002:28`](../../migrations/000002_job_posts.up.sql#L28) |
| [8](#8-text-arrays-versus-a-child-table) | `text[]` vs a child table | [`0002:68`](../../migrations/000002_job_posts.up.sql#L68) |
| [9](#9-jsonb-instead-of-fifteen-nullable-columns) | `jsonb` vs nullable columns | [`0003:30`](../../migrations/000003_rubric.up.sql#L30) |
| [10](#10-unique-job_post_id-position-and-the-reorder-trap) | `UNIQUE (job_post_id, position)` | [`0002:89`](../../migrations/000002_job_posts.up.sql#L89) |
| [11](#11-the-duplicate-application-race) | The duplicate-application race | [`0004:39`](../../migrations/000004_applications.up.sql#L39) |
| [12](#12-append-only-event-log) | The append-only event log | [`0005:42`](../../migrations/000005_scores_events.up.sql#L42) |
| [13](#13-why-evidence_answer_id-is-nullable) | Why `evidence_answer_id` is nullable | [`0005:6`](../../migrations/000005_scores_events.up.sql#L6) |
| [14](#14-composite-indexes-and-the-prefix-rule) | Composite indexes, measured | [`0005:79`](../../migrations/000005_scores_events.up.sql#L79) |

---

## 1. Why `timestamptz`, and never `timestamp`

**📍 Open:** [`migrations/000001_users.up.sql:30`](../../migrations/000001_users.up.sql#L30)

The names mislead. `timestamp` (strictly `timestamp without time zone`) stores
no zone and performs **no conversion** — it keeps the digits it was given.
`timestamptz` does not store a zone either; it converts the input to UTC on the
way in and stores one absolute instant.

With users in Dhaka, London and Sydney that difference decides correctness. A
token created at 23:00 in Dhaka stored as `timestamp` keeps `23:00`; a Sydney
interviewer reads `23:00` and renders a Sydney evening, four hours in the
future, for something that already happened. The column cannot say which,
because the offset was discarded at write time. As `timestamptz` the same write
stores `17:00Z` and each client renders its own wall clock from one instant.

It also makes comparison mean something. `WHERE expires_at < now()` is "has
this token expired?". Under `timestamptz` that compares two instants and is
true exactly once, worldwide. Under `timestamp` it compares bare digits against
a server-local clock, so a token expires at a different real moment for each
user — and session expiry quietly depends on the server's `TZ`.

Both types are 8 bytes. There is no storage saving to trade away.

**In Go**, these scan into `time.Time`, which carries an instant plus a
location; pgx hands you UTC. Format at the edge, in the HTTP response. Never
store a formatted string.

---

## 2. `CHECK` constraints instead of Postgres enums

**📍 Open:** [`migrations/000001_users.up.sql:39`](../../migrations/000001_users.up.sql#L39)

A Postgres `ENUM` is a type, and types are harder to change than constraints.

Adding a value means `ALTER TYPE ... ADD VALUE`, which historically could not
run inside a transaction block — awkward for a tool that wraps each migration
in one. Removing or renaming a value is worse: there is no `DROP VALUE`, so the
real procedure is to create a replacement type, rewrite every column using it,
and drop the old one. Enum values also carry a fixed sort order — declaration
order — which surprises anyone expecting alphabetical.

With a `CHECK`, widening the set later is `ALTER TABLE ... DROP CONSTRAINT`
followed by adding the wider one: plain SQL, transactional, reversible in a
down migration.

The enum's real advantage is that the valid set lives in one introspectable
place. You give that up, and pay a little storage: `text` holds `'interviewer'`
as 11 bytes plus overhead where an enum holds 4.

**Every constraint in this schema is named.** An anonymous constraint gets a
generated name you did not choose and cannot rely on — and the API layer wants
to map `users_role_check` to "role must be one of…" rather than leak a driver
error. The name is the contract; see [section 11](#11-the-duplicate-application-race).

---

## 3. `uuid` instead of a sequence

**📍 Open:** [`migrations/000001_users.up.sql:12`](../../migrations/000001_users.up.sql#L12)

Three reasons, in order of how much they bite:

1. **A sequential integer leaks information.** `/users/412` tells any caller
   there are at least 412 users and that `/users/411` exists. Two signups a
   week apart reveal the growth rate. On a public hiring API, candidate `id=7`
   is visibly an early user. An id a client can guess is an id a client will
   walk.
2. **The client can generate it.** A UUID is unique without asking the
   database, so the API can mint an id before the insert, log it, and reference
   it in the same request — no `RETURNING` round trip.
3. **Sequences do not survive being split.** Every `bigserial` in every
   database starts at 1, so two sources of rows collide on any merge.

The costs are real: 16 bytes against 4 or 8, worse index locality (random
UUIDs scatter B-tree inserts where a sequence appends to one page), and an
unreadable id in logs. At this scale none of those binds; the enumeration leak
does.

`gen_random_uuid()` gives **version 4** — 122 random bits, no ordering. If
insert-heavy index churn ever shows up in profiling, UUIDv7 is the modern
answer: time-ordered, so it appends like a sequence while staying unguessable.

`pgcrypto` is enabled at [`0001:10`](../../migrations/000001_users.up.sql#L10)
although Postgres 13+ has `gen_random_uuid()` in core, so it is redundant on
the project's Postgres 16. It is kept so the migration also applies on older
servers, and because `digest()` and `crypt()` are plausibly wanted later. The
down migration deliberately leaves it installed: an extension is
database-wide, not this migration's property to remove.

---

## 4. Storing a hash of the refresh token

**📍 Open:** [`migrations/000001_users.up.sql:50`](../../migrations/000001_users.up.sql#L50)

A refresh token is a bearer credential: whoever presents it gets new access
tokens. For the life of the session it is equivalent to a password.

So picture a stolen database dump — a leaked backup, a misconfigured replica,
SQL injection, a laptop. If the table holds tokens, the attacker holds live
sessions for every user and can mint access tokens silently: no password, no
login event, nothing in the auth log. If it holds SHA-256 digests, the attacker
holds 32-byte strings the API rejects, and inverting them is not feasible.

The flow: generate a random token, send the plaintext to the client **once**,
store `sha256(token)`, and on refresh hash the incoming token and look up that
digest. That lookup is why `token_hash` carries the `UNIQUE` constraint — the
index *is* the lookup path.

**The distinction from `password_hash` one column away matters.** Passwords
need *slow* hashes — bcrypt, argon2 — deliberately expensive and per-row
salted, because users choose guessable passwords and an attacker runs a
dictionary. A refresh token is 256 bits from a CSPRNG: no dictionary exists and
there is nothing to guess, so a single fast SHA-256 suffices and salting buys
nothing. Using bcrypt here would make every refresh slow; using SHA-256 on the
password column would be a serious bug.

---

## 5. The delete policy

**📍 Open:** [`migrations/000004_applications.up.sql:1`](../../migrations/000004_applications.up.sql#L1)

Nineteen foreign keys need an answer to "what happens when the parent is
deleted?". Rather than decide per column, the schema follows one rule, written
at the top of migration 0004:

> A row that is **part of** an application cascades with it; a row that merely
> **refers to a person** is set to `NULL` so the record outlives the account; a
> row that is **evidence** refuses to let its definition be deleted underneath
> it.

| Action | Where | Why |
|---|---|---|
| `CASCADE` | `application_id`, `job_post_id`, `candidate_id`, `refresh_tokens.user_id` | containment — the child has no standing alone |
| `SET NULL` | `created_by`, `actor_id`, `assigned_by`, `evidence_answer_id` | the record outlives the person or the optional link |
| `RESTRICT` | `application_answers.question_id`, `scores.criterion_id`, `reviews.interviewer_id` | evidence — deleting the definition makes the row uninterpretable |

**The consequence is that a job post with an answered application cannot be
hard-deleted at all.** `DELETE FROM job_posts` cascades toward `questions`, the
`RESTRICT` on `application_answers.question_id` refuses, and the whole
statement aborts:

```
ERROR:  update or delete on table "questions" violates foreign key constraint
        "application_answers_question_id_fkey" on table "application_answers"
```

That is intended. `job_posts.status` already has `'closed'`; closing is the
supported path, and the schema cannot silently destroy submitted candidate
data. The same logic makes a user who has filed a review undeletable — retire
such an account by anonymising the row rather than removing it.

**Postgres indexes the referenced side of a foreign key automatically and never
the referencing side.** Every delete action therefore needs an index on the
child column, or each parent delete scans a whole table. Seven still lack one;
see [Still open](#still-open).

---

## 6. The functional unique index on `lower(email)`

**📍 Open:** [`migrations/000001_users.up.sql:48`](../../migrations/000001_users.up.sql#L48)

`citext` was the alternative. It makes case-insensitivity an invisible property
of the column: every `=`, `GROUP BY` and join silently folds case, including
where that was not intended, and the behaviour disappears from the query so a
reader cannot see it.

An index on the expression puts the rule where it can be read. It costs one
discipline: the lookup must be written `WHERE lower(email) = lower($1)`, or
Postgres cannot use the index. Confirmed with `EXPLAIN`:
`Index Scan using users_email_lower_idx`.

A plain `UNIQUE (email)` would let `Rahim@x.com` and `rahim@x.com` both exist.
The email column still stores what the user typed, capitals and all, so display
is unaffected.

**The Go idiom here is the expression index**: an index over a computed value,
usable only when the query's expression matches textually.

---

## 7. `numeric` for money, never `float`

**📍 Open:** [`migrations/000002_job_posts.up.sql:28`](../../migrations/000002_job_posts.up.sql#L28)

`float8` is IEEE-754 binary floating point, and `0.1` is not representable as a
binary fraction any more than `1/3` is in decimal. The error compounds:

| expression | result |
|---|---|
| `0.1::float8 + 0.2::float8 = 0.3::float8` | **`f`** |
| `0.1::numeric + 0.2::numeric = 0.3::numeric` | **`t`** |

A `WHERE salary_min = 50000.10` that silently matches nothing is a bug you do
not find by reading the code, and `SUM()` drifts by an amount that depends on
the order rows were summed — so the same query can return different totals
after a `VACUUM` reorders the heap.

`numeric(12, 2)` is **12 significant digits, 2 after the point**, enforced:

- `99999.999` inserts as `100000.00` — the scale **rounds silently**.
- `12345678901.00` is rejected: `numeric field overflow — must round to an
  absolute value less than 10^10`.

Rejected: **integer minor units** (a `bigint` of paisa), exact and fast, but
every read and write needs a ×100 conversion someone eventually forgets.
**Postgres's `money` type**, whose scale comes from the server's `lc_monetary`
setting, so its meaning changes with configuration — and it still stores no
currency.

**In Go, never scan a `numeric` into `float64`** — that reintroduces the exact
representation error the column type exists to prevent. Use `pgtype.Numeric`,
`shopspring/decimal`, or a string.

---

## 8. Text arrays versus a child table

**📍 Open:** [`migrations/000002_job_posts.up.sql:68`](../../migrations/000002_job_posts.up.sql#L68)

`text[]` is a real array-typed column, not a comma-joined string:

| operation | meaning |
|---|---|
| `choices[1]` | **1-indexed**, not 0 |
| `array_length(choices, 1)` | length of the first dimension |
| `choices @> ARRAY['Hybrid']` | containment — GIN-indexable |
| `unnest(choices)` | one row per element |

Two sharp edges: the declared dimension is **not enforced**
(`ARRAY[['a','b'],['c','d']]::text[]` is accepted as the same type), and the
length is unbounded.

**Use an array when** the list is small, ordered, owned entirely by its row,
read as a unit, and its elements need no attributes. **Use a child table when**
you need a foreign key into an element, per-element attributes, a constraint
per element, or independent aggregation.

`choices` qualifies: display-only strings rendered as radio buttons, meaningless
apart from their question. **But you cannot point a foreign key at an array
element.** When `application_answers` records which option was picked, there is
no referential integrity available — it stores the chosen text, and nothing
stops that drifting if a recruiter edits `choices` afterwards. That is the
price, and the signal that would push this to a child table if choice-scoring
ever matters.

**In Go**, pgx maps `text[]` to `[]string` cleanly.

---

## 9. `jsonb` instead of fifteen nullable columns

**📍 Open:** [`migrations/000003_rubric.up.sql:30`](../../migrations/000003_rubric.up.sql#L30)

`jsonb` stores JSON decoded, in a binary form, parsed once on write. `json`
keeps the text verbatim and reparses on every access:

| cast of `{"b":1, "a":2,  "a":3}` | stored as |
|---|---|
| `::jsonb` | `{"a": 3, "b": 1}` — keys sorted, duplicate dropped, whitespace gone |
| `::json` | byte-for-byte as written |

Operators: `->` keeps `jsonb`, `->>` extracts `text` (castable with
`(config->>'min_words')::int`), `@>` tests containment and is what a GIN index
accelerates.

The four criterion kinds take **disjoint** parameters — `keyword_match` wants a
keyword list, `experience_years` a threshold, `answer_depth` a word count.
Flattened into columns, every row carries fourteen `NULL`s and one value, and
nothing connects `min_words` to `kind = 'answer_depth'`: a `keyword_match` row
with `min_words = 120` and no keywords is perfectly legal and uninterpretable.
The nullable columns give the *appearance* of validation with none of the
substance, and each new kind becomes a migration plus a permanently-null column
for every existing row.

**What you give up is real**: the database no longer validates shape. A typo'd
key is stored happily — `{"min_wrods": 120}` is valid `jsonb` and a silently
broken criterion — and you cannot put a foreign key inside `jsonb`, which
matters for `boolean_all`'s question ids. Validation moves to Go, at the API
boundary, where each `kind` unmarshals into its own struct and a bad payload is
a 400 rather than a bad row. That is a discriminated union keyed on `kind`;
pgx hands you the column as bytes, so `json.RawMessage` plus a switch.

`NOT NULL DEFAULT '{}'::jsonb` is deliberate: without it, "no parameters" could
be `NULL` or `{}`, and every reader would handle both.

---

## 10. `UNIQUE (job_post_id, position)` and the reorder trap

**📍 Open:** [`migrations/000002_job_posts.up.sql:89`](../../migrations/000002_job_posts.up.sql#L89)

Without it, `ORDER BY position` is not a total order and SQL guarantees nothing
about ties. Two questions at position 2 render in whatever order the executor
produces, which can change between identical requests after an `UPDATE` moves a
row or the plan switches to a parallel scan. The visible symptom is a form that
shuffles; the invisible one is scoring aligning answers to the wrong criterion.

Scoping to `job_post_id` lets every post number its own questions from 1 —
verified: position 1 on a second post inserts fine while position 1 exists on
the first.

**The trap.** A unique constraint is checked per row, immediately, so the
obvious way to insert a question at the top cannot work:

```
UPDATE questions SET position = position + 1 WHERE job_post_id = $1;
ERROR:  duplicate key value violates unique constraint
        "questions_job_post_id_position_key"
```

Row 1 becomes 2 while 2 still exists. Whether it fails depends on the order the
executor visits rows — so it may pass in development and fail in production.

The constraint is declared as a **table constraint rather than a bare unique
index** precisely so it can be made `DEFERRABLE INITIALLY IMMEDIATE`, letting a
reordering transaction opt in with `SET CONSTRAINTS ... DEFERRED`. Tested: the
bulk shift then succeeds, and a genuine duplicate is still caught, at `COMMIT`
instead of at the statement. A unique *index* cannot be deferred.

---

## 11. The duplicate-application race

**📍 Open:** [`migrations/000004_applications.up.sql:39`](../../migrations/000004_applications.up.sql#L39)

A handler that does `SELECT`-then-`INSERT` is **check-then-act**, and there is a
window between the two in which a second request runs. Raced with two
connections 0.4 s apart:

| | constraint dropped | constraint present |
|---|---|---|
| request A sees | `0` existing | `0` existing |
| request B sees | `0` existing | `0` existing |
| result | **2 duplicate rows** | one row; B gets `23505` |

Note that **B still saw zero both times**. The check was useless; what changed
is that the second `INSERT` could not complete.

A transaction does not save you, because **there is no row to lock**.
`SELECT ... FOR UPDATE` locks rows that exist, and the whole problem is that
none does yet. Under `READ COMMITTED` — Postgres's default and pgx's — both
transactions read a snapshot without the row and neither blocks.
`SERIALIZABLE` would catch it by aborting one with a serialization failure to
retry, at a cost on every other query. An advisory lock works and is a
hand-rolled lock you must remember everywhere.

The unique index needs none of that: the check happens *inside* the B-tree
insertion, so the index entry is the lock. There is no window, because the
check and the write are one operation.

**So the handler never pre-checks.** Either
`INSERT ... ON CONFLICT (job_post_id, candidate_id) DO NOTHING` — which
returned `INSERT 0 0`, and with `RETURNING id` an empty result distinguishes
201 from 409 — or let it fail and read the error:

```
ERROR:  23505: duplicate key value violates unique constraint "applications_job_post_id_candidate_id_key"
CONSTRAINT NAME:  applications_job_post_id_candidate_id_key
```

**This is the `errors.As` case.** In Go, `errors.As(err, &pgErr)` with a
`*pgconn.PgError`, then switch on `pgErr.Code == "23505"` and
`pgErr.ConstraintName`. Never `strings.Contains(err.Error(), "duplicate")` —
that breaks on a Postgres upgrade, under a non-English locale, and cannot tell
this constraint from the five other unique constraints in the schema. It is
also why every constraint is named: `ConstraintName` only helps if you chose
the name.

---

## 12. The append-only event log

**📍 Open:** [`migrations/000005_scores_events.up.sql:42`](../../migrations/000005_scores_events.up.sql#L42)

An event is not a record of *state*; it is a claim that something happened at
an instant. Editing one does not correct data, it falsifies history. If
`status_changed → rejected` can be rewritten, the table stops answering the
only questions it exists for: who rejected this candidate, when, and what the
status was before.

For a hiring system that is not tidiness. If a candidate alleges unfairness,
the log is the evidence, and evidence that can be silently edited is not
evidence.

Structurally, `applications.status` becomes a **cache**: the authoritative
answer is the latest `status_changed` event, and the column is a denormalised
copy kept for cheap querying. If they disagree, the log wins.

Two things say so, both by absence: **no `updated_at` and no status column**.
Compare `reviews` at [`0006:37`](../../migrations/000006_review.up.sql#L37),
which *has* `updated_at` precisely because an opinion may legitimately be
revised.

**The intent is not yet enforced.** On the live table an `UPDATE` changed the
type, replaced the payload and backdated `created_at` by ten days — it
succeeded, as did a `DELETE`. The rule currently lives in the shape of the
table and the discipline of whoever writes the code. See
[Still open](#still-open).

`type` is the one `text` column with no `CHECK`, deliberately: new event types
are added by the code that emits them, and an append-only log should never
refuse to record something that did happen.

---

## 13. Why `evidence_answer_id` is nullable

**📍 Open:** [`migrations/000005_scores_events.up.sql:6`](../../migrations/000005_scores_events.up.sql#L6)

Only one of the four criterion kinds scores a single answer:

| kind | reads | single answer? |
|---|---|---|
| `answer_depth` | one specific answer | **yes** |
| `keyword_match` | the CV at `cv_url` | no |
| `experience_years` | CV, or several answers | no |
| `boolean_all` | several questions at once | no |

`NULL` therefore means "no single answer is the evidence" — a real and common
state, not missing data. `NOT NULL` would force the scorer to invent an
evidence link for three kinds out of four, and a fabricated citation on a
scorecard is worse than an absent one.

It is the same tri-state reasoning as `refresh_tokens.revoked_at` (where `NULL`
means "still valid") and `rubric_criteria.question_id`, and it mirrors that
last one deliberately: a criterion targeting no question produces a score
citing no answer.

`SET NULL` rather than `RESTRICT` follows: the link is a convenience, and
losing it must not destroy the score.

---

## 14. Composite indexes and the prefix rule

**📍 Open:** [`migrations/000005_scores_events.up.sql:79`](../../migrations/000005_scores_events.up.sql#L79)

An index on `(a, b)` serves `WHERE a = …` but not `WHERE b = …`. Column order
is a design decision: equality columns first.

Measured at 60,300 events across 200 applications, for
`WHERE application_id = $1 ORDER BY created_at DESC LIMIT 20`:

| index | plan | rows read to return 20 |
|---|---|---|
| `(application_id, created_at DESC)` | `Index Scan` | **20** |
| `(application_id, created_at)` ASC | `Index Scan Backward` | **20** |
| `(application_id)` alone | `Bitmap Heap Scan` → `Sort` (top-N heapsort, 27 kB) | **300** + 300 heap blocks |

With only `application_id` indexed, Postgres must find *every* event for the
application and sort them before it can know which twenty are newest — and that
cost grows with the application's history. The composite stores them already
ordered, so the scan stops at 20 and the work becomes independent of history
length.

**The `DESC` itself is near-cosmetic**, which is worth knowing rather than
believing otherwise: a B-tree is bidirectional, so the ASC index produced an
identical plan via `Index Scan Backward`. It is written `DESC` to document the
only ordering this table is read in. Direction genuinely matters only for a
**mixed** ordering such as `ORDER BY created_at DESC, id ASC`, which no
single-direction index can satisfy.

The same prefix rule explains why
[`questions_job_post_id_idx`](../../migrations/000002_job_posts.up.sql#L97) is
nearly redundant: the unique constraint's index already leads with
`job_post_id`. Measured, the planner does prefer the narrower standalone index
(96 kB vs 512 kB) for pure equality, so it is not useless — just the smallest
win of the three indexes on that table.

---

## Verified in this module

| Check | Result |
|---|---|
| `migrate up` (all six) | `1/u` … `6/u`, version `6`, `dirty = f`, 11 tables |
| `migrate down 1` × 6 | 11 → 9 → 7 → 5 → 4 → 2 → 0 tables |
| up → `down -all` → up | clean, `dirty = f` throughout |
| **Duplicate race, no constraint** | both saw `0`; **2 rows** created |
| **Duplicate race, with constraint** | one row; B got `23505` + constraint name |
| `ON CONFLICT DO NOTHING` | `INSERT 0 0` — silent no-op |
| Case-insensitive email collision | rejected by `users_email_lower_idx` |
| Login lookup plan | `Index Scan using users_email_lower_idx` |
| `0.1+0.2 = 0.3` float / numeric | **`f`** / **`t`** |
| `salary_min = 99999.999` | stored `100000.00` (scale rounds silently) |
| `salary_min = 12345678901.00` | rejected, `numeric field overflow` |
| Every `CHECK` (role, status × 2, vacancies, kind × 2, weight, mark) | rejects out-of-range values |
| `mark` 1 and 10 / 0 and 11 | accepted / rejected (`BETWEEN` is inclusive) |
| Bulk `position + 1` | **fails** — reorder trap reproduced |
| Same, `DEFERRABLE` + `SET CONSTRAINTS DEFERRED` | succeeds; real duplicate still caught at `COMMIT` |
| Delete a recruiter | post, event, assignment survive; FKs `NULL` |
| Delete an answered question / scored criterion | **blocked** by `RESTRICT` |
| Delete a job post with an answered application | **blocked** — close it instead |
| Delete an application | answers, scores, events, assignments, reviews → `0` |
| `UPDATE`/`DELETE` on `application_events` | **both succeeded** — not yet enforced |
| Timeline query, 3 index variants | 20 / 20 / 300 rows read |

## Still open

Deliberate gaps, each a decision rather than an oversight:

- **No currency on `salary_min`/`salary_max`.** `80000.00` is unreadable
  without knowing BDT, GBP or AUD — and the user base spans all three. Cheaper
  to add before the table holds rows whose currency nobody can recover.
- **Append-only is not enforced.** A trigger on `application_events` would also
  block the `ON DELETE CASCADE` from `applications`, since cascades fire row
  triggers; `REVOKE UPDATE, DELETE` on the API's role is the cleaner layer,
  because cascades run as the table owner.
- **Seven foreign keys have no index on the referencing side**:
  `job_posts.created_by`, `rubric_criteria.question_id`,
  `application_answers.question_id`, `scores.criterion_id`,
  `scores.evidence_answer_id`, `application_events.actor_id`,
  `interviewer_assignments.assigned_by`. The two `RESTRICT` ones scan on
  *every* attempted parent delete.
- **`updated_at` never updates itself** on any table. Either a
  `BEFORE UPDATE` trigger (correct whoever writes, including `psql`) or setting
  it in every `UPDATE` (visible in the query, easy to forget in one path).
- **No `CHECK (earned <= available)` on `scores`.** Nothing stops `earned = 99`
  of `available = 10`, which would silently break every percentage.

## Next

**Module 2** — [`02-database.md`](02-database.md): the pgx connection pool,
a `/readyz` that actually pings, migrations that run on container start, and
Adminer for reading this schema in a browser.
