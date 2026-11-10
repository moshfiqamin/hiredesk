# Module 3 — Authentication

This note covers password hashing (sections 1–10), access tokens (sections
11–17), the error format every endpoint shares (sections 18–25), registration
and login (sections 26–35), and refresh tokens, logout, the current-user
endpoint and the seed command (sections 36–45).

**How to read this.** Every section names the exact file and line to open.
Click the link, read the code, then come back.

---

## Contents

| # | Topic | Code |
|---|---|---|
| [1](#1-what-a-hash-function-is-and-why-the-password-is-never-stored) | What a hash function is, and why the password is never stored | [`password.go:48`](../../internal/auth/password.go#L48) |
| [2](#2-why-slow-is-the-feature) | Why slow is the feature | [`password.go:82`](../../internal/auth/password.go#L82) |
| [3](#3-the-cost-parameter-and-what-12-means) | The cost parameter, and what 12 means | [`password.go:21`](../../internal/auth/password.go#L21) |
| [4](#4-the-salt-and-why-two-hashes-of-one-password-differ) | The salt, and why two hashes of one password differ | [`password_test.go:88`](../../internal/auth/password_test.go#L88) |
| [5](#5-reading-a-bcrypt-hash-string) | Reading a bcrypt hash string | [`password_test.go:16`](../../internal/auth/password_test.go#L16) |
| [6](#6-verify-and-the-time-it-takes) | `Verify`, and the time it takes | [`password.go:94`](../../internal/auth/password.go#L94) |
| [7](#7-two-things-bcrypt-does-that-hash-has-to-correct) | Two things bcrypt does that `Hash` has to correct | [`password.go:71`](../../internal/auth/password.go#L71) |
| [8](#8-sentinel-errors-and-errorsis) | Sentinel errors and `errors.Is` | [`password.go:33`](../../internal/auth/password.go#L33) |
| [9](#9-the-first-tests-in-the-repository) | The first tests in the repository | [`password_test.go:35`](../../internal/auth/password_test.go#L35) |
| [10](#10-direct-and-indirect-dependencies) | Direct and indirect dependencies | [`go.mod:8`](../../go.mod#L8) |
| [11](#11-the-three-parts-of-a-jwt) | The three parts of a JWT | [`token.go:113`](../../internal/auth/token.go#L113) |
| [12](#12-the-payload-is-readable-by-anyone) | The payload is readable by anyone | [`token_test.go:316`](../../internal/auth/token_test.go#L316) |
| [13](#13-what-the-signature-actually-proves) | What the signature actually proves | [`token.go:115`](../../internal/auth/token.go#L115) |
| [14](#14-the-algorithm-confusion-attack) | The algorithm-confusion attack | [`token.go:152`](../../internal/auth/token.go#L152) |
| [15](#15-why-only-fifteen-minutes) | Why only fifteen minutes | [`token.go:17`](../../internal/auth/token.go#L17) |
| [16](#16-refusing-to-start-on-a-weak-secret) | Refusing to start on a weak secret | [`config.go:278`](../../internal/config/config.go#L278) |
| [17](#17-go-idioms-appearing-for-the-first-time) | Struct embedding, methods, an injected clock | [`token.go:45`](../../internal/auth/token.go#L45) |
| [18](#18-one-shape-for-every-error) | One shape for every error | [`api/errors.go:29`](../../internal/api/errors.go#L29) |
| [19](#19-sentinel-errors-again-and-why-errorsis-exists) | Sentinel errors again, and why `errors.Is` exists | [`domain/errors.go:17`](../../internal/domain/errors.go#L17) |
| [20](#20-what-w-does-and-why-it-matters-here) | What `%w` does, and why it matters here | [`domain/errors_test.go:30`](../../internal/domain/errors_test.go#L30) |
| [21](#21-why-the-status-codes-live-in-api-and-not-in-domain) | Why the status codes live in `api`, not `domain` | [`api/errors.go:57`](../../internal/api/errors.go#L57) |
| [22](#22-unexpected-errors-log-everything-say-nothing) | Unexpected errors: log everything, say nothing | [`api/errors.go:89`](../../internal/api/errors.go#L89) |
| [23](#23-validation-errors-that-name-each-field) | Validation errors that name each field | [`domain/errors.go:52`](../../internal/domain/errors.go#L52) |
| [24](#24-errors-that-never-reach-a-handler) | Errors that never reach a handler | [`router.go:32`](../../internal/api/router.go#L32) |
| [25](#25-more-go-idioms-appearing-for-the-first-time) | A nil pointer that is not a nil error, `errors.As`, `TestMain` | [`domain/errors.go:73`](../../internal/domain/errors.go#L73) |
| [26](#26-one-request-three-layers) | One request, three layers | [`auth_handler.go:23`](../../internal/api/auth_handler.go#L23) |
| [27](#27-hand-written-sql-with-pgx) | Hand-written SQL with pgx | [`users.go:47`](../../internal/store/users.go#L47) |
| [28](#28-reading-the-postgresql-error-code-with-errorsas) | Reading the PostgreSQL error code with `errors.As` | [`users.go:123`](../../internal/store/users.go#L123) |
| [29](#29-struct-tags-and-how-two-packages-read-the-same-field) | Struct tags, and how two packages read the same field | [`auth_handler.go:74`](../../internal/api/auth_handler.go#L74) |
| [30](#30-validation-decode-clean-check-report) | Validation: decode, clean, check, report | [`validation.go:64`](../../internal/api/validation.go#L64) |
| [31](#31-why-the-response-is-its-own-struct) | Why the response is its own struct | [`auth_handler.go:92`](../../internal/api/auth_handler.go#L92) |
| [32](#32-registration) | Registration | [`auth_handler.go:142`](../../internal/api/auth_handler.go#L142) |
| [33](#33-login-and-user-enumeration) | Login, and user enumeration | [`auth_handler.go:192`](../../internal/api/auth_handler.go#L192) |
| [34](#34-the-router-and-v1) | The router, and `/v1` | [`router.go:55`](../../internal/api/router.go#L55) |
| [35](#35-go-idioms-appearing-for-the-first-time-in-the-handlers) | Interfaces, build tags, functions that return functions | [`health.go:14`](../../internal/api/health.go#L14) |
| [36](#36-two-tokens-two-jobs) | Two tokens, two jobs | [`refresh.go:15`](../../internal/auth/refresh.go#L15) |
| [37](#37-why-mathrand-is-dangerous-for-tokens) | Why `math/rand` is dangerous for tokens | [`refresh.go:33`](../../internal/auth/refresh.go#L33) |
| [38](#38-why-the-database-holds-a-hash-and-why-sha-256-is-enough) | Why the database holds a hash, and why SHA-256 is enough | [`refresh.go:58`](../../internal/auth/refresh.go#L58) |
| [39](#39-what-rotation-buys) | What rotation buys | [`tokens.go:104`](../../internal/store/tokens.go#L104) |
| [40](#40-a-reused-token-ends-every-session) | A reused token ends every session | [`auth_handler.go:281`](../../internal/api/auth_handler.go#L281) |
| [41](#41-two-requests-one-token-the-row-lock) | Two requests, one token: the row lock | [`tokens.go:128`](../../internal/store/tokens.go#L128) |
| [42](#42-logout) | Logout | [`auth_handler.go:331`](../../internal/api/auth_handler.go#L331) |
| [43](#43-the-auth-middleware-and-v1me) | The auth middleware, and `/v1/me` | [`middleware.go:24`](../../internal/api/middleware.go#L24) |
| [44](#44-the-seed-command) | The seed command | [`cmd/seed/main.go:95`](../../cmd/seed/main.go#L95) |
| [45](#45-go-idioms-appearing-for-the-first-time-in-the-session-code) | Transactions, nullable columns, arrays and slices, goroutines | [`tokens.go:108`](../../internal/store/tokens.go#L108) |

---

## 1. What a hash function is, and why the password is never stored

**📍 Open:** [`internal/auth/password.go:48`](../../internal/auth/password.go#L48)

A hash function takes input of any length and returns a fixed-length output,
with three properties that matter here:

- **Deterministic.** The same input always gives the same output. That is what
  makes login possible: hash what the user typed, compare with what is stored.
- **One-way.** Given the output there is no calculation that returns the
  input. The only way "back" is to guess an input, hash it, and compare.
- **Avalanche.** Changing one bit of input changes about half the output bits,
  so a near-miss guess looks nothing like a hit. There is no "getting warmer".

So `users.password_hash` holds the output, never the input, and login is
"hash the attempt, compare" rather than "compare the attempt with the stored
password".

**Why this way.** Plan for the day the `users` table is read by someone who
should not have it — a SQL injection, a backup left in a public bucket, a
laptop with a database dump on it. If the column holds passwords, every
account is compromised at that moment, and so is every *other* site where
those people reused the password. If it holds hashes, the attacker holds a
list of puzzles, each of which has to be solved by guessing.

It also limits what insiders see. Nobody with `make psql` access, and no log
line or error message, can reveal a password the system never kept.

**The rejected alternative: encryption.** Encrypting the column with a key
sounds similar but is reversible by design — whoever takes the database can
usually take the key too, since the application needs it at runtime. Hashing
has no key to steal. The application never needs the password back; it only
ever needs to answer "is this attempt the same?".

**Not to be confused with** the token hashing in
[`01-schema.md`](01-schema.md): refresh tokens there are stored as SHA-256,
a *fast* hash. That is correct for tokens and wrong for passwords — section 2
is the reason.

---

## 2. Why slow is the feature

**📍 Open:** [`internal/auth/password.go:82`](../../internal/auth/password.go#L82)

Since the only attack on a hash is guessing, the defender's one lever is the
price of a guess.

General-purpose hashes — MD5, SHA-1, SHA-256 — are built to be fast, because
their job is checksumming files and signing data. A single modern GPU computes
SHA-256 on the order of billions of times per second. Human-chosen passwords
are not random: the few hundred million most common ones cover a large share
of real accounts. Against a fast hash, that whole list is tried against a
stolen hash in under a second.

bcrypt is built to be slow, and to stay slow on hardware built to attack it.
At cost 12 it took **385 ms** per hash on this machine (section 3). The same
GPU that does billions of SHA-256 per second manages on the order of a
thousand bcrypt-12 guesses per second — bcrypt keeps rewriting a 4 KB table
as it runs, which defeats the massive parallelism GPUs depend on.

The asymmetry is what makes this a good trade:

| | Guesses needed | Cost of 385 ms each |
|---|---|---|
| A legitimate user logging in | 1 | not noticeable |
| An attacker, per account | millions to billions | years of compute |

Slowness is normally a defect because the cost falls on the user. Here almost
none of it does: the user pays once, the attacker pays once *per guess*.

**What it does not do.** bcrypt slows an attacker who has the hashes. It does
nothing about someone guessing through the login endpoint — that is a
rate-limiting problem. It also cannot save `password123`: a slow hash makes a
big guess list expensive, not a short one.

**The cost on our side.** A login now holds one CPU core for ~385 ms. A few
hundred simultaneous logins would saturate the server, so the login endpoint
is also a denial-of-service lever and needs a rate limit when it is built.

**Rejected: salted SHA-256.** Fixes rainbow tables (section 4), does nothing
about speed. **Rejected for now: Argon2id.** It is the current first
recommendation because it is also memory-hard, and it is in the same
`golang.org/x/crypto` module. bcrypt was chosen because it has one tuning
knob instead of three, a 25-year track record, and a self-describing hash
format every language can read. Section 3 explains why switching later does
not require resetting anyone's password.

---

## 3. The cost parameter, and what 12 means

**📍 Open:** [`internal/auth/password.go:21`](../../internal/auth/password.go#L21)

The cost is an **exponent**, not a count. bcrypt runs its expensive key setup
2<sup>cost</sup> times, so cost 12 is 4,096 rounds and every +1 doubles the
time. Measured on this machine (Apple silicon, one core, Go 1.27.3):

| Cost | Rounds | Time per hash |
|---|---|---|
| 4 (`bcrypt.MinCost`) | 16 | 1.5 ms |
| 10 (`bcrypt.DefaultCost`) | 1,024 | 95 ms |
| **12** | **4,096** | **385 ms** |
| 14 | 16,384 | 1.52 s |

The numbers are hardware-dependent: a CI runner or a small cloud instance
will be slower. The doubling is not — 10 → 12 → 14 is 4× each time above.

**Why 12.** The usual target is "as slow as login can tolerate", commonly
quoted as 250–500 ms. Cost 10, the library default, dates from slower
hardware. 14 makes every login a second and a half. 12 is in the range.

**Why the cost is stored in the hash.** Hardware gets faster, so the right
cost rises over time. Because every hash records the cost it was made with
(section 5), `Verify` needs no configuration: it reads the cost out of the
hash it was given. Changing `Cost` to 13 next year affects new hashes only,
and old ones keep verifying. The standard upgrade path is to re-hash at the
new cost when a user next logs in successfully — the one moment the server
has the plaintext.

**Why a constant rather than configuration.** `Cost` is a security decision,
not a deployment one. An environment variable would make it possible to run
production at cost 4 by accident.

---

## 4. The salt, and why two hashes of one password differ

**📍 Open:** [`internal/auth/password_test.go:88`](../../internal/auth/password_test.go#L88)

A salt is 16 random bytes generated fresh for each hash and mixed into the
calculation. The stored value is effectively `bcrypt(password, salt, cost)`,
not `bcrypt(password)`.

That is the whole explanation for the test at line 88: `Hash` is called twice
with the same password, each call draws a different salt from the operating
system's random source, so the two outputs share nothing after the `$2a$12$`
prefix.

**Without a salt, three things go wrong:**

1. **Identical passwords are visible.** Two users with the same password
   would have the same hash. An attacker sorts the column, finds the most
   common value, and knows which accounts share a weak password.
2. **One guess tests every account.** Hash `password123` once and compare it
   against a million rows.
3. **The work can be done in advance.** A precomputed table of
   hash → password for the common passwords (a "rainbow table") can be built
   once and reused against every unsalted database ever leaked.

With a salt, every row is a separate problem. A guess has to be re-hashed for
each account, at 385 ms each, and nothing computed before the breach is of
any use.

**Where bcrypt puts it: inside the hash string**, in plain view (section 5).
That surprises people, but the salt is not a secret and does not need to be.
Its job is to be *different per row*, not hidden. `Verify` has to know the
salt to repeat the calculation, so it has to be stored somewhere; putting it
in the same string means there is no separate `salt` column to get out of
sync, and no way to forget to generate one.

This is why the schema has one `password_hash` column and nothing else.

---

## 5. Reading a bcrypt hash string

**📍 Open:** [`internal/auth/password_test.go:16`](../../internal/auth/password_test.go#L16)

The hash on that line, a cost-12 hash of `correct horse battery staple`:

```text
$2a$12$n88Xsg8Ll97cyK1lcLqe7OWOYPAZij9vxGdlA6m2YJVW3o6pAk4vC
```

It is always 60 characters, and `$` is the field separator:

```text
$2a$  12$  n88Xsg8Ll97cyK1lcLqe7O  WOYPAZij9vxGdlA6m2YJVW3o6pAk4vC
 │    │    │                       │
 │    │    │                       └ hash   31 chars = 23 bytes
 │    │    └ salt                    22 chars = 16 bytes
 │    └ cost: 2^12 = 4,096 rounds
 └ algorithm and version
```

| Part | Meaning |
|---|---|
| `$2a$` | `2` is bcrypt. `a` is the revision of the format — the one that defined how non-ASCII passwords are encoded. You will also meet `2b` (an OpenBSD bug fix) and `2y` (PHP's name for the same fix). Go always *writes* `2a`. |
| `12$` | The cost, as two decimal digits. An exponent: 2<sup>12</sup> rounds. |
| next 22 characters | The salt. 16 random bytes, encoded. |
| last 31 characters | The actual hash output. 23 bytes, encoded. |

The salt and hash run together with no separator; the split is by position.
Both are in bcrypt's own base64 alphabet, `./A–Za–z0–9`, which is why a hash
can contain `.` and `/` but never `+` or `=`.

**So `$2a$12$` reads as: "bcrypt, revision a, 4,096 rounds".** Everything
`Verify` needs — algorithm, cost, salt — is in the string. That is what
`TestVerifyStoredHash` demonstrates: a hash made by a different process on a
different day verifies with no other input.

It also means the prefix is a quick diagnostic. `$2a$10$` in the database
would mean something hashed at the library default rather than through
`auth.Hash`.

---

## 6. `Verify`, and the time it takes

**📍 Open:** [`internal/auth/password.go:94`](../../internal/auth/password.go#L94)

`Verify` returns a plain `bool`. There is no error, because there is nothing
a caller should do differently between "wrong password" and "the stored hash
is corrupt": both are a failed login, and telling the client which one it was
is an information leak.

**The timing problem.** `bcrypt.CompareHashAndPassword` parses the hash
first. If the hash is malformed it returns immediately — microseconds, not
385 ms. Now consider the login handler that will call this:

1. Look up the user by email.
2. If found, `Verify(user.PasswordHash, attempt)`.
3. If not found — there is no hash to verify.

If step 3 answers in 2 ms and step 2 in 390 ms, the response time tells an
attacker which email addresses have accounts, without a single correct
password. That is **user enumeration by timing**.

**The fix** is at [line 102](../../internal/auth/password.go#L102). When
bcrypt rejects the hash for any structural reason, `Verify` runs one more
comparison against `dummyHash` — a real, well-formed cost-12 hash — and
throws the result away. The handler can then call `Verify("", attempt)` for
an unknown email and take the same ~385 ms as for a known one.

`dummyHash` ([line 31](../../internal/auth/password.go#L31)) is a hash of 32
random bytes that were never saved, so no password is known to match it. Even
if one were found, that branch returns `false` unconditionally — the test case
"empty hash, fallback's own password" pins that.

**"As far as bcrypt allows" — the limits:**

- A stored hash at a **different cost** takes a different time. The cost *is*
  the running time; nothing can hide it.
- The final byte comparison inside bcrypt already uses
  `subtle.ConstantTimeCompare`, so a nearly-right hash takes no longer than a
  completely wrong one.
- `Verify` equalises its own work. The *handler* still has to call it on the
  not-found path — it cannot fix a caller that returns early.

**Rejected: generate the dummy at startup.** `var dummy = generate()` would
avoid a hard-coded constant, but adds ~385 ms to every process start and
every test binary. **Rejected: generate lazily on first use** — the first
malformed verify would then take twice as long as every later one. The
constant's one weakness, drifting from `Cost`, is covered by
`TestDummyHashMatchesCost`.

**Proved by** `TestVerifyMalformedHashTakesComparableTime`
([line 173](../../internal/auth/password_test.go#L173)), which times both
paths. With the fallback line removed the malformed path is thousands of
times faster and the test fails.

---

## 7. Two things bcrypt does that `Hash` has to correct

**📍 Open:** [`internal/auth/password.go:71`](../../internal/auth/password.go#L71)

**bcrypt will hash an empty password.** `GenerateFromPassword([]byte(""), …)`
succeeds and returns a valid hash — confirmed by running it. An account whose
password is the empty string is a bug, so `Hash` refuses it with
`ErrEmptyPassword` before bcrypt is called.

**bcrypt reads at most 72 bytes.** It is a property of the underlying
Blowfish cipher. Two consequences, each handled separately:

- *Hashing* ([line 78](../../internal/auth/password.go#L78)). Go's library
  returns an error for a longer password. `Hash` checks first and returns its
  own `ErrPasswordTooLong`, so the future handler can turn it into a 400
  without importing bcrypt.
- *Comparing* ([line 112](../../internal/auth/password.go#L112)). Here the
  library does **not** error — it silently ignores everything after byte 72.
  Confirmed by running it: a hash of 72 `a`s accepts 72 `a`s followed by `b`.
  Since `Hash` never produces a hash for such a password, an attempt longer
  than 72 bytes cannot be genuine, and `Verify` returns `false`. The check
  runs *after* the comparison so a long attempt does not get a fast answer.

**Bytes, not characters.** `len(s)` on a Go string counts bytes. In UTF-8 an
ASCII letter is one byte, `é` is two, and `🔑` is four — so 19 key emoji is
19 characters but 76 bytes, and is rejected. There is a test case for exactly
that.

**Rejected: pre-hash with SHA-256 to lift the limit.** A common trick, but it
adds its own pitfall (raw SHA-256 output can contain a zero byte, which some
bcrypt implementations treat as end-of-string) for the sake of passwords over
72 bytes. A clear 400 is simpler and honest.

---

## 8. Sentinel errors and `errors.Is`

**📍 Open:** [`internal/auth/password.go:33`](../../internal/auth/password.go#L33)

`ErrEmptyPassword` and `ErrPasswordTooLong` are **sentinel errors**: package
level variables created once with `errors.New`, exported so callers can
recognise them. The convention is the `Err` prefix.

A caller checks for one with `errors.Is(err, auth.ErrEmptyPassword)` rather
than `err == auth.ErrEmptyPassword`. The difference appears as soon as an
error is wrapped: `fmt.Errorf("register: %w", err)` produces a *new* error
value, so `==` is false, but `errors.Is` unwraps the chain and still finds
the sentinel. The `errorlint` linter in this project flags the `==` form.

Line 102 uses the same idiom in the other direction, to recognise
`bcrypt.ErrMismatchedHashAndPassword` — "the hash was fine, the password was
wrong" — as distinct from every structural failure.

**When to use which.** A sentinel is right when the caller only needs to know
*which* failure occurred. A named error *type* with `errors.As` — as
`internal/config` uses — is right when the caller needs data out of the
error, such as which field was invalid. Neither involves matching on the
message text, which breaks the day someone rewords it.

The third error path, at
[line 86](../../internal/auth/password.go#L86), wraps with `%w` so the cause
is preserved for logging.

---

## 9. The first tests in the repository

**📍 Open:** [`internal/auth/password_test.go:35`](../../internal/auth/password_test.go#L35)

Go's testing is built into the toolchain: a file ending `_test.go`, a
function named `TestXxx(t *testing.T)`, and `go test` finds it. There are no
assertions in the standard library — a test is ordinary code that calls
`t.Errorf` (record a failure, keep going) or `t.Fatalf` (record and stop this
test) when something is wrong.

**Table-driven tests.** Both main tests declare a slice of anonymous structs,
one element per case, then loop over it. Adding a case is adding one line,
and the checking logic exists once. It is the dominant style in Go code.

- **`t.Run(tt.name, …)`** makes each row a named subtest. A failure reports
  as `TestVerify/malformed:_truncated`, and one row can be run alone with
  `go test -run 'TestVerify/truncated'`.
- **`t.Parallel()`** lets subtests run concurrently. Since Go 1.22 each loop
  iteration gets its own `tt`, so the old `tt := tt` line is no longer needed.
- **`t.Helper()`** in `mustHash` makes a failure report the caller's line
  instead of the helper's.
- **`package auth`, not `package auth_test`.** The test is inside the package
  so it can reach the unexported `hashAtCost`, `verifyWithFallback` and
  `dummyHash`.

**"Returns false rather than panicking"** needs no special machinery. A panic
inside a test fails it, so calling `Verify` with seven kinds of broken hash
and checking for `false` covers both halves of the requirement.

### Why the tables run at cost 4

**📍 Open:** [`internal/auth/password_test.go:21`](../../internal/auth/password_test.go#L21)

The first version called `Hash` and `Verify` directly, 21 bcrypt operations
at cost 12. Without the race detector that is fine. With it — and both
`make test` and CI use `-race` — every memory access is instrumented, and one
cost-12 operation took about **4.5 seconds**. The package took **38.7 s**.

A slow test suite gets run less, so the code was split
([`password.go:68`](../../internal/auth/password.go#L68)): `Hash` and
`Verify` are one-line wrappers that supply `Cost` and `dummyHash`, and the
logic lives in `hashAtCost` and `verifyWithFallback`, which take those as
parameters. The tables exercise every branch at `bcrypt.MinCost`, where an
operation is about a millisecond. Three small tests then cover what the
tables cannot: `Hash` really produces `$2a$12$`, `Verify` accepts a real
cost-12 hash, and `dummyHash` is at `Cost`. Result: **7.1 s** under `-race`.

**Rejected: a package variable the tests lower.** `var cost = 12` with a test
that sets it to 4 is shorter, but it is mutable global state that parallel
tests would race on — the very thing `-race` exists to catch. Passing the
value as an argument has no shared state at all.

---

## 10. Direct and indirect dependencies

**📍 Open:** [`go.mod:8`](../../go.mod#L8)

`golang.org/x/crypto` was already in `go.mod`, marked `// indirect`: Gin
needs it, this project's own code did not. Now `internal/auth` imports it, so
it moves to the first `require` block with no comment. The version did not
change and nothing was downloaded.

`github.com/golang-jwt/jwt/v5` made the same move one step later. It had been
added before any code imported it, which `go mod tidy` treats as unused and
removes; once `token.go` imported it, it became a legitimate direct
dependency. The `/v5` in the path is part of the module's name — Go requires
the major version in the import path from v2 onwards, so v4 and v5 are
different modules that could even be used side by side.

The distinction is bookkeeping, but CI enforces it: the workflow runs
`go mod tidy` and fails if the file changes. `golang.org/x/…` is the Go
team's own extension of the standard library — the same authors and review
process, released on a separate schedule.

---

## 11. The three parts of a JWT

**📍 Open:** [`internal/auth/token.go:113`](../../internal/auth/token.go#L113)

A JSON Web Token is three base64url strings joined by dots. This is a real
one, issued by the test suite for a `candidate`:

```text
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9
.eyJyb2xlIjoiY2FuZGlkYXRlIiwic3ViIjoiNWIwZjNhNTItN2MxZS00ZDBhLTlhMzUtMmY2YzFmMGU4ZDExIiwiZXhwIjoxNzY4NDc5MzAwLCJpYXQiOjE3Njg0Nzg0MDAsImp0aSI6Iks3WldMTEc3TlM1VkVJRERVWklaSFhaNDJFIn0
.kArzeBn6J-mwyHqx596CVqWo8dIunYamRW_UmNMm7Lw
```

(Line breaks added; the real token is one line.)

**Part 1 — the header.** Decoded:

```json
{"alg":"HS256","typ":"JWT"}
```

Metadata about the token itself: `alg` is the algorithm the signature was
made with, `typ` says this is a JWT. Every HS256 token from any system starts
with the same `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`, which is why JWTs are
recognisable on sight — `eyJ` is what `{"` looks like in base64.

**Part 2 — the payload.** Decoded:

```json
{
  "role": "candidate",
  "sub":  "5b0f3a52-7c1e-4d0a-9a35-2f6c1f0e8d11",
  "exp":  1768479300,
  "iat":  1768478400,
  "jti":  "K7ZWLLG7NS5VEIDDUZIZHXZ42E"
}
```

The **claims** — statements the server is making about the bearer.

| Claim | Name | Here |
|---|---|---|
| `sub` | subject | The user's ID. *Who* the token is about. |
| `role` | — | Our own claim, not a standard one. What they may do. |
| `iat` | issued at | Unix seconds: 2026-01-15 12:00:00 UTC. |
| `exp` | expires | Unix seconds: `iat` + 900, i.e. 15 minutes later. |
| `jti` | JWT ID | A random identifier unique to this one token. |

`sub`, `iat`, `exp` and `jti` are *registered* claims — names reserved by the
JWT standard (RFC 7519) so every library agrees on their meaning. `role` is a
*private* claim: the standard allows any extra field.

**Part 3 — the signature.** 32 bytes, shown as 43 characters. It is
`HMAC-SHA256(secret, part1 + "." + part2)`. Section 13 is about what that
buys.

**base64url, not base64.** Ordinary base64 uses `+`, `/` and `=`, all of
which mean something in a URL. base64url swaps them for `-` and `_` and drops
the padding, so a token can travel in a header, a query string or a cookie
untouched.

---

## 12. The payload is readable by anyone

**📍 Open:** [`internal/auth/token_test.go:316`](../../internal/auth/token_test.go#L316)

**base64 is an encoding, not encryption.** There is no key. It is a way of
writing bytes using only safe characters, exactly like writing a number in
hex. Anyone can reverse it.

`TestPayloadIsReadableWithoutTheSecret` proves it. `decodePayload`
([line 78](../../internal/auth/token_test.go#L78)) splits a token on the
dots, base64-decodes the middle part and parses the JSON — with the standard
library only. The jwt package is not involved and **the secret is never
used**, yet it reads the user ID and role straight out.

The same thing takes one line in a browser console, or a paste into any of
the JWT-decoder websites.

**So assume every token will be read by:** the user it belongs to, any
JavaScript running on the page (including a malicious browser extension),
every proxy and load balancer that logs headers, and anyone who finds it in a
log file or a bug report.

**Never put in a claim:**

- passwords or password hashes
- API keys, database credentials, the JWT secret itself
- anything personal that is not needed — email, phone, address
- internal detail that helps an attacker — permission lists, internal
  hostnames, feature flags

**Put in only** what the server needs to authorise a request without a
database lookup, and nothing else. Here that is an opaque user ID and a
role. The test asserts the payload has **exactly** those five claims, so
adding a sixth by accident fails the build.

**Rejected: encrypting the token (JWE).** It exists, and hides the payload,
at the cost of a second algorithm, a second key and a much larger attack
surface. Keeping secrets out of the payload is simpler than protecting them
inside it.

---

## 13. What the signature actually proves

**📍 Open:** [`internal/auth/token.go:115`](../../internal/auth/token.go#L115)

The signature is an **HMAC**: a hash of the header and payload that also
mixes in a secret key. Without the key, the correct output cannot be
computed.

To verify, the server recomputes the HMAC over the first two parts with its
own secret and compares the result with part 3. A match proves exactly two
things:

1. **Integrity.** Not one bit of the header or payload has changed since
   signing. The avalanche property from section 1 means changing
   `candidate` to `admin` gives a completely different HMAC.
2. **Authenticity.** Whoever produced it held the secret. Since only this
   server does, this server issued it.

That is the whole trick behind stateless auth: the server does not need to
*remember* that it issued a token for user X with role Y. It can *recognise
its own signature* on the claim.

**Proved by the "payload tampered" case**
([line 224](../../internal/auth/token_test.go#L224)): it rewrites the role
to `admin`, keeps the original signature, and gets

```text
auth: invalid token: token signature is invalid: signature is invalid
```

**What the signature does *not* prove** — each of these has caught someone:

- **Not confidentiality.** Section 12. Signed is not secret.
- **Not that the claims are still true.** The token says "role: admin *as of
  12:00*". If the user was demoted at 12:05, the signature is still valid.
- **Not that the user still exists**, or has not been banned.
- **Not who is presenting it.** It is a *bearer* token: whoever holds it is
  treated as the subject. A stolen token is as good as the original.

Those last three are all the same problem, and section 15 is the answer.

**Symmetric means one key does both jobs.** HS256 uses the same secret to
sign and to verify, so anything that can check a token can also mint one.
That is fine for a single API. Once a second service needs to verify tokens,
the right move is an asymmetric algorithm (RS256, ES256, EdDSA) where the
private key signs and a public key only verifies.

---

## 14. The algorithm-confusion attack

**📍 Open:** [`internal/auth/token.go:152`](../../internal/auth/token.go#L152)

Look again at the header: `{"alg":"HS256"}`. The token tells the server
which algorithm to verify it with — and the header is written by whoever
made the token. **Before the signature is checked, `alg` is attacker
input.** A verifier that obeys it is letting the suspect choose the test.

**Attack 1 — `alg: none`.** The JWT standard defines an "unsecured" token
with algorithm `none` and an empty signature. An attacker takes any token,
edits the payload to `"role":"admin"`, sets the header to `{"alg":"none"}`
and deletes the signature. A naive library reads the header, sees there is
nothing to verify, and accepts it. This was a real vulnerability across many
JWT libraries in 2015.

**Attack 2 — RS256 → HS256 key confusion.** Take a server that signs with
RS256: a private key signs, the *public* key verifies, and the public key
is, as the name says, public. Its code is roughly `verify(token,
publicKey)`. The attacker forges a token with `"alg":"HS256"` and signs it
with HMAC, using **the public key's bytes as the HMAC secret**. The server
reads `HS256`, runs HMAC with the key it was given — the public key — and
the signature matches. The attacker forged a token knowing nothing secret.

Both attacks have the same root: **the algorithm came from the token
instead of from the server.**

**The fix** is `keyFor`. The library calls it after decoding the header and
before verifying anything, to ask "which key?". That is the moment to check
the algorithm, and [line 156](../../internal/auth/token.go#L156) does: if
`alg` is not exactly `HS256`, it returns an error instead of a key, and
verification never starts. The server's code decides the algorithm; the
token only gets to agree.

**Why compare the string, not the type.** The common Go idiom is a type
assertion, `token.Method.(*jwt.SigningMethodHMAC)`. But that type covers
HS256, HS384 and HS512. The requirement is HS256 only, so the check
compares `Alg()` exactly.

**What the tests show.** Three cases present a foreign algorithm
([line 239](../../internal/auth/token_test.go#L239)): `none`, HS384 signed
with the *correct* secret, and ES256 signed with a key the attacker
generated. All three are rejected.

It is worth being exact about which line stops which. With the check
deleted, `none` and `ES256` are *still* rejected — golang-jwt has its own
defences: `none` needs a special opt-in constant as its key, and ES256
refuses a `[]byte` key. Only the **HS384** case fails:

```text
--- FAIL: TestParseAccessToken/alg_HS384_with_the_correct_secret
    ParseAccessToken() error = <nil>, want auth: invalid token
```

So in this codebase today the check is the thing that pins the algorithm to
one value, and a second layer behind the library's. It becomes the *only*
layer the day the key type changes — which is exactly when attack 2 happens.

**Rejected: `jwt.WithValidMethods([]string{"HS256"})`.** A parser option
that does the same job, and equally correct. The explicit check was chosen
because the requirement asked for it and because it puts the reasoning in
this repository's code rather than in an option name. Using both would make
the second unreachable and therefore untestable.

---

## 15. Why only fifteen minutes

**📍 Open:** [`internal/auth/token.go:17`](../../internal/auth/token.go#L17)

Section 13 ended with three things a valid signature does not prove. They
share a cause: **the server keeps no record of the tokens it issued, so it
cannot take one back.** That is the price of stateless auth.

A session ID in a database can be deleted, and the next request fails. A
JWT has no row to delete. Once signed it is valid until `exp`, and nothing
the server does changes that:

- **Stolen token** → the thief is that user until it expires.
- **User logs out** → the client forgets the token, but a copy still works.
- **Admin demoted, account banned, password changed** → the old token still
  says `admin`.

Since a token cannot be revoked, the only control is **how long it lives**.
`exp` is the revocation mechanism. Fifteen minutes means the worst case for
every bullet above is fifteen minutes, not thirty days.

**Why it is not actually inconvenient.** The user does not log in every
fifteen minutes. The design uses two tokens with opposite properties:

| | Access token | Refresh token |
|---|---|---|
| Lifetime | 15 minutes | days or weeks |
| Sent | on every API request | only to the refresh endpoint |
| Checked by | signature alone — no database | a database lookup |
| Revocable | no | **yes** — delete the row |

When the access token expires, the client quietly exchanges its refresh
token for a new one. The user notices nothing. But that exchange hits the
`refresh_tokens` table from [`01-schema.md`](01-schema.md), so a revoked or
deleted refresh token stops the chain. Logout, ban and password change
become real within fifteen minutes at most.

The exposure also differs: the access token travels with every request, so
it is the one most likely to leak, and it is the one that is nearly
worthless shortly after.

**Why not shorter.** One minute would mean a database lookup per minute per
user, which gives back most of what stateless tokens saved. **Why not
longer.** An hour is common; a day defeats the purpose. Fifteen minutes is
the conventional middle.

**Expired is reported separately**
([line 137](../../internal/auth/token.go#L137)). `ErrExpiredToken` and
`ErrInvalidToken` are different errors because the client's next move is
different: *refresh* versus *log in again*. And the library validates `exp`
only after the signature passes, so "expired" is never said about a forgery
— the "different secret and expired" test case pins that order.

**`WithExpirationRequired`**
([line 78](../../internal/auth/token.go#L78)) closes a gap: by default the
library accepts a correctly signed token that has no `exp` at all, and such
a token would live forever.

**What `jti` is for.** Fifteen minutes is still a window. If one specific
token must die *now*, the standard answer is a small denylist of `jti`
values, each kept only until that token's `exp`. Nothing does that yet; the
random `jti` is what makes it possible later.

---

## 16. Refusing to start on a weak secret

**📍 Open:** [`internal/config/config.go:278`](../../internal/config/config.go#L278)

Everything above rests on one assumption: nobody else has the secret.

**Why length matters.** An attacker with a single token has the header, the
payload and the correct signature — everything needed to test a guess
**offline**: compute `HMAC(guess, header.payload)`, compare. No request
reaches this server, so no rate limit applies, and HMAC-SHA256 is fast by
design (section 2, working against us this time). A secret like `secret`
or `hiredesk-dev` falls in seconds to a wordlist, after which the attacker
can sign `"role":"admin"` for any user.

Thirty-two bytes is HMAC-SHA256's natural key size: its output is 32 bytes,
and a longer key adds nothing. `openssl rand -hex 32` produces 64 hex
characters carrying 256 bits.

**Two checks, in order** ([`config.go:163`](../../internal/config/config.go#L163)):

1. **Unset, empty or only whitespace** → the existing `MissingVarsError`.
2. **Set but under 32 bytes** → the new `WeakSecretError`.

Both make `Load` return an error, and `main` exits before the listener
opens. This is the "fail fast at startup" convention: a server that boots
with a guessable key works perfectly, right up until it is exploited.

**Why a new error type.** `InvalidVarError` prints the offending value,
which is right for `PORT=abc` and wrong for a secret. A rejected secret is
often a *real* one with a typo, and startup errors go to logs.
`WeakSecretError` ([line 139](../../internal/config/config.go#L139))
carries only the name and the two lengths, and a test asserts the message
never contains the value.

**The placeholder had to change.** `.env.example` used to contain
`JWT_SECRET=replace_me_with_openssl_rand_hex_32` — 35 characters. It would
have **passed** the new check, so `cp .env.example .env` would have started
the API on a key published on GitHub. The placeholder is now `run_make_env`
(12 characters), and `TestEnvExamplePlaceholderIsRejected` reads the real
file and fails if anyone lengthens it. `make env` is unaffected: it replaces
the whole line.

**What the check cannot do.** It measures length, not randomness: 32
letter `a`s pass. Detecting low entropy reliably is not possible, so the
rule is to generate the key, never type it.

**Rejected: generating a secret at startup when none is set.** Convenient,
and wrong twice: every restart would invalidate every token, and two
instances behind a load balancer would reject each other's tokens.

---

## 17. Go idioms appearing for the first time

**Struct embedding.**
**📍 Open:** [`internal/auth/token.go:45`](../../internal/auth/token.go#L45)

`Claims` contains `jwt.RegisteredClaims` with **no field name**. That is
embedding: the inner struct's fields and methods are *promoted*, so
`claims.Subject` works as if `Subject` were declared on `Claims`. It is
composition, not inheritance — `Claims` *has a* `RegisteredClaims`, and the
long form `claims.RegisteredClaims.Subject` still works.

It matters here because the library accepts any type satisfying its
`jwt.Claims` **interface** — a set of methods such as
`GetExpirationTime()`. `RegisteredClaims` has them; embedding promotes
them; so `Claims` satisfies the interface without one line of method code.
Go interfaces are satisfied implicitly: there is no `implements` keyword.

Embedding also flattens the JSON, which is why the payload in section 11 has
`sub` at the top level rather than nested.

**Struct tags.** `` `json:"role"` `` on
[line 40](../../internal/auth/token.go#L40) tells `encoding/json` to use
the key `role`. Without it the key would be the field name, `Role`.

**A type with methods, instead of package functions.**
**📍 Open:** [`internal/auth/token.go:49`](../../internal/auth/token.go#L49)

`Hash` and `Verify` are plain functions because they need nothing but their
arguments. Tokens need the secret. A package-level `var secret` would be
hidden global state, set from who-knows-where — against this project's rule
that configuration is passed explicitly. So the secret lives in a
`TokenManager`, built once in `main` from `cfg.JWTSecret`, and
`NewAccessToken` / `ParseAccessToken` are **methods** on it.

The receiver is a pointer (`m *TokenManager`) so every caller shares the one
manager rather than copying it.

**Injecting the clock.**
**📍 Open:** [`internal/auth/token.go:56`](../../internal/auth/token.go#L56)

Testing "an expired token is rejected" by really waiting takes 15 minutes.
Instead the manager holds `now func() time.Time` — a **function stored in a
field**. Production sets it to `time.Now`; a test sets it to a function
returning a fixed instant plus an offset. The whole expiry boundary — one
second before, exactly at, one second after — runs in microseconds and
gives the same answer every time.

[Line 74](../../internal/auth/token.go#L74) passes the parser a **closure**:
an anonymous function that captures `m` and calls `m.now()` when invoked.
Passing `m.now` directly would copy the function value at that moment, and a
test replacing the field afterwards would have no effect on the parser.

**Two `%w` verbs in one error**
([line 138](../../internal/auth/token.go#L138)).
`fmt.Errorf("%w: %w", ErrExpiredToken, err)` wraps *both*. The handler can
ask `errors.Is(err, auth.ErrExpiredToken)` without importing the jwt
package, while the library's own detail is still there for the log.

**`t.Setenv`**
([`config_test.go:44`](../../internal/config/config_test.go#L44)) sets an
environment variable for one test and restores the old value afterwards. It
changes the whole process, so the testing package forbids combining it with
`t.Parallel` — which is why the config tests are sequential.

---

## 18. One shape for every error

**📍 Open:** [`internal/api/errors.go:29`](../../internal/api/errors.go#L29)

Every failure this API reports, from a bad password to a crashed handler, has
this body:

```json
{"error":{"code":"not_found","message":"The requested resource was not found.","details":{}}}
```

| Field | For | Stability |
|---|---|---|
| `code` | programs — a client writes `if code == "duplicate"` | a contract; changing one is a breaking change |
| `message` | people — shown in a toast or a log | may be reworded at any time |
| `details` | forms — field name to problem | always an object, empty unless validation failed |

The HTTP status says which *family* of failure it is; `code` says which one.
Two different failures share 409 (`duplicate` and `conflict`), and a status
alone could not tell a client which.

**Why set this up before any handler exists.** With no rule, each handler
invents its own: one returns `{"error":"not found"}`, the next
`{"message":"..."}`, a third plain text. Every client then needs a parser per
endpoint. Fixing the shape first means handlers never get the chance — they
call one function, [`Respond`](../../internal/api/errors.go#L89), and it is the
only code in the project that writes an error body.

**`details` is never absent and never `null`.**
[`errors.go:43`](../../internal/api/errors.go#L43) has no `omitempty`, and
[`errors.go:141`](../../internal/api/errors.go#L141) swaps a nil map for an
empty one, because Go encodes a nil map as `null`. A field that is sometimes an
object, sometimes missing and sometimes `null` is three shapes, and the client
would have to check before every read.

**Rejected: `omitempty` on `details`.** It is the more common choice and saves
thirteen bytes. It was turned down because "exactly one shape" is the whole
requirement, and the test's `decode` helper
([`errors_test.go:59`](../../internal/api/errors_test.go#L59)) fails if the
error object has anything but exactly three keys.

**Rejected: RFC 9457 `application/problem+json`.** A real standard with `type`,
`title`, `status`, `detail` and `instance`. It is a good choice for a public
API with many consumers; here it would add five fields nobody reads.

**One exception.** `/readyz` answers a failed check with
`{"status":"degraded","database":"unreachable"}`. That body is read by a load
balancer, not an API client, and was left alone.

---

## 19. Sentinel errors again, and why `errors.Is` exists

**📍 Open:** [`internal/domain/errors.go:17`](../../internal/domain/errors.go#L17)

[Section 8](#8-sentinel-errors-and-errorsis) introduced a sentinel error: one
value, created once with `errors.New`, stored in a package-level variable, and
used as a signal that callers compare against. `ErrNotFound` is not a
description of a problem. It is a *name* for a category of outcome.

**Why `errors.New` gives something comparable.** `errors.New` returns a pointer
to a small struct. Two calls with identical text give two different pointers:

```go
errors.New("not found") == errors.New("not found") // false
```

So the text is irrelevant to identity. `ErrNotFound` equals only itself, which
is what makes it safe to compare — nobody can produce an equal value by
accident, and rewording the message breaks nothing.
[`TestSentinelsAreDistinct`](../../internal/domain/errors_test.go#L9) checks
all thirty-six pairs.

**Why `==` is not enough.** `==` asks "is this exactly that value?" It works
only until someone adds context:

```go
err := fmt.Errorf("select job 42: %w", domain.ErrNotFound)

err == domain.ErrNotFound          // false — err is a new, different value
errors.Is(err, domain.ErrNotFound) // true
```

Wrapping creates a new error that *contains* the old one. `errors.Is` unwraps
layer by layer and compares at each step, so it asks the question that was
meant: "is this, or anything inside it, that value?"

A sentinel that is never wrapped is a sentinel with no context, and an error
with no context is hard to debug. So in practice errors get wrapped, and `==`
quietly stops matching — with no compile error, only a 500 where a 404 was
expected. That is why the `errorlint` linter in this repository rejects `==` on
errors outright.

**What breaks without it.** With [`errors.go:71`](../../internal/api/errors.go#L71)
changed to `err == m.target`, every bare sentinel still maps correctly and all
three wrapped rows of the table fail, along with the validation test. Verified
by making the change and running the tests.

**Rejected: matching on the message**, `strings.Contains(err.Error(), "not
found")`. It breaks when the text is reworded and matches errors that merely
mention the phrase.

**Rejected: one error type with a `Kind` field** (`&domain.Error{Kind:
NotFound}`). It is a sound design in larger codebases. Sentinels were chosen
because they are what the standard library itself uses (`io.EOF`,
`sql.ErrNoRows`, `context.Canceled`), so the habit transfers.

---

## 20. What `%w` does, and why it matters here

**📍 Open:** [`internal/domain/errors_test.go:30`](../../internal/domain/errors_test.go#L30)

`fmt.Errorf` builds a new error from a format string. Two verbs print an
existing error into it, and they print the same text:

```go
fmt.Errorf("login: %w", inner) // text + a link back to inner
fmt.Errorf("login: %v", inner) // text only
```

`%w` stores `inner` inside the new error and gives the new error an `Unwrap`
method that returns it. That link is the chain `errors.Is` and `errors.As`
walk. `%v` formats `inner` to a string and throws the value away, so the chain
ends there. The test at
[`errors_test.go:50`](../../internal/domain/errors_test.go#L50) wraps the same
error both ways and shows `errors.Is` succeeding for one and failing for the
other.

**Why it matters here.** A request that fails passes through three layers, and
each has something different to say:

| Layer | What it knows | What it returns |
|---|---|---|
| `store` | the query and the driver's error | `fmt.Errorf("select job %s: %w", id, domain.ErrNotFound)` |
| service | the business operation | `fmt.Errorf("apply to job: %w", err)` |
| `api` | HTTP | calls `Respond(c, err)` |

By the time the error reaches `Respond` its text is
`apply to job: select job 42: not found` — useful in a log, wrong for a client.
`Respond` needs only one fact from it: *which category?* `%w` is what lets the
lower layers add all the context they like without destroying that fact. Use
`%v` at any one layer and the category is lost, and a 404 turns into a 500.

**Two `%w` in one error.** Since Go 1.20 a format string may wrap more than one
error. The store will use this to keep the driver's error *and* attach a
category:

```go
fmt.Errorf("insert user: %w: %w", domain.ErrDuplicate, pgErr)
```

`errors.Is` finds either. The table has a row for exactly this at
[`errors_test.go:113`](../../internal/api/errors_test.go#L113), and it also
checks the driver's text stays out of the response.

---

## 21. Why the status codes live in `api`, and not in `domain`

**📍 Open:** [`internal/api/errors.go:57`](../../internal/api/errors.go#L57)

The translation is one table, and it is the only place in the project where a
domain error and an HTTP status appear on the same line:

| Domain error | Status | `code` |
|---|---|---|
| `ErrValidation` | 422 | `validation_failed` |
| `ErrInvalidCredentials` | 401 | `invalid_credentials` |
| `ErrTokenExpired` | 401 | `token_expired` (added in section 43) |
| `ErrUnauthenticated` | 401 | `unauthenticated` (added in section 43) |
| `ErrForbidden` | 403 | `forbidden` |
| `ErrNotFound` | 404 | `not_found` |
| `ErrDuplicate` | 409 | `duplicate` |
| `ErrConflict` | 409 | `conflict` |
| anything else | 500 | `internal_error` |

The tempting alternative is to give each domain error its status directly — an
`ErrNotFound` that carries `404`. Four reasons it was rejected:

1. **Dependencies must point one way.** `api` imports `domain`. If `domain`
   knew about HTTP it would import `net/http`, and the layer meant to be the
   stable centre would depend on a delivery detail. Check that it does not: the
   import block at [`domain/errors.go:8`](../../internal/domain/errors.go#L8)
   lists `errors`, `sort` and `strings`, and nothing else.
2. **HTTP is one way in, not the only one.** A background worker sending
   interview reminders, a CLI that imports candidates, and a test all call the
   same domain code. None has a status code to send. "Not found" means the same
   to all of them; "404" means something to one.
3. **The same error is not always the same status.** `ErrNotFound` from "get
   job 42" is a 404. `ErrNotFound` from "look up the user for this login" must
   surface as a 401, or the API confirms which emails are registered. That is a
   decision about what to reveal over HTTP, and only the HTTP layer can make
   it.
4. **Status choices are API design and change for API reasons.** Switching
   validation from 422 to 400 to match another service is a one-line edit to
   this table, with no business rule touched.

**It is a slice, not a map.** `map[error]mapping` looks natural and would be
wrong: a map lookup compares with `==`, so a wrapped error would miss
(section 19). Each row has to be tried with `errors.Is`, in order.

**Why 422 and not 400.** 400 is reserved for a body that cannot be read at all
— broken JSON. 422 says the request was understood and breaks a rule. A client
can then tell "my code is building the request wrongly" from "the user typed
something invalid".

**Why two codes share 409.** `duplicate` is "this already exists"; `conflict`
is "valid, but not now" — moving an application to *hired* on a job that has
closed. The status is the same and the client's reaction is not.

---

## 22. Unexpected errors: log everything, say nothing

**📍 Open:** [`internal/api/errors.go:89`](../../internal/api/errors.go#L89)

An error that matches no row of the table is one nobody planned for: the
database is down, a query has a typo, a pointer was nil. `Respond` does two
things with it, and they go to different audiences:

- **To the log**, at [`errors.go:95`](../../internal/api/errors.go#L95): the
  method, the path, and the complete error chain.
- **To the client**: status 500, code `internal_error`, and a fixed sentence
  that is the same for every cause.

**What could leak.** Suppose the handler simply returned `err.Error()`. This is
what a Postgres driver reports when a unique constraint is violated:

```text
ERROR: duplicate key value violates unique constraint "users_email_key"
(SQLSTATE 23505) DETAIL: Key (email)=(alice@example.com) already exists.
```

Sent to a client, that one string discloses:

| Fragment | What an attacker learns |
|---|---|
| `SQLSTATE 23505`, the phrasing | the database is Postgres — which exploits to try |
| `users_email_key` | a table named `users` with a column `email` — the names needed to write an SQL injection payload |
| `Key (email)=(alice@example.com)` | **another user's personal data**, and proof that address is registered |

Other errors are worse. A failed connection can print
`failed to connect to host=db.internal user=hiredesk database=hiredesk`, which
is the internal network layout and a username. A syntax error prints part of
the query. A panic prints file paths and the names of dependencies with their
versions.

None of this is the breach on its own. It is **reconnaissance**: it turns
blind guessing into aimed attempts. And the same response that helps an
attacker is useless to an honest client, who cannot fix a server's database
either way.

**Known errors are filtered too.** This is the less obvious half. An error that
*does* match the table still has text nobody vetted — `select job 42: not
found` is harmless, but the store is free to wrap anything. So
[`errors.go:101`](../../internal/api/errors.go#L101) never sends `err.Error()` at
all. The client gets the table's fixed message, plus field details that were
written to be shown (section 23). The test wraps `ErrDuplicate` together with
the driver message above and asserts none of it appears in the body.

**Expected errors are not logged.** A 404 or a failed login is the API working.
Logging them at the same level as a crash buries the crashes.
`TestRespondMapsDomainErrors` asserts the log stays empty.

**What the log line is careful about.** The path is printed with `%q`, so a
request for a path containing a newline cannot forge a second, fake log line.
The query string is left out entirely, because query strings carry tokens and
reset codes, and a log file is read by more people than the database is.

**What breaks without it.** With the 500 branch changed to send `err.Error()`,
`TestRespondHidesUnexpectedErrors` fails on every fragment it checks.

**Rejected: showing the detail when `ENV` is `development`.** Convenient,
and it means the code path that leaks exists in the binary and is one
misconfigured variable from production. The log is in the terminal next to the
request anyway.

---

## 23. Validation errors that name each field

**📍 Open:** [`internal/domain/errors.go:52`](../../internal/domain/errors.go#L52)

A sentinel can say *that* validation failed, and nothing more — it is a single
shared value, so it cannot carry which fields. For that a **type** is needed:
`ValidationError` is a struct with a map from field name to problem.

It is still an `ErrValidation`. The `Unwrap` method at
[`errors.go:96`](../../internal/domain/errors.go#L96) returns the sentinel, so
`errors.Is(err, domain.ErrValidation)` is true and the table in section 21
needs no special row. Code that only cares about the category never has to
know the type exists.

`Respond` is the one caller that wants the fields, and it gets them with
`errors.As` at [`api/errors.go:108`](../../internal/api/errors.go#L108):

```json
{"error":{"code":"validation_failed","message":"One or more fields are invalid.",
  "details":{"email":"is not a valid address","password":"must be at least 12 characters"}}}
```

**Why report every field at once.** A form that reveals one mistake per
submission makes the user submit five times. Collecting into a map and
returning once is the reason `Add` exists.

**Why this text is allowed out when `err.Error()` is not.** Section 22's rule
is about text nobody wrote for a client. These strings are written by
validation code, about input the client sent, for the purpose of being shown.
The doc comment on `Fields` states that obligation.

**Rejected: `details` as `map[string]any`.** It would allow nested objects and
lists of problems per field. Nothing needs them yet, and a client can rely on
"string to string" far more easily than on "anything".

---

## 24. Errors that never reach a handler

**📍 Open:** [`internal/api/router.go:32`](../../internal/api/router.go#L32)

`Respond` only runs when a handler calls it. Three failures happen where no
handler is involved, and Gin's defaults for all three break the one-shape rule:

| Situation | Gin's default | Now |
|---|---|---|
| No route matches the path | plain text `404 page not found` | 404 `route_not_found` |
| Path exists, method does not | also a plain-text 404 | 405 `method_not_allowed` |
| A handler panics | 500 with an **empty** body | 500 `internal_error` |

That is why the router is not built with `gin.Default()`. `gin.Default()` is
`gin.New()` plus a logger and a recovery middleware; the two are now listed by
hand so that [`api.Recovery`](../../internal/api/errors.go#L129) can stand in
for the second. `gin.CustomRecovery` still does the important part — it catches
the panic, logs the value and a stack trace, and keeps the process alive — and
only the response body is ours.

`HandleMethodNotAllowed` at [`router.go:37`](../../internal/api/router.go#L37) is off in
Gin by default. Left off, `DELETE /healthz` is reported as "no such endpoint",
which is untrue and sends a client looking for a typo in the URL.

**`route_not_found` is a different code from `not_found`.** One means the URL
is wrong; the other means the URL is right and the record does not exist. A
client retries neither, but a developer debugging wants to know which.

---

## 25. More Go idioms appearing for the first time

**A nil pointer that is not a nil error.**
[`domain/errors.go:73`](../../internal/domain/errors.go#L73)

This is the best-known trap in Go's error handling. An interface value holds
two things: a type and a value. It equals `nil` only when *both* are empty.

```go
func validate() error {
	var v *domain.ValidationError // a nil pointer
	return v                      // an error holding (type *ValidationError, value nil)
}

validate() != nil // true — the type half is filled in
```

So a function that returns a nil `*ValidationError` through an `error` return
type reports failure for valid input. `Err()` exists to make that impossible:
it returns the untyped `nil` when there is nothing wrong, and the type's doc
comment tells callers to use it.

**The zero value is usable.** `var v domain.ValidationError` works with no
constructor. The catch is that the zero value of a map is `nil`, and *writing*
to a nil map panics (reading is fine). `Add` creates the map on first use, at
[`errors.go:62`](../../internal/domain/errors.go#L62). `sync.Mutex` and
`bytes.Buffer` in the standard library follow the same design.

**`errors.As` — the counterpart to `errors.Is`.**
[`api/errors.go:108`](../../internal/api/errors.go#L108)

`errors.Is` asks "is a particular *value* in the chain?" `errors.As` asks "is
anything of this *type* in the chain?" and, if so, copies it into the variable
whose address it is given. It takes a pointer to the target so it has somewhere
to write. It replaces a type assertion (`err.(*ValidationError)`), which only
looks at the outermost error and fails as soon as anything wraps it.

**`TestMain`.** [`api/errors_test.go:25`](../../internal/api/errors_test.go#L25)

A function with exactly this name runs *instead of* the tests, once per
package, and must call `m.Run()` to run them. It is the place for setup that
has to happen before any test — here, putting Gin into test mode so it does not
print a route table.

**`httptest`.** [`api/errors_test.go:48`](../../internal/api/errors_test.go#L48)

`httptest.NewRecorder()` is a `ResponseWriter` that writes into memory, and
`httptest.NewRequest` builds a request without a network. Together they test a
handler in microseconds with no port opened, which is how every handler in this
project will be tested.

**Why some of these tests are not parallel.** The standard `log` package writes
to one process-wide destination. A test that redirects it
([`errors_test.go:33`](../../internal/api/errors_test.go#L33)) would capture
another test's output if both ran at once. The same reasoning made the config
tests sequential in section 17.

---

## 26. One request, three layers

**📍 Open:** [`internal/api/auth_handler.go:23`](../../internal/api/auth_handler.go#L23)

A registration passes through three packages, and each knows one thing:

| Package | Knows about | Does not know about |
|---|---|---|
| `api` | HTTP, JSON, status codes | SQL, pgx |
| `domain` | what a user is, which errors exist | HTTP, SQL |
| `store` | SQL, pgx, PostgreSQL error codes | HTTP, JSON |

The handler does not hold a `*store.UserStore`. It holds a `UserStore`
**interface** declared in the `api` package, listing only the methods the
handlers call. `*store.UserStore` satisfies it without ever mentioning it —
Go checks the method set, not a declaration (section 35).

**Why the interface lives with the caller.** The handler tests run against
[`fakeUsers`](../../internal/api/auth_handler_test.go#L28), a map in memory.
No database, no Docker, and the whole `api` suite finishes in under two seconds
with the race detector on. Had the handler taken the concrete store type, every
handler test would need Postgres.

**What the fake must get right.** A fake that behaves differently from the real
thing gives tests that pass for the wrong reason. This one keeps the two
behaviours the handlers rely on: emails are unique ignoring case, and a missing
user is `domain.ErrNotFound`. The real store's versions of both are checked
against a real Postgres in
[`users_integration_test.go`](../../internal/store/users_integration_test.go#L113).

**Everything is assembled in one place.**
[`main.go:81`](../../cmd/api/main.go#L81) builds the pool, the store, the token
manager and the handler, and hands them down. Nothing below `main` reads
configuration or opens a connection.

**Rejected: a service layer between handler and store.** It is the usual next
step and will come when there is logic that is neither HTTP nor SQL — moving an
application between stages, for instance. Registration today is "hash, insert",
and a layer that only forwards calls teaches nothing.

---

## 27. Hand-written SQL with pgx

**📍 Open:** [`internal/store/users.go:47`](../../internal/store/users.go#L47)

Three queries, each a string constant next to the code that runs it.

**Placeholders.** `$1`, `$2` … are not string formatting. The SQL text and the
values travel to Postgres separately, and the server never parses a value as
SQL. That is the whole defence against SQL injection: an email of
`'; DROP TABLE users; --` is stored as a strange email. Building the query with
`fmt.Sprintf` would hand that string to the parser.

**`RETURNING`.** The database assigns `id`, `created_at` and `updated_at`.
`RETURNING` ([`users.go:56`](../../internal/store/users.go#L56)) sends the
stored row back in the same round trip, so there is no second `SELECT` and no
window in which the row could change between the two.

**`QueryRow` reports nothing until `Scan`.** `QueryRow` returns a `pgx.Row` and
no error. Every failure — a constraint violation on the `INSERT`, no matching
row on a `SELECT` — comes out of `Scan`
([`users.go:109`](../../internal/store/users.go#L109)). Forgetting to call
`Scan` on an `INSERT … RETURNING` means the insert's error is never seen.

**`lower(email) = lower($1)`.**
[`users.go:74`](../../internal/store/users.go#L74) is written to match the
expression the unique index was built on, `users_email_lower_idx ON users
(lower(email))`. Checked with `EXPLAIN`: the plan is `Index Scan using
users_email_lower_idx`. Written as `WHERE email = $1` the query would scan the
table and would miss a row stored in different case.

**One column list.** `userColumns` at
[`users.go:30`](../../internal/store/users.go#L30) is shared by all three
queries and read in the same order by `scanUser`. With hand-written SQL the
classic bug is a `SELECT` and a `Scan` that disagree about column order; one
constant and one function leave a single place to get it right.

**A malformed ID is "not found".** `GET /users/42` is as much a miss as an
unknown UUID. The parameter is cast in SQL (`$1::text::uuid`), Postgres answers
with SQLSTATE `22P02`, and `GetUserByID` turns that into `ErrNotFound`
([`users.go:96`](../../internal/store/users.go#L96)) rather than a 500.

**Why no ORM or query builder.** The SQL in the file is the SQL that runs.
There is nothing to learn but SQL and nothing generated to debug, and the
`lower(email)` index trick above is easy to write and awkward to express
through most ORMs. The price is repetition and no compile-time check that a
query matches the schema — which is what the integration tests are for.

---

## 28. Reading the PostgreSQL error code with `errors.As`

**📍 Open:** [`internal/store/users.go:123`](../../internal/store/users.go#L123)

When Postgres rejects a statement, pgx returns a `*pgconn.PgError` — a struct
with the server's own fields:

| Field | For a duplicate email |
|---|---|
| `Code` | `23505` |
| `Message` | `duplicate key value violates unique constraint "users_email_lower_idx"` |
| `ConstraintName` | `users_email_lower_idx` |
| `Detail` | `Key (lower(email))=(alice@example.com) already exists.` |

`Code` is the SQLSTATE: five characters, defined by the SQL standard and stable
across Postgres versions and languages. Class `23` is "integrity constraint
violation"; `23505` is specifically a unique violation, `23503` a foreign key,
`23514` a check constraint.

**The problem.** The store receives a value of type `error`. An interface value
exposes only the `Error() string` method, so `err.Code` does not compile. And
the `*pgconn.PgError` may not be the outermost error: pgx or this code may have
wrapped it.

**What `errors.As` does**, at
[`users.go:131`](../../internal/store/users.go#L131):

```go
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation {
```

It walks the chain exactly as `errors.Is` does, but at each step asks "can this
error be assigned to a `*pgconn.PgError`?" instead of "is this that value?".
On the first match it copies the error into `pgErr` and returns true. From then
on `pgErr` is an ordinary struct pointer and `pgErr.Code` is an ordinary field.

It takes `&pgErr` — a pointer to the pointer — because it has to *write* the
result somewhere. Passing `pgErr` itself would hand it a copy of a nil pointer.

[`TestTranslateKeepsThePostgresErrorReadable`](../../internal/store/users_test.go#L77)
wraps a `PgError` three layers deep and reads `Code` and `ConstraintName` back
out.

**Why the code and not the message.** `strings.Contains(err.Error(),
"duplicate key")` breaks when the server's locale is not English (`lc_messages`
translates the text) and matches any error that happens to contain the phrase.

**Only `23505` becomes `ErrDuplicate`.** A check-constraint violation is also
class 23, and treating the whole class as "duplicate" would tell a client their
email is taken when the real problem is an invalid role. The table has a row
for exactly that, and the integration test inserts a role of `superuser` to
confirm it against a real server.

**The original is kept.** `translate` returns
`fmt.Errorf("%s: %w: %w", op, domain.ErrDuplicate, err)` — two `%w`
(section 20). `errors.Is` finds the sentinel for the api layer, `errors.As`
still finds the `PgError` for anyone who wants the constraint name, and
section 22 guarantees none of it reaches the client.

**`pgx.ErrNoRows` is a sentinel, so it is `errors.Is`.**
[`users.go:124`](../../internal/store/users.go#L124). The rule of thumb: a
specific *value* → `errors.Is`; a *type* whose fields you want → `errors.As`.

**What breaks without this function.** Without the mapping a duplicate email is
an unrecognised error, `Respond` logs it and returns a 500, and the client
cannot tell "choose another email" from "the server is broken".

---

## 29. Struct tags, and how two packages read the same field

**📍 Open:** [`internal/api/auth_handler.go:74`](../../internal/api/auth_handler.go#L74)

```go
Email string `json:"email" validate:"required,email,max=254"`
```

The text in backticks is a **struct tag**. To the compiler it is an inert
string attached to the field: it changes nothing about how the field is stored
or used, and a typo in it is not a compile error. It is metadata, there for any
package that cares to look.

**The format is a convention.** `key:"value"` pairs separated by spaces. Each
package claims a key and ignores the others. That is how one field carries
instructions for two libraries that have never heard of each other.

**How they are read: reflection.** The `reflect` package lets a program inspect
its own types at run time — list a struct's fields, get their names, types and
tags. Both libraries do this the first time they see a type:

| Package | Looks up | Meaning here |
|---|---|---|
| `encoding/json` | `field.Tag.Get("json")` | "this field is called `email` in JSON" |
| `validator` | `field.Tag.Get("validate")` | "must be present, an email address, at most 254 characters" |

This project does the same thing by hand in one place.
[`validation.go:35`](../../internal/api/validation.go#L35) registers a function
with the validator that reads the **json** tag of a failing field, so the error
says `full_name` — the name the client sent — and not `FullName`, the Go name
the client has never seen.

**The rules used.** Comma-separated, checked left to right, stopping at the
first failure for that field:

| Rule | Meaning |
|---|---|
| `required` | not the zero value — for a string, not `""` |
| `email` | shaped like an email address |
| `min=8` / `max=72` | length in **characters** for a string |

**Why tags and not `if` statements.** The rules sit beside the field they
constrain, so the struct is the documentation of what the endpoint accepts.

**What tags cannot do.** Because they are strings, nothing checks them until
the code runs. `validate:"requried"` compiles, and panics the first time the
struct is validated. A test that validates every request type at least once —
as these do — is the guard.

**Lower-case fields are invisible.** Both libraries can only see exported
fields. A field named `email` with a perfect tag is silently skipped by JSON.

---

## 30. Validation: decode, clean, check, report

**📍 Open:** [`internal/api/validation.go:64`](../../internal/api/validation.go#L64)

Every handler that takes a body does the same four steps, and each has its own
failure:

| Step | Where | Failure |
|---|---|---|
| Decode the JSON | [`decodeJSON`](../../internal/api/validation.go#L49) | 400 `malformed_request` |
| Clean the values | in the handler | — |
| Check the rules | [`validateStruct`](../../internal/api/validation.go#L64) | 422 `validation_failed`, with `details` |
| Do the work | the handler | whatever the store reports |

**Clean before checking.** `" Alice@Example.com "` is a valid address with
spaces around it. Validating first would reject it; trimming and lowercasing
first accepts it. That ordering is why this project calls the validator itself
rather than using Gin's `binding:"…"` tags, which validate inside
`ShouldBindJSON` — before the handler has had a chance to clean anything.

**The password is never cleaned.** A leading space is a legitimate character
in a password. Trimming it at registration and not at login (or the reverse)
would lock the user out.

**From the validator's error to the envelope.** `validate.Struct` returns a
`validator.ValidationErrors` — a slice with one entry per failed field.
[`validation.go:73`](../../internal/api/validation.go#L73) gets at it with
`errors.As` (section 28), and each entry becomes one line of the
`domain.ValidationError` from section 23:

```json
{"error":{"code":"validation_failed","message":"One or more fields are invalid.",
  "details":{"email":"must be a valid email address","full_name":"is required",
             "password":"must be at least 8 characters"}}}
```

That is real output from the running server. The wording is ours
([`problemFor`](../../internal/api/validation.go#L86)): the validator's own
message is `Key: 'registerRequest.Password' Error:Field validation for
'Password' failed on the 'min' tag`, which names a Go type.

**A 400 hides the decoder's message for the same reason.** `encoding/json`
reports `cannot unmarshal number into Go struct field
registerRequest.password of type string`.

**Characters versus bytes.** `max=72` counts characters; bcrypt's limit is 72
*bytes*. Twenty key emoji are 20 characters and 80 bytes, so they pass the tag
and fail `auth.Hash`. The handler catches `auth.ErrPasswordTooLong` at
[`auth_handler.go:163`](../../internal/api/auth_handler.go#L163) and reports it
as a validation error on `password`, since it is still the client's input at
fault.

**The body is capped at 1 MB**
([`validation.go:50`](../../internal/api/validation.go#L50)). Without a cap the
decoder reads whatever a client sends.

---

## 31. Why the response is its own struct

**📍 Open:** [`internal/api/auth_handler.go:92`](../../internal/api/auth_handler.go#L92)

`domain.User` ([`user.go:17`](../../internal/domain/user.go#L17)) has seven
fields including `PasswordHash`. `userResponse` has five and no hash. The
handler copies from one to the other in `newUserResponse`.

The shortcut would be to serialise `domain.User` directly and hide the hash:

```go
PasswordHash string `json:"-"`
```

It works today. It was rejected for five reasons:

1. **It is a denylist.** Every field is public unless someone remembers to
   mark it. The next sensitive column — a reset token, a failed-login count, an
   internal note — leaks the moment it is added, and nothing fails. A separate
   struct is an allowlist: a field reaches a client only when someone types it
   into `userResponse`. Forgetting is safe.
2. **It protects one encoder.** `json:"-"` means nothing to `log.Printf("%+v",
   user)`, to an XML or CSV export, or to another JSON library reading a
   different tag. The hash is hidden in exactly one of the places it could
   escape.
3. **It breaks the other direction.** The same tag stops JSON *decoding* into
   the field. The day a user is cached in Redis as JSON, the hash is silently
   dropped on the way in and every cached user fails to log in.
4. **It ties the API to the table.** Rename a column and the public JSON
   changes with it. With two structs the database can be refactored freely and
   the API contract changes only on purpose.
5. **It puts JSON in the domain.** `domain.User` has no tags at all, by the
   same reasoning that kept HTTP status codes out of `domain` in section 21.

**What breaks without it.** [`TestRegister`](../../internal/api/auth_handler_test.go#L187)
asserts the response has *exactly* five keys, and that neither the hash, the
password, nor the words `password` or `hash` appear anywhere in the body.
Adding a field to `domain.User` cannot change that; adding one to
`userResponse` fails the test until the expectation is updated on purpose.

**The cost** is one small struct and one copying function per resource. That
is the trade: a few lines of boring code against a class of leak.

**DTO** stands for *data transfer object* — a type whose only job is to define
what crosses a boundary. `registerRequest` is one too, for the inbound
direction, and for the same reason it has no `role` field.

---

## 32. Registration

**📍 Open:** [`internal/api/auth_handler.go:142`](../../internal/api/auth_handler.go#L142)

**Candidates only.** The role is not read from the request and then checked. It
is not read at all: `registerRequest` has no such field, the JSON decoder drops
unknown keys, and [`auth_handler.go:180`](../../internal/api/auth_handler.go#L180)
writes the constant. A request carrying `"role":"admin"` gets a candidate
account — tested, and confirmed against the running server. Letting a client
set fields it should not is called *mass assignment*, and the defence is the
same allowlist idea as section 31.

**No "does this email exist?" check before the insert.** It looks tidy and has
a race: two requests for the same address can both see "no", and both insert.
Only the unique index can decide, because only the database sees both. So the
handler just inserts ([`auth_handler.go:176`](../../internal/api/auth_handler.go#L176))
and the store reports the index's verdict as `ErrDuplicate` → 409.

**The email is stored lowercased**, as asked. Note that the migration's comment
says the column keeps the address as typed; that comment is now out of date
(see *Still open*). Uniqueness never depended on it — the index is on
`lower(email)` either way.

**`full_name` is required** because the `users` table declares it `NOT NULL`.
The brief did not mention it; without it every insert would fail.

**201, not 200**, because a resource was created.

**The 409 is itself a small leak.** It tells anyone who asks whether an address
is registered — the very thing section 33 goes to lengths to hide at login. It
is what was asked for and what most sites do, because the alternative ("if that
address is new, we have emailed you a link") needs an email service. Until
then, rate limiting on this endpoint is the mitigation.

---

## 33. Login, and user enumeration

**📍 Open:** [`internal/api/auth_handler.go:192`](../../internal/api/auth_handler.go#L192)

**User enumeration** is finding out which accounts exist. Suppose login
answered honestly:

```text
unknown@example.com / x   →  404 "no account with that email"
alice@example.com   / x   →  401 "wrong password"
```

An attacker with a list of a million addresses from some other site's breach
sends each one with any password and keeps those that answer 401. They now
hold:

- **A target list for password guessing.** Every guess from here on is spent on
  an account that exists.
- **A target list for credential stuffing** — trying the password each person
  used on the breached site.
- **A phishing list.** "Your HireDesk application has been updated" is far more
  convincing sent only to people who have a HireDesk account.
- **A privacy breach in itself.** On a hiring platform, the fact that someone
  has an account means they are looking for a job. Their current employer can
  check.

So both failures return the same thing. Real output, byte for byte identical:

```json
{"error":{"code":"invalid_credentials","message":"Email or password is incorrect.","details":{}}}
```

**Same bytes is not enough: same time.** bcrypt at cost 12 takes about a third
of a second (section 3). If an unknown email returned immediately, the
attacker would not need to read the response — they would time it. So at
[`auth_handler.go:211`](../../internal/api/auth_handler.go#L211) the
not-found path calls `Verify` with an empty hash, which runs a full comparison
against the built-in dummy hash from section 6 and always returns false.

Measured on the running server, real bcrypt, real Postgres:

| Request | Status | Time |
|---|---|---|
| Wrong password, known email | 401 | 371.4 ms |
| Unknown email | 401 | 371.5 ms |
| Correct password | 200 | 372.4 ms |

**What breaks without that one line.**
[`TestLoginFailuresAreIndistinguishable`](../../internal/api/auth_handler_test.go#L421)
compares status, body and headers, and then counts password comparisons. With
the line deleted the bodies still match and the test fails on the count: one
comparison for two requests.

**One sentinel does the work.** Both paths call
`Respond(c, domain.ErrInvalidCredentials)`. There is no second message that
could drift from the first.

**A real database failure is not disguised as a 401.** Only `ErrNotFound`
takes the quiet path ([`auth_handler.go:206`](../../internal/api/auth_handler.go#L206)).
Anything else is a 500 and a log line; telling users their password is wrong
because the database is down would be a lie that sends them to reset it.

**Login does not apply the password rules.** `loginRequest` has `required`
and no `min=8`. Login compares; it does not judge. If the minimum is raised to
12 next year, everyone with a 9-character password must still get in.

**`Cache-Control: no-store`** on the success response, so no browser cache or
proxy keeps a copy of the token.

**The response uses OAuth's field names** — `access_token`, `token_type`,
`expires_in` — because HTTP client libraries already know them.

---

## 34. The router, and `/v1`

**📍 Open:** [`internal/api/router.go:55`](../../internal/api/router.go#L55)

`router.Group("/v1")` returns an object with the same `GET`/`POST` methods as
the engine, which prefixes every path registered on it. Groups nest, so
`/v1` + `/auth` + `/login` is `POST /v1/auth/login`.

**Why version at all, before there is a second version.** An API's URLs are a
promise to every client that has been written against it. A mobile app in
someone's pocket cannot be updated on demand. With a prefix, a breaking change
ships as `/v2` *beside* `/v1`, and old clients keep working. Adding the prefix
later means changing every URL — which is itself the breaking change it was
meant to avoid.

**Why the probes are outside it.** `/healthz` and `/readyz` are read by Docker
and load balancers, whose configuration names the path. They are not part of
the API's contract and do not change when it does.

**A group is also where shared middleware goes.** The next step adds
`v1.Group("/jobs", requireAuth)` and every route in it is protected by one
line.

**The routes moved out of `main`.** `NewRouter` takes a
[`Deps`](../../internal/api/router.go#L14) struct and returns a finished
engine, so a test can build the same router the server runs —
[`TestRoutes`](../../internal/api/router_test.go#L20) does, and checks that
`/auth/register` without the version is a 404.

**Rejected: versioning by header** (`Accept: application/vnd.hiredesk.v1+json`).
Purists prefer it because the URL then names only the resource. It cannot be
typed into a browser or seen in an access log, and for an API meant to be
explored by someone reading the README that matters more.

---

## 35. Go idioms appearing for the first time in the handlers

**Interfaces are satisfied implicitly.**
[`health.go:14`](../../internal/api/health.go#L14)

```go
type Pinger interface {
	Ping(ctx context.Context) error
}
```

`*pgxpool.Pool` has a method with exactly that signature, so it *is* a
`Pinger`. pgx was written years before this interface and has never heard of
it. There is no `implements` keyword; the compiler checks at the point of use
(`DB: pool` in `main`). This is why Go interfaces are usually small and are
declared by the code that *needs* the behaviour, not the code that provides
it: "accept interfaces, return structs".

**A function that returns a function.**
[`health.go:28`](../../internal/api/health.go#L28)

Gin wants a `func(*gin.Context)`. `readyz` needs a database and a timeout as
well. So `readyz(db, timeout)` is called once at startup and *returns* the
handler; the returned function keeps `db` and `timeout` alive for as long as
it exists. That is a closure, already met as the injected clock in section 17.

**Method values.** [`router.go:61`](../../internal/api/router.go#L61)

`deps.Auth.Register` with no parentheses is not a call. It is a function value
with the receiver already bound — a `func(*gin.Context)` that remembers which
`AuthHandler` it belongs to. It is the method equivalent of the closure above.

**Build tags.**
[`users_integration_test.go:1`](../../internal/store/users_integration_test.go#L1)

`//go:build integration` on the first line means the file is compiled only
when the build is given `-tags integration`. A plain `go test ./...` does not
see it at all, which is how the database tests stay out of the way of anyone
without a database.

**`sync.Mutex` in the fake.**
[`auth_handler_test.go:28`](../../internal/api/auth_handler_test.go#L28)

A Go map is not safe for concurrent use; two goroutines writing at once is a
crash. The fake store guards its map with a mutex, as any shared in-memory
state must. The real store does not need one because the pool and Postgres do
the coordinating.

**`const` strings joined at compile time.**
[`users.go:56`](../../internal/store/users.go#L56)

`` `INSERT … RETURNING ` + userColumns `` is two constants concatenated by the
compiler into one. No string is built at run time, and nothing a user supplies
is ever part of it.

---

## 36. Two tokens, two jobs

**📍 Open:** [`internal/auth/refresh.go:15`](../../internal/auth/refresh.go#L15)

Section 15 ended on a debt: an access token lasts fifteen minutes because it
cannot be recalled, and something has to stop that meaning "log in again every
fifteen minutes". The refresh token is that something.

| | Access token | Refresh token |
|---|---|---|
| What it is | a signed JWT | 32 random bytes |
| Meaning | carries claims anyone can read | none — it is only a lookup key |
| Checked by | verifying a signature, no database | finding its hash in the database |
| Lifetime | 15 minutes | 30 days, restarting on every use |
| Sent | with every API request | to `/v1/auth/refresh` and `/v1/auth/logout` only |
| Revocable | no | yes, immediately |
| Uses | unlimited until expiry | **one** |

The two designs are opposites on purpose. The access token is cheap to check
and impossible to revoke, so it is short-lived. The refresh token costs a
database query and can be revoked at once, so it may live long — and because
it is sent rarely, it is exposed rarely.

**Rejected: making the refresh token a JWT too.** It would carry an expiry and
a subject without a lookup. But a refresh token *must* be looked up — to see
whether it was revoked — so the signature buys nothing, and a random string has
no algorithm to confuse and no payload to leak.

---

## 37. Why `math/rand` is dangerous for tokens

**📍 Open:** [`internal/auth/refresh.go:33`](../../internal/auth/refresh.go#L33)

Go has two packages called `rand`, and they answer different questions.

**`math/rand` asks: "does this look random?"** It is built for simulations,
shuffling and tests. Its numbers come from a formula: start from a *seed*, apply
arithmetic, emit a number, repeat. Three properties follow, all fatal for a
secret:

1. **Same seed, same sequence.** That is a feature — a simulation can be
   replayed exactly. For a token it means anyone who learns or guesses the seed
   can generate every token the server will ever issue.
2. **Seeds are guessable.** The traditional seed is the current time. A server
   started "some time this week" has a few hundred billion possible nanosecond
   seeds, which is a modest search for one machine.
3. **The state can be worked out from the output.** The generator's whole
   internal state is a few numbers. An attacker who collects enough tokens —
   by simply logging in repeatedly — can solve for that state and predict the
   next token, which will belong to somebody else.

Newer Go versions seed the package's top-level functions randomly, which fixes
the first two for those functions. The package documentation still says it is
not for security-sensitive work, and code that builds its own generator with
`rand.New(rand.NewSource(seed))` has all three problems. "It is usually fine
now" is not a property to rest an authentication system on.

**`crypto/rand` asks: "can anyone predict this?"** It does not run a formula
of its own. It reads from the operating system's cryptographic generator
(`getrandom` on Linux, `arc4random` on macOS), which is continuously fed by
unpredictable physical events — interrupt timings, hardware noise — and is
designed so that seeing every previous output tells you nothing about the next
one. There is no seed to guess and no state to reconstruct.

| | `math/rand` | `crypto/rand` |
|---|---|---|
| Purpose | simulations, shuffles, tests | keys, tokens, salts |
| Source | arithmetic on a seed | the operating system |
| Reproducible | yes, by design | no, by design |
| Predictable from past output | yes | no |
| Speed | very fast | fast enough |

**How much randomness.** [`refresh.go:19`](../../internal/auth/refresh.go#L19)
asks for 32 bytes: 256 bits, 2²⁵⁶ ≈ 10⁷⁷ possible tokens. Guessing a live one
is not a matter of patience; there are more possibilities than atoms in the
galaxy.

**The import is the whole safeguard.** Both packages export `Read`, so
`rand.Read(raw)` compiles with either. The only line that decides is the import
at the top of the file. That is why this repository's token code lives in one
small file, and why
[`TestNewRefreshTokenIsDifferentEveryTime`](../../internal/auth/refresh_test.go#L49)
exists — a thousand draws with no repeat cannot prove unpredictability, but a
generator with a fixed seed fails it on the second run.

---

## 38. Why the database holds a hash, and why SHA-256 is enough

**📍 Open:** [`internal/auth/refresh.go:58`](../../internal/auth/refresh.go#L58)

**Why not store the token.** A refresh token is a password the server chose.
Anyone holding it *is* that user for up to thirty days. If the `refresh_tokens`
table held tokens, then everything that can read the table could log in as
everyone: a leaked backup, a read-only reporting account, an SQL injection in
some unrelated query, a curious person with database access.

Storing the hash makes the table useless for that. The API accepts a token and
hashes it; it does not accept a hash. And a hash cannot be reversed
(section 1). Checked on the running server: after five logins and refreshes,
none of the issued tokens appears anywhere in the table, and every stored value
is 64 hex characters.

**Then why not bcrypt, as for passwords?** Section 2 argued at length that a
*fast* hash is the wrong tool. That argument had a premise, and the premise
does not hold here.

bcrypt defends against **guessing**. A stolen password hash is attacked by
hashing candidate passwords and comparing. That works because people choose
passwords from a small space — a few billion likely strings. bcrypt cannot
shrink that space; it makes each guess cost a third of a second instead of a
nanosecond, turning hours into centuries.

The measure of "how big is the space" is **entropy**, in bits:

| Input | Entropy | Guesses needed | With fast SHA-256 (10¹² per second) |
|---|---|---|---|
| A typical human password | 20–40 bits | 10⁶ – 10¹² | **under a second** |
| A refresh token | 256 bits | 10⁷⁷ | 10⁵⁷ years |

For the token, slowing each guess down is pointless: there is no list of likely
tokens to try. Making 10⁷⁷ guesses a million times slower leaves a number that
still means "never".

So bcrypt would cost a third of a second of CPU on every refresh and buy
nothing. And it would cost one more thing: **bcrypt hashes cannot be looked
up.** Each has its own salt, so the same input hashes differently every time
(section 4). There would be no way to find a token's row except by trying
every row. SHA-256 with no salt gives the same output for the same input, so
`WHERE token_hash = $1` is an index lookup.

**Why no salt.** A salt exists to stop one precomputed table cracking many
hashes, and to stop two users with the same password having the same hash.
Neither can happen with random 256-bit inputs: no table can cover the space,
and no two tokens are ever equal.

**The rule to take away.** Slow, salted hashes are for secrets *people choose*.
Fast hashes are fine for secrets *the server generates* with enough randomness.
The same reasoning covers API keys, password-reset links and email-verification
tokens.

**The lookup is not a timing leak.** Comparing secrets with `==` can leak how
many leading characters matched. Here the attacker controls the token, not its
hash, and cannot steer the hash towards a chosen prefix — that would mean
breaking SHA-256.

**The store never sees a token.** Generation and hashing live in `auth`; every
method of [`TokenStore`](../../internal/store/tokens.go#L33) takes a hash. The
one place a token exists in memory is the handler, between the request and the
response.
[`TestLoginIssuesARefreshTokenAndStoresOnlyItsHash`](../../internal/api/session_test.go#L206)
checks what the store was handed.

**A malformed token never reaches the database.**
[`HashRefreshToken`](../../internal/auth/refresh.go#L45) returns `false` for
anything that is not exactly 32 bytes of base64url, so garbage — including a
megabyte of it, which is in the test table — is refused with no query.

---

## 39. What rotation buys

**📍 Open:** [`internal/store/tokens.go:104`](../../internal/store/tokens.go#L104)

**Rotation** means a refresh token works once. Each exchange returns a *new*
refresh token along with the new access token, and the one presented is
revoked. A session is therefore a chain:

```text
login → R1 ──use──▶ R2 ──use──▶ R3 ──use──▶ R4 …
        revoked     revoked     revoked     live
```

Without rotation the same token would be presented every fifteen minutes for
thirty days. Rotation buys three things:

1. **A stolen token has a short useful life.** A copy of R2 is worth something
   only until the real client next refreshes — at most fifteen minutes for an
   active user — instead of thirty days.
2. **Theft becomes visible.** This is the important one. If tokens are
   single-use, then a token arriving a second time proves two parties hold it.
   A reusable token gives the server nothing to notice: the thief's requests
   look exactly like the owner's. Section 40 is what is done with that signal.
3. **Sessions slide.** Each new token gets a fresh thirty days. Someone who
   uses the app weekly never logs in again; a session nobody uses dies on its
   own.

**The exchange is one transaction.** Revoking the old token and storing the new
are two statements. A crash between them would leave the user with neither.
`BEGIN … COMMIT` makes them all-or-nothing, and
[`TestTokenStoreRotateIsAllOrNothing`](../../internal/store/tokens_integration_test.go#L280)
forces the second statement to fail and checks the first was undone.

**Revoked rows are kept, not deleted.** Detection depends on recognising a
token that *used to be* valid. A deleted row is indistinguishable from a token
that never existed, and the signal would be lost.

**What it costs.** One write per refresh, and a client that must store the new
token every time. A client that refreshes and then fails to save the answer
has logged itself out.

---

## 40. A reused token ends every session

**📍 Open:** [`internal/api/auth_handler.go:281`](../../internal/api/auth_handler.go#L281)

A revoked token arrives at `/v1/auth/refresh`. A correct client never sends
one: it replaces its token the moment it is exchanged. So two parties hold
tokens from this account, and one of them should not.

The obvious response is to refuse that one request. Here is why that is not
enough. There are two ways the situation can have arisen, and **the server
cannot tell them apart**:

```text
Case A — the thief got there first        Case B — the owner got there first

  thief  presents R1 → gets R2              owner  presents R1 → gets R2
  owner  presents R1 → REPLAY               thief  presents R1 → REPLAY
```

Both look identical from the server: R1 used, then R1 used again. Now suppose
only the replayed request is refused:

| | Refuse only the replay | Revoke everything |
|---|---|---|
| **Case A** | The owner is rejected. **The thief holds R2 and keeps the account** for as long as they keep refreshing. | Thief's R2 is dead. Owner logs in again. |
| **Case B** | Thief is rejected. Fine. | Owner's R2 is dead too. Owner logs in again. |

In Case A, refusing only the replay punishes the victim and rewards the
attacker — the alarm goes off and the burglar stays in the house. Since A and B
are indistinguishable, the server has to act as though it is A. The only cost
of being wrong is that a legitimate user types their password again.

That is the whole argument: **the replayed token is not the dangerous one. It
is already dead. The dangerous token is the one issued by the earlier use**,
and the only way to be sure of killing it is to kill everything.

**Why every session and not just this chain.** Strictly, only the chain
descended from the stolen token is known to be compromised, and some systems
track a "family" ID to revoke exactly that. This one revokes all of the user's
tokens, because whatever exposed one token — malware, a shared computer, a
leaked backup — may well have exposed the others, and the table has no family
column. Erring wide costs a few more logins.

**The response gives nothing away.** The replay gets the same 401 body as an
expired token, an unknown token, or garbage.
[`TestRefreshFailuresAreIndistinguishable`](../../internal/api/session_test.go#L347)
compares six kinds of failure byte for byte. A thief should not learn that
detection fired.

**The server does say so in its own log**, at
[`auth_handler.go:297`](../../internal/api/auth_handler.go#L297): user ID,
client address and how many sessions were ended, and never a token or hash.
This is the one expected-path event that is logged, because it is the one
somebody should look at.

**If the mass revocation itself fails**, the request is answered with a 500 and
still issues nothing; the presented token is already revoked.
[`TestRefreshReplayWhenRevocationFails`](../../internal/api/session_test.go#L426).

**What breaks without it.**
[`TestRefreshReplayRevokesEverySession`](../../internal/api/session_test.go#L291)
logs one user in on two devices, rotates one token, replays the old one, and
then checks that the rotated token, the other device and nothing belonging to
a second user are affected as they should be. On the running server: two live
tokens before the replay, zero after.

**What it does not do.** Access tokens already issued keep working until they
expire. Measured on the running server: after the replay revoked everything,
the old access token still got a 200 from `/v1/me`. That is the fifteen-minute
window of section 15, and closing it needs the `jti` denylist listed under
*Still open*.

**A logged-out token counts as reuse.** Logout revokes a token, so presenting
it afterwards takes the same path and ends the user's other sessions as well.
That is deliberate — nothing should present a logged-out token — but it does
mean a buggy client can sign its user out everywhere.

---

## 41. Two requests, one token: the row lock

**📍 Open:** [`internal/store/tokens.go:128`](../../internal/store/tokens.go#L128)

Rotation says "one token in, one token out". Written the obvious way, that is
false under load:

```text
Request A                        Request B
SELECT … → not revoked
                                 SELECT … → not revoked
UPDATE revoke old
INSERT new token (A)
                                 UPDATE revoke old   (already revoked, no error)
                                 INSERT new token (B)
```

Both requests saw a valid token, because both looked before either wrote. One
token has produced two, and neither request was flagged. If A is a thief and B
the owner, the thief now holds a perfectly good session that no later check
will ever catch — the detection in section 40 has been bypassed entirely by
arriving at the same moment.

**`FOR UPDATE` closes the window.** It locks the selected row until the
transaction ends. The second request's `SELECT` waits at that line. When the
first commits, the second proceeds, re-reads the row, finds it revoked, and is
reported as reuse — which revokes the token the first request just received.

**Verified by removing it.**
[`TestTokenStoreRotateConcurrently`](../../internal/store/tokens_integration_test.go#L301)
starts twenty goroutines that present the same token at the same instant,
against a real PostgreSQL:

| | Successful rotations | Refused as reuse | Live tokens afterwards |
|---|---|---|---|
| With `FOR UPDATE` | **1** | 19 | 1 |
| With `FOR UPDATE` deleted | **2** | 18 | 2 |

**Why this is in the store and not the handler.** The request asked for four
store functions — create, look up, revoke one, revoke all — and they are all
there. But a handler that calls "look up" and then "revoke" has the gap drawn
above between the two calls, and nothing in Go can close it; only the database
sees both requests. So the exchange is a fifth method, and the handler makes
one call.

**The policy stays in the handler.** `RotateRefreshToken` only *reports* reuse,
as a [`*domain.TokenReuseError`](../../internal/domain/token.go#L37) carrying
the user ID. Deciding to revoke everything, logging it and choosing the
response are in `Refresh`, where they can be read in one place.

**Revoked is checked before expired**
([`tokens.go:140`](../../internal/store/tokens.go#L140)). A replayed token is a
replay however old it is, and the integration test has a row that is both.

**A known sharp edge.** A legitimate client that sends the same refresh twice
— a retry after a timeout where the first response was lost — is treated as an
attack and signed out everywhere. Some systems allow a few seconds' grace for
this. This one does not, on the view that being logged out is recoverable and a
missed theft is not.

---

## 42. Logout

**📍 Open:** [`internal/api/auth_handler.go:331`](../../internal/api/auth_handler.go#L331)

Logout revokes the presented refresh token and returns **204 No Content**.

**Only that token.** Logging out on a phone leaves the laptop signed in.

**It does not require an access token.** The refresh token in the body is the
credential. A user whose access token expired five minutes ago must still be
able to log out, and requiring a valid access token would force a refresh
first — creating a session in order to end one.

**It always answers 204**, for a live token, an already-revoked one, one that
never existed, and garbage
([`TestLogoutIsIdempotentAndRevealsNothing`](../../internal/api/session_test.go#L476)).
An endpoint that said "no such token" would be a free way to test guesses.

**The revocation never overwrites.** `AND revoked_at IS NULL` at
[`tokens.go:70`](../../internal/store/tokens.go#L70) means a second logout
leaves the first timestamp alone, so the row keeps recording when the token
really died.

**What logout cannot do** is cancel the access token, for the reason in
section 40. A client should discard it; a copy elsewhere works for up to
fifteen minutes.

---

## 43. The auth middleware, and `/v1/me`

**📍 Open:** [`internal/api/middleware.go:24`](../../internal/api/middleware.go#L24)

**Middleware** is a function that runs before a handler and can stop the
request. `requireAuth` reads `Authorization: Bearer <token>`, verifies the
token with the `TokenManager` from section 11, and either answers 401 or
leaves the verified claims on the request for the handler.

It is attached to a **group**
([`router.go:73`](../../internal/api/router.go#L73)), not to a route. Every
route registered on `protected` is guarded, so protecting the next endpoint is
adding a line inside the braces, and forgetting to protect it is impossible.

**Two 401s, on purpose:**

| Code | Means | Client should |
|---|---|---|
| `token_expired` | genuine token, past its fifteen minutes | call `/v1/auth/refresh`, then retry |
| `unauthenticated` | no token, or one that does not verify | send the user to log in |

This is what `ErrExpiredToken` was kept separate for in section 15. It leaks
nothing: `exp` is readable in the token by anyone (section 12).

**`WWW-Authenticate: Bearer`** is sent on every 401 from a protected route.
RFC 6750 requires it; it tells a client which scheme the server expects.

**A refresh token is not an access token.** Sending one as a Bearer token gets
a 401 — it is not a JWT. And an access token sent to `/v1/auth/refresh` gets a
401 — it is not 32 bytes of base64url. Both are in the test tables.

**`/v1/me` reads the database**
([`auth_handler.go:354`](../../internal/api/auth_handler.go#L354)) rather than
answering from the claims. The token says who the caller was when it was
issued. Reading the row makes the answer current and means a user deleted
since then gets a 401, not a profile
([`TestMeForADeletedUser`](../../internal/api/session_test.go#L599)).

**How the claims travel.** `c.Set` stores a value on the request under a string
key, and `c.Get` returns it as `any`.
[`claimsFrom`](../../internal/api/middleware.go#L63) turns that back into
`*auth.Claims` with a type assertion (section 45), so handlers never touch the
key.

**Not built yet: roles.** `requireAuth` proves *who*. Nothing yet checks *what
they may do*; that is a second, small middleware reading `claims.Role`, and it
arrives with the first endpoint that needs it.

---

## 44. The seed command

**📍 Open:** [`cmd/seed/main.go:95`](../../cmd/seed/main.go#L95)

`make seed` creates four accounts so the API can be used without registering:

| Role | Email | Password |
|---|---|---|
| admin | `admin@hiredesk.test` | `admin-password` |
| interviewer | `interviewer@hiredesk.test` | `interviewer-password` |
| candidate | `candidate1@hiredesk.test` | `candidate-password` |
| candidate | `candidate2@hiredesk.test` | `candidate-password` |

It is the only way to get an admin or an interviewer: registration creates
candidates and nothing else (section 32).

**Idempotent** means running it twice has the same result as running it once.
Each account is looked up first and skipped if present. That lookup is only an
optimisation, to avoid a third of a second of bcrypt per account. What actually
makes a second run safe is the unique index: if two runs race, the loser's
insert fails with `ErrDuplicate` and is treated as "already there"
([`main.go:122`](../../cmd/seed/main.go#L122)). Same reasoning as registration
in section 32.

**An existing account is never changed.** If you alter the admin's password
and run the seed again, your password stays.

**It refuses to run in production**
([`main.go:65`](../../cmd/seed/main.go#L65)). These passwords are in a public
repository. The project rule is that secrets are never committed; these are
committed precisely because they are *not* secrets, and the guard is what keeps
that true. `.test` is a top-level domain reserved so that it can never be
registered, so none of the addresses can belong to anyone.

**It is a second binary in the same image.** `cmd/seed` is built next to
`cmd/api` in the [`Dockerfile`](../../Dockerfile#L31), and `make seed`
([`Makefile:134`](../../Makefile#L134)) runs it inside the running API
container, which already has `DATABASE_URL`. Nothing is needed on the host but
Docker.

**It goes through the same code as the API** — `config.Load`, `store.New`,
`auth.Hash`, `UserStore.CreateUser` — so a seeded user is exactly what a
registered one would be, bcrypt cost included.

**Rejected: a SQL file of `INSERT`s.** It would need password hashes written
into the repository, computed by hand, and would silently go stale the day
`Cost` changes.

---

## 45. Go idioms appearing for the first time in the session code

**A transaction, and `defer tx.Rollback`.**
[`tokens.go:108`](../../internal/store/tokens.go#L108)

`Begin` returns a `tx` with the same `Exec` and `QueryRow` methods as the pool,
but everything run on it is provisional until `Commit`. The `defer` on the next
line is the standard pattern: every early `return` in the function rolls back
automatically, so no error path can forget to. After a successful `Commit`,
`Rollback` does nothing.

**A pointer for "may be absent".**
[`domain/token.go:21`](../../internal/domain/token.go#L21)

`RevokedAt *time.Time`. A `time.Time` always holds some instant; it has no
value meaning "never". A pointer adds one: `nil`. This is the usual way to
represent a nullable column, and pgx fills it in by itself.

**Arrays and slices.** [`refresh.go:59`](../../internal/auth/refresh.go#L59)

`sha256.Sum256` returns `[32]byte` — an **array**, whose length is part of its
type and which is copied when assigned. `hex.EncodeToString` wants `[]byte` — a
**slice**, a view onto some array with a length that can vary. `sum[:]` makes a
slice covering the whole array. Most Go code uses slices; arrays appear where a
size is fixed by definition, as a hash's is.

**Named results.** [`refresh.go:26`](../../internal/auth/refresh.go#L26)

`func NewRefreshToken() (token, hash string, err error)` names its return
values. Here that is documentation only: two strings come back, and the names
say which is which. A caller who swapped them would store the token and hand
out the hash.

**A type assertion with `ok`.**
[`middleware.go:63`](../../internal/api/middleware.go#L63)

`value.(*auth.Claims)` converts an `any` back to a concrete type. The
one-result form panics if the type is wrong; the two-result form,
`claims, ok := …`, reports it. `errcheck` in this repository rejects the
one-result form.

**`switch` with no value.**
[`auth_handler.go:281`](../../internal/api/auth_handler.go#L281)

`switch { case cond1: … case cond2: … default: … }` is an `if / else if` chain
laid out vertically. A `case` can list several conditions separated by commas,
meaning "any of these".

**Goroutines, a `WaitGroup` and a channel as a starting gun.**
[`tokens_integration_test.go:301`](../../internal/store/tokens_integration_test.go#L301)

`go func() { … }()` starts a function running concurrently. `sync.WaitGroup`
counts them so the test can wait for all twenty. Each one first blocks on
`<-release`; `close(release)` unblocks every reader at once, so the requests
truly overlap rather than running one after another. A `sync.Mutex` guards the
counters they all write to.

**`for range n`.** `for range 1000` runs the body a thousand times, with no
loop variable needed.

---

## Verified

| Check | Result |
|---|---|
| `go test -race -cover ./...` | `auth` **94.1%**, `api` **87.8%**, `domain` **92.3%**, `config` 59.8%, `cmd/seed` 47.4%, `store` 6.5% without the integration tag |
| Uncovered in `auth` | two error wraps that need the OS random source or HMAC itself to fail |
| **Passwords** | |
| Same suite at cost 12 throughout, `-race` | passed, but **38.7 s** — the reason for section 9's split |
| Correct password | verifies |
| Wrong password, wrong case, empty password | all `false` |
| Empty password to `Hash` | `ErrEmptyPassword`, empty string returned |
| Same password hashed twice | two different strings, both verify |
| 72 bytes / 73 bytes / 19 emoji (76 bytes) | accepted / `ErrPasswordTooLong` / `ErrPasswordTooLong` |
| 7 malformed hashes (empty, garbage, plaintext, truncated, bad version, bad cost, bad prefix) | all `false`, no panic |
| Malformed-hash path timing | within 4× of the well-formed path; **fails** with the fallback removed (7.9 µs against 23.6 ms) |
| Raw bcrypt, 72 correct bytes + 1 more | **accepted** by the library — `Verify` rejects it |
| Raw bcrypt, empty password | **hashed** by the library — `Hash` rejects it |
| Time per hash at cost 10 / 12 / 14 | 95 ms / 385 ms / 1.52 s |
| **Tokens** | |
| Sign then parse | same `sub`, `role`, `iat`, `exp` (= `iat` + 15 min); `jti` present |
| Two tokens, same user, same second | different `jti` |
| Payload decoded with base64 + JSON only | readable, exactly five claims |
| 1 s before expiry / at expiry / 1 s after | accepted / `ErrExpiredToken` / `ErrExpiredToken` |
| Signed with a different secret | `ErrInvalidToken` |
| Different secret **and** expired | `ErrInvalidToken`, not expired |
| Payload tampered, role → `admin` | `ErrInvalidToken` — `signature is invalid` |
| Signature removed | `ErrInvalidToken` |
| `alg: none` | `ErrInvalidToken` |
| `alg: HS384` with the correct secret | `ErrInvalidToken`; **accepted** with the `alg` check removed |
| `alg: ES256`, attacker's key | `ErrInvalidToken` |
| Correctly signed, no `exp` | `ErrInvalidToken` |
| Empty string / `not.a.token` | `ErrInvalidToken` |
| Empty user ID or role to `NewAccessToken` | error, no token |
| **Secret validation** | |
| `go run ./cmd/api`, `JWT_SECRET` unset | `missing required environment variable(s): JWT_SECRET`, exit 1 |
| `go run ./cmd/api`, `JWT_SECRET=tooshort` | `JWT_SECRET is too short: 8 characters, need at least 32`, exit 1 |
| `go run ./cmd/api`, the `.env.example` placeholder | `too short: 12 characters`, exit 1 |
| 31 / 32 / 64 characters | rejected / accepted / accepted |
| 20 characters padded with 20 spaces | rejected, reported as 20 |
| Secret value in the error message | never present |
| **Errors** | |
| Each of the six sentinels through `Respond` | 404, 409, 401, 403, 422, 409 with the matching `code` |
| Sentinel wrapped once / twice / alongside a driver error | same status and code; none of the wrapping text in the body |
| Same rows with `errors.Is` replaced by `==` | bare sentinels pass, the three wrapped rows and the validation test **fail** |
| `%w` against `%v` on the same error | `errors.Is` true / false |
| `ValidationError` with two fields, wrapped | 422, both fields in `details` |
| `ValidationError` with no fields | `Err()` is `nil` |
| Unexpected error carrying a Postgres message | 500, fixed message; constraint, email and SQLSTATE absent from the body, present in the log |
| Same test with the 500 branch sending `err.Error()` | **fails** on every fragment |
| Query string of the failing request | absent from the log |
| Domain errors | nothing logged |
| Handler registered after one that calls `Respond` | does not run |
| Unknown path / wrong method / panic | 404 `route_not_found` / 405 `method_not_allowed` / 500 `internal_error`, all in the envelope |
| Every error body | exactly one top-level key and exactly `code`, `message`, `details`; `details` always an object |
| **Store** (real PostgreSQL 16, all six migrations applied) | |
| `go test -race -tags integration ./internal/store/` | **passes** — 11 integration tests and 2 unit tests |
| Create, then read by email and by ID | identical rows; UUID and timestamps assigned by the database |
| Read by email in upper case | same row |
| Second insert, same email in upper case | `ErrDuplicate`; `errors.As` reads `23505` and `users_email_lower_idx` from the wrapped error |
| Unknown email / unknown ID / ID `42` / empty ID | `ErrNotFound` for all four |
| Role `superuser` | refused by `users_role_check`, and **not** reported as a duplicate |
| `EXPLAIN` of the email lookup | `Index Scan using users_email_lower_idx` |
| Rows left behind by the tests | none |
| **Registration and login** (the real binary, real bcrypt, same database) | |
| Register `"  Demo@Example.COM "` with `"role":"admin"` | **201**; email `demo@example.com`, role `candidate`, five fields, no hash |
| Stored row | `demo@example.com`, `candidate`, hash begins `$2a$12$`, 60 characters |
| Register the same address again | **409** `duplicate` |
| Register with a bad email, 5-character password, no name | **422**, three entries in `details` |
| Truncated JSON | **400** `malformed_request` |
| Login, correct password | **200** in 372 ms; token, `Bearer`, `expires_in` 900, user |
| Login, wrong password / unknown email | **401** in 371.4 ms / 371.5 ms; bodies identical byte for byte |
| `POST /auth/login` without `/v1` | **404** `route_not_found` |
| `/healthz`, `/readyz` | 200, 200 |
| Unexpected errors logged during the run | none |
| **Handler tests** (in-memory store) | |
| Register response | exactly `id`, `email`, `full_name`, `role`, `created_at` |
| Seven invalid bodies, including 20 emoji (80 bytes) as a password | 422 with the expected `details` each; nothing stored |
| Five malformed bodies on both endpoints | 400; the decoder's message absent |
| Login token | parses with the server's `TokenManager`; `sub` is the user's ID |
| Wrong password against unknown email | same status, body and headers; one password comparison each |
| Same test with the dummy comparison removed | **fails** — one comparison for two requests |
| Account with a 3-character password | can still log in |
| Store returning a driver error | 500 `internal_error`; cause logged, not returned |
| `/readyz` with the database down | 503, address not in the body; `/healthz` still 200 |
| **Refresh tokens: store** (real PostgreSQL 16) | |
| Create, then read by hash | owner, expiry, `revoked_at` NULL; a second insert of the same hash is `ErrDuplicate` |
| Revoke one, twice | revoked once; the timestamp does not move the second time; other tokens untouched |
| Revoke all for a user with 3 tokens, 1 already revoked | returns 2; another user's token untouched |
| Rotate | returns the owner; old revoked, new live, exactly one live token |
| Rotate an unknown / expired / revoked / revoked-and-expired token | `ErrNotFound` / `ErrTokenExpired` / `TokenReuseError` / `TokenReuseError`; nothing written in any case |
| Rotate where storing the new token fails | old token **not** revoked — the transaction rolled back |
| 20 concurrent rotations of one token, run 5 times | exactly **1** succeeds, 19 are reuse, 1 live token |
| Same test with `FOR UPDATE` deleted | **fails** — 2 succeed, 2 live tokens |
| Rows left behind | none |
| **Refresh tokens: the real binary** (real bcrypt, same database) | |
| Login | 200; refresh token is 43 characters |
| `GET /v1/me` with the access token / with none / with the refresh token | 200 admin / 401 `unauthenticated` + `WWW-Authenticate: Bearer` / 401 |
| Refresh, first use | 200; a different refresh token |
| Live tokens for the user, two sessions open | 2 |
| **Replay** of the used token | 401 `unauthenticated` |
| Live tokens for the user after the replay | **0** |
| Refresh with the token the first use obtained / with the other session | 401 / 401 |
| Refresh with a never-issued token | 401, body identical to the replay's |
| Old access token after everything was revoked | **still 200** — valid until its 15 minutes end |
| Logout / logout again / refresh after logout | 204 / 204 / 401 |
| Login after all of that | 200 |
| `token_hash` values in the table | all 64 characters; no issued token present |
| Server log | `security: refresh token reuse detected for user … revoked 2 session(s)`; no token or hash |
| **Handler tests** (in-memory stores) | |
| What the store is handed at login | the SHA-256 of the token, expiring 30 days out — never the token |
| Replay with two devices and a second user | both devices and the rotated token dead; the other user untouched; login works again |
| Six kinds of refresh failure, including expiry at exactly 30 days | identical 401 bodies |
| One second before the 30 days | refresh succeeds |
| Replay when the mass revocation fails | 500, no tokens issued, cause logged |
| Logout of one device | the other still refreshes |
| Logout with a live, revoked, unknown and malformed token | 204 with no body, all four |
| `/v1/me` with `Bearer`, `bearer`, `BEARER` | 200 |
| `/v1/me` with 11 bad credentials (`alg: none`, wrong secret, no signature, Basic auth, …) | 401 with a Bearer challenge; `token_expired` only for the genuinely expired one |
| `/v1/me` for a user deleted after login | 401 |
| **Token generation** | |
| `NewRefreshToken` | 32 bytes, 43 characters, hash equals an independently computed SHA-256 |
| 1,000 tokens | no repeat |
| 32 zero bytes | the published SHA-256 test value |
| Eight malformed inputs, including 1 MB | rejected without a hash |
| **Seed** (the real binary, same database) | |
| First run | 4 created: 1 admin, 1 interviewer, 2 candidates; hashes begin `$2a$12$` |
| Second run | 0 created, 4 already present, exit 0 |
| `ENV=production` | refuses, exit 1 |
| Lookup says absent, insert says taken | counted as present, no error |
| **Gates** | |
| `gofmt -l .` / `go build ./...` / `go vet ./...` | clean / OK / OK |
| `golangci-lint run ./...`, with and without `--build-tags integration` | **0 issues** |
| `go mod tidy -diff` | clean — `x/crypto`, `jwt/v5` and `validator/v10` are direct dependencies |
| **Before publishing** (re-run on the final tree) | |
| `gofmt -l .` / `go mod tidy -diff` / `go build ./...` / `go vet ./...`, with and without `-tags integration` | all clean |
| `go test -race -count=1 -cover ./...` | all packages pass; coverage as in the first row |
| `go test -race -count=1 -cover -tags integration ./internal/store/...` against a fresh PostgreSQL 16 with the six migrations applied | 13 tests pass, `store` **90.2%** |
| `golangci-lint run ./...`, with and without `--build-tags integration` | **0 issues** |
| `CGO_ENABLED=0 GOOS=linux go build ./cmd/...` — the two build commands in the Dockerfile | both binaries build |
| The two real binaries against that database: seed twice, seed with `ENV=production`, login, `/v1/me` with and without a token, refresh, replay, wrong password, register with a mixed-case email, logout | 4 created then 0 created / refused / 200 / 200 and 401 / 200 / 401 / 401 `invalid_credentials` / 201 with the email lowercased / 204 |
| Every `#L` link in these notes (306) | resolves to an existing line; every target in a file this module added or changed was read against its section |

## Still open

- **The Compose stack was not run for this module.** The store tests and the
  live runs used a temporary PostgreSQL 16 with the migrations applied by
  `psql`. The Dockerfile's two build commands were run on their own and both
  binaries build for Linux, but **`make up`, `make seed` and
  `make test-integration` were not run inside Docker**.
- **Access tokens outlive revocation** by up to fifteen minutes (section 40).
- **No absolute session lifetime.** A thief who steals a token from an
  abandoned session and keeps refreshing is never challenged, because the
  owner never returns to trigger the replay.
- **No grace period for a double-sent refresh** (section 41).
- **A logged-out token presented later ends the user's other sessions**
  (section 40).
- **Refresh tokens travel in the JSON body.** For a browser front end an
  `HttpOnly` cookie is safer, since page scripts cannot read it.
- **Revoked and expired rows are never deleted.** The table grows by one row
  per login and per refresh.
- **No role checks.** `requireAuth` proves who the caller is, not what they
  may do.
- **Registering does not log the user in**; it returns no tokens.
- **Nothing revokes sessions on a password change**, because there is no
  password-change endpoint yet.
- **Token reuse is only logged.** Nobody is alerted and the user is not told.
- **CI does not run the integration tests.** The workflow has no Postgres
  service, so the hand-written SQL is checked only when someone runs
  `make test-integration`.
- **The migration comment on `users.email` is out of date.** It says the
  address is stored as typed; registration now stores it lowercased.
- **Registration reveals whether an email is registered** (section 32).
- **No rate limit on register or login.**
- **No email verification.** Anyone can register any address.
- **Unknown JSON fields are ignored**, not rejected.
- **`Content-Type` is not checked.**
- **Login timing is equal only from the password check on.** A slow database
  lookup for one path and not the other would still show.
- **No request ID.** A 500 cannot yet be matched to its log line except by
  time and path.
- **A cancelled request is logged as an internal error.** When a client
  disconnects mid-query the driver returns `context.Canceled`, which matches no
  row of the table.
- **No rate limit on login** — see section 2.
- **No re-hash on login** when a stored hash's cost is below `Cost`.
- **No `jti` denylist**, so a single token cannot be revoked before `exp`.
- **`role` is an unchecked string.** `NewAccessToken` rejects empty but not
  `"superuser"`; the database's `users_role_check` is the only guard.
- **No secret rotation.** Changing `JWT_SECRET` invalidates every token at
  once. Rotating without that needs a key ID (`kid`) in the header.
- **`internal/config` is at 59.8%.** Only the `JWT_SECRET` paths are tested.

## Next

Run the stack once in Docker (`make up`, `make seed`, `make test-integration`)
— the one thing not yet verified. After that, role checks, and the first
endpoints that are about hiring: job posts.
