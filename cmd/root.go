package cmd

import (
	"fmt"
	"os"

	"github.com/devenock/api-doc-gen/internal/prompt"
	"github.com/devenock/api-doc-gen/pkg/analyzer"
	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/generator"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "apidoc-gen",
		Short: "Automatic API documentation generator",
		Long: `apidoc-gen is a CLI tool that scans your codebase and automatically
generates API documentation in various formats including Swagger, Postman,
or a custom Docusaurus-based website.`,
		Version: "1.0.0",
	}

	generateCmd = &cobra.Command{
		Use:   "generate [path]",
		Short: "Generate API documentation",
		Long:  `Scan a codebase and generate API documentation in the format of your choice.`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  runGenerate,
	}

	initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration file",
		Long:  `Create a configuration file (.apidoc-gen.yaml) in the current directory.`,
		RunE:  runInit,
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .apidoc-gen.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")

	// Generate command flags
	generateCmd.Flags().StringP("output", "o", "./docs", "output directory for generated documentation")
	generateCmd.Flags().StringP("type", "t", "", "documentation type (swagger|postman|custom)")
	generateCmd.Flags().StringP("framework", "f", "", "backend framework (gin|echo|fiber|gorilla|chi)")
	generateCmd.Flags().Bool("interactive", true, "use interactive mode")
	generateCmd.Flags().StringSlice("exclude", []string{}, "directories to exclude from scanning")
	generateCmd.Flags().String("base-path", "", "base path for API endpoints")
	generateCmd.Flags().String("title", "API Documentation", "API title")
	generateCmd.Flags().String("version", "1.0.0", "API version")
	generateCmd.Flags().String("description", "", "API description")

	// Bind flags to viper
	viper.BindPFlag("output", generateCmd.Flags().Lookup("output"))
	viper.BindPFlag("type", generateCmd.Flags().Lookup("type"))
	viper.BindPFlag("framework", generateCmd.Flags().Lookup("framework"))
	viper.BindPFlag("interactive", generateCmd.Flags().Lookup("interactive"))
	viper.BindPFlag("exclude", generateCmd.Flags().Lookup("exclude"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("base-path", generateCmd.Flags().Lookup("base-path"))
	viper.BindPFlag("title", generateCmd.Flags().Lookup("title"))
	viper.BindPFlag("version", generateCmd.Flags().Lookup("version"))
	viper.BindPFlag("description", generateCmd.Flags().Lookup("description"))

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
		// Verbose:     viper.GetBool("verbose"),
	}

	// Interactive mode
	if viper.GetBool("interactive") && cfg.DocType == "" {
		if err := prompt.GetUserPreferences(cfg); err != nil {
			return fmt.Errorf("failed to get user preferences: %w", err)
		}
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// if cfg.Verbose {
	// 	fmt.Println("🔍 Starting API documentation generation...")
	// 	fmt.Printf("   Project Path: %s\n", cfg.ProjectPath)
	// 	fmt.Printf("   Output: %s\n", cfg.Output)
	// 	fmt.Printf("   Type: %s\n", cfg.DocType)
	// 	fmt.Printf("   Framework: %s\n", cfg.Framework)
	// }

	// Analyze codebase
	fmt.Println("📊 Analyzing codebase...")
	apiAnalyzer := analyzer.NewAnalyzer(cfg)
	apiSpec, err := apiAnalyzer.Analyze()
	if err != nil {
		return fmt.Errorf("failed to analyze codebase: %w", err)
	}

	// if cfg.Verbose {
	// 	fmt.Printf("   Found %d endpoints\n", len(apiSpec.Endpoints))
	// }

	// Generate documentation
	fmt.Printf("📝 Generating %s documentation...\n", cfg.DocType)
	gen, err := generator.NewGenerator(cfg.DocType, cfg)
	if err != nil {
		return fmt.Errorf("failed to create generator: %w", err)
	}

	if err := gen.Generate(apiSpec); err != nil {
		return fmt.Errorf("failed to generate documentation: %w", err)
	}

	fmt.Printf("✅ Documentation generated successfully at: %s\n", cfg.Output)
	return nil
}

func runInit(cmd *cobra.Command, args []string) error {
	configPath := ".apidoc-gen.yaml"

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Configuration file already exists at %s\n", configPath)
		return nil
	}

	defaultConfig := `# API Documentation Generator Configuration
# Generated by apidoc-gen

# Output directory for generated documentation
output: ./docs

# Documentation type: swagger, postman, or custom
type: swagger

# Backend framework (optional): gin, echo, fiber, gorilla, chi
# Leave empty for auto-detection
framework: ""

# Directories to exclude from scanning
exclude:
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
		return fmt.Errorf("failed to create config file: %w", err)
	}

	fmt.Printf("✅ Configuration file created: %s\n", configPath)
	fmt.Println("You can now customize the configuration and run 'apidoc-gen generate'")
	return nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
