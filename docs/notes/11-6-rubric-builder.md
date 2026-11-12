# Module 11, part 6: The rubric builder

The screen that shows the domain best: an admin composes a rubric from four
kinds of criterion, each with its own settings, watches the weights add up
to 100 in a running total and a proportional bar, and saves a new version.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | Four config shapes without a tangle of conditionals | [`web/src/lib/rubric/kinds.tsx:49`](../../web/src/lib/rubric/kinds.tsx#L49) |
| 2 | A mapped type that keeps the registry honest | [`web/src/lib/rubric/kinds.tsx:309`](../../web/src/lib/rubric/kinds.tsx#L309) |
| 3 | The card asks the registry, never the kind | [`web/src/components/rubric/criterion-card.tsx:44`](../../web/src/components/rubric/criterion-card.tsx#L44) |
| 4 | A schema that needs the role's questions | [`web/src/lib/rubric/schema.ts:30`](../../web/src/lib/rubric/schema.ts#L30) |
| 5 | The total is derived, not stored | [`web/src/components/rubric/weight-panel.tsx:27`](../../web/src/components/rubric/weight-panel.tsx#L27) |
| 6 | Summing in hundredths, not floats | [`web/src/lib/rubric/schema.ts:19`](../../web/src/lib/rubric/schema.ts#L19) |
| 7 | Not-yet-100 that informs instead of blocking | [`web/src/components/rubric/weight-panel.tsx:52`](../../web/src/components/rubric/weight-panel.tsx#L52) |
| 8 | The weight distribution: one proportional bar | [`web/src/components/rubric/weight-panel.tsx:89`](../../web/src/components/rubric/weight-panel.tsx#L89) |
| 9 | Explaining each kind where it is chosen | [`web/src/components/rubric/criterion-card.tsx:118`](../../web/src/components/rubric/criterion-card.tsx#L118) |
| 10 | Only this role's questions, only the right kinds | [`web/src/components/rubric/criterion-card.tsx:45`](../../web/src/components/rubric/criterion-card.tsx#L45) |
| 11 | Changing kind, and the stale errors it left | [`web/src/components/rubric/criterion-card.tsx:52`](../../web/src/components/rubric/criterion-card.tsx#L52) |
| 12 | The keyword tag input | [`web/src/components/rubric/keyword-input.tsx:58`](../../web/src/components/rubric/keyword-input.tsx#L58) |
| 13 | Saving a version, and offering a rescore | [`web/src/components/rubric/rubric-builder.tsx:93`](../../web/src/components/rubric/rubric-builder.tsx#L93) |
| 14 | Verification | — |

---

## 1. Four config shapes without a tangle of conditionals

**📍 Open:** [`web/src/lib/rubric/kinds.tsx:49`](../../web/src/lib/rubric/kinds.tsx#L49)

Each criterion kind has its own config:

| Kind | Config | Reads |
|---|---|---|
| `keyword_match` | `{ keywords: string[] }` | a text or choice question |
| `experience_years` | `{ min, target }` | a number question |
| `answer_depth` | `{ min_chars, target_chars }` | a text question |
| `boolean_all` | `{ question_ids: string[] }` | several yes/no questions, no `question_id` |

The straightforward version writes `if (kind === "keyword_match") … else if
…` in the editor, again in validation, again when converting to and from the
API, again for the help text, again for the example. Five places, four
branches each, and a fifth kind means finding all twenty.

Instead, each kind is described once, by a `KindDefinition`: its label and
summary, which question kinds it reads, its empty config, conversions to and
from the API, its checks, a worked example, and its editor component.
`criterionKinds` maps each kind to its definition. Everything else looks the
kind up and uses what it finds. This is the frontend twin of the scoring
engine's registry in Go (module 6.1, "a function type and registry versus a
switch"), and for the same reason: behaviour that varies by kind lives with
the kind.

Adding a fifth kind is one new entry. The compiler then points at nothing
else, because nothing else names a kind.

## 2. A mapped type that keeps the registry honest

**📍 Open:** [`web/src/lib/rubric/kinds.tsx:309`](../../web/src/lib/rubric/kinds.tsx#L309)

```ts
const criterionKinds: { [K in CriterionKind]: KindDefinition<ConfigFor[K]> } = { … }
```

A **mapped type** builds an object type by looping over a union of keys.
Here it says: for every kind `K`, there is an entry, and that entry's
editor, checks, `empty()` and `toApi()` all use `ConfigFor[K]`, that kind's
config. Forget a kind and it does not compile; give `answer_depth` an editor
that reads `keywords` and it does not compile.

The first attempt, `satisfies Record<CriterionKind, KindDefinition<never>>`,
failed to type-check: `KindDefinition<C>` uses `C` both as an input
(`check(config)`) and an output (`empty()`), so no single type argument fits
all four entries. The mapped type gives each entry its own.

A criterion in the form holds a config of *whichever* kind it has right now,
so the code that renders it cannot know `K` statically. `kindOf`
([line 322](../../web/src/lib/rubric/kinds.tsx#L322)) is the one place that
widens a definition to the general config type, rather than a cast at every
use.

## 3. The card asks the registry, never the kind

**📍 Open:** [`web/src/components/rubric/criterion-card.tsx:44`](../../web/src/components/rubric/criterion-card.tsx#L44)

`CriterionCard` contains no branch on kind. It looks up `def = kindOf(kind)`
and then:

- `def.needsQuestion` decides whether the question select appears;
- `def.questionKinds` filters which questions it lists;
- `<def.Editor …/>` ([line 175](../../web/src/components/rubric/criterion-card.tsx#L175))
  renders the kind's own settings (tag input, two number fields, or a
  checkbox list);
- `def.example(config, weight)` writes the worked example.

The config is one form field, managed by a `Controller`: each editor receives
`value` and `onChange` for its whole config object, typed to its own shape.
react-hook-form only ever sees `criteria.N.config`, so its paths do not need
to know that config differs by kind.

## 4. A schema that needs the role's questions

**📍 Open:** [`web/src/lib/rubric/schema.ts:30`](../../web/src/lib/rubric/schema.ts#L30)

Whether a criterion is valid depends on data outside it: the question it
reads must be on *this* role, and of a kind it can score (an experience
criterion cannot read a yes/no answer). So the schema is built by a function
that takes the role's questions, `rubricFormSchema(questions)`, and its
`superRefine` checks the chosen question against them with the same rules as
`scoring.CheckAgainstForm`. The config's own checks come from the registry
([line 61](../../web/src/lib/rubric/schema.ts#L61)): the schema calls
`def.check` and files each problem under `config.<field>`. The type is still
derived, from the function's return type: `z.infer<ReturnType<typeof
rubricFormSchema>>`.

The weight total is deliberately **not** in the schema; section 7 says why.

## 5. The total is derived, not stored

**📍 Open:** [`web/src/components/rubric/weight-panel.tsx:27`](../../web/src/components/rubric/weight-panel.tsx#L27)

The tempting implementation keeps the total in state:

```ts
const [total, setTotal] = useState(0);
// …and in every weight's onChange: setTotal(t => t - old + new)
```

Now there are two sources of truth, the weights and the total, and every
operation must update both: typing a weight, removing a criterion, adding
one, discarding changes, loading a saved rubric, a server error that resets
a field. Miss one and the total says 100 while the weights say 90, and the
Save button trusts the wrong one.

`weightTotal` is a function of the criteria. The builder reads the criteria
with `useWatch` and calls it on every render
([`rubric-builder.tsx:77`](../../web/src/components/rubric/rubric-builder.tsx#L77)),
so the total *cannot* disagree with the weights: there is nothing to keep
in sync. Summing a few numbers per keystroke costs nothing. The rule of
thumb: state is for what cannot be computed from other state.

## 6. Summing in hundredths, not floats

**📍 Open:** [`web/src/lib/rubric/schema.ts:19`](../../web/src/lib/rubric/schema.ts#L19)

`33.33 + 33.33 + 33.34` in JavaScript floating point is
`99.99999999999999`, not 100. A total compared with `=== 100` would refuse a
correct rubric. The Go side compares with a tolerance (module 6.2). The
client avoids the problem instead: `toHundredths("33.33")` parses the text
into the integer 3333, and integers add exactly. The total is "exactly 100"
when it is exactly 10000. Parsing the typed text directly, not
`Number(text) * 100`, matters too: `0.29 * 100` is `28.999999999999996`.

## 7. Not-yet-100 that informs instead of blocking

**📍 Open:** [`web/src/components/rubric/weight-panel.tsx:52`](../../web/src/components/rubric/weight-panel.tsx#L52)

While a rubric is being built, being under 100 is the normal state, not an
error. Putting the sum rule in the schema would paint a red error from the
moment the first criterion is added, and list "weights must sum to 100" among
real mistakes. So the schema checks each criterion, and the total has a panel
of its own with three states, each told apart by icon, wording and colour,
never colour alone:

| State | Icon | Says | Save |
|---|---|---|---|
| under 100 | dashed circle | "60 points left to allocate", and how to fix it: raise a weight or add a criterion | disabled |
| exactly 100 | check | "Weights add up to 100" | enabled once something changed |
| over 100 | warning, red | "10 points over 100", and how to fix it: lower a weight or remove a criterion | disabled |

It informs rather than blocks in four ways:

- **The number is the gap**, not just a verdict: "60 points left" tells the
  admin how much to give out, so they can type it straight into a weight.
- **The fix is named**, in both directions.
- **The bar shows it**: the unallocated part is drawn as a hatched "free"
  segment; an over-allocated bar overflows a marked 100 line.
- **The reason sits beside the disabled button**: "Save is available once the
  weights add up to exactly 100." The status line is a live region, so a
  screen reader hears the state change as weights are typed.

`canSave` ([`rubric-builder.tsx:126`](../../web/src/components/rubric/rubric-builder.tsx#L126))
is then three plain conditions: something changed, the total is exact, and no
save is running.

## 8. The weight distribution: one proportional bar

**📍 Open:** [`web/src/components/rubric/weight-panel.tsx:89`](../../web/src/components/rubric/weight-panel.tsx#L89)

A rubric is a whole of 100 points divided among criteria: a part-to-whole
relationship. The chosen picture is **one stacked horizontal bar**, 100 points
wide, one segment per criterion, numbered to match the cards and the legend
beneath.

| Option | Why not |
|---|---|
| pie or donut | angles and areas are compared far less accurately than lengths; and a pie is always a full circle, so it cannot show "60 left" or "10 over" |
| one bar per criterion | compares criteria well, but loses the whole: you cannot see the gap to 100 |
| **one stacked bar** | lengths are compared accurately, the whole is the bar, the gap and the overflow are visible |

Details that make it honest:

- The scale is `max(100, total)` ([line 49](../../web/src/components/rubric/weight-panel.tsx#L49)).
  Over 100, the bar does not squeeze the segments to fit; a red line marks
  100 and the excess sits past it.
- Segments are tints of the one accent, separated by a gap, and identified by
  **number**, not colour. The band hues are reserved for score bands, and a
  criterion is not a score.
- The bar is `aria-hidden`: it is a picture of the legend, an `<ol>` that a
  screen reader reads as numbers.

## 9. Explaining each kind where it is chosen

**📍 Open:** [`web/src/components/rubric/criterion-card.tsx:118`](../../web/src/components/rubric/criterion-card.tsx#L118)

The kind is a radio group of four cards, each showing its name *and* its one
sentence, from the registry's `summary`, so the explanation is on screen at
the moment of choosing: "Reads a number of years: nothing at or below the
minimum, full weight at the target, a straight line between." A tooltip would
have to be found, hovered, and is unreachable on touch.

Below the settings, a worked example uses the admin's own numbers:

> For example: 1 year or fewer earns nothing; 5 or more earns all 20
> points; 3 years earns 10 points.

That sentence is the scoring engine's formula, applied, and it updates as
the numbers change. Each setting also has a one-line hint ("At or below
this, no points"; "“go” matches “Go” and “go,” but not “Google”").

## 10. Only this role's questions, only the right kinds

**📍 Open:** [`web/src/components/rubric/criterion-card.tsx:45`](../../web/src/components/rubric/criterion-card.tsx#L45)

The question select lists only questions fetched for this role
(`useQuestions(id)`), filtered to the kinds this criterion can read. An
experience criterion offers only number questions; All yes offers only
yes/no questions as checkboxes. When the role has no question of a usable
kind, the select is replaced by a sentence and a link: "This role has no
number question for this to read. Add one in Questions." The schema checks
the same thing again (section 4), in case a question was removed in another
tab, and the server checks it a third time.

## 11. Changing kind, and the stale errors it left

**📍 Open:** [`web/src/components/rubric/criterion-card.tsx:52`](../../web/src/components/rubric/criterion-card.tsx#L52)

Changing kind resets the config to the new kind's empty one, since the shapes
differ, and keeps the chosen question only if the new kind can read it, the
same clearing rule as the question builder (11.5).

Testing found a bug: after switching a card to Answer depth, it still showed
"Choose the written or choice question it scores", the message for keyword
match. `setValue` does not re-validate, so errors written for the old kind
survived. `changeKind` now clears the question and config errors
([line 65](../../web/src/components/rubric/criterion-card.tsx#L65)); they are
re-checked on the next edit or on save.

## 12. The keyword tag input

**📍 Open:** [`web/src/components/rubric/keyword-input.tsx:58`](../../web/src/components/rubric/keyword-input.tsx#L58)

Enter or a comma turns what was typed into a tag; a pasted "go, gin,
postgres" becomes three. Enter is `preventDefault`ed, or it would submit the
whole rubric. Backspace in an empty box removes the last tag. Leaving the box
adds what is in it, so a typed keyword is never silently lost. Duplicates are
refused case-insensitively, as the engine matches. Each tag's remove button
is named "Remove keyword go", and a live region announces each add, remove or
refusal.

## 13. Saving a version, and offering a rescore

**📍 Open:** [`web/src/components/rubric/rubric-builder.tsx:93`](../../web/src/components/rubric/rubric-builder.tsx#L93)

`PUT /v1/job-posts/:id/rubric` stores the whole rubric as the next version
(module 7.1); scores already given keep the version that produced them. The
builder says so above the criteria. The response counts
`applications_to_rescore`, and when it is above zero the success toast offers
**Rescore all** rather than doing it: a rescore rewrites every score and
appends an event to every application's timeline, which is the admin's call.

A 422 from the server has keys like `criteria[2].config.question_ids[0]`; a
regular expression ([line 111](../../web/src/components/rubric/rubric-builder.tsx#L111))
maps them onto `criteria.2.config.question_ids`, so each message appears in
its card. `criteria` itself (the server's sum check) goes to the banner.

## 14. Verification

| Check | Result |
|---|---|
| `npx tsc --noEmit`, `npm run lint` | clean (after replacing `satisfies … KindDefinition<never>` with the mapped type) |
| Empty rubric | "100 points left to allocate"; Save disabled with "Save is available once the weights add up to exactly 100." |
| Keyword match, weight 40, four keywords | "60 points left to allocate"; example "An answer using 2 of these 4 keywords earns 20 points of 40 points." |
| Add answer depth 35 (50 → 400 chars) and All yes 35 | "10 points over 100"; Save disabled; bar overflows the 100 mark |
| All yes weight to 25 | "Weights add up to 100"; Save enabled |
| Kind change by real clicks | question cleared when the new kind cannot read it; no stale messages (after the fix in section 11) |
| Save | PUT body had only each kind's config, `question_id: null` for All yes; toast "Rubric saved as version 1"; reason "No changes since the last save." |
| Stubbed 422 | `criteria[1].config` under card 2, `criteria[2].config.question_ids[0]` under card 3, `criteria` in the banner |
| Open Platform Engineer from the role header | toast "“Platform Engineer” is open for applications" (11.3's success path) |
| Close a throwaway open role from the roles menu | confirm dialog; status Open → Closed at 150ms; toast "… is closed" (11.3's close path) |
| Rescore all from the toast | not exercised: it would rewrite the 500 seeded demo scores |
| `make fmt && make lint && make test && go build ./...` | see the commit body |
