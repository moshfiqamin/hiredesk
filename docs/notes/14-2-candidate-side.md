# Module 14, part 2 — The candidate's half

The only pages a stranger sees: the public **role board** and **role page**,
**registration**, the **application form**, rendered from the role's
questions and saved as a draft while the candidate types, and
**My applications** with a pipeline tracker.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-a-public-page-for-one-role) | A public page for one role | [`job_listing.go:139`](../../internal/store/job_listing.go#L139) |
| [2](#2-the-board-and-the-role-page) | The board and the role page | [`roles-board.tsx:78`](../../web/src/components/listings/roles-board.tsx#L78) |
| [3](#3-a-form-rendered-from-data-and-still-type-safe) | **A form rendered from data, and still type-safe** | [`question-fields.tsx:180`](../../web/src/components/candidate/question-fields.tsx#L180) |
| [4](#4-client-rules-that-mirror-the-servers) | Client rules that mirror the server's | [`application-form.ts:45`](../../web/src/lib/applications/application-form.ts#L45) |
| [5](#5-422s-mapped-onto-fields) | 422s mapped onto fields | [`application-form.ts:106`](../../web/src/lib/applications/application-form.ts#L106) |
| [6](#6-the-draft-and-a-question-that-changed) | **The draft, and a question that changed** | [`draft.ts:46`](../../web/src/lib/applications/draft.ts#L46) |
| [7](#7-my-applications-and-the-tracker) | My applications, and the tracker | [`my-applications.tsx:128`](../../web/src/components/candidate/my-applications.tsx#L128) |
| [8](#8-should-a-candidate-see-their-own-score) | **Should a candidate see their own score?** | [`my-applications.tsx:33`](../../web/src/components/candidate/my-applications.tsx#L33) |
| [9](#9-verification) | Verification | — |

---

## 1. A public page for one role

**📍 Open:** [`internal/store/job_listing.go:139`](../../internal/store/job_listing.go#L139)

`GET /v1/job-posts/:id` needs a session, and a stranger should see the
questions *before* signing up. So `GET /v1/job-posts/:id/listing` is public,
like the listing, registered on `v1` itself, so no token is read
([`router.go:131`](../../internal/api/router.go#L131)). `p.status = 'open'`
is in the SQL: a draft, a closed post and a missing ID are all the same 404,
which reveals nothing about posts that are not open. The response has its
own type, so an admin field can never leak into it by being added elsewhere,
and it never includes the rubric.

Vacancies and skill tags are computed by the same SQL fragment the listing
uses, `listingLaterals` ([line 16](../../internal/store/job_listing.go#L16)),
so the card and the page cannot disagree about either.

## 2. The board and the role page

**📍 Open:** [`web/src/components/listings/roles-board.tsx:78`](../../web/src/components/listings/roles-board.tsx#L78)

Each card leads with what a job seeker scans for (title, place, contract,
skills) and states places left in words. The whole card is one link,
stretched over it with `::after`: one tab stop and one announcement, not a
cluster of links to the same page. Its focus ring is drawn on the card with
`has-[a:focus-visible]`. "Show more roles" pages by keyset cursor
(`useInfiniteQuery`), and the count sits in a `role="status"` region, so
added cards are announced.

The role page lists every question with what kind of answer it takes and
whether it is required, and offers the next step for whoever is looking
([`role-detail.tsx:137`](../../web/src/components/listings/role-detail.tsx#L137)):
apply (candidate), sign up to apply (signed out, returning to the form
after), or a note for staff accounts. React 19 hoists the `<title>` it
renders into `<head>`, so the tab names the role.

## 3. A form rendered from data, and still type-safe

**📍 Open:** [`web/src/components/candidate/question-fields.tsx:180`](../../web/src/components/candidate/question-fields.tsx#L180)

The questions arrive at runtime, so the form's *shape* cannot be a
TypeScript type. Type safety is kept in three places instead:

1. **One value shape.** Every answer is a `string` keyed by question ID
   ([`application-form.ts:13`](../../web/src/lib/applications/application-form.ts#L13)),
   which is exactly what the API takes: `"4"`, `"true"`, `""` for
   unanswered. The form state is a plain record; the *kind* decides how a
   string is edited and checked, not how it is stored.
2. **A mapped type over the kinds.**
   `{ [K in QuestionKind]: ComponentType<QuestionFieldProps> }`: if
   `QuestionKind` gains a fifth member, this object stops compiling until
   the new kind has an input. The form never switches on kind itself; it
   looks the component up. This is the registry pattern from the rubric
   builder (11.6).
3. **Exhaustive switches.** `answerProblem` and `describeQuestion` switch
   on `q.kind` with no `default`. TypeScript knows the switch covers the
   union, and a new kind turns the missing return into a compile error.

The schema is built per form, from the questions, with Zod's `superRefine`
([`application-form.ts:72`](../../web/src/lib/applications/application-form.ts#L72)),
calling the same functions the server-mirroring rules use. There is one
copy of each rule.

Inputs per kind:
- **text:** a textarea with a live minimum counter. It is not a live region
  (a count per keystroke would drown a screen reader), but it is tied to the
  field and says "minimum reached" in screen-reader text.
- **number:** `type="text" inputmode="decimal"`
  ([line 52](../../web/src/components/candidate/question-fields.tsx#L52)),
  because `type="number"` changes value on the scroll wheel, accepts `e`,
  and reports `""` for a half-typed `1.`.
- **yes/no, and choice up to five options:** native radios in a
  `<fieldset>` named by its `<legend>`.
- **More than five options:** a native `<select>`.

Required questions carry a visible `*`, hidden from screen readers, plus
`required` on the input, and the intro explains the mark.

## 4. Client rules that mirror the server's

**📍 Open:** [`web/src/lib/applications/application-form.ts:45`](../../web/src/lib/applications/application-form.ts#L45)

Each rule is `domain.ValidateApplication`, written again for the browser.
The details are where the two could quietly disagree:

| Server (Go) | Client |
|---|---|
| A blank answer counts as no answer | `.trim() === ""` → required error, or fine if optional |
| Length in runes (`utf8.RuneCountInString`) | `[...s.trim()].length` (code points; `.length` counts UTF-16 units, 2 for an emoji) ([line 21](../../web/src/lib/applications/application-form.ts#L21)) |
| `strconv.ParseFloat`, not NaN or Inf | `Number.isFinite(Number(text))` |
| Choice matched case-insensitively | `toLowerCase()` comparison |
| CV: absolute `http`/`https` URL with a host, at most 2,048 **bytes** | `new URL`, protocol and host check, `TextEncoder` length ([line 26](../../web/src/lib/applications/application-form.ts#L26)) |

Client validation is for the candidate's convenience. The server
re-validates every submission (05-1, section 6).

## 5. 422s mapped onto fields

**📍 Open:** [`web/src/lib/applications/application-form.ts:106`](../../web/src/lib/applications/application-form.ts#L106)

The submission 422 has its own shape: `details.answers` is a list of
`{question_id, code, min_length, got, ...}`. Each entry becomes
`answers.<id>` with the same words the client check would have used, and
`setError(..., { shouldFocus })` moves focus to the first
([`application-form.tsx:85`](../../web/src/components/candidate/application-form.tsx#L85)).
`already_applied` gets its own message with a link to My applications.

## 6. The draft, and a question that changed

**📍 Open:** [`web/src/lib/applications/draft.ts:46`](../../web/src/lib/applications/draft.ts#L46)

The form saves to `localStorage` half a second after the last change:
one write per pause, not per keystroke. The visible line says "Draft saved
at 14:02"; the screen-reader announcement happens once, on the first save,
and again on a failure
([`application-form.tsx:269`](../../web/src/components/candidate/application-form.tsx#L269)).
The key includes the user and the role, plus a version
([`draft.ts:21`](../../web/src/lib/applications/draft.ts#L21)), so two
people sharing a browser never see each other's drafts. The draft is
cleared after a successful submit.

**A question that changed between sessions.** In practice the server locks
a form once its role opens (04-3), so an open role's questions do not
change. The draft handles it anyway, because a draft outlives the code that
wrote it. Each saved answer is stored *with the kind and prompt of the
question it answered*. On load, fitted against the current questions
([line 66](../../web/src/lib/applications/draft.ts#L66)):

| Saved answer vs current question | Result |
|---|---|
| Same ID, same kind | Restored |
| Same ID and kind, reworded prompt | Restored, and listed: "please reread your answer to: …" |
| Same ID, different kind (text where a number is now wanted) | Dropped, and named |
| Choice no longer offered | Dropped, and named |
| Question gone | Dropped, and named |
| New question | Starts blank |

The rejected alternative was to restore blindly by ID, which would put a
paragraph into a number field and fail validation with no explanation.
Discarding the whole draft on any change was also rejected: it throws away
an hour of writing because one option was renamed. Storage that is blocked
(private mode, full quota) is caught everywhere, and the form says the draft
is not being saved rather than pretending.

## 7. My applications, and the tracker

**📍 Open:** [`web/src/components/candidate/my-applications.tsx:128`](../../web/src/components/candidate/my-applications.tsx#L128)

Each application is a card with the role, the submitted date and a
four-step tracker: Received, Shortlisted, Interview, Offer. Reached steps
are solid; the current one also carries a "Now" tag and
`aria-current="step"`. Each step says "done", "current step" or "to come"
in screen-reader text, so the fills are never the only signal.

**Rejection** is not a fifth step: it can happen at any stage. The track
stops at the stage it was rejected from, marked with a cross and "not
progressing". The list says only "rejected", so for those applications
alone the detail is fetched: its status events say where it stopped. A
candidate has a handful of applications, so this is a few requests, not an
N+1 worth optimising. The wording is plain and not cold, and it invites
other applications.

## 8. Should a candidate see their own score?

**📍 Open:** [`web/src/components/candidate/my-applications.tsx:33`](../../web/src/components/candidate/my-applications.tsx#L33)

**Recommendation: no, not as a number, and the API already refuses it**
(`GET .../score` is a 403 for the candidate; decision D3). Implemented as:
no score anywhere on the candidate's pages, and a line saying every
application is read by a person.

Why:

- **It is a screening aid, not a verdict.** The score orders the reading
  pile; people decide. Showing it frames it as the decision, which invites
  "I scored 82%, why was I rejected?" when the score was never the reason.
- **It teaches the rubric.** The breakdown says which keywords counted.
  A candidate who sees "matched 2 of 5 keywords" learns to keyword-stuff
  the next application, which degrades the very signal the score is for.
  The skill tags on the public page already say what a role looks for, and
  that is the honest version of this information.
- **It is relative to a rubric that can change.** A rescore against a new
  version changes the number after the fact; a candidate who saw 70 and
  later 64 would rightly ask what happened.
- **What a candidate is owed is where they are, and that is the tracker.**

The alternative, if a candidate is ever owed an explanation (for example,
after a rejection, on request), is the summary D3 sketches: plain-language
reasons without keywords or weights. That is listed under "What I would
build next" in the README.

## 9. Verification

| Check | Result |
|---|---|
| `go test ./internal/...` | `TestJobListingDetail`: public, 200 with questions; draft, closed and missing all 404; malformed 400; no admin fields, no rubric |
| `make test-integration` | `TestOpenJobListingHidesEverythingButOpenPosts`; matrix column for the public page (byte-identical for every caller) |
| Browser, signed out | Board, role page, "Create an account to apply" → registration → straight back to the form |
| Draft | Typed a short answer, saw the live "36 / 80" error, reloaded: "We restored the draft…"; storage key per user and role; cleared after submit |
| Submit | Redirected to My applications; tracker at Received ("Now") |
| `e2e/full-flow.spec.ts` | Candidate registers, applies, tracker step has `aria-current="step"` |
