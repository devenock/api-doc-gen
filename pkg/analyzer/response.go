package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"net/http"
	"strconv"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

// ---- Response inference ----
//
// This is the response-side counterpart of the request-binding inference in
// request.go (findBindingTypeName et al.): same AST-walking approach, same
// reliance on collectLocalTypedVars for identifier types, same depth-limited
// delegation following. Unlike the request side, a handler can legitimately
// emit more than one response (a success path and one or more error paths),
// so extractResponses returns every distinct status code it finds rather
// than a single type name. Per-framework response-call recognition
// (recognizeGinResponseCall/recognizeEchoResponseCall/
// recognizeFiberResponseCall) also lives here, alongside the net/http-style
// walkNetHTTPResponses used by Gorilla/Chi/generic handlers.

// responseCall is a normalized, framework-agnostic description of a single
// response-emitting call found in a handler body.
type responseCall struct {
	status   int
	hasBody  bool
	isJSON   bool // false for String/Data/XML/Blob/SendString — still worth a response entry, just no inferred schema
	bodyExpr ast.Expr
}

// Per-framework response-emitting method tables (review §3, Step 1).
var (
	ginJSONResponseMethods = map[string]bool{
		"JSON": true, "IndentedJSON": true, "PureJSON": true,
		"SecureJSON": true, "AsciiJSON": true, "AbortWithStatusJSON": true,
	}
	ginNonJSONResponseMethods  = map[string]bool{"String": true, "Data": true, "XML": true}
	ginBodylessResponseMethods = map[string]bool{"Status": true, "AbortWithStatus": true}

	echoJSONResponseMethods    = map[string]bool{"JSON": true, "JSONPretty": true}
	echoNonJSONResponseMethods = map[string]bool{"String": true, "Blob": true}
	echoBodylessResponseMethod = "NoContent"
)

// httpStatusConstants maps net/http's exported Status* identifiers to their
// integer values, so `http.StatusCreated` resolves the same as a literal 201
// without needing full type information (go/types — see review §4).
var httpStatusConstants = map[string]int{
	"StatusContinue": 100, "StatusSwitchingProtocols": 101, "StatusProcessing": 102,
	"StatusOK": 200, "StatusCreated": 201, "StatusAccepted": 202,
	"StatusNonAuthoritativeInfo": 203, "StatusNoContent": 204, "StatusResetContent": 205,
	"StatusPartialContent": 206, "StatusMultiStatus": 207, "StatusAlreadyReported": 208,
	"StatusIMUsed":          226,
	"StatusMultipleChoices": 300, "StatusMovedPermanently": 301, "StatusFound": 302,
	"StatusSeeOther": 303, "StatusNotModified": 304, "StatusUseProxy": 305,
	"StatusTemporaryRedirect": 307, "StatusPermanentRedirect": 308,
	"StatusBadRequest": 400, "StatusUnauthorized": 401, "StatusPaymentRequired": 402,
	"StatusForbidden": 403, "StatusNotFound": 404, "StatusMethodNotAllowed": 405,
	"StatusNotAcceptable": 406, "StatusProxyAuthRequired": 407, "StatusRequestTimeout": 408,
	"StatusConflict": 409, "StatusGone": 410, "StatusLengthRequired": 411,
	"StatusPreconditionFailed": 412, "StatusRequestEntityTooLarge": 413,
	"StatusRequestURITooLong": 414, "StatusUnsupportedMediaType": 415,
	"StatusRequestedRangeNotSatisfiable": 416, "StatusExpectationFailed": 417,
	"StatusTeapot": 418, "StatusMisdirectedRequest": 421, "StatusUnprocessableEntity": 422,
	"StatusLocked": 423, "StatusFailedDependency": 424, "StatusTooEarly": 425,
	"StatusUpgradeRequired": 426, "StatusPreconditionRequired": 428,
	"StatusTooManyRequests": 429, "StatusRequestHeaderFieldsTooLarge": 431,
	"StatusUnavailableForLegalReasons": 451,
	"StatusInternalServerError":        500, "StatusNotImplemented": 501, "StatusBadGateway": 502,
	"StatusServiceUnavailable": 503, "StatusGatewayTimeout": 504,
	"StatusHTTPVersionNotSupported": 505, "StatusVariantAlsoNegotiates": 506,
	"StatusInsufficientStorage": 507, "StatusLoopDetected": 508,
	"StatusNotExtended": 510, "StatusNetworkAuthenticationRequired": 511,
}

// statusDescription returns a short human-readable description for a status
// code, used as the OpenAPI response's required "description" field.
func statusDescription(status int) string {
	if status >= 200 && status < 300 {
		return "Successful response"
	}
	if text := http.StatusText(status); text != "" {
		return text
	}
	return fmt.Sprintf("Response %d", status)
}

// resolveStatusCode resolves a call argument to an HTTP status code (review
// §3, Step 2): either an integer literal (c.JSON(201, ...)) or a
// net/http Status* constant (c.JSON(http.StatusCreated, ...)).
func resolveStatusCode(expr ast.Expr) (int, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.INT {
			return 0, false
		}
		n, err := strconv.Atoi(e.Value)
		if err != nil {
			return 0, false
		}
		return n, true
	case *ast.SelectorExpr:
		pkgIdent, ok := e.X.(*ast.Ident)
		// fiber re-exports the same Status* constants (identical values) as
		// its own idiomatic convenience aliases — fiber.StatusCreated is as
		// common in real Fiber code as http.StatusCreated.
		if !ok || (pkgIdent.Name != "http" && pkgIdent.Name != "fiber") {
			return 0, false
		}
		code, ok := httpStatusConstants[e.Sel.Name]
		return code, ok
	}
	return 0, false
}

// substArg substitutes expr with its mapped replacement when expr is a bare
// identifier matching a helper function's parameter name — see
// resolveHelperDelegate. subst is nil on a direct (non-delegated) call, in
// which case substitution is a no-op.
func substArg(expr ast.Expr, subst map[string]ast.Expr) ast.Expr {
	if subst == nil {
		return expr
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		return expr
	}
	if replacement, ok := subst[id.Name]; ok {
		return replacement
	}
	return expr
}

// recognizeResponseCall inspects call and, if it matches a known
// response-emitting method for the analyzer's configured framework, returns
// the normalized call info and true. recvName is the handler's own context
// parameter name (e.g. "c") — required so an unrelated foo.JSON(...) call
// elsewhere in the body is never mistaken for a response.
func (a *Analyzer) recognizeResponseCall(call *ast.CallExpr, recvName string, subst map[string]ast.Expr) (responseCall, bool) {
	switch a.framework {
	case models.FrameWorkGin:
		return recognizeGinResponseCall(call, recvName, subst)
	case models.FrameWorkEcho:
		return recognizeEchoResponseCall(call, recvName, subst)
	case models.FrameWorkFiber:
		return recognizeFiberResponseCall(call, recvName, subst)
	}
	return responseCall{}, false
}

func recognizeGinResponseCall(call *ast.CallExpr, recvName string, subst map[string]ast.Expr) (responseCall, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return responseCall{}, false
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok || recv.Name != recvName {
		return responseCall{}, false
	}
	method := sel.Sel.Name

	switch {
	case ginJSONResponseMethods[method]:
		if len(call.Args) < 2 {
			return responseCall{}, false
		}
		status, ok := resolveStatusCode(substArg(call.Args[0], subst))
		if !ok {
			return responseCall{}, false
		}
		return responseCall{status: status, hasBody: true, isJSON: true, bodyExpr: substArg(call.Args[1], subst)}, true
	case ginNonJSONResponseMethods[method]:
		if len(call.Args) < 1 {
			return responseCall{}, false
		}
		status, ok := resolveStatusCode(substArg(call.Args[0], subst))
		if !ok {
			return responseCall{}, false
		}
		return responseCall{status: status, hasBody: true, isJSON: false}, true
	case ginBodylessResponseMethods[method]:
		if len(call.Args) < 1 {
			return responseCall{}, false
		}
		status, ok := resolveStatusCode(substArg(call.Args[0], subst))
		if !ok {
			return responseCall{}, false
		}
		return responseCall{status: status, hasBody: false}, true
	}
	return responseCall{}, false
}

func recognizeEchoResponseCall(call *ast.CallExpr, recvName string, subst map[string]ast.Expr) (responseCall, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return responseCall{}, false
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok || recv.Name != recvName {
		return responseCall{}, false
	}
	method := sel.Sel.Name

	switch {
	case echoJSONResponseMethods[method]:
		if len(call.Args) < 2 {
			return responseCall{}, false
		}
		status, ok := resolveStatusCode(substArg(call.Args[0], subst))
		if !ok {
			return responseCall{}, false
		}
		return responseCall{status: status, hasBody: true, isJSON: true, bodyExpr: substArg(call.Args[1], subst)}, true
	case echoNonJSONResponseMethods[method]:
		if len(call.Args) < 1 {
			return responseCall{}, false
		}
		status, ok := resolveStatusCode(substArg(call.Args[0], subst))
		if !ok {
			return responseCall{}, false
		}
		return responseCall{status: status, hasBody: true, isJSON: false}, true
	case method == echoBodylessResponseMethod:
		if len(call.Args) < 1 {
			return responseCall{}, false
		}
		status, ok := resolveStatusCode(substArg(call.Args[0], subst))
		if !ok {
			return responseCall{}, false
		}
		return responseCall{status: status, hasBody: false}, true
	}
	return responseCall{}, false
}

// recognizeFiberResponseCall handles both Fiber shapes: a direct call
// (c.JSON(obj), status implicitly 200) and the chained form
// (c.Status(code).JSON(obj)) where the status is set by a preceding call
// whose result is immediately re-selected on.
func recognizeFiberResponseCall(call *ast.CallExpr, recvName string, subst map[string]ast.Expr) (responseCall, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return responseCall{}, false
	}
	method := sel.Sel.Name

	if recv, ok := sel.X.(*ast.Ident); ok && recv.Name == recvName {
		switch method {
		case "JSON":
			if len(call.Args) < 1 {
				return responseCall{}, false
			}
			return responseCall{status: 200, hasBody: true, isJSON: true, bodyExpr: substArg(call.Args[0], subst)}, true
		case "SendString":
			return responseCall{status: 200, hasBody: true, isJSON: false}, true
		case "SendStatus":
			if len(call.Args) < 1 {
				return responseCall{}, false
			}
			status, ok := resolveStatusCode(substArg(call.Args[0], subst))
			if !ok {
				return responseCall{}, false
			}
			return responseCall{status: status, hasBody: false}, true
		}
		return responseCall{}, false
	}

	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return responseCall{}, false
	}
	innerSel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || innerSel.Sel.Name != "Status" {
		return responseCall{}, false
	}
	innerRecv, ok := innerSel.X.(*ast.Ident)
	if !ok || innerRecv.Name != recvName || len(inner.Args) < 1 {
		return responseCall{}, false
	}
	status, ok := resolveStatusCode(substArg(inner.Args[0], subst))
	if !ok {
		return responseCall{}, false
	}
	switch method {
	case "JSON":
		if len(call.Args) < 1 {
			return responseCall{}, false
		}
		return responseCall{status: status, hasBody: true, isJSON: true, bodyExpr: substArg(call.Args[0], subst)}, true
	case "SendString":
		return responseCall{status: status, hasBody: true, isJSON: false}, true
	}
	return responseCall{}, false
}

// isMapLikeLiteralType reports whether t is a raw map type (map[string]any{})
// or a well-known framework convenience alias for one: gin.H or fiber.Map.
func isMapLikeLiteralType(t ast.Expr) bool {
	switch e := t.(type) {
	case *ast.MapType:
		return true
	case *ast.SelectorExpr:
		return e.Sel.Name == "H" || e.Sel.Name == "Map"
	}
	return false
}

// schemaFromMapLiteralKeys builds an inline object schema from a map-literal
// response body's keys (gin.H{"id": u.ID, "name": u.Name} or a raw
// map[string]any{...}), typed as opaque strings since the literal's values
// are arbitrary expressions with no declared type to inspect cheaply.
// Non-string-literal keys (a computed key expression) are skipped — they
// can't be named in an OpenAPI property map.
func schemaFromMapLiteralKeys(lit *ast.CompositeLit) models.Schema {
	props := make(map[string]models.Schema)
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		keyLit, ok := kv.Key.(*ast.BasicLit)
		if !ok || keyLit.Kind != token.STRING {
			continue
		}
		props[strings.Trim(keyLit.Value, `"`)] = models.Schema{Type: "string"}
	}
	return models.Schema{Type: "object", Properties: props}
}

// findFuncDecl returns the top-level function or method declaration named
// funcName in file, or nil.
func findFuncDecl(file *ast.File, funcName string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == funcName {
			return fd
		}
	}
	return nil
}

// resolveCallReturnTypeName is a name-based fallback (go/types-free — see
// review §4) for resolving the type of a function call used as a response
// body expression, e.g. c.JSON(200, buildResponse(user)). Only handles the
// simple, extremely common case of a same-file, non-generic function/method
// with a named result type; anything else (cross-package calls, generics)
// returns "".
func resolveCallReturnTypeName(file *ast.File, call *ast.CallExpr) string {
	name := calleeBaseName(call.Fun)
	if name == "" {
		return ""
	}
	fd := findFuncDecl(file, name)
	if fd == nil || fd.Type.Results == nil {
		return ""
	}
	for _, res := range fd.Type.Results.List {
		typeName := typeExprToName(res.Type)
		if typeName == "" || typeName == "error" {
			continue
		}
		return typeName
	}
	return ""
}

// findIdentAssignedCallType looks for `name := someFunc(...)` within body
// and, if found, resolves someFunc's return type by name. Covers the very
// common `user := service.Create(req); c.JSON(201, user)` shape that
// collectLocalTypedVars's composite-literal-only tracking doesn't reach.
func findIdentAssignedCallType(file *ast.File, body *ast.BlockStmt, name string) string {
	var result string
	ast.Inspect(body, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != name || i >= len(assign.Rhs) {
				continue
			}
			if call, ok := assign.Rhs[i].(*ast.CallExpr); ok {
				if typ := resolveCallReturnTypeName(file, call); typ != "" {
					result = typ
				}
			}
		}
		return true
	})
	return result
}

// resolveResponseBodySchema resolves the OpenAPI schema (and, when it comes
// from a named type worth registering in components/schemas, the type name)
// for a response body expression (review §3, Step 3). varTypes is the
// enclosing block's local variable types (from collectLocalTypedVars); body
// is the same block, used for the CallExpr-assigned-identifier fallback.
func (a *Analyzer) resolveResponseBodySchema(file *ast.File, funcName string, expr ast.Expr, body *ast.BlockStmt, varTypes map[string]string) (schema models.Schema, typeName string) {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return a.resolveResponseBodySchema(file, funcName, e.X, body, varTypes)
		}
	case *ast.CompositeLit:
		if e.Type == nil {
			return models.Schema{Type: "object"}, ""
		}
		if isMapLikeLiteralType(e.Type) {
			return schemaFromMapLiteralKeys(e), ""
		}
		name := localTypeName(typeExprToName(e.Type))
		if resolved, ok, isLocal := a.resolveRequestSchema(file, funcName, name); ok {
			if isLocal {
				return resolved, ""
			}
			return resolved, name
		}
		return models.Schema{Type: "object"}, ""
	case *ast.Ident:
		typ := varTypes[e.Name]
		if typ == "" {
			typ = findIdentAssignedCallType(file, body, e.Name)
		}
		if typ != "" {
			name := localTypeName(typ)
			if resolved, ok, isLocal := a.resolveRequestSchema(file, funcName, name); ok {
				if isLocal {
					return resolved, ""
				}
				return resolved, name
			}
		}
	case *ast.CallExpr:
		if typ := resolveCallReturnTypeName(file, e); typ != "" {
			name := localTypeName(typ)
			if resolved, ok, isLocal := a.resolveRequestSchema(file, funcName, name); ok {
				if isLocal {
					return resolved, ""
				}
				return resolved, name
			}
		}
	}
	return models.Schema{Type: "object"}, ""
}

// recordResponseCall converts a recognized responseCall into a
// models.Response entry, resolving and registering its body schema when
// present (review §3, Step 5).
func (a *Analyzer) recordResponseCall(file *ast.File, funcName string, rc responseCall, body *ast.BlockStmt, varTypes map[string]string, responses map[int]models.Response) {
	if !rc.hasBody || !rc.isJSON {
		responses[rc.status] = models.Response{Description: statusDescription(rc.status)}
		return
	}
	schema, typeName := a.resolveResponseBodySchema(file, funcName, rc.bodyExpr, body, varTypes)
	if typeName != "" {
		a.addSchemaAndRefsToModels(typeName, schema)
	}
	responses[rc.status] = models.Response{
		Description: statusDescription(rc.status),
		Content: map[string]models.Content{
			"application/json": {Schema: schema},
		},
	}
}

// firstParamName returns the name of fd's first parameter (the context
// param for Gin/Echo/Fiber handlers, the http.ResponseWriter param for
// stdlib/Gorilla/Chi handlers), or "" if the signature has no named params.
func firstParamName(fd *ast.FuncDecl) string {
	if fd.Type.Params == nil || len(fd.Type.Params.List) == 0 {
		return ""
	}
	first := fd.Type.Params.List[0]
	if len(first.Names) == 0 {
		return ""
	}
	return first.Names[0].Name
}

// matchWriteHeader recognizes w.WriteHeader(status) and resolves status.
func matchWriteHeader(call *ast.CallExpr, writerName string) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteHeader" || len(call.Args) < 1 {
		return 0, false
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok || recv.Name != writerName {
		return 0, false
	}
	return resolveStatusCode(call.Args[0])
}

// matchJSONEncode recognizes json.NewEncoder(w).Encode(v) and returns v.
func matchJSONEncode(call *ast.CallExpr, writerName string) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Encode" || len(call.Args) < 1 {
		return nil, false
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	innerSel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || innerSel.Sel.Name != "NewEncoder" || len(inner.Args) < 1 {
		return nil, false
	}
	pkgIdent, ok := innerSel.X.(*ast.Ident)
	if !ok || pkgIdent.Name != "json" {
		return nil, false
	}
	argIdent, ok := inner.Args[0].(*ast.Ident)
	if !ok || argIdent.Name != writerName {
		return nil, false
	}
	return call.Args[0], true
}

// walkNetHTTPResponses handles the Gorilla/Chi (net/http) response pattern:
// w.WriteHeader(status) followed by json.NewEncoder(w).Encode(v), correlated
// by position within each block (the function body and each nested
// if/else block are scanned independently via ast.Inspect naturally
// visiting each *ast.BlockStmt, so branch-specific status/body pairs like an
// error branch vs. a success branch resolve separately without being
// conflated).
func (a *Analyzer) walkNetHTTPResponses(file *ast.File, funcName string, fd *ast.FuncDecl, writerName string, varTypes map[string]string, responses map[int]models.Response) {
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		status := 200 // net/http's default if WriteHeader is never called
		for _, stmt := range block.List {
			exprStmt, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := exprStmt.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			if s, ok := matchWriteHeader(call, writerName); ok {
				status = s
				if _, has := responses[status]; !has {
					// A WriteHeader with nothing (yet) encoded after it in this
					// block — bodyless unless a later Encode in this same block
					// overwrites the entry below.
					responses[status] = models.Response{Description: statusDescription(status)}
				}
				continue
			}
			if bodyExpr, ok := matchJSONEncode(call, writerName); ok {
				schema, typeName := a.resolveResponseBodySchema(file, funcName, bodyExpr, block, varTypes)
				if typeName != "" {
					a.addSchemaAndRefsToModels(typeName, schema)
				}
				responses[status] = models.Response{
					Description: statusDescription(status),
					Content: map[string]models.Content{
						"application/json": {Schema: schema},
					},
				}
			}
		}
		return true
	})
}

// isContextPassthroughCall reports whether call's first argument is the
// handler's own context/receiver identifier — the signal used to find
// candidate response-writing helper calls (review §3, Step 4) like
// respondError(c, http.StatusNotFound, "not found").
func isContextPassthroughCall(call *ast.CallExpr, recvName string) bool {
	if len(call.Args) == 0 {
		return false
	}
	id, ok := call.Args[0].(*ast.Ident)
	if !ok || id.Name != recvName {
		return false
	}
	switch call.Fun.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return true
	}
	return false
}

// flattenParamNames returns a FieldList's parameter names in declaration
// order ("" for an unnamed parameter), so they can be zipped positionally
// against a call's argument list.
func flattenParamNames(fl *ast.FieldList) []string {
	if fl == nil {
		return nil
	}
	var names []string
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			names = append(names, "")
			continue
		}
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	return names
}

// resolveHelperDelegate identifies what a candidate helper call (found by
// isContextPassthroughCall) delegates to, and builds the parameter
// substitution map so a status/message argument passed at the call site
// (respondError(c, http.StatusNotFound, "not found")) resolves correctly
// when we recurse into the helper's own body and find it referenced there
// only by parameter name (see substArg). Only follows a free function or a
// method on the caller's own receiver (h.someOtherMethod(c)), mirroring
// findDelegatedBindingTypeName's guard against wandering into an unrelated
// type's same-named method.
func (a *Analyzer) resolveHelperDelegate(file *ast.File, callerFd *ast.FuncDecl, call *ast.CallExpr) (delegateName string, subst map[string]ast.Expr) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		delegateName = fn.Name
	case *ast.SelectorExpr:
		if callerFd.Recv == nil || len(callerFd.Recv.List) == 0 || len(callerFd.Recv.List[0].Names) == 0 {
			return "", nil
		}
		ownRecv := callerFd.Recv.List[0].Names[0].Name
		id, ok := fn.X.(*ast.Ident)
		if !ok || id.Name != ownRecv {
			return "", nil
		}
		delegateName = fn.Sel.Name
	default:
		return "", nil
	}
	if delegateName == "" || delegateName == callerFd.Name.Name {
		return "", nil
	}
	delegateFd := findFuncDecl(file, delegateName)
	if delegateFd == nil {
		return delegateName, nil
	}
	subst = make(map[string]ast.Expr)
	for i, name := range flattenParamNames(delegateFd.Type.Params) {
		if name == "" || i >= len(call.Args) {
			continue
		}
		subst[name] = call.Args[i]
	}
	return delegateName, subst
}

// extractResponses walks handler funcName's body for response-emitting
// calls and builds one OpenAPI response entry per distinct status code
// found (review §3). Returns nil (not an error) when funcName can't be
// found or nothing was recognized — callers fall back to a clearly-labeled
// placeholder (Step 6) rather than treating this as a hard failure.
func (a *Analyzer) extractResponses(file *ast.File, funcName string) map[int]models.Response {
	return a.extractResponsesDepth(file, funcName, nil, 0)
}

// extractResponsesDepth is extractResponses' implementation. subst carries a
// parameter->argument substitution when this call is itself a recursion into
// a helper function (see resolveHelperDelegate); depth caps the recursion
// the same way findBindingTypeNameDepth caps request-side delegation.
func (a *Analyzer) extractResponsesDepth(file *ast.File, funcName string, subst map[string]ast.Expr, depth int) map[int]models.Response {
	fd := findFuncDecl(file, funcName)
	if fd == nil || fd.Body == nil {
		return nil
	}
	recvName := firstParamName(fd)
	if recvName == "" {
		return nil
	}
	varTypes, _, _, _ := collectLocalTypedVars(fd.Body)
	responses := make(map[int]models.Response)

	if a.framework == models.FrameWorkGorilla || a.framework == models.FrameWorkChi {
		a.walkNetHTTPResponses(file, funcName, fd, recvName, varTypes, responses)
		return responses
	}

	var helperCalls []*ast.CallExpr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if rc, ok := a.recognizeResponseCall(call, recvName, subst); ok {
			a.recordResponseCall(file, funcName, rc, fd.Body, varTypes, responses)
			return true
		}
		if isContextPassthroughCall(call, recvName) {
			helperCalls = append(helperCalls, call)
		}
		return true
	})

	if len(responses) == 0 && depth < 2 {
		for _, call := range helperCalls {
			delegate, callSubst := a.resolveHelperDelegate(file, fd, call)
			if delegate == "" {
				continue
			}
			nested := a.extractResponsesDepth(file, delegate, callSubst, depth+1)
			for status, resp := range nested {
				responses[status] = resp
			}
			if len(responses) > 0 {
				break // first helper call that actually resolves something wins
			}
		}
	}
	return responses
}
