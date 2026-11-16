package redis

import (
	"context"
	"testing"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// The private flag decides channel authorization, so it has to round-trip
// through Redis. Channels stored before the flag existed read back public.
func TestChannelStore_KeepsPrivateFlag(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore(newTestClient(t), "test:channels")

	for _, opts := range []streaming.ChannelOptions{
		{ID: "secret", Name: "secret", Private: true},
		{ID: "news", Name: "news"},
	} {
		if err := s.Create(ctx, local.NewLocalChannel(opts)); err != nil {
			t.Fatal(err)
		}
	}

	for id, want := range map[string]bool{"secret": true, "news": false} {
		ch, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}

		p, ok := ch.(interface{ IsPrivate() bool })
		if !ok {
			t.Fatalf("redis channel has no IsPrivate method")
		}

		if p.IsPrivate() != want {
			t.Errorf("channel %s: IsPrivate = %v, want %v", id, p.IsPrivate(), want)
		}
	}
}
