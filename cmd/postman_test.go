package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintPostmanInstructions_QuietPrintsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "collection.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { printPostmanInstructions(dir, true) })
	if out != "" {
		t.Errorf("expected no output in quiet mode; got:\n%s", out)
	}
}

func TestPrintPostmanInstructions_NoCollectionFilePrintsNothing(t *testing.T) {
	dir := t.TempDir() // no collection.json written
	out := captureStdout(t, func() { printPostmanInstructions(dir, false) })
	if out != "" {
		t.Errorf("expected no output when collection.json doesn't exist; got:\n%s", out)
	}
}

func TestPrintPostmanInstructions_PrintsPathAndSteps(t *testing.T) {
	dir := t.TempDir()
	collectionPath := filepath.Join(dir, "collection.json")
	if err := os.WriteFile(collectionPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() { printPostmanInstructions(dir, false) })
	for _, want := range []string{collectionPath, "Import", "drag"} {
		if !strings.Contains(out, want) {
			t.Errorf("printPostmanInstructions output missing %q; got:\n%s", want, out)
		}
	}
}
