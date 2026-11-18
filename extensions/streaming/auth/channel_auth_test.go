package auth

import (
	"context"
	"testing"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func TestChannelAuthorizer_DefaultPolicy(t *testing.T) {
	ctx := context.Background()
	store := local.NewChannelStore()

	for _, opts := range []streaming.ChannelOptions{
		{ID: "news", Name: "news"},
		{ID: "secret", Name: "secret", Private: true},
	} {
		if err := store.Create(ctx, local.NewLocalChannel(opts)); err != nil {
			t.Fatal(err)
		}
	}

	ca := NewChannelAuthorizer(store)

	cases := []struct {
		channel string
		want    bool
	}{
		{"news", true},
		{"secret", false},
		// Unknown channels are left to the store, which refuses them itself.
		{"missing", true},
	}

	for _, tc := range cases {
		got, err := ca.CanSubscribe(ctx, "alice", tc.channel)
		if err != nil {
			t.Fatalf("%s: %v", tc.channel, err)
		}

		if got != tc.want {
			t.Errorf("%s: CanSubscribe = %v, want %v", tc.channel, got, tc.want)
		}
	}
}
