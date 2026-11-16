package router

import "testing"

// BenchmarkSchemaValidator_Patterns validates an object whose string fields
// carry a pattern and the email and uuid formats. Each of those used to compile
// its regular expression on every call.
func BenchmarkSchemaValidator_Patterns(b *testing.B) {
	schema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"slug":  {Type: "string", Pattern: `^[a-z0-9]+(?:-[a-z0-9]+)*$`},
			"code":  {Type: "string", Pattern: `^[A-Z]{3}-[0-9]{4}$`},
			"email": {Type: "string", Format: "email"},
			"id":    {Type: "string", Format: "uuid"},
		},
	}
	data := map[string]any{
		"slug":  "hello-world",
		"code":  "ABC-1234",
		"email": "someone@example.com",
		"id":    "3f1c8a52-9b1e-4c1a-8f2e-5d6b7a8c9d0e",
	}
	v := NewSchemaValidator()

	b.ReportAllocs()

	for b.Loop() {
		if err := v.ValidateRequest(schema, data); err != nil {
			b.Fatal(err)
		}
	}
}
