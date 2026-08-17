package storage_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/hls/storage"
)

// keysUnder lists everything below prefix, sorted, so assertions read in a
// stable order.
func keysUnder(t *testing.T, b storage.Backend, prefix string) []string {
	t.Helper()

	objects, err := b.List(context.Background(), prefix)
	if err != nil {
		t.Fatalf("list %s: %v", prefix, err)
	}

	keys := make([]string, 0, len(objects))
	for _, obj := range objects {
		keys = append(keys, obj.Key)
	}

	sort.Strings(keys)

	return keys
}

// writeSegments produces segments 0..n-1 for one variant, the way the manager
// would over the life of a live stream.
func writeSegments(t *testing.T, s *storage.HLSStorage, streamID, variantID string, n int) {
	t.Helper()

	for i := 0; i < n; i++ {
		if err := s.SaveSegment(context.Background(), streamID, variantID, i, []byte("x")); err != nil {
			t.Fatalf("save segment %d: %v", i, err)
		}
	}
}

// Segment keys are numbered, and listing returns them in lexicographic order,
// where "segment_10.ts" sorts before "segment_2.ts". Cleanup that trusts list
// order therefore deletes the newest segments and keeps the oldest, which is
// exactly backwards for a DVR window.
func TestCleanupKeepsTheNewestSegmentsNotTheLexicographicallyLast(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	s := storage.NewHLSStorage(b, "hls")

	writeSegments(t, s, "s1", "v1", 15)

	if err := s.CleanupOldSegments(ctx, "s1", 5); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	keys := keysUnder(t, b, "hls/s1/")
	if len(keys) != 5 {
		t.Fatalf("kept %d segments, want 5: %v", len(keys), keys)
	}

	for i := 10; i < 15; i++ {
		want := fmt.Sprintf("hls/s1/v1/segment_%d.ts", i)
		if !contains(keys, want) {
			t.Errorf("segment %d was deleted; the five newest must survive. kept: %v", i, keys)
		}
	}
}

// DVRWindowSize is per variant everywhere else in this extension: the tracker
// trims per variant and the playlist advertises the last N of one variant. If
// cleanup spends the same budget across every variant at once it deletes
// segments the playlists still reference.
func TestCleanupAppliesTheWindowPerVariant(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	s := storage.NewHLSStorage(b, "hls")

	for _, variant := range []string{"360p", "720p", "1080p"} {
		writeSegments(t, s, "s1", variant, 12)
	}

	if err := s.CleanupOldSegments(ctx, "s1", 4); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	for _, variant := range []string{"360p", "720p", "1080p"} {
		keys := keysUnder(t, b, "hls/s1/"+variant+"/")
		if len(keys) != 4 {
			t.Errorf("variant %s kept %d segments, want 4: %v", variant, len(keys), keys)
		}

		for i := 8; i < 12; i++ {
			want := fmt.Sprintf("hls/s1/%s/segment_%d.ts", variant, i)
			if !contains(keys, want) {
				t.Errorf("variant %s lost segment %d; kept: %v", variant, i, keys)
			}
		}
	}
}

// Playlists and metadata live under the same stream prefix as the segments.
func TestCleanupTouchesOnlySegments(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	s := storage.NewHLSStorage(b, "hls")

	writeSegments(t, s, "s1", "v1", 10)

	if err := s.SavePlaylist(ctx, "s1", "v1", "#EXTM3U"); err != nil {
		t.Fatalf("save playlist: %v", err)
	}

	if err := s.SaveMasterPlaylist(ctx, "s1", "#EXTM3U"); err != nil {
		t.Fatalf("save master: %v", err)
	}

	if err := s.CleanupOldSegments(ctx, "s1", 2); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	for _, key := range []string{"hls/s1/v1/playlist.m3u8", "hls/s1/master.m3u8"} {
		ok, err := b.Exists(ctx, key)
		if err != nil {
			t.Fatalf("exists %s: %v", key, err)
		}

		if !ok {
			t.Errorf("cleanup deleted %s, which is not a segment", key)
		}
	}
}

// A stream holding fewer segments than the window is the normal case early in
// a broadcast, and nothing should be removed.
func TestCleanupBelowTheWindowDeletesNothing(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	s := storage.NewHLSStorage(b, "hls")

	writeSegments(t, s, "s1", "v1", 3)

	if err := s.CleanupOldSegments(ctx, "s1", 10); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if keys := keysUnder(t, b, "hls/s1/"); len(keys) != 3 {
		t.Errorf("kept %d segments, want all 3: %v", len(keys), keys)
	}
}

// Anything under the stream prefix that is not a key this package generates
// gets left alone. Deleting a file we cannot order is worse than keeping it.
func TestCleanupIgnoresUnrecognisedSegmentKeys(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	s := storage.NewHLSStorage(b, "hls")

	writeSegments(t, s, "s1", "v1", 8)

	stray := "hls/s1/v1/handmade.ts"
	if err := b.Put(ctx, stray, strings.NewReader("x"), "video/MP2T"); err != nil {
		t.Fatalf("put stray: %v", err)
	}

	if err := s.CleanupOldSegments(ctx, "s1", 2); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	ok, err := b.Exists(ctx, stray)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}

	if !ok {
		t.Error("cleanup deleted a .ts key it could not parse a segment number from")
	}
}

func contains(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}

	return false
}
