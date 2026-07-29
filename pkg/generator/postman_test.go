package generator

import (
	"testing"
	"time"

	"github.com/devenock/api-doc-gen/pkg/config"
	"github.com/devenock/api-doc-gen/pkg/models"
)

// TestGenerateExampleFromSchema_SelfReferentialSchema is a regression test:
// generateExampleFromSchema used to recurse through $ref with no cycle guard,
// so any target project with a self-referential struct (a tree/category/
// comment-reply shape — all common) would stack-overflow the CLI while
// building the Postman example body. It now must terminate and still produce
// a sane example for the non-cyclic fields.
func TestGenerateExampleFromSchema_SelfReferentialSchema(t *testing.T) {
	g := &PostmanGenerator{config: &config.Config{}}
	components := map[string]models.Schema{
		"Category": {
			Type: "object",
			Properties: map[string]models.Schema{
				"name": {Type: "string"},
				"children": {
					Type:  "array",
					Items: &models.Schema{Ref: "#/components/schemas/Category"},
				},
			},
		},
	}
	ref := models.Schema{Ref: "#/components/schemas/Category"}

	result := runWithTimeout(t, func() interface{} {
		return g.generateExampleFromSchema(ref, components)
	})

	obj, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if obj["name"] != "string" {
		t.Errorf("obj[name] = %v, want the string placeholder", obj["name"])
	}
	if _, ok := obj["children"]; !ok {
		t.Errorf("expected \"children\" field to still be present, got %v", obj)
	}
}

// TestGenerateExampleFromSchema_MutualCycle covers an indirect cycle
// (A -> B -> A), not just direct self-reference.
func TestGenerateExampleFromSchema_MutualCycle(t *testing.T) {
	g := &PostmanGenerator{config: &config.Config{}}
	components := map[string]models.Schema{
		"A": {Type: "object", Properties: map[string]models.Schema{
			"b": {Ref: "#/components/schemas/B"},
		}},
		"B": {Type: "object", Properties: map[string]models.Schema{
			"a": {Ref: "#/components/schemas/A"},
		}},
	}
	ref := models.Schema{Ref: "#/components/schemas/A"}

	result := runWithTimeout(t, func() interface{} {
		return g.generateExampleFromSchema(ref, components)
	})
	if _, ok := result.(map[string]interface{}); !ok {
		t.Fatalf("expected map result, got %T", result)
	}
}

func runWithTimeout(t *testing.T, fn func() interface{}) interface{} {
	t.Helper()
	done := make(chan interface{}, 1)
	go func() { done <- fn() }()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("did not terminate on a cyclic schema (missing recursion guard)")
		return nil
	}
}
