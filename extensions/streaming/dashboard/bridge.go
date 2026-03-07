package dashboard

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forgeui/bridge"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// RegisterBridge registers all streaming bridge functions on the given bridge instance.
func RegisterBridge(b *bridge.Bridge, manager internal.Manager, config internal.Config) error {
	reg := &bridgeRegistry{
		manager: manager,
		config:  config,
	}

	// Stats
	if err := b.Register("streaming.getStats", reg.getStats,
		bridge.WithDescription("Get overall streaming statistics"),
		bridge.WithFunctionCache(5*time.Second),
	); err != nil {
		return err
	}

	// Rooms
	if err := b.Register("streaming.getRooms", reg.getRooms,
		bridge.WithDescription("List all streaming rooms"),
		bridge.WithFunctionCache(5*time.Second),
	); err != nil {
		return err
	}

	if err := b.Register("streaming.getRoom", reg.getRoom,
		bridge.WithDescription("Get details for a specific room"),
	); err != nil {
		return err
	}

	// Channels
	if err := b.Register("streaming.getChannels", reg.getChannels,
		bridge.WithDescription("List all streaming channels"),
		bridge.WithFunctionCache(5*time.Second),
	); err != nil {
		return err
	}

	// Connections
	if err := b.Register("streaming.getConnections", reg.getConnections,
		bridge.WithDescription("List active connections"),
		bridge.WithFunctionCache(3*time.Second),
	); err != nil {
		return err
	}

	// Presence
	if err := b.Register("streaming.getPresence", reg.getPresence,
		bridge.WithDescription("Get online users presence data"),
		bridge.WithFunctionCache(3*time.Second),
	); err != nil {
		return err
	}

	// Playground actions (no cache)
	if err := b.Register("streaming.createRoom", reg.createRoom,
		bridge.WithDescription("Create a new streaming room"),
	); err != nil {
		return err
	}

	if err := b.Register("streaming.deleteRoom", reg.deleteRoom,
		bridge.WithDescription("Delete a streaming room"),
	); err != nil {
		return err
	}

	if err := b.Register("streaming.sendMessage", reg.sendMessage,
		bridge.WithDescription("Send a test message to a room"),
	); err != nil {
		return err
	}

	// Config
	if err := b.Register("streaming.getConfig", reg.getConfig,
		bridge.WithDescription("Get current streaming configuration"),
		bridge.WithFunctionCache(30*time.Second),
	); err != nil {
		return err
	}

	return nil
}

// bridgeRegistry holds references for bridge function handlers.
type bridgeRegistry struct {
	manager internal.Manager
	config  internal.Config
}

// ---- Parameter types ----

type emptyParams struct{}

type roomIDParams struct {
	RoomID string `json:"room_id"`
}

type createRoomParams struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
	Private     bool   `json:"private"`
}

type sendMessageParams struct {
	RoomID  string `json:"room_id"`
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

// ---- Response types ----

type statsResponse struct {
	TotalConnections int     `json:"total_connections"`
	TotalRooms       int     `json:"total_rooms"`
	TotalChannels    int     `json:"total_channels"`
	TotalMessages    int64   `json:"total_messages"`
	OnlineUsers      int     `json:"online_users"`
	MessagesPerSec   float64 `json:"messages_per_sec"`
	Uptime           string  `json:"uptime"`
}

type roomResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
	MemberCount int    `json:"member_count"`
	Private     bool   `json:"private"`
	Archived    bool   `json:"archived"`
	Created     string `json:"created"`
}

type channelResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	SubscriberCount int    `json:"subscriber_count"`
	MessageCount    int64  `json:"message_count"`
	Created         string `json:"created"`
}

type connectionResponse struct {
	ID            string   `json:"id"`
	UserID        string   `json:"user_id"`
	Rooms         []string `json:"rooms"`
	Subscriptions []string `json:"subscriptions"`
	LastActivity  string   `json:"last_activity"`
	Closed        bool     `json:"closed"`
}

type presenceResponse struct {
	UserID       string `json:"user_id"`
	Status       string `json:"status"`
	CustomStatus string `json:"custom_status"`
	LastSeen     string `json:"last_seen"`
	Connections  int    `json:"connections"`
}

type configResponse struct {
	Backend            string `json:"backend"`
	Distributed        bool   `json:"distributed"`
	Rooms              bool   `json:"rooms"`
	Channels           bool   `json:"channels"`
	Presence           bool   `json:"presence"`
	TypingIndicators   bool   `json:"typing_indicators"`
	MessageHistory     bool   `json:"message_history"`
	SessionResumption  bool   `json:"session_resumption"`
	MaxConnsPerUser    int    `json:"max_connections_per_user"`
	MaxRoomsPerUser    int    `json:"max_rooms_per_user"`
	MaxChannelsPerUser int    `json:"max_channels_per_user"`
	MaxMessageSize     int    `json:"max_message_size"`
}

type actionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
}

// ---- Handler implementations ----

func (r *bridgeRegistry) getStats(ctx bridge.Context, params emptyParams) (*statsResponse, error) {
	stats, err := r.manager.GetStats(context.Background())
	if err != nil {
		return nil, err
	}

	return &statsResponse{
		TotalConnections: stats.TotalConnections,
		TotalRooms:       stats.TotalRooms,
		TotalChannels:    stats.TotalChannels,
		TotalMessages:    stats.TotalMessages,
		OnlineUsers:      stats.OnlineUsers,
		MessagesPerSec:   stats.MessagesPerSec,
		Uptime:           stats.Uptime.String(),
	}, nil
}

func (r *bridgeRegistry) getRooms(ctx bridge.Context, params emptyParams) ([]roomResponse, error) {
	rooms, err := r.manager.ListRooms(context.Background())
	if err != nil {
		return nil, err
	}

	bgCtx := context.Background()
	result := make([]roomResponse, 0, len(rooms))

	for _, room := range rooms {
		memberCount, _ := room.MemberCount(bgCtx)
		result = append(result, roomResponse{
			ID:          room.GetID(),
			Name:        room.GetName(),
			Description: room.GetDescription(),
			Owner:       room.GetOwner(),
			MemberCount: memberCount,
			Private:     room.IsPrivate(),
			Archived:    room.IsArchived(),
			Created:     room.GetCreated().Format(time.RFC3339),
		})
	}

	return result, nil
}

func (r *bridgeRegistry) getRoom(ctx bridge.Context, params roomIDParams) (*roomResponse, error) {
	if params.RoomID == "" {
		return nil, errors.New("room_id is required")
	}

	bgCtx := context.Background()

	room, err := r.manager.GetRoom(bgCtx, params.RoomID)
	if err != nil {
		return nil, err
	}

	memberCount, _ := room.MemberCount(bgCtx)

	return &roomResponse{
		ID:          room.GetID(),
		Name:        room.GetName(),
		Description: room.GetDescription(),
		Owner:       room.GetOwner(),
		MemberCount: memberCount,
		Private:     room.IsPrivate(),
		Archived:    room.IsArchived(),
		Created:     room.GetCreated().Format(time.RFC3339),
	}, nil
}

func (r *bridgeRegistry) getChannels(ctx bridge.Context, params emptyParams) ([]channelResponse, error) {
	channels, err := r.manager.ListChannels(context.Background())
	if err != nil {
		return nil, err
	}

	bgCtx := context.Background()
	result := make([]channelResponse, 0, len(channels))

	for _, ch := range channels {
		subCount, _ := ch.GetSubscriberCount(bgCtx)
		result = append(result, channelResponse{
			ID:              ch.GetID(),
			Name:            ch.GetName(),
			SubscriberCount: subCount,
			MessageCount:    ch.GetMessageCount(),
			Created:         ch.GetCreated().Format(time.RFC3339),
		})
	}

	return result, nil
}

func (r *bridgeRegistry) getConnections(ctx bridge.Context, params emptyParams) ([]connectionResponse, error) {
	conns := r.manager.GetAllConnections()
	result := make([]connectionResponse, 0, len(conns))

	for _, conn := range conns {
		result = append(result, connectionResponse{
			ID:            conn.ID(),
			UserID:        conn.GetUserID(),
			Rooms:         conn.GetJoinedRooms(),
			Subscriptions: conn.GetSubscriptions(),
			LastActivity:  conn.GetLastActivity().Format(time.RFC3339),
			Closed:        conn.IsClosed(),
		})
	}

	return result, nil
}

func (r *bridgeRegistry) getPresence(ctx bridge.Context, params emptyParams) ([]presenceResponse, error) {
	conns := r.manager.GetAllConnections()
	bgCtx := context.Background()

	seen := make(map[string]bool)
	var result []presenceResponse

	for _, conn := range conns {
		uid := conn.GetUserID()
		if uid == "" || seen[uid] {
			continue
		}

		seen[uid] = true

		presence, _ := r.manager.GetPresence(bgCtx, uid)
		resp := presenceResponse{
			UserID: uid,
			Status: "online",
		}

		if presence != nil {
			resp.Status = presence.Status
			resp.CustomStatus = presence.CustomStatus
			resp.LastSeen = presence.LastSeen.Format(time.RFC3339)
			resp.Connections = len(presence.Connections)
		}

		result = append(result, resp)
	}

	return result, nil
}

func (r *bridgeRegistry) createRoom(ctx bridge.Context, params createRoomParams) (*actionResult, error) {
	if params.Name == "" {
		return nil, errors.New("room name is required")
	}

	if params.Owner == "" {
		return nil, errors.New("room owner is required")
	}

	room := local.NewRoom(internal.RoomOptions{
		ID:          uuid.New().String(),
		Name:        params.Name,
		Description: params.Description,
		Owner:       params.Owner,
		Private:     params.Private,
	})

	if err := r.manager.CreateRoom(context.Background(), room); err != nil {
		return nil, err
	}

	return &actionResult{
		Success: true,
		Message: "Room created successfully",
		ID:      room.GetID(),
	}, nil
}

func (r *bridgeRegistry) deleteRoom(ctx bridge.Context, params roomIDParams) (*actionResult, error) {
	if params.RoomID == "" {
		return nil, errors.New("room_id is required")
	}

	if err := r.manager.DeleteRoom(context.Background(), params.RoomID); err != nil {
		return nil, err
	}

	return &actionResult{
		Success: true,
		Message: "Room deleted successfully",
	}, nil
}

func (r *bridgeRegistry) sendMessage(ctx bridge.Context, params sendMessageParams) (*actionResult, error) {
	if params.RoomID == "" {
		return nil, errors.New("room_id is required")
	}

	if params.Message == "" {
		return nil, errors.New("message is required")
	}

	msg := &internal.Message{
		ID:        uuid.New().String(),
		Type:      internal.MessageTypeMessage,
		RoomID:    params.RoomID,
		UserID:    params.UserID,
		Data:      params.Message,
		Timestamp: time.Now(),
	}

	if err := r.manager.BroadcastToRoom(context.Background(), params.RoomID, msg); err != nil {
		return nil, err
	}

	return &actionResult{
		Success: true,
		Message: "Message sent successfully",
		ID:      msg.ID,
	}, nil
}

func (r *bridgeRegistry) getConfig(ctx bridge.Context, params emptyParams) (*configResponse, error) {
	return &configResponse{
		Backend:            r.config.Backend,
		Distributed:        r.config.EnableDistributed,
		Rooms:              r.config.EnableRooms,
		Channels:           r.config.EnableChannels,
		Presence:           r.config.EnablePresence,
		TypingIndicators:   r.config.EnableTypingIndicators,
		MessageHistory:     r.config.EnableMessageHistory,
		SessionResumption:  r.config.EnableSessionResumption,
		MaxConnsPerUser:    r.config.MaxConnectionsPerUser,
		MaxRoomsPerUser:    r.config.MaxRoomsPerUser,
		MaxChannelsPerUser: r.config.MaxChannelsPerUser,
		MaxMessageSize:     r.config.MaxMessageSize,
	}, nil
}
