# Module 11, part 4: The role form

One form for creating and editing a role: a Zod schema the TypeScript types
are derived from, react-hook-form for state, rules that mirror the server's,
server errors mapped back onto fields, and a warning before unsaved changes
are lost.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | The schema, and types derived from it | [`web/src/lib/roles/role-form-schema.ts:62`](../../web/src/lib/roles/role-form-schema.ts#L62) |
| 2 | Input type versus output type | [`web/src/lib/roles/role-form-schema.ts:12`](../../web/src/lib/roles/role-form-schema.ts#L12) |
| 3 | Rules that mirror the server | [`web/src/lib/roles/role-form-schema.ts:25`](../../web/src/lib/roles/role-form-schema.ts#L25) |
| 4 | A cross-field rule in Zod | [`web/src/lib/roles/role-form-schema.ts:50`](../../web/src/lib/roles/role-form-schema.ts#L50) |
| 5 | Re-validating the other field with `deps` | [`web/src/components/roles/role-form.tsx:210`](../../web/src/components/roles/role-form.tsx#L210) |
| 6 | react-hook-form and the resolver | [`web/src/components/roles/role-form.tsx:46`](../../web/src/components/roles/role-form.tsx#L46) |
| 7 | Mapping a 422 onto fields with `setError` | [`web/src/lib/forms.ts:12`](../../web/src/lib/forms.ts#L12) |
| 8 | Why client validation does not replace the server's | [`web/src/components/roles/role-form.tsx:82`](../../web/src/components/roles/role-form.tsx#L82) |
| 9 | A disabled submit that says why | [`web/src/components/roles/role-form.tsx:237`](../../web/src/components/roles/role-form.tsx#L237) |
| 10 | One component for create and edit | [`web/src/components/roles/role-form.tsx:36`](../../web/src/components/roles/role-form.tsx#L36) |
| 11 | What PATCH can and cannot clear | [`web/src/lib/roles/role-form-schema.ts:113`](../../web/src/lib/roles/role-form-schema.ts#L113) |
| 12 | Warning before unsaved changes are lost | [`web/src/hooks/use-unsaved-changes.ts:31`](../../web/src/hooks/use-unsaved-changes.ts#L31) |
| 13 | A native `<select>` | [`web/src/components/ui/native-select.tsx:11`](../../web/src/components/ui/native-select.tsx#L11) |
| 14 | Verification | — |

---

## 1. The schema, and types derived from it

**📍 Open:** [`web/src/lib/roles/role-form-schema.ts:62`](../../web/src/lib/roles/role-form-schema.ts#L62)

A Zod schema is a value that exists at runtime and checks data. A TypeScript
type exists only at compile time and checks code. A form needs both: the
schema to validate what the user typed, the type so the code handling the
values is checked.

Writing them separately means writing every field twice:

```ts
interface RoleForm { title: string; vacancies: number }        // the type
const schema = z.object({ title: z.string(), vacancies: z.number() }); // the schema
```

Nothing ties them together. Add `location` to the schema and forget the
interface, and the code compiles while the form silently drops the field.
Make `vacancies` optional in one and not the other, and the two disagree
about what valid data looks like, with no error anywhere.

`z.infer<typeof roleFormSchema>` asks the compiler to compute the type *from*
the schema. There is one definition; the type cannot drift because it is not
a separate thing. Change the schema and every use of the type is re-checked.

## 2. Input type versus output type

**📍 Open:** [`web/src/lib/roles/role-form-schema.ts:12`](../../web/src/lib/roles/role-form-schema.ts#L12)

An HTML input's value is always a string, even `<input type="number">`. A
schema that declared `vacancies: z.number()` would fail on every keystroke.
So the numeric fields are validated as text and then **transformed**:

```ts
z.string().regex(/^\d+$/).transform(Number).pipe(z.number().min(1))
```

`transform` converts the checked string; `pipe` hands the result to a second
schema, so `min(1)` runs on a number, not on the text `"0"`.

A schema with transforms has two types, and Zod derives both:

| Type | Derived with | What it is | Used for |
|---|---|---|---|
| `RoleFormValues` | `z.input<…>` | every field a string | the form's state and defaults |
| `RoleFormOutput` | `z.infer<…>` (= `z.output`) | `vacancies: number`, a blank salary `undefined` | what `handleSubmit` receives |

`useForm<RoleFormValues, unknown, RoleFormOutput>` gives react-hook-form
both, so the inputs are typed as text and the submit handler as numbers.

## 3. Rules that mirror the server

**📍 Open:** [`web/src/lib/roles/role-form-schema.ts:25`](../../web/src/lib/roles/role-form-schema.ts#L25)

Each rule is the client copy of one in
[`domain.validateJobPost`](../../internal/domain/job_post.go#L316) or
`domain.ParseMoney`:

| Field | Server rule | Client rule |
|---|---|---|
| `title` | 1–200 runes after trimming | `trim().min(1).max(200)` |
| `vacancies` | at least 1, at most the int column's maximum | whole number, `min(1)`, `max(2147483647)` |
| `salary_min`, `salary_max` | non-negative, at most two decimals | `^\d+(\.\d{1,2})?$` or blank |
| both salaries | min not above max, when both set | the refine in section 4 |

One subtlety in `title`: Go counts characters with
`utf8.RuneCountInString`, JavaScript's `.length` counts UTF-16 code units.
They agree for every character in the Basic Multilingual Plane, which
includes Bengali; an emoji counts 2 in JavaScript and 1 in Go, so the client
is, if anything, slightly stricter. The server is the one that decides.

## 4. A cross-field rule in Zod

**📍 Open:** [`web/src/lib/roles/role-form-schema.ts:50`](../../web/src/lib/roles/role-form-schema.ts#L50)

A field's schema sees only that field's value; `salary_min` cannot know
`salary_max`. A rule about two fields goes on the **object**, which sees them
all, with `.refine(check, options)`:

- `check` receives the whole parsed object (numbers, after the transforms)
  and returns `true` when valid.
- `path: ["salary_min"]` says which field the error belongs to. Without it
  the error is attached to the form as a whole and appears under no input.
  `salary_min` is chosen because the server reports the same problem under
  that name, so a client error and a server error land in the same place.
- `when` decides whether the rule runs at all
  ([line 58](../../web/src/lib/roles/role-form-schema.ts#L58)).

`when` fixes a real bug, found by testing Zod 4 directly. By default an
object refine still runs when one of its fields has already failed. With
`salary_min: "abc"` the transform never ran, so the refine compared the
string `"abc"` with the number `5`, got `false`, and added a second,
misleading error: "minimum must not be more than the maximum" under a field
that does not contain a number. The guard runs the rule only when neither
salary field has an issue of its own:

| Input | Default | With `when` |
|---|---|---|
| title blank, min 9, max 5 | title error, salary error | title error, salary error |
| min `"abc"`, max 5 | "enter an amount", **and** "min more than max" | "enter an amount" only |

## 5. Re-validating the other field with `deps`

**📍 Open:** [`web/src/components/roles/role-form.tsx:210`](../../web/src/components/roles/role-form.tsx#L210)

react-hook-form re-validates the field being edited and updates only that
field's error. The min-versus-max error lives on `salary_min`, so typing a
larger maximum would fix the problem and leave the error showing. `deps`
names fields to re-validate alongside: each salary lists the other, so
editing either clears or raises the shared error. Verified: min 90000, max
70000 shows the error; changing the maximum to 120000 removes it.

## 6. react-hook-form and the resolver

**📍 Open:** [`web/src/components/roles/role-form.tsx:46`](../../web/src/components/roles/role-form.tsx#L46)

react-hook-form keeps inputs **uncontrolled**: `register("title")` returns a
`ref`, `name`, `onChange` and `onBlur` to spread onto the input, and the DOM
holds the value. Typing in one field re-renders nothing else, which a form of
`useState`s per field would not manage.

`zodResolver(roleFormSchema)` makes the schema the validator: on each
validation react-hook-form runs the whole schema and converts Zod's issues
into its `errors` object, keyed by `path`. `mode: "onTouched"` shows a
field's error only after the user has left it once, then updates it on every
keystroke, so nobody is told off for an empty title before they have typed.

Every input carries `aria-invalid` and an `aria-describedby` that points at
its error and hint ([line 123](../../web/src/components/roles/role-form.tsx#L123)),
so a screen reader reads the problem when the field takes focus.

`useWatch` reads the title for the character counter. The linter rejected
`watch()`: it returns a new function on every render, which the React
Compiler cannot memoise; `useWatch` subscribes to one field instead.

## 7. Mapping a 422 onto fields with `setError`

**📍 Open:** [`web/src/lib/forms.ts:12`](../../web/src/lib/forms.ts#L12)

The API's 422 envelope has the same field names the form uses:

```json
{"error":{"code":"validation_failed","details":{"title":"must be at most 200 characters"}}}
```

`applyServerErrors` walks `details` and, for each field the form has, calls
`setError(field, { type: "server", message })`. The message appears under
that input exactly as a client error would, `aria-invalid` turns on, and
`shouldFocus` on the first one moves the keyboard there. A field the form
does not have (or any non-422 error) goes to `root.server`, shown in a banner
above the form, so nothing is lost and nothing lands under the wrong input.

The alternative, one red box saying "One or more fields are invalid", tells
the user something is wrong and leaves them to find it.

The server writes messages to follow a field name ("must be at most 200
characters"); standing alone under a label they read better capitalised, so
the first letter is upper-cased. A server error clears when the field is
next validated, so it does not linger after the user fixes it.

## 8. Why client validation does not replace the server's

**📍 Open:** [`web/src/components/roles/role-form.tsx:82`](../../web/src/components/roles/role-form.tsx#L82)

Client validation is a convenience for an honest user. It is not a control:

- **Anyone can skip the browser.** `curl`, Swagger UI, or the devtools
  console send whatever they like straight to the API. Module 5.1 makes the
  same point for applications.
- **Clients drift.** An old tab still running last week's bundle, a mobile
  app on an old version, or a rule changed on the server and not yet here.
- **Some rules need data only the server has.** Uniqueness, whether a post is
  still a draft, whether a candidate already applied, whether a question
  belongs to this post. A browser cannot check those, and checking them first
  and writing later is a race (module 5.3).

So the server validates everything, and the client mirrors what it can to
save a round trip. `applyServerErrors` is the bridge for everything the
client did not catch.

## 9. A disabled submit that says why

**📍 Open:** [`web/src/components/roles/role-form.tsx:237`](../../web/src/components/roles/role-form.tsx#L237)

The button is disabled while the form is invalid, while it is submitting
(no double-submit creating two drafts), and, when editing, until something
has changed. A disabled button on its own is a puzzle, so a line beside it
([line 245](../../web/src/components/roles/role-form.tsx#L245)) says why:
"Fix the 2 highlighted fields to continue", "A title and at least one vacancy
are required", or "No changes yet". It is an `aria-live` region, so the
reason is announced when it changes.

## 10. One component for create and edit

**📍 Open:** [`web/src/components/roles/role-form.tsx:36`](../../web/src/components/roles/role-form.tsx#L36)

The fields, rules, error mapping and guard are identical; only the starting
values, the request and the destination differ. Two components would mean
every new field and rule written twice, and the two drifting. `RoleForm`
takes an optional `post`:

| | Create (no `post`) | Edit (`post`) |
|---|---|---|
| defaults | `emptyRoleForm` | `roleFormValuesFrom(post)` |
| request | `POST /v1/job-posts`, every filled field | `PATCH /v1/job-posts/:id`, changed fields only ([line 68](../../web/src/components/roles/role-form.tsx#L68)) |
| button | "Create draft" | "Save changes", disabled until dirty |

After a save, `reset()` with the server's response makes the form clean
before navigating, which also turns the unsaved-changes warning off. The
edit page keys the form by post ID, so moving to another role remounts it
with that role's defaults rather than keeping the last one's.

## 11. What PATCH can and cannot clear

**📍 Open:** [`web/src/lib/roles/role-form-schema.ts:113`](../../web/src/lib/roles/role-form-schema.ts#L113)

`PATCH /v1/job-posts/:id` treats a missing key and `null` the same way:
"keep the stored value" (module 4.1). That makes text fields clearable
(sending `""` stores an empty description) but not numbers or dates: there
is no way to say "remove the salary". The form tells the admin so beneath the
salary and closing-date fields when editing, instead of letting them blank a
field, save, and find the old value still there. Changing the API to accept
`null` as "clear" is the real fix, noted for later.

## 12. Warning before unsaved changes are lost

**📍 Open:** [`web/src/hooks/use-unsaved-changes.ts:31`](../../web/src/hooks/use-unsaved-changes.ts#L31)

The App Router has no API for blocking a navigation, so each way out of the
page is handled where the browser exposes it:

| Way out | Handled by |
|---|---|
| reload, close the tab, type a URL | `beforeunload` ([line 23](../../web/src/hooks/use-unsaved-changes.ts#L23)); the browser shows its own wording |
| any link in the app | one capture-phase click listener on `document` ([line 63](../../web/src/hooks/use-unsaved-changes.ts#L63)) |
| the Back button | a guard history entry ([line 52](../../web/src/hooks/use-unsaved-changes.ts#L52)) |

The link listener works because of event order. React listens on the app's
root element; a capture listener on `document` runs earlier, and calling
`preventDefault` there is seen by `next/link`'s click handler, which skips
navigation when the event is already default-prevented (verified in
`next/dist/client/app-dir/link.js`). One listener therefore guards every
link, including the sidebar, without changing any of them.

For Back, the hook pushes a copy of the current entry, so the first Back
press lands on the same page and fires `popstate`, which is the moment to
ask. Staying pushes the copy again; leaving calls `history.back()` once more.
Next.js patches `pushState` and copies its own state into the new entry, so
the router treats the copy as one of its own instead of reloading.

The warning is active only while the form is dirty and not submitting
(`isDirty && !isSubmitting`). After a save the form is reset and the app
navigates with `router.replace`, which overwrites the guard entry.

## 13. A native `<select>`

**📍 Open:** [`web/src/components/ui/native-select.tsx:11`](../../web/src/components/ui/native-select.tsx#L11)

The Base UI `Select` from shadcn is a custom listbox. For a form field with
five options, the browser's own `<select>` is better: `register()` works on
it directly, every screen reader knows it, and on a phone it opens the
platform's picker. It is styled with the same tokens as `Input`, with the
default arrow replaced, so it looks native to the design rather than to the
browser.

## 14. Verification

| Check | Result |
|---|---|
| `npx tsc --noEmit`, `npm run lint` | clean (after replacing `watch` with `useWatch`, and `i.path` with `i.path?.`) |
| Zod 4 directly, in Node | default refine reported a bogus min/max error for `"abc"`; with `when`, only "enter an amount" |
| New form, untouched | submit disabled; "A title and at least one vacancy are required." |
| Title cleared and left | "Give the role a title" |
| Vacancies 0, min 90000, max 70000 | vacancy error and the min/max error on `salary_min`; submit disabled |
| Max changed to 120000 | min/max error cleared (via `deps`) |
| Min `abc` | only "Enter an amount like 85000 or 85000.50" |
| Stubbed 422 with `title`, `vacancies` and an unknown `closes_by` | errors under Title and Vacancies, focus on Title, `aria-invalid` set, `closes_by` in the banner |
| Dirty form: click a sidebar link, refuse | stayed on `/admin/roles/new`; `confirm` asked |
| Dirty form: Back, refuse | stayed; asked again |
| Dirty form: click a link, accept | navigated to `/admin/roles` |
| Create "Platform Engineer" | `POST` succeeded, landed on the role's overview with every field shown |
| Edit: change location only | `PATCH` body was exactly `{"location":"Dhaka"}`; no warning on the redirect after saving |
| `make fmt && make lint && make test && go build ./...` | see the commit body |
