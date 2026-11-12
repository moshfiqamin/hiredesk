# Module 13, part 3 — The candidate page and the ranking

The two screens that use the score components: the **candidate detail
page**, where an admin decides what happens to one application, and the
**ranking leaderboard** per role, where they see the whole pool and where
the vacancies run out. Two small API additions make both honest.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-ordering-the-page-for-a-decision) | **Ordering the page for a decision** | [`application-detail.tsx:130`](../../web/src/components/applications/application-detail.tsx#L130) |
| [2](#2-legal-transitions-without-a-second-copy-of-the-rules) | **Legal transitions without a second copy of the rules** | [`application_handler.go:233`](../../internal/api/application_handler.go#L233) |
| [3](#3-the-status-buttons) | The status buttons | [`application-detail.tsx:201`](../../web/src/components/applications/application-detail.tsx#L201) |
| [4](#4-a-timeline-that-reads-as-a-story) | **A timeline that reads as a story** | [`timeline.ts:25`](../../web/src/lib/applications/timeline.ts#L25) |
| [5](#5-reviews-and-the-visibility-rules) | Reviews and the visibility rules | [`application-detail.tsx:349`](../../web/src/components/applications/application-detail.tsx#L349) |
| [6](#6-the-leaderboard) | The leaderboard | [`leaderboard.tsx:29`](../../web/src/components/ranking/leaderboard.tsx#L29) |
| [7](#7-a-breakdown-bar-without-n1) | **A breakdown bar without N+1** | [`ranking_breakdown.go:15`](../../internal/store/ranking_breakdown.go#L15) |
| [8](#8-verification) | Verification | — |

---

## 1. Ordering the page for a decision

**📍 Open:** [`web/src/components/applications/application-detail.tsx:130`](../../web/src/components/applications/application-detail.tsx#L130)

An admin opens this page to decide something: shortlist, move on, reject.
So the order follows the questions they ask, in the order they ask them:

1. **Who, for what, and where are they now?** — name, status badge, role,
   date, and the CV link, in the header.
2. **What can I do?** — the legal next moves, top right in the header, so
   the action is visible without scrolling, next to the status it changes.
3. **Is the score right?** — the score with its evidence, the largest block,
   first in the main column
   ([line 137](../../web/src/components/applications/application-detail.tsx#L137)).
4. **What did the people who met them think?** — a review summary (mean
   mark, who has not reviewed yet) and the interviewers, in the side column
   ([line 152](../../web/src/components/applications/application-detail.tsx#L152)).
5. **Detail on demand** — answers, reviews and the timeline, in tabs
   ([line 158](../../web/src/components/applications/application-detail.tsx#L158)),
   so the page is not one long scroll.

The rejected order was the API's: application, answers, events. That is
the order the data is *stored* in, not the order a decision is *made* in.
On a phone the side column drops below the score, which keeps the score
ahead of the summaries — still the right priority.

## 2. Legal transitions without a second copy of the rules

**📍 Open:** [`internal/api/application_handler.go:233`](../../internal/api/application_handler.go#L233)

The pipeline (submitted → shortlisted → interview → offer, rejected from
anywhere, final) is written once, in Go:
[`domain/application_status.go:21`](../../internal/domain/application_status.go#L21).
The UI must offer only the moves the server will accept.

Three options:

- **Copy the table into TypeScript.** Simple, and wrong the first time
  someone edits one side. Nothing would fail; the UI would just offer a
  button that answers 409, or hide a move that is allowed.
- **Try any move and show the 409.** The server already sends
  `allowed_next_statuses` with an `invalid_transition` error, but only
  *after* a failed attempt. Buttons that fail on purpose are a bad interface.
- **Ask the server.** Chosen. `GET /v1/applications/:id` now returns
  `allowed_next_statuses`, filled from the same function the PATCH uses:
  `domain.AllowedApplicationTransitions(app.Status)`
  ([line 405](../../internal/api/application_handler.go#L405)).

One table, two readers. The handler test asserts the response equals that
function's output for every caller
([`application_handler_test.go:375`](../../internal/api/application_handler_test.go#L375)),
so the response and the PATCH cannot drift apart. The client holds no
pipeline at all — only a label per target status, with a fallback, so a
status added later still gets a button.

What the list *cannot* know in advance is the vacancy check: an offer on a
role whose vacancies are full is a 409 `no_vacancies`. The mutation reads
that error's details and says "every vacancy already has an offer (3 of 3)"
([`use-applications.ts:129`](../../web/src/hooks/use-applications.ts#L129)).

## 3. The status buttons

**📍 Open:** [`web/src/components/applications/application-detail.tsx:201`](../../web/src/components/applications/application-detail.tsx#L201)

- Forward moves are primary buttons with an arrow; **Reject** is an outline
  button behind a confirmation dialog
  ([line 224](../../web/src/components/applications/application-detail.tsx#L224)),
  because rejection is final and one mis-click should not end an
  application.
- A rejected application gets no buttons, just "Final: no further moves".
- **Not optimistic**, unlike the roles list in 11.3. An offer can be
  refused by the vacancy check, and briefly showing "Offer" before taking
  it back is worse, on a deliberate one-off action, than a short wait.
  After the PATCH, every application and job-post query is invalidated: the
  detail brings back new allowed moves and a new timeline entry; lists and
  the ranking show the new status.

## 4. A timeline that reads as a story

**📍 Open:** [`web/src/lib/applications/timeline.ts:25`](../../web/src/lib/applications/timeline.ts#L25)

The event log stores `application.assigned` with
`{"interviewer_id": "…"}`. A tool people use all day should not make them
translate enums and UUIDs in their heads: each translation is a small cost,
paid on every event, by every user, every day, and it is where misreadings
happen ("assigned" — assigned to whom? by whom?). A sentence carries the
actor, the action and the object at once:

> Ada Admin assigned Ivan Interviewer as interviewer

`describeEvent` turns each type and payload into one sentence:

- interviewer IDs are resolved from what the page already holds — the
  assignments list and the reviews both carry names
  ([`application-detail.tsx:402`](../../web/src/components/applications/application-detail.tsx#L402)) —
  and an ID it cannot resolve (an interviewer since unassigned, with no
  review) becomes "an interviewer", never a UUID;
- a rescore shows the totals before and after, which the event keeps
  precisely because the old score rows are gone (07-4);
- an unknown event type (the server adds one before the client knows it)
  still renders, as "Actor: type in words"
  ([line 59](../../web/src/lib/applications/timeline.ts#L59));
- times are relative ("3 hours ago", via `Intl.RelativeTimeFormat`) with the
  full date in the `title` and the `<time dateTime>`; newest first, because
  what changed last is what someone comes back to see.

Each sentence has an icon by kind (applied, moved, rejected, scored, people,
review), so the eye can skim a long history before reading it.

## 5. Reviews and the visibility rules

**📍 Open:** [`web/src/components/applications/application-detail.tsx:349`](../../web/src/components/applications/application-detail.tsx#L349)

9.2 enforces review visibility **in SQL**: an admin gets every review, an
interviewer only their own, a candidate no `reviews` key at all. The UI
does not re-filter; it renders what the server sent and keeps the
server's distinction between **absent** ("not visible to you") and
**empty** ("no interviewer has filed a review yet")
([line 352](../../web/src/components/applications/application-detail.tsx#L352)).
Hiding data in the frontend would be the excessive-data-exposure mistake
9.2 was written to avoid.

The review summary in the side column shows the mean mark and names who is
still to review, from the assignments' `reviewed` flag. Notes render as
text children, so a review is as XSS-safe as an answer (13.2).

## 6. The leaderboard

**📍 Open:** [`web/src/components/ranking/leaderboard.tsx:29`](../../web/src/components/ranking/leaderboard.tsx#L29)

A new **Ranking** tab on the role page. Each row: position, name, status,
percentage, a mini breakdown bar, and a chevron; the whole row is one link
to the candidate.

- **The cut-off** ([line 60](../../web/src/components/ranking/leaderboard.tsx#L60))
  is a dashed rule with words — "Cut-off: the top 3 fill the vacancies" —
  inserted after row *vacancies*. It is an `<li>`, so a screen reader meets
  it at the same place in the list. It is drawn only when it falls among
  scored rows; an unscored applicant has no position relative to a score
  line.
- **Highlighting without a game**
  ([line 106](../../web/src/components/ranking/leaderboard.tsx#L106)): rows
  above the line get a quiet tinted square behind the rank number. No
  trophies, medals, gold/silver/bronze or confetti — those say "winner",
  and this is a shortlist of people, decided by humans. The rank stays a
  plain number, and the tint is repeated in words for screen readers
  ("within the vacancies").
- **The mini bar** ([line 132](../../web/src/components/ranking/leaderboard.tsx#L132))
  is the part-1 encoding in miniature: the bar's full width is the whole
  rubric, each segment as wide as a criterion's available points, filled
  as far as earned. It starts at zero; the percentage beside it is the
  fact. On a phone it stacks under the percentage
  ([line 141](../../web/src/components/ranking/leaderboard.tsx#L141)).

## 7. A breakdown bar without N+1

**📍 Open:** [`internal/store/ranking_breakdown.go:15`](../../internal/store/ranking_breakdown.go#L15)

The ranking endpoint returned totals only. A per-criterion bar needs each
row's criterion points. The obvious client fix — call
`/applications/:id/score` for each visible row — is the N+1 of 07-5,
moved into the browser: 50 rows, 50 requests.

Instead the ranking response gains a `breakdown` array, loaded by **one**
query for the whole page: `WHERE s.application_id = ANY($1)` with every
scored ID as one array parameter
([line 30](../../internal/store/ranking_breakdown.go#L30)), a correlated
subquery keeping each application to its latest rubric version (as
`ScoreCard` does), ordered by rubric position. The handler collects the
scored IDs first and makes that one call
([`application_handler.go:831`](../../internal/api/application_handler.go#L831)).

It is a separate method on purpose: `RankApplications` is still 07-5's
deliberate N+1, waiting for its step-2 rewrite, and this change leaves it
untouched. Whatever step 2 becomes, the breakdown costs one round trip.
The integration test checks that each row's criterion points add up to its
ranking total, and that an unscored application has none
([`ranking_test.go:78`](../../test/integration/ranking_test.go#L78)).

## 8. Verification

| Command / check | Result |
|---|---|
| `make fmt && make lint` | formatted; golangci-lint 0 issues |
| `make test` | all packages ok; `internal/api` 91.3% coverage |
| `make test-integration` | `internal/store` ok, `test/integration` ok (65s) |
| `go build ./...` | ok |
| `npx tsc --noEmit`, `npm run lint`, `npm run build` in `web/` | no errors; `/admin/roles/[id]/ranking` built |
| `GET /v1/applications/:id` after shortlist → interview | `allowed_next_statuses: ["offer", "rejected"]` |
| `GET /v1/job-posts/:id/ranking` on `make seed-bulk`'s 500 applications | each row has four criteria; unscored rows `[]` |
| Detail page, top candidate | Make offer / Reject in the header; timeline reads "Ada Admin assigned Ivan Interviewer as interviewer" |
| Leaderboard, 3 vacancies | Cut-off after rank 3; ranks 1–3 tinted |
| Both pages at 375×812, light and dark | No horizontal scroll; mini bar under the percentage |
