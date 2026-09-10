package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenock/specyl/pkg/config"
	"github.com/devenock/specyl/pkg/models"
)

func TestSwaggerGenerate_WritesExpectedFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Output: dir, Title: "My API", Version: "1.0.0", Quiet: true}
	g := NewSwaggerGenerator(cfg)
	spec := &models.APISpec{
		Title:   "My API",
		Version: "1.0.0",
		Endpoints: []models.Endpoint{
			{Method: "GET", Path: "/health", Responses: map[int]models.Response{200: {Description: "ok"}}},
		},
	}
	if err := g.Generate(spec); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, f := range []string{"openapi.json", "openapi.yaml", "index.html"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected %s to be written: %v", f, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	paths, ok := doc["paths"].(map[string]interface{})
	if !ok || paths["/health"] == nil {
		t.Errorf("openapi.json paths = %v, want a /health entry", doc["paths"])
	}
}

func TestGenerateSwaggerUI_EscapesTitleAgainstXSS(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Output: dir, Title: `</title><script>alert(1)</script>`}
	g := NewSwaggerGenerator(cfg)
	path := filepath.Join(dir, "index.html")
	if err := g.generateSwaggerUI(path); err != nil {
		t.Fatalf("generateSwaggerUI: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("title was embedded unescaped: a crafted title can inject a script into the generated Swagger UI page")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected the title to be HTML-escaped, got: %s", html)
	}
}

func TestSchemaToMap_RefIsExclusive(t *testing.T) {
	s := models.Schema{Ref: "#/components/schemas/Widget", Type: "object", Description: "should be dropped"}
	m := schemaToMap(s)
	if len(m) != 1 || m["$ref"] != "#/components/schemas/Widget" {
		t.Errorf("schemaToMap with Ref set = %v, want only {$ref: ...} (OpenAPI 3.0 forbids $ref siblings)", m)
	}
}

func TestSchemaToMap_NullableOnNonRefSchema(t *testing.T) {
	s := models.Schema{Type: "number", Format: "double", Nullable: true}
	m := schemaToMap(s)
	if m["nullable"] != true {
		t.Errorf("schemaToMap = %v, want nullable: true for a pointer-derived schema", m)
	}
}

func TestSchemaToMap_OmitsNullableWhenFalse(t *testing.T) {
	s := models.Schema{Type: "string"}
	m := schemaToMap(s)
	if _, ok := m["nullable"]; ok {
		t.Errorf("schemaToMap = %v, want no \"nullable\" key for a non-nullable schema", m)
	}
}

func TestConvertToOpenAPI_AddsBearerSecuritySchemeOnlyWhenUsed(t *testing.T) {
	cfg := &config.Config{Title: "T", Version: "1.0.0"}
	g := NewSwaggerGenerator(cfg)

	noAuth := &models.APISpec{Endpoints: []models.Endpoint{
		{Method: "GET", Path: "/x", Responses: map[int]models.Response{}},
	}}
	if _, ok := g.convertToOpenAPI(noAuth)["components"]; ok {
		t.Error("no endpoint uses security, want no components.securitySchemes")
	}

	withAuth := &models.APISpec{Endpoints: []models.Endpoint{
		{Method: "GET", Path: "/x", Security: []map[string][]string{{"BearerAuth": {}}}, Responses: map[int]models.Response{}},
	}}
	comp, ok := g.convertToOpenAPI(withAuth)["components"].(map[string]interface{})
	if !ok {
		t.Fatal("expected components when an endpoint uses security")
	}
	if _, ok := comp["securitySchemes"]; !ok {
		t.Error("expected components.securitySchemes.BearerAuth")
	}
}
