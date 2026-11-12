# Module 11, part 2: Server state with TanStack Query

The conventions every admin page follows: how data is fetched and cached,
how cache keys are named, how mutations refresh what they change, and how a
page shows loading, empty and failed states. Also the pieces those depend on:
an API client, a signed-in session, and one new API endpoint.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | What TanStack Query replaces | [`web/src/hooks/use-job-posts.ts:12`](../../web/src/hooks/use-job-posts.ts#L12) |
| 2 | Stale time versus cache time | [`web/src/lib/query/client.ts:42`](../../web/src/lib/query/client.ts#L42) |
| 3 | Retrying: why a 422 is never retried | [`web/src/lib/query/client.ts:17`](../../web/src/lib/query/client.ts#L17) |
| 4 | Refetch on window focus: kept on | [`web/src/lib/query/client.ts:55`](../../web/src/lib/query/client.ts#L55) |
| 5 | Mutations are not retried | [`web/src/lib/query/client.ts:61`](../../web/src/lib/query/client.ts#L61) |
| 6 | The query key convention | [`web/src/lib/query/keys.ts:4`](../../web/src/lib/query/keys.ts#L4) |
| 7 | Invalidation, and why mutations need it | [`web/src/hooks/use-job-posts.ts:31`](../../web/src/hooks/use-job-posts.ts#L31) |
| 8 | One pattern for loading, empty and error | [`web/src/components/states/states.tsx:94`](../../web/src/components/states/states.tsx#L94) |
| 9 | The provider, and devtools in development only | [`web/src/components/providers.tsx:12`](../../web/src/components/providers.tsx#L12) |
| 10 | The API client and the refresh race | [`web/src/lib/api/client.ts:79`](../../web/src/lib/api/client.ts#L79) |
| 11 | The session, and where the token lives | [`web/src/lib/auth/session.ts:87`](../../web/src/lib/auth/session.ts#L87) |
| 12 | A browser-only gate with `use(browser())` | [`web/src/components/layout/admin-shell.tsx:30`](../../web/src/components/layout/admin-shell.tsx#L30) |
| 13 | Instant-navigation validation, scoped | [`web/next.config.ts:15`](../../web/next.config.ts#L15) |
| 14 | The open redirect on the login page | [`web/src/app/login/login-form.tsx:16`](../../web/src/app/login/login-form.tsx#L16) |
| 15 | The new endpoint: `GET /v1/admin/job-posts` | [`internal/store/job_posts.go:344`](../../internal/store/job_posts.go#L344) |
| 16 | Embedding a struct to share JSON fields | [`internal/api/job_post_handler.go:91`](../../internal/api/job_post_handler.go#L91) |
| 17 | Verification | — |

---

## 1. What TanStack Query replaces

**📍 Open:** [`web/src/hooks/use-job-posts.ts:12`](../../web/src/hooks/use-job-posts.ts#L12)

The obvious way to load data in React is a `useState` for the result and a
`useEffect` that fetches it:

```tsx
const [posts, setPosts] = useState(null);
useEffect(() => { fetch(url).then(r => r.json()).then(setPosts); }, []);
```

That works for one component on one page. Each of the following then has to
be built by hand, in every component that fetches:

| Problem | What you would write |
|---|---|
| Loading and error flags | two more `useState`s, set in the right order on every path |
| Race conditions | a "cancelled" flag in the effect's cleanup, or an `AbortController`, so a slow old response cannot overwrite a fast new one when the input changes |
| Deduplication | a shared promise map, so three components asking for the same data make one request |
| Caching across pages | a global store, so leaving a page and coming back does not refetch and flash a spinner |
| Staleness | timestamps per entry, and rules for when cached data is too old to trust |
| Background refresh | logic to show the old data while new data loads, instead of blanking the page |
| Refetch on focus or reconnect | `visibilitychange` and `online` listeners |
| Retries with backoff | a loop with timers, and a rule for which errors are worth retrying |
| Keeping other screens in step after a write | a way for a mutation to tell every list and detail that shows the changed record to refresh |
| Optimistic updates with rollback | a snapshot of the cache before the write, and code to restore it on failure (used in 11.3) |
| Garbage collection | eviction of cache entries nobody has looked at for a while |
| Strict Mode double effects | React 18+ runs effects twice in development; a naive effect fetches twice |

TanStack Query is a cache for **server state**: data the browser holds a copy
of but does not own. `useJobPosts` is the whole cost of fetching the roles
list. The hook returns `data`, `error`, `isPending`, `isFetching` and
`refetch`, and every item in the table above is handled by the library.

The `signal` passed to `queryFn` ([line 17](../../web/src/hooks/use-job-posts.ts#L17))
is an `AbortSignal`. When a query's component unmounts or its key changes
before the response arrives, TanStack Query aborts the `fetch`, so the race
condition in the second row cannot happen.

## 2. Stale time versus cache time

**📍 Open:** [`web/src/lib/query/client.ts:42`](../../web/src/lib/query/client.ts#L42)

They answer two different questions:

- **`staleTime`: is this data still good enough to show without asking
  again?** While a query is fresh, using it costs nothing. Once stale, it is
  still shown, but the next use triggers a background refetch.
- **`gcTime`** (formerly `cacheTime`): **once no component is using this
  data, how long is it kept in memory?** After that it is deleted, and the
  next use starts from a loading state.

A concrete walk through, with `staleTime: 30s` and `gcTime: 5min`:

| Time | What the admin does | What happens |
|---|---|---|
| 0:00 | opens Roles | no cache: skeleton, one request |
| 0:10 | opens a role, comes back to Roles | data is 10s old, fresh: shown instantly, **no request** |
| 0:45 | opens a role, comes back | data is 45s old, stale: shown instantly, refetched in the background, table updates if anything changed |
| 0:50 | goes to Applications | the Roles query has no component; the 5-minute `gcTime` clock starts |
| 4:00 | comes back to Roles | still in memory (3m10s unused): shown instantly, refetched because stale |
| 6:00 (after 5 more unused minutes) | comes back | entry was garbage-collected: skeleton and a fresh request |

Stale data is never *hidden*; it is shown while being replaced. That is the
difference from a plain cache with expiry.

**Why 30 seconds.** The default is 0: every mount refetches. On a dashboard
that means a request each time the admin moves between a role and its list,
for data that very rarely changed in between. Much longer (minutes) would show
a colleague's change late. 30 seconds removes the navigation chatter, and the
admin's *own* changes do not depend on it at all: mutations invalidate what
they change (section 7), so those appear immediately.

## 3. Retrying: why a 422 is never retried

**📍 Open:** [`web/src/lib/query/client.ts:17`](../../web/src/lib/query/client.ts#L17)

TanStack Query retries a failed query three times by default, with
exponential backoff. That is right for failures that might pass (a 502 while
the API restarts, a dropped connection) and wrong for failures that cannot.

A **422 `validation_failed`** means the server read the request and refused
its content: a title of 201 characters, a filter value that is not a status.
The same bytes sent again get the same answer, deterministically. Retrying
three times with backoff means the user waits several seconds staring at a
spinner, for an error that was known after the first response, while the API
does the same validation work four times. The same holds for 400, 401, 403,
404 and 409: none of them changes by waiting.

`shouldRetry` therefore refuses to retry any 4xx, except **408** (request
timeout) and **429** (rate limited), whose meaning is "not now" rather than
"not this". Everything else (5xx, and status 0 for a network failure) is
retried up to twice. `ApiError.isClientError`
([`errors.ts:31`](../../web/src/lib/api/errors.ts#L31)) is what makes the rule
one line: the client turns every failure into an `ApiError` with a status.

## 4. Refetch on window focus: kept on

**📍 Open:** [`web/src/lib/query/client.ts:55`](../../web/src/lib/query/client.ts#L55)

The setting refetches stale queries when the browser tab regains focus. Many
apps turn it off because it surprises developers, who switch to their editor
and back and see requests fire.

It stays **on** here, deliberately. The person using this is an admin who
moves between HireDesk, email and a calendar all day, while colleagues
shortlist and reject candidates. Coming back to a tab that has been in the
background for an hour, the pipeline on screen is an hour old; acting on it
(bulk-rejecting the "submitted" column) could act on candidates someone else
already moved. Focus refetch fixes that at the moment it matters.

The cost is bounded by `staleTime`: only queries older than 30 seconds
refetch, and only those on screen. Flicking between tabs every few seconds
costs nothing. The rejected alternative, `false`, would trade a few cheap
requests for decisions made on stale data.

## 5. Mutations are not retried

**📍 Open:** [`web/src/lib/query/client.ts:61`](../../web/src/lib/query/client.ts#L61)

A query is a read: repeating it is harmless. A mutation is a write, and a
write that timed out may have **succeeded** on the server before the
connection dropped. Retrying it automatically could create a second job post,
or append a second `application.rescored` event. So mutations default to no
retries, and the user decides, having seen the error.

## 6. The query key convention

**📍 Open:** [`web/src/lib/query/keys.ts:4`](../../web/src/lib/query/keys.ts#L4)

A query key is an array that identifies a piece of cached data. Two
components with the same key share one cache entry and one request.

The convention, written at the top of the file:

```
[resource]                        everything about it
[resource, "list"]                every list
[resource, "list", filters]       one list
[resource, "detail", id]          one record
[resource, "detail", id, part]    something belonging to it
```

The brief suggested `['jobPosts', id]`. The extra `"list"` / `"detail"`
segment is deliberate:

- **Invalidation works by prefix.** `invalidateQueries({ queryKey: ["jobPosts", "list"] })`
  refreshes every list and leaves cached details alone. With
  `['jobPosts', id]` beside `['jobPosts']` there is no key that means "the
  lists but not the details".
- **No collisions.** In `['jobPosts', x]`, `x` could be an ID or a word like
  `"drafts"`. The segment makes the position's meaning fixed.
- **Filters last, as one object.** TanStack Query hashes keys with object
  properties sorted, so `{status, sort}` and `{sort, status}` are one entry.

Keys are built by functions in one object (`queryKeys.jobPosts.detail(id)`),
never typed inline. `as const` makes TypeScript infer the exact tuple type
(`readonly ["jobPosts", "detail", string]`) rather than `string[]`, so a typo
in a segment is a compile error.

## 7. Invalidation, and why mutations need it

**📍 Open:** [`web/src/hooks/use-job-posts.ts:31`](../../web/src/hooks/use-job-posts.ts#L31)

The cache holds copies of server data. A mutation changes the server, and
the copies do not know: after creating a role, the cached roles list is
missing a row, and with a 30-second `staleTime` it would stay that way.

**Invalidation** marks cached entries stale and immediately refetches those
currently on screen; the rest refetch when next used. `useCreateJobPost`
does two things on success:

1. `setQueryData` ([line 39](../../web/src/hooks/use-job-posts.ts#L39))
   writes the response into the new post's detail entry. The response *is*
   the post, so opening it next needs no request.
2. `invalidateQueries` ([line 43](../../web/src/hooks/use-job-posts.ts#L43))
   on `["jobPosts", "list"]`, because a list's contents depend on things the
   client cannot reproduce exactly (sort order, server-computed counts).
   Splicing the row in by hand would duplicate server logic in the browser.

Returning the `invalidateQueries` promise from `onSuccess` makes the mutation
stay pending until the list has refetched, so a page that navigates on
success lands on a list that already has the new row.

## 8. One pattern for loading, empty and error

**📍 Open:** [`web/src/components/states/states.tsx:94`](../../web/src/components/states/states.tsx#L94)

Every page needs the same four branches, and each page written separately
gets them subtly different: one forgets the empty case, another shows an
error over data that was fine a second ago. `QueryView` decides once:

| State | Shown |
|---|---|
| no data yet, pending | the page's own skeleton, passed as `loading` |
| no data, failed | `ErrorState`: message, request ID, retry |
| data, empty | the page's `EmptyState` |
| data | `children(data)` |

The test at [line 109](../../web/src/components/states/states.tsx#L109) is
on `data === undefined`, not on `isError`. Once a query has data, a failed
*background* refetch keeps the data on screen: the query cache's `onError`
([`client.ts:31`](../../web/src/lib/query/client.ts#L31)) shows a toast
instead. Replacing a working table with an error because one refresh failed
would be worse than showing slightly old data.

`ErrorState` uses `role="alert"` so a screen reader announces the failure,
and shows the server's `request_id`, which is what an operator needs to find
the request in the logs (module 10.2). `EmptyState` takes an action so the
empty screen can offer the next step, not just say "nothing here".

`children` is a function (a *render prop*) so that it runs only when `data`
exists, and TypeScript knows `data` is `T`, not `T | undefined`, inside it.

## 9. The provider, and devtools in development only

**📍 Open:** [`web/src/components/providers.tsx:12`](../../web/src/components/providers.tsx#L12)

The `QueryClient` is created with `useState(makeQueryClient)`
([line 25](../../web/src/components/providers.tsx#L25)). Passing the
function, not its result, makes React call it once, on first render. A
client created at module level would be shared by every request the server
renders: one user's cached data could appear in another's HTML.

The devtools are mounted only in development, in two layers:

- `process.env.NODE_ENV` is replaced by a literal at build time, so a
  production build contains `false ? dynamic(...) : () => null`. The bundler
  removes the dead branch, and the devtools code never ships.
- `next/dynamic` with `ssr: false` loads them only in the browser, inside
  their own `<Suspense>` ([line 32](../../web/src/components/providers.tsx#L32))
  so that rendering them client-side does not hold back anything else.

The package also no-ops itself outside development, but relying on that
still bundles the import; the explicit condition does not.

## 10. The API client and the refresh race

**📍 Open:** [`web/src/lib/api/client.ts:79`](../../web/src/lib/api/client.ts#L79)

`api()` is the one function that talks to the backend. It adds the bearer
token, serialises JSON, and turns every failure into an `ApiError` carrying
the envelope's `code`, `message`, `details` and `request_id`. A failure
before any response (API down, DNS, CORS) becomes status 0, code
`network_error`, so even that has a readable message. An `AbortError` is
re-thrown untouched ([line 66](../../web/src/lib/api/client.ts#L66)): it is
TanStack Query cancelling a request on purpose, not a failure to report.

Access tokens last 15 minutes. On a 401 `token_expired`, the client uses the
refresh token and repeats the request once. The trap is concurrency: when a
page loads, several queries can hit the expired token at the same moment.
If each refreshed, the server, which **rotates** refresh tokens and treats
reuse of an old one as theft (module 3), would see the same token used three
times and revoke the whole session. `refreshing` holds the one refresh in
progress; every caller awaits the same promise.

This does not cover two *tabs* refreshing at the same instant; each has its
own module state. That is rare (both tabs must make a request in the same
few milliseconds after expiry) and ends in a re-login, not a leak.

## 11. The session, and where the token lives

**📍 Open:** [`web/src/lib/auth/session.ts:87`](../../web/src/lib/auth/session.ts#L87)

The tokens are kept in `localStorage`. The trade-off, stated plainly: any
script running on the page can read `localStorage`, so an XSS bug would
expose the tokens. The stronger design is an httpOnly cookie set by a
server-side route (a backend-for-frontend), which scripts cannot read. That
needs the API to accept cookies and a CSRF defence, a larger change than this
module; it is noted as the upgrade path. React escapes everything it renders
and the app uses no `dangerouslySetInnerHTML`, which is what keeps XSS out.

`useSession` uses `useSyncExternalStore`, React's hook for reading a store
that lives outside React. It takes a `subscribe` function, a `getSnapshot`
for the browser, and a third function for the server, which has no
`localStorage` and so renders signed-out. A `storage` event listener
([line 68](../../web/src/lib/auth/session.ts#L68)) keeps tabs in step: signing
out in one tab signs out every tab.

## 12. A browser-only gate with `use(browser())`

**📍 Open:** [`web/src/components/layout/admin-shell.tsx:30`](../../web/src/components/layout/admin-shell.tsx#L30)

The admin shell must know whether someone is signed in before it renders
anything, and only the browser can know. Rendering it on the server too
would produce a signed-out frame that then switches to signed-in: a flash,
and a redirect effect that could fire before the real session is read.

React 19.3's `browser()` returns a value that `use()` suspends on during
server rendering and passes through in the browser. The shell therefore
renders only in the browser, where its first render already has the real
session; the server sends the `<Suspense>` fallback from
[`admin/layout.tsx`](../../web/src/app/admin/layout.tsx#L11) instead. The
redirect to `/login?next=…` is in an effect
([line 37](../../web/src/components/layout/admin-shell.tsx#L37)), because
navigating during render is a side effect React may repeat.

The gate is a convenience, not security. Every admin endpoint checks the
token and the role on the server (module 3); a user who bypassed the gate
would get 401s and 403s, not data.

## 13. Instant-navigation validation, scoped

**📍 Open:** [`web/next.config.ts:15`](../../web/next.config.ts#L15)

With Cache Components, Next 16 checks in development that navigating to each
page shows UI immediately, by prerendering it on the server. Every admin page
failed that check: it sits under the browser-only shell, so on the server it
never renders, and the dev overlay reported "Could not validate `instant`"
on each.

That is not a defect to fix but a property of this design: the admin console
is a client-rendered app behind a browser-held session. The validation level
is set to `manual-warning`, which validates only segments that export
`instant`. The alternative, `export const instant = false` on every admin
layout and page, says the same thing in many places, and a new page that
forgot it would bring the warning back.

## 14. The open redirect on the login page

**📍 Open:** [`web/src/app/login/login-form.tsx:16`](../../web/src/app/login/login-form.tsx#L16)

The shell sends a signed-out visitor to `/login?next=/admin/roles` so they
land back where they were. Following `next` blindly is an **open redirect**:
a phishing email could link to the real login page with
`next=https://evil.example`, and after a genuine sign-in the user would be
sent to a copy of the site. `safeNext` accepts only a path on this site. A
leading `//` is refused too, because `//evil.example` is a
protocol-relative URL to another host.

`next` is read from `window.location` inside the submit handler rather than
with `useSearchParams`, which would make the page wait on the URL during
prerendering for a value only needed on submit.

## 15. The new endpoint: `GET /v1/admin/job-posts`

**📍 Open:** [`internal/store/job_posts.go:344`](../../internal/store/job_posts.go#L344)

`useJobPosts` needs every post, drafts included, and the API had no such
route: `GET /v1/job-posts` is the **public** listing, open posts only, and
deliberately reads no token at all (module 4.4). Making it role-aware would
put an auth branch into an endpoint whose safety comes from not having one.
So the admin list is a separate route, registered at
[`router.go:173`](../../internal/api/router.go#L173) behind `RequireRole(admin)`.

Each row carries two figures the roles list (11.3) shows: the application
count and the remaining vacancies. One `LATERAL` subquery per post computes
both in a single pass over `applications_job_post_id_status_idx`, with
`count(*) FILTER (WHERE status = 'offer')` for the offers. `FILTER` is one
aggregate with a condition attached, so the index is read once for both
counts instead of once per figure.

The list is **not paged**, and that is a judgement about the data, not an
oversight. An organisation has tens of open and recent roles, not thousands,
and the roles screen filters and sorts in the browser. Applications are the
opposite case, and 11.7 pages them on the server.

## 16. Embedding a struct to share JSON fields

**📍 Open:** [`internal/api/job_post_handler.go:91`](../../internal/api/job_post_handler.go#L91)

A list row is a job post without its questions, plus two counts. The fields
were moved into `jobPostFields`, which both `jobPostResponse` and
`jobPostSummaryResponse` **embed**: a field declared with a type and no name.

Go promotes an embedded struct's fields to the outer struct, so
`resp.Title` works, and `encoding/json` encodes them as if declared in the
outer struct. The JSON stays flat, with no `jobPostFields` key. The
rejected alternative was embedding `jobPostResponse` itself and hiding
`questions`: `json:"-"` on a shadowing field does not work, because the
encoder drops `-` fields before deciding which name wins, so the embedded
`questions` would still be emitted.

The handler test decodes the response into `map[string]any`, so it checks
the keys a client actually sees, including that `questions` is absent.

## 17. Verification

| Command or check | Result |
|---|---|
| `go test ./internal/api/` | ok, including `TestListJobPostSummaries`, `TestListJobPostSummariesIsAdminOnly`, and the spec-drift tests that require the new route in `openapi.yaml` |
| `go test -tags integration -run TestListJobPostSummaries ./internal/store/` (against `make up`) | ok: a draft is listed, every status counts as an application, two offers on one vacancy show 0 remaining, newest first |
| `curl /v1/admin/job-posts` as admin | 4 posts; the bulk demo post shows `application_count: 500` |
| `npx tsc --noEmit`, `npm run lint` (in `web/`) | no errors, no warnings |
| Browser: visit `/admin/roles` signed out | redirected to `/login?next=%2Fadmin%2Froles` |
| Browser: sign in as the seed admin | lands on `/admin/roles`, list rendered from the new endpoint |
| Dev overlay after the validation change | no issues; server log clean |
| `npm run build` (in `web/`) | see the commit body |
| `make fmt && make lint && make test && go build ./...` | see the commit body |
