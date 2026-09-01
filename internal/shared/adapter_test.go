package shared

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A backend that supports nothing wide must be describable by the zero value,
// because that is what forge assumes for any adapter it cannot type-assert.
func TestCapabilities_ZeroValueSupportsNothing(t *testing.T) {
	var c Capabilities

	assert.False(t, c.MethodNotAllowed)
	assert.False(t, c.Constraints)
	assert.False(t, c.ConflictDetection)
	assert.False(t, c.TypedParams)
	assert.False(t, c.AnyMethod)
}

func TestRouteKind_LongLived(t *testing.T) {
	tests := []struct {
		kind RouteKind
		want bool
	}{
		{KindHTTP, false},
		{KindSSE, true},
		{KindWebSocket, true},
		{KindWebTransport, true},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.kind.LongLived())
		})
	}
}

// String is the on-the-wire name the AsyncAPI generator used to read out of
// the "route.type" metadata string, so these values are a compatibility
// contract, not cosmetics.
func TestRouteKind_StringMatchesLegacyMetadataValues(t *testing.T) {
	assert.Equal(t, "http", KindHTTP.String())
	assert.Equal(t, "sse", KindSSE.String())
	assert.Equal(t, "websocket", KindWebSocket.String())
	assert.Equal(t, "webtransport", KindWebTransport.String())
}

// KindHTTP must be the zero value so a RouteSpec built without thinking about
// streaming describes an ordinary request/response route.
func TestRouteSpec_ZeroValueIsAPlainHTTPRoute(t *testing.T) {
	var spec RouteSpec

	assert.Equal(t, KindHTTP, spec.Kind)
	assert.Equal(t, "", spec.Method, `an empty Method means "every method"`)
}
