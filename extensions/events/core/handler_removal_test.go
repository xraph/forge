package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryUnregisterRemovesOnlyMatchingNameAndEventType(t *testing.T) {
	registry := NewHandlerRegistry(nil, nil)
	noop := EventHandlerFunc(func(context.Context, *Event) error { return nil })
	first := NewTypedEventHandler("first", []string{"test"}, noop)
	last := NewTypedEventHandler("last", []string{"test"}, noop)

	for _, handler := range []EventHandler{first, noop, last, noop} {
		require.NoError(t, registry.Register("test", handler))
	}

	require.NoError(t, registry.Register("other", noop))
	require.NoError(t, registry.Unregister("test", noop.Name()))
	require.Equal(t, []EventHandler{first, last}, registry.GetHandlers("test"))
	require.Len(t, registry.GetHandlers("other"), 1, "a name on another event type must remain")
	require.ErrorContains(t, registry.Unregister("test", noop.Name()), "not found")
	require.ErrorContains(t, registry.Unregister("missing", noop.Name()), "no handlers registered")
	require.Equal(t, []EventHandler{first, last}, registry.GetHandlers("test"))
}
