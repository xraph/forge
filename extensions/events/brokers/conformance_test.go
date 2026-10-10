package brokers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/events/core"
)

var _ core.DurableMessageBroker = (*RedisBroker)(nil)

func TestDurableRouteRejectsEphemeralBrokers(t *testing.T) {
	redis, err := NewRedisBroker(map[string]any{"enable_streams": false}, nil, nil)
	require.NoError(t, err)

	for name, broker := range map[string]core.MessageBroker{"memory": NewMemoryBroker(nil, nil), "redis-pubsub": redis} {
		t.Run(name, func(t *testing.T) {
			durable, err := core.RequireDurableBroker(broker)
			require.ErrorIs(t, err, core.ErrDurableDeliveryUnavailable)
			require.Nil(t, durable)
		})
	}

	require.ErrorIs(t, redis.SubscribeDurable(t.Context(), "critical", testHandler("ledger", func(context.Context, *core.Event) error { return nil })), core.ErrDurableDeliveryUnavailable)
}

func TestDurableRouteReportsConfiguredGuaranteesBeforeConnect(t *testing.T) {
	broker, err := NewRedisBroker(map[string]any{"enable_streams": true, "consumer_group": "ledger", "consumer_name": "replica-1"}, nil, nil)
	require.NoError(t, err)
	durable, err := core.RequireDurableBroker(broker)
	require.NoError(t, err)
	capabilities, err := durable.DurableCapabilities()
	require.NoError(t, err)
	require.Equal(t, "topic", capabilities.Ordering)
	require.Equal(t, 1, capabilities.MaxInFlight)
	require.True(t, capabilities.AcknowledgeAfterHandler)
	require.True(t, capabilities.PendingRecovery)
	require.Equal(t, "ledger", capabilities.ConsumerGroup)
	require.Equal(t, "replica-1", capabilities.ReplicaID)
}

// TestDurableConformanceCommitAcknowledgementAndRecovery drives the optional
// interface against Redis rather than checking only its type or metadata.
func TestDurableConformanceCommitAcknowledgementAndRecovery(t *testing.T) {
	client, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{})
	durable, err := core.RequireDurableBroker(broker)
	require.NoError(t, err)

	started := make(chan string, 1)
	subCtx, cancel := context.WithCancel(t.Context())
	require.NoError(t, durable.SubscribeDurable(subCtx, topic, testHandler("ledger", func(ctx context.Context, event *core.Event) error {
		started <- event.ID

		<-ctx.Done()

		return errors.New("effects did not commit")
	})))
	event := core.NewEvent("test", "account", nil)
	require.NoError(t, durable.Publish(t.Context(), topic, *event))

	select {
	case id := <-started:
		require.Equal(t, event.ID, id)
	case <-time.After(3 * time.Second):
		t.Fatal("durable handler did not receive event")
	}

	group := streamGroup(broker.config.ConsumerGroup, "ledger")
	pending, err := client.XPending(t.Context(), topic, group).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), pending.Count)
	cancel()
	waitRedis(t, func() bool { return broker.GetStats()["subscriptions"] == 0 })
	pending, err = client.XPending(t.Context(), topic, group).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), pending.Count, "failed effects must remain recoverable")

	recovered := make(chan string, 1)

	require.NoError(t, durable.SubscribeDurable(t.Context(), topic, testHandler("ledger", func(_ context.Context, event *core.Event) error {
		recovered <- event.ID

		return nil
	})))

	select {
	case id := <-recovered:
		require.Equal(t, event.ID, id)
	case <-time.After(3 * time.Second):
		t.Fatal("durable handler did not recover pending event")
	}

	waitRedis(t, func() bool {
		pending, err := client.XPending(t.Context(), topic, group).Result()

		return err == nil && pending.Count == 0
	})
}

func TestRedisPubSubSetupCancellationKeepsTopicListener(t *testing.T) {
	client, topic := redisTestClient(t)
	broker, err := NewRedisBroker(map[string]any{"address": client.Options().Addr, "enable_streams": false}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, broker.Connect(t.Context(), nil))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		require.NoError(t, broker.Close(ctx))
	})

	first, second := make(chan string, 1), make(chan string, 1)
	setupCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	require.NoError(t, broker.Subscribe(setupCtx, topic, testHandler("first", func(_ context.Context, event *core.Event) error {
		first <- event.ID

		return nil
	})))
	cancel()

	nextSetup, nextCancel := context.WithCancel(t.Context())
	require.NoError(t, broker.Subscribe(nextSetup, topic, testHandler("second", func(_ context.Context, event *core.Event) error {
		second <- event.ID

		return nil
	})))
	nextCancel()

	event := core.NewEvent("test", "account", nil)
	require.NoError(t, broker.Publish(t.Context(), topic, *event))

	for _, received := range []<-chan string{first, second} {
		select {
		case id := <-received:
			require.Equal(t, event.ID, id)
		case <-time.After(3 * time.Second):
			t.Fatal("setup cancellation stopped Pub/Sub delivery")
		}
	}

	require.NoError(t, broker.HealthCheck(t.Context()))
	require.Equal(t, 1, broker.GetStats()["subscriptions"])
	require.NoError(t, broker.Unsubscribe(t.Context(), topic, "first"))
	require.NoError(t, broker.Unsubscribe(t.Context(), topic, "second"))
	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("third", func(_ context.Context, event *core.Event) error {
		first <- event.ID

		return nil
	})))
	require.NoError(t, broker.Publish(t.Context(), topic, *event))

	select {
	case id := <-first:
		require.Equal(t, event.ID, id)
	case <-time.After(3 * time.Second):
		t.Fatal("topic did not accept a fresh subscription")
	}
}

type incompleteDurableBroker struct{ core.MessageBroker }

func (incompleteDurableBroker) DurableCapabilities() (core.DurableBrokerCapabilities, error) {
	return core.DurableBrokerCapabilities{}, nil
}
func (incompleteDurableBroker) SubscribeDurable(context.Context, string, core.EventHandler) error {
	return nil
}

func TestDurableRouteRejectsIncompleteCapabilityContract(t *testing.T) {
	durable, err := core.RequireDurableBroker(incompleteDurableBroker{})
	require.ErrorIs(t, err, core.ErrDurableDeliveryUnavailable)
	require.Nil(t, durable)
}
