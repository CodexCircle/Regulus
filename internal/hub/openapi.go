package hub

import (
	_ "embed"
	"net/http"
)

// openAPISpec describes the HTTP API. A test keeps it in sync with the routes.
//
//go:embed openapi.yaml
var openAPISpec []byte

func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(openAPISpec)
}
