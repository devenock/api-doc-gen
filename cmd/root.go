package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenock/api-doc-gen/internal/annotations"
	"github.com/devenock/api-doc-gen/internal/prompt"
	"github.com/devenock/api-doc-gen/pkg/analyzer"
	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/generator"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Exit codes for scripting (0 = success, 1 = usage/validation, 2 = runtime error).
const (
	ExitSuccess       = 0
	ExitUsageError    = 1
	ExitRuntimeError  = 2
)

// exitCodeError allows RunE to specify exit code.
type exitCodeError struct {
	err  error
	code int
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "apidoc-gen",
		Short: "Automatic API documentation generator",
		Long: `apidoc-gen is a CLI tool that scans your codebase and automatically
generates API documentation in various formats including Swagger, Postman,
or a custom Docusaurus-based website.`,
		Version: "1.0.0",
		Example: `  api-doc-gen init
  api-doc-gen generate
  api-doc-gen generate --type swagger --output ./docs
  api-doc-gen generate ./my-api --no-interactive --type postman`,
	}

	generateCmd = &cobra.Command{
		Use:   "generate [path]",
		Short: "Generate API documentation",
		Long:  `Scan a codebase and generate API documentation in the format of your choice. Use --no-interactive (or set --type) for CI/scripts.`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  runGenerate,
		Example: `  api-doc-gen generate
  api-doc-gen generate .
  api-doc-gen generate --type swagger -o ./docs
  api-doc-gen generate --no-interactive --type postman --title "My API"`,
	}

	initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration file",
		Long:  `Create a configuration file (.apidoc-gen.yaml) in the current directory.`,
		RunE:  runInit,
		Example: `  api-doc-gen init`,
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .apidoc-gen.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "suppress progress output (errors still printed to stderr)")

	// Generate command flags
	generateCmd.Flags().StringP("output", "o", "./docs", "output directory for generated documentation")
	generateCmd.Flags().StringP("type", "t", "", "documentation type (swagger|postman|custom)")
	generateCmd.Flags().StringP("framework", "f", "", "backend framework (gin|echo|fiber|gorilla|chi)")
	generateCmd.Flags().Bool("interactive", true, "use interactive mode when type is not set")
	generateCmd.Flags().BoolP("no-interactive", "y", false, "disable interactive mode (use config/flags only; good for CI)")
	generateCmd.Flags().StringSlice("exclude", []string{}, "directories to exclude from scanning")
	generateCmd.Flags().String("base-path", "", "base path for API endpoints")
	generateCmd.Flags().String("title", "API Documentation", "API title")
	generateCmd.Flags().String("version", "1.0.0", "API version")
	generateCmd.Flags().String("description", "", "API description")
	generateCmd.Flags().Bool("dry-run", false, "analyze and show what would be generated without writing files")
	generateCmd.Flags().Bool("show-config", false, "print effective config (file + env + flags) and exit")
	generateCmd.Flags().Bool("serve", false, "after generating (swagger only), serve docs and print the access URL")
	generateCmd.Flags().Bool("write-annotations", false, "write swag-style comment blocks above handler functions (same-file handlers only)")

	// Bind flags to viper
	viper.BindPFlag("output", generateCmd.Flags().Lookup("output"))
	viper.BindPFlag("type", generateCmd.Flags().Lookup("type"))
	viper.BindPFlag("framework", generateCmd.Flags().Lookup("framework"))
	viper.BindPFlag("interactive", generateCmd.Flags().Lookup("interactive"))
	viper.BindPFlag("no-interactive", generateCmd.Flags().Lookup("no-interactive"))
	viper.BindPFlag("exclude", generateCmd.Flags().Lookup("exclude"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("quiet", rootCmd.PersistentFlags().Lookup("quiet"))
	viper.BindPFlag("base-path", generateCmd.Flags().Lookup("base-path"))
	viper.BindPFlag("title", generateCmd.Flags().Lookup("title"))
	viper.BindPFlag("version", generateCmd.Flags().Lookup("version"))
	viper.BindPFlag("description", generateCmd.Flags().Lookup("description"))
	viper.BindPFlag("dry-run", generateCmd.Flags().Lookup("dry-run"))
	viper.BindPFlag("show-config", generateCmd.Flags().Lookup("show-config"))
	viper.BindPFlag("serve", generateCmd.Flags().Lookup("serve"))
	viper.BindPFlag("write-annotations", generateCmd.Flags().Lookup("write-annotations"))

	// Add commands
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(initCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".apidoc-gen")
	}

	viper.SetEnvPrefix("APIDOC")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil && viper.GetBool("verbose") {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func runGenerate(cmd *cobra.Command, args []string) error {
	// Determine project path
	projectPath := "."
	if len(args) > 0 {
		projectPath = args[0]
	}

	cfg := &config.Config{
		ProjectPath: projectPath,
		Output:      viper.GetString("output"),
		DocType:     viper.GetString("type"),
		Framework:   viper.GetString("framework"),
		Exclude:     viper.GetStringSlice("exclude"),
		BasePath:    viper.GetString("base-path"),
		Title:       viper.GetString("title"),
		Version:     viper.GetString("version"),
		Description: viper.GetString("description"),
		Servers:     []config.ServerConfig{},
		Verbose:     viper.GetBool("verbose"),
		Quiet:       viper.GetBool("quiet"),
	}
	// Load servers from config file (viper unmarshals .apidoc-gen.yaml "servers" key)
	_ = viper.UnmarshalKey("servers", &cfg.Servers)

	// --show-config: print effective config and exit (before interactive so no prompt)
	if viper.GetBool("show-config") {
		printShowConfig(cfg)
		return nil
	}

	// Interactive mode: skip if --no-interactive/-y or if --type is already set
	quiet := viper.GetBool("quiet")
	useInteractive := viper.GetBool("interactive") && !viper.GetBool("no-interactive") && cfg.DocType == ""
	if useInteractive {
		if cfgFile == "" && viper.ConfigFileUsed() == "" {
			// Config file not found; suggest init (only in interactive)
			if !quiet {
				fmt.Fprintln(os.Stderr, "Tip: run 'api-doc-gen init' to create .apidoc-gen.yaml with defaults.")
			}
		}
		if err := prompt.GetUserPreferences(cfg); err != nil {
			return &exitCodeError{fmt.Errorf("failed to get user preferences: %w", err), ExitUsageError}
		}
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return &exitCodeError{fmt.Errorf("invalid configuration: %w", err), ExitUsageError}
	}

	// --dry-run: analyze and report, do not write files
	if viper.GetBool("dry-run") {
		return runDryRun(cfg, quiet)
	}

	if !quiet {
		if cfg.Verbose {
			fmt.Println("🔍 Starting API documentation generation...")
			fmt.Printf("   Project Path: %s\n", cfg.ProjectPath)
			fmt.Printf("   Output: %s\n", cfg.Output)
			fmt.Printf("   Type: %s\n", cfg.DocType)
			fmt.Printf("   Framework: %s\n", cfg.Framework)
		}

		// Analyze codebase
		fmt.Println("📊 Analyzing codebase...")
	}
	apiAnalyzer := analyzer.NewAnalyzer(cfg)
	apiSpec, err := apiAnalyzer.Analyze()
	if err != nil {
		return &exitCodeError{fmt.Errorf("failed to analyze codebase: %w", err), ExitRuntimeError}
	}

	if !quiet {
		if cfg.Verbose {
			fmt.Printf("   Found %d endpoints\n", len(apiSpec.Endpoints))
		}

		// Generate documentation
		fmt.Printf("📝 Generating %s documentation...\n", cfg.DocType)
	}
	gen, err := generator.NewGenerator(cfg.DocType, cfg)
	if err != nil {
		return &exitCodeError{fmt.Errorf("failed to create generator: %w", err), ExitRuntimeError}
	}

	if err := gen.Generate(apiSpec); err != nil {
		return &exitCodeError{fmt.Errorf("failed to generate documentation: %w", err), ExitRuntimeError}
	}

	if !quiet {
		fmt.Printf("✅ Documentation generated successfully at: %s\n", cfg.Output)
		printDocsAccessURL(cfg)
	}

	// --write-annotations: write swag comments above handler functions
	if viper.GetBool("write-annotations") {
		n, err := annotations.WriteSwagAnnotations(apiSpec.Endpoints, cfg.BasePath)
		if err != nil && !quiet {
			fmt.Fprintf(os.Stderr, "Warning: write-annotations: %v\n", err)
		} else if !quiet && n > 0 {
			fmt.Printf("   Wrote swag annotations to %d handler(s).\n", n)
		}
	}

	// --serve: start local static server and print URL (swagger only)
	if viper.GetBool("serve") && cfg.DocType == "swagger" {
		return runServeDocs(cfg.Output, quiet)
	}
	return nil
}

// printDocsAccessURL prints how to open the generated docs (file URL and optional server URL).
func printDocsAccessURL(cfg *config.Config) {
	absOut, err := filepath.Abs(cfg.Output)
	if err != nil {
		absOut = cfg.Output
	}
	switch cfg.DocType {
	case "swagger":
		indexPath := filepath.Join(absOut, "index.html")
		fmt.Println()
		fmt.Println("📖 View Swagger UI:")
		fmt.Printf("   • File:  file://%s\n", filepath.ToSlash(indexPath))
		serverURL := "http://localhost:8080"
		if len(cfg.Servers) > 0 && cfg.Servers[0].URL != "" {
			serverURL = strings.TrimSuffix(cfg.Servers[0].URL, "/")
		}
		fmt.Printf("   • If your API serves the %q directory at /docs: %s/docs\n", cfg.Output, serverURL)
		fmt.Println("   • Or run: api-doc-gen generate --serve  (starts a local preview server)")
	default:
		fmt.Printf("   Output directory: %s\n", absOut)
	}
}

// runServeDocs serves the output directory and prints the access URL; blocks until interrupted.
func runServeDocs(outputDir string, quiet bool) error {
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return &exitCodeError{fmt.Errorf("failed to resolve output path: %w", err), ExitRuntimeError}
	}
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		return &exitCodeError{fmt.Errorf("output directory does not exist: %s", absDir), ExitRuntimeError}
	}
	port := "8765"
	addr := ":" + port
	if !quiet {
		fmt.Println()
		fmt.Printf("🌐 Serving docs at http://localhost%s\n", addr)
		fmt.Printf("   Open in browser: http://localhost%s/index.html\n", addr)
		fmt.Println("   Press Ctrl+C to stop")
		fmt.Println()
	}
	handler := http.FileServer(http.Dir(absDir))
	err = http.ListenAndServe(addr, handler)
	if err != nil && (err == http.ErrServerClosed || strings.Contains(err.Error(), "closed")) {
		return nil
	}
	if err != nil {
		return &exitCodeError{fmt.Errorf("serve failed: %w", err), ExitRuntimeError}
	}
	return nil
}

func runInit(cmd *cobra.Command, args []string) error {
	configPath := ".apidoc-gen.yaml"

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Configuration file already exists at %s\n", configPath)
		return nil
	}

	projectPath := "."
	if len(args) > 0 {
		projectPath = args[0]
	}
	detectedFramework := analyzer.DetectFramework(projectPath)
	frameworkLine := `framework: ""`
	if detectedFramework != "" {
		frameworkLine = fmt.Sprintf(`framework: %q`, detectedFramework)
	}

	defaultConfig := fmt.Sprintf(`# API Documentation Generator Configuration
# Generated by api-doc-gen

# Output directory for generated documentation
output: ./docs

# Documentation type: swagger, postman, or custom
type: swagger

# Backend framework (optional): gin, echo, fiber, gorilla, chi
# Set by init from go.mod; leave empty for auto-detection
%s

# Directories to exclude from scanning
`, frameworkLine) + `exclude:
  - vendor
  - node_modules
  - .git
  - test
  - tests

# API base path (e.g., /api/v1)
base_path: ""

# API metadata
title: "API Documentation"
version: "1.0.0"
description: "Auto-generated API documentation"

# Server configuration
servers:
  - url: "http://localhost:8080"
    description: "Development server"

# Verbose output
verbose: false
`

	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		return &exitCodeError{fmt.Errorf("failed to create config file: %w", err), ExitRuntimeError}
	}

	fmt.Printf("✅ Configuration file created: %s\n", configPath)
	if detectedFramework != "" {
		fmt.Printf("   Detected framework: %s (from go.mod)\n", detectedFramework)
	}
	fmt.Println("You can now customize the configuration and run 'api-doc-gen generate'")
	return nil
}

// printShowConfig prints the effective configuration (for --show-config).
func printShowConfig(cfg *config.Config) {
	fmt.Printf("project_path: %q\n", cfg.ProjectPath)
	fmt.Printf("output: %q\n", cfg.Output)
	fmt.Printf("type: %q\n", cfg.DocType)
	fmt.Printf("framework: %q\n", cfg.Framework)
	fmt.Printf("base_path: %q\n", cfg.BasePath)
	fmt.Printf("title: %q\n", cfg.Title)
	fmt.Printf("version: %q\n", cfg.Version)
	fmt.Printf("description: %q\n", cfg.Description)
	fmt.Printf("exclude: %v\n", cfg.Exclude)
	fmt.Printf("verbose: %v\n", cfg.Verbose)
	fmt.Printf("quiet: %v\n", cfg.Quiet)
	fmt.Printf("servers: %d\n", len(cfg.Servers))
	for i, s := range cfg.Servers {
		fmt.Printf("  [%d] url=%q description=%q\n", i, s.URL, s.Description)
	}
}

// runDryRun runs analysis and prints what would be generated without writing.
func runDryRun(cfg *config.Config, quiet bool) error {
	apiAnalyzer := analyzer.NewAnalyzer(cfg)
	apiSpec, err := apiAnalyzer.Analyze()
	if err != nil {
		return &exitCodeError{fmt.Errorf("failed to analyze codebase: %w", err), ExitRuntimeError}
	}
	if !quiet {
		fmt.Println("dry-run: would generate the following")
		fmt.Printf("  output: %s\n", cfg.Output)
		fmt.Printf("  type: %s\n", cfg.DocType)
		fmt.Printf("  endpoints: %d\n", len(apiSpec.Endpoints))
		for _, ep := range apiSpec.Endpoints {
			fmt.Printf("    %s %s\n", ep.Method, ep.Path)
		}
	}
	return nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code := ExitUsageError
		var exitErr *exitCodeError
		if errors.As(err, &exitErr) {
			code = exitErr.code
		}
		os.Exit(code)
	}
}
