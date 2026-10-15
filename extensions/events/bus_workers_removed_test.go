package events

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBusStatsHaveNoWorkers(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{DefaultBroker: "b", WorkerCount: 4, BufferSize: 8})
	require.NoError(t, bus.RegisterBroker("b", &deliveryBroker{}))
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	stats := bus.GetStats()
	require.NotContains(t, stats, "workers")
	require.NotContains(t, stats, "workers_count")
	require.Equal(t, 8, stats["buffer_size"]) // still reported: the field is deprecated, not removed
}
