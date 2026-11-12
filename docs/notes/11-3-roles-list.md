# Module 11, part 3: The roles list

The first real screen: every job post in a dense, sortable table, with status
filters, title search, optimistic open and close, and a card layout on
phones.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | What an optimistic update is | [`web/src/hooks/use-job-posts.ts:88`](../../web/src/hooks/use-job-posts.ts#L88) |
| 2 | Rolling one back in TanStack Query | [`web/src/hooks/use-job-posts.ts:112`](../../web/src/hooks/use-job-posts.ts#L112) |
| 3 | Why the server still decides | [`web/src/hooks/use-job-posts.ts:128`](../../web/src/hooks/use-job-posts.ts#L128) |
| 4 | Status not by colour alone | [`web/src/components/roles/status-badge.tsx:9`](../../web/src/components/roles/status-badge.tsx#L9) |
| 5 | Tabular figures and right-aligned numbers | [`web/src/app/globals.css:322`](../../web/src/app/globals.css#L322) |
| 6 | Filtering and sorting in the browser | [`web/src/components/roles/roles-view.tsx:77`](../../web/src/components/roles/roles-view.tsx#L77) |
| 7 | Sortable headers that a screen reader can follow | [`web/src/components/roles/roles-view.tsx:267`](../../web/src/components/roles/roles-view.tsx#L267) |
| 8 | Only the moves the lifecycle allows | [`web/src/components/roles/roles-view.tsx:432`](../../web/src/components/roles/roles-view.tsx#L432) |
| 9 | Table to cards at 375px | [`web/src/components/roles/roles-view.tsx:337`](../../web/src/components/roles/roles-view.tsx#L337) |
| 10 | Skeletons shaped like the table | [`web/src/components/roles/roles-view.tsx:503`](../../web/src/components/roles/roles-view.tsx#L503) |
| 11 | An empty state that teaches | [`web/src/components/roles/roles-view.tsx:471`](../../web/src/components/roles/roles-view.tsx#L471) |
| 12 | Verification | — |

---

## 1. What an optimistic update is

**📍 Open:** [`web/src/hooks/use-job-posts.ts:88`](../../web/src/hooks/use-job-posts.ts#L88)

A normal (pessimistic) update waits: click "Open", send the request, show a
spinner, and change the badge only when the server answers. An
**optimistic** update changes the badge the instant the user clicks, on the
assumption that the server will agree, and corrects itself if it does not.

It is the right choice when two things hold: the request usually succeeds,
and the client can predict the result exactly. Opening a role changes one
field from `draft` to `open`; the client knows that without asking. It would
be the wrong choice for, say, creating a post, whose ID and timestamps only
the server can produce.

`onMutate` runs before the request is sent and does four things, in order:

1. **Cancels in-flight reads** of the list and the detail
   ([line 94](../../web/src/hooks/use-job-posts.ts#L94)). A refetch that
   started a moment earlier would otherwise land *after* the optimistic write
   and overwrite it with the old status, so the badge would flicker back.
2. **Takes a snapshot** of both cache entries exactly as they were
   ([line 98](../../web/src/hooks/use-job-posts.ts#L98)).
3. **Writes the prediction** into the cache
   ([line 104](../../web/src/hooks/use-job-posts.ts#L104)). The updater
   returns a *new* array with a *new* object for the changed row, never a
   mutated one: React and TanStack Query detect changes by reference, so an
   object edited in place looks unchanged and nothing re-renders.
4. **Returns the snapshot**. Whatever `onMutate` returns is handed to
   `onError` and `onSettled` as `context`. That is how the rollback finds it,
   without a variable outside the hook.

## 2. Rolling one back in TanStack Query

**📍 Open:** [`web/src/hooks/use-job-posts.ts:112`](../../web/src/hooks/use-job-posts.ts#L112)

When the request fails, `onError` writes the snapshot back with
`setQueryData`. The badge returns to "Draft", and a toast says why.

The most common failure here is not a network problem but the server
refusing: opening a draft with no questions or no rubric is a 409
`not_ready`, whose `details` name each missing piece. `transitionError`
([line 65](../../web/src/hooks/use-job-posts.ts#L65)) uses those sentences
rather than the generic message, so the toast reads "The post has no
questions. Add at least one before opening it. The post has no rubric…",
which tells the admin what to do next.

Success also gets a toast. An optimistic change is easy to miss because
nothing *loads*; the confirmation is the only sign the server agreed.

## 3. Why the server still decides

**📍 Open:** [`web/src/hooks/use-job-posts.ts:128`](../../web/src/hooks/use-job-posts.ts#L128)

`onSettled` runs after success *or* failure and invalidates both entries, so
they are refetched. After a success this replaces the prediction with the
server's record, including what the client could not predict (`updated_at`).
After a failure it guards against a snapshot that was itself out of date.
The optimistic write is a placeholder for the truth, never a substitute.

## 4. Status not by colour alone

**📍 Open:** [`web/src/components/roles/status-badge.tsx:9`](../../web/src/components/roles/status-badge.tsx#L9)

About 1 in 12 men and 1 in 200 women have some colour-vision deficiency. A
badge that is green for open and grey for closed is, to many of them, two
identical badges. Colour also disappears in greyscale printing, on a dim
projector, and for screen-reader users, who get no colour at all. WCAG 1.4.1
(Use of Color) makes this a requirement, not a nicety.

Each status here differs in four ways besides hue:

| Status | Icon shape | Border | Fill and weight |
|---|---|---|---|
| Draft | hollow dashed circle | dashed | none, muted text |
| Open | filled dot | solid, strong | selected fill, semibold |
| Closed | lock | none | sunken fill, secondary text |

Plus the word itself. Each cue carries meaning: dashed reads as unfinished,
the lock reads as final, and the solid, heaviest badge is the one an admin
acts on, which is why the eye finds "Open" first after the title. The badges
use no new hue at all: sage is reserved for actions, selection and focus,
and the blue, ochre and vermilion belong to score bands, so borrowing any of
them would make a status look like a score.

## 5. Tabular figures and right-aligned numbers

**📍 Open:** [`web/src/app/globals.css:322`](../../web/src/app/globals.css#L322)

Most fonts draw digits with **proportional** widths: a 1 is narrower than an
8. That suits prose and wrecks a column, where `111` is visibly shorter than
`888` and the digits of different rows do not sit under each other.
**Tabular figures** give every digit the same advance width, so in a column
the units, tens and hundreds line up and magnitudes can be compared at a
glance. `font-variant-numeric: tabular-nums` switches them on; it was set
for every `table` in 11.1, and the `.tabular` class covers numbers outside
tables, such as the filter counts. The application and vacancy figures also
use IBM Plex Mono, which is tabular by design.

Numbers are **right-aligned** because that aligns place values: 500 and 1
end at the same edge, so the ones sit over the ones. Left-aligned, 1 would
start where 500 starts and look comparable. The column header aligns with
its numbers, and the sort arrow moves to the header's left (`flex-row-reverse`)
so the label's last letter stays on the column's edge.

## 6. Filtering and sorting in the browser

**📍 Open:** [`web/src/components/roles/roles-view.tsx:77`](../../web/src/components/roles/roles-view.tsx#L77)

The roles endpoint returns every post (11.2 explains why it is not paged), so
filtering and sorting happen in the browser, instantly, with no request per
keystroke. `visibleRoles` is a pure function of the posts and the three
controls, and `useMemo` recomputes it only when one of those changes.

Two details:

- `.filter()` returns a new array before `.sort()`, which sorts in place.
  Sorting the cached array directly would reorder the data TanStack Query
  holds, a mutation other components would see.
- Every sort falls back to newest first, so rows that tie on the chosen
  column (all the zero-application drafts) keep a stable, meaningful order
  instead of shuffling between renders.

Status sorts `draft → open → closed` rather than alphabetically
([line 53](../../web/src/components/roles/roles-view.tsx#L53)): ascending
puts the roles still needing work at the top.

## 7. Sortable headers that a screen reader can follow

**📍 Open:** [`web/src/components/roles/roles-view.tsx:267`](../../web/src/components/roles/roles-view.tsx#L267)

Each header is a `<button>` inside a `<th>`, so it is reachable with Tab and
activated with Enter or Space. `aria-sort` is on the `<th>`, not the button,
because it describes the column, and only the sorted column carries it; a
screen reader announces "Applications, column header, sorted descending".
The filter buttons use `aria-pressed`
([line 235](../../web/src/components/roles/roles-view.tsx#L235)), and a
visually hidden live region ([line 177](../../web/src/components/roles/roles-view.tsx#L177))
announces "3 of 7 roles shown" whenever the result count changes, so a filter
that empties the table is not silent.

## 8. Only the moves the lifecycle allows

**📍 Open:** [`web/src/components/roles/roles-view.tsx:432`](../../web/src/components/roles/roles-view.tsx#L432)

The menu offers "Open" only on a draft and "Close" only on an open role, the
same `draft → open → closed` map the server enforces (module 4.2). Showing a
disabled "Open" on a closed role would invite the question "why not?"; not
offering it answers it.

Close asks for confirmation first, because the server treats it as final.
Confirmation and optimism are not in conflict: the dialog guards against a
mis-click, and once confirmed, the badge changes immediately.

## 9. Table to cards at 375px

**📍 Open:** [`web/src/components/roles/roles-view.tsx:337`](../../web/src/components/roles/roles-view.tsx#L337)

A seven-column table at 375px either shrinks text past legibility or scrolls
sideways, hiding the status and actions off-screen. Below 768px the table is
hidden ([line 286](../../web/src/components/roles/roles-view.tsx#L286)) and
each role becomes a card: title and actions on top, status and date, then the
two figures. The figures are a `<dl>` ([line 363](../../web/src/components/roles/roles-view.tsx#L363)),
so each number is announced with its label; in the table the column header
does that job, and a card has no header.

Both layouts render, and CSS shows one. Choosing in JavaScript would need the
viewport width during render, which the server does not have, so the first
paint would show the wrong layout on phones.

## 10. Skeletons shaped like the table

**📍 Open:** [`web/src/components/roles/roles-view.tsx:503`](../../web/src/components/roles/roles-view.tsx#L503)

A centred spinner says "wait" and nothing else, and when the table arrives
everything below it jumps. The skeleton reproduces the table's frame: the
filter bar, the header row, and six rows with a two-line title block, a pill
where the badge goes, and right-aligned bars where the numbers go, at the
real row height. The page keeps its shape from first paint to data, and the
admin already knows where to look. On phones the skeleton is card-shaped.

## 11. An empty state that teaches

**📍 Open:** [`web/src/components/roles/roles-view.tsx:471`](../../web/src/components/roles/roles-view.tsx#L471)

A new admin's first screen is this one, empty. "No roles yet" reports an
absence and leaves them to work out the product. The empty state explains
what a role is here (a private draft with a description, a form and a
rubric), shows the three steps that follow (draft, open, review by score),
and offers the one action that starts them.

A *filtered* empty table is a different situation and gets a different
message: "No roles match", what was searched for, and a "Clear filters"
button. Showing the first-run explainer there would tell an admin with forty
roles that they have none.

## 12. Verification

| Check | Result |
|---|---|
| `npx tsc --noEmit`, `npm run lint` | clean |
| Browser, 1280px | seven roles across three statuses; badges distinct in shape; numbers right-aligned in Plex Mono |
| Open a draft with no questions, request delayed 2s | status read "Draft" before, "Open" 300ms after the click, "Draft" after the 409; error toast listed both missing pieces |
| Filter Draft | three drafts only; count chips 7 / 3 / 3 / 1 |
| Sort Applications | 500, 500, 1 descending; `aria-sort="descending"` on that header only; second click ascending |
| Search "zzz" | "No roles match" with Clear filters; live region "0 of 7 roles shown" |
| Browser, 375px | cards; `scrollWidth` equals `clientWidth` (no sideways scroll) |
| A successful open and a confirmed close | verified in 11.6, once a draft has questions and a rubric to open with |
| `make fmt && make lint && make test && go build ./...` | see the commit body |
