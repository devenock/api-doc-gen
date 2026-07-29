package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// writeProject materializes files (relative path -> content) under a fresh
// temp directory and returns its root.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func analyze(t *testing.T, dir, framework string) *models.APISpec {
	t.Helper()
	cfg := &config.Config{
		ProjectPath: dir,
		Framework:   framework,
		DocType:     "swagger",
		Title:       "Test API",
		Version:     "1.0.0",
	}
	spec, err := NewAnalyzer(cfg).Analyze()
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return spec
}

func findEndpoint(t *testing.T, spec *models.APISpec, method, path string) models.Endpoint {
	t.Helper()
	for _, e := range spec.Endpoints {
		if e.Method == method && e.Path == path {
			return e
		}
	}
	var got []string
	for _, e := range spec.Endpoints {
		got = append(got, e.Method+" "+e.Path)
	}
	t.Fatalf("endpoint %s %s not found; got %v", method, path, got)
	return models.Endpoint{}
}

func TestAnalyze_Gin_GroupsParamsBindingAndAuth(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import "github.com/gin-gonic/gin"

type CreateUserRequest struct {
	Name string ` + "`json:\"name\"`" + `
}

func main() {
	r := gin.Default()
	v1 := r.Group("/api/v1")
	v1.GET("/users/:id", GetUser)
	v1.POST("/users", CreateUser)

	admin := v1.Group("/admin", AuthMiddleware())
	admin.GET("/stats", GetStats)
}

func GetUser(c *gin.Context) {}

// CreateUser creates a new user.
func CreateUser(c *gin.Context) {
	var req CreateUserRequest
	c.ShouldBindJSON(&req)
}

func GetStats(c *gin.Context) {}
func AuthMiddleware() gin.HandlerFunc { return nil }
`,
	})

	spec := analyze(t, dir, "gin")

	get := findEndpoint(t, spec, "GET", "/api/v1/users/{id}")
	if len(get.Parameters) != 1 || get.Parameters[0].Name != "id" || get.Parameters[0].In != "path" {
		t.Errorf("GetUser params = %+v, want a single path param \"id\"", get.Parameters)
	}
	if len(get.Security) != 0 {
		t.Errorf("GetUser is not behind auth middleware, want no Security, got %v", get.Security)
	}

	create := findEndpoint(t, spec, "POST", "/api/v1/users")
	if create.RequestBody == nil {
		t.Fatal("CreateUser: expected a request body inferred from ShouldBindJSON(&req)")
	}
	if create.RequestTypeName != "CreateUserRequest" {
		t.Errorf("RequestTypeName = %q, want CreateUserRequest", create.RequestTypeName)
	}
	if !strings.Contains(create.Description, "creates a new user") {
		t.Errorf("Description = %q, want it to include the doc comment", create.Description)
	}

	stats := findEndpoint(t, spec, "GET", "/api/v1/admin/stats")
	if len(stats.Security) == 0 {
		t.Error("GetStats is registered on a group behind AuthMiddleware(), want Security to be set")
	}
}

func TestAnalyze_Gin_EmbeddedFieldsPromotedIntoSchema(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import "github.com/gin-gonic/gin"

type BaseModel struct {
	ID uint ` + "`json:\"id\"`" + `
}

type Product struct {
	BaseModel
	Name string ` + "`json:\"name\"`" + `
}

func main() {
	r := gin.Default()
	r.POST("/products", CreateProduct)
}

func CreateProduct(c *gin.Context) {
	var p Product
	c.ShouldBindJSON(&p)
}
`,
	})

	spec := analyze(t, dir, "gin")
	schema, ok := spec.Models["Product"]
	if !ok {
		t.Fatal("expected Product schema in spec.Models")
	}
	if _, ok := schema.Properties["id"]; !ok {
		t.Errorf("Product.Properties = %v, want promoted embedded field \"id\"", schema.Properties)
	}
	if _, ok := schema.Properties["name"]; !ok {
		t.Errorf("Product.Properties = %v, want \"name\"", schema.Properties)
	}
}

// TestAnalyze_Gin_JSONTagSemantics is the "edge-jsontags" fixture from the
// code review: a struct exercising every json/binding tag edge case in one
// place. It must model encoding/json's actual marshaling behavior, not Go's
// pointer-ness or a naive tag-name split.
func TestAnalyze_Gin_JSONTagSemantics(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import "github.com/gin-gonic/gin"

type Account struct {
	ID           string  ` + "`json:\"id\"`" + `
	Email        string  ` + "`json:\"email\" binding:\"required\"`" + `
	Nickname     string  ` + "`json:\"nickname,omitempty\"`" + `
	PasswordHash string  ` + "`json:\"-\"`" + `
	APIToken     string
	internalFlag bool
	Balance      *float64 ` + "`json:\"balance\"`" + `
}

func CreateAccount(c *gin.Context) {
	var a Account
	c.ShouldBindJSON(&a)
	c.JSON(201, a)
}

func main() {
	r := gin.Default()
	r.POST("/accounts", CreateAccount)
	r.Run()
}
`,
	})

	spec := analyze(t, dir, "gin")
	schema, ok := spec.Models["Account"]
	if !ok {
		t.Fatal("expected Account schema in spec.Models")
	}

	if _, ok := schema.Properties["PasswordHash"]; ok {
		t.Error(`json:"-" field PasswordHash must not appear in the schema (secret-bearing field leak)`)
	}
	if _, ok := schema.Properties["internalFlag"]; ok {
		t.Error("unexported field internalFlag must not appear in the schema (encoding/json never marshals it)")
	}
	// APIToken has no json tag at all. encoding/json still marshals an
	// exported, untagged field under its Go name — so unlike PasswordHash
	// (explicit json:"-") it correctly belongs in the schema. A reviewer
	// wanting API tokens redacted from docs needs to tag the field, the same
	// way they'd need to for encoding/json itself to stop marshaling it.
	if _, ok := schema.Properties["APIToken"]; !ok {
		t.Error("APIToken has no exclusion tag, so encoding/json would marshal it — it should still appear in the schema")
	}
	for _, name := range []string{"id", "email", "nickname", "balance"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("expected tagged property %q in schema, got %v", name, schema.Properties)
		}
	}

	wantRequired := []string{"email"}
	if !slicesEqualUnordered(schema.Required, wantRequired) {
		t.Errorf("Required = %v, want %v (only binding:\"required\" fields, regardless of pointer-ness)", schema.Required, wantRequired)
	}

	balance := schema.Properties["balance"]
	if !balance.Nullable {
		t.Error("Balance is a pointer field, want Nullable=true (pointer-ness means nullable, not required)")
	}

	create := findEndpoint(t, spec, "POST", "/accounts")
	if create.RequestTypeName != "Account" {
		t.Errorf("RequestTypeName = %q, want Account", create.RequestTypeName)
	}
}

// TestAnalyze_JSONTag_DashCommaIsALiteralFieldName covers encoding/json's
// escape for a field that must be marshaled under the literal name "-":
// json:"-," (note the trailing comma) — distinct from json:"-" (exclude).
func TestAnalyze_JSONTag_DashCommaIsALiteralFieldName(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import "github.com/gin-gonic/gin"

type Weird struct {
	Dash string ` + "`json:\"-,\"`" + `
}

func Create(c *gin.Context) {
	var w Weird
	c.ShouldBindJSON(&w)
}

func main() {
	r := gin.Default()
	r.POST("/weird", Create)
}
`,
	})

	spec := analyze(t, dir, "gin")
	schema, ok := spec.Models["Weird"]
	if !ok {
		t.Fatal("expected Weird schema in spec.Models")
	}
	if _, ok := schema.Properties["-"]; !ok {
		t.Errorf(`json:"-," should marshal under the literal name "-", got properties %v`, schema.Properties)
	}
}

func slicesEqualUnordered(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(want))
	for _, w := range want {
		seen[w] = true
	}
	for _, g := range got {
		if !seen[g] {
			return false
		}
	}
	return true
}

func TestAnalyze_Fiber_GroupAndBodyParser(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import "github.com/gofiber/fiber/v2"

type LoginRequest struct {
	Email string ` + "`json:\"email\"`" + `
}

func main() {
	app := fiber.New()
	api := app.Group("/api")
	api.Post("/login", Login)
}

func Login(c *fiber.Ctx) error {
	var req LoginRequest
	return c.BodyParser(&req)
}
`,
	})

	spec := analyze(t, dir, "fiber")
	ep := findEndpoint(t, spec, "POST", "/api/login")
	if ep.RequestBody == nil || ep.RequestTypeName != "LoginRequest" {
		t.Errorf("Login: RequestTypeName = %q, want LoginRequest (from BodyParser)", ep.RequestTypeName)
	}
}

func TestAnalyze_Gorilla_SubrouterMethodsAndAuth(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/health", HealthCheck)

	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(AuthMiddleware)
	api.HandleFunc("/users", ListUsers).Methods("GET")
	api.HandleFunc("/users", CreateUser).Methods(http.MethodPost)
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {}
func ListUsers(w http.ResponseWriter, r *http.Request)   {}
func CreateUser(w http.ResponseWriter, r *http.Request)  {}
func AuthMiddleware(next http.Handler) http.Handler      { return next }
`,
	})

	spec := analyze(t, dir, "gorilla")

	health := findEndpoint(t, spec, "GET", "/health")
	if len(health.Security) != 0 {
		t.Errorf("HealthCheck has no auth middleware, want no Security, got %v", health.Security)
	}

	list := findEndpoint(t, spec, "GET", "/api/v1/users")
	if len(list.Security) == 0 {
		t.Error("ListUsers is under a subrouter with .Use(AuthMiddleware), want Security to be set")
	}

	create := findEndpoint(t, spec, "POST", "/api/v1/users")
	if len(create.Security) == 0 {
		t.Error("CreateUser (.Methods(http.MethodPost)) should resolve to POST and inherit auth")
	}
}

func TestAnalyze_Chi_NestedRoutesAndScopedAuth(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", Ping)

		r.Route("/admin", func(r chi.Router) {
			r.Use(AuthMiddleware)
			r.Get("/stats", Stats)
		})
	})
}

func Ping(w http.ResponseWriter, r *http.Request)  {}
func Stats(w http.ResponseWriter, r *http.Request) {}
func AuthMiddleware(next http.Handler) http.Handler { return next }
`,
	})

	spec := analyze(t, dir, "chi")

	ping := findEndpoint(t, spec, "GET", "/api/v1/ping")
	if len(ping.Security) != 0 {
		t.Errorf("Ping is outside the /admin scope, want no Security, got %v", ping.Security)
	}

	stats := findEndpoint(t, spec, "GET", "/api/v1/admin/stats")
	if len(stats.Security) == 0 {
		t.Error("Stats is inside the /admin scope with .Use(AuthMiddleware), want Security to be set")
	}
}

func TestAnalyze_SkipsSymlinkedGoFiles(t *testing.T) {
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.go")
	if err := os.WriteFile(secretFile, []byte(`package outside

import "net/http"

func SecretHandler(w http.ResponseWriter, r *http.Request) {}

func init() {
	http.HandleFunc("/should-not-appear", SecretHandler)
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.24\n",
		"main.go": `package main

import "net/http"

func Health(w http.ResponseWriter, r *http.Request) {}

func main() {
	http.HandleFunc("/health", Health)
}
`,
	})
	if err := os.Symlink(secretFile, filepath.Join(dir, "evil.go")); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	// No known framework markers in go.mod -> generic net/http route parsing.
	spec := analyze(t, dir, "")

	for _, ep := range spec.Endpoints {
		if ep.Path == "/should-not-appear" {
			t.Fatal("a route registered in a symlinked .go file must not be included in the analysis")
		}
	}
	findEndpoint(t, spec, "GET", "/health")
}

func TestDetectFramework(t *testing.T) {
	tests := []struct {
		require string
		want    string
	}{
		{"require github.com/gin-gonic/gin v1.9.0", "gin"},
		{"require github.com/labstack/echo/v4 v4.11.0", "echo"},
		{"require github.com/gofiber/fiber/v2 v2.50.0", "fiber"},
		{"require github.com/gorilla/mux v1.8.0", "gorilla"},
		{"require github.com/go-chi/chi/v5 v5.0.0", "chi"},
		{"require github.com/spf13/cobra v1.8.0", ""},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		content := "module example.com/x\n\ngo 1.24\n\n" + tt.require + "\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := DetectFramework(dir); got != tt.want {
			t.Errorf("DetectFramework(%q) = %q, want %q", tt.require, got, tt.want)
		}
	}
}
