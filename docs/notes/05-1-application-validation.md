# Module 5, part 1 — Validating an application

This note covers what a candidate's submission is checked against before
anything is stored: the endpoint (section 1), the six rules and the order
they run in (sections 2–3), why every problem is reported at once except
one (sections 4–5), why the server validates even though the frontend
will (section 6), how the rules are laid out so they can be tested without
Gin (section 7), the error shape (section 8), and the Go idioms that are
new here (section 9).

The database write itself is [part 2](05-2-submit-transaction.md).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-the-endpoint) | The endpoint | [`router.go:110`](../../internal/api/router.go#L110) |
| [2](#2-the-rules) | The rules | [`submission.go:184`](../../internal/domain/submission.go#L184) |
| [3](#3-rule-1-and-why-a-draft-is-a-404) | Rule 1, and why a draft is a 404 | [`submission.go:41`](../../internal/domain/submission.go#L41) |
| [4](#4-why-collect-every-error) | Why collect every error | [`submission.go:192`](../../internal/domain/submission.go#L192) |
| [5](#5-the-one-place-failing-fast-is-right) | The one place failing fast is right | [`applications.go:447`](../../internal/store/applications.go#L447) |
| [6](#6-why-the-server-validates-when-the-frontend-already-does) | Why the server validates when the frontend already does | [`application_handler.go:354`](../../internal/api/application_handler.go#L354) |
| [7](#7-testing-the-rules-without-gin) | Testing the rules without Gin | [`submission_test.go:96`](../../internal/domain/submission_test.go#L96) |
| [8](#8-the-error-shape) | The error shape | [`submission.go:132`](../../internal/domain/submission.go#L132) |
| [9](#9-go-idioms-appearing-for-the-first-time) | Go idioms appearing for the first time | [`application_handler.go:83`](../../internal/api/application_handler.go#L83) |

---

## 1. The endpoint

**📍 Open:** [`internal/api/router.go:110`](../../internal/api/router.go#L110)

`POST /v1/job-posts/:id/applications`, body:

```json
{
  "cv_url": "https://files.example/cv.pdf",
  "answers": [
    {"question_id": "…", "answer": "I have built three hiring tools in Go."},
    {"question_id": "…", "answer": 4},
    {"question_id": "…", "answer": true}
  ]
}
```

The chain is `RequireAuth → RequireRole(candidate) → Submit`. `RequireRole`
is attached to this one route rather than to a group, because no other
candidate-only route exists yet; a group with one member would be ceremony.
An admin or interviewer is refused with 403 before the body is read.

| Outcome | Status | `code` |
|---|---|---|
| Stored | 201, `Location: /v1/applications/<id>` | — |
| Post missing, or a draft | 404 | `not_found` |
| Post closed | 409 | `not_accepting_applications` |
| Any input rule broken | 422 | `validation_failed` |
| Already applied to this post | 409 | `already_applied` (since [part 3](05-3-duplicate-applications.md)) |
| Body is not JSON, or an `answer` is an object, list or `null` | 400 | `malformed_request` |

## 2. The rules

**📍 Open:** [`internal/domain/submission.go:184`](../../internal/domain/submission.go#L184)

| # | Rule | Failure | Where |
|---|---|---|---|
| 1 | The post exists and is open | 404 / 409, **stops here** | [`submission.go:41`](../../internal/domain/submission.go#L41) |
| 2 | Every required question has a non-blank answer | 422 `required` | [`submission.go:237`](../../internal/domain/submission.go#L237) |
| 3 | No answer names a question that is not on this form, and none answers a question twice | 422 `unknown_question`, `duplicate_answer` | [`submission.go:192`](../../internal/domain/submission.go#L192) |
| 4 | Each answer suits its question's kind | 422 `invalid_number`, `invalid_boolean`, `invalid_choice` | [`submission.go:264`](../../internal/domain/submission.go#L264) |
| 5 | A text answer has at least `min_length` characters | 422 `too_short` with `min_length` and `got` | [`submission.go:264`](../../internal/domain/submission.go#L264) |
| 6 | `cv_url` is an absolute `http` or `https` URL | 422 under `cv_url` | [`submission.go:316`](../../internal/domain/submission.go#L316) |

Decisions inside the rules, each one a place where the brief left room:

- **Blank is absent.** An answer of `""` or only whitespace fails a required
  question (rule 2) and is dropped for an optional one, so the database never
  holds an empty answer that a scorer would have to special-case.
- **Rule 3 covers duplicates too.** The schema has
  `UNIQUE (application_id, question_id)`. Without a check here, a repeated
  answer would surface as a unique violation at insert time, which
  `translate` maps to a 409 `duplicate` that names nothing. Catching it here
  gives a 422 naming the exact answer.
- **Number** uses `strconv.ParseFloat` and then refuses `NaN` and `±Inf`,
  which `ParseFloat` happily accepts and no scorer can compare with a
  threshold. `1e400` overflows and is refused too.
- **Boolean** is exactly `true` or `false`, in any case. `strconv.ParseBool`
  was rejected: it also accepts `1`, `t`, `F`, which would store three
  spellings of one fact.
- **Choice** matches case-insensitively and stores the form's own spelling.
  That is safe because module 4.3 already refuses a form with two options
  differing only in case.
- **min_length** counts runes, not bytes, after trimming. Twenty `é` pass a
  minimum of 20; `"   short   "` does not pass by padding.
- **cv_url** — see section 9 for why `url.Parse` alone is not enough. A
  2,048-byte cap was added, since links much longer than that stop working in
  browsers and CDNs.

Each answer is also **returned in canonical form** — trimmed, `TRUE` → `true`,
`remote` → `Remote` — so the stored text is predictable for the scorer in a
later module.

## 3. Rule 1, and why a draft is a 404

**📍 Open:** [`internal/domain/submission.go:41`](../../internal/domain/submission.go#L41)

The brief says "exists and is open, else 409". Two of the three ways to fail
it do not give 409:

- **A post that does not exist** is a 404, as it is on every other route.
- **A draft** is also a 404. [`docs/decisions.md`](../decisions.md) D1, and
  `GET /v1/job-posts/:id`, already hide drafts from everyone but admins.
  Answering 409 "this post is a draft" would tell a candidate the ID is real
  and unpublished: the same leak D1 exists to prevent, through a different
  route.
- **A closed post** is the 409. The candidate can see a closed post, so
  saying it is closed reveals nothing, and 409 is right: the request is fine,
  the resource's *state* refuses it.

`CheckAcceptingApplications` is where this lives, not the handler, because
"a draft is invisible to candidates" is a business rule. Any other caller —
a bulk import, a future GraphQL layer — gets the same answer.

## 4. Why collect every error

**📍 Open:** [`internal/domain/submission.go:192`](../../internal/domain/submission.go#L192)

A form with eight questions, three of them wrong. Failing fast:

```text
submit → 422 "q2 is required"          fix q2
submit → 422 "q5 must be a number"     fix q5
submit → 422 "q7 too short"            fix q7
submit → 201
```

Four round trips, and the candidate learns about the problems one at a time,
with the form apparently moving the goalposts on each submit. Collecting:

```text
submit → 422 [q2 required, q5 invalid_number, q7 too_short]   fix all three
submit → 201
```

The reasons this is the better API, beyond the round trips:

- **A frontend can mark every bad field at once.** The response is a list
  keyed by `question_id`, which maps straight onto the form's inputs.
- **The order is stable.** Problems come out in rule order (2, 3, 4, 5), and
  within a rule in form or request order, because the loop appends to one
  slice per rule and joins them at the end. Two identical requests get
  byte-identical responses, which makes them testable and cacheable.
- **It is no more work for the server.** Every rule from 2 to 6 needs only
  the form and the request, both already in memory. Checking all of them
  costs a few microseconds more than stopping at the first.

The existing `ValidationError` from module 3 already collects; this endpoint
needs a richer shape (section 8) but the same principle.

## 5. The one place failing fast is right

**📍 Open:** [`internal/store/applications.go:447`](../../internal/store/applications.go#L447)

Rule 1 stops everything. If the post is closed, a draft or missing, nothing
else is checked:

1. **The other rules have nothing to check against.** Rules 2–5 compare
   answers with the post's form. For a missing post there is no form. For a
   draft, loading and comparing against the form would itself reveal the
   form of an unpublished post: "q-abc123 is required" tells the candidate a
   question ID they should never have seen.
2. **No answer to the other problems would help.** Telling a candidate that
   their CV link is malformed on a post that is closed invites them to fix
   it and resubmit, only to be refused again for a reason they were not told
   the first time. The closed status is the only thing they need to know.
3. **It is a different kind of failure.** Rules 2–6 are about *the request*
   (422: fix your input). Rule 1 is about *the resource* (409/404: nothing
   you send will work). Mixing the two in one response would mean one status
   code describing two different situations.

The general principle: collect errors that are independent of each other and
that the caller can fix; stop at an error that makes the rest meaningless.

## 6. Why the server validates when the frontend already does

**📍 Open:** [`internal/api/application_handler.go:354`](../../internal/api/application_handler.go#L354)

Frontend validation is a convenience for the candidate. Server validation is
the guarantee. They are not the same check done twice:

- **The frontend is not the only client.** `curl`, a script, a mobile app
  built next year, a partner integration: anything that speaks HTTP reaches
  this endpoint without running the frontend's JavaScript. The server is the
  one place every request passes.
- **The frontend is under the attacker's control.** Validation in the browser
  can be switched off in developer tools in seconds. Anything that matters —
  the post being open, the answer being one of the allowed choices — cannot
  be enforced by code the user can edit.
- **The frontend's copy of the rules goes stale.** It fetched the form when
  the page loaded. If the post closes while the candidate is typing, only the
  server knows. Rule 1 is checked under a row lock (part 2) for this reason.
- **Bad data outlives the bug that let it in.** A boolean stored as `"yes"`
  breaks the scorer in a later module, on every rescore, forever. The
  database cannot express "this text must parse as the kind of its
  question", so the server is the last place the rule can be enforced.

The frontend should still validate: it is faster for the candidate. But it
mirrors the server's rules; it does not replace them.

## 7. Testing the rules without Gin

**📍 Open:** [`internal/domain/submission_test.go:96`](../../internal/domain/submission_test.go#L96)

The work is split in three layers, and only one of them knows HTTP:

| Layer | Knows | Does | Tested by |
|---|---|---|---|
| `domain` | Questions, answers, statuses | Every rule: [`CheckAcceptingApplications`](../../internal/domain/submission.go#L41), [`ValidateApplication`](../../internal/domain/submission.go#L184) | Plain table tests, no I/O |
| `store` | SQL, transactions | Loads status and form under a lock, calls the domain functions, writes | Integration tests on real PostgreSQL |
| `api` | JSON, HTTP, roles | Decodes, maps to `domain.NewApplication`, maps errors to statuses | Handler tests with a fake store |

`ValidateApplication(form []Question, in NewApplication)` is a **pure
function**: its result depends only on its arguments, and it reads no clock,
no database and no global. That is what makes it testable with nothing but a
slice literal. The test builds one valid submission, and each table row
changes one thing about it:

```go
{"a required answer that is only spaces", func(in *NewApplication) { answer(in, qWhy, " \t\n ") },
    []string{AnswerRequired}, false},
```

**📍 Open:** [`submission_test.go:165`](../../internal/domain/submission_test.go#L165)
through [`submission_test.go:306`](../../internal/domain/submission_test.go#L306) — one
table per rule, then [`TestValidateApplicationCollectsEveryProblem`](../../internal/domain/submission_test.go#L306),
which breaks every rule at once and checks the exact list and order.

The rejected alternative was validating in the handler with Gin's binding
tags (`binding:"required,url"`). Those cannot express "required depends on
the question", "the answer's type depends on another row", or "collect
across the whole answers array", and every test would need an
`httptest.Recorder` and a router to reach them.

## 8. The error shape

**📍 Open:** [`internal/domain/submission.go:132`](../../internal/domain/submission.go#L132)

```json
{
  "error": {
    "code": "validation_failed",
    "message": "One or more fields are invalid.",
    "details": {
      "cv_url": "must be an absolute http or https URL",
      "answers": [
        {"question_id": "…", "code": "required", "message": "is required"},
        {"question_id": "…", "index": 2, "code": "too_short",
         "message": "must be at least 50 characters", "min_length": 50, "got": 12}
      ]
    }
  }
}
```

- **A new type, `SubmissionError`, not `ValidationError`.** `ValidationError`
  is `map[string]string`: one message per field. `too_short` needs numbers
  beside the message, and several answers can be wrong at once, so a flat map
  cannot hold it. `SubmissionError` still `Unwrap`s to `ErrValidation`, so
  the api package's mapping table gives it 422 with no new row.
- **`cv_url` stays a string** under its own key, as fields do on every other
  endpoint, so a client's existing "show the message next to the field" code
  works for it unchanged.
- **`index`** is the answer's position in the request, so a client with two
  inputs bound to one question ID can still tell which is meant. It is left
  out for a missing answer, which has no position — hence the `NoIndex`
  constant (−1) rather than `0`, which is a real position.
- **Maps built key by key**, not a struct with `json:",omitempty"`:
  `omitempty` drops every zero value, so `"index": 0` would vanish for the
  first answer.
- **`Error()` holds no input.** It is the log line, and logs must not carry a
  candidate's answers or links. The test checks that it does not.

## 9. Go idioms appearing for the first time

### Custom `UnmarshalJSON`

**📍 Open:** [`internal/api/application_handler.go:83`](../../internal/api/application_handler.go#L83)

`answerValue` is a named type over `string` with an `UnmarshalJSON` method.
`encoding/json` checks every destination for that method (the
`json.Unmarshaler` interface) and, if present, hands it the raw bytes instead
of decoding them itself. That is how `"answer": 4`, `"answer": "4"` and
`"answer": true` all arrive as text, while `{}`, `[]` and `null` are refused.
The number keeps its exact spelling — `4.50` stays `"4.50"` — because the raw
bytes are stored, not a `float64` round trip.

### Type switch

```go
switch s := decoded.(type) {
case string:
    *v = answerValue(s)
```

`x.(type)` is only valid in a `switch`. In each `case`, `s` has the type that
case names, so no second type assertion is needed. A case listing several
types (`case float64, bool:`) gives `s` the original interface type.

### Struct embedding in a response

**📍 Open:** [`internal/api/application_handler.go:116`](../../internal/api/application_handler.go#L116)

`submittedApplicationResponse` embeds `applicationResponse` (a field with a
type but no name). `encoding/json` treats an embedded struct's fields as if
they were declared on the outer struct, so the response is one flat object
with `answers` added, rather than `{"application": {…}, "answers": […]}`.
The list endpoints keep returning `applicationResponse` alone.

### `url.Parse` is lenient

**📍 Open:** [`internal/domain/submission.go:316`](../../internal/domain/submission.go#L316)

`url.Parse("cv.pdf")` succeeds: it is a valid *relative* reference. So is
`/files/cv.pdf` and `//host/path`. "Absolute" means a scheme **and** a host,
so both are checked explicitly after parsing. `Parse` lowercases the scheme,
which is why `HTTPS://…` passes.

---

## Verified

| Command | Result |
|---|---|
| `go test -race ./internal/domain/` | ok — 8 test functions, 60 subtests over the six rules |
| `go test -race -cover ./...` | all ok; `domain` 96.0 %, `api` 92.7 % |
| `go test -race -run TestSubmitApplication ./internal/api/` | ok — 15 subtests: the 201 path, 8 refusals before the store is called, 4 store errors mapped to statuses |
| `go test -race -count=1 -tags integration ./test/...` | ok — matrix now has a column for this route: candidates reach rule 1 and get 409, every other role 403 or 401 |
| `golangci-lint run ./...` | 0 issues |

## Still open

- **Upper bound on a text answer.** Only the 1 MiB body cap limits one. A
  per-answer maximum belongs with `min_length` on the question.
- **The CV is a link, not an upload.** Nothing checks that it resolves or
  points at a document. Fetching it would make this request depend on a
  third-party server's speed; that belongs in a background job.

## Next

[Part 2](05-2-submit-transaction.md): writing the application, its answers
and its event in one transaction.
