package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

func printPostmanInstructions(outputDir string, quiet bool) {
	if quiet {
		return
	}
	collectionPath := filepath.Join(outputDir, "collection.json")
	if _, err := os.Stat(collectionPath); err != nil {
		return
	}
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
