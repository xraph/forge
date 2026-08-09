package local

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// msg builds a message with a timestamp offset from a fixed base, so ordering
// assertions do not depend on wall-clock resolution.
func msg(id, roomID, userID string, offset time.Duration) *streaming.Message {
	return &streaming.Message{
		ID:        id,
		Type:      streaming.MessageTypeMessage,
		RoomID:    roomID,
		UserID:    userID,
		Data:      "body of " + id,
		Timestamp: msgBase.Add(offset),
	}
}

var msgBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func TestMessageStore_SaveAndGet(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	m := msg("m1", "room-1", "alice", 0)

	if err := s.Save(ctx, m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(ctx, "m1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != "m1" {
		t.Errorf("Get returned %q, want m1", got.ID)
	}

	if _, err := s.Get(ctx, "nope"); !errors.Is(err, streaming.ErrMessageNotFound) {
		t.Errorf("Get missing = %v, want ErrMessageNotFound", err)
	}
}

func TestMessageStore_SaveBatch(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	batch := []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-1", "bob", time.Minute),
	}

	if err := s.SaveBatch(ctx, batch); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}

	count, err := s.GetMessageCount(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}

	if count != 2 {
		t.Errorf("GetMessageCount = %d, want 2", count)
	}
}

func TestMessageStore_SaveWithoutRoomOrUserIsNotIndexed(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	if err := s.Save(ctx, &streaming.Message{ID: "orphan", Timestamp: msgBase}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Retrievable by ID, but absent from every index.
	if _, err := s.Get(ctx, "orphan"); err != nil {
		t.Errorf("Get = %v, want the message to be retrievable by ID", err)
	}

	history, err := s.GetHistory(ctx, "", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 0 {
		t.Errorf("GetHistory(\"\") = %d messages, want 0", len(history))
	}
}

func TestMessageStore_GetHistoryOrdersNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	// Saved out of chronological order to prove the sort is real.
	for _, m := range []*streaming.Message{
		msg("middle", "room-1", "alice", time.Minute),
		msg("oldest", "room-1", "alice", 0),
		msg("newest", "room-1", "alice", 2*time.Minute),
	} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	history, err := s.GetHistory(ctx, "room-1", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	want := []string{"newest", "middle", "oldest"}
	if len(history) != len(want) {
		t.Fatalf("GetHistory = %d messages, want %d", len(history), len(want))
	}

	for i, id := range want {
		if history[i].ID != id {
			t.Errorf("message %d = %q, want %q", i, history[i].ID, id)
		}
	}
}

func TestMessageStore_GetHistoryUnknownRoom(t *testing.T) {
	history, err := NewMessageStore().GetHistory(context.Background(), "nope", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 0 {
		t.Errorf("GetHistory(unknown room) = %d, want 0", len(history))
	}
}

func TestMessageStore_GetHistoryFilters(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	messages := []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-1", "bob", 10*time.Minute),
		msg("m3", "room-1", "alice", 20*time.Minute),
	}

	messages[2].ThreadID = "t1"
	messages[1].Event = "special-event"

	for _, m := range messages {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	tests := []struct {
		name  string
		query streaming.HistoryQuery
		want  []string
	}{
		{
			name:  "no filters",
			query: streaming.HistoryQuery{},
			want:  []string{"m3", "m2", "m1"},
		},
		{
			name:  "limit keeps the newest",
			query: streaming.HistoryQuery{Limit: 2},
			want:  []string{"m3", "m2"},
		},
		{
			name:  "before excludes later messages",
			query: streaming.HistoryQuery{Before: msgBase.Add(15 * time.Minute)},
			want:  []string{"m2", "m1"},
		},
		{
			name:  "after excludes earlier messages",
			query: streaming.HistoryQuery{After: msgBase.Add(5 * time.Minute)},
			want:  []string{"m3", "m2"},
		},
		{
			name:  "before and after bracket a window",
			query: streaming.HistoryQuery{After: msgBase.Add(5 * time.Minute), Before: msgBase.Add(15 * time.Minute)},
			want:  []string{"m2"},
		},
		{
			name:  "filter by user",
			query: streaming.HistoryQuery{UserID: "alice"},
			want:  []string{"m3", "m1"},
		},
		{
			name:  "filter by thread",
			query: streaming.HistoryQuery{ThreadID: "t1"},
			want:  []string{"m3"},
		},
		{
			name:  "search matches message body",
			query: streaming.HistoryQuery{SearchTerm: "body of m1"},
			want:  []string{"m1"},
		},
		{
			name:  "search matches the event name",
			query: streaming.HistoryQuery{SearchTerm: "SPECIAL-EVENT"},
			want:  []string{"m2"},
		},
		{
			name:  "search with no match",
			query: streaming.HistoryQuery{SearchTerm: "nothing here"},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.GetHistory(ctx, "room-1", tt.query)
			if err != nil {
				t.Fatalf("GetHistory: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("GetHistory = %d messages, want %d", len(got), len(tt.want))
			}

			for i, id := range tt.want {
				if got[i].ID != id {
					t.Errorf("message %d = %q, want %q", i, got[i].ID, id)
				}
			}
		})
	}
}

func TestMessageStore_ThreadHistory(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	root := msg("root", "room-1", "alice", 0)

	reply1 := msg("reply1", "room-1", "bob", time.Minute)
	reply1.ThreadID = "t1"

	reply2 := msg("reply2", "room-1", "carol", 2*time.Minute)
	reply2.ThreadID = "t1"

	other := msg("other", "room-1", "dave", 3*time.Minute)
	other.ThreadID = "t2"

	for _, m := range []*streaming.Message{root, reply1, reply2, other} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	thread, err := s.GetThreadHistory(ctx, "room-1", "t1", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetThreadHistory: %v", err)
	}

	if len(thread) != 2 {
		t.Fatalf("GetThreadHistory = %d messages, want 2", len(thread))
	}

	if thread[0].ID != "reply2" {
		t.Errorf("newest thread message = %q, want reply2", thread[0].ID)
	}

	empty, err := s.GetThreadHistory(ctx, "room-1", "absent", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetThreadHistory: %v", err)
	}

	if len(empty) != 0 {
		t.Errorf("GetThreadHistory(absent) = %d, want 0", len(empty))
	}
}

func TestMessageStore_GetUserMessages(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	for _, m := range []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-2", "alice", time.Minute),
		msg("m3", "room-1", "bob", 2*time.Minute),
	} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// User messages span rooms.
	got, err := s.GetUserMessages(ctx, "alice", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetUserMessages: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("GetUserMessages(alice) = %d, want 2", len(got))
	}

	none, err := s.GetUserMessages(ctx, "nobody", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetUserMessages: %v", err)
	}

	if len(none) != 0 {
		t.Errorf("GetUserMessages(nobody) = %d, want 0", len(none))
	}
}

func TestMessageStore_Search(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	hit := msg("hit", "room-1", "alice", 0)
	hit.Data = "Hello World"

	miss := msg("miss", "room-1", "bob", time.Minute)
	miss.Data = "unrelated"

	nonString := msg("non-string", "room-1", "carol", 2*time.Minute)
	nonString.Data = map[string]any{"text": "hello world"}

	for _, m := range []*streaming.Message{hit, miss, nonString} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got, err := s.Search(ctx, "room-1", "hello", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Search only inspects string Data and Event, so the map payload is missed
	// even though it contains the term.
	if len(got) != 1 || got[0].ID != "hit" {
		t.Errorf("Search(hello) = %v, want only the string-bodied message", got)
	}
}

func TestMessageStore_Counts(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	for _, m := range []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-1", "alice", time.Minute),
		msg("m3", "room-1", "bob", 2*time.Minute),
	} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	total, err := s.GetMessageCount(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}

	if total != 3 {
		t.Errorf("GetMessageCount = %d, want 3", total)
	}

	byUser, err := s.GetMessageCountByUser(ctx, "room-1", "alice")
	if err != nil {
		t.Fatalf("GetMessageCountByUser: %v", err)
	}

	if byUser != 2 {
		t.Errorf("GetMessageCountByUser(alice) = %d, want 2", byUser)
	}

	if n, _ := s.GetMessageCount(ctx, "unknown"); n != 0 {
		t.Errorf("GetMessageCount(unknown) = %d, want 0", n)
	}

	if n, _ := s.GetMessageCountByUser(ctx, "unknown", "alice"); n != 0 {
		t.Errorf("GetMessageCountByUser(unknown room) = %d, want 0", n)
	}
}

func TestMessageStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	for _, m := range []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-1", "alice", time.Minute),
	} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	if err := s.Delete(ctx, "m1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Get(ctx, "m1"); !errors.Is(err, streaming.ErrMessageNotFound) {
		t.Errorf("Get after Delete = %v, want ErrMessageNotFound", err)
	}

	if err := s.Delete(ctx, "m1"); !errors.Is(err, streaming.ErrMessageNotFound) {
		t.Errorf("second Delete = %v, want ErrMessageNotFound", err)
	}
}

func TestMessageStore_DeleteMaintainsTheRoomIndex(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	for _, m := range []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-1", "alice", time.Minute),
	} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	if err := s.Delete(ctx, "m1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	history, err := s.GetHistory(ctx, "room-1", streaming.HistoryQuery{})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 1 || history[0].ID != "m2" {
		t.Errorf("GetHistory = %v, want [m2]", history)
	}

	count, err := s.GetMessageCount(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}

	if count != 1 {
		t.Errorf("GetMessageCount = %d, want 1", count)
	}
}

func TestMessageStore_DeleteByRoom(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	for _, m := range []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-1", "bob", time.Minute),
		msg("m3", "room-2", "alice", 2*time.Minute),
	} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	if err := s.DeleteByRoom(ctx, "room-1"); err != nil {
		t.Fatalf("DeleteByRoom: %v", err)
	}

	if history, _ := s.GetHistory(ctx, "room-1", streaming.HistoryQuery{}); len(history) != 0 {
		t.Errorf("room-1 history = %d messages, want 0", len(history))
	}

	if history, _ := s.GetHistory(ctx, "room-2", streaming.HistoryQuery{}); len(history) != 1 {
		t.Errorf("room-2 history = %d messages, want 1 (untouched)", len(history))
	}

	// Deleting a room with no messages is a no-op, not an error.
	if err := s.DeleteByRoom(ctx, "unknown"); err != nil {
		t.Errorf("DeleteByRoom(unknown) = %v, want nil", err)
	}
}

func TestMessageStore_DeleteByUser(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	for _, m := range []*streaming.Message{
		msg("m1", "room-1", "alice", 0),
		msg("m2", "room-1", "bob", time.Minute),
	} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	if err := s.DeleteByUser(ctx, "alice"); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}

	if _, err := s.Get(ctx, "m1"); !errors.Is(err, streaming.ErrMessageNotFound) {
		t.Errorf("alice's message survived DeleteByUser: %v", err)
	}

	if _, err := s.Get(ctx, "m2"); err != nil {
		t.Errorf("bob's message was deleted too: %v", err)
	}

	if err := s.DeleteByUser(ctx, "nobody"); err != nil {
		t.Errorf("DeleteByUser(nobody) = %v, want nil", err)
	}
}

func TestMessageStore_DeleteOld(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	old := &streaming.Message{
		ID: "old", RoomID: "room-1", UserID: "alice",
		Timestamp: time.Now().Add(-48 * time.Hour),
	}
	recent := &streaming.Message{
		ID: "recent", RoomID: "room-1", UserID: "alice",
		Timestamp: time.Now(),
	}

	for _, m := range []*streaming.Message{old, recent} {
		if err := s.Save(ctx, m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	if err := s.DeleteOld(ctx, 24*time.Hour); err != nil {
		t.Fatalf("DeleteOld: %v", err)
	}

	if _, err := s.Get(ctx, "old"); !errors.Is(err, streaming.ErrMessageNotFound) {
		t.Errorf("old message survived DeleteOld: %v", err)
	}

	if _, err := s.Get(ctx, "recent"); err != nil {
		t.Errorf("recent message was deleted: %v", err)
	}
}

func TestMessageStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	for _, call := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"Connect", s.Connect},
		{"Ping", s.Ping},
		{"Disconnect", s.Disconnect},
	} {
		if err := call.fn(ctx); err != nil {
			t.Errorf("%s = %v, want nil (no-op for the local backend)", call.name, err)
		}
	}
}

func TestMessageStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	s := NewMessageStore()

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			for i := range 50 {
				id := fmt.Sprintf("w%d-m%d", w, i)

				_ = s.Save(ctx, msg(id, fmt.Sprintf("room-%d", i%3), fmt.Sprintf("u%d", w), time.Duration(i)*time.Second))
				_, _ = s.Get(ctx, id)
				_, _ = s.GetHistory(ctx, "room-0", streaming.HistoryQuery{Limit: 10})
				_, _ = s.GetMessageCount(ctx, "room-0")
				_, _ = s.Search(ctx, "room-0", "body", streaming.HistoryQuery{})
				_ = s.Delete(ctx, id)
			}
		}(w)
	}

	wg.Wait()
}
