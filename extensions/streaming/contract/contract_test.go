package contract

import (
	"context"
	"errors"
	"testing"
	"time"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// stubManager is a minimal Manager satisfying the slice (f) contract handler
// callsites. It panics on methods we don't exercise so a missing-coverage gap
// surfaces loudly.
type stubManager struct {
	streaming.Manager // embed to inherit everything; override what we use

	stats          *streaming.ManagerStats
	rooms          []streaming.Room
	channels       []streaming.Channel
	connections    []streaming.EnhancedConnection
	presence       map[string]*streaming.UserPresence
	createErr      error
	deleteErr      error
	sendErr        error
	setPresenceErr error
	kickErr        error
	createdRoom    streaming.Room
	deletedID      string
	setUser        string
	setStatus      string
	kickedConn     string
	kickedReason   string
}

func (s *stubManager) GetStats(_ context.Context) (*streaming.ManagerStats, error) {
	return s.stats, nil
}
func (s *stubManager) GetAllConnections() []streaming.EnhancedConnection {
	return s.connections
}
func (s *stubManager) ListRooms(_ context.Context) ([]streaming.Room, error) {
	return s.rooms, nil
}
func (s *stubManager) GetRoom(_ context.Context, id string) (streaming.Room, error) {
	for _, r := range s.rooms {
		if r.GetID() == id {
			return r, nil
		}
	}
	return nil, streaming.ErrRoomNotFound
}
func (s *stubManager) ListChannels(_ context.Context) ([]streaming.Channel, error) {
	return s.channels, nil
}
func (s *stubManager) GetPresence(_ context.Context, userID string) (*streaming.UserPresence, error) {
	if p, ok := s.presence[userID]; ok {
		return p, nil
	}
	return nil, errors.New("presence not found")
}
func (s *stubManager) CreateRoom(_ context.Context, r streaming.Room) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.createdRoom = r
	s.rooms = append(s.rooms, r)
	return nil
}
func (s *stubManager) DeleteRoom(_ context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deletedID = id
	return nil
}
func (s *stubManager) BroadcastToRoom(_ context.Context, _ string, _ *streaming.Message) error {
	return s.sendErr
}
func (s *stubManager) SetPresence(_ context.Context, userID, status string) error {
	if s.setPresenceErr != nil {
		return s.setPresenceErr
	}
	s.setUser = userID
	s.setStatus = status
	return nil
}
func (s *stubManager) KickConnection(_ context.Context, connID, reason string) error {
	if s.kickErr != nil {
		return s.kickErr
	}
	s.kickedConn = connID
	s.kickedReason = reason
	return nil
}
func (s *stubManager) GetRoomMembers(_ context.Context, _ string) ([]streaming.Member, error) {
	return nil, nil
}
func (s *stubManager) GetModerationLog(_ context.Context, _ string, _ int) ([]streaming.ModerationEvent, error) {
	return nil, nil
}

func newStub() *stubManager {
	return &stubManager{
		stats: &streaming.ManagerStats{
			TotalConnections: 5,
			TotalRooms:       3,
			TotalChannels:    2,
			TotalMessages:    100,
			OnlineUsers:      4,
			MessagesPerSec:   2.5,
			Uptime:           5 * time.Minute,
			MemoryUsage:      1024,
		},
		presence: map[string]*streaming.UserPresence{},
	}
}

func managerOf(s *stubManager) ManagerProvider { return func() streaming.Manager { return s } }

func TestStatsHandler(t *testing.T) {
	s := newStub()
	h := statsHandler(managerOf(s))
	got, err := h(context.Background(), struct{}{}, dashcontract.Principal{})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.TotalConnections != 5 || got.TotalMessages != 100 || got.MessagesPerSec != 2.5 {
		t.Errorf("stats projection wrong: %+v", got)
	}
	if got.UptimeSeconds != 300 {
		t.Errorf("uptime = %d, want 300", got.UptimeSeconds)
	}
}

func TestStatsHandler_NilManager(t *testing.T) {
	h := statsHandler(func() streaming.Manager { return nil })
	_, err := h(context.Background(), struct{}{}, dashcontract.Principal{})
	if err == nil {
		t.Fatal("expected unavailable error")
	}
	if ce, ok := err.(*dashcontract.Error); !ok || ce.Code != dashcontract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

func TestKickConnection_BadRequestOnEmptyID(t *testing.T) {
	s := newStub()
	h := kickConnectionHandler(managerOf(s))
	_, err := h(context.Background(), KickConnectionInput{ConnID: ""}, dashcontract.Principal{})
	if ce, ok := err.(*dashcontract.Error); !ok || ce.Code != dashcontract.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", err)
	}
}

func TestKickConnection_PassesThroughToManager(t *testing.T) {
	s := newStub()
	h := kickConnectionHandler(managerOf(s))
	res, err := h(context.Background(), KickConnectionInput{ConnID: "c_42", Reason: "spam"}, dashcontract.Principal{})
	if err != nil {
		t.Fatalf("kick: %v", err)
	}
	if !res.OK || s.kickedConn != "c_42" || s.kickedReason != "spam" {
		t.Errorf("expected kick to land, got conn=%q reason=%q ok=%v", s.kickedConn, s.kickedReason, res.OK)
	}
}

func TestDeleteRoom_NotFoundMapsToCodeNotFound(t *testing.T) {
	s := newStub()
	s.deleteErr = streaming.ErrRoomNotFound
	h := deleteRoomHandler(managerOf(s))
	_, err := h(context.Background(), DeleteRoomInput{ID: "missing"}, dashcontract.Principal{})
	if ce, ok := err.(*dashcontract.Error); !ok || ce.Code != dashcontract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestSendMessage_RequiresRoomAndContent(t *testing.T) {
	s := newStub()
	h := sendMessageHandler(managerOf(s))
	cases := []SendMessageInput{
		{RoomID: "", Content: "hi"},
		{RoomID: "r1", Content: ""},
	}
	for _, in := range cases {
		_, err := h(context.Background(), in, dashcontract.Principal{})
		if ce, ok := err.(*dashcontract.Error); !ok || ce.Code != dashcontract.CodeBadRequest {
			t.Errorf("expected CodeBadRequest for %+v, got %v", in, err)
		}
	}
}

func TestSetPresence_PassesThrough(t *testing.T) {
	s := newStub()
	h := setPresenceHandler(managerOf(s))
	res, err := h(context.Background(), SetPresenceInput{UserID: "u1", Status: "away"}, dashcontract.Principal{})
	if err != nil {
		t.Fatalf("setPresence: %v", err)
	}
	if !res.OK || s.setUser != "u1" || s.setStatus != "away" {
		t.Errorf("expected presence to land, got user=%q status=%q", s.setUser, s.setStatus)
	}
}

func TestSetPresence_BadRequestOnEmptyFields(t *testing.T) {
	s := newStub()
	h := setPresenceHandler(managerOf(s))
	_, err := h(context.Background(), SetPresenceInput{}, dashcontract.Principal{})
	if ce, ok := err.(*dashcontract.Error); !ok || ce.Code != dashcontract.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", err)
	}
}

func TestConfigHandler_ProjectsFeaturesAndLimits(t *testing.T) {
	cfg := streaming.Config{
		Backend:               "redis",
		EnableDistributed:     true,
		NodeID:                "node-1",
		EnableRooms:           true,
		EnableChannels:        false,
		EnablePresence:        true,
		MaxConnectionsPerUser: 5,
		PingInterval:          25 * time.Second,
	}
	h := configHandler(func() streaming.Config { return cfg })
	got, err := h(context.Background(), struct{}{}, dashcontract.Principal{})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if got.BackendType != "redis" || !got.Distributed || got.NodeID != "node-1" {
		t.Errorf("backend / distributed / nodeID projection wrong: %+v", got)
	}
	if got.Features["rooms"] != true || got.Features["channels"] != false {
		t.Errorf("features projection wrong: %+v", got.Features)
	}
	if got.Limits["maxConnectionsPerUser"].(int) != 5 {
		t.Errorf("limits projection wrong: %+v", got.Limits)
	}
	if got.Timeouts["pingInterval"].(string) != "25s" {
		t.Errorf("timeouts projection wrong: %+v", got.Timeouts)
	}
}

func TestPresenceList_AggregatesOverUniqueUsers(t *testing.T) {
	s := newStub()
	s.presence["alice"] = &streaming.UserPresence{
		UserID:   "alice",
		Status:   "online",
		LastSeen: time.Now(),
	}
	s.connections = []streaming.EnhancedConnection{
		fakeConn{id: "c1", userID: "alice"},
		fakeConn{id: "c2", userID: "alice"}, // duplicate
		fakeConn{id: "c3", userID: ""},      // anonymous, skipped
	}
	h := presenceListHandler(managerOf(s))
	got, err := h(context.Background(), struct{}{}, dashcontract.Principal{})
	if err != nil {
		t.Fatalf("presence: %v", err)
	}
	if len(got.Presence) != 1 || got.Presence[0].UserID != "alice" {
		t.Errorf("expected 1 unique presence entry, got %+v", got.Presence)
	}
}

// --- Lightweight EnhancedConnection stub ------------------------------------

type fakeConn struct {
	streaming.EnhancedConnection // embed to satisfy the interface; override what we use
	id                           string
	userID                       string
}

func (f fakeConn) ID() string                 { return f.id }
func (f fakeConn) GetUserID() string          { return f.userID }
func (f fakeConn) GetTransport() string       { return "websocket" }
func (f fakeConn) GetJoinedRooms() []string   { return nil }
func (f fakeConn) GetSubscriptions() []string { return nil }
func (f fakeConn) GetLastActivity() time.Time { return time.Time{} }
func (f fakeConn) IsClosed() bool             { return false }
