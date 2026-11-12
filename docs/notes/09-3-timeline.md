# Module 9, part 3 — The timeline, the event audit, and a tighter authorization test

`GET /v1/applications/:id/events` (admins, and the interviewers assigned):
the application's whole history, newest first, keyset-paged, each event with
its actor resolved to a name. Plus an audit of which events are written and
by whom, and seventy-two new cells in `TestAuthorizationMatrix`.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-the-log-can-answer-that-a-status-column-cannot) | **What the log can answer that a status column cannot** | — |
| [2](#2-the-endpoint) | The endpoint | [`application_handler.go:1032`](../../internal/api/application_handler.go#L1032) |
| [3](#3-resolving-the-actor-at-read-time) | **Resolving the actor at read time** | [`applications.go:602`](../../internal/store/applications.go#L602) |
| [4](#4-the-audit-are-all-seven-events-written) | The audit: are all seven events written? | [`timeline_test.go:33`](../../test/integration/timeline_test.go#L33) |
| [5](#5-the-audit-does-every-event-have-an-actor) | The audit: does every event have an actor? | — |
| [6](#6-what-the-log-is-worth-in-a-dispute) | **What the log is worth in a dispute** | — |
| [7](#7-the-authorization-matrix) | The authorization matrix | [`authz_test.go:306`](../../test/integration/authz_test.go#L306) |
| [8](#8-verification) | Verification | — |

---

## 1. What the log can answer that a status column cannot

A `status` column with an `updated_at` stores **the latest fact and when it
last changed**. Each UPDATE overwrites the one before. It answers exactly
one question: *where is this application now, and since when?*

An append-only event log stores **every change, who made it, and what it
changed from and to**, and nothing in it is ever overwritten. That answers
the questions the column destroyed:

| Question | Status column | Event log |
|---|---|---|
| Where is it now? | yes | yes (the latest `status_changed`) |
| **Who** rejected it? | no: overwritten state has no author | `actor` on the event |
| What was it **before** it was rejected? | no | `from` in the payload |
| Was it **ever** shortlisted? | no, if it has moved since | yes |
| How long did it sit at each stage? | no, only since the last change | differences between event times |
| Was the mark changed **after** the decision? | no; `reviews.updated_at` says *that* it changed, not from what or relative to what | `review_updated` with `from_mark` and `to_mark`, ordered against `status_changed` |
| What score did the decision rest on? | no; a rescore replaces the scores | `rescored` with old and new totals and rubric versions |
| Who could see it, and when? | no | `assigned` / `unassigned` |

The general point: **state is a summary of history**, and a summary can be
rebuilt from the history but never the other way round. `updated_at` is one
lossy number standing in for a whole sequence. The table is append-only by
design (000005 gives it no `updated_at` and no UPDATE path), because an
event that can be edited is a claim, not a record. A correction is a new
event.

The cost is storage and some read complexity, both tiny here: a handful of
events per application, read through an index that already exists.

## 2. The endpoint

**📍 Open:** [`internal/api/application_handler.go:1032`](../../internal/api/application_handler.go#L1032)

- **Access** follows the score endpoint (07-3): `CanViewApplication` first,
  so anyone who may not see the application gets the standard 404, then
  `CanViewTimeline`, so a candidate who can see their own application gets
  a 403 for its timeline. They already know it exists, so a 404 would only
  mislead.
- **What each caller sees** is `auth.ApplicationViewFor(user)`, the same
  view as the detail endpoint (09-2), applied in SQL by the same
  `addEventVisibility`. An assigned interviewer sees every event except
  other interviewers' review events.
- **Newest first, keyset-paged**, `?limit=20&after=<cursor>`, the same
  opaque cursor as the other lists (04-4), on `(created_at, id)`, served by
  `application_events_application_id_created_at_idx`. Filtering in SQL is
  what keeps pages full; filtering after the `LIMIT` would make short pages.
- **Each entry**: `id`, `type`, `actor` (`{id, name, role}` or `null`),
  `payload` (always an object, never `null`) and `created_at`.

## 3. Resolving the actor at read time

**📍 Open:** [`internal/store/applications.go:602`](../../internal/store/applications.go#L602)

The event stores only `actor_id`. The name comes from a `LEFT JOIN users`
when the timeline is read:

```sql
FROM application_events e LEFT JOIN users u ON u.id = e.actor_id
```

`LEFT`, because a system event has no actor, and an actor whose account was
deleted has `actor_id` set to NULL (`ON DELETE SET NULL`). Either way the
event still appears, with `"actor": null`.

**Why not copy the name into the payload when writing the event?**

- **One source of truth.** A person's name lives in `users`. A copy in each
  of their events is a denormalisation (08-2), and drifts the moment
  someone fixes a typo in their name, marries, or transitions. The timeline
  would show different names for the same person on different days.
- **The right to be forgotten.** Under GDPR a person can ask for their
  personal data to be erased. With names in `users` only, anonymising one
  row (`full_name = 'Former employee'`) covers every event at once, while
  the event itself, a record that *a decision was made*, survives. With
  names copied into thousands of jsonb payloads, erasure means rewriting
  an append-only log.
- **Smaller events**, and no question of which name format was current
  when.
- **The join is cheap**: one primary-key lookup per event on a page of at
  most 100.

`TestTimeline_EveryEventTypeIsWritten` ends by renaming Ivy and checking
that her earlier review events show the **new** name.

**When copying would be the right choice.** When the question is "what did
this record say *at the time*?", not "who is this person now?":

- **Legal or compliance records** that must be reproducible exactly as
  issued, such as an offer letter's signatory, an invoice's billing name,
  or an audit report signed by a named officer;
- **The source of names is outside your control or may disappear**: events
  from another system, or a service you cannot join to;
- **The log is exported or shipped elsewhere** (a data warehouse, an event
  stream), where consumers cannot join back to your `users` table;
- **Read volume is enormous** and the join measurably costs more than the
  storage.

Even then, the usual answer is to keep the ID too, so both questions stay
answerable.

## 4. The audit: are all seven events written?

**📍 Open:** [`test/integration/timeline_test.go:33`](../../test/integration/timeline_test.go#L33)

Static, by `grep` for each constant in `internal/store`:

| Event | Written in | Actor | Payload |
|---|---|---|---|
| `application.submitted` | `SubmitApplication` | the candidate | `job_post_id`, `answers`, `rubric_version` |
| `application.status_changed` | `TransitionApplication` | the admin | `from`, `to` |
| `application.rescored` | `rescore` (one or all) | the admin | old and new totals and versions, `trigger` |
| `application.assigned` | `AssignInterviewer`, only if inserted | the admin | `interviewer_id` |
| `application.unassigned` | `UnassignInterviewer`, only if deleted | the admin | `interviewer_id` |
| `application.reviewed` | `CreateReview` | the interviewer | `review_id`, `interviewer_id`, `mark` |
| `application.review_updated` | `UpdateReview`, only if something changed | the interviewer | `review_id`, `interviewer_id`, `from_mark`, `to_mark`, `notes_changed` |

**Before this module three were written**, so four were missing:
`assigned`, `unassigned`, `reviewed` and `review_updated` did not exist,
because the features they describe did not exist. All seven are written
now, each from exactly one place, each in the same transaction as the
change it records.

Dynamically, `TestTimeline_EveryEventTypeIsWritten` drives one application
through every route (submit, assign two interviewers, unassign one, review,
correct, rescore, shortlist), walks the timeline two events at a time, and
requires all seven types, eight events, no repeats across pages, newest
first, and a resolved actor on every one.

Two other things I checked and left alone:

- **`job_post_events` is a separate log** (000007) for post-level events
  such as opening, closing and rubric changes. They are about the post, not
  an application, so they rightly do not appear in this timeline. A
  dispute might still want "which rubric was current when I was scored?",
  and the answer is in `rescored`'s `new_rubric_version` and in each
  score's own `rubric_version`.
- **The rubric event is called `rubric_replaced`**, without the `job_post.`
  prefix the application events use. Harmless, but if you rename it,
  existing rows keep the old name; that is a note for the job-post module,
  not this one.

## 5. The audit: does every event have an actor?

**Yes.** Live database and test databases both:

```sql
SELECT type, count(*), count(*) FILTER (WHERE actor_id IS NULL) FROM application_events GROUP BY type;
```

returned zero events without an actor for every type, and the timeline test
asserts the same. **There are no system-generated application events
today**: every write path is triggered by an authenticated person, and each
passes that person's ID. Even a bulk `rescore-all` names the admin who
asked for it, and `make seed-bulk` submits through the real path, so its
events name each bulk candidate.

**Is a NULL actor ever a bug?** Two cases would produce one, neither a bug:

- **A future system job** (an automatic rejection when a post closes, a
  nightly rescore). `actor_id` is nullable exactly for this (000005's
  comment). Such events should say so in their payload, e.g.
  `{"trigger": "job_post_closed"}`, so "nobody" is distinguishable from
  "someone we lost".
- **A deleted account.** `ON DELETE SET NULL` keeps the event and drops the
  link. There is no account deletion endpoint yet, and reviewers *cannot*
  be deleted at all (`reviews.interviewer_id` is `ON DELETE RESTRICT`), so
  for review events this cannot happen. If deletion is added, prefer
  anonymising the `users` row over deleting it: the actor then resolves to
  "Former employee" rather than to nothing.

A NULL actor on an event written by a request handler **would** be a bug,
and the test would catch it.

## 6. What the log is worth in a dispute

Suppose a rejected candidate writes in: *"I was rejected unfairly. I think
someone marked me down after the fact."* With only a status column, the
company can say "you were rejected on 14 March". With this log, it can
reconstruct the whole process, in order, with names:

```text
02 Mar 09:00  application.submitted        Applicant         rubric_version 1
02 Mar 11:20  application.assigned         Ada Admin         interviewer: Ivy
02 Mar 11:21  application.assigned         Ada Admin         interviewer: Omar
04 Mar 15:02  application.reviewed         Omar Interviewer  mark 4
05 Mar 10:40  application.reviewed         Ivy Interviewer   mark 7
09 Mar 16:00  application.rescored         Ada Admin         62.5 → 58.0, rubric v1 → v2
14 Mar 09:15  application.status_changed   Ada Admin         submitted → rejected
15 Mar 08:30  application.review_updated   Ivy Interviewer   7 → 3
```

That answers, from records rather than memory:

- **Who decided, and when**: Ada, on 14 March.
- **What the decision rested on**: marks of 4 and 7, and a score that a
  rubric change had moved from 62.5 to 58.0 five days earlier. Was the
  rubric changed to justify a decision already made? The ordering is
  visible.
- **The candidate's suspicion, confirmed or refuted.** In this example Ivy
  lowered her mark from 7 to 3 *the day after* the rejection. That is
  exactly the after-the-fact change the candidate alleged, and without
  `from_mark` there would be no way to know the mark at decision time was 7.
- **Process fairness**: two independent interviewers, neither able to see
  the other's mark before giving their own (09-2), which is a stronger
  position than "we had a quick chat".

Its value as evidence depends on properties this design already has:

- **Append-only and written in the same transaction as the change**, so
  it cannot miss a change or claim one that did not happen (05-2).
- **Attributed**, so every entry names a person.
- **Complete enough to replay the decision**: from/to values, not just
  "something changed".

What it does *not* yet have, and what a regulated deployment would add:
**tamper evidence**. An administrator with database access could still
`DELETE` an event. Revoking UPDATE and DELETE on the table from the
application's role, or chaining each event to the previous one with a hash,
turns "we promise we did not edit it" into "you can verify it". Under the
EU AI Act and GDPR Article 22, automated or scored hiring decisions
increasingly need exactly this kind of explanation, and a candidate may
have a right to obtain it.

## 7. The authorization matrix

**📍 Open:** [`test/integration/authz_test.go:306`](../../test/integration/authz_test.go#L306)

Nine new endpoint columns, for every one of the eight callers: **72 new
cells**, each checking the exact status and, for refusals, the error code
or byte-for-byte equality with a missing record.

| Column | admin | interviewer assigned to A | interviewer assigned to nothing | candidate (owns A) | candidate (owns B) |
|---|---|---|---|---|---|
| `GET` assignments of A | 200 | 403 | 403 | 403 | 403 |
| `POST` assign A's interviewer again | **200** (idempotent) | 403 | 403 | 403 | 403 |
| `POST` assign a candidate to B | **422** | 403 | 403 | 403 | 403 |
| `DELETE` unassigned interviewer from B | **204** (idempotent) | 403 | 403 | 403 | 403 |
| `POST` review of A | 403 | **201** | **404** | 403 | 403 |
| `POST` review of B | 403 | **404** | 404 | 403 | 403 |
| `PATCH` my review of A | 403 | **200** | **404** | 403 | 403 |
| `GET` events of A | 200 | **200** | **404** | 403 | 404 |
| `GET` events of B | 200 | **404** | 404 | 404 | 403 |

Anonymous and deleted-user tokens get 401 everywhere; the candidate with a
forged admin claim gets the candidate-who-owns-A column, because the
database's role wins over the token's.

**The assigned-versus-unassigned distinction** is the bold cells. The same
role, interviewer, gets 201/200 on A and 404 on B for the assigned one, and
404 on both for the other. A role check alone (`RequireRole(interviewer)`)
would have given both of them the same answer; the row-level assignment is
what separates them.

**Writing cells into a shared database.** The matrix runs every caller
against one database, in order, so a cell that changes data can break a
later row. The admin's write cells were chosen so they change nothing: the
repeat assignment and the no-op unassignment are idempotent, and the
candidate assignment is refused. A check after the matrix proves it: no
candidate in `interviewer_assignments`, **zero** assignment events, exactly
**one** review (the assigned interviewer's 201). So the matrix doubles as
an idempotency test.

## 8. Verification

| Command | Result |
|---|---|
| `make test-integration` | all pass, including `TestAuthorizationMatrix` with the 72 new cells and the side-effect check |
| `go test -tags integration -run TestTimeline -v ./test/integration/` | `TestTimeline_EveryEventTypeIsWritten` passes: eight events across four pages, all seven types |
| `grep -rn "domain.EventApplication…" internal/store` for each constant | each written from exactly one place (table in section 4) |
| `psql`: events by type with `actor_id IS NULL` | 0 for every type on the live database |
| `curl` `GET …/events?limit=3` on the live stack | `review_updated` by Ivan Interviewer (`from_mark 8, to_mark 9`), `reviewed` by Ivan Interviewer (`mark 8`), `assigned` by Ada Admin |
