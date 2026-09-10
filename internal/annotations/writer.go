package annotations

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

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
