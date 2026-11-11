# Module 7, part 5 — The ranking, and the N+1 it starts with

`GET /v1/job-posts/:id/ranking`: every applicant to a post, best total
first. This part is a deliberate two-step exercise. **Step 1** (this
commit) writes it the naive way, with the N+1 problem, and makes it
measurable. **Step 2** (the next commit) replaces it with one query, and
this note gains its second half: `LEFT JOIN`, `NULLS LAST`, and reading
`EXPLAIN ANALYZE` on both.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-n1-means) | **What N+1 means** | [`ranking.go:64`](../../internal/store/ranking.go#L64) |
| [2](#2-why-it-is-the-most-common-performance-bug) | **Why it is the most common performance bug** | — |
| [3](#3-the-naive-version-line-by-line) | The naive version, line by line | [`ranking.go:23`](../../internal/store/ranking.go#L23) |
| [4](#4-one-behaviour-two-implementations) | One behaviour, two implementations | [`ranking_test.go:18`](../../test/integration/ranking_test.go#L18) |
| [5](#5-make-seed-bulk) | `make seed-bulk` | [`bulk.go:44`](../../cmd/seed/bulk.go#L44) |
| [6](#6-measuring-it) | Measuring it | — |

---

## 1. What N+1 means

**📍 Open:** [`internal/store/ranking.go:64`](../../internal/store/ranking.go#L64)

One query fetches a list of N things; then, for each of them, another query
fetches something about it. 1 + N queries, where one (or two) would do.
Here: one query for the 500 applications, then 500 queries, one per
application, for its total.

Each of those 500 queries is fast — PostgreSQL answers it from
`scores_application_id_idx` in microseconds. The cost is not the work, it
is the **round trip**: the application sends the query, waits for the
network, the server parses, plans and executes, the result comes back. That
fixed cost is paid 500 times for 500 tiny answers. The database is barely
working; the request is mostly waiting.

The give-away is that the number of queries grows with the data. A page
that ran 12 queries with 10 applicants runs 502 with 500, without anyone
changing a line.

## 2. Why it is the most common performance bug

- **The code looks right.** A loop that "gets the total for this
  application" is the most natural way to write it, and reads well.
- **It is fast in development.** With five test applications on a laptop,
  where a round trip to a database in a local container takes tens of
  microseconds, the page is instant. The bug only appears with real data
  and a real network.
- **ORMs hide it.** `application.Scores` in an ORM looks like a field and
  issues a query each time it is touched. This project writes SQL by hand,
  so the loop is at least visible.
- **It degrades smoothly.** Nothing errors. The page simply gets slower as
  the post gets popular, until it times out — usually on the most important
  post, at the busiest moment.
- **It hides from per-query monitoring.** No single query is slow, so a
  "slow query log" shows nothing. Only counting queries per request finds it.

## 3. The naive version, line by line

**📍 Open:** [`internal/store/ranking.go:23`](../../internal/store/ranking.go#L23)

1. **Does the post exist?** One query, so a missing post is a 404 rather
   than an empty ranking.
2. **Every application, with its candidate's name**
   ([line 37](../../internal/store/ranking.go#L37)). Every one, not the
   first `limit`: the order depends on totals not yet known.
3. **The loop** ([line 64](../../internal/store/ranking.go#L64)): for each
   application, `SELECT sum(earned), sum(available), max(rubric_version)
   FROM scores WHERE application_id = $1`
   ([line 59](../../internal/store/ranking.go#L59)). Over no rows,
   `sum` and `max` return NULL, not 0, so an unscored application scans
   into nil pointers and gets no totals.
4. **Sort in Go** ([line 81](../../internal/store/ranking.go#L81)): by
   total descending, unscored last, then earliest submission, then ID — a
   full order, so the same data always ranks the same way.
5. **Cut to `limit`** and number the ranks.

That is `2 + N` statements: 502 for 500 applicants. Steps 4 and 5 are also
wasteful — all 500 rows travel to Go to keep 50 — but the round trips
dominate.

The handler ([`application_handler.go:671`](../../internal/api/application_handler.go#L671))
is admin only. `limit` defaults to 50 and is capped at 500, so the whole
bulk post fits on one page for measuring. Unscored entries have `null`
totals, not 0: a candidate who scored zero and one never scored are
different facts.

## 4. One behaviour, two implementations

**📍 Open:** [`test/integration/ranking_test.go:18`](../../test/integration/ranking_test.go#L18)

`TestRankApplications` pins what the ranking returns — descending totals,
ties broken by earliest submission, unscored applications included and
last, `limit` respected, a missing post not found — without caring how it
is computed. It passes against this version; step 2 must pass it
*unchanged*. That is what makes the rewrite safe: the test is the
definition of "the same result, faster".

## 5. `make seed-bulk`

**📍 Open:** [`cmd/seed/bulk.go:44`](../../cmd/seed/bulk.go#L44)

`make seed-bulk` runs the seeder in the API container with `-bulk 500`:

- one new open post, with four questions and a four-criterion rubric,
  created through the same stores the API uses;
- 500 new candidates, each submitting through
  `ApplicationStore.SubmitApplication` — so every score is a real one, with
  its evidence, written by the code under test, not rows typed into SQL;
- answers that vary with the applicant's number
  ([`bulkAnswer`](../../cmd/seed/bulk.go#L150)), so totals spread out and
  some tie;
- the last 10 applications then lose their scores
  ([line 135](../../cmd/seed/bulk.go#L135)), so the ranking has unscored
  rows to place last.

The candidates' password hash is a 60-character string shaped like bcrypt
but not one ([line 27](../../cmd/seed/bulk.go#L27)). Every password fails
against it, so these accounts cannot sign in; hashing 500 real passwords at
cost 12 would take about two minutes for nothing. Each run makes a new post
and new addresses (prefixed with the post's ID), so it can be repeated.
Like `make seed`, it refuses to run with `ENV=production`.

A run takes under a second: 500 submissions, each a short transaction.

## 6. Measuring it

What to expect on a laptop, with the API and PostgreSQL in Docker:

| | Statements per request | Time |
|---|---|---|
| Naive (this commit) | 502 | **about 30–40 ms** |
| One query (step 2) | 2 | a few milliseconds |

The naive number is lower than the "hundreds of milliseconds" often quoted,
and that is the lesson of section 2: a round trip between two containers on
one machine costs about 60 microseconds. On a deployed stack, where the
database is a network hop away (0.5–1 ms), the same 502 round trips cost
**250–500 ms** — while the single query barely changes. The *ratio* is what
to record; the absolute numbers depend on the network.

The commands are in the reply that accompanies this commit, and step 2 adds
the before-and-after numbers to the README.
