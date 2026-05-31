package prompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/postman"
	"github.com/manifoldco/promptui"
)

// PromptPostmanAPIKey asks the user for a Postman API key with masked input.
// It only validates that the key is non-empty and looks plausibly long enough;
// real validation happens via a postman.Client.Me() call by the caller.
func PromptPostmanAPIKey() (string, error) {
	fmt.Println()
	fmt.Println("📮 Postman login")
	fmt.Println("   Generate an API key at: https://postman.co/settings/me/api-keys")
	fmt.Println("   (it is saved locally with 0600 permissions; not sent anywhere except api.getpostman.com)")
	p := promptui.Prompt{
		Label: "Paste your Postman API key",
		Mask:  '*',
		Validate: func(s string) error {
			if len(strings.TrimSpace(s)) < 10 {
				return errors.New("that does not look like a Postman API key (too short)")
			}
			return nil
		},
	}
	key, err := p.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(key), nil
}

// setupPostmanInWizard handles Postman desktop detection and API key collection
// as part of the interactive wizard. The user knows what will happen before
// generation starts: auto-upload + open, or local file only.
func setupPostmanInWizard() {
	fmt.Println()
	if postman.IsDesktopInstalled() {
		key, source := postman.LoadAPIKey()
		if key != "" {
			fmt.Printf("   ✅ Postman desktop detected — already logged in (%s)\n", source)
			fmt.Println("   Your collection will be uploaded and Postman will open automatically.")
			return
		}
		// Desktop found but no key yet — prompt now so the user doesn't get
		// interrupted again after generation finishes.
		fmt.Println("   📮 Postman desktop detected. Enter your API key to enable auto-upload.")
		apiKey, err := PromptPostmanAPIKey()
		if err != nil {
			// User cancelled or hit Ctrl-C — continue without a key.
			fmt.Println("   ↳ No key entered. collection.json will be generated for manual import.")
			return
		}
		path, err := postman.SaveAPIKey(apiKey)
		if err != nil {
			fmt.Printf("   ↳ Could not save API key (%v). You will be prompted again after generation.\n", err)
			return
		}
		fmt.Printf("   🔐 API key saved to %s\n", path)
		fmt.Println("   Your collection will be uploaded and Postman will open automatically.")
	} else {
		fmt.Println("   📦 Postman desktop not installed.")
		fmt.Println("   A collection.json will be generated — import it when ready:")
		fmt.Println("      • Desktop: https://www.postman.com/downloads/")
		fmt.Println("      • Web:     https://web.postman.co → Import → Upload File")
	}
	fmt.Println()
}

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
			setupPostmanInWizard()
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

	// For swagger output, offer to write swag annotations directly to handler files.
	if cfg.DocType == "swagger" {
		annotationsPrompt := promptui.Select{
			Label: "Write swag annotations to handler files? (adds // @Summary etc. above each handler)",
			Items: []string{"Yes — write annotations", "No — docs only"},
		}
		_, annotationsChoice, err := annotationsPrompt.Run()
		if err == nil {
			cfg.WriteAnnotations = annotationsChoice == "Yes — write annotations"
		}
	}

	fmt.Println("\nConfiguration complete!")

	return nil
}
