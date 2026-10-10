package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/events/core"
	"github.com/xraph/forge/extensions/events/stores"
)

type deliveryBroker struct {
	mu                                         sync.Mutex
	publishes, connects, closes, subscriptions int
	publish                                    func(context.Context) error
	connectErr, subscribeErr, closeErr         error
	onSubscribe                                func()
}

func (b *deliveryBroker) Connect(context.Context, any) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connects++

	return b.connectErr
}
func (b *deliveryBroker) Publish(ctx context.Context, _ string, _ core.Event) error {
	b.mu.Lock()
	b.publishes++
	fn := b.publish
	b.mu.Unlock()

	if fn != nil {
		return fn(ctx)
	}

	return nil
}
func (b *deliveryBroker) Subscribe(context.Context, string, core.EventHandler) error {
	b.mu.Lock()
	b.subscriptions++
	fn, err := b.onSubscribe, b.subscribeErr
	b.mu.Unlock()

	if fn != nil {
		fn()
	}

	return err
}
func (b *deliveryBroker) Unsubscribe(context.Context, string, string) error { return nil }
func (b *deliveryBroker) Close(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closes++

	return b.closeErr
}
func (b *deliveryBroker) HealthCheck(context.Context) error { return nil }
func (b *deliveryBroker) GetStats() map[string]any          { return map[string]any{} }

func testDeliveryBus(t *testing.T, config EventBusConfig) *EventBusImpl {
	t.Helper()

	bus, err := NewEventBus(EventBusOptions{Store: stores.NewMemoryEventStore(nil, nil), HandlerRegistry: core.NewHandlerRegistry(nil, nil), Config: config})
	require.NoError(t, err)

	return bus.(*EventBusImpl)
}

func TestBusRejectsMissingRoutes(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	require.ErrorContains(t, bus.Start(context.Background()), "at least one broker")
	require.NoError(t, bus.RegisterBroker("available", &deliveryBroker{}))
	bus.defaultBroker = "missing"
	require.ErrorContains(t, bus.Start(context.Background()), "default broker missing")
}

func TestBusDefaultRouteAndCommittedBridge(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{DefaultBroker: "selected"})
	selected, other := &deliveryBroker{}, &deliveryBroker{}
	require.NoError(t, bus.RegisterBroker("selected", selected))
	require.NoError(t, bus.RegisterBroker("other", other))
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	event := core.NewEvent("trade.committed", "trade-1", "effect")
	require.NoError(t, bus.Publish(context.Background(), event))
	require.Equal(t, 1, selected.publishes)
	require.Zero(t, other.publishes)
	require.NoError(t, bus.PublishTo(context.Background(), "selected", event))
	count, err := bus.store.GetEventCount(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.Equal(t, 2, selected.publishes)
}

func TestBusFanoutReturnsPartialFailure(t *testing.T) {
	failure := errors.New("destination unavailable")
	bus := testDeliveryBus(t, EventBusConfig{})
	good := &deliveryBroker{}
	bad := &deliveryBroker{publish: func(context.Context) error { return failure }}

	require.NoError(t, bus.RegisterBroker("good", good))
	require.NoError(t, bus.RegisterBroker("bad", bad))
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	err := bus.Publish(context.Background(), core.NewEvent("trade.committed", "trade-1", "effect"))
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "broker bad")
	require.Equal(t, 1, good.publishes)
	require.Equal(t, 1, bad.publishes)
}

func TestBusStopCancelsAdmittedPublishBeforeClose(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	broker := &deliveryBroker{publish: func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(exited)

		return ctx.Err()
	}}
	bus := testDeliveryBus(t, EventBusConfig{WorkerCount: 2})
	require.NoError(t, bus.RegisterBroker("broker", broker))
	require.NoError(t, bus.Start(context.Background()))

	published := make(chan error, 1)
	go func() {
		published <- bus.PublishTo(context.Background(), "broker", core.NewEvent("trade.committed", "trade-1", "effect"))
	}()

	<-entered

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, bus.Stop(stopCtx))

	select {
	case <-exited:
	default:
		t.Fatal("broker closed before publish exited")
	}

	require.ErrorIs(t, <-published, context.Canceled)
	require.Equal(t, 1, broker.closes)
	require.Error(t, bus.PublishTo(context.Background(), "broker", core.NewEvent("trade.committed", "trade-2", "effect")))
}

func TestBusStopDeadlineKeepsCleanupAndBlocksRestart(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	broker := &deliveryBroker{publish: func(context.Context) error {
		close(entered)
		<-release

		return nil
	}}
	bus := testDeliveryBus(t, EventBusConfig{})
	require.NoError(t, bus.RegisterBroker("broker", broker))
	require.NoError(t, bus.Start(context.Background()))

	published := make(chan error, 1)
	go func() {
		published <- bus.PublishTo(context.Background(), "broker", core.NewEvent("trade.committed", "trade-1", "effect"))
	}()

	<-entered

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, bus.Stop(stopCtx), context.DeadlineExceeded)
	require.ErrorContains(t, bus.Start(context.Background()), "stopping")
	require.Error(t, bus.RegisterBroker("other", &deliveryBroker{}))
	close(release)
	require.NoError(t, <-published)
	require.NoError(t, bus.Stop(context.Background()))
	require.NoError(t, bus.Start(context.Background()))
	require.NoError(t, bus.Stop(context.Background()))
	require.Equal(t, 2, broker.closes)
}

func TestBusRestartRestoresSubscriptionsOnceAndAllowsCallbacks(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{WorkerCount: 1})
	broker := &deliveryBroker{onSubscribe: func() { _ = bus.GetStats() }}
	require.NoError(t, bus.RegisterBroker("broker", broker))
	require.NoError(t, bus.Start(context.Background()))

	handler := core.NewTypedEventHandler("logical-consumer", []string{"trade.committed"}, func(context.Context, *core.Event) error { return nil })
	require.NoError(t, bus.Subscribe("trade.committed", handler))
	require.ErrorContains(t, bus.Subscribe("trade.committed", handler), "already subscribed")
	require.NoError(t, bus.Stop(context.Background()))
	require.NoError(t, bus.Start(context.Background()))
	require.NoError(t, bus.Start(context.Background()))
	require.Equal(t, 2, broker.subscriptions)
	require.NoError(t, bus.Stop(context.Background()))
}

func TestBusStartupRollsBackConnectedBrokers(t *testing.T) {
	failure := errors.New("connect failed")
	bus := testDeliveryBus(t, EventBusConfig{})
	first := &deliveryBroker{}
	require.NoError(t, bus.RegisterBroker("a-connected", first))
	require.NoError(t, bus.RegisterBroker("b-failed", &deliveryBroker{connectErr: failure}))
	require.ErrorIs(t, bus.Start(context.Background()), failure)
	require.Equal(t, 1, first.closes)
	require.Error(t, bus.Publish(context.Background(), core.NewEvent("trade.committed", "trade-1", "effect")))
}

func TestBusCanceledPublishDoesNotSaveOrDispatch(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	broker := &deliveryBroker{}
	require.NoError(t, bus.RegisterBroker("broker", broker))
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, bus.Publish(ctx, core.NewEvent("trade.committed", "trade-1", "effect")), context.Canceled)
	require.Zero(t, broker.publishes)

	count, err := bus.store.GetEventCount(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestBusSubscriptionFailureRollsBackRegistry(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	good, bad := &deliveryBroker{}, &deliveryBroker{subscribeErr: errors.New("subscribe failed")}
	require.NoError(t, bus.RegisterBroker("a-good", good))
	require.NoError(t, bus.RegisterBroker("b-bad", bad))
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	handler := core.NewTypedEventHandler("logical-consumer", []string{"trade.committed"}, func(context.Context, *core.Event) error { return nil })
	require.ErrorContains(t, bus.Subscribe("trade.committed", handler), "b-bad")
	require.Empty(t, bus.handlerRegistry.GetHandlers("trade.committed"))
}

type lifecycleStore struct {
	core.EventStore

	closes              int
	healthErr, closeErr error
}

func (s *lifecycleStore) Close(context.Context) error {
	s.closes++

	return s.closeErr
}
func (s *lifecycleStore) HealthCheck(context.Context) error { return s.healthErr }

func TestServiceInjectedStoreBorrowedAcrossRestart(t *testing.T) {
	store := &lifecycleStore{EventStore: stores.NewMemoryEventStore(nil, nil)}
	config := DefaultConfig()
	config.Store.Type = "authoritative"
	service := NewEventService(config, nil, nil, WithEventStore(store))
	require.NoError(t, service.Start(context.Background()))
	bus := service.GetEventBus()
	require.Same(t, store, service.GetEventStore())
	require.NoError(t, bus.Publish(context.Background(), core.NewEvent("trade.committed", "trade-1", "effect")))
	require.NoError(t, service.Stop(context.Background()))
	require.NoError(t, service.Start(context.Background()))
	require.Same(t, bus, service.GetEventBus())
	count, err := service.GetEventStore().GetEventCount(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.NoError(t, service.Stop(context.Background()))
	require.Zero(t, store.closes)
}

func TestServiceOwnedFactoryClosesEachRunAndRollsBack(t *testing.T) {
	var created []*lifecycleStore

	factory := func(context.Context, StoreConfig) (core.EventStore, error) {
		store := &lifecycleStore{EventStore: stores.NewMemoryEventStore(nil, nil)}
		created = append(created, store)

		return store, nil
	}

	service := NewEventService(DefaultConfig(), nil, nil, WithEventStoreFactory(factory))
	for range 2 {
		require.NoError(t, service.Start(context.Background()))
		require.NoError(t, service.Stop(context.Background()))
	}

	require.Len(t, created, 2)
	require.Equal(t, 1, created[0].closes)
	require.Equal(t, 1, created[1].closes)

	unhealthy := &lifecycleStore{EventStore: stores.NewMemoryEventStore(nil, nil), healthErr: errors.New("unhealthy")}
	service = NewEventService(DefaultConfig(), nil, nil, WithEventStoreFactory(func(context.Context, StoreConfig) (core.EventStore, error) { return unhealthy, nil }))
	require.ErrorContains(t, service.Start(context.Background()), "unhealthy")
	require.Equal(t, 1, unhealthy.closes)
}

func TestServiceInvalidConfigurationFailsStartup(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"persistent":       func(c *Config) { c.Store.Type = "postgres" },
		"unknown broker":   func(c *Config) { c.Brokers[0].Type = "typo" },
		"no broker":        func(c *Config) { c.Brokers = nil },
		"missing default":  func(c *Config) { c.Bus.DefaultBroker = "missing" },
		"duplicate broker": func(c *Config) { c.Brokers = append(c.Brokers, c.Brokers[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			config := DefaultConfig()
			mutate(&config)
			require.Error(t, NewEventService(config, nil, nil).Start(context.Background()))
		})
	}
}

func TestServiceFactoryNilAndErrors(t *testing.T) {
	invalidFactory := func(context.Context, StoreConfig) (core.EventStore, error) {
		//nolint:nilnil // Tests the service rejection of an invalid factory result.
		return nil, nil
	}
	service := NewEventService(DefaultConfig(), nil, nil, WithEventStoreFactory(invalidFactory))
	require.ErrorContains(t, service.Start(context.Background()), "returned nil")

	failure := errors.New("factory failed")
	service = NewEventService(DefaultConfig(), nil, nil, WithEventStoreFactory(func(context.Context, StoreConfig) (core.EventStore, error) { return nil, failure }))
	require.ErrorIs(t, service.Start(context.Background()), failure)
}

func TestServiceOwnedFactoryRollsBackBusCreationFailure(t *testing.T) {
	store := &lifecycleStore{EventStore: stores.NewMemoryEventStore(nil, nil)}
	config := DefaultConfig()
	config.Bus.DefaultBroker = "broken"
	config.Brokers = []BrokerConfig{{Name: "broken", Type: "nats", Enabled: true, Config: map[string]any{"url": "nats://127.0.0.1:invalid-port"}}}
	service := NewEventService(config, nil, nil, WithEventStoreFactory(func(context.Context, StoreConfig) (core.EventStore, error) { return store, nil }))
	require.ErrorContains(t, service.Start(context.Background()), "failed to start event bus")
	require.Equal(t, 1, store.closes)
	require.Nil(t, service.GetEventBus())
}

func TestBusConcurrentRouteSelectionAndPublish(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{DefaultBroker: "first"})
	require.NoError(t, bus.RegisterBroker("first", &deliveryBroker{}))
	require.NoError(t, bus.RegisterBroker("second", &deliveryBroker{}))
	require.NoError(t, bus.Start(context.Background()))

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 50 {
				require.NoError(t, bus.SetDefaultBroker("first"))
				require.NoError(t, bus.Publish(context.Background(), core.NewEvent("trade.committed", "trade-1", "effect")))
				require.NoError(t, bus.SetDefaultBroker("second"))
			}
		})
	}

	wg.Wait()
	require.NoError(t, bus.Stop(context.Background()))
}

func TestBusStopReportsBrokerCloseFailure(t *testing.T) {
	failure := errors.New("close failed")
	bus := testDeliveryBus(t, EventBusConfig{})
	require.NoError(t, bus.RegisterBroker("broker", &deliveryBroker{closeErr: failure}))
	require.NoError(t, bus.Start(context.Background()))
	require.ErrorIs(t, bus.Stop(context.Background()), failure)
	require.ErrorIs(t, bus.Stop(context.Background()), failure)
}

func TestServiceRejectsExplicitNilInjection(t *testing.T) {
	var typedNil *lifecycleStore
	for _, option := range []EventServiceOption{WithEventStore(nil), WithEventStore(typedNil), WithEventStoreFactory(nil)} {
		require.Error(t, NewEventService(DefaultConfig(), nil, nil, option).Start(context.Background()))
	}
}

func TestServiceStopDeadlinePreservesOwnedStoreUntilPublishExits(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	bus := testDeliveryBus(t, EventBusConfig{})
	broker := &deliveryBroker{publish: func(context.Context) error {
		close(entered)
		<-release

		return nil
	}}
	require.NoError(t, bus.RegisterBroker("broker", broker))
	require.NoError(t, bus.Start(context.Background()))
	store := &lifecycleStore{EventStore: bus.store}
	service := NewEventService(DefaultConfig(), nil, nil)
	service.bus, service.store, service.started, service.ownsStore = bus, store, true, true

	published := make(chan error, 1)
	go func() {
		published <- bus.PublishTo(context.Background(), "broker", core.NewEvent("trade.committed", "trade-1", "effect"))
	}()

	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, service.Stop(ctx), context.DeadlineExceeded)
	require.ErrorContains(t, service.Start(context.Background()), "stopping")
	require.Zero(t, store.closes)
	close(release)
	require.NoError(t, <-published)
	require.NoError(t, service.Stop(context.Background()))
	require.Equal(t, 1, store.closes)
	require.NoError(t, service.Stop(context.Background()))
	require.Equal(t, 1, store.closes)
}

func TestServiceBorrowedStoreHealthFailureDoesNotCloseStore(t *testing.T) {
	failure := errors.New("authoritative store unavailable")
	store := &lifecycleStore{EventStore: stores.NewMemoryEventStore(nil, nil), healthErr: failure}
	service := NewEventService(DefaultConfig(), nil, nil, WithEventStore(store))
	require.ErrorIs(t, service.Start(context.Background()), failure)
	require.Zero(t, store.closes)
}
