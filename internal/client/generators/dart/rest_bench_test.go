package dart

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// manyOperationsSpec builds a document with n resources, each with a GET and
// a PUT, which is the shape that exposed expandMethods replacing over the whole
// of rest.dart once per operation.
func manyOperationsSpec(n int) *client.APISpec {
	spec := &client.APISpec{
		Info:    client.APIInfo{Title: "Scale", Version: "1"},
		Schemas: make(map[string]*client.Schema, n),
	}

	for i := range n {
		model := fmt.Sprintf("Model%d", i)
		spec.Schemas[model] = &client.Schema{
			Type: "object",
			Properties: map[string]*client.Schema{
				"id":        {Type: "string"},
				"user_name": {Type: "string"},
			},
			Required: []string{"id"},
		}

		ref := &client.Schema{Ref: "#/components/schemas/" + model}
		idParam := client.Parameter{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}
		ok := map[int]*client.Response{200: {
			Description: "ok",
			Content:     map[string]*client.MediaType{"application/json": {Schema: ref}},
		}}

		spec.Endpoints = append(spec.Endpoints,
			client.Endpoint{
				Method: "GET", Path: fmt.Sprintf("/res%d/{id}", i), OperationID: fmt.Sprintf("res%d.get", i),
				PathParams: []client.Parameter{idParam}, Responses: ok,
			},
			client.Endpoint{
				Method: "PUT", Path: fmt.Sprintf("/res%d/{id}", i), OperationID: fmt.Sprintf("res%d.put", i),
				PathParams: []client.Parameter{idParam}, Responses: ok,
				RequestBody: &client.RequestBody{
					Required: true,
					Content:  map[string]*client.MediaType{"application/json": {Schema: ref}},
				},
			},
		)
	}

	return spec
}

func TestExpandMethodsFillsEveryPlaceholder(t *testing.T) {
	cfg := client.DefaultConfig()
	cfg.Language = "dart"
	cfg.PackageName = "scale"

	out, err := (&Generator{}).Generate(context.Background(), manyOperationsSpec(50), cfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	rest := out.Files["lib/src/rest.dart"]
	if strings.Contains(rest, "@@method:") {
		t.Fatal("rest.dart still carries a method placeholder")
	}

	for i := range 50 {
		for _, method := range []string{"GET", "PUT"} {
			if want := fmt.Sprintf("`%s /res%d/{id}`", method, i); !strings.Contains(rest, want) {
				t.Errorf("rest.dart has no method documented as %s", want)
			}
		}
	}
}

// BenchmarkGenerateManyOperations measures generation over a large document.
// Before expandMethods rendered in one pass, time grew with the square of the
// operation count.
func BenchmarkGenerateManyOperations(b *testing.B) {
	for _, n := range []int{500, 2000} {
		spec := manyOperationsSpec(n)

		cfg := client.DefaultConfig()
		cfg.Language = "dart"
		cfg.PackageName = "scale"

		b.Run(fmt.Sprintf("ops=%d", 2*n), func(b *testing.B) {
			for b.Loop() {
				if _, err := (&Generator{}).Generate(context.Background(), spec, cfg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
