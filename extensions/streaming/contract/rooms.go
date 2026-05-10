package contract

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func roomsListHandler(mgr ManagerProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (RoomsList, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (RoomsList, error) {
		m := mgr()
		if m == nil {
			return RoomsList{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		rooms, err := m.ListRooms(ctx)
		if err != nil {
			return RoomsList{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		out := make([]RoomInfo, 0, len(rooms))
		for _, r := range rooms {
			members, _ := r.MemberCount(ctx)
			out = append(out, projectRoom(r, members))
		}
		return RoomsList{Rooms: out}, nil
	}
}

func roomDetailHandler(mgr ManagerProvider) func(ctx context.Context, in RoomDetailInput, _ contract.Principal) (RoomInfo, error) {
	return func(ctx context.Context, in RoomDetailInput, _ contract.Principal) (RoomInfo, error) {
		if in.ID == "" {
			return RoomInfo{}, &contract.Error{Code: contract.CodeBadRequest, Message: "id is required"}
		}
		m := mgr()
		if m == nil {
			return RoomInfo{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		r, err := m.GetRoom(ctx, in.ID)
		if err != nil {
			return RoomInfo{}, &contract.Error{Code: contract.CodeNotFound, Message: err.Error()}
		}
		count, _ := r.MemberCount(ctx)
		return projectRoom(r, count), nil
	}
}

func roomMembersHandler(mgr ManagerProvider) func(ctx context.Context, in RoomDetailInput, _ contract.Principal) (MembersList, error) {
	return func(ctx context.Context, in RoomDetailInput, _ contract.Principal) (MembersList, error) {
		if in.ID == "" {
			return MembersList{}, &contract.Error{Code: contract.CodeBadRequest, Message: "id is required"}
		}
		m := mgr()
		if m == nil {
			return MembersList{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		members, err := m.GetRoomMembers(ctx, in.ID)
		if err != nil {
			return MembersList{}, &contract.Error{Code: contract.CodeNotFound, Message: err.Error()}
		}
		out := make([]MemberInfo, 0, len(members))
		for _, mb := range members {
			out = append(out, MemberInfo{
				UserID:      mb.GetUserID(),
				Role:        mb.GetRole(),
				JoinedAt:    mb.GetJoinedAt(),
				Permissions: append([]string{}, mb.GetPermissions()...),
			})
		}
		return MembersList{Members: out}, nil
	}
}

func roomModerationHandler(mgr ManagerProvider) func(ctx context.Context, in RoomDetailInput, _ contract.Principal) (ModerationLog, error) {
	return func(ctx context.Context, in RoomDetailInput, _ contract.Principal) (ModerationLog, error) {
		if in.ID == "" {
			return ModerationLog{}, &contract.Error{Code: contract.CodeBadRequest, Message: "id is required"}
		}
		m := mgr()
		if m == nil {
			return ModerationLog{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		entries, err := m.GetModerationLog(ctx, in.ID, 100)
		if err != nil {
			return ModerationLog{}, &contract.Error{Code: contract.CodeNotFound, Message: err.Error()}
		}
		return ModerationLog{Entries: projectModerationEntries(entries)}, nil
	}
}

func createRoomHandler(mgr ManagerProvider) func(ctx context.Context, in CreateRoomInput, _ contract.Principal) (CommandResult, error) {
	return func(ctx context.Context, in CreateRoomInput, _ contract.Principal) (CommandResult, error) {
		if in.Name == "" {
			return CommandResult{}, &contract.Error{Code: contract.CodeBadRequest, Message: "name is required"}
		}
		m := mgr()
		if m == nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		room := local.NewRoom(streaming.RoomOptions{
			ID:          uuid.NewString(),
			Name:        in.Name,
			Description: in.Description,
			Owner:       in.Owner,
			Private:     in.Private,
		})
		if err := m.CreateRoom(ctx, room); err != nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		return CommandResult{OK: true, Message: "room created", ID: room.GetID()}, nil
	}
}

func deleteRoomHandler(mgr ManagerProvider) func(ctx context.Context, in DeleteRoomInput, _ contract.Principal) (CommandResult, error) {
	return func(ctx context.Context, in DeleteRoomInput, _ contract.Principal) (CommandResult, error) {
		if in.ID == "" {
			return CommandResult{}, &contract.Error{Code: contract.CodeBadRequest, Message: "id is required"}
		}
		m := mgr()
		if m == nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		if err := m.DeleteRoom(ctx, in.ID); err != nil {
			if errors.Is(err, streaming.ErrRoomNotFound) {
				return CommandResult{}, &contract.Error{Code: contract.CodeNotFound, Message: err.Error()}
			}
			return CommandResult{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		return CommandResult{OK: true, Message: "room deleted"}, nil
	}
}

func sendMessageHandler(mgr ManagerProvider) func(ctx context.Context, in SendMessageInput, _ contract.Principal) (CommandResult, error) {
	return func(ctx context.Context, in SendMessageInput, _ contract.Principal) (CommandResult, error) {
		if in.RoomID == "" || in.Content == "" {
			return CommandResult{}, &contract.Error{Code: contract.CodeBadRequest, Message: "roomID and content are required"}
		}
		m := mgr()
		if m == nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeUnavailable, Message: "streaming manager not running"}
		}
		msg := &streaming.Message{
			Type:   streaming.MessageTypeMessage,
			RoomID: in.RoomID,
			UserID: in.UserID,
			Data:   in.Content,
		}
		if err := m.BroadcastToRoom(ctx, in.RoomID, msg); err != nil {
			return CommandResult{}, &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
		}
		return CommandResult{OK: true, Message: "message sent"}, nil
	}
}

func projectRoom(r streaming.Room, members int) RoomInfo {
	return RoomInfo{
		ID:          r.GetID(),
		Name:        r.GetName(),
		Description: r.GetDescription(),
		Owner:       r.GetOwner(),
		Members:     members,
		Private:     r.IsPrivate(),
		Archived:    r.IsArchived(),
		Created:     r.GetCreated(),
		Updated:     r.GetUpdated(),
	}
}

func projectModerationEntries(entries []streaming.ModerationEvent) []ModerationEntry {
	out := make([]ModerationEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, ModerationEntry{
			Timestamp: e.Timestamp,
			Action:    e.Type,
			ActorID:   e.ModeratorID,
			TargetID:  e.TargetID,
			Reason:    e.Reason,
			Metadata:  e.Metadata,
		})
	}
	return out
}
