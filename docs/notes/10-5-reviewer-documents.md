# Module 10, part 5: The documents a reviewer reads, and the clean-clone test

`README.md` and `docs/decisions.md` rewritten or extended for
someone opening the repository cold, then tested the way that person would:
a fresh clone, the documented commands only.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | The README's order, and why | [`README.md:1`](../../README.md#L1) |
| 2 | Only measured numbers | [`README.md:318`](../../README.md#L318) |
| 3 | The decision records | [`docs/decisions.md:1`](../decisions.md#L1) |
| 4 | The clean-clone test | — |

---

## 1. The README's order, and why

**📍 Open:** [`README.md:1`](../../README.md#L1)

A reviewer gives a repository about thirty seconds before deciding whether
to read on. So the order is: one sentence on what it does, then the single
most distinctive output (a real score breakdown, every number traced to an
answer) **above the fold**, then how to run it, then why it is built the way
it is. Reference tables (endpoints, errors, commands, configuration) come
last. They must exist and be accurate, but nobody reads them first.

The badge stays on the first line: a red badge is the first thing a reviewer
should be able to see.

The score example is not hand-written. It is the response captured in the
browser walkthrough of part 1.

## 2. Only measured numbers

**📍 Open:** [`README.md:318`](../../README.md#L318)

Every figure in the README was produced in this module and can be reproduced:

| Figure | Source |
|---|---|
| Unit coverage per package | `go test -race -count=1 -cover ./...` |
| Store 83.1%, all of `internal/` 69.9% | integration run with `-coverpkg=./internal/...`, merged per package |
| 334 matrix subtests | `go test -tags integration -run TestAuthorizationMatrix -v` and a count of passing subtests |
| Ranking: 53.9 ms median | `make seed-bulk`, then 20 timed requests after 3 warm-up ones; the median of the server's own `duration_ms` |
| 74 MB image, 13 migrations, 12 tables | `docker images`, `ls migrations/*.up.sql`, `information_schema.tables` |

What was **not** measured is not claimed. The N+1 fix (step 2 of the ranking)
was never built, and the earlier note's "30–40 ms / a few milliseconds" were
expectations, so the README gives the measured "before" and says plainly that
there is no "after". The previous README's "500 applications take about 50 ms"
for rescore-all was not re-measured, and was left out rather than repeated.

## 3. The decision records

**📍 Open:** [`docs/decisions.md:1`](../decisions.md#L1)

D1 (403 versus 404) already existed and was kept. D5–D9 were added: Gin,
hand-written SQL, PostgreSQL search over Elasticsearch, a generated column
over a trigger, and stored scores. Each states the decision, why, what was
rejected, and what it costs. The cost is the part a reviewer trusts: a
decision record without a downside reads as advocacy.

## 4. The clean-clone test

Procedure, as specified: clone to a fresh directory, `cp .env.example .env`,
`make up`, `curl` healthz, `make test`. It was run against the module's
commit, cloned into an empty directory, under its own Compose project name
so it started from an empty database. The developer's own stack and data were
stopped first and restored afterwards.

**Pass 1**, following the procedure literally:

| Step | Result | Undocumented? |
|---|---|---|
| `git clone` | the commit, all files present | no |
| `cp .env.example .env` | — | — |
| `make up` | build, migrate and start all report success, and the links are printed | — |
| `curl localhost:8080/healthz` | **connection refused**. `hiredesk-api Restarting`; log: `JWT_SECRET is too short: 12 characters, need at least 32` | **yes**: the README never said not to copy the template, and `make up` gives no sign of the crash loop |
| `rm .env && make up` | `.env` generated with a random secret; API healthy | **yes**: the recovery was not written down |
| `curl /healthz`, `/readyz`, `/docs` | `200`, `200`, `200`; 13 migrations applied to the empty database | no |
| `make seed` | `4 created` | no |
| `make test` | all packages pass | partly: needs Go 1.27.3, which the quick start did not mention |
| — | `internal/config` coverage **81.7%** under `make test` against 72.5% under plain `go test` | **yes**: the Makefile exports `.env` into the test process, so the README's "numbers from `make test`" was wrong |

**Fixed in the README:** a warning against copying `.env.example`, the
one-line recovery, a `curl` check with what to do if it fails, Go 1.27.3 in
the quick start, the race detector's C-compiler need on Linux, and coverage
numbers attributed to the command that produced them.

**Not fixed here:** two Makefile changes would remove the traps at
the source instead of documenting them: `make up` waiting for the API to be
healthy before printing links, and `make test` not exporting `.env`. Both
are recorded as follow-ups.

**Pass 2**, following the corrected README word for word, from a new clone
of the commit that fixed it:

| Step | Result |
|---|---|
| `git clone … && cd hiredesk && make up` | `created .env from .env.example, with a freshly generated JWT_SECRET`; stack up |
| `curl -s localhost:8080/healthz` | `{"status":"ok"}` |
| `make seed` | `4 created, 0 already present` |
| `/docs`, then `POST /v1/auth/login` as the admin | `200`, `200` |
| `make test` | 8 packages `ok`, no failures |

No step was needed that the README does not state.
