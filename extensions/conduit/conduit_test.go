package conduit_test

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/xraph/forge/extensions/conduit"
	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
)

type order struct {
	ID string `json:"id"`
}

var placed = conduit.Event[order]("orders.placed.v1")

func config(service, instance string, durable bool, mode conduit.DeliveryMode) conduit.Config {
	return conduit.Config{Identity: conduit.Identity{Namespace: "test", ServiceID: service, InstanceID: instance}, Streams: map[string]conduit.StreamConfig{"orders": {Provider: "broker", Subjects: []string{"orders.>"}}}, Subscriptions: map[string]conduit.SubscriptionConfig{"process": {Stream: "orders", Mode: mode, Durable: durable, BroadcastID: instance, Timeout: time.Second, MaxAttempts: 2, RetryDelay: time.Millisecond, MaxInFlight: 8}}}
}

func brokerServer(t *testing.T, dir string, port int) *server.Server {
	t.Helper()

	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: dir, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}

	go srv.Start()

	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("broker did not start")
	}

	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })

	return srv
}

func start(t *testing.T, r *conduit.Runtime) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()

		if err := r.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
}
func wait(t *testing.T, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("condition timed out")
}
func runtimeFor(t *testing.T, cfg conduit.Config, provider conduit.Provider, opts ...conduit.Option) *conduit.Runtime {
	t.Helper()

	r, err := conduit.New(cfg, append([]conduit.Option{conduit.WithProvider("broker", provider)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func TestReplicaDelivery(t *testing.T) {
	for _, kind := range []string{"memory", "jetstream"} {
		t.Run(kind, func(t *testing.T) {
			broker := memory.New()

			var url string
			if kind == "jetstream" {
				url = brokerServer(t, t.TempDir(), -1).ClientURL()
			}

			provider := func() conduit.Provider {
				if url != "" {
					return jetstream.New(jetstream.Options{URL: url})
				}

				return broker
			}

			var mu sync.Mutex

			shared := map[string]int{}
			fanout := map[string]map[string]int{"a": {}, "b": {}}

			for _, instance := range []string{"a", "b"} {
				cfg := config("billing", instance, url != "", conduit.Competing)
				cfg.Subscriptions["cache"] = conduit.SubscriptionConfig{Stream: "orders", Mode: conduit.Broadcast, Durable: url != "", BroadcastID: instance, MaxInFlight: 8}

				r := runtimeFor(t, cfg, provider())
				if err := conduit.Subscribe(r, placed, func(_ context.Context, m conduit.Message[order]) error {
					mu.Lock()
					shared[m.Data.ID]++
					mu.Unlock()

					return nil
				}, conduit.Consumer("process")); err != nil {
					t.Fatal(err)
				}

				if err := conduit.Subscribe(r, placed, func(_ context.Context, m conduit.Message[order]) error {
					mu.Lock()
					fanout[instance][m.Data.ID]++
					mu.Unlock()

					return nil
				}, conduit.Consumer("cache")); err != nil {
					t.Fatal(err)
				}

				start(t, r)
			}

			producer := runtimeFor(t, config("orders", "producer", false, conduit.Competing), provider())
			start(t, producer)

			for i := range 20 {
				receipt, err := conduit.Publish(t.Context(), producer, placed, order{ID: strconv.Itoa(i)})
				if err != nil {
					t.Fatal(err)
				}

				if receipt.Persisted != (url != "") {
					t.Fatal("incorrect durability receipt")
				}
			}

			wait(t, func() bool {
				mu.Lock()
				defer mu.Unlock()

				return len(shared) == 20 && len(fanout["a"]) == 20 && len(fanout["b"]) == 20
			})
			mu.Lock()
			defer mu.Unlock()

			for id, n := range shared {
				if n != 1 || fanout["a"][id] != 1 || fanout["b"][id] != 1 {
					t.Fatalf("wrong distribution for %s: %d", id, n)
				}
			}
		})
	}
}

func TestDurableRestartAndTargetedRecovery(t *testing.T) {
	dir := t.TempDir()
	srv := brokerServer(t, dir, -1)
	url, port := srv.ClientURL(), srv.Addr().(*net.TCPAddr).Port
	provider := func() *jetstream.Provider { return jetstream.New(jetstream.Options{URL: url}) }

	var (
		attempts  atomic.Int32
		recovered atomic.Bool
	)

	r := runtimeFor(t, config("billing", "old", true, conduit.Competing), provider())
	if err := conduit.Subscribe(r, placed, func(context.Context, conduit.Message[order]) error {
		attempts.Add(1)

		return errors.New("payment unavailable")
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	start(t, r)

	if _, err := conduit.Publish(t.Context(), r, placed, order{ID: "failed"}, conduit.MessageID("stable")); err != nil {
		t.Fatal(err)
	}

	var letter conduit.DeadLetter

	wait(t, func() bool {
		letters, _, err := r.DeadLetters(t.Context(), "broker", "process", "", 10)
		if err != nil || len(letters) != 1 {
			return false
		}

		letter = letters[0]

		return true
	})

	if attempts.Load() != 2 {
		t.Fatalf("expected two attempts, got %d", attempts.Load())
	}

	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	srv.Shutdown()
	srv.WaitForShutdown()
	brokerServer(t, dir, port)

	r2 := runtimeFor(t, config("billing", "new", true, conduit.Competing), provider())
	if err := conduit.Subscribe(r2, placed, func(_ context.Context, m conduit.Message[order]) error {
		if m.Envelope.ID != "stable" || m.Delivery.Destination.InstanceID != "new" {
			return conduit.Permanent(errors.New("identity changed"))
		}

		recovered.Store(true)

		return nil
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	start(t, r2)

	letters, _, err := r2.DeadLetters(t.Context(), "broker", "process", "", 10)
	if err != nil || len(letters) != 1 {
		t.Fatalf("dead letter did not survive restart: %v", err)
	}

	if _, err := r2.ReplayDeadLetter(t.Context(), "broker", "process", letter.ID); err != nil {
		t.Fatal(err)
	}

	wait(t, recovered.Load)

	if _, err := r2.ReplayDeadLetter(t.Context(), "broker", "process", letter.ID); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("expected replay conflict: %v", err)
	}

	outsider := runtimeFor(t, config("outside", "other", true, conduit.Competing), provider())
	start(t, outsider)

	letters, _, err = outsider.DeadLetters(t.Context(), "broker", "", "", 10)
	if err != nil || len(letters) != 0 {
		t.Fatalf("cross-service leak: %v", err)
	}
}

func TestHooksValidationAndPanicIsolation(t *testing.T) {
	var (
		mu     sync.Mutex
		stages []core.Stage
	)

	r := runtimeFor(t, config("billing", "one", false, conduit.Competing), memory.New(), conduit.WithHooks(conduit.HookFuncs{HookName: "audit", OnPublish: func(_ context.Context, e *conduit.Envelope) error {
		e.Headers = map[string]string{"trace": "abc"}

		return nil
	}, OnEvent: func(_ context.Context, e conduit.HookEvent) {
		mu.Lock()

		stages = append(stages, e.Stage)
		mu.Unlock()

		if e.Message != nil {
			e.Message.Headers["trace"] = "mutated"
		}
	}}, conduit.HookFuncs{HookName: "panic-observer", OnEvent: func(context.Context, conduit.HookEvent) { panic("observer") }}))

	var handled atomic.Int32

	if err := conduit.Subscribe(r, placed, func(_ context.Context, m conduit.Message[order]) error {
		if m.Envelope.Headers["trace"] != "abc" {
			t.Error("observer mutated delivery")
		}

		handled.Add(1)

		return nil
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	start(t, r)

	if _, err := conduit.Publish(t.Context(), r, placed, order{ID: "a"}); err != nil {
		t.Fatal(err)
	}

	wait(t, func() bool { return handled.Load() == 1 })

	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	for _, stage := range []core.Stage{core.Starting, core.Ready, core.Published, core.Handled, core.Acknowledged, core.Stopped} {
		if !slices.Contains(stages, stage) {
			t.Fatalf("missing hook %s", stage)
		}
	}

	for _, event := range r.RecentEvents() {
		if event.Message != nil && (len(event.Message.Data) > 0 || len(event.Message.Headers) > 0) {
			t.Fatal("diagnostic payload leak")
		}
	}
}
