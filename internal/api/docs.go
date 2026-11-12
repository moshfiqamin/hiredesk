package api

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"

	spec "github.com/moshfiqamin/hiredesk/api"
)

// docsCSP is the Content-Security-Policy of the documentation pages. Every
// script and stylesheet comes from this server, so nothing else is allowed;
// Swagger UI sets inline styles and uses data: URIs for its icons, hence the
// two exceptions. connect-src 'self' is what lets "Try it out" call this API
// and nothing else.
const docsCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// registerDocs serves Swagger UI at /docs and the OpenAPI document it renders
// at /docs/openapi.yaml, both from files compiled into the binary.
func registerDocs(router *gin.Engine) error {
	// Strips the directory prefix, so /docs/assets/x.js maps to
	// swagger-ui/x.js inside the embedded tree.
	assets, err := fs.Sub(spec.SwaggerUI, "swagger-ui")
	if err != nil {
		return err
	}

	// Read once at startup: the page cannot change while the binary runs.
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return err
	}

	docs := router.Group("/docs", docsHeaders)

	// /docs/ needs no route of its own: Gin redirects a trailing slash to the
	// registered path. The page links its assets root-relative, so either URL
	// loads the same files.
	docs.GET("", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
	docs.GET("/openapi.yaml", func(c *gin.Context) {
		// application/yaml is the registered media type (RFC 9512).
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", spec.Spec)
	})
	// http.FS adapts the embedded tree to http.FileSystem, which sets
	// Content-Type from the extension and answers Range and conditional
	// requests.
	docs.StaticFS("/assets", http.FS(assets))

	return nil
}

// docsHeaders applies the documentation pages' security headers.
func docsHeaders(c *gin.Context) {
	c.Header("Content-Security-Policy", docsCSP)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
}
