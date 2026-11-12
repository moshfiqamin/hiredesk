# Module 4, part 1 — Job posts

This note covers creating, reading and updating job posts: the routes and who
may call them (section 1), telling "field absent" from "field set to empty" in
a JSON body (sections 2–6), validating a partial update against the stored
row (sections 7–8), checking the `:id` before it reaches the database
(section 9), money without floats (section 10), hiding drafts (section 11),
and the Go idioms and tests that are new here (sections 12–13).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-three-routes-two-guards) | Three routes, two guards | [`router.go:164`](../../internal/api/router.go#L164) |
| [2](#2-the-trap-a-string-has-no-value-that-means-not-sent) | The trap: a `string` has no value that means "not sent" | [`job_post_handler.go:73`](../../internal/api/job_post_handler.go#L73) |
| [3](#3-the-pointer-field-approach) | The pointer-field approach | [`job_post_handler.go:73`](../../internal/api/job_post_handler.go#L73) |
| [4](#4-from-request-to-patch-to-row) | From request, to patch, to row | [`job_post.go:277`](../../internal/domain/job_post.go#L277) |
| [5](#5-the-third-state-pointers-cannot-express-null) | The third state pointers cannot express: `null` | [`job_post_handler_test.go:681`](../../internal/api/job_post_handler_test.go#L681) |
| [6](#6-the-same-trap-on-post) | The same trap on `POST` | [`job_post_handler.go:50`](../../internal/api/job_post_handler.go#L50) |
| [7](#7-a-partial-update-cannot-be-validated-on-its-own) | A partial update cannot be validated on its own | [`job_post.go:310`](../../internal/domain/job_post.go#L310) |
| [8](#8-read-merge-check-write-in-one-transaction) | Read, merge, check, write — in one transaction | [`job_posts.go:122`](../../internal/store/job_posts.go#L122) |
| [9](#9-why-the-id-is-checked-before-the-query) | Why the ID is checked before the query | [`validation.go:80`](../../internal/api/validation.go#L80) |
| [10](#10-money-without-floats) | Money without floats | [`money.go:22`](../../internal/domain/money.go#L22) |
| [11](#11-drafts-are-hidden-by-the-query) | Drafts are hidden by the query | [`job_posts.go:90`](../../internal/store/job_posts.go#L90) |
| [12](#12-go-idioms-appearing-for-the-first-time) | Go idioms appearing for the first time | [`job_post.go:270`](../../internal/domain/job_post.go#L270) |
| [13](#13-the-tests) | The tests | [`job_post_handler_test.go:588`](../../internal/api/job_post_handler_test.go#L588) |

---

## 1. Three routes, two guards

**📍 Open:** [`internal/api/router.go:164`](../../internal/api/router.go#L164)

| Route | Who | Handler |
|---|---|---|
| `POST /v1/job-posts` | admin | [`Create`](../../internal/api/job_post_handler.go#L199) |
| `GET /v1/job-posts/:id` | any signed-in user | [`Get`](../../internal/api/job_post_handler.go#L276) |
| `PATCH /v1/job-posts/:id` | admin | [`Update`](../../internal/api/job_post_handler.go#L299) |

`GET` is registered on `protected`, so it runs `RequireAuth` and then the
handler. The two writes are registered on a nested group, at
[`router.go:173`](../../internal/api/router.go#L173):

```go
admin := protected.Group("", RequireRole(domain.RoleAdmin))
```

A group with an empty path adds middleware and no URL segment. The chain for
a write is therefore `RequireAuth → RequireRole → handler`, and that order is
a decision: a candidate who sends a malformed ID or a broken body to `PATCH`
gets a 403, not a 400. They learn nothing about what a valid request looks
like, and the server spends nothing reading a megabyte of JSON from someone
who was never going to be allowed to send it.

This is the first route `RequireRole` guards. Module 3 wrote and tested it
with nothing to protect.

**Rejected:** checking the role inside each handler. It works until the day a
handler is added without the check, and nothing fails when that happens. With
the group, a route cannot be registered in the wrong place without it being
visible in the route table.

---

## 2. The trap: a `string` has no value that means "not sent"

**📍 Open:** [`internal/api/job_post_handler.go:73`](../../internal/api/job_post_handler.go#L73)

Every Go type has a **zero value**, the value a variable holds before
anything is assigned to it: `""` for a string, `0` for an int, `false` for a
bool. `encoding/json` decodes by *assigning to the fields it finds keys for*
and leaving the rest alone. So for a struct written the obvious way:

```go
type updateJobPostRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}
```

these two bodies produce **exactly the same struct**:

| Body | `Title` | `Description` |
|---|---|---|
| `{"title":"New title"}` | `"New title"` | `""` — never assigned, still the zero value |
| `{"title":"New title","description":""}` | `"New title"` | `""` — assigned the empty string |

After decoding there is nothing left to tell them apart. The information was
in the *presence of the key*, and a `string` has nowhere to keep it: every
possible value of a string is a legitimate thing for the client to have sent.

The handler then has to guess, and both guesses are wrong:

- **Treat `""` as "not sent".** Now no field can ever be cleared. An admin who
  deletes a description sends `"description":""` and the old text stays.
- **Treat `""` as "set to empty".** Now `{"title":"New title"}` wipes the
  description, the location and the employment type, and sets vacancies to 0.
  A request to rename a post destroys the rest of it.

The second is the dangerous one, because it passes a casual test: send every
field, get every field back. It only shows when a client sends one.

The same holds for `int` (is `0` "zero vacancies" or "not sent"?) and for
`bool` (is `false` "turn it off" or "not sent"?).

---

## 3. The pointer-field approach

**📍 Open:** [`internal/api/job_post_handler.go:73`](../../internal/api/job_post_handler.go#L73)

A pointer has one value a plain type lacks: `nil`, "points at nothing". The
zero value of `*string` is `nil`, not `""`. So:

```go
type updateJobPostRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}
```

| Body | `Description` after decoding |
|---|---|
| `{"title":"New title"}` | `nil` — the decoder never touched it |
| `{"title":"New title","description":""}` | a pointer to `""` |
| `{"title":"New title","description":"Text"}` | a pointer to `"Text"` |

When the decoder meets a key whose field is a nil pointer, it allocates a new
value, decodes into that, and stores its address in the field. When it does
not meet the key, the field keeps its zero value, which is `nil`. Presence of
the key has become `!= nil`, and it survives decoding.

Reading it takes two steps, always in this order:

```go
if req.Description != nil {     // was it sent?
	text := *req.Description    // what was sent — may be ""
}
```

`*req.Description` on a nil pointer panics, which is why the check comes
first and why the code never dereferences outside one.

**Why pointers and not something else:**

| Alternative | Why not |
|---|---|
| `map[string]any` | Presence is easy (`_, ok := m["title"]`), and everything else is lost: every value is `any`, so each needs a type assertion, a typo in a key is silent, and the struct that documents the body is gone. |
| Decode twice: once into the struct, once into `map[string]json.RawMessage` for the key set | Works, and reads the body twice and keeps two views of it in step by hand. |
| A separate `"fields":["title"]` list in the body | Pushes the problem to every client, who must now keep the list and the values consistent. |
| A custom `Optional[T]` type | The right tool when `null` must mean something — see section 5. More machinery than this endpoint needs. |

**What it costs.** Each pointer field is a separate small allocation, and
every read needs a nil check. For a request body that is nothing. The same
trade would be wrong in a struct held by the million.

---

## 4. From request, to patch, to row

**📍 Open:** [`internal/domain/job_post.go:277`](../../internal/domain/job_post.go#L277)

The nil-or-not distinction has to survive three hops, and it is a pointer on
every one of them:

| Hop | Type | Where |
|---|---|---|
| The JSON body | `updateJobPostRequest` — pointers, with `json` tags | [`job_post_handler.go:73`](../../internal/api/job_post_handler.go#L73) |
| What the store is asked to do | `domain.JobPostPatch` — pointers, no tags | [`job_post.go:258`](../../internal/domain/job_post.go#L258) |
| The stored post | `domain.JobPost` | [`job_post.go:179`](../../internal/domain/job_post.go#L179) |

The handler copies one to the next. The two are separate types for the reason
`userResponse` is separate from `domain.User` (module 3, section 31): the
request struct is the HTTP contract, with `json.Number` salaries and `json`
tags, and the domain must not know JSON exists.

`Apply` is where "only fields present change" is finally true:

```go
if patch.Title != nil {
	p.Title = *patch.Title
}
```

One `if` per field. A nil field skips its assignment, so the stored value is
what remains.

There are two kinds of assignment in `Apply`, and the difference is worth a
look. `Title` and `Vacancies` are `NOT NULL` columns, so `JobPost` holds them
as plain values and `Apply` dereferences: `p.Title = *patch.Title`. The
nullable columns are pointers in `JobPost` too (nil is `NULL`), so at
[`job_post.go:287`](../../internal/domain/job_post.go#L287) `Apply` assigns
the pointer itself: `p.Description = patch.Description`.

So in `JobPost`, a pointer means "this column can be NULL". In `JobPostPatch`,
a pointer means "this field may not have been sent". Same syntax, two
different questions — which is exactly what section 5 runs into.

---

## 5. The third state pointers cannot express: `null`

**📍 Open:** [`internal/api/job_post_handler_test.go:681`](../../internal/api/job_post_handler_test.go#L681)

JSON has three states for a key, and a pointer has two values:

| Body | Meaning a client might intend | `*string` after decoding |
|---|---|---|
| `{}` | leave it alone | `nil` |
| `{"description":null}` | clear it — set the column to `NULL` | **`nil`** |
| `{"description":""}` | set it to the empty string | pointer to `""` |

`encoding/json` handles `null` for a pointer field by setting the pointer to
nil. That is the same nil an absent key leaves. **Absent and `null` collapse
into one**, and no amount of care in the handler can separate them
afterwards.

This API therefore documents the only honest reading: **`null` means "not
sent"**. The test at the link above pins it: a body of
`{"description":null,"salary_max":null}` produces an empty patch.

**What that costs here.** A nullable field, once set, cannot be returned to
`NULL` through `PATCH`. A description can be emptied (`""`), which is nearly
the same thing. A salary cannot: `"salary_min":0` stores zero, which is not
"negotiable". That is a real gap, and it is listed under *Still open*.

**The fix, when it is needed**, is a type that records all three states by
implementing `json.Unmarshaler`:

```go
type Optional[T any] struct {
	Set   bool // the key was present
	Value *T   // nil when the value was null
}

func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true // only called when the key is present
	if string(data) == "null" {
		return nil
	}
	o.Value = new(T)
	return json.Unmarshal(data, o.Value)
}
```

That is a sketch for reading, not code in this repository. The decoder only
calls `UnmarshalJSON` for keys it finds, so `Set` stays false for an absent
one. It was not built now because the brief asked for the pointer approach,
and because seeing where pointers stop is the reason to reach for this later.

---

## 6. The same trap on `POST`

**📍 Open:** [`internal/api/job_post_handler.go:50`](../../internal/api/job_post_handler.go#L50)

The trap is not special to `PATCH`. It appears wherever "not sent" and the
zero value should be treated differently, and `vacancies` on create is one:

| Body | Should mean |
|---|---|
| `{"title":"x"}` | one vacancy — the default |
| `{"title":"x","vacancies":0}` | an error — a post hires at least one person |

With `Vacancies int` both decode to `0`. The handler would have to either
apply the default to an explicit `0`, silently "correcting" bad input into a
post the admin did not ask for, or reject a body that only left it out. With
`Vacancies *int`, at
[`job_post_handler.go:227`](../../internal/api/job_post_handler.go#L227), nil
gets the default and a pointer to `0` reaches validation and is refused.

`Title`, by contrast, is a plain `string` in the create request, on purpose.
It is required, so "not sent" and "sent empty" are the same mistake with the
same answer, and there is nothing for a pointer to distinguish.

**The rule of thumb:** a field needs a pointer when absent and zero must be
*handled differently*. Not every optional field does.

---

## 7. A partial update cannot be validated on its own

**📍 Open:** [`internal/domain/job_post.go:310`](../../internal/domain/job_post.go#L310)

The rules are in
[`validateJobPost`](../../internal/domain/job_post.go#L316): a title of 1 to
200 characters, at least one vacancy, and `salary_min <= salary_max` when
both are set.

Take a stored post with a salary range of 50,000 to 90,000 and this request:

```json
{"salary_min": 95000}
```

Looked at alone, the body is fine. It is only wrong beside a `salary_max` the
client did not send. A rule that spans two fields cannot be checked against a
patch that carries one of them; it can only be checked against **the post as
it would be after the patch**.

That is why `Validate` is a method on `JobPost`, not on `JobPostPatch`, and
why its comment says to call it after `Apply`. `NewJobPost.Validate` calls
the same function, so a post cannot be edited into a state it could not have
been created in.

The title is counted with `utf8.RuneCountInString`, not `len`. `len` of a
string is its length in **bytes**, and `len("café")` is 5. A 200-byte limit
would allow 200 characters of English and 66 of Bengali.

**Where the rules live.** In `domain`, not in `validate` struct tags as
module 3's `registerRequest` does. Tags work on one request struct; these
rules must also run on a merged post that never was a request.

---

## 8. Read, merge, check, write — in one transaction

**📍 Open:** [`internal/store/job_posts.go:122`](../../internal/store/job_posts.go#L122)

`UpdateJobPost` does four things in order:

1. `SELECT … FOR UPDATE` — read the row and lock it
   ([`job_posts.go:140`](../../internal/store/job_posts.go#L140)).
2. `post.Apply(patch)` — merge
   ([`job_posts.go:147`](../../internal/store/job_posts.go#L147)).
3. `post.Validate()` — check the result.
4. `UPDATE` every editable column from the merged post, then commit.

**Why the lock.** The check in step 3 is made against the row read in step 1.
If another request changes the row between 1 and 4, the check was made
against a row that no longer exists. Concretely, with 50,000–100,000 stored:

| Request A: `{"salary_min":80000}` | Request B: `{"salary_max":70000}` |
|---|---|
| reads 50,000–100,000 | reads 50,000–100,000 |
| merged: 80,000–100,000 ✔ | merged: 50,000–70,000 ✔ |
| writes | writes |

Both pass. The row ends as 80,000–70,000, which neither would have been
allowed to create. `FOR UPDATE` makes B wait at step 1 until A commits; B
then reads 80,000–100,000, merges to 80,000–70,000, and is refused. Module 3,
section 41, used the same lock for refresh tokens.

`TestJobPostStoreUpdateConcurrently` runs exactly that race twenty times. With
`FOR UPDATE` removed it fails on the first round with "2 updates succeeded".

**Why the `UPDATE` writes every column.** By step 4 the merged post holds the
right value for every field, changed or not, so one fixed statement covers
every possible patch. The rejected alternatives:

| Alternative | Why not |
|---|---|
| Build the `SET` list from the patch (`SET title = $1` only when title is set) | SQL text assembled at run time: every combination is a different statement, placeholder numbers must be tracked by hand, and it is the shape of code SQL injection comes from, even when done safely. |
| `SET title = COALESCE($2, title)` | One statement and no read, and it cannot run the salary rule, which needs both values in Go. It would also make it impossible to ever write `NULL`, since `NULL` is its "not sent". |
| Read on the pool, validate, then `UPDATE` without a transaction | The race in the table above. |

**An empty patch writes nothing**
([`job_posts.go:154`](../../internal/store/job_posts.go#L154)), so
`updated_at` does not move for a request that changed nothing.

**Why this is in the store.** There is no service layer; handlers call
stores. The merge and the check must happen while the lock is held, and the
lock belongs to a transaction only the store can open. The rules themselves
are still in `domain` — the store only decides *when* they run.

---

## 9. Why the ID is checked before the query

**📍 Open:** [`internal/api/validation.go:80`](../../internal/api/validation.go#L80)

`pathUUID` reads `:id`, and answers 400 with the code `invalid_id` unless
[`isUUID`](../../internal/api/validation.go#L96) accepts it. The handler
never calls the store for an ID that fails.

The `id` column is a `uuid`. Asked for `WHERE id = 'abc'`, PostgreSQL does
not answer "no rows" — it raises an error, SQLSTATE `22P02`, because `'abc'`
cannot be converted to the column's type. Without the check, something has
to deal with that. The reasons to deal with it *before* the query:

1. **It is the client's mistake, and the status should say so.** `/job-posts/abc`
   can never succeed, however often it is retried. That is what 400 means.
   Left to the database, the error is either an unhandled 500 — which blames
   the server and pages someone — or it is translated to 404, which tells the
   client the request was fine and the record is merely absent.
2. **A database round trip is the most expensive thing a request does.** A
   connection is taken from the pool, which is a small fixed number of them,
   a network round trip is made, and a statement is parsed — to learn
   something a short loop in Go can decide with no I/O. A client hammering malformed IDs would
   otherwise be spending the pool that real requests need.
3. **An error raised inside a transaction poisons it.** After any error,
   PostgreSQL refuses every further statement in that transaction until it is
   rolled back. `UpdateJobPost` runs in one. Rejecting bad input before
   `Begin` keeps "the input was malformed" from ever looking like "the
   transaction failed".
4. **Nothing the client typed reaches the logs or the driver as an error.**
   The 22P02 message quotes the offending value. Handled in the handler, the
   value is neither logged nor echoed in the response.
5. **Defence in depth.** The queries use placeholders, so `1' OR '1'='1`
   was never going to be executed as SQL. The check means it is not even sent
   as a parameter. Two independent barriers are better than one that is
   believed to be perfect.

**What it does not replace.** The store still casts `$1::text::uuid` and
still maps `22P02` to `ErrNotFound`
([`translateByID`](../../internal/store/job_posts.go#L441)). A store method
can be called by code that is not this handler, and it must be safe on its
own.

**What `isUUID` checks.** The canonical text form only: 36 characters,
hyphens at positions 8, 13, 18 and 23, hexadecimal digits elsewhere, either
case. It deliberately does *not* check the version or variant bits.
PostgreSQL's `uuid` type stores any 128 bits, and the test fixtures use IDs
such as `00000000-0000-4000-8000-00000000000a`; a stricter check would refuse
IDs the database holds.

| Alternative | Why not |
|---|---|
| `uuid.Parse` from `github.com/google/uuid` | Already in `go.sum` as an indirect dependency. It also accepts `{…}`, `urn:uuid:…` and 32 hex digits with no hyphens, so "valid" would mean four spellings of every URL. Making it a direct dependency for one shape check is not worth it. |
| The validator's `uuid` tag | Lower case only, and reaching for a reflection-based validator to test one string is indirect. |
| A regular expression | Correct and slower, and harder to read than the loop. |

**Not changed:** `GET /v1/applications/:id` still answers 404 for a malformed
ID, as module 3 left it. That choice is pinned by the authorization matrix
and by decision D1, and changing it was not part of this module.

---

## 10. Money without floats

**📍 Open:** [`internal/domain/money.go:22`](../../internal/domain/money.go#L22)

The columns are `numeric(12, 2)`, chosen in module 1 so that a salary read
back is the salary written. That promise has to hold in Go and in JSON too,
and `float64` breaks it: 0.1 has no exact binary representation, so
`85000.10` becomes `85000.100000000005820766…` the moment it is a float.

`Money` is an `int64` count of **minor units** — 8500010 for 85000.10.
Integers are exact, and `min <= max` is one integer comparison.

The value never passes through a float at any hop:

| Hop | How |
|---|---|
| JSON → Go | The request field is `*json.Number` ([`job_post_handler.go:60`](../../internal/api/job_post_handler.go#L60)). `json.Number` is a string holding the digits exactly as sent. The default, decoding into `float64`, would have rounded before any code ran. |
| text → `Money` | [`ParseMoney`](../../internal/domain/money.go#L28) splits on the point and parses integers. It refuses a sign, an exponent, a third decimal place, and an eleventh digit before the point — the last because `numeric(12, 2)` would overflow and turn bad input into a 500. |
| Go → PostgreSQL | `$6::bigint::numeric / 100` ([`job_posts.go:64`](../../internal/store/job_posts.go#L64)). The division is done by PostgreSQL in `numeric` arithmetic, which is exact. |
| PostgreSQL → Go | `(salary_min * 100)::bigint` ([`job_posts.go:22`](../../internal/store/job_posts.go#L22)). |
| Go → JSON | [`moneyJSON`](../../internal/api/job_post_handler.go#L172) renders `85000.10` as a `json.Number`, which the encoder writes without quotes. |

A client may send `85000`, `85000.5`, `85000.50` or the string `"85000.50"`;
it always receives a number with two decimal places.

**Rejected:** `github.com/shopspring/decimal`, the usual library. It is the
right choice once money is multiplied or divided. Here amounts are only
stored and compared, and one small file avoids a dependency.

---

## 11. Drafts are hidden by the query

**📍 Open:** [`internal/store/job_posts.go:90`](../../internal/store/job_posts.go#L90)

A draft is unfinished work. Admins see it; to everyone else it does not exist
yet, and — following decision D1 — "does not exist" is a 404 identical to a
truly missing ID.

The rule has two halves:

- **Who:** [`auth.CanViewDraftJobPosts`](../../internal/auth/policy.go#L96),
  a pure function of the user, like the three policies in module 3.
- **How:** its answer is passed to the store as `includeDrafts`, and applied
  in SQL at [`job_posts.go:97`](../../internal/store/job_posts.go#L97):

```sql
WHERE id = $1::text::uuid
  AND ($2 OR status <> 'draft')
```

When `$2` is true the `OR` is true for every row. When it is false, only a
non-draft row passes.

**Why in the SQL** rather than fetching the post and checking `Status` in the
handler: a hidden draft and a missing ID now run the same query and get the
same empty result, so they cost the same and cannot be told apart by timing.
It is also the pattern `ListJobPosts` needs, where filtering in Go would mean
reading rows only to throw them away (module 3, section 58).

`ListJobPosts` takes the same flag. No endpoint calls it yet.

---

## 12. Go idioms appearing for the first time

**📍 Open:** [`internal/domain/job_post.go:270`](../../internal/domain/job_post.go#L270)

**Comparing structs with `==`.** `IsEmpty` is `p == JobPostPatch{}`. Go
compares structs field by field when every field is comparable, and pointers
are: two pointers are equal when they point at the same variable, or are both
nil. `JobPostPatch{}` is all nils, so the comparison is true exactly when
nothing was set. Add a slice or map field to the struct and this line stops
compiling — which is the compiler saying the shortcut no longer applies.

**A one-method interface, declared by the consumer.**
[`queryer`](../../internal/store/job_posts.go#L32) has only `Query`.
`*pgxpool.Pool` and `pgx.Tx` both have that method, so
[`listQuestions`](../../internal/store/job_posts.go#L390) runs on either: on
the pool for a read, and inside the transaction for an update
([`job_posts.go:181`](../../internal/store/job_posts.go#L181)). Neither type
declares that it implements `queryer`; in Go a type satisfies an interface by
having the methods, and the interface is written where it is *used*.

**A `switch` with an initialiser and no tag.** At
[`job_post.go:321`](../../internal/domain/job_post.go#L321):

```go
switch n := utf8.RuneCountInString(title); {
case n == 0:
case n > MaxJobTitleLength:
}
```

`n := …;` declares `n` for the switch only. With no value after the
semicolon, each `case` is a boolean condition and the first true one runs —
an `if`/`else if` chain, laid out as a table.

**A pointer to a pointer.**
[`scanJobPost`](../../internal/store/job_posts.go#L422) passes
`&p.Description`, where `Description` is already a `*string`. pgx receives a
`**string` and can therefore set the *inner* pointer: to nil for `NULL`, to a
new string otherwise. A plain `*string` argument would give it a string to
fill and no way to say "there was none".

**`ptr[T]` in the tests.** Go has no `&"text"` or `&3`; the address-of
operator needs a variable. Tests that fill pointer fields use
`func ptr[T any](v T) *T { return &v }`. The parameter `v` is a variable, so
its address can be taken, and `[T any]` makes one function serve every type.

**A named type over `int64`.** `type Money int64` is a distinct type with
`int64`'s representation. `Money(100) > Money(50)` works; passing a `Money`
where an `int64` is wanted does not compile without a conversion. That is
what stops minor units being mistaken for whole ones.

---

## 13. The tests

**📍 Open:** [`internal/api/job_post_handler_test.go:588`](../../internal/api/job_post_handler_test.go#L588)

| Test | What it pins |
|---|---|
| [`TestUpdateJobPostAbsentVersusEmpty`](../../internal/api/job_post_handler_test.go#L588) | Sections 2–6, end to end: an absent key is nil in the patch and unchanged in the store; `""` and `0` are non-nil and written; `vacancies: 0` is a 422 where an absent `vacancies` is a 200; `null` is absent; `{}` changes nothing |
| [`TestJobPostApply`](../../internal/domain/job_post_test.go#L98) | The same, on `Apply` alone, with no HTTP |
| [`TestJobPostIDMustBeAUUID`](../../internal/api/job_post_handler_test.go#L530) | Nine malformed IDs get 400 `invalid_id` on `GET` and `PATCH`, the ID is not echoed, and **the store is called zero times** |
| [`TestJobPostWritesAreAdminOnly`](../../internal/api/job_post_handler_test.go#L780) | 403 for the other roles and 401 for no token, before the ID or body is read |
| [`TestCreateJobPostValidation`](../../internal/api/job_post_handler_test.go#L373) | Each rule, and that several failures are reported together |
| [`TestNewJobPostValidate`](../../internal/domain/job_post_test.go#L16) | The boundaries: 200 and 201 characters, 200 characters that are 400 bytes, min equal to max |
| [`TestParseMoney`](../../internal/domain/money_test.go#L8) | What is and is not an amount |
| [`TestJobPostStoreUpdateConcurrently`](../../internal/store/job_posts_integration_test.go#L331) | Section 8's race, against PostgreSQL |
| [`TestJobPostStoreUpdate`](../../internal/store/job_posts_integration_test.go#L217) | Partial updates in real SQL; a refused patch writes nothing, including its valid fields |
| [`TestAuthorizationMatrix`](../../test/integration/authz_test.go#L309) | Seven new columns: every caller against every job post route |

The handler tests use a fake store that records the patch it was given, so
they can assert on *nil versus non-nil* directly rather than inferring it
from a response.

The matrix needed three additions to hold writes: a request body per
endpoint, and the statuses 201, 400 and 403. Its route-completeness check is
why they were added — it failed the moment the routes existed without
columns.

---

## Verified

| Check | Result |
|---|---|
| `gofmt -l .` | no files |
| `go vet ./...`, with and without `-tags integration` | clean |
| `golangci-lint run ./...` (13 linters, integration tag on) | 0 issues |
| `go test -race -cover ./...` | pass — `api` **91.1%**, `auth` **95.2%**, `domain` **92.6%**, `config` 59.8%, `cmd/seed` 47.4%, `store` 3.0% without the integration tag |
| `make test-integration` | store tests and the matrix pass |
| **Deliberate mistakes** | |
| `FOR UPDATE` removed from `UpdateJobPost` | `TestJobPostStoreUpdateConcurrently` fails, round 0: "2 updates succeeded, want exactly 1" |
| Draft filter replaced with `OR true` | `TestJobPostStoreGet` fails, and three matrix cells: both interviewers and the candidate open the draft |
| `Validate` skipped in `UpdateJobPost` | `TestJobPostStoreUpdate` and the concurrency test fail |
| **Compose stack, as the seeded admin and candidate** | |
| `POST {"title":"  Backend Engineer  "}` | 201; title trimmed, `status: draft`, `vacancies: 1`, unset fields `null`, `questions: []` |
| `POST` with `salary_min: 85000.10`, `salary_max: "120000"`, `closes_at` at +06:00 | 201; `85000.10`, `120000.00`, `closes_at` in UTC |
| `POST` empty title, `vacancies: 0`, min above max | 422 naming all three fields |
| `POST` as a candidate | 403 |
| `GET` a draft as admin / as candidate | 200 / 404 |
| `GET /v1/job-posts/not-a-uuid`, and `/1' OR '1'='1` | 400 `invalid_id`; no "internal error" line in the API log |
| `PATCH` title, description and both salaries | 200; `vacancies`, `status`, `created_at` unchanged, `updated_at` moved |
| `PATCH {"description":""}` | 200; the row holds `''`, not `NULL`; everything else unchanged |
| `PATCH {"salary_min":95000}` against a stored maximum of 90,000 | 422 on `salary_min` |
| `PATCH {"vacancies":0}` | 422 |
| `PATCH {}` | 200; `updated_at` unchanged |
| `PATCH` as a candidate / with ID `123` as admin / no token | 403 / 400 / 401 |
| Rows created by these checks | removed afterwards |
| Every `#L` link in this note | resolves to the line described; anchors in notes 00, 02 and 03 that this module's code moved were updated |

## Still open

- **`null` cannot clear a field** (section 5). A salary, a closing date, a
  location or an employment type, once set, cannot be returned to `NULL`
  through `PATCH`. The fix is the `Optional[T]` type sketched there.
- ~~`status` cannot be changed.~~ Part 2 added `POST …/open` and
  `POST …/close`. `"status"` in a `PATCH` body is still ignored.
- **No list endpoint.** `ListJobPosts` exists and is tested, and nothing calls
  it. It has no `LIMIT`; pagination should arrive with the endpoint.
- **Questions can be read and not written.** `GET` returns them; no endpoint
  creates, edits or reorders them.
- **The salary rule is not a database constraint.** `vacancies > 0` is a
  `CHECK`; `salary_min <= salary_max` is enforced only in Go. A row written by
  hand can break it, after which any `PATCH` to that post is refused until
  the range is fixed in the same request. A migration adding the `CHECK`
  would close it.
- **`employment_type` is free text**, as the schema left it. A `CHECK` or a
  fixed list would stop `full-time`, `Full time` and `FT` coexisting.
- **No length limit on `description`, `location` or `employment_type`**
  beyond the 1 MiB cap on the whole body.
- **`closes_at` may be in the past.** Nothing checks it, since a draft is not
  yet open and the rule belongs with publishing.
- **Two admins editing the same post overwrite each other, field by field.**
  The lock makes each update consistent; it does not tell the second admin
  that the post changed since they loaded it. That needs a version number or
  an `If-Match` header.
- **`applications/:id` and `job-posts/:id` disagree on a malformed ID**: 404
  for the first, 400 for the second (section 9).
- **The store tests share one database** and are still not run in CI (module
  3, *Still open*).

## Next

Publishing and closing a post, and managing its questions; then submitting an
application to an open post.
