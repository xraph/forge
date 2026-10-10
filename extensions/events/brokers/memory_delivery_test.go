package brokers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/events/core"
)

func TestMemoryBrokerCloseCancelsHandlersAndPublish(t *testing.T) {
	broker := NewMemoryBroker(nil, nil)
	require.NoError(t, broker.Connect(t.Context(), nil))

	started := make(chan struct{}, 1)

	require.NoError(t, broker.Subscribe(t.Context(), "test", testHandler("handler", func(ctx context.Context, _ *core.Event) error {
		select {
		case started <- struct{}{}:
		default:
		}

		<-ctx.Done()

		return ctx.Err()
	})))
	require.NoError(t, broker.Publish(t.Context(), "test", *core.NewEvent("test", "account", nil)))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 20 {
				_ = broker.Publish(t.Context(), "test", *core.NewEvent("test", "account", nil))
			}
		})
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	require.NoError(t, broker.Close(ctx))
	workers.Wait()
	require.Error(t, broker.Publish(t.Context(), "test", *core.NewEvent("test", "account", nil)))
	require.NoError(t, broker.Connect(t.Context(), nil))
	require.NoError(t, broker.Close(ctx))
}
