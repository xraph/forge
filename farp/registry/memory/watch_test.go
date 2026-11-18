package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/farp"
)

func watchManifest(t *testing.T, svc, inst string) *farp.SchemaManifest {
	t.Helper()

	schema := map[string]any{"paths": map[string]any{}}

	hash, err := farp.CalculateSchemaChecksum(schema)
	if err != nil {
		t.Fatal(err)
	}

	return &farp.SchemaManifest{
		Version: farp.ProtocolVersion, ServiceName: svc, InstanceID: inst,
		Endpoints: farp.SchemaEndpoints{Health: "/health"},
		Schemas: []farp.SchemaDescriptor{{
			Type: farp.SchemaTypeOpenAPI, SpecVersion: "3.1.0", ContentType: "application/json",
			Location:     farp.SchemaLocation{Type: farp.LocationTypeInline},
			InlineSchema: schema, Hash: hash,
		}},
	}
}

// After Close the watch goroutine must exit instead of spinning on a closed
// channel and invoking the callback with nil events.
func TestWatchManifestsStopsAfterClose(t *testing.T) {
	reg := NewRegistry()

	var calls, nils atomic.Int64

	err := reg.WatchManifests(context.Background(), "svc", func(ev *farp.ManifestEvent) {
		calls.Add(1)

		if ev == nil {
			nils.Add(1)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	if calls.Load() != 0 || nils.Load() != 0 {
		t.Fatalf("after Close: %d callbacks (%d nil), want 0", calls.Load(), nils.Load())
	}
}

// A slow consumer must still observe the final state of every instance even
// when far more events are produced than the old 10-slot buffer held.
func TestWatchManifestsSlowConsumerDoesNotLoseState(t *testing.T) {
	reg := NewRegistry()
	ctx := t.Context()

	release := make(chan struct{})

	var (
		mu   sync.Mutex
		seen = map[string]farp.EventType{}
	)

	err := reg.WatchManifests(ctx, "svc", func(ev *farp.ManifestEvent) {
		<-release

		mu.Lock()
		seen[ev.Manifest.InstanceID] = ev.Type
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}

	const n = 200
	for i := range n {
		if err := reg.RegisterManifest(ctx, watchManifest(t, "svc", fmt.Sprintf("i-%d", i))); err != nil {
			t.Fatal(err)
		}
	}

	// Remove every other instance so the final state differs from the first event.
	for i := 0; i < n; i += 2 {
		if err := reg.DeleteManifest(ctx, fmt.Sprintf("i-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	close(release)

	deadline := time.Now().Add(5 * time.Second)

	for {
		mu.Lock()
		got := len(seen)
		mu.Unlock()

		if got == n {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("saw %d of %d instances", got, n)
		}

		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	for i := range n {
		want := farp.EventTypeAdded
		if i%2 == 0 {
			want = farp.EventTypeRemoved
		}

		if got := seen[fmt.Sprintf("i-%d", i)]; got != want {
			t.Fatalf("instance i-%d final event = %s, want %s", i, got, want)
		}
	}
}

func TestWatchManifestsRequiresHandler(t *testing.T) {
	if err := NewRegistry().WatchManifests(context.Background(), "svc", nil); err == nil {
		t.Fatal("nil handler accepted")
	}
}
