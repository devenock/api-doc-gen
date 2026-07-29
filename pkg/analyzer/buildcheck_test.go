package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckBuild_ValidProjectPasses(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/valid\n\ngo 1.24\n",
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`,
	})

	result := CheckBuild(dir)
	if result.Skipped {
		t.Skip("go toolchain not available in this environment")
	}
	if !result.OK {
		t.Errorf("CheckBuild = %+v, want OK for a valid project", result)
	}
}

func TestCheckBuild_BrokenProjectFails(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"go.mod": "module example.com/broken\n\ngo 1.24\n",
		"main.go": `package main

func main() {
	undefinedFunction()
}
`,
	})

	result := CheckBuild(dir)
	if result.Skipped {
		t.Skip("go toolchain not available in this environment")
	}
	if result.OK {
		t.Error("CheckBuild = OK, want failure for a project calling an undefined function")
	}
	if result.Output == "" {
		t.Error("expected non-empty Output describing the failure")
	}
}

// TestCheckBuild_SkipsWithoutGoToolchain covers the tool's own Docker
// runtime image: a slim alpine image with just the compiled binary and no
// Go toolchain. CheckBuild must degrade to "skipped", not error, when `go`
// isn't on PATH.
func TestCheckBuild_SkipsWithoutGoToolchain(t *testing.T) {
	emptyPathDir := t.TempDir()
	t.Setenv("PATH", emptyPathDir)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := CheckBuild(dir)
	if !result.Skipped {
		t.Errorf("CheckBuild = %+v, want Skipped=true with no `go` on PATH", result)
	}
}
