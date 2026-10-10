package brokers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/events/core"
)

type logicalAnonymousHandler struct{ core.EventHandler }

func (*logicalAnonymousHandler) Name() string        { return "forge-internal-anonymous:test" }
func (*logicalAnonymousHandler) LogicalName() string { return "anonymous-handler" }

func TestRedisStreamsRejectsAnonymousLogicalNameWithTransportIdentity(t *testing.T) {
	broker, err := NewRedisBroker(map[string]any{"enable_streams": true}, nil, nil)
	require.NoError(t, err)

	handler := &logicalAnonymousHandler{EventHandler: core.EventHandlerFunc(func(context.Context, *core.Event) error { return nil })}

	// Validation must reject the logical name before any Redis command is issued.
	broker.mu.Lock()
	defer broker.mu.Unlock()

	require.ErrorContains(t, broker.subscribeStream(t.Context(), "test", handler), "stable named handler")
	require.Empty(t, broker.subscriptions)
}
