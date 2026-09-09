package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/devenock/specyl/pkg/analyzer"
	"github.com/devenock/specyl/pkg/config"
	"github.com/devenock/specyl/pkg/models"
)

// validateOpenAPI loads and validates a generated openapi.json against the
// OpenAPI 3.0 JSON Schema, failing the test with kin-openapi's error if the
// document is invalid.
func validateOpenAPI(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc, err := openapi3.NewLoader().LoadFromData(data)
	if err != nil {
		t.Fatalf("%s did not parse as OpenAPI: %v", path, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Errorf("%s is not a valid OpenAPI 3.0 document: %v", path, err)
	}
}

// TestSwaggerGenerate_OutputIsValidOpenAPI is review §6.1: the generator
// hand-builds the OpenAPI document as map[string]interface{}, so nothing
// short of an actual schema validator catches a malformed edge case (empty
// properties, a missing required "description" on a response, $ref
// siblings). Runs across a spread of shapes — empty, minimal, and every
// feature the analyzer can produce at once — since a bug in one code path
// (e.g. nullable, or an endpoint with no properties) wouldn't necessarily
// show up in another.
func TestSwaggerGenerate_OutputIsValidOpenAPI(t *testing.T) {
	tests := []struct {
		name string
		spec *models.APISpec
	}{
		{"empty", &models.APISpec{Title: "Empty API", Version: "1.0.0"}},
		{
			"minimal",
			&models.APISpec{
				Title: "Minimal API", Version: "1.0.0",
				Endpoints: []models.Endpoint{
					{Method: "GET", Path: "/health", Responses: map[int]models.Response{200: {Description: "ok"}}},
				},
			},
		},
		{"kitchen sink", kitchenSinkSpec()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := &config.Config{Output: dir, Title: tt.spec.Title, Version: tt.spec.Version, Quiet: true}
			if err := NewSwaggerGenerator(cfg).Generate(tt.spec); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			validateOpenAPI(t, filepath.Join(dir, "openapi.json"))
		})
	}
}

// TestSwaggerGenerate_RealProjectOutputIsValidOpenAPI runs the full
// analyzer+generator pipeline against a bundled real-world example project
// and validates the result — a stronger check than hand-crafted specs,
// since real code exercises combinations of analyzer inference (embedded
// fields, multiple responses per endpoint, nullable pointers, map-literal
// bodies, etc.) that a synthetic spec might not happen to combine.
func TestSwaggerGenerate_RealProjectOutputIsValidOpenAPI(t *testing.T) {
	projectPath := filepath.Join("..", "..", "examples", "ecommerce-gin")
	if _, err := os.Stat(projectPath); err != nil {
		t.Skipf("example project not available: %v", err)
	}

	cfg := &config.Config{
		ProjectPath: projectPath,
		Framework:   "gin",
		DocType:     "swagger",
		Title:       "Ecommerce API",
		Version:     "1.0.0",
		Quiet:       true,
	}
	spec, err := analyzer.NewAnalyzer(cfg).Analyze()
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(spec.Endpoints) == 0 {
		t.Fatal("expected at least one endpoint from the example project")
	}

	dir := t.TempDir()
	cfg.Output = dir
	if err := NewSwaggerGenerator(cfg).Generate(spec); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	validateOpenAPI(t, filepath.Join(dir, "openapi.json"))
}

// kitchenSinkSpec exercises every schema feature the analyzer can produce in
// one document: a $ref'd request body, multiple typed responses on one
// endpoint (review §3), a nullable pointer field (review §2.3/2.4), an
// array-of-$ref property, an inline object with no declared properties, and
// a security requirement (which pulls in the BearerAuth scheme).
func kitchenSinkSpec() *models.APISpec {
	return &models.APISpec{
		Title:       "Kitchen Sink API",
		Version:     "1.0.0",
		Description: "Exercises every schema shape at once.",
		BasePath:    "/api/v1",
		Servers:     []models.Server{{URL: "http://localhost:8080", Description: "dev"}},
		Endpoints: []models.Endpoint{
			{
				Method: "POST", Path: "/users", Tags: []string{"users"},
				Security: []map[string][]string{{"BearerAuth": {}}},
				RequestBody: &models.RequestBody{
					Required: true,
					Content: map[string]models.Content{"application/json": {
						Schema: models.Schema{Ref: "#/components/schemas/CreateUserRequest"},
					}},
				},
				Responses: map[int]models.Response{
					201: {Description: "Created", Content: map[string]models.Content{
						"application/json": {Schema: models.Schema{Ref: "#/components/schemas/User"}},
					}},
					400: {Description: "Bad Request", Content: map[string]models.Content{
						"application/json": {Schema: models.Schema{Type: "object"}},
					}},
				},
			},
			{
				Method: "DELETE", Path: "/users/{id}", Tags: []string{"users"},
				Parameters: []models.Parameter{{Name: "id", In: "path", Required: true, Schema: models.Schema{Type: "string"}}},
				Security:   []map[string][]string{{"BearerAuth": {}}},
				Responses:  map[int]models.Response{204: {Description: "No Content"}},
			},
		},
		Models: map[string]models.Schema{
			"CreateUserRequest": {
				Type:     "object",
				Required: []string{"email"},
				Properties: map[string]models.Schema{
					"email":   {Type: "string"},
					"balance": {Type: "number", Format: "double", Nullable: true},
				},
			},
			"User": {
				Type: "object",
				Properties: map[string]models.Schema{
					"id":    {Type: "string"},
					"email": {Type: "string"},
					"tags":  {Type: "array", Items: &models.Schema{Ref: "#/components/schemas/Tag"}},
					"meta":  {Type: "object"},
				},
			},
			"Tag": {
				Type:       "object",
				Properties: map[string]models.Schema{"name": {Type: "string"}},
			},
		},
	}
}
