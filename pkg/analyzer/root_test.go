package analyzer

import (
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/devenock/specyl/pkg/config"
)

// newRootedAnalyzer builds an Analyzer with root opened exactly as Analyze()
// opens it, without running a full Analyze() pass - for tests that exercise
// rootReadFile/rootParseFile/walkProjectDir directly.
func newRootedAnalyzer(t *testing.T, projectPath string) *Analyzer {
	t.Helper()
	root, err := os.OpenRoot(projectPath)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { root.Close() })
	return &Analyzer{config: &config.Config{ProjectPath: projectPath}, root: root}
}

func TestRootReadFile_ReadsRegularFileWithinProject(t *testing.T) {
	dir := writeProject(t, map[string]string{"main.go": "package main\n"})
	a := newRootedAnalyzer(t, dir)

	data, err := a.rootReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("rootReadFile: %v", err)
	}
	if string(data) != "package main\n" {
		t.Errorf("got %q", data)
	}
}

func TestRootReadFile_RefusesSymlinkEscapingProject(t *testing.T) {
	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.go")
	if err := os.WriteFile(secret, []byte("package secret\nconst Key = \"leak-me\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := writeProject(t, map[string]string{"main.go": "package main\n"})
	link := filepath.Join(dir, "evil.go")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	a := newRootedAnalyzer(t, dir)
	data, err := a.rootReadFile(link)
	if err == nil {
		t.Fatalf("expected rootReadFile to refuse a symlink escaping the project, got data: %q", data)
	}
}

func TestRootReadFile_RefusesSymlinkWithinProject(t *testing.T) {
	// os.Root itself would follow a symlink that stays within the root (only
	// escaping ones are blocked at that layer) - rootReadFile must add its
	// own check on top, matching the pre-os.Root behavior of refusing every
	// symlink unconditionally, not just ones that escape.
	dir := writeProject(t, map[string]string{
		"real.go": "package main\nconst X = 1\n",
	})
	link := filepath.Join(dir, "alias.go")
	if err := os.Symlink(filepath.Join(dir, "real.go"), link); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	a := newRootedAnalyzer(t, dir)
	if _, err := a.rootReadFile(link); err == nil {
		t.Fatal("expected rootReadFile to refuse an in-project symlink too, got nil error")
	}
}

func TestRootParseFile_RefusesSymlinkEscapingProject(t *testing.T) {
	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.go")
	if err := os.WriteFile(secret, []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := writeProject(t, map[string]string{"main.go": "package main\n"})
	link := filepath.Join(dir, "evil.go")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	a := newRootedAnalyzer(t, dir)
	if node, err := a.rootParseFile(token.NewFileSet(), link, 0); err == nil {
		t.Fatalf("expected rootParseFile to refuse a symlink escaping the project, got node: %+v", node)
	}
}

func TestWalkProjectDir_SkipsSymlinkedDirEntirely(t *testing.T) {
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "inside.go"), []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := writeProject(t, map[string]string{"main.go": "package main\n"})
	if err := os.Symlink(outsideDir, filepath.Join(dir, "linked_dir")); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	a := newRootedAnalyzer(t, dir)
	var visited []string
	err := a.walkProjectDir(func(path string, d fs.DirEntry) error {
		visited = append(visited, filepath.Base(path))
		return nil
	})
	if err != nil {
		t.Fatalf("walkProjectDir: %v", err)
	}
	for _, name := range visited {
		if name == "inside.go" {
			t.Errorf("walkProjectDir descended into a symlinked directory; visited: %v", visited)
		}
	}
}
