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
	"github.com/devenock/api-doc-gen/pkg/postman"
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
generates API documentation as Swagger/OpenAPI or a Postman Collection.`,
		Version: "1.0.0",
		Example: `  api-doc-gen init
  api-doc-gen generate
  api-doc-gen generate --type swagger --output ./docs
  api-doc-gen generate ./my-api --no-interactive --type postman`,
		// Don't print the usage/help screen for runtime/validation errors.
		// We print errors ourselves in Execute(); Cobra would otherwise print
		// the same error twice plus a wall of usage text.
		SilenceUsage:  true,
		SilenceErrors: true,
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
		SilenceUsage: true,
	}

	initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration file",
		Long:  `Create a configuration file (.apidoc-gen.yaml) in the current directory.`,
		RunE:  runInit,
		Example: `  api-doc-gen init`,
		SilenceUsage: true,
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
	generateCmd.Flags().StringP("type", "t", "", "documentation type (swagger|postman)")
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

	// Postman upload flags (only honored when --type=postman)
	generateCmd.Flags().Bool("upload", false, "(postman) force upload to Postman; error out if no API key is available (good for CI)")
	generateCmd.Flags().Bool("no-upload", false, "(postman) skip the auto-upload step even if a Postman API key is available")
	generateCmd.Flags().Bool("direct-import", false, "(postman) import directly into the Postman desktop app — no API key or account needed")
	generateCmd.Flags().String("postman-api-key", "", "(postman) API key for the upload step; takes precedence over env and credentials file")
	generateCmd.Flags().String("postman-workspace", "", "(postman) workspace UID to upload to (default: your default workspace)")

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
	viper.BindPFlag("upload", generateCmd.Flags().Lookup("upload"))
	viper.BindPFlag("no-upload", generateCmd.Flags().Lookup("no-upload"))
	viper.BindPFlag("direct-import", generateCmd.Flags().Lookup("direct-import"))
	viper.BindPFlag("postman-api-key", generateCmd.Flags().Lookup("postman-api-key"))
	viper.BindPFlag("postman-workspace", generateCmd.Flags().Lookup("postman-workspace"))

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
		ProjectPath:         projectPath,
		Output:              viper.GetString("output"),
		DocType:             viper.GetString("type"),
		Framework:           viper.GetString("framework"),
		Exclude:             viper.GetStringSlice("exclude"),
		BasePath:            viper.GetString("base-path"),
		Title:               viper.GetString("title"),
		Version:             viper.GetString("version"),
		Description:         viper.GetString("description"),
		Servers:             []config.ServerConfig{},
		Verbose:             viper.GetBool("verbose"),
		Quiet:               viper.GetBool("quiet"),
		PostmanAPIKey:       viper.GetString("postman-api-key"),
		PostmanWorkspaceUID: viper.GetString("postman-workspace"),
		PostmanUpload:       viper.GetBool("upload"),
		PostmanNoUpload:     viper.GetBool("no-upload"),
		PostmanDirectImport: viper.GetBool("direct-import"),
		WriteAnnotations:    viper.GetBool("write-annotations"),
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

	// Postman: auto-upload (interactive prompt for API key if needed). Only runs
	// for --type=postman; respects --no-upload; in non-interactive mode without
	// a key it silently skips unless --upload was passed (then errors).
	if cfg.DocType == "postman" {
		useInteractiveUpload := viper.GetBool("interactive") && !viper.GetBool("no-interactive")
		if err := runPostmanUpload(cfg, useInteractiveUpload, quiet); err != nil {
			return err
		}
	}

	// --write-annotations: write swag comments above handler functions.
	// Enabled by the flag, the config file, or the interactive prompt answer.
	if viper.GetBool("write-annotations") || cfg.WriteAnnotations {
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

// runPostmanUpload uploads the generated collection.json to Postman, with
// interactive prompting for an API key if none is configured. It returns nil
// (and may print a tip) when no key is available in non-interactive mode and
// --upload was not passed; in that case the user just gets the local file.
func runPostmanUpload(cfg *config.Config, interactive, quiet bool) error {
	if cfg.PostmanNoUpload {
		return nil
	}

	collectionPath := filepath.Join(cfg.Output, "collection.json")
	if _, err := os.Stat(collectionPath); err != nil {
		// Generation should have produced this file; if not, nothing to upload.
		return nil
	}

	// Direct-import path: open Postman desktop and import the local file
	// without touching the Postman cloud API. No API key required.
	if cfg.PostmanDirectImport && postman.IsDesktopInstalled() {
		if !quiet {
			fmt.Println()
			fmt.Println("🚀 Importing collection into Postman desktop...")
			fmt.Println("   (Postman will open and import your collection — this may take a moment)")
		}
		if err := postman.ImportToDesktop(collectionPath); err != nil {
			if !quiet {
				fmt.Fprintf(os.Stderr, "   ↳ Automatic import failed: %v\n", err)
				fmt.Printf("   Open Postman and import this file manually: %s\n", collectionPath)
			}
		} else if !quiet {
			fmt.Println("   ✅ Collection imported into Postman!")
		}
		return nil
	}

	apiKey, source := cfg.PostmanAPIKey, "flag:--postman-api-key"
	if apiKey == "" {
		apiKey, source = postman.LoadAPIKey()
	}

	if apiKey == "" {
		if interactive {
			key, err := prompt.PromptPostmanAPIKey()
			if err != nil {
				if !quiet {
					fmt.Fprintf(os.Stderr, "   ↳ skipping Postman upload (no API key entered): %v\n", err)
				}
				return nil
			}
			path, serr := postman.SaveAPIKey(key)
			if serr != nil {
				return &exitCodeError{fmt.Errorf("save Postman credentials: %w", serr), ExitRuntimeError}
			}
			if !quiet {
				fmt.Printf("   🔐 Saved Postman API key to %s (mode 0600)\n", path)
			}
			apiKey, source = key, "file:"+path
		} else if cfg.PostmanUpload {
			return &exitCodeError{
				errors.New("--upload was set but no Postman API key was found (set --postman-api-key, APIDOC_POSTMAN_API_KEY, or POSTMAN_API_KEY)"),
				ExitUsageError,
			}
		} else {
			// No API key and non-interactive (and --upload not forced).
			// If the Postman desktop app is installed, import the collection
			// directly — no account or API key required.
			if postman.IsDesktopInstalled() {
				if !quiet {
					fmt.Println()
					fmt.Println("🚀 Importing collection into Postman desktop...")
					fmt.Println("   (Postman will open and import your collection — this may take a moment)")
				}
				if err := postman.ImportToDesktop(collectionPath); err != nil {
					if !quiet {
						fmt.Fprintf(os.Stderr, "   ↳ Automatic import failed: %v\n", err)
						fmt.Printf("   Open Postman and import this file manually: %s\n", collectionPath)
					}
				} else if !quiet {
					fmt.Println("   ✅ Collection imported into Postman!")
				}
			} else {
				if !quiet {
					fmt.Println()
					fmt.Printf("📦 Collection saved: %s\n", collectionPath)
					fmt.Println("   Postman is not installed.")
					fmt.Println("   • Download: https://www.postman.com/downloads/")
					fmt.Println("   • Web:      https://web.postman.co → Import → Upload File")
				}
			}
			return nil
		}
	}

	collectionJSON, err := os.ReadFile(collectionPath)
	if err != nil {
		return &exitCodeError{fmt.Errorf("read collection.json: %w", err), ExitRuntimeError}
	}

	client := postman.NewClient(apiKey)

	if cfg.Verbose && !quiet {
		fmt.Printf("   🔑 Using Postman API key from %s\n", source)
	}

	cachedUID := postman.LoadCachedUID(cfg.ProjectPath, cfg.Title)
	if !quiet {
		if cachedUID != "" {
			fmt.Printf("☁️  Updating existing Postman collection (uid=%s)...\n", cachedUID)
		} else {
			fmt.Println("☁️  Uploading new Postman collection...")
		}
	}

	var resp *postman.CollectionResponse
	if cachedUID != "" {
		resp, err = client.UpdateCollection(cachedUID, collectionJSON)
		if err != nil {
			// Cached UID may be stale (collection deleted upstream). Fall back to create.
			if !quiet {
				fmt.Fprintf(os.Stderr, "   ↳ update failed (%v); creating a new collection instead\n", err)
			}
			resp, err = client.CreateCollection(collectionJSON, cfg.PostmanWorkspaceUID)
		}
	} else {
		resp, err = client.CreateCollection(collectionJSON, cfg.PostmanWorkspaceUID)
	}
	if err != nil {
		return &exitCodeError{fmt.Errorf("postman upload failed: %w", err), ExitRuntimeError}
	}

	if err := postman.SaveCachedUID(cfg.ProjectPath, cfg.Title, resp.Collection.UID); err != nil && !quiet {
		fmt.Fprintf(os.Stderr, "   ↳ warning: failed to cache collection UID: %v\n", err)
	}

	if !quiet {
		fmt.Println()
		fmt.Println("📮 View in Postman:")
		fmt.Printf("   • %s\n", postman.WebURL(resp.Collection.UID))
		fmt.Printf("   • Collection: %s (uid=%s)\n", resp.Collection.Name, resp.Collection.UID)
	}

	// Open Postman desktop when available (interactive only — skip in CI).
	if interactive && postman.IsDesktopInstalled() {
		if !quiet {
			fmt.Println("   🚀 Opening Postman...")
		}
		if err := postman.OpenDesktop(resp.Collection.UID); err != nil && !quiet {
			fmt.Fprintf(os.Stderr, "   ↳ could not open Postman: %v\n", err)
		}
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

# Documentation type: swagger or postman
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
	if cfg.DocType == "postman" {
		fmt.Printf("postman.upload: %v\n", cfg.PostmanUpload)
		fmt.Printf("postman.no_upload: %v\n", cfg.PostmanNoUpload)
		fmt.Printf("postman.workspace: %q\n", cfg.PostmanWorkspaceUID)
		_, source := postman.LoadAPIKey()
		if cfg.PostmanAPIKey != "" {
			fmt.Println("postman.api_key: <set via --postman-api-key>")
		} else if source != "" {
			fmt.Printf("postman.api_key: <set via %s>\n", source)
		} else {
			fmt.Println("postman.api_key: <not set>")
		}
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
