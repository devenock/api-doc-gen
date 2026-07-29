package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate_ProjectPathMustExist(t *testing.T) {
	cfg := &Config{ProjectPath: filepath.Join(t.TempDir(), "does-not-exist"), DocType: "swagger"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for nonexistent project path, got nil")
	}
}

func TestValidate_RejectsUnknownDocType(t *testing.T) {
	cfg := &Config{ProjectPath: t.TempDir(), DocType: "docx"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid doc type, got nil")
	}
}

func TestValidate_AcceptsSwaggerAndPostman(t *testing.T) {
	for _, dt := range []string{"swagger", "postman"} {
		cfg := &Config{ProjectPath: t.TempDir(), DocType: dt}
		if err := cfg.Validate(); err != nil {
			t.Errorf("DocType %q: unexpected error: %v", dt, err)
		}
	}
}

func TestValidate_FillsDefaults(t *testing.T) {
	cfg := &Config{ProjectPath: t.TempDir(), DocType: "swagger"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Output != "./docs" {
		t.Errorf("Output = %q, want ./docs", cfg.Output)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", cfg.Version)
	}
	if cfg.Title != "API Documentation" {
		t.Errorf("Title = %q, want fallback %q (no go.mod present)", cfg.Title, "API Documentation")
	}
	wantExclude := []string{"vendor", "node_modules", ".git", "test", "tests"}
	if len(cfg.Exclude) != len(wantExclude) {
		t.Fatalf("Exclude = %v, want %v", cfg.Exclude, wantExclude)
	}
	for i, v := range wantExclude {
		if cfg.Exclude[i] != v {
			t.Errorf("Exclude[%d] = %q, want %q", i, cfg.Exclude[i], v)
		}
	}
}

func TestValidate_DoesNotOverrideExplicitValues(t *testing.T) {
	cfg := &Config{
		ProjectPath: t.TempDir(),
		DocType:     "swagger",
		Output:      "./out",
		Title:       "My API",
		Version:     "2.0.0",
		Exclude:     []string{"fixtures"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Output != "./out" || cfg.Title != "My API" || cfg.Version != "2.0.0" {
		t.Errorf("Validate overwrote explicit config: %+v", cfg)
	}
	if len(cfg.Exclude) != 1 || cfg.Exclude[0] != "fixtures" {
		t.Errorf("Exclude = %v, want [fixtures]", cfg.Exclude)
	}
}

func TestValidate_TitleFromGoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/acme/my-cool_api\n\ngo 1.24\n")

	cfg := &Config{ProjectPath: dir, DocType: "swagger"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Title != "My Cool Api" {
		t.Errorf("Title = %q, want %q", cfg.Title, "My Cool Api")
	}
}

func TestValidate_SymlinkedGoModIsIgnored(t *testing.T) {
	// A symlinked go.mod could point at an arbitrary file outside the project
	// tree; detectProjectName must refuse to follow it and fall back instead
	// of reading (and deriving a title from) whatever it points to.
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "real.mod")
	writeFile(t, outside, "module should-not-be-read\n")
	if err := os.Symlink(outside, filepath.Join(dir, "go.mod")); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	cfg := &Config{ProjectPath: dir, DocType: "swagger"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Title != "API Documentation" {
		t.Errorf("Title = %q, want fallback (symlinked go.mod must not be followed)", cfg.Title)
	}
}

func TestShouldExclude(t *testing.T) {
	cfg := &Config{Exclude: []string{"vendor", ".git"}}
	if !cfg.ShouldExclude("vendor") {
		t.Error("expected vendor to be excluded")
	}
	if cfg.ShouldExclude("git") {
		t.Error("bare \"git\" must not match \".git\" (exact match only)")
	}
	if cfg.ShouldExclude("src") {
		t.Error("src should not be excluded")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
