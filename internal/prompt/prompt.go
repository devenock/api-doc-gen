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

	// API title (default from config file / flags)
	titlePrompt := promptui.Prompt{
		Label:   "API Title",
		Default: cfg.Title,
	}
	title, err := titlePrompt.Run()
	if err != nil {
		return err
	}
	if title != "" {
		cfg.Title = title
	}

	// API Version (default from config file / flags)
	versionPrompt := promptui.Prompt{
		Label:   "API Version",
		Default: cfg.Version,
	}
	version, err := versionPrompt.Run()
	if err != nil {
		return err
	}
	if version != "" {
		cfg.Version = version
	}

	// Base Path (default from config file / flags)
	basePathPrompt := promptui.Prompt{
		Label:   "API Base Path (e.g., /api/v1)",
		Default: cfg.BasePath,
	}
	basePath, err := basePathPrompt.Run()
	if err != nil {
		return err
	}
	cfg.BasePath = basePath

	// Output directory (default from config file / flags)
	outputPrompt := promptui.Prompt{
		Label:   "Output Directory",
		Default: cfg.Output,
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
