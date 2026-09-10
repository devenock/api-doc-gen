package analyzer

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

var ginCasedMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}
var fiberCasedMethods = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Delete": true,
	"Patch": true, "Head": true, "Options": true,
}

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

	path, ok := a.literalStringArg(callExpr.Args[0])
	if !ok {
		return
	}

	if len(callExpr.Args) < 2 {
		return
	}
	if _, isLiteral := callExpr.Args[len(callExpr.Args)-1].(*ast.BasicLit); isLiteral {
		return
	}

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
		Path:       path,
		Method:     method,
		Summary:    fmt.Sprintf("%s %s", method, path),
		Tags:       tags,
		Security:   security,
		Parameters: extractPathParams(path),
		Responses:  make(map[int]models.Response),
	}

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
