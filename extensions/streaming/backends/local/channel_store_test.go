package local

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func newChannel(id string) *LocalChannel {
	return NewLocalChannel(streaming.ChannelOptions{ID: id, Name: id})
}

func seedChannel(t *testing.T, s streaming.ChannelStore, id string) *LocalChannel {
	t.Helper()

	ch := newChannel(id)
	if err := s.Create(context.Background(), ch); err != nil {
		t.Fatalf("Create(%s): %v", id, err)
	}

	return ch
}

func subscription(connID, userID string, filters map[string]any) streaming.Subscription {
	return NewLocalSubscription(streaming.SubscriptionOptions{
		ConnID:  connID,
		UserID:  userID,
		Filters: filters,
	})
}

func TestChannelStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	seedChannel(t, s, "chan-1")

	got, err := s.Get(ctx, "chan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.GetID() != "chan-1" {
		t.Errorf("Get returned %q, want chan-1", got.GetID())
	}

	if err := s.Create(ctx, newChannel("chan-1")); !errors.Is(err, streaming.ErrChannelAlreadyExists) {
		t.Errorf("duplicate Create = %v, want ErrChannelAlreadyExists", err)
	}

	if _, err := s.Get(ctx, "nope"); !errors.Is(err, streaming.ErrChannelNotFound) {
		t.Errorf("Get missing = %v, want ErrChannelNotFound", err)
	}

	if ok, _ := s.Exists(ctx, "chan-1"); !ok {
		t.Error("Exists(chan-1) = false, want true")
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(list) != 1 {
		t.Errorf("List = %d channels, want 1", len(list))
	}

	if err := s.Delete(ctx, "chan-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if err := s.Delete(ctx, "chan-1"); !errors.Is(err, streaming.ErrChannelNotFound) {
		t.Errorf("second Delete = %v, want ErrChannelNotFound", err)
	}
}

func TestChannelStore_DeleteDropsSubscriptions(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	seedChannel(t, s, "chan-1")

	if err := s.AddSubscription(ctx, "chan-1", subscription("c1", "alice", nil)); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if err := s.Delete(ctx, "chan-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.GetSubscriptions(ctx, "chan-1"); !errors.Is(err, streaming.ErrChannelNotFound) {
		t.Errorf("GetSubscriptions after Delete = %v, want ErrChannelNotFound", err)
	}

	channels, err := s.GetUserChannels(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserChannels: %v", err)
	}

	if len(channels) != 0 {
		t.Errorf("GetUserChannels(alice) = %d, want 0 after the channel was deleted", len(channels))
	}
}

func TestChannelStore_Subscriptions(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	seedChannel(t, s, "chan-1")

	if err := s.AddSubscription(ctx, "chan-1", subscription("c1", "alice", nil)); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if err := s.AddSubscription(ctx, "chan-1", subscription("c2", "bob", nil)); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	count, err := s.GetSubscriberCount(ctx, "chan-1")
	if err != nil {
		t.Fatalf("GetSubscriberCount: %v", err)
	}

	if count != 2 {
		t.Errorf("GetSubscriberCount = %d, want 2", count)
	}

	subscribed, err := s.IsSubscribed(ctx, "chan-1", "c1")
	if err != nil {
		t.Fatalf("IsSubscribed: %v", err)
	}

	if !subscribed {
		t.Error("IsSubscribed(c1) = false, want true")
	}

	if subscribed, _ := s.IsSubscribed(ctx, "chan-1", "c9"); subscribed {
		t.Error("IsSubscribed(c9) = true, want false")
	}

	if err := s.RemoveSubscription(ctx, "chan-1", "c1"); err != nil {
		t.Fatalf("RemoveSubscription: %v", err)
	}

	if subscribed, _ := s.IsSubscribed(ctx, "chan-1", "c1"); subscribed {
		t.Error("IsSubscribed(c1) = true after removal, want false")
	}
}

func TestChannelStore_SubscriptionErrors(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	seedChannel(t, s, "chan-1")

	if err := s.AddSubscription(ctx, "chan-1", subscription("c1", "alice", nil)); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	tests := []struct {
		name string
		call func() error
		want error
	}{
		{
			name: "subscribe to a missing channel",
			call: func() error { return s.AddSubscription(ctx, "nope", subscription("c1", "alice", nil)) },
			want: streaming.ErrChannelNotFound,
		},
		{
			name: "subscribe the same connection twice",
			call: func() error { return s.AddSubscription(ctx, "chan-1", subscription("c1", "alice", nil)) },
			want: streaming.ErrAlreadySubscribed,
		},
		{
			name: "unsubscribe from a missing channel",
			call: func() error { return s.RemoveSubscription(ctx, "nope", "c1") },
			want: streaming.ErrChannelNotFound,
		},
		{
			name: "unsubscribe a connection that is not subscribed",
			call: func() error { return s.RemoveSubscription(ctx, "chan-1", "c9") },
			want: streaming.ErrNotSubscribed,
		},
		{
			name: "list subscriptions of a missing channel",
			call: func() error { _, err := s.GetSubscriptions(ctx, "nope"); return err },
			want: streaming.ErrChannelNotFound,
		},
		{
			name: "count subscribers of a missing channel",
			call: func() error { _, err := s.GetSubscriberCount(ctx, "nope"); return err },
			want: streaming.ErrChannelNotFound,
		},
		{
			name: "is-subscribed on a missing channel",
			call: func() error { _, err := s.IsSubscribed(ctx, "nope", "c1"); return err },
			want: streaming.ErrChannelNotFound,
		},
		{
			name: "publish to a missing channel",
			call: func() error { return s.Publish(ctx, "nope", &streaming.Message{}) },
			want: streaming.ErrChannelNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestChannelStore_GetUserChannels(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	for _, id := range []string{"chan-1", "chan-2", "chan-3"} {
		seedChannel(t, s, id)
	}

	// alice is on two channels, once via two connections on the same channel.
	for _, sub := range []struct{ channel, conn, user string }{
		{"chan-1", "c1", "alice"},
		{"chan-1", "c2", "alice"},
		{"chan-2", "c3", "alice"},
		{"chan-3", "c4", "bob"},
	} {
		if err := s.AddSubscription(ctx, sub.channel, subscription(sub.conn, sub.user, nil)); err != nil {
			t.Fatalf("AddSubscription: %v", err)
		}
	}

	channels, err := s.GetUserChannels(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserChannels: %v", err)
	}

	// Two connections on chan-1 must still yield chan-1 once.
	if len(channels) != 2 {
		t.Errorf("GetUserChannels(alice) = %d channels, want 2 (deduplicated)", len(channels))
	}

	none, err := s.GetUserChannels(ctx, "nobody")
	if err != nil {
		t.Fatalf("GetUserChannels: %v", err)
	}

	if len(none) != 0 {
		t.Errorf("GetUserChannels(nobody) = %d, want 0", len(none))
	}
}

func TestChannelStore_PublishCountsMessages(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	ch := seedChannel(t, s, "chan-1")

	for range 3 {
		if err := s.Publish(ctx, "chan-1", &streaming.Message{ID: "m"}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	if got := ch.GetMessageCount(); got != 3 {
		t.Errorf("GetMessageCount = %d, want 3", got)
	}
}

func TestChannelStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

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

func TestLocalChannel_MethodsAreDelegatedToTheStore(t *testing.T) {
	// LocalChannel deliberately refuses the subscription and publish methods:
	// the ChannelStore owns that state. The interface still requires them, so
	// they return ErrInvalidChannel rather than silently doing nothing.
	ctx := context.Background()
	ch := newChannel("chan-1")

	tests := []struct {
		name string
		call func() error
	}{
		{"Subscribe", func() error { return ch.Subscribe(ctx, subscription("c1", "alice", nil)) }},
		{"Unsubscribe", func() error { return ch.Unsubscribe(ctx, "c1") }},
		{"GetSubscribers", func() error { _, err := ch.GetSubscribers(ctx); return err }},
		{"GetSubscriberCount", func() error { _, err := ch.GetSubscriberCount(ctx); return err }},
		{"IsSubscribed", func() error { _, err := ch.IsSubscribed(ctx, "c1"); return err }},
		{"Publish", func() error { return ch.Publish(ctx, &streaming.Message{}) }},
		{"Delete", func() error { return ch.Delete(ctx) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, streaming.ErrInvalidChannel) {
				t.Errorf("got %v, want ErrInvalidChannel", err)
			}
		})
	}
}

func TestLocalSubscription_Accessors(t *testing.T) {
	sub := NewLocalSubscription(streaming.SubscriptionOptions{
		ConnID:  "c1",
		UserID:  "alice",
		Filters: map[string]any{"topic": "news"},
	})

	if sub.GetConnID() != "c1" || sub.GetUserID() != "alice" {
		t.Errorf("accessors = %q/%q, want c1/alice", sub.GetConnID(), sub.GetUserID())
	}

	if sub.GetSubscribedAt().IsZero() {
		t.Error("GetSubscribedAt is zero, want the construction time")
	}

	filters := sub.GetFilters()
	filters["topic"] = "mutated"

	if sub.GetFilters()["topic"] != "news" {
		t.Error("GetFilters handed out the internal map; mutating it changed the subscription")
	}
}

func TestLocalSubscription_MatchesFilter(t *testing.T) {
	tests := []struct {
		name    string
		filters map[string]any
		msg     *streaming.Message
		want    bool
	}{
		{
			name: "no filters matches everything",
			msg:  &streaming.Message{ID: "m"},
			want: true,
		},
		{
			name:    "empty filter map matches everything",
			filters: map[string]any{},
			msg:     &streaming.Message{ID: "m"},
			want:    true,
		},
		{
			name:    "matching metadata",
			filters: map[string]any{"topic": "news"},
			msg:     &streaming.Message{Metadata: map[string]any{"topic": "news"}},
			want:    true,
		},
		{
			name:    "non-matching metadata value",
			filters: map[string]any{"topic": "news"},
			msg:     &streaming.Message{Metadata: map[string]any{"topic": "sport"}},
			want:    false,
		},
		{
			name:    "missing metadata key",
			filters: map[string]any{"topic": "news"},
			msg:     &streaming.Message{Metadata: map[string]any{"other": "x"}},
			want:    false,
		},
		{
			name:    "nil metadata with a filter set",
			filters: map[string]any{"topic": "news"},
			msg:     &streaming.Message{ID: "m"},
			want:    false,
		},
		{
			name:    "all filters must match",
			filters: map[string]any{"topic": "news", "lang": "en"},
			msg:     &streaming.Message{Metadata: map[string]any{"topic": "news"}},
			want:    false,
		},
		{
			name:    "every filter satisfied",
			filters: map[string]any{"topic": "news", "lang": "en"},
			msg:     &streaming.Message{Metadata: map[string]any{"topic": "news", "lang": "en", "extra": 1}},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := NewLocalSubscription(streaming.SubscriptionOptions{ConnID: "c1", Filters: tt.filters})

			if got := sub.MatchesFilter(tt.msg); got != tt.want {
				t.Errorf("MatchesFilter = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocalSubscription_SetFilters(t *testing.T) {
	sub := NewLocalSubscription(streaming.SubscriptionOptions{ConnID: "c1"})

	msg := &streaming.Message{Metadata: map[string]any{"topic": "news"}}

	if !sub.MatchesFilter(msg) {
		t.Fatal("MatchesFilter = false with no filters, want true")
	}

	sub.SetFilters(map[string]any{"topic": "sport"})

	if sub.MatchesFilter(msg) {
		t.Error("MatchesFilter = true after SetFilters to a non-matching value")
	}
}

func TestChannelStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	for i := range 4 {
		seedChannel(t, s, fmt.Sprintf("chan-%d", i))
	}

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			for i := range 50 {
				channelID := fmt.Sprintf("chan-%d", i%4)
				connID := fmt.Sprintf("c%d-%d", w, i)

				_ = s.AddSubscription(ctx, channelID, subscription(connID, fmt.Sprintf("u%d", w), nil))
				_, _ = s.GetSubscriptions(ctx, channelID)
				_, _ = s.GetSubscriberCount(ctx, channelID)
				_, _ = s.GetUserChannels(ctx, fmt.Sprintf("u%d", w))
				_ = s.Publish(ctx, channelID, &streaming.Message{ID: "m"})
				_ = s.RemoveSubscription(ctx, channelID, connID)
			}
		}(w)
	}

	wg.Wait()
}
