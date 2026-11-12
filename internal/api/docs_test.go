package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	spec "github.com/moshfiqamin/hiredesk/api"
)

// openAPIDoc is the part of the OpenAPI document these tests read.
type openAPIDoc struct {
	// yaml.Node, not openAPIOperation: a path item also holds "parameters",
	// a list, beside its operations. operations decodes the rest.
	Paths      map[string]map[string]yaml.Node `yaml:"paths"`
	Components map[string]map[string]yaml.Node `yaml:"components"`
}

type openAPIOperation struct {
	OperationID string                `yaml:"operationId"`
	Security    []map[string][]string `yaml:"security"`
	Responses   map[string]any        `yaml:"responses"`
}

func loadSpec(t *testing.T) openAPIDoc {
	t.Helper()
	var doc openAPIDoc
	if err := yaml.Unmarshal(spec.Spec, &doc); err != nil {
		t.Fatalf("api/openapi.yaml is not valid YAML: %v", err)
	}
	return doc
}

// operations returns every documented operation, keyed "METHOD /path".
func operations(t *testing.T, doc openAPIDoc) map[string]openAPIOperation {
	t.Helper()
	out := map[string]openAPIOperation{}
	for path, item := range doc.Paths {
		for method, node := range item {
			if method == "parameters" {
				continue
			}
			var op openAPIOperation
			if err := node.Decode(&op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			out[strings.ToUpper(method)+" "+path] = op
		}
	}
	return out
}

// ginParam matches a Gin path parameter, such as :id.
var ginParam = regexp.MustCompile(`:([A-Za-z]+)`)

// publicRoutes are the routes that take no token. Every other documented
// operation must require bearerAuth, and these must not.
var publicRoutes = []string{
	"GET /healthz",
	"GET /readyz",
	"POST /v1/auth/register",
	"POST /v1/auth/login",
	"POST /v1/auth/refresh",
	"POST /v1/auth/logout",
	"GET /v1/job-posts",
	"GET /v1/job-posts/{id}/listing",
}

// TestOpenAPICoversEveryRoute fails when a route is added to the router
// without being documented, or documented without existing. Either way the
// spec has drifted from the code, and a client generated from it would be
// wrong.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	doc := loadSpec(t)
	s := newTestServer(t)

	routes := map[string]bool{}
	for _, r := range s.engine.Routes() {
		// The documentation's own routes are not part of the API.
		if strings.HasPrefix(r.Path, "/docs") {
			continue
		}
		// HEAD is added for static files only; there are none outside /docs.
		path := ginParam.ReplaceAllString(r.Path, "{$1}")
		routes[r.Method+" "+path] = true
	}

	documented := map[string]bool{}
	for op := range operations(t, doc) {
		documented[op] = true
	}

	for route := range routes {
		if !documented[route] {
			t.Errorf("%s is served but not in api/openapi.yaml", route)
		}
	}
	for op := range documented {
		if !routes[op] {
			t.Errorf("%s is in api/openapi.yaml but not served", op)
		}
	}
}

func TestOpenAPISecurityMatchesTheRouter(t *testing.T) {
	doc := loadSpec(t)

	for route, op := range operations(t, doc) {
		requiresBearer := len(op.Security) == 1 && op.Security[0]["bearerAuth"] != nil

		switch {
		case slices.Contains(publicRoutes, route) && requiresBearer:
			t.Errorf("%s is public but documented as needing a token", route)
		case !slices.Contains(publicRoutes, route) && !requiresBearer:
			t.Errorf("%s needs a token but has no bearerAuth security requirement", route)
		case requiresBearer && op.Responses["401"] == nil:
			t.Errorf("%s needs a token but documents no 401", route)
		}
	}
}

// TestOpenAPIRefsResolve checks every local $ref names a component that
// exists, which a YAML parser alone does not.
func TestOpenAPIRefsResolve(t *testing.T) {
	doc := loadSpec(t)

	refs := regexp.MustCompile(`\$ref: "#/components/([A-Za-z]+)/([A-Za-z0-9]+)"`).FindAllStringSubmatch(string(spec.Spec), -1)
	if len(refs) < 100 {
		t.Fatalf("found %d $refs; the pattern no longer matches how the spec writes them", len(refs))
	}
	for _, ref := range refs {
		section, name := ref[1], ref[2]
		if _, ok := doc.Components[section][name]; !ok {
			t.Errorf("$ref to #/components/%s/%s, which does not exist", section, name)
		}
	}
}

// TestErrorEnvelopeIsDefinedOnce guards the point of the components section:
// the envelope is described in one place and referenced everywhere else.
func TestErrorEnvelopeIsDefinedOnce(t *testing.T) {
	text := string(spec.Spec)
	if n := strings.Count(text, "\n    ErrorEnvelope:\n"); n != 1 {
		t.Errorf("ErrorEnvelope is defined %d times, want 1", n)
	}
	if n := strings.Count(text, `$ref: "#/components/schemas/ErrorEnvelope"`); n < 8 {
		t.Errorf("ErrorEnvelope is referenced %d times, want one per shared error response", n)
	}
}

func TestDocsAreServedFromTheBinary(t *testing.T) {
	s := newTestServer(t)

	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/docs", contentType: "text/html; charset=utf-8", contains: `<div id="swagger-ui">`},
		{path: "/docs/openapi.yaml", contentType: "application/yaml; charset=utf-8", contains: "openapi: 3.0.3"},
		{path: "/docs/assets/swagger-ui-bundle.js", contentType: "text/javascript; charset=utf-8", contains: "SwaggerUIBundle"},
		{path: "/docs/assets/swagger-ui.css", contentType: "text/css; charset=utf-8", contains: ".swagger-ui"},
		{path: "/docs/assets/init.js", contentType: "text/javascript; charset=utf-8", contains: "persistAuthorization: true"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := s.get(tt.path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.contentType)
			}
			if !strings.Contains(rec.Body.String(), tt.contains) {
				t.Errorf("body does not contain %q", tt.contains)
			}
			if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
				t.Errorf("Content-Security-Policy = %q, want scripts limited to this server", csp)
			}
		})
	}

	// The trailing-slash form reaches the same page.
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/docs" {
		t.Errorf("/docs/: status = %d, Location = %q; want a redirect to /docs", rec.Code, rec.Header().Get("Location"))
	}
}
