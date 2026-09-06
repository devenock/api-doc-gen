package analyzer

import (
	"regexp"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/models"
)

// Path-string helpers shared by every route-family parser: deriving a
// Swagger tag from a path, normalizing framework-specific path-param syntax
// (Gorilla's {id}/{id:regex}, Chi/Gin's {id}/:id) to a single form, and
// extracting path parameters for the OpenAPI spec.

// tagFromPath returns a tag (e.g. "products", "users") from the path for OpenAPI grouping.
// Uses the first path segment that looks like a resource name (skips "api", "v1", "v2", params like ":id").
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

// gorillaTypedVarRe matches Gorilla's regex-constrained path vars, e.g.
// {id:[0-9]+} -> captures "id" so callers can normalize to {id}.
var gorillaTypedVarRe = regexp.MustCompile(`\{([^:{}]+):[^{}]+\}`)

// normalizeBracePath rewrites Gorilla's {param:pattern} segments to plain
// {param} so the path is a valid OpenAPI path template (and so Swagger UI's
// "Try it out" can substitute the parameter correctly).
func normalizeBracePath(path string) string {
	return gorillaTypedVarRe.ReplaceAllString(path, "{$1}")
}

// normalizeColonPath converts Gin/Echo/Fiber colon-style path params (:id)
// to OpenAPI brace style ({id}) so the stored path is spec-compliant.
func normalizeColonPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// pathParamBraceStripRe, pathParamColonRe, and pathParamBraceRe back
// extractPathParams. Compiled once at package init — extractPathParams runs
// once per endpoint, so recompiling them on every call (as before) meant
// three regexp compilations per endpoint for no benefit, mirroring the
// package-level gorillaTypedVarRe right above.
var (
	pathParamBraceStripRe = regexp.MustCompile(`\{[^}]+\}`)
	pathParamColonRe      = regexp.MustCompile(`:([^/]+)`)
	pathParamBraceRe      = regexp.MustCompile(`\{([^}]+)\}`)
)

// extractPathParams extracts path parameters from route path.
// Supports :param (Gin, Echo, Fiber) and {param} (Chi, Gorilla — callers
// should normalize Gorilla's {param:pattern} form via normalizeBracePath
// before calling this, so the colon isn't mistaken for :param syntax).
func extractPathParams(path string) []models.Parameter {
	var params []models.Parameter
	// :param style (e.g. /users/:id). Braced segments are stripped first so a
	// colon inside {id:pattern} (before normalization) is never mistaken for this.
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
