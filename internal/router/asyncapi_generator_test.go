package router

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/xraph/forge/internal/shared"
)

// Test message types.
type ChatMessage struct {
	Text   string `description:"Message text"    json:"text"`
	RoomID string `description:"Room identifier" json:"room_id"`
}

type ChatEvent struct {
	ID        string    `description:"Event ID"        json:"id"`
	UserID    string    `description:"User ID"         json:"user_id"`
	Text      string    `description:"Message text"    json:"text"`
	RoomID    string    `description:"Room ID"         json:"room_id"`
	Timestamp time.Time `description:"Event timestamp" format:"date-time" json:"timestamp"`
}

type NotificationEvent struct {
	Type    string `description:"Notification type"    enum:"info,warning,error" json:"type"`
	Message string `description:"Notification message" json:"message"`
}

func TestAsyncAPIGenerator_WebSocket(t *testing.T) {
	router := NewRouter()

	// Register WebSocket route with messages
	err := router.WebSocket("/ws/chat/{roomId}", func(ctx Context, conn Connection) error {
		return nil
	},
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithName("chat"),
		WithSummary("Chat WebSocket"),
		WithDescription("Real-time chat communication"),
		WithAsyncAPITags("chat", "messaging"),
	)
	if err != nil {
		t.Fatalf("Failed to register WebSocket route: %v", err)
	}

	// Create AsyncAPI generator
	config := shared.AsyncAPIConfig{
		Title:       "Test API",
		Description: "Test AsyncAPI",
		Version:     "1.0.0",
		Servers: map[string]*shared.AsyncAPIServer{
			"production": {
				Host:     "api.example.com",
				Protocol: "wss",
			},
		},
	}

	generator := newAsyncAPIGenerator(config, router)
	spec := generator.Generate()

	// Verify spec
	if spec.AsyncAPI != "3.0.0" {
		t.Errorf("Expected AsyncAPI version 3.0.0, got %s", spec.AsyncAPI)
	}

	if spec.Info.Title != "Test API" {
		t.Errorf("Expected title 'Test API', got '%s'", spec.Info.Title)
	}

	// Check channels
	if len(spec.Channels) == 0 {
		t.Fatal("Expected at least one channel")
	}

	// Check for chat channel
	var chatChannel *shared.AsyncAPIChannel

	for _, channel := range spec.Channels {
		if channel.Address == "/ws/chat/{roomId}" {
			chatChannel = channel

			break
		}
	}

	if chatChannel == nil {
		t.Fatal("Chat channel not found")
	}

	// Verify channel has messages
	if len(chatChannel.Messages) == 0 {
		t.Error("Expected channel to have messages")
	}

	// Check operations
	if len(spec.Operations) == 0 {
		t.Fatal("Expected at least one operation")
	}

	// Verify send and receive operations exist
	hasSend := false
	hasReceive := false

	for _, op := range spec.Operations {
		if op.Action == "send" {
			hasSend = true
		}

		if op.Action == "receive" {
			hasReceive = true
		}
	}

	if !hasSend {
		t.Error("Expected a send operation")
	}

	if !hasReceive {
		t.Error("Expected a receive operation")
	}
}

func TestAsyncAPIGenerator_SSE(t *testing.T) {
	router := NewRouter()

	// Register SSE route with messages
	err := router.EventStream("/sse/notifications", func(ctx Context, stream Stream) error {
		return nil
	},
		WithSSEMessage("notification", NotificationEvent{}),
		WithName("notifications"),
		WithSummary("Notification Stream"),
		WithDescription("Server-sent notification events"),
		WithAsyncAPITags("notifications"),
	)
	if err != nil {
		t.Fatalf("Failed to register SSE route: %v", err)
	}

	// Create AsyncAPI generator
	config := shared.AsyncAPIConfig{
		Title:   "Notification API",
		Version: "1.0.0",
	}

	generator := newAsyncAPIGenerator(config, router)
	spec := generator.Generate()

	// Verify spec
	if spec.AsyncAPI != "3.0.0" {
		t.Errorf("Expected AsyncAPI version 3.0.0, got %s", spec.AsyncAPI)
	}

	// Check channels
	if len(spec.Channels) == 0 {
		t.Fatal("Expected at least one channel")
	}

	// Verify channel has SSE message
	var notificationChannel *shared.AsyncAPIChannel

	for _, channel := range spec.Channels {
		if channel.Address == "/sse/notifications" {
			notificationChannel = channel

			break
		}
	}

	if notificationChannel == nil {
		t.Fatal("Notification channel not found")
	}

	// Verify HTTP binding for SSE
	if notificationChannel.Bindings == nil || notificationChannel.Bindings.HTTP == nil {
		t.Error("Expected HTTP binding for SSE channel")
	}

	// Check operations - SSE should only have receive
	for _, op := range spec.Operations {
		if op.Action == "send" {
			t.Error("SSE should not have send operation")
		}

		if op.Action != "receive" {
			t.Errorf("Expected receive action, got %s", op.Action)
		}
	}
}

func TestAsyncAPIGenerator_MultipleMessages(t *testing.T) {
	router := NewRouter()

	// Register SSE with multiple message types
	err := router.EventStream("/sse/feed", func(ctx Context, stream Stream) error {
		return nil
	},
		WithSSEMessages(map[string]any{
			"post":    map[string]string{"title": "string"},
			"comment": map[string]string{"text": "string"},
			"like":    map[string]string{"user_id": "string"},
		}),
	)
	if err != nil {
		t.Fatalf("Failed to register SSE route: %v", err)
	}

	config := shared.AsyncAPIConfig{
		Title:   "Feed API",
		Version: "1.0.0",
	}

	generator := newAsyncAPIGenerator(config, router)
	spec := generator.Generate()

	// Find feed channel
	var feedChannel *shared.AsyncAPIChannel

	for _, channel := range spec.Channels {
		if channel.Address == "/sse/feed" {
			feedChannel = channel

			break
		}
	}

	if feedChannel == nil {
		t.Fatal("Feed channel not found")
	}

	// Verify multiple messages
	if len(feedChannel.Messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(feedChannel.Messages))
	}

	expectedMessages := []string{"post", "comment", "like"}
	for _, msgName := range expectedMessages {
		if _, ok := feedChannel.Messages[msgName]; !ok {
			t.Errorf("Expected message '%s' not found", msgName)
		}
	}
}

func TestAsyncAPIGenerator_ChannelParameters(t *testing.T) {
	router := NewRouter()

	err := router.WebSocket("/ws/rooms/{roomId}/users/{userId}", func(ctx Context, conn Connection) error {
		return nil
	},
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
	)
	if err != nil {
		t.Fatalf("Failed to register WebSocket route: %v", err)
	}

	config := shared.AsyncAPIConfig{
		Title:   "Room API",
		Version: "1.0.0",
	}

	generator := newAsyncAPIGenerator(config, router)
	spec := generator.Generate()

	// Find the channel
	var roomChannel *shared.AsyncAPIChannel
	for _, channel := range spec.Channels {
		roomChannel = channel

		break
	}

	if roomChannel == nil {
		t.Fatal("Room channel not found")
	}

	// Verify parameters extracted from path
	if len(roomChannel.Parameters) != 2 {
		t.Errorf("Expected 2 parameters, got %d", len(roomChannel.Parameters))
	}

	if _, ok := roomChannel.Parameters["roomId"]; !ok {
		t.Error("Expected roomId parameter")
	}

	if _, ok := roomChannel.Parameters["userId"]; !ok {
		t.Error("Expected userId parameter")
	}
}

func TestAsyncAPIGenerator_JSONSerialization(t *testing.T) {
	router := NewRouter()

	err := router.WebSocket("/ws/chat", func(ctx Context, conn Connection) error {
		return nil
	},
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithSummary("Chat"),
	)
	if err != nil {
		t.Fatalf("Failed to register WebSocket route: %v", err)
	}

	config := shared.AsyncAPIConfig{
		Title:      "Test API",
		Version:    "1.0.0",
		PrettyJSON: true,
	}

	generator := newAsyncAPIGenerator(config, router)
	spec := generator.Generate()

	// Try to serialize to JSON
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatalf("Failed to serialize spec to JSON: %v", err)
	}

	// Verify it's valid JSON
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to parse generated JSON: %v", err)
	}

	// Verify required fields
	if asyncAPI, ok := decoded["asyncapi"].(string); !ok || asyncAPI != "3.0.0" {
		t.Error("Missing or incorrect asyncapi field")
	}

	if info, ok := decoded["info"].(map[string]any); !ok {
		t.Error("Missing info field")
	} else {
		if title, ok := info["title"].(string); !ok || title != "Test API" {
			t.Error("Missing or incorrect info.title field")
		}
	}

	if channels, ok := decoded["channels"].(map[string]any); !ok || len(channels) == 0 {
		t.Error("Missing or empty channels field")
	}

	if operations, ok := decoded["operations"].(map[string]any); !ok || len(operations) == 0 {
		t.Error("Missing or empty operations field")
	}
}

func TestAsyncAPIGenerator_CustomChannelName(t *testing.T) {
	router := NewRouter()

	err := router.WebSocket("/ws/complex/path/chat", func(ctx Context, conn Connection) error {
		return nil
	},
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithAsyncAPIChannelName("simple-chat"),
	)
	if err != nil {
		t.Fatalf("Failed to register WebSocket route: %v", err)
	}

	config := shared.AsyncAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	generator := newAsyncAPIGenerator(config, router)
	spec := generator.Generate()

	// Verify custom channel name is used
	if _, ok := spec.Channels["simple-chat"]; !ok {
		t.Error("Expected channel with custom name 'simple-chat'")
	}
}

func TestAsyncAPIGenerator_Tags(t *testing.T) {
	router := NewRouter()

	err := router.WebSocket("/ws/chat", func(ctx Context, conn Connection) error {
		return nil
	},
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithAsyncAPITags("chat", "messaging", "real-time"),
	)
	if err != nil {
		t.Fatalf("Failed to register WebSocket route: %v", err)
	}

	config := shared.AsyncAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	generator := newAsyncAPIGenerator(config, router)
	spec := generator.Generate()

	// Find operation and verify tags
	for _, op := range spec.Operations {
		if len(op.Tags) == 0 {
			t.Error("Expected operation to have tags")
		}

		tagNames := make([]string, len(op.Tags))
		for i, tag := range op.Tags {
			tagNames[i] = tag.Name
		}

		expectedTags := []string{"chat", "messaging", "real-time"}
		for _, expected := range expectedTags {
			found := slices.Contains(tagNames, expected)

			if !found {
				t.Errorf("Expected tag '%s' not found", expected)
			}
		}
	}
}

func TestPathToChannelID(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/ws/chat", "ws-chat"},
		{"/ws/chat/{roomId}", "ws-chat-roomid"},
		{"/sse/notifications", "sse-notifications"},
		{"/api/v1/stream/{userId}", "api-v1-stream-userid"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := pathToChannelID(tt.path)
			if result != tt.expected {
				t.Errorf("pathToChannelID(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestExtractChannelParameters(t *testing.T) {
	tests := []struct {
		path               string
		expectedParamCount int
		expectedParams     []string
	}{
		{"/ws/chat", 0, []string{}},
		{"/ws/chat/{roomId}", 1, []string{"roomId"}},
		{"/ws/rooms/{roomId}/users/{userId}", 2, []string{"roomId", "userId"}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			params := extractChannelParameters(tt.path)

			if len(params) != tt.expectedParamCount {
				t.Errorf("Expected %d parameters, got %d", tt.expectedParamCount, len(params))
			}

			for _, expectedParam := range tt.expectedParams {
				if _, ok := params[expectedParam]; !ok {
					t.Errorf("Expected parameter '%s' not found", expectedParam)
				}
			}
		})
	}
}
