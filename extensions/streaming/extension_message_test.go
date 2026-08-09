package streaming

import (
	"context"
	"sync"
	"testing"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// recordingManager records the manager calls handleMessage routes to.
// It embeds Manager so it satisfies the (very large) interface; only the
// methods handleMessage can reach are implemented.
type recordingManager struct {
	Manager

	mu sync.Mutex

	saved           []*Message
	roomBroadcasts  []routedMessage
	chanBroadcasts  []routedMessage
	joins           []routedConn
	leaves          []routedConn
	typingStarts    []routedUser
	typingStops     []routedUser
	presenceUpdates []routedUser
	saveErr         error
	broadcastErr    error
	joinErr         error
	leaveErr        error
	typingErr       error
	presenceErr     error
}

type routedMessage struct {
	target string
	msg    *Message
}

type routedConn struct {
	connID string
	roomID string
}

type routedUser struct {
	userID string
	value  string
}

func (m *recordingManager) SaveMessage(ctx context.Context, message *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.saved = append(m.saved, message)

	return m.saveErr
}

func (m *recordingManager) BroadcastToRoom(ctx context.Context, roomID string, message *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.roomBroadcasts = append(m.roomBroadcasts, routedMessage{target: roomID, msg: message})

	return m.broadcastErr
}

func (m *recordingManager) BroadcastToChannel(ctx context.Context, channelID string, message *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.chanBroadcasts = append(m.chanBroadcasts, routedMessage{target: channelID, msg: message})

	return m.broadcastErr
}

func (m *recordingManager) JoinRoom(ctx context.Context, connID, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.joins = append(m.joins, routedConn{connID: connID, roomID: roomID})

	return m.joinErr
}

func (m *recordingManager) LeaveRoom(ctx context.Context, connID, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.leaves = append(m.leaves, routedConn{connID: connID, roomID: roomID})

	return m.leaveErr
}

func (m *recordingManager) StartTyping(ctx context.Context, userID, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.typingStarts = append(m.typingStarts, routedUser{userID: userID, value: roomID})

	return m.typingErr
}

func (m *recordingManager) StopTyping(ctx context.Context, userID, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.typingStops = append(m.typingStops, routedUser{userID: userID, value: roomID})

	return m.typingErr
}

func (m *recordingManager) SetPresence(ctx context.Context, userID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.presenceUpdates = append(m.presenceUpdates, routedUser{userID: userID, value: status})

	return m.presenceErr
}

// counts snapshots every recorded routing path.
func (m *recordingManager) counts() routingCounts {
	m.mu.Lock()
	defer m.mu.Unlock()

	return routingCounts{
		saved:     len(m.saved),
		room:      len(m.roomBroadcasts),
		channel:   len(m.chanBroadcasts),
		joins:     len(m.joins),
		leaves:    len(m.leaves),
		typingOn:  len(m.typingStarts),
		typingOff: len(m.typingStops),
		presence:  len(m.presenceUpdates),
	}
}

type routingCounts struct {
	saved     int
	room      int
	channel   int
	joins     int
	leaves    int
	typingOn  int
	typingOff int
	presence  int
}

// newTestExtension builds an Extension with just the fields handleMessage uses.
func newTestExtension(cfg Config, mgr Manager) *Extension {
	return &Extension{config: cfg, manager: mgr}
}

func TestExtension_HandleMessageRouting(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*Config)
		msg        *Message
		wantErr    bool
		wantCounts routingCounts
	}{
		{
			name:       "message with room ID is saved and broadcast to the room",
			msg:        &Message{Type: MessageTypeMessage, RoomID: "room-1"},
			wantCounts: routingCounts{saved: 1, room: 1},
		},
		{
			name:       "message with channel ID is broadcast to the channel",
			msg:        &Message{Type: MessageTypeMessage, ChannelID: "chan-1"},
			wantCounts: routingCounts{channel: 1},
		},
		{
			name: "room takes precedence over channel",
			msg:  &Message{Type: MessageTypeMessage, RoomID: "room-1", ChannelID: "chan-1"},
			// The room branch returns before the channel branch is considered.
			wantCounts: routingCounts{saved: 1, room: 1},
		},
		{
			name:       "message with neither room nor channel is dropped",
			msg:        &Message{Type: MessageTypeMessage},
			wantCounts: routingCounts{},
		},
		{
			name:       "history disabled skips the save but still broadcasts",
			configure:  func(c *Config) { c.EnableMessageHistory = false },
			msg:        &Message{Type: MessageTypeMessage, RoomID: "room-1"},
			wantCounts: routingCounts{room: 1},
		},
		{
			name:       "join routes to JoinRoom",
			msg:        &Message{Type: MessageTypeJoin, RoomID: "room-1"},
			wantCounts: routingCounts{joins: 1},
		},
		{
			name:       "join without a room ID is a no-op",
			msg:        &Message{Type: MessageTypeJoin},
			wantCounts: routingCounts{},
		},
		{
			name:       "leave routes to LeaveRoom",
			msg:        &Message{Type: MessageTypeLeave, RoomID: "room-1"},
			wantCounts: routingCounts{leaves: 1},
		},
		{
			name:       "leave without a room ID is a no-op",
			msg:        &Message{Type: MessageTypeLeave},
			wantCounts: routingCounts{},
		},
		{
			name:       "typing true routes to StartTyping",
			msg:        &Message{Type: MessageTypeTyping, RoomID: "room-1", Data: true},
			wantCounts: routingCounts{typingOn: 1},
		},
		{
			name:       "typing false routes to StopTyping",
			msg:        &Message{Type: MessageTypeTyping, RoomID: "room-1", Data: false},
			wantCounts: routingCounts{typingOff: 1},
		},
		{
			name:    "typing with non-bool data is an error",
			msg:     &Message{Type: MessageTypeTyping, RoomID: "room-1", Data: "yes"},
			wantErr: true,
		},
		{
			name:       "typing without a room ID is a no-op",
			msg:        &Message{Type: MessageTypeTyping, Data: true},
			wantCounts: routingCounts{},
		},
		{
			name:       "typing disabled is a no-op",
			configure:  func(c *Config) { c.EnableTypingIndicators = false },
			msg:        &Message{Type: MessageTypeTyping, RoomID: "room-1", Data: true},
			wantCounts: routingCounts{},
		},
		{
			name:       "presence routes to SetPresence",
			msg:        &Message{Type: MessageTypePresence, Data: StatusAway},
			wantCounts: routingCounts{presence: 1},
		},
		{
			name:    "presence with non-string data is an error",
			msg:     &Message{Type: MessageTypePresence, Data: 42},
			wantErr: true,
		},
		{
			name:       "presence disabled is a no-op",
			configure:  func(c *Config) { c.EnablePresence = false },
			msg:        &Message{Type: MessageTypePresence, Data: StatusAway},
			wantCounts: routingCounts{},
		},
		{
			name:       "unknown message type is ignored",
			msg:        &Message{Type: "not-a-real-type", RoomID: "room-1"},
			wantCounts: routingCounts{},
		},
		{
			name:       "empty message type is ignored",
			msg:        &Message{RoomID: "room-1"},
			wantCounts: routingCounts{},
		},
		{
			name:       "system message type is ignored",
			msg:        &Message{Type: streaming.MessageTypeSystem, RoomID: "room-1"},
			wantCounts: routingCounts{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			if tt.configure != nil {
				tt.configure(&cfg)
			}

			mgr := &recordingManager{}
			ext := newTestExtension(cfg, mgr)

			conn := newTestConn("c1", "u1")

			err := ext.handleMessage(context.Background(), conn, tt.msg)

			if tt.wantErr {
				if err == nil {
					t.Fatal("handleMessage: want error, got nil")
				}

				return
			}

			assertNoError(t, err)

			if got := mgr.counts(); got != tt.wantCounts {
				t.Errorf("routing = %+v, want %+v", got, tt.wantCounts)
			}
		})
	}
}

func TestExtension_HandleMessagePropagatesManagerErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*recordingManager)
		msg       *Message
	}{
		{
			name:      "room broadcast failure",
			configure: func(m *recordingManager) { m.broadcastErr = errFake },
			msg:       &Message{Type: MessageTypeMessage, RoomID: "room-1"},
		},
		{
			name:      "channel broadcast failure",
			configure: func(m *recordingManager) { m.broadcastErr = errFake },
			msg:       &Message{Type: MessageTypeMessage, ChannelID: "chan-1"},
		},
		{
			name:      "join failure",
			configure: func(m *recordingManager) { m.joinErr = errFake },
			msg:       &Message{Type: MessageTypeJoin, RoomID: "room-1"},
		},
		{
			name:      "leave failure",
			configure: func(m *recordingManager) { m.leaveErr = errFake },
			msg:       &Message{Type: MessageTypeLeave, RoomID: "room-1"},
		},
		{
			name:      "typing failure",
			configure: func(m *recordingManager) { m.typingErr = errFake },
			msg:       &Message{Type: MessageTypeTyping, RoomID: "room-1", Data: true},
		},
		{
			name:      "presence failure",
			configure: func(m *recordingManager) { m.presenceErr = errFake },
			msg:       &Message{Type: MessageTypePresence, Data: StatusOnline},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &recordingManager{}
			tt.configure(mgr)

			ext := newTestExtension(testConfig(), mgr)

			err := ext.handleMessage(context.Background(), newTestConn("c1", "u1"), tt.msg)
			assertErrorIs(t, err, errFake)
		})
	}
}

func TestExtension_HandleMessageSaveFailureDoesNotBlockBroadcast(t *testing.T) {
	// SaveMessage's error is deliberately discarded so a history outage cannot
	// stop live delivery.
	mgr := &recordingManager{saveErr: errFake}
	ext := newTestExtension(testConfig(), mgr)

	assertNoError(t, ext.handleMessage(
		context.Background(),
		newTestConn("c1", "u1"),
		&Message{Type: MessageTypeMessage, RoomID: "room-1"},
	))

	if got := mgr.counts(); got.room != 1 {
		t.Errorf("room broadcasts = %d, want 1 despite the save failure", got.room)
	}
}

func TestExtension_HandleMessageUsesConnectionUserForTypingAndPresence(t *testing.T) {
	// Typing and presence take the user ID from the connection, not from the
	// message body, so a client cannot act on another user's behalf here.
	mgr := &recordingManager{}
	ext := newTestExtension(testConfig(), mgr)

	conn := newTestConn("c1", "real-user")

	ctx := context.Background()

	assertNoError(t, ext.handleMessage(ctx, conn, &Message{
		Type:   MessageTypeTyping,
		RoomID: "room-1",
		UserID: "spoofed-user",
		Data:   true,
	}))

	assertNoError(t, ext.handleMessage(ctx, conn, &Message{
		Type:   MessageTypePresence,
		UserID: "spoofed-user",
		Data:   StatusAway,
	}))

	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if len(mgr.typingStarts) != 1 || mgr.typingStarts[0].userID != "real-user" {
		t.Errorf("StartTyping user = %v, want real-user", mgr.typingStarts)
	}

	if len(mgr.presenceUpdates) != 1 || mgr.presenceUpdates[0].userID != "real-user" {
		t.Errorf("SetPresence user = %v, want real-user", mgr.presenceUpdates)
	}
}

func TestExtension_HandleMessageOverwritesClientUserID(t *testing.T) {

	mgr := &recordingManager{}
	ext := newTestExtension(testConfig(), mgr)

	conn := newTestConn("c1", "real-user")

	assertNoError(t, ext.handleMessage(context.Background(), conn, &Message{
		Type:   MessageTypeMessage,
		RoomID: "room-1",
		UserID: "spoofed-user",
	}))

	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if got := mgr.roomBroadcasts[0].msg.UserID; got != "real-user" {
		t.Errorf("broadcast UserID = %q, want real-user", got)
	}
}

func TestExtension_HandleMessageJoinUsesConnectionID(t *testing.T) {
	mgr := &recordingManager{}
	ext := newTestExtension(testConfig(), mgr)

	conn := newTestConn("conn-42", "u1")

	ctx := context.Background()

	assertNoError(t, ext.handleMessage(ctx, conn, &Message{Type: MessageTypeJoin, RoomID: "room-1"}))
	assertNoError(t, ext.handleMessage(ctx, conn, &Message{Type: MessageTypeLeave, RoomID: "room-1"}))

	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if len(mgr.joins) != 1 || mgr.joins[0] != (routedConn{connID: "conn-42", roomID: "room-1"}) {
		t.Errorf("joins = %v, want [{conn-42 room-1}]", mgr.joins)
	}

	if len(mgr.leaves) != 1 || mgr.leaves[0] != (routedConn{connID: "conn-42", roomID: "room-1"}) {
		t.Errorf("leaves = %v, want [{conn-42 room-1}]", mgr.leaves)
	}
}
