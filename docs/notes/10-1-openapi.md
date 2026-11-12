# Module 10, part 1: OpenAPI, Swagger UI and `go:embed`

The whole API described in one OpenAPI 3 document, rendered as Swagger UI at
`GET /docs`, with both compiled into the binary.

## Contents

| # | Section | Open |
|---|---|---|
| 1 | What OpenAPI is for, beyond documentation | [`api/openapi.yaml:1`](../../api/openapi.yaml#L1) |
| 2 | One error envelope, referenced everywhere | [`api/openapi.yaml:1177`](../../api/openapi.yaml#L1177) |
| 3 | Bearer auth per route, and why Authorize works | [`api/openapi.yaml:1026`](../../api/openapi.yaml#L1026) |
| 4 | `go:embed` | [`api/embed.go:12`](../../api/embed.go#L12) |
| 5 | Serving the docs, and a CSP | [`internal/api/docs.go:17`](../../internal/api/docs.go#L17) |
| 6 | Keeping the spec honest | [`internal/api/docs_test.go:77`](../../internal/api/docs_test.go#L77) |
| 7 | Verification | — |

---

## 1. What OpenAPI is for, beyond documentation

**📍 Open:** [`api/openapi.yaml:1`](../../api/openapi.yaml#L1)

OpenAPI is a machine-readable contract: every path, method, parameter, body,
response and status code, in a format tools can read. The rendered page is
the least of it. The same file drives:

- **Client generation.** `openapi-generator` or `oapi-codegen` turns the spec
  into a typed client in TypeScript, Go, Python or Kotlin. A frontend calls
  `api.getScore(id)` and gets a `ScoreBreakdown` type, and a renamed field
  breaks the frontend's *build*, not its users.
- **Server stubs.** The same generators can emit Go interfaces for the
  handlers. HireDesk does the reverse (code first, spec beside it) and closes
  the gap with tests (section 6).
- **Contract testing.** Tools such as Schemathesis read the spec and fire
  generated requests at the running server, checking each response against
  the declared schema. They find a 500 where the spec promises a 422, a
  `null` where it says `required`, or a status code nobody documented.
- **Mocking.** Prism or Stoplight serve fake responses straight from the
  examples, so a frontend team can build against `/v1/applications/{id}/score`
  before the backend exists, and swap to the real server without code changes.
- **Gateways and validation.** API gateways (Kong, AWS API Gateway) import
  OpenAPI to configure routes, auth and request validation at the edge.

The recurring theme: *one* source of truth, which several tools consume
instead of several hand-maintained copies drifting apart.

**Why OpenAPI 3.0.3 and not 3.1.** 3.1 aligns with JSON Schema and is the
future, but generator and validator support for it is still uneven. 3.0.3 is
what every tool reads. The cost is `nullable: true` instead of
`type: [string, "null"]`.

**Why written by hand rather than generated from code** (swaggo comments, for
example): annotations on every handler would put a second, unchecked copy of
each route's contract into the code, at a comment density the project rules
forbid. The rejected alternative's one advantage, that it cannot drift, is
bought back by the tests in section 6.

## 2. One error envelope, referenced everywhere

**📍 Open:** [`api/openapi.yaml:1177`](../../api/openapi.yaml#L1177), then the
shared responses from [`api/openapi.yaml:1109`](../../api/openapi.yaml#L1109)

`ErrorEnvelope` is defined once under `components/schemas`. Nine shared
responses (`BadRequest`, `Unauthorized`, `NotFound`, `Conflict`,
`PayloadTooLarge`, `TooManyRequests` and the rest) point at it with `$ref`,
and every operation points at those. About 200 response entries reach one
definition.

Why that matters rather than writing the schema out twenty times:

- **It is the code's own shape.** The Go side has one `errorBody` type,
  written by one function, `write`. The spec mirrors that: one definition,
  so it can only disagree with the code in one place.
- **Change once, correct everywhere.** This module added `request_id` to
  every error. With `$ref`, that was one edit. With twenty copies it is
  twenty edits, and the copy someone forgets becomes a lie that a generated
  client believes.
- **Generators produce one type.** A client generator turns each inline copy
  into its own class: `GetScore404Error`, `Login401Error`. With `$ref` it
  makes one `ErrorEnvelope`, so a frontend writes one error handler.
- **Readers learn it once.** A reviewer sees "every error is this shape",
  which is the design decision, not a coincidence repeated per route.

`TestErrorEnvelopeIsDefinedOnce` fails if a second definition appears.

## 3. Bearer auth per route, and why Authorize works

**📍 Open:** [`api/openapi.yaml:1026`](../../api/openapi.yaml#L1026),
[`api/swagger-ui/init.js:10`](../../api/swagger-ui/init.js#L10)

```yaml
securitySchemes:
  bearerAuth: { type: http, scheme: bearer, bearerFormat: JWT }
```

`type: http, scheme: bearer` tells Swagger UI to show one text box under
**Authorize** and to send `Authorization: Bearer <value>` on every operation
that lists `security: [{ bearerAuth: [] }]`. It is applied **per operation**,
not globally, because seven routes are public. A global requirement with
`security: []` overrides would also work, but then a forgotten override makes
a public route look protected. Per route, the padlock icon in the UI is the
access table.

Three details make the button work end to end:

1. **`servers: [{ url: / }]`**
   ([`:36`](../../api/openapi.yaml#L36)). A relative server, so "Try it out"
   calls the host that served the page. Hard-coding `http://localhost:8080`
   would break on any other port or host.
2. **Same origin.** `/docs` and `/v1` are served by the same process, so
   Swagger UI's calls are not cross-origin and need no CORS entry.
3. **`persistAuthorization: true`.** Without it a page reload forgets the
   token. With it, the token sits in that browser's `localStorage`, which is
   acceptable for a development tool and is why the access token is
   short-lived.

## 4. `go:embed`

**📍 Open:** [`api/embed.go:12`](../../api/embed.go#L12)

```go
//go:embed openapi.yaml
var Spec []byte

//go:embed swagger-ui
var SwaggerUI embed.FS
```

**What it does.** `//go:embed` is a compiler directive, not a comment: there
is no space after `//`, and it must sit directly above a package-level
variable. At build time the compiler reads the named files and stores their
bytes *inside the executable*. A `[]byte` or `string` variable gets one file;
an `embed.FS` gets a read-only file tree that implements `fs.FS`, so the
standard library's `http.FS`, `fs.Sub` and `fs.ReadFile` all accept it.

**Why it lives in its own package at the repository root.** A directive can
only reach files in its package's directory or below; `..` is refused. The
task asked for `api/openapi.yaml`, so the embedding package is `api/`. It is
imported as `spec "github.com/moshfiqamin/hiredesk/api"` because
`internal/api` is also called `api`, and an **import alias** gives a package a
local name in one file.

**Why embedding beats serving from disk, for a containerised app:**

- **One artefact.** The image's runtime stage copies only the binary
  ([`Dockerfile:66`](../../Dockerfile#L66) runs it). Serving from disk means
  remembering to `COPY` the files too. The day someone forgets, `/docs`
  404s in production and works on every laptop.
- **No working-directory assumptions.** `os.Open("api/openapi.yaml")` is
  relative to wherever the process was started. Under Docker,
  systemd or a test runner that differs, and the bug only shows up there.
- **Docs and code ship together.** The spec in a binary is the spec that
  binary was built with. A volume-mounted spec can describe a different
  version of the API than the one answering.
- **Read-only by construction.** Nothing at runtime can modify embedded
  files, so a compromised process cannot rewrite the docs page to phish a
  token.

The cost is binary size (1.6 MB of embedded files, almost all of it Swagger UI's bundle), and a rebuild
to change a typo in the docs. Both are cheap.

**Vendored, not loaded from a CDN.** The Swagger UI bundle and stylesheet are
copied from `swagger-ui-dist` 5.17.14, checked against the npm registry's
SHA-1 for that release, with its Apache 2.0 `LICENSE` and `NOTICE` beside
them. A CDN would make the docs depend on a third party at runtime, and let
that third party run script on a page that holds a bearer token.

## 5. Serving the docs, and a CSP

**📍 Open:** [`internal/api/docs.go:17`](../../internal/api/docs.go#L17)

- `fs.Sub` ([`:25`](../../internal/api/docs.go#L25)) strips the
  `swagger-ui/` prefix, so `/docs/assets/x.js` maps to `swagger-ui/x.js`.
- `StaticFS` with `http.FS` ([`:51`](../../internal/api/docs.go#L51)) gives
  correct `Content-Type`s, `Range` and conditional requests for free.
- The page links its assets root-relative (`/docs/assets/...`), so `/docs`
  and `/docs/` load the same files. Gin redirects `/docs/` to `/docs`.

The `Content-Security-Policy` allows scripts and styles from this server
only, and `connect-src 'self'` limits "Try it out" to this API. That is why
the initialiser is a separate `init.js` rather than an inline `<script>`: an
inline script would need `'unsafe-inline'`, which is the hole CSP exists to
close. `validatorUrl: null` stops Swagger UI from sending the spec to
`validator.swagger.io`, which the CSP would block anyway.

## 6. Keeping the spec honest

**📍 Open:** [`internal/api/docs_test.go:77`](../../internal/api/docs_test.go#L77)

A hand-written spec drifts. Four tests stop that:

| Test | Fails when |
|---|---|
| `TestOpenAPICoversEveryRoute` | a route is served but not documented, or documented but not served (Gin's `:id` is rewritten to `{id}` and compared both ways) |
| `TestOpenAPISecurityMatchesTheRouter` ([`:109`](../../internal/api/docs_test.go#L109)) | a public route is padlocked, a protected one is not, or a protected one documents no 401 |
| `TestOpenAPIRefsResolve` ([`:128`](../../internal/api/docs_test.go#L128)) | a `$ref` names a component that does not exist |
| `TestErrorEnvelopeIsDefinedOnce` | the envelope is defined twice |

The first one caught a real error while this module was written: an unquoted
`?` in a YAML flow mapping (`{ prompt: Years of Go?, … }`) is a syntax error,
and the spec did not parse.

**`yaml.Node` for a mixed map.** A path item holds both operations (objects)
and `parameters` (a list). Decoding it straight into
`map[string]operation` fails on the list. Decoding into `map[string]yaml.Node`
first, then `node.Decode(&op)` for each key except `parameters`, handles both.

## 7. Verification

| Command or action | Result |
|---|---|
| `openapi-spec-validator api/openapi.yaml` (0.7.1) | `OK` |
| `go test ./internal/api -run 'OpenAPI\|Docs\|ErrorEnvelope'` | pass |
| `curl -s -o /dev/null -w '%{http_code} %{content_type}' localhost:8080/docs` | `200 text/html; charset=utf-8` |
| `…/docs/openapi.yaml` | `200 application/yaml; charset=utf-8` |
| `…/docs/assets/swagger-ui-bundle.js` | `200`, 1,452,753 bytes, served from the binary |
| Browser console on `/docs` | no errors (the CSP blocks nothing the page needs) |
| In Swagger UI only: log in as admin, Authorize, create a post, add three questions, set a four-criterion rubric, open the post, log in as a candidate, Authorize, apply, read the score as the candidate, Authorize as admin, read the score | 200, 201, 201 ×3, 200 (version 1), 200 (`open`), 200, 201, **403**, **200** with the full breakdown. Every call sent `Authorization: Bearer` |
