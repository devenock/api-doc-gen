package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

// Gorilla/mux route parsing: subrouter/prefix tracking, .Methods() chains,
// and net/http-style handlers (see walkNetHTTPResponses in response.go).

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
		path, ok := a.literalStringArg(inner.Args[0])
		if !ok {
			return true
		}
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

	path, ok := a.literalStringArg(callExpr.Args[0])
	if !ok {
		return
	}

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
	path, ok := a.literalStringArg(inner.Args[0])
	if !ok {
		return
	}

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
