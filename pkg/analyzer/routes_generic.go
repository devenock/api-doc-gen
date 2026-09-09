package analyzer

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

// Fallback route parsing used when the framework wasn't detected/recognized:
// a best-effort ast.Inspect over the whole file for anything that looks like
// a route registration, rather than a framework-specific structured walk.

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
		if path, ok := a.literalStringArg(callExpr.Args[0]); ok {
			if _, isLiteral := callExpr.Args[len(callExpr.Args)-1].(*ast.BasicLit); !isLiteral {
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
		if path, ok := a.literalStringArg(callExpr.Args[0]); ok {
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
