package conduit_test

import (
	"context"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/conduit"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
)

func TestDurableOfflineCursorAndDiscovery(t *testing.T) {
	url := brokerServer(t, t.TempDir(), -1).ClientURL()

	var mu sync.Mutex

	seen := map[string]int{}
	consumer := func(instance string) *conduit.Runtime {
		provider := jetstream.New(jetstream.Options{URL: url})

		r := runtimeFor(t, config("billing", instance, true, conduit.Competing), provider, conduit.WithRegistry(provider))
		if err := conduit.Subscribe(r, placed, func(_ context.Context, m conduit.Message[order]) error {
			mu.Lock()
			seen[m.Data.ID]++
			mu.Unlock()

			return nil
		}, conduit.Consumer("process")); err != nil {
			t.Fatal(err)
		}

		start(t, r)

		return r
	}
	first := consumer("first")
	producerProvider := jetstream.New(jetstream.Options{URL: url})
	producer := runtimeFor(t, config("orders", "producer", false, conduit.Competing), producerProvider, conduit.WithRegistry(producerProvider))
	start(t, producer)

	if _, err := conduit.Publish(t.Context(), producer, placed, order{ID: "before"}); err != nil {
		t.Fatal(err)
	}

	wait(t, func() bool {
		snapshot, err := first.Snapshot(t.Context())

		return err == nil && snapshot.Acknowledged == 1
	})

	if err := first.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := conduit.Publish(t.Context(), producer, placed, order{ID: "offline"}); err != nil {
		t.Fatal(err)
	}

	replacement := consumer("replacement")

	wait(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return seen["offline"] == 1
	})
	mu.Lock()
	if seen["before"] != 1 {
		t.Error("acknowledged event was replayed after replacement")
	}
	mu.Unlock()

	instances, err := replacement.Instances(t.Context())
	if err != nil || len(instances) != 2 {
		t.Fatalf("instance registry lost a peer or retained a stopped instance: %v %+v", err, instances)
	}

	resolved, err := producerProvider.Resolve(t.Context(), "test", "billing")
	if err != nil || len(resolved) != 1 || resolved[0].Identity.InstanceID != "replacement" {
		t.Fatalf("logical discovery failed: %v %+v", err, resolved)
	}

	foreign, err := producerProvider.Resolve(t.Context(), "other", "billing")
	if err != nil || len(foreign) != 0 {
		t.Fatalf("namespace leaked: %v", err)
	}
}
