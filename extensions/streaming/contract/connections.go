package contract

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func connectionsListHandler(mgr ManagerProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (ConnectionsList, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (ConnectionsList, error) {
		m := mgr()
		if m == nil {
			return ConnectionsList{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		conns := m.GetAllConnections()
		out := make([]ConnectionInfo, 0, len(conns))
		for _, c := range conns {
			out = append(out, projectConnection(c))
		}
		return ConnectionsList{Connections: out}, nil
	}
}

func kickConnectionHandler(mgr ManagerProvider) func(ctx context.Context, in KickConnectionInput, _ contract.Principal) (CommandResult, error) {
	return func(ctx context.Context, in KickConnectionInput, _ contract.Principal) (CommandResult, error) {
		m := mgr()
		if m == nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		if in.ConnID == "" {
			return CommandResult{}, &contract.Error{Code: contract.CodeBadRequest, Message: "connID is required"}
		}
		if err := m.KickConnection(ctx, in.ConnID, in.Reason); err != nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		return CommandResult{OK: true, Message: "connection kicked"}, nil
	}
}

func projectConnection(c streaming.EnhancedConnection) ConnectionInfo {
	status := "active"
	if c.IsClosed() {
		status = "closed"
	}
	return ConnectionInfo{
		ConnID:        c.ID(),
		UserID:        c.GetUserID(),
		Transport:     c.GetTransport(),
		JoinedRooms:   append([]string{}, c.GetJoinedRooms()...),
		Subscriptions: append([]string{}, c.GetSubscriptions()...),
		LastActivity:  c.GetLastActivity(),
		Status:        status,
	}
}
