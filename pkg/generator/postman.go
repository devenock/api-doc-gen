package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// PostmanGenerator generates Postman collections
type PostmanGenerator struct {
	config *config.Config
}

// NewPostmanGenerator creates a new Postman generator
func NewPostmanGenerator(cfg *config.Config) *PostmanGenerator {
	return &PostmanGenerator{config: cfg}
}

// PostmanCollection represents a Postman collection
type PostmanCollection struct {
	Info struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schema      string `json:"schema"`
	} `json:"info"`
	Item     []PostmanItem     `json:"item"`
	Variable []PostmanVariable `json:"variable,omitempty"`
}

// PostmanItem represents a Postman request or folder
type PostmanItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Item        []PostmanItem   `json:"item,omitempty"`
	Request     *PostmanRequest `json:"request,omitempty"`
	Response    []interface{}   `json:"response,omitempty"`
}

// PostmanRequest represents a Postman request
type PostmanRequest struct {
	Method      string          `json:"method"`
	Header      []PostmanHeader `json:"header"`
	Body        *PostmanBody    `json:"body,omitempty"`
	URL         PostmanURL      `json:"url"`
	Description string          `json:"description,omitempty"`
}

// PostmanHeader represents a request header
type PostmanHeader struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`
}

// PostmanBody represents a request body
type PostmanBody struct {
	Mode    string                 `json:"mode"`
	Raw     string                 `json:"raw,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// PostmanURL represents a request URL
type PostmanURL struct {
	Raw      string            `json:"raw"`
	Protocol string            `json:"protocol,omitempty"`
	Host     []string          `json:"host"`
	Path     []string          `json:"path"`
	Query    []PostmanQuery    `json:"query,omitempty"`
	Variable []PostmanVariable `json:"variable,omitempty"`
}

// PostmanQuery represents a query parameter
type PostmanQuery struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// PostmanVariable represents a variable
type PostmanVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

// Generate creates a Postman collection
func (g *PostmanGenerator) Generate(spec *models.APISpec) error {
	// Create output directory
	if err := os.MkdirAll(g.config.Output, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	collection := g.convertToPostman(spec)

	// Write collection to file
	outputPath := filepath.Join(g.config.Output, "collection.json")
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create collection file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(collection); err != nil {
		return fmt.Errorf("failed to write collection: %w", err)
	}

	if !g.config.Quiet {
		fmt.Printf("   📄 Postman Collection: %s\n", outputPath)
		fmt.Println("   💡 Import this file into Postman to use the collection")
	}
	return nil
}

// convertToPostman converts APISpec to Postman collection format
func (g *PostmanGenerator) convertToPostman(spec *models.APISpec) *PostmanCollection {
	collection := &PostmanCollection{}

	collection.Info.Name = spec.Title
	collection.Info.Description = spec.Description
	collection.Info.Schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

	// Add base URL variable
	if len(spec.Servers) > 0 {
		collection.Variable = []PostmanVariable{
			{
				Key:   "baseUrl",
				Value: spec.Servers[0].URL,
				Type:  "string",
			},
		}
	}

	// Group endpoints by tags or create a flat structure
	endpointsByTag := g.groupEndpointsByTag(spec.Endpoints)

	if len(endpointsByTag) > 1 {
		// Create folders for each tag
		for tag, endpoints := range endpointsByTag {
			folder := PostmanItem{
				Name: tag,
				Item: []PostmanItem{},
			}
			for _, endpoint := range endpoints {
				folder.Item = append(folder.Item, g.convertEndpointToItem(endpoint, spec))
			}
			collection.Item = append(collection.Item, folder)
		}
	} else {
		// Flat structure
		for _, endpoint := range spec.Endpoints {
			collection.Item = append(collection.Item, g.convertEndpointToItem(endpoint, spec))
		}
	}

	return collection
}

// groupEndpointsByTag groups endpoints by their tags
func (g *PostmanGenerator) groupEndpointsByTag(endpoints []models.Endpoint) map[string][]models.Endpoint {
	grouped := make(map[string][]models.Endpoint)

	for _, endpoint := range endpoints {
		tag := "Endpoints"
		if len(endpoint.Tags) > 0 {
			tag = endpoint.Tags[0]
		}
		grouped[tag] = append(grouped[tag], endpoint)
	}

	return grouped
}

// convertEndpointToItem converts an endpoint to a Postman item
func (g *PostmanGenerator) convertEndpointToItem(endpoint models.Endpoint, spec *models.APISpec) PostmanItem {
	item := PostmanItem{
		Name:        endpoint.Summary,
		Description: endpoint.Description,
		Response:    []interface{}{},
	}

	// Create request
	request := &PostmanRequest{
		Method: endpoint.Method,
		Header: []PostmanHeader{
			{
				Key:   "Content-Type",
				Value: "application/json",
				Type:  "text",
			},
		},
		URL:         g.createPostmanURL(endpoint, spec),
		Description: endpoint.Description,
	}

	// Add headers from parameters
	for _, param := range endpoint.Parameters {
		if param.In == "header" {
			request.Header = append(request.Header, PostmanHeader{
				Key:         param.Name,
				Value:       fmt.Sprintf("{{%s}}", param.Name),
				Description: param.Description,
				Type:        "text",
			})
		}
	}

	// Add request body if present
	if endpoint.RequestBody != nil {
		body := &PostmanBody{
			Mode: "raw",
			Options: map[string]interface{}{
				"raw": map[string]interface{}{
					"language": "json",
				},
			},
		}

		// Generate example body
		if jsonContent, ok := endpoint.RequestBody.Content["application/json"]; ok {
			exampleBody := g.generateExampleFromSchema(jsonContent.Schema)
			bodyBytes, _ := json.MarshalIndent(exampleBody, "", "  ")
			body.Raw = string(bodyBytes)
		}

		request.Body = body
	}

	item.Request = request
	return item
}

// createPostmanURL creates a Postman URL structure
func (g *PostmanGenerator) createPostmanURL(endpoint models.Endpoint, spec *models.APISpec) PostmanURL {
	path := endpoint.Path
	if g.config.BasePath != "" {
		path = g.config.BasePath + path
	}

	// Parse path segments
	pathSegments := []string{}
	for _, segment := range strings.Split(strings.Trim(path, "/"), "/") {
		if segment != "" {
			pathSegments = append(pathSegments, segment)
		}
	}

	url := PostmanURL{
		Raw:      "{{baseUrl}}" + path,
		Protocol: "http",
		Host:     []string{"{{baseUrl}}"},
		Path:     pathSegments,
	}

	// Add query parameters
	for _, param := range endpoint.Parameters {
		if param.In == "query" {
			url.Query = append(url.Query, PostmanQuery{
				Key:         param.Name,
				Value:       fmt.Sprintf("{{%s}}", param.Name),
				Description: param.Description,
				Disabled:    !param.Required,
			})
		}
	}

	// Add path variables
	for _, param := range endpoint.Parameters {
		if param.In == "path" {
			url.Variable = append(url.Variable, PostmanVariable{
				Key:   param.Name,
				Value: param.Example.(string),
				Type:  "string",
			})
		}
	}

	return url
}

// generateExampleFromSchema generates an example object from a schema
func (g *PostmanGenerator) generateExampleFromSchema(schema models.Schema) interface{} {
	if schema.Example != nil {
		return schema.Example
	}

	switch schema.Type {
	case "object":
		obj := make(map[string]interface{})
		for name, prop := range schema.Properties {
			obj[name] = g.generateExampleFromSchema(prop)
		}
		return obj
	case "array":
		if schema.Items != nil {
			return []interface{}{g.generateExampleFromSchema(*schema.Items)}
		}
		return []interface{}{}
	case "string":
		return "string"
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return false
	default:
		return nil
	}
}
