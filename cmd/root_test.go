package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestIsInteractiveTerminal_PipeIsNotInteractive(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isInteractiveTerminal(r) {
		t.Error("a pipe must not be reported as an interactive terminal")
	}
}

func TestIsInteractiveTerminal_DevNullIsNotInteractive(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isInteractiveTerminal(f) {
		t.Error("/dev/null must not be reported as an interactive terminal")
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestInitConfig_MalformedConfigFileWarns(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	if err := os.WriteFile(".specyl.yaml", []byte("type: swagger\n  bad indentation: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldCfgFile := cfgFile
	cfgFile = ""
	t.Cleanup(func() { cfgFile = oldCfgFile })

	out := captureStderr(t, initConfig)
	if !strings.Contains(out, "could not read config file") {
		t.Errorf("expected a warning about the malformed config file; got:\n%s", out)
	}
}

func TestInitConfig_MissingConfigFileIsSilent(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	oldCfgFile := cfgFile
	cfgFile = ""
	t.Cleanup(func() { cfgFile = oldCfgFile })

	out := captureStderr(t, initConfig)
	if out != "" {
		t.Errorf("a missing (as opposed to malformed) config file is the common case and must stay silent; got:\n%s", out)
	}
}
