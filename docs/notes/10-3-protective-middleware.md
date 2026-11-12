# Module 10, part 3: Protective middleware

A rate limit on `/v1/auth/*`, a body-size limit, explicit CORS, and a
recovery middleware that keeps one panic from becoming everyone's problem.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | The middleware order | [`internal/api/router.go:71`](../../internal/api/router.go#L71) |
| 2 | How a token bucket works | [`internal/api/ratelimit.go:48`](../../internal/api/ratelimit.go#L48) |
| 3 | The per-IP map is a memory leak, unless bounded | [`internal/api/ratelimit.go:63`](../../internal/api/ratelimit.go#L63) |
| 4 | 429, `Retry-After`, and IPv6 | [`internal/api/ratelimit.go:141`](../../internal/api/ratelimit.go#L141) |
| 5 | The body limit, and why 1 MiB | [`internal/api/body_limit.go:19`](../../internal/api/body_limit.go#L19) |
| 6 | CORS, and why a wildcard with credentials is dangerous | [`internal/api/cors.go:38`](../../internal/api/cors.go#L38) |
| 7 | Recovery, and what happens to the other requests | [`internal/api/recovery.go:20`](../../internal/api/recovery.go#L20) |
| 8 | The temporary panic route | — |
| 9 | Verification | — |

---

## 1. The middleware order

**📍 Open:** [`internal/api/router.go:71`](../../internal/api/router.go#L71)

```
RequestID → AccessLog → Recovery → CORS → BodyLimit → (route: RateLimit → RequireAuth → RequireRole → handler)
```

Each position is deliberate. The ID comes first, so everything has one. The
access log sits outside recovery, so it logs the 500 a panic became. CORS
comes before any refusal: a 401 or 413 without CORS headers is shown to a
browser script as an opaque network error. The body limit comes before
anything reads a body. Engine-level middleware also runs for unknown routes
and wrong methods, so a CORS preflight to any path is answered.

## 2. How a token bucket works

**📍 Open:** [`internal/api/ratelimit.go:48`](../../internal/api/ratelimit.go#L48)

Picture a bucket that holds at most **10 tokens** and starts full. Each
request takes one token. With no token left, the request is refused. Tokens
drip back in at a steady rate, here **one every six seconds** (10 per
minute), and the bucket never holds more than 10.

So a client may send 10 requests in a burst, and after that one every six
seconds. A client that pauses for a minute is back to a full bucket. The
policy allows natural bursts, such as a user mistyping a password three
times quickly, while capping the sustained rate an attacker gets: 10 guesses
a minute, 14,400 a day, from one address.

`golang.org/x/time/rate` implements this. `rate.NewLimiter(limit, burst)`
takes the refill rate (tokens per second) and the bucket size.
`rate.Every(window / requests)` ([`:51`](../../internal/api/ratelimit.go#L51))
converts "one per six seconds" into that rate. It does not run a timer: it
records when it last updated, and computes how many tokens have dripped in
since, on each call. A million idle limiters cost no CPU.

Rejected alternatives: a **fixed window** ("10 per calendar minute") lets 20
requests through in two seconds across a minute boundary. A **sliding log**
is exact, but stores every request's timestamp.

## 3. The per-IP map is a memory leak, unless bounded

**📍 Open:** [`internal/api/ratelimit.go:63`](../../internal/api/ratelimit.go#L63)

One bucket per IP means a `map[string]*bucket`. The careless version adds an
entry for every new address and never removes one. Each entry is only about
100 bytes, but:

- **Over time**, every address that ever touched `/v1/auth` stays in memory:
  phones changing networks, NAT pools, crawlers. The map grows for the life
  of the process.
- **Under attack**, it grows on demand. An attacker who cycles source
  addresses (a botnet, or one IPv6 host with 2^64 addresses) adds an entry
  per request, until the process runs out of memory. The rate limiter
  becomes the denial-of-service vector it was meant to prevent.

**How it is bounded here, twice:**

1. **Idle sweep, which forgets nothing.** A bucket unused for a whole window
   (one minute) has refilled completely. It is *identical* to the fresh
   bucket a new request from that IP would get, so deleting it loses no
   information. `sweep` ([`:112`](../../internal/api/ratelimit.go#L112))
   deletes every such bucket. It runs lazily, inside `allow`, at most once a
   window ([`:74`](../../internal/api/ratelimit.go#L74)). There is no
   background goroutine to start, stop or leak. In steady state the map
   holds only the addresses active in the last minute.
2. **A hard cap**, `authMaxClients = 10,000`
   ([`cmd/api/main.go:28`](../../cmd/api/main.go#L28)), about 1 MB. If more
   distinct addresses than that are active *within one minute*, sweeping
   cannot help: none are idle. Then one random bucket is evicted
   ([`:90`](../../internal/api/ratelimit.go#L90)). Go randomises map
   iteration order, so taking the first key from a `range` is a random
   choice in O(1).

**Why evict, not refuse new addresses.** Refusing every new IP once the map
is full hands an attacker with 10,000 addresses a switch that locks *every*
legitimate user out of logging in. Eviction degrades instead: an evicted
attacker's bucket resets, but an attacker with 10,000 addresses already has
100,000 attempts a minute, so per-IP limiting was no longer what stood in
their way.

`TestRateLimitSweepsIdleBuckets`
([`protection_test.go:76`](../../internal/api/protection_test.go#L76))
fills 5,000 buckets, advances a fake clock one minute, and finds one left.
`TestRateLimitNeverExceedsItsCap`
([`:98`](../../internal/api/protection_test.go#L98)) sends 1,000 addresses
at a cap of 50, and the map never exceeds 50.

**The mutex.** Handlers run on many goroutines at once, and Go maps are not
safe for concurrent writes: the runtime aborts with `concurrent map writes`.
One `sync.Mutex` guards the map. `TestRateLimitIsSafeConcurrently` fires 50
goroutines at one IP under `-race` and expects exactly 10 to pass.

## 4. 429, `Retry-After`, and IPv6

**📍 Open:** [`internal/api/ratelimit.go:141`](../../internal/api/ratelimit.go#L141)

`ReserveN` ([`:103`](../../internal/api/ratelimit.go#L103)) is used instead
of `Allow` because a reservation reports *how long* until a token is
available, which `Retry-After` needs. A refused reservation is cancelled at
once, so refusals do not dig the client deeper into debt. The header is whole
seconds rounded **up**: rounding down sends the client back a moment too
early, to be refused again.

**IPv6 by /64** ([`:153`](../../internal/api/ratelimit.go#L153)). An ISP
typically gives one subscriber a /64, which is 2^64 addresses. Limiting
single IPv6 addresses would let one client rotate endlessly. IPv4 is used
as-is.

**The client IP comes from `c.ClientIP()`**, with `SetTrustedProxies(nil)`
([`router.go:87`](../../internal/api/router.go#L87)), so it is the TCP peer
address and cannot be spoofed with `X-Forwarded-For`. Behind a load balancer
every request would seem to come from the balancer's address. The fix is to
trust that proxy's CIDR, not to trust the header.

**Scope.** In-memory and per process. Three replicas each allow 10 a minute.
For several instances the bucket state must be shared (Redis with a Lua
script, or the gateway's limiter).

## 5. The body limit, and why 1 MiB

**📍 Open:** [`internal/api/body_limit.go:19`](../../internal/api/body_limit.go#L19)

Without a limit, a client can send a 10 GB body and the JSON decoder will try
to read it. `http.MaxBytesReader` wraps the body so the read that crosses the
limit fails with `*http.MaxBytesError`, and tells the server to close the
connection rather than drain the rest.

Two paths reach 413:

- `Content-Length` larger than the limit is refused before a byte is read
  ([`BodyLimit`, `:28`](../../internal/api/body_limit.go#L28)).
- A chunked body has no length. Its oversize is discovered mid-read, and
  `decodeJSON` turns the wrapped `*http.MaxBytesError` into the same 413
  with `errors.As` ([`validation.go:62`](../../internal/api/validation.go#L62)).
  Before this module it was a 400 `malformed_request`, which blamed the
  client's JSON for a size problem.

**Why 1 MiB.** The largest legitimate body is an application submission. A
form may require a text answer of up to 10,000 characters
(`MaxQuestionMinLength`), and a character is at most four bytes in UTF-8, so
about 40 KB per long answer. A long form with several such answers is still
in the low hundreds of kilobytes. 1 MiB leaves comfortable headroom for that
and caps what one hostile request can make the server read and allocate.
64 KB would refuse a legitimate long form. 10 MB would multiply the cost of a
flood by ten for nothing.

## 6. CORS, and why a wildcard with credentials is dangerous

**📍 Open:** [`internal/api/cors.go:38`](../../internal/api/cors.go#L38)

**What CORS is.** Browsers enforce the same-origin policy: a script on
`https://evil.example` may *send* a request to this API, but may not *read*
the response unless the API says that origin may. CORS headers are how the
API says so. CORS relaxes a browser protection. It never adds protection
against curl or a server, which ignore it entirely.

**What this middleware does.** Origins come from `ALLOWED_ORIGINS`, validated
at startup ([`config.go:342`](../../internal/config/config.go#L342)): exact
`scheme://host[:port]`, no paths, and **no `*`, which is refused at boot**.
For a listed origin it echoes that one origin
([`:63`](../../internal/api/cors.go#L63)), allows credentials, and exposes
`X-Request-ID` and `Retry-After` to scripts. A preflight (`OPTIONS` with
`Access-Control-Request-Method`, [`:70`](../../internal/api/cors.go#L70)) is
answered with the five methods the API uses and the three request headers it
reads: `Authorization`, `Content-Type`, `X-Request-ID`. An unlisted origin
gets no CORS headers at all, and the browser withholds the response. `Vary:
Origin` keeps a shared cache from serving one origin's answer to another.

**Why a wildcard with credentials is dangerous.** The spec already forbids
`Access-Control-Allow-Origin: *` together with
`Access-Control-Allow-Credentials: true`, and browsers reject it. So the
"wildcard" people deploy is the workaround: **reflect whatever `Origin`
arrives** and allow credentials. That enables this attack:

1. A user is signed in to the real app, which stores its auth in a cookie
   (or any credential the browser attaches automatically).
2. They visit `https://evil.example`, perhaps through a link in an email.
3. Its script calls `fetch("https://api.hiredesk.example/v1/applications",
   {credentials: "include"})`. The browser attaches the user's cookie.
4. The API reflects `Origin: https://evil.example` and says credentials are
   allowed, so the browser **hands the response to the attacker's script**:
   every application, score and review that user can see.
5. The script can also send writes (change statuses, file reviews) and read
   the results.

That is cross-origin data theft, with the user's own session. The only
defence is an explicit allowlist. HireDesk sends a bearer token, not a
cookie, so a browser does not attach it on its own, which makes this API less
exposed than a cookie-based one. "Allow credentials" was requested so a future
frontend can use cookies. The allowlist is what makes that safe to switch on.

## 7. Recovery, and what happens to the other requests

**📍 Open:** [`internal/api/recovery.go:20`](../../internal/api/recovery.go#L20)

**How a panic travels.** `net/http` serves each connection on its own
goroutine. A panic unwinds *that goroutine's* stack, running deferred
functions as it goes. `recover()`, called inside a deferred function on the
same goroutine, stops the unwinding and returns the panic value. A `recover`
in another goroutine sees nothing. That is why recovery must be middleware
that runs inside every request.

**With this recovery**, the panic stops at `Recovery`. The client gets a 500
envelope with its `request_id`, and the log gets the panic value and
`debug.Stack()` ([`:45`](../../internal/api/recovery.go#L45)) under the same
ID. The goroutine returns normally, and **every other in-flight request is
untouched**: they are on other goroutines, never involved.
`TestPanicDoesNotDisturbOtherRequests`
([`protection_test.go:366`](../../internal/api/protection_test.go#L366))
holds one request mid-handler, panics another, and checks the first still
completes with its body. It ran live in the container too (section 8).

**Without any recovery in Gin**, there are two layers to know about:

- `net/http` itself recovers a panicking handler. It logs the stack to
  stderr, **closes that connection**, and the client sees a dropped
  connection or `EOF`, with no status and no envelope. Other requests survive.
  So "a panic kills the server" is not true of a plain `net/http` handler.
- **A panic on a goroutine the handler started** (`go sendEmail()`) is
  *not* covered by anything. No recovery, Gin's or `net/http`'s, sits on that
  goroutine's stack. An unrecovered panic on any goroutine terminates the
  **whole process**, and every in-flight request on it dies mid-response.
  Background goroutines need their own `defer recover()`.

So recovery middleware is what turns "connection dropped, stack on stderr"
into "a proper 500 the client can report, logged with its request ID". It
does not save you from panics in goroutines you spawn yourself.

**Two edge cases handled.** `http.ErrAbortHandler`
([`:31`](../../internal/api/recovery.go#L31)) is net/http's sentinel for "drop
this connection quietly", so it is re-panicked for the server to handle. And
if the handler had already started writing, a status can no longer be sent,
so the chain is aborted and the client sees a truncated response.

Why not `gin.Recovery()`: it answers with an empty 500 body, and writes the
request headers to the log, masking `Authorization` but not cookies.

## 8. The temporary panic route

To prove recovery in the real binary, not only in a test,
`GET /debug/panic` was added to the router, the image rebuilt, and:

```bash
(printf '{"email":"admin@hiredesk.test",'; sleep 4; printf '"password":"admin-password"}') \
  | curl -s -X POST -T - -H 'Content-Type: application/json' -H 'X-Request-ID: slow-inflight' localhost:8080/v1/auth/login &
sleep 1; curl -si localhost:8080/debug/panic -H 'X-Request-ID: panic-demo'
```

The first command streams a login body in two halves, four seconds apart,
using curl's chunked upload, so that request is in flight. The second hits
the panic route while it waits.

| Observed | |
|---|---|
| Panic response | `500` with `{"error":{"code":"internal_error",…,"request_id":"panic-demo"}}` |
| In-flight login | `200` after 4.40 s, with tokens |
| `curl /healthz` afterwards | `{"status":"ok"}`, container `healthy` |
| Log | `level=ERROR msg="panic recovered" panic="temporary test route: deliberate panic" … stack="goroutine 16 [running]:…"`, `request_id=panic-demo`, then the access line `status=500 request_id=panic-demo` |

The route was then removed, the image rebuilt, and `GET /debug/panic` returned
`404 route_not_found`. `grep -rn debug/panic` over the Go sources finds
nothing.

## 9. Verification

| Command | Result |
|---|---|
| 11 × `POST /v1/auth/login` with a wrong password | `401` ×10, then `429` with `Retry-After` and `{"error":{"code":"rate_limited",…,"details":{"retry_after_seconds":…}}}` |
| 2 MiB body to `POST /v1/job-posts` | `413` `payload_too_large`, `limit_bytes: 1048576`, before authentication |
| `ALLOWED_ORIGINS='*'` at startup | `configuration error: invalid value for ALLOWED_ORIGINS: "*": wildcards are not allowed; list each origin explicitly`, exit 1 |
| Preflight from `http://localhost:3000` with it allowed | `204`, `Allow-Origin: http://localhost:3000`, `Allow-Credentials: true`, five methods, three headers, `Max-Age: 600` |
| `GET /v1/me` from that origin without a token | `401` that still carries `Allow-Origin` |
| Preflight from `https://evil.example` | no `Access-Control-Allow-*` headers |
| `go test -race ./internal/api` | pass; coverage 91.4% |
