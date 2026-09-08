package analyzer

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/models"
)

// Chi route parsing. Structurally narrower than the other route families:
// walkChiStmts walks a router's setup statements directly (r.Route/r.Group/
// r.Use/r.Get-style calls) rather than an unscoped ast.Inspect over the
// whole file, so it tracks nesting and inherited auth without needing the
// group-variable bookkeeping in routegroups.go.

// chiValidMethods are the HTTP-verb methods chi.Router exposes (Go-cased: Get, Post, ...).
var chiValidMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

// isChiAuthMiddlewareCall reports whether a r.Use(...) call's first argument
// looks like an auth middleware, using the same name heuristic as buildGinAuthGroups.
func (a *Analyzer) isChiAuthMiddlewareCall(call *ast.CallExpr) bool {
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
	return a.middlewareLooksLikeAuth(name)
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
		if sel.Sel.Name == "Use" && a.isChiAuthMiddlewareCall(call) {
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
			pathVal, ok := a.literalStringArg(call.Args[0])
			if !ok {
				continue
			}
			lit, ok := call.Args[1].(*ast.FuncLit)
			if !ok || lit.Body == nil {
				continue
			}
			subPath := joinPath(prefix, pathVal)
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
	pathVal, ok := a.literalStringArg(call.Args[0])
	if !ok {
		return
	}
	path := normalizeBracePath(joinPath(prefix, pathVal))
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
