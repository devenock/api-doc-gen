package postman

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// IsDesktopInstalled reports whether the Postman desktop app is installed on
// the current machine by checking well-known install paths per OS.
func IsDesktopInstalled() bool {
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat("/Applications/Postman.app"); err == nil {
			return true
		}
		home, _ := os.UserHomeDir()
		_, err := os.Stat(filepath.Join(home, "Applications", "Postman.app"))
		return err == nil
	case "linux":
		if _, err := exec.LookPath("postman"); err == nil {
			return true
		}
		home, _ := os.UserHomeDir()
		candidates := []string{
			filepath.Join(home, ".local", "share", "Postman", "Postman"),
			"/opt/Postman/Postman",
			"/usr/bin/postman",
			"/usr/local/bin/postman",
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
		return false
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			return false
		}
		entries, err := os.ReadDir(filepath.Join(localAppData, "Postman"))
		return err == nil && len(entries) > 0
	}
	return false
}

// OpenDesktop launches the Postman desktop app. When collectionUID is non-empty
// it navigates directly to that collection using the postman:// URL scheme.
func OpenDesktop(collectionUID string) error {
	u := "postman://"
	if collectionUID != "" {
		u = fmt.Sprintf("postman://app/collections/%s", collectionUID)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "linux":
		if err := exec.Command("xdg-open", u).Start(); err == nil {
			return nil
		}
		if path, err := exec.LookPath("postman"); err == nil {
			return exec.Command(path).Start()
		}
		home, _ := os.UserHomeDir()
		for _, p := range []string{
			filepath.Join(home, ".local", "share", "Postman", "Postman"),
			"/opt/Postman/Postman",
		} {
			if _, err := os.Stat(p); err == nil {
				return exec.Command(p).Start()
			}
		}
		return fmt.Errorf("could not locate Postman binary")
	case "windows":
		return exec.Command("cmd", "/c", "start", "", u).Start()
	}
	return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
}
