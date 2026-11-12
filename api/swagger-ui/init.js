// Boots Swagger UI against the spec served beside this page.
window.addEventListener("load", function () {
  window.ui = SwaggerUIBundle({
    url: "/docs/openapi.yaml",
    dom_id: "#swagger-ui",

    // Keeps the token pasted into "Authorize" across page reloads, in this
    // browser's localStorage only. Without it every reload signs the reviewer
    // out of the docs.
    persistAuthorization: true,

    // Opens every operation in "Try it out" mode, so driving the API takes
    // one click per call instead of two.
    tryItOutEnabled: true,

    // Shows how long each call took, next to its response.
    displayRequestDuration: true,

    // Operations stay collapsed so the tag list is the first thing seen.
    docExpansion: "list",

    // A filter box above the tags, for finding an operation by name.
    filter: true,

    // The spec carries its own examples; Swagger UI's "valid" badge would
    // phone home to validator.swagger.io, which this page must never do.
    validatorUrl: null,
  });
});
