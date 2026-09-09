package generator

import (
	"reflect"
	"testing"
	"time"

	"github.com/devenock/specyl/pkg/config"
	"github.com/devenock/specyl/pkg/models"
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

// TestConvertToPostman_DeterministicFolderOrder is a regression test: folders
// were previously built by ranging a Go map directly, so collection.json's
// item order (and therefore its diff against the previous run) was
// randomized on every invocation, defeating the tool's own CI-friendliness.
func TestConvertToPostman_DeterministicFolderOrder(t *testing.T) {
	g := &PostmanGenerator{config: &config.Config{}}
	spec := &models.APISpec{
		Endpoints: []models.Endpoint{
			{Method: "GET", Path: "/zebras", Tags: []string{"zebras"}, Responses: map[int]models.Response{}},
			{Method: "GET", Path: "/apples", Tags: []string{"apples"}, Responses: map[int]models.Response{}},
			{Method: "GET", Path: "/mangoes", Tags: []string{"mangoes"}, Responses: map[int]models.Response{}},
		},
	}

	var first []string
	for i := 0; i < 20; i++ {
		collection := g.convertToPostman(spec)
		var names []string
		for _, item := range collection.Item {
			names = append(names, item.Name)
		}
		if i == 0 {
			first = names
			continue
		}
		if !reflect.DeepEqual(names, first) {
			t.Fatalf("run %d: folder order = %v, want the stable order from run 0: %v", i, names, first)
		}
	}
	want := []string{"apples", "mangoes", "zebras"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("folder order = %v, want alphabetical %v", first, want)
	}
}

func TestRequestName(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"GET", "/users", "get_users"},
		{"GET", "/users/:id", "get_user"},
		{"POST", "/products", "add_product"},
		{"DELETE", "/products/{id}", "delete_product"},
		{"PUT", "/categories/:id", "update_category"},
	}
	for _, tt := range tests {
		if got := requestName(tt.method, tt.path); got != tt.want {
			t.Errorf("requestName(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestGenerateExampleFromSchema_BasicTypes(t *testing.T) {
	g := &PostmanGenerator{config: &config.Config{}}
	tests := []struct {
		name   string
		schema models.Schema
		want   interface{}
	}{
		{"string", models.Schema{Type: "string"}, "string"},
		{"integer", models.Schema{Type: "integer"}, 0},
		{"number", models.Schema{Type: "number"}, 0.0},
		{"boolean", models.Schema{Type: "boolean"}, false},
		{"explicit example wins", models.Schema{Type: "string", Example: "wins"}, "wins"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.generateExampleFromSchema(tt.schema, nil)
			if got != tt.want {
				t.Errorf("got %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}
