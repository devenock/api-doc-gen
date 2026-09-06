// Package analyzer statically analyzes a Go project's AST to extract its API
// surface (routes, request/response bodies, models) with no annotations
// required. This file holds the Analyzer type, its top-level Analyze()
// orchestration, framework detection, and the parseFile dispatch that hands
// each file off to the matching route-family parser. See:
//
//   - schema.go: Go struct -> OpenAPI schema conversion
//   - request.go / response.go: request-body binding / response inference
//   - routegroups.go: shared route-group-prefix and auth-group tracking
//   - routes_gin.go / routes_gorilla.go / routes_chi.go / routes_generic.go:
//     per-framework route parsers (Echo and Fiber share Gin's, see
//     routes_gin.go)
//   - endpoint.go: assembling/backfilling/deduplicating endpoints
//   - paths.go: path-string normalization and param extraction
package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// isSymlink reports whether info describes a symbolic link. filepath.Walk
// never recurses into a symlinked directory (Lstat reports it as a symlink,
// not a directory), but it does NOT protect against a symlinked *file* —
// os.ReadFile/parser.ParseFile follow symlinks at the OS level regardless of
// how Walk reached the path. Without this check, a crafted repository
// containing a `.go`-named symlink pointing outside the project tree (e.g.
// at another local Go module, or any other file that happens to parse as
// Go) would have that target's content read and folded into the generated
// docs — and, with --write-annotations, written back to. Every
// filepath.Walk callback in this package must skip symlinks.
func isSymlink(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}

// readNonSymlinkFile reads path only if it is a regular file, not a
// symlink — the same defense-in-depth reasoning as isSymlink, applied to
// the handful of single-file reads (go.mod, .env) that sit outside the
// filepath.Walk callbacks and so aren't covered by that check.
func readNonSymlinkFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if isSymlink(info) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(path)
}

// Analyzer analyzes the codebase to extract API information
type Analyzer struct {
	config           *config.Config
	framework        models.FrameWorkType
	endpoints        []models.Endpoint
	models           map[string]models.Schema
	typeRegistry     map[string]models.Schema // type name -> schema (for request/response resolution)
	curGroupPrefix   map[string]string        // per-file: variable name -> path prefix (Gin/Echo/Fiber Group, Gorilla Subrouter)
	curAuthGroups    map[string]bool          // per-file: variable name -> true if group uses auth middleware
	curFilePath      string                   // current file being parsed (for SourceFile on endpoints)
	consumedCalls    map[*ast.CallExpr]bool   // per-file: gorilla HandleFunc/Handle calls already consumed by a .Methods() chain
	parseDiagnostics []ParseDiagnostic        // .go files that matched the walk but failed to parse — see Diagnostics()
	authMatches      []string                 // middleware names matched as auth-like — see AuthMiddlewareMatches()
	bindHintMatches  []string                 // call names matched as JSON-binding by hint (not exact method name) — see BindHintMatches()
}

// BindHintMatches returns every distinct call name matched as a JSON-binding
// call by name hint (bindMethodHints — "bind", "decode", "unmarshal",
// "parse") rather than an exact known framework method, in first-seen
// order. Populated only when config.Verbose is set. Lets a user see exactly
// which project-specific wrapper calls (e.g. h.decodeBody) were guessed to
// be request-body binding.
func (a *Analyzer) BindHintMatches() []string {
	return a.bindHintMatches
}

// recordBindHintMatch appends text to bindHintMatches, deduplicated.
func (a *Analyzer) recordBindHintMatch(text string) {
	for _, seen := range a.bindHintMatches {
		if seen == text {
			return
		}
	}
	a.bindHintMatches = append(a.bindHintMatches, text)
}

// AuthMiddlewareMatches returns every distinct middleware identifier name
// that was matched as auth-like during analysis (via the built-in heuristic
// or the configured AuthMiddleware list), in first-seen order. Populated
// only when config.Verbose is set — see (*Analyzer).middlewareLooksLikeAuth.
// Lets a user see exactly what was guessed instead of trusting a silent
// substring match.
func (a *Analyzer) AuthMiddlewareMatches() []string {
	return a.authMatches
}

// ParseDiagnostic records a single .go file that was found during the
// project walk but could not be parsed, and why. A file failing to parse
// means every route and type it defines is silently absent from the
// generated docs — Diagnostics() exists so that absence is visible instead
// of indistinguishable from "this file legitimately has no routes".
type ParseDiagnostic struct {
	File string
	Err  error
}

// Diagnostics returns every .go file the project walk found but could not
// parse. Call after Analyze() returns.
func (a *Analyzer) Diagnostics() []ParseDiagnostic {
	return a.parseDiagnostics
}

// recordParseFailure appends a parse diagnostic, skipping duplicates —
// collectTypesInFile (pass 1) and parseFile (pass 2) both parse every .go
// file, so a file that fails to parse would otherwise be recorded twice.
func (a *Analyzer) recordParseFailure(filePath string, err error) {
	for _, d := range a.parseDiagnostics {
		if d.File == filePath {
			return
		}
	}
	a.parseDiagnostics = append(a.parseDiagnostics, ParseDiagnostic{File: filePath, Err: err})
}

// NewAnalyzer creates a new Analyzer
func NewAnalyzer(cfg *config.Config) *Analyzer {
	return &Analyzer{
		config:       cfg,
		framework:    models.FrameWorkUnknown,
		endpoints:    []models.Endpoint{},
		models:       make(map[string]models.Schema),
		typeRegistry: make(map[string]models.Schema),
	}
}

// Framework returns the framework Analyze() resolved (explicitly configured
// or auto-detected from go.mod), as a string. Call after Analyze() returns.
func (a *Analyzer) Framework() string {
	return string(a.framework)
}

// Analyze scans the codebase and extracts API information
func (a *Analyzer) Analyze() (*models.APISpec, error) {
	// Detect framework if not specified
	if a.config.Framework == "" {
		if err := a.detectFramework(); err != nil {
			return nil, err
		}
	} else {
		a.framework = models.FrameWorkType(a.config.Framework)
	}

	if a.config.Verbose {
		fmt.Printf("   Detected framework: %s\n", a.framework)
	}

	// Pass 1: collect type definitions from all .go files for request/response schema resolution
	err := filepath.Walk(a.config.ProjectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if isSymlink(info) {
			return nil
		}
		if info.IsDir() {
			for _, exclude := range a.config.Exclude {
				if filepath.Base(path) == exclude {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		return a.collectTypesInFile(path)
	})
	if err != nil {
		return nil, err
	}

	// Flatten embedded (anonymous) struct fields now that every type in the
	// project is known — must run before Pass 2 resolves request/response
	// schemas so promoted fields (e.g. from a gorm.Model-style base struct)
	// are present in the schemas handlers reference.
	a.resolveEmbeddedFields()

	// Pass 2: extract routes and resolve handler request/response from type registry
	err = filepath.Walk(a.config.ProjectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if isSymlink(info) {
			return nil
		}
		if info.IsDir() {
			for _, exclude := range a.config.Exclude {
				if filepath.Base(path) == exclude {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		return a.parseFile(path)
	})
	if err != nil {
		return nil, err
	}

	// Ensure every endpoint has a tag from path (for Swagger grouping by module)
	for i := range a.endpoints {
		if len(a.endpoints[i].Tags) == 0 {
			if t := tagFromPath(a.endpoints[i].Path); t != "" {
				a.endpoints[i].Tags = []string{t}
			}
		}
		// Description fallback: humanize summary when it looks like a handler name (CamelCase) and description is empty
		if a.endpoints[i].Description == "" && a.endpoints[i].Summary != "" {
			if looksLikeHandlerName(a.endpoints[i].Summary) {
				a.endpoints[i].Description = humanizeHandlerName(a.endpoints[i].Summary)
				a.endpoints[i].Summary = humanizeHandlerName(a.endpoints[i].Summary)
			}
		}
	}

	// Resolve SourceFile for handlers in other packages (e.g. controllers.CreateUser -> controllers/user_controller.go)
	a.resolveHandlerSourceFiles()

	// Final pass: fill request body schemas for any endpoint still missing them.
	// Covers same-package different-file handlers (bare idents like r.POST("/x", Create)
	// where Create lives in a sibling file) and any other case the per-file scan missed.
	a.resolveRemainingRequestBodies()

	// Same fallback pass for responses (review §3).
	a.resolveRemainingResponses()

	// Only now — after every resolution tier has had a chance — fall back to
	// a clearly-labeled placeholder for endpoints where response inference
	// truly found nothing, rather than silently claiming a shape was
	// inferred when it wasn't (review §3, Step 6).
	for i := range a.endpoints {
		if len(a.endpoints[i].Responses) == 0 {
			a.endpoints[i].Responses[200] = models.Response{
				Description: "Response shape could not be inferred",
				Content: map[string]models.Content{
					"application/json": {Schema: models.Schema{Type: "object"}},
				},
			}
		}
	}

	// Deduplicate by (method, path), keeping first occurrence
	a.endpoints = a.deduplicateEndpoints(a.endpoints)

	// Create API spec
	spec := &models.APISpec{
		Title:       a.config.Title,
		Version:     a.config.Version,
		Description: a.config.Description,
		BasePath:    a.config.BasePath,
		Endpoints:   a.endpoints,
		Models:      a.models,
	}

	// Add servers if configured
	if len(a.config.Servers) > 0 {
		for _, srv := range a.config.Servers {
			spec.Servers = append(spec.Servers, models.Server{
				URL:         srv.URL,
				Description: srv.Description,
			})
		}
	} else {
		// Default server: try to read port from .env, fall back to :8080
		spec.Servers = []models.Server{
			{
				URL:         detectServerURL(a.config.ProjectPath),
				Description: "Development server",
			},
		}
	}

	// Surface parse failures instead of leaving a user whose routes are
	// missing with no way to find out why (review §5.3). The summary always
	// prints when there were failures — verbose mode additionally lists
	// which files and why.
	if len(a.parseDiagnostics) > 0 && !a.config.Quiet {
		fmt.Fprintf(os.Stderr, "⚠️  %d file(s) could not be parsed (use -v for detail)\n", len(a.parseDiagnostics))
		if a.config.Verbose {
			for _, d := range a.parseDiagnostics {
				fmt.Fprintf(os.Stderr, "   %s: %v\n", d.File, d.Err)
			}
		}
	}

	// Heuristic transparency (review §5.2): -v shows exactly what the
	// name-based auth/bind-call heuristics guessed, so a false positive or
	// negative is visible instead of a silent, unexplained security-relevant
	// guess.
	if a.config.Verbose && !a.config.Quiet {
		if len(a.authMatches) > 0 {
			fmt.Fprintf(os.Stderr, "   Matched as auth middleware: %s\n", strings.Join(a.authMatches, ", "))
		}
		if len(a.bindHintMatches) > 0 {
			fmt.Fprintf(os.Stderr, "   Matched as request-body binding by name hint: %s\n", strings.Join(a.bindHintMatches, ", "))
		}
	}

	return spec, nil
}

// detectServerURL returns the base URL for the API server by scanning the
// project's .env file for a PORT / APP_PORT / SERVER_PORT entry.
// Falls back to http://localhost:8080 when nothing is found.
func detectServerURL(projectPath string) string {
	data, err := readNonSymlinkFile(filepath.Join(projectPath, ".env"))
	if err != nil {
		return "http://localhost:8080"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		for _, key := range []string{"PORT=", "APP_PORT=", "SERVER_PORT=", "HTTP_PORT="} {
			if strings.HasPrefix(line, key) {
				port := strings.Trim(strings.TrimPrefix(line, key), `"' `)
				if port != "" {
					return "http://localhost:" + port
				}
			}
		}
	}
	return "http://localhost:8080"
}

// frameworkMarkers maps each supported framework to the go.mod dependency
// substring that identifies it, in detection priority/display order.
var frameworkMarkers = []struct {
	framework string
	marker    string
}{
	{string(models.FrameWorkGin), "github.com/gin-gonic/gin"},
	{string(models.FrameWorkEcho), "github.com/labstack/echo"},
	{string(models.FrameWorkFiber), "github.com/gofiber/fiber"},
	{string(models.FrameWorkGorilla), "github.com/gorilla/mux"},
	{string(models.FrameWorkChi), "github.com/go-chi/chi"},
}

// DetectFrameworks scans the project's go.mod for every supported
// framework's dependency and returns all that are found — zero, one, or
// more than one. A project can genuinely depend on two routing frameworks
// at once (e.g. Gin for the API, Chi in a vendored subpackage), so unlike a
// single best-guess this doesn't hide that ambiguity from the caller.
// Lines marked `// indirect` are skipped: a framework pulled in transitively
// by some other dependency, and never imported by the project's own code,
// must not count as "this project uses it".
func DetectFrameworks(projectPath string) []string {
	goModPath := filepath.Join(projectPath, "go.mod")
	content, err := readNonSymlinkFile(goModPath)
	if err != nil {
		return nil
	}
	var found []string
	for _, fm := range frameworkMarkers {
		for _, line := range strings.Split(string(content), "\n") {
			if strings.Contains(line, "// indirect") {
				continue
			}
			if strings.Contains(line, fm.marker) {
				found = append(found, fm.framework)
				break
			}
		}
	}
	return found
}

// DetectFramework scans the project (e.g. go.mod) and returns the framework
// identifier: "gin", "echo", "fiber", "gorilla", "chi", or "" when zero or
// more than one framework is detected — on ambiguity, a caller silently
// picking one is worse than an honest "unknown", so this deliberately
// returns "" rather than the first match the way a naive switch would.
// Callers that can act on the ambiguity (a warning, prompting for
// --framework) should call DetectFrameworks directly instead; see
// (*Analyzer).detectFramework and runInit in cmd/root.go.
func DetectFramework(projectPath string) string {
	frameworks := DetectFrameworks(projectPath)
	if len(frameworks) == 1 {
		return frameworks[0]
	}
	return ""
}

// detectFramework attempts to detect the framework being used
func (a *Analyzer) detectFramework() error {
	frameworks := DetectFrameworks(a.config.ProjectPath)
	if len(frameworks) > 1 && !a.config.Quiet {
		fmt.Fprintf(os.Stderr,
			"⚠️  Multiple frameworks detected in go.mod (%s) — pass --framework to disambiguate. Falling back to generic route detection.\n",
			strings.Join(frameworks, ", "))
	}
	if len(frameworks) != 1 {
		a.framework = models.FrameWorkUnknown
		return nil
	}
	a.framework = models.FrameWorkType(frameworks[0])
	return nil
}

// parseFile parses a Go file and extracts route information
func (a *Analyzer) parseFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		// Recorded (not silently dropped) even though pass 1 already parsed
		// this same file and would have recorded the identical failure —
		// recordParseFailure dedupes by path.
		a.recordParseFailure(filePath, err)
		return nil // Skip files that can't be parsed
	}

	a.curFilePath = filePath
	// Build route group prefix map and auth groups for this file
	a.curGroupPrefix = nil
	a.curAuthGroups = nil
	a.consumedCalls = nil
	switch a.framework {
	case models.FrameWorkGin, models.FrameWorkEcho, models.FrameWorkFiber:
		a.curGroupPrefix = a.buildGinGroupPrefixes(node)
		a.curAuthGroups = a.buildGinAuthGroups(node)
	case models.FrameWorkGorilla:
		a.curGroupPrefix = a.buildGorillaSubrouterPrefixes(node)
		a.curAuthGroups = a.buildGinAuthGroups(node) // .Use(...) detection is framework-agnostic
		a.consumedCalls = make(map[*ast.CallExpr]bool)
	case models.FrameWorkChi:
		// Chi's r.Route("/x", func(r chi.Router) {...}) nesting reuses the same
		// receiver identifier ("r") at every level, so a flat var->prefix map
		// (like Gin's Group() handling) can't represent it. Walk each function
		// body recursively instead, carrying prefix/auth state through closures.
		for _, decl := range node.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				a.walkChiStmts(fd.Body.List, node, "", false)
			}
		}
		return nil
	}

	// Visit all nodes in the AST
	ast.Inspect(node, func(n ast.Node) bool {
		switch a.framework {
		case models.FrameWorkGin:
			a.parseGinRoutes(n, node)
		case models.FrameWorkEcho:
			a.parseEchoRoutes(n, node)
		case models.FrameWorkFiber:
			a.parseFiberRoutes(n, node)
		case models.FrameWorkGorilla:
			a.parseGorillaMethods(n, node) // .Methods("POST") chain first
			a.parseGorillaRoutes(n, node)
		default:
			// Try to detect routes from common patterns
			a.parseGenericRoutes(n, node)
		}
		return true
	})

	return nil
}
