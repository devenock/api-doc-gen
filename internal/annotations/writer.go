// Package annotations implements --write-annotations: writing swag-style
// `// @...` comment blocks above same-file handler functions, so a project
// that adopts api-doc-gen can migrate to swaggo/swag-compatible annotations
// without writing them by hand.
package annotations

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/models"
)

// WriteSwagAnnotations writes swag-style comment blocks above handler functions for each endpoint that has SourceFile and HandlerName set.
// basePath is prepended to route paths in @Router (e.g. /api/v1). It can be empty.
// projectPath scopes every read/write to its tree via os.Root (Go 1.24+):
// SourceFile was discovered during an earlier, separate analysis pass, and
// this write step runs later still, in its own call — without root
// confinement, a symlink swapped in for that path anytime in between (a
// wider window than a single directory walk) would have its target read
// and, worse, written to, regardless of where it points. See the equivalent
// guard (with the fuller rationale) in pkg/analyzer.
// typePackageName maps a type name (RequestTypeName/ResponseTypeName) to the
// Go package it's declared in (models.APISpec.TypePackageName), so a
// cross-package reference can be written as swag expects (models.Foo)
// instead of a bare name swag can only resolve within the handler's own
// package.
func WriteSwagAnnotations(projectPath string, endpoints []models.Endpoint, basePath string, typePackageName map[string]string) (written int, err error) {
	root, err := os.OpenRoot(projectPath)
	if err != nil {
		return 0, fmt.Errorf("open project directory: %w", err)
	}
	defer root.Close()

	// Group by (SourceFile, HandlerName); collect all (path, method) per handler
	type key struct{ file, handler string }
	groups := make(map[key][]models.Endpoint)
	for _, ep := range endpoints {
		if ep.SourceFile == "" || ep.HandlerName == "" {
			continue
		}
		k := key{ep.SourceFile, ep.HandlerName}
		groups[k] = append(groups[k], ep)
	}

	for k, eps := range groups {
		n, e := writeSwagToFile(root, projectPath, k.file, k.handler, eps, basePath, typePackageName)
		if e != nil {
			return written, e
		}
		written += n
	}
	return written, nil
}

func writeSwagToFile(root *os.Root, projectPath, filePath, handlerName string, endpoints []models.Endpoint, basePath string, typePackageName map[string]string) (int, error) {
	if len(endpoints) == 0 {
		return 0, nil
	}
	ep := endpoints[0] // use first for summary, description, tags, params, body, response

	rel, err := filepath.Rel(projectPath, filePath)
	if err != nil {
		return 0, fmt.Errorf("resolve %s relative to project: %w", filePath, err)
	}
	// Refuse a symlink exactly as the original analysis walk would have -
	// os.Root follows symlinks that stay within the root (only escaping ones
	// are blocked), so this still needs its own explicit check.
	info, err := root.Lstat(rel)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", filePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("refusing to follow symlink %s", filePath)
	}

	content, err := root.ReadFile(rel)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filePath, err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", filePath, err)
	}

	var funcLine int
	for _, decl := range node.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != handlerName {
			continue
		}
		funcLine = fset.Position(fd.Pos()).Line
		break
	}
	if funcLine == 0 {
		return 0, nil
	}

	block := buildSwagBlock(ep, endpoints, basePath, node.Name.Name, node.Imports, typePackageName)
	newContent := insertOrReplaceSwagBlock(content, funcLine, block)
	if string(newContent) == string(content) {
		return 0, nil
	}
	if err := root.WriteFile(rel, newContent, 0644); err != nil {
		return 0, fmt.Errorf("write %s: %w", filePath, err)
	}
	return 1, nil
}

func buildSwagBlock(ep models.Endpoint, all []models.Endpoint, basePath, handlerPkg string, handlerImports []*ast.ImportSpec, typePackageName map[string]string) []string {
	var lines []string
	lines = append(lines, "// @Summary "+escapeSwagLine(ep.Summary))
	if ep.Description != "" {
		lines = append(lines, "// @Description "+escapeSwagLine(ep.Description))
	}
	if len(ep.Tags) > 0 {
		escapedTags := make([]string, len(ep.Tags))
		for i, t := range ep.Tags {
			escapedTags[i] = escapeSwagLine(t)
		}
		lines = append(lines, "// @Tags "+strings.Join(escapedTags, ","))
	}
	lines = append(lines, "// @Accept json")
	lines = append(lines, "// @Produce json")

	for _, p := range ep.Parameters {
		in := p.In
		if in == "" {
			in = "path"
		}
		// swag's own @Param regexp requires a non-empty quoted description
		// (`"([^"]+)"`, one-or-more) - an empty "" fails it outright and
		// aborts parsing the whole file, so a param with no description
		// (common for path/query params, which rarely get one) falls back
		// to its name rather than emitting a description-shaped hole.
		desc := p.Description
		if desc == "" {
			desc = p.Name
		}
		lines = append(lines, fmt.Sprintf("// @Param %s %s string %t %q", escapeSwagLine(p.Name), in, p.Required, desc))
	}
	if ep.RequestBody != nil {
		typeName := qualifyTypeName(ep.RequestTypeName, handlerPkg, handlerImports, typePackageName)
		if typeName == "" {
			typeName = "object"
		}
		lines = append(lines, fmt.Sprintf("// @Param request body %s true \"Request body\"", typeName))
	}

	respType := qualifyTypeName(ep.ResponseTypeName, handlerPkg, handlerImports, typePackageName)
	if respType == "" {
		respType = "object"
	}
	lines = append(lines, fmt.Sprintf("// @Success 200 {object} %s \"Success\"", respType))
	if len(ep.Security) > 0 {
		lines = append(lines, "// @Security BearerAuth")
	}
	for _, e := range all {
		path := escapeSwagLine(e.Path)
		if basePath != "" {
			path = strings.TrimSuffix(escapeSwagLine(basePath), "/") + "/" + strings.TrimPrefix(path, "/")
		}
		lines = append(lines, fmt.Sprintf("// @Router %s [%s]", path, strings.ToLower(e.Method)))
	}
	return lines
}

// qualifyTypeName returns typeName as swag needs it in the handler's file:
// bare if it's declared in the handler's own package (or its package is
// unknown), or "pkg.TypeName" when it's declared elsewhere - swag can only
// resolve a bare name within the annotated function's own package (see
// PackagesDefinitions.FindTypeSpec upstream), so a cross-package reference
// left unqualified fails with "cannot find type definition" when swag
// itself later parses these annotations.
//
// The matching import is guaranteed to already be present in
// handlerImports: whatever binding code in this same handler originally
// referenced typeName (var req models.CreateUserRequest) necessarily
// imports models to compile. If no matching import is found regardless
// (typePackageName has no entry, e.g. the type was resolved before this
// tracking existed, or came from an update this file predates), the bare
// name is returned unchanged rather than guessing a qualifier that might be
// wrong.
func qualifyTypeName(typeName, handlerPkg string, handlerImports []*ast.ImportSpec, typePackageName map[string]string) string {
	if typeName == "" {
		return ""
	}
	declPkg := typePackageName[typeName]
	if declPkg == "" || declPkg == handlerPkg {
		return typeName
	}
	for _, imp := range handlerImports {
		path := strings.Trim(imp.Path.Value, `"`)
		alias := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			alias = path[idx+1:]
		}
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == declPkg {
			return alias + "." + typeName
		}
	}
	return typeName
}

// escapeSwagLine strips embedded newlines before a value is interpolated
// into a `// @...` comment line. Every such value here ultimately traces
// back to a string literal in the analyzed project's own source (route
// paths, handler doc comments) — normally that can't contain a raw newline,
// but a backtick raw-string literal legitimately can. Without this, a
// deliberately crafted route path containing one could break out of the `//`
// comment when --write-annotations writes it back, turning the rest of the
// crafted string into literal (non-comment) lines injected into the
// project's own .go file — every field embedded here must go through this,
// not just the ones that happen to look free-form (Summary/Description).
func escapeSwagLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// insertOrReplaceSwagBlock inserts the swag block before the function at funcLine, or replaces an existing swag block (comment lines containing @).
func insertOrReplaceSwagBlock(content []byte, funcLine int, block []string) []byte {
	lines := strings.Split(string(content), "\n")
	if funcLine < 1 || funcLine > len(lines) {
		return content
	}
	insertAt := funcLine - 1 // 0-based index of the function line

	// Back up to skip existing swag block (consecutive // @ lines immediately above the function)
	start := insertAt
	for start > 0 {
		prev := strings.TrimSpace(lines[start-1])
		if !strings.HasPrefix(prev, "//") || !strings.Contains(prev, "@") {
			break
		}
		start--
	}

	var newLines []string
	newLines = append(newLines, lines[:start]...)
	newLines = append(newLines, block...)
	newLines = append(newLines, lines[insertAt:]...)
	return []byte(strings.Join(newLines, "\n"))
}
