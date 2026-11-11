# Module 6, part 2 — Answer depth, rubric validation, and the whole-engine suite

This note finishes the scoring engine. It covers the fourth strategy,
`answer_depth`, and the difference between bytes and characters
(sections 1–2); `ValidateCriteria` and why it reports every problem at once
(sections 3–5); why weights are compared with a tolerance and never `==`
(section 6); turning decoder errors into messages a client may see
(section 7); the whole-engine test suite (section 8); **a hand calculation
of the full rubric, criterion by criterion** (section 9); the fuzz test and
whether it earns its place (section 10); coverage, and which uncovered lines
matter (section 11); proving the package is pure (section 12); new Go idioms
(section 13); and what was run (section 14).

Part 1 is [06-1-scoring-engine.md](06-1-scoring-engine.md).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-answer_depth) | `answer_depth` | [`depth.go:25`](../../internal/scoring/depth.go#L25) |
| [2](#2-bytes-runes-and-a-candidate-writing-in-bengali) | Bytes, runes, and a candidate writing in Bengali | [`depth.go:52`](../../internal/scoring/depth.go#L52) |
| [3](#3-validatecriteria-and-why-it-collects-every-problem) | `ValidateCriteria`, and why it collects every problem | [`validate.go:27`](../../internal/scoring/validate.go#L27) |
| [4](#4-a-kind-that-needs-a-question) | A kind that needs a question | [`engine.go:219`](../../internal/scoring/engine.go#L219) |
| [5](#5-one-list-of-rules-two-ways-to-report-them) | One list of rules, two ways to report them | [`engine.go:199`](../../internal/scoring/engine.go#L199) |
| [6](#6-why-weights-are-never-compared-with-) | Why weights are never compared with `==` | [`validate.go:18`](../../internal/scoring/validate.go#L18) |
| [7](#7-config-errors-a-client-may-see) | Config errors a client may see | [`engine.go:379`](../../internal/scoring/engine.go#L379) |
| [8](#8-the-whole-engine-suite) | The whole-engine suite | [`engine_test.go:121`](../../internal/scoring/engine_test.go#L121) |
| [9](#9-the-full-rubric-by-hand) | **The full rubric, by hand** | [`engine_test.go:121`](../../internal/scoring/engine_test.go#L121) |
| [10](#10-the-fuzz-test-and-whether-it-is-worth-it) | The fuzz test, and whether it is worth it | [`keyword_test.go:284`](../../internal/scoring/keyword_test.go#L284) |
| [11](#11-coverage-and-the-lines-it-misses) | Coverage, and the lines it misses | — |
| [12](#12-proving-the-package-is-pure) | Proving the package is pure | — |
| [13](#13-go-idioms-new-in-this-part) | Go idioms new in this part | — |
| [14](#14-verification) | Verification | — |

---

## 1. `answer_depth`

**📍 Open:** [`internal/scoring/depth.go:25`](../../internal/scoring/depth.go#L25)

Config `{"min_chars": 50, "target_chars": 400}`. The same ramp as
`experience_years`, on a character count instead of a number:

| Characters | Fraction | Weight 10 earns |
|---|---|---|
| 40 | below min → 0 | 0 |
| 50 | (50 − 50) / 350 = 0 | 0 |
| 225 | (225 − 50) / 350 = 0.5 | 5 |
| 400 | at target → 1 | 10 |
| 5000 | clamped → 1 | 10 |

"Below `min_chars` scores 0" and the ramp agree at the edge: at exactly
`min_chars` the ramp is already 0, so there is no jump. The fields are
`int`, so `{"target_chars": 50.5}` is a config error rather than a silently
truncated 50. `target_chars` is a pointer so "absent" is an error, not 0;
`target_chars <= min_chars` is an error because the ramp would divide by
zero — the same three rules as `experience_years`, for the same reasons.

**Whitespace is collapsed before counting** (`strings.Fields` splits on any
run of Unicode whitespace and drops the ends; joining with one space puts
back exactly one separator per gap). Without it, an answer of twenty words
followed by 300 spaces would score as 380 characters. That is a cheaper
trick to pull than padding with words, and the test
"whitespace collapsed" pins it ([`depth_test.go:58`](../../internal/scoring/depth_test.go#L58)).

This is stricter than the form's `min_length` check, which trims the ends
but counts inner whitespace
([`submission.go:305`](../../internal/domain/submission.go#L305)). That is
deliberate: `min_length` decides whether an answer is *accepted*; here
characters are worth points.

## 2. Bytes, runes, and a candidate writing in Bengali

**📍 Open:** [`internal/scoring/depth.go:52`](../../internal/scoring/depth.go#L52)

A Go `string` is a read-only slice of **bytes**. Go source and most text are
UTF-8, where one character takes one to four bytes:

- **`len(s)`** returns the number of bytes. It is O(1) — the length is
  stored with the string.
- **`utf8.RuneCountInString(s)`** returns the number of **runes**. A rune is
  Go's name for a Unicode code point (`rune` is an alias of `int32`). It
  has to walk the whole string, decoding as it goes.

Measured, not assumed:

| String | Meaning | `len` (bytes) | `RuneCountInString` |
|---|---|---|---|
| `Go` | — | 2 | 2 |
| `café` | — | 5 | 4 |
| `আমি` | "I" in Bengali | 9 | 3 |
| `বাংলা` | "Bangla" | 15 | 5 |
| `👋` | waving hand | 4 | 1 |

Every Bengali letter is in the block U+0980–U+09FF, which UTF-8 encodes in
**three bytes**. So for a candidate writing in Bengali, `len` reports three
times what they wrote. Counting bytes, with min 50 and target 400:

- 134 Bengali characters is 402 bytes: **full marks** for about a third of
  the answer an English writer needs.
- 17 Bengali characters is 51 bytes: **above the minimum** with a sentence
  fragment.

That is not a rounding error — it is a rule that grades people by which
script they type in. `TestAnswerDepth_CountsRunesNotBytes`
([`depth_test.go:104`](../../internal/scoring/depth_test.go#L104)) gives
120 characters in Bengali, emoji and accented Latin, and checks that each
earns exactly half of a 240 target; counted in bytes they would all earn
full marks. The test checks its own samples' byte and rune counts first,
so a typo in a sample cannot make it pass for the wrong reason.

**What runes still get wrong.** A rune is a code point, not what a reader
sees as one character (a *grapheme cluster*). Bengali vowel signs are their
own code points: `বাংলা` looks like three or four units to a reader and is
five runes. A family emoji 👨‍👩‍👧 is five runes (three faces joined by two
zero-width joiners) and 18 bytes. Counting graphemes properly needs the
Unicode segmentation rules, which the standard library does not have; it
would mean a third-party dependency in a package whose point is having
none. Runes are within a small factor of what a reader counts, in every
script, where bytes are off by up to 4× and unevenly by script. They also
match what the form's `min_length` already counts. That is the trade.

## 3. `ValidateCriteria`, and why it collects every problem

**📍 Open:** [`internal/scoring/validate.go:27`](../../internal/scoring/validate.go#L27)

`ValidateCriteria(criteria) error` is the check to run before a rubric is
saved. It returns `nil` or a `*domain.ValidationError` — the type the API
already turns into a 422 with a `details` object
([`errors.go`](../../internal/domain/errors.go)) — keyed by field:

```json
{
  "criteria[0].weight":      "must be a finite number of at least 0",
  "criteria[1].kind":        "must be one of answer_depth, boolean_all, experience_years, keyword_match",
  "criteria[2].config":      "keywords must list at least one keyword",
  "criteria[3].question_id": "is required for kind answer_depth",
  "criteria[4].weight":      "must have at most two decimal places",
  "criteria":                "weights must sum to 100; they sum to 40.01"
}
```

That is the output of `TestValidateCriteria_CollectsAllErrors`
([`validate_test.go:167`](../../internal/scoring/validate_test.go#L167)), where
five criteria are broken five ways.

**Why every problem, not the first.** A rubric is edited as a whole, in a
form, by a person. Stopping at the first error means:

1. The recruiter fixes criterion 0, saves, and is told about criterion 1.
   Fixes it, saves, criterion 2. Six problems, six round trips — each one
   feeling like the system moving the goalposts.
2. Some problems interact. The weights sum to 40 *because* criterion 0 is
   −30. Seeing both at once explains the sum; seeing them one at a time
   makes the second look unrelated.
3. A client can highlight every bad field at once, because each problem is
   keyed by its field.

The rule is the same one the form validation uses
([05-1](05-1-application-validation.md)): collect when the input is a
document a person will correct; fail fast when the first failure makes the
rest meaningless. `ValidateCriteria` does both where each fits:

- An **unknown kind** stops the checks *for that criterion*, because the
  kind decides what the config means — but not for the others.
- A **NaN or infinite weight** skips the sum check, because a sum containing
  NaN says nothing.
- An **empty rubric** returns immediately: there is nothing else to check.

What `ValidateCriteria` adds on top of `Compile`: at least one criterion,
weights with at most two decimals (what the `numeric(6, 2)` column can
store), and weights summing to 100. `Compile` deliberately does not require
the sum: the percentage is of what is available, so an unbalanced rubric
still scores consistently, and a draft rubric can be previewed.

## 4. A kind that needs a question

**📍 Open:** [`internal/scoring/engine.go:219`](../../internal/scoring/engine.go#L219)

In part 1, a `keyword_match` criterion with no `question_id` scored
"no answer provided" for everyone. That was a quiet failure: the rubric was
wrong, and it showed up as every candidate losing the same points. Now each
registry entry says whether its kind reads its own question:

```go
domain.CriterionKindKeywordMatch: {build: newKeywordMatch, needsQuestion: true},
domain.CriterionKindBooleanAll:   {build: newBooleanAll},
```

`Compile` refuses with `ErrMissingQuestion`, and `ValidateCriteria` reports
`criteria[i].question_id`. `boolean_all` is the exception — it reads
`config.question_ids`, and already refuses a `question_id` on top.

The distinction the package now draws: **a missing question is the rubric
author's mistake (an error); a missing answer is the candidate's choice
(a score of 0).**

## 5. One list of rules, two ways to report them

**📍 Open:** [`internal/scoring/engine.go:199`](../../internal/scoring/engine.go#L199)

`checkCriterion` runs every per-criterion check and returns a list of
problems. `Compile` takes the first and stops; `ValidateCriteria` takes all
of them. The alternative — two separate sets of checks — is how a rubric
ends up passing validation and then failing to compile, or the other way
round. With one function, that cannot happen.

Each problem carries two things: a `message` safe to show a client, and an
`err` wrapping a sentinel (`ErrUnknownKind`, `ErrInvalidConfig`,
`ErrMissingQuestion`) for code that branches with `errors.Is`.

## 6. Why weights are never compared with `==`

**📍 Open:** [`internal/scoring/validate.go:18`](../../internal/scoring/validate.go#L18)

A `float64` stores a number as a binary fraction. Most decimal fractions —
0.1, 5.6, 66.6 — have no exact binary form, just as 1/3 has no exact
decimal form. Each is stored as the nearest representable value, and adding
them adds the tiny errors too.

A real rubric:

```go
var sum float64
for _, w := range []float64{5.6, 66.6, 27.8} {
    sum += w
}
// sum is 99.999999999999986, and sum == 100 is false
```

Checking with `==` would tell a recruiter that 5.6 + 66.6 + 27.8 is not 100.
This is not rare: across about 1.5 million three-weight rubrics with
two-decimal weights summing to exactly 100 on paper, about 8.5% missed 100
by a rounding error when added as `float64` (scratch program, section 14).
A test pins one such case and checks that it really does miss 100, so the
test cannot pass by accident
([`validate_test.go:43`](../../internal/scoring/validate_test.go#L43)).

A trap when trying this yourself: `fmt.Println(0.1+0.2 == 0.3)` prints
**true**. Those are *untyped constants*, which Go evaluates exactly at
compile time. Only once the values are `float64` variables does the
rounding appear — `a, b := 0.1, 0.2; a+b == 0.3` is false.

**What tolerance, and why `1e-6`.** The tolerance has to sit between two
numbers:

- **Bigger than the rounding error.** Adding a few dozen weights near 100
  leaves an error of about 1e-14.
- **Smaller than the smallest real difference.** Weights are stored with two
  decimals, so a genuinely wrong sum is off by at least 0.01.

`1e-6` is eight orders of magnitude above the first and four below the
second. Anything in that band works; `0.005` (half a hundredth) is the
other defensible choice. What does not work is a tolerance as wide as the
data's own resolution: `0.01` would accept 99.99.

The two-decimal check uses the same reasoning
([`validate.go:72`](../../internal/scoring/validate.go#L72)): `15.01 * 100`
is not exactly 1501 in floating point, so it is compared with its nearest
whole number with a tolerance. The sum is printed rounded to two places, so
the message says "40.01", not "40.004999999999995".

## 7. Config errors a client may see

**📍 Open:** [`internal/scoring/engine.go:379`](../../internal/scoring/engine.go#L379)

`ValidateCriteria`'s messages go to the client, and the domain's rule is
that field messages contain no internals. The JSON decoder's own messages
do: `json: cannot unmarshal string into Go struct field
keywordConfig.keywords of type []string` names a Go type. `decodeReason`
rewrites each kind of decoding error with `errors.As`:

| Config | Message |
|---|---|
| `{"keywords":` | `is not valid JSON` |
| `["go"]` | `must be a JSON object` |
| `{"keywords":"go"}` | `"keywords" has the wrong type: got string` |
| `{"min":0,"traget":2}` | `unknown field "traget"` |

Worth knowing: in Go 1.27, `encoding/json` is implemented on top of the
newer `encoding/json/v2` engine (the stack trace from a mistake in a scratch
program ran through `v2_stream.go`). The exported error types are the same,
which is why the code matches on types, not on message text — except for
"unknown field", which has no typed error, so the prefix is checked and
stripped.

The strategies' own reasons ("target is required") are carried in an
unexported `configError` type that wraps `ErrInvalidConfig`, so
`ValidateCriteria` can show the reason without the `invalid criterion
config:` prefix that logs get.

## 8. The whole-engine suite

**📍 Open:** [`internal/scoring/engine_test.go:121`](../../internal/scoring/engine_test.go#L121)

`engine_test.go` was rewritten around one realistic rubric,
`borderlessWorkRubric`, and one candidate, `borderlessWorkAnswers`. The
rubric's labels, kinds and weights are the ones from the brief; the brief's
overview document is not in the repository, so the keyword lists and
thresholds are mine, chosen for a backend Go role.

| Test | What it proves |
|---|---|
| `TestScore_BorderlessWorkRubric` | Every criterion's points and `Detail`, the total (70.25), the available (100) and the percentage — section 9 |
| `TestScore_MissingAnswer` | Dropping two answers costs exactly those criteria (70.25 → 54.25), no error; no answers at all is 0/100 |
| `TestScore_EmptyRubric` | nil and `{}` give zeroes with no NaN; so does an all-zero-weight rubric |
| `TestScore_UnknownKindReturnsError` | One bad kind in the middle of a valid rubric stops everything, and the error names that criterion |
| `TestScore_EvidenceIsSet` | Each single-answer criterion points at its answer, even when it earned 0; `boolean_all` points at none |
| `TestScore_PercentageMath` | Full, nothing, an unbalanced rubric (3/4 = 75%), a third, a zero-weight criterion |
| `TestScore_DeterministicAcrossRuns` | 50 runs bit-identical (`reflect.DeepEqual`, not "close"), answers in reverse order identical, a reused compiled rubric identical |
| `TestScore_MissingQuestion` | Each single-answer kind refuses an empty `question_id` |
| `TestCriterionError_Message` | The log message names criterion, kind and reason |

**Why 50 runs in the determinism test.** Go deliberately randomises the
order `range` visits a map, every time. The registry and the answer index
are both maps. If anything in the result depended on map order, repeated
runs are how it would show. (The package never ranges over a map while
scoring — the answer map is only looked up by key — and the test is what
proves it.)

**Why the exact total is compared with a tolerance.** 2/3 × 15 is not
exactly 10 in floating point, any more than 5.6 + 66.6 + 27.8 is 100.
`approxEqual` allows 1e-9 — far below a cent, far above rounding error.
Exact to the cent is the meaningful "exact" for a score stored as
`numeric(6, 2)`.

## 9. The full rubric, by hand

**📍 Open:** [`internal/scoring/engine_test.go:121`](../../internal/scoring/engine_test.go#L121)

Every criterion is one of three formulas, multiplied by its weight:

- **keyword_match:** keywords found ÷ keywords listed
- **ramp** (`experience_years`, `answer_depth`): (value − min) ÷ (target − min),
  0 at or below min, 1 at or above target
- **boolean_all:** 1 if every answer is true, else 0

Then: total = sum of points; percentage = total ÷ sum of weights × 100.

### 1. Go knowledge — keyword_match, weight 30

Keywords: `go`, `goroutine`, `channel`, `interface`, `context`.

> I have written Go for three years. Each request gets a context with a
> deadline, work fans out to goroutines, and results come back over a
> channel.

| Keyword | Found? | Why |
|---|---|---|
| go | ✓ | "Go" — case does not matter; space after it |
| goroutine | ✗ | only "goroutine**s**": the next character is a letter, so not a whole word |
| channel | ✓ | "channel." — a full stop is a boundary |
| interface | ✗ | not in the answer |
| context | ✓ | "context with" |

3 ÷ 5 = 0.6. 0.6 × 30 = **18**.

The "goroutines" miss is the whole-word rule working as designed, and it is
a lesson for whoever writes the rubric: list the plural if you mean it.

### 2. Database skill — keyword_match, weight 20

Keywords: `postgres`, `index`, `transaction`, `migration`.

> I run Postgres in production: every slow query gets an index, and money
> only moves inside a transaction.

postgres ✓, index ✓ ("index,"), transaction ✓ ("transaction."),
migration ✗. 3 ÷ 4 = 0.75. 0.75 × 20 = **15**.

### 3. Observability — keyword_match, weight 15

Keywords: `prometheus`, `grafana`, `tracing`.

> I export Prometheus metrics from every service, and I add tracing to each
> handler rather than relying on logs alone.

prometheus ✓, grafana ✗, tracing ✓ ("tracing to"). 2 ÷ 3 = 0.666… 0.666… × 15 = **10**.

### 4. Answer depth — answer_depth, weight 10

Config: min 50, target 400. The project answer is 260 characters (it is all
ASCII and has no double spaces, so characters, runes and bytes agree here).
To check the count yourself, see section 14.

(260 − 50) ÷ (400 − 50) = 210 ÷ 350 = 0.6. 0.6 × 10 = **6**.

### 5. Experience — experience_years, weight 15

Config: min 0, target 4. Answer: "3".

(3 − 0) ÷ (4 − 0) = 0.75. 0.75 × 15 = **11.25**.

### 6. Remote readiness — boolean_all, weight 10

Questions: overlap hours, reliable internet, workspace. Answers: true, true,
true. All three → 1. 1 × 10 = **10**.

### Total

| Criterion | Formula | Points | Out of |
|---|---|---|---|
| Go knowledge | 3/5 × 30 | 18 | 30 |
| Database skill | 3/4 × 20 | 15 | 20 |
| Observability | 2/3 × 15 | 10 | 15 |
| Answer depth | 210/350 × 10 | 6 | 10 |
| Experience | 3/4 × 15 | 11.25 | 15 |
| Remote readiness | 1 × 10 | 10 | 10 |
| **Total** | | **70.25** | **100** |

Percentage: 70.25 ÷ 100 × 100 = **70.25%**. The test asserts every number in
this table, and every `Detail` string, and passes.

**Try changing one thing.** Answer "false" to the workspace question: remote
readiness drops to 0 (all or nothing), total 60.25. Add the word
"goroutine" to the Go answer: 4/5 × 30 = 24, total 76.25. Doing one of these
on paper and then in the test is the fastest way to trust the engine.

## 10. The fuzz test, and whether it is worth it

**📍 Open:** [`internal/scoring/keyword_test.go:284`](../../internal/scoring/keyword_test.go#L284)

A **fuzz test** is a test function that takes generated inputs. Go's fuzzer
starts from a seed corpus (`f.Add`) and mutates it, keeping inputs that
reach new code paths, looking for any input that makes the test fail. Plain
`go test` runs only the seeds, so the fuzz test is also an ordinary
regression test in CI; `go test -fuzz` runs the generator.

A fuzz test is only worth having if it checks **properties** — statements
true for every input — rather than "it did not crash". The keyword matcher
has four real ones:

1. Scoring never panics, and an answer never causes an error.
2. A one-keyword criterion earns all of its weight or none.
3. The keyword between spaces always matches.
4. A keyword made only of letters, digits and `_` never matches inside
   `"z" + keyword + "z"` — the "go" versus "google" rule for *every* such
   keyword, in every script.

Property 4 holds because such a text contains no non-word character, so a
match would need the keyword to touch the start or end of the text, and the
added `z` on each side rules that out — case folding maps one rune to one
rune, so it cannot change that.

**Verdict: worth it, modestly.** It is ~70 lines, its seeds run in
milliseconds on every `go test`, and properties 3 and 4 are the matcher's
whole contract stated generally. A minute of fuzzing ran **227,330** inputs
with no failure. Writing it also caught a mismatch in the test itself: the
first draft used `unicode.IsDigit` (decimal digits only), while the
pattern's `\p{N}` is every Unicode number (`²`, `Ⅻ`), so the helper now
uses `unicode.IsNumber`.

What it is *not*: a hunt for bugs in the regexp engine. RE2 is linear-time
by design and fuzzed upstream. And the fuzzer's throughput sometimes fell
to near zero — generated keywords grow long, and every iteration compiles
them, which is the cost from 06-1 §10 again. Not worth fuzzing:
`experience_years` and `answer_depth`, whose inputs are one number and one
count; their table tests already cover every branch.

## 11. Coverage, and the lines it misses

`go test -cover`: **98.2%** of statements. Four lines are never run:

| Line | Code | Does it matter? |
|---|---|---|
| [`engine.go:324`](../../internal/scoring/engine.go#L324) | `clampFraction`: `return 1` | **No.** Every strategy already caps its own fraction at 1, so the engine's clamp never fires. It is a safety net for a future strategy, and it cannot be reached through the public API. |
| [`engine.go:355`](../../internal/scoring/engine.go#L355) | `configReason` fallback | **No.** Reached only if a constructor returned an error that is not a `configError`; none does. |
| [`engine.go:394`](../../internal/scoring/engine.go#L394) | `decodeReason` default | **No.** Every error the decoder produced for every malformed config tried falls into a typed case. Kept so a new error shape gets a safe message rather than a Go type name. |
| [`keyword.go:58`](../../internal/scoring/keyword.go#L58) | `regexp.Compile` failed | **No.** After `QuoteMeta` every keyword is a literal; the fuzzer compiled hundreds of thousands without one failure. Kept because a panic would be worse than an error. |

All four are defensive branches that exist so that a *future* mistake fails
safely. Forcing coverage of them would mean exporting internals or
injecting failures, which would cost more than it proves. The two `Error()`
methods were uncovered at first too; those **did** matter — they are what a
log shows — so `TestCriterionError_Message` now pins the text.

Coverage is a floor, not a goal: the mutation checks in 06-1 (swapping in
`strings.Contains` or `\b` and watching tests fail) say more about these
tests than the percentage does.

## 12. Proving the package is pure

Purity is about what the code *does*, and no tool proves that alone. Three
checks together come close.

**The direct imports are standard library, plus `domain`:**

```
bytes encoding/json errors fmt io math regexp slices strconv strings
unicode/utf8 github.com/moshfiqamin/hiredesk/internal/domain
```

**The whole dependency tree has no framework.** `go list -deps` prints
every package the package needs, transitively: 83 packages. Filtering to
non-standard-library ones leaves exactly two, this module's own:

```
github.com/moshfiqamin/hiredesk/internal/domain
github.com/moshfiqamin/hiredesk/internal/scoring
```

No `gin`, no `pgx`, no `net/http`, no `database/sql` — checked one by one.

**Honest caveat.** The tree does contain `time`, `os` and `net/url`. Traced:
`time` comes from `domain` (it uses `time.Time` as a field type) and from
`os` and `encoding/json` internally; `os` comes from `fmt` (which can print
to stdout); `net/url` comes from `domain`'s CV URL check. *Importing* a
package is not *calling* it. So the third check is the code itself:

**No call that could make it impure.** `grep` for `time.`, `rand.`, `os.`,
`http.`, `sql.` in the package's non-test files finds nothing.

Together: no framework is reachable, nothing in the package touches the
clock, randomness or I/O, and the determinism test confirms the behaviour.

## 13. Go idioms new in this part

- **`len` versus `utf8.RuneCountInString`** — section 2.
- **Untyped constants are exact.** `0.1 + 0.2 == 0.3` is true as constants
  and false as `float64` variables — section 6.
- **A struct as a map value.** `map[string]kindSpec{...: {build: ...,
  needsQuestion: true}}` — the element type can be omitted inside a
  composite literal, so each entry is just `{...}`.
- **`for run := range 50`** (Go 1.22+) loops 50 times; ranging over an
  integer replaces `for i := 0; i < 50; i++`.
- **`reflect.DeepEqual`** compares two values field by field, through slices
  and pointers. Right for "bit-identical" in a test; too slow and too
  strict (NaN ≠ NaN) for production comparisons.
- **`slices.Clone` and `slices.Reverse`** — a copy to reverse, so the
  original order is still available.
- **`testing.F`, `f.Add`, `f.Fuzz`** — section 10.
- **`errors.As` against several error types in one `switch`** —
  `decodeReason` tries `*json.UnmarshalTypeError`, then `*json.SyntaxError`.

## 14. Verification

| Command | Result |
|---|---|
| `go test -race -cover ./internal/scoring/` | pass, 98.2% of statements |
| `go vet ./internal/scoring/` | clean |
| `go test -race -count=1 ./internal/scoring/` | pass (`-count=1` defeats the test cache) |
| `go test -run '^$' -fuzz '^FuzzKeywordMatch$' -fuzztime 60s ./internal/scoring/` | 227,330 executions, no failure, no `testdata/` corpus written |
| `go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/scoring` | only `internal/domain` and `internal/scoring` |
| `go list -deps ./internal/scoring \| grep -E 'gin\|pgx\|net/http\|database/sql'` | nothing |
| `go tool cover -func` | the four uncovered lines in section 11 |
| scratch program: three-weight rubrics summing to 100 on paper | 128,883 of 1,516,515 miss 100 exactly as `float64`; `5.6 + 66.6 + 27.8` = 99.999999999999986 |
| scratch program: `len` and `RuneCountInString` | the table in section 2 |
| `make fmt && make lint && make test && go build ./...` | clean, 0 issues, all packages pass, builds |

**Checking the 260 yourself.** In the VS Code terminal, at the repo root:

```bash
printf '%s' "I rebuilt our invoicing service in Go. The old one timed out under load, so I profiled it, found an N+1 query, and replaced it with one join. I added tests for the slow paths first, then shipped behind a flag. p99 latency fell from 2.1s to 180ms within a week." | wc -m
```

Counts the characters in the project answer (`wc -m` counts characters, not
bytes); it should print 260.
