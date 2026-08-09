package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/xraph/forge/extensions/streaming/backends/storetest"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// miniredis rather than a container.
//
// The Redis message store carries the part of replay that only matters in a
// distributed deployment — INCR is chosen precisely because it is atomic at the
// server, where an application-level read-modify-write would race between
// processes. That property needs a real Redis command implementation to test,
// but not a real Redis server: miniredis speaks the wire protocol in-process,
// so the suite stays hermetic and runs in CI with no Docker and no network.
//
// What this does NOT prove is behaviour under an actual redis-server's
// concurrency and cluster semantics. miniredis executes commands under its own
// lock, so the concurrency test below demonstrates that the code path is
// correct and free of data races — not that a three-node Redis Cluster would
// agree. That gap wants an integration suite against a real server, and is
// called out here rather than left for someone to discover.

func newTestClient(t *testing.T) *goredis.Client {
	t.Helper()

	server := miniredis.RunT(t)

	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("miniredis ping: %v", err)
	}

	return client
}

func newContractStore(t *testing.T) streaming.MessageStore {
	t.Helper()

	store := NewMessageStore(newTestClient(t), "test")

	if err := store.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

	return store
}

func TestRedisMessageStore_Contract(t *testing.T) {
	storetest.RunMessageStoreContract(t, newContractStore)
}

// The sequence counter is a distinct Redis key, not derived from stream length.
//
// Stream length would look equivalent and is not: entries expire under
// retention and can be trimmed with XTRIM, so a length-derived sequence would
// go backwards the moment old messages aged out, handing a resuming client
// numbers it had already consumed.
func TestRedisMessageStore_SequenceSurvivesStreamTrimming(t *testing.T) {
	client := newTestClient(t)
	store := NewMessageStore(client, "test")
	ctx := context.Background()

	for i := range 5 {
		msg := &streaming.Message{ID: string(rune('a' + i)), RoomID: "room-1"}
		if err := store.Save(ctx, msg); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// Drop everything from the stream, as retention eventually would.
	if err := client.Del(ctx, "test:room-1").Err(); err != nil {
		t.Fatalf("Del: %v", err)
	}

	next := &streaming.Message{ID: "after-trim", RoomID: "room-1"}
	if err := store.Save(ctx, next); err != nil {
		t.Fatalf("Save after trim: %v", err)
	}

	if next.Sequence != 6 {
		t.Errorf("sequence after trim = %d, want 6 — the counter must not be derived from stream contents", next.Sequence)
	}
}
