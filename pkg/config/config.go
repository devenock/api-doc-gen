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
	// PostmanDirectImport records that the user chose "import directly" (no
	// API key) in the interactive wizard, or passed --direct-import. It is
	// currently NOT read anywhere in the upload flow — runPostmanUpload
	// (cmd/root.go) only branches on PostmanUpload — so today it has no
	// effect beyond skipping the cloud-upload API-key prompt inside the
	// wizard itself; the actual outcome (open Postman desktop if installed,
	// print manual-import instructions) is identical to the default path.
	// True one-step local-file import (e.g. via a temporary localhost server
	// + a postman:// import-by-URL request) is not implemented.
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
	modPath := filepath.Join(projectPath, "go.mod")
	// Don't follow a symlinked go.mod — a crafted project could point it at
	// an arbitrary file elsewhere on disk (see the matching check in
	// pkg/analyzer for the same reasoning applied to the directory walk).
	if info, err := os.Lstat(modPath); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "API Documentation"
	}
	data, err := os.ReadFile(modPath)
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
		// Matched by exact basename (see analyzer.go), so this must be ".git"
		// — a bare "git" never matches a real directory and silently excludes
		// nothing.
		c.Exclude = []string{"vendor", "node_modules", ".git", "test", "tests"}
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
