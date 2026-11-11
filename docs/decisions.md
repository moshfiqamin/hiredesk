# Decisions

Short records of choices that shape the API's behaviour, written so the
reasoning survives after the conversation that produced it. Each states the
decision, why, what was rejected, and what it costs.

The longer, teaching-oriented explanations live in [`notes/`](notes/).

---

## D1. A resource the caller may not see is a 404, not a 403

**Date:** 2026-11-11
**Status:** accepted
**Applies to:** every endpoint that addresses one record by ID — today
`GET /v1/applications/:id`.

### Decision

When an authenticated caller asks for a specific record that they are not
allowed to view, the API answers exactly as if the record did not exist:

- status **404**, code `not_found`;
- a body **byte-for-byte identical** to the one for an ID that really does not
  exist;
- the same work on the server: one query in both cases, so the response time
  does not tell them apart either.

A malformed ID (not a UUID) is also a 404 with the same body.

### When 403 is still right

403 `forbidden` is used where the caller **already knows the thing exists**,
so refusing reveals nothing new:

| Situation | Status | Why |
|---|---|---|
| The caller may not view this record | **404** | a 403 would confirm the ID is real |
| The caller may view it but not perform this action on it — e.g. an interviewer, assigned and able to read an application, asking to change its status | **403** | they can already see it; hiding it would be pointless and confusing |
| The route itself is limited to some roles (`RequireRole`) | **403** | routes are public — they are in the README — so the endpoint's existence is no secret |
| A list endpoint | **200** with only the visible rows | a list never refuses; it simply contains less |

The test for which to use: *would a 403 tell the caller something they do not
already know?* If yes, 404.

### Why

**404 leaks less.** A 403 means "this exists, and it is not yours". A 404
means "there is nothing here for you". With 403, anyone with an account can
probe IDs and learn which are real.

That matters more here than the IDs being UUIDs might suggest:

- **IDs leak.** They appear in URLs, browser history, logs, screenshots and
  shared links. A candidate who obtains another application's ID should not be
  able to confirm it is live.
- **Existence is itself information.** In a hiring system, "an application
  exists for this post from this person" can be sensitive — for example, a
  current employee looking elsewhere.
- **Consistency is what makes it work.** If *some* refusals were 403, the
  difference between a 403 and a 404 would be the leak.

This is the convention GitHub uses for private repositories, and RFC 9110
§15.5.4 explicitly allows it: a server may answer 404 instead of 403 when it
does not wish to reveal that a forbidden resource exists.

### Rejected: 403 everywhere

The more literal reading of HTTP: 403 is the status for "authenticated, not
permitted". It is easier to debug, because a client developer sees why they
were refused. Rejected because it turns every protected endpoint into an
oracle for which IDs exist, and the debugging benefit goes to the people we
would least like to help.

### Rejected: 403 for records the caller *could* guess, 404 otherwise

Too subtle to apply consistently, and every inconsistency is a leak.

### Costs

- **Harder to debug.** A legitimate user who is refused sees "not found" and
  may report a missing record. Today the access log records the same thing for
  both — a 404 and the path — so support cannot tell them apart either. A
  server-side log line for refusals would fix that, and must never reach the
  response.
- **Must be applied everywhere.** One new endpoint that answers 403 for an
  unseen record undoes the protection for that resource type.
  `TestAuthorizationMatrix` asserts the refused and absent responses are
  identical, and must gain rows for every new endpoint.
- **Timing must match too.** A refusal that does more work than a miss is
  measurable. `GetApplication` fetches the row and the assignment flag in one
  query for this reason.

### Enforced by

- [`internal/api/application_handler.go`](../internal/api/application_handler.go)
  — a failed `CanViewApplication` responds with `domain.ErrNotFound`.
- [`test/integration/authz_test.go`](../test/integration/authz_test.go)
  — `TestAuthorizationMatrix` checks every role against every endpoint, and
  that each refusal's body equals the body for an ID that does not exist.
- [`internal/api/application_handler_test.go`](../internal/api/application_handler_test.go)
  — the same comparison without a database.
