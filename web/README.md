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
| `src/app/layout.tsx` | Root layout: fonts, theme provider, query provider, toaster |
| `src/app/login/` | Sign-in page |
| `src/app/admin/` | The admin console, behind the session gate in `components/layout/admin-shell.tsx` |
| `src/app/tokens/` | The design-token specimen, at `/tokens` |
| `src/lib/api/` | The fetch wrapper (`client.ts`), the error type and the API's types |
| `src/lib/auth/session.ts` | The signed-in session, kept in localStorage |
| `src/lib/query/` | The TanStack Query client and its defaults (`client.ts`), and every query key (`keys.ts`) |
| `src/hooks/` | One file of query and mutation hooks per resource |
| `src/components/states/` | The shared loading, empty and error states, and `QueryView` |
| `src/components/ui/` | shadcn/ui components, restyled only through the tokens |

The reasoning behind each choice is in the module 11 notes, starting at
[`docs/notes/11-1-frontend-foundation.md`](../docs/notes/11-1-frontend-foundation.md).
