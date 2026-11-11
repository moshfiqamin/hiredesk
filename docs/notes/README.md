# Design notes

One note per module, or for a large module one note per part: the reasoning
behind each decision, the alternatives rejected, and the Go and
infrastructure concepts involved.

Every section names the exact file and line to open, so a note can be read
alongside the code it describes.

| Module | Note | Covers |
|---|---|---|
| 0 | [00-foundation.md](00-foundation.md) | Go modules, `internal/`, config loading, Gin, Docker, Compose, Makefile, CI |
| 1 | [01-schema.md](01-schema.md) | `timestamptz`, `CHECK` vs enums, uuid keys, token hashing, delete policy, `numeric` money, `text[]`, `jsonb`, unique constraints and the duplicate-application race |
| 2 | [02-database.md](02-database.md) | Connection pools, pgx pool tuning, the startup ping, `/healthz` vs `/readyz`, `context.WithTimeout`, graceful shutdown, migrations on startup, Adminer |
| 3 | [03-auth.md](03-auth.md) | Hash functions, bcrypt cost and salt, reading a `$2a$12$` hash, timing-safe verification, sentinel errors, table-driven tests, the three parts of a JWT, what a signature proves, algorithm confusion, token lifetime, `JWT_SECRET` validation, struct embedding, the error envelope, `errors.Is` and `%w`, mapping domain errors to HTTP statuses, why raw errors must not reach a client, hand-written SQL with pgx, PostgreSQL error codes and `errors.As`, struct tags, request validation, response DTOs, user enumeration, `/v1` routing, `crypto/rand` versus `math/rand`, hashing server-generated tokens, refresh-token rotation and replay detection, transactions and `FOR UPDATE`, auth middleware, the seed command, how Gin chains middleware, `c.Next()` and `c.Abort()`, the forgotten-abort bug, typed context keys, reloading the user instead of trusting the token's role, `RequireRole` and 401 versus 403, variadic parameters, the `slices` package, pure functions, resource-level authorization policy, deny by default, why negative tests matter more, exhaustive table-driven tests and counting combinations, filtering in SQL rather than Go, 403 versus 404, testcontainers-go, the authorization matrix |
| 4.1 | [04-1-job-posts.md](04-1-job-posts.md) | Absent versus empty in a JSON body, zero values, pointer fields, why `null` and absent collapse, partial updates, validating a patch against the stored row, read-merge-check-write under `FOR UPDATE`, validating a UUID before querying, money as integer minor units, `json.Number`, hiding drafts in SQL, the first `RequireRole` routes, struct comparison with `==`, consumer-side interfaces, pointer-to-pointer scanning |
| 4.2 | [04-2-job-post-lifecycle.md](04-2-job-post-lifecycle.md) | State machines, a transition map versus `if` chains, deny by default, one route per transition, checking and writing under one lock, `FOR KEY SHARE` on foreign-key inserts, 409 versus 400 versus 422, readiness to open and what a missing rubric breaks, a sibling audit table, writing the event in the same transaction, `errors.As` with an interface target, `map[string]any`, nil versus empty slices in JSON, `count(*) FILTER` |
| 4.3 | [04-3-questions.md](04-3-questions.md) | Locking the form when a post opens and why that is a domain decision about fairness, row locks that make a status check hold, question validation, why an empty list counts as absent, the unique-constraint collision in a reorder, row-by-row versus end-of-statement versus commit-time checking, `DEFERRABLE` constraints, why the reorder needs a transaction, `unnest … WITH ORDINALITY`, `IS DISTINCT FROM`, `*[]string`, command tags, `break` in Go |
| 4.4 | [04-4-public-listing.md](04-4-public-listing.md) | A public, unauthenticated route, what `OFFSET` makes PostgreSQL physically do, keyset pagination and its index, row-value comparison, the skip-and-repeat bug and why keyset avoids it, an immutable sort key with a tie-breaker, opaque versioned cursors, fetching one extra row instead of counting, capping `limit`, computing derived values in SQL with `LATERAL`, partial indexes |
| 5.1 | [05-1-application-validation.md](05-1-application-validation.md) | The six submission rules and their order, collecting every error versus failing fast, why rule 1 stops everything and why a draft is a 404, why the server validates when the frontend already does, pure domain functions tested without Gin, a structured 422 with `min_length` and `got`, custom `UnmarshalJSON`, type switches, embedding a struct in a JSON response, why `url.Parse` alone does not mean absolute |
| 5.2 | [05-2-submit-transaction.md](05-2-submit-transaction.md) | What a transaction is in PostgreSQL (one connection, `xid`, MVCC visibility, `COMMIT` and `ROLLBACK`), `defer tx.Rollback` and why it is safe after `Commit`, what autocommit would leave behind, `FOR SHARE` versus `FOR UPDATE`, the event row inside the transaction and the outbox pattern, multi-row insert with `unnest`, a built-up `VALUES` list, `pgx.CopyFrom` and when it wins, forcing a failure with a trigger |
| 5.3 | [05-3-duplicate-applications.md](05-3-duplicate-applications.md) | The check-then-insert race walked through step by step, time-of-check to time-of-use, why `FOR UPDATE` cannot lock a row that does not exist yet, why a unique constraint is the only reliable guard, SQLSTATE 23505 with `errors.As` and the constraint name, why a 500 would be a bug, goroutines, the Go 1.22 loop variable, `sync.WaitGroup` and a start gate, what `-race` detects and what it cannot |
| 5.4 | [05-4-application-pipeline.md](05-4-application-pipeline.md) | The application lifecycle map, 409 versus 422 for a status, why events store both `from` and `to`, locking the post to serialise offers, the READ COMMITTED snapshot bug in "lock and count in one statement", proving a concurrency test can fail, building `WHERE` from fixed fragments with parameters, why `IS NULL OR` hurts the planner, filters with keyset pagination, why `count(*)` is O(matches) under MVCC and five alternatives, a `REPEATABLE READ` snapshot for count and page, loading history only after authorization |

## Why these are separate from the code

The code carries the comments a working project needs: doc comments on exported
identifiers, and short notes where intent is not obvious from the line itself.
Longer explanation — why an alternative was rejected, what breaks without a
given decision, how a Go idiom works — lives here instead.

Keeping them apart means the code stays readable at the density a reviewer
expects, while none of the reasoning is lost.
