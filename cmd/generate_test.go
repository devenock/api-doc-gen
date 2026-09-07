package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devenock/api-doc-gen/pkg/config"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. The functions under test here (printShowConfig,
// runDryRun) print directly via fmt.Print*, not through an injectable writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// ginFixtureProject writes a minimal Gin project (go.mod + a handler with
// one route) under a fresh temp dir and returns its root.
func ginFixtureProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module testapi\n\ngo 1.24\n\nrequire github.com/gin-gonic/gin v1.9.1\n",
		"main.go": `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/ping", pingHandler)
	r.Run()
}

func pingHandler(c *gin.Context) {
	c.JSON(200, gin.H{"message": "pong"})
}
`,
	}
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

func TestPrintShowConfig(t *testing.T) {
	cfg := &config.Config{
		ProjectPath: "/tmp/proj",
		Output:      "./docs",
		DocType:     "swagger",
		Framework:   "gin",
		Title:       "My API",
		Version:     "1.2.3",
		Servers:     []config.ServerConfig{{URL: "http://localhost:8080", Description: "dev"}},
	}

	out := captureStdout(t, func() { printShowConfig(cfg) })

	for _, want := range []string{
		`project_path: "/tmp/proj"`,
		`type: "swagger"`,
		`framework: "gin"`,
		`title: "My API"`,
		`version: "1.2.3"`,
		"servers: 1",
		`url="http://localhost:8080"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("printShowConfig output missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunDryRun_ReportsEndpointsWithoutWriting(t *testing.T) {
	dir := ginFixtureProject(t)
	outDir := filepath.Join(t.TempDir(), "docs")

	cfg := &config.Config{
		ProjectPath: dir,
		Output:      outDir,
		DocType:     "swagger",
		Title:       "Test API",
		Version:     "1.0.0",
	}

	out := captureStdout(t, func() {
		if err := runDryRun(cfg, false); err != nil {
			t.Fatalf("runDryRun: %v", err)
		}
	})

	if !strings.Contains(out, "GET /ping") {
		t.Errorf("expected dry-run output to list GET /ping; got:\n%s", out)
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create the output directory; stat err=%v", err)
	}
}

func TestRunGenerate_NoTypeNonInteractiveStdin_FailsFastWithClearError(t *testing.T) {
	dir := ginFixtureProject(t)

	// A closed pipe mimics stdin redirected from /dev/null or a script - not
	// a terminal, and reading it returns EOF immediately. Without the
	// isInteractiveTerminal guard, this used to reach promptui, which itself
	// doesn't hang here, but does write raw terminal escape sequences into
	// stdout/stderr before failing with a cryptic "^D" error instead of this
	// clear, actionable one.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin; _ = r.Close() }()

	rootCmd.SetArgs([]string{"generate", dir, "-o", t.TempDir()})
	err = rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error when stdin is not a terminal and no --type/--no-interactive was given")
	}
	if !strings.Contains(err.Error(), "not an interactive terminal") {
		t.Errorf("error = %v, want a message about stdin not being an interactive terminal", err)
	}
}

func TestRunGenerate_SwaggerNoInteractive(t *testing.T) {
	dir := ginFixtureProject(t)
	outDir := t.TempDir()

	rootCmd.SetArgs([]string{
		"generate", dir,
		"--no-interactive",
		"--type", "swagger",
		"-o", outDir,
		"--quiet",
		"--skip-build-check",
	})
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, name := range []string{"openapi.json", "openapi.yaml", "index.html"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("expected %s to be generated: %v", name, err)
		}
	}
}

// TestRunGenerate_ServeFalseReturnsWithoutBlocking guards against the --serve
// flag going back to being a no-op: swagger generation used to always call
// runServeDocs (which blocks until Ctrl+C) whenever --quiet wasn't set,
// completely ignoring --serve's value. This asserts --serve=false actually
// skips it - if it doesn't, this test hangs instead of failing cleanly.
func TestRunGenerate_ServeFalseReturnsWithoutBlocking(t *testing.T) {
	dir := ginFixtureProject(t)
	outDir := t.TempDir()

	rootCmd.SetArgs([]string{
		"generate", dir,
		"--no-interactive",
		"--type", "swagger",
		"-o", outDir,
		"--skip-build-check",
		"--serve=false",
	})
	done := make(chan error, 1)
	go func() { done <- rootCmd.ExecuteContext(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("generate did not return within 5s - --serve=false is not skipping runServeDocs")
	}
}
