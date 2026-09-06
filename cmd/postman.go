package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/devenock/api-doc-gen/internal/prompt"
	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/postman"
)

// runPostmanUpload generates the Postman collection file, tells the user
// where it is, and opens Postman so it is ready to receive the import.
// For automated upload via the Postman cloud API, pass --upload.
func runPostmanUpload(ctx context.Context, cfg *config.Config, interactive, quiet bool) error {
	collectionPath := filepath.Join(cfg.Output, "collection.json")
	if _, err := os.Stat(collectionPath); err != nil {
		return nil
	}

	// --upload: push to Postman cloud (requires API key).
	if cfg.PostmanUpload {
		return runPostmanAPIUpload(ctx, cfg, collectionPath, interactive, quiet)
	}

	// Default: show where the file is and how to import it.
	if !quiet {
		absPath, _ := filepath.Abs(collectionPath)
		fmt.Println()
		fmt.Printf("\U0001f4e6 Postman collection ready: %s\n", absPath)
		fmt.Println()
		fmt.Println("   To import into Postman:")
		fmt.Println("   1. Open Postman")
		fmt.Println("   2. Click Import (top-left of the sidebar)")
		fmt.Println("   3. Choose Upload Files and select the file above")
		fmt.Println()
		fmt.Println("   Or drag the file directly into the Postman sidebar.")
	}

	// Open Postman so it is ready for the import (skip in CI/quiet mode).
	if postman.IsDesktopInstalled() && !quiet {
		postman.OpenDesktop("") //nolint:errcheck
	}

	return nil
}

// runPostmanAPIUpload uploads the collection to Postman cloud and opens the
// desktop app to the resulting collection. Only called when --upload is set.
func runPostmanAPIUpload(ctx context.Context, cfg *config.Config, collectionPath string, interactive, quiet bool) error {
	apiKey, source := cfg.PostmanAPIKey, "flag:--postman-api-key"
	if apiKey == "" {
		apiKey, source = postman.LoadAPIKey()
	}
	if apiKey == "" {
		if interactive {
			if !quiet {
				fmt.Println()
				fmt.Println("Postman API key required for upload.")
				fmt.Println("Get a free key at: https://postman.co/settings/me/api-keys")
				fmt.Println()
			}
			key, err := prompt.PromptPostmanAPIKey()
			if err != nil || key == "" {
				return &exitCodeError{errors.New("no Postman API key provided"), ExitUsageError}
			}
			path, serr := postman.SaveAPIKey(key)
			if serr != nil {
				return &exitCodeError{fmt.Errorf("save Postman credentials: %w", serr), ExitRuntimeError}
			}
			if !quiet {
				fmt.Printf("   API key saved to %s\n", path)
			}
			apiKey, source = key, "file:"+path
		} else {
			return &exitCodeError{
				errors.New("--upload requires a Postman API key " +
					"(set --postman-api-key, APIDOC_POSTMAN_API_KEY, or POSTMAN_API_KEY)"),
				ExitUsageError,
			}
		}
	}

	collectionJSON, err := os.ReadFile(collectionPath)
	if err != nil {
		return &exitCodeError{fmt.Errorf("read collection.json: %w", err), ExitRuntimeError}
	}

	client := postman.NewClient(apiKey)
	if cfg.Verbose && !quiet {
		fmt.Printf("   Using Postman API key from %s\n", source)
	}

	cachedUID := postman.LoadCachedUID(cfg.ProjectPath, cfg.Title)
	if !quiet {
		if cachedUID != "" {
			fmt.Printf("☁️  Updating Postman collection (uid=%s)...\n", cachedUID)
		} else {
			fmt.Println("☁️  Uploading collection to Postman...")
		}
	}

	var resp *postman.CollectionResponse
	if cachedUID != "" {
		resp, err = client.UpdateCollection(ctx, cachedUID, collectionJSON)
		if err != nil {
			if !quiet {
				fmt.Fprintf(os.Stderr, "   update failed (%v); creating new collection\n", err)
			}
			resp, err = client.CreateCollection(ctx, collectionJSON, cfg.PostmanWorkspaceUID)
		}
	} else {
		resp, err = client.CreateCollection(ctx, collectionJSON, cfg.PostmanWorkspaceUID)
	}
	if err != nil {
		return &exitCodeError{fmt.Errorf("postman upload failed: %w", err), ExitRuntimeError}
	}

	if err := postman.SaveCachedUID(cfg.ProjectPath, cfg.Title, resp.Collection.UID); err != nil && !quiet {
		fmt.Fprintf(os.Stderr, "   warning: failed to cache collection UID: %v\n", err)
	}

	if !quiet {
		fmt.Println()
		fmt.Printf("\U0001f4ee Collection uploaded: %s\n", postman.WebURL(resp.Collection.UID))
	}

	if postman.IsDesktopInstalled() && !quiet {
		fmt.Println("   Opening Postman...")
		time.Sleep(2 * time.Second)
		postman.OpenDesktop(resp.Collection.UID) //nolint:errcheck
	}

	return nil
}
