# Module 13, part 1 — The score breakdown

The screen that answers "why did this candidate get 66%?". One prominent
total, then one bar per criterion, heaviest first, each bar reading as
"24 of 30" and never just "24". It is the page the README screenshot shows,
so it has to be right before it is pretty.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-recharts-or-plain-css) | **Recharts or plain CSS** — the case for not adding the library | [`score-breakdown.tsx:30`](../../web/src/components/score/score-breakdown.tsx#L30) |
| [2](#2-why-a-bar-chart-and-not-radar-or-donut) | **Why bars, and why not radar or donut** | — |
| [3](#3-one-axis-starting-at-zero) | **One axis, starting at zero** | [`score-breakdown.tsx:40`](../../web/src/components/score/score-breakdown.tsx#L40) |
| [4](#4-readable-without-a-legend) | **Readable without a legend** | [`score-breakdown.tsx:47`](../../web/src/components/score/score-breakdown.tsx#L47) |
| [5](#5-colour-by-band-never-colour-alone) | Colour by band, never colour alone | [`score-breakdown.tsx:13`](../../web/src/components/score/score-breakdown.tsx#L13) |
| [6](#6-sorted-by-weight) | Sorted by weight | [`score-breakdown.tsx:39`](../../web/src/components/score/score-breakdown.tsx#L39) |
| [7](#7-motion-one-entry-transition) | Motion: one entry transition | [`score-breakdown.tsx:101`](../../web/src/components/score/score-breakdown.tsx#L101) |
| [8](#8-verification) | Verification | — |

---

## 1. Recharts or plain CSS

**📍 Open:** [`web/src/components/score/score-breakdown.tsx:30`](../../web/src/components/score/score-breakdown.tsx#L30)

The brief said Recharts, unless a plain CSS bar would genuinely be better. It
would, for four reasons that are about this shape of data, not about taste:

1. **The evidence lives between the bars.** Part 2 puts the question, the
   highlighted excerpt and a link under each bar. A Recharts `BarChart` is a
   single `<svg>`; there is nowhere inside it to put a paragraph of HTML
   text. You would end up drawing the chart, then absolutely positioning HTML
   over it and keeping the two aligned by hand.
2. **SVG text does not wrap.** At 375px a criterion called "Experience with
   distributed systems" has to wrap onto two lines. In HTML that is free; in
   SVG every label is a `<text>` element that runs off the edge unless you
   measure and split it yourself.
3. **Accessibility for nothing.** An ordered list (`<ol>`) whose items
   contain the criterion name and "24 of 30" as real text is read correctly
   by every screen reader with no extra work. A chart needs a hidden table
   or `aria-label`s on each shape to say the same thing.
4. **Weight.** Recharts pulls in a good chunk of JavaScript (it depends on
   several d3 modules) to draw what is, here, four `<span>`s with a width.

What Recharts would give back — axes with ticks, tooltips, a legend — are
all things this design deliberately does without (sections 3 and 4). The
rejected alternative was therefore not "Recharts vs CSS" but "a chart
library working against the layout vs HTML working with it". Had the screen
been a time series, or hundreds of points, the answer would flip.

## 2. Why a bar chart, and not radar or donut

A bar chart encodes a number as **length along a common baseline**. That is
the channel people read most accurately: in perception studies (Cleveland
and McGill's ranking of encodings), position and length on a shared scale
beat angle, area and colour by a wide margin. Comparing magnitudes — "is
Experience doing better than Depth?" — is exactly what the admin is here to
do.

**Donut (and pie)** encodes numbers as angle and arc area. People misjudge
both, especially for slices that are not adjacent. Worse, a donut means
"parts of a whole": the slices add up to 100%. A criterion's *earned* points
are not a share of anything the reader cares about; what matters is each
criterion against *its own* maximum. A donut cannot show "24 of 30" for four
criteria at once.

**Radar** puts each criterion on its own spoke and joins the dots into a
polygon. Three problems:

- the area of the polygon, which is what the eye reads, grows with the
  *square* of the values, so small differences look large;
- the shape depends on the **order of the spokes** — reorder the criteria
  and the same candidate looks spiky or round;
- every spoke has its own scale, so two criteria cannot be compared by
  length unless both happen to have the same maximum.

## 3. One axis, starting at zero

**📍 Open:** [`web/src/components/score/score-breakdown.tsx:40`](../../web/src/components/score/score-breakdown.tsx#L40)

Every bar is drawn on **one points axis, from zero to the largest
criterion's available points**:

- the grey track is `available / maxAvailable` wide — so a 30-point
  criterion's track is three times as long as a 10-point one's;
- the coloured fill inside it is `earned / available` of the track.

So "24 of 30" is literally visible: a long track, four-fifths full. And
because the tracks share one scale, the bars also show *weight*, which is
why sorting by weight (section 6) produces a staircase.

**Why zero matters.** A bar's meaning *is* its length. If the axis started
at 20, a score of 24 would draw a bar of length 4 and a score of 21 a bar of
length 1 — four times as long, for a score 14% higher. Truncated axes are
the most common way charts mislead, usually by accident. With bars there is
no valid reason to start anywhere but zero.

The rejected alternative was a 0–100% track per criterion. It is simpler
and looks tidier, but it throws away weight: a 5-point criterion at 100%
would look as important as a 40-point one at 100%.

## 4. Readable without a legend

**📍 Open:** [`web/src/components/score/score-breakdown.tsx:47`](../../web/src/components/score/score-breakdown.tsx#L47)

A legend makes the reader look away from the data, remember a mapping, and
look back. This screen labels values directly instead:

- **the total** is a large number (`text-4xl`, monospaced, `tabular-nums`
  so digits line up), with "93.8 of 100 points" and the band beside it;
- **every row** carries its own criterion name on the left and
  "earned of available" on the right, in the same row as its bar;
- **the band** is spelled out as a word in the total's chip, and repeated in
  the bar's fill *pattern* as well as its colour (section 5);
- **the rubric version** sits in small muted text at the top right
  ([line 57](../../web/src/components/score/score-breakdown.tsx#L57)) —
  findable when someone asks "which rubric was this?", invisible otherwise.

The bar itself is `aria-hidden`: the numbers are the accessible fact, and
the bar only repeats them for the eye.

## 5. Colour by band, never colour alone

**📍 Open:** [`web/src/components/score/score-breakdown.tsx:13`](../../web/src/components/score/score-breakdown.tsx#L13)

The bands (strong ≥ 75%, adequate ≥ 50%, weak below) come from the 11.1
tokens, through the existing `band()` helper in `cells.tsx`, so this screen
and the applications table can never disagree about where "adequate"
starts. The fills differ in **pattern** as well as hue — solid, diagonal
hatching, dashed outline — the same scheme as the score-band chip, so they
survive colour blindness and greyscale printing (WCAG 1.4.1, "Use of
Colour"). And the number is always present beside the bar.

## 6. Sorted by weight

**📍 Open:** [`web/src/components/score/score-breakdown.tsx:39`](../../web/src/components/score/score-breakdown.tsx#L39)

`[...score.breakdown].sort(...)` — the spread copies the array first,
because `Array.prototype.sort` sorts *in place*, and `score` is TanStack
Query's cached object. Mutating it would reorder the cache under every
other component reading it.

Heaviest first, because a reader's attention runs out from the top: the
criteria that decide the total should be read first.

## 7. Motion: one entry transition

**📍 Open:** [`web/src/components/score/score-breakdown.tsx:101`](../../web/src/components/score/score-breakdown.tsx#L101)

The fill slides in a few pixels as the page loads (`animate-in
slide-in-from-left-2`, from `tw-animate-css`). `motion-safe:` applies it
only when the user has not asked their OS for reduced motion. Nothing
happens on hover, and nothing changes size after load, so no layout moves
under the cursor.

A zero renders **no fill element at all**, rather than a zero-width one: the
weak band's dashed border would otherwise still draw a 2px sliver at the
start of the track
([line 99](../../web/src/components/score/score-breakdown.tsx#L99)).

## 8. Verification

| Command / check | Result |
|---|---|
| `npx tsc --noEmit` and `npm run lint` in `web/` | No errors |
| `npm run build` in `web/` | Builds, every route listed |
| Seeded top candidate (93.8%) at 1024px, dark theme | Total, four bars on one axis, 40-point track longest |
| Same page at 375×812, light theme | No horizontal scroll (`scrollWidth` 375 = `innerWidth`), names and numbers wrap |
| Submitted a deliberately weak application (0%) | Empty tracks, no fill sliver after the fix, every row explains its zero |
