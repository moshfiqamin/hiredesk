# Module 3 — Authentication

This note covers password hashing (sections 1–10), access tokens (sections
11–17), the error format every endpoint shares (sections 18–25), registration
and login (sections 26–35), refresh tokens, logout, the current-user endpoint
and the seed command (sections 36–45), the authentication and role
middleware (sections 46–52), resource-level authorization policy (sections
53–57), and applying it: per-role SQL, 403 versus 404, and the authorization
matrix against a real PostgreSQL (sections 58–62).

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
| [16](#16-refusing-to-start-on-a-weak-secret) | Refusing to start on a weak secret | [`config.go:397`](../../internal/config/config.go#L397) |
| [17](#17-go-idioms-appearing-for-the-first-time) | Struct embedding, methods, an injected clock | [`token.go:45`](../../internal/auth/token.go#L45) |
| [18](#18-one-shape-for-every-error) | One shape for every error | [`api/errors.go:30`](../../internal/api/errors.go#L30) |
| [19](#19-sentinel-errors-again-and-why-errorsis-exists) | Sentinel errors again, and why `errors.Is` exists | [`domain/errors.go:18`](../../internal/domain/errors.go#L18) |
| [20](#20-what-w-does-and-why-it-matters-here) | What `%w` does, and why it matters here | [`domain/errors_test.go:33`](../../internal/domain/errors_test.go#L33) |
| [21](#21-why-the-status-codes-live-in-api-and-not-in-domain) | Why the status codes live in `api`, not `domain` | [`api/errors.go:76`](../../internal/api/errors.go#L76) |
| [22](#22-unexpected-errors-log-everything-say-nothing) | Unexpected errors: log everything, say nothing | [`api/errors.go:120`](../../internal/api/errors.go#L120) |
| [23](#23-validation-errors-that-name-each-field) | Validation errors that name each field | [`domain/errors.go:80`](../../internal/domain/errors.go#L80) |
| [24](#24-errors-that-never-reach-a-handler) | Errors that never reach a handler | [`router.go:61`](../../internal/api/router.go#L61) |
| [25](#25-more-go-idioms-appearing-for-the-first-time) | A nil pointer that is not a nil error, `errors.As`, `TestMain` | [`domain/errors.go:101`](../../internal/domain/errors.go#L101) |
| [26](#26-one-request-three-layers) | One request, three layers | [`auth_handler.go:23`](../../internal/api/auth_handler.go#L23) |
| [27](#27-hand-written-sql-with-pgx) | Hand-written SQL with pgx | [`users.go:47`](../../internal/store/users.go#L47) |
| [28](#28-reading-the-postgresql-error-code-with-errorsas) | Reading the PostgreSQL error code with `errors.As` | [`users.go:123`](../../internal/store/users.go#L123) |
| [29](#29-struct-tags-and-how-two-packages-read-the-same-field) | Struct tags, and how two packages read the same field | [`auth_handler.go:74`](../../internal/api/auth_handler.go#L74) |
| [30](#30-validation-decode-clean-check-report) | Validation: decode, clean, check, report | [`validation.go:134`](../../internal/api/validation.go#L134) |
| [31](#31-why-the-response-is-its-own-struct) | Why the response is its own struct | [`auth_handler.go:92`](../../internal/api/auth_handler.go#L92) |
| [32](#32-registration) | Registration | [`auth_handler.go:142`](../../internal/api/auth_handler.go#L142) |
| [33](#33-login-and-user-enumeration) | Login, and user enumeration | [`auth_handler.go:192`](../../internal/api/auth_handler.go#L192) |
| [34](#34-the-router-and-v1) | The router, and `/v1` | [`router.go:103`](../../internal/api/router.go#L103) |
| [35](#35-go-idioms-appearing-for-the-first-time-in-the-handlers) | Interfaces, build tags, functions that return functions | [`health.go:14`](../../internal/api/health.go#L14) |
| [36](#36-two-tokens-two-jobs) | Two tokens, two jobs | [`refresh.go:15`](../../internal/auth/refresh.go#L15) |
| [37](#37-why-mathrand-is-dangerous-for-tokens) | Why `math/rand` is dangerous for tokens | [`refresh.go:33`](../../internal/auth/refresh.go#L33) |
| [38](#38-why-the-database-holds-a-hash-and-why-sha-256-is-enough) | Why the database holds a hash, and why SHA-256 is enough | [`refresh.go:58`](../../internal/auth/refresh.go#L58) |
| [39](#39-what-rotation-buys) | What rotation buys | [`tokens.go:104`](../../internal/store/tokens.go#L104) |
| [40](#40-a-reused-token-ends-every-session) | A reused token ends every session | [`auth_handler.go:281`](../../internal/api/auth_handler.go#L281) |
| [41](#41-two-requests-one-token-the-row-lock) | Two requests, one token: the row lock | [`tokens.go:128`](../../internal/store/tokens.go#L128) |
| [42](#42-logout) | Logout | [`auth_handler.go:334`](../../internal/api/auth_handler.go#L334) |
| [43](#43-the-auth-middleware-and-v1me) | The auth middleware, and `/v1/me` | [`middleware.go:43`](../../internal/api/middleware.go#L43) |
| [44](#44-the-seed-command) | The seed command | [`cmd/seed/main.go:106`](../../cmd/seed/main.go#L106) |
| [45](#45-go-idioms-appearing-for-the-first-time-in-the-session-code) | Transactions, nullable columns, arrays and slices, goroutines | [`tokens.go:108`](../../internal/store/tokens.go#L108) |
| [46](#46-what-middleware-is-and-how-gin-chains-it) | What middleware is, and how Gin chains it | [`router.go:129`](../../internal/api/router.go#L129) |
| [47](#47-cnext-and-cabort) | `c.Next()` and `c.Abort()` | [`errors.go:173`](../../internal/api/errors.go#L173) |
| [48](#48-forgetting-abort-what-actually-goes-wrong) | Forgetting `Abort`: what actually goes wrong | [`middleware_test.go:132`](../../internal/api/middleware_test.go#L132) |
| [49](#49-a-typed-context-key-and-currentuser) | A typed context key, and `CurrentUser` | [`middleware.go:20`](../../internal/api/middleware.go#L20) |
| [50](#50-why-the-user-is-reloaded-instead-of-trusting-the-tokens-role) | Why the user is reloaded instead of trusting the token's role | [`middleware.go:69`](../../internal/api/middleware.go#L69) |
| [51](#51-requirerole) | `RequireRole` | [`middleware.go:99`](../../internal/api/middleware.go#L99) |
| [52](#52-testing-middleware-with-httptest-and-the-go-idioms-new-here) | Testing middleware with `httptest`; variadics, `slices`, atomics, `recover` | [`middleware_test.go:82`](../../internal/api/middleware_test.go#L82) |
| [53](#53-resource-level-policy-and-what-makes-a-function-pure) | Resource-level policy, and what makes a function pure | [`policy.go:5`](../../internal/auth/policy.go#L5) |
| [54](#54-why-a-plain-function-and-not-middleware) | Why a plain function and not middleware | [`policy.go:22`](../../internal/auth/policy.go#L22) |
| [55](#55-the-rules-and-the-decisions-behind-the-two-that-were-not-specified) | The rules, and the two that were not specified | [`policy.go:65`](../../internal/auth/policy.go#L65) |
| [56](#56-why-the-negative-cases-matter-more-than-the-positive-ones) | Why the negative cases matter more | [`policy_test.go:74`](../../internal/auth/policy_test.go#L74) |
| [57](#57-counting-the-combinations) | Counting the combinations | [`policy_test.go:154`](../../internal/auth/policy_test.go#L154) |
| [58](#58-filtering-in-sql-not-in-go) | Filtering in SQL, not in Go | [`applications.go:62`](../../internal/store/applications.go#L62) |
| [59](#59-two-encodings-of-one-rule-and-the-check-that-they-agree) | Two encodings of one rule, and the check that they agree | [`application_handler.go:378`](../../internal/api/application_handler.go#L378) |
| [60](#60-403-or-404) | 403 or 404 | [`decisions.md`](../decisions.md) |
| [61](#61-testcontainers-go-and-why-a-real-postgresql-beats-a-mock) | testcontainers-go, and why a real PostgreSQL beats a mock | [`authz_test.go:93`](../../test/integration/authz_test.go#L93) |
| [62](#62-testauthorizationmatrix) | `TestAuthorizationMatrix`; generics, spreading a slice, package build tags | [`authz_test.go:324`](../../test/integration/authz_test.go#L324) |

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

**📍 Open:** [`internal/config/config.go:397`](../../internal/config/config.go#L397)

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

**Two checks, in order** ([`config.go:200`](../../internal/config/config.go#L200)):

1. **Unset, empty or only whitespace** → the existing `MissingVarsError`.
2. **Set but under 32 bytes** → the new `WeakSecretError`.

Both make `Load` return an error, and `main` exits before the listener
opens. This is the "fail fast at startup" convention: a server that boots
with a guessable key works perfectly, right up until it is exploited.

**Why a new error type.** `InvalidVarError` prints the offending value,
which is right for `PORT=abc` and wrong for a secret. A rejected secret is
often a *real* one with a typo, and startup errors go to logs.
`WeakSecretError` ([line 176](../../internal/config/config.go#L176))
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

**📍 Open:** [`internal/api/errors.go:30`](../../internal/api/errors.go#L30)

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
call one function, [`Respond`](../../internal/api/errors.go#L118), and it is the
only code in the project that writes an error body.

**`details` is never absent and never `null`.**
[`errors.go:47`](../../internal/api/errors.go#L47) has no `omitempty`, and
[`errors.go:169`](../../internal/api/errors.go#L169) swaps a nil map for an
empty one, because Go encodes a nil map as `null`. A field that is sometimes an
object, sometimes missing and sometimes `null` is three shapes, and the client
would have to check before every read.

**Rejected: `omitempty` on `details`.** It is the more common choice and saves
thirteen bytes. It was turned down because "exactly one shape" is the whole
requirement, and the test's `decode` helper
([`errors_test.go:80`](../../internal/api/errors_test.go#L80)) fails if the
error object has anything but exactly three keys.

**Rejected: RFC 9457 `application/problem+json`.** A real standard with `type`,
`title`, `status`, `detail` and `instance`. It is a good choice for a public
API with many consumers; here it would add five fields nobody reads.

**One exception.** `/readyz` answers a failed check with
`{"status":"degraded","database":"unreachable"}`. That body is read by a load
balancer, not an API client, and was left alone.

---

## 19. Sentinel errors again, and why `errors.Is` exists

**📍 Open:** [`internal/domain/errors.go:18`](../../internal/domain/errors.go#L18)

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

**What breaks without it.** With [`errors.go:102`](../../internal/api/errors.go#L102)
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

**📍 Open:** [`internal/domain/errors_test.go:33`](../../internal/domain/errors_test.go#L33)

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
[`errors_test.go:53`](../../internal/domain/errors_test.go#L53) wraps the same
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
[`errors_test.go:139`](../../internal/api/errors_test.go#L139), and it also
checks the driver's text stays out of the response.

---

## 21. Why the status codes live in `api`, and not in `domain`

**📍 Open:** [`internal/api/errors.go:76`](../../internal/api/errors.go#L76)

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

**📍 Open:** [`internal/api/errors.go:120`](../../internal/api/errors.go#L120)

An error that matches no row of the table is one nobody planned for: the
database is down, a query has a typo, a pointer was nil. `Respond` does two
things with it, and they go to different audiences:

- **To the log**, at [`errors.go:125`](../../internal/api/errors.go#L125): the
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
[`errors.go:137`](../../internal/api/errors.go#L137) never sends `err.Error()` at
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

**📍 Open:** [`internal/domain/errors.go:80`](../../internal/domain/errors.go#L80)

A sentinel can say *that* validation failed, and nothing more — it is a single
shared value, so it cannot carry which fields. For that a **type** is needed:
`ValidationError` is a struct with a map from field name to problem.

It is still an `ErrValidation`. The `Unwrap` method at
[`errors.go:133`](../../internal/domain/errors.go#L133) returns the sentinel, so
`errors.Is(err, domain.ErrValidation)` is true and the table in section 21
needs no special row. Code that only cares about the category never has to
know the type exists.

`Respond` is the one caller that wants the fields, and it gets them with
`errors.As` at [`api/errors.go:146`](../../internal/api/errors.go#L146):

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

**📍 Open:** [`internal/api/router.go:61`](../../internal/api/router.go#L61)

`Respond` only runs when a handler calls it. Three failures happen where no
handler is involved, and Gin's defaults for all three break the one-shape rule:

| Situation | Gin's default | Now |
|---|---|---|
| No route matches the path | plain text `404 page not found` | 404 `route_not_found` |
| Path exists, method does not | also a plain-text 404 | 405 `method_not_allowed` |
| A handler panics | 500 with an **empty** body | 500 `internal_error` |

That is why the router is not built with `gin.Default()`. `gin.Default()` is
`gin.New()` plus a logger and a recovery middleware; the two are now listed by
hand so that [`api.Recovery`](../../internal/api/recovery.go#L20) can stand in
for the second. `gin.CustomRecovery` still does the important part — it catches
the panic, logs the value and a stack trace, and keeps the process alive — and
only the response body is ours.

`HandleMethodNotAllowed` at [`router.go:80`](../../internal/api/router.go#L80) is off in
Gin by default. Left off, `DELETE /healthz` is reported as "no such endpoint",
which is untrue and sends a client looking for a typo in the URL.

**`route_not_found` is a different code from `not_found`.** One means the URL
is wrong; the other means the URL is right and the record does not exist. A
client retries neither, but a developer debugging wants to know which.

---

## 25. More Go idioms appearing for the first time

**A nil pointer that is not a nil error.**
[`domain/errors.go:101`](../../internal/domain/errors.go#L101)

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
[`errors.go:90`](../../internal/domain/errors.go#L90). `sync.Mutex` and
`bytes.Buffer` in the standard library follow the same design.

**`errors.As` — the counterpart to `errors.Is`.**
[`api/errors.go:146`](../../internal/api/errors.go#L146)

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
[`main.go:127`](../../cmd/api/main.go#L127) builds the pool, the store, the token
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
[`validation.go:38`](../../internal/api/validation.go#L38) registers a function
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

**📍 Open:** [`internal/api/validation.go:134`](../../internal/api/validation.go#L134)

Every handler that takes a body does the same four steps, and each has its own
failure:

| Step | Where | Failure |
|---|---|---|
| Decode the JSON | [`decodeJSON`](../../internal/api/validation.go#L53) | 400 `malformed_request` |
| Clean the values | in the handler | — |
| Check the rules | [`validateStruct`](../../internal/api/validation.go#L134) | 422 `validation_failed`, with `details` |
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
[`validation.go:143`](../../internal/api/validation.go#L143) gets at it with
`errors.As` (section 28), and each entry becomes one line of the
`domain.ValidationError` from section 23:

```json
{"error":{"code":"validation_failed","message":"One or more fields are invalid.",
  "details":{"email":"must be a valid email address","full_name":"is required",
             "password":"must be at least 8 characters"}}}
```

That is real output from the running server. The wording is ours
([`problemFor`](../../internal/api/validation.go#L156)): the validator's own
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
([`validation.go:57`](../../internal/api/validation.go#L57)). Without a cap the
decoder reads whatever a client sends.

---

## 31. Why the response is its own struct

**📍 Open:** [`internal/api/auth_handler.go:92`](../../internal/api/auth_handler.go#L92)

`domain.User` ([`user.go:26`](../../internal/domain/user.go#L26)) has seven
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

**What breaks without it.** [`TestRegister`](../../internal/api/auth_handler_test.go#L211)
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
[`TestLoginFailuresAreIndistinguishable`](../../internal/api/auth_handler_test.go#L448)
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

**📍 Open:** [`internal/api/router.go:103`](../../internal/api/router.go#L103)

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

**A group is also where shared middleware goes.** `RequireAuth` is attached
to one, so every route in it is protected by one line (section 46).

**The routes moved out of `main`.** `NewRouter` takes a
[`Deps`](../../internal/api/router.go#L16) struct and returns a finished
engine, so a test can build the same router the server runs —
[`TestRoutes`](../../internal/api/router_test.go#L22) does, and checks that
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

**Method values.** [`router.go:110`](../../internal/api/router.go#L110)

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
[`TestLoginIssuesARefreshTokenAndStoresOnlyItsHash`](../../internal/api/session_test.go#L207)
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
[`TestRefreshFailuresAreIndistinguishable`](../../internal/api/session_test.go#L349)
compares six kinds of failure byte for byte. A thief should not learn that
detection fired.

**The server does say so in its own log**, at
[`auth_handler.go:297`](../../internal/api/auth_handler.go#L297): user ID,
client address and how many sessions were ended, and never a token or hash.
This is the one expected-path event that is logged, because it is the one
somebody should look at.

**If the mass revocation itself fails**, the request is answered with a 500 and
still issues nothing; the presented token is already revoked.
[`TestRefreshReplayWhenRevocationFails`](../../internal/api/session_test.go#L428).

**What breaks without it.**
[`TestRefreshReplayRevokesEverySession`](../../internal/api/session_test.go#L292)
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

**📍 Open:** [`internal/api/auth_handler.go:334`](../../internal/api/auth_handler.go#L334)

Logout revokes the presented refresh token and returns **204 No Content**.

**Only that token.** Logging out on a phone leaves the laptop signed in.

**It does not require an access token.** The refresh token in the body is the
credential. A user whose access token expired five minutes ago must still be
able to log out, and requiring a valid access token would force a refresh
first — creating a session in order to end one.

**It always answers 204**, for a live token, an already-revoked one, one that
never existed, and garbage
([`TestLogoutIsIdempotentAndRevealsNothing`](../../internal/api/session_test.go#L478)).
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

**📍 Open:** [`internal/api/middleware.go:43`](../../internal/api/middleware.go#L43)

*Rewritten as `RequireAuth` later in this module; sections 46–52 cover the
current version. What follows still holds.*

**Middleware** is a function that runs before a handler and can stop the
request. `RequireAuth` reads `Authorization: Bearer <token>`, verifies the
token with the `TokenManager` from section 11, and either answers 401 or
leaves the authenticated user on the request for the handler.

It is attached to a **group**
([`router.go:129`](../../internal/api/router.go#L129)), not to a route. Every
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

**The database is read on every guarded request**
([`middleware.go:69`](../../internal/api/middleware.go#L69)), not only by
`/v1/me`. The token says who the caller was when it was issued. Reading the
row makes the answer current and means a user deleted since then gets a 401,
not a profile
([`TestMeForADeletedUser`](../../internal/api/session_test.go#L601)).
Section 50 has the full argument.

**How the user travels** to the handler — a typed key and `CurrentUser` — is
section 49. Roles are section 51.

---

## 44. The seed command

**📍 Open:** [`cmd/seed/main.go:106`](../../cmd/seed/main.go#L106)

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
([`main.go:133`](../../cmd/seed/main.go#L133)). Same reasoning as registration
in section 32.

**An existing account is never changed.** If you alter the admin's password
and run the seed again, your password stays.

**It refuses to run in production**
([`main.go:72`](../../cmd/seed/main.go#L72)). These passwords are in a public
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
[`middleware.go:142`](../../internal/api/middleware.go#L142)

`value.(*domain.User)` converts an `any` back to a concrete type. The
one-result form panics if the type is wrong; the two-result form,
`user, ok := …`, reports it. `errcheck` in this repository rejects the
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

## 46. What middleware is, and how Gin chains it

**📍 Open:** [`internal/api/router.go:129`](../../internal/api/router.go#L129)

A Gin route is not one function. It is a **list** of `gin.HandlerFunc`, and
each one has the same signature, `func(c *gin.Context)`. "Middleware" is
just the name for the entries in front of the last one: they get to look at
the request first, and they may stop it.

The list is built from three places, outermost first:

| Registered with | Applies to | Here |
|---|---|---|
| `router.Use(…)` | every route on the engine | `gin.Logger()`, `Recovery()` ([`router.go:61`](../../internal/api/router.go#L61)) |
| `v1.Group("", …)` | every route registered on that group | `RequireAuth` ([`router.go:129`](../../internal/api/router.go#L129)) |
| the route itself | that route only | `deps.Auth.Me` |

So a request for `GET /v1/me` runs this list:

```
[0] gin.Logger   [1] Recovery   [2] RequireAuth   [3] Me
```

Gin keeps an **index** into the list on the `gin.Context`. It starts at the
first entry and, each time a function returns, moves to the next. That is the
whole mechanism: there is no special "middleware" type, and nothing in Go
itself is involved. A handler and a middleware differ only in where they sit.

**Why a group and not each route.** Attaching `RequireAuth` to the group means
every route registered inside it is guarded. Protecting a new endpoint is
adding a line inside the braces, and forgetting to protect one is impossible
unless it is registered outside them.

**Why `RequireAuth` returns a function.** `RequireAuth(tokens, users)` is
called **once**, when the router is built, and returns the
`gin.HandlerFunc` that runs on every request. The returned function closes over
`tokens` and `users` (a *closure*, first seen in section 35), so each request
gets its dependencies without a global variable.

---

## 47. `c.Next()` and `c.Abort()`

**📍 Open:** [`internal/api/middleware.go:87`](../../internal/api/middleware.go#L87) and [`internal/api/errors.go:173`](../../internal/api/errors.go#L173)

Both move Gin's index; neither stops the Go function that calls it.

**`c.Next()`** runs the *rest* of the list immediately, and returns to the
caller when it has finished. Code after `c.Next()` therefore runs **after**
the handler. This is how `gin.Logger` measures latency:

```go
start := time.Now()
c.Next()                         // the handler runs inside this call
log(time.Since(start), c.Writer.Status())
```

A middleware that only needs to act *before* the handler does not call it.
Returning normally is enough, because Gin's loop moves to the next entry
anyway. `RequireAuth` and `RequireRole` both work that way, which is why
neither contains `c.Next()`.

**`c.Abort()`** sets the index past the end of the list (to `abortIndex`, 63),
so when the current function returns Gin's loop finds nothing left to run.
It does **not** write a response, and it does **not** return from the current
function. `c.AbortWithStatusJSON(status, body)` is `Abort` plus writing the
body.

In this codebase every error goes through `Respond`, which ends in
[`write`](../../internal/api/errors.go#L165), which calls
`c.AbortWithStatusJSON`. So rejecting a request is always two lines:

```go
rejectBearer(c, domain.ErrUnauthenticated) // writes the 401 and aborts the chain
return                                     // leaves this function
```

Each line does a job the other cannot:

| | Stops later handlers | Stops the rest of *this* function |
|---|---|---|
| `c.Abort()` | yes | **no** |
| `return` | **no** | yes |

Forget `return` and the middleware carries on: here, it would go on to parse
a token that is not there. Forget the abort and the next section happens.

---

## 48. Forgetting `Abort`: what actually goes wrong

**📍 Open:** [`internal/api/middleware_test.go:132`](../../internal/api/middleware_test.go#L132)

This is the bug the two-line pattern prevents. A middleware that writes an
error with `c.JSON` and returns, without aborting:

```go
func buggyAuth(c *gin.Context) {
	if c.GetHeader("Authorization") == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return // leaves buggyAuth; Gin then runs the next handler
	}
}
```

In front of a handler that deletes something, and called with no token at
all, it produced this (run once as a throwaway test, then deleted):

**Output — not a command:**

```
[GIN-debug] [WARNING] Headers were already written. Wanted to override status code 401 with 200
status=401
body={"error":"unauthenticated"}{"deleted":42}
handler side effects=1
```

Three separate failures in one request:

1. **The handler ran.** The delete happened, for a caller with no credentials.
   This is the one that matters: authentication that only *reports* failure
   is not authentication.
2. **The status lies.** The 401 was written first, and HTTP sends the status
   line before the body, so the handler's 200 could not replace it. Gin's
   warning is the only sign. Access logs, metrics and the client all record a
   refused request — for an action that took place.
3. **The body is two JSON documents back to back**, which is not valid JSON.
   A client parsing it fails, so the mistake looks like a client bug.

**Why it is common.** `return` *looks* final, and `c.JSON` *looks* like
answering the request. Neither is: a middleware returning normally is how it
says "carry on".

**How this codebase guards against it.**

- There is one place an error is written, and it always aborts
  ([`errors.go:173`](../../internal/api/errors.go#L173)). No middleware calls
  `c.JSON` for a failure.
- Every middleware test counts how many requests reached the handler and
  checks the handler's output is absent from the body.

**The guard was tested by breaking it.** With `AbortWithStatusJSON` in
`write` changed to `c.JSON`, the new tests failed exactly as above:

**Output — not a command:**

```
middleware_test.go:122: handler ran 1 time(s); it must never run without a valid credential
middleware_test.go:272: body is not the envelope: invalid character 'h' after top-level value
    {"error":{"code":"forbidden","message":"You do not have permission to do that.","details":{}}}handler ran
```

The change was reverted with `git checkout` and the tests pass again.

---

## 49. A typed context key, and `CurrentUser`

**📍 Open:** [`internal/api/middleware.go:20`](../../internal/api/middleware.go#L20) and [`internal/api/middleware.go:134`](../../internal/api/middleware.go#L134)

`c.Set(key, value)` stores a value for the rest of this request, and
`c.Get(key)` reads it back. In Gin 1.11 the store is a `map[any]any`, so the
key can be any comparable value.

**What a map key really is.** When the key is an interface value, Go compares
**both the dynamic type and the value**. So these are two different keys,
even though they print the same:

```go
type contextKey string
const currentUserKey contextKey = "currentUser"

c.Set(currentUserKey, user)   // key: (api.contextKey, "currentUser")
c.Set("currentUser", other)   // key: (string, "currentUser")
```

`contextKey` is a **defined type**: a new type whose *underlying* type is
`string`. It has the same representation as a string but is not
interchangeable with one, so a `string` never equals a `contextKey`.

**Why that matters.**

- **No collisions.** Any package in the process — including a third-party
  middleware — can call `c.Set("currentUser", …)` or `c.Set("user", …)`. With
  a plain string key, the last writer wins, silently. `contextKey` is
  unexported, so no other package can even construct a value of it.
- **No forgery.** For the same reason, nothing outside `api` can plant a
  user for `CurrentUser` to return. A test proves it: a user stored under
  the plain string `"currentUser"` is not found
  ([`middleware_test.go:363`](../../internal/api/middleware_test.go#L363)).
- **Typos stop compiling.** A misspelt *constant* name is a compile error. A
  misspelt string literal is a `Get` that quietly returns nothing.

The previous version used `const claimsKey = "auth.claims"`, a plain string.
It worked, but only because nothing else happened to use that text.

The same rule applies to `context.Context`: `context.WithValue` with a string
key is flagged by `staticcheck` (SA1029) for exactly this reason.

**Rejected alternative: `type contextKey struct{}`.** Also collision-proof,
and takes no memory. A string-based type was chosen because the key stays
readable when `c.Keys` is printed while debugging.

**Why a helper at all.** `c.Get` returns `any`, so every caller would need a
type assertion and a decision about what to do when it fails.
`CurrentUser` does that once:

- the two-value assertion `value.(*domain.User)`
  ([`middleware.go:142`](../../internal/api/middleware.go#L142)) reports a
  wrong type through `ok` instead of panicking;
- it returns `(domain.User, bool)`, so a handler on an unguarded route gets
  `false` and must handle it, rather than a nil pointer to dereference;
- it returns a **copy** ([`middleware.go:146`](../../internal/api/middleware.go#L146)).
  `*user` dereferences the pointer, and returning a struct by value copies
  every field. A handler that changes its copy cannot change what
  `RequireRole`, or anything later, sees.

`Me` is now three lines: read `CurrentUser`, respond
([`auth_handler.go:357`](../../internal/api/auth_handler.go#L357)).

---

## 50. Why the user is reloaded instead of trusting the token's role

**📍 Open:** [`internal/api/middleware.go:69`](../../internal/api/middleware.go#L69)

The access token carries a `role` claim, and its signature is valid. It is
still not used for authorisation.

**What the signature proves, and what it does not.** It proves this server
issued the token, and that nobody changed it since (section 13). It says
nothing about whether it is *still true*. A token is a **snapshot** taken at
login, and it stays valid for fifteen minutes whatever happens in between.

Within those fifteen minutes, an admin might:

| Change in the database | Trusting the claim | Reloading the row |
|---|---|---|
| demote an interviewer to candidate | still an interviewer until `exp` | candidate on the next request |
| delete an account | still authenticated until `exp` | 401 on the next request |
| promote a candidate | must log in again to see it | effective on the next request |

All three were run against the Compose stack with one access token held
throughout:

**Output — not a command:**

```
me as interviewer                        → role "interviewer", 200
UPDATE users SET role='candidate' …       → role "candidate",   200  (same token)
DELETE FROM users …                       → unauthenticated,    401  (same token)
```

**What it costs.** One primary-key lookup per authenticated request. `users.id`
is the primary key, so it is an index read of one row, on a pool that is
already open. That is cheap compared with the cost of acting on a stale role.

**Alternatives rejected.**

- **Trust the claim, shorten the token.** Narrows the window but never
  closes it, and makes clients refresh constantly.
- **A denylist of revoked tokens.** Needs a lookup on every request anyway,
  plus a place to keep the list.
- **A `token_version` column, copied into the token.** Bump it on any change
  and reject tokens with an old number. Also a lookup per request, plus a
  migration — and it only refuses; it does not give the handler the current
  user.

**The role is still in the token.** Nothing reads it for decisions. It is
harmless, but it is also unnecessary; see *Still open*.

**A failed lookup is a 500, not a 401.** The request asked for 401 "on any
failure". A database that cannot be reached says nothing about the
credential, which may be perfectly valid. A 401 tells a well-behaved client
its tokens are bad, so it discards them and sends the user back to the login
form — during an outage, when login will fail too. A 500 says "try again".
Only a lookup that *succeeds* and finds no user is a 401
([`middleware.go:70`](../../internal/api/middleware.go#L70)).

---

## 51. `RequireRole`

**📍 Open:** [`internal/api/middleware.go:99`](../../internal/api/middleware.go#L99)

`RequireRole(roles ...string)` lets a request through if `CurrentUser` holds
one of the roles, and answers 403 otherwise. It reads the user `RequireAuth`
stored, so **order matters**:

```go
staff := protected.Group("", RequireRole(domain.RoleAdmin, domain.RoleInterviewer))
```

`protected` already runs `RequireAuth`, so the chain is
`… RequireAuth → RequireRole → handler`.

**401 versus 403.** 401 means "I do not know who you are" and carries a
`WWW-Authenticate` challenge: logging in might help. 403 means "I know who you
are, and the answer is no": logging in again would not help, so no challenge
is sent ([`middleware_test.go:226`](../../internal/api/middleware_test.go#L226)).

**Registered in the wrong order** — `RequireRole` without `RequireAuth` in
front — `CurrentUser` finds nothing. That is a mistake in this server, not the
client's, so it is a 500, and the log says which middleware is missing
([`middleware.go:115`](../../internal/api/middleware.go#L115)).

**It panics on a mistake in the route table.**
[`middleware.go:101`](../../internal/api/middleware.go#L101)

`RequireRole()` with no roles, or with a misspelling like `"admn"`, would
refuse every request — silently, and only noticed when someone tries the
endpoint. These are programming errors fixed by editing code, not runtime
conditions, so `RequireRole` panics. It runs while `NewRouter` builds the
routes, so the server fails at startup and the first test that builds the
router fails too. The standard library does the same in
`regexp.MustCompile`. Returning an error was rejected: every route
registration would need error handling for a condition that is always a bug.

**`slices.Clone`** ([`middleware.go:111`](../../internal/api/middleware.go#L111))
copies the roles. Called as `RequireRole(roles...)`, the function receives
the caller's own slice, not a copy, and a later `roles[0] = …` would change
which roles the route accepts. A test does exactly that
([`middleware_test.go:329`](../../internal/api/middleware_test.go#L329)).

**`slices.Contains`** ([`middleware.go:122`](../../internal/api/middleware.go#L122))
is a linear scan. With at most three roles, it is faster than building a map.

**`domain.IsRole`** ([`user.go:14`](../../internal/domain/user.go#L14)) lists
the valid roles in one place, beside the constants. It lives in `domain`
because the set of roles is a rule of the hiring pipeline, not of HTTP.

**Not wired to a route yet.** No endpoint needs a role today; job posts will
be the first. The tests use their own small engine.

---

## 52. Testing middleware with `httptest`, and the Go idioms new here

**📍 Open:** [`internal/api/middleware_test.go:82`](../../internal/api/middleware_test.go#L82)

**The test the brief asked for** —
`TestRequireAuthNeverRunsTheHandlerWithoutAValidCredential` — builds an
engine with `RequireAuth` in front of a handler that counts its calls, sends
six bad requests through `httptest`, and checks for each:

- the counter is still **0**;
- the status is 401, and the body is the envelope with the right `code`;
- the handler's output is **absent** from the body (section 48's double body);
- a `WWW-Authenticate: Bearer` challenge is present.

The six cases: no header, `Basic` scheme, `Bearer ` with nothing after it,
garbage, an expired token, and a valid token for a user who does not exist.

`httptest.NewRecorder` is an `http.ResponseWriter` that keeps what was written,
and `engine.ServeHTTP` runs the whole chain in the test's goroutine. No port
is opened.

**`gin.CreateTestContext`**
([`middleware_test.go:351`](../../internal/api/middleware_test.go#L351)) builds a
bare `*gin.Context` for testing `CurrentUser` directly, without a route.

**Go idioms appearing for the first time**

- **Variadic parameters.** `roles ...string` accepts any number of strings,
  and inside the function `roles` is a `[]string`. A caller with a slice
  already passes it with `RequireRole(roles...)` — which hands over the
  slice itself, hence `slices.Clone` above.
- **The `slices` package.** Generic helpers over any slice type, in the
  standard library since Go 1.21: `Clone`, `Contains`, `Sort`, `Index`.
- **Defined types**, `type contextKey string` (section 49).
- **`atomic.Int32` and `atomic.Pointer[T]`**
  ([`middleware_test.go:24`](../../internal/api/middleware_test.go#L24)). Counters
  safe to update from several goroutines. `ServeHTTP` runs synchronously, so a
  plain `int` would pass today; the atomic type keeps the helper safe if a
  test ever sends requests concurrently. `Pointer[T]` is a **generic** type:
  `T` is filled in at the use site, here `domain.User`.
- **`recover()` in a test**
  ([`middleware_test.go:320`](../../internal/api/middleware_test.go#L320)). A
  deferred function that calls `recover` stops a panic and returns its value,
  or `nil` if there was no panic. That is how the test asserts that
  `RequireRole` panics.

---

## 53. Resource-level policy, and what makes a function pure

**📍 Open:** [`internal/auth/policy.go:5`](../../internal/auth/policy.go#L5)

`RequireRole` answers "what *kind* of user is this?". Most questions in a
hiring pipeline are about **one user and one record**: may *this* candidate
see *this* application? `policy.go` answers those, as three functions with
one shape:

```go
func CanViewApplication(u domain.User, app domain.Application, assigned bool) bool
func CanReviewApplication(u domain.User, app domain.Application, assigned bool) bool
func CanChangeApplicationStatus(u domain.User, app domain.Application, assigned bool) bool
```

**A function is pure when both of these hold:**

1. **Same arguments, same answer, every time.** Nothing else feeds in: no
   clock, no database, no environment variable, no global, no random number,
   no request.
2. **It changes nothing.** No writes, no logging, no modifying its arguments.

`policy.go` imports exactly one package, `domain`, for the types. That import
list is the proof: there is nothing in scope that could reach the outside
world. Note also that the arguments are passed **by value** — a `domain.User`,
not a `*domain.User` — so the function receives copies and could not change the
caller's structs even by mistake.

**Why it matters for authorization in particular.**

- **The whole rule is visible in one place.** A reviewer, an auditor, or you in
  six months, reads twenty lines and knows exactly who can do what. There is no
  hidden input that changes the answer on a Tuesday.
- **It can be tested exhaustively.** With no database to set up, every
  combination runs in microseconds: 36 cases × 3 functions, plus the
  invariants, in well under a millisecond. Impure code gets tested on the few
  cases someone bothered to set up.
- **A failure is a wrong rule, never a flaky dependency.** If a test fails,
  the policy is wrong. It cannot be a timeout, a missing row or a stale cache.
- **The decision is separate from gathering the facts.** Loading the user,
  the application and the assignment is the caller's job, and can fail
  (database down). The decision itself cannot fail, so it returns a plain
  `bool`, not `(bool, error)`.

**Why `assigned` is a parameter.** Whether an interviewer is assigned lives
in `interviewer_assignments`. Looking it up inside the policy would make the
policy impure. Instead the caller looks it up and passes the answer in — a
technique sometimes called *functional core, imperative shell*: the shell does
I/O, the core decides.

---

## 54. Why a plain function and not middleware

**📍 Open:** [`internal/api/middleware.go:99`](../../internal/api/middleware.go#L99) for contrast

Middleware runs **before** the handler, and at that point it has:

- the request: method, path, headers, the `:id` in the URL;
- the user `RequireAuth` loaded.

What it does **not** have is the **resource**. `GET /v1/applications/:id`
gives middleware a string ID. Whose application that is, and whether an
interviewer is assigned to it, are in the database, and loading them is what
the handler is about to do anyway.

So middleware can enforce "you must be an admin to reach this route" — a rule
about the user alone. It cannot enforce "you must own this application"
without either:

- **loading the application itself**, and then the handler loads it again, or
  reaches into the context for it: two components now share hidden state, and
  every route needs its own loader; or
- **guessing from the URL**, which is how authorization bugs happen.

The policy is called by the handler, after it has loaded what it needs:

```go
app, err := h.apps.GetApplication(ctx, id)
assigned, err := h.assignments.IsAssigned(ctx, app.ID, user.ID)
if !auth.CanViewApplication(user, *app, assigned) { … }
```

Being a plain function also means it is **reusable outside HTTP**: a list
endpoint can filter rows with it, a background job can check it, an admin CLI
can call it. Middleware is tied to Gin.

The two layers work together, not instead of each other. `RequireRole` is a
cheap coarse filter at the door; the policy is the precise check once the
record is in hand.

---

## 55. The rules, and the decisions behind the two that were not specified

**📍 Open:** [`internal/auth/policy.go:65`](../../internal/auth/policy.go#L65)

`CanViewApplication` follows the brief exactly. The brief named the other two
and gave them the same shape, but not their rules. These were chosen:

| | admin | interviewer | candidate | any other role |
|---|---|---|---|---|
| **View** | always | if assigned | if they own it | never |
| **Review** | **never** | if assigned **and** not their own | never | never |
| **Change status** | always | never | never | never |

- **Admins do not review.** An admin decides the outcome; the marks they read
  to decide come from someone else. If an admin could mark and decide, the
  review step would be a formality.
- **No self-review.** An interviewer can also be a candidate — `candidate_id`
  references any user. One assigned to their own application may view it,
  per the brief's rule, but may not mark it.
- **Interviewers do not change status.** They recommend through a review.
- **Candidates never act**, even on their own application. "Change status" on
  your own application means awarding yourself an offer.

If any of these is not what you intend, each is a one-line change to the
function and a few cells of the table.

**Three defences that run through all three functions:**

- **Deny by default.** Each `switch` lists the roles that may act; everything
  else lands in `default: return false`
  ([`policy.go:30`](../../internal/auth/policy.go#L30)). A new role added to the
  database gets nothing until someone writes a rule for it. The opposite
  shape — `if role == candidate { return false }` — would hand every unknown
  role everything.
- **Exact role comparison.** `"Admin"` is not `"admin"`. The database stores
  lowercase and its `CHECK` enforces it; a case-insensitive match would only
  widen the door.
- **Empty IDs never match** ([`policy.go:158`](../../internal/auth/policy.go#L158)).
  A zero-value `User` and a zero-value `Application` both have ID `""`, and
  `"" == ""` is true. Without the guard, a handler that forgot to load the user
  would grant a candidate ownership of an application that failed to load.

**`CanChangeApplicationStatus` ignores two of its arguments**
([`policy.go:136`](../../internal/auth/policy.go#L136)). The blank identifier `_`
as a parameter name says so, and stops the linter asking why they are unused.
They stay so all three functions are called the same way, and so a later rule
— "not once an offer has been accepted" — needs no change at any call site.

---

## 56. Why the negative cases matter more than the positive ones

**📍 Open:** [`internal/auth/policy_test.go:74`](../../internal/auth/policy_test.go#L74)

**The two kinds of failure are not symmetric.**

- A policy that wrongly says **no** is noticed within the hour: a real user
  hits a 403 and reports it.
- A policy that wrongly says **yes** is noticed by nobody. The user it lets in
  is not going to file a bug. It surfaces in a breach report, months later.

So the tests that matter most are the ones that assert `false`, because
nothing else in the system will catch those mistakes. In this table,
**89 of the 108 expectations are `false`**.

**Measured, not argued.** Each rule in `policy.go` was broken in turn — ten
plausible mistakes — and the suite run against each. Then the same ten were
run against a throwaway copy of the test that checked **only** the `true`
expectations (19 of 108), as a happy-path suite would:

| Mistake introduced | Full table | Happy paths only |
|---|---|---|
| Drop the empty-ID guard in `owns` | caught | caught |
| `&&` becomes `\|\|` in `owns` | caught | caught |
| Candidate may view any application | caught | **survived** |
| Interviewer may view without assignment | caught | **survived** |
| Unknown roles may view (fail open) | caught | **survived** |
| Interviewer may review their own application | caught | **survived** |
| Review ignores assignment | caught | **survived** |
| Admin may review | caught | **survived** |
| Status change: deny candidates instead of allow admins | caught | **survived** |
| Case-insensitive role comparison | caught | **survived** |

The happy-path suite caught **2 of 10**, and those two only because `owns` is
also used in a *negative* position (`!owns` in review), so widening it took a
grant away. Every mistake that *adds* access survived. That is the shape of
real authorization bugs: they add access.

One mistake — self-review — was caught by **exactly one** assertion: the row
`interviewer / owner / assigned`. Remove that row and nothing would notice.
That is why the table is complete rather than "representative".

---

## 57. Counting the combinations

**📍 Open:** [`internal/auth/policy_test.go:154`](../../internal/auth/policy_test.go#L154)

**What the code can tell apart.** The rules branch on three inputs:

| Input | Classes the code distinguishes | Values tested |
|---|---|---|
| role | admin, interviewer, candidate, *anything else* | 6: the three, plus `""`, `"Admin"`, `"superuser"` for *anything else* |
| ownership | owns it; someone else's; both IDs empty (the guard) | 3 |
| assigned | true, false | 2 |

- **Minimum to touch every class:** 4 × 3 × 2 = **24** combinations per
  function, **72** expectations in all.
- **What the table has:** 6 × 3 × 2 = **36** rows, each checked against all
  three functions = **108** expectations (19 true, 89 false).

**Does the test cover all of them? Yes — and a test proves it, rather than a
count by eye.** `TestPolicyTableIsComplete` builds the full cross product of
the three value lists and fails if any combination is missing from
`policyTable`, appears twice, or if the row count differs. Adding a role to
`testRoles` without writing its six rows fails the build.

**What "all" does not include**, honestly:

- **Every possible string.** `role` is a `string`, so there are infinitely
  many values. They are covered by *equivalence class*: every value that is
  not one of the three roles takes the same `default` branch, and three
  different kinds of wrong are tested. Full coverage of a string is not
  achievable; full coverage of the branches is.
- **Fields the rules ignore.** `Status`, `JobPostID` and the rest do not appear
  in any rule. `TestPolicyInvariants` re-runs all 36 combinations under each of
  the five statuses and fails if any answer changes, so the day a rule starts
  depending on status, the test says the table needs a status dimension.
- **Whether the caller computes `assigned` correctly.** A pure function trusts
  its inputs. If a handler passes `true` for the wrong user, the policy cannot
  know. That is tested where the lookup lives, once it exists.

**Invariants checked across every combination**
([`policy_test.go:204`](../../internal/auth/policy_test.go#L204)):

- nobody may review or change what they may not view;
- nobody may both review and change status.

---

## 58. Filtering in SQL, not in Go

**📍 Open:** [`internal/store/applications.go:62`](../../internal/store/applications.go#L62)

`ListApplications` runs a **different query for each role**. Since module 5.4 it is one query whose scope condition is chosen by role; see [05-4](05-4-application-pipeline.md#5-filters-without-string-concatenating-values).

| Role | Query | Index that serves it |
|---|---|---|
| admin | every row | — |
| candidate | `WHERE a.candidate_id = $1` ([`:77`](../../internal/store/applications.go#L77)) | `applications_candidate_id_idx` |
| interviewer | `WHERE EXISTS (… ia.interviewer_id = $1)` ([`:84`](../../internal/store/applications.go#L84)); a `JOIN` until module 5.4 | `interviewer_assignments_interviewer_id_idx` |
| anything else | no query: `ErrForbidden` ([`:89`](../../internal/store/applications.go#L89)) | — |

The rejected design is the one that looks simpler:

```go
all, _ := store.AllApplications(ctx)        // SELECT * FROM applications
for _, a := range all {
	if auth.CanViewApplication(user, a, isAssigned(a)) {
		visible = append(visible, a)
	}
}
```

**The performance half.** Every request reads, transfers and decodes every
application in the system, to return a handful. With 50,000 applications a
candidate who has three waits for 50,000 rows. Worse for interviewers:
`isAssigned` is one query **per row** — the *N+1 query* problem. The SQL
version reads only the matching rows, through an index.

**The security half — the specific bug it invites.** The fetch-everything
version is only safe if **every** code path that touches `all` remembers to
filter. The data is unfiltered by default, and the filter is something added
afterwards. That fails in predictable ways:

1. **Pagination applied before the filter.** Someone adds `LIMIT 20` to the
   query for speed. The database returns the first 20 applications *in the
   system*, Go filters them, and a candidate gets 0 rows on page 1 and their
   own on page 37. Then someone adds a `total` field from `COUNT(*)` — which
   tells every candidate how many applications the whole company has received.
2. **The second endpoint forgets.** A CSV export, a count, a search, a
   "recent activity" widget: each reuses `AllApplications`, and the one whose
   author forgot the loop ships everyone's CVs.
3. **The unfiltered slice escapes.** It gets logged when debugging, put in an
   error message, or cached under a key without the user in it.

Each of these is a bug where the **default is "everything"** and safety
depends on remembering. With the filter in SQL the default flips: the rows a
caller may not see **never leave the database**, so no later mistake in Go can
leak them. There is nothing to forget, because there is nothing to filter.

**It denies by default too.** A role with no `case` gets no query at all. The
tempting alternative — admin query as the fallback, with `WHERE` clauses added
for the roles that need restricting — would hand an unknown role every row.
That exact mistake was introduced as a test (section 62) and caught.

**`pgx.CollectRows`** ([`:145`](../../internal/store/applications.go#L133)) reads
every row through a function, closes the result set, and returns `rows.Err()`.
That last one matters: an error part-way through a result (a dropped
connection after row 400) is reported only by `rows.Err()`, and a hand-written
`for rows.Next()` loop that forgets it returns a silently truncated list.

---

## 59. Two encodings of one rule, and the check that they agree

**📍 Open:** [`internal/api/application_handler.go:378`](../../internal/api/application_handler.go#L378)

The visibility rule now exists twice:

- **for many rows**, as the three SQL queries above;
- **for one row**, as `auth.CanViewApplication` (section 53), called by `Get`.

That duplication is the price of filtering in SQL: a pure Go function cannot
become a `WHERE` clause. Two encodings can **drift** — someone adds "admins of
a post see its applications" to the policy and forgets the query. The symptom
is a list showing rows that then 404 when opened, or the reverse.

`TestAuthorizationMatrix` checks the two against each other for every caller
([`authz_test.go:620`](../../test/integration/authz_test.go#L620)): an
application is in the list **if and only if** fetching it alone returns 200.

**`Get` needs two facts, fetched in one query.**
[`applications.go:327`](../../internal/store/applications.go#L327) returns the
row and, via `EXISTS (…)`, whether the caller is assigned to it. Then the
handler calls the pure policy. The store gathers facts; the policy decides.

---

## 60. 403 or 404

**📍 Open:** [`docs/decisions.md`](../decisions.md) — decision D1

**A refused view answers 404, identical to a missing record**
([`application_handler.go:382`](../../internal/api/application_handler.go#L382)).
The full record — when 403 is still right, what was rejected, what it costs —
is in `docs/decisions.md`. The core argument:

| | 403 Forbidden | 404 Not Found |
|---|---|---|
| Tells the caller | "this exists, and it is not yours" | "nothing here for you" |
| Lets any account holder discover which IDs are real | **yes** | no |
| Helps a legitimate developer debug | yes | less |

**404 leaks less**, and it only works if it is applied *everywhere* and
*exactly*: if refusals and misses differed in any way — status, body, a header,
or the time taken — the difference would be the leak. So:

- the body comes from the same `Respond(c, domain.ErrNotFound)` call in both
  paths, with no wrapped message (the client never sees one anyway, section 22);
- a malformed ID is also a 404 with the same body;
- the store fetches row and assignment in **one** query, so a refusal does not
  take a measurably different time from a miss.

**403 is still right** when the caller already knows the thing exists: a route
limited by `RequireRole` (routes are public), or an action on a record they can
view but not change.

---

## 61. testcontainers-go, and why a real PostgreSQL beats a mock

**📍 Open:** [`test/integration/authz_test.go:93`](../../test/integration/authz_test.go#L93)

**How it works.** testcontainers-go is a Go library that talks to the Docker
daemon's API — the same API the `docker` command uses. `postgres.Run`:

1. pulls `postgres:16-alpine` if it is not present, and creates a container
   from it, publishing port 5432 on a **random free port** of the host, so
   parallel test runs never collide;
2. copies the six `*.up.sql` migrations into `/docker-entrypoint-initdb.d`
   ([`:107`](../../test/integration/authz_test.go#L111)), which the official
   image runs with `psql`, in order, the first time it initialises — the same
   files, applied the same way, as production;
3. **waits** ([`:111`](../../test/integration/authz_test.go#L115)) until the
   server has logged "ready to accept connections" twice (the image restarts
   once after running init scripts) and the port accepts connections. Without
   a wait strategy, the test would race the database's startup;
4. returns a handle whose `ConnectionString` gives the DSN with the random
   port filled in.

**Cleanup has two layers.** `testcontainers.CleanupContainer`
([`:116`](../../test/integration/authz_test.go#L120)) removes the container when
the test ends. If the test process is killed before that can run, a small
companion container called **Ryuk** — started automatically by the library —
notices the test process's connection drop and removes everything it started.
No container outlives the run.

**Why not a mock.** A mock `ApplicationStore` returns whatever the test tells
it to. But in this change, **the SQL is the authorization**. The things that
could be wrong are things only PostgreSQL can answer:

- does `WHERE a.candidate_id = $1` really restrict to one candidate?
- does the inner `JOIN` drop unassigned applications, and can it produce
  duplicates? (`LEFT JOIN` would leak; the test caught that.)
- does `'not-a-uuid'::uuid` raise 22P02, and is that mapped to 404?
- does `EXISTS` see the assignment?
- do the migrations even apply on a clean database?

A mock of the store would have to re-implement the filtering in Go to be
useful — which is precisely the design section 58 rejects, now living in the
test, where it can be right while the SQL is wrong. The handler unit tests in
`application_handler_test.go` *do* use a fake store, but only to test the
handler's own logic: what it passes the store, and how it maps errors.

**Why a container and not the Compose database.** The store tests in
`internal/store` use the running stack, so they must tolerate whatever else is
in it (the admin check there says "contains", not "equals"). The matrix needs
to know *exactly* which rows exist, to assert an exact list. A container of its
own starts empty, every time.

**The costs.** Docker must be running, and the test takes about 7 seconds the
first time (seconds after, with the image cached). It sits behind the
`integration` build tag, like the store tests, so `make test` stays fast and
needs nothing.

---

## 62. `TestAuthorizationMatrix`

**📍 Open:** [`test/integration/authz_test.go:324`](../../test/integration/authz_test.go#L324)

**The shape.** One row per caller, one column per endpoint, the expected
status in each cell — readable as a matrix:

| caller | `/v1/me` | list | get A | get B | get missing | get malformed |
|---|---|---|---|---|---|---|
| anonymous | 401 | 401 | 401 | 401 | 401 | 401 |
| token for a deleted user | 401 | 401 | 401 | 401 | 401 | 401 |
| admin | 200 | 200 · A, B | 200 | 200 | 404 | 404 |
| interviewer assigned to A | 200 | 200 · A | 200 | 404 | 404 | 404 |
| interviewer assigned to nothing | 200 | 200 · none | 404 | 404 | 404 | 404 |
| candidate who owns A | 200 | 200 · A | 200 | 404 | 404 | 404 |
| candidate who owns B | 200 | 200 · B | 404 | 200 | 404 | 404 |
| candidate with a forged admin claim | 200 | 200 · A | 200 | 404 | 404 | 404 |

8 callers × 6 endpoints = **48 cells**, plus **6** list-agreement checks (one
per caller who can list) = 54 subtests.

**What each cell asserts, beyond the status:**

- a **401** carries the `unauthenticated` code;
- a **404** is **byte-for-byte** the body this same caller gets for an ID that
  does not exist — decision D1, tested on every refusal;
- a **list** contains **exactly** the expected IDs, no more, no fewer, none
  twice. Asserting the status alone would be worthless here: a list that leaks
  every application still returns 200;
- a **single fetch** returns the application asked for.

**The matrix cannot fall behind the router.**
[`assertEveryProtectedRouteIsInTheMatrix`](../../test/integration/authz_test.go#L627)
reads the router's registered routes and fails if any `/v1` route outside
`/v1/auth` has no column. Adding an endpoint without deciding who may call it
turns the test red.

**Tested by breaking it.** Each mistake below was introduced, the test run,
and the change reverted:

| Mistake | Caught by |
|---|---|
| Candidate query loses its `WHERE` | matrix: 3 cells, lists too long |
| Interviewer `JOIN` becomes `LEFT JOIN` | matrix: 2 cells, lists too long |
| Refusal answers 403 instead of 404 | matrix: 6 cells |
| Policy check removed from `Get` | matrix: 11 cells |
| Refusal answers a different error (409) | matrix: 6 cells |
| A new protected route not added to the matrix | the route-completeness check |
| Unknown role falls through to the admin query | **not by the matrix** — see below |

The last one survived the matrix, and that is correct: `users_role_check` stops
an unknown role from ever being stored, so no HTTP request can carry one.
The branch is still worth defending, so
[`TestListApplicationsRefusesUnknownRoles`](../../internal/store/applications_integration_test.go#L155)
calls the store directly with `""`, `"Admin"` and `"superuser"`; with the
mistake reintroduced it fails, returning both applications to each.

**Also run in CI.** A new `authorization-matrix` job in
`.github/workflows/ci.yml` runs it on every push. GitHub's Ubuntu runners
provide Docker, which is all testcontainers needs.

**Go idioms appearing for the first time**

- **A generic type.** `listResponse[T any]`
  ([`application_handler.go:134`](../../internal/api/application_handler.go#L134))
  is one struct for every list the API will return. `T any` is a *type
  parameter*; `listResponse[applicationResponse]` fills it in.
- **Spreading a slice into a variadic call.** `s.db.Query(ctx, query, args...)`
  passes a `[]any` as the separate arguments `Query` expects — empty for the
  admin query.
- **A build tag on a whole package.** Every file in `test/integration` starts
  `//go:build integration`, so without the tag the package does not exist as
  far as `go test ./...` is concerned.

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
| **The Compose stack** (Docker Desktop, image rebuilt from this commit) | |
| `make up` | image builds with both binaries; migrations exit 0; api healthy; `/readyz` 200 |
| Inside the api container | `hiredesk-api` and `hiredesk-seed` in `/usr/local/bin`, running as UID 10001 |
| `make seed`, twice | 4 created, 0 already present / 0 created, 4 already present |
| Through the published port: login, `/v1/me`, refresh, replay, logout | 200 / 200 / 200 / 401 / 204 |
| `make test-integration` with the database published on 5432 | **fails on this machine** — `role "hiredesk" does not exist`: a Postgres installed on the host owns `localhost:5432` and answers instead (see *Still open*) |
| `make up POSTGRES_PORT=5433`, then `make test-integration POSTGRES_PORT=5433` | pass |
| Rows the tests left in the Compose database | none — the 4 seeded users, 0 live tokens |
| Image size, summed from `docker history` / compressed | **42.6 MB** / 15.5 MB — up from 22.4 MB / 9.2 MB at module 0: the api binary is now 22.7 MB and the seeder adds 9.4 MB |
| **Middleware and roles** (sections 46–52) | |
| `go test -race -count=1 ./...` | all pass; `api` **89.6%**, `domain` **93.8%**; `RequireAuth`, `RequireRole`, `CurrentUser` each **100%** |
| Six bad credentials through `RequireAuth` | handler reached **0** times each; 401 envelope; Bearer challenge; handler output absent |
| Token says admin, row says candidate | `CurrentUser` is a candidate; an admin-only route answers 403 |
| Role changed in the database between two requests with one token | 200, then 403 |
| Database failing during the lookup | 500, cause logged and absent from the body; handler not reached |
| `RequireRole`: 6 role and route combinations | allowed → 200 and handler reached once; refused → 403 `forbidden`, no challenge, handler not reached |
| `RequireRole` without `RequireAuth` | 500, log names the missing middleware |
| `RequireRole()`, `RequireRole("admn")`, `RequireRole(admin, "superuser")` | all panic |
| `CurrentUser`: unguarded route / same text under a plain string key / wrong type / caller edits its copy | false / false / false / stored user unchanged |
| `write` changed to `c.JSON` (no abort), temporarily | the new tests **fail**: handler ran, body is two JSON documents |
| Forgotten-abort demo (throwaway test, deleted) | status 401, body `{"error":…}{"deleted":42}`, handler side effect ran once, Gin warns *Headers were already written* |
| **Compose stack**, rebuilt with the middleware | |
| `/v1/me` as interviewer, then `UPDATE users SET role='candidate'`, same token | role `interviewer` 200, then role `candidate` 200 |
| `DELETE` the user, same token | 401 `unauthenticated` |
| No token | 401 with `WWW-Authenticate: Bearer realm="hiredesk"` |
| Postgres stopped, valid token | 500 `internal_error`; log: `require auth: load user: get user by id: …` |
| `golangci-lint`, with and without the integration tag | **0 issues** |
| **Authorization policy** (sections 53–57) | |
| `TestPolicy`: 36 combinations × 3 functions | 108 expectations, 19 true and 89 false, all pass |
| `TestPolicyTableIsComplete` | 36 rows = 6 roles × 3 ownerships × 2 assignments; none missing or repeated |
| `TestPolicyInvariants` under 5 statuses | no review or status change without view; never both review and change; no answer depends on status |
| Zero-value user and application | nothing granted |
| Coverage of `policy.go` | **100%** of all four functions; `auth` package **95.1%** |
| Ten deliberate mistakes in `policy.go`, one at a time | all **10 caught**; restored with `cp`, identical to the original |
| The same ten against a positive-only copy of the test (throwaway, deleted) | **2 of 10** caught |
| **Applying the policy** (sections 58–62) | |
| `TestAuthorizationMatrix`, real PostgreSQL 16 via testcontainers-go v0.44.0 | 54 subtests pass: 48 cells + 6 list-agreement checks; ~7 s cold, ~3 s warm |
| Six mistakes in the store, handler and router (section 62) | all caught by the matrix |
| Unknown role falling through to the admin query | survives the matrix (unreachable over HTTP); caught by `TestListApplicationsRefusesUnknownRoles` |
| Store integration tests on the Compose database | list per role exact; admin list contains both; unknown roles `ErrForbidden`; `GetApplication` assigned flag; missing, malformed and empty IDs `ErrNotFound` |
| Handler unit tests (fake store) | 9 role/record cases; refusal and absence identical in status, body and headers; list passes the database's user, not the token's; empty list is `{"data":[]}`; store failure is a logged 500 |
| `make test-integration` | `internal/store` and `test/integration` both pass |
| Compose stack, two applications inserted by hand | candidate1 lists `[a1]`; admin `[a1, a2]`; unassigned interviewer `[]`, then `[a2]` once assigned; candidate1 → own 200, other's 404, missing 404; rows removed afterwards |
| Every `#L` link in these notes | resolves to an existing line; every target in a file this change touched was read against its section |

## Still open

- **`make test-integration` can reach the wrong database.** It connects to
  `localhost:$POSTGRES_PORT`. On a machine that already runs Postgres on
  5432, the host's server binds `127.0.0.1` and wins over Docker's wildcard
  bind, so the tests fail with `role "hiredesk" does not exist`. Setting
  `POSTGRES_PORT=5433` in `.env` avoids it. `make up` and `make seed` are
  unaffected, because they never leave the Compose network.
- **The seeder ships in the runtime image**, which costs 9.4 MB. A separate
  build target or Compose profile would keep it out of a production image.
- **Access tokens outlive refresh-token revocation** by up to fifteen
  minutes (section 40). Deleting the user or changing their role takes effect
  at once, because `RequireAuth` reads the row (section 50); logging out does
  not end an access token already issued.
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
- **`CanReviewApplication` and `CanChangeApplicationStatus` are not called
  yet.** No review or status endpoint exists. `CanViewApplication` is applied
  in `GET /v1/applications/:id`.
- **The application list is unbounded.** No pagination or limit yet. When one
  is added, the `LIMIT` must go in each per-role query, after its `WHERE` —
  never on an unfiltered query (section 58).
- **The visibility rule is written twice**, as SQL and as
  `CanViewApplication`. The matrix checks they agree for the seeded data; a
  divergence that only shows with other data would not be caught.
- **A refusal is not logged as one.** The access log shows a 404 and the path,
  the same as a miss. Support cannot tell "not yours" from "not there"
  without a deliberate log line, which would need to stay out of the response.
- ~~`TestAuthorizationMatrix` covers read endpoints only.~~ Module 4 added
  the job post routes, two of which write.
- **`assigned` is trusted.** The policy cannot check that the caller looked up
  the assignment for the right user and application.
- ~~`RequireRole` guards no route yet.~~ It guards creating and updating job
  posts as of module 4.
- **The token still carries a `role` claim** that nothing reads for
  decisions. Removing it would make the token smaller and remove a value that
  looks authoritative and is not.
- **One database read per authenticated request.** Cheap now; a short cache
  would trade a little staleness for fewer reads if it ever matters.
- **Registering does not log the user in**; it returns no tokens.
- **Nothing revokes sessions on a password change**, because there is no
  password-change endpoint yet.
- **Token reuse is only logged.** Nobody is alerted and the user is not told.
- **CI runs only part of the integration tests.** The authorization matrix
  runs in its own container on every push; the store tests in
  `internal/store` still need the Compose stack and run only with
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
- **`role` is an unchecked string in `NewAccessToken`.** It rejects empty but
  not `"superuser"`. It no longer matters for access, since roles are read
  from the database, where `users_role_check` guards them.
- **No secret rotation.** Changing `JWT_SECRET` invalidates every token at
  once. Rotating without that needs a key ID (`kid`) in the header.
- **`internal/config` is at 59.8%.** Only the `JWT_SECRET` paths are tested.

## Next

Job posts, with `RequireRole` limiting who may create and edit them; then
submitting an application, and the review and status endpoints that
`CanReviewApplication` and `CanChangeApplicationStatus` were written for.
