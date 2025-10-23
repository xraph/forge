package filters

import (
	"context"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// mockConnection implements streaming.EnhancedConnection for testing
type mockConnection struct {
	id       string
	userID   string
	rooms    []string
	channels []string
}

func (m *mockConnection) ID() string                         { return m.id }
func (m *mockConnection) GetUserID() string                  { return m.userID }
func (m *mockConnection) SetUserID(userID string)            { m.userID = userID }
func (m *mockConnection) GetSessionID() string               { return "" }
func (m *mockConnection) SetSessionID(sessionID string)      {}
func (m *mockConnection) GetMetadata(key string) (any, bool) { return nil, false }
func (m *mockConnection) SetMetadata(key string, value any)  {}
func (m *mockConnection) GetJoinedRooms() []string           { return m.rooms }
func (m *mockConnection) AddRoom(roomID string)              { m.rooms = append(m.rooms, roomID) }
func (m *mockConnection) RemoveRoom(roomID string)           {}
func (m *mockConnection) IsInRoom(roomID string) bool {
	for _, r := range m.rooms {
		if r == roomID {
			return true
		}
	}
	return false
}
func (m *mockConnection) GetSubscriptions() []string { return m.channels }
func (m *mockConnection) AddSubscription(channelID string) {
	m.channels = append(m.channels, channelID)
}
func (m *mockConnection) RemoveSubscription(channelID string) {}
func (m *mockConnection) IsSubscribed(channelID string) bool {
	for _, c := range m.channels {
		if c == channelID {
			return true
		}
	}
	return false
}
func (m *mockConnection) Read() ([]byte, error)      { return nil, nil }
func (m *mockConnection) ReadJSON(v any) error       { return nil }
func (m *mockConnection) Write(data []byte) error    { return nil }
func (m *mockConnection) WriteJSON(v any) error      { return nil }
func (m *mockConnection) Close() error               { return nil }
func (m *mockConnection) Context() context.Context   { return context.Background() }
func (m *mockConnection) RemoteAddr() string         { return "127.0.0.1" }
func (m *mockConnection) LocalAddr() string          { return "127.0.0.1" }
func (m *mockConnection) GetLastActivity() time.Time { return time.Now() }
func (m *mockConnection) UpdateActivity()            {}
func (m *mockConnection) IsClosed() bool             { return false }
func (m *mockConnection) MarkClosed()                {}

// mockFilter is a simple filter for testing
type mockFilter struct {
	name        string
	priority    int
	shouldBlock bool
}

func (m *mockFilter) Name() string  { return m.name }
func (m *mockFilter) Priority() int { return m.priority }
func (m *mockFilter) Filter(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
	if m.shouldBlock {
		return nil, nil
	}
	return msg, nil
}

func TestFilterChain_Add(t *testing.T) {
	chain := NewFilterChain()

	filter1 := &mockFilter{name: "filter1", priority: 10}
	filter2 := &mockFilter{name: "filter2", priority: 5}

	chain.Add(filter1)
	chain.Add(filter2)

	filters := chain.List()
	if len(filters) != 2 {
		t.Errorf("expected 2 filters, got %d", len(filters))
	}

	// Should be sorted by priority (lower first)
	if filters[0].Name() != "filter2" {
		t.Errorf("expected filter2 first, got %s", filters[0].Name())
	}
}

func TestFilterChain_Remove(t *testing.T) {
	chain := NewFilterChain()

	filter1 := &mockFilter{name: "filter1", priority: 10}
	filter2 := &mockFilter{name: "filter2", priority: 5}

	chain.Add(filter1)
	chain.Add(filter2)
	chain.Remove("filter1")

	filters := chain.List()
	if len(filters) != 1 {
		t.Errorf("expected 1 filter, got %d", len(filters))
	}

	if filters[0].Name() != "filter2" {
		t.Errorf("expected filter2, got %s", filters[0].Name())
	}
}

func TestFilterChain_Apply(t *testing.T) {
	tests := []struct {
		name        string
		filters     []MessageFilter
		shouldBlock bool
	}{
		{
			name: "all filters pass",
			filters: []MessageFilter{
				&mockFilter{name: "filter1", priority: 10, shouldBlock: false},
				&mockFilter{name: "filter2", priority: 20, shouldBlock: false},
			},
			shouldBlock: false,
		},
		{
			name: "one filter blocks",
			filters: []MessageFilter{
				&mockFilter{name: "filter1", priority: 10, shouldBlock: false},
				&mockFilter{name: "filter2", priority: 20, shouldBlock: true},
			},
			shouldBlock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewFilterChain()
			for _, filter := range tt.filters {
				chain.Add(filter)
			}

			msg := &streaming.Message{
				ID:   "msg1",
				Type: "text",
				Data: "test message",
			}

			conn := &mockConnection{id: "conn1", userID: "user1"}
			ctx := context.Background()

			result, err := chain.Apply(ctx, msg, conn)

			if err != nil {
				t.Errorf("Apply() error = %v", err)
				return
			}

			blocked := result == nil
			if blocked != tt.shouldBlock {
				t.Errorf("Apply() blocked = %v, want %v", blocked, tt.shouldBlock)
			}
		})
	}
}

func TestSimpleFilter(t *testing.T) {
	filterFunc := func(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
		// Block messages containing "spam"
		if text, ok := msg.Data.(string); ok {
			if text == "spam" {
				return nil, nil
			}
		}
		return msg, nil
	}

	filter := NewSimpleFilter("spam_filter", 10, filterFunc)

	msg := &streaming.Message{
		ID:   "msg1",
		Type: "text",
		Data: "spam",
	}

	conn := &mockConnection{id: "conn1", userID: "user1"}
	ctx := context.Background()

	result, err := filter.Filter(ctx, msg, conn)
	if err != nil {
		t.Errorf("Filter() error = %v", err)
	}

	if result != nil {
		t.Error("expected message to be blocked")
	}
}

func BenchmarkFilterChain_Apply(b *testing.B) {
	chain := NewFilterChain()
	for i := 0; i < 5; i++ {
		chain.Add(&mockFilter{
			name:        string(rune('a' + i)),
			priority:    i * 10,
			shouldBlock: false,
		})
	}

	msg := &streaming.Message{
		ID:   "msg1",
		Type: "text",
		Data: "test message",
	}

	conn := &mockConnection{id: "conn1", userID: "user1"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Apply(ctx, msg, conn)
	}
}
