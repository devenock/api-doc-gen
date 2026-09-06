package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInit_CreatesConfigWithDetectedFramework(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	goMod := "module testapi\n\ngo 1.24\n\nrequire github.com/gin-gonic/gin v1.9.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".apidoc-gen.yaml"))
	if err != nil {
		t.Fatalf("expected .apidoc-gen.yaml to be created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `framework: "gin"`) {
		t.Errorf("expected detected framework \"gin\" in config, got:\n%s", content)
	}
	if !strings.Contains(content, "output: ./docs") {
		t.Errorf("expected default output dir in config, got:\n%s", content)
	}
}

func TestRunInit_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	configPath := filepath.Join(dir, ".apidoc-gen.yaml")
	sentinel := "# hand-edited, do not touch\n"
	if err := os.WriteFile(configPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sentinel {
		t.Errorf("expected existing config to be left untouched, got:\n%s", string(data))
	}
}

func TestRunInit_NoFrameworkDetected(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	goMod := "module testapi\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".apidoc-gen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `framework: ""`) {
		t.Errorf("expected empty framework when none detected, got:\n%s", string(data))
	}
}
