package contract

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func statsHandler(mgr ManagerProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (StatsResponse, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (StatsResponse, error) {
		m := mgr()
		if m == nil {
			return StatsResponse{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		stats, err := m.GetStats(ctx)
		if err != nil {
			return StatsResponse{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		return statsToResponse(stats), nil
	}
}

func statsToResponse(s *streaming.ManagerStats) StatsResponse {
	if s == nil {
		return StatsResponse{}
	}
	return StatsResponse{
		TotalConnections: s.TotalConnections,
		TotalRooms:       s.TotalRooms,
		TotalChannels:    s.TotalChannels,
		TotalMessages:    s.TotalMessages,
		OnlineUsers:      s.OnlineUsers,
		MessagesPerSec:   s.MessagesPerSec,
		UptimeSeconds:    int64(s.Uptime.Seconds()),
		MemoryBytes:      s.MemoryUsage,
	}
}
