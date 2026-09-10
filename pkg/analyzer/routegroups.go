package analyzer

import (
	"go/ast"
	"go/token"
	"strings"
)

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

var middlewareAuthNames = map[string]bool{
	"Auth": true, "JWT": true, "JWTAuth": true, "AuthRequired": true,
	"MiddlewareAuth": true, "AuthMiddleware": true, "RequireAuth": true,
}

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

	// Propagate auth protection down through nested groups
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
