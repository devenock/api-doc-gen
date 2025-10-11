package config

import (
	"errors"
	"os"
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
	Verbose     string
}

// ServerConfig represent a server cpnfiguration
type ServerConfig struct {
	URL         string
	Description string
}

// validate check if the configuration is valid
func (c *Config) Validate() error {
	// check if project path exists
	if _, err := os.Stat(c.ProjectPath); os.IsNotExist(err) {
		return errors.New("project path does not exist")
	}

	// validate documentation type
	validTypes := map[string]bool{
		"swagger": true,
		"postman": true,
		"custom":  true,
	}

	if !validTypes[c.DocType] {
		return errors.New("invalid documentation type. Must be: swagger, postman or custom")
	}

	// set defaults
	if c.Output == "" {
		c.Output = "./docs"
	}

	if c.Title == "" {
		c.Title = "API Documentation"
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
