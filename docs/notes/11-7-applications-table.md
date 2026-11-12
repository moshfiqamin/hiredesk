# Module 11, part 7: The applications table

The screen an admin uses most: every application, paged by the server,
filtered by status and role, sorted by date or score, searchable through the
module 8 full-text endpoint, with bulk status changes and a click-through to
each candidate. Every bit of what is on screen lives in the URL.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | Why server-side pagination, even for a few hundred rows | [`internal/store/applications.go:176`](../../internal/store/applications.go#L176) |
| 2 | What the list endpoint had to learn | [`internal/api/application_handler.go:153`](../../internal/api/application_handler.go#L153) |
| 3 | Keyset paging by score, and its one weakness | [`internal/store/applications.go:184`](../../internal/store/applications.go#L184) |
| 4 | A cursor that knows its sort | [`internal/api/application_handler.go:320`](../../internal/api/application_handler.go#L320) |
| 5 | Who sees scores and reviewers | [`internal/api/application_handler.go:309`](../../internal/api/application_handler.go#L309) |
| 6 | The URL as the single source of truth | [`web/src/lib/applications/view.ts:18`](../../web/src/lib/applications/view.ts#L18) |
| 7 | How debouncing works, and the 300ms | [`web/src/hooks/use-debounced-callback.ts:14`](../../web/src/hooks/use-debounced-callback.ts#L14) |
| 8 | Keeping the search box and the URL in step | [`web/src/components/applications/applications-view.tsx:92`](../../web/src/components/applications/applications-view.tsx#L92) |
| 9 | Search is per role, and pages by offset | [`web/src/hooks/use-applications.ts:53`](../../web/src/hooks/use-applications.ts#L53) |
| 10 | A score column that reads like a shortlist | [`web/src/components/applications/cells.tsx:55`](../../web/src/components/applications/cells.tsx#L55) |
| 11 | Bulk status change, one request per row | [`web/src/hooks/use-applications.ts:69`](../../web/src/hooks/use-applications.ts#L69) |
| 12 | Paging without a flash: `keepPreviousData` | [`web/src/hooks/use-applications.ts:39`](../../web/src/hooks/use-applications.ts#L39) |
| 13 | Empty, loading and error, per situation | [`web/src/components/applications/applications-view.tsx:529`](../../web/src/components/applications/applications-view.tsx#L529) |
| 14 | The candidate detail page | [`web/src/components/applications/application-detail.tsx:84`](../../web/src/components/applications/application-detail.tsx#L84) |
| 15 | The matrix column module 11.2 forgot | [`test/integration/authz_test.go:303`](../../test/integration/authz_test.go#L303) |
| 16 | Verification | — |

---

## 1. Why server-side pagination, even for a few hundred rows

**📍 Open:** [`internal/store/applications.go:176`](../../internal/store/applications.go#L176)

The development database already holds 1,002 applications (the module 7.5
bulk seed, twice). Fetching all of them to page in the browser would mean:

- **Bytes and time on every visit.** Each row carries names, a title, a
  score and a list of assignees. A thousand of them is a payload measured in
  hundreds of kilobytes, downloaded and parsed before the first row shows, to
  display twenty-five.
- **The server's work done anyway.** Every row needs its score summed and its
  assignees joined. The server would do that for all thousand on every load,
  where a page needs twenty-five.
- **Stale copies.** A thousand rows held in the browser are a snapshot.
  Applications keep arriving and moving through the pipeline; a page fetched
  when needed shows the pipeline as it is.
- **It does not stay "a few hundred".** Applications grow with every open
  role and are never deleted, unlike roles (11.2 keeps the roles list
  unpaged because an organisation has tens of them). A design that works at
  300 and falls over at 30,000 has to be rewritten exactly when the product
  is succeeding.
- **Authorization stays in one place.** The server returns only rows the
  caller may see, filtered in SQL (module 3). A client-side pager over
  "everything" invites an endpoint that returns everything.

So the browser asks for one page, `limit=25`, and a `total` for "Showing
26–50 of 1,002".

## 2. What the list endpoint had to learn

**📍 Open:** [`internal/api/application_handler.go:153`](../../internal/api/application_handler.go#L153)

`GET /v1/applications` returned bare application rows: IDs, status, dates.
The table needs the candidate's name, the role's title, the score and the
assigned interviewers. Fetching those per row would be twenty-five extra
requests per page, the N+1 that module 7.5 measured. Each row now carries
`candidate_name`, `job_post_title`, `score` and `assignees`, joined in the
same statement.

The store builds two statement shapes
([`applicationPageQuery`](../../internal/store/applications.go#L176)):

- **Newest first:** find the page first, by the `(submitted_at, id)` index,
  then join names, score and assignees for those twenty-five rows only.
- **By score:** the order depends on the score, so every matching row is
  scored before any can be ordered, and the page is cut afterwards.

The score is the newest rubric version's sum as a percentage
([`latestPercentage`](../../internal/store/applications.go#L158)), the same
rule as the score card. The integration test proves it: an application with
90 under version 1 and 40 under version 2 lists at 40. Assignees are
aggregated into one `jsonb` array per row with `jsonb_agg`, which pgx decodes
straight into a Go slice.

`domain.Application` gained these as list-only fields, as it already had
`Answers` "only set by SubmitApplication". That kept the store interface, and
the test fakes implementing it, unchanged.

## 3. Keyset paging by score, and its one weakness

**📍 Open:** [`internal/store/applications.go:184`](../../internal/store/applications.go#L184)

Keyset paging (module 4.4) continues "after the last row seen" with a row
comparison. By score, the sort key is `(sort_score, submitted_at, id)`, where
`sort_score` is the percentage or `-1` for an unscored application. The `-1`
keeps the key non-null, so one comparison
([line 190](../../internal/store/applications.go#L190)) works for every row
and unscored applications sort after all scored ones.

Verified against the real data: paging all 1,002 by score, 100 at a time,
returned 1,002 unique IDs, in non-increasing score order, unscored last.

The weakness, stated rather than hidden: module 4.4 insisted on an
**immutable** sort key, and a score is not one. A rescore can move an
application between pages while someone is paging, so it could be seen twice
or skipped. That is acceptable here: rescores are rare, deliberate admin
actions, and the next load is correct. The cost is the other compromise:
scoring every match per page is O(matches). At a thousand that is
milliseconds. Past tens of thousands per role, the fix is to store the
latest percentage on the application row (written by submit and rescore, in
their transactions) and index `(job_post_id, score_percentage, submitted_at,
id)`, which makes the score order as cheap as the date order.

## 4. A cursor that knows its sort

**📍 Open:** [`internal/api/application_handler.go:320`](../../internal/api/application_handler.go#L320)

The cursor gained an optional score, `s`
([`cursor.go:35`](../../internal/api/cursor.go#L35)). `omitempty` leaves it
out of date-order cursors, so a cursor issued before this module still
decodes, and the version stays 1. A cursor from one order used with the
other would start the page at a meaningless point, so the handler refuses the
mismatch as `400 invalid_cursor` either way. Both directions are tested.

## 5. Who sees scores and reviewers

**📍 Open:** [`internal/api/application_handler.go:309`](../../internal/api/application_handler.go#L309)

| Caller | `score` | `assignees` | `sort=score` |
|---|---|---|---|
| admin | `{ "percentage": 82.5 }`, or `null` inside when unscored | the list, `[]` if nobody | allowed |
| interviewer | as admin (only their assigned rows are listed) | **absent** | allowed |
| candidate | **absent** | **absent** | **403** |

A candidate may not see their score (module 7.3), so they may not sort by it
either: the order alone would leak it. The response distinguishes "hidden"
from "not scored yet": `score` is absent for the first and
`{"percentage": null}` for the second. A bare nullable number could not tell
them apart. `Assignees` is a pointer to a slice
([line 167](../../internal/api/application_handler.go#L167)) because
`omitempty` drops a nil pointer but not an empty slice behind one: an admin
sees `[]`, everyone else sees no key. The store computes nothing it will not
return (the score and assignee joins are left out of the SQL for roles that
may not see them), and the store integration test checks all three roles.

## 6. The URL as the single source of truth

**📍 Open:** [`web/src/lib/applications/view.ts:18`](../../web/src/lib/applications/view.ts#L18)

Status, role, sort, search term, cursor, page number and search offset are
query parameters, and nothing else. The component **derives** the view from
the URL on every render
([`applications-view.tsx:68`](../../web/src/components/applications/applications-view.tsx#L68))
and changes it only by navigating ([`go`](../../web/src/components/applications/applications-view.tsx#L80)).
There is no second copy in `useState` to fall out of step with the address
bar, so:

- **Share and bookmark:** the URL *is* the view. Opening
  `?role=…&q=kubernetes%20terraform` in a fresh tab restored the role, the
  search box and the results.
- **Back and Forward work:** filter changes and "Next page" use
  `router.push`, so each is a history entry. Typing uses `router.replace`, so
  a search does not leave a Back step per pause.
- **Reload keeps your place.**

`parseView` validates everything it reads (an unknown status, a role that is
not a UUID, a negative page): a hand-edited link degrades to a sensible page
instead of a 422. `withFilters`
([line 76](../../web/src/lib/applications/view.ts#L76)) resets the cursor,
page and offset on any filter change: a cursor belongs to the result set it
came from. `viewToSearch` leaves defaults out, so URLs stay short.

The query key includes the same view, so TanStack Query caches each view
separately: going Back to page 1 shows it instantly from cache.

The page wraps the view in `<Suspense>`: `useSearchParams` reads the URL,
which is not known while prerendering (11.2 explains why Next asks for it).

## 7. How debouncing works, and the 300ms

**📍 Open:** [`web/src/hooks/use-debounced-callback.ts:14`](../../web/src/hooks/use-debounced-callback.ts#L14)

Without debouncing, typing "postgres" sends eight requests, for "p", "po",
"pos"…, and the responses can arrive out of order, so the table might settle
on the results for "postg". Debouncing waits until the input has been quiet
for a set time and then acts once.

The mechanism is a timer that every call resets:

1. A keystroke calls the debounced function.
2. It cancels any pending timer (`clearTimeout`,
   [line 29](../../web/src/hooks/use-debounced-callback.ts#L29)) and starts a
   new one for 300ms.
3. Another keystroke within 300ms cancels that timer and starts again.
4. Only when 300ms pass with no keystroke does the timer fire, once, with the
   latest text.

Two details make the hook correct. The timer calls `latest.current`, updated
after every render ([line 19](../../web/src/hooks/use-debounced-callback.ts#L19)),
so it runs the current closure, not the one from the first keystroke. And the
pending timer is cleared on unmount, so a search never fires for a page that
has been left.

**Why 300ms.** A steady typist leaves roughly 150–250ms between keystrokes,
and pauses for longer at the end of a word. 300ms is just above the
in-word gap: a word is sent once, when the typist pauses, yet the results
still appear to follow the typing. Below about 200ms it fires mid-word for
many people; above about 500ms it starts to feel unresponsive. Verified:
typing "postgres" with real key presses sent exactly one request.

The first version debounced the *value* (`useDebouncedValue`) and wrote the
URL in an effect. The React Compiler lint refused it
(`react-hooks/set-state-in-effect`): setting state in an effect body causes
an extra render pass. Debouncing the *callback* puts the work where it
belongs, in response to the event.

## 8. Keeping the search box and the URL in step

**📍 Open:** [`web/src/components/applications/applications-view.tsx:92`](../../web/src/components/applications/applications-view.tsx#L92)

The box needs local state: if it rendered the URL's `q` directly, each
keystroke would wait for a navigation. So there are briefly two copies, and
they are reconciled in both directions:

- **Box to URL:** the debounced callback reads the *current* URL from
  `window.location` (other filters may have changed since the keystroke) and
  records what it wrote in `syncedQ` before navigating.
- **URL to box:** when the URL's `q` changes without the box (Back, Forward),
  the box is reset during render
  ([line 111](../../web/src/components/applications/applications-view.tsx#L111)).
  Adjusting state when the thing it mirrors changes is React's documented
  alternative to an effect.

Recording `syncedQ` first matters: otherwise the URL update arriving a few
milliseconds after the debounce would reset the box, erasing anything typed
in between.

## 9. Search is per role, and pages by offset

**📍 Open:** [`web/src/hooks/use-applications.ts:53`](../../web/src/hooks/use-applications.ts#L53)

The module 8 endpoint is `GET /v1/job-posts/:id/applications/search`: one
role's answers. So search runs when a role is chosen; with text but no role,
a hint under the box says "Search looks inside one role's answers. Choose a
role to run it." Search results are ordered by relevance and paged by
offset, as module 8.3 decided (relevance is computed per query, so there is
no stored key for a cursor), so search mode has Previous and Next, and status
and sort are disabled with the reason given. The search entries now include
assignees too, so the table looks the same in both modes.

## 10. A score column that reads like a shortlist

**📍 Open:** [`web/src/components/applications/cells.tsx:55`](../../web/src/components/applications/cells.tsx#L55)

After the name, the eye should land on the score. The cell is a right-aligned
percentage in tabular Plex Mono, then an 80px bar. The number is the fact;
the bars, stacked down the column, let the eye compare twenty-five scores
without reading them. The bar's fill repeats the score band's pattern (solid,
hatched, dashed outline, [line 41](../../web/src/components/applications/cells.tsx#L41)),
so band does not depend on hue, and a hidden ", strong" gives screen readers
the same.

Sorting by score adds a **Rank** column, computed from the page
(`(page − 1) × 25 + i + 1`), and an arrow on the Score header. Rank 1 to 25
on page one, 26 onward on page two: it reads as a shortlist. Everything else
in the row is deliberately quieter: status badges are neutral except Offer
and Rejected, and interviewers are small initials (`role="img"` with all
names as its label, since initials alone would be read as letters).

## 11. Bulk status change, one request per row

**📍 Open:** [`web/src/hooks/use-applications.ts:69`](../../web/src/hooks/use-applications.ts#L69)

The API changes one application's status per request, and each is judged on
its own: a submitted application can be shortlisted, one at the offer stage
cannot. A bulk endpoint would need its own partial-failure semantics; the
per-row route already has them. So the hook sends one PATCH per selected row,
four at a time (four workers sharing one queue,
[line 77](../../web/src/hooks/use-applications.ts#L77)): bounded load on the
API, without fifty requests at once or one at a time.

The confirmation names the count ("Move 2 applications to Shortlisted?") and
says what happens to the rest ("Each one moves only if its current status
allows it"). The result is reported honestly: with one success and one 409,
the toast read "Moved 1 of 2 applications to Shortlisted" and named the
refused candidate with the server's reason. The selection lives in a child
keyed by the view, so changing filter or page remounts it and a selection
never carries over to rows not on screen. The header checkbox shows the
third state, `aria-checked="mixed"`, when some rows are selected.

## 12. Paging without a flash: `keepPreviousData`

**📍 Open:** [`web/src/hooks/use-applications.ts:39`](../../web/src/hooks/use-applications.ts#L39)

Every filter or page change is a new query key, and a new key starts with no
data, which would drop the table to a skeleton on every click.
`placeholderData: keepPreviousData` keeps showing the previous rows while the
new ones load; the table dims and sets `aria-busy` while
`isPlaceholderData` is true. The skeleton appears only for a truly first
load.

## 13. Empty, loading and error, per situation

**📍 Open:** [`web/src/components/applications/applications-view.tsx:529`](../../web/src/components/applications/applications-view.tsx#L529)

An empty table means different things, and each gets its own words:

| Situation | Title | Offered |
|---|---|---|
| no applications at all | "No applications yet", and that they arrive scored | Go to roles |
| filters match nothing | "No offer applications for Backend Engineer (Go)…", "No one has been made an offer yet." | Clear filters |
| search matches nothing | "No answers match “kubernetes terraform”", and that every word must appear, so fewer words find more | Clear search |

Loading is a skeleton with the table's columns. A failed load shows the
shared `ErrorState`, with the request ID and Try again. A forced 500 showed
it after three attempts: one try and two retries, the 11.2 policy for a 5xx.

## 14. The candidate detail page

**📍 Open:** [`web/src/components/applications/application-detail.tsx:84`](../../web/src/components/applications/application-detail.tsx#L84)

The name links to `/admin/applications/:id`: the score breakdown (each
criterion's points, the engine's explanation and the evidence excerpt), the
answers under their questions, and the history. The detail endpoint has no
candidate name; the submission event's actor *is* the candidate, and the
timeline resolves actor names when read (module 9.3), so the name comes from
there. The role and score queries live in a child component that renders
once the application has loaded, so they never fire with an empty role ID.

## 15. The matrix column module 11.2 forgot

**📍 Open:** [`test/integration/authz_test.go:303`](../../test/integration/authz_test.go#L303)

Running the full integration suite for this module failed:
`GET /v1/admin/job-posts is not in TestAuthorizationMatrix; add a column for
it`. Module 11.2 added the route and ran the store and handler tests, but not
the authorization matrix, which fails by design for any protected route it
does not cover. The column is added here (admin 200, every other role 403,
anonymous 401). The 11.2 to 11.6 commits therefore fail that one test
locally; the pushed head passes, and the gap is the lesson: run
`make test-integration`, not only the package that changed.

## 16. Verification

| Check | Result |
|---|---|
| `go test ./internal/api/` | ok, including sort reaching the store, the score cursor (an unscored last row gives `-1`), candidate 403, both cursor mismatches 400, an unknown sort 422, and the JSON keys per role |
| `go test -tags integration ./internal/store/` | ok; `TestListApplicationsByScore`: two 80s newer first, then 40 (newest rubric version only), then unscored; page two continues correctly; admin, interviewer and candidate each get exactly their fields |
| `make test-integration` | first run failed on the missing matrix column (section 15); passes with it |
| API, all 1,002 rows by score, 100 per page | 1,002 unique IDs, non-increasing order, unscored last |
| Browser, sort by score | rank 1 at 100.0%, then the 93.8% group; "Showing 1–25 of 1,002" |
| Next, Next, Back | pages 26–50 and 51–75, then Back to 26–50 from the URL |
| Type "postgres" with a role chosen (real keys) | one search request; URL gained `q=postgres`; one match |
| Open a search URL in a new load | box, role and empty state restored from the URL |
| `status=offer` for a role with none | "No offer applications for …", Clear filters |
| Bulk: two rows → Shortlisted, PATCH stubbed 200 then 409 | dialog "Move 2 applications to Shortlisted?"; two PATCH bodies; toast "Moved 1 of 2…" naming the refused one; selection cleared |
| Header checkbox | `mixed` with one selected; "25 applications selected" after select all |
| Forced 500 on the list | ErrorState with `REQ123` after 3 attempts |
| Detail page | name from the submission event, 100/100 breakdown with evidence, answers (yes/no shown as Yes/No), seven history entries |
| `make fmt && make lint && make test && go build ./...` | see the commit body |
