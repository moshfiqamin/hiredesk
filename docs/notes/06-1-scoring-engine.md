# Module 6, part 1 — The scoring engine

This note covers the package that turns a candidate's answers and a job
post's rubric into a score: what "pure" means and why it matters
(section 1), the `Criterion` type and its `Config` field (sections 2–3), the
function type and registry that dispatch on kind (section 4), why the
package compiles a rubric before scoring it (section 5), the edge cases
(section 6), and the three strategies — `keyword_match` (sections 7–10),
`experience_years` (section 11) and `boolean_all` (section 12). Section 13
lists the Go idioms that appear here for the first time; section 14 is the
tests and what was run.

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-makes-the-package-pure) | What makes the package pure | [`engine.go:1`](../../internal/scoring/engine.go#L1) |
| [2](#2-criterion-and-the-existing-answer) | `Criterion`, and the existing `Answer` | [`criterion.go:16`](../../internal/domain/criterion.go#L16) |
| [3](#3-config-jsonrawmessage-not-mapstringany) | `Config`: `json.RawMessage`, not `map[string]any` | [`criterion.go:28`](../../internal/domain/criterion.go#L28) |
| [4](#4-a-function-type-and-a-registry-instead-of-a-switch) | A function type and a registry instead of a switch | [`engine.go:122`](../../internal/scoring/engine.go#L122) |
| [5](#5-compile-once-score-many) | Compile once, score many | [`engine.go:177`](../../internal/scoring/engine.go#L177) |
| [6](#6-the-edge-cases) | The edge cases | [`engine.go:273`](../../internal/scoring/engine.go#L273) |
| [7](#7-keyword_match-and-the-stringscontains-bug) | `keyword_match`, and the `strings.Contains` bug | [`keyword.go:81`](../../internal/scoring/keyword.go#L81) |
| [8](#8-word-boundaries-and-why-not-b) | Word boundaries, and why not `\b` | [`keyword.go:74`](../../internal/scoring/keyword.go#L74) |
| [9](#9-what-regexpquotemeta-protects-against) | What `regexp.QuoteMeta` protects against | [`keyword.go:76`](../../internal/scoring/keyword.go#L76) |
| [10](#10-what-precompiling-saves) | What precompiling saves | [`keyword_test.go:254`](../../internal/scoring/keyword_test.go#L254) |
| [11](#11-experience_years-parsefloat-and-clamping) | `experience_years`: `ParseFloat` and clamping | [`experience.go:57`](../../internal/scoring/experience.go#L57) |
| [12](#12-boolean_all-a-strategy-that-reads-several-answers) | `boolean_all`: a strategy that reads several answers | [`boolean.go:22`](../../internal/scoring/boolean.go#L22) |
| [13](#13-go-idioms-new-in-this-part) | Go idioms new in this part | — |
| [14](#14-the-tests-and-verification) | The tests, and verification | [`engine_test.go:121`](../../internal/scoring/engine_test.go#L121) |

---

## 1. What makes the package pure

**📍 Open:** [`internal/scoring/engine.go:1`](../../internal/scoring/engine.go#L1)

A pure function's output depends only on its arguments, and calling it
changes nothing outside it. For a package that means: no database, no HTTP,
no `time.Now()`, no `rand`, no global variable that is written after
start-up. Look at the import list — `bytes`, `encoding/json`, `errors`,
`fmt`, `io`, `math`, `regexp`, `strconv`, `strings`, `unicode/utf8` and
`domain`. Nothing there can reach outside the process or observe when it
runs. The one package-level variable, `registry`, is filled at
initialisation and only ever read.

Why care:

- **Tests are plain values in, plain values out.** Every test in the package
  builds a `[]domain.Criterion` and a `[]domain.Answer` literal and checks
  the `Result`. No testcontainers, no fake store, no frozen clock. The whole
  suite runs in about a second, under `-race`.
- **A score can be reproduced.** The `scores` table keeps `rubric_version`
  so a score stays explainable after the rubric changes
  ([`000005_scores_events.up.sql`](../../migrations/000005_scores_events.up.sql)).
  That promise only holds if feeding the same answers and the same rubric
  version back in gives the same number. If the engine read the clock
  (say, "years of experience as of today") the number would drift.
- **It can be called from anywhere.** The HTTP handler that scores on
  submit, a future batch job that rescores after a rubric revision, a CLI
  that previews a rubric — all call the same function, and none has to set
  up a database to do it.
- **Concurrency is free.** No shared mutable state means no locks. A
  compiled `*Rubric` can score applications from many goroutines at once.

The rejected alternative is a scorer that loads the rubric itself
(`ScoreApplication(ctx, db, applicationID)`). It is shorter to call and much
harder to test: every scoring rule would need a database to exercise. The
store will load rows; this package only does arithmetic on them.

## 2. `Criterion`, and the existing `Answer`

**📍 Open:** [`internal/domain/criterion.go:16`](../../internal/domain/criterion.go#L16)

`Criterion` mirrors one `rubric_criteria` row. Three decisions:

- **`Weight` is `float64`** although the column is `numeric(6, 2)`. Scoring
  multiplies a weight by a fraction like 3/5, which is not a two-decimal
  number anyway; the store will round `Earned` back to two places when it
  writes a `scores` row. Using `domain.Money`-style integer cents would make
  the arithmetic exact but every strategy uglier, for no gain the column
  could keep.
- **`QuestionID` is a `string`, with `""` meaning NULL**, rather than
  `*string`. The engine only ever asks "is there a question, and an answer
  to it?", and an empty string answers the first half without a nil check.
  *Changed in part 2:* a kind that reads one answer and names no question is
  now refused with `ErrMissingQuestion` rather than scored as "no answer
  provided" — a rubric bug, not a candidate's skipped question
  ([06-2 §4](06-2-depth-and-validation.md#4-a-kind-that-needs-a-question)).
- **The kind constants include `answer_depth`.** It had no scorer in part 1;
  part 2 added one ([06-2 §1](06-2-depth-and-validation.md#1-answer_depth)).

**`Answer` was not redefined.** `domain.Answer` already exists
([`application.go:74`](../../internal/domain/application.go#L74)) with `ID`,
`QuestionID` and `Text` — exactly the fields asked for — and it is what the
store already returns. A second `Answer` type would need converting between
the two at every call.

## 3. `Config`: `json.RawMessage`, not `map[string]any`

**📍 Open:** [`internal/domain/criterion.go:28`](../../internal/domain/criterion.go#L28)

`json.RawMessage` is a named `[]byte` holding JSON that has not been decoded
yet. `map[string]any` is JSON that has been decoded into Go's generic types.
The difference is what each strategy has to do with it.

With `map[string]any`, `{"min": 0, "target": 2}` arrives as
`map[string]any{"min": float64(0), "target": float64(2)}` — every JSON
number becomes `float64`, every array `[]any`, every object
`map[string]any`. Reading the keyword list looks like this:

```go
raw, ok := c.Config["keywords"].([]any)   // type assertion 1
for _, v := range raw {
    s, ok := v.(string)                   // type assertion 2, per element
    ...
}
```

Each assertion is a place to forget the `ok` check and panic, and a
misspelt key (`"keyword"`) just returns `nil, false` — indistinguishable
from "not set".

With `json.RawMessage`, each strategy declares the shape it wants and
decodes once ([`engine.go:361`](../../internal/scoring/engine.go#L361)):

```go
type keywordConfig struct {
    Keywords []string `json:"keywords"`
}
```

`encoding/json` does the type checking (`{"keywords": "go"}` is an error),
and `DisallowUnknownFields` turns a misspelt key into an error too. A test
pins both ([`engine_test.go:500`](../../internal/scoring/engine_test.go#L500)).

| | `map[string]any` | `json.RawMessage` |
|---|---|---|
| Decoding | Once, eagerly, into generic types | Per strategy, into a typed struct |
| Reading a field | Type assertion per value | A struct field |
| Wrong type | Found when the assertion runs | Error from `Decode` |
| Misspelt key | Silent: looks like "absent" | Error, with `DisallowUnknownFields` |
| Numbers | Always `float64` (an `int` needs a conversion) | Whatever the struct says |
| Round-trip to the DB | Re-encoded, key order lost | Bytes as stored |

What `map[string]any` buys: you can inspect a config without knowing its
kind. Nothing here needs to. Since `pgx` scans `jsonb` straight into a
`[]byte`, `RawMessage` also costs the store nothing.

## 4. A function type and a registry instead of a switch

**📍 Open:** [`internal/scoring/engine.go:122`](../../internal/scoring/engine.go#L122)

```go
type scorer func(answers answerIndex) outcome
type newScorer func(c domain.Criterion) (scorer, error)

var registry = map[string]kindSpec{
    domain.CriterionKindKeywordMatch:    {build: newKeywordMatch, needsQuestion: true},
    domain.CriterionKindExperienceYears: {build: newExperienceYears, needsQuestion: true},
    domain.CriterionKindAnswerDepth:     {build: newAnswerDepth, needsQuestion: true},
    domain.CriterionKindBooleanAll:      {build: newBooleanAll},
}
```

(In part 1 the map value was the constructor alone; part 2 wrapped it in a
small struct so the registry can also say which kinds need a question.)

In Go, functions are values: they have types, can be stored in variables
and maps, and passed around. `type scorer func(...) outcome` names a
function *signature*, so the compiler checks that every strategy has
exactly that shape. The registry is a map from kind to function.

The rejected alternative is the obvious one:

```go
switch c.Kind {
case "keyword_match":
    // forty lines of keyword logic
case "experience_years":
    // thirty lines of ramp logic
...
}
```

Why the registry wins:

- **One strategy per file, each testable on its own.** The switch puts every
  strategy inside one function that grows with each kind. `keyword.go`
  knows nothing of `experience.go`.
- **Adding a kind is additive.** Write `answer_depth.go`, add one map
  line. Nothing existing is edited, so nothing existing can break.
- **The signature is enforced.** A strategy that forgets to return a
  detail, or wants an extra parameter, does not compile. In a switch each
  case can quietly do its own thing.
- **The dispatch has one unknown-kind path.** `spec, ok := registry[c.Kind]`
  — `!ok` is the only place an unknown kind is detected
  ([`engine.go:210`](../../internal/scoring/engine.go#L210)). A switch needs a
  `default:` someone has to remember.
- **The engine owns the shared rules.** The strategy returns a fraction in
  `outcome`; the engine multiplies by the weight and clamps to 0–1
  ([`engine.go:319`](../../internal/scoring/engine.go#L319)). So "no criterion
  can earn more than its weight" is enforced in one place, for every
  strategy, including ones not written yet.

A switch is fine for three trivial cases. It stops being fine when each case
is real logic with its own config, which is the situation here.

Why not an interface (`type Scorer interface { Score(...) }`) with one
struct per kind? It would work. A one-method interface and a function type
are the same idea in Go; the function type is less ceremony when the
strategy has no state beyond what its closure captures.

## 5. Compile once, score many

**📍 Open:** [`internal/scoring/engine.go:177`](../../internal/scoring/engine.go#L177)

The brief asked for `Score(answers, criteria) (Result, error)` and a
registry of scorers. That function exists
([`engine.go:281`](../../internal/scoring/engine.go#L281)), but it is two
steps underneath, and the registry maps to *constructors*:

1. `Compile(criteria) (*Rubric, error)` — decode every config, validate it,
   compile every regexp. All errors happen here.
2. `(*Rubric).Score(answers) Result` — no error return. Scoring cannot fail.

The keyword strategy is why. It must compile a regexp per keyword (section
8), and compiling is expensive (section 10). Three ways to avoid doing it
for every application:

- **A package-level cache** (`sync.Map` from config to regexp). Hidden
  mutable state — the package would no longer be pure, and the cache never
  shrinks.
- **Compile inside `Score` every time.** Simple, and 34× slower (section 10).
- **Make compilation a separate step** whose result is an immutable value.
  This one. It keeps purity, and `Score` is still there as a one-call
  convenience.

A useful side effect: every way a rubric can be broken (unknown kind, bad
config, negative weight) is now discovered *before* any application is
looked at. A future "save rubric" endpoint can call `Compile` and reject a
broken rubric with a 422, using the same code that will later score it.

## 6. The edge cases

**📍 Open:** [`internal/scoring/engine.go:273`](../../internal/scoring/engine.go#L273)

All four are listed in the package doc comment, where a caller reads them.

**Empty criteria — no divide-by-zero.** `Percentage` is the package's only
division, and it is guarded by `if res.TotalAvailable > 0`. In Go,
`0.0 / 0.0` does not panic; it produces `NaN`, which then compares false to
everything, encodes to JSON as an *error*, and poisons every sum it joins.
The guard also covers a rubric whose weights are all zero (zero weight
"parks" a criterion, per the schema), which an `if len(criteria) == 0`
check would have missed. `Breakdown` is `make(..., 0, n)`, not `nil`, so
it encodes as `[]`.

**Unknown kind — an error.** A rubric the engine cannot fully apply should
not produce a number. Scoring the criteria it knows and skipping the rest
would give a total out of the wrong denominator, which looks like a valid
score. The error is a `*CriterionError` wrapping `ErrUnknownKind`, so the
caller can `errors.Is` the category and `errors.As` for the criterion ID.

**Missing answer — zero, not an error.**
([`engine.go:305`](../../internal/scoring/engine.go#L305)) A question can be
optional; a candidate who skips it has submitted a perfectly valid
application. An error here would mean one skipped optional question makes
the whole application unscoreable — and an error is about the *system*
being wrong, which it is not. Zero says exactly what happened: the candidate
earned none of that criterion's points. `Detail` says why, so a reviewer
does not mistake it for a bad answer, and `EvidenceAnswerID` is empty
because there is no answer to point at.

The rule of thumb: **errors are for the rubric author, scores are for the
candidate.** Anything the candidate typed — missing, empty, "a few",
"-3" — produces a score and an explanation. Only a broken rubric produces
an error.

`singleAnswer` is where the missing-answer rule lives, once. It is an
*adapter*: it takes a function of one answer and returns a `scorer`. So a
single-answer strategy never sees the missing case at all.

## 7. `keyword_match`, and the `strings.Contains` bug

**📍 Open:** [`internal/scoring/keyword.go:81`](../../internal/scoring/keyword.go#L81)

The score is the share of keywords found, times the weight. The obvious
implementation is
`strings.Contains(strings.ToLower(answer), strings.ToLower(keyword))`.
That is a substring test, and for short keywords it is badly wrong:

| Keyword | Answer | `strings.Contains` | Correct |
|---|---|---|---|
| `go` | "I search with google" | match | no match |
| `go` | "two years ago" | match | no match |
| `go` | "it is going well" | match | no match |
| `go` | "I write golang" | match | no match |
| `java` | "javascript only" | match | no match |
| `sql` | "nosql" | match | no match |

Why this is a serious bug, not a cosmetic one:

- **It changes who gets hired.** The score ranks applicants. A candidate who
  wrote "I started coding a long time ago" outscores one who wrote nothing
  about Go — the ranking is wrong, not just the label.
- **It is silent.** Nothing errors. The scorecard says "matched 1 of 5
  keywords: go", which looks correct unless someone reads the answer.
- **It is unfair in a way that can be gamed.** Anyone who knows the trick
  pads an answer with "google", "ago" and "going".
- **The score is meant to be defensible.** The schema makes `detail`
  `NOT NULL` because a rejection has to be explainable to the candidate.
  "You matched 'go' because you wrote 'ago'" is not defensible.

This was checked, not assumed: swapping `strings.Contains` into
`matchKeywords` makes 9 test cases fail (verification table, section 14).

Each keyword counts once however often it appears ("go go go" is 1 of 5),
and the matched keywords are listed in config order so the `Detail` is
stable.

## 8. Word boundaries, and why not `\b`

**📍 Open:** [`internal/scoring/keyword.go:74`](../../internal/scoring/keyword.go#L74)

A **word boundary** is a position *between* characters, where a word
character is on one side and a non-word character (or the start/end of the
text) is on the other. In most regexp engines `\b` matches that position
without consuming a character. So `\bgo\b` matches "go" in "I write go." —
there is a boundary between the space and "g", and between "o" and "." —
but not in "google", because between "o" and "o" there is none.

The brief asked for word boundaries. The code uses explicit edges instead
of `\b`:

```go
(?i)(?:^|[^\p{L}\p{N}_])   +   QuoteMeta(kw)   +   (?:[^\p{L}\p{N}_]|$)
```

"Start of text or a non-word character", the keyword, then "a non-word
character or end of text". Two reasons `\b` is not enough here:

1. **`\b` needs a word character on one side.** In "I write C++ and Go",
   after the last `+` comes a space. Both are non-word characters, so there
   is *no* boundary there, and `\bc\+\+\b` can never match "C++" followed by
   a space. It does match inside "c++11", because `+`→`1` is a boundary. So
   `\b` gets the required `c++` test exactly backwards. Same for `c#`,
   `.net`, `[go]`.
2. **Go's `\b` is ASCII-only.** RE2 (the engine behind `regexp`) defines a
   word character as `[0-9A-Za-z_]`. "é" is not one, so `\bcaf\b` matches
   inside "café". `\p{L}` (any letter, any script) and `\p{N}` (any digit)
   fix that.

Swapping in `\b` makes 10 test cases fail (section 14), which is the
evidence for the choice.

Go's `regexp` has no lookahead or lookbehind (RE2 omits them to guarantee
linear-time matching), so the edge characters become part of the match.
That is harmless: we only ask *whether* there is a match, never where.

`(?i)` makes the pattern case-insensitive using Unicode case folding, so the
keyword "golang" matches "Golang" and the keyword "PostgreSQL" matches
"postgresql".

A hyphen counts as a non-word character, so "go" matches "go-based".
That is how `\b` behaves too, and is the reading a recruiter expects.

## 9. What `regexp.QuoteMeta` protects against

**📍 Open:** [`internal/scoring/keyword.go:76`](../../internal/scoring/keyword.go#L76)

Keywords are typed by a recruiter. Pasted into a pattern unescaped, they are
*interpreted* as regexp syntax:

| Keyword | Unescaped, it means | Effect |
|---|---|---|
| `node.js` | `node`, any one character, `js` | matches "nodexjs" |
| `c++` | `c`, one or more, one or more | **fails to compile** (`invalid nested repetition operator`) |
| `f(x` | unclosed group | **fails to compile** |
| `a\|b` | "a" or "b" | matches every answer containing "a" |
| `[go]` | one character, g or o | matches any "o" |

`QuoteMeta` escapes every metacharacter (`\.+*?()|[]{}^$`), so the keyword
is matched as literal text. It is the regexp equivalent of a parameterised
SQL query: data stays data and cannot change the meaning of the code around
it. Each row in the table above is a test case
([`keyword_test.go:199`](../../internal/scoring/keyword_test.go#L199)).

The code still checks `regexp.Compile`'s error rather than using
`MustCompile`, which panics. After `QuoteMeta` a failure is essentially
impossible, but keywords are user data, and a panic would take down the
request; an error is reported against the criterion.

## 10. What precompiling saves

**📍 Open:** [`internal/scoring/keyword_test.go:254`](../../internal/scoring/keyword_test.go#L254)

`regexp.Compile` parses the pattern and builds a program for the matching
machine. That is real work — and `\p{L}` is a character class of every
letter in Unicode, so these patterns are expensive to build. `MatchString`
on an already-compiled `*regexp.Regexp` only runs the program.

The two benchmarks score the same ten-keyword criterion against the same
paragraph, once compiling every time (as `Score` does) and once with the
rubric compiled up front:

| Benchmark | Time per op | Memory per op | Allocations |
|---|---|---|---|
| `BenchmarkScore` (compile every call) | ~967 µs | ~811 KB | 712 |
| `BenchmarkRubricScore` (compiled once) | ~28 µs | ~1.4 KB | 8 |

About **34× faster and 590× less memory**. Scoring one application on
submit, a millisecond does not matter. Rescoring 10,000 applications after
a rubric revision, it is 9.7 seconds versus 0.3, and 8 GB of garbage for
the collector versus 14 MB. That is why section 5 split compilation out.

`*regexp.Regexp` is safe for concurrent use, so one compiled rubric can be
shared by any number of goroutines.

## 11. `experience_years`: `ParseFloat` and clamping

**📍 Open:** [`internal/scoring/experience.go:57`](../../internal/scoring/experience.go#L57)

**How `strconv.ParseFloat` reports failure.** Like most of Go's standard
library it returns two values, `(float64, error)`, rather than throwing.
On failure the `error` is a `*strconv.NumError` wrapping either
`strconv.ErrSyntax` ("a few", "", "2 years") or `strconv.ErrRange`
("1e400"). The float on failure is `0` for a syntax error, and `±Inf` for a
range error.

That last detail is why ignoring the error is a bug and not just bad style:

- `years, _ := strconv.ParseFloat("a few", 64)` gives `0`. With `min` 0 that
  scores 0 — correct by accident, with a wrong `Detail` ("0 years, at or
  below the minimum") that tells the reviewer the candidate claimed zero.
- With `min` 2 and an answer of "a few", `0` still scores 0 — but the
  explanation is now a lie about what the candidate said.
- `"1e400"` returns `+Inf` *and* an error. Ignoring the error, `+Inf` passes
  `years >= target` and scores **full marks**.

So the code checks `err` and also `math.IsNaN` / `math.IsInf`, because
`ParseFloat("NaN")` and `ParseFloat("Inf")` succeed with no error at all.
Submission validation already refuses those
([`submission.go:271`](../../internal/domain/submission.go#L271)), but the
engine is pure and should not depend on who called it. An unreadable answer
scores 0 with `could not read "a few" as a number of years`, quoting at most
40 characters of it so a 10 KB answer does not end up in every score row.

**Why clamping matters.** The ramp is
`(years − min) / (target − min)`. With min 0 and target 2:

| Years | Unclamped fraction | Weight 10 earns |
|---|---|---|
| 1 | 0.5 | 5 |
| 2 | 1.0 | 10 |
| 5 | 2.5 | **25** |
| 20 | 10.0 | **100** |

Unclamped, 5 years earns 250% of the criterion's weight. A rubric of
"experience, weight 10" and "keyword match, weight 30" would then be
dominated by experience for anyone senior, which is not what the recruiter
set up — the weights are supposed to be the ceiling. The total could also
exceed `TotalAvailable`, so `Percentage` could pass 100. The strategy clamps
at `years >= target` (with a `Detail` saying so), and the engine clamps
every fraction again as a safety net (section 4).

Negative answers score 0 with their own `Detail` ("-1 years is negative").
Config is checked at compile time: `target` is required (a pointer, so
"absent" and `0` differ), `min` may not be negative, and `target` must be
greater than `min` — otherwise the ramp divides by zero.

## 12. `boolean_all`: a strategy that reads several answers

**📍 Open:** [`internal/scoring/boolean.go:22`](../../internal/scoring/boolean.go#L22)

Every other strategy reads the answer to `criterion.QuestionID`.
`boolean_all` reads the answers to every question in
`config.question_ids`, and earns the full weight only if all of them are
true.

**How the interface stayed clean.** The common signature is the general
one: `scorer` receives the *whole* answer index (a map from question ID to
answer). A strategy that needs several answers — `boolean_all` — implements
`scorer` directly and looks up what it needs. A strategy that needs one is
written against a single `domain.Answer` and wrapped by `singleAnswer`
(section 6), which does the lookup, the missing-answer rule and the evidence
link. So:

- the one-answer strategies are as simple as if the interface took one
  answer;
- the many-answer strategy is not forced through a one-answer signature;
- the engine does not need to know which kind is which.

Rejected alternatives:

- **Pass `[]domain.Answer` to every scorer.** Each strategy would loop to
  find its answer, and repeat the missing-answer logic.
- **Two signatures and a flag in the registry** ("this kind wants many").
  The engine would branch on kind again, which is what the registry exists
  to avoid.
- **Make `QuestionID` a list.** It is one nullable column in the schema.

Other decisions:

- **All-or-nothing.** 2 of 3 earns 0, as asked; the `Detail` still says
  "2 of 3 conditions met" so a reviewer sees how close it was.
- **A missing answer is "not met", not an error,** for the same reason as
  section 6. The `Detail` adds "(1 unanswered)" so a skip is not mistaken
  for a "false".
- **`strconv.ParseBool`** accepts `true`, `True`, `TRUE`, `t` and `1`.
  Submission stores booleans as exactly `"true"` / `"false"`, so this only
  adds tolerance; anything it rejects is "not met".
- **An empty `question_ids` is a config error.** "All of no conditions are
  met" is vacuously true and would give every candidate full marks.
- **Setting `question_id` as well is a config error**, so there is one
  source of truth for which questions are read.
- **`EvidenceAnswerID` is empty,** matching the schema comment on
  `scores.evidence_answer_id`: the score rests on several answers.

## 13. Go idioms new in this part

- **Function types.** `type scorer func(answerIndex) outcome` gives a
  function signature a name, so it can be a map value, a parameter or a
  return type, and the compiler checks every strategy against it.
- **Closures.** `newKeywordMatch` returns a function that refers to
  `patterns`, a local variable of the outer function. That variable lives on
  as long as the returned function does — which is how a compiled criterion
  carries its regexps without a struct.
- **`json.RawMessage`** — section 3.
- **`json.Decoder.DisallowUnknownFields`.** `json.Unmarshal` ignores unknown
  keys; the `Decoder` can be told not to. `dec.Token()` returning `io.EOF`
  after the first value confirms nothing trails it.
- **Two `%w` in one `fmt.Errorf`** (Go 1.20+): `fmt.Errorf("%w: %w",
  ErrInvalidConfig, err)` keeps both the category and the decoder's error in
  the chain, so `errors.Is` finds either.
- **An error type with `Unwrap`.** `*CriterionError` carries the criterion
  ID and wraps the category, so a caller uses `errors.As` for the detail and
  `errors.Is` for the category — the same pattern as
  `*domain.FormLockedError`.
- **NaN compares false to everything.** `!(f > 0)` in `clampFraction` is
  true for `NaN`, where `f <= 0` would be false. That is why it is written
  the "odd" way.
- **`[]rune(s)[:n]`** truncates by characters, not bytes, so a multi-byte
  character is never cut in half.
- **`b.Loop()`** (Go 1.24+) is the modern benchmark loop: it replaces
  `for i := 0; i < b.N; i++` and keeps setup before the loop out of the
  timing.

## 14. The tests, and verification

**📍 Open:** [`internal/scoring/engine_test.go:121`](../../internal/scoring/engine_test.go#L121)

The tests are in `package scoring_test` — a separate, *external* test
package — so they can only use the exported API: `Score`, `Compile`,
`Result`. That keeps them honest about what a caller sees, and lets the
internals change freely.

| File | Tests |
|---|---|
| [`engine_test.go`](../../internal/scoring/engine_test.go#L121) | Rewritten in part 2 around a full rubric; see [06-2 §8](06-2-depth-and-validation.md#8-the-whole-engine-suite) |
| [`keyword_test.go`](../../internal/scoring/keyword_test.go#L55) | `TestKeywordMatch_WholeWordOnly`, `_CaseInsensitive`, `_PartialCredit`, `_EmptyAnswer`, `_EmptyKeywordList`, `_RegexpSpecialChars`; two benchmarks |
| [`experience_test.go`](../../internal/scoring/experience_test.go#L26) | `TestExperienceYears_LinearRamp`, `_Unparseable`, `_Negative`, `_AboveTarget`, `_InvalidConfig` |
| [`boolean_test.go`](../../internal/scoring/boolean_test.go#L24) | `TestBooleanAll_RequiresAll`, `_AllTrue`, `_MissingAnswer`, `_InvalidConfig` |

**Verification**

| Command | Result |
|---|---|
| `go test -race -cover ./internal/scoring/ ./internal/domain/` | pass; scoring 98.0% coverage, domain 95.9% |
| `matchKeywords` temporarily using `strings.Contains` | 9 cases fail: google, ago, going, golang, cargo, café, two non-ASCII neighbours, c++11 |
| `compileKeyword` temporarily using `\b…\b` | 10 cases fail: every `c++` case, `c#`/`.net`, `[go]`, café, both non-ASCII neighbours |
| `make fmt` | no changes |
| `make lint` | 0 issues |
| `make test` | every package passes |
| `go build ./...` | builds |
| `go test -run '^$' -bench . -benchmem ./internal/scoring/` | 967 µs / 811 KB / 712 allocs per op compiling every call; 28 µs / 1.4 KB / 8 allocs compiled once (Apple M4) |

Both temporary swaps were reverted, and the suite re-run green, before
anything else.
