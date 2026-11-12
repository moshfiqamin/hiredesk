# Module 14, part 3 — Responsive, accessible, and locked with a test

The final frontend pass: every page at 375, 768 and 1280px; WCAG 2.2 AA in
both themes, checked automatically and by hand; and one Playwright test that
walks the whole product. Section 9 is a defect this pass found in the rubric
builder, handed over with its fix.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-every-page-i-changed-for-small-screens) | **Every page I changed for small screens** | [`admin-shell.tsx:121`](../../web/src/components/layout/admin-shell.tsx#L121) |
| [2](#2-the-automated-audit) | The automated audit | [`a11y-responsive.spec.ts:135`](../../web/e2e/audit/a11y-responsive.spec.ts#L135) |
| [3](#3-contrast-the-actual-ratios) | **Contrast: the actual ratios** | [`contrast-report.mjs:49`](../../web/scripts/contrast-report.mjs#L49) |
| [4](#4-what-automated-tools-cannot-catch-and-what-i-checked-by-hand) | **What automated tools cannot catch, and what I checked by hand** | — |
| [5](#5-charts-and-their-text-alternatives) | Charts and their text alternatives | — |
| [6](#6-the-end-to-end-test) | The end-to-end test | [`full-flow.spec.ts:33`](../../web/e2e/full-flow.spec.ts#L33) |
| [7](#7-why-role-and-label-selectors) | **Why role and label selectors** | [`full-flow.spec.ts:13`](../../web/e2e/full-flow.spec.ts#L13) |
| [8](#8-keeping-one-e2e-test-valuable-not-slow-and-flaky) | **Keeping one e2e test valuable, not slow and flaky** | [`playwright.config.ts:8`](../../web/playwright.config.ts#L8) |
| [9](#9-handed-over-the-rubric-builder-loses-a-criterion-when-its-kind-changes) | **Handed over: the rubric builder loses a criterion when its kind changes** | [`criterion-card.tsx:52`](../../web/src/components/rubric/criterion-card.tsx#L52) |
| [10](#10-verification) | Verification | — |

---

## 1. Every page I changed for small screens

**📍 Open:** [`web/src/components/layout/admin-shell.tsx:121`](../../web/src/components/layout/admin-shell.tsx#L121)

| Where | Change | Why |
|---|---|---|
| Every admin page | Sidebar becomes a top bar plus a drawer below `md` ([`sheet.tsx:26`](../../web/src/components/ui/sheet.tsx#L26)) | The sidebar was a cramped row of links at 375px |
| Every public, candidate and interviewer page | New header ([`site-shell.tsx:34`](../../web/src/components/layout/site-shell.tsx#L34)); links in a drawer below `md` | — |
| Every dialog and confirmation | Bottom sheet below `sm`, centred card above ([`dialog.tsx:56`](../../web/src/components/ui/dialog.tsx#L56)) | A full-width sheet at the bottom is where a thumb reaches, and scrolls inside itself when tall |
| `/admin/roles` | Table becomes cards below `xl`, not `md` ([`roles-view.tsx:286`](../../web/src/components/roles/roles-view.tsx#L286)) | **The audit found sideways scrolling at 768px**: beside the 14rem sidebar, the main column is narrower than the table until about 1280px |
| `/admin/applications` | New card list below `xl` ([`applications-view.tsx:274`](../../web/src/components/applications/applications-view.tsx#L274)) | The table scrolled sideways inside its box at every width under 1280px |
| `/roles/[id]` | `<dl>` restructured | axe: `dt`/`dd` nested inside an extra `div` |
| Candidate page tabs | Inactive label colour ([`tabs.tsx:60`](../../web/src/components/ui/tabs.tsx#L60)) | 4.06:1, under AA |
| Admin sidebar | Email in `text-secondary` | 4.4:1 on the sidebar surface, under AA |
| `/my-applications` | Current tracker step solid, with "Now" | It read as "not reached" |
| Review form | Mark radios laid over their buttons | Whole 40px square clickable |

Drawers and sheets are Base UI's Dialog, so they trap focus, close on
Escape and return focus to the button that opened them.

## 2. The automated audit

**📍 Open:** [`web/e2e/audit/a11y-responsive.spec.ts:135`](../../web/e2e/audit/a11y-responsive.spec.ts#L135)

`make a11y` runs 17 pages × 3 widths × 2 themes = **102 page checks**, each
as the right kind of user (anonymous, candidate, interviewer, admin), with
real IDs read from the running stack. For each it asserts:

- **axe-core** with the WCAG 2.0, 2.1 and 2.2 A and AA rule sets: zero
  violations;
- **no horizontal scroll**: `scrollWidth <= innerWidth`;
- **exactly one `h1`**;
- **a visible focus indicator** on everything Tab reaches (at 1280px,
  light). It presses Tab up to 60 times and fails any element that takes
  focus with no outline, no ring and no ring on a wrapper within three
  levels ([line 99](../../web/e2e/audit/a11y-responsive.spec.ts#L99)).

The first run found the issues in section 1. The final run: **102 checks,
0 violations, 0 sideways scroll, one h1 on every page, 0 elements focused
without an indicator.** The findings are written to
`web/test-results/a11y-responsive.json`.

It is one test that loops, not 102 tests, on purpose: the API allows ten
auth requests a minute per address, and a test per combination re-ran the
sign-in setup every time a worker restarted after a failure.

## 3. Contrast: the actual ratios

**📍 Open:** [`web/scripts/contrast-report.mjs:49`](../../web/scripts/contrast-report.mjs#L49)

`npm run contrast` reads `globals.css` and computes the WCAG ratio of every
pairing the tokens are used in. It exits non-zero on any failure, so it can
gate a build. Body text needs 4.5:1; borders and focus rings need 3:1
(WCAG 1.4.3 and 1.4.11).

| Pair | Light | Dark |
|---|---|---|
| foreground on background | 14.10 | 14.59 |
| foreground on surface (cards) | 15.13 | 13.63 |
| text-secondary on background | 6.78 | 8.52 |
| text-secondary on surface | 7.28 | 7.96 |
| text-secondary on sidebar | 6.00 | 8.75 |
| muted-foreground on background | 4.98 | 6.34 |
| muted-foreground on surface | 5.35 | 5.92 |
| muted-foreground on surface-sunken | 4.71 | 6.51 |
| primary-foreground on primary (buttons) | 5.92 | 8.05 |
| primary-ink on primary-soft (chips, highlights) | 7.16 | 8.41 |
| primary-ink on surface | 8.64 | 9.97 |
| band-strong on its background | 5.76 | 7.47 |
| band-adequate on its background | 5.46 | 8.45 |
| band-weak on its background | 5.29 | 7.28 |
| band-weak / destructive on surface | 6.20 | 7.79 |
| input border on surface (non-text, 3:1) | 3.18 | 3.45 |
| focus ring on background (non-text, 3:1) | 5.32 | 8.14 |

All pass. One pairing does not, and is now unused: `muted-foreground` on
the light sidebar is **4.40:1**. That was the sidebar email, now
`text-secondary` (6.00:1).

## 4. What automated tools cannot catch, and what I checked by hand

axe checks what a machine can decide from the DOM: a colour pair's ratio,
whether an input has *a* name, whether ARIA attributes are valid, whether
headings exist. Studies of real audits put that at roughly a third of WCAG
failures. It cannot judge **meaning, order or behaviour**: whether a name is
*the right* name, whether focus goes somewhere sensible after an action,
whether a live region speaks at the right moment or far too often, whether
the reading order makes sense, or whether a keyboard user can get out of
something. A page can score zero violations and be unusable.

So these were checked by hand, in the browser:

- **Labels resolve to the right text.** The browser's tree summary showed
  the CV field as "https://" (its placeholder) and the radios as
  "true"/"false" (their values). I checked `input.labels` directly: "Link
  to your CV", "Yes", "No". The `*` is `aria-hidden`, so the accessible name
  has no stray asterisk.
- **Drawer:** focus moves into it on open; Escape closes it; focus returns
  to "Open menu".
- **Assignment dialog at 375px:** opens as a bottom sheet; Tab cycles
  inside it; the focused button shows a 3px ring and a border. I measured
  the computed `box-shadow` rather than eyeballing it.
- **Mark scale:** one group of ten, so arrow keys move 1 → 10; each radio
  announces "8, Lean yes, without reservation", not just "8".
- **Live regions say things once:** the draft announces on the first save
  and on a failure, not on every pause in typing; the character counters
  are described by the field, not live; toasts (Sonner) use `aria-live`.
- **Errors are tied to fields** with `aria-describedby`, and a server 422
  moves focus to the first field it names.
- **Tab order** on the application form follows the visual order: CV link,
  then each question in turn, then Send.
- **Headings:** one `h1` per page (asserted by the audit), then section
  `h2`s.

**Not done**, and worth saying: no session with a real screen reader
(VoiceOver or NVDA), no check at 200% and 400% zoom (375px is close to,
but not, WCAG's 320px reflow width), and no Windows high-contrast mode.

## 5. Charts and their text alternatives

Every visual encoding here repeats information that is also in text:

- **Score breakdown bars:** `aria-hidden`; each row says "24 of 30" in text
  next to its bar (13.1).
- **Ranking mini bars:** `aria-hidden`, followed by screen-reader text
  listing every criterion's points.
- **Interviewer load bars:** `aria-hidden`; "4 open, 1 awaiting review" in
  text, and "heavier than most" in words.
- **Rubric weight bar:** paired with the "Weight by criterion" list.
- **Pipeline tracker:** each step says "done", "current step" or "to come"
  in text; the current step carries `aria-current="step"`.

## 6. The end-to-end test

**📍 Open:** [`web/e2e/full-flow.spec.ts:33`](../../web/e2e/full-flow.spec.ts#L33)

`make e2e` runs four steps, one per persona hand-off:

1. Admin signs in through the form, creates a role, adds two questions,
   builds a two-criterion rubric, opens the role.
2. A new candidate finds it on the public board, registers from the role
   page, lands on the form, applies, and sees the tracker at "Received".
3. Admin opens the Ranking tab, follows the candidate, sees **80.0%** and
   the engine's detail strings, then assigns Ivan through the dialog.
4. Ivan signs in, follows "Review E2E Candidate …" from the worklist,
   chooses "8, Lean yes, without reservation", submits, and sees his own
   review with Edit; the admin reloads and sees "8.0".

Last run: **4 passed in 8.9s.**

## 7. Why role and label selectors

**📍 Open:** [`web/e2e/full-flow.spec.ts:13`](../../web/e2e/full-flow.spec.ts#L13)

A selector is a claim about what stays the same.
`.h-8.w-full.rounded-lg` claims the styling will not change, and the
styling is exactly what changes most. `getByRole("button", { name: "Send
application" })` claims the page has a button called "Send application",
which is a property of the product: if it stops being true, a user would
notice too.

Two more benefits:

- **The test checks accessibility on the side.** If a field cannot be found
  by its label, a screen-reader user cannot find it either. The test
  failing for that reason is the test working.
- **It reads as the flow.** "Click Create account, fill Full name" is the
  sentence a person would write.

The limits showed up while writing it. This Next.js version keeps the
previous page in the DOM, hidden, so `getByLabel("Title")` also matched the
roles list's "Search by title" box: labels are matched `exact: true`. A
label containing a visual `*` is matched through its accessible name with
`getByRole`, which ignores `aria-hidden` text. And "Keywords" named both an
input and a list, so the input is `getByRole("textbox", …)`. Each of these
is still a role or a label, never a class.

## 8. Keeping one e2e test valuable, not slow and flaky

**📍 Open:** [`web/playwright.config.ts:8`](../../web/playwright.config.ts#L8)

- **One path, the one that matters.** It checks that the pieces connect
  (a role a candidate can find, an application that gets scored, a review
  the admin can see), not every edge case. Edge cases live in the Go unit
  and integration tests, which are milliseconds and deterministic.
- **No sleeps.** Every wait is an assertion that retries until true
  (`toBeVisible`, `toHaveURL`), so the test waits as long as the app needs
  and no longer.
- **Fresh data per run.** The role title and candidate email carry a
  timestamp, so runs never collide and the test needs no database reset.
- **Separate contexts per person**
  ([`full-flow.spec.ts:33`](../../web/e2e/full-flow.spec.ts#L33)), so three
  people are signed in at once and nobody signs out. That also keeps the run
  at four auth requests, under the API's ten a minute.
- **The real build.** It runs against the Docker containers `make up`
  starts, not a dev server, so what passes is what ships.
- **A failure explains itself.** A trace (DOM, network, console) is kept on
  failure. That trace found the rubric defect below in minutes: the console
  error and the action before it.
- **Serial and honest about it.** The steps depend on each other, so they
  run in order; a failure skips what can no longer be true instead of
  producing four failures.

## 9. Handed over: the rubric builder loses a criterion when its kind changes

**📍 Open:** [`web/src/components/rubric/criterion-card.tsx:52`](../../web/src/components/rubric/criterion-card.tsx#L52)

Found by the end-to-end test. This is module 11.6 code, so it is recorded here as a known defect rather
than changed in this module.

**What happens.** Add a second criterion, type its name and weight, then
choose "Experience in years". The name and weight still show in the inputs,
but the form state has lost them: Save shows "Name the criterion" and "A
number like 20…" for a criterion whose fields look full. In other
sequences, such as changing a kind, filling Minimum and Target, then adding
another criterion, the card's config becomes `undefined` and the page
crashes with `Cannot read properties of undefined (reading 'min')` from
`example()`.

**Fix, part 1 (verified):** in `criterion-card.tsx`, replace the line
`const example = …`
([line 48](../../web/src/components/rubric/criterion-card.tsx#L48)) with:

```tsx
  // During a kind change or a reset, the watched kind and config can arrive
  // in separate renders. Until the config has this kind's shape, skip the
  // worked example rather than read fields that are not there.
  const shapeFits = config != null && Object.keys(def.empty()).every((key) => key in config);
  const example = hundredths !== null && hundredths > 0 && shapeFits ? def.example(config, hundredths / 100) : null;
```

This is a guard: it renders "no example" for the one render where kind and
config disagree. I applied it temporarily, rebuilt, and the crash stopped;
then I reverted it.

**Fix, part 2 (the cause, not verified):** the kind radio is wired with
`register(..., { onChange })`, and `changeKind` then calls `setValue` on
`config` and `question_id` separately. That is three writes to one
field-array item, each notifying watchers on its own. Make the change one
write instead: drop `register` on the radios, make them controlled
(`checked={kind === k}`), and in `changeKind` set the whole item at once:

```tsx
    const item = getValues(`criteria.${index}`);
    const keepQuestion = nextDef.needsQuestion && current && nextDef.questionKinds.includes(current.kind);
    setValue(
      `criteria.${index}`,
      { ...item, kind: next, config: nextDef.empty(), question_id: keepQuestion ? item.question_id : "" },
      { shouldDirty: true },
    );
```

(`current` is computed as it is today, before this.) Confirm by changing
the second criterion's kind in the browser and saving.

**Then lock it:** in `e2e/full-flow.spec.ts`, make the second criterion
"Experience in years" on question 2, as the comment there
([line 90](../../web/e2e/full-flow.spec.ts#L90)) says, and update the
expected score.

## 10. Verification

| Command | Result |
|---|---|
| `make a11y` (contrast + audit) | Contrast: every pair passes in both themes. Audit: 102 page checks, 0 axe violations, 0 sideways scroll, one h1 each, 0 focus gaps |
| `make e2e` | 4 passed (8.9s) |
| `npx tsc --noEmit`, `npm run lint`, `npm run build` | clean; every route built |
| Browser, 375px | Drawer (focus in, Escape, focus back); assignment dialog as a bottom sheet with focus trapped and a visible ring |
