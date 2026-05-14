package contract

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// presenceListHandler aggregates presence across the unique user IDs from
// active connections. The legacy templ contributor does the same per-user
// loop server-side; we centralize it here so the React shell can render a
// flat resource.list.
func presenceListHandler(mgr ManagerProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (PresenceList, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (PresenceList, error) {
		m := mgr()
		if m == nil {
			return PresenceList{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		seen := make(map[string]struct{})
		uids := make([]string, 0)
		for _, c := range m.GetAllConnections() {
			uid := c.GetUserID()
			if uid == "" {
				continue
			}
			if _, ok := seen[uid]; ok {
				continue
			}
			seen[uid] = struct{}{}
			uids = append(uids, uid)
		}

		out := make([]PresenceInfo, 0, len(uids))
		for _, uid := range uids {
			p, err := m.GetPresence(ctx, uid)
			if err != nil || p == nil {
				continue
			}
			out = append(out, PresenceInfo{
				UserID:       p.UserID,
				Status:       p.Status,
				CustomStatus: p.CustomStatus,
				LastSeen:     p.LastSeen,
				// p.Connections holds connection IDs; we surface those as
				// the "rooms" column proxy until UserPresence carries an
				// explicit room list.
				Rooms: append([]string{}, p.Connections...),
			})
		}
		return PresenceList{Presence: out}, nil
	}
}

func setPresenceHandler(mgr ManagerProvider) func(ctx context.Context, in SetPresenceInput, _ contract.Principal) (CommandResult, error) {
	return func(ctx context.Context, in SetPresenceInput, _ contract.Principal) (CommandResult, error) {
		if in.UserID == "" || in.Status == "" {
			return CommandResult{}, &contract.Error{Code: contract.CodeBadRequest, Message: "userID and status are required"}
		}
		m := mgr()
		if m == nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		if err := m.SetPresence(ctx, in.UserID, in.Status); err != nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		return CommandResult{OK: true, Message: "presence updated"}, nil
	}
}
