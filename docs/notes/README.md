# Design notes

One note per module: the reasoning behind each decision, the alternatives
rejected, and the Go and infrastructure concepts involved.

Every section names the exact file and line to open, so a note can be read
alongside the code it describes.

| Module | Note | Covers |
|---|---|---|
| 0 | [00-foundation.md](00-foundation.md) | Go modules, `internal/`, config loading, Gin, Docker, Compose, Makefile, CI |
| 1 | [01-schema.md](01-schema.md) | `timestamptz`, `CHECK` vs enums, uuid keys, token hashing, delete policy, `numeric` money, `text[]`, `jsonb`, unique constraints and the duplicate-application race |
| 2 | [02-database.md](02-database.md) | Connection pools, pgx pool tuning, the startup ping, `/healthz` vs `/readyz`, `context.WithTimeout`, graceful shutdown, migrations on startup, Adminer |

## Why these are separate from the code

The code carries the comments a working project needs: doc comments on exported
identifiers, and short notes where intent is not obvious from the line itself.
Longer explanation — why an alternative was rejected, what breaks without a
given decision, how a Go idiom works — lives here instead.

Keeping them apart means the code stays readable at the density a reviewer
expects, while none of the reasoning is lost.
