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

// PostmanAuth represents Postman request/collection auth config
type PostmanAuth struct {
	Type   string              `json:"type"`
	Bearer []PostmanAuthBearer `json:"bearer,omitempty"`
}

// PostmanAuthBearer is a single key/value pair inside a bearer auth block
type PostmanAuthBearer struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

// PostmanRequest represents a Postman request
type PostmanRequest struct {
	Method      string          `json:"method"`
	Header      []PostmanHeader `json:"header"`
	Auth        *PostmanAuth    `json:"auth,omitempty"`
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

	// Collection-level variables: baseUrl + bearerToken placeholder
	baseURL := ""
	if len(spec.Servers) > 0 {
		baseURL = spec.Servers[0].URL
	}
	collection.Variable = []PostmanVariable{
		{Key: "baseUrl", Value: baseURL, Type: "string"},
		{Key: "bearerToken", Value: "", Type: "string"},
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

// requestName builds a short Postman request name from the HTTP method and path.
// Examples: GET /users/:id → get_user, POST /products → add_product
func requestName(method, path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")

	lastResource := ""
	endsWithParam := false

	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ":") || (strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) {
			endsWithParam = true
			continue
		}
		lastResource = strings.ToLower(strings.ReplaceAll(seg, "-", "_"))
		break
	}

	if lastResource == "" {
		lastResource = "resource"
	}

	m := strings.ToUpper(method)
	if endsWithParam || m == "POST" || m == "DELETE" {
		lastResource = singularizeWord(lastResource)
	}

	var verb string
	switch m {
	case "GET":
		verb = "get"
	case "POST":
		verb = "add"
	case "PUT", "PATCH":
		verb = "update"
	case "DELETE":
		verb = "delete"
	default:
		verb = strings.ToLower(method)
	}

	return verb + "_" + lastResource
}

// singularizeWord strips a trailing 's' for common English plural patterns.
func singularizeWord(word string) string {
	if strings.HasSuffix(word, "ies") && len(word) > 3 {
		return word[:len(word)-3] + "y"
	}
	for _, suffix := range []string{"ics", "us", "ss", "is"} {
		if strings.HasSuffix(word, suffix) {
			return word
		}
	}
	if strings.HasSuffix(word, "s") && len(word) > 2 {
		return word[:len(word)-1]
	}
	return word
}

// convertEndpointToItem converts an endpoint to a Postman item
func (g *PostmanGenerator) convertEndpointToItem(endpoint models.Endpoint, spec *models.APISpec) PostmanItem {
	item := PostmanItem{
		Name:        requestName(endpoint.Method, endpoint.Path),
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

	// Wire up bearer auth for protected endpoints
	if len(endpoint.Security) > 0 {
		request.Auth = &PostmanAuth{
			Type: "bearer",
			Bearer: []PostmanAuthBearer{
				{Key: "token", Value: "{{bearerToken}}", Type: "string"},
			},
		}
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

	// Add request body. For POST/PUT/PATCH always include a body editor —
	// use the inferred schema when available, otherwise an empty object so
	// Postman still shows the body field for the user to fill in.
	bodyMethods := map[string]bool{"POST": true, "PUT": true, "PATCH": true}
	if endpoint.RequestBody != nil || bodyMethods[endpoint.Method] {
		body := &PostmanBody{
			Mode: "raw",
			Options: map[string]interface{}{
				"raw": map[string]interface{}{
					"language": "json",
				},
			},
		}

		if endpoint.RequestBody != nil {
			if jsonContent, ok := endpoint.RequestBody.Content["application/json"]; ok {
				exampleBody := g.generateExampleFromSchema(jsonContent.Schema, spec.Models)
				bodyBytes, _ := json.MarshalIndent(exampleBody, "", "  ")
				body.Raw = string(bodyBytes)
			}
		} else {
			body.Raw = "{}"
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

	// Do NOT set Protocol here. The {{baseUrl}} variable already contains
	// the full base URL including scheme (e.g. "http://localhost:8080"), so
	// adding a separate Protocol field causes Postman to double-prefix it.
	url := PostmanURL{
		Raw:  "{{baseUrl}}" + path,
		Host: []string{"{{baseUrl}}"},
		Path: pathSegments,
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

	// Add path variables. param.Example is interface{} and may be nil
	// (the analyzer does not currently set it for path params), so we must
	// guard the type assertion to avoid a panic.
	for _, param := range endpoint.Parameters {
		if param.In == "path" {
			value := ""
			if param.Example != nil {
				if s, ok := param.Example.(string); ok {
					value = s
				} else {
					value = fmt.Sprint(param.Example)
				}
			}
			url.Variable = append(url.Variable, PostmanVariable{
				Key:   param.Name,
				Value: value,
				Type:  "string",
			})
		}
	}

	return url
}

// generateExampleFromSchema generates an example object from a schema.
// componentSchemas is the OpenAPI components/schemas map used to resolve $ref.
func (g *PostmanGenerator) generateExampleFromSchema(schema models.Schema, componentSchemas map[string]models.Schema) interface{} {
	// Resolve $ref before doing anything else.
	if schema.Ref != "" {
		refName := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
		if resolved, ok := componentSchemas[refName]; ok {
			return g.generateExampleFromSchema(resolved, componentSchemas)
		}
		return map[string]interface{}{}
	}

	if schema.Example != nil {
		return schema.Example
	}

	switch schema.Type {
	case "object":
		obj := make(map[string]interface{})
		for name, prop := range schema.Properties {
			obj[name] = g.generateExampleFromSchema(prop, componentSchemas)
		}
		return obj
	case "array":
		if schema.Items != nil {
			return []interface{}{g.generateExampleFromSchema(*schema.Items, componentSchemas)}
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
