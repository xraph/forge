package brokers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/events/core"
)

func redisTestClient(t *testing.T) (*redis.Client, string) {
	t.Helper()

	address := os.Getenv("FORGE_EVENTS_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set FORGE_EVENTS_REDIS_TEST_ADDR for real Redis qualification")
	}

	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(t.Context()).Err())

	topic := "forge-events-test:" + uuid.NewString()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		keys, _ := client.Keys(ctx, topic+"*").Result()
		if len(keys) > 0 {
			_ = client.Del(ctx, keys...).Err()
		}

		_ = client.Close()
	})

	return client, topic
}
func redisTestBroker(t *testing.T, config map[string]any) *RedisBroker {
	t.Helper()

	config["address"] = os.Getenv("FORGE_EVENTS_REDIS_TEST_ADDR")
	config["enable_streams"] = true
	config["stream_claim_idle"] = "30ms"
	config["stream_poll_interval"] = "5ms"
	config["stream_lease_duration"] = "90ms"
	broker, err := NewRedisBroker(config, nil, nil)
	require.NoError(t, err)
	require.NoError(t, broker.Connect(t.Context(), nil))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		require.NoError(t, broker.Close(ctx))
	})

	return broker
}
func testHandler(name string, handle func(context.Context, *core.Event) error) core.EventHandler {
	return core.NewTypedEventHandler(name, []string{"test"}, handle)
}
func waitRedis(t *testing.T, condition func() bool) {
	t.Helper()
	require.Eventually(t, condition, 3*time.Second, 5*time.Millisecond)
}

func TestRedisStreamsConfiguredSubscribe(t *testing.T) {
	_, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{})
	received := make(chan string, 1)

	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(_ context.Context, event *core.Event) error {
		received <- event.ID

		return nil
	})))
	event := core.NewEvent("test", "account", nil)
	require.NoError(t, broker.Publish(t.Context(), topic, *event))

	select {
	case id := <-received:
		require.Equal(t, event.ID, id)
	case <-time.After(time.Second):
		t.Fatal("Streams publication did not reach configured subscriber")
	}
}

func TestRedisStreamsFailureRetriesAndDeadLetter(t *testing.T) {
	client, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{"stream_max_deliveries": 3})

	var calls atomic.Int64

	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(_ context.Context, event *core.Event) error {
		calls.Add(1)

		return errors.New("transaction failed")
	})))
	event := core.NewEvent("test", "account", nil)
	require.NoError(t, broker.Publish(t.Context(), topic, *event))
	waitRedis(t, func() bool {
		n, _ := client.XLen(t.Context(), topic+":forge:dead-letter").Result()

		return n == 1
	})
	entries, err := client.XRange(t.Context(), topic+":forge:dead-letter", "-", "+").Result()
	require.NoError(t, err)
	require.Equal(t, int64(3), calls.Load())
	require.Equal(t, event.ID, entries[0].Values["event_id"])
	require.Equal(t, "transaction failed", entries[0].Values["error"])
	require.Equal(t, "3", entries[0].Values["attempts"])
	require.Equal(t, topic, entries[0].Values["source_stream"])
	require.NotEmpty(t, entries[0].Values["source_id"])
	pending, err := client.XPending(t.Context(), topic, streamGroup(broker.config.ConsumerGroup, "ledger")).Result()
	require.NoError(t, err)
	require.Zero(t, pending.Count)
}

func TestRedisStreamsRestartAndDuplicateDelivery(t *testing.T) {
	client, topic := redisTestClient(t)
	first := redisTestBroker(t, map[string]any{"consumer_name": "replica-a"})
	started := make(chan string, 1)

	require.NoError(t, first.Subscribe(t.Context(), topic, testHandler("ledger", func(ctx context.Context, event *core.Event) error {
		started <- event.ID

		<-ctx.Done()

		return ctx.Err()
	})))
	event := core.NewEvent("test", "account", nil)
	require.NoError(t, first.Publish(t.Context(), topic, *event))

	select {
	case id := <-started:
		require.Equal(t, event.ID, id)
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	require.NoError(t, first.Close(t.Context()))
	pending, err := client.XPending(t.Context(), topic, streamGroup(first.config.ConsumerGroup, "ledger")).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), pending.Count)
	second := redisTestBroker(t, map[string]any{"consumer_name": "replica-b"})
	received := make(chan string, 1)

	require.NoError(t, second.Subscribe(t.Context(), topic, testHandler("ledger", func(_ context.Context, event *core.Event) error {
		received <- event.ID

		return nil
	})))

	select {
	case id := <-received:
		require.Equal(t, event.ID, id)
	case <-time.After(time.Second):
		t.Fatal("pending event was not reclaimed")
	}

	waitRedis(t, func() bool {
		p, _ := client.XPending(t.Context(), topic, streamGroup(first.config.ConsumerGroup, "ledger")).Result()

		return p != nil && p.Count == 0
	})
}

func TestRedisStreamsPendingClaimAfterProcessDeath(t *testing.T) {
	client, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{})
	event := core.NewEvent("test", "account", nil)
	data, err := json.Marshal(event)
	require.NoError(t, err)

	group := streamGroup(broker.config.ConsumerGroup, "ledger")
	require.NoError(t, client.XGroupCreateMkStream(t.Context(), topic, group, "0").Err())
	require.NoError(t, client.XAdd(t.Context(), &redis.XAddArgs{Stream: topic, Values: map[string]any{"data": data}}).Err())
	require.NoError(t, client.XReadGroup(t.Context(), &redis.XReadGroupArgs{Group: group, Consumer: "dead-process", Streams: []string{topic, ">"}, Count: 1, Block: -1}).Err())

	received := make(chan string, 1)

	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(_ context.Context, event *core.Event) error {
		received <- event.ID

		return nil
	})))

	select {
	case id := <-received:
		require.Equal(t, event.ID, id)
	case <-time.After(time.Second):
		t.Fatal("dead process pending event was not reclaimed")
	}
}

func TestRedisStreamsReplicaOrderingAndBoundedInflight(t *testing.T) {
	client, topic := redisTestClient(t)
	first := redisTestBroker(t, map[string]any{})
	second := redisTestBroker(t, map[string]any{})

	var (
		active    atomic.Int64
		maxActive atomic.Int64
		mu        sync.Mutex
		seen      []string
	)

	handler := func(_ context.Context, event *core.Event) error {
		n := active.Add(1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}

		time.Sleep(40 * time.Millisecond)
		mu.Lock()

		seen = append(seen, event.ID)
		mu.Unlock()
		active.Add(-1)

		return nil
	}
	require.NoError(t, first.Subscribe(t.Context(), topic, testHandler("ledger", handler)))
	require.NoError(t, second.Subscribe(t.Context(), topic, testHandler("ledger", handler)))

	expected := make([]string, 6)
	for i := range expected {
		event := core.NewEvent("test", "account", nil)
		expected[i] = event.ID
		require.NoError(t, first.Publish(t.Context(), topic, *event))
	}

	waitRedis(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(seen) == len(expected)
	})
	mu.Lock()
	require.Equal(t, expected, seen)
	mu.Unlock()
	require.Equal(t, int64(1), maxActive.Load())
	groups, err := client.XInfoGroups(t.Context(), topic).Result()
	require.NoError(t, err)
	require.Len(t, groups, 1)
}

func TestRedisStreamsCapacityPreservesUnreadAndPending(t *testing.T) {
	client, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{"stream_max_len": 2})
	group := streamGroup(broker.config.ConsumerGroup, "offline")
	require.NoError(t, client.XGroupCreateMkStream(t.Context(), topic, group, "0").Err())

	for range 2 {
		require.NoError(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)))
	}

	require.ErrorContains(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)), "STREAM_CAPACITY")
	read, err := client.XReadGroup(t.Context(), &redis.XReadGroupArgs{Group: group, Consumer: "offline", Streams: []string{topic, ">"}, Count: 2, Block: -1}).Result()
	require.NoError(t, err)
	require.ErrorContains(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)), "STREAM_CAPACITY")

	for _, message := range read[0].Messages {
		require.NoError(t, client.XAck(t.Context(), topic, group, message.ID).Err())
	}

	require.NoError(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)))
	length, err := client.XLen(t.Context(), topic).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), length)
	require.ErrorContains(t, broker.Subscribe(t.Context(), topic, testHandler("new-group", func(context.Context, *core.Event) error { return nil })), "RECOVERY_REQUIRED")
}

func TestRedisStreamsHistoryGapStopsEffects(t *testing.T) {
	client, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{})
	group := streamGroup(broker.config.ConsumerGroup, "ledger")
	require.NoError(t, client.XGroupCreateMkStream(t.Context(), topic, group, "0").Err())
	require.NoError(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)))
	require.NoError(t, client.XTrimMaxLen(t.Context(), topic, 0).Err())

	var calls atomic.Int64

	require.Error(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(context.Context, *core.Event) error {
		calls.Add(1)

		return nil
	})))
	require.ErrorContains(t, broker.HealthCheck(t.Context()), "recovery required")
	require.Zero(t, calls.Load())
}

func TestRedisPubSubCompatibilityAndConcurrentClose(t *testing.T) {
	_, topic := redisTestClient(t)
	broker, err := NewRedisBroker(map[string]any{"address": os.Getenv("FORGE_EVENTS_REDIS_TEST_ADDR")}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, broker.Connect(t.Context(), nil))

	received := make(chan string, 1)

	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(ctx context.Context, event *core.Event) error {
		select {
		case received <- event.ID:
		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	})))
	event := core.NewEvent("test", "account", nil)
	require.NoError(t, broker.Publish(t.Context(), topic, *event))

	select {
	case id := <-received:
		require.Equal(t, event.ID, id)
	case <-time.After(time.Second):
		t.Fatal("Pub/Sub event was not received")
	}

	var workers sync.WaitGroup
	for range 10 {
		workers.Go(func() {
			for range 10 {
				_ = broker.Publish(t.Context(), topic, *event)
			}
		})
	}

	require.NoError(t, broker.Close(t.Context()))
	workers.Wait()
	require.Error(t, broker.Publish(t.Context(), topic, *event))
}

func TestRedisStreamsRejectUnsupportedConfiguration(t *testing.T) {
	for name, config := range map[string]map[string]any{"aggregate-ordering": {"stream_ordering": "aggregate"}, "no-capacity": {"stream_max_len": 0}, "empty-group": {"consumer_group": ""}} {
		t.Run(name, func(t *testing.T) {
			config["enable_streams"] = true
			_, err := NewRedisBroker(config, nil, nil)
			require.Error(t, err)
		})
	}

	broker, err := NewRedisBroker(nil, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, broker.config.ConsumerName)
}

func BenchmarkRedisStreamsPublish(b *testing.B) {
	address := os.Getenv("FORGE_EVENTS_REDIS_TEST_ADDR")
	if address == "" {
		b.Skip("set FORGE_EVENTS_REDIS_TEST_ADDR")
	}

	topic := "forge-events-benchmark:" + uuid.NewString()

	broker, err := NewRedisBroker(map[string]any{"address": address, "enable_streams": true, "stream_max_len": b.N + 1}, nil, nil)
	if err != nil {
		b.Fatal(err)
	}

	if err = broker.Connect(b.Context(), nil); err != nil {
		b.Fatal(err)
	}
	defer func() {
		_ = broker.client.Del(context.Background(), topic, streamStateKey(topic)).Err()
		_ = broker.Close(context.Background())
	}()

	event := core.NewEvent("test", "account", nil)

	samples := make([]time.Duration, 0, b.N)
	b.ResetTimer()

	for range b.N {
		start := time.Now()

		if err = broker.Publish(b.Context(), topic, *event); err != nil {
			b.Fatal(fmt.Errorf("publish: %w", err))
		}

		samples = append(samples, time.Since(start))
	}

	b.StopTimer()
	slices.Sort(samples)

	if len(samples) > 0 {
		b.ReportMetric(float64(samples[(len(samples)-1)*95/100].Microseconds()), "p95-us")
		b.ReportMetric(float64(samples[(len(samples)-1)*99/100].Microseconds()), "p99-us")
	}
}

func TestRedisStreamsDeletedAndRecreatedHistory(t *testing.T) {
	for _, recreate := range []bool{false, true} {
		t.Run(strconv.FormatBool(recreate), func(t *testing.T) {
			client, topic := redisTestClient(t)
			broker := redisTestBroker(t, map[string]any{})
			group := streamGroup(broker.config.ConsumerGroup, "ledger")
			require.NoError(t, client.XGroupCreateMkStream(t.Context(), topic, group, "0").Err())

			for range 2 {
				require.NoError(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)))
			}

			require.NoError(t, client.Del(t.Context(), topic).Err())

			if recreate {
				data, err := json.Marshal(core.NewEvent("test", "account", nil))
				require.NoError(t, err)

				for range 2 {
					require.NoError(t, client.XAdd(t.Context(), &redis.XAddArgs{Stream: topic, Values: map[string]any{"data": data}}).Err())
				}
			}

			require.Error(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(context.Context, *core.Event) error {
				t.Error("effects resumed across missing history")

				return nil
			})))
			require.ErrorContains(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)), "RECOVERY_REQUIRED")
			require.Error(t, broker.HealthCheck(t.Context()))
		})
	}
}

func TestRedisStreamsPoisonAndDeadLetterCapacity(t *testing.T) {
	client, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{"stream_max_len": 1, "stream_max_deliveries": 2})
	require.NoError(t, client.XAdd(t.Context(), &redis.XAddArgs{Stream: topic, Values: map[string]any{"data": "bad-json"}}).Err())
	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(context.Context, *core.Event) error {
		t.Error("malformed event invoked handler")

		return nil
	})))
	waitRedis(t, func() bool {
		n, _ := client.XLen(t.Context(), topic+":forge:dead-letter").Result()

		return n == 1
	})

	var calls atomic.Int64

	require.NoError(t, broker.Unsubscribe(t.Context(), topic, "ledger"))
	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(context.Context, *core.Event) error {
		calls.Add(1)

		return errors.New("poison")
	})))
	require.NoError(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)))
	waitRedis(t, func() bool { return calls.Load() == 2 })
	time.Sleep(120 * time.Millisecond)
	require.Equal(t, int64(2), calls.Load())
	pending, err := client.XPending(t.Context(), topic, streamGroup(broker.config.ConsumerGroup, "ledger")).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), pending.Count)
	n, err := client.XLen(t.Context(), topic+":forge:dead-letter").Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.ErrorContains(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)), "STREAM_CAPACITY")
}

func TestRedisStreamsLogicalHandlersReceiveIndependently(t *testing.T) {
	_, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{})
	received := make(chan string, 2)

	for _, name := range []string{"ledger", "audit"} {
		require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler(name, func(_ context.Context, event *core.Event) error {
			received <- event.ID

			return nil
		})))
	}

	event := core.NewEvent("test", "account", nil)
	require.NoError(t, broker.Publish(t.Context(), topic, *event))

	for range 2 {
		select {
		case id := <-received:
			require.Equal(t, event.ID, id)
		case <-time.After(time.Second):
			t.Fatal("logical subscriber missed event")
		}
	}

	require.Error(t, broker.Subscribe(t.Context(), topic, core.EventHandlerFunc(func(context.Context, *core.Event) error { return nil })))
	require.Error(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(context.Context, *core.Event) error { return nil })))
}

func TestRedisStreamsRetryPreservesTopicOrder(t *testing.T) {
	_, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{})

	var (
		mu    sync.Mutex
		seen  []string
		calls atomic.Int64
	)

	require.NoError(t, broker.Subscribe(t.Context(), topic, testHandler("ledger", func(_ context.Context, event *core.Event) error {
		if calls.Add(1) == 1 {
			return errors.New("transient")
		}

		mu.Lock()

		seen = append(seen, event.ID)
		mu.Unlock()

		return nil
	})))

	first := core.NewEvent("test", "one", nil)
	second := core.NewEvent("test", "two", nil)

	require.NoError(t, broker.Publish(t.Context(), topic, *first))
	require.NoError(t, broker.Publish(t.Context(), topic, *second))
	waitRedis(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(seen) == 2
	})
	mu.Lock()
	require.Equal(t, []string{first.ID, second.ID}, seen)
	mu.Unlock()
}

func TestRedisStreamsShutdownDuringPublish(t *testing.T) {
	client, topic := redisTestClient(t)
	broker := redisTestBroker(t, map[string]any{"stream_max_len": 1000})

	var (
		workers   sync.WaitGroup
		published atomic.Int64
	)

	for range 8 {
		workers.Go(func() {
			for range 25 {
				if broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)) == nil {
					published.Add(1)
				}
			}
		})
	}

	waitRedis(t, func() bool { return published.Load() > 0 })

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	require.NoError(t, broker.Close(ctx))
	workers.Wait()

	length, err := client.XLen(t.Context(), topic).Result()
	require.NoError(t, err)
	require.Equal(t, published.Load(), length)
	require.Equal(t, published.Load(), broker.GetStats()["messages_published"])
	require.Error(t, broker.Publish(t.Context(), topic, *core.NewEvent("test", "account", nil)))
}
