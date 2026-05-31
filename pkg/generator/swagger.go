package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
	"gopkg.in/yaml.v3"
)

// SwaggerGenerator generates Swagger/OpenAPI documentation
type SwaggerGenerator struct {
	config *config.Config
}

// NewSwaggerGenerator creates a new Swagger generator
func NewSwaggerGenerator(cfg *config.Config) *SwaggerGenerator {
	return &SwaggerGenerator{config: cfg}
}

// Generate creates Swagger documentation
func (g *SwaggerGenerator) Generate(spec *models.APISpec) error {
	// Create output directory
	if err := os.MkdirAll(g.config.Output, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Convert to OpenAPI 3.0 format
	openAPI := g.convertToOpenAPI(spec)

	// Generate YAML
	yamlPath := filepath.Join(g.config.Output, "openapi.yaml")
	if err := g.writeYAML(yamlPath, openAPI); err != nil {
		return fmt.Errorf("failed to write YAML: %w", err)
	}

	// Generate JSON
	jsonPath := filepath.Join(g.config.Output, "openapi.json")
	if err := g.writeJSON(jsonPath, openAPI); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	// Generate Swagger UI HTML
	htmlPath := filepath.Join(g.config.Output, "index.html")
	if err := g.generateSwaggerUI(htmlPath); err != nil {
		return fmt.Errorf("failed to generate Swagger UI: %w", err)
	}

	if !g.config.Quiet {
		fmt.Printf("   📄 OpenAPI YAML: %s\n", yamlPath)
		fmt.Printf("   📄 OpenAPI JSON: %s\n", jsonPath)
		fmt.Printf("   🌐 Swagger UI: %s\n", htmlPath)
	}
	return nil
}

// convertToOpenAPI converts APISpec to OpenAPI 3.0 format
func (g *SwaggerGenerator) convertToOpenAPI(spec *models.APISpec) map[string]interface{} {
	openAPI := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       spec.Title,
			"version":     spec.Version,
			"description": spec.Description,
		},
		"servers": []map[string]interface{}{},
		"paths":   map[string]interface{}{},
	}

	// Add servers
	for _, server := range spec.Servers {
		openAPI["servers"] = append(openAPI["servers"].([]map[string]interface{}),
			map[string]interface{}{
				"url":         server.URL,
				"description": server.Description,
			})
	}

	// Add paths
	paths := openAPI["paths"].(map[string]interface{})
	for _, endpoint := range spec.Endpoints {
		path := endpoint.Path
		if g.config.BasePath != "" {
			path = g.config.BasePath + path
		}

		if paths[path] == nil {
			paths[path] = make(map[string]interface{})
		}

		pathItem := paths[path].(map[string]interface{})
		pathItem[strings.ToLower(endpoint.Method)] = g.convertEndpoint(endpoint)
	}

	// Components: schemas and security schemes
	components := make(map[string]interface{})
	if len(spec.Models) > 0 {
		schemas := make(map[string]interface{}, len(spec.Models))
		for name, s := range spec.Models {
			schemas[name] = schemaToMap(s)
		}
		components["schemas"] = schemas
	}
	// Add BearerAuth security scheme when any endpoint uses security
	for _, ep := range spec.Endpoints {
		if len(ep.Security) > 0 {
			components["securitySchemes"] = map[string]interface{}{
				"BearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
					"description":  "JWT token from login or register",
				},
			}
			break
		}
	}
	if len(components) > 0 {
		openAPI["components"] = components
	}

	return openAPI
}

// schemaToMap converts Schema to a plain map so YAML/JSON encoding never sees struct tags.
func schemaToMap(s models.Schema) map[string]interface{} {
	// $ref must be the only property — no siblings allowed in OpenAPI 3.0.
	if s.Ref != "" {
		return map[string]interface{}{"$ref": s.Ref}
	}
	typ := s.Type
	if typ == "" {
		typ = "object"
	}
	out := map[string]interface{}{"type": typ}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Properties) > 0 {
		props := make(map[string]interface{})
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		out["properties"] = props
	}
	if s.Items != nil {
		out["items"] = schemaToMap(*s.Items)
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Example != nil {
		out["example"] = s.Example
	}
	if s.AdditionalProperties != nil {
		out["additionalProperties"] = s.AdditionalProperties
	}
	return out
}

// contentToMap converts Content to a plain map so YAML/JSON encoding never sees struct tags.
func contentToMap(c models.Content) map[string]interface{} {
	out := map[string]interface{}{"schema": schemaToMap(c.Schema)}
	if c.Example != nil {
		out["example"] = c.Example
	}
	return out
}

// contentMapToMap converts map[string]Content to map[string]interface{}.
func contentMapToMap(m map[string]models.Content) map[string]interface{} {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = contentToMap(v)
	}
	return out
}

// convertEndpoint converts an Endpoint to OpenAPI operation
func (g *SwaggerGenerator) convertEndpoint(endpoint models.Endpoint) map[string]interface{} {
	operation := map[string]interface{}{
		"summary":     endpoint.Summary,
		"description": endpoint.Description,
		"responses":   map[string]interface{}{},
	}

	if len(endpoint.Tags) > 0 {
		operation["tags"] = endpoint.Tags
	}

	// Add parameters
	if len(endpoint.Parameters) > 0 {
		params := []map[string]interface{}{}
		for _, param := range endpoint.Parameters {
			params = append(params, map[string]interface{}{
				"name":        param.Name,
				"in":          param.In,
				"description": param.Description,
				"required":    param.Required,
				"schema":      schemaToMap(param.Schema),
			})
		}
		operation["parameters"] = params
	}

	// Add request body. When the analyzer inferred a typed schema, use it.
	// For POST/PUT/PATCH with no inferred schema, show a generic JSON editor
	// so Swagger UI always renders a body field for methods that carry a payload.
	if endpoint.RequestBody != nil {
		operation["requestBody"] = map[string]interface{}{
			"description": endpoint.RequestBody.Description,
			"required":    endpoint.RequestBody.Required,
			"content":     contentMapToMap(endpoint.RequestBody.Content),
		}
	} else if endpoint.Method == "POST" || endpoint.Method == "PUT" || endpoint.Method == "PATCH" {
		operation["requestBody"] = map[string]interface{}{
			"required": true,
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": map[string]interface{}{"type": "object"},
				},
			},
		}
	}

	// Add responses
	responses := operation["responses"].(map[string]interface{})
	for code, response := range endpoint.Responses {
		responses[fmt.Sprintf("%d", code)] = map[string]interface{}{
			"description": response.Description,
			"content":     contentMapToMap(response.Content),
		}
	}

	// Add security if present
	if len(endpoint.Security) > 0 {
		operation["security"] = endpoint.Security
	}

	return operation
}

// writeYAML writes the OpenAPI spec to a YAML file
func (g *SwaggerGenerator) writeYAML(path string, data interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	return encoder.Encode(data)
}

// writeJSON writes the OpenAPI spec to a JSON file
func (g *SwaggerGenerator) writeJSON(path string, data interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// generateSwaggerUI generates a Swagger UI HTML file
func (g *SwaggerGenerator) generateSwaggerUI(path string) error {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>` + g.config.Title + `</title>
    <link rel="stylesheet" type="text/css" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
    <style>
        body { margin: 0; padding: 0; }
        #cors-notice {
            background: #1c3557;
            border-bottom: 1px solid #2d5a8e;
            color: #90cdf4;
            padding: 0.7rem 1.25rem;
            display: flex;
            align-items: center;
            justify-content: space-between;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            font-size: 0.875rem;
            gap: 1rem;
        }
        #cors-notice code {
            background: rgba(255,255,255,0.12);
            padding: 0.1em 0.4em;
            border-radius: 3px;
            font-size: 0.8125rem;
        }
        #cors-notice button {
            background: none;
            border: none;
            color: #90cdf4;
            cursor: pointer;
            font-size: 1.125rem;
            flex-shrink: 0;
            line-height: 1;
            padding: 0;
        }
        #cors-notice button:hover { color: #fff; }
    </style>
</head>
<body>
    <div id="cors-notice">
        <span>
            💡 <strong>CORS:</strong> If "Execute" fails with a network error, your backend needs CORS headers.
            Add CORS middleware:
            <code>github.com/gin-contrib/cors</code> (Gin) &nbsp;·&nbsp;
            <code>echo/middleware.CORS()</code> (Echo) &nbsp;·&nbsp;
            <code>github.com/gofiber/contrib/fibercors</code> (Fiber) &nbsp;·&nbsp;
            <code>github.com/rs/cors</code> (Gorilla / Chi / stdlib)
        </span>
        <button onclick="document.getElementById('cors-notice').remove()" title="Dismiss">✕</button>
    </div>
    <div id="swagger-ui"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            window.ui = SwaggerUIBundle({
                url: "./openapi.json",
                dom_id: '#swagger-ui',
                deepLinking: true,
                persistAuthorization: true,
                withCredentials: false,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                requestInterceptor: function(req) {
                    if (req.body && !req.headers['Content-Type']) {
                        req.headers['Content-Type'] = 'application/json';
                    }
                    return req;
                }
            });
        };
    </script>
</body>
</html>`

	return os.WriteFile(path, []byte(html), 0644)
}
