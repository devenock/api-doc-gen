package analyzer

import (
	"go/ast"
	"go/token"
	"strings"
)

// Group-prefix and auth-group tracking shared across route-family parsers:
// Gin/Echo/Fiber's r.Group(...) (buildGinGroupPrefixes, buildGinAuthGroups)
// as well as Gorilla's Subrouter and Chi's Route (which reuse groupVarKey
// and joinPath directly from their own files). Despite the "Gin" names,
// these are framework-agnostic — Echo and Fiber share the exact same
// grouping shape, so there was never a separate implementation to split out.

// collectStringConsts records package-level const/var declarations whose
// value is a single string literal (const apiPrefix = "/api/v1"), collected
// during pass 1 (collectTypesInFile) before any route parsing runs, so
// literalStringArg can resolve a path/prefix argument passed by name in
// pass 2 - a pattern real projects commonly use for route paths and version
// prefixes instead of inline literals. Grouped declarations where the
// number of names and values don't match (e.g. `const (a = iota; b)`) are
// skipped: there's no literal value to record for those.
func (a *Analyzer) collectStringConsts(genDecl *ast.GenDecl) {
	for _, spec := range genDecl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok || len(valueSpec.Names) != len(valueSpec.Values) {
			continue
		}
		for i, name := range valueSpec.Names {
			lit, ok := valueSpec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if a.stringConsts == nil {
				a.stringConsts = make(map[string]string)
			}
			a.stringConsts[name.Name] = strings.Trim(lit.Value, `"`)
		}
	}
}

// literalStringArg resolves expr to a string value: either a direct string
// literal, or a bare identifier referencing a package-level const/var
// collected by collectStringConsts. Every route-family parser uses this
// (instead of a bare *ast.BasicLit type-assertion) for the path/prefix
// argument of a route or group-registration call, so `const apiPrefix =
// "/api/v1"; v1 := r.Group(apiPrefix)` resolves the same as an inline
// `r.Group("/api/v1")` rather than silently losing the prefix (or, for a
// single route registration, dropping the endpoint entirely).
func (a *Analyzer) literalStringArg(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		return strings.Trim(e.Value, `"`), true
	case *ast.Ident:
		v, ok := a.stringConsts[e.Name]
		return v, ok
	}
	return "", false
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
		// Group() takes the path plus optional variadic middleware in both Gin
		// (Group(path string, handlers ...HandlerFunc)) and Fiber (Group(prefix
		// string, handlers ...Handler)) — admin := app.Group("/admin", authMW) is
		// extremely common, so only the path (first arg) is required, not exactly one.
		if !ok || sel.Sel.Name != "Group" || len(call.Args) < 1 {
			return true
		}
		path, ok := a.literalStringArg(call.Args[0])
		if !ok {
			return true
		}
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

// middlewareAuthNames are exact identifier names always treated as
// auth-like middleware, on top of the "auth"/"jwt" substring heuristic —
// the built-in default used when config.AuthMiddleware is unset.
var middlewareAuthNames = map[string]bool{
	"Auth": true, "JWT": true, "JWTAuth": true, "AuthRequired": true,
	"MiddlewareAuth": true, "AuthMiddleware": true, "RequireAuth": true,
}

// middlewareLooksLikeAuth reports whether a middleware identifier name
// should be treated as protecting its route(s) with authentication, and (in
// --verbose mode) records the match so a user can see exactly what was
// guessed rather than trusting a silent heuristic (review §5.2).
//
// When config.AuthMiddleware is set, name must exactly match one of those
// entries (case-insensitive) — precise and user-controlled, no guessing at
// all. Otherwise falls back to the built-in heuristic (middlewareAuthNames
// plus a substring match on "auth"/"jwt"), unchanged from before this
// became configurable, so a project that hasn't touched the new option
// sees no behavior change.
func (a *Analyzer) middlewareLooksLikeAuth(name string) bool {
	if name == "" {
		return false
	}
	var matched bool
	if len(a.config.AuthMiddleware) > 0 {
		for _, configured := range a.config.AuthMiddleware {
			if strings.EqualFold(configured, name) {
				matched = true
				break
			}
		}
	} else {
		lower := strings.ToLower(name)
		matched = middlewareAuthNames[name] || strings.Contains(lower, "auth") || strings.Contains(lower, "jwt")
	}
	if matched && a.config.Verbose {
		a.recordAuthMatch(name)
	}
	return matched
}

// recordAuthMatch appends name to authMatches, deduplicated.
func (a *Analyzer) recordAuthMatch(name string) {
	for _, seen := range a.authMatches {
		if seen == name {
			return
		}
	}
	a.authMatches = append(a.authMatches, name)
}

// buildGinAuthGroups returns variable names for route groups that use auth-like middleware (.Use(Auth()), .Use(JWT()), etc.).
func (a *Analyzer) buildGinAuthGroups(file *ast.File) map[string]bool {
	// looksLikeAuth checks a middleware argument expression by name, however
	// it's referenced: Auth (bare ident), Auth() (called), or c.AuthMW (a
	// method/field value passed without invocation — common when middleware
	// lives on a DI container, e.g. app.Group("/admin", c.AuthMW, ...)).
	looksLikeAuth := func(arg ast.Expr) bool {
		var name string
		switch e := arg.(type) {
		case *ast.Ident:
			name = e.Name
		case *ast.SelectorExpr:
			name = e.Sel.Name
		case *ast.CallExpr:
			switch f := e.Fun.(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
			}
		}
		return a.middlewareLooksLikeAuth(name)
	}

	authGroups := make(map[string]bool)
	type link struct{ child, parent string }
	var links []link

	ast.Inspect(file, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.CallExpr:
			// group.Use(authMiddleware) — middleware attached after the group exists.
			sel, ok := stmt.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Use" || len(stmt.Args) == 0 {
				return true
			}
			key := groupVarKey(sel.X)
			if key != "" && looksLikeAuth(stmt.Args[0]) {
				authGroups[key] = true
			}
		case *ast.AssignStmt:
			// child := parent.Group("/path", authMiddleware, ...) — middleware
			// passed inline at group creation, which both Gin's and Fiber's
			// Group() accept as variadic trailing args.
			if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
				return true
			}
			call, ok := stmt.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || len(call.Args) < 1 {
				return true
			}
			childKey := groupVarKey(stmt.Lhs[0])
			parentKey := groupVarKey(sel.X)
			if childKey == "" || parentKey == "" {
				return true
			}
			links = append(links, link{childKey, parentKey})
			for _, arg := range call.Args[1:] {
				if looksLikeAuth(arg) {
					authGroups[childKey] = true
					break
				}
			}
		}
		return true
	})

	// Propagate auth protection down through nested groups — middleware set
	// on a parent group applies to every route registered on its sub-groups
	// too, in both Gin and Fiber.
	for {
		changed := false
		for _, l := range links {
			if authGroups[l.parent] && !authGroups[l.child] {
				authGroups[l.child] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return authGroups
}
