// Package conformance supplies the broker-independent Conduit adapter checks.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

// Factory returns a connection to the same broker for each simulated service instance.
type Factory func() core.Provider

// Run checks competing groups, broadcast, namespace isolation, restart cursors and recovery.
// It uses real broker connections supplied by the caller, with no simulated wire responses.
func Run(t *testing.T, factory Factory) {
	t.Helper()

	for _, name := range []string{"replicas", "restart", "retry-and-recovery", "topology", "unsettled-restart", "sequence", "retention"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()

			ns := "conformance-" + core.NewID()
			cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1}
			newProvider := func() core.Provider {
				p := factory()
				if err := p.Connect(ctx); err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() {
					if err := p.Close(context.WithoutCancel(t.Context())); err != nil {
						t.Error(err)
					}
				})

				if err := p.EnsureStream(ctx, ns, cfg); err != nil {
					t.Fatal(err)
				}

				return p
			}
			p := newProvider()
			binding := func(instance string, mode core.DeliveryMode, sub string) core.Binding {
				return core.Binding{Identity: core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: instance}, Stream: cfg, Subscription: core.SubscriptionConfig{ID: sub, Stream: cfg.Name, MessageType: "orders.placed.v1", Mode: mode, Durable: p.Capabilities().Durable, Concurrency: 1, MaxInFlight: 2, Timeout: time.Second, MaxAttempts: 2, RetryDelay: time.Millisecond, StartAt: "all", BroadcastID: instance}}
			}
			subscribe := func(provider core.Provider, b core.Binding) core.Subscription {
				s, err := provider.Subscribe(ctx, b)
				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() {
					if err := s.Close(context.WithoutCancel(t.Context())); err != nil {
						t.Error(err)
					}
				})

				return s
			}
			publish := func(provider core.Provider, id string) core.Receipt {
				r, err := provider.Publish(ctx, ns, cfg, core.Envelope{ID: id, Type: "orders.placed.v1", Source: core.Identity{Namespace: ns, ServiceID: "orders", InstanceID: "publisher"}, Data: []byte(`{"id":"42"}`), CreatedAt: time.Now()})
				if err != nil {
					t.Fatal(err)
				}

				if r.MessageID != id || r.Sequence == 0 || r.Persisted != provider.Capabilities().Durable {
					t.Fatalf("incorrect receipt: %+v", r)
				}

				return r
			}
			next := func(s core.Subscription) core.Delivery {
				d, err := s.Next(ctx)
				if err != nil {
					t.Fatal(err)
				}

				return d
			}
			ack := func(d core.Delivery) {
				if err := d.Ack(ctx); err != nil {
					t.Fatal(err)
				}
			}

			switch name {
			case "replicas":
				a := subscribe(p, binding("a", core.Competing, "process"))
				q := newProvider()
				b := subscribe(q, binding("b", core.Competing, "process"))
				fanA := subscribe(p, binding("fan-a", core.Broadcast, "cache"))
				fanB := subscribe(q, binding("fan-b", core.Broadcast, "cache"))

				readctx, stop := context.WithCancel(ctx)
				defer stop()

				var wg sync.WaitGroup

				deliveries := make(chan string, 32)
				failures := make(chan error, 2)

				for _, s := range []core.Subscription{a, b} {
					wg.Go(func() {
						for {
							d, err := s.Next(readctx)
							if err != nil {
								if readctx.Err() == nil {
									failures <- err
								}

								return
							}

							if err := d.Ack(ctx); err != nil {
								failures <- err

								return
							}

							deliveries <- d.Message().ID
						}
					})
				}

				for i := range 6 {
					publish(p, fmt.Sprintf("replica-%d", i))
				}

				seen := map[string]bool{}

				for range 6 {
					select {
					case err := <-failures:
						t.Fatal(err)
					case id := <-deliveries:
						if seen[id] {
							t.Fatalf("duplicate competing delivery %s", id)
						}

						seen[id] = true
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}

				for _, s := range []core.Subscription{fanA, fanB} {
					fan := map[string]bool{}

					for range 6 {
						d := next(s)
						if fan[d.Message().ID] {
							t.Fatal("broadcast duplicate")
						}

						fan[d.Message().ID] = true
						ack(d)
					}
				}

				stop()
				wg.Wait()

				select {
				case id := <-deliveries:
					t.Fatalf("competing delivery repeated: %s", id)
				default:
				}

				isolated := binding("other", core.Competing, "process")

				isolated.Identity.Namespace = ns + "-other"
				if err := q.EnsureStream(ctx, isolated.Identity.Namespace, cfg); err != nil {
					t.Fatal(err)
				}

				s := subscribe(q, isolated)

				emptyCtx, end := context.WithTimeout(ctx, 400*time.Millisecond)
				defer end()

				if _, err := s.Next(emptyCtx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("namespace leakage: %v", err)
				}
			case "restart":
				b := binding("old", core.Competing, "restart")
				s := subscribe(p, b)
				publish(p, "first")
				ack(next(s))

				if err := s.Close(ctx); err != nil {
					t.Fatal(err)
				}

				publish(p, "offline")

				q := newProvider()
				b.Identity.InstanceID = "replacement"
				replacement := subscribe(q, b)

				d := next(replacement)
				if d.Message().ID != "offline" {
					t.Fatalf("cursor lost: %s", d.Message().ID)
				}

				ack(d)

				fan := binding("stable", core.Broadcast, "restart-fan")
				fan.Subscription.StartAt = "new"
				fan.Subscription.Durable = p.Capabilities().Durable
				s = subscribe(p, fan)
				publish(p, "fan-first")
				ack(next(s))

				if err := s.Close(ctx); err != nil {
					t.Fatal(err)
				}

				if p.Capabilities().Durable {
					publish(p, "fan-offline")

					fan.Identity.InstanceID = "new-process"
					s = subscribe(q, fan)

					d = next(s)
					if d.Message().ID != "fan-offline" {
						t.Fatal("stable broadcast cursor lost")
					}

					ack(d)
				}
			case "retry-and-recovery":
				b := binding("retry", core.Competing, "retry")
				s := subscribe(p, b)
				other := subscribe(p, binding("observer", core.Broadcast, "other"))
				publish(p, "retry-id")

				d := next(s)
				ack(next(other))

				info := d.Info()
				if err := d.Retry(ctx, 60*time.Millisecond); err != nil {
					t.Fatal(err)
				}

				started := time.Now()

				d = next(s)
				if time.Since(started) < 40*time.Millisecond || d.Info().Attempt < 2 || d.Message().ID != "retry-id" {
					t.Fatal("retry identity, attempt or delay lost")
				}

				m, ok := p.(core.Management)
				if !ok {
					t.Fatal("dead letters require management")
				}

				l := core.DeadLetter{ID: "retry-id", Message: d.Message(), Delivery: info, FailedAt: time.Now(), Reason: "test"}
				if err := m.StoreDeadLetter(ctx, ns, l); err != nil {
					t.Fatal(err)
				}

				if err := d.Reject(ctx); err != nil {
					t.Fatal(err)
				}

				records, _, err := m.ListDeadLetters(ctx, b.Identity, b.Subscription.ID, "", 10)
				if err != nil || len(records) != 1 {
					t.Fatalf("failure missing: %v %v", records, err)
				}

				foreign := b.Identity
				foreign.ServiceID = "another"

				records, _, err = m.ListDeadLetters(ctx, foreign, "", "", 10)
				if err != nil || len(records) != 0 {
					t.Fatalf("failure scope leaked: %v %v", records, err)
				}

				r, err := m.ReplayDeadLetter(ctx, b.Identity, b.Subscription.ID, l.ID)
				if err != nil || r.MessageID != "retry-id" {
					t.Fatalf("replay failed: %+v %v", r, err)
				}

				d = next(s)
				if d.Message().TargetConsumer != b.ConsumerID() {
					t.Fatal("recovery is not consumer-scoped")
				}

				ack(d)

				emptyCtx, end := context.WithTimeout(ctx, 400*time.Millisecond)
				defer end()

				if _, err := other.Next(emptyCtx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("recovery leaked to broadcast group: %v", err)
				}

				if _, err := m.ReplayDeadLetter(ctx, b.Identity, b.Subscription.ID, l.ID); !errors.Is(err, core.ErrConflict) {
					t.Fatalf("replayed record should conflict: %v", err)
				}
			case "unsettled-restart":
				b := binding("crashed", core.Competing, "unsettled")
				s := subscribe(p, b)
				publish(p, "unsettled")

				d := next(s)
				original := d.Message().ID

				if err := s.Close(ctx); err != nil {
					t.Fatal(err)
				}

				q := newProvider()
				b.Identity.InstanceID = "takeover"
				s = subscribe(q, b)

				d = next(s)
				if d.Message().ID != original {
					t.Fatal("unsettled event lost after replica replacement")
				}

				ack(d)
			case "sequence":
				first := publish(p, "skip-this")
				second := publish(p, "from-here")
				b := binding("cursor", core.Competing, "cursor")
				b.Subscription.StartAt = "sequence"
				b.Subscription.StartSequence = first.Sequence + 1
				s := subscribe(p, b)

				d := next(s)
				if d.Message().ID != "from-here" || d.Info().Sequence != second.Sequence {
					t.Fatal("start sequence ignored")
				}

				ack(d)
			case "retention":
				cfg.Name = "shortlived"

				cfg.MaxAge = 100 * time.Millisecond
				if err := p.EnsureStream(ctx, ns, cfg); err != nil {
					t.Fatal(err)
				}

				publish(p, "expired")

				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-timer.C:
				}

				b := binding("retention", core.Competing, "retained")
				s := subscribe(p, b)
				publish(p, "fresh")

				d := next(s)
				if d.Message().ID != "fresh" {
					t.Fatal("expired event delivered")
				}

				ack(d)

			case "topology":
				changed := cfg

				changed.Subjects = []string{"different.>"}
				if err := p.EnsureStream(ctx, ns, changed); !errors.Is(err, core.ErrConflict) {
					t.Fatalf("stream conflict ignored: %v", err)
				}

				b := binding("a", core.Competing, "policy")
				subscribe(p, b)
				b.Identity.InstanceID = "b"

				b.Subscription.MessageType = "orders.cancelled.v1"
				if _, err := p.Subscribe(ctx, b); !errors.Is(err, core.ErrConflict) {
					t.Fatalf("group conflict ignored: %v", err)
				}
			}
		})
	}
}
