package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if strings.Contains(out, "postman.") {
		t.Errorf("expected no postman.* fields for a non-postman DocType; got:\n%s", out)
	}
}

func TestPrintShowConfig_PostmanFields(t *testing.T) {
	cfg := &config.Config{DocType: "postman", PostmanWorkspaceUID: "ws-1"}
	out := captureStdout(t, func() { printShowConfig(cfg) })
	if !strings.Contains(out, `postman.workspace: "ws-1"`) {
		t.Errorf("expected postman.workspace in output; got:\n%s", out)
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
