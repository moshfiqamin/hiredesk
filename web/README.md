# HireDesk web

The Next.js frontend for the HireDesk API. App Router, TypeScript (strict),
Tailwind CSS v4 and shadcn/ui on Base UI.

## Run

From the repository root, `make up` builds and starts it with the rest of the
stack at <http://localhost:3000>.

To work on it with hot reload instead, start the API with `make up`, then:

```bash
cd web && npm install && npm run dev
```

## Where things are

| Path | Holds |
|---|---|
| `src/app/globals.css` | Every design token: palette, semantic colours, type and spacing scales, light and dark |
| `src/app/layout.tsx` | Root layout: fonts, theme provider, toaster |
| `src/components/ui/` | shadcn/ui components, restyled only through the tokens |
| `src/components/theme-*.tsx` | next-themes provider and the toggle |

The reasoning behind each choice is in
[`docs/notes/11-1-frontend-foundation.md`](../docs/notes/11-1-frontend-foundation.md).
