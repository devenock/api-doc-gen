package generator

import (
	"fmt"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// Generator defines the interface for documentation generators
type Generator interface {
	Generate(spec *models.APISpec) error
}

// NewGenerator creates a new generator based on the documentation type
func NewGenerator(docType string, cfg *config.Config) (Generator, error) {
	switch docType {
	case "swagger":
		return NewSwaggerGenerator(cfg), nil
	case "postman":
		return NewPostmanGenerator(cfg), nil
	case "custom":
		return NewCustomGenerator(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported documentation type: %s", docType)
	}
}
