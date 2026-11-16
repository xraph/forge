package auth

import (
	"context"

	"github.com/xraph/forge/errors"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// ChannelAuthorizerFunc adapts a function to ChannelAuthorizer.
type ChannelAuthorizerFunc func(ctx context.Context, userID, channelID string) (bool, error)

// CanSubscribe calls f.
func (f ChannelAuthorizerFunc) CanSubscribe(ctx context.Context, userID, channelID string) (bool, error) {
	return f(ctx, userID, channelID)
}

// channelAuthorizer is the default ChannelAuthorizer.
type channelAuthorizer struct {
	channelStore streaming.ChannelStore
}

// NewChannelAuthorizer returns the default channel policy: anyone the manager
// lets through may subscribe to a public channel, and nobody may subscribe to
// a private one.
//
// A channel has no member list the store could check, so the default policy
// cannot tell who belongs to a private channel and refuses everyone. An
// application that uses private channels supplies its own ChannelAuthorizer,
// which knows its users, for example with ChannelAuthorizerFunc.
//
// A channel the store does not know is left to the store to refuse, as before:
// it has no private flag to leak through.
func NewChannelAuthorizer(channelStore streaming.ChannelStore) ChannelAuthorizer {
	return &channelAuthorizer{channelStore: channelStore}
}

// CanSubscribe implements ChannelAuthorizer.
func (ca *channelAuthorizer) CanSubscribe(ctx context.Context, userID, channelID string) (bool, error) {
	channel, err := ca.channelStore.Get(ctx, channelID)
	if errors.Is(err, streaming.ErrChannelNotFound) {
		return true, nil
	}

	if err != nil {
		return false, err
	}

	return !IsPrivateChannel(channel), nil
}

// IsPrivateChannel reports whether a channel was created with
// ChannelOptions.Private. The Channel interface predates the flag, so it is
// read through an optional IsPrivate method; a channel without one is public.
func IsPrivateChannel(channel streaming.Channel) bool {
	p, ok := channel.(interface{ IsPrivate() bool })

	return ok && p.IsPrivate()
}
