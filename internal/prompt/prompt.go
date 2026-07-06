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

// setupPostmanInWizard handles Postman desktop detection and lets the user
// choose how their collection should be delivered. It updates cfg in place so
// runPostmanUpload knows which path to take after generation.
func setupPostmanInWizard(cfg *config.Config) {
	fmt.Println()

	if !postman.IsDesktopInstalled() {
		fmt.Println("   📦 Postman desktop not found on this machine.")
		fmt.Println("   A collection.json will be saved — import it when ready:")
		fmt.Println("      • Desktop: https://www.postman.com/downloads/")
		fmt.Println("      • Web:     https://web.postman.co → Import → Upload File")
		fmt.Println()
		return
	}

	fmt.Println("   📮 Postman detected on your machine.")

	const (
		optDirect = "Import directly into Postman (no account or API key needed)"
		optCloud  = "Sync to Postman cloud (requires API key — enables team sharing & repeat updates)"
		optFile   = "Save to file only — I'll import manually"
	)

	modePrompt := promptui.Select{
		Label: "How would you like to import your collection?",
		Items: []string{optDirect, optCloud, optFile},
	}
	_, choice, err := modePrompt.Run()
	if err != nil {
		// Ctrl-C or error — default to file only.
		fmt.Println("   ↳ Saving collection to file.")
		fmt.Println()
		return
	}

	switch choice {
	case optDirect:
		cfg.PostmanDirectImport = true
		fmt.Println("   ✅ Postman will open and import your collection automatically after generation.")

	case optCloud:
		key, source := postman.LoadAPIKey()
		if key != "" {
			fmt.Printf("   ✅ Already logged in (%s) — collection will be uploaded and Postman will open.\n", source)
			break
		}
		apiKey, err := PromptPostmanAPIKey()
		if err != nil {
			// User skipped key entry — fall back to direct import.
			cfg.PostmanDirectImport = true
			fmt.Println("   ↳ No key entered. Falling back to direct import into Postman desktop.")
			break
		}
		path, err := postman.SaveAPIKey(apiKey)
		if err != nil {
			cfg.PostmanDirectImport = true
			fmt.Printf("   ↳ Could not save API key (%v). Falling back to direct import.\n", err)
			break
		}
		fmt.Printf("   🔐 API key saved to %s\n", path)
		fmt.Println("   Your collection will be uploaded and Postman will open automatically.")

	case optFile:
		cfg.PostmanNoUpload = true
		fmt.Println("   ↳ collection.json will be saved for manual import.")
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
			Items: []string{"Swagger/OpenAPI", "Postman Collection"},
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
			setupPostmanInWizard(cfg)
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
