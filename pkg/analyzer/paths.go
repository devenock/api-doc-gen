package analyzer

import (
	"regexp"
	"strings"

	"github.com/devenock/specyl/pkg/models"
)

func tagFromPath(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if p == "" || p == "api" || strings.HasPrefix(p, "v") && len(p) <= 3 || strings.HasPrefix(p, ":") || strings.HasPrefix(p, "{") {
			continue
		}
		return p
	}
	return ""
}

var gorillaTypedVarRe = regexp.MustCompile(`\{([^:{}]+):[^{}]+\}`)

func normalizeBracePath(path string) string {
	return gorillaTypedVarRe.ReplaceAllString(path, "{$1}")
}

func normalizeColonPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

var (
	pathParamBraceStripRe = regexp.MustCompile(`\{[^}]+\}`)
	pathParamColonRe      = regexp.MustCompile(`:([^/]+)`)
	pathParamBraceRe      = regexp.MustCompile(`\{([^}]+)\}`)
)

func extractPathParams(path string) []models.Parameter {
	var params []models.Parameter

	braceStripped := pathParamBraceStripRe.ReplaceAllString(path, "")
	for _, name := range pathParamColonRe.FindAllStringSubmatch(braceStripped, -1) {
		if len(name) >= 2 {
			params = append(params, models.Parameter{
				Name:     name[1],
				In:       "path",
				Required: true,
				Schema:   models.Schema{Type: "string"},
			})
		}
	}
	// {param} style (e.g. /users/{id})
	for _, name := range pathParamBraceRe.FindAllStringSubmatch(path, -1) {
		if len(name) >= 2 {
			paramName := name[1]
			if idx := strings.Index(paramName, ":"); idx >= 0 {
				paramName = paramName[:idx]
			}
			params = append(params, models.Parameter{
				Name:     paramName,
				In:       "path",
				Required: true,
				Schema:   models.Schema{Type: "string"},
			})
		}
	}
	return params
}
