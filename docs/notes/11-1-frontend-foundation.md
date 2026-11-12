# Module 11, part 1: The frontend foundation and the design system

A Next.js app in `web/`, started by `make up` with the rest of the stack, and,
before any screen exists, the design tokens every screen will be built from.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | Choosing the look before writing code | [`docs/design/hiredesk-prototype.html`](../design/hiredesk-prototype.html) |
| 2 | The App Router directory conventions | [`web/src/app/layout.tsx:42`](../../web/src/app/layout.tsx#L42) |
| 3 | shadcn/ui, Base UI, and the missing `form` | [`web/components.json:3`](../../web/components.json#L3) |
| 4 | Three layers of tokens | [`web/src/app/globals.css:26`](../../web/src/app/globals.css#L26) |
| 5 | Why tokens come before screens | [`web/src/app/globals.css:59`](../../web/src/app/globals.css#L59) |
| 6 | One accent, and score bands that survive colour blindness | [`web/src/app/globals.css:79`](../../web/src/app/globals.css#L79) |
| 7 | Contrast, measured | [`web/src/app/globals.css:71`](../../web/src/app/globals.css#L71) |
| 8 | Type and spacing scales | [`web/src/app/globals.css:101`](../../web/src/app/globals.css#L101) |
| 9 | Dark mode is a second set of values | [`web/src/app/globals.css:157`](../../web/src/app/globals.css#L157) |
| 10 | Tailwind v4 `@theme inline` | [`web/src/app/globals.css:219`](../../web/src/app/globals.css#L219) |
| 11 | next-themes and the flash of the wrong theme | [`web/src/app/layout.tsx:49`](../../web/src/app/layout.tsx#L49) |
| 12 | A hydration mismatch React will not repair | [`web/src/components/theme-toggle.tsx:25`](../../web/src/components/theme-toggle.tsx#L25) |
| 13 | Self-hosted fonts | [`web/src/app/layout.tsx:12`](../../web/src/app/layout.tsx#L12) |
| 14 | `NEXT_PUBLIC_` is baked in at build time | [`web/Dockerfile:22`](../../web/Dockerfile#L22) |
| 15 | The standalone image and the Compose service | [`docker-compose.yml:120`](../../docker-compose.yml#L120) |
| 16 | CORS for the new origin | [`docker-compose.yml:108`](../../docker-compose.yml#L108) |
| 17 | Verification | — |

---

## 1. Choosing the look before writing code

**📍 Open:** [`docs/design/hiredesk-prototype.html`](../design/hiredesk-prototype.html)

Template-built frontends tend to converge on a few looks: a purple gradient
hero, cream with a serif and terracotta, or near-black with one neon accent.
To avoid that, the direction was settled before any code, in three steps.
The design priority was an "operate" mode: for a tool used six hours a day,
scanability and consistency rank above expression.

1. **A first set of directions,** rejected: the navy one did not fit.
2. **A reference photo.** Four free-licence Unsplash photos of real hiring
   moments, with palettes sampled from their pixels. *Across the desk* (an
   interviewer taking notes; sage walls, a birch desk, graphite) was chosen.
3. **An interactive prototype** in that palette, with admin, interviewer and
   candidate views, finished before this module started.

Deriving colours from a photograph of the domain gives the palette a reason
to exist, so it is not just a default. Sage also stays clear of the three
score-band hues (section 6), so the accent never reads as a score.

## 2. The App Router directory conventions

**📍 Open:** [`web/src/app/layout.tsx:42`](../../web/src/app/layout.tsx#L42)

In the App Router, **folders are routes and special file names are roles**.
`src/app/jobs/[id]/` is the URL `/jobs/:id`; what happens there depends on
which of these files the folder contains:

| File | Role | Re-renders on navigation? |
|---|---|---|
| `layout.tsx` | Wraps every page below it. Holds shared chrome (later: the sidebar). | No: it stays mounted, so its state survives moving between child pages |
| `page.tsx` | The content for exactly this URL. Without it the folder is not a route. | Yes |
| `loading.tsx` | Shown immediately while `page.tsx` is fetching. Next wraps the page in a React `<Suspense>` with this as the fallback. | Yes |
| `error.tsx` | Shown when the page throws; must be a Client Component | Yes |
| `not-found.tsx` | Shown for `notFound()` or an unmatched URL | Yes |

The root `layout.tsx` is the only one that must render `<html>` and `<body>`.
Files are **Server Components by default**: they run on the server and ship
no JavaScript. A file opts into the browser with `"use client"` at the top,
as [`theme-provider.tsx`](../../web/src/components/theme-provider.tsx#L1) does,
because it reads `localStorage`.

`LayoutProps<"/">` in the layout's signature is a Next 16 global type,
generated from the route tree. It types `children` without a hand-written
interface.

## 3. shadcn/ui, Base UI, and the missing `form`

**📍 Open:** [`web/components.json:3`](../../web/components.json#L3)

shadcn/ui is not a dependency you import from. `npx shadcn add button` copies
the component's source into `src/components/ui/`, where it belongs to this
project. That is why the components can be restyled entirely through tokens.

The current CLI defaults to the `base-nova` style, built on **Base UI**
rather than Radix. In that style the `form` component the brief asked for is
an **empty registry entry**: `shadcn add form` reports success and writes
nothing. Its replacement is `field`, which was added instead and is what the
login form (11.3) will compose with react-hook-form. The alternative was
re-initialising on a Radix style just to get the old `form` wrapper. That
would have tied the project to an older component set for one convenience.

shadcn also installed `cn`, its own class-merging package (repository
`shadcn-ui/cn`), as a drop-in for `clsx` plus `tailwind-merge`. It was
checked before being kept.

## 4. Three layers of tokens

**📍 Open:** [`web/src/app/globals.css:26`](../../web/src/app/globals.css#L26)

| Layer | Example | Who reads it |
|---|---|---|
| Palette | `--palette-sage-600: #4a6b5b` | Only the semantic layer |
| Semantic | `--primary: var(--palette-sage-600)` | Components |
| Bridge | `--color-primary: var(--primary)` in `@theme inline` | Tailwind, which turns it into `bg-primary` |

The palette names say what the colour *is* (sage, birch). The semantic names
say what it is *for* (primary action, muted text). A component asking for
`text-muted-foreground` keeps working if muted text later moves from moss to
birch, because only one line changes. Hex codes in components would make
that a search-and-replace across every file, with no way to tell a "muted
text" grey from an identical "border" grey.

shadcn's own variable names (`--card`, `--accent`, `--input`) are pointed at
the semantic tokens at [`globals.css:129`](../../web/src/app/globals.css#L129),
so the copied components pick up the palette without being edited. Note the
naming clash: in shadcn, `--accent` means a subtle *hover* background, not
the brand accent. HireDesk's one accent is `--primary`.

## 5. Why tokens come before screens

**📍 Open:** [`web/src/app/globals.css:59`](../../web/src/app/globals.css#L59)

Styling screen by screen produces drift. The roles list gets a border of
`#ddd`, the applications table `#e0e0e0`, the dialog `gray-200`. Each was
reasonable on the day, and together they make an interface look assembled.
Spacing drifts the same way (13px here, 14px there), and so does type.

Tokens prevent it in three ways:

- **A closed set.** A screen can only choose from what exists. Something
  missing is a visible event: a new token, discussed once, instead of a
  silent one-off.
- **One source.** Dark mode, a contrast fix, or a rebrand is one edit. The
  input-border fix in section 7 changed one variable and every field moved
  together.
- **Reviewable.** The home page is temporarily a specimen of every token
  ([`page.tsx`](../../web/src/app/page.tsx#L1)), so the whole system can be
  judged in both themes before a feature depends on it.

## 6. One accent, and score bands that survive colour blindness

**📍 Open:** [`web/src/app/globals.css:79`](../../web/src/app/globals.css#L79)

Deep sage is used for exactly three things: the primary action, the current
selection, and keyboard focus. When colour means "act here" and nothing else,
the eye finds the action without reading.

The score bands at [`globals.css:91`](../../web/src/app/globals.css#L91) avoid
the red/green pair that about one in twelve men cannot tell apart. They use
three hues from the Okabe–Ito palette, designed for colour-vision deficiency
(blue, ochre, vermilion), and never rely on hue alone. The `.score-band` chip
at [`globals.css:341`](../../web/src/app/globals.css#L341) carries three
redundant cues:

| Band | Colour | Shape | Word |
|---|---|---|---|
| Strong (75+) | blue | solid square | "Strong" |
| Adequate (50–74) | ochre | hatched square | "Adequate" |
| Weak (<50) | vermilion | dashed outline | "Weak" |

In greyscale, or to someone with no colour vision at all, the shape and the
word still identify the band. The hatch is drawn with
`repeating-linear-gradient`. That is a pattern, not the decorative gradient
the brief rules out.

## 7. Contrast, measured

**📍 Open:** [`web/src/app/globals.css:71`](../../web/src/app/globals.css#L71)

Every text pair was computed with the WCAG 2 relative-luminance formula, not
judged by eye. AA requires 4.5:1 for body text, 3:1 for large text and for
the edges of controls (success criterion 1.4.11).

| Pair | Light | Dark |
|---|---|---|
| Body on surface | 15.13 | 13.63 |
| Secondary on surface | 7.28 | 7.96 |
| Muted on surface | 5.35 | 5.92 |
| Muted on page (worst case) | 4.98 | 6.34 |
| Muted on sunken | 4.71 | 6.51 |
| Button text on primary | 5.92 | 8.05 |
| Primary as link text | 5.71 | 7.60 |
| Focus ring against page (needs 3) | 5.32 | 8.14 |
| Input border against surface (needs 3) | 3.18 | 3.45 |
| Strong band text on its fill | 5.76 | 7.47 |
| Adequate band | 5.46 | 8.45 |
| Weak band / destructive | 5.29 | 7.28 |

The first measurement **failed** on input borders: 1.68:1 and 1.63:1. The
hairline grey that suits table dividers is too faint to show where a text
field is. The fix is a separate `--border-field` token. Decorative dividers
stay quiet, and fields get an edge that meets 3:1. One token change fixed
every input, select and textarea at once.

Disabled buttons fall below 4.5:1 (shadcn renders them at 50% opacity).
WCAG explicitly exempts inactive controls, so this is left as it is.

## 8. Type and spacing scales

**📍 Open:** [`web/src/app/globals.css:101`](../../web/src/app/globals.css#L101)

**Type:** 12 / 13 / 14 / 16 / 19 / 24 / 30 px. The base is 14px, not the web's
usual 16: in a table-heavy tool, 16px body fits fewer rows on a laptop
screen, and recruiters compare rows. Steps of about 1.15–1.25 suit a product
UI, which has many type roles. Bigger jumps add noise without adding
hierarchy. Sizes are fixed `rem`, not fluid `clamp()`, because a heading
inside a sidebar should not shrink with the window.

The scale *replaces* Tailwind's defaults ([`globals.css:277`](../../web/src/app/globals.css#L277)),
so `text-sm` means 13px everywhere, inside shadcn's components too. Adding
parallel names instead would have left the components on Tailwind's sizes
and every screen with two type systems.

**Fonts:** Instrument Sans for everything readable, IBM Plex Mono for
figures, IDs and scores, where digits must line up. One sans family is right
for a product UI; a display face would be a costume here.

**Spacing:** a 4px grid, `--space-1` to `--space-12` at
[`globals.css:113`](../../web/src/app/globals.css#L113). Tailwind's own
spacing unit is set to the same 4px, so `p-3` and `var(--space-3)` are
always the same 12px.

## 9. Dark mode is a second set of values

**📍 Open:** [`web/src/app/globals.css:157`](../../web/src/app/globals.css#L157)

The `.dark` block redefines **every** colour token. Nothing is inverted
automatically, because inversion gives wrong results: a pale band fill
becomes a muddy dark one, and the sage accent loses contrast against a dark
ground. Each dark value was chosen and then measured (section 7). The accent
gets lighter (`#8bb8a1`) and the text on it gets darker, the opposite of the
light theme. Scales, spacing and radius are not repeated, because they do not
change with the theme.

`color-scheme: dark` in the same block tells the browser to draw native
controls and scrollbars dark as well.

## 10. Tailwind v4 `@theme inline`

**📍 Open:** [`web/src/app/globals.css:219`](../../web/src/app/globals.css#L219)

Tailwind v4 is configured in CSS, with no `tailwind.config.js`. `@theme`
declares variables that become utilities: `--color-surface` creates
`bg-surface`, `text-surface` and `border-surface`.

The `inline` keyword matters. Without it, Tailwind copies the value it sees
at build time into each utility, which is the light value. With it, the
utility emits `var(--surface)`, so when `.dark` redefines `--surface` on
`<html>`, every `bg-surface` follows. `@custom-variant dark` at
[`globals.css:23`](../../web/src/app/globals.css#L23) makes `dark:` utilities
follow that class instead of the operating system's setting, so an explicit
choice in the app wins.

## 11. next-themes and the flash of the wrong theme

**📍 Open:** [`web/src/app/layout.tsx:49`](../../web/src/app/layout.tsx#L49)

The flash happens like this. The server cannot know a visitor's theme: it
lives in `localStorage` and in the OS setting, both in the browser. So the
server sends light HTML, the browser paints it, React loads, reads the
preference, and switches to dark. For a few hundred milliseconds a dark-mode
user sees a white page.

next-themes prevents it by injecting a small **blocking inline script** at
the top of `<body>`, before any content. The browser runs it before painting
anything below it. It reads `localStorage.theme`, falls back to
`prefers-color-scheme`, and sets `class="dark"` on `<html>`. By the first
paint the right tokens are active. Verified in section 17: the script is in
the served HTML ahead of the page content.

The settings on the provider ([`layout.tsx:53`](../../web/src/app/layout.tsx#L53)):

- `attribute="class"` matches the `.dark` selector the tokens use.
- `defaultTheme="system"` with `enableSystem` follows the OS until the user
  chooses.
- `disableTransitionOnChange` suspends CSS transitions for the instant of
  switching, so 150ms colour transitions do not ripple across the page.

`suppressHydrationWarning` on `<html>` is needed because the script changes
that element's class before React hydrates. React would otherwise warn that
server and client HTML differ. The flag affects that one element only, not
its children.

## 12. A hydration mismatch React will not repair

**📍 Open:** [`web/src/components/theme-toggle.tsx:25`](../../web/src/components/theme-toggle.tsx#L25)

The first toggle labelled itself from the theme: "Switch to light theme" when
dark. In the browser, after loading in dark mode, it still said "Switch to
dark theme". It was found while checking the page, not by the build or the
linter.

The cause: on the server, `resolvedTheme` is undefined, so the label was
"Switch to dark". In the browser, next-themes already knows the theme during
hydration, so React's first client render computed "Switch to light". React
19 does not patch mismatched attributes after hydration in production. It
keeps the server's value, so the stale label stayed.

The fix is to render nothing during render that depends on the theme. The
label is constant ("Toggle colour theme"), `resolvedTheme` is read only
inside the click handler, and both icons render with CSS (`dark:block` /
`dark:hidden`) choosing which one shows. The alternative, a `mounted` flag
that renders a placeholder until after hydration, also works, but it makes
the button appear a frame late, a small flash of its own.

## 13. Self-hosted fonts

**📍 Open:** [`web/src/app/layout.tsx:12`](../../web/src/app/layout.tsx#L12)

`next/font/google` downloads fonts from Google **during `next build`**. That
build failed in this environment: Turbopack fetches fonts from Rust code that
did not go through the network proxy. The same would happen in any CI or
Docker build without outbound access to Google.

The fonts now come from the `@fontsource` npm packages and load with
`next/font/local`. The build needs nothing beyond npm, the font files'
licence (SIL OFL) travels with the package, and no user's browser ever
contacts Google. `next/font` still generates the `@font-face` rules,
preloads the files, and computes fallback metrics so text does not jump when
the font arrives.

## 14. `NEXT_PUBLIC_` is baked in at build time

**📍 Open:** [`web/Dockerfile:22`](../../web/Dockerfile#L22)

`process.env.NEXT_PUBLIC_API_URL` in browser code is not read when the page
runs. `next build` replaces it with the literal string. So the value has to
exist when the image is **built**, which is why it is a Docker `ARG` and a
Compose `build.args` entry, not a runtime `environment` entry. Setting it at
`docker run` time would change nothing.

The value is `http://localhost:8080`, not `http://api:8080`. The service name
`api` resolves only inside the Compose network, but these requests come from
the user's browser on the host, which reaches the API through the published
port.

## 15. The standalone image and the Compose service

**📍 Open:** [`docker-compose.yml:120`](../../docker-compose.yml#L120)

`output: "standalone"` in [`next.config.ts:6`](../../web/next.config.ts#L6)
makes the build trace exactly which files the server imports and copy them
to `.next/standalone`, with a `server.js` entry point. The
[Dockerfile](../../web/Dockerfile#L28) runtime stage copies only that, plus
the static assets, so it needs no `npm install` and no source. It runs as the
image's unprivileged `node` user ([`Dockerfile:42`](../../web/Dockerfile#L42))
and binds `0.0.0.0` ([`Dockerfile:35`](../../web/Dockerfile#L35)); bound to
localhost, Docker's port mapping could not reach it.

The service depends on `api` starting, but not on a healthcheck: pages render
without the API and will show their own error state when it is down.
`web/` is added to the root `.dockerignore` so the Go image's build context
does not carry `node_modules`.

## 16. CORS for the new origin

**📍 Open:** [`docker-compose.yml:108`](../../docker-compose.yml#L108)

The frontend at `localhost:3000` and the API at `localhost:8080` are
different origins, since the port is part of an origin. The browser blocks
the frontend from reading API responses unless the API names its origin in
`Access-Control-Allow-Origin`. Module 10.3 built that allowlist, with no
entries by default.

Compose now defaults `ALLOWED_ORIGINS` to `http://localhost:<WEB_PORT>`, so
`make up` works with no configuration. The Go binary's own default stays
empty: deny by default is still right for anyone running the API without
this frontend. An empty value in `.env` counts as unset for Compose's `:-`
syntax, so existing `.env` files get the new default too.

## 17. Verification

| Command or check | Result |
|---|---|
| `grep strict web/tsconfig.json` | `"strict": true` |
| `npx tsc --noEmit` (in `web/`) | no errors |
| `npm run lint` (in `web/`) | no warnings |
| `npm run build` (in `web/`) | compiled; `/` prerendered as static |
| Contrast script, both themes | all pairs pass after the `--border-field` fix (table in section 7) |
| `docker compose config -q` | valid |
| `docker compose build web` | image built |
| `make up` | postgres, migrate, api, web, adminer all started; prints the Web link |
| `curl localhost:3000/` | 200 |
| `curl -H 'Origin: http://localhost:3000' localhost:8080/healthz` | `Access-Control-Allow-Origin: http://localhost:3000` |
| Served HTML | next-themes script present before the page content |
| Browser: system dark, then toggle | dark tokens on first paint; switches to light; preference saved |
| Browser: saved `dark`, reload | renders dark with no light frame; no console errors |
| `make fmt && make lint && make test && go build ./...` | see the commit body |
