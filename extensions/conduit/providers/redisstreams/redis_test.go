package redisstreams_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
