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
	curGroupPrefix map[string]string        // per-file: variable name -> path prefix (Gin/Echo/Fiber Group, Gorilla Subrouter)
	curAuthGroups  map[string]bool          // per-file: variable name -> true if group uses auth middleware
	curFilePath    string                   // current file being parsed (for SourceFile on endpoints)
	consumedCalls  map[*ast.CallExpr]bool   // per-file: gorilla HandleFunc/Handle calls already consumed by a .Methods() chain
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

	return spec, nil
}

// detectServerURL returns the base URL for the API server by scanning the
// project's .env file for a PORT / APP_PORT / SERVER_PORT entry.
// Falls back to http://localhost:8080 when nothing is found.
func detectServerURL(projectPath string) string {
	data, err := os.ReadFile(filepath.Join(projectPath, ".env"))
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
	var embeds []string
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			// Anonymous (embedded) field, e.g. `gorm.Model` or a local `BaseModel`.
			// With an explicit json tag it behaves like a normal nested property;
			// without one, encoding/json promotes its fields to the top level, so
			// record it for the field-promotion post-pass (resolveEmbeddedFields)
			// that runs once every type in the project has been collected —
			// the embedded type may be defined in a file not parsed yet.
			embeddedName := embeddedTypeName(f.Type)
			if embeddedName == "" {
				continue // qualified (cross-package) embed, e.g. gorm.Model — can't resolve locally
			}
			if tagName := jsonFieldTagName(f.Tag); tagName != "" {
				props[tagName] = a.goTypeToSchema(f.Type)
			} else {
				embeds = append(embeds, embeddedName)
			}
			continue
		}
		fieldName := f.Names[0].Name
		if tagName := jsonFieldTagName(f.Tag); tagName != "" {
			fieldName = tagName
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
		Embeds:     embeds,
	}
}

// embeddedTypeName returns the local type name for an embedded field's type
// expression (`Foo` or `*Foo`), or "" for anything not locally resolvable
// (qualified selectors like `gorm.Model` live in another package/module).
func embeddedTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// jsonFieldTagName extracts the json tag name from a struct field's tag, or
// "" if there is none (or it's explicitly "-").
func jsonFieldTagName(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	t := strings.Trim(tag.Value, "`")
	for _, part := range strings.Split(t, " ") {
		if strings.HasPrefix(part, "json:") {
			jsonTag := strings.Trim(strings.TrimPrefix(part, "json:"), `"`)
			if idx := strings.Index(jsonTag, ","); idx >= 0 {
				jsonTag = jsonTag[:idx]
			}
			if jsonTag != "" && jsonTag != "-" {
				return jsonTag
			}
		}
	}
	return ""
}

// resolveEmbeddedFields flattens anonymous (embedded) struct fields recorded
// during collectTypesInFile into their parent schema's Properties, mirroring
// Go's field-promotion behavior for JSON marshaling. Runs after every file in
// the project has been scanned so embedding a type defined in another file
// resolves correctly regardless of file processing order.
func (a *Analyzer) resolveEmbeddedFields() {
	resolved := make(map[string]bool)
	var flatten func(name string) models.Schema
	flatten = func(name string) models.Schema {
		s, ok := a.typeRegistry[name]
		if !ok || resolved[name] || len(s.Embeds) == 0 {
			return s
		}
		resolved[name] = true // guard against embedding cycles
		if s.Properties == nil {
			s.Properties = make(map[string]models.Schema)
		}
		for _, embedded := range s.Embeds {
			parent := flatten(embedded)
			for propName, propSchema := range parent.Properties {
				if _, exists := s.Properties[propName]; !exists {
					s.Properties[propName] = propSchema
				}
			}
			s.Required = append(s.Required, parent.Required...)
		}
		a.typeRegistry[name] = s
		return s
	}
	for name := range a.typeRegistry {
		flatten(name)
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

// groupVarKey returns a stable string identity for a router-group variable
// expression, so both `v1 := r.Group(...)` (plain identifier) and
// `rt.staff = api.Group(...)` / route calls like `rt.staff.POST(...)`
// (struct-field-based, common in DI-style router setups where routers are
// organized as named fields on a struct) can be tracked and looked up by the
// same prefix/auth maps. Returns "" for anything else (e.g. a function call
// result), which callers treat as "no known prefix."
func groupVarKey(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		base := groupVarKey(e.X)
		if base == "" {
			return ""
		}
		return base + "." + e.Sel.Name
	}
	return ""
}

// buildGinGroupPrefixes builds a map of variable name -> path prefix from
// Group() calls (e.g. v1 := r.Group("/api/v1"), or rt.staff = api.Group("/staff"))
// so we can emit full paths.
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
		childKey := groupVarKey(assign.Lhs[0])
		parentKey := groupVarKey(sel.X)
		if childKey == "" || parentKey == "" {
			return true
		}
		links = append(links, link{childKey, parentKey, path})
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
		key := groupVarKey(sel.X)
		if key == "" {
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
			authGroups[key] = true
		}
		return true
	})
	return authGroups
}

// bindMethodHints are substrings (checked case-insensitively against the
// call's package/receiver-qualified name) that mark a call as a JSON
// body-binding call even when it's a project-specific wrapper around the
// standard framework methods (e.g. ShouldBindAndValidate, h.decodeBody,
// utils.ParseJSON). Real codebases very commonly wrap binding in a shared
// helper for consistent error handling, so matching by exact method name
// alone misses a large fraction of real handlers.
var bindMethodHints = []string{"bind", "decode", "unmarshal", "parse"}

// nonBodyBindMethods are framework methods that contain a bind-like hint but
// bind from a source other than the JSON body (query string, URI params,
// headers) and must not be mistaken for request-body binding.
var nonBodyBindMethods = map[string]bool{
	"ShouldBindQuery": true, "BindQuery": true,
	"ShouldBindUri": true, "BindUri": true,
	"ShouldBindHeader": true, "BindHeader": true,
}

// findBindingTypeName scans a handler function body for JSON-binding calls
// and returns the unqualified type name bound from the request body, or "".
//
// Recognized patterns (Gin / Echo / Fiber / stdlib / wrappers / generics):
//
//	c.ShouldBindJSON(&req) / c.BindJSON(&req) / c.ShouldBind(&req) / c.Bind(&req)
//	c.BodyParser(&req) / c.BodyParser(req)
//	json.NewDecoder(r.Body).Decode(&req) / json.Unmarshal(body, &req)
//	h.bindAndValidate(c, &req)          (project-specific wrapper — matched by name hint)
//	req.Bind(c) / req.Validate()        (self-binding request struct — struct is the receiver)
//	bind.JSON[LoginRequest](c, &req)    (explicit generic type argument)
func findBindingTypeName(file *ast.File, funcName string) string {
	return findBindingTypeNameDepth(file, funcName, 0)
}

// findBindingTypeNameDepth is findBindingTypeName's implementation, plus a
// bounded fallback (findDelegatedBindingTypeName) for thin wrapper handlers.
// depth caps delegation-chain recursion so a cycle can't loop forever.
func findBindingTypeNameDepth(file *ast.File, funcName string, depth int) string {
	bindMethods := map[string]bool{
		"ShouldBindJSON": true, "BindJSON": true,
		"ShouldBind": true, "Bind": true, "BodyParser": true,
		"Decode": true, "Unmarshal": true,
	}

	body := findFuncBody(file, funcName)
	if body == nil {
		return ""
	}
	varTypes, _, _, _ := collectLocalTypedVars(body)

	var result string
	ast.Inspect(body, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		exactName := calleeBaseName(call.Fun)
		if exactName == "" {
			exactName = calleeBaseName(genericBaseExpr(call.Fun))
		}
		if exactName != "" && nonBodyBindMethods[exactName] {
			return true
		}
		hintText := calleeHintText(call.Fun)
		if hintText == "" {
			hintText = calleeHintText(genericBaseExpr(call.Fun))
		}
		if !(bindMethods[exactName] || hasBindHint(hintText)) {
			return true
		}

		// Explicit generic type argument takes priority when present:
		// bind[LoginRequest](c) / pkg.Bind[LoginRequest](c, &req)
		if typ := genericTypeArg(call.Fun); typ != "" {
			result = localTypeName(typ)
			return false
		}

		if len(call.Args) == 0 {
			return true
		}

		// Pattern 1: bindJSON(c, &req) / c.ShouldBindJSON(&req) — struct var is an argument.
		last := call.Args[len(call.Args)-1]
		if varName := identOrAddrIdentName(last); varName != "" && varTypes[varName] != "" {
			result = localTypeName(varTypes[varName])
			return false
		}
		// Pattern 2: req.Bind(c) — struct var is the method receiver.
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && varTypes[id.Name] != "" {
				result = localTypeName(varTypes[id.Name])
				return false
			}
		}
		return true
	})
	if result != "" {
		return result
	}
	if depth >= 2 {
		return "" // cap delegation-chain recursion
	}
	return findDelegatedBindingTypeName(file, funcName, depth)
}

// findDelegatedBindingTypeName handles thin wrapper handlers whose entire
// job is delegating to another method on the same receiver, e.g.:
//
//	func (h *MpesaHandler) B2CResult(c *fiber.Ctx) error  { return h.handleB2CCallback(c) }
//	func (h *MpesaHandler) B2CTimeout(c *fiber.Ctx) error { return h.handleB2CCallback(c) }
//
// where the real binding call lives in handleB2CCallback, not in the
// wrapper. Only follows a call whose receiver identifier matches the
// wrapper's own receiver, so it can't wander into an unrelated type's
// same-named method.
func findDelegatedBindingTypeName(file *ast.File, funcName string, depth int) string {
	var fd *ast.FuncDecl
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == funcName && f.Body != nil {
			fd = f
			break
		}
	}
	if fd == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
		return ""
	}
	recvName := fd.Recv.List[0].Names[0].Name
	if recvName == "" {
		return ""
	}

	var delegateFunc string
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if delegateFunc != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == recvName {
			delegateFunc = sel.Sel.Name
		}
		return true
	})
	if delegateFunc == "" || delegateFunc == funcName {
		return ""
	}
	return findBindingTypeNameDepth(file, delegateFunc, depth+1)
}

// findAddressTakenStructVar is a last-resort structural fallback for request
// body detection on POST/PUT/PATCH handlers: it looks for a locally-declared
// variable of a named type whose address is taken somewhere in the function
// body — the overwhelmingly common reason being a call to a project-specific
// bind/validate helper this package can't recognize by name at all (a fluent
// builder, a validation library, a helper named nothing like "bind"). Picks
// the first such variable in declaration order, skipping obvious response
// types so a response DTO built later in the handler isn't mistaken for the
// request body.
func findAddressTakenStructVar(file *ast.File, funcName string) string {
	body := findFuncBody(file, funcName)
	if body == nil {
		return ""
	}
	varTypes, order, addressTaken, typeAsserted := collectLocalTypedVars(body)
	for _, name := range order {
		if !addressTaken[name] && !typeAsserted[name] {
			continue
		}
		typ := localTypeName(varTypes[name])
		if strings.Contains(strings.ToLower(typ), "response") {
			continue
		}
		return typ
	}
	return ""
}

// findLocalStructType looks for a struct type declared LOCALLY inside the
// named function's body (`type req struct {...}` as a statement, not a
// package-level declaration) — a common pattern for small, handler-specific
// request DTOs that don't warrant a dedicated exported type. Local types are
// resolved per-function rather than added to the global type registry
// because their names are not unique across the project — it's idiomatic for
// many unrelated handlers to each independently name their local request
// struct "req".
func (a *Analyzer) findLocalStructType(file *ast.File, funcName, typeName string) (models.Schema, bool) {
	body := findFuncBody(file, funcName)
	if body == nil {
		return models.Schema{}, false
	}
	var found *ast.StructType
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			return true
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != typeName {
				continue
			}
			if st, ok := typeSpec.Type.(*ast.StructType); ok {
				found = st
			}
		}
		return true
	})
	if found == nil {
		return models.Schema{}, false
	}
	return a.buildSchemaFromStruct(found), true
}

// resolveRequestSchema looks up typeName in the global type registry first
// (package-level types, resolvable across files), then falls back to a type
// declared locally inside funcName's own body. See findLocalStructType.
func (a *Analyzer) resolveRequestSchema(file *ast.File, funcName, typeName string) (models.Schema, bool) {
	if schema, ok := a.typeRegistry[typeName]; ok {
		return schema, true
	}
	return a.findLocalStructType(file, funcName, typeName)
}

// findFuncBody returns the body of the top-level function or method named
// funcName in file, or nil if not found.
func findFuncBody(file *ast.File, funcName string) *ast.BlockStmt {
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == funcName && fd.Body != nil {
			return fd.Body
		}
	}
	return nil
}

// collectLocalTypedVars walks body and returns every local variable declared
// with a named type (`var x T`, `x := T{}`, `x := &T{}`, `x := new(T)`,
// `x := v.(T)`), its declaration order, the set of variable names whose
// address is taken (`&x`) anywhere in the body, and the set of variable
// names obtained via a type assertion (`x := v.(*T)` / `x, ok := v.(*T)`) —
// the latter is how frameworks that bind the request body in middleware and
// hand it to the handler through a context value typically surface it
// (e.g. `req := c.MustGet("body").(*CreateRequest)`).
func collectLocalTypedVars(body *ast.BlockStmt) (varTypes map[string]string, order []string, addressTaken map[string]bool, typeAsserted map[string]bool) {
	varTypes = make(map[string]string)
	addressTaken = make(map[string]bool)
	typeAsserted = make(map[string]bool)
	recordVar := func(name, typ string) {
		if typ == "" {
			return
		}
		if _, seen := varTypes[name]; !seen {
			order = append(order, name)
		}
		varTypes[name] = typ
	}
	ast.Inspect(body, func(n ast.Node) bool {
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
						recordVar(id.Name, name)
					}
				}
			}
		// req := LoginRequest{} / req := &LoginRequest{} / req := new(LoginRequest) / req := v.(*LoginRequest)
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
						recordVar(lhs.Name, typeExprToName(expr.Type))
					}
				case *ast.UnaryExpr:
					if expr.Op == token.AND {
						if cl, ok := expr.X.(*ast.CompositeLit); ok && cl.Type != nil {
							recordVar(lhs.Name, typeExprToName(cl.Type))
						}
					}
				case *ast.CallExpr:
					if id, ok := expr.Fun.(*ast.Ident); ok && id.Name == "new" && len(expr.Args) == 1 {
						recordVar(lhs.Name, typeExprToName(expr.Args[0]))
					}
				case *ast.TypeAssertExpr:
					if expr.Type != nil {
						recordVar(lhs.Name, typeExprToName(expr.Type))
						typeAsserted[lhs.Name] = true
					}
				}
			}
		case *ast.UnaryExpr:
			if s.Op == token.AND {
				if id, ok := s.X.(*ast.Ident); ok {
					addressTaken[id.Name] = true
				}
			}
		}
		return true
	})
	return
}

// identOrAddrIdentName returns the identifier name from `&x` or a plain `x`
// expression, or "" for anything else.
func identOrAddrIdentName(e ast.Expr) string {
	switch a := e.(type) {
	case *ast.UnaryExpr:
		if a.Op == token.AND {
			if id, ok := a.X.(*ast.Ident); ok {
				return id.Name
			}
		}
	case *ast.Ident:
		return a.Name
	}
	return ""
}

// calleeBaseName returns the exact method/function name being called
// (`Sel.Name` for a selector, the identifier itself for a plain call), used
// for matching against the known bindMethods whitelist.
func calleeBaseName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

// calleeHintText returns a lowercase-friendly "qualifier.name" (or just
// "name") string for hint-based matching, so a package/receiver qualifier
// like "bind" in `bind.JSON(...)` still counts even though the method name
// itself ("JSON") doesn't contain a bind-like hint.
func calleeHintText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name + "." + x.Sel.Name
		}
		return x.Sel.Name
	}
	return ""
}

// hasBindHint reports whether text (as produced by calleeHintText) looks
// like a binding/decoding call by name even though it isn't one of the known
// framework methods.
func hasBindHint(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, hint := range bindMethodHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// genericBaseExpr returns the callee expression underneath an explicit
// generic type-argument list (the `bind` in `bind[T](...)`), or nil if fun
// isn't a generic instantiation.
func genericBaseExpr(fun ast.Expr) ast.Expr {
	switch f := fun.(type) {
	case *ast.IndexExpr:
		return f.X
	case *ast.IndexListExpr:
		return f.X
	}
	return nil
}

// genericTypeArg extracts the first explicit generic type argument from a
// call's function expression, e.g. the "LoginRequest" in bind[LoginRequest]
// or bind.JSON[LoginRequest]. Returns "" when fun has no type arguments.
func genericTypeArg(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.IndexExpr:
		return typeExprToName(f.Index)
	case *ast.IndexListExpr:
		if len(f.Indices) > 0 {
			return typeExprToName(f.Indices[0])
		}
	}
	return ""
}

// extractQueryParams scans a handler function body for query-parameter reads
// and returns them as optional query Parameters. Supports Gin/Fiber-style
// c.Query/DefaultQuery, Echo-style c.QueryParam, and the stdlib/Gorilla/Chi
// r.URL.Query().Get("name") pattern.
func extractQueryParams(file *ast.File, funcName string) []models.Parameter {
	queryMethods := map[string]bool{
		"Query": true, "DefaultQuery": true, "QueryParam": true,
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
		return nil
	}

	seen := make(map[string]bool)
	var params []models.Parameter
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		var name string
		switch {
		case queryMethods[sel.Sel.Name]:
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				name = strings.Trim(lit.Value, `"`)
			}
		case sel.Sel.Name == "Get":
			// r.URL.Query().Get("name") / req.URL.Query().Get("name")
			inner, ok := sel.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			innerSel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok || innerSel.Sel.Name != "Query" {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				name = strings.Trim(lit.Value, `"`)
			}
		}

		if name != "" && !seen[name] {
			seen[name] = true
			params = append(params, models.Parameter{
				Name:     name,
				In:       "query",
				Required: false,
				Schema:   models.Schema{Type: "string"},
			})
		}
		return true
	})
	return params
}

// finishEndpoint fills in handler-derived fields for an endpoint whose
// Path/Method/Tags/Security/Parameters are already set: comments, request/response
// schemas (resolved from the handler's signature or body-binding calls), query
// parameters, summary/description fallbacks, and HandlerName/Package/SourceFile
// (used by --write-annotations). handlerArg is the route call's handler argument
// (an *ast.Ident for same-file handlers, or *ast.SelectorExpr like controllers.Create
// for cross-package handlers, which only get HandlerName/HandlerPackage recorded here).
func (a *Analyzer) finishEndpoint(ep *models.Endpoint, handlerArg ast.Expr, file *ast.File) {
	var handlerName, handlerPkg string
	switch h := handlerArg.(type) {
	case *ast.Ident:
		handlerName = h.Name
	case *ast.SelectorExpr:
		if pkgIdent, ok := h.X.(*ast.Ident); ok {
			// Single-level: handlers.CreateUser  or  userHandler.CreateUser
			handlerPkg = pkgIdent.Name
			handlerName = h.Sel.Name
		} else if _, ok := h.X.(*ast.SelectorExpr); ok {
			// Multi-level: r.auth.Register, s.user.Create, etc.
			// Common in dependency-injection style routing where a struct field
			// holds the handler group. Extract the method name and use "_" as a
			// sentinel package so resolveHandlerSourceFiles falls through to the
			// function-name-only fallback in findFileWithFunction.
			handlerName = h.Sel.Name
			handlerPkg = "_"
		}
	}

	if handlerName != "" {
		ep.Summary = handlerName
		// Same-file handler: get comments and request/response types from current file
		if handlerPkg == "" {
			a.extractHandlerComments(file, handlerName, ep)
			reqTypeName, respTypeName := getHandlerRequestAndResponseTypes(file, handlerName)
			// Standard Gin/Echo/Fiber handlers have func(c *gin.Context) — no typed body
			// param — so fall back to scanning the body for binding calls.
			if reqTypeName == "" {
				reqTypeName = findBindingTypeName(file, handlerName)
			}
			// Last resort for POST/PUT/PATCH: a locally-declared struct var whose
			// address is taken somewhere in the body, even if we don't recognize
			// the call it's passed to (project-specific bind/validate helpers).
			if reqTypeName == "" && (ep.Method == "POST" || ep.Method == "PUT" || ep.Method == "PATCH") {
				reqTypeName = findAddressTakenStructVar(file, handlerName)
			}
			if reqTypeName != "" {
				reqTypeName = localTypeName(reqTypeName)
				if schema, ok := a.resolveRequestSchema(file, handlerName, reqTypeName); ok {
					ep.RequestTypeName = reqTypeName
					a.addSchemaAndRefsToModels(reqTypeName, schema)
					ep.RequestBody = &models.RequestBody{
						Required: true,
						Content: map[string]models.Content{
							"application/json": {Schema: schema},
						},
					}
				}
			}
			if respTypeName != "" {
				respTypeName = localTypeName(respTypeName)
				if schema, ok := a.resolveRequestSchema(file, handlerName, respTypeName); ok {
					ep.ResponseTypeName = respTypeName
					a.addSchemaAndRefsToModels(respTypeName, schema)
					ep.Responses[200] = models.Response{
						Description: "Successful response",
						Content: map[string]models.Content{
							"application/json": {Schema: schema},
						},
					}
				}
			}
			ep.Parameters = append(ep.Parameters, extractQueryParams(file, handlerName)...)
		}
	}

	// Add default response if not set from handler return type
	if _, has := ep.Responses[200]; !has {
		ep.Responses[200] = models.Response{
			Description: "Successful response",
			Content: map[string]models.Content{
				"application/json": {
					Schema: models.Schema{Type: "object"},
				},
			},
		}
	}

	// Description/summary fallback when no comment
	if ep.Description == "" && handlerName != "" {
		ep.Description = humanizeHandlerName(handlerName)
	}
	if ep.Summary == handlerName && handlerName != "" {
		ep.Summary = humanizeHandlerName(handlerName)
	}

	// For --write-annotations: record handler location (same file or package to resolve later)
	if handlerName != "" {
		ep.HandlerName = handlerName
		ep.HandlerPackage = handlerPkg
		if handlerPkg == "" && a.curFilePath != "" {
			ep.SourceFile = a.curFilePath
		}
	}
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
			// Parse with comments so extractHandlerComments can read doc blocks.
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
			if err == nil {
				// Extract the doc comment — overrides the humanized-name fallback.
				a.extractHandlerComments(node, ep.HandlerName, ep)

				// Scan the body for JSON-binding calls to get the request body type.
				if ep.RequestBody == nil {
					typName := findBindingTypeName(node, ep.HandlerName)
					if typName == "" && (ep.Method == "POST" || ep.Method == "PUT" || ep.Method == "PATCH") {
						typName = findAddressTakenStructVar(node, ep.HandlerName)
					}
					if typName != "" {
						if schema, ok := a.resolveRequestSchema(node, ep.HandlerName, typName); ok {
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
				ep.Parameters = append(ep.Parameters, extractQueryParams(node, ep.HandlerName)...)
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
					if filepath.Base(path) == ex {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || path == ep.SourceFile {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			content := string(raw)
			// Match both standalone functions ("func CreateUser(") and method
			// receivers ("func (h *Handler) CreateUser(") by checking for the
			// function name preceded by a space and followed by "(".
			if !strings.Contains(content, " "+ep.HandlerName+"(") {
				return nil
			}
			if a.extractBodyFromFile(ep, path) {
				// Always update SourceFile to the file that actually contains the
				// handler — the previous value may have been the router file (set
				// as a placeholder when handlerPkg was unknown).
				ep.SourceFile = path
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
		// extractBodyFromFile is only ever called for POST/PUT/PATCH endpoints
		// (see resolveRemainingRequestBodies), so the structural fallback is safe here.
		typName = findAddressTakenStructVar(node, ep.HandlerName)
	}
	if typName == "" {
		return false
	}
	schema, ok := a.resolveRequestSchema(node, ep.HandlerName, typName)
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
// When the package-name match finds nothing (pkgName may be a variable/instance name, not a package), it falls back to
// searching the whole project for any file that defines funcName — handling patterns like "userHandler.CreateUser"
// where "userHandler" is a struct instance, not a package.
func findFileWithFunction(projectPath string, exclude []string, pkgName, funcName string) string {
	skipDir := func(path string) bool {
		for _, ex := range exclude {
			if filepath.Base(path) == ex {
				return true
			}
		}
		return false
	}

	// Pass 1: match by package declaration or directory name.
	var found string
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDir(path) {
				return filepath.SkipDir
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
	if found != "" {
		return found
	}

	// Pass 2: pkgName may be a variable/instance — search all files by function name using a fast text pre-filter.
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if info.IsDir() {
			if skipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil || !strings.Contains(string(raw), " "+funcName+"(") {
			return nil
		}
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
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

// ginCasedMethods and fiberCasedMethods list the HTTP-verb method names as
// each framework's router actually spells them: Gin and Echo use Go's
// ALL-CAPS HTTP method constants (GET, POST, ...); Fiber uses Go-idiomatic
// capitalized method names (Get, Post, ...) — the same casing Chi uses,
// though Chi is parsed by a separate, structurally narrower walk
// (walkChiStmts) that isn't exposed to this ambiguity in the first place.
// Matching must be exact-case and framework-aware: case-insensitive matching
// reintroduces false positives from unrelated single-arg calls that happen
// to share a method name with a routing verb — most importantly Fiber's own
// c.Get("X-Header") for reading a request header, which uses the exact same
// casing as a Fiber route registration.
var ginCasedMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}
var fiberCasedMethods = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Delete": true,
	"Patch": true, "Head": true, "Options": true,
}

// parseGinRoutes extracts routes from Gin, Echo, and Fiber (all three share
// this AST shape: router.METHOD("/path", handlers...)).
func (a *Analyzer) parseGinRoutes(n ast.Node, file *ast.File) {
	callExpr, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	validMethods := ginCasedMethods
	if a.framework == models.FrameWorkFiber {
		validMethods = fiberCasedMethods
	}
	method := selExpr.Sel.Name
	if !validMethods[method] {
		return
	}
	method = strings.ToUpper(method) // normalize for the stored Endpoint.Method / spec output

	// Extract path
	if len(callExpr.Args) < 1 {
		return
	}

	pathLit, ok := callExpr.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}

	// A genuine route registration always passes at least one handler after
	// the path, and a handler is always a function reference — never a
	// literal. This is what keeps non-routing calls that share a method name
	// with a routing verb (Fiber's c.Get("X-Header", "default")) from being
	// mistaken for a route now that Fiber's capitalized casing is accepted.
	if len(callExpr.Args) < 2 {
		return
	}
	if _, isLiteral := callExpr.Args[len(callExpr.Args)-1].(*ast.BasicLit); isLiteral {
		return
	}

	path := strings.Trim(pathLit.Value, `"`)

	// Prepend group prefix if receiver is a tracked group variable, whether a
	// plain local (products.GET(...)) or a struct field (rt.products.GET(...)).
	if a.curGroupPrefix != nil {
		if key := groupVarKey(selExpr.X); key != "" {
			if p := a.curGroupPrefix[key]; p != "" {
				path = joinPath(p, path)
			}
		}
	}
	path = normalizeColonPath(path)

	// Tag from path for Swagger grouping (e.g. /api/v1/products -> "products")
	var tags []string
	if tag := tagFromPath(path); tag != "" {
		tags = []string{tag}
	}

	// Mark protected routes when this group uses auth middleware
	var security []map[string][]string
	if a.curAuthGroups != nil {
		if key := groupVarKey(selExpr.X); key != "" && a.curAuthGroups[key] {
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
	// The >=2-args check above guarantees this is present and non-literal.
	handlerArg := callExpr.Args[len(callExpr.Args)-1]
	a.finishEndpoint(&endpoint, handlerArg, file)

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

// buildGorillaSubrouterPrefixes builds a map of variable name -> path prefix
// from gorilla/mux subrouter chains (e.g. api := r.PathPrefix("/api/v1").Subrouter())
// so nested routes resolve to their full path, mirroring buildGinGroupPrefixes.
func (a *Analyzer) buildGorillaSubrouterPrefixes(file *ast.File) map[string]string {
	type link struct{ child, parent, path string }
	var links []link
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		outer, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		outerSel, ok := outer.Fun.(*ast.SelectorExpr)
		if !ok || outerSel.Sel.Name != "Subrouter" {
			return true
		}
		inner, ok := outerSel.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		innerSel, ok := inner.Fun.(*ast.SelectorExpr)
		if !ok || innerSel.Sel.Name != "PathPrefix" || len(inner.Args) != 1 {
			return true
		}
		pathLit, ok := inner.Args[0].(*ast.BasicLit)
		if !ok || pathLit.Kind != token.STRING {
			return true
		}
		path := strings.Trim(pathLit.Value, `"`)
		childKey := groupVarKey(assign.Lhs[0])
		parentKey := groupVarKey(innerSel.X)
		if childKey == "" || parentKey == "" {
			return true
		}
		links = append(links, link{childKey, parentKey, path})
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

// gorillaMethodValidity lists the HTTP methods recognized in .Methods() chains.
var gorillaValidMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

// buildGorillaEndpoints builds one rich endpoint per HTTP method for a gorilla/mux
// route registration, extracting the same handler comments/request body/query
// params/auth that parseGinRoutes extracts for Gin/Echo/Fiber/Chi.
func (a *Analyzer) buildGorillaEndpoints(path string, methods []string, handlerArg ast.Expr, file *ast.File, receiverName string) []models.Endpoint {
	path = normalizeBracePath(path)
	if a.curGroupPrefix != nil && receiverName != "" {
		if p := a.curGroupPrefix[receiverName]; p != "" {
			path = joinPath(p, path)
		}
	}

	var tags []string
	if tag := tagFromPath(path); tag != "" {
		tags = []string{tag}
	}

	var security []map[string][]string
	if a.curAuthGroups != nil && a.curAuthGroups[receiverName] {
		security = []map[string][]string{{"BearerAuth": {}}}
	}

	endpoints := make([]models.Endpoint, 0, len(methods))
	for _, method := range methods {
		ep := models.Endpoint{
			Path:       path,
			Method:     method,
			Summary:    fmt.Sprintf("%s %s", method, path),
			Tags:       tags,
			Security:   security,
			Parameters: extractPathParams(path),
			Responses:  make(map[int]models.Response),
		}
		a.finishEndpoint(&ep, handlerArg, file)
		endpoints = append(endpoints, ep)
	}
	return endpoints
}

// parseGorillaRoutes extracts routes from gorilla/mux HandleFunc/Handle calls
// that are not wrapped in a .Methods() chain (parseGorillaMethods handles
// those via a.consumedCalls so the same registration isn't added twice).
func (a *Analyzer) parseGorillaRoutes(n ast.Node, file *ast.File) {
	callExpr, ok := n.(*ast.CallExpr)
	if !ok || a.consumedCalls[callExpr] {
		return
	}

	selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	if selExpr.Sel.Name != "HandleFunc" && selExpr.Sel.Name != "Handle" {
		return
	}

	if len(callExpr.Args) < 2 {
		return
	}

	pathLit, ok := callExpr.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}
	path := strings.Trim(pathLit.Value, `"`)

	receiverName := groupVarKey(selExpr.X)

	// No .Methods() chain: gorilla/mux matches any HTTP method on this route.
	// We record it as GET so the path is visible; if a .Methods() call exists
	// it will have already marked this call as consumed and we won't get here.
	handlerArg := callExpr.Args[len(callExpr.Args)-1]
	a.endpoints = append(a.endpoints, a.buildGorillaEndpoints(path, []string{"GET"}, handlerArg, file, receiverName)...)
}

// parseGorillaMethods handles .Methods("POST", ...) chained after HandleFunc/Handle.
// It marks the inner HandleFunc/Handle call as consumed (via a.consumedCalls) so
// parseGorillaRoutes skips it when ast.Inspect visits that node directly —
// otherwise both functions would add an endpoint for the same registration
// (one with the correct method, one with the wrong default GET).
func (a *Analyzer) parseGorillaMethods(n ast.Node, file *ast.File) {
	callExpr, ok := n.(*ast.CallExpr)
	if !ok || len(callExpr.Args) == 0 {
		return
	}
	sel, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Methods" {
		return
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok || len(inner.Args) < 2 {
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

	// httpMethodConsts maps the net/http package-level constant names to their
	// string values so that .Methods(http.MethodPost) is handled the same way
	// as .Methods("POST").
	httpMethodConsts := map[string]string{
		"MethodGet": "GET", "MethodPost": "POST", "MethodPut": "PUT",
		"MethodDelete": "DELETE", "MethodPatch": "PATCH",
		"MethodHead": "HEAD", "MethodOptions": "OPTIONS",
	}

	var methods []string
	for _, arg := range callExpr.Args {
		var method string
		switch v := arg.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				method = strings.ToUpper(strings.Trim(v.Value, `"`))
			}
		case *ast.SelectorExpr:
			// e.g. http.MethodPost
			if mapped, ok := httpMethodConsts[v.Sel.Name]; ok {
				method = mapped
			}
		}
		if method != "" && gorillaValidMethods[method] {
			methods = append(methods, method)
		}
	}
	if len(methods) == 0 {
		return
	}

	if a.consumedCalls == nil {
		a.consumedCalls = make(map[*ast.CallExpr]bool)
	}
	a.consumedCalls[inner] = true

	receiverName := groupVarKey(innerSel.X)
	handlerArg := inner.Args[len(inner.Args)-1]
	a.endpoints = append(a.endpoints, a.buildGorillaEndpoints(path, methods, handlerArg, file, receiverName)...)
}

// chiValidMethods are the HTTP-verb methods chi.Router exposes (Go-cased: Get, Post, ...).
var chiValidMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

// isChiAuthMiddlewareCall reports whether a r.Use(...) call's first argument
// looks like an auth middleware, using the same name heuristic as buildGinAuthGroups.
func isChiAuthMiddlewareCall(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	var name string
	switch arg := call.Args[0].(type) {
	case *ast.Ident:
		name = arg.Name
	case *ast.CallExpr:
		if c, ok := arg.Fun.(*ast.Ident); ok {
			name = c.Name
		}
	}
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "auth") || strings.Contains(lower, "jwt")
}

// walkChiStmts recursively extracts routes from a chi.Router setup, tracking
// the path prefix and inherited auth state through nested r.Route(...)/r.Group(...)
// closures. Chi reuses the same receiver identifier (conventionally "r") at every
// nesting level, so this can't be modeled with a flat var->prefix map the way
// Gin's r.Group() assignments are (buildGinGroupPrefixes) — each closure's body
// has to be walked with its own prefix/auth context instead.
func (a *Analyzer) walkChiStmts(stmts []ast.Stmt, file *ast.File, prefix string, inheritedAuth bool) {
	// A .Use(...) call anywhere in this scope protects every route registered
	// in this scope, per chi's middleware-before-routes convention.
	scopeAuth := inheritedAuth
	for _, stmt := range stmts {
		call := chiCallFromStmt(stmt)
		if call == nil {
			continue
		}
		sel := call.Fun.(*ast.SelectorExpr)
		if sel.Sel.Name == "Use" && isChiAuthMiddlewareCall(call) {
			scopeAuth = true
		}
	}

	for _, stmt := range stmts {
		call := chiCallFromStmt(stmt)
		if call == nil {
			continue
		}
		sel := call.Fun.(*ast.SelectorExpr)

		switch {
		case chiValidMethods[strings.ToUpper(sel.Sel.Name)]:
			a.buildChiEndpoint(call, file, prefix, scopeAuth)
		case sel.Sel.Name == "Route" && len(call.Args) == 2:
			pathLit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || pathLit.Kind != token.STRING {
				continue
			}
			lit, ok := call.Args[1].(*ast.FuncLit)
			if !ok || lit.Body == nil {
				continue
			}
			subPath := joinPath(prefix, strings.Trim(pathLit.Value, `"`))
			a.walkChiStmts(lit.Body.List, file, subPath, scopeAuth)
		case sel.Sel.Name == "Group" && len(call.Args) == 1:
			lit, ok := call.Args[0].(*ast.FuncLit)
			if !ok || lit.Body == nil {
				continue
			}
			a.walkChiStmts(lit.Body.List, file, prefix, scopeAuth)
		}
	}
}

// chiCallFromStmt returns the CallExpr for a statement like `r.Get("/", h)`
// (an ExprStmt wrapping a CallExpr whose Fun is a method selector), or nil.
func chiCallFromStmt(stmt ast.Stmt) *ast.CallExpr {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if _, ok := call.Fun.(*ast.SelectorExpr); !ok {
		return nil
	}
	return call
}

// buildChiEndpoint builds a rich endpoint (comments, request/response schema,
// query params, tags) for a single chi.Router HTTP-verb call, mirroring the
// extraction parseGinRoutes does for Gin/Echo/Fiber.
func (a *Analyzer) buildChiEndpoint(call *ast.CallExpr, file *ast.File, prefix string, auth bool) {
	if len(call.Args) < 2 {
		return
	}
	pathLit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}
	path := normalizeBracePath(joinPath(prefix, strings.Trim(pathLit.Value, `"`)))
	method := strings.ToUpper(call.Fun.(*ast.SelectorExpr).Sel.Name)

	var tags []string
	if tag := tagFromPath(path); tag != "" {
		tags = []string{tag}
	}
	var security []map[string][]string
	if auth {
		security = []map[string][]string{{"BearerAuth": {}}}
	}

	ep := models.Endpoint{
		Path:       path,
		Method:     method,
		Summary:    fmt.Sprintf("%s %s", method, path),
		Tags:       tags,
		Security:   security,
		Parameters: extractPathParams(path),
		Responses:  make(map[int]models.Response),
	}
	handlerArg := call.Args[len(call.Args)-1]
	a.finishEndpoint(&ep, handlerArg, file)
	a.endpoints = append(a.endpoints, ep)
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

	// Framework is unknown here, so accept either casing convention
	// (ginCasedMethods for Gin/Echo, fiberCasedMethods for Fiber) — but still
	// require a real handler (>=2 args, last arg not a literal) so unrelated
	// single-arg/literal-arg calls like header or context-value lookups
	// aren't fabricated into endpoints.
	method := selExpr.Sel.Name
	isRouteMethod := ginCasedMethods[method] || fiberCasedMethods[method]

	// Pattern 1: router.GET("/path", handler) / router.Get("/path", handler)
	if isRouteMethod && len(callExpr.Args) >= 2 {
		if pathLit, ok := callExpr.Args[0].(*ast.BasicLit); ok && pathLit.Kind == token.STRING {
			if _, isLiteral := callExpr.Args[len(callExpr.Args)-1].(*ast.BasicLit); !isLiteral {
				path := strings.Trim(pathLit.Value, `"`)
				ep := a.newGenericEndpoint(path, strings.ToUpper(method))
				a.extractHandlerComments(file, lastHandlerName(callExpr.Args), ep)
				a.endpoints = append(a.endpoints, *ep)
			}
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
			var docLines []string
			for _, comment := range funcDecl.Doc.List {
				text := strings.TrimPrefix(comment.Text, "//")
				text = strings.TrimSpace(text)
				if text != "" {
					docLines = append(docLines, text)
				}
			}
			if len(docLines) > 0 {
				// Summary = first comment line (the short one-liner above the func).
				endpoint.Summary = docLines[0]
				// Description = all lines joined — gives full context in the spec.
				endpoint.Description = strings.Join(docLines, " ")
			}
		}
		break
	}
}

// gorillaTypedVarRe matches Gorilla's regex-constrained path vars, e.g.
// {id:[0-9]+} -> captures "id" so callers can normalize to {id}.
var gorillaTypedVarRe = regexp.MustCompile(`\{([^:{}]+):[^{}]+\}`)

// normalizeBracePath rewrites Gorilla's {param:pattern} segments to plain
// {param} so the path is a valid OpenAPI path template (and so Swagger UI's
// "Try it out" can substitute the parameter correctly).
func normalizeBracePath(path string) string {
	return gorillaTypedVarRe.ReplaceAllString(path, "{$1}")
}

// normalizeColonPath converts Gin/Echo/Fiber colon-style path params (:id)
// to OpenAPI brace style ({id}) so the stored path is spec-compliant.
func normalizeColonPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// extractPathParams extracts path parameters from route path.
// Supports :param (Gin, Echo, Fiber) and {param} (Chi, Gorilla — callers
// should normalize Gorilla's {param:pattern} form via normalizeBracePath
// before calling this, so the colon isn't mistaken for :param syntax).
func extractPathParams(path string) []models.Parameter {
	var params []models.Parameter
	// :param style (e.g. /users/:id). Braced segments are stripped first so a
	// colon inside {id:pattern} (before normalization) is never mistaken for this.
	braceStripped := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "")
	colonRe := regexp.MustCompile(`:([^/]+)`)
	for _, name := range colonRe.FindAllStringSubmatch(braceStripped, -1) {
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
			paramName := name[1]
			if idx := strings.Index(paramName, ":"); idx >= 0 {
				paramName = paramName[:idx]
			}
			params = append(params, models.Parameter{
				Name:     paramName,
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
