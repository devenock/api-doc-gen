package analyzer

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

var chiValidMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

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

func (a *Analyzer) walkChiStmts(stmts []ast.Stmt, file *ast.File, prefix string, inheritedAuth bool) {

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
