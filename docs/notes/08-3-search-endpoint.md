# Module 8, part 3 — The search endpoint, and proving it safe

`GET /v1/job-posts/:id/applications/search?q=` (admin only): full-text
search over a post's applications, most relevant first, each with its score,
paged, and unable to produce a 500 whatever is typed into `q`.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-plainto_tsquery-not-to_tsquery) | **`plainto_tsquery`, not `to_tsquery`** | [`search.go:53`](../../internal/store/search.go#L53) |
| [2](#2-why-the-drop-table-string-is-harmless) | **Why the `DROP TABLE` string is harmless** | [`search.go:97`](../../internal/store/search.go#L97) |
| [3](#3-why-ilike-go-would-be-wrong-twice) | **Why `ILIKE '%go%'` would be wrong, twice** | [`search_test.go:128`](../../test/integration/search_test.go#L128) |
| [4](#4-every-hostile-input-and-what-it-gets) | Every hostile input, and what it gets | [`search.go:97`](../../internal/domain/search.go#L97) |
| [5](#5-the-query-rank-a-page-then-join) | The query: rank, take a page, then join | [`search.go:74`](../../internal/store/search.go#L74) |
| [6](#6-offset-paging-here-keyset-everywhere-else) | Offset paging here, keyset everywhere else | [`application_handler.go:763`](../../internal/api/application_handler.go#L763) |
| [7](#7-the-tests) | The tests | [`search_test.go:170`](../../test/integration/search_test.go#L170) |
| [8](#8-verification) | Verification | — |

---

## 1. `plainto_tsquery`, not `to_tsquery`

**📍 Open:** [`internal/store/search.go:53`](../../internal/store/search.go#L53)

Both turn text into a `tsquery`, the search-side counterpart of a
`tsvector`. They differ in what they think the text **is**:

- **`to_tsquery`** parses its input as a small **query language**: `&`
  (and), `|` (or), `!` (not), `<->` (followed by), parentheses, `:*`
  (prefix) and quoted terms. The input must be a syntactically valid
  expression.
- **`plainto_tsquery`** treats its input as **plain words**. It ignores
  every operator character, stems the words, drops stop words, and puts `&`
  between whatever is left. It cannot fail on syntax because it has none.

**What exactly happens with `"go & postgres"`.** I ran it, and the honest
answer is that this one particular string does **not** break `to_tsquery`:

```text
to_tsquery('english', 'go & postgres')   →  'go' & 'postgr'
```

`&` is a valid operator with an operand on each side, so it parses. The
string is a 500 waiting to happen because of its **neighbours**, which any
user will type sooner or later. Each of these, sent to `to_tsquery`, made
PostgreSQL raise an error, and an error from the database becomes a 500:

| Input | `to_tsquery` |
|---|---|
| `go postgres` (two words, no operator) | `ERROR: syntax error in tsquery` |
| `go &` (trailing operator) | `ERROR: no operand in tsquery` |
| `go & & postgres` | `ERROR: syntax error in tsquery` |
| `(go` | `ERROR: syntax error in tsquery` |
| `go !postgres` | `ERROR: syntax error in tsquery` |
| `'; DROP TABLE users; --` | `ERROR: syntax error in tsquery` |
| `the and a` | `ERROR: syntax error in tsquery` |

So `to_tsquery` turns ordinary search-box typing into a 500 on the most
basic input there is: two words with a space. The same inputs through
`plainto_tsquery`:

| Input | `plainto_tsquery` |
|---|---|
| `go & postgres` | `'go' & 'postgr'` |
| `'; DROP TABLE users; --` | `'drop' & 'tabl' & 'user'` |
| `!!!???` | *(empty; NOTICE "contains only stop words or doesn't contain lexemes")* |
| `go:* \| rust !java` | `'go' & 'rust' & 'java'` |

I proved the difference in the test suite rather than asserting it: with
`plainto_tsquery` swapped for `to_tsquery` in `search.go`,
`TestSearch_HostileInput` failed four subtests (punctuation, stop words,
broken syntax and the injection string), each with `status = 500`. Swapped
back, all pass.

**What is given up.** Users cannot write `go | rust` or `postgres -mysql`.
If that were wanted, the right tool is **`websearch_to_tsquery`**
(PostgreSQL 11+): it understands search-engine syntax (`"quoted phrase"`,
`or`, `-excluded`) and, like `plainto_tsquery`, never raises a syntax error.
It is the natural upgrade if recruiters ask for operators. The rejected
alternative was escaping user input into `to_tsquery` syntax ourselves:
re-implementing a parser's quoting rules in Go is how injection bugs are
born.

## 2. Why the `DROP TABLE` string is harmless

**📍 Open:** [`internal/store/search.go:97`](../../internal/store/search.go#L97)

There are two separate protections, and it helps to know which is which.

**Protection 1: the value never becomes SQL.** The query text is a Go
constant containing `$2`:

```sql
FROM applications a, plainto_tsquery('english', $2) AS tsq
```

and the value is passed **beside** it: `tx.Query(ctx, pageQuery, jobPostID,
query, limit, offset)`. pgx uses PostgreSQL's **extended query protocol**,
which sends separate messages over the connection:

```text
Parse   statement text: "… plainto_tsquery('english', $2) …"   ← fixed, contains no user input
Bind    parameters:     $1 = '778868b9-…'
                        $2 = ''; DROP TABLE users; --'          ← bytes, typed as text
                        $3 = 20,  $4 = 0
Execute
```

PostgreSQL parses and plans the statement **before it ever sees the
parameters**. By the time `$2` arrives, the shape of the statement is
fixed: `$2` is a text value passed to a function. It cannot end a string,
start a new statement, or comment anything out, because there is no parser
left running for it to confuse. The server sees one text value whose
contents happen to spell SQL, and passes it to `plainto_tsquery`, which
searches for the words `drop`, `tabl` and `user`.

**Compare string concatenation:**

```go
// NEVER do this.
sql := "SELECT … FROM applications a, plainto_tsquery('english', '" + q + "') AS tsq WHERE …"
```

With `q = '; DROP TABLE users; --` the server receives this as **statement
text**:

```sql
SELECT … FROM applications a, plainto_tsquery('english', ''; DROP TABLE users; --') AS tsq WHERE …
```

Read it as PostgreSQL would: `''` closes the string literal empty; `;` ends
the statement; `DROP TABLE users` is a **new statement**; `--` comments out
the leftover `') AS tsq WHERE …` so the whole thing is syntactically valid.
Whether the DROP actually runs depends on details (pgx's extended protocol
refuses more than one statement per Parse, and `users` is referenced by
foreign keys), but defences that depend on details are luck, not security.
Change the payload to `') OR 1=1 --` and it reads every row instead, as one
perfectly legal statement.

**Protection 2: `plainto_tsquery` has no syntax to inject into.** Even
inside the query language, the value is plain words. This is about not
crashing (section 1), not about security. Protection 1 alone already makes
injection impossible.

The test checks the outcome, not the mechanism: it sends the string through
the real router to real PostgreSQL, expects a `200` with no matches, then
counts the `users` table and requires the same number of rows as before.

## 3. Why `ILIKE '%go%'` would be wrong, twice

**📍 Open:** [`test/integration/search_test.go:128`](../../test/integration/search_test.go#L128)

**Correctness: it matches letters, not words.** `ILIKE '%go%'` is true for
any text containing the letters g-o anywhere: **Go**ogle, **go**od,
al**go**rithm, ca**go**, Hu**go**, **go**vernment, ergo. A recruiter
searching for Go developers would get everyone who "searched Google".
`TestSearch_WholeWordOnly` shows both on the same two rows: full-text search
returns only "I write Go every day", and the test's own `ILIKE '%go%'`
count over the same rows is 2.

It is also wrong in the other direction. `ILIKE '%testing%'` does **not**
match "I tested it", because there is no stemming. And it has no notion of
relevance, so there is nothing to order by except chance.

Trying to fix it with word boundaries (`~* '\mgo\M'`) solves the Google
problem and none of the others: no stemming, no ranking, no stop words, and
it is slower still.

**Performance: it cannot use an index.** A B-tree index is sorted by the
start of the value, so it helps `LIKE 'go%'`, where the start is known. A
leading `%` means the match can start anywhere, and no ordering helps. The
plan is always a sequential scan that runs the pattern match against every
row's full text:

```text
Seq Scan on applications  Filter: (search_document ~~* '%goroutine%'::text)
```

That cost grows linearly with the table: every search reads every
document. The GIN index looks up the lexeme and reads only the rows that
contain it. (A trigram GIN index from `pg_trgm` *can* serve `ILIKE
'%go%'`, but it would just make the wrong answer fast.)

## 4. Every hostile input, and what it gets

**📍 Open:** [`internal/domain/search.go:97`](../../internal/domain/search.go#L97)

The principle: **refuse in Go only what PostgreSQL would turn into an
error; pass everything else through, and let `plainto_tsquery` make it
harmless.** `NormalizeSearchQuery` trims `q`, then:

| Input | Response | Why |
|---|---|---|
| missing, `""` | **422** `q: is required` | nothing to search for; the client made a mistake it can fix |
| `"   \t  "` | **422** | trimmed, it is empty |
| `!!! ??? ()&\|:*'"\` | **200**, `total: 0` | valid text with no lexemes; `@@` with an empty query is simply false |
| `the and a` | **200**, `total: 0` | stop words only; same |
| `go & postgres` | **200**, matches | `&` is ignored; it means go AND postgres |
| `go & & (postgres` | **200**, same matches | broken syntax is irrelevant to `plainto_tsquery` |
| 201+ characters | **422** `q: must be at most 200 characters` | counted in characters, not bytes, so 200 Bengali or CJK characters are allowed |
| `résumé`, `東京` | **200**, matches | the default parser keeps accented and CJK words as lexemes |
| `'; DROP TABLE users; --` | **200**, `total: 0` | section 2 |
| `%FF%FE` (invalid UTF-8) | **422** `q: must be valid UTF-8 text` | the database encoding is UTF-8, and PostgreSQL rejects other bytes with SQLSTATE 22021: a **500** |
| `go%00postgres` (NUL byte) | **422** `q: must not contain a NUL character` | PostgreSQL text cannot contain `\0` at all: a **500** |

The last two were not on the brief's list. I found them by asking "what
bytes can a URL carry that a database cannot?" Gin percent-decodes the query
string, so `%FF` arrives in Go as the raw byte `0xFF`. Without the checks,
each is a 500.

Why is the punctuation-only query a 200 and not a 422? Deciding "contains no
searchable words" requires knowing the stop-word list and the parser's rules,
which only PostgreSQL knows. Duplicating them in Go would drift. An empty
page is also a true answer: no application matches those words.

`limit` follows the rest of the API: refused below 1, silently capped at
100. `offset` is **refused**, not capped, above 10,000: quietly lowering an
offset would return a different page from the one asked for, and a client
would loop forever on what it thinks is page 600.

## 5. The query: rank, take a page, then join

**📍 Open:** [`internal/store/search.go:74`](../../internal/store/search.go#L74)

- **`plainto_tsquery(…, $2) AS tsq` in `FROM`**: a function in `FROM`
  produces a one-row "table", evaluated once per statement, and the name
  `tsq` lets `WHERE` and `ts_rank` share it rather than parsing the query
  twice.
- **`ts_rank(search_vector, tsq)`** scores how well a document matches:
  more occurrences of the query's lexemes score higher. It is relative, not
  a percentage, and comparable only within one query. It needs the stored
  vector of every matching row, which is one reason the column is `STORED`.
- **The inner query ranks and pages; the outer one joins.** Ordering by
  relevance means every match must be ranked; there is no way around that.
  But only the 20 rows on the page need a candidate name and score totals,
  so the `users` join and the `LATERAL` score lookup sit outside the
  `LIMIT`. Joined inside, they would run for every match.
- **`LEFT JOIN LATERAL (… GROUP BY rubric_version ORDER BY rubric_version
  DESC LIMIT 1)`**: `LATERAL` lets a subquery in `FROM` refer to the outer
  row (`p.id`), so it runs once per page row. It sums that application's
  scores per rubric version and keeps the latest, which is the same rule as
  `ScoreCard`. `LEFT … ON true` keeps applications with no scores, whose
  totals come back `NULL` and are sent as `null`.
- **Tie-break `submitted_at, id`**: equal relevance is common (two answers
  mentioning a word once), and without a total order, offset paging could
  show a row on two pages or on none.
- **`REPEATABLE READ`, read only**: the existence check, the count and the
  page read one snapshot, so `total` cannot disagree with the page (5.4).
- **A missing post is a 404, not an empty result**, so a mistyped ID is
  distinguishable from "no matches".

## 6. Offset paging here, keyset everywhere else

**📍 Open:** [`internal/api/application_handler.go:763`](../../internal/api/application_handler.go#L763)

Every other list in this API uses keyset cursors (4.4) because `OFFSET n`
makes PostgreSQL produce and throw away n rows. Search uses `offset` anyway:

- **There is no stable sort key to put in a cursor.** Keyset paging needs
  "the row after (x, y)", where x is stored and indexed. Relevance is
  computed per query, not stored, and an index cannot pre-sort by it. A
  cursor would carry a float and still have to rank every match to find
  where it falls, so it would save nothing.
- **Ranking already touches every match.** The offset skips rows that had
  to be ranked anyway, so it adds little on top.
- **People do not page deep into search results.** The first page or two is
  what matters, and `offset` is capped at 10,000 to bound the worst case.
- **The skip-and-repeat problem** (a new row shifting the pages) is milder
  here: applications to one post arrive slowly, and a search is a snapshot
  a recruiter re-runs, not a feed they scroll.

## 7. The tests

**📍 Open:** [`test/integration/search_test.go:170`](../../test/integration/search_test.go#L170)

All in `test/integration/search_test.go`, through the **real router**,
real stores and real PostgreSQL in a throwaway container. Applications are
submitted through `SubmitApplication`, so the documents being searched are
the ones production would build.

| Test | Proves |
|---|---|
| `TestSearch_WholeWordOnly` | "go" finds "I write Go every day" and not "I searched Google for the answer"; the same rows under `ILIKE '%go%'` give 2 |
| `TestSearch_Stemming` | "testing" finds "I tested the payment service thoroughly", and not the unrelated application |
| `TestSearch_HostileInput` | the table in section 4, row by row: exact status, `validation_failed` on every 422, exact matches on every 200, and the `users` table untouched after the injection string |
| `TestSearch_RankedPagedAndScored` | three mentions outrank one; relevance strictly descends; every result carries score totals; `limit=1` gives `next_offset: 1`, page 2 gives the other match and `next_offset: null`; a missing post is a 404 |
| `TestAuthorizationMatrix` | gains a `search` column: admin 200; interviewers, candidates and the forged-admin token 403; anonymous and deleted users 401 |

The unit tests in `internal/api/search_test.go` cover the handler against a
fake store: every refusal is a 422 naming the right parameter **and the
store is never called**; the `limit` cap; and 200 multi-byte characters are
accepted.

## 8. Verification

| Command | Result |
|---|---|
| `make test` | all packages pass; `internal/api` 92.7 %, `internal/domain` 95.9 % coverage |
| `make test-integration` | `internal/store` and `test/integration` pass |
| `go test -tags integration -run 'TestSearch\|TestRebuild' -v ./test/integration/` | the six search tests pass |
| the same, with `to_tsquery` in place of `plainto_tsquery` | four `TestSearch_HostileInput` subtests fail with `status = 500`; reverted |
| `make lint` | `0 issues.` |
| `curl` against the live stack, as the seeded admin, on the 500-application post | `goroutine`: 200, `total=277`; empty and whitespace: 422; punctuation, `go & postgres`, the injection string and `résumé`: 200, `total=0`; `%FF` and 201 characters: 422 |
| `psql`: `to_tsquery` on each input in section 1 | the errors shown in the table there |
