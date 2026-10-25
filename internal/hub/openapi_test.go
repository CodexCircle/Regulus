package hub

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/CodexCircle/Regulus/internal/auth"
)

// The OpenAPI description must list exactly the endpoints the hub serves.
func TestOpenAPIMatchesTheRoutes(t *testing.T) {
	var spec struct {
		OpenAPI string                               `yaml:"openapi"`
		Paths   map[string]map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") {
		t.Fatalf("openapi version = %q", spec.OpenAPI)
	}

	var documented []string
	for path, ops := range spec.Paths {
		for method := range ops {
			documented = append(documented, strings.ToUpper(method)+" "+path)
		}
	}

	served := []string{"GET /healthz"}
	served = append(served, auth.Routes...)
	for _, r := range apiRoutes(Deps{}) {
		served = append(served, r.pattern)
	}

	slices.Sort(documented)
	slices.Sort(served)
	if !slices.Equal(documented, served) {
		t.Fatalf("openapi.yaml and the routes differ\ndocumented: %v\nserved:     %v", documented, served)
	}
}

func TestOpenAPIIsServed(t *testing.T) {
	rec := get(t, NewHandler(Deps{UI: fstest.MapFS{}}), "/api/openapi.yaml")
	if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "openapi: 3.1.0") {
		t.Fatalf("status = %d, body starts %.40q", rec.Code, rec.Body.String())
	}
}
