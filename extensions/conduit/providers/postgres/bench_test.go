package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

// The benchmarks measure your own database. Run them with
//
//	CONDUIT_TEST_POSTGRES=postgres://... go test -run '^$' -bench . -benchtime=2000x ./providers/postgres
//
// Every publish commits and sends a NOTIFY, so the numbers follow your commit latency.

func benchProvider(b *testing.B) *Provider {
	b.Helper()

	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		b.Skip("set CONDUIT_TEST_POSTGRES to benchmark the PostgreSQL provider")
	}

	p := New(Options{URL: dsn, AutoMigrate: true, PollInterval: 100 * time.Millisecond})
	if err := p.Connect(b.Context()); err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = p.Close(context.WithoutCancel(b.Context())) })

	return p
}

func benchStream(b *testing.B, p *Provider) (string, core.StreamConfig) {
	b.Helper()

	ns := "bench-" + core.NewID()
	cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1}

	if err := p.EnsureStream(b.Context(), ns, cfg); err != nil {
		b.Fatal(err)
	}

	return ns, cfg
}

func BenchmarkPublish(b *testing.B) {
	p := benchProvider(b)
	ns, cfg := benchStream(b, p)

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		if _, err := p.Publish(b.Context(), ns, cfg, envelope(ns, fmt.Sprintf("m-%d", i), placed)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublishParallel(b *testing.B) {
	p := benchProvider(b)
	ns, cfg := benchStream(b, p)

	var next atomic.Int64

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := p.Publish(b.Context(), ns, cfg, envelope(ns, fmt.Sprintf("m-%d", next.Add(1)), placed)); err != nil {
				b.Error(err)

				return
			}
		}
	})
}

// BenchmarkPublishAndDeliver runs one competing subscription on two providers, two
// workers each, acknowledging as fast as they can. It reports how many messages a second
// go from Publish to Ack.
func BenchmarkPublishAndDeliver(b *testing.B) {
	const replicas, workers, publishers = 2, 2, 8

	publisher := benchProvider(b)
	ns, cfg := benchStream(b, publisher)

	sub := core.Binding{
		Identity: core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: "a"},
		Stream:   cfg,
		Subscription: core.SubscriptionConfig{
			ID: "bench", Stream: cfg.Name, MessageType: placed, Mode: core.Competing, Durable: true, Concurrency: workers, MaxInFlight: 64,
			Timeout: 30 * time.Second, MaxAttempts: 2, RetryDelay: time.Millisecond, StartAt: "all",
		},
	}

	ctx, stop := context.WithCancel(b.Context())

	var (
		acked   atomic.Int64
		running sync.WaitGroup
	)

	defer func() { stop(); running.Wait() }()

	for replica := range replicas {
		p := benchProvider(b)
		binding := sub
		binding.Identity.InstanceID = fmt.Sprintf("replica-%d", replica)

		s, err := p.Subscribe(b.Context(), binding)
		if err != nil {
			b.Fatal(err)
		}

		for range workers {
			running.Go(func() {
				for {
					d, err := s.Next(ctx)
					if err != nil {
						return
					}

					if err := d.Ack(ctx); err != nil && ctx.Err() == nil {
						b.Error(err)
					}

					acked.Add(1)
				}
			})
		}
	}

	total := int64(b.N)
	started := time.Now()

	b.ResetTimer()

	var (
		sent       atomic.Int64
		publishing sync.WaitGroup
	)

	for range publishers {
		publishing.Go(func() {
			for i := sent.Add(1); i <= total; i = sent.Add(1) {
				if _, err := publisher.Publish(ctx, ns, cfg, envelope(ns, fmt.Sprintf("m-%d", i), placed)); err != nil {
					b.Error(err)

					return
				}
			}
		})
	}

	publishing.Wait()

	for acked.Load() < total {
		if time.Since(started) > 5*time.Minute {
			b.Fatalf("delivered %d of %d", acked.Load(), total)
		}

		time.Sleep(time.Millisecond)
	}

	b.StopTimer()
	b.ReportMetric(float64(total)/time.Since(started).Seconds(), "msgs/s")
}
