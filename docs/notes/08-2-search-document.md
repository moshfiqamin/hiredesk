# Module 8, part 2 — Populating the search document, and repairing it

Part 1 made `search_vector` impossible to get out of step with
`search_document`. This part is about `search_document` itself: who writes
it, what goes in it, how nothing else blanks it, and how to rebuild it if it
ever does go wrong.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-denormalisation-means) | **What denormalisation means** | — |
| [2](#2-why-denormalise-here-instead-of-joining) | **Why denormalise here instead of joining** | — |
| [3](#3-what-goes-in-the-document) | What goes in the document | [`search.go:48`](../../internal/domain/search.go#L48) |
| [4](#4-should-the-candidates-name-be-in-it) | **Should the candidate's name be in it?** | — |
| [5](#5-written-inside-the-submit-transaction) | Written inside the submit transaction | [`applications.go:810`](../../internal/store/applications.go#L810) |
| [6](#6-nothing-else-blanks-it) | Nothing else blanks it | [`search_test.go:297`](../../test/integration/search_test.go#L297) |
| [7](#7-the-repair-command) | **The repair command, and why it is worth having** | [`search.go:166`](../../internal/store/search.go#L166) |
| [8](#8-one-rule-two-languages) | One rule, two languages | [`search_test.go:339`](../../test/integration/search_test.go#L339) |
| [9](#9-go-idioms-in-this-part) | Go idioms in this part | [`search.go:71`](../../internal/domain/search.go#L71) |
| [10](#10-verification) | Verification | — |

---

## 1. What denormalisation means

A **normalised** schema stores each fact exactly once. The candidate's
answers live in `application_answers`, one row per answer, and nowhere else.
To know what an application said, you join to that table. Normalisation is
the default because a fact stored once can never disagree with itself.

**Denormalisation** is storing a fact, or something derived from it, a
second time, somewhere it is cheaper to read. `search_document` is a copy
of the text in `application_answers`, flattened onto the `applications` row.

It is a trade, every time:

- **you gain** reads: the data is where the query needs it, in the shape it
  needs, with no join or aggregation at read time, and it can be indexed in
  that shape;
- **you pay** in writes, which must now update two places, and in a new kind
  of bug, **drift**: the copy and the original disagreeing because one write
  path forgot the copy.

Familiar examples: a `comment_count` column on a post, an order row that
copies the price at the time of purchase, a search engine's index of a
website. This project already did it once: `scores.rubric_version` copies a
value from `rubric_criteria` so a score stays interpretable after the rubric
changes.

## 2. Why denormalise here instead of joining

The alternative is to search `application_answers` directly:

```sql
SELECT aa.application_id, ts_rank(to_tsvector('english', aa.answer_text), q)
FROM application_answers aa
JOIN applications a ON a.id = aa.application_id, plainto_tsquery('english', $2) q
WHERE a.job_post_id = $1 AND to_tsvector('english', aa.answer_text) @@ q
```

with an expression index on `to_tsvector('english', answer_text)`. It
works, and has no copy to drift. It loses on three counts:

1. **"All the words" spans answers.** `plainto_tsquery('go postgres')`
   means *go AND postgres*. Matched per answer row, a candidate who wrote
   "Go" in one answer and "Postgres" in another matches **neither** row, and
   is not found. Fixing that means aggregating every answer's vector per
   application at query time (`tsvector_agg` does not exist; you would
   `string_agg` the text and re-parse it), which no index can serve.
2. **Ranking is per answer, not per application.** `ts_rank` would score
   each answer alone, and an application would need its answers' ranks
   combined somehow, by `max`, `sum` or something else, each with its own
   distortions.
3. **Cost.** The search would touch several answer rows per application,
   group them, and only then rank and page. With the document on the
   application row it is one index lookup, one row per match, no grouping.

**When the join would be the better choice:**

- answers are **edited often** after submission, so keeping a copy in sync
  costs more than searching the source. Here answers are immutable once
  submitted;
- the results must say **which answer matched**, per question, for
  highlighting. A per-answer search returns that for free; with the
  document you would run `ts_headline` on the whole document instead;
- searching is **rare** and the data small, so the join's cost does not
  matter and the drift risk is not worth taking;
- you want to search **one question only** ("who mentioned Kubernetes in
  *Why this role?*"). The flattened document has lost which text came from
  which question.

## 3. What goes in the document

**📍 Open:** [`internal/domain/search.go:48`](../../internal/domain/search.go#L48)

`domain.SearchDocument(form, answers)` builds it:

- **text and choice answers only.** A number answer is stored as `"4"` and
  a boolean as `"true"`. Indexed, `"true"` would make every candidate who
  ticked any yes/no box match a search for `true`, and `"4"` matches
  nothing anyone would search for. The rejected alternative was to index
  every answer, which is simpler and adds only noise;
- **in form order**, not the order the client sent them, so the document
  reads like the form, and the Go and SQL versions agree (SQL orders by
  `questions.position`);
- **joined by newlines.** The separator has no effect on search, since the
  parser treats any whitespace the same, but it keeps the stored text
  readable in `psql`;
- **cut to 100,000 characters**, never splitting a character (part 1,
  section 9).

It is a pure function in `domain`: no database and no HTTP, so it is
tested in microseconds by `TestSearchDocument`, including the out-of-order,
unanswered-question and multi-byte-truncation cases.

## 4. Should the candidate's name be in it?

My view: **no**, and the code leaves it out. Four reasons:

1. **It is a different question.** "Find Ada's application" is a lookup by
   name: exact or fuzzy matching on `users.full_name`, which wants a
   trigram index (`pg_trgm`) and no stemming. Full-text search on answers
   is "find people who know Kubernetes". Mixing them makes both worse: a
   candidate surnamed *Rust* or *Swift*, or called *Go Takahashi*, would
   match skill searches for their name, and English stemming mangles names
   (`Jones` → `jone`).
2. **It would be a second denormalisation, and one that can drift.** Answers
   never change after submission; a name can. Every name change would have
   to rewrite the document of every application that person ever made, or
   the search would silently find them under their old name.
3. **Fair screening.** A recruiter searching on skills should find people by
   what they wrote. Keeping identity out of the searchable text is a small,
   real step toward the blind screening many hiring processes aim for. The
   name is still *returned* in every result; it just is not what is
   matched.
4. **It is cheap to add later**, as a separate `?name=` filter on
   `users.full_name`, if anyone asks for it.

## 5. Written inside the submit transaction

**📍 Open:** [`internal/store/applications.go:810`](../../internal/store/applications.go#L810)

The document is built from `clean.Answers` (validated, canonical, empty
answers removed) and `form` (already loaded, in display order), and passed
as `$4` in the `INSERT` that creates the application row.

The rejected alternative was an `UPDATE applications SET search_document =
… WHERE id = $1` after `insertAnswers`, computing the document in SQL from
the rows just inserted. That would have one obvious advantage: a single SQL
expression shared with the repair. It costs:

- **two writes of the row** in one transaction, the INSERT and then the
  UPDATE, leaving a dead tuple behind for every submission;
- **two parses**: `search_vector` computed for `''` at INSERT, and again for
  the real text at UPDATE;
- **one more round trip.**

Building it in Go first means one write, one parse, no extra statement. The
price is that the rule exists twice (section 8).

Because it is part of the same transaction, the guarantee from 5.2 extends
to it: an application exists **if and only if** its answers, scores, event
and search document do. A failure anywhere rolls all of it back.

## 6. Nothing else blanks it

**📍 Open:** [`test/integration/search_test.go:297`](../../test/integration/search_test.go#L297)

Every statement that writes `applications`, found with
`grep -rn -e "UPDATE applications" -e "INSERT INTO applications"`:

| Write | Touches `search_document`? |
|---|---|
| `SubmitApplication`'s INSERT | sets it |
| `TransitionApplication`'s UPDATE | `SET status, updated_at` only |
| `RescoreApplication` / `RescoreJobPost` | write `scores` and `application_events`; never `applications` |
| `RebuildSearchDocuments` | sets it, to the correct value |

So no current path blanks it. "Make sure" means holding that in a test, so
a future change cannot quietly break it. `TestSearchDocumentSurvivesEveryWrite`
submits, then rescores, rescores the whole post, and shortlists, checking
after each that the document is byte-for-byte unchanged and still found.

The remaining risk is the `DEFAULT ''` itself: a **future** INSERT that
forgets the column gets `''` without complaint, and that application is
silently unsearchable. Dropping the default after the backfill would turn
that into a loud `NOT NULL` violation. I kept it because the brief asked for
it, and because five test files insert application rows directly,
without answers, where `''` is the correct document. The repair command is
the backstop for that case.

## 7. The repair command

**📍 Open:** [`internal/store/search.go:166`](../../internal/store/search.go#L166)

`make reindex-search` runs `cmd/reindex`, which calls
`RebuildSearchDocuments`:

```text
search documents: 0 of 501 rewritten
```

**Why have one, if the generated column cannot drift?** Because the
generated column only guarantees `search_vector` matches `search_document`.
It says nothing about whether `search_document` matches the **answers**,
and that second link is ordinary application code, which can be wrong:

- **a bug in a future write path**: an import job, an admin "edit answer"
  feature, a new INSERT that forgets the column (section 6);
- **a change to the rule.** If tomorrow number answers should be included,
  or the cap raised, every existing document is "wrong" under the new rule.
  The repair is how you apply it to old rows;
- **hand edits**: someone fixing an answer in `psql` during an incident;
- **restores and copies** from a backup taken mid-migration, or a partial
  data import.

Without it, recovering from any of those means writing a one-off SQL script
under pressure, at the moment you least want to. With it, the fix is one
command that has already been tested.

Its design, line by line:

- **one statement**, a data-modifying CTE (`WITH … UPDATE … RETURNING`), so
  it is atomic. Ctrl-C cancels it on the server and nothing changes;
- **`WHERE a.search_document <> doc.text`** writes only rows that are wrong.
  On a healthy database it reads everything and writes nothing: no dead
  tuples and no GIN churn. The count it prints doubles as a **drift
  detector**: anything above 0 means some write path is broken;
- **`LEFT JOIN`s and `coalesce(…, '')`**: an application with no text
  answers still gets a row in `doc`, so a wrongly non-empty document on it
  is reset to `''`;
- **`FILTER (WHERE q.kind IN ('text','choice'))`** excludes number and
  boolean answers from the aggregate without excluding the application;
- **`$1` is `domain.MaxSearchDocumentChars`**, passed from Go, so the cap is
  one constant;
- **it does not touch `updated_at`.** Rebuilding a derived field is
  maintenance, not a change to the application;
- **safe while the API is serving.** Row locks are taken only on rows it
  rewrites, and a submission running at the same time already writes the
  correct value.

**Why a binary and not a SQL file in the Makefile?** The SQL then lives in
Go next to its test, the cap comes from the Go constant, and it runs with the
API's own configuration. It ships in the production image (the `Dockerfile`
builds `hiredesk-reindex`), and unlike `cmd/seed` it does not refuse
`ENV=production`, because repairing production is what it is for.

At a much larger scale, millions of applications, one statement would hold
locks too long. You would batch it by ID range (`WHERE a.id > $last ORDER
BY a.id LIMIT 1000`), committing each batch.

## 8. One rule, two languages

**📍 Open:** [`test/integration/search_test.go:339`](../../test/integration/search_test.go#L339)

The rule "text and choice answers, form order, newline-joined, cut to
100,000 characters" now exists in three places: `domain.SearchDocument` (Go),
`RebuildSearchDocuments` (SQL), and the migration's backfill (SQL, frozen
in time like every migration; it must never call application code that may
change after it).

Three copies of a rule is exactly the drift problem this part is about, so
a test pins the two live ones together.
`TestRebuildSearchDocumentsAgreesWithSubmit` submits applications through
the Go path, runs the SQL repair, and requires **0 rewritten**. Any
difference between the two rules would show up as a rewrite. One
application's answer is 100,010 three-byte characters long, so both must cut
at exactly the same place: Go counts runes, and SQL's `left()` counts
characters in a UTF-8 database, which is the same thing. It then damages two
documents and checks that exactly those two are restored and found again.

The live database confirmed the backfill agrees too: right after the
migration, `make reindex-search` reported `0 of 501 rewritten`.

## 9. Go idioms in this part

**📍 Open:** [`internal/domain/search.go:71`](../../internal/domain/search.go#L71)

**`for i := range s` over a string.** Ranging over a string steps one
*character* (rune) at a time, not one byte, and `i` is the **byte offset**
where that character starts. So the n-th iteration's `i` is exactly where
the first n characters end, and `s[:i]` cuts there without splitting a
multi-byte character. `s[:n]` would cut at byte n, which can land in the
middle of `語` (three bytes) and produce invalid UTF-8, which PostgreSQL
would then reject.

**The `len(s) <= n` fast path.** `len` on a string is its length in bytes.
A character is at least one byte, so a string of at most n bytes has at most
n characters and needs no counting. Most documents take this path.

**A map for lookup, a slice for order.** `byQuestion` is a
`map[string]string` so finding an answer is O(1). The output order comes
from ranging over `form`, a slice, because Go deliberately randomises map
iteration order. Ranging over the map would give a different document on
each run.

**Named results in `RebuildSearchDocuments`.** `(rewritten, total int, err
error)` names the results so the signature documents itself. Two bare
`int`s would not say which is which.

## 10. Verification

| Command | Result |
|---|---|
| `go test ./internal/domain/` | `TestSearchDocument`, `TestSearchDocumentIsCutToTheLimit`, `TestTruncateChars` pass |
| `make test-integration` | `TestSearchDocumentSurvivesEveryWrite` and `TestRebuildSearchDocumentsAgreesWithSubmit` pass, with the rest of the suite |
| `make reindex-search` on the live stack after migrating | `search documents: 0 of 501 rewritten` |
| blank three documents in `psql`, then `make reindex-search` | `3 of 501 rewritten`, plus the drift warning |
| `grep -rn -e "UPDATE applications" -e "INSERT INTO applications" --include='*.go'` | only the writes listed in section 6, plus test fixtures |
