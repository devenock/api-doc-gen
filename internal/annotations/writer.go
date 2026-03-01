package annotations

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/models"
)

// WriteSwagAnnotations writes swag-style comment blocks above handler functions for each endpoint that has SourceFile and HandlerName set.
// basePath is prepended to route paths in @Router (e.g. /api/v1). It can be empty.
func WriteSwagAnnotations(endpoints []models.Endpoint, basePath string) (written int, err error) {
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
		n, e := writeSwagToFile(k.file, k.handler, eps, basePath)
		if e != nil {
			return written, e
		}
		written += n
	}
	return written, nil
}

func writeSwagToFile(filePath, handlerName string, endpoints []models.Endpoint, basePath string) (int, error) {
	if len(endpoints) == 0 {
		return 0, nil
	}
	ep := endpoints[0] // use first for summary, description, tags, params, body, response

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
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

	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filePath, err)
	}

	block := buildSwagBlock(ep, endpoints, basePath)
	newContent := insertOrReplaceSwagBlock(content, funcLine, block)
	if string(newContent) == string(content) {
		return 0, nil
	}
	if err := os.WriteFile(filePath, newContent, 0644); err != nil {
		return 0, fmt.Errorf("write %s: %w", filePath, err)
	}
	return 1, nil
}

func buildSwagBlock(ep models.Endpoint, all []models.Endpoint, basePath string) []string {
	var lines []string
	lines = append(lines, "// @Summary "+escapeSwagLine(ep.Summary))
	if ep.Description != "" {
		lines = append(lines, "// @Description "+escapeSwagLine(ep.Description))
	}
	if len(ep.Tags) > 0 {
		lines = append(lines, "// @Tags "+strings.Join(ep.Tags, ","))
	}
	lines = append(lines, "// @Accept json")
	lines = append(lines, "// @Produce json")

	for _, p := range ep.Parameters {
		lines = append(lines, fmt.Sprintf("// @Param %s path string true %q", p.Name, p.Description))
	}
	if ep.RequestBody != nil {
		typeName := ep.RequestTypeName
		if typeName == "" {
			typeName = "object"
		}
		lines = append(lines, fmt.Sprintf("// @Param request body %s true \"Request body\"", typeName))
	}

	respType := "object"
	if ep.ResponseTypeName != "" {
		respType = ep.ResponseTypeName
	}
	lines = append(lines, fmt.Sprintf("// @Success 200 {object} %s \"Success\"", respType))
	if len(ep.Security) > 0 {
		lines = append(lines, "// @Security BearerAuth")
	}
	for _, e := range all {
		path := e.Path
		if basePath != "" {
			path = strings.TrimSuffix(basePath, "/") + "/" + strings.TrimPrefix(path, "/")
		}
		lines = append(lines, fmt.Sprintf("// @Router %s [%s]", path, strings.ToLower(e.Method)))
	}
	return lines
}

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
