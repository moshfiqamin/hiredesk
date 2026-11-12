# Module 14, part 1 — The interviewer's half

Three pieces: the admin's **assignment dialog**, the interviewer's
**worklist** at `/assignments`, and the **review form**. Plus the check that
matters most: that an interviewer never receives another interviewer's mark,
verified in the network responses, not on screen.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-why-the-api-grew-two-reads) | Why the API grew two reads | [`interviewers.go:14`](../../internal/store/interviewers.go#L14) |
| [2](#2-the-assignment-dialog-interviewers-only-with-their-load) | **The assignment dialog: interviewers only, with their load** | [`assign-dialog.tsx:21`](../../web/src/components/applications/assign-dialog.tsx#L21) |
| [3](#3-the-worklist-unreviewed-first) | **The worklist, unreviewed first** | [`assignments-view.tsx:20`](../../web/src/components/interviewer/assignments-view.tsx#L20) |
| [4](#4-a-1-10-scale-two-people-use-the-same-way) | **A 1–10 scale two people use the same way** | [`scale.ts:17`](../../web/src/lib/reviews/scale.ts#L17) |
| [5](#5-the-review-form-and-your-own-review) | The review form, and your own review | [`review-form.tsx:22`](../../web/src/components/interviewer/review-form.tsx#L22) |
| [6](#6-verifying-in-the-network-response-not-on-screen) | **Verifying in the network response, not on screen** | [`applications.go:652`](../../internal/store/applications.go#L652) |
| [7](#7-verification) | Verification | — |

---

## 1. Why the API grew two reads

**📍 Open:** [`internal/store/interviewers.go:14`](../../internal/store/interviewers.go#L14)

- **`GET /v1/admin/interviewers`.** The picker needs a list of people who
  *can* interview, and nothing in the API listed users. The query selects
  `WHERE u.role = 'interviewer'`, so a candidate or admin cannot be offered.
  The server still refuses one with a 422 if a client sends it anyway (9.1),
  so the list is a convenience and the API is still the enforcement. Load is
  counted in the same statement with `count(*) FILTER (WHERE ...)`
  ([line 24](../../internal/store/interviewers.go#L24)): open assignments
  (applications not yet at offer or rejected) and the part of those not yet
  reviewed. `LEFT JOIN`s keep interviewers with nothing assigned, which is
  the person an admin most wants to find.
- **`reviewed_by_me` on the interviewer's list.** The worklist sorts on
  "have I reviewed this?". Without it the client would fetch every
  application's detail to find out (an N+1 in the browser). It is one
  `EXISTS` per row on the unique `(application_id, interviewer_id)` index
  ([`applications.go:233`](../../internal/store/applications.go#L233)), and
  present only for interviewers
  ([`application_handler.go:171`](../../internal/api/application_handler.go#L171)).
  It says nothing about anyone else's review.

The new route has a column in the authorization matrix
([`authz_test.go:493`](../../test/integration/authz_test.go#L493)): admin
200, every other role 403, anonymous 401.

## 2. The assignment dialog: interviewers only, with their load

**📍 Open:** [`web/src/components/applications/assign-dialog.tsx:21`](../../web/src/components/applications/assign-dialog.tsx#L21)

Opened from **Manage** in the candidate page's Interviewers panel.

- **Ordered for spreading work** ([line 35](../../web/src/components/applications/assign-dialog.tsx#L35)):
  people already assigned first, so revoking is one glance away, then
  lightest load first. An alphabetical list puts the same person on top
  every time, and that person ends up with all the interviews.
- **Load in numbers and a bar.** "4 open, 1 awaiting review". The bar is
  scaled to the busiest interviewer, so it compares people with each other.
  Anyone more than two above the team mean is marked "heavier than most"
  ([line 77](../../web/src/components/applications/assign-dialog.tsx#L77))
  in words, not colour alone.
- **Revoking** is a Remove button. It is not behind a confirmation: it is
  reversible by assigning again, and any review that person filed is kept
  (9.1).
- Every mutation invalidates the application, the lists and the directory
  ([`use-reviews.ts:33`](../../web/src/hooks/use-reviews.ts#L33)), because
  one assignment changes three views at once.

## 3. The worklist, unreviewed first

**📍 Open:** [`web/src/components/interviewer/assignments-view.tsx:20`](../../web/src/components/interviewer/assignments-view.tsx#L20)

Each row: candidate, status, role, score, "To review" or "Reviewed", and the
next action: **Review** (primary) or **View your review** (outline).

**Why unreviewed first.** A worklist exists to be emptied. An interviewer
opens it to find out what is waiting on them, and every reviewed row above an
unreviewed one is a row they have to read past. Unreviewed rows come first,
and within each group the oldest application first: the candidate who has
waited longest is the next one owed a decision. The header says the count in
words ("2 waiting for your review"), in a `role="status"` region. Reviewed
rows stay on the page so an interviewer can go back to their own review.

## 4. A 1–10 scale two people use the same way

**📍 Open:** [`web/src/lib/reviews/scale.ts:17`](../../web/src/lib/reviews/scale.ts#L17)

A bare number input invites each interviewer to bring their own scale: one
person's 6 is "fine", another's is "barely". Averaging them then measures
the interviewers more than the candidate. The scale is built so the number
carries the same meaning for everyone:

1. **Five bands, each anchored in observable evidence**, not feelings:
   "Clear no — could not show the core skills the role needs", up to
   "Strong yes — clearly above the bar; would raise the level of the team".
   This is the behaviourally anchored rating scale approach: descriptions of
   what was seen, which two observers can agree on.
2. **Against the role's bar, not other candidates.** The form says so
   explicitly. A mark relative to "today's candidates" drifts with whoever
   happened to apply this week.
3. **Two marks per band, with a meaning.** Within a band, the higher mark is
   "without reservation", the lower "with a reservation". That gives ten
   points of resolution without asking anyone to invent the difference
   between a 6 and a 7. It is said in words, both visibly and in each
   radio's accessible name ("8, Lean yes, without reservation").
4. **Band first, then the mark.** The hint asks for the band first, which is
   the decision; the mark refines it.
5. **Independence stated.** "Other interviewers' marks are hidden from you"
   tells the interviewer why there is nothing to anchor on, and section 6
   makes it true.

It is one radio group of ten (one `name`), so arrow keys run 1 to 10 across
the bands ([`review-form.tsx:99`](../../web/src/components/interviewer/review-form.tsx#L99)).
Each radio is a transparent layer over its 40px button, so the whole square
is the click target and the radio still takes focus. The ring shows on the
button through `has-focus-visible`.

## 5. The review form, and your own review

**📍 Open:** [`web/src/components/interviewer/review-form.tsx:22`](../../web/src/components/interviewer/review-form.tsx#L22)

- **Notes** have a live count, "70 / 5,000 characters"
  ([line 163](../../web/src/components/interviewer/review-form.tsx#L163)),
  in code points, as the server counts (`utf8.RuneCountInString`). The count
  is tied to the field with `aria-describedby` and is not a live region, so
  it is read with the field rather than on every keystroke.
- **Submit** sends `POST .../reviews`, or `PATCH .../reviews/mine` when a
  review exists. Empty notes on a correction send `""`, which the API
  documents as clearing them.
- **After submitting**, the panel becomes "Your review": the mark, the
  band's words, the notes and an **Edit** button
  ([line 22](../../web/src/components/interviewer/review-form.tsx#L22)).
  The panel takes the review from the detail's `reviews` array, filtered to
  the signed-in interviewer
  ([`interviewer-application.tsx:58`](../../web/src/components/interviewer/interviewer-application.tsx#L58)).
  The filter is not what protects anyone (section 6); it makes explicit
  whose review this panel edits.

## 6. Verifying in the network response, not on screen

**📍 Open:** [`internal/store/applications.go:652`](../../internal/store/applications.go#L652)

**Why the network and not the screen.** A screen that shows only your own
mark proves the *page* hides the rest, not that the *browser* never got
them. If the API sent every review and the UI filtered, any interviewer
could open DevTools, or call the API with their own token, and read every
other interviewer's mark: OWASP's "excessive data exposure". The rule from
9.2 is that the SQL never reads another interviewer's review for this
viewer ([line 652](../../internal/store/applications.go#L652) for reviews,
[line 630](../../internal/store/applications.go#L630) for the review
*events*, whose payload carries the mark). So the evidence has to be the
bytes on the wire.

**What was checked.** Two interviewers, Ivan and Nadia, were assigned to the
same application; Ivan had reviewed it with an 8. Signed in as Nadia in a
browser:

| Response Nadia's browser received | Ivan's mark? | What it contained |
|---|---|---|
| `GET /v1/applications?limit=100` | no | `"reviewed_by_me": false`; no `assignees` key |
| `GET /v1/applications/:id` before her review | no | `"reviews": []`; no `application.reviewed` event |
| `POST /v1/applications/:id/reviews` (mark 7) | — | 201 |
| `GET /v1/applications/:id` after her review | no | `reviews` held exactly one entry, hers (mark 7); the only review event was her own |

Then, with Nadia's token, every endpoint she can reach was searched for
`"mark"`, `"from_mark"` and `"to_mark"`:

| Endpoint | Status | Mark fields | Ivan's review events |
|---|---|---|---|
| `/v1/applications` | 200 | 0 | 0 |
| `/v1/applications/:id` | 200 | 0 | 0 |
| `/v1/applications/:id/events` | 200 | 0 | 0 |
| `/v1/applications/:id/score` | 200 | 0 | 0 |
| `/v1/applications/:id/assignments` | 403 | — | — |
| `/v1/admin/interviewers` | 403 | — | — |

One thing she *can* see: Ivan's user ID, in the `application.assigned`
event. Who else is interviewing is visible by design (9.2 decided that), but
how they marked is not.

## 7. Verification

| Check | Result |
|---|---|
| `go test ./internal/...` | ok; `TestInterviewers` pins the directory body and the 403s; `TestListReviewedByMe` pins the flag for interviewers only |
| `make test-integration` | `TestInterviewerDirectoryAndOwnReviewFlag`: interviewers only, loads before and after a review, flag false then true, never on an admin row. Matrix column for `GET /v1/admin/interviewers` |
| Network check above | Ivan's mark appears in no response Nadia receives |
| Browser, as Nadia | Worklist shows the row "To review"; mark 7 and notes submitted; panel became "Your review" with Edit |
| `e2e/full-flow.spec.ts` | Admin assigns through the dialog; interviewer reviews; admin sees "8.0" |
