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

	if err := viper.ReadInConfig(); err == nil && viper.GetBool("verbose") {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
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
