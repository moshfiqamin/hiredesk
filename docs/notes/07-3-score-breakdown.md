# Module 7, part 3 — The score breakdown

`GET /v1/applications/:id/score`: the endpoint the whole project exists to
show. Every point, the criterion that awarded it, and the sentence that
earned it. This note covers **who may see it, and whether a candidate should
see their own** (section 1); the response and where each field comes from
(section 2); **the one query, its joins, and assembling nested JSON in Go**
(sections 3–4); **how the N+1 was avoided, and the test that proves it**
(section 5); the excerpt (section 6) and **why it is cut on the server**
(section 7); new Go idioms (section 8); and what was run (section 9).

Part 2 is [07-2-scoring-on-submit.md](07-2-scoring-on-submit.md).

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-who-may-see-a-breakdown) | **Who may see a breakdown** | [`policy.go:47`](../../internal/auth/policy.go#L47) |
| [2](#2-the-response) | The response | [`application_handler.go:533`](../../internal/api/application_handler.go#L533) |
| [3](#3-one-query-four-tables) | **One query, four tables** | [`applications.go:673`](../../internal/store/applications.go#L673) |
| [4](#4-flat-rows-to-nested-json) | **Flat rows to nested JSON** | [`application_handler.go:566`](../../internal/api/application_handler.go#L566) |
| [5](#5-how-the-n1-was-avoided) | **How the N+1 was avoided** | [`scoring_test.go:294`](../../test/integration/scoring_test.go#L294) |
| [6](#6-the-excerpt) | The excerpt | [`excerpt.go:35`](../../internal/scoring/excerpt.go#L35) |
| [7](#7-why-the-excerpt-is-cut-on-the-server) | **Why the excerpt is cut on the server** | [`application_handler.go:587`](../../internal/api/application_handler.go#L587) |
| [8](#8-go-idioms-new-in-this-part) | Go idioms new in this part | — |
| [9](#9-verification) | Verification | — |

---

## 1. Who may see a breakdown

**📍 Open:** [`internal/auth/policy.go:47`](../../internal/auth/policy.go#L47)

| Caller | Gets | Why |
|---|---|---|
| Admin | 200 | decides outcomes |
| Interviewer assigned to the application | 200 | reviewing it |
| Interviewer not assigned | 404 | may not see the application at all |
| Another candidate | 404 | may not see the application at all |
| **The candidate who applied** | **403** | may see the application, not its score |
| No such application | 404 | — |

**Should a candidate see their own breakdown? Recommended: no, not this
one.** The reasoning:

- **It publishes the rubric.** The breakdown says "matched 2 of 5
  keywords: go, gin" and names the criteria and weights. One candidate
  shares a screenshot, and every later applicant stuffs `goroutine` and
  `kubernetes` into their answer. The rubric only measures anything while
  candidates are answering the questions rather than the scorer.
- **It is not the decision.** The number is an automated first pass; a
  human shortlists, interviews and decides. Showing 34% invites a dispute
  over a figure that may have had nothing to do with the outcome.
- **The project's promise is already kept.** "Every point has a receipt" is
  a promise to the hiring manager, so that *they* can answer "why was I
  rejected?" with evidence. The receipt exists; it is answered through a
  person.

The counter-argument deserves to be stated: transparency is a virtue, and
data-protection law (GDPR Article 15 access, Article 22 on automated
decisions) can entitle a candidate to their data and to meaningful
information about automated processing. That is a reason to build a
**different, candidate-facing view** if it is ever required — one that
shows criteria labels and the candidate's own answers, but not keywords,
weights or configs — not a reason to hand candidates the evaluator's
working.

**Why 403 for one's own, but 404 for someone else's.** The rest of the API
answers "not yours" with 404, so a caller cannot probe for IDs that exist
(`docs/decisions.md` D1, [03-auth](03-auth.md)). That reasoning needs a
secret to protect. A candidate already knows their own application exists
— they can `GET` it. A 404 here would hide nothing and wrongly suggest the
application is gone; a 403 says truthfully "it exists, and this part is not
for you". For anyone who may not see the application, the handler still
returns the exact 404 of an absent one, checked byte for byte in the
authorization matrix.

The handler applies the two policies in order
([`application_handler.go:624`](../../internal/api/application_handler.go#L624)):
`CanViewApplication` first (404), then `CanViewScore` (403, line 518).
`CanViewScore` is narrower than `CanViewApplication`, never wider;
`TestCanViewScore` checks that for every combination of role, ownership and
assignment.

The score card is loaded **only after** both pass.
`TestApplicationScoreLoadsNothingForARefusedCaller` checks the store was not
called for a refused request — so refused and absent requests do the same
work, and timing reveals nothing either.

## 2. The response

**📍 Open:** [`internal/api/application_handler.go:533`](../../internal/api/application_handler.go#L533)

The shape from section 3 of the overview, field for field:

```text
{
  "application_id": "d5f1a1b8-…",
  "total_earned": 66.8, "total_available": 100, "percentage": 66.8,
  "rubric_version": 1,
  "breakdown": [
    { "criterion": "Go knowledge", "kind": "keyword_match",
      "earned": 12, "available": 30,
      "detail": "matched 2 of 5 keywords: go, gin",
      "evidence": { "question": "Describe a project you built",
                    "answer_id": "a188f925-…",
                    "excerpt": "...services for a few years. Last year I built a REST API in Go using Gin, with goroutines handling the background jobs..." } },
    { "criterion": "Remote readiness", "kind": "boolean_all",
      "earned": 10, "available": 10, "detail": "2 of 2 conditions met",
      "evidence": null }
  ]
}
```

That is abridged output from the live check, not a command.

| Field | Comes from |
|---|---|
| `criterion`, `kind` | the `rubric_criteria` row the score points at — the version that scored it |
| `earned`, `available`, `detail` | the `scores` row, as stored at submission |
| `rubric_version` | the scores' own copy |
| `evidence.question` | `questions.prompt`, through the evidence answer |
| `evidence.answer_id` | `scores.evidence_answer_id` |
| `evidence.excerpt` | cut from `application_answers.answer_text` in Go (section 6) |
| `total_*`, `percentage` | summed in Go from the rows (section 4) |

`evidence` is `null`, not `{}`, when there is no single answer: an empty
object would claim evidence exists and has no fields. An application with
no stored scores is a 404 with its own code, `not_scored`, so a client can
tell "nothing to show yet" from "no such application".

**Why the totals are computed, not stored.** They are derivable from the
rows, so storing them would be a second copy that could disagree. Summing
in Go makes "total equals the sum of the breakdown" true by construction —
the first of the workflow's five checks.

**A rounding detail.** Each stored value has two decimals, but adding
float64s does not always stay on two decimals: `0.1 + 0.2` is
`0.30000000000000004`, because neither has an exact binary form. The totals
and the percentage are rounded to cents
([line 471](../../internal/api/application_handler.go#L566)), so a column
adding to 0.3 is shown as 0.3.

## 3. One query, four tables

**📍 Open:** [`internal/store/applications.go:673`](../../internal/store/applications.go#L673)

```sql
SELECT rc.…, s.earned, s.available, s.detail, s.rubric_version,
       aa.id::text, q.prompt, aa.answer_text
FROM scores s
JOIN rubric_criteria rc        ON rc.id = s.criterion_id
LEFT JOIN application_answers aa ON aa.id = s.evidence_answer_id
LEFT JOIN questions q          ON q.id = aa.question_id
WHERE s.application_id = $1
  AND s.rubric_version = (SELECT max(rubric_version) FROM scores WHERE application_id = $1)
ORDER BY rc.position
```

That is the query's structure, for reading; the real text is in the file.
Each join, and why it is the kind it is:

- **`scores` is the driving table.** One row per criterion of the version
  that scored this application. `scores_application_id_idx` finds them.
- **`JOIN rubric_criteria`** (inner). Every score has a criterion — the
  foreign key is `NOT NULL` and `RESTRICT` ([07-1 section 3](07-1-rubric-endpoints.md#3-why-the-old-rows-must-be-kept)) — so an inner join
  can never drop a row. It joins by the criterion's *ID*, not "the current
  rubric", which is why an old score shows the old labels.
- **`LEFT JOIN application_answers`**. `evidence_answer_id` is NULL for
  `boolean_all` and unanswered questions. An inner join would delete those
  scores from the card, and the totals would stop adding up.
- **`LEFT JOIN questions`** — and this one is subtle. `aa.question_id` is
  `NOT NULL`, so it looks safe as an inner join. It is not: its condition
  reads `aa`, which is all NULL on the rows the previous LEFT JOIN kept. An
  inner join on `q.id = NULL` is never true, so it would drop exactly those
  rows again. Once a chain goes LEFT, everything hanging off the optional
  side must be LEFT too.
- **The version subquery.** A rescore replaces the rows, so there should
  only ever be one version per application. If that ever failed, the card
  would add up criteria from two rubrics that never coexisted; the
  subquery makes that impossible to display.

`EXPLAIN ANALYZE` on the live database: the scores come from a Bitmap Index
Scan on `scores_application_id_idx`; each answer and question by its primary
key, in a nested loop. `rubric_criteria` was a Seq Scan — on a five-row
table, where reading the whole table is cheaper than an index; the planner
switches to `rubric_criteria_pkey` as it grows. Execution time 0.23 ms.

## 4. Flat rows to nested JSON

**📍 Open:** [`internal/api/application_handler.go:566`](../../internal/api/application_handler.go#L566)

SQL returns rectangles; the response is a tree. Two steps turn one into the
other.

**Scan: one row → one `domain.ScoreEntry`.** `scanScoreEntry`
([`applications.go:722`](../../internal/store/applications.go#L722)) reads
the criterion columns into `e.Criterion`, the score columns into the entry,
and the three evidence columns into `*string`s
([line 479](../../internal/store/applications.go#L586)). A NULL scans into
a nil pointer, which a plain `string` cannot represent. The three are NULL
together, so the evidence becomes a `*domain.ScoreEvidence` only when all
three are present. The nesting already exists after the scan: an entry
*holds* its criterion and *points at* its evidence.

**Respond: domain → wire.** `newScoreResponse` walks the entries once: it
builds each `scoreEntryResponse`, nests an `evidenceResponse` only when
there is evidence, cuts the excerpt, and adds to the running totals. The
`json` struct tags give the wire names; `encoding/json` does the rest.

Why not have PostgreSQL build the JSON (`json_agg(json_build_object(…))`)?
It would work, and is sometimes the right tool. Here it would put the
response's shape in SQL, the totals and the excerpt would still need Go,
and the excerpt has to use the scorer's keyword rule — which only exists in
Go. One place for the shape is better than two.

## 5. How the N+1 was avoided

**📍 Open:** [`test/integration/scoring_test.go:294`](../../test/integration/scoring_test.go#L294)

The N+1 version is the one most people write first:

```text
scores := SELECT * FROM scores WHERE application_id = $1        -- 1 query
for each score:
    criterion := SELECT * FROM rubric_criteria WHERE id = ...   -- N queries
    answer    := SELECT * FROM application_answers WHERE id = ... -- N more
    question  := SELECT * FROM questions WHERE id = ...         -- N more
```

That is pseudocode, not something to run. It is "1 + N" in name and 1 + 3N
here: a ten-criterion rubric costs 31 round trips. Each is fast alone, but a
round trip is network latency plus parse, plan and execute — the fixed cost
dominates a query that returns one row.

The fix is the query in section 3: **the joins do in one statement what the
loop did in many**, and the number of statements no longer depends on N.
Joins on primary keys are what a relational database is built for.

**Proving it rather than claiming it.** `TestScoreEndpoint` attaches a pgx
`QueryTracer` to a second pool
([`queryCounter`, line 282](../../test/integration/scoring_test.go#L282)),
whose `TraceQueryStart` increments an atomic counter. It then calls the
endpoint through the real router:

- with a **4**-criterion rubric: **3** statements;
- after replacing the rubric with **8** criteria and resubmitting: still
  **3** statements.

The three are `RequireAuth` loading the user, `GetApplication` (the
application and the assignment check in one round trip, from
[03-auth](03-auth.md)), and the score card. An N+1 would have shown 13 and
then 25.

The authorization query and the card query are kept separate on purpose,
although they could be folded into one: loading nothing until the caller is
allowed (section 1) is worth one round trip.

The workflow's N+1 *timing* exercise belongs to the ranking endpoint, later
in this module, where N is 500 applications rather than a handful of
criteria.

## 6. The excerpt

**📍 Open:** [`internal/scoring/excerpt.go:35`](../../internal/scoring/excerpt.go#L35)

`Excerpt(criterion, answer)` returns about 120 characters, with `...` where
it was cut:

1. **Collapse whitespace.** `strings.Fields` + join, as in `answer_depth`:
   a candidate's line breaks would otherwise break the scorecard's layout.
2. **Short answers come back whole.** Under 120 characters, no window.
3. **Find the anchor.** For `keyword_match`, the earliest place — in
   reading order, not config order — that any keyword matches, by the
   scorer's own whole-word regex
   ([`firstKeywordMatch`, line 83](../../internal/scoring/excerpt.go#L83)).
   For other kinds, or no match, the start of the answer.
4. **Centre a window on it**
   ([line 54](../../internal/scoring/excerpt.go#L54)), then slide it back
   if centring pushed an edge past either end of the text.
5. **Snap to word boundaries** — each cut edge moves to the nearest space,
   at most 15 characters, and never across the match. Past 15 the word is
   cut: a 300-character URL must not empty the excerpt.
6. **Add `...`** only on a side that was actually cut.

**Finding the keyword without the edge characters.** The scorer's regex
includes the character before and after the keyword in its match, because
RE2 has no lookaround ([06-1](06-1-scoring-engine.md)). To find where the
keyword itself starts, `compileKeyword`
([`keyword.go:74`](../../internal/scoring/keyword.go#L74)) now wraps the
keyword in a capture group. `MatchString`, all the scorer calls, is
unaffected; `FindStringSubmatchIndex` returns the group's offsets at
indexes 2 and 3 ([line 104](../../internal/scoring/excerpt.go#L104)).

**Characters, not bytes.** The regex reports byte offsets. They are
converted to rune offsets by counting the runes before them
([line 48](../../internal/scoring/excerpt.go#L48)) and the window is cut
from a `[]rune`. Cutting bytes would split a Bengali character's three
bytes and produce invalid UTF-8; `TestExcerptCountsCharactersNotBytes`
([`excerpt_test.go:163`](../../internal/scoring/excerpt_test.go#L163))
checks both validity and width.

**Why it lives in `scoring`.** The excerpt must agree with the score. If it
used `strings.Contains`, an answer saying "I worked at Google… then wrote Go
services" would highlight *Google* for a keyword `go` that the scorer, which
matches whole words, did not count. Putting `Excerpt` beside the scorer
means one regex decides both. A test pins exactly that case.

## 7. Why the excerpt is cut on the server

**📍 Open:** [`internal/api/application_handler.go:587`](../../internal/api/application_handler.go#L587)

The alternative — send the whole answer and let the browser trim it — is
worse on every axis that matters:

- **Data exposure.** Whatever the API sends, the client has: it is in the
  network tab, the browser cache, and any log or proxy in between. An
  interviewer allowed to see "the sentence that earned the points" would
  receive the full answer, and with ten criteria the same 1,500-character
  answer up to ten times over. The response should carry what the caller
  needs, not what the screen happens to show.
- **One rule, one place.** The excerpt depends on the keyword rule (section
  6). A browser would need a JavaScript re-implementation of RE2 whole-word
  matching with Unicode classes — which would drift from the Go one, and
  then highlight words the scorer did not count. Every client (dashboard,
  mobile app, a future CSV export) would need its own copy.
- **The contract.** "Never return a 1,500-character answer inside a score
  breakdown" is a property of the API, testable here:
  `TestApplicationScoreBody` and `TestScoreEndpoint` both assert that text
  from the end of a long answer never appears in the body.
- **Size.** A ten-criterion card is a few kilobytes instead of tens.

The cost is that the server reads the full answer from PostgreSQL. That is
inside the data centre, a few kilobytes, in the same query; it is the right
place to pay it. Trimming in SQL with `left(answer_text, 120)` would be
cheaper still, but would cut before the keyword whenever the match is past
character 120 — which is exactly the case the excerpt exists for.

## 8. Go idioms new in this part

**`*string` for a nullable column.** pgx scans SQL NULL into a nil
pointer and a value into a pointer to it. A plain `string` cannot be "no
value", and scanning NULL into one is an error.

**`sync/atomic` types.** `atomic.Int64` (Go 1.19) is a counter safe to
increment from many goroutines without a mutex — the tracer is called from
whichever goroutine runs a query. Its zero value is ready to use.

**Interfaces satisfied implicitly.** `queryCounter` becomes a pgx tracer
just by having `TraceQueryStart` and `TraceQueryEnd` methods with the right
signatures; nothing declares "implements". The test assigns it to
`cfg.ConnConfig.Tracer`, and the compiler checks the fit there.

**Struct embedding in a response.** `replacedRubricResponse` embeds
`rubricResponse` ([07-1](07-1-rubric-endpoints.md)), so its fields appear at
the top level of the JSON, as in [05-1](05-1-application-validation.md).

## 9. Verification

| Command | Result |
|---|---|
| `go test -race ./internal/api/ -run Score` | pass: 9 caller cases, refusal bytes equal absence, no store call for a refused caller, body shape, `not_scored` |
| `go test -race ./internal/scoring/ -run Excerpt` | pass: 10 table cases, whole words at the edges, Bengali counted in characters |
| `go test -race ./internal/auth/ -run CanViewScore` | pass: every role × ownership × assignment, and never wider than `CanViewApplication` |
| `make test-integration`, `TestScoreEndpoint` | 200; totals 72.5 of 100; excerpt around the keyword; **3 statements with 4 criteria, 3 with 8** |
| `make test-integration`, authorization matrix | `scoreA`: admin 200, assigned interviewer 200, owner 403, forged admin claim 403, others 404 identical to absent |
| live: admin `GET /score` | the response in section 2 |
| live: owner / other candidate | 403 `forbidden` / 404 `not_found` |
| live: rubric replaced, then `GET /score` | still `rubric_version: 1`, version 1's four criteria |
| live: `EXPLAIN ANALYZE` of the card query | index scan on `scores_application_id_idx`, pkey lookups, 0.23 ms |
| `make fmt && make lint && make test && go build ./...` | clean, 0 issues, all pass, builds |

**Reading a breakdown yourself.** With the stack up, in the VS Code
terminal, with `$ADMIN` an admin token and `$APP` an application ID:

```bash
curl -s localhost:8080/v1/applications/$APP/score -H "Authorization: Bearer $ADMIN" | jq
```

Prints the breakdown. Check by eye that `total_earned` is the sum of the
entries, and that each excerpt is short and contains a matched keyword.
