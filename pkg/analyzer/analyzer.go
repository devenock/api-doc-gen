package analyzer

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

var errStopWalk = errors.New("stop walk")

// Analyzer analyzes the codebase to extract API information
type Analyzer struct {
	config         *config.Config
	framework      models.FrameWorkType
	endpoints      []models.Endpoint
	models         map[string]models.Schema
	typeRegistry   map[string]models.Schema // type name -> schema (for request/response resolution)
	curGroupPrefix map[string]string        // per-file: variable name -> path prefix (Gin/Echo/Fiber Group)
	curAuthGroups  map[string]bool          // per-file: variable name -> true if group uses auth middleware
	curFilePath    string                   // current file being parsed (for SourceFile on endpoints)
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
		if info.IsDir() {
			for _, exclude := range a.config.Exclude {
				if strings.Contains(path, exclude) {
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

	// Pass 2: extract routes and resolve handler request/response from type registry
	err = filepath.Walk(a.config.ProjectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			for _, exclude := range a.config.Exclude {
				if strings.Contains(path, exclude) {
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

// DetectFramework scans the project (e.g. go.mod) and returns the framework
// identifier: "gin", "echo", "fiber", "gorilla", "chi", or "" if unknown.
// Useful for init so the config file can be pre-filled.
func DetectFramework(projectPath string) string {
	goModPath := filepath.Join(projectPath, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return ""
	}
	contentStr := string(content)
	switch {
	case strings.Contains(contentStr, "github.com/gin-gonic/gin"):
		return string(models.FrameWorkGin)
	case strings.Contains(contentStr, "github.com/labstack/echo"):
		return string(models.FrameWorkEcho)
	case strings.Contains(contentStr, "github.com/gofiber/fiber"):
		return string(models.FrameWorkFiber)
	case strings.Contains(contentStr, "github.com/gorilla/mux"):
		return string(models.FrameWorkGorilla)
	case strings.Contains(contentStr, "github.com/go-chi/chi"):
		return string(models.FrameWorkChi)
	default:
		return ""
	}
}

// detectFramework attempts to detect the framework being used
func (a *Analyzer) detectFramework() error {
	detected := DetectFramework(a.config.ProjectPath)
	if detected == "" {
		a.framework = models.FrameWorkUnknown
		return nil
	}
	a.framework = models.FrameWorkType(detected)
	return nil
}

// collectTypesInFile parses a Go file and adds struct type definitions to the type registry.
func (a *Analyzer) collectTypesInFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		return nil
	}
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			name := typeSpec.Name.Name
			schema := a.buildSchemaFromStruct(structType)
			if schema.Type != "" || len(schema.Properties) > 0 {
				a.typeRegistry[name] = schema
			}
		}
	}
	return nil
}

// buildSchemaFromStruct converts an ast.StructType to a models.Schema (object with properties).
func (a *Analyzer) buildSchemaFromStruct(st *ast.StructType) models.Schema {
	if st.Fields == nil {
		return models.Schema{Type: "object"}
	}
	props := make(map[string]models.Schema)
	var required []string
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue
		}
		fieldName := f.Names[0].Name
		if f.Tag != nil {
			tag := strings.Trim(f.Tag.Value, "`")
			for _, part := range strings.Split(tag, " ") {
				if strings.HasPrefix(part, "json:") {
					jsonTag := strings.Trim(strings.TrimPrefix(part, "json:"), `"`)
					if idx := strings.Index(jsonTag, ","); idx >= 0 {
						jsonTag = jsonTag[:idx]
					}
					if jsonTag != "" && jsonTag != "-" {
						fieldName = jsonTag
					}
					break
				}
			}
		}
		fieldSchema := a.goTypeToSchema(f.Type)
		props[fieldName] = fieldSchema
		// Only require non-pointer fields
		if !isPointerType(f.Type) && fieldSchema.Type != "" && fieldSchema.Ref == "" {
			required = append(required, fieldName)
		}
	}
	return models.Schema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

// goTypeToSchema maps a Go ast.Expr type to an OpenAPI-style Schema.
func (a *Analyzer) goTypeToSchema(expr ast.Expr) models.Schema {
	switch t := expr.(type) {
	case *ast.Ident:
		return a.identToSchema(t.Name)
	case *ast.StarExpr:
		return a.goTypeToSchema(t.X)
	case *ast.ArrayType:
		item := a.goTypeToSchema(t.Elt)
		return models.Schema{Type: "array", Items: &item}
	case *ast.MapType:
		return models.Schema{Type: "object", AdditionalProperties: map[string]interface{}{}}
	case *ast.SelectorExpr:
		// e.g. time.Time
		if ident, ok := t.X.(*ast.Ident); ok {
			if ident.Name == "time" && t.Sel.Name == "Time" {
				return models.Schema{Type: "string", Format: "date-time"}
			}
		}
		return models.Schema{Type: "object"}
	case *ast.InterfaceType:
		return models.Schema{Type: "object"}
	default:
		return models.Schema{Type: "object"}
	}
}

// addSchemaAndRefsToModels adds the schema and any referenced types to a.models so OpenAPI components/schemas can resolve $ref.
func (a *Analyzer) addSchemaAndRefsToModels(name string, s models.Schema) {
	a.models[name] = s
	if s.Ref != "" {
		refName := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		if refName != "" && refName != name {
			if nested, ok := a.typeRegistry[refName]; ok {
				a.addSchemaAndRefsToModels(refName, nested)
			}
		}
	}
	for _, prop := range s.Properties {
		if prop.Ref != "" {
			refName := strings.TrimPrefix(prop.Ref, "#/components/schemas/")
			if refName != "" {
				if nested, ok := a.typeRegistry[refName]; ok {
					a.addSchemaAndRefsToModels(refName, nested)
				}
			}
		}
	}
	if s.Items != nil {
		if s.Items.Ref != "" {
			refName := strings.TrimPrefix(s.Items.Ref, "#/components/schemas/")
			if refName != "" {
				if nested, ok := a.typeRegistry[refName]; ok {
					a.addSchemaAndRefsToModels(refName, nested)
				}
			}
		}
	}
}

func (a *Analyzer) identToSchema(name string) models.Schema {
	switch name {
	case "string":
		return models.Schema{Type: "string"}
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return models.Schema{Type: "integer", Format: "int64"}
	case "float32", "float64":
		return models.Schema{Type: "number", Format: "double"}
	case "bool":
		return models.Schema{Type: "boolean"}
	case "interface{}":
		return models.Schema{Type: "object"}
	default:
		// Named struct: use ref if in registry, else object
		if _, ok := a.typeRegistry[name]; ok {
			return models.Schema{Ref: "#/components/schemas/" + name}
		}
		return models.Schema{Type: "object"}
	}
}

// getHandlerRequestAndResponseTypes returns the type names for the handler's second param (request body) and first return (response).
func getHandlerRequestAndResponseTypes(file *ast.File, handlerName string) (reqTypeName, respTypeName string) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != handlerName || fd.Type.Params == nil {
			continue
		}
		params := fd.Type.Params.List
		if len(params) >= 2 {
			reqTypeName = typeExprToName(params[1].Type)
		}
		if fd.Type.Results != nil && len(fd.Type.Results.List) >= 1 {
			respTypeName = typeExprToName(fd.Type.Results.List[0].Type)
		}
		return reqTypeName, respTypeName
	}
	return "", ""
}

// localTypeName returns the unqualified type name (e.g. "pkg.CreateRequest" -> "CreateRequest").
func localTypeName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

func typeExprToName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeExprToName(t.X)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
		return ""
	default:
		return ""
	}
}

func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

// looksLikeHandlerName returns true if s looks like a Go handler name (CamelCase, no spaces, no slash).
func looksLikeHandlerName(s string) bool {
	if s == "" || strings.Contains(s, " ") || strings.Contains(s, "/") {
		return false
	}
	// At least one lower and one upper for CamelCase, or single word
	hasUpper := false
	hasLower := false
	for _, r := range s {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	return hasUpper && (hasLower || len(s) <= 2)
}

// humanizeHandlerName turns a handler name like "CreateProduct" into "Create product".
func humanizeHandlerName(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// parseFile parses a Go file and extracts route information
func (a *Analyzer) parseFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil // Skip files that can't be parsed
	}

	a.curFilePath = filePath
	// Build route group prefix map and auth groups for this file (Gin/Echo/Fiber only)
	a.curGroupPrefix = nil
	a.curAuthGroups = nil
	if a.framework == models.FrameWorkGin || a.framework == models.FrameWorkEcho || a.framework == models.FrameWorkFiber {
		a.curGroupPrefix = a.buildGinGroupPrefixes(node)
		a.curAuthGroups = a.buildGinAuthGroups(node)
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

// buildGinGroupPrefixes builds a map of variable name -> path prefix from
// Group() calls (e.g. v1 := r.Group("/api/v1")) so we can emit full paths.
func (a *Analyzer) buildGinGroupPrefixes(file *ast.File) map[string]string {
	type link struct{ child, parent, path string }
	var links []link
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Group" || len(call.Args) != 1 {
			return true
		}
		pathLit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || pathLit.Kind != token.STRING {
			return true
		}
		path := strings.Trim(pathLit.Value, `"`)
		childIdent, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		parentIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		links = append(links, link{childIdent.Name, parentIdent.Name, path})
		return true
	})
	prefix := make(map[string]string)
	for {
		changed := false
		for _, l := range links {
			parentPrefix := prefix[l.parent] // empty if root
			full := joinPath(parentPrefix, l.path)
			if prefix[l.child] != full {
				prefix[l.child] = full
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return prefix
}

func joinPath(prefix, path string) string {
	prefix = strings.Trim(prefix, "/")
	path = strings.Trim(path, "/")
	if prefix == "" {
		if path == "" {
			return "/"
		}
		return "/" + path
	}
	if path == "" {
		return "/" + prefix
	}
	return "/" + prefix + "/" + path
}

// buildGinAuthGroups returns variable names for route groups that use auth-like middleware (.Use(Auth()), .Use(JWT()), etc.).
func (a *Analyzer) buildGinAuthGroups(file *ast.File) map[string]bool {
	authGroups := make(map[string]bool)
	authNames := map[string]bool{
		"Auth": true, "JWT": true, "JWTAuth": true, "AuthRequired": true,
		"MiddlewareAuth": true, "AuthMiddleware": true, "RequireAuth": true,
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Use" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// First arg to Use() is the middleware (e.g. Auth(), JWT())
		var name string
		switch arg := call.Args[0].(type) {
		case *ast.Ident:
			name = arg.Name
		case *ast.CallExpr:
			if c, ok := arg.Fun.(*ast.Ident); ok {
				name = c.Name
			}
		}
		if authNames[name] || strings.Contains(strings.ToLower(name), "auth") || strings.Contains(strings.ToLower(name), "jwt") {
			authGroups[ident.Name] = true
		}
		return true
	})
	return authGroups
}

// findBindingTypeName scans a handler function body for common JSON-binding
// calls and returns the unqualified type name bound from the request body, or "".
//
// Recognized patterns (Gin / Echo / Fiber / stdlib):
//
//	c.ShouldBindJSON(&req) / c.BindJSON(&req) / c.ShouldBind(&req) / c.Bind(&req)
//	c.BodyParser(&req) / c.BodyParser(req)
//	json.NewDecoder(r.Body).Decode(&req) / json.Unmarshal(body, &req)
func findBindingTypeName(file *ast.File, funcName string) string {
	bindMethods := map[string]bool{
		"ShouldBindJSON": true, "BindJSON": true,
		"ShouldBind": true, "Bind": true, "BodyParser": true,
		"Decode": true, "Unmarshal": true,
	}

	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == funcName && fd.Body != nil {
			body = fd.Body
			break
		}
	}
	if body == nil {
		return ""
	}

	// variable name → declared type name within this function
	varTypes := make(map[string]string)
	var result string

	ast.Inspect(body, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		switch s := n.(type) {
		// var req LoginRequest
		case *ast.GenDecl:
			if s.Tok == token.VAR {
				for _, spec := range s.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || vs.Type == nil {
						continue
					}
					name := typeExprToName(vs.Type)
					for _, id := range vs.Names {
						varTypes[id.Name] = name
					}
				}
			}
		// req := LoginRequest{} / req := &LoginRequest{} / req := new(LoginRequest)
		case *ast.AssignStmt:
			for i, rhs := range s.Rhs {
				if i >= len(s.Lhs) {
					break
				}
				lhs, ok := s.Lhs[i].(*ast.Ident)
				if !ok {
					continue
				}
				switch expr := rhs.(type) {
				case *ast.CompositeLit:
					if expr.Type != nil {
						varTypes[lhs.Name] = typeExprToName(expr.Type)
					}
				case *ast.UnaryExpr:
					if expr.Op == token.AND {
						if cl, ok := expr.X.(*ast.CompositeLit); ok && cl.Type != nil {
							varTypes[lhs.Name] = typeExprToName(cl.Type)
						}
					}
				case *ast.CallExpr:
					if id, ok := expr.Fun.(*ast.Ident); ok && id.Name == "new" && len(expr.Args) == 1 {
						varTypes[lhs.Name] = typeExprToName(expr.Args[0])
					}
				}
			}
		// c.ShouldBindJSON(&req) / c.Bind(req) / json.Decode(&req) …
		case *ast.CallExpr:
			sel, ok := s.Fun.(*ast.SelectorExpr)
			if !ok || !bindMethods[sel.Sel.Name] || len(s.Args) == 0 {
				break
			}
			last := s.Args[len(s.Args)-1]
			var varName string
			switch a := last.(type) {
			case *ast.UnaryExpr:
				if a.Op == token.AND {
					if id, ok := a.X.(*ast.Ident); ok {
						varName = id.Name
					}
				}
			case *ast.Ident:
				varName = a.Name
			}
			if varName != "" && varTypes[varName] != "" {
				result = localTypeName(varTypes[varName])
			}
		}
		return true
	})
	return result
}

// resolveHandlerSourceFiles sets SourceFile for endpoints that have HandlerPackage and HandlerName
// by finding the .go file that defines that function (e.g. controllers.CreateUser -> controllers/user_controller.go).
func (a *Analyzer) resolveHandlerSourceFiles() {
	for i := range a.endpoints {
		ep := &a.endpoints[i]
		if ep.HandlerName == "" || ep.SourceFile != "" {
			continue
		}
		if ep.HandlerPackage == "" {
			continue
		}
		filePath := findFileWithFunction(a.config.ProjectPath, a.config.Exclude, ep.HandlerPackage, ep.HandlerName)
		if filePath != "" {
			ep.SourceFile = filePath
			// For cross-package handlers the function signature is just func(c *gin.Context),
			// so we can't get the body type from params. Scan the body for binding calls instead.
			if ep.RequestBody == nil {
				fset := token.NewFileSet()
				node, err := parser.ParseFile(fset, filePath, nil, 0)
				if err == nil {
					if typName := findBindingTypeName(node, ep.HandlerName); typName != "" {
						if schema, ok := a.typeRegistry[typName]; ok {
							ep.RequestTypeName = typName
							a.addSchemaAndRefsToModels(typName, schema)
							ep.RequestBody = &models.RequestBody{
								Required: true,
								Content: map[string]models.Content{
									"application/json": {Schema: schema},
								},
							}
						}
					}
				}
			}
		}
	}
}

// resolveRemainingRequestBodies is a final pass that fills request body schemas
// for endpoints that still have none after the per-file and cross-package passes.
// It handles the common case of same-package, different-file handlers: routes like
// r.POST("/products", Create) where Create is in a sibling file of the same package.
func (a *Analyzer) resolveRemainingRequestBodies() {
	for i := range a.endpoints {
		ep := &a.endpoints[i]
		if ep.RequestBody != nil || ep.HandlerName == "" {
			continue
		}
		if ep.Method != "POST" && ep.Method != "PUT" && ep.Method != "PATCH" {
			continue
		}

		// 1. Try the already-known source file first (cheapest).
		if ep.SourceFile != "" {
			if a.extractBodyFromFile(ep, ep.SourceFile) {
				continue
			}
		}

		// 2. Walk the project looking for a file that defines the handler.
		//    Use a fast string pre-filter to avoid parsing every .go file.
		_ = filepath.Walk(a.config.ProjectPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || ep.RequestBody != nil {
				return nil
			}
			if info.IsDir() {
				for _, ex := range a.config.Exclude {
					if strings.Contains(path, ex) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || path == ep.SourceFile {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil || !strings.Contains(string(raw), "func "+ep.HandlerName) {
				return nil
			}
			if a.extractBodyFromFile(ep, path) {
				return errStopWalk
			}
			return nil
		})
	}
}

// extractBodyFromFile parses filePath, finds ep.HandlerName, scans its body for
// binding calls, and populates ep.RequestBody when a known type is resolved.
// Returns true if a schema was successfully attached.
func (a *Analyzer) extractBodyFromFile(ep *models.Endpoint, filePath string) bool {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		return false
	}
	typName := findBindingTypeName(node, ep.HandlerName)
	if typName == "" {
		return false
	}
	schema, ok := a.typeRegistry[typName]
	if !ok {
		return false
	}
	ep.RequestTypeName = typName
	a.addSchemaAndRefsToModels(typName, schema)
	ep.RequestBody = &models.RequestBody{
		Required: true,
		Content: map[string]models.Content{
			"application/json": {Schema: schema},
		},
	}
	return true
}

// findFileWithFunction returns the path of a .go file that declares package matching pkgName (or in dir pkgName) and defines func funcName.
func findFileWithFunction(projectPath string, exclude []string, pkgName, funcName string) string {
	var found string
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			for _, ex := range exclude {
				if strings.Contains(path, ex) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}
		pkgDecl := node.Name.Name
		dirName := filepath.Base(filepath.Dir(path))
		// Match package name (e.g. "controllers") or directory name (e.g. .../controllers/...)
		if pkgDecl != pkgName && dirName != pkgName {
			return nil
		}
		for _, decl := range node.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != funcName {
				continue
			}
			found = path
			return errStopWalk
		}
		return nil
	})
	return found
}

// tagFromPath returns a tag (e.g. "products", "users") from the path for OpenAPI grouping.
// Uses the first path segment that looks like a resource name (skips "api", "v1", "v2", params like ":id").
func tagFromPath(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if p == "" || p == "api" || strings.HasPrefix(p, "v") && len(p) <= 3 || strings.HasPrefix(p, ":") || strings.HasPrefix(p, "{") {
			continue
		}
		return p
	}
	return ""
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

	// Prepend group prefix if receiver is a variable (e.g. products.GET("/", ...))
	if a.curGroupPrefix != nil {
		if ident, ok := selExpr.X.(*ast.Ident); ok && ident.Name != "" {
			if p := a.curGroupPrefix[ident.Name]; p != "" {
				path = joinPath(p, path)
			}
		}
	}

	// Tag from path for Swagger grouping (e.g. /api/v1/products -> "products")
	var tags []string
	if tag := tagFromPath(path); tag != "" {
		tags = []string{tag}
	}

	// Mark protected routes when this group uses auth middleware
	var security []map[string][]string
	if a.curAuthGroups != nil {
		if ident, ok := selExpr.X.(*ast.Ident); ok && a.curAuthGroups[ident.Name] {
			security = []map[string][]string{{"BearerAuth": {}}}
		}
	}

	endpoint := models.Endpoint{
		Path:        path,
		Method:      method,
		Summary:     fmt.Sprintf("%s %s", method, path),
		Tags:        tags,
		Security:    security,
		Parameters:  extractPathParams(path),
		Responses:   make(map[int]models.Response),
	}

	// Extract handler: always use the last argument so middleware chains like
	// r.GET("/path", authMiddleware, handler) resolve to the real handler.
	var handlerName, handlerPkg string
	if len(callExpr.Args) >= 2 {
		switch arg := callExpr.Args[len(callExpr.Args)-1].(type) {
		case *ast.Ident:
			handlerName = arg.Name
			handlerPkg = ""
		case *ast.SelectorExpr:
			if pkgIdent, ok := arg.X.(*ast.Ident); ok {
				handlerPkg = pkgIdent.Name
				handlerName = arg.Sel.Name
			}
		}
		if handlerName != "" {
			endpoint.Summary = handlerName
			// Same-file handler: get comments and request/response types from current file
			if handlerPkg == "" {
				a.extractHandlerComments(file, handlerName, &endpoint)
				reqTypeName, respTypeName := getHandlerRequestAndResponseTypes(file, handlerName)
				// Standard Gin/Echo/Fiber handlers have func(c *gin.Context) — no typed body
				// param — so fall back to scanning the body for binding calls.
				if reqTypeName == "" {
					reqTypeName = findBindingTypeName(file, handlerName)
				}
				if reqTypeName != "" {
					reqTypeName = localTypeName(reqTypeName)
					if schema, ok := a.typeRegistry[reqTypeName]; ok {
						endpoint.RequestTypeName = reqTypeName
						a.addSchemaAndRefsToModels(reqTypeName, schema)
						endpoint.RequestBody = &models.RequestBody{
							Required: true,
							Content: map[string]models.Content{
								"application/json": {Schema: schema},
							},
						}
					}
				}
				if respTypeName != "" {
					respTypeName = localTypeName(respTypeName)
					if schema, ok := a.typeRegistry[respTypeName]; ok {
						endpoint.ResponseTypeName = respTypeName
						a.addSchemaAndRefsToModels(respTypeName, schema)
						endpoint.Responses[200] = models.Response{
							Description: "Successful response",
							Content: map[string]models.Content{
								"application/json": {Schema: schema},
							},
						}
					}
				}
			}
		}
	}

	// Add default response if not set from handler return type
	if _, has := endpoint.Responses[200]; !has {
		endpoint.Responses[200] = models.Response{
			Description: "Successful response",
			Content: map[string]models.Content{
				"application/json": {
					Schema: models.Schema{Type: "object"},
				},
			},
		}
	}

	// Description/summary fallback when no comment
	if endpoint.Description == "" && handlerName != "" {
		endpoint.Description = humanizeHandlerName(handlerName)
	}
	if endpoint.Summary == handlerName && handlerName != "" {
		endpoint.Summary = humanizeHandlerName(handlerName)
	}

	// For --write-annotations: record handler location (same file or package to resolve later)
	if handlerName != "" {
		endpoint.HandlerName = handlerName
		endpoint.HandlerPackage = handlerPkg
		if handlerPkg == "" && a.curFilePath != "" {
			endpoint.SourceFile = a.curFilePath
		}
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

// parseGenericRoutes attempts to extract routes from unknown frameworks.
// It handles two patterns:
//  1. router.GET("/path", handler) — method-named selectors (framework-agnostic)
//  2. http.HandleFunc("/path", handler) / mux.HandleFunc / mux.Handle — stdlib net/http
func (a *Analyzer) parseGenericRoutes(n ast.Node, file *ast.File) {
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

	// Pattern 1: router.GET("/path", handler)
	if validMethods[method] && len(callExpr.Args) >= 1 {
		if pathLit, ok := callExpr.Args[0].(*ast.BasicLit); ok && pathLit.Kind == token.STRING {
			path := strings.Trim(pathLit.Value, `"`)
			ep := a.newGenericEndpoint(path, method)
			a.extractHandlerComments(file, lastHandlerName(callExpr.Args), ep)
			a.endpoints = append(a.endpoints, *ep)
		}
		return
	}

	// Pattern 2: http.HandleFunc("/path", handler) or mux.Handle("/path", handler)
	// Method is unknown for stdlib registrations; default to GET so the path is visible.
	if (selExpr.Sel.Name == "HandleFunc" || selExpr.Sel.Name == "Handle") && len(callExpr.Args) >= 2 {
		if pathLit, ok := callExpr.Args[0].(*ast.BasicLit); ok && pathLit.Kind == token.STRING {
			path := strings.Trim(pathLit.Value, `"`)
			ep := a.newGenericEndpoint(path, "GET")
			a.extractHandlerComments(file, lastHandlerName(callExpr.Args), ep)
			a.endpoints = append(a.endpoints, *ep)
		}
	}
}

// lastHandlerName returns the function name from the last argument in a route
// call's arg list. This handles middleware chains like r.GET("/p", mid, handler)
// where the real handler is always the final argument.
func lastHandlerName(args []ast.Expr) string {
	for i := len(args) - 1; i >= 0; i-- {
		switch arg := args[i].(type) {
		case *ast.Ident:
			return arg.Name
		case *ast.SelectorExpr:
			return arg.Sel.Name
		}
	}
	return ""
}

// newGenericEndpoint builds a minimal Endpoint with a default 200 response.
func (a *Analyzer) newGenericEndpoint(path, method string) *models.Endpoint {
	var tags []string
	if t := tagFromPath(path); t != "" {
		tags = []string{t}
	}
	ep := &models.Endpoint{
		Path:       path,
		Method:     method,
		Summary:    fmt.Sprintf("%s %s", method, path),
		Tags:       tags,
		Parameters: extractPathParams(path),
		Responses:  make(map[int]models.Response),
	}
	ep.Responses[200] = models.Response{
		Description: "Successful response",
		Content: map[string]models.Content{
			"application/json": {Schema: models.Schema{Type: "object"}},
		},
	}
	return ep
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
