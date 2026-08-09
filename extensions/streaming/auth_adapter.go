package streaming

import (
	"context"

	streamauth "github.com/xraph/forge/extensions/streaming/auth"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// authMessageStore adapts a streaming MessageStore to the narrow read-only
// interface the message authorizer needs.
//
// The auth package deliberately declares its own minimal MessageStore rather
// than importing the full streaming one: an authorizer needs to know who wrote
// a message and where it lives, and nothing else. Handing it the complete store
// would let a policy decision reach for Save or Delete, which is exactly the
// coupling the narrow interface exists to prevent. This adapter is the seam.
type authMessageStore struct {
	store streaming.MessageStore
}

func newAuthMessageStore(store streaming.MessageStore) streamauth.MessageStore {
	return &authMessageStore{store: store}
}

func (a *authMessageStore) Get(ctx context.Context, messageID string) (*streamauth.MessageInfo, error) {
	msg, err := a.store.Get(ctx, messageID)
	if err != nil {
		return nil, err
	}

	return &streamauth.MessageInfo{
		ID:        msg.ID,
		UserID:    msg.UserID,
		RoomID:    msg.RoomID,
		ChannelID: msg.ChannelID,
		Content:   msg.Data,
		Metadata:  msg.Metadata,
	}, nil
}
