package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// Analyzer analyzes the codebase to extract API information
type Analyzer struct {
	config    *config.Config
	framework models.FrameWorkType
	endpoints []models.Endpoint
	models    map[string]models.Schema
}

// NewAnalyzer creates a new Analyzer
func NewAnalyzer(cfg *config.Config) *Analyzer {
	return &Analyzer{
		config:    cfg,
		framework: models.FrameWorkUnknown,
		endpoints: []models.Endpoint{},
		models:    make(map[string]models.Schema),
	}
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

	// Walk through the project directory
	err := filepath.Walk(a.config.ProjectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories
		if info.IsDir() {
			for _, exclude := range a.config.Exclude {
				if strings.Contains(path, exclude) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Only process Go files
		if filepath.Ext(path) != ".go" {
			return nil
		}

		// Parse the file
		return a.parseFile(path)
	})

	if err != nil {
		return nil, err
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
		// Default server
		spec.Servers = []models.Server{
			{
				URL:         "http://localhost:8080",
				Description: "Development server",
			},
		}
	}

	return spec, nil
}

// detectFramework attempts to detect the framework being used
func (a *Analyzer) detectFramework() error {
	goModPath := filepath.Join(a.config.ProjectPath, "go.mod")

	content, err := os.ReadFile(goModPath)
	if err != nil {
		// If go.mod doesn't exist, try to detect from imports
		a.framework = models.FrameWorkUnknown
		return nil
	}

	contentStr := string(content)

	// Check for framework dependencies
	if strings.Contains(contentStr, "github.com/gin-gonic/gin") {
		a.framework = models.FrameWorkGin
	} else if strings.Contains(contentStr, "github.com/labstack/echo") {
		a.framework = models.FrameWorkEcho
	} else if strings.Contains(contentStr, "github.com/gofiber/fiber") {
		a.framework = models.FrameWorkFiber
	} else if strings.Contains(contentStr, "github.com/gorilla/mux") {
		a.framework = models.FrameWorkGorilla
	} else if strings.Contains(contentStr, "github.com/go-chi/chi") {
		a.framework = models.FrameWorkChi
	} else {
		a.framework = models.FrameWorkUnknown
	}

	return nil
}

// parseFile parses a Go file and extracts route information
func (a *Analyzer) parseFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil // Skip files that can't be parsed
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
			a.parseGorillaMethods(n) // .Methods("POST") chain first
			a.parseGorillaRoutes(n, node)
		case models.FrameWorkChi:
			a.parseChiRoutes(n, node)
		default:
			// Try to detect routes from common patterns
			a.parseGenericRoutes(n, node)
		}
		return true
	})

	return nil
}

// parseGinRoutes extracts routes from Gin framework
func (a *Analyzer) parseGinRoutes(n ast.Node, file *ast.File) {
	callExpr, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	// Check for Gin route methods (GET, POST, PUT, DELETE, PATCH, etc.)
	method := strings.ToUpper(selExpr.Sel.Name)
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true,
	}

	if !validMethods[method] {
		return
	}

	// Extract path
	if len(callExpr.Args) < 1 {
		return
	}

	pathLit, ok := callExpr.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}

	path := strings.Trim(pathLit.Value, `"`)

	// Create endpoint
	endpoint := models.Endpoint{
		Path:        path,
		Method:      method,
		Summary:     fmt.Sprintf("%s %s", method, path),
		Parameters:  extractPathParams(path),
		Responses:   make(map[int]models.Response),
	}

	// Extract handler function name and look for comments
	if len(callExpr.Args) >= 2 {
		if ident, ok := callExpr.Args[1].(*ast.Ident); ok {
			endpoint.Summary = ident.Name
			// Try to find the handler function's comment
			a.extractHandlerComments(file, ident.Name, &endpoint)
		}
	}

	// Add default response
	endpoint.Responses[200] = models.Response{
		Description: "Successful response",
		Content: map[string]models.Content{
			"application/json": {
				Schema: models.Schema{
					Type: "object",
				},
			},
		},
	}

	a.endpoints = append(a.endpoints, endpoint)
}

// parseEchoRoutes extracts routes from Echo framework
func (a *Analyzer) parseEchoRoutes(n ast.Node, file *ast.File) {
	// Similar pattern to Gin
	a.parseGinRoutes(n, file)
}

// parseFiberRoutes extracts routes from Fiber framework
func (a *Analyzer) parseFiberRoutes(n ast.Node, file *ast.File) {
	// Similar pattern to Gin
	a.parseGinRoutes(n, file)
}

// parseGorillaRoutes extracts routes from Gorilla Mux
func (a *Analyzer) parseGorillaRoutes(n ast.Node, file *ast.File) {
	callExpr, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	// Check for HandleFunc or Handle
	if selExpr.Sel.Name != "HandleFunc" && selExpr.Sel.Name != "Handle" {
		return
	}

	// Extract path and method from Methods() chain or inline
	if len(callExpr.Args) < 1 {
		return
	}

	pathLit, ok := callExpr.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}

	path := strings.Trim(pathLit.Value, `"`)

	endpoint := models.Endpoint{
		Path:        path,
		Method:      "GET", // default; may be updated when .Methods() is seen
		Summary:     fmt.Sprintf("Handler for %s", path),
		Parameters:  extractPathParams(path),
		Responses:   make(map[int]models.Response),
	}

	endpoint.Responses[200] = models.Response{
		Description: "Successful response",
		Content: map[string]models.Content{
			"application/json": {
				Schema: models.Schema{
					Type: "object",
				},
			},
		},
	}

	a.endpoints = append(a.endpoints, endpoint)
}

// parseGorillaMethods handles .Methods("POST") chained after HandleFunc/Handle
func (a *Analyzer) parseGorillaMethods(n ast.Node) {
	callExpr, ok := n.(*ast.CallExpr)
	if !ok || len(callExpr.Args) == 0 {
		return
	}
	sel, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Methods" {
		return
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok || len(inner.Args) < 1 {
		return
	}
	innerSel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || (innerSel.Sel.Name != "HandleFunc" && innerSel.Sel.Name != "Handle") {
		return
	}
	pathLit, ok := inner.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}
	path := strings.Trim(pathLit.Value, `"`)
	method := "GET"
	if lit, ok := callExpr.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		method = strings.ToUpper(strings.Trim(lit.Value, `"`))
	}
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true,
	}
	if !validMethods[method] {
		return
	}
	// Update the most recently added endpoint with this path to use the correct method
	for i := len(a.endpoints) - 1; i >= 0; i-- {
		if a.endpoints[i].Path == path {
			a.endpoints[i].Method = method
			return
		}
	}
	// .Methods() visited before HandleFunc: add endpoint with correct method
	a.endpoints = append(a.endpoints, models.Endpoint{
		Path:        path,
		Method:      method,
		Summary:     fmt.Sprintf("%s %s", method, path),
		Parameters:  extractPathParams(path),
		Responses:   make(map[int]models.Response),
	})
	ep := &a.endpoints[len(a.endpoints)-1]
	ep.Responses[200] = models.Response{
		Description: "Successful response",
		Content: map[string]models.Content{
			"application/json": {Schema: models.Schema{Type: "object"}},
		},
	}
}

// parseChiRoutes extracts routes from Chi router
func (a *Analyzer) parseChiRoutes(n ast.Node, file *ast.File) {
	// Similar to Gin
	a.parseGinRoutes(n, file)
}

// parseGenericRoutes attempts to extract routes from unknown frameworks
func (a *Analyzer) parseGenericRoutes(n ast.Node, file *ast.File) {
	// Try to detect HTTP method patterns
	callExpr, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	method := strings.ToUpper(selExpr.Sel.Name)
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true,
	}

	if validMethods[method] && len(callExpr.Args) >= 1 {
		if pathLit, ok := callExpr.Args[0].(*ast.BasicLit); ok && pathLit.Kind == token.STRING {
			path := strings.Trim(pathLit.Value, `"`)
			endpoint := models.Endpoint{
				Path:        path,
				Method:      method,
				Summary:     fmt.Sprintf("%s %s", method, path),
				Parameters:  extractPathParams(path),
				Responses:   make(map[int]models.Response),
			}

			endpoint.Responses[200] = models.Response{
				Description: "Successful response",
				Content: map[string]models.Content{
					"application/json": {
						Schema: models.Schema{Type: "object"},
					},
				},
			}

			a.endpoints = append(a.endpoints, endpoint)
		}
	}
}

// extractHandlerComments extracts comments from handler functions
func (a *Analyzer) extractHandlerComments(file *ast.File, handlerName string, endpoint *models.Endpoint) {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != handlerName {
			continue
		}

		if funcDecl.Doc != nil {
			var description strings.Builder
			for _, comment := range funcDecl.Doc.List {
				text := strings.TrimPrefix(comment.Text, "//")
				text = strings.TrimSpace(text)
				description.WriteString(text)
				description.WriteString(" ")
			}
			endpoint.Description = strings.TrimSpace(description.String())

			// Use first line as summary if not too long
			lines := strings.Split(endpoint.Description, "\n")
			if len(lines) > 0 && len(lines[0]) < 100 {
				endpoint.Summary = lines[0]
			}
		}
		break
	}
}

// extractPathParams extracts path parameters from route path.
// Supports :param (Gin, Echo, Fiber) and {param} (Chi).
func extractPathParams(path string) []models.Parameter {
	var params []models.Parameter
	// :param style (e.g. /users/:id)
	colonRe := regexp.MustCompile(`:([^/]+)`)
	for _, name := range colonRe.FindAllStringSubmatch(path, -1) {
		if len(name) >= 2 {
			params = append(params, models.Parameter{
				Name:     name[1],
				In:       "path",
				Required: true,
				Schema:   models.Schema{Type: "string"},
			})
		}
	}
	// {param} style (e.g. /users/{id})
	braceRe := regexp.MustCompile(`\{([^}]+)\}`)
	for _, name := range braceRe.FindAllStringSubmatch(path, -1) {
		if len(name) >= 2 {
			params = append(params, models.Parameter{
				Name:     name[1],
				In:       "path",
				Required: true,
				Schema:   models.Schema{Type: "string"},
			})
		}
	}
	return params
}

// deduplicateEndpoints keeps first occurrence of each (method, path).
func (a *Analyzer) deduplicateEndpoints(endpoints []models.Endpoint) []models.Endpoint {
	seen := make(map[string]bool)
	var out []models.Endpoint
	for _, ep := range endpoints {
		key := ep.Method + " " + ep.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ep)
	}
	return out
}
