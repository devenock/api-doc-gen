package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devenock/specyl/pkg/config"
	"github.com/devenock/specyl/pkg/models"
)

// determinismTestSpec builds a spec with enough map-keyed data (multiple
// tags, multiple schema properties, multiple registered models) that any
// output path ranging a map without sorting first would have a real chance
// of producing different byte output across runs.
func determinismTestSpec() *models.APISpec {
	return &models.APISpec{
		Title:   "Determinism Test API",
		Version: "1.0.0",
		Endpoints: []models.Endpoint{
			{Method: "GET", Path: "/zebras", Tags: []string{"zebras"}, Responses: map[int]models.Response{200: {Description: "ok"}}},
			{Method: "GET", Path: "/apples", Tags: []string{"apples"}, Responses: map[int]models.Response{200: {Description: "ok"}}},
			{
				Method: "POST", Path: "/mangoes", Tags: []string{"mangoes"},
				RequestBody: &models.RequestBody{
					Required: true,
					Content: map[string]models.Content{"application/json": {Schema: models.Schema{
						Type: "object",
						Properties: map[string]models.Schema{
							"a": {Type: "string"}, "b": {Type: "integer"},
							"c": {Type: "boolean"}, "d": {Type: "number"},
						},
					}}},
				},
				Responses: map[int]models.Response{201: {Description: "created"}},
			},
		},
		Models: map[string]models.Schema{
			"Widget": {Type: "object", Properties: map[string]models.Schema{
				"id": {Type: "string"}, "name": {Type: "string"}, "price": {Type: "number"},
			}},
			"Gadget": {Type: "object", Properties: map[string]models.Schema{
				"id": {Type: "string"}, "kind": {Type: "string"},
			}},
		},
	}
}

// TestSwaggerGenerate_DeterministicAcrossRuns is a regression test (review
// §6.3): generating from the identical spec repeatedly must produce
// byte-identical output. Go map iteration order is not guaranteed stable
// even across repeated iterations of the same map within one process, so
// any output path that ranges a map without sorting first has a real chance
// of being caught by this across enough iterations.
func TestSwaggerGenerate_DeterministicAcrossRuns(t *testing.T) {
	spec := determinismTestSpec()
	var first map[string][]byte
	for i := 0; i < 10; i++ {
		dir := t.TempDir()
		cfg := &config.Config{Output: dir, Title: spec.Title, Version: spec.Version, Quiet: true}
		if err := NewSwaggerGenerator(cfg).Generate(spec); err != nil {
			t.Fatalf("run %d: Generate: %v", i, err)
		}
		got := map[string][]byte{}
		for _, f := range []string{"openapi.json", "openapi.yaml"} {
			b, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				t.Fatalf("run %d: read %s: %v", i, f, err)
			}
			got[f] = b
		}
		if first == nil {
			first = got
			continue
		}
		for _, f := range []string{"openapi.json", "openapi.yaml"} {
			if string(got[f]) != string(first[f]) {
				t.Fatalf("run %d: %s differs from run 0's output:\n--- run 0 ---\n%s\n--- run %d ---\n%s",
					i, f, first[f], i, got[f])
			}
		}
	}
}

func TestPostmanGenerate_DeterministicAcrossRuns(t *testing.T) {
	spec := determinismTestSpec()
	var first []byte
	for i := 0; i < 10; i++ {
		dir := t.TempDir()
		cfg := &config.Config{Output: dir, Title: spec.Title, Version: spec.Version, Quiet: true}
		if err := NewPostmanGenerator(cfg).Generate(spec); err != nil {
			t.Fatalf("run %d: Generate: %v", i, err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "collection.json"))
		if err != nil {
			t.Fatalf("run %d: read collection.json: %v", i, err)
		}
		if first == nil {
			first = got
			continue
		}
		if string(got) != string(first) {
			t.Fatalf("run %d: collection.json differs from run 0's output:\n--- run 0 ---\n%s\n--- run %d ---\n%s",
				i, first, i, got)
		}
	}
}
