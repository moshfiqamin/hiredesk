# Module 4, part 4 — The public job listing

This note covers `GET /v1/job-posts`: a route anyone may call (section 1),
the fields it returns and how they are computed in SQL (sections 2–3), what
`OFFSET` makes PostgreSQL physically do and how keyset pagination avoids it
(sections 4–6), the rows `OFFSET` skips or repeats when data changes
(section 7), the cursor (section 8), why `limit` is capped (section 9), and
the idioms and tests that are new here (sections 10–11).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-a-route-with-no-authentication) | A route with no authentication | [`router.go:90`](../../internal/api/router.go#L90) |
| [2](#2-what-a-listing-shows) | What a listing shows | [`job_listing_handler.go:38`](../../internal/api/job_listing_handler.go#L38) |
| [3](#3-remaining-vacancies-and-skill-tags-in-sql) | Remaining vacancies and skill tags, in SQL | [`job_listing.go:52`](../../internal/store/job_listing.go#L52) |
| [4](#4-what-offset-10000-physically-does) | What `OFFSET 10000` physically does | [`000009_job_posts_open_listing_idx.up.sql:16`](../../migrations/000009_job_posts_open_listing_idx.up.sql#L16) |
| [5](#5-keyset-pagination) | Keyset pagination | [`job_listing.go:56`](../../internal/store/job_listing.go#L56) |
| [6](#6-the-sort-key-must-never-change-and-never-tie) | The sort key must never change, and never tie | [`job_listing.go:43`](../../internal/domain/job_listing.go#L43) |
| [7](#7-the-second-offset-bug-skipped-and-repeated-rows) | The second `OFFSET` bug: skipped and repeated rows | [`job_listing_integration_test.go:272`](../../internal/store/job_listing_integration_test.go#L272) |
| [8](#8-the-cursor) | The cursor | [`cursor.go:39`](../../internal/api/cursor.go#L39) |
| [9](#9-why-limit-is-capped) | Why `limit` is capped | [`job_listing_handler.go:124`](../../internal/api/job_listing_handler.go#L124) |
| [10](#10-idioms-appearing-for-the-first-time) | Idioms appearing for the first time | [`job_listing.go:103`](../../internal/store/job_listing.go#L103) |
| [11](#11-the-tests) | The tests | [`job_listing_handler_test.go:202`](../../internal/api/job_listing_handler_test.go#L202) |

---

## 1. A route with no authentication

**📍 Open:** [`internal/api/router.go:90`](../../internal/api/router.go#L90)

```go
v1.GET("/job-posts", deps.Listings.List)
```

It is registered on `v1` itself, not on the `protected` group, so
`RequireAuth` never runs. The `Authorization` header is not read at all: a
visitor with an expired or forged token gets the same page as one with none,
and no `WWW-Authenticate` header. `TestJobListingIsPublic` sends all three.

A public route changes what the handler may do. Nothing in the response may
depend on who is asking, and nothing may reveal more than a stranger should
see. Two things enforce that:

- **The query itself filters `status = 'open'`.** Drafts and closed posts are
  never read, so no handler bug can leak them.
- **The response is its own type**, `jobListingResponse`, not
  `jobPostResponse`. A field later added to the admin view (an internal
  note, say) cannot appear here by accident.

`TestAuthorizationMatrix` has a column for the listing in which every caller
— anonymous, a deleted user's token, every role, a forged admin claim — must
get a 200 whose body is byte-for-byte what an anonymous request gets at that
moment, and which contains no draft's ID.

The route-completeness check in the matrix flagged this route the moment it
existed, because it is a `/v1` route outside `/v1/auth`. That is the check
working: even a public route has to be in the matrix.

---

## 2. What a listing shows

**📍 Open:** [`internal/api/job_listing_handler.go:38`](../../internal/api/job_listing_handler.go#L38)

```json
{"data":[{"id":"…","title":"Demo SRE","location":"Chittagong","employment_type":"full-time",
          "skill_tags":["kubernetes"],"remaining_vacancies":2}],
 "next_cursor":"eyJ2IjoxLCJ0Ijoi…"}
```

`id` is beyond the brief's list, because without it a visitor cannot open the
post. Nothing else is: no salaries, no status (it is always open), no author,
no questions (they come with `GET /v1/job-posts/:id`).

`next_cursor` is always present: a string while more posts follow, `null` on
the last page. A pointer field gives `null`; an empty string would be a value
a client has to know is special.

**Where skill tags come from.** The schema has no tags column. The skills a
post screens for are the keywords of its `keyword_match` rubric criteria, in
`config`, which module 1 left free-form. This part defines the shape:
`{"keywords": ["Go", "PostgreSQL"]}`. Tags are those keywords, lower-cased,
trimmed, de-duplicated and sorted, from the post's **current** rubric version
only.

**The trade-off.** These keywords are what the scorer looks for in a CV.
Publishing them tells a candidate exactly what to put in theirs. That is
arguably fine — a job advert that lists its required skills does the same —
but it is a decision, listed under *Still open*. The alternative is a
separate, admin-written `skill_tags` column that has nothing to do with
scoring.

---

## 3. Remaining vacancies and skill tags, in SQL

**📍 Open:** [`internal/store/job_listing.go:52`](../../internal/store/job_listing.go#L52)

The query has three steps:

```sql
WITH page AS (               -- 1. the posts on this page, and no others
	SELECT … FROM job_posts
	WHERE status = 'open' AND (created_at, id) < ($1, $2)
	ORDER BY created_at DESC, id DESC
	LIMIT $3
)
SELECT …, greatest(p.vacancies - o.offers, 0), …
FROM page p
CROSS JOIN LATERAL (SELECT count(*) … ) o       -- 2. offers, per post
LEFT JOIN LATERAL (SELECT array_agg(…) … ) t ON true   -- 3. tags, per post
```

**Why the `page` step comes first.** `LATERAL` lets a subquery refer to the
row it is joined to, so it runs once per row. Putting the `LIMIT` inside the
CTE means it runs once per row *of the page* — 21 times for a page of 20 —
never once per open post. `EXPLAIN ANALYZE` against `job_posts` showed the
plan: `Limit` innermost, then each subquery as an index-only scan of
`applications_job_post_id_status_idx` and
`rubric_criteria_job_post_id_rubric_version_idx`.

**Why not count in Go.** Looping in Go means either one query per post (21
round trips per page: the "N+1" pattern) or fetching every offer for every
post on the page and counting in memory. Both move data to Go only to throw
most of it away. In SQL, the index answers "how many offers for this post" by
counting index entries, and one number crosses the network.

**`greatest(…, 0)`.** Two offers for one vacancy is legitimate — an offer can
be declined — so `vacancies − offers` can be negative. A listing showing "−1
vacancies" is nonsense; zero is what a visitor needs to see. Only `offer`
counts: `submitted`, `shortlisted`, `interview` and `rejected` leave the
vacancy open.

**Malformed `config` must not break a public page.**
`jsonb_array_elements_text` raises an error if given anything but an array. A
single criterion with `{"keywords": "go"}` would then turn the whole listing
into a 500 for every visitor. The `CASE WHEN jsonb_typeof(…) = 'array'` at
[`job_listing.go:86`](../../internal/store/job_listing.go#L86) reads such a
criterion as having no keywords instead.

---

## 4. What `OFFSET 10000` physically does

**📍 Open:** [`migrations/000009_job_posts_open_listing_idx.up.sql:16`](../../migrations/000009_job_posts_open_listing_idx.up.sql#L16)

The natural way to page is:

```sql
SELECT … FROM job_posts WHERE status = 'open'
ORDER BY created_at DESC, id DESC
OFFSET 10000 LIMIT 20
```

It reads as "skip 10,000 rows". PostgreSQL has no way to skip them. To return
rows 10,001 to 10,020 it must **produce rows 1 to 10,020 and throw away the
first 10,000**. Physically:

1. It descends the B-tree index to the first entry in sort order.
2. It walks the leaf pages entry by entry, in order. A B-tree page knows its
   own entries and its neighbours, but **not how many entries come before
   it**, so there is no way to jump to "the 10,001st".
3. For each entry it visits the table row the entry points to, to check the
   row is visible to this transaction (MVCC: the index does not record which
   transactions can see a row) and to read the columns the query returns.
4. It hands the row up to the `Limit` node, which discards it until 10,000
   have gone by.

Measured with `EXPLAIN (ANALYZE, BUFFERS)` on a throwaway table of 200,000
posts (180,000 open), using the same index as the listing:

| Query | Rows the index scan produced | Pages touched | Execution |
|---|---|---|---|
| `OFFSET 0 LIMIT 20` | 20 | 4 | 0.034 ms |
| `OFFSET 10000 LIMIT 20` | **10,020** | 156 | 0.653 ms |
| `OFFSET 170000 LIMIT 20` | **170,020** | 2,605 | 10.729 ms |

`actual rows=170020` under a `Limit (actual rows=20)` is the whole story:
170,000 rows were found, checked and discarded. 2,605 pages is about 20 MB of
8 KB pages pulled through the buffer cache — or from disk, on a server where
they are not cached — to return twenty rows.

The cost grows in a straight line with the offset, so the deepest pages are
the slowest, and every visitor who pages deep pays it again. These numbers
are from a laptop with everything in memory; a real table with wider rows,
concurrent writers and a cold cache is slower, but the shape is the same.

---

## 5. Keyset pagination

**📍 Open:** [`internal/store/job_listing.go:56`](../../internal/store/job_listing.go#L56)

Instead of "skip N rows", say **where the last page ended** and ask for what
comes after:

```sql
WHERE status = 'open'
  AND (created_at, id) < ($1, $2)      -- the last row the client saw
ORDER BY created_at DESC, id DESC
LIMIT 21
```

The index is ordered by exactly `(created_at DESC, id DESC)`. PostgreSQL uses
the comparison as an **index condition**: it descends the tree straight to
the point `($1, $2)`, the same way it would find a single row by key, and
reads forward from there. The rows before that point are never visited.

The same table, at the same two depths:

| Query | Rows produced | Pages touched | Execution |
|---|---|---|---|
| keyset, after row 10,000 | 20 | 5 | 0.040 ms |
| keyset, after row 170,000 | 20 | 4 | 0.014 ms |

Five runs each of the deepest page, wall-clock in psql: `OFFSET` 9.6–9.9 ms,
keyset 0.11–0.16 ms. A check confirmed both queries return the identical 20
rows. The plan shows the difference in one line:

```
Index Cond: (ROW(created_at, id) < ROW('2026-07-03 07:52:00+00', '6248…'::uuid))
```

The cost of a page is now the cost of a tree descent plus one page of rows:
**the same on page 1 and page 9,000**.

**The index** is migration 9, a **partial** index:

```sql
CREATE INDEX job_posts_open_created_at_id_idx
    ON job_posts (created_at DESC, id DESC)
    WHERE status = 'open';
```

It holds only open posts, so it contains exactly the rows the listing reads,
and stays small however many drafts and closed posts pile up. PostgreSQL uses
it because the query's `WHERE status = 'open'` matches the index's predicate.

**What keyset gives up.** There is no "jump to page 47": a position can only
be reached by walking to it, or by a cursor someone already has. There is no
total count either, since counting is exactly the work keyset avoids. For an
infinite-scroll or "next page" job board, neither is missed.

---

## 6. The sort key must never change, and never tie

**📍 Open:** [`internal/domain/job_listing.go:43`](../../internal/domain/job_listing.go#L43)

A cursor is a promise: "everything before this point has been shown". That
only holds if a row cannot move across the point. So the sort key must be:

**Immutable.** `created_at` and `id` never change after a post is written.
`updated_at` would look like a better "newest first" — and would be a bug.
An admin fixes a typo, `updated_at` jumps to now, and the post moves from
page 5 to the top. A visitor on page 3 never sees it — it is now behind their
cursor — while a visitor on page 1 sees it twice. Keyset would have the
`OFFSET` bug from section 7 all over again.

**Unique.** Two posts can share a `created_at`. If the sort key were
`created_at` alone and a page ended between two posts with the same
timestamp, `created_at < $1` would skip the second one. Adding `id` as a
tie-breaker makes every position unique, and the row comparison
`(created_at, id) < ($1, $2)` means "earlier timestamp, or the same timestamp
and a smaller id". `TestListOpenJobPostsTies` inserts five posts in the same
microsecond and reads them two at a time: all five come back, once each, in
`id` order.

**The cost of this choice.** The listing is ordered by when a post was
*created*, not when it was *opened*. A post that sat in draft for a month
appears a month down the list the day it opens. The fix is an `opened_at`
column, set once by `TransitionJobPost` and never changed — immutable, so it
would be just as good a sort key. Listed under *Still open*.

---

## 7. The second `OFFSET` bug: skipped and repeated rows

**📍 Open:** [`internal/store/job_listing_integration_test.go:272`](../../internal/store/job_listing_integration_test.go#L272)

`OFFSET` is a count from the top. If anything above the client's position
changes between two requests, the count lands somewhere else.

Six open posts, P1 newest, pages of three:

```
P1  P2  P3 | P4  P5  P6
   page 1  |   page 2
```

**A post is added: a row is repeated.** The client loads page 1 and sees P1,
P2, P3. Before it asks for page 2, an admin opens NEW, which is newer than all
of them and goes to the top:

```
NEW  P1  P2 | P3  P4  P5 | P6
```

The client asks for `OFFSET 3 LIMIT 3` and gets **P3, P4, P5**. P3 is shown
twice; P6 is pushed to a page they may never request.

**A post is removed: a row is skipped.** Same start. Before page 2, P2 is
closed and leaves the listing. Everything after it moves up one place:

```
P1  P3  P4 | P5  P6
```

`OFFSET 3` now starts at **P5**. P4 moved into page 1's slots *after* the
client loaded page 1, so the client **never sees P4 at all** — no error, no
gap they could notice, a vacancy silently missing from the board.

On a job board both happen constantly: posts open and close all day, and a
visitor reading slowly through pages is exactly the visitor most likely to be
caught.

**Why keyset is immune.** The cursor is not "three rows from the top", it is
"after P3's `(created_at, id)`". NEW sorts before P3, so it is not after the
cursor: page 2 is P4, P5, P6. P2 closing changes nothing after P3: page 2 is
still P4, P5, P6. Even if **P3 itself** closes, the cursor still holds P3's
sort key, which still marks the same point in the order — the row is gone,
the position is not.

`TestListOpenJobPostsIsStableWhileDataChanges` runs all three scenarios
against PostgreSQL. In each it checks that keyset returns P4, P5, P6 — and, in
the first two, that the `OFFSET` query on the same data returns exactly the
wrong answers above: P3, P4, P5 (the repeat) and P5, P6 (the skip).

What keyset does not do: a post opened *after* the client's cursor position
— older than P3 — will appear on a later page, which is correct. A post opened
*above* it will not appear until the client starts again from page 1, which is
also correct: the client has already been past that point.

---

## 8. The cursor

**📍 Open:** [`internal/api/cursor.go:39`](../../internal/api/cursor.go#L39)

A cursor encodes the last row's sort key:

```
eyJ2IjoxLCJ0IjoiMjAyNi0xMS0xMVQxMDo1OTo0MC4yMzU2NTZaIiwiaWQiOiJhOWQ3…
  → {"v":1,"t":"2026-11-11T10:59:40.235656Z","id":"a9d78ba0-f4a5-…"}
```

**Opaque.** Base64 hides nothing — anyone can decode it, as above — but it
tells clients this is a token to hand back, not a format to build. The sort
key behind it can then change without breaking anyone who followed that.

**Versioned.** `"v":1`. If the sort key changes (to `opened_at`, say), old
cursors would hold the wrong kind of position. Bumping the version makes
them fail cleanly as invalid, instead of being misread as a position in the
new order.

**Microseconds.** At
[`cursor.go:47`](../../internal/api/cursor.go#L47) the time is written with
`time.RFC3339Nano`. PostgreSQL stores microseconds. A cursor rounded to
milliseconds would sit *after* every row created later in the same
millisecond — and those rows would come back again on the next page.
`TestCursorRoundTrip` checks a time with six decimal places survives.

**URL-safe.** `base64.RawURLEncoding` uses `-` and `_` instead of `+` and
`/`, and no `=` padding, so the cursor goes in a query string without
escaping.

**Strict when reading.** `decodeCursor` refuses anything over 256
characters, anything that is not this base64 alphabet, JSON with an unknown
key (`DisallowUnknownFields`), another version, a bad timestamp or an id that
is not a UUID. Any of them is a **400 `invalid_cursor`**, telling the client
to start from the first page. A 400 rather than a 422: the value is not a
cursor at all, and no edit to it can make it one.

**Not signed.** A forged cursor can only start the listing at another point
in public data, which any visitor could reach by paging. There is nothing for
a signature to protect. A cursor over private data would need one, or the
caller's identity checked on every page anyway.

**`next_cursor` is issued only if more rows exist.** The store fetches
`limit + 1` rows
([`job_listing.go:103`](../../internal/store/job_listing.go#L103)). If it
gets the extra one, there is a next page, and the extra row is dropped. If
not, `next_cursor` is `null`. Without this, a page of exactly `limit` rows
would always offer a cursor, and the client would make one more request for
an empty page. `TestJobListingNoCursorWhenExactlyFull` pins it.

---

## 9. Why `limit` is capped

**📍 Open:** [`internal/api/job_listing_handler.go:124`](../../internal/api/job_listing_handler.go#L124)

`limit` defaults to 20. Above 100 it is served as 100
([`job_listing_handler.go:140`](../../internal/api/job_listing_handler.go#L140)),
and anything that is not a whole number of at least 1 is a 422.

**Without a cap, `limit` is a dial anyone can turn to "everything".** This
route needs no account, so "anyone" is anyone on the internet, and so is any
script. With `?limit=1000000` and the 180,000 open posts of the test table,
one request would:

- read every open post's index entry and row;
- run both `LATERAL` subqueries 180,000 times each;
- hold all 180,000 rows in a Go slice, then encode them: at the ~187 bytes per
  listing measured on the Compose stack, about **34 MB** of JSON in memory and
  on the wire;
- hold a database connection for the whole time. The pool has 10 by default,
  so ten such requests at once leave nothing for anyone else, including
  `/readyz` and every signed-in user.

Those figures are estimates from the measurements above, not a load test, but
their shape is not in doubt: the work of an uncapped request grows with the
size of the table, and the caller chooses how much. That is a
denial-of-service handle, and no amount of keyset cleverness removes it —
keyset makes the *start* of a page cheap, not a page of unbounded length.

With the cap, the worst a single request can cost is one page of 100: one
tree descent, 101 index entries, 202 small index lookups, about 19 KB of
JSON.

**Clamp or refuse?** Both are defensible. This API serves 100 when asked for
more, as GitHub's API does, because a client asking for "as many as you
will give me" gets a useful answer, and `next_cursor` tells it there is more.
Zero, negatives and non-numbers are refused, because there is no sensible
reading of them. `strconv.Atoi` also refuses a number too large for an `int`,
so `limit=99999999999999999999` cannot wrap around to something small or
negative.

---

## 10. Idioms appearing for the first time

**📍 Open:** [`internal/store/job_listing.go:103`](../../internal/store/job_listing.go#L103)

**Row-value comparison.** `(created_at, id) < ($1, $2)` compares tuples left
to right, like words in a dictionary: true if `created_at < $1`, or if they
are equal and `id < $2`. It is the SQL standard's way of saying "after this
position in a two-column order", and it is what lets PostgreSQL use the
two-column index as a single range. The longhand
`created_at < $1 OR (created_at = $1 AND id < $2)` means the same thing and
the planner is worse at turning it into one index range.

**Typed `NULL` parameters.** On the first page there is no cursor. Rather
than two queries, the store passes `nil` for both and the SQL says
`$1::timestamptz IS NULL OR …`. The casts tell PostgreSQL the parameters'
types, which it cannot infer from `IS NULL` alone.

**`LATERAL`.** A subquery in `FROM` normally cannot see the other tables in
the same `FROM`. `LATERAL` lets it, and it runs once per row it is joined to
— a `for` loop written in SQL, run inside the database. `CROSS JOIN LATERAL`
for the offer count, which always returns a row; `LEFT JOIN LATERAL … ON true`
for the tags, so a post whose subquery finds nothing still appears.

**`array_agg(DISTINCT … ORDER BY …)`** collects values into a Postgres
array, de-duplicated and sorted, which pgx scans straight into a `[]string`.

**Named results and `min`.** `ListOpenJobPosts` declares
`(listings []domain.JobListing, more bool, err error)`, which documents what
each of three return values is. `min(limit, domain.MaxListingLimit)` uses the
built-in `min`, in Go since 1.21, with no `math` import and no conversion to
`float64`.

**`c.GetQuery`** returns the value and whether the parameter was present at
all. `c.Query` alone returns `""` for both "absent" and `?limit=`, which this
handler must tell apart: absent is the default, an empty value is an error.

---

## 11. The tests

**📍 Open:** [`internal/api/job_listing_handler_test.go:202`](../../internal/api/job_listing_handler_test.go#L202)

| Test | What it pins |
|---|---|
| [`TestJobListingIsPublic`](../../internal/api/job_listing_handler_test.go#L115) | No header, a valid token, a garbage token: all 200, no `WWW-Authenticate` |
| [`TestJobListingEnvelopeAndFields`](../../internal/api/job_listing_handler_test.go#L135) | Exactly `data` and `next_cursor`; exactly the six fields per item; `[]` and `null` for empty values |
| [`TestJobListingPagesThroughEverythingOnce`](../../internal/api/job_listing_handler_test.go#L202) | Seven posts in pages of three: every post once, in order, three pages |
| [`TestJobListingNoCursorWhenExactlyFull`](../../internal/api/job_listing_handler_test.go#L243) | No cursor to an empty page |
| [`TestJobListingLimit`](../../internal/api/job_listing_handler_test.go#L259) | Default, cap, and every invalid form, without calling the store |
| [`TestCursorRoundTrip`](../../internal/api/job_listing_handler_test.go#L300) / [`TestInvalidCursors`](../../internal/api/job_listing_handler_test.go#L328) | Microseconds and time zones survive; twelve kinds of bad cursor are refused |
| [`TestListOpenJobPostsTies`](../../internal/store/job_listing_integration_test.go#L116) | Section 6 |
| [`TestListOpenJobPostsRemainingVacancies`](../../internal/store/job_listing_integration_test.go#L163) | Only offers count; never below zero |
| [`TestListOpenJobPostsSkillTags`](../../internal/store/job_listing_integration_test.go#L216) | Normalising, current version only, malformed config ignored |
| [`TestListOpenJobPostsIsStableWhileDataChanges`](../../internal/store/job_listing_integration_test.go#L272) | Section 7, with the `OFFSET` answers alongside |
| [`TestAuthorizationMatrix`](../../test/integration/authz_test.go#L260) | Every caller gets the anonymous response, with no drafts in it |

The store tests share a database with every other test, and the listing is
global. Each test therefore places its posts in its own year far in the future
and starts reading from a cursor just above that range. That is only possible
because keyset positions can be any sort key, not just one a previous page
returned.

---

## Verified

| Check | Result |
|---|---|
| `gofmt -l .` | no files |
| `go vet ./...`, with and without `-tags integration` | clean |
| `golangci-lint run ./...` | 0 issues |
| `go test -race -cover ./...` | pass — `api` **92.9%**, `auth` **95.2%**, `domain` **94.8%** |
| `make migrate-up` | version 9 |
| `make test-integration` | store tests and the matrix pass |
| `EXPLAIN (ANALYZE, BUFFERS)`, 200,000-row table | the tables in sections 4 and 5 |
| Plan of the real query on `job_posts` | `Limit` inside the CTE; each `LATERAL` an index-only scan per page row |
| **Compose stack, no `Authorization` header** | |
| Three posts opened through the API, one draft, offers inserted by hand | the three open posts, newest first; the draft absent |
| Remaining vacancies: 3 with one offer and one interview / 1 with two offers / 2 with none | 2 / 0 / 2 |
| Skill tags from `["Go","PostgreSQL"," go "]` | `["go","postgresql"]` |
| `?limit=2`, then `&after=<next_cursor>` | two posts and a cursor; then the third and `next_cursor: null` |
| A garbage bearer token | 200 |
| `?limit=0` / `?limit=5000` / a non-cursor `after` | 422 / 200 / 400 `invalid_cursor` |
| `internal error` lines in the API log | 0 |
| Rows created by these checks | removed afterwards |
| Every `#L` link in this note | resolves to the line described; anchors in earlier notes moved by this part's code were updated |

## Still open

- **Screening keywords are public** (section 2). If that is unwanted, give
  posts a separate `skill_tags` column that admins write for the advert.
- **The listing is ordered by creation, not by opening** (section 6). An
  immutable `opened_at`, set once by `TransitionJobPost`, would fix it and is
  an equally good sort key.
- **No filtering.** Location, employment type and tag filters would each add a
  condition to the `page` step; filtering by tag would want the tags stored
  somewhere indexable rather than computed per page.
- **No caching headers.** The response is the same for everyone, so a short
  `Cache-Control: public, max-age=60` would let a CDN absorb most of the
  traffic.
- **No rate limit.** The cap bounds one request; nothing bounds how many.
- **A post with no vacancies left stays listed**, showing 0. Whether a full
  post should disappear before it is closed is a product decision.
- **`ListJobPosts`**, the admin listing from part 1, is still unbounded and
  still unused.

## Next

Module 5.
