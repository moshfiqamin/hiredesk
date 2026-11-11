# Module 8, part 1 — The search schema: tsvector, GIN, and a generated column

Migration `000012_search` gives every application a searchable text,
`search_document`, a parsed form of it, `search_vector`, and a GIN index on
that. This note explains each piece from the ground up, using real output
from this project's PostgreSQL 16.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-why-the-file-is-000012-not-0007) | Why the file is `000012`, not `0007` | — |
| [2](#2-what-a-tsvector-actually-is) | **What a tsvector actually is** | [`000012_search.up.sql:54`](../../migrations/000012_search.up.sql#L54) |
| [3](#3-stemming) | **Stemming** | — |
| [4](#4-stop-words) | **Stop words** | — |
| [5](#5-the-gin-index-and-why-not-a-b-tree) | **The GIN index, and why not a B-tree** | [`000012_search.up.sql:64`](../../migrations/000012_search.up.sql#L64) |
| [6](#6-generated-always-as--stored) | **`GENERATED ALWAYS AS … STORED`** | [`000012_search.up.sql:54`](../../migrations/000012_search.up.sql#L54) |
| [7](#7-the-cost-what-every-update-pays) | **The cost: what every UPDATE pays** | — |
| [8](#8-the-order-of-the-statements) | The order of the statements | [`000012_search.up.sql:29`](../../migrations/000012_search.up.sql#L29) |
| [9](#9-the-1-mb-ceiling) | The 1 MB ceiling | [`search.go:17`](../../internal/domain/search.go#L17) |
| [10](#10-the-down-migration) | The down migration | [`000012_search.down.sql:10`](../../migrations/000012_search.down.sql#L10) |
| [11](#11-verification) | Verification | — |

---

## 1. Why the file is `000012`, not `0007`

The brief asked for `migrations/0007_search.up.sql`. golang-migrate reads the
leading number as the **version**, and version 7 already exists
(`000007_job_post_events`). Two files claiming version 7 is an error, and a
database already at version 11 would never run a "new" version 7 anyway. It
has to be the next number, and it is zero-padded to six digits like the
others so they sort correctly as strings, which the integration tests rely
on when they glob and sort `migrations/*.up.sql`.

## 2. What a tsvector actually is

**📍 Open:** [`migrations/000012_search.up.sql:54`](../../migrations/000012_search.up.sql#L54)

A `tsvector` is a document turned into a **sorted list of distinct
normalised words (lexemes), each with the positions it appeared at**. It is
not the text any more. It is what is left once a search engine has decided
what the text is "about".

Run against this project's database:

```sql
SELECT to_tsvector('english', 'I built a REST API in Go and tested it');
```

Output (not a command):

```text
'api':5 'built':2 'go':7 'rest':4 'test':9
```

`ts_debug` shows what happened to each word. Numbering the words 1 to 10:

| # | Word | Result | Why |
|---|---|---|---|
| 1 | `I` | dropped | stop word |
| 2 | `built` | `'built':2` | already a base form; the English stemmer does not turn irregular verbs into `build` |
| 3 | `a` | dropped | stop word |
| 4 | `REST` | `'rest':4` | lowercased |
| 5 | `API` | `'api':5` | lowercased |
| 6 | `in` | dropped | stop word |
| 7 | `Go` | `'go':7` | lowercased, and *not* a stop word, which matters for this project |
| 8 | `and` | dropped | stop word |
| 9 | `tested` | `'test':9` | **stemmed**: the `-ed` suffix removed |
| 10 | `it` | dropped | stop word |

Three things to notice:

- **The positions are the original word positions**, gaps and all: `rest`
  is 4 because `a` was word 3, even though `a` is gone. That is what lets
  phrase search (`<->`) and `ts_rank_cd` know which words were near each
  other.
- **The list is sorted alphabetically**, not in text order. A `tsvector` is
  built to be looked up, not read.
- **A word appearing twice is stored once**, with two positions:
  `'go':3,7`.

## 3. Stemming

Stemming reduces a word to a common root, so that different grammatical
forms of the same word become the **same lexeme**. The `english`
configuration uses the Snowball English stemmer, which strips suffixes by
rule:

| Word | Lexeme |
|---|---|
| `test`, `tests`, `tested`, `testing` | `test` |
| `searched` | `search` |
| `Postgres` | `postgr` |
| `Google` | `googl` |

The root does not have to be a real word (`postgr`, `googl`). It only has to
be the same for every form.

**Why "testing" finds "tested":** the *query* is stemmed by the same rules
as the document. `plainto_tsquery('english', 'testing')` produces the query
`'test'`, and the stored `tsvector` of "I tested it" contains `'test'`. The
match is on the stem, so it is an exact comparison of two equal strings,
which is exactly what an index can answer. `TestSearch_Stemming` proves it
end to end.

The trade-off is that stemming is a heuristic. `university` and `universe`
both stem to `univers`, so a search for one finds the other. For CV and
answer text that is an acceptable price for "testing" finding "tested".

## 4. Stop words

Stop words are words so common that they say nothing about what a document
is about: `a`, `an`, `the`, `in`, `and`, `it`, `I`, `of`, `to`, and about a
hundred more. The `english` configuration drops them before stemming, from
the list in PostgreSQL's `english.stop` file.

`a`, `in` and `and` disappeared for that reason. Keeping them would:

- **bloat the index.** `the` is in every application, so its entry in the
  GIN index would list every row, and a search for it would return them all;
- **not change any result.** A query of `go and postgres` means `go` and
  `postgres`; requiring the word `and` too would only exclude documents that
  happened to phrase it differently.

A query made *only* of stop words, such as `the and a`, reduces to an empty
query. PostgreSQL issues a NOTICE and matches nothing. Part 3 explains why
the endpoint treats that as an empty page rather than an error.

## 5. The GIN index, and why not a B-tree

**📍 Open:** [`migrations/000012_search.up.sql:64`](../../migrations/000012_search.up.sql#L64)

**GIN** (Generalized Inverted Index) is the index at the back of a book. For
every lexeme it stores the list of rows that contain it:

```text
'api'    → rows 12, 40, 41, 97 …
'go'     → rows 12, 13, 40, …
'postgr' → rows 40, 97, …
```

A search for `go & postgres` looks up two keys, intersects two lists, and
has its answer. The cost depends on how many rows match, not on how many
rows the table has.

A **B-tree** orders whole values: it can answer `=`, `<`, `>`, `BETWEEN` and
`LIKE 'prefix%'`, because those are all questions about a value's place in
a sorted order. "Does this document *contain* the word `go`?" is not a
question about order. No sorting of whole documents puts all the ones
containing `go` next to each other, so a B-tree on `search_vector` could not
help `@@` at all. PostgreSQL does not even offer a B-tree operator class for
`@@`.

The rejected alternative was **GiST**, PostgreSQL's other index type for
`tsvector`. GiST stores a lossy signature per row: smaller and cheaper to
update, but every hit must be rechecked against the row, and it gets
noticeably slower as the table grows. GIN is the standard choice when reads
outnumber writes, which they do for search: an application is written once
and searched many times.

## 6. `GENERATED ALWAYS AS … STORED`

**📍 Open:** [`migrations/000012_search.up.sql:54`](../../migrations/000012_search.up.sql#L54)

A generated column is a column whose value PostgreSQL computes from other
columns of the same row, every time the row is inserted or one of those
columns changes. `STORED` means the result is written to disk with the row,
like any other column. PostgreSQL 16 has no virtual (computed-on-read)
generated columns, and an index needs a stored value anyway.

`GENERATED ALWAYS` means **nothing else may write it**. An `INSERT` or
`UPDATE` that names `search_vector` is refused with an error.

**Why this is safer than a trigger.** The traditional way was a `BEFORE
INSERT OR UPDATE` trigger that sets `NEW.search_vector :=
to_tsvector(...)`. That works, but each of these can quietly break it:

| A trigger can… | A generated column cannot, because… |
|---|---|
| be dropped, disabled (`ALTER TABLE … DISABLE TRIGGER`), or skipped by `session_replication_role = replica` during a restore | it is part of the column's definition; there is nothing separate to disable |
| be overwritten: someone writes `search_vector` directly and the trigger is not written to stop them | writing it directly is an error |
| be written with the wrong condition (`UPDATE OF other_column`) and miss an update | PostgreSQL tracks the dependency itself |
| use a different text-search configuration from the query | the expression is visible in `\d applications` |
| live in a separate migration from the column, so the two drift | they are one statement |

The generated column reduces "is the vector in sync?" from a property of
code to a property of the schema. The only thing that can be stale is
`search_document` itself, which is part 2's subject.

**Why `'english'` must be written out.** The one-argument
`to_tsvector(text)` uses the session setting `default_text_search_config`,
so the same text could produce different vectors in different sessions.
PostgreSQL only accepts an **immutable** expression in a generated column,
and the one-argument form is merely *stable*, so it is refused. The
two-argument form with a constant configuration is immutable. The queries in
part 3 must pass `'english'` too, or the query would be stemmed differently
from the documents.

## 7. The cost: what every UPDATE pays

A generated column is not free. On every `UPDATE` that **changes
`search_document`**, PostgreSQL:

1. **re-parses the whole document**: tokenises, drops stop words, stems
   each word, sorts and de-duplicates, which is CPU proportional to the
   text length;
2. **writes a new row version** with the new `tsvector`. Under MVCC every
   UPDATE writes a new tuple anyway, but the tuple is now bigger: the
   vector is roughly as large as the text it came from;
3. **updates the GIN index**: removes the old row from the posting list of
   every lexeme it had, and adds it to every lexeme it now has. GIN softens
   this with a *pending list*, inserting into a small unsorted area that is
   merged into the main index later (on `VACUUM`, or once it exceeds
   `gin_pending_list_limit`). Writes are cheaper, and searches read the
   pending list too until it is merged;
4. **loses HOT.** A Heap-Only-Tuple update, which skips touching any index,
   is only possible when no indexed column changes. Changing
   `search_document` changes `search_vector`, which is indexed, so that
   update can never be HOT.

What it does **not** cost: an UPDATE that leaves `search_document` alone,
such as a status change or a rescore. PostgreSQL recomputes a stored
generated column only when a column it depends on is in the `SET` list,
so those writes pay nothing extra. They can stay HOT, since
`search_vector` did not change.

Measured on 200 rows of 5,000-word documents:

| Statement | Time |
|---|---|
| `UPDATE … SET status = 'b'`, table **with** the generated column | 0.61 ms |
| the same, on an identical table **without** it | 0.46 ms |
| `UPDATE … SET doc = doc \|\| ' x'`, with the generated column | 1,070 ms |

The third line is the price: about 5 ms per 5,000-word document, all of it
re-parsing.

In this project the document is written once, at submission, and only the
repair command ever rewrites it, so the cost is paid once per application.
That is the access pattern full-text search is built for.

## 8. The order of the statements

**📍 Open:** [`migrations/000012_search.up.sql:29`](../../migrations/000012_search.up.sql#L29)

The brief listed the steps as column, generated column, index, backfill.
The migration runs them as **column, backfill, generated column, index**:

| Order | What happens to each existing row |
|---|---|
| Brief's order | the generated column rewrites the table and computes `to_tsvector('')`; the index is built over empty vectors; the backfill then writes every row **again**, computes the vector **again**, and inserts it into the GIN index **one row at a time** |
| This order | the backfill writes each row once; adding the generated column computes each vector once; `CREATE INDEX` then builds the GIN index **in bulk**, sorting every entry first, which is far faster than row-by-row insertion and leaves a compact index |

The first `ADD COLUMN` has a constant default, so since PostgreSQL 11 it is
a catalogue change only: the default is recorded once and no row is
rewritten.

The whole file runs as one implicit transaction: golang-migrate sends it as
one multi-statement string, and PostgreSQL runs such a string atomically. If
the index build failed, the columns and the backfill would be rolled back
with it. That is also why the index is not `CONCURRENTLY`: that form cannot
run inside a transaction. On a large production table you would split it
into its own migration so the table is not locked while the index builds.

The backfill applies the same rule as the Go code. Part 2 explains the
rule, and the test that keeps the two versions in agreement.

## 9. The 1 MB ceiling

**📍 Open:** [`internal/domain/search.go:17`](../../internal/domain/search.go#L17)

A `tsvector` may not exceed 1 MB. While writing this I measured what text
reaches that:

```sql
SELECT pg_column_size(to_tsvector('english', string_agg('w'||g, ' ')))
FROM generate_series(1, 40000) g;
```

268,893 characters of distinct words produced a 557,992-byte vector, about
twice the text. 150,000 such words failed outright with
`string is too long for tsvector (1597986 bytes, max 1048575 bytes)`.

The API accepts request bodies up to 1 MB, so a candidate could submit
answers that make the generated column throw, and **the submit would be a
500.** The document is therefore cut to 100,000 characters, at most about
400 KB of vector, in every place it is built (Go, the backfill and the
repair). 100,000 characters is around 15,000 words, far more than any form
answer.

## 10. The down migration

**📍 Open:** [`migrations/000012_search.down.sql:10`](../../migrations/000012_search.down.sql#L10)

The order is the reverse of the up migration, and it is forced:
PostgreSQL refuses to drop `search_document` while a generated column
depends on it. Dropping a column drops its indexes, so the `DROP INDEX` is
redundant, but writing it out makes the down read as an exact mirror of
the up. `IF EXISTS` everywhere means a half-applied state can still be
rolled back.

## 11. Verification

Commands run, and what was observed:

| Command | Result |
|---|---|
| `make migrate-up` | version 12; `\d applications` shows `search_document text not null default ''`, `search_vector tsvector generated always as (to_tsvector('english'::regconfig, search_document)) stored`, and `applications_search_vector_idx gin (search_vector)` |
| `SELECT count(*) FILTER (WHERE search_document <> '')` | 501 of 501 existing applications backfilled |
| `make migrate-down` | version 11; no `search` column left in `\d applications` |
| `make migrate-up` again | version 12: the pair round-trips |
| `to_tsvector('english', 'I built a REST API in Go and tested it')` | `'api':5 'built':2 'go':7 'rest':4 'test':9` |
| `ts_debug(...)` on the same sentence | `I`, `a`, `in`, `and`, `it` give empty lexemes; `tested` gives `{test}` |
| `to_tsvector('english','tested') @@ plainto_tsquery('english','testing')` | `t` |
| `to_tsvector` of 150,000 distinct words | error: `string is too long for tsvector` |
| `EXPLAIN ANALYZE` of a search on the 500-application post with `enable_seqscan = off` | `Bitmap Index Scan on applications_search_vector_idx` |
| The same with the planner left alone | `Seq Scan`, 0.13 ms. Correct: 277 of 501 rows match and the table is 30 pages, so reading them all is cheaper than the index. The index pays off when a term is selective and the table is large |
| `EXPLAIN ANALYZE … WHERE search_document ILIKE '%goroutine%'` | `Seq Scan`, always: a leading `%` cannot use any B-tree |
| `UPDATE` timings on two 200-row temporary tables (section 7) | status-only 0.61 ms vs 0.46 ms without the column; changing the document 1,070 ms |
