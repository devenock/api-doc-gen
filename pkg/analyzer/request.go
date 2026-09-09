package analyzer

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

// This file resolves a handler's request-body type: finding the
// Bind/ShouldBindJSON/BodyParser/Decode-style call in a handler body (or a
// helper it delegates to), and extracting query parameters. Framework
// differences here are just method-name tables (bindMethodHints,
// nonBodyBindMethods) checked against arbitrary call expressions, not
// separate parsers per framework.

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
func (a *Analyzer) findBindingTypeName(file *ast.File, funcName string) string {
	return a.findBindingTypeNameDepth(file, funcName, 0)
}

// findBindingTypeNameDepth is findBindingTypeName's implementation, plus a
// bounded fallback (findDelegatedBindingTypeName) for thin wrapper handlers.
// depth caps delegation-chain recursion so a cycle can't loop forever.
func (a *Analyzer) findBindingTypeNameDepth(file *ast.File, funcName string, depth int) string {
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
		if !bindMethods[exactName] {
			if !hasBindHint(hintText) {
				return true
			}
			if a.config.Verbose {
				a.recordBindHintMatch(hintText)
			}
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
	return a.findDelegatedBindingTypeName(file, funcName, depth)
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
func (a *Analyzer) findDelegatedBindingTypeName(file *ast.File, funcName string, depth int) string {
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
	return a.findBindingTypeNameDepth(file, delegateFunc, depth+1)
}

// responseBodyIdentNames returns the names of local identifiers used as the
// body argument of a response-emitting call in fd's body — e.g. the "resp"
// in `resp := &UserResponse{...}; c.JSON(200, resp)`, or the "user" in
// `json.NewEncoder(w).Encode(user)`. Used by findAddressTakenStructVar to
// exclude an address-taken variable that is actually the response, not the
// request — precisely, from the same call recognition response inference
// (review §3) already does, rather than guessing from the variable's type
// name the way the pre-§3 heuristic had to.
func (a *Analyzer) responseBodyIdentNames(fd *ast.FuncDecl, recvName string) map[string]bool {
	names := make(map[string]bool)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var bodyExpr ast.Expr
		if a.framework == models.FrameWorkGorilla || a.framework == models.FrameWorkChi {
			bodyExpr, _ = matchJSONEncode(call, recvName)
		} else if rc, ok := a.recognizeResponseCall(call, recvName, nil); ok && rc.hasBody {
			bodyExpr = rc.bodyExpr
		}
		if id, ok := bodyExpr.(*ast.Ident); ok {
			names[id.Name] = true
		}
		return true
	})
	return names
}

// findAddressTakenStructVar is a last-resort structural fallback for request
// body detection on POST/PUT/PATCH handlers: it looks for a locally-declared
// variable of a named type whose address is taken somewhere in the function
// body — the overwhelmingly common reason being a call to a project-specific
// bind/validate helper this package can't recognize by name at all (a fluent
// builder, a validation library, a helper named nothing like "bind"). Picks
// the first such variable in declaration order, excluding any variable
// already identified as a response body (responseBodyIdentNames) so a
// response DTO built later in the handler isn't mistaken for the request
// body.
func (a *Analyzer) findAddressTakenStructVar(file *ast.File, funcName string) string {
	fd := findFuncDecl(file, funcName)
	if fd == nil || fd.Body == nil {
		return ""
	}
	varTypes, order, addressTaken, typeAsserted := collectLocalTypedVars(fd.Body)

	var excluded map[string]bool
	if recvName := firstParamName(fd); recvName != "" {
		excluded = a.responseBodyIdentNames(fd, recvName)
	}

	for _, name := range order {
		if !addressTaken[name] && !typeAsserted[name] {
			continue
		}
		if excluded[name] {
			continue
		}
		return localTypeName(varTypes[name])
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
// The third return value reports whether the match came from a local
// (function-scoped) declaration — callers must not register those in the
// project-wide component-schema map (addSchemaAndRefsToModels), since local
// type names are not unique across the project (many handlers independently
// name their request struct "req"); doing so would silently and
// non-deterministically overwrite one handler's schema with another's under
// a shared, misleading name in the published spec.
func (a *Analyzer) resolveRequestSchema(file *ast.File, funcName, typeName string) (schema models.Schema, ok bool, isLocal bool) {
	if schema, ok := a.typeRegistry[typeName]; ok {
		return schema, true, false
	}
	localSchema, localOk := a.findLocalStructType(file, funcName, typeName)
	return localSchema, localOk, true
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
