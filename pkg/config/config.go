package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Config represent application configuration
type Config struct {
	ProjectPath string
	Output      string
	DocType     string
	Framework   string
	Exclude     []string
	BasePath    string
	Title       string
	Version     string
	Description string
	Servers     []ServerConfig
	Verbose     bool
	Quiet       bool

	// WriteAnnotations writes swag-style comment blocks above handler functions.
	WriteAnnotations bool

	// Postman upload settings (only honored when DocType == "postman").
	// PostmanAPIKey is resolved at runtime from --postman-api-key, env, or the
	// credentials file; do not persist it to .apidoc-gen.yaml (it is a secret).
	PostmanAPIKey       string
	PostmanWorkspaceUID string
	PostmanUpload       bool // --upload: force upload, error if no API key available
	PostmanNoUpload     bool // --no-upload: skip the upload step entirely
	// PostmanDirectImport skips the cloud API entirely and imports the collection
	// directly into the Postman desktop app via a temporary localhost server.
	// Set by the interactive wizard when the user picks "Import directly".
	PostmanDirectImport bool
}

// ServerConfig represent a server configuration
type ServerConfig struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

// detectProjectName reads go.mod and returns a human-readable project name
// derived from the module path's last segment. Falls back to "API Documentation".
func detectProjectName(projectPath string) string {
	data, err := os.ReadFile(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return "API Documentation"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		mod := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		if idx := strings.LastIndex(mod, "/"); idx >= 0 {
			mod = mod[idx+1:]
		}
		mod = strings.ReplaceAll(mod, "-", " ")
		mod = strings.ReplaceAll(mod, "_", " ")
		words := strings.Fields(mod)
		for i, w := range words {
			if len(w) > 0 {
				words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
			}
		}
		if result := strings.Join(words, " "); result != "" {
			return result
		}
	}
	return "API Documentation"
}

// Validate checks if the configuration is valid and returns clear, actionable errors.
func (c *Config) Validate() error {
	// check if project path exists
	if _, err := os.Stat(c.ProjectPath); os.IsNotExist(err) {
		return errors.New("project path does not exist: " + c.ProjectPath + " (check the path or run from the project root)")
	}

	// validate documentation type
	validTypes := map[string]bool{
		"swagger": true,
		"postman": true,
	}

	if !validTypes[c.DocType] {
		return errors.New("invalid documentation type \"" + c.DocType + "\": use swagger or postman (set --type or run with interactive mode)")
	}

	// set defaults
	if c.Output == "" {
		c.Output = "./docs"
	}

	if c.Title == "" {
		c.Title = detectProjectName(c.ProjectPath)
	}

	if c.Version == "" {
		c.Version = "1.0.0"
	}

	if len(c.Exclude) == 0 {
		c.Exclude = []string{"vendor", "node_modules", "git", "test", "tests"}
	}
	return nil
}

// ShouldExclude checks if a path should be excluded
func (c *Config) ShouldExclude(path string) bool {
	for _, exclude := range c.Exclude {
		if path == exclude {
			return true
		}
	}
	return false
}
