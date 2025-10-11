package models

// APISpec represents the complete API specification
type APISpec struct {
	Title       string            `json:"title" yaml:"title"`
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description" yaml:"description"`
	BasePath    string            `json:"basePath" yaml:"basePath"`
	Servers     []Server          `json:"servers" yaml:"servers"`
	Endpoints   []Endpoint        `json:"endpoints" yaml:"endpoints"`
	Models      map[string]Schema `json:"models" yaml:"models"`
}

// server represent an API server
type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description" yaml:"description"`
}

// endpoint represent a single API endpoint
type Endpoint struct {
	Path        string                `json:"path" yaml:"path"`
	Method      string                `json:"method" yaml:"method"`
	Summary     string                `json:"summary" yaml:"summary"`
	Description string                `json:"description" yaml:"description"`
	Tags        []string              `json:"tags" yaml:"tags"`
	Parameters  []Parameter           `json:"parameters" yaml:"parameters"`
	RequestBody *RequestBody          `json:"requestBody, omitempty" yaml:"requestBody, omitempty"`
	Responses   map[int]Response      `json:"responses" yaml:"responses"`
	Security    []map[string][]string `json:"security, omitempty" yaml:"security, omitempty"`
}

// parameter represent an API parameter
type Parameter struct {
	Name        string      `json:"name" yaml:"name"`
	In          string      `json:"in" yaml:"in"`
	Description string      `json:description" yaml"description"`
	Required    bool        `json:"required" yaml:"required"`
	Schema      Schema      `json:"schema" yaml"schema"`
	Example     interface{} `json:"example, omitempty" yaml:"example, omitempty"`
}

// requestBody represent a request body/payload
type RequestBody struct {
	Description string `json:"description" yaml:"description"`
	Required    string `json:"required" yaml:"required"`
	Content     string `json:"content" yaml:"content"`
}

// content represents content with a schema
type Content struct {
	Schema  Schema      `json:"schema" yaml:"schema"`
	Example interface{} `json:"example, omitempty" yaml:"example, omitempty"`
}

// response represents API response
type Response struct {
	Description string             `json:"description" yaml:"description"`
	Content     map[string]Content `json:"content, omitempty" yaml:"content, omitempty"`
	Headers     map[string]Header  `json:"headers, omitempty" yaml:"headers, omitempty"`
}

// header represent a response header
type Header struct {
	Description string `json:"description" yaml:"desription"`
	Schema      Schema `json:"schema" yaml:"schema"`
}

// schema represent a data schema
type Schema struct {
	Type                 string            `json:"type" yaml:"type"`
	Format               string            `json:"format, omitempty" yaml:"format, omitempty"`
	Description          string            `json:"description, omitempty" yaml:"description, omitempty"`
	Properties           map[string]Schema `json:"properties, omitempty" yaml:"properties, omitempty"`
	Items                *Schema           `json:"items, omitempty" yaml:"items, omitempty"`
	Required             []string          `json:"required, omitempty" yaml:"required, omitempty"`
	Enum                 []interface{}     `json:"enum, omitempty" yaml:"enum, omitempty"`
	Example              interface{}       `json:"example, omitempty" yaml:"example, omitempty"`
	AdditionalProperties interface{}       `json:"additionalProperties, omitempty" yaml:"additionalProperties, omitempty"`
	Ref                  string            `json:"$ref, omitempty" yaml:"$ref, omitempty"`
}

// FrameWorkType represents supported frameworks
type FrameWorkType string

const (
	FrameWorkGin     FrameWorkType = "gin"
	FrameWorkFiber   FrameWorkType = "fiber"
	FrameWorkEcho    FrameWorkType = "echo"
	FrameWorkGorilla FrameWorkType = "gorilla"
	FrameWorkChi     FrameWorkType = "chi"
	FrameWorkUnknown FrameWorkType = "unknown"
)

// DocType represents documentation types
type DocType string

const (
	DocTypePostman DocType = "postman"
	DocTypeSwagger DocType = "swagger"
	DocTypeCustom  DocType = "custom"
)
