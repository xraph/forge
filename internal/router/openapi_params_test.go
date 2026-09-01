package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPathParamsFromPath_TypesAndWildcards(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantNames  []string
		wantSchema []string // OpenAPI "type" per parameter, in order
	}{
		{
			name:       "colon parameter is a string",
			path:       "/users/:id",
			wantNames:  []string{"id"},
			wantSchema: []string{"string"},
		},
		{
			name:       "brace parameter is a string",
			path:       "/users/{id}",
			wantNames:  []string{"id"},
			wantSchema: []string{"string"},
		},
		{
			name:       "int constraint becomes an integer",
			path:       "/users/{id:int}",
			wantNames:  []string{"id"},
			wantSchema: []string{"integer"},
		},
		{
			name:       "uint constraint becomes an integer",
			path:       "/pages/{n:uint}",
			wantNames:  []string{"n"},
			wantSchema: []string{"integer"},
		},
		{
			name:       "uuid constraint stays a string",
			path:       "/orders/{id:uuid}",
			wantNames:  []string{"id"},
			wantSchema: []string{"string"},
		},
		{
			name:       "wildcard is no longer dropped",
			path:       "/files/*",
			wantNames:  []string{"filepath"},
			wantSchema: []string{"string"},
		},
		{
			name:       "named wildcard uses its name",
			path:       "/files/*path",
			wantNames:  []string{"path"},
			wantSchema: []string{"string"},
		},
		{
			name:       "parameters and a wildcard together",
			path:       "/{org}/files/*",
			wantNames:  []string{"org", "filepath"},
			wantSchema: []string{"string", "string"},
		},
		{
			name:      "no parameters",
			path:      "/static/health",
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPathParamsFromPath(tt.path)

			names := make([]string, 0, len(got))
			types := make([]string, 0, len(got))

			for _, p := range got {
				names = append(names, p.Name)
				require.NotNil(t, p.Schema, "parameter %q must carry a schema", p.Name)
				types = append(types, p.Schema.Type)
			}

			if tt.wantNames == nil {
				assert.Empty(t, names)

				return
			}

			assert.Equal(t, tt.wantNames, names)
			assert.Equal(t, tt.wantSchema, types)
		})
	}
}

// The existing TestConvertPathToOpenAPIFormat in openapi_test.go covers the
// colon and brace cases. These are the two things pathspec adds: a constraint
// is erased in the OpenAPI template, and a wildcard becomes a named parameter
// instead of vanishing.
func TestConvertPathToOpenAPIFormat_ConstraintsAndWildcards(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/users/{id:int}", "/users/{id}"},
		{"/invoices/{status:enum(draft|sent)}", "/invoices/{status}"},
		{"/files/*", "/files/{filepath}"},
		{"/files/*path", "/files/{path}"},
		{"/{org}/files/*", "/{org}/files/{filepath}"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, ConvertPathToOpenAPIFormat(tt.in))
		})
	}
}

func TestExtractPathParamsFromPath_EnumBecomesASchemaEnum(t *testing.T) {
	got := extractPathParamsFromPath("/invoices/{status:enum(draft|sent|paid)}")

	require.Len(t, got, 1)
	require.NotNil(t, got[0].Schema)
	assert.Equal(t, "string", got[0].Schema.Type)
	assert.Equal(t, []any{"draft", "sent", "paid"}, got[0].Schema.Enum)
}
