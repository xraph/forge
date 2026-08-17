package storage_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/hls/storage"
	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/memdriver"
)

// newBackend returns a TroveBackend over an in-memory Trove. Testing the
// adapter against a real Trove rather than a mock is the point: the mapping
// this type performs is only interesting where the two APIs disagree, and a
// mock would agree with whatever the adapter happened to do.
func newBackend(t *testing.T) *storage.TroveBackend {
	t.Helper()

	drv := memdriver.New()
	if err := drv.Open(context.Background(), ""); err != nil {
		t.Fatalf("open driver: %v", err)
	}

	tr, err := trove.Open(drv)
	if err != nil {
		t.Fatalf("open trove: %v", err)
	}

	t.Cleanup(func() { _ = tr.Close(context.Background()) })

	b := storage.NewTroveBackend(tr, "hls-test")
	if err := b.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}

	return b
}

func TestTroveBackendRoundTrips(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)

	if err := b.Put(ctx, "a/b/segment_0.ts", strings.NewReader("payload"), "video/MP2T"); err != nil {
		t.Fatalf("put: %v", err)
	}

	r, err := b.Get(ctx, "a/b/segment_0.ts")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(got) != "payload" {
		t.Errorf("read %q, want payload", got)
	}
}

// A missing key is the ordinary case on the segment path -- the player asks
// for a segment that has not been produced yet -- so it must not surface as an
// error.
func TestTroveBackendExistsIsFalseNotAnError(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)

	ok, err := b.Exists(ctx, "nothing/here.ts")
	if err != nil {
		t.Fatalf("exists on a missing key returned %v, want nil", err)
	}

	if ok {
		t.Error("exists = true for a key that was never written")
	}

	if err := b.Put(ctx, "here.ts", strings.NewReader("x"), ""); err != nil {
		t.Fatalf("put: %v", err)
	}

	ok, err = b.Exists(ctx, "here.ts")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}

	if !ok {
		t.Error("exists = false for a key that was just written")
	}
}

// Backend documents this, and the cleanup loops in HLSStorage depend on it.
func TestTroveBackendDeleteOfAbsentKeySucceeds(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)

	if err := b.Delete(ctx, "never/existed.ts"); err != nil {
		t.Errorf("delete of an absent key returned %v, want nil", err)
	}
}

func TestTroveBackendListFiltersByPrefix(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)

	for _, key := range []string{"hls/s1/v1/segment_0.ts", "hls/s1/v1/playlist.m3u8", "hls/s2/v1/segment_0.ts"} {
		if err := b.Put(ctx, key, strings.NewReader("data"), ""); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}

	objects, err := b.List(ctx, "hls/s1/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(objects) != 2 {
		t.Fatalf("list returned %d objects, want 2: %+v", len(objects), objects)
	}

	for _, obj := range objects {
		if !strings.HasPrefix(obj.Key, "hls/s1/") {
			t.Errorf("list returned %q, which is outside the prefix", obj.Key)
		}

		if obj.Size != int64(len("data")) {
			t.Errorf("object %q has size %d, want %d", obj.Key, obj.Size, len("data"))
		}
	}
}

// GetStorageStats and DeleteStream both list before they have written
// anything, and neither should fail on a stream that does not exist.
func TestTroveBackendListOfMissingPrefixIsEmpty(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)

	objects, err := b.List(ctx, "no/such/stream/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(objects) != 0 {
		t.Errorf("list returned %d objects for a prefix that was never written", len(objects))
	}
}

// Every node in a distributed stream calls this at startup against the same
// bucket, so the second one through must not fail.
func TestTroveBackendEnsureBucketIsIdempotent(t *testing.T) {
	b := newBackend(t) // already ensured once

	if err := b.EnsureBucket(context.Background()); err != nil {
		t.Errorf("second EnsureBucket returned %v, want nil", err)
	}
}

func TestTroveBackendHealth(t *testing.T) {
	if err := newBackend(t).Health(context.Background()); err != nil {
		t.Errorf("health: %v", err)
	}
}
