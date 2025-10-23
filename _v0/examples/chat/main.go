package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/rs/xid"
	"github.com/xraph/forge/v0"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/streaming"
)

// Global streaming manager instance
var globalStreamingManager streaming.StreamingManager

// =============================================================================
// MIDDLEWARE FOR AUTHENTICATION AND CORS
// =============================================================================

// Simple CORS middleware
func corsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow all origins for development - be more restrictive in production
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Mock authentication middleware that adds a user ID for testing
func mockAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// For development, we'll accept any connection and assign a test user ID
			// In production, you'd validate JWT tokens or API keys here

			// Check for authorization header or query param
			userID := "anonymous"
			username := "Anonymous"

			if auth := r.Header.Get("Authorization"); auth != "" {
				// Extract user ID from token (mocked)
				if strings.HasPrefix(auth, "Bearer ") {
					userID = "user-" + auth[7:] // Use part of token as user ID
				}
			} else if queryUserID := r.URL.Query().Get("user_id"); queryUserID != "" {
				userID = queryUserID
				if queryUsername := r.URL.Query().Get("username"); queryUsername != "" {
					username = queryUsername
				} else {
					// Generate a better default username based on user ID
					username = generateUsernameFromID(userID)
				}
			} else {
				// For WebSocket testing, assign a default user ID
				userID = "test-user-123"
				username = "Test User"
			}

			// Add user information to request context
			ctx := r.Context()
			ctx = context.WithValue(ctx, "user_id", userID)
			ctx = context.WithValue(ctx, "username", username)

			// Store user metadata for connection tracking
			userMetadata := map[string]interface{}{
				"username":  username,
				"avatar":    getUserAvatar(userID),
				"joined_at": time.Now(),
			}
			ctx = context.WithValue(ctx, "user_metadata", userMetadata)

			r = r.WithContext(ctx)

			fmt.Printf("Auth middleware: User ID: %s, Username: %s\n", userID, username)
			next.ServeHTTP(w, r)
		})
	}
}

func getUserAvatar(userID string) string {
	// Simple avatar mapping - in production, this could be from a database
	avatarMap := map[string]string{
		"alice":         "👩‍💼",
		"bob":           "👨‍💻",
		"test-user-123": "👤",
	}

	if avatar, exists := avatarMap[userID]; exists {
		return avatar
	}

	// Custom users get a default avatar
	if strings.HasPrefix(userID, "custom_") {
		return "👤"
	}

	// Default avatar for unknown users
	return "👤"
}

// Updated helper function to get user metadata from context
func getUserMetadataFromContext(ctx context.Context) map[string]interface{} {
	if metadata, ok := ctx.Value("user_metadata").(map[string]interface{}); ok {
		return metadata
	}
	return make(map[string]interface{})
}

// Helper function to get user ID from context
func getUserIDFromContext(ctx context.Context) string {
	if userID, ok := ctx.Value("user_id").(string); ok {
		return userID
	}
	return "anonymous"
}

// Helper function to get username from context
func getUsernameFromContext(ctx context.Context) string {
	if username, ok := ctx.Value("username").(string); ok {
		return username
	}
	return "Anonymous"
}

func generateUsernameFromID(userID string) string {
	switch userID {
	case "alice":
		return "Alice"
	case "bob":
		return "Bob"
	case "test-user-123":
		return "Test User"
	default:
		if strings.HasPrefix(userID, "custom_") {
			// Extract name from custom user ID
			parts := strings.Split(userID, "_")
			if len(parts) > 1 {
				return strings.Title(strings.Join(parts[1:], " "))
			}
		}
		return strings.Title(strings.ReplaceAll(userID, "_", " "))
	}
}

// Helper function to send current user list
func sendCurrentUserList(ctx context.Context, conn streaming.Connection, room streaming.Room) error {
	// Get all connections in the room
	connections := room.GetConnections()

	// Build user list
	var users []map[string]interface{}
	seenUsers := make(map[string]bool)

	for _, roomConn := range connections {
		userID := roomConn.UserID()
		if userID != "" && !seenUsers[userID] {
			seenUsers[userID] = true

			// Extract username from connection metadata or context
			username := getUsernameForConnection(roomConn)
			avatar := getUserAvatar(userID)

			users = append(users, map[string]interface{}{
				"user_id":  userID,
				"username": username,
				"avatar":   avatar,
				"status":   "online",
			})
		}
	}

	// Send current users message
	currentUsersData := map[string]interface{}{
		"type":      "current_users",
		"users":     users,
		"count":     len(users),
		"timestamp": time.Now(),
	}

	currentUsersMsg := &streaming.Message{
		ID:        xid.New().String(),
		Type:      streaming.MessageTypeEvent,
		Data:      currentUsersData,
		Timestamp: time.Now(),
		RoomID:    room.ID(),
		From:      "system",
	}

	return conn.Send(ctx, currentUsersMsg)
}

func getUsernameForConnection(conn streaming.Connection) string {
	// Try to get username from connection metadata
	if metadata := conn.Metadata(); metadata != nil {
		if username, ok := metadata["username"].(string); ok && username != "" {
			return username
		}
	}

	// Fallback: generate username from user ID
	userID := conn.UserID()
	return generateUsernameFromID(userID)
}

// =============================================================================
// STREAMING MESSAGE TYPES
// =============================================================================

// ChatMessage represents a chat message in the WebSocket
type ChatMessage struct {
	ID        string    `json:"id" description:"Unique message identifier"`
	UserID    string    `json:"user_id" description:"ID of the user sending the message"`
	Username  string    `json:"username" description:"Username of the sender"`
	Content   string    `json:"content" description:"Message content" validate:"required,max=1000"`
	RoomID    string    `json:"room_id" description:"Chat room identifier"`
	Timestamp time.Time `json:"timestamp" description:"Message timestamp"`
	Type      string    `json:"type" description:"Message type" enum:"text,image,file,system"`
}

// NotificationEvent represents a real-time notification
type NotificationEvent struct {
	ID       string                 `json:"id" description:"Notification unique identifier"`
	Type     string                 `json:"type" description:"Notification type" enum:"message,alert,system,update"`
	Title    string                 `json:"title" description:"Notification title"`
	Message  string                 `json:"message" description:"Notification message content"`
	UserID   string                 `json:"user_id" description:"Target user ID"`
	Data     map[string]interface{} `json:"data,omitempty" description:"Additional notification data"`
	Priority string                 `json:"priority" description:"Notification priority" enum:"low,normal,high,urgent"`
	Read     bool                   `json:"read" description:"Read status"`
}

// LiveDataEvent represents real-time data updates
type LiveDataEvent struct {
	Source    string            `json:"source" description:"Data source identifier"`
	Type      string            `json:"type" description:"Data type" enum:"stock_price,weather,sensor,metric"`
	Data      interface{}       `json:"data" description:"Live data payload"`
	Timestamp time.Time         `json:"timestamp" description:"Data timestamp"`
	Version   int64             `json:"version" description:"Data version number"`
	Metadata  map[string]string `json:"metadata,omitempty" description:"Additional metadata"`
}

// =============================================================================
// ROOM MANAGEMENT
// =============================================================================

// ensureRoomExists ensures a room exists, creating it if necessary
func ensureRoomExists(ctx context.Context, roomID string) (streaming.Room, error) {
	// Try to get existing room
	room, err := globalStreamingManager.GetRoom(roomID)
	if err == nil {
		return room, nil
	}

	// Room doesn't exist, create it
	roomConfig := streaming.DefaultRoomConfig()
	roomConfig.Type = streaming.RoomTypePublic
	roomConfig.MaxConnections = 100
	roomConfig.EnablePresence = true
	roomConfig.PersistMessages = true
	roomConfig.MaxMessageHistory = 100

	room, err = globalStreamingManager.CreateRoom(ctx, roomID, roomConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create room: %w", err)
	}

	return room, nil
}

// =============================================================================
// STREAMING HANDLERS (FIXED)
// =============================================================================

// Chat WebSocket handler - FIXED to properly handle rooms and connections
func chatWebSocketHandler(ctx common.Context, conn streaming.Connection) error {
	// Get user information from context
	userID := getUserIDFromContext(ctx)
	username := getUsernameFromContext(ctx)
	userMetadata := getUserMetadataFromContext(ctx)
	roomID := "room-1"

	fmt.Printf("Chat WebSocket connection established: userID=%s, username=%s, connID=%s\n",
		userID, username, conn.ID())

	// Ensure the room exists
	room, err := ensureRoomExists(ctx, roomID)
	if err != nil {
		fmt.Printf("Failed to ensure room exists: %v\n", err)
		return err
	}

	// Set connection properties
	if err := conn.SetRoomID(roomID); err != nil {
		fmt.Printf("Failed to set room ID on connection: %v\n", err)
		return err
	}

	if err := conn.SetUserID(userID); err != nil {
		fmt.Printf("Failed to set user ID on connection: %v\n", err)
		return err
	}

	// UPDATED: Set connection metadata with user information
	conn.SetMetadata("username", username)
	conn.SetMetadata("avatar", getUserAvatar(userID))
	conn.SetMetadata("joined_at", time.Now())

	// Set additional metadata from context
	for key, value := range userMetadata {
		conn.SetMetadata(key, value)
	}

	// Add connection to room
	if err := room.AddConnection(conn); err != nil {
		fmt.Printf("Failed to add connection to room: %v\n", err)
		return err
	}

	// Send welcome message
	welcomeData := map[string]interface{}{
		"type":      "welcome",
		"message":   fmt.Sprintf("Welcome to the chat, %s!", username),
		"user_id":   userID,
		"username":  username,
		"timestamp": time.Now(),
	}

	welcomeMsg := &streaming.Message{
		ID:        xid.New().String(),
		Type:      streaming.MessageTypeEvent,
		Data:      welcomeData,
		Timestamp: time.Now(),
		RoomID:    roomID,
		From:      "system",
	}

	if err := conn.SendEvent(ctx, welcomeMsg); err != nil {
		fmt.Printf("Failed to send welcome message: %v\n", err)
	}

	// Send current user list to the newly connected user
	if err := sendCurrentUserList(ctx, conn, room); err != nil {
		fmt.Printf("Failed to send current user list: %v\n", err)
	}

	// Broadcast user join message to room (excluding the new user)
	userJoinData := map[string]interface{}{
		"type":      "user_joined",
		"user_id":   userID,
		"username":  username,
		"avatar":    getUserAvatar(userID),
		"timestamp": time.Now(),
	}

	joinMessage := streaming.NewSystemMessage("user_joined", fmt.Sprintf("%s joined the chat", username), userJoinData)
	joinMessage.RoomID = roomID

	// Broadcast to everyone except the joining user
	if err := room.BroadcastExcept(ctx, conn.ID(), joinMessage); err != nil {
		fmt.Printf("Failed to broadcast user join message: %v\n", err)
	}

	// Set up message handler for this connection
	conn.OnMessage(func(c streaming.Connection, message *streaming.Message) error {
		return handleChatMessage(ctx, room, c, message)
	})

	// Wait for context cancellation (connection close)
	<-ctx.Done()

	// Remove connection from room on disconnect
	if err := room.RemoveConnection(conn.ID()); err != nil {
		fmt.Printf("Failed to remove connection from room: %v\n", err)
	}

	// Broadcast user leave message
	userLeaveData := map[string]interface{}{
		"type":      "user_left",
		"user_id":   userID,
		"username":  username,
		"avatar":    getUserAvatar(userID),
		"timestamp": time.Now(),
	}

	leaveMessage := streaming.NewSystemMessage("user_left", fmt.Sprintf("%s left the chat", username), userLeaveData)
	leaveMessage.RoomID = roomID
	if err := room.Broadcast(ctx, leaveMessage); err != nil {
		fmt.Printf("Failed to broadcast user leave message: %v\n", err)
	}

	fmt.Printf("Chat WebSocket connection closed: userID=%s\n", userID)
	return ctx.Err()
}

// Handle individual chat messages
func handleChatMessage(ctx context.Context, room streaming.Room, conn streaming.Connection, message *streaming.Message) error {
	userID := getUserIDFromContext(ctx)
	username := getUsernameFromContext(ctx)

	fmt.Printf("Handling chat message from %s: %+v\n", userID, message)

	if message.Type == streaming.MessageTypeTyping {
		data, ok := message.Data.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid message data: %v", message.Data)
		}

		chatMessage := &streaming.Message{
			ID:        xid.New().String(),
			Type:      streaming.MessageTypeEvent,
			Data:      data,
			Timestamp: time.Now(),
			RoomID:    room.ID(),
			From:      userID,
		}

		// Broadcast to everyone except sender
		return room.BroadcastExcept(ctx, conn.ID(), chatMessage)
	} else if data, ok := message.Data.(map[string]interface{}); ok {
		msgType, exists := data["type"]
		if exists && msgType == "chat_message" {
			// This is a chat message, broadcast it to the room
			chatData := map[string]interface{}{
				"type":      "chat_message",
				"user_id":   userID,
				"username":  username,
				"content":   data["content"],
				"timestamp": time.Now(),
			}

			chatMessage := &streaming.Message{
				ID:        xid.New().String(),
				Type:      streaming.MessageTypeEvent,
				Data:      chatData,
				Timestamp: time.Now(),
				RoomID:    room.ID(),
				From:      userID,
			}

			// Broadcast to everyone except sender
			return room.BroadcastExcept(ctx, conn.ID(), chatMessage)
		}
	}

	return nil
}

// Notification SSE handler - FIXED to not fail on authentication
func notificationSSEHandler(ctx common.Context, conn streaming.Connection) error {
	userID := getUserIDFromContext(ctx)
	username := getUsernameFromContext(ctx)

	fmt.Printf("Notification SSE connection established: userID=%s, connID=%s\n",
		userID, conn.ID())

	// Send initial notification
	notificationData := map[string]interface{}{
		"type":      "notification",
		"title":     "Connection Established",
		"message":   fmt.Sprintf("Welcome %s! You are now connected to real-time notifications", username),
		"user_id":   userID,
		"timestamp": time.Now(),
	}

	initialMsg := &streaming.Message{
		ID:        xid.New().String(),
		Type:      streaming.MessageTypeEvent,
		Data:      notificationData,
		Timestamp: time.Now(),
	}

	if err := conn.Send(ctx, initialMsg); err != nil {
		fmt.Printf("Failed to send initial notification: %v\n", err)
		return err
	}

	// Send periodic notifications
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("Notification SSE connection closed: userID=%s\n", userID)
			return ctx.Err()
		case <-ticker.C:
			periodicData := map[string]interface{}{
				"type":      "update",
				"title":     "Periodic Update",
				"message":   fmt.Sprintf("System update at %s", time.Now().Format(time.RFC3339)),
				"user_id":   userID,
				"timestamp": time.Now(),
			}

			periodicMsg := &streaming.Message{
				ID:        xid.New().String(),
				Type:      streaming.MessageTypeEvent,
				Data:      periodicData,
				Timestamp: time.Now(),
			}

			if err := conn.Send(ctx, periodicMsg); err != nil {
				fmt.Printf("Failed to send periodic notification: %v\n", err)
				return err
			}
		}
	}
}

// Live data SSE handler (public, no auth required)
func liveDataSSEHandler(ctx common.Context, conn streaming.Connection) error {
	fmt.Printf("Live data SSE connection established: connID=%s\n", conn.ID())

	// Send initial data
	initialData := map[string]interface{}{
		"type":      "data_update",
		"source":    "demo-source",
		"value":     100,
		"unit":      "requests/sec",
		"timestamp": time.Now(),
		"version":   1,
	}

	initialMsg := &streaming.Message{
		ID:        xid.New().String(),
		Type:      streaming.MessageTypeEvent,
		Data:      initialData,
		Timestamp: time.Now(),
	}

	if err := conn.Send(ctx, initialMsg); err != nil {
		fmt.Printf("Failed to send initial data: %v\n", err)
		return err
	}

	// Send live data updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	version := int64(1)
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("Live data SSE connection closed\n")
			return ctx.Err()
		case <-ticker.C:
			version++

			// Simulate random data
			value := 80 + (time.Now().UnixNano() % 40) // Random value between 80-120

			dataUpdate := map[string]interface{}{
				"type":      "data_update",
				"source":    "demo-source",
				"value":     value,
				"unit":      "requests/sec",
				"timestamp": time.Now(),
				"version":   version,
			}

			updateMsg := &streaming.Message{
				ID:        xid.New().String(),
				Type:      streaming.MessageTypeEvent,
				Data:      dataUpdate,
				Timestamp: time.Now(),
			}

			if err := conn.Send(ctx, updateMsg); err != nil {
				fmt.Printf("Failed to send data update: %v\n", err)
				return err
			}
		}
	}
}

// =============================================================================
// BASIC API ENDPOINTS
// =============================================================================

// Streaming stats endpoint
func streamingStatsHandler(w http.ResponseWriter, r *http.Request) {
	if globalStreamingManager != nil {
		stats := globalStreamingManager.GetStats()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, `{
			"total_connections": %d,
			"active_connections": %d,
			"total_rooms": %d,
			"active_rooms": %d,
			"total_messages": %d,
			"messages_per_second": %.2f,
			"bytes_transferred": %d,
			"uptime": "%s",
			"status": "running",
			"timestamp": "%s"
		}`,
			stats.TotalConnections,
			stats.ActiveConnections,
			stats.TotalRooms,
			stats.ActiveRooms,
			stats.TotalMessages,
			stats.MessagesPerSecond,
			stats.BytesTransferred,
			stats.Uptime.String(),
			time.Now().Format(time.RFC3339))
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"status": "initializing",
			"timestamp": "%s"
		}`, time.Now().Format(time.RFC3339))
	}
}

// =============================================================================
// MAIN APPLICATION (FIXED)
// =============================================================================

func main() {
	// Create streaming configuration with all protocols enabled
	streamingConfig := streaming.DefaultStreamingConfig()
	streamingConfig.EnableWebSocket = true
	streamingConfig.EnableSSE = true
	streamingConfig.EnableLongPolling = true
	streamingConfig.MaxConnections = 1000
	streamingConfig.ConnectionTimeout = 60 * time.Second

	// Let the framework handle heartbeats - reduce frequency to avoid conflicts
	streamingConfig.HeartbeatInterval = 45 * time.Second

	// Add connection stability settings
	streamingConfig.MaxMessageSize = 32768 // 32KB
	streamingConfig.MessageQueueSize = 100

	// Create application with proper configuration INCLUDING STREAMING
	app, err := forge.NewApplication("streaming-example", "1.0.0",
		forge.WithDescription("Complete streaming example with AsyncAPI documentation"),
		forge.WithStopTimeout(30*time.Second),

		// ADD EXPLICIT STREAMING CONFIGURATION
		forge.WithFullStreamingSupport(streamingConfig),

		forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "Streaming API",
			Description: "REST API with real-time streaming capabilities",
			EnableUI:    true,
			AutoUpdate:  true,
		}),
		forge.WithAsyncAPI(forge.AsyncAPIConfig{
			Title:       "Real-time Streaming API",
			Version:     "1.0.0",
			Description: "AsyncAPI specification for WebSocket and SSE endpoints with comprehensive message documentation",
			SpecPath:    "/asyncapi.json",
			UIPath:      "/asyncapi",
			EnableUI:    true,
		}),
	)
	if err != nil {
		log.Fatal("Failed to create application:", err)
	}

	globalStreamingManager = app.StreamingManager()
	fmt.Println("✅ Got streaming manager from application", globalStreamingManager)

	// Pre-create the main chat room
	ctx := context.Background()
	_, err = ensureRoomExists(ctx, "room-1")
	if err != nil {
		fmt.Printf("⚠️  Failed to pre-create chat room: %v\n", err)
	} else {
		fmt.Println("✅ Pre-created main chat room")
	}

	// Add CORS middleware
	app.UseMiddleware(corsMiddleware())

	// Add mock authentication middleware
	app.UseMiddleware(mockAuthMiddleware())

	// Register streaming stats endpoint
	app.UseMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/streaming/stats" && r.Method == "GET" {
				streamingStatsHandler(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Chat WebSocket with PROPER streaming configuration
	if err := app.WebSocket("/ws/chat", chatWebSocketHandler,
		common.StreamingHandlerInfo{
			Protocol:          "websocket",
			Summary:           "Real-time chat WebSocket",
			Description:       "WebSocket endpoint for real-time chat communication with message history and presence updates",
			Tags:              []string{"chat", "websocket", "realtime"},
			Authentication:    false, // Disabled for easier testing
			ConnectionLimit:   1000,
			HeartbeatInterval: 30 * time.Second,
			MessageTypes: map[string]reflect.Type{
				"chat_message": reflect.TypeOf(ChatMessage{}),
				"welcome":      reflect.TypeOf(map[string]interface{}{}),
				"heartbeat":    reflect.TypeOf(map[string]interface{}{}),
			},
		},
	); err != nil {
		log.Fatal("Failed to register chat WebSocket:", err)
	}

	// Notification SSE with proper configuration
	if err := app.EventStream("/sse/notifications", notificationSSEHandler,
		common.StreamingHandlerInfo{
			Protocol:        "sse",
			Summary:         "Real-time notifications",
			Description:     "Server-Sent Events endpoint for delivering real-time notifications to authenticated users",
			Tags:            []string{"notifications", "sse", "realtime"},
			Authentication:  false, // Disabled for easier testing
			ConnectionLimit: 500,
			EventTypes: map[string]reflect.Type{
				"notification": reflect.TypeOf(NotificationEvent{}),
			},
		},
	); err != nil {
		log.Fatal("Failed to register notification SSE:", err)
	}

	// Public live data SSE
	if err := app.EventStream("/sse/live-data", liveDataSSEHandler,
		common.StreamingHandlerInfo{
			Protocol:        "sse",
			Summary:         "Live data streaming",
			Description:     "Public Server-Sent Events endpoint for streaming live data updates (no authentication required)",
			Tags:            []string{"data", "streaming", "sse", "public"},
			Authentication:  false,
			ConnectionLimit: 200,
			EventTypes: map[string]reflect.Type{
				"data_update": reflect.TypeOf(LiveDataEvent{}),
				"heartbeat":   reflect.TypeOf(map[string]interface{}{}),
			},
		},
	); err != nil {
		log.Fatal("Failed to register live data SSE:", err)
	}

	// Start HTTP server
	go func() {
		if err := app.StartServer(":8080"); err != nil {
			log.Printf("Failed to start HTTP server: %v", err)
		}
	}()

	// Print information about available endpoints
	fmt.Println("🚀 Streaming Example Application Started!")
	fmt.Println()
	fmt.Println("📊 Available Endpoints:")
	fmt.Println("  REST API:")
	fmt.Println("    GET  /api/v1/health          - Health check")
	fmt.Println("    GET  /streaming/stats        - Streaming statistics")
	fmt.Println()
	fmt.Println("  📡 Streaming:")
	fmt.Println("    WS   /ws/chat                 - Real-time chat (no auth required for testing)")
	fmt.Println("    SSE  /sse/notifications      - Notifications (no auth required for testing)")
	fmt.Println("    SSE  /sse/live-data          - Live data (public)")
	fmt.Println()
	fmt.Println("  📖 Documentation:")
	fmt.Println("    GET  /docs                   - OpenAPI UI (Swagger)")
	fmt.Println("    GET  /openapi.json           - OpenAPI specification")
	fmt.Println("    GET  /asyncapi               - AsyncAPI UI")
	fmt.Println("    GET  /asyncapi.json          - AsyncAPI specification")
	fmt.Println()
	fmt.Println("💡 Test WebSocket Connection:")
	fmt.Println("  JavaScript in browser console:")
	fmt.Println("    const ws = new WebSocket('ws://localhost:8080/ws/chat?user_id=alice&username=Alice');")
	fmt.Println("    ws.onopen = () => console.log('Connected!');")
	fmt.Println("    ws.onmessage = (e) => console.log('Message:', e.data);")
	fmt.Println("    ws.onerror = (e) => console.error('Error:', e);")
	fmt.Println()
	fmt.Println("  Or use the test HTML file provided")
	fmt.Println()
	fmt.Println("🌐 Open in browser:")
	fmt.Println("  http://localhost:8080/docs      - REST API docs")
	fmt.Println("  http://localhost:8080/asyncapi  - Streaming API docs")
	fmt.Println()

	// Run the application (blocks until shutdown)
	if err := app.Run(); err != nil {
		log.Fatal("Application failed:", err)
	}
}
