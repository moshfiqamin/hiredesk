# Module 11, part 5: The question builder

A role's application form, built visually: add, edit, remove and reorder
questions, kind-specific fields that appear and clear with the kind, a live
preview of the candidate's view, and an explained lock once the role opens.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | Reordering: buttons first, dragging as a shortcut | [`web/src/components/questions/question-builder.tsx:158`](../../web/src/components/questions/question-builder.tsx#L158) |
| 2 | What the keyboard path needs: focus and announcements | [`web/src/components/questions/question-builder.tsx:188`](../../web/src/components/questions/question-builder.tsx#L188) |
| 3 | The ARIA the pattern uses | [`web/src/components/questions/question-card.tsx:80`](../../web/src/components/questions/question-card.tsx#L80) |
| 4 | One shape for every kind | [`web/src/lib/questions/schema.ts:30`](../../web/src/lib/questions/schema.ts#L30) |
| 5 | Clearing fields on a kind change | [`web/src/components/questions/question-card.tsx:127`](../../web/src/components/questions/question-card.tsx#L127) |
| 6 | What bad data the clearing keeps from the server | [`web/src/lib/questions/schema.ts:135`](../../web/src/lib/questions/schema.ts#L135) |
| 7 | `useFieldArray`, nested, and the `id` clash | [`web/src/components/questions/question-builder.tsx:147`](../../web/src/components/questions/question-builder.tsx#L147) |
| 8 | Saving a form with no "replace all" endpoint | [`web/src/lib/questions/sync.ts:38`](../../web/src/lib/questions/sync.ts#L38) |
| 9 | The live preview, made `inert` | [`web/src/components/questions/form-preview.tsx:16`](../../web/src/components/questions/form-preview.tsx#L16) |
| 10 | An explained lock instead of a disabled form | [`web/src/components/questions/question-builder.tsx:82`](../../web/src/components/questions/question-builder.tsx#L82) |
| 11 | Reading a ref during render | [`web/src/components/questions/question-builder.tsx:259`](../../web/src/components/questions/question-builder.tsx#L259) |
| 12 | Verification | — |

---

## 1. Reordering: buttons first, dragging as a shortcut

**📍 Open:** [`web/src/components/questions/question-builder.tsx:158`](../../web/src/components/questions/question-builder.tsx#L158)

The brief allowed drag and drop only if it could be made keyboard accessible.
The first version used dnd-kit with its `KeyboardSensor`: focus the handle,
Space to pick up, arrows to move, Space to drop, with live announcements. It
was then tested with real key presses, and it did not hold up:

- With three questions on a normal-height page, Space picked a question up
  and the announcement fired, but **ArrowUp did nothing**. Logging the
  coordinate getter showed it computing the right target. Reading
  `KeyboardSensor.handleKeyDown` showed why: when the target lies in the upper
  half of the viewport and the page can scroll, the sensor **scrolls the page
  and returns** instead of moving the item, expecting the scroll to update
  the drop target. In this layout it did not.
- With `scrollBehavior: "auto"`, two presses moved the question one place and
  the page jumped from 68px to 585px scrolled.

A keyboard user cannot see why a key press was swallowed, and a screen-reader
user cannot see the page jump at all. Behaviour that depends on where the list
sits in the viewport is not accessible, however good the announcements are.

So the decision, as the brief said, went to accessibility:

| Way to reorder | Who it serves | Status |
|---|---|---|
| **Move up / Move down buttons** on every card | keyboard, screen reader, voice control ("click move up"), switch access, mouse, touch | the primary mechanism |
| dragging the grip handle | mouse and touch users who prefer it | a pointer-only shortcut (`PointerSensor` only) |

Keeping the drag is allowed because the buttons exist: WCAG 2.5.7 (Dragging
Movements) asks that anything done by dragging can also be done with a
single pointer without dragging. The handle is `aria-hidden` and not
focusable ([`question-card.tsx:62`](../../web/src/components/questions/question-card.tsx#L62)):
offering keyboard users a second, unreliable way to do what the buttons do
reliably would only add a trap.

## 2. What the keyboard path needs: focus and announcements

**📍 Open:** [`web/src/components/questions/question-builder.tsx:188`](../../web/src/components/questions/question-builder.tsx#L188)

Buttons are accessible by default, but reordering adds two problems a plain
button does not have.

**Where focus goes.** Cards are keyed by `useFieldArray`'s stable row ID, so
when a card moves React moves its DOM node, and the pressed button keeps
focus as it travels. Pressing Enter three times walks a question from fourth
to first. At the top or bottom, though, the pressed button becomes disabled,
and a disabled element drops focus to the page. `moveBy` detects the edge
([line 197](../../web/src/components/questions/question-builder.tsx#L197))
and moves focus to the same card's other move button. Removing a question
moves focus to "Add question" ([line 211](../../web/src/components/questions/question-builder.tsx#L211))
for the same reason: the focused button has just ceased to exist.

**What changed.** A sighted user sees the card move; a screen-reader user
hears nothing unless told. Each move and removal writes a sentence to a
visually hidden `role="status"` region ([line 300](../../web/src/components/questions/question-builder.tsx#L300)):
"Moved “Where would you work from?” to position 1 of 4." Pointer drags get
the same through dnd-kit's announcements ([line 172](../../web/src/components/questions/question-builder.tsx#L172)).

## 3. The ARIA the pattern uses

**📍 Open:** [`web/src/components/questions/question-card.tsx:80`](../../web/src/components/questions/question-card.tsx#L80)

| Element | ARIA | Why |
|---|---|---|
| the list | `<ol aria-label="Questions">` | an ordered list announces "list, 4 items" and each item's position, which is the information reordering changes |
| move buttons | `aria-label="Move “Why…?” up"` | an icon-only button needs a name; including the prompt says *which* question, since every card has the same two arrows |
| edge buttons | `disabled` | "dimmed" is announced; moving past the end is not offered |
| announcement | `role="status"` (implies `aria-live="polite"`) | spoken after the current utterance, without stealing focus |
| drag handle | `aria-hidden="true"`, no `tabIndex` | pointer-only, so kept out of the accessibility tree and tab order |
| each field | `aria-invalid`, `aria-describedby` to its error | the problem is read when the field takes focus |

Had keyboard dragging been kept, the handle would have needed what dnd-kit's
`attributes` supply: `role="button"`, `tabIndex={0}`,
`aria-roledescription="sortable"`, and `aria-describedby` pointing at
instructions ("press Space to pick up…"), plus `aria-pressed` while lifted.
That is the standard pattern; it was the sensor's behaviour, not the ARIA,
that ruled it out.

## 4. One shape for every kind

**📍 Open:** [`web/src/lib/questions/schema.ts:30`](../../web/src/lib/questions/schema.ts#L30)

Every question in the builder carries every field (`min_length` and
`choices` included) whatever its kind. A discriminated union, one object
shape per kind, would describe the API more exactly, but in a form it means
the shape of a row changes when the kind select changes, which react-hook-form
handles poorly (registered inputs disappearing mid-edit). With one shape,
changing kind is a value change, and the kind decides what must be filled
and what must be empty.

That decision lives in `superRefine` ([line 48](../../web/src/lib/questions/schema.ts#L48)),
`refine`'s richer form: it receives a context and can report several issues,
each at its own path. Text needs a whole-number minimum length; choice needs
one to fifty options, unique case-insensitively ([line 70](../../web/src/lib/questions/schema.ts#L70),
as the server compares them); and every other kind must have neither
(lines [57](../../web/src/lib/questions/schema.ts#L57) and
[77](../../web/src/lib/questions/schema.ts#L77)). The types, as before, are
derived with `z.infer`.

## 5. Clearing fields on a kind change

**📍 Open:** [`web/src/components/questions/question-card.tsx:127`](../../web/src/components/questions/question-card.tsx#L127)

The kind select's `onChange` resets the kind-specific fields with
`fieldsForKind` ([`schema.ts:113`](../../web/src/lib/questions/schema.ts#L113)):
a minimum length of 0 for text, no options for anything. The fields
themselves render only for their kind
([line 159](../../web/src/components/questions/question-card.tsx#L159)), so
the screen and the data agree.

Hiding a field is not clearing it. Without the reset, switching a text
question with "at least 80 characters" to Choice would hide the input but
keep `"80"` in the form state, invisible and still submitted. Switching back
would resurrect it. Verified both ways: text (80) → choice → text comes back
at 0, and a choice with two options → yes/no → choice comes back empty.

## 6. What bad data the clearing keeps from the server

**📍 Open:** [`web/src/lib/questions/schema.ts:135`](../../web/src/lib/questions/schema.ts#L135)

Three kinds of bad data, each confirmed against the running API:

1. **Options on a non-choice question.** `PATCH {"kind":"text"}` on a choice
   question answered **422** `choices: must be absent unless kind is
   choice`. A PATCH is applied on top of the stored row (module 4.1), so the
   old options were still there.
2. **A minimum length on a number.** `PATCH {"kind":"number","min_length":80}`
   answered **422** `min_length: applies only to text questions`.
3. What the server would *accept* if it were less strict: an answer to a
   number question checked against a leftover 80-character minimum, or a
   scoring criterion reading options that the candidate never saw.

The server refuses the first two, so without clearing the admin would hit a
422 they could not understand, about fields they could no longer see.
`patchBody` therefore sends every kind-specific field on every PATCH,
cleared where it does not apply: `choices: []` and `min_length: 0`. The
domain treats an empty list as absent (module 4.3), which is how a choice
question becomes another kind. `createBody` ([line 118](../../web/src/lib/questions/schema.ts#L118))
can simply leave them out, since there is no stored row underneath a POST.

## 7. `useFieldArray`, nested, and the `id` clash

**📍 Open:** [`web/src/components/questions/question-builder.tsx:147`](../../web/src/components/questions/question-builder.tsx#L147)

`useFieldArray` manages a list inside a form: `append`, `remove` and `move`
keep each row's values, errors and dirty state attached to the right row,
and each row gets a stable `id` used as the React `key` and the sortable ID.
Using the array index as a key would make React reuse the first card's DOM
node for whatever is now first, so a typed-in value or focus would appear to
jump between questions.

That generated `id` is why the server's question ID is called `questionId`
([`schema.ts:34`](../../web/src/lib/questions/schema.ts#L34)): a field named
`id` would be overwritten by the library's own.

The options of a choice question are a second field array nested inside the
first ([`question-card.tsx:191`](../../web/src/components/questions/question-card.tsx#L191)).
Each option is an object `{ value }`, not a bare string, because the library
needs an object to attach its key to. An issue about the whole list ("add at
least one option") arrives under `.root`; one about a single option under its
index ([line 195](../../web/src/components/questions/question-card.tsx#L195)).

`FormProvider` ([line 253](../../web/src/components/questions/question-builder.tsx#L253))
puts the form in context, so each card calls `useFormContext()` instead of
receiving `register`, `control` and `errors` as props through every level.

## 8. Saving a form with no "replace all" endpoint

**📍 Open:** [`web/src/lib/questions/sync.ts:38`](../../web/src/lib/questions/sync.ts#L38)

The API offers one question at a time: POST, PATCH, DELETE, and a PUT for
the order. `saveQuestions` turns the builder's state into the fewest of
those: delete what was removed, create what is new, patch only what differs
from the server (`sameQuestion`), and send one reorder at the end if the
order still differs ([line 81](../../web/src/lib/questions/sync.ts#L81)).
New questions are appended by the server, so a form built top to bottom
needs no reorder at all.

Several requests are not a transaction: one can fail halfway. Two things keep
that safe. The baseline (what the server holds) is updated after every
request that succeeds, and a created question's new ID is written back into
the builder at once ([line 66](../../web/src/lib/questions/sync.ts#L66)). So
pressing Save again after a failure resumes: it will not delete twice or
create the same question a second time. A 422 is mapped onto the fields of
the question it was for, and a 409 `form_locked` (someone opened the role in
another tab) shows a toast and refreshes into the locked view.

## 9. The live preview, made `inert`

**📍 Open:** [`web/src/components/questions/form-preview.tsx:16`](../../web/src/components/questions/form-preview.tsx#L16)

The preview reads the builder's values with `useWatch` and renders each
question the way a candidate's form will: a text box with "At least 80
characters", Yes/No radios, the options of a choice, an asterisk for
required and "(optional)" otherwise.

It is drawn to look interactive, and must not *be* interactive. The HTML
`inert` attribute removes everything inside from the tab order and the
accessibility tree, so a keyboard user does not tab through a second,
non-functional copy of the form, and a screen reader does not read it as
controls. The heading above it stays outside `inert`, so the panel is still
findable and named.

## 10. An explained lock instead of a disabled form

**📍 Open:** [`web/src/components/questions/question-builder.tsx:82`](../../web/src/components/questions/question-builder.tsx#L82)

The server refuses every question write once a role leaves draft (module
4.3), because candidates have answered this exact form and changing it would
make earlier answers mean something different from later ones.

The cheap way to show that is to render the editor with every control
`disabled`. The admin then sees a form that does not respond, and has to
guess: a bug? missing permission? a loading state? Disabled controls are
also skipped by Tab and announced only as "dimmed", so a screen-reader user
may never learn the form is there at all.

The locked view instead removes the editor (zero inputs on the page, checked)
and shows the questions as text, with a `role="note"` panel that says three
things: **what** ("This form is locked"), **why** (the role is open, and
candidates have been answering these questions, so changes would make scores
compare unfairly), and **what to do instead** (create a new role, linked).
A reason turns "it's broken" into "it's protecting something", and the next
step turns a dead end into a path. The preview stays, because seeing the
candidate's view is still useful.

## 11. Reading a ref during render

**📍 Open:** [`web/src/components/questions/question-builder.tsx:259`](../../web/src/components/questions/question-builder.tsx#L259)

The linter (React Compiler rules) rejected `onSubmit={handleSubmit(onSubmit)}`:
`onSubmit` reads the baseline ref, and calling `handleSubmit` during render
passes it to a function that *could* read it then. A ref is not state; a
render that read it would not re-run when it changed. Wrapping the call in
the event handler (`(event) => handleSubmit(onSubmit)(event)`) makes it run
only on submit, which is when the ref is meant to be read.

## 12. Verification

| Check | Result |
|---|---|
| `npx tsc --noEmit`, `npm run lint` | clean |
| dnd-kit `KeyboardSensor`, real key presses | Space picked up; ArrowUp did not move (getter returned the right target; sensor scrolled instead); with `scrollBehavior: "auto"` the page jumped 68 → 585px. Keyboard sensor removed |
| Move up, Enter ×3 on question 4 | order became 4, 1, 2, 3; focus moved to that card's "Move down" at the top; live region "Moved “Where would you work from?” to position 1 of 4." |
| Text (min 80) → choice → text | minimum length back to 0, no options |
| Choice with options → yes/no → choice | options empty |
| Options "Remote" and "remote" | "Already in the list" |
| Save four new questions | four POSTs, each with only its kind's fields; no reorder request |
| Edit: choice → text, remove one, move last to top | `DELETE` once, `PATCH` with `"min_length":0,"choices":[]`, one `PUT /order`; server's form matched the builder |
| `curl PATCH {"kind":"text"}` on a choice question | 422 `choices: must be absent unless kind is choice` |
| `curl PATCH {"kind":"number","min_length":80}` | 422 `min_length: applies only to text questions` |
| Open role's Questions tab | lock note with reason and link; 0 inputs on the page; preview shown |
| `make fmt && make lint && make test && go build ./...` | see the commit body |
