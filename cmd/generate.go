package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/devenock/specyl/internal/annotations"
	"github.com/devenock/specyl/internal/prompt"
	"github.com/devenock/specyl/pkg/analyzer"
	"github.com/devenock/specyl/pkg/config"
	"github.com/devenock/specyl/pkg/generator"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var generateCmd = &cobra.Command{
	Use:   "generate [path]",
	Short: "Generate API documentation",
	Long:  `Scan a codebase and generate API documentation in the format of your choice. Use --no-interactive (or set --type) for CI/scripts.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runGenerate,
	Example: `  specyl generate
  specyl generate .
  specyl generate --type swagger -o ./docs
  specyl generate --no-interactive --type postman --title "My API"`,
	SilenceUsage: true,
}

func init() {
	generateCmd.Flags().StringP("output", "o", "./docs", "output directory for generated documentation")
	generateCmd.Flags().StringP("type", "t", "", "documentation type (swagger|postman)")
	generateCmd.Flags().StringP("framework", "f", "", "backend framework (gin|echo|fiber|gorilla|chi)")
	generateCmd.Flags().Bool("interactive", true, "use interactive mode when type is not set")
	generateCmd.Flags().BoolP("no-interactive", "y", false, "disable interactive mode (use config/flags only; good for CI)")
	generateCmd.Flags().StringSlice("exclude", []string{}, "directories to exclude from scanning")
	generateCmd.Flags().StringSlice("tags", []string{}, "filter endpoints by tag: plain name to include, !name to exclude (comma-separated)")
	generateCmd.Flags().String("base-path", "", "base path for API endpoints")
	generateCmd.Flags().String("title", "", "API title (default: project name from go.mod)")
	generateCmd.Flags().String("version", "1.0.0", "API version")
	generateCmd.Flags().String("description", "", "API description")
	generateCmd.Flags().Bool("dry-run", false, "analyze and show what would be generated without writing files")
	generateCmd.Flags().Bool("show-config", false, "print effective config (file + env + flags) and exit")
	generateCmd.Flags().Bool("serve", true, "after generating (swagger only), serve docs locally and open them in your browser; pass --serve=false to skip")
	generateCmd.Flags().Bool("write-annotations", false, "write swag-style comment blocks above handler functions (same-file handlers only)")
	generateCmd.Flags().Bool("skip-build-check", false, "skip the 'go vet ./...' pre-flight check against the target project")
	generateCmd.Flags().Bool("required-by-default", false, "mark every struct field required unless it has json:\",omitempty\" (default: only binding/validate:\"required\" tags count)")

	_ = viper.BindPFlag("output", generateCmd.Flags().Lookup("output"))
	_ = viper.BindPFlag("type", generateCmd.Flags().Lookup("type"))
	_ = viper.BindPFlag("framework", generateCmd.Flags().Lookup("framework"))
	_ = viper.BindPFlag("interactive", generateCmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("no-interactive", generateCmd.Flags().Lookup("no-interactive"))
	_ = viper.BindPFlag("exclude", generateCmd.Flags().Lookup("exclude"))
	_ = viper.BindPFlag("tags", generateCmd.Flags().Lookup("tags"))
	_ = viper.BindPFlag("base-path", generateCmd.Flags().Lookup("base-path"))
	_ = viper.BindPFlag("title", generateCmd.Flags().Lookup("title"))
	_ = viper.BindPFlag("version", generateCmd.Flags().Lookup("version"))
	_ = viper.BindPFlag("description", generateCmd.Flags().Lookup("description"))
	_ = viper.BindPFlag("dry-run", generateCmd.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("show-config", generateCmd.Flags().Lookup("show-config"))
	_ = viper.BindPFlag("serve", generateCmd.Flags().Lookup("serve"))
	_ = viper.BindPFlag("write-annotations", generateCmd.Flags().Lookup("write-annotations"))
	_ = viper.BindPFlag("skip-build-check", generateCmd.Flags().Lookup("skip-build-check"))
	_ = viper.BindPFlag("required-by-default", generateCmd.Flags().Lookup("required-by-default"))

	rootCmd.AddCommand(generateCmd)
}

func runGenerate(cmd *cobra.Command, args []string) error {
	// Determine project path
	projectPath := "."
	if len(args) > 0 {
		projectPath = args[0]
	}

	cfg := &config.Config{
		ProjectPath:       projectPath,
		Output:            viper.GetString("output"),
		DocType:           viper.GetString("type"),
		Framework:         viper.GetString("framework"),
		Exclude:           viper.GetStringSlice("exclude"),
		Tags:              viper.GetStringSlice("tags"),
		BasePath:          viper.GetString("base-path"),
		Title:             viper.GetString("title"),
		Version:           viper.GetString("version"),
		Description:       viper.GetString("description"),
		Servers:           []config.ServerConfig{},
		Verbose:           viper.GetBool("verbose"),
		Quiet:             viper.GetBool("quiet"),
		WriteAnnotations:  viper.GetBool("write-annotations"),
		SkipBuildCheck:    viper.GetBool("skip-build-check"),
		RequiredByDefault: viper.GetBool("required-by-default"),
		OutputFromFlag:    cmd.Flags().Changed("output"),
	}
	// Load servers from config file (viper unmarshals .specyl.yaml "servers" key)
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
	if useInteractive && !isInteractiveTerminal(os.Stdin) {
		return &exitCodeError{
			errors.New("no --type given and stdin is not an interactive terminal — pass --type (swagger|postman) and --no-interactive, e.g. in CI/scripts (see -h)"),
			ExitUsageError,
		}
	}
	if useInteractive {
		if cfgFile == "" && viper.ConfigFileUsed() == "" {
			// Config file not found; suggest init (only in interactive)
			if !quiet {
				fmt.Fprintln(os.Stderr, "Tip: run 'specyl init' to create .specyl.yaml with defaults.")
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
		n, err := annotations.WriteSwagAnnotations(cfg.ProjectPath, apiSpec.Endpoints, cfg.BasePath, apiSpec.TypePackageName)
		if err != nil && !quiet {
			fmt.Fprintf(os.Stderr, "Warning: write-annotations: %v\n", err)
		} else if !quiet && n > 0 {
			fmt.Printf("   Wrote swag annotations to %d handler(s).\n", n)
		}
	}

	if cfg.DocType == "swagger" && !quiet {
		if viper.GetBool("serve") {
			return runServeDocs(cmd.Context(), cfg.Output, quiet, apiAnalyzer.DetectedPort())
		}
		fmt.Printf("   Open %s in your browser to view it.\n", filepath.Join(cfg.Output, "index.html"))
	}

	// Postman: tell the user where the file is and how to import it.
	if cfg.DocType == "postman" {
		printPostmanInstructions(cfg.Output, quiet)
	}

	return nil
}

func listenForPreview(preferredPort string) (net.Listener, string, error) {
	const fallbackPort = "8765"
	tried := []string{fallbackPort}
	if preferredPort != "" && preferredPort != fallbackPort {
		tried = []string{preferredPort, fallbackPort}
	}
	var lastErr error
	for _, p := range tried {
		ln, err := net.Listen("tcp", "127.0.0.1:"+p)
		if err == nil {
			return ln, p, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

func runServeDocs(ctx context.Context, outputDir string, quiet bool, preferredPort string) error {
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return &exitCodeError{fmt.Errorf("failed to resolve output path: %w", err), ExitRuntimeError}
	}
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		return &exitCodeError{fmt.Errorf("output directory does not exist: %s", absDir), ExitRuntimeError}
	}

	ln, port, err := listenForPreview(preferredPort)
	if err != nil {
		return &exitCodeError{fmt.Errorf("failed to start preview server: %w", err), ExitRuntimeError}
	}
	browserURL := "http://localhost:" + port + "/index.html"

	if !quiet {
		fmt.Println()
		fmt.Printf("🌐 Swagger UI: %s\n", browserURL)
		if preferredPort != "" && port != preferredPort {
			fmt.Printf("   (port %s is in use - probably the app itself is running - using %s instead)\n", preferredPort, port)
		}
		fmt.Println()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser(browserURL)
	}()

	var lastActivity atomic.Int64
	fileServer := http.FileServer(http.Dir(absDir))
	trackedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastActivity.Store(time.Now().UnixNano())
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Handler:           trackedHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	const (
		idleGracePeriod = 2 * time.Second
		neverLoadedCap  = 10 * time.Second
	)
	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(neverLoadedCap)
	for {
		select {
		case <-ctx.Done():
			shutdown()
			return nil
		case err := <-serveErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return &exitCodeError{fmt.Errorf("serve failed: %w", err), ExitRuntimeError}
			}
			return nil
		case <-ticker.C:
			last := lastActivity.Load()
			if last == 0 {
				if time.Now().After(deadline) {
					shutdown()
					return nil
				}
				continue
			}
			if time.Since(time.Unix(0, last)) >= idleGracePeriod {
				shutdown()
				return nil
			}
		}
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
	_ = cmd.Start()
}

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
}
