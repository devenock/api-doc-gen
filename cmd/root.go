// Package cmd implements the api-doc-gen CLI (Cobra commands, flag/config
// wiring, and the run logic for each subcommand). See generate.go, init.go,
// and postman.go for the subcommand implementations.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// Exit codes for scripting (0 = success, 1 = usage/validation, 2 = runtime error).
const (
	ExitSuccess      = 0
	ExitUsageError   = 1
	ExitRuntimeError = 2
)

// exitCodeError allows RunE to specify exit code.
type exitCodeError struct {
	err  error
	code int
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

// version is set at release-build time via:
//
//	go build -ldflags "-X github.com/devenock/api-doc-gen/cmd.version=vX.Y.Z"
//
// (the Makefile and Dockerfile do this from `git describe`). Left as "dev"
// for plain `go build`/`go run`.
var version = "dev"

// resolvedVersion returns the ldflags-injected version, or — when that
// wasn't set (e.g. the binary came from `go install .../api-doc-gen@vX.Y.Z`)
// — the module version Go recorded in the binary at install time.
func resolvedVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "api-doc-gen",
		Short: "Automatic API documentation generator",
		Long: `api-doc-gen is a CLI tool that scans your codebase and automatically
generates API documentation as Swagger/OpenAPI or a Postman Collection.`,
		Version: resolvedVersion(),
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
)

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .apidoc-gen.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "suppress progress output (errors still printed to stderr)")

	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("quiet", rootCmd.PersistentFlags().Lookup("quiet"))
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

	if err := viper.ReadInConfig(); err != nil {
		// A missing config file is the common case (most projects don't have
		// one) and expected to be silent. Anything else - malformed YAML, a
		// permission error, or an explicit --config path that doesn't exist -
		// means the user's settings were silently dropped in favor of
		// flags/env/defaults, which is exactly the kind of ambiguity this
		// tool is supposed to surface rather than hide (see the README's
		// "Why" section), so it's reported regardless of --verbose.
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "⚠️  could not read config file: %v — continuing with flags/env/defaults only\n", err)
		}
	} else if viper.GetBool("verbose") {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

// isInteractiveTerminal reports whether f is an actual interactive terminal.
// promptui doesn't hang when stdin isn't a terminal (it errors out on
// immediate EOF), but without this check the wizard still attempts to draw
// and then fails with a raw, cryptic error - and in the process writes
// terminal escape sequences into whatever stdin/stdout actually are, which
// is exactly the kind of thing that corrupts a captured CI log. Callers use
// this to skip straight to the same clear, actionable error the
// --no-interactive path already produces.
//
// A plain os.ModeCharDevice check is not enough here: /dev/null - one of the
// most common ways scripts redirect stdin to signal "no input available",
// and exactly the case this guards against - is itself a character device,
// so it would pass a bare mode check as "interactive". term.IsTerminal does
// the real ioctl-based check (TIOCGETA-equivalent) that /dev/null fails.
func isInteractiveTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code := ExitUsageError
		var exitErr *exitCodeError
		if errors.As(err, &exitErr) {
			code = exitErr.code
		}
		os.Exit(code)
	}
}
