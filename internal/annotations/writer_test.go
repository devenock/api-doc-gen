package annotations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenock/api-doc-gen/pkg/models"
)

func writeSource(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteSwagAnnotations_InsertsBlockAboveHandler(t *testing.T) {
	dir := t.TempDir()
	file := writeSource(t, dir, "handlers.go", `package handlers

import "net/http"

func CreateUser(w http.ResponseWriter, r *http.Request) {}
`)

	endpoints := []models.Endpoint{
		{
			Path: "/users", Method: "POST", Summary: "CreateUser", Description: "Create a user.",
			Tags: []string{"users"}, SourceFile: file, HandlerName: "CreateUser",
			RequestBody:     &models.RequestBody{},
			RequestTypeName: "CreateUserRequest",
			Security:        []map[string][]string{{"BearerAuth": {}}},
		},
	}

	n, err := WriteSwagAnnotations(endpoints, "/api/v1")
	if err != nil {
		t.Fatalf("WriteSwagAnnotations: %v", err)
	}
	if n != 1 {
		t.Fatalf("wrote %d files, want 1", n)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(out)

	for _, want := range []string{
		"// @Summary CreateUser",
		"// @Description Create a user.",
		"// @Tags users",
		`// @Param request body CreateUserRequest true "Request body"`,
		"// @Security BearerAuth",
		"// @Router /api/v1/users [post]",
		"func CreateUser(w http.ResponseWriter, r *http.Request) {}",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("output missing %q, got:\n%s", want, content)
		}
	}
}

func TestWriteSwagAnnotations_ReplacesExistingBlockInsteadOfDuplicating(t *testing.T) {
	dir := t.TempDir()
	file := writeSource(t, dir, "handlers.go", `package handlers

import "net/http"

// @Summary Old summary
// @Router /old [get]
func GetUser(w http.ResponseWriter, r *http.Request) {}
`)

	endpoints := []models.Endpoint{
		{Path: "/users/{id}", Method: "GET", Summary: "GetUser", SourceFile: file, HandlerName: "GetUser"},
	}
	if _, err := WriteSwagAnnotations(endpoints, ""); err != nil {
		t.Fatalf("WriteSwagAnnotations: %v", err)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(out)

	if strings.Contains(content, "Old summary") {
		t.Errorf("expected the stale swag block to be replaced, got:\n%s", content)
	}
	if strings.Count(content, "func GetUser") != 1 {
		t.Errorf("expected exactly one handler declaration, got:\n%s", content)
	}
	if !strings.Contains(content, "// @Router /users/{id} [get]") {
		t.Errorf("expected updated @Router line, got:\n%s", content)
	}
}

func TestWriteSwagAnnotations_GroupsMultipleRoutesOnSameHandler(t *testing.T) {
	dir := t.TempDir()
	file := writeSource(t, dir, "handlers.go", `package handlers

import "net/http"

func Health(w http.ResponseWriter, r *http.Request) {}
`)

	endpoints := []models.Endpoint{
		{Path: "/health", Method: "GET", Summary: "Health", SourceFile: file, HandlerName: "Health"},
		{Path: "/healthz", Method: "GET", Summary: "Health", SourceFile: file, HandlerName: "Health"},
	}
	n, err := WriteSwagAnnotations(endpoints, "")
	if err != nil {
		t.Fatalf("WriteSwagAnnotations: %v", err)
	}
	if n != 1 {
		t.Fatalf("wrote %d files, want 1 (both routes share one handler)", n)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(out)
	if !strings.Contains(content, "// @Router /health [get]") || !strings.Contains(content, "// @Router /healthz [get]") {
		t.Errorf("expected a @Router line per route sharing the handler, got:\n%s", content)
	}
}

func TestWriteSwagAnnotations_SkipsEndpointsWithoutSourceInfo(t *testing.T) {
	endpoints := []models.Endpoint{{Path: "/x", Method: "GET"}}
	n, err := WriteSwagAnnotations(endpoints, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("wrote %d files, want 0 (no SourceFile/HandlerName means nothing to write)", n)
	}
}
