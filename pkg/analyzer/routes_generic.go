package analyzer

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

func (a *Analyzer) parseGenericRoutes(n ast.Node, file *ast.File) {
	callExpr, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

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

	if (selExpr.Sel.Name == "HandleFunc" || selExpr.Sel.Name == "Handle") && len(callExpr.Args) >= 2 {
		if path, ok := a.literalStringArg(callExpr.Args[0]); ok {
			ep := a.newGenericEndpoint(path, "GET")
			a.extractHandlerComments(file, lastHandlerName(callExpr.Args), ep)
			a.endpoints = append(a.endpoints, *ep)
		}
	}
}

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
