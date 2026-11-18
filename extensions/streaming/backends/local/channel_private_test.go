package local

import (
	"context"
	"testing"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// opaqueChannel is a Channel that is not a *LocalChannel, so Create has to
// copy it.
type opaqueChannel struct {
	*LocalChannel
}

// The private flag decides channel authorization, so it has to survive both a
// direct Create and the copy Create makes of a foreign Channel.
func TestChannelStore_KeepsPrivateFlag(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	direct := NewLocalChannel(streaming.ChannelOptions{ID: "a", Name: "a", Private: true})
	copied := opaqueChannel{NewLocalChannel(streaming.ChannelOptions{ID: "b", Name: "b", Private: true})}
	public := NewLocalChannel(streaming.ChannelOptions{ID: "c", Name: "c"})

	for _, ch := range []streaming.Channel{direct, copied, public} {
		if err := s.Create(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}

	for id, want := range map[string]bool{"a": true, "b": true, "c": false} {
		ch, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}

		if got := ch.(*LocalChannel).IsPrivate(); got != want {
			t.Errorf("channel %s: IsPrivate = %v, want %v", id, got, want)
		}
	}
}
