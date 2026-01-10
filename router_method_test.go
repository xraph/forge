package forge_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

func TestWithMethod_Export(t *testing.T) {
	container := di.NewContainer()
	router := forge.NewRouter(forge.WithContainer(container))

	// Test that WithMethod is exported and works
	err := router.SSE("/events",
		func(ctx forge.Context) error {
			return ctx.WriteSSE("test", "data")
		},
		forge.WithMethod(http.MethodPost),
	)

	require.NoError(t, err)

	// Verify route was registered with POST method
	routes := router.Routes()
	found := false

	for _, route := range routes {
		if route.Path == "/events" {
			found = true

			assert.Equal(t, "POST", route.Method)
		}
	}

	assert.True(t, found)
}

func TestWithMethod_DefaultGET(t *testing.T) {
	container := di.NewContainer()
	router := forge.NewRouter(forge.WithContainer(container))

	// Test default behavior without WithMethod
	err := router.SSE("/events",
		func(ctx forge.Context) error {
			return ctx.WriteSSE("test", "data")
		},
	)

	require.NoError(t, err)

	// Verify route defaults to GET
	routes := router.Routes()

	for _, route := range routes {
		if route.Path == "/events" {
			assert.Equal(t, "GET", route.Method)
		}
	}
}

func TestWithMethod_CombineWithOtherOptions(t *testing.T) {
	container := di.NewContainer()
	router := forge.NewRouter(forge.WithContainer(container))

	// Test combining WithMethod with other options
	err := router.SSE("/events",
		func(ctx forge.Context) error {
			return nil
		},
		forge.WithMethod(http.MethodPost),
		forge.WithName("post-sse"),
		forge.WithTags("streaming", "events"),
		forge.WithSummary("POST SSE endpoint"),
	)

	require.NoError(t, err)

	// Verify all options are applied
	routes := router.Routes()

	for _, route := range routes {
		if route.Path == "/events" {
			assert.Equal(t, "POST", route.Method)
			assert.Equal(t, "post-sse", route.Name)
			assert.Contains(t, route.Tags, "streaming")
			assert.Contains(t, route.Tags, "events")
			assert.Equal(t, "POST SSE endpoint", route.Summary)
		}
	}
}
