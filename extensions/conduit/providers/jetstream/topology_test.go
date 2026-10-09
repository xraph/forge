package jetstream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	js "github.com/nats-io/nats.go/jetstream"
	"github.com/xraph/forge/extensions/conduit/core"
)

func TestExistingBucketsCannotWeakenDurabilityOrLeases(t *testing.T) {
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}

	go srv.Start()

	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })

	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("broker did not start")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	provider := New(Options{URL: srv.ClientURL()})
	if err := provider.Connect(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = provider.Close(context.Background()) })

	client, err := provider.client()
	if err != nil {
		t.Fatal(err)
	}

	for _, prefix := range []string{"FC_DLQ_", "FC_DISC_"} {
		if _, err := client.CreateKeyValue(ctx, js.KeyValueConfig{Bucket: prefix + hash("test"), Storage: js.MemoryStorage, History: 1}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := provider.bucket(ctx, "test"); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("accepted non-durable dead letters: %v", err)
	}

	if _, err := provider.discoveryBucket(ctx, "test"); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("accepted discovery without its lease: %v", err)
	}
}
