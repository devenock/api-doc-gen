package prompt

import (
	"fmt"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/manifoldco/promptui"
)

// GetUserPreferences prompts the user for their preferences
func GetUserPreferences(cfg *config.Config) error {
	fmt.Println("\nWelcome to API Documentation Generator!")

	// Documentation Type selection
	if cfg.DocType == "" {
		docTypePrompt := promptui.Select{
			Label: "Select Documentation type",
			Items: []string{"Swagger/OpenAPI", "Postman Collection", "Custom Docusaurus Site"},
		}

		_, docType, err := docTypePrompt.Run()
		if err != nil {
			return err
		}

		switch docType {
		case "Swagger/OpenAPI":
			cfg.DocType = "swagger"
		case "Postman Collection":
			cfg.DocType = "postman"
		case "Custom Docusaurus Site":
			cfg.DocType = "custom"
		}
	}

	// Framework selection
	if cfg.Framework == "" {
		frameworkPrompt := promptui.Select{
			Label: "Select your backend framework (or Auto-detect)",
			Items: []string{"Auto-detect", "Gin", "Echo", "Fiber", "Gorilla Mux", "Chi"},
		}

		_, framework, err := frameworkPrompt.Run()
		if err != nil {
			return err
		}

		switch framework {
		case "Gin":
			cfg.Framework = "gin"
		case "Fiber":
			cfg.Framework = "fiber"
		case "Chi":
			cfg.Framework = "chi"
		case "Echo":
			cfg.Framework = "echo"
		case "Gorilla Mux":
			cfg.Framework = "gorilla"
		default:
			cfg.Framework = ""
		}
	}

	// API title
	if cfg.Title == "API Documentation" {
		titlePrompt := promptui.Prompt{
			Label:   "API Title",
			Default: "My API",
		}

		title, err := titlePrompt.Run()
		if err != nil {
			return err
		}

		if title != "" {
			cfg.Title = title
		}
	}

	// API Version
	versionPrompt := promptui.Prompt{
		Label:   "API Version",
		Default: "1.0.0",
	}

	version, err := versionPrompt.Run()
	if err != nil {
		return err
	}

	if version != "" {
		cfg.Version = version
	}

	// Base Path
	basePathPrompt := promptui.Prompt{
		Label:   "API Base Path (e.g., /api/v1)",
		Default: "",
	}

	basePath, err := basePathPrompt.Run()
	if err != nil {
		return err
	}
	cfg.BasePath = basePath

	// Output directory
	outputPrompt := promptui.Prompt{
		Label:   "Output Directory",
		Default: "./docs",
	}

	output, err := outputPrompt.Run()
	if err != nil {
		return err
	}
	if output != "" {
		cfg.Output = output
	}

	fmt.Println("\nConfiguration complete!")

	return nil
}
