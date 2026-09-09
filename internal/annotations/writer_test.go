package annotations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenock/specyl/pkg/models"
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

	n, err := WriteSwagAnnotations(dir, endpoints, "/api/v1", nil)
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

// TestWriteSwagAnnotations_ParamUsesActualLocationAndRequiredness guards
// against a real bug found by actually running swag against generated
// output: every @Param line was hardcoded as "path ... true", regardless of
// the parameter's real location - a query param (already correctly tagged
// In: "query", Required: false on the endpoint model) came out identical to
// a required path param, silently misdescribing the API to anything that
// consumes these annotations.
func TestWriteSwagAnnotations_ParamUsesActualLocationAndRequiredness(t *testing.T) {
	dir := t.TempDir()
	file := writeSource(t, dir, "handlers.go", `package handlers

import "net/http"

func ListUsers(w http.ResponseWriter, r *http.Request) {}
`)

	endpoints := []models.Endpoint{
		{
			Path: "/users", Method: "GET", Summary: "ListUsers",
			SourceFile: file, HandlerName: "ListUsers",
			Parameters: []models.Parameter{
				{Name: "id", In: "path", Required: true, Description: "user ID"},
				{Name: "role", In: "query", Required: false},
			},
		},
	}
	if _, err := WriteSwagAnnotations(dir, endpoints, "", nil); err != nil {
		t.Fatalf("WriteSwagAnnotations: %v", err)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(out)

	for _, want := range []string{
		`// @Param id path string true "user ID"`,
		// No description was given for "role" - it must still come out as a
		// non-empty quoted string (swag's own @Param regexp requires at
		// least one character between the quotes, or it fails to parse the
		// comment at all and aborts the whole file).
		`// @Param role query string false "role"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("output missing %q, got:\n%s", want, content)
		}
	}
}

// TestWriteSwagAnnotations_QualifiesCrossPackageTypeName guards against
// another bug confirmed by running swag directly: request/response type
// names were always written bare (CreateUserRequest), which swag can only
// resolve within the annotated handler's own package. Handlers and their
// request/response structs living in separate packages (handlers/ +
// models/) is the normal, idiomatic layout - and exactly how every example
// in this repo is organized - so an unqualified name reliably failed with
// "cannot find type definition" the moment swag itself parsed the output.
func TestWriteSwagAnnotations_QualifiesCrossPackageTypeName(t *testing.T) {
	dir := t.TempDir()
	file := writeSource(t, dir, "handlers.go", `package handlers

import (
	"net/http"

	"example.com/api/models"
)

func CreateUser(w http.ResponseWriter, r *http.Request) {
	var req models.CreateUserRequest
	_ = req
}
`)

	endpoints := []models.Endpoint{
		{
			Path: "/users", Method: "POST", Summary: "CreateUser",
			SourceFile: file, HandlerName: "CreateUser",
			RequestBody:      &models.RequestBody{},
			RequestTypeName:  "CreateUserRequest",
			ResponseTypeName: "UserResponse", // declared in "handlers" itself - must stay bare
		},
	}
	typePackageName := map[string]string{
		"CreateUserRequest": "models",
		"UserResponse":      "handlers",
	}
	if _, err := WriteSwagAnnotations(dir, endpoints, "", typePackageName); err != nil {
		t.Fatalf("WriteSwagAnnotations: %v", err)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(out)

	if !strings.Contains(content, `// @Param request body models.CreateUserRequest true "Request body"`) {
		t.Errorf("expected the cross-package request type to be qualified as models.CreateUserRequest, got:\n%s", content)
	}
	if !strings.Contains(content, `// @Success 200 {object} UserResponse "Success"`) {
		t.Errorf("expected the same-package response type to stay bare (UserResponse), got:\n%s", content)
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
	if _, err := WriteSwagAnnotations(dir, endpoints, "", nil); err != nil {
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
	n, err := WriteSwagAnnotations(dir, endpoints, "", nil)
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

func TestWriteSwagAnnotations_StripsEmbeddedNewlinesFromPathAndParams(t *testing.T) {
	// A route path is normally lexically incapable of containing a raw
	// newline, but a backtick raw-string literal in the analyzed project's
	// own source can legitimately span multiple lines. If that newline were
	// written verbatim into a `// @Router ...` comment, everything after it
	// would land as literal (non-comment) source in the target file instead
	// of staying inside the comment block.
	dir := t.TempDir()
	file := writeSource(t, dir, "handlers.go", `package handlers

import "net/http"

func Evil(w http.ResponseWriter, r *http.Request) {}
`)

	// Deliberately does NOT start with "func " (or any other token that would
	// coincidentally match a legitimate declaration line) - the point is to
	// prove this marker never appears anywhere except inside a `//` comment,
	// not to smuggle it past an allowlist.
	const injected = "PWNED_MARKER_should_never_appear_outside_a_comment"
	endpoints := []models.Endpoint{
		{
			Path:       "/products/\n" + injected,
			Method:     "GET",
			Summary:    "Evil",
			Tags:       []string{"tag\n" + injected},
			SourceFile: file, HandlerName: "Evil",
			Parameters: []models.Parameter{{Name: "id\n" + injected, Description: "id"}},
		},
	}
	if _, err := WriteSwagAnnotations(dir, endpoints, "", nil); err != nil {
		t.Fatalf("WriteSwagAnnotations: %v", err)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(out)

	// The marker may still appear (harmlessly) inline within a single
	// comment line - escapeSwagLine joins around the newline with a space
	// rather than deleting it. What must never happen is the marker landing
	// on a line that isn't a `//` comment (i.e. it escaped into real source).
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, injected) {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			t.Errorf("injected marker escaped the comment block on non-comment line: %q\nfull output:\n%s", line, content)
		}
	}
	// The original handler declaration must still be intact and singular.
	if strings.Count(content, "func Evil(w http.ResponseWriter, r *http.Request) {}") != 1 {
		t.Errorf("expected exactly one intact handler declaration, got:\n%s", content)
	}
}

func TestWriteSwagAnnotations_SkipsEndpointsWithoutSourceInfo(t *testing.T) {
	endpoints := []models.Endpoint{{Path: "/x", Method: "GET"}}
	n, err := WriteSwagAnnotations(t.TempDir(), endpoints, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("wrote %d files, want 0 (no SourceFile/HandlerName means nothing to write)", n)
	}
}
