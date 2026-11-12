// Package api holds the OpenAPI document that describes the HTTP API, and the
// Swagger UI page that renders it, compiled into the binary with go:embed.
//
// It lives at the repository root, beside nothing but its own files, because
// an embed directive can only reach files in its package's directory or below.
package api

import "embed"

// Spec is the OpenAPI 3 document, byte for byte as committed.
//
//go:embed openapi.yaml
var Spec []byte

// SwaggerUI is the Swagger UI page and its assets: index.html, init.js, and
// the vendored swagger-ui-dist 5.17.14 bundle and stylesheet.
//
//go:embed swagger-ui
var SwaggerUI embed.FS
