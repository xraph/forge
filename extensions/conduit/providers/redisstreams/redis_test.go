package redisstreams_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/providers/redisstreams"
)

func connect(t *testing.T, options redisstreams.Options) *redisstreams.Provider {
	t.Helper()

	url := os.Getenv("CONDUIT_TEST_REDIS")
	if url == "" {
		if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
			t.Fatal("CONDUIT_TEST_REDIS is required")
		}

		t.Skip("set CONDUIT_TEST_REDIS to run Redis provider tests")
	}

	options.URL = url
	p := redisstreams.New(options)

	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	return p
}

var orders = core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1}

func binding(ns, sub string) core.Binding {
	return core.Binding{
		Identity: core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: "a"},
		Stream:   orders,
		Subscription: core.SubscriptionConfig{
			ID: sub, Stream: "orders", MessageType: "orders.placed.v1", Mode: core.Competing, Durable: true,
			Concurrency: 1, MaxInFlight: 64, Timeout: time.Second, MaxAttempts: 2, RetryDelay: time.Millisecond, StartAt: "all",
		},
	}
}

func envelope(ns, id string) core.Envelope {
	return core.Envelope{
		ID: id, Type: "orders.placed.v1", ContentType: "application/json", Data: []byte(`{}`), CreatedAt: time.Now(),
		Source: core.Identity{Namespace: ns, ServiceID: "orders", InstanceID: "publisher"},
	}
}

func TestPauseStopsEveryReplica(t *testing.T) {
	ctx := t.Context()
	p, q := connect(t, redisstreams.Options{}), connect(t, redisstreams.Options{})
	ns := "pause-" + core.NewID()

	for _, provider := range []*redisstreams.Provider{p, q} {
		if err := provider.EnsureStream(ctx, ns, orders); err != nil {
			t.Fatal(err)
		}
	}

	b := binding(ns, "race")
	other := b
	other.Identity.InstanceID = "b"

	var delivered atomic.Int64

	readers, stop := context.WithCancel(ctx)
	defer stop()

	for _, pair := range []struct {
		provider *redisstreams.Provider
		binding  core.Binding
	}{{p, b}, {q, other}} {
		s, err := pair.provider.Subscribe(ctx, pair.binding)
		if err != nil {
			t.Fatal(err)
		}

		go func() {
			for {
				d, err := s.Next(readers)
				if err != nil {
					return
				}

				// Every settle waits for an AOF fsync (about a second on appendfsync everysec),
				// so settle concurrently or the consumers cannot keep up with the publisher.
				go func() { _ = d.Ack(ctx) }()

				delivered.Add(1)
			}
		}()
	}

	// Each publish also waits for an AOF fsync, so a sequential publisher would need minutes
	// on appendfsync everysec. Publish in parallel: the writes share one fsync.
	var publishers sync.WaitGroup

	// A message is readable before its publish is confirmed, so wait for the confirmations
	// before the test (and its context) ends.
	defer publishers.Wait()

	publishers.Go(func() {
		slots := make(chan struct{}, 40)

		for i := range 200 {
			slots <- struct{}{}

			publishers.Go(func() {
				defer func() { <-slots }()

				if _, err := p.Publish(ctx, ns, orders, envelope(ns, fmt.Sprintf("m-%d", i))); err != nil {
					t.Errorf("publish m-%d: %v", i, err)
				}
			})
		}
	})

	for delivered.Load() < 50 {
		time.Sleep(5 * time.Millisecond)
	}

	if err := p.PauseConsumer(ctx, b, true); err != nil {
		t.Fatal(err)
	}

	// Reads already inside Redis when the pause landed may still return.
	time.Sleep(50 * time.Millisecond)

	held := delivered.Load()

	time.Sleep(500 * time.Millisecond)

	if after := delivered.Load(); after != held {
		t.Fatalf("delivered %d messages after the pause", after-held)
	}

	if err := p.PauseConsumer(ctx, b, false); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for delivered.Load() < 200 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := delivered.Load(); got != 200 {
		t.Fatalf("resume delivered %d of 200", got)
	}
}

func TestReplacementKeepsRegistration(t *testing.T) {
	ctx := t.Context()
	old := connect(t, redisstreams.Options{})

	time.Sleep(5 * time.Millisecond) // the replacement process starts later

	replacement := connect(t, redisstreams.Options{})
	ns := "restart-" + core.NewID()
	instance := core.Instance{Identity: core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: "pod-0"}, Version: "1", Ready: true}

	if err := old.Register(ctx, instance); err != nil {
		t.Fatal(err)
	}

	if err := replacement.Register(ctx, instance); err != nil {
		t.Fatal(err)
	}

	// The old process shuts down after its replacement registered under the same name.
	stopping := instance
	stopping.Ready = false

	if err := old.Register(ctx, stopping); err != nil {
		t.Fatal(err)
	}

	if err := old.Deregister(ctx, instance.Identity); err != nil {
		t.Fatal(err)
	}

	resolved, err := replacement.Resolve(ctx, ns, "billing")
	if err != nil || len(resolved) != 1 || !resolved[0].Ready {
		t.Fatalf("the old process removed its replacement: %+v %v", resolved, err)
	}
}

func TestRetryWithTheSameIDGetsItsOwnReply(t *testing.T) {
	caller := connect(t, redisstreams.Options{})
	ns := "rs-" + core.NewID()
	identity := core.Identity{Namespace: ns, ServiceID: "web", InstanceID: "caller"}

	binding := func(instance string) core.RPCBinding {
		return core.RPCBinding{
			Identity: core.Identity{Namespace: ns, ServiceID: "pricing", InstanceID: instance}, Method: "quote",
			Config: core.RPCConfig{Timeout: 10 * time.Second, Concurrency: 1, MaxInFlight: 1}, MaxPayloadBytes: 1 << 20,
		}
	}

	var calls atomic.Int64

	handler := func(delay time.Duration) func(context.Context, core.RPCRequest) core.RPCResponse {
		return func(_ context.Context, r core.RPCRequest) core.RPCResponse {
			attempt := calls.Add(1)

			time.Sleep(delay)

			return core.RPCResponse{ID: r.Envelope.ID, Source: core.Identity{Namespace: ns, ServiceID: "pricing"}, Data: fmt.Appendf(nil, `{"attempt":%d}`, attempt)}
		}
	}

	// The first replica answers after its caller has given up, and stops taking new work.
	slow := connect(t, redisstreams.Options{})

	first, err := slow.ServeRPC(t.Context(), binding("a"), handler(1500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	request := core.RPCRequest{
		Service: "pricing", Method: "quote", Deadline: time.Now().Add(time.Minute),
		Envelope: core.Envelope{ID: core.NewID(), Type: "quote", Source: identity, ContentType: "application/json", Data: []byte(`{}`), CreatedAt: time.Now()},
	}

	timedOut, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	if _, err := caller.RequestRPC(timedOut, identity, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the first attempt to time out: %v", err)
	}

	go func() { _ = first.Close(context.WithoutCancel(t.Context())) }()

	time.Sleep(500 * time.Millisecond) // its reader has stopped; its handler is still running

	second, err := connect(t, redisstreams.Options{}).ServeRPC(t.Context(), binding("b"), handler(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = second.Close(context.WithoutCancel(t.Context())) }()

	retry, cancelRetry := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancelRetry()

	response, err := caller.RequestRPC(retry, identity, request)
	if err != nil {
		t.Fatal(err)
	}

	if string(response.Data) != `{"attempt":2}` {
		t.Fatalf("the retry received %s instead of its own reply", response.Data)
	}
}

// TestServingProceduresLeavesThePoolFree serves more procedures than the pool has
// connections. Serving must not pin a connection per procedure, or every other command
// queues behind the blocked reads.
func TestServingProceduresLeavesThePoolFree(t *testing.T) {
	caller := connect(t, redisstreams.Options{})

	options, err := redis.ParseURL(os.Getenv("CONDUIT_TEST_REDIS"))
	if err != nil {
		t.Fatal(err)
	}

	options.PoolSize = 3
	server := connect(t, redisstreams.Options{Redis: options})
	ns := "pool-" + core.NewID()
	identity := core.Identity{Namespace: ns, ServiceID: "web", InstanceID: "caller"}

	for i := range 6 {
		method := fmt.Sprintf("m%d", i)
		binding := core.RPCBinding{
			Identity: core.Identity{Namespace: ns, ServiceID: "pricing", InstanceID: "a"}, Method: method,
			Config: core.RPCConfig{Timeout: 5 * time.Second, Concurrency: 1, MaxInFlight: 1}, MaxPayloadBytes: 1 << 20,
		}

		served, err := server.ServeRPC(t.Context(), binding, func(_ context.Context, r core.RPCRequest) core.RPCResponse {
			return core.RPCResponse{ID: r.Envelope.ID, Source: binding.Identity, Data: []byte(`"` + method + `"`)}
		})
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = served.Close(context.WithoutCancel(t.Context())) })
	}

	if err := server.EnsureStream(t.Context(), ns, orders); err != nil {
		t.Fatal(err)
	}

	// Inspect waits on nothing but the pool. Publish also waits for an AOF fsync, which
	// takes up to a second on appendfsync everysec, so it gets that second on top.
	for i := range 5 {
		started := time.Now()

		if _, err := server.Inspect(t.Context(), ns, orders); err != nil {
			t.Fatal(err)
		}

		if took := time.Since(started); took > 200*time.Millisecond {
			t.Fatalf("Inspect %d took %v while serving", i, took)
		}

		started = time.Now()

		if _, err := server.Publish(t.Context(), ns, orders, envelope(ns, core.NewID())); err != nil {
			t.Fatal(err)
		}

		if _, err := server.Inspect(t.Context(), ns, orders); err != nil {
			t.Fatal(err)
		}

		if took := time.Since(started); took > 1200*time.Millisecond {
			t.Fatalf("Publish and Inspect %d took %v while serving", i, took)
		}
	}

	for i := range 6 {
		method := fmt.Sprintf("m%d", i)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		deadline, _ := ctx.Deadline()
		request := core.RPCRequest{
			Service: "pricing", Method: method, Deadline: deadline,
			Envelope: core.Envelope{ID: core.NewID(), Type: method, Source: identity, ContentType: "application/json", Data: []byte(`{}`), CreatedAt: time.Now()},
		}

		response, err := caller.RequestRPC(ctx, identity, request)

		cancel()

		if err != nil || response.Error != nil || string(response.Data) != `"`+method+`"` {
			t.Fatalf("%s answered %+v %v", method, response, err)
		}
	}
}
