package postman

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolateConfigDir points CredentialsPath at a throwaway directory so tests
// never read or write the real user credentials file, and clears the env
// vars LoadAPIKey checks first so tests control exactly what it sees.
// CredentialsPath resolves its base directory via os.UserConfigDir(), which
// reads a different env var per OS (XDG_CONFIG_HOME/HOME on Unix, AppData on
// Windows) - all three must be set or this isolates nothing on Windows.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgconfig"))
	t.Setenv("AppData", filepath.Join(tmp, "AppData", "Roaming"))
	t.Setenv(EnvAPIDocPostmanKey, "")
	t.Setenv(EnvPostmanKey, "")
}

func TestSaveAndLoadAPIKey_RoundTrip(t *testing.T) {
	isolateConfigDir(t)

	if key, source := LoadAPIKey(); key != "" || source != "" {
		t.Fatalf("expected no key before saving, got (%q, %q)", key, source)
	}

	path, err := SaveAPIKey("test-key-123")
	if err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("credentials file not created: %v", err)
	}
	// Windows has no POSIX permission bits - os.WriteFile(0o600) there just
	// clears the read-only attribute, and Stat reports back something like
	// 0666, not 0600. The 0600 request is still correct/harmless to make (see
	// SaveAPIKey), but asserting the exact bits back only makes sense where
	// the OS actually models them.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("credentials file perm = %o, want 0600 (it holds a secret)", perm)
		}
	}

	key, source := LoadAPIKey()
	if key != "test-key-123" {
		t.Errorf("LoadAPIKey key = %q, want test-key-123", key)
	}
	if source != "file:"+path {
		t.Errorf("LoadAPIKey source = %q, want file:%s", source, path)
	}
}

func TestLoadAPIKey_EnvVarsTakePriorityOverFile(t *testing.T) {
	isolateConfigDir(t)
	if _, err := SaveAPIKey("from-file"); err != nil {
		t.Fatal(err)
	}

	t.Setenv(EnvPostmanKey, "from-generic-env")
	if key, source := LoadAPIKey(); key != "from-generic-env" || source != "env:"+EnvPostmanKey {
		t.Errorf("LoadAPIKey = (%q, %q), want (from-generic-env, env:%s)", key, source, EnvPostmanKey)
	}

	t.Setenv(EnvAPIDocPostmanKey, "from-apidoc-env")
	if key, source := LoadAPIKey(); key != "from-apidoc-env" || source != "env:"+EnvAPIDocPostmanKey {
		t.Errorf("LoadAPIKey = (%q, %q), want (from-apidoc-env, env:%s)", key, source, EnvAPIDocPostmanKey)
	}
}

func TestSaveAPIKey_RejectsEmpty(t *testing.T) {
	isolateConfigDir(t)
	if _, err := SaveAPIKey(""); err == nil {
		t.Fatal("expected error saving an empty API key")
	}
}

func TestClearAPIKey(t *testing.T) {
	isolateConfigDir(t)
	if _, err := SaveAPIKey("k"); err != nil {
		t.Fatal(err)
	}
	if err := ClearAPIKey(); err != nil {
		t.Fatalf("ClearAPIKey: %v", err)
	}
	if key, _ := LoadAPIKey(); key != "" {
		t.Errorf("expected no key after ClearAPIKey, got %q", key)
	}
	// Clearing again (file already gone) must not error.
	if err := ClearAPIKey(); err != nil {
		t.Errorf("ClearAPIKey on missing file: %v", err)
	}
}

func TestCollectionUIDCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if uid := LoadCachedUID(dir, "My API"); uid != "" {
		t.Fatalf("expected empty cache initially, got %q", uid)
	}
	if err := SaveCachedUID(dir, "My API", "uid-123"); err != nil {
		t.Fatalf("SaveCachedUID: %v", err)
	}
	if uid := LoadCachedUID(dir, "My API"); uid != "uid-123" {
		t.Errorf("LoadCachedUID = %q, want uid-123", uid)
	}

	// A second title in the same cache file must not collide with the first.
	if err := SaveCachedUID(dir, "Other API", "uid-456"); err != nil {
		t.Fatalf("SaveCachedUID: %v", err)
	}
	if uid := LoadCachedUID(dir, "My API"); uid != "uid-123" {
		t.Errorf("LoadCachedUID(My API) = %q, want uid-123 unaffected by the second save", uid)
	}

	if err := ClearCachedUID(dir, "My API"); err != nil {
		t.Fatalf("ClearCachedUID: %v", err)
	}
	if uid := LoadCachedUID(dir, "My API"); uid != "" {
		t.Errorf("expected empty after ClearCachedUID, got %q", uid)
	}
	if uid := LoadCachedUID(dir, "Other API"); uid != "uid-456" {
		t.Errorf("ClearCachedUID(My API) must not remove Other API's entry, got %q", uid)
	}
}

func TestSaveCachedUID_RequiresTitleAndUID(t *testing.T) {
	dir := t.TempDir()
	if err := SaveCachedUID(dir, "", "uid"); err == nil {
		t.Error("expected error with empty title")
	}
	if err := SaveCachedUID(dir, "title", ""); err == nil {
		t.Error("expected error with empty uid")
	}
}
