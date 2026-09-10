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
	ProjectPath       string
	Output            string
	DocType           string
	Framework         string
	Exclude           []string
	BasePath          string
	Title             string
	Version           string
	Description       string
	Servers           []ServerConfig
	Verbose           bool
	Quiet             bool
	WriteAnnotations  bool
	OutputFromFlag    bool
	SkipBuildCheck    bool
	RequiredByDefault bool
	AuthMiddleware    []string
	Tags              []string
}

// ServerConfig represent a server configuration
type ServerConfig struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

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
		c.Exclude = []string{"vendor", "node_modules", ".git", "test", "tests"}
	}
	return nil
}

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
