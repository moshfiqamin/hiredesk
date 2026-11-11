# Module 9, part 2 — Reviews, and keeping other interviewers' marks out of the response

`POST /v1/applications/:id/reviews` and `PATCH /v1/applications/:id/reviews/mine`
(assigned interviewers only), plus reviews in `GET /v1/applications/:id`:
every review for an admin, only your own for an interviewer, none for a
candidate. The interesting part is the last rule, and the place it actually
has to be enforced.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-anchoring-bias-and-why-the-rule-exists) | **Anchoring bias, and why the rule exists** | — |
| [2](#2-why-hiding-it-in-the-frontend-is-a-security-bug) | **Why hiding it in the frontend is a security bug** | — |
| [3](#3-it-was-already-leaking-through-the-events) | It was already leaking, through the events | [`policy.go:109`](../../internal/auth/policy.go#L109) |
| [4](#4-one-decision-two-enforcement-points) | **One decision, two enforcement points: avoiding the tangle** | [`applications.go:497`](../../internal/store/applications.go#L497) |
| [5](#5-the-mark-0-11-and-eight) | The mark: `0`, `11` and `"eight"` | [`review_handler.go:214`](../../internal/api/review_handler.go#L214) |
| [6](#6-notes) | Notes | [`review.go:102`](../../internal/domain/review.go#L102) |
| [7](#7-one-review-each-and-the-409) | One review each, and the 409 | [`reviews.go:326`](../../internal/store/reviews.go#L326) |
| [8](#8-correcting-a-review) | Correcting a review | [`reviews.go:353`](../../internal/store/reviews.go#L353) |
| [9](#9-the-tests) | The tests | [`review_test.go:37`](../../test/integration/review_test.go#L37) |
| [10](#10-verification) | Verification | — |

---

## 1. Anchoring bias, and why the rule exists

**Anchoring** is the tendency to lean too heavily on the first number you
see when estimating something, even when you know that number is
irrelevant. In the classic demonstration, people spun a rigged wheel that
landed on 10 or 65, then estimated the percentage of African countries in
the UN. The group that saw 65 guessed far higher than the group that saw 10.
A random number moved a factual estimate.

In hiring, the anchor is a colleague's mark. An interviewer who reads
"Omar gave 9/10" before forming their own view does not start from the
candidate's answers; they start from 9 and adjust, usually not far enough.
Their mark drifts toward Omar's. The team then sees two 9s and concludes
there is strong agreement, when what it has is **one opinion counted
twice**.

That defeats the reason for having several interviewers at all:
independent judgments whose errors partly cancel out. It also amplifies
whoever reviews first, which is often the most senior or the fastest, not
the most accurate, and it makes one interviewer's bias (towards a school,
an accent, a CV format) everyone's.

So the product rule: **an interviewer sees only their own review.** Admins,
who decide, see all of them, *after* the interviewers have formed their
views independently. This is standard practice in structured interviewing,
where interviewers submit scorecards before any debrief.

## 2. Why hiding it in the frontend is a security bug

"Hidden in the frontend" means the server sends every review, and
JavaScript decides not to render the others. The marks are then still
**in the response**:

- **The browser's developer tools** (Network tab) show the raw JSON of
  every request. Pressing F12 is all it takes.
- **The API is the product, not the UI.** Anyone with a valid token can
  call `GET /v1/applications/:id` with `curl`, a script, or Postman, and
  never run the frontend's filtering code at all.
- **Other clients will be written.** A mobile app, an integration or a
  second frontend would each have to re-implement the hiding correctly,
  forever. One forgets, and the data was always there to show.
- **Caches, logs and proxies** store full responses. A mark in a response
  is a mark in every HAR file someone attaches to a bug report.

So it is not cosmetic. It is a real **authorization failure**: data the
user is not entitled to is *disclosed to them*, and "we did not draw it"
is not a control. OWASP lists exactly this as *Excessive Data Exposure*
(API3 in the API Security Top 10): the API returns more than the client
needs and trusts the client to filter.

The principle that follows: **the server decides what each caller may
receive, and sends only that.** The frontend may decide what to *display*,
but only among things the user is already allowed to know. The brief's
wording, "absent from the API response", is the right test, and the
integration test checks it literally: it searches the **raw response
bytes** for Omar's review ID and notes text, not just for a decoded field.

## 3. It was already leaking, through the events

**📍 Open:** [`internal/auth/policy.go:109`](../../internal/auth/policy.go#L109)

Planning this, I found the rule would have been broken through a door that
already existed. `GET /v1/applications/:id` returns the application's
**events**, all of them, to anyone who may view the application. The new
`application.reviewed` event carries the mark (part 3 explains why it
should). So without more work:

- **Ivy** would read Omar's mark from the `events` list, even with the
  `reviews` list perfectly filtered;
- the **candidate** would read every interviewer's mark on their own
  application.

And it was **already leaking something today**: `application.rescored`
events carry `old_total_earned` / `new_total_earned`, and the candidate
could see them, although `CanViewScore` deliberately refuses candidates
their score (07-3). I checked the live database: the payload holds the
totals.

This is in scope rather than a hand-me-down fix, because the requirement
is that marks be absent from the response, and the detail response is
where they would leak. Fixing it needs one coherent visibility rule for
events, and that rule covers the rescore totals too. **It does change
existing behaviour**: a candidate's application detail now lists only
`application.submitted` and `application.status_changed` events. They lose
`application.rescored`, which they should never have had.

## 4. One decision, two enforcement points: avoiding the tangle

**📍 Open:** [`internal/store/applications.go:497`](../../internal/store/applications.go#L497)

"Admins see everything, interviewers see their own, candidates see none"
now applies to two lists (reviews and events), in two endpoints (detail and
timeline), each with pagination or ordering. The tangle to avoid is an
`if user.Role == …` in every handler and every query, each subtly
different, and each a place for the next change to be missed.

The design has three parts.

**1. One pure function decides, and returns a value, not a boolean.**

```go
func ApplicationViewFor(u domain.User) domain.ApplicationView
```

returns `{ViewerID, Reviews: ReviewsAll | ReviewsOwn | ReviewsNone,
EventTypes: nil | [...]}`. It sits in `internal/auth` next to the other
policy functions, reads nothing but its argument, and denies by default
(an unknown role gets no reviews and an empty, non-nil, event list). A
boolean such as `canSeeReviews` cannot express "only *these* reviews", so
every caller would grow its own `if`. A **scope value** carries the whole
answer to the code that applies it. `TestApplicationViewFor` pins the
result for every role, including `""`, `"Admin"` and `"superuser"`.

**2. The store applies the view in SQL, so the rows never leave the
database.** Two small helpers do it:

- `listReviews` adds `WHERE r.interviewer_id = $viewer` for `ReviewsOwn`,
  and **does not run the query at all** for `ReviewsNone`.
- `addEventVisibility` adds `e.type = ANY($types)` when there is a type
  list, and for review events `(type is not a review event OR
  payload->>'interviewer_id' = $viewer)`.

Filtering in Go after the query would work too, but it reads data the
caller may not see into memory, where the next change can serialise it by
accident. It also breaks pagination: a page of 20 events filtered down to
13 in Go is a short page with a cursor in the wrong place.

**3. Each rule is written once, and the two lists share it.** "A review
event is visible exactly when its review is" means one `ReviewScope` drives
both. Both event queries (detail, oldest first; timeline, newest first and
paged) call the same `addEventVisibility`. The handlers contain no role
checks for this at all; they pass `auth.ApplicationViewFor(user)` straight
through.

The result reads as a table rather than a tangle:

| Role | Reviews | Review events | Other events |
|---|---|---|---|
| admin | all | all | all |
| interviewer | own | own | all (they may see the score anyway) |
| candidate | none, and no `reviews` key | none | `submitted`, `status_changed` only |
| anything else | none | none | none |

`reviews` is a `*[]reviewResponse` with `omitempty`: **absent** for a
candidate, `[]` for an interviewer who has not reviewed yet. That keeps
"you may not see reviews" and "there are none for you yet" different on the
wire.

## 5. The mark: `0`, `11` and `"eight"`

**📍 Open:** [`internal/api/review_handler.go:214`](../../internal/api/review_handler.go#L214)

`0` and `11` are easy: a whole number out of range, `domain.CheckMark`. The
database's `reviews_mark_check` is the second line.

`"eight"` is the interesting one. Declared as `Mark int`, the JSON decoder
would fail on a string, and `decodeJSON` would answer **400
malformed_request** for the whole body. But the body *is* well-formed JSON
with one invalid field, and the brief rightly wants **422**, naming `mark`.
So `mark` decodes into a `markValue` whose `UnmarshalJSON` never fails. It
records what it found:

- `null` or absent → not present (in a PATCH, "leave it alone");
- decoded with `UseNumber()`, so a number stays as its **literal text**
  (`json.Number`) instead of becoming a `float64`;
- `strconv.Atoi` on that text accepts only optional sign plus digits, so
  `8` passes while `8.0`, `8.5`, `8e0` and `99999999999999999999999` fail;
- anything that is not a number (`"eight"`, `"8"`, `true`, `{}`) is "not a
  whole number".

The handler then reports `mark: must be a whole number from 1 to 10` for
every failure. That one message tells the client what *is* accepted, which
is more useful than a different message per way of being wrong.

**Why refuse `"8"` and `8.0`, which are obviously eight?** A mark is an
integer by contract. Accepting strings invites clients to send them, and
accepting `8.0` invites `7.5`. A strict contract is the one that never
needs a migration.

`UnmarshalJSON` is a **method on `*markValue`** (pointer receiver) because
it must modify the value it is called on. `encoding/json` finds it through
the `json.Unmarshaler` interface. Go interfaces are satisfied implicitly:
having a method with the right signature is enough, with no `implements`
keyword.

## 6. Notes

**📍 Open:** [`internal/domain/review.go:102`](../../internal/domain/review.go#L102)

- **Optional**, trimmed, and blank becomes `NULL`: a review with whitespace
  for notes has no notes.
- **At most 5,000 characters** (runes, not bytes; see 06-2): several
  paragraphs, a review rather than an essay, and an upper bound on what an
  admin reads per candidate.
- **No NUL character.** JSON can carry `"\u0000"`, and PostgreSQL `text`
  cannot store it: the insert would fail and the client would get a 500.
  Found by the same question as in 08-3: what can the transport carry that
  the database cannot?

## 7. One review each, and the 409

**📍 Open:** [`internal/store/reviews.go:326`](../../internal/store/reviews.go#L326)

The `reviews_application_id_interviewer_id_key` unique constraint (from
000006) is the rule; the store maps a violation *of that constraint by
name* to `domain.ErrAlreadyReviewed`, which the error table turns into
**409 `already_reviewed`** with a message pointing at `PATCH
…/reviews/mine`. No `SELECT` first: as with duplicate applications, only
the constraint survives two requests at once, and the test fires five.

This is the deliberate opposite of part 1. A second *assignment* is a
retry of the same intent, so idempotent 200. A second *review* is either a
client bug or an attempt to replace a mark without leaving an edit trail,
so it is refused and the client is sent to the correction route. A mark
changing is an event worth recording; a POST that silently overwrote it
would hide that.

Inside the transaction, the store first locks the assignment `FOR SHARE`.
The handler checked the assignment a few milliseconds earlier, but without
the lock an admin could unassign Ivy in between, and her review would be
filed by someone no longer allowed to see the application. `FOR SHARE`
makes the admin's `DELETE` wait. Two interviewers reviewing at once both
take `FOR SHARE` on different rows, so they never block each other.

The insert and the interviewer's name come back from one statement, a
data-modifying CTE: `WITH r AS (INSERT … RETURNING *) SELECT …, u.full_name
FROM r JOIN users u …`.

## 8. Correcting a review

**📍 Open:** [`internal/store/reviews.go:353`](../../internal/store/reviews.go#L353)

`PATCH /v1/applications/:id/reviews/mine`:

- **`mine` instead of a review ID.** With `/reviews/:reviewID`, the handler
  would need to check that the review belongs to the caller, and forgetting
  that check is the classic *insecure direct object reference* (IDOR):
  change the ID in the URL and edit someone else's review. With `mine`
  there is no ID to change; the store looks the review up by `(application,
  caller)`.
- **Partial**: send `mark`, `notes` or both; at least one, or 422.
  `"notes": ""` removes the notes, and `null` means "not sent", the
  convention every PATCH here follows.
- **`FOR UPDATE OF r`** locks the review row (not the joined `users` row),
  so two corrections run one after the other and the event's `from_mark`
  is always the mark that correction actually replaced.
- **A correction that changes nothing records nothing.** Same mark, same
  notes: 200 with the review, no `updated_at` bump, no event. The history
  records changes, and a double-clicked "Save" is not two corrections.
- **The event keeps `from_mark` and `to_mark`, and `notes_changed`, but not
  the notes text.** The marks are what a dispute turns on (part 3); the
  notes can be long, and the current text is in the review. If notes
  history mattered (for example in a regulated hiring process), the
  alternative is a `review_revisions` table holding every version.
- **No review yet** is 404 `no_review` (a sub-case of not found, mapped
  before it), saying to POST first.

## 9. The tests

**📍 Open:** [`test/integration/review_test.go:37`](../../test/integration/review_test.go#L37)

| Test | Proves |
|---|---|
| `TestReviews_InterviewerSeesOnlyTheirOwn` | Omar reviews first with a distinctive mark and notes. **Before** Ivy reviews, her detail shows `"reviews":[]`, and neither her detail nor her timeline contains Omar's review ID, his notes text, or even the string `application.reviewed`. **After** she reviews, she sees exactly her own. The admin sees both reviews and both events. The candidate's response has no `reviews` key and contains neither review, no interviewer's ID, and no review or assignment event |
| `TestReviews_ValidationAndOnePerInterviewer` | `0`, `11`, `"eight"`, `7.5` and a missing mark are each 422 with nothing stored; `7` is 201; a second review is 409 `already_reviewed`; five concurrent second attempts are all 409; the event payload holds `review_id`, `interviewer_id` and `mark`; an unassigned interviewer gets 404 |
| `TestReviews_Correction` | correcting a review never filed is 404 `no_review`; mark 7 to 8 keeps the notes; repeating it adds no event; clearing the notes gives `"notes":null`; the two events carry `7→8, notes_changed false` and `8→8, notes_changed true` |
| `internal/api` `TestCreateReview` | thirteen bad marks, including `"8"`, `8.0`, `8e0`, `true`, an object and a 23-digit number, are each 422 naming `mark` and never reach the store; 1 and 10 pass; long and NUL notes are 422 |
| `internal/api` `TestReviewsInApplicationDetail` | the view each role is given, and the resulting `reviews` key: two, one, absent |
| `internal/auth` `TestApplicationViewFor` | the view for every role, including the three invalid ones |

## 10. Verification

| Command | Result |
|---|---|
| `make test`, `make test-integration`, `make lint` | all pass; `0 issues.`; `internal/auth` 96.0 %, `internal/domain` 96.0 % coverage |
| change `ApplicationViewFor` to give interviewers `ReviewsAll` | `TestReviews_InterviewerSeesOnlyTheirOwn` fails at `Ivy's detail before reviewing`; reverted |
| `psql` on the live database: an `application.rescored` payload | `{"new_total_earned": 100, "old_total_earned": 66.8, …}`, the totals a candidate could read before this change |
| `curl` on the live stack as the seeded interviewer | `"mark":"eight"`: 422 `must be a whole number from 1 to 10`; `8`: 201; again: 409 `already_reviewed`; PATCH to 9: 200 |
