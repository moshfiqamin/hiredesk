# Module 10, part 2: Structured logging and request correlation

`log/slog` throughout, a request ID on every request, response and log line,
and an audit that no credential is ever logged.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | Why structured JSON beats printf | [`internal/platform/logger.go:62`](../../internal/platform/logger.go#L62) |
| 2 | Level and format from the environment | [`internal/config/config.go:316`](../../internal/config/config.go#L316) |
| 3 | The request ID middleware | [`internal/api/request_id.go:25`](../../internal/api/request_id.go#L25) |
| 4 | How context carries the ID without polluting signatures | [`internal/platform/logger.go:97`](../../internal/platform/logger.go#L97) |
| 5 | The access log | [`internal/api/access_log.go:85`](../../internal/api/access_log.go#L85) |
| 6 | The ID in every error body | [`internal/api/errors.go:53`](../../internal/api/errors.go#L53) |
| 7 | Audit: where a secret could leak, and what stops it | [`internal/platform/logger.go:25`](../../internal/platform/logger.go#L25) |
| 8 | Debugging production with a request ID | — |
| 9 | Verification | — |

---

## 1. Why structured JSON beats printf

**📍 Open:** [`internal/platform/logger.go:62`](../../internal/platform/logger.go#L62)

Before this module, a failure was logged as:

```
2026/11/12 09:30:01 internal error: POST "/v1/auth/register": insert user: …
```

and now as:

```json
{"time":"…","level":"ERROR","msg":"internal error","method":"POST","path":"/v1/auth/register","error":"insert user: …","request_id":"Q7…","user_id":"5f0d…"}
```

With one user, the first is fine. Once there are many requests at once from
many users:

- **Lines interleave.** Twenty requests in flight write their lines in
  arbitrary order. Without a shared key per request, which "insert user"
  failure belongs to which 500 is guesswork. Structured logs carry
  `request_id` on every line, so filtering by it reassembles one request's
  story.
- **Queries, not greps.** A collector (Loki, CloudWatch, Datadog, Elastic)
  parses JSON into fields. `status >= 500 AND route = "/v1/applications/:id/score"`
  is a query. Against printf text it is a regular expression that breaks the
  first time someone rewords a message.
- **Aggregation.** `route` is the pattern, not the path, so "p95
  `duration_ms` per endpoint" is one group-by. Paths with IDs in them cannot
  be grouped.
- **No injection.** A path such as `/x%0Alevel=ERROR msg=forged` cannot add
  a fake line: JSON escapes the newline inside a string value. printf output
  needed `%q` everywhere to get the same protection, and one forgotten `%s`
  undid it.
- **Typed values.** `duration_ms` is a number and `status` an integer, so a
  dashboard can compute with them without parsing.

**slog in one paragraph.** `log/slog` (Go 1.21+) separates a **Logger** (the
API you call: `Info`, `ErrorContext`, `With`) from a **Handler** (what turns
a record into bytes). `slog.NewJSONHandler` and `slog.NewTextHandler` ship
with the standard library. Attributes are key/value pairs, and `slog.String`,
`slog.Int` and friends build them without the reflection that loose `any`
pairs need. `slog.SetDefault` installs a logger process-wide
([`cmd/api/main.go:52`](../../cmd/api/main.go#L52)), and also routes the old
`log` package through it.

## 2. Level and format from the environment

**📍 Open:** [`internal/config/config.go:316`](../../internal/config/config.go#L316)

`LOG_LEVEL` is `debug|info|warn|error`, parsed into a `slog.Level` (an `int`)
at startup, so a typo refuses to boot rather than logging everything.
`LOG_FORMAT` defaults to `text` in development, which is a person reading a
terminal, and `json` otherwise. `text` in production is a startup error:
"pretty in development only" is enforced, not hoped for.

Rejected: a hand-coloured "pretty" handler. It is more code to maintain, and
`slog.TextHandler`'s `key=value` output is already readable.

Gin's debug mode prints routes as bare text. `gin.DebugPrintRouteFunc` sends
them through slog at debug level instead
([`cmd/api/main.go:110`](../../cmd/api/main.go#L110)), so every line on
stdout stays parseable.

## 3. The request ID middleware

**📍 Open:** [`internal/api/request_id.go:25`](../../internal/api/request_id.go#L25)

1. **Accept an inbound `X-Request-ID`**, if it passes
   `validRequestID` ([`:50`](../../internal/api/request_id.go#L50)): 1–128
   characters of letters, digits and `._:-`. A load balancer or a frontend
   can then use one ID across several services. Anything else is replaced,
   because the value is written into every log line, and a client-chosen
   newline or quote there is a log-forging tool.
2. **Otherwise generate one** with `crypto/rand.Text()`
   ([`:31`](../../internal/api/request_id.go#L31)): 26 base32 characters, 130
   random bits. Not `math/rand`, which is predictable, and not a counter,
   which leaks request volume.
3. **Store it in the request's context**, not in `gin.Context`'s key-value
   map (section 4 explains why).
4. **Echo it** in the `X-Request-ID` response header, set before the handler
   runs so even a panic's 500 carries it.

It is the first middleware in the chain
([`router.go:71`](../../internal/api/router.go#L71)), so everything after it
can see the ID.

## 4. How context carries the ID without polluting signatures

**📍 Open:** [`internal/platform/logger.go:97`](../../internal/platform/logger.go#L97),
then [`internal/platform/context.go:8`](../../internal/platform/context.go#L8)

**The problem.** A store function three calls below a handler fails and
logs. To include the request ID, the obvious options are bad: add a
`requestID string` parameter to every function in between (dozens of
signatures, all changed for logging), or pass a `*slog.Logger` down instead
(the same, with a different type).

**What `context.Context` is.** Every handler already passes `ctx` down: it
carries the request's cancellation and deadline, so a client that hangs up
cancels its database query. A context can also carry **request-scoped
values**: `context.WithValue(parent, key, value)` returns a child that
answers `ctx.Value(key)`. The ID rides on the `ctx` that is already being
passed, so no signature changes.

**The idiomatic slog pattern** has two halves:

1. **Put the value in the context once.** `platform.WithRequestID(ctx, id)`.
   The key is an unexported type, `type ctxKey int`
   ([`context.go:8`](../../internal/platform/context.go#L8)), so no other
   package can construct the same key and collide with or overwrite it. This
   is why Go discourages plain strings as context keys.
2. **Have the handler read it.** `contextHandler`
   ([`logger.go:97`](../../internal/platform/logger.go#L97)) wraps the JSON
   or text handler. Its `Handle` method
   ([`:103`](../../internal/platform/logger.go#L103)) receives the `ctx` that
   was given to `InfoContext` or `ErrorContext`, reads the request and user
   IDs, adds them to the record, and passes it on.

So a call site is just:

```go
slog.ErrorContext(ctx, "internal error", slog.String("error", err.Error()))
```

and the line carries `request_id` and `user_id` without the function knowing
either exists. The one discipline required: use the `…Context` variants and
pass the real `ctx`, not `context.Background()`.

**Struct embedding in the handler.** `contextHandler` embeds `slog.Handler`.
Embedding promotes the embedded value's methods, so `Enabled` comes for
free, and only `Handle`, `WithAttrs` and `WithGroup` are overridden.
`WithAttrs` and `WithGroup` must re-wrap the result. Otherwise
`logger.With("component", "x")` returns the *inner* handler, and the context
attributes silently disappear from that logger. `TestContextAttributesSurviveWith`
pins this.

**The user ID** is added the same way, by `RequireAuth` once the user is
loaded ([`middleware.go:85`](../../internal/api/middleware.go#L85)).
`c.Request = c.Request.WithContext(...)` replaces the request on the shared
`gin.Context`, so middleware further out, the access log included, sees it
after `c.Next()` returns.

**Rejected: a logger in the context** (`ctx = WithLogger(ctx, logger.With("request_id", id))`).
Also common, and it works. Rejected because every call site must fetch the
logger first, and code that forgets falls back to the global logger with no
ID. The handler approach cannot be forgotten at the call site.

## 5. The access log

**📍 Open:** [`internal/api/access_log.go:85`](../../internal/api/access_log.go#L85)

One line per request, written after `c.Next()` so the status is known:
`method`, `path`, `route`, `status`, `duration_ms`, `bytes`, `client_ip`,
plus `request_id` and `user_id` from the context. Level `ERROR` for 5xx,
`INFO` otherwise. 4xx are normal client behaviour, not server faults.
`slog.LogAttrs` ([`:110`](../../internal/api/access_log.go#L110)) takes typed
attributes, which avoids boxing every value into `any` on the hottest log
line.

It replaces `gin.Logger()`, which writes unstructured text *and the query
string*. It sits **outside** `Recovery`, so a panic is logged with the 500
it became. It also feeds `InFlight` ([`:17`](../../internal/api/access_log.go#L17)),
which graceful shutdown reads (part 4).

## 6. The ID in every error body

**📍 Open:** [`internal/api/errors.go:53`](../../internal/api/errors.go#L53)

`request_id` is now a fourth field of the envelope, always present and
always a string. All error responses go through one function, `write`, so
one line added it everywhere: unknown routes, 405s, 413s, 429s and panics.

**What this did to the 403/404 tests.** Decision D1 says a refused record and
a missing one must be byte-for-byte identical. Two requests now have two IDs.
The tests send a **fixed** `X-Request-ID`, so the comparison stays whole and
strict. The ID depends only on the request, never on the record, so it
reveals nothing.

## 7. Audit: where a secret could leak, and what stops it

**📍 Open:** [`internal/platform/logger.go:25`](../../internal/platform/logger.go#L25)

Every path by which a password, token, the JWT secret or an `Authorization`
header could reach the log, and its status:

| Where it could leak | Found | Done |
|---|---|---|
| `gin.Logger()` logs the URL *with its query string* | yes, if a client ever put a token in `?access_token=` | Replaced. The access log writes `URL.Path` only, and `TestAccessLogRecordsEveryField` sends `?refresh_token=leaky` and asserts it is absent |
| `gin.Recovery` dumps request headers on panic | it masks `Authorization`, but writes every other header, cookies included | Replaced. `Recovery` logs panic, method, path and stack, no headers |
| Request bodies (passwords, refresh tokens) | never logged anywhere | Kept that way. No middleware reads or logs a body |
| `Respond` logs `err.Error()` for a 500 | errors wrap context such as IDs and SQL errors, never a token: the token store works only with SHA-256 hashes | Checked every `fmt.Errorf` in `auth` and `store`: no plaintext token or password is ever formatted into an error |
| Refresh-token reuse warning | logs the affected user ID, client IP and the count revoked | Unchanged, now structured. No token or hash |
| Validation errors for `password` | the message is "must be at least 8 characters", never the value | Unchanged |
| `JWT_SECRET` too short | `WeakSecretError` reports the length, never the value | Unchanged (module 0) |
| Logging the config at startup | `*Config` holds the secret and `DATABASE_URL` (which contains the database password) | `Config.LogValue` ([`config.go:129`](../../internal/config/config.go#L129)) implements `slog.LogValuer` and lists only safe fields. `TestConfigLogValueHidesSecrets` |
| pgx connection errors | pgx v5 redacts the password from connection strings in its own errors | Relied on, noted |
| A future careless `slog.Info("x", "password", p)` | possible | **Safety net:** the handler's `ReplaceAttr` redacts any attribute whose key contains `password`, `secret`, `token`, `authorization`, `cookie`, `api_key`, `database_url` or `dsn` (`TestSensitiveKeysAreRedacted`) |

The safety net cannot see inside a struct logged as one value
(`slog.Any("req", request)`). That is why the rule is never to log a request,
a header map or a config struct whole, and why `Config` defines its own
`LogValue`.

**The end-to-end proof** is `TestLoginNeverLogsCredentials`
([`observability_test.go:251`](../../internal/api/observability_test.go#L251)).
At debug level, it runs register, login, a failed login, refresh, an
authenticated call, a replayed refresh token and logout through the
production router. It then fails if the output contains the password, the
wrong password, any access or refresh token, the JWT secret, or the string
`Bearer `. Against the running container, the same check (grep the last ten
minutes of logs during the browser walkthrough) found zero matches.

## 8. Debugging production with a request ID

A user reports: "Saving my rubric failed. It said *Something went wrong*,
request ID `QUDNKFH6XPX4VNZUE3XCTQQD4N`."

1. **Find every line of that request.** In the log tool, filter
   `request_id = "QUDNKFH6XPX4VNZUE3XCTQQD4N"`. Out of millions of lines,
   this returns the handful from one request: the access line (`PUT`,
   `/v1/job-posts/…/rubric`, `500`, `duration_ms=5003`) and the `internal
   error` line just before it with the wrapped cause, for example `replace
   rubric: … context deadline exceeded`.
2. **Know who and when.** The same lines carry `user_id` and the timestamp.
   There is no need to ask the user which account or what time, and no
   guessing between two admins who both saved at 14:02.
3. **Read the cause, not the symptom.** The client was told only
   `internal_error`. The log has the wrapped chain, written by `Respond`
   under the same ID, and says where it failed.
4. **Widen deliberately.** With the time and the route known, query
   `route = "/v1/job-posts/:id/rubric" AND status >= 500` over that window. Is
   it one user or everyone? Then look at the database logs for those
   seconds.
5. **Follow it across services.** Because an inbound `X-Request-ID` is kept,
   a frontend or gateway that sends one gets the same ID in its own logs.
   One ID then spans every hop.

Without the ID, step 1 is "grep for 500s around 2 pm and guess", which on a
busy server is not a method.

## 9. Verification

| Command or action | Result |
|---|---|
| `go test -race ./internal/platform ./internal/config ./internal/api` | pass; platform coverage 94.3% |
| `curl -si localhost:8080/healthz` | `X-Request-Id: F322C53HFBCWICNPYC4OTX57QY` header, 26 characters |
| Candidate requests the score in Swagger UI | body `request_id` `QUDNKFH6XPX4VNZUE3XCTQQD4N`; the server's access line has the same `request_id` and the candidate's `user_id` |
| `docker compose logs api --since 10m \| grep -ciE 'eyJhbGci\|candidate-password\|admin-password\|Bearer '` after the walkthrough | `0` |
| Startup log | `msg=starting config.port=8080 … config.allowed_origins=[]`, with no secret and no `DATABASE_URL` |
