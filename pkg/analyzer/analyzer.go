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
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// Analyzer analyzes the codebase to extract API information
type Analyzer struct {
	config            *config.Config
	framework         models.FrameWorkType
	endpoints         []models.Endpoint
	models            map[string]models.Schema
	typeRegistry      map[string]models.Schema // type name -> schema (for request/response resolution)
	typePackageName   map[string]string        // type name -> the Go package it's declared in (e.g. "models"), for qualifying cross-package type refs in --write-annotations output
	stringConsts      map[string]string        // project-wide: identifier -> value, for package-level `const x = "..."` / `var x = "..."` declarations (see literalStringArg)
	curGroupPrefix    map[string]string        // per-file: variable name -> path prefix (Gin/Echo/Fiber Group, Gorilla Subrouter)
	curAuthGroups     map[string]bool          // per-file: variable name -> true if group uses auth middleware
	curFilePath       string                   // current file being parsed (for SourceFile on endpoints)
	consumedCalls     map[*ast.CallExpr]bool   // per-file: gorilla HandleFunc/Handle calls already consumed by a .Methods() chain
	parseDiagnostics  []ParseDiagnostic        // .go files that matched the walk but failed to parse — see Diagnostics()
	authMatches       []string                 // middleware names matched as auth-like — see AuthMiddlewareMatches()
	bindHintMatches   []string                 // call names matched as JSON-binding by hint (not exact method name) — see BindHintMatches()
	detectedPort      string                   // port parsed from the app's own .Run/.Listen/.Start/http.ListenAndServe call, if found — see detectListenPort
	resolvedLocalPort string                   // the port actually used for the auto-detected Servers[0] URL (detectedPort, or the .env/hardcoded fallback from detectServerURL) — see DetectedPort

	// root scopes every file read/parse in this package to config.ProjectPath's
	// tree, opened once in Analyze() and closed when it returns. Plain
	// filepath.Walk + os.ReadFile/parser.ParseFile checks whether an entry is
	// a symlink (via Lstat) separately from actually opening it — a crafted
	// repository could swap a regular file for a symlink pointing outside the
	// project tree in the window between the two, and the subsequent
	// os.ReadFile/parser.ParseFile-by-path would follow it regardless of what
	// the walk saw. os.Root (Go 1.24+) resolves and opens in one confined
	// operation instead: root.Open of anything that resolves outside the
	// root fails outright ("path escapes from parent"), closing that race
	// rather than just narrowing it. See walkProjectDir/rootReadFile/
	// rootParseFile below, which every file access in this package goes
	// through instead of calling os/go-parser directly.
	root *os.Root
}

// walkProjectDir walks a.root (== a.config.ProjectPath) and invokes fn for
// every entry, converting fs.WalkDir's root-relative path back into the same
// full-path form (config.ProjectPath-joined) every caller already expects,
// so existing exclude-list/extension/SourceFile-equality comparisons keep
// working unchanged. Skips symlinks unconditionally — fs.WalkDir reports a
// symlinked directory's type without descending into it (matching
// filepath.Walk's behavior), so this only needs to check d.Type(), not
// separately guard against recursing into one.
func (a *Analyzer) walkProjectDir(fn func(fullPath string, d fs.DirEntry) error) error {
	return fs.WalkDir(a.root.FS(), ".", func(relPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		return fn(filepath.Join(a.config.ProjectPath, relPath), d)
	})
}

// rootReadFile reads fullPath (which must be config.ProjectPath-joined, as
// every path this package produces is) through a.root — see the Analyzer.root
// doc comment for why this, rather than os.ReadFile, is required. Refuses a
// symlink at rel itself, matching walkProjectDir's unconditional symlink
// skip: os.Root follows symlinks that stay within the root (only blocking
// ones that escape it), so without this an in-root symlink swapped in after
// the walk already passed it as a regular file would still be followed.
func (a *Analyzer) rootReadFile(fullPath string) ([]byte, error) {
	rel, err := filepath.Rel(a.config.ProjectPath, fullPath)
	if err != nil {
		return nil, err
	}
	info, err := a.root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrNotExist
	}
	return a.root.ReadFile(rel)
}

// rootParseFile parses fullPath through rootReadFile instead of letting
// parser.ParseFile open the path directly — same reasoning as rootReadFile.
// fullPath is still passed to parser.ParseFile as the filename (for position
// info in the returned AST and any parse-error messages).
func (a *Analyzer) rootParseFile(fset *token.FileSet, fullPath string, mode parser.Mode) (*ast.File, error) {
	data, err := a.rootReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	return parser.ParseFile(fset, fullPath, data, mode)
}

// readProjectFile reads a single well-known file (go.mod, .env) directly
// under projectPath, refusing a symlink exactly as rootReadFile does — used
// by the package-level DetectFrameworks/detectServerURL, which run before
// (or without) an Analyzer/its root, so they open a short-lived root of
// their own scoped to just this one read.
func readProjectFile(projectPath, name string) ([]byte, error) {
	root, err := os.OpenRoot(projectPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrNotExist
	}
	return root.ReadFile(name)
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

// DetectedPort returns the same port used to build the auto-detected
// Servers[0] URL: the app's own source (detectListenPort, e.g. from
// r.Run(":8080")) when found, else whatever detectServerURL resolved (a
// .env PORT entry, or its own hardcoded :8080 fallback). Call after
// Analyze() returns. Returns "" only when config.Servers was set explicitly
// (a user-configured .apidoc-gen.yaml `servers:` entry may not be a local
// port at all - a remote host, a different scheme - so it would be wrong to
// also try binding a local server, e.g. the Swagger UI preview server, to
// it).
func (a *Analyzer) DetectedPort() string {
	if len(a.config.Servers) > 0 {
		return ""
	}
	return a.resolvedLocalPort
}

// Analyze scans the codebase and extracts API information
func (a *Analyzer) Analyze() (*models.APISpec, error) {
	root, err := os.OpenRoot(a.config.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("open project directory: %w", err)
	}
	a.root = root
	defer a.root.Close()

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
	err = a.walkProjectDir(func(path string, d fs.DirEntry) error {
		if d.IsDir() {
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
	err = a.walkProjectDir(func(path string, d fs.DirEntry) error {
		if d.IsDir() {
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

	// Filter by --tags, if set. Must run after tags are fully assigned above.
	a.endpoints = a.filterEndpointsByTags(a.endpoints)

	// Create API spec
	spec := &models.APISpec{
		Title:           a.config.Title,
		Version:         a.config.Version,
		Description:     a.config.Description,
		BasePath:        a.config.BasePath,
		Endpoints:       a.endpoints,
		Models:          a.models,
		TypePackageName: a.typePackageName,
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
		// Default server: prefer the port the app's own code actually listens
		// on (detectListenPort, scanned during pass 2 above); fall back to
		// .env, then :8080 - see detectServerURL. Also drives DetectedPort()
		// below, so the two stay in sync - a caller trying to reuse "the
		// port we detected" (e.g. the Swagger UI preview server) must see
		// exactly what ended up in this URL, not just the code-scan half of it.
		url := detectServerURL(a.config.ProjectPath)
		if a.detectedPort != "" {
			url = "http://localhost:" + a.detectedPort
		}
		if idx := strings.LastIndex(url, ":"); idx >= 0 {
			a.resolvedLocalPort = url[idx+1:]
		}
		spec.Servers = []models.Server{
			{
				URL:         url,
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
// project's .env file for a PORT / APP_PORT / SERVER_PORT entry. This is the
// fallback used when detectListenPort found nothing in the app's own code
// (e.g. the port is read from an env var at runtime rather than passed as a
// literal/constant) - see the call site in Analyze. Falls back further to
// http://localhost:8080 when nothing is found there either.
func detectServerURL(projectPath string) string {
	data, err := readProjectFile(projectPath, ".env")
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

// detectListenPort scans a single AST node for the app's own "start
// listening" call - Gin/Echo's .Run, Fiber's .Listen, Echo's .Start, or the
// standard library's http.ListenAndServe - and records the port from its
// address argument (":8080", "0.0.0.0:8080", "localhost:8080", ...) so the
// generated docs' server URL points at the port the application actually
// listens on, not a generic default. Only the first match found across the
// whole project is kept; a project has exactly one such call in practice.
// The address argument is resolved via literalStringArg, so a named
// constant (const addr = ":8080") is picked up exactly like an inline
// literal would be.
func (a *Analyzer) detectListenPort(n ast.Node) {
	if a.detectedPort != "" {
		return
	}
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	var addrArg ast.Expr
	switch sel.Sel.Name {
	case "Run", "Listen", "Start":
		addrArg = call.Args[0]
	case "ListenAndServe":
		// Only the stdlib net/http package-level function - not just any
		// method that happens to also be named ListenAndServe.
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "http" {
			addrArg = call.Args[0]
		}
	}
	if addrArg == nil {
		return
	}
	addr, ok := a.literalStringArg(addrArg)
	if !ok {
		return
	}
	if port := portFromAddr(addr); port != "" {
		a.detectedPort = port
	}
}

// portFromAddr extracts the port from a net.Listen-style address (":8080",
// "0.0.0.0:8080", "localhost:8080"). Returns "" if there's no colon or
// nothing purely numeric follows the last one - e.g. an empty address
// (Gin's r.Run() with no args defaults to :8080, but that default lives in
// Gin's source, not this literal, so there's nothing to parse here) or a
// unix socket path.
func portFromAddr(addr string) string {
	idx := strings.LastIndex(addr, ":")
	if idx == -1 {
		return ""
	}
	port := addr[idx+1:]
	if port == "" {
		return ""
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return port
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
	content, err := readProjectFile(projectPath, "go.mod")
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
	node, err := a.rootParseFile(fset, filePath, parser.ParseComments)
	if err != nil {
		// Recorded (not silently dropped) even though pass 1 already parsed
		// this same file and would have recorded the identical failure —
		// recordParseFailure dedupes by path.
		a.recordParseFailure(filePath, err)
		return nil // Skip files that can't be parsed
	}

	// Look for the app's own listen call in every file regardless of
	// framework - in particular, this must run before the Chi branch below,
	// which returns early and would otherwise skip Chi files entirely, even
	// though a Chi app's http.ListenAndServe call commonly lives right next
	// to its router setup in the same file.
	ast.Inspect(node, func(n ast.Node) bool {
		a.detectListenPort(n)
		return true
	})

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
