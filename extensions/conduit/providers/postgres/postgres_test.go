package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

const placed = "orders.placed.v1"

func connect(t *testing.T, options Options) *Provider {
	t.Helper()

	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
			t.Fatal("CONDUIT_TEST_POSTGRES is required")
		}

		t.Skip("set CONDUIT_TEST_POSTGRES to run PostgreSQL provider tests")
	}

	if options.Pool == nil {
		options.URL = dsn
	}

	options.AutoMigrate = true
	if options.PollInterval == 0 {
		options.PollInterval = 100 * time.Millisecond
	}

	p := New(options)
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	return p
}

func newStream(t *testing.T, p *Provider) (string, core.StreamConfig) {
	t.Helper()

	ns := "pg-" + core.NewID()
	cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1}

	if err := p.EnsureStream(t.Context(), ns, cfg); err != nil {
		t.Fatal(err)
	}

	return ns, cfg
}

func envelope(ns, id, messageType string) core.Envelope {
	return core.Envelope{
		ID: id, Type: messageType, ContentType: "application/json", Data: []byte(`{"id": 42}`), CreatedAt: time.Now(),
		Source: core.Identity{Namespace: ns, ServiceID: "orders", InstanceID: "publisher"},
	}
}

func binding(ns string, cfg core.StreamConfig, mode core.DeliveryMode, sub, start string) core.Binding {
	return core.Binding{
		Identity: core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: "a"},
		Stream:   cfg,
		Subscription: core.SubscriptionConfig{
			ID: sub, Stream: cfg.Name, MessageType: placed, Mode: mode, Durable: true, Concurrency: 1, MaxInFlight: 8,
			Timeout: time.Second, MaxAttempts: 2, RetryDelay: time.Millisecond, StartAt: start, BroadcastID: "a",
		},
	}
}

func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func TestRejectsAnUnsafeSchema(t *testing.T) {
	p := New(Options{URL: "postgres://localhost/none", Schema: "conduit'; drop"})
	if err := p.Connect(t.Context()); err == nil {
		t.Fatal("an unsafe schema name was accepted")
	}
}

func TestEnsureStreamAgreesAcrossProviders(t *testing.T) {
	ctx := t.Context()
	p, q := connect(t, Options{}), connect(t, Options{})
	ns := "pg-" + core.NewID()
	cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1}

	errs := make(chan error, 2)

	for _, provider := range []*Provider{p, q} {
		go func() { errs <- provider.EnsureStream(ctx, ns, cfg) }()
	}

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent declaration failed: %v", err)
		}
	}

	changed := cfg
	changed.Subjects = []string{"other.>"}

	if err := p.EnsureStream(ctx, ns, changed); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("a conflicting declaration was accepted: %v", err)
	}
}
