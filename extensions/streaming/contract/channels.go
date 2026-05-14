package contract

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func channelsListHandler(mgr ManagerProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (ChannelsList, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (ChannelsList, error) {
		m := mgr()
		if m == nil {
			return ChannelsList{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		channels, err := m.ListChannels(ctx)
		if err != nil {
			return ChannelsList{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		out := make([]ChannelInfo, 0, len(channels))
		for _, c := range channels {
			subs := 0
			if sc, ok := c.(interface{ SubscriberCount() int }); ok {
				subs = sc.SubscriberCount()
			}
			out = append(out, ChannelInfo{
				ID:              c.GetID(),
				Name:            c.GetName(),
				SubscriberCount: subs,
				MessageCount:    c.GetMessageCount(),
			})
		}
		return ChannelsList{Channels: out}, nil
	}
}
