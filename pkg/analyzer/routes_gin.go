package analyzer

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/models"
)

// parseGinRoutes is the shared route-registration parser for Gin, Echo, and
// Fiber: all three expose a `router.METHOD(path, ...handlers)` shape close
// enough that parseEchoRoutes and parseFiberRoutes just delegate to it
// directly rather than duplicating the walk. There is no separate
// echo.go/fiber.go — this is genuinely one implementation, not three.

// ginCasedMethods and fiberCasedMethods list the HTTP-verb method names as
// each framework's router actually spells them: Gin and Echo use Go's
// ALL-CAPS HTTP method constants (GET, POST, ...); Fiber uses Go-idiomatic
// capitalized method names (Get, Post, ...) — the same casing Chi uses,
// though Chi is parsed by a separate, structurally narrower walk
// (walkChiStmts) that isn't exposed to this ambiguity in the first place.
// Matching must be exact-case and framework-aware: case-insensitive matching
// reintroduces false positives from unrelated single-arg calls that happen
// to share a method name with a routing verb — most importantly Fiber's own
// c.Get("X-Header") for reading a request header, which uses the exact same
// casing as a Fiber route registration.
var ginCasedMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}
var fiberCasedMethods = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Delete": true,
	"Patch": true, "Head": true, "Options": true,
}

// parseGinRoutes extracts routes from Gin, Echo, and Fiber (all three share
// this AST shape: router.METHOD("/path", handlers...)).
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

	// A genuine route registration always passes at least one handler after
	// the path, and a handler is always a function reference — never a
	// literal. This is what keeps non-routing calls that share a method name
	// with a routing verb (Fiber's c.Get("X-Header", "default")) from being
	// mistaken for a route now that Fiber's capitalized casing is accepted.
	if len(callExpr.Args) < 2 {
		return
	}
	if _, isLiteral := callExpr.Args[len(callExpr.Args)-1].(*ast.BasicLit); isLiteral {
		return
	}

	// Prepend group prefix if receiver is a tracked group variable, whether a
	// plain local (products.GET(...)) or a struct field (rt.products.GET(...)).
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

	// Extract handler: always use the last argument so middleware chains like
	// r.GET("/path", authMiddleware, handler) resolve to the real handler.
	// The >=2-args check above guarantees this is present and non-literal.
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
