package events

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/events/brokers"
	"github.com/xraph/forge/extensions/events/core"
)

func TestBusAnonymousUnsubscribeDoesNotRestoreDuplicates(t *testing.T) {
	for _, completion := range []string{"success", "retry", "shutdown"} {
		t.Run(completion, func(t *testing.T) {
			bus := testDeliveryBus(t, EventBusConfig{})
			require.NoError(t, bus.RegisterBroker("a-memory", brokers.NewMemoryBroker(nil, nil)))
			require.NoError(t, bus.RegisterBroker("z-memory", brokers.NewMemoryBroker(nil, nil)))

			if completion != "success" {
				require.NoError(t, bus.RegisterBroker("b-transient", &transientUnsubscriber{}))
			}

			require.NoError(t, bus.Start(t.Context()))
			t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

			var anonymousCalls atomic.Int64

			var secondAnonymousCalls atomic.Int64

			var namedCalls atomic.Int64

			anonymous := core.EventHandlerFunc(func(context.Context, *core.Event) error {
				anonymousCalls.Add(1)

				return nil
			})
			anotherAnonymous := core.EventHandlerFunc(func(context.Context, *core.Event) error {
				secondAnonymousCalls.Add(1)

				return nil
			})
			named := core.NewTypedEventHandler("ledger", []string{"test"}, func(context.Context, *core.Event) error {
				namedCalls.Add(1)

				return nil
			})

			require.NoError(t, bus.Subscribe("test", anonymous))
			require.NoError(t, bus.Subscribe("test", named))
			require.NoError(t, bus.Subscribe("test", anotherAnonymous))
			require.ErrorContains(t, bus.Subscribe("test", named), "already subscribed")

			require.NoError(t, bus.PublishTo(t.Context(), "a-memory", core.NewEvent("test", "account", "payload")))
			require.NoError(t, bus.PublishTo(t.Context(), "z-memory", core.NewEvent("test", "account", "payload")))
			require.Eventually(t, func() bool {
				return anonymousCalls.Load() == 2 && secondAnonymousCalls.Load() == 2 && namedCalls.Load() == 2
			}, time.Second, time.Millisecond)

			err := bus.Unsubscribe("test", anonymous.Name())
			if completion == "success" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "b-transient")
				require.Len(t, bus.handlerRegistry.GetHandlers("test"), 3, "failed removal must retain intent until completed")

				if completion == "retry" {
					require.NoError(t, bus.Unsubscribe("test", anonymous.Name()))
				} else {
					require.NoError(t, bus.Stop(t.Context()))
				}
			}

			registered := bus.handlerRegistry.GetHandlers("test")
			require.Len(t, registered, 1, "all anonymous intent must be removed together")
			require.Same(t, named, registered[0])
			require.Equal(t, 0, bus.GetStats()["pending_subscription_removals"])

			if completion != "shutdown" {
				require.ErrorContains(t, bus.Unsubscribe("test", anonymous.Name()), "not registered")
				require.NoError(t, bus.PublishTo(t.Context(), "a-memory", core.NewEvent("test", "account", "payload")))
				require.NoError(t, bus.PublishTo(t.Context(), "z-memory", core.NewEvent("test", "account", "payload")))
				require.Eventually(t, func() bool { return namedCalls.Load() == 4 }, time.Second, time.Millisecond)
				require.NoError(t, bus.Stop(t.Context()))
				require.Equal(t, int64(2), anonymousCalls.Load(), "memory brokers must remove the first callback")
				require.Equal(t, int64(2), secondAnonymousCalls.Load(), "memory brokers must remove the second callback")
			}

			previousNamedCalls := namedCalls.Load()

			require.NoError(t, bus.Start(t.Context()))
			require.NoError(t, bus.PublishTo(t.Context(), "a-memory", core.NewEvent("test", "account", "payload")))
			require.NoError(t, bus.PublishTo(t.Context(), "z-memory", core.NewEvent("test", "account", "payload")))
			require.Eventually(t, func() bool { return namedCalls.Load() == previousNamedCalls+2 }, time.Second, time.Millisecond)
			require.NoError(t, bus.Stop(t.Context()))
			require.Equal(t, int64(2), anonymousCalls.Load(), "first callback must stay removed after restart")
			require.Equal(t, int64(2), secondAnonymousCalls.Load(), "second callback must stay removed after restart")
		})
	}
}

func TestBusRejectsAnonymousSubscribeDuringPendingRemoval(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	flaky := &transientUnsubscriber{}

	require.NoError(t, bus.RegisterBroker("a-memory", brokers.NewMemoryBroker(nil, nil)))
	require.NoError(t, bus.RegisterBroker("b-transient", flaky))
	require.NoError(t, bus.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	anonymous := core.EventHandlerFunc(func(context.Context, *core.Event) error { return nil })
	require.NoError(t, bus.Subscribe("test", anonymous))
	require.ErrorContains(t, bus.Unsubscribe("test", anonymous.Name()), "b-transient")
	require.ErrorContains(t, bus.Subscribe("test", anonymous), "removal is pending")
	require.Equal(t, 1, flaky.subscriptions, "pending removal must reject admission before touching any broker")
	require.Len(t, bus.handlerRegistry.GetHandlers("test"), 1)

	require.NoError(t, bus.Subscribe("other", anonymous), "another topic must remain available")

	named := core.NewTypedEventHandler("ledger", []string{"test"}, func(context.Context, *core.Event) error { return nil })
	require.NoError(t, bus.Subscribe("test", named), "another name must remain available")
	require.NoError(t, bus.Unsubscribe("test", anonymous.Name()))
	require.Len(t, bus.handlerRegistry.GetHandlers("test"), 1)
	require.NoError(t, bus.Subscribe("test", anonymous), "completed removal must allow a fresh subscription")
}

func TestBusAnonymousFanoutRollbackPreservesPriorHandler(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	bad := &deliveryBroker{}

	require.NoError(t, bus.RegisterBroker("a-memory", brokers.NewMemoryBroker(nil, nil)))
	require.NoError(t, bus.RegisterBroker("b-failure", bad))
	require.NoError(t, bus.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	var originalCalls atomic.Int64

	var rejectedCalls atomic.Int64

	original := core.EventHandlerFunc(func(context.Context, *core.Event) error {
		originalCalls.Add(1)

		return nil
	})
	rejected := core.EventHandlerFunc(func(context.Context, *core.Event) error {
		rejectedCalls.Add(1)

		return nil
	})

	require.NoError(t, bus.Subscribe("test", original))

	bad.subscribeErr = context.DeadlineExceeded

	require.ErrorContains(t, bus.Subscribe("test", rejected), "b-failure")
	require.Equal(t, 2, bad.subscriptions)
	require.Len(t, bus.handlerRegistry.GetHandlers("test"), 1)
	require.Equal(t, original.Name(), bus.handlerRegistry.GetHandlers("test")[0].Name())
	require.NoError(t, bus.PublishTo(t.Context(), "a-memory", core.NewEvent("test", "account", "payload")))
	require.Eventually(t, func() bool { return originalCalls.Load() == 1 }, time.Second, time.Millisecond)
	require.NoError(t, bus.Stop(t.Context()))
	require.Zero(t, rejectedCalls.Load())

	bad.subscribeErr = nil

	require.NoError(t, bus.Start(t.Context()))
	require.NoError(t, bus.PublishTo(t.Context(), "a-memory", core.NewEvent("test", "account", "payload")))
	require.Eventually(t, func() bool { return originalCalls.Load() == 2 }, time.Second, time.Millisecond)
	require.NoError(t, bus.Stop(t.Context()))
	require.Zero(t, rejectedCalls.Load(), "failed callback must not return after restart")
}

func TestBusReservesAnonymousTransportNamespace(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	broker := &deliveryBroker{}

	require.NoError(t, bus.RegisterBroker("broker", broker))
	require.NoError(t, bus.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	handler := core.NewTypedEventHandler(anonymousSubscriptionPrefix+"user-name", []string{"test"}, func(context.Context, *core.Event) error { return nil })
	require.ErrorContains(t, bus.Subscribe("test", handler), "reserved for internal subscriptions")
	require.Zero(t, broker.subscriptions)
	require.Empty(t, bus.handlerRegistry.GetHandlers("test"))
}

type nameRecordingBroker struct {
	deliveryBroker

	subscriptionNames, removalNames []string
}

func (b *nameRecordingBroker) Subscribe(ctx context.Context, _ string, handler core.EventHandler) error {
	b.subscriptionNames = append(b.subscriptionNames, handler.Name())

	return b.deliveryBroker.Subscribe(ctx, "test", handler)
}

func (b *nameRecordingBroker) Unsubscribe(_ context.Context, _ string, name string) error {
	b.removalNames = append(b.removalNames, name)

	return nil
}

func TestBusAnonymousTransportIdentitySurvivesRestart(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	recorder := &nameRecordingBroker{}

	require.NoError(t, bus.RegisterBroker("broker", recorder))
	require.NoError(t, bus.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	anonymous := core.EventHandlerFunc(func(context.Context, *core.Event) error { return nil })
	named := core.NewTypedEventHandler("ledger", []string{"test"}, anonymous)
	require.NoError(t, bus.Subscribe("test", anonymous))
	require.NoError(t, bus.Subscribe("test", named))
	require.NoError(t, bus.Subscribe("test", anonymous))
	require.NotEqual(t, recorder.subscriptionNames[0], recorder.subscriptionNames[2])
	require.Equal(t, "ledger", recorder.subscriptionNames[1], "named transport identity must remain unchanged")

	registered := bus.handlerRegistry.GetHandlers("test")
	require.Equal(t, "anonymous-handler", registered[0].Name())
	require.Same(t, named, registered[1])
	require.Equal(t, "anonymous-handler", registered[2].Name())
	require.NoError(t, bus.Stop(t.Context()))
	require.NoError(t, bus.Start(t.Context()))
	require.Equal(t, recorder.subscriptionNames[:3], recorder.subscriptionNames[3:])
	require.NoError(t, bus.Unsubscribe("test", anonymous.Name()))
	require.ElementsMatch(t, []string{recorder.subscriptionNames[0], recorder.subscriptionNames[2]}, recorder.removalNames)
	require.Same(t, named, bus.handlerRegistry.GetHandlers("test")[0])
}

type blockingUnsubscribeBroker struct {
	deliveryBroker

	entered, release chan struct{}
}

func (b *blockingUnsubscribeBroker) Unsubscribe(context.Context, string, string) error {
	close(b.entered)
	<-b.release

	return context.DeadlineExceeded
}

func TestBusConcurrentAnonymousSubscribeCannotEscapeRemoval(t *testing.T) {
	bus := testDeliveryBus(t, EventBusConfig{})
	blocked := &blockingUnsubscribeBroker{entered: make(chan struct{}), release: make(chan struct{})}

	require.NoError(t, bus.RegisterBroker("a-memory", brokers.NewMemoryBroker(nil, nil)))
	require.NoError(t, bus.RegisterBroker("b-blocked", blocked))
	require.NoError(t, bus.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	anonymous := core.EventHandlerFunc(func(context.Context, *core.Event) error { return nil })
	require.NoError(t, bus.Subscribe("test", anonymous))

	removed, subscribed := make(chan error, 1), make(chan error, 1)
	go func() { removed <- bus.Unsubscribe("test", anonymous.Name()) }()

	<-blocked.entered

	go func() { subscribed <- bus.Subscribe("test", anonymous) }()

	close(blocked.release)

	require.ErrorContains(t, <-removed, "b-blocked")
	require.ErrorContains(t, <-subscribed, "removal is pending")
	require.Equal(t, 1, blocked.subscriptions)
	require.Len(t, bus.handlerRegistry.GetHandlers("test"), 1)
}

type rollbackMemoryBroker struct {
	core.MessageBroker

	failSubscribe, failRemoval bool
}

func (b *rollbackMemoryBroker) Subscribe(ctx context.Context, topic string, handler core.EventHandler) error {
	if err := b.MessageBroker.Subscribe(ctx, topic, handler); err != nil {
		return err
	}

	if b.failSubscribe {
		return context.DeadlineExceeded
	}

	return nil
}

func (b *rollbackMemoryBroker) Unsubscribe(ctx context.Context, topic, name string) error {
	if b.failRemoval {
		return context.DeadlineExceeded
	}

	return b.MessageBroker.Unsubscribe(ctx, topic, name)
}

func TestBusFailedRollbackRetainsCleanupForRetryOrShutdown(t *testing.T) {
	for _, completion := range []string{"retry", "shutdown"} {
		for _, prior := range []bool{true, false} {
			t.Run(completion+"/prior="+strconv.FormatBool(prior), func(t *testing.T) {
				bus := testDeliveryBus(t, EventBusConfig{})
				first := &rollbackMemoryBroker{MessageBroker: brokers.NewMemoryBroker(nil, nil)}
				second := &rollbackMemoryBroker{MessageBroker: brokers.NewMemoryBroker(nil, nil)}

				require.NoError(t, bus.RegisterBroker("a-first", first))
				require.NoError(t, bus.RegisterBroker("b-second", second))
				require.NoError(t, bus.Start(t.Context()))
				t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

				var originalCalls atomic.Int64

				var rejectedCalls atomic.Int64

				var namedCalls atomic.Int64

				original := core.EventHandlerFunc(func(context.Context, *core.Event) error {
					originalCalls.Add(1)

					return nil
				})
				rejected := core.EventHandlerFunc(func(context.Context, *core.Event) error {
					rejectedCalls.Add(1)

					return nil
				})
				named := core.NewTypedEventHandler("ledger", []string{"test"}, func(context.Context, *core.Event) error {
					namedCalls.Add(1)

					return nil
				})

				if prior {
					require.NoError(t, bus.Subscribe("test", original))
				}

				require.NoError(t, bus.Subscribe("test", named))

				first.failRemoval, second.failRemoval, second.failSubscribe = true, true, true

				require.ErrorIs(t, bus.Subscribe("test", rejected), context.DeadlineExceeded)

				publishBoth := func() {
					for _, name := range []string{"a-first", "b-second"} {
						require.NoError(t, bus.PublishTo(t.Context(), name, core.NewEvent("test", "account", "payload")))
					}
				}
				publishBoth()
				require.Eventually(t, func() bool { return rejectedCalls.Load() == 2 && namedCalls.Load() == 2 }, time.Second, time.Millisecond)
				require.Equal(t, 1, bus.GetStats()["pending_subscription_removals"])
				require.ErrorContains(t, bus.HealthCheck(t.Context()), "removals remain incomplete")
				require.ErrorContains(t, bus.Subscribe("test", rejected), "removal is pending")

				if prior {
					require.Eventually(t, func() bool { return originalCalls.Load() == 2 }, time.Second, time.Millisecond)
				}

				if completion == "retry" {
					require.ErrorIs(t, bus.Unsubscribe("test", original.Name()), context.DeadlineExceeded)

					if prior {
						require.Len(t, bus.handlerRegistry.GetHandlers("test"), 2, "incomplete removal must preserve successful registry intent")
					} else {
						require.Len(t, bus.handlerRegistry.GetHandlers("test"), 1)
					}

					first.failRemoval, second.failRemoval, second.failSubscribe = false, false, false

					require.NoError(t, bus.Unsubscribe("test", original.Name()), "cleanup must be addressable even without an earlier registration")
					publishBoth()
					require.Eventually(t, func() bool { return namedCalls.Load() == 4 }, time.Second, time.Millisecond)
					require.NoError(t, bus.Stop(t.Context()))
					require.Equal(t, int64(2), rejectedCalls.Load(), "successful logical removal must stop the rejected callback at both brokers")
				} else {
					require.ErrorContains(t, bus.HealthCheck(t.Context()), "removals remain incomplete")
					require.ErrorContains(t, bus.Subscribe("test", rejected), "removal is pending")
					require.NoError(t, bus.Stop(t.Context()))
				}

				require.Equal(t, 0, bus.GetStats()["pending_subscription_removals"])

				first.failRemoval, second.failRemoval, second.failSubscribe = false, false, false

				require.NoError(t, bus.Start(t.Context()))

				previousNamed := namedCalls.Load()
				previousOriginal := originalCalls.Load()

				publishBoth()
				require.Eventually(t, func() bool { return namedCalls.Load() == previousNamed+2 }, time.Second, time.Millisecond)
				require.NoError(t, bus.Stop(t.Context()))
				require.Equal(t, int64(2), rejectedCalls.Load(), "rejected callbacks must never restore")

				if completion == "shutdown" && prior {
					require.Equal(t, previousOriginal+2, originalCalls.Load(), "cleanup-only shutdown must preserve successful registry intent")
				} else {
					require.Equal(t, previousOriginal, originalCalls.Load())
				}
			})
		}
	}
}
