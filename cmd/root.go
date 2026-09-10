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

const (
	ExitSuccess      = 0
	ExitUsageError   = 1
	ExitRuntimeError = 2
)

type exitCodeError struct {
	err  error
	code int
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

var version = "dev"

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
		Use:   "specyl",
		Short: "Automatic API documentation generator",
		Long: `Specyl is a CLI tool that scans your codebase and automatically
generates API documentation as Swagger/OpenAPI or a Postman Collection.`,
		Version: resolvedVersion(),
		Example: `  specyl init
  specyl generate
  specyl generate --type swagger --output ./docs
  specyl generate ./my-api --no-interactive --type postman`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .specyl.yaml)")
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
		viper.SetConfigName(".specyl")
	}

	viper.SetEnvPrefix("SPECYL")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "⚠️  could not read config file: %v — continuing with flags/env/defaults only\n", err)
		}
	} else if viper.GetBool("verbose") {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

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
