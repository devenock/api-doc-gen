package postman

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
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

// ImportToDesktop imports a collection file directly into the Postman desktop
// app without requiring a Postman account or API key. It starts a temporary
// localhost HTTP server, opens the Postman import URL scheme pointing at it,
// and waits until Postman fetches the file (or 20 s) before shutting down.
func ImportToDesktop(collectionPath string) error {
	dir := filepath.Dir(collectionPath)
	name := filepath.Base(collectionPath)

	// Signal channel — closed when Postman fetches the collection file.
	fetched := make(chan struct{})
	once := make(chan struct{}, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, collectionPath)
		// Signal only on the first fetch.
		select {
		case once <- struct{}{}:
			close(fetched)
		default:
		}
	})
	// Serve any other file in the same dir (unlikely to be needed, harmless).
	mux.Handle("/", http.FileServer(http.Dir(dir)))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start local import server: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck

	importURL := fmt.Sprintf(
		"postman://app/collections/import?url=%s",
		url.QueryEscape(fmt.Sprintf("http://127.0.0.1:%d/%s", port, name)),
	)

	var openErr error
	switch runtime.GOOS {
	case "darwin":
		openErr = exec.Command("open", importURL).Start()
	case "linux":
		openErr = exec.Command("xdg-open", importURL).Start()
	case "windows":
		openErr = exec.Command("cmd", "/c", "start", "", importURL).Start()
	default:
		openErr = fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx) //nolint:errcheck
	}

	if openErr != nil {
		shutdown()
		return openErr
	}

	// Wait for Postman to fetch the file, or give up after 20 s.
	select {
	case <-fetched:
		time.Sleep(500 * time.Millisecond) // brief grace period
	case <-time.After(20 * time.Second):
	}

	shutdown()
	return nil
}

// OpenDesktop launches the Postman desktop app. When collectionUID is non-empty
// it navigates directly to that collection using the postman:// URL scheme.
// Errors here are non-fatal — the collection is already on Postman cloud.
func OpenDesktop(collectionUID string) error {
	url := "postman://"
	if collectionUID != "" {
		url = fmt.Sprintf("postman://app/collections/%s", collectionUID)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		if err := exec.Command("xdg-open", url).Start(); err == nil {
			return nil
		}
		// Fallback: launch the binary directly
		if path, err := exec.LookPath("postman"); err == nil {
			return exec.Command(path).Start()
		}
		home, _ := os.UserHomeDir()
		candidates := []string{
			filepath.Join(home, ".local", "share", "Postman", "Postman"),
			"/opt/Postman/Postman",
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return exec.Command(p).Start()
			}
		}
		return fmt.Errorf("could not locate Postman binary")
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	}
	return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
}
