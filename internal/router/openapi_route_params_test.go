package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/vessel"
)

// paramNamed returns the named parameter from an operation, or nil.
func paramNamed(op *Operation, name, in string) *Parameter {
	if op == nil {
		return nil
	}

	for i := range op.Parameters {
		if op.Parameters[i].Name == name && op.Parameters[i].In == in {
			return &op.Parameters[i]
		}
	}

	return nil
}

// routeParamSpec registers one route carrying the given options and returns the
// generated operation for it.
func routeParamSpec(t *testing.T, path string, opts ...RouteOption) *Operation {
	t.Helper()

	router := NewRouter(WithContainer(vessel.New()))

	require.NoError(t, router.GET(path, func(ctx Context) error { return nil }, opts...))

	gen := newOpenAPIGenerator(OpenAPIConfig{Title: "Test", Version: "1"}, router, nil, "")

	spec, err := gen.Generate()
	require.NoError(t, err)

	// Look the operation up rather than keying on the registered path: the
	// generator rewrites :id into {id} on its way into the document.
	require.Len(t, spec.Paths, 1, "one route was registered")

	for _, item := range spec.Paths {
		require.NotNil(t, item.Get, "the registered GET should be in the document")

		return item.Get
	}

	return nil
}

// WithParameter used to write route metadata that nothing read. It compiled, it
// ran, it returned no error, and the parameter reached no document -- so every
// client generated off that document was missing a parameter the server honours.
//
// The declaration is the whole point of the option. If it does not arrive here,
// the option is a comment with a function call around it.
func TestWithParameter_ReachesTheDocument(t *testing.T) {
	op := routeParamSpec(t, "/things",
		WithOperationID("listThings"),
		WithParameter("tenant", "query", "Tenant to scope the listing to", true, "acme"),
	)

	param := paramNamed(op, "tenant", "query")
	require.NotNil(t, param, "the declared parameter should be in the document")

	assert.Equal(t, "Tenant to scope the listing to", param.Description)
	assert.True(t, param.Required)
	assert.Equal(t, "acme", param.Example)

	require.NotNil(t, param.Schema, "a parameter needs a schema for a client to type it")
	assert.Equal(t, "string", param.Schema.Type)
}

// The option carries no type, so the example is the only thing that can say what
// the parameter holds. A parameter typed off an integer example as a string is
// how a client ends up quoting a number.
func TestWithParameter_TypesTheSchemaFromTheExample(t *testing.T) {
	cases := []struct {
		name    string
		example any
		want    string
	}{
		{name: "string", example: "acme", want: "string"},
		{name: "integer", example: 25, want: "integer"},
		{name: "number", example: 1.5, want: "number"},
		{name: "boolean", example: true, want: "boolean"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := routeParamSpec(t, "/things",
				WithOperationID("listThings"),
				WithParameter("limit", "query", "", false, tc.example),
			)

			param := paramNamed(op, "limit", "query")
			require.NotNil(t, param)
			require.NotNil(t, param.Schema)
			assert.Equal(t, tc.want, param.Schema.Type)
		})
	}
}

// A repeatable parameter is an array, and a slice example is the only way this
// option can say so. Query parameters default to style form with explode true,
// so an array is already "send it once per value".
func TestWithParameter_TypesASliceExampleAsAnArray(t *testing.T) {
	op := routeParamSpec(t, "/things",
		WithOperationID("listThings"),
		WithParameter("resource", "query", "Repeatable", false, []string{"https://api.example.com"}),
	)

	param := paramNamed(op, "resource", "query")
	require.NotNil(t, param)
	require.NotNil(t, param.Schema)

	assert.Equal(t, "array", param.Schema.Type)
	require.NotNil(t, param.Schema.Items)
	assert.Equal(t, "string", param.Schema.Items.Type)
}

// With no example there is nothing to infer from, and a parameter with no schema
// is one a generator cannot type at all. String is the least surprising floor.
func TestWithParameter_FallsBackToStringWithoutAnExample(t *testing.T) {
	op := routeParamSpec(t, "/things",
		WithOperationID("listThings"),
		WithParameter("cursor", "query", "", false, nil),
	)

	param := paramNamed(op, "cursor", "query")
	require.NotNil(t, param)
	require.NotNil(t, param.Schema)
	assert.Equal(t, "string", param.Schema.Type)
	assert.Nil(t, param.Example, "no example was given, so none should be published")
}

// A path parameter named in the template is already described from the template.
// The declared one must not double it up, and the richer of the two wins, which
// is the same precedence the struct-derived sources already follow.
func TestWithParameter_DoesNotDuplicateAParameterAlreadyDescribed(t *testing.T) {
	op := routeParamSpec(t, "/things/:id",
		WithOperationID("getThing"),
		WithParameter("id", "path", "Thing ID", true, "123"),
	)

	count := 0

	for _, p := range op.Parameters {
		if p.Name == "id" && p.In == "path" {
			count++
		}
	}

	assert.Equal(t, 1, count, "the parameter should appear exactly once")
}

// Several declarations on one route all have to arrive, in any location.
func TestWithParameter_CarriesEveryDeclaration(t *testing.T) {
	op := routeParamSpec(t, "/things",
		WithOperationID("listThings"),
		WithParameter("tenant", "query", "", false, "acme"),
		WithParameter("X-Request-Id", "header", "", false, "abc"),
	)

	assert.NotNil(t, paramNamed(op, "tenant", "query"))
	assert.NotNil(t, paramNamed(op, "X-Request-Id", "header"))
}
