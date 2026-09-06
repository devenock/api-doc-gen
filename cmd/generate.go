package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/devenock/api-doc-gen/internal/annotations"
	"github.com/devenock/api-doc-gen/internal/prompt"
	"github.com/devenock/api-doc-gen/pkg/analyzer"
	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/generator"
	"github.com/devenock/api-doc-gen/pkg/postman"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var generateCmd = &cobra.Command{
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

func init() {
	generateCmd.Flags().StringP("output", "o", "./docs", "output directory for generated documentation")
	generateCmd.Flags().StringP("type", "t", "", "documentation type (swagger|postman)")
	generateCmd.Flags().StringP("framework", "f", "", "backend framework (gin|echo|fiber|gorilla|chi)")
	generateCmd.Flags().Bool("interactive", true, "use interactive mode when type is not set")
	generateCmd.Flags().BoolP("no-interactive", "y", false, "disable interactive mode (use config/flags only; good for CI)")
	generateCmd.Flags().StringSlice("exclude", []string{}, "directories to exclude from scanning")
	generateCmd.Flags().String("base-path", "", "base path for API endpoints")
	generateCmd.Flags().String("title", "", "API title (default: project name from go.mod)")
	generateCmd.Flags().String("version", "1.0.0", "API version")
	generateCmd.Flags().String("description", "", "API description")
	generateCmd.Flags().Bool("dry-run", false, "analyze and show what would be generated without writing files")
	generateCmd.Flags().Bool("show-config", false, "print effective config (file + env + flags) and exit")
	generateCmd.Flags().Bool("serve", false, "after generating (swagger only), serve docs and print the access URL")
	generateCmd.Flags().Bool("write-annotations", false, "write swag-style comment blocks above handler functions (same-file handlers only)")
	generateCmd.Flags().Bool("skip-build-check", false, "skip the `go vet ./...` pre-flight check against the target project")

	// Postman upload flags (only honored when --type=postman)
	generateCmd.Flags().Bool("upload", false, "(postman) force upload to Postman; error out if no API key is available (good for CI)")
	generateCmd.Flags().Bool("no-upload", false, "(postman) skip the auto-upload step even if a Postman API key is available")
	generateCmd.Flags().Bool("direct-import", false, "(postman) import directly into the Postman desktop app — no API key or account needed")
	generateCmd.Flags().String("postman-api-key", "", "(postman) API key for the upload step; takes precedence over env and credentials file")
	generateCmd.Flags().String("postman-workspace", "", "(postman) workspace UID to upload to (default: your default workspace)")

	// Bind flags to viper. Errors are discarded: they can only occur if the
	// flag name doesn't exist on the FlagSet, which would mean a typo above —
	// a programmer error that go vet/tests would catch, not a runtime
	// condition worth surfacing to the user.
	_ = viper.BindPFlag("output", generateCmd.Flags().Lookup("output"))
	_ = viper.BindPFlag("type", generateCmd.Flags().Lookup("type"))
	_ = viper.BindPFlag("framework", generateCmd.Flags().Lookup("framework"))
	_ = viper.BindPFlag("interactive", generateCmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("no-interactive", generateCmd.Flags().Lookup("no-interactive"))
	_ = viper.BindPFlag("exclude", generateCmd.Flags().Lookup("exclude"))
	_ = viper.BindPFlag("base-path", generateCmd.Flags().Lookup("base-path"))
	_ = viper.BindPFlag("title", generateCmd.Flags().Lookup("title"))
	_ = viper.BindPFlag("version", generateCmd.Flags().Lookup("version"))
	_ = viper.BindPFlag("description", generateCmd.Flags().Lookup("description"))
	_ = viper.BindPFlag("dry-run", generateCmd.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("show-config", generateCmd.Flags().Lookup("show-config"))
	_ = viper.BindPFlag("serve", generateCmd.Flags().Lookup("serve"))
	_ = viper.BindPFlag("write-annotations", generateCmd.Flags().Lookup("write-annotations"))
	_ = viper.BindPFlag("skip-build-check", generateCmd.Flags().Lookup("skip-build-check"))
	_ = viper.BindPFlag("upload", generateCmd.Flags().Lookup("upload"))
	_ = viper.BindPFlag("no-upload", generateCmd.Flags().Lookup("no-upload"))
	_ = viper.BindPFlag("direct-import", generateCmd.Flags().Lookup("direct-import"))
	_ = viper.BindPFlag("postman-api-key", generateCmd.Flags().Lookup("postman-api-key"))
	_ = viper.BindPFlag("postman-workspace", generateCmd.Flags().Lookup("postman-workspace"))

	rootCmd.AddCommand(generateCmd)
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
		SkipBuildCheck:      viper.GetBool("skip-build-check"),
		OutputFromFlag:      cmd.Flags().Changed("output"),
	}
	// Load servers from config file (viper unmarshals .apidoc-gen.yaml "servers" key)
	_ = viper.UnmarshalKey("servers", &cfg.Servers)
	_ = viper.UnmarshalKey("auth_middleware", &cfg.AuthMiddleware)

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

	// Pre-flight: does the target project actually build? A project with
	// build errors can still be partially analyzed (the AST parser doesn't
	// need type-correct code), but the result may be incomplete or
	// misleading, so warn — never block; see runBuildCheck.
	if !cfg.SkipBuildCheck {
		runBuildCheck(cfg, quiet)
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

	// A zero-endpoint result is always a degenerate, actionable situation —
	// warn regardless of --verbose so it isn't mistaken for a normal success
	// (the generator will otherwise happily write a well-formed but empty
	// collection/spec and the run will still print "generated successfully").
	if len(apiSpec.Endpoints) == 0 && !quiet {
		fmt.Fprintln(os.Stderr, "⚠️  No endpoints found — the generated file will be empty.")
		fmt.Fprintf(os.Stderr, "   Detected framework: %s\n", apiAnalyzer.Framework())
		fmt.Fprintln(os.Stderr, "   Common causes:")
		fmt.Fprintln(os.Stderr, "   - the path passed to 'generate' isn't the directory containing go.mod")
		fmt.Fprintln(os.Stderr, "   - the framework wasn't auto-detected — pass --framework explicitly (gin|echo|fiber|gorilla|chi)")
		fmt.Fprintln(os.Stderr, "   - routes are registered through a pattern this tool doesn't recognize yet")
		fmt.Fprintln(os.Stderr, "   Run with -v for more detail, or --dry-run to inspect without writing files.")
	}

	if !quiet {
		if cfg.Verbose {
			fmt.Printf("   Detected framework: %s\n", apiAnalyzer.Framework())
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
	}

	// --write-annotations: write swag comments above handler functions.
	if viper.GetBool("write-annotations") || cfg.WriteAnnotations {
		n, err := annotations.WriteSwagAnnotations(apiSpec.Endpoints, cfg.BasePath)
		if err != nil && !quiet {
			fmt.Fprintf(os.Stderr, "Warning: write-annotations: %v\n", err)
		} else if !quiet && n > 0 {
			fmt.Printf("   Wrote swag annotations to %d handler(s).\n", n)
		}
	}

	// Swagger: start a local server and open the browser automatically.
	// In --quiet mode (CI/scripts) skip the server and browser open.
	if cfg.DocType == "swagger" && !quiet {
		return runServeDocs(cmd.Context(), cfg.Output, quiet)
	}

	// Postman: import into desktop (or prompt/upload via cloud API).
	if cfg.DocType == "postman" {
		useInteractiveUpload := viper.GetBool("interactive") && !viper.GetBool("no-interactive")
		if err := runPostmanUpload(cmd.Context(), cfg, useInteractiveUpload, quiet); err != nil {
			return err
		}
	}

	return nil
}

// runServeDocs serves the output directory on a local port, opens the browser
// automatically, and blocks until ctx is canceled (Ctrl+C).
func runServeDocs(ctx context.Context, outputDir string, quiet bool) error {
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return &exitCodeError{fmt.Errorf("failed to resolve output path: %w", err), ExitRuntimeError}
	}
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		return &exitCodeError{fmt.Errorf("output directory does not exist: %s", absDir), ExitRuntimeError}
	}

	port := "8765"
	browserURL := "http://localhost:" + port + "/index.html"

	if !quiet {
		fmt.Println()
		fmt.Printf("🌐 Starting Swagger UI at %s\n", browserURL)
		fmt.Println("   Press Ctrl+C to stop")
		fmt.Println()
	}

	// Open the browser after a short delay so the server is ready to accept connections.
	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser(browserURL)
	}()

	srv := &http.Server{Addr: ":" + port, Handler: http.FileServer(http.Dir(absDir))}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		// Execute() installed the signal handler that canceled ctx; give the
		// server a moment to close its listener/connections cleanly.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return &exitCodeError{fmt.Errorf("serve failed: %w", err), ExitRuntimeError}
		}
		return nil
	}
}

// openBrowser opens url in the system default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		return
	}
	cmd.Start() //nolint:errcheck
}

// runBuildCheck runs analyzer.CheckBuild against cfg.ProjectPath and prints
// a warning if the project doesn't build. It never returns an error: the
// check is advisory only (see the SkipBuildCheck doc comment) — a project
// that fails to build can still be worth generating docs from, so the tool
// warns and continues rather than blocking.
func runBuildCheck(cfg *config.Config, quiet bool) {
	result := analyzer.CheckBuild(cfg.ProjectPath)
	if result.Skipped || result.OK || quiet {
		return
	}
	if result.Err != nil {
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "⚠️  Build check did not complete: %v\n", result.Err)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "⚠️  This project does not currently pass `go vet ./...` — generated docs may be incomplete or incorrect.")
	if cfg.Verbose {
		fmt.Fprintln(os.Stderr, result.Output)
	} else {
		fmt.Fprintln(os.Stderr, "   Run with -v for details, or pass --skip-build-check to suppress this check.")
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
