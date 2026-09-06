// Package config defines the Config struct that carries a generate run's
// settings end to end (flags, config file, and env vars are all merged into
// it by cmd/generate.go before Validate is called) and validates it.
package config

import (
	"errors"
	"fmt"
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

	// OutputFromFlag is true when Output was set via an explicit --output/-o
	// flag on this invocation, as opposed to a config file, env var, or the
	// default. Output resolves relative to the current working directory (not
	// ProjectPath — see the README's cross-directory example, which passes an
	// absolute -o for exactly this reason), and Validate only trusts an
	// Output that escapes the working directory tree when a human typed it on
	// the command line for this run. A malicious .apidoc-gen.yaml committed
	// to a repo (auth_middleware and friends are meant to be shared/trusted,
	// but output is a filesystem write target) must not be able to silently
	// redirect where generate writes files.
	OutputFromFlag bool

	// SkipBuildCheck disables the `go vet ./...` pre-flight check that runs
	// against the target project by default. The check only warns (it never
	// blocks generation) — this flag exists for skipping it entirely, e.g.
	// in CI against a branch mid-refactor, or environments without a Go
	// toolchain where the check would otherwise just no-op anyway.
	SkipBuildCheck bool

	// AuthMiddleware, when set (via .apidoc-gen.yaml's auth_middleware key),
	// is the exact list of middleware identifier names (case-insensitive)
	// that mark a route group as authenticated, overriding the analyzer's
	// built-in heuristic (a common-name set plus a substring match on
	// "auth"/"jwt") entirely. For projects whose middleware names the
	// heuristic gets wrong in either direction — a false positive like
	// "AuthorMiddleware", or a false negative like "requireSession".
	AuthMiddleware []string

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

// readProjectFile reads a single well-known file (here, just go.mod) directly
// under projectPath, refusing to follow it if it's a symlink — a crafted
// project could otherwise point it at an arbitrary file elsewhere on disk.
// Uses os.Root (Go 1.24+) rather than a plain Lstat-then-ReadFile so the
// symlink check and the read are confined to projectPath's tree as a single
// operation instead of two separate steps a race could fall between (see the
// matching helper and its longer rationale in pkg/analyzer).
func readProjectFile(projectPath, name string) ([]byte, error) {
	root, err := os.OpenRoot(projectPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrNotExist
	}
	return root.ReadFile(name)
}

// detectProjectName reads go.mod and returns a human-readable project name
// derived from the module path's last segment. Falls back to "API Documentation".
func detectProjectName(projectPath string) string {
	data, err := readProjectFile(projectPath, "go.mod")
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

	if !c.OutputFromFlag {
		if err := checkOutputWithinWorkingDir(c.Output); err != nil {
			return err
		}
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

// checkOutputWithinWorkingDir rejects an Output path that resolves outside
// the current working directory's tree. Only called when Output did not come
// from an explicit --output/-o flag (see OutputFromFlag) — i.e. it came from
// a config file, env var, or the "./docs" default, none of which a user
// necessarily typed or reviewed for this specific run.
func checkOutputWithinWorkingDir(output string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output path %q: %w", output, err)
	}
	rel, err := filepath.Rel(cwd, absOutput)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf(
			"output directory %q resolves outside the current directory (%s) — "+
				"this came from a config file, env var, or the default, not something typed on the command line. "+
				"Pass --output explicitly to confirm this is intentional",
			output, cwd,
		)
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
