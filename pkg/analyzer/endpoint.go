package analyzer

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

// This file assembles a single models.Endpoint once its route/method are
// known: resolving the handler's source file, backfilling request/response
// bodies that a first pass left unresolved (a handler can be defined in a
// different file/package than where it's registered), extracting doc
// comments, and deduplicating endpoints registered more than once.

var errStopWalk = errors.New("stop walk")

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
				reqTypeName = a.findBindingTypeName(file, handlerName)
			}
			// Last resort for POST/PUT/PATCH: a locally-declared struct var whose
			// address is taken somewhere in the body, even if we don't recognize
			// the call it's passed to (project-specific bind/validate helpers).
			if reqTypeName == "" && (ep.Method == "POST" || ep.Method == "PUT" || ep.Method == "PATCH") {
				reqTypeName = a.findAddressTakenStructVar(file, handlerName)
			}
			if reqTypeName != "" {
				reqTypeName = localTypeName(reqTypeName)
				if schema, ok, isLocal := a.resolveRequestSchema(file, handlerName, reqTypeName); ok {
					ep.RequestTypeName = reqTypeName
					if !isLocal {
						a.addSchemaAndRefsToModels(reqTypeName, schema)
					}
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
				if schema, ok, isLocal := a.resolveRequestSchema(file, handlerName, respTypeName); ok {
					ep.ResponseTypeName = respTypeName
					if !isLocal {
						a.addSchemaAndRefsToModels(respTypeName, schema)
					}
					ep.Responses[200] = models.Response{
						Description: "Successful response",
						Content: map[string]models.Content{
							"application/json": {Schema: schema},
						},
					}
				}
			}
			// Response inference: walk the handler body for response-emitting
			// calls (review §3). Runs regardless of whether the signature-based
			// respTypeName path above found anything — for context-style
			// Gin/Echo/Fiber/Gorilla/Chi handlers (the overwhelming majority)
			// that path never matches, since these frameworks return no typed
			// value at all.
			for status, resp := range a.extractResponses(file, handlerName) {
				ep.Responses[status] = resp
			}
			ep.Parameters = append(ep.Parameters, extractQueryParams(file, handlerName)...)
		}
		// Cross-package/cross-file handlers (handlerPkg != "") are resolved
		// later by resolveHandlerSourceFiles / resolveRemainingResponses, once
		// their defining file is located. ep.Responses is deliberately left
		// empty here rather than placeholder-filled, so those later passes can
		// tell "not yet resolved" apart from "resolved, no response found".
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
		filePath := a.findFileWithFunction(ep.HandlerPackage, ep.HandlerName)
		if filePath != "" {
			ep.SourceFile = filePath
			// Parse with comments so extractHandlerComments can read doc blocks.
			fset := token.NewFileSet()
			node, err := a.rootParseFile(fset, filePath, parser.ParseComments)
			if err == nil {
				// Extract the doc comment — overrides the humanized-name fallback.
				a.extractHandlerComments(node, ep.HandlerName, ep)

				// Scan the body for JSON-binding calls to get the request body type.
				if ep.RequestBody == nil {
					typName := a.findBindingTypeName(node, ep.HandlerName)
					if typName == "" && (ep.Method == "POST" || ep.Method == "PUT" || ep.Method == "PATCH") {
						typName = a.findAddressTakenStructVar(node, ep.HandlerName)
					}
					if typName != "" {
						if schema, ok, isLocal := a.resolveRequestSchema(node, ep.HandlerName, typName); ok {
							ep.RequestTypeName = typName
							if !isLocal {
								a.addSchemaAndRefsToModels(typName, schema)
							}
							ep.RequestBody = &models.RequestBody{
								Required: true,
								Content: map[string]models.Content{
									"application/json": {Schema: schema},
								},
							}
						}
					}
				}
				if len(ep.Responses) == 0 {
					for status, resp := range a.extractResponses(node, ep.HandlerName) {
						ep.Responses[status] = resp
					}
				}
				ep.Parameters = append(ep.Parameters, extractQueryParams(node, ep.HandlerName)...)
			}
		}
	}
}

// resolveRemainingResponses is the response-side counterpart of
// resolveRemainingRequestBodies: a final pass for endpoints whose response
// is still unresolved after the per-file and cross-package passes — same-
// package, different-file handlers like r.POST("/x", Create) where Create
// lives in a sibling file of the same package.
func (a *Analyzer) resolveRemainingResponses() {
	for i := range a.endpoints {
		ep := &a.endpoints[i]
		if len(ep.Responses) != 0 || ep.HandlerName == "" {
			continue
		}

		if ep.SourceFile != "" {
			fset := token.NewFileSet()
			if node, err := a.rootParseFile(fset, ep.SourceFile, 0); err == nil {
				for status, resp := range a.extractResponses(node, ep.HandlerName) {
					ep.Responses[status] = resp
				}
			}
			if len(ep.Responses) != 0 {
				continue
			}
		}

		_ = a.walkProjectDir(func(path string, d fs.DirEntry) error {
			if len(ep.Responses) != 0 {
				return nil
			}
			if d.IsDir() {
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
			raw, readErr := a.rootReadFile(path)
			if readErr != nil || !strings.Contains(string(raw), " "+ep.HandlerName+"(") {
				return nil
			}
			fset := token.NewFileSet()
			node, err := a.rootParseFile(fset, path, 0)
			if err != nil {
				return nil
			}
			for status, resp := range a.extractResponses(node, ep.HandlerName) {
				ep.Responses[status] = resp
			}
			if len(ep.Responses) != 0 {
				return errStopWalk
			}
			return nil
		})
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
		_ = a.walkProjectDir(func(path string, d fs.DirEntry) error {
			if ep.RequestBody != nil {
				return nil
			}
			if d.IsDir() {
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
			raw, readErr := a.rootReadFile(path)
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
	node, err := a.rootParseFile(fset, filePath, 0)
	if err != nil {
		return false
	}
	typName := a.findBindingTypeName(node, ep.HandlerName)
	if typName == "" {
		// extractBodyFromFile is only ever called for POST/PUT/PATCH endpoints
		// (see resolveRemainingRequestBodies), so the structural fallback is safe here.
		typName = a.findAddressTakenStructVar(node, ep.HandlerName)
	}
	if typName == "" {
		return false
	}
	schema, ok, isLocal := a.resolveRequestSchema(node, ep.HandlerName, typName)
	if !ok {
		return false
	}
	ep.RequestTypeName = typName
	if !isLocal {
		a.addSchemaAndRefsToModels(typName, schema)
	}
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
func (a *Analyzer) findFileWithFunction(pkgName, funcName string) string {
	skipDir := func(path string) bool {
		for _, ex := range a.config.Exclude {
			if filepath.Base(path) == ex {
				return true
			}
		}
		return false
	}

	// Pass 1: match by package declaration or directory name.
	var found string
	_ = a.walkProjectDir(func(path string, d fs.DirEntry) error {
		if d.IsDir() {
			if skipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		fset := token.NewFileSet()
		node, err := a.rootParseFile(fset, path, 0)
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
	_ = a.walkProjectDir(func(path string, d fs.DirEntry) error {
		if found != "" {
			return nil
		}
		if d.IsDir() {
			if skipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := a.rootReadFile(path)
		if readErr != nil || !strings.Contains(string(raw), " "+funcName+"(") {
			return nil
		}
		fset := token.NewFileSet()
		node, err := a.rootParseFile(fset, path, 0)
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

// filterEndpointsByTags keeps only endpoints matching a.config.Tags, when
// set — see the Config.Tags doc comment for the include/exclude convention.
// Runs after tags are fully assigned (the tagFromPath fallback in Analyze),
// so filtering sees the same tags the generated docs will show.
func (a *Analyzer) filterEndpointsByTags(endpoints []models.Endpoint) []models.Endpoint {
	if len(a.config.Tags) == 0 {
		return endpoints
	}
	var include, exclude []string
	for _, f := range a.config.Tags {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if strings.HasPrefix(f, "!") {
			exclude = append(exclude, strings.TrimPrefix(f, "!"))
		} else {
			include = append(include, f)
		}
	}
	hasAny := func(epTags, filterTags []string) bool {
		for _, want := range filterTags {
			for _, epTag := range epTags {
				if epTag == want {
					return true
				}
			}
		}
		return false
	}
	var out []models.Endpoint
	for _, ep := range endpoints {
		if hasAny(ep.Tags, exclude) {
			continue
		}
		if len(include) > 0 && !hasAny(ep.Tags, include) {
			continue
		}
		out = append(out, ep)
	}
	return out
}
