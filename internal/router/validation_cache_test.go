package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompiledPattern_CachesAndSkipsInvalid(t *testing.T) {
	a := compiledPattern(`^[a-z]+-cache-test$`)
	require.NotNil(t, a)
	assert.Same(t, a, compiledPattern(`^[a-z]+-cache-test$`), "a pattern is compiled once")

	assert.Nil(t, compiledPattern(`([unclosed`))
	assert.Nil(t, compiledPattern(`([unclosed`), "an invalid pattern is remembered as invalid")
}

func TestSchemaValidator_PatternAndFormatsStillValidate(t *testing.T) {
	schema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"slug":  {Type: "string", Pattern: `^[a-z0-9]+(?:-[a-z0-9]+)*$`},
			"bad":   {Type: "string", Pattern: `([unclosed`},
			"email": {Type: "string", Format: "email"},
			"id":    {Type: "string", Format: "uuid"},
		},
	}
	v := NewSchemaValidator()

	require.NoError(t, v.ValidateRequest(schema, map[string]any{
		"slug":  "hello-world",
		"bad":   "anything", // an invalid pattern is skipped, not enforced
		"email": "someone@example.com",
		"id":    "3F1C8A52-9B1E-4C1A-8F2E-5D6B7A8C9D0E",
	}))

	err := v.ValidateRequest(schema, map[string]any{
		"slug":  "Hello World",
		"email": "not-an-email",
		"id":    "3f1c8a52-9b1e-1c1a-8f2e-5d6b7a8c9d0e", // version 1
	})
	require.Error(t, err)

	var verr *ValidationErrors
	require.ErrorAs(t, err, &verr)
	assert.Len(t, verr.Errors, 3)
}

// The validation pass used to compile three regular expressions per call.
func TestSchemaValidator_PatternsDoNotRecompile(t *testing.T) {
	schema := &Schema{Type: "string", Pattern: `^[A-Z]{3}-[0-9]{4}$`, Format: "email"}
	v := NewSchemaValidator()

	allocs := testing.AllocsPerRun(100, func() {
		_ = v.ValidateRequest(schema, "ABC-1234")
	})

	// Compiling either expression costs dozens of allocations; a cached
	// lookup plus the error bookkeeping costs a handful.
	assert.Less(t, allocs, 20.0)
}
