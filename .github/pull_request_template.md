## What and why

<!-- One or two sentences: what this changes and the reason for it. -->

## How it was verified

<!-- Commands run and what they showed. -->

## Checklist

- [ ] `make fmt && make lint && make test && go build ./...` pass locally
- [ ] `make test-integration` passes if SQL, a migration or a route changed
- [ ] New configuration is in `internal/config`, `.env.example` and the README
- [ ] No secret, credential or local path in the diff
- [ ] README tables and the relevant `docs/notes/` entry are up to date
