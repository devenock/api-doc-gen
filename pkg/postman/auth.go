package postman

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Environment variables checked (in order) before falling back to the credentials file.
const (
	EnvAPIDocPostmanKey = "APIDOC_POSTMAN_API_KEY"
	EnvPostmanKey       = "POSTMAN_API_KEY"
)

// CacheFileName is written into the project directory next to .apidoc-gen.yaml.
// It maps collection title -> Postman collection UID so subsequent runs update
// instead of creating duplicates. Safe to delete; safe to gitignore.
const CacheFileName = ".apidoc-gen-cache.json"

// Credentials is the on-disk shape of the credentials file.
type Credentials struct {
	APIKey string `json:"api_key"`
}

// CredentialsPath returns the absolute path to the credentials file
// (typically ~/.config/apidoc-gen/credentials.json on Unix-like systems).
func CredentialsPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback for environments where UserConfigDir() fails (very rare).
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("locate config dir: %w", err)
		}
		cfgDir = filepath.Join(home, ".config")
	}
	return filepath.Join(cfgDir, "apidoc-gen", "credentials.json"), nil
}

// LoadAPIKey returns the Postman API key and a short label describing where it
// came from (for verbose output). The lookup order is:
//
//  1. APIDOC_POSTMAN_API_KEY
//  2. POSTMAN_API_KEY
//  3. Credentials file (CredentialsPath)
//
// Both return values are empty when no key is configured.
func LoadAPIKey() (key, source string) {
	if v := os.Getenv(EnvAPIDocPostmanKey); v != "" {
		return v, "env:" + EnvAPIDocPostmanKey
	}
	if v := os.Getenv(EnvPostmanKey); v != "" {
		return v, "env:" + EnvPostmanKey
	}
	path, err := CredentialsPath()
	if err != nil {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil || creds.APIKey == "" {
		return "", ""
	}
	return creds.APIKey, "file:" + path
}

// SaveAPIKey writes the API key to the credentials file with 0600 perms,
// creating the parent directory with 0700 if necessary. Returns the path
// written so callers can show the user where the key is stored.
func SaveAPIKey(key string) (string, error) {
	if key == "" {
		return "", errors.New("api key is empty")
	}
	path, err := CredentialsPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create credentials dir: %w", err)
	}
	data, err := json.MarshalIndent(Credentials{APIKey: key}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write credentials: %w", err)
	}
	return path, nil
}

// ClearAPIKey removes the credentials file. Missing-file is not an error.
func ClearAPIKey() error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CollectionCache stores collection UIDs keyed by collection title so we can
// PUT (update) on subsequent runs instead of POSTing duplicates.
type CollectionCache struct {
	Collections map[string]string `json:"collections"`
}

func cachePath(projectPath string) string {
	if projectPath == "" {
		projectPath = "."
	}
	return filepath.Join(projectPath, CacheFileName)
}

// LoadCachedUID returns the cached Postman UID for (projectPath, title), or "".
func LoadCachedUID(projectPath, title string) string {
	data, err := os.ReadFile(cachePath(projectPath))
	if err != nil {
		return ""
	}
	var c CollectionCache
	if err := json.Unmarshal(data, &c); err != nil || c.Collections == nil {
		return ""
	}
	return c.Collections[title]
}

// SaveCachedUID upserts a (title -> uid) mapping into the project's cache file.
func SaveCachedUID(projectPath, title, uid string) error {
	if title == "" || uid == "" {
		return errors.New("title and uid required")
	}
	path := cachePath(projectPath)
	var c CollectionCache
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	if c.Collections == nil {
		c.Collections = map[string]string{}
	}
	c.Collections[title] = uid
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// ClearCachedUID removes a single (title) entry from the project's cache file.
func ClearCachedUID(projectPath, title string) error {
	path := cachePath(projectPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var c CollectionCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	if c.Collections == nil {
		return nil
	}
	delete(c.Collections, title)
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}
