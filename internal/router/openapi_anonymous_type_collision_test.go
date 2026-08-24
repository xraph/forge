package router

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/shared"
)

// Anonymous types have no name of their own, so the generator shows them all as
// "Object". That is a display choice, and it used to be an identity choice too:
// every unnamed type keyed the component registry under the same string, so the
// second one to arrive looked like a different type claiming a name the first
// already held. The generator reported that as a pinned-name conflict -- advice
// nobody could act on, since neither name was ever pinned -- and returned no
// document at all.
//
// One endpoint answering with free-form JSON and another answering with an
// empty body is all it takes, and that pair turns up in almost any real API.
func registerFreeFormResponse(t *testing.T, r Router) {
	t.Helper()
	require.NoError(t, r.GET("/manifest",
		func(ctx shared.Context, req *collisionEmptyRequest) (*map[string]any, error) {
			return &map[string]any{}, nil
		}))
}

func registerEmptyResponse(t *testing.T, r Router) {
	t.Helper()
	require.NoError(t, r.POST("/revoke",
		func(ctx shared.Context, req *collisionEmptyRequest) (*struct{}, error) {
			return &struct{}{}, nil
		}))
}

func TestOpenAPI_DistinctAnonymousResponsesDoNotCollide(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mapFirst bool
	}{
		{name: "MapFirst", mapFirst: true},
		{name: "StructFirst", mapFirst: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "AnonTypes", Version: "1.0.0"}))

			if tc.mapFirst {
				registerFreeFormResponse(t, r)
				registerEmptyResponse(t, r)
			} else {
				registerEmptyResponse(t, r)
				registerFreeFormResponse(t, r)
			}

			spec := r.OpenAPISpec()
			require.NotNil(t, spec,
				"two distinct unnamed types were reported as one name conflict, so no spec was generated")
			requireEveryRefResolves(t, spec)
		})
	}
}

// Generate returns the error alongside the spec, and a caller that asks for the
// spec twice must get the same answer both times. Guarding it here keeps the
// registry from carrying conflict state over between passes.
func TestOpenAPI_AnonymousResponsesGenerateIsRepeatable(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "AnonRepeat", Version: "1.0.0"}))
	registerFreeFormResponse(t, r)
	registerEmptyResponse(t, r)

	first := r.OpenAPISpec()
	require.NotNil(t, first)

	second := r.OpenAPISpec()
	require.NotNil(t, second, "a second Generate pass lost the spec the first one produced")
	require.Equal(t, len(first.Components.Schemas), len(second.Components.Schemas))
}
