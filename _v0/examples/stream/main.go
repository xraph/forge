package main

import (
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/xraph/forge/v0"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/streaming"
)

// =============================================================================
// STREAMING MESSAGE TYPES WITH PROPER DOCUMENTATION
// =============================================================================

// ChatMessage represents a chat message in the WebSocket
type ChatMessage struct {
	forge.StreamMessage
	ID        string    `json:"id" description:"Unique message identifier"`
	UserID    string    `json:"user_id" description:"ID of the user sending the message"`
	Username  string    `json:"username" description:"Username of the sender"`
	Content   string    `json:"content" description:"Message content" validate:"required,max=1000"`
	RoomID    string    `json:"room_id" description:"Chat room identifier"`
	Timestamp time.Time `json:"timestamp" description:"Message timestamp"`
	Type      string    `json:"type" description:"Message type" enum:"text,image,file,system"`
}

// UserPresenceMessage represents user presence updates
type UserPresenceMessage struct {
	UserID   string `json:"user_id" description:"User identifier"`
	Username string `json:"username" description:"Username"`
	Status   string `json:"status" description:"Presence status" enum:"online,away,busy,offline"`
	RoomID   string `json:"room_id" description:"Room identifier"`
	Action   string `json:"action" description:"Presence action" enum:"joined,left,typing_start,typing_stop"`
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
// STREAMING HANDLERS
// =============================================================================

// Chat WebSocket handler with dependency injection
func chatWebSocketHandler(ctx common.Context, conn streaming.Connection) error {
	l := ctx.Logger()
	userID := ctx.UserID()

	if userID == "" {
		return fmt.Errorf("user not authenticated")
	}

	l.Info("Chat WebSocket connection established",
		logger.String("user_id", userID),
		logger.String("connection_id", conn.ID()),
	)

	// Send welcome message
	welcomeMsg := ChatMessage{
		ID:        fmt.Sprintf("welcome-%d", time.Now().UnixNano()),
		Type:      "system",
		Content:   "Welcome to the chat!",
		Timestamp: time.Now(),
		UserID:    "system",
		Username:  "System",
		RoomID:    "general",
	}

	if err := conn.Send(ctx, &forge.StreamMessage{
		Type: "chat_message",
		Data: welcomeMsg,
		ID:   welcomeMsg.ID,
	}); err != nil {
		l.Error("Failed to send welcome message", logger.Error(err))
		return err
	}

	// Handle incoming messages
	for {
		select {
		case <-ctx.Done():
			l.Info("Chat WebSocket connection closed by context")
			return ctx.Err()
		default:
			// In a real implementation, you'd listen for incoming messages
			// and handle them here. For this example, we'll just keep the connection alive.
			time.Sleep(1 * time.Second)

			// Send periodic heartbeat
			heartbeat := &forge.StreamMessage{
				Type:      "heartbeat",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"type":      "heartbeat",
					"timestamp": time.Now(),
					"status":    "connected",
				},
			}

			if err := conn.Send(ctx, heartbeat); err != nil {
				l.Error("Failed to send heartbeat", logger.Error(err))
				return err
			}
		}
	}
}

// Notification SSE handler
func notificationSSEHandler(ctx common.Context, conn streaming.Connection) error {
	l := ctx.Logger()
	userID := ctx.UserID()

	if userID == "" {
		return fmt.Errorf("user not authenticated")
	}

	l.Info("Notification SSE connection established",
		logger.String("user_id", userID),
		logger.String("connection_id", conn.ID()),
	)

	// Send initial notification
	notification := NotificationEvent{
		ID:       fmt.Sprintf("notif-%d", time.Now().UnixNano()),
		Type:     "system",
		Title:    "Connection Established",
		Message:  "You are now connected to real-time notifications",
		UserID:   userID,
		Priority: "normal",
		Read:     false,
	}

	if err := conn.Send(ctx, &forge.StreamMessage{
		Type: "notification",
		Data: notification,
		ID:   notification.ID,
	}); err != nil {
		l.Error("Failed to send initial notification", logger.Error(err))
		return err
	}

	// Send periodic notifications
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Info("Notification SSE connection closed by context")
			return ctx.Err()
		case <-ticker.C:
			periodicNotification := NotificationEvent{
				ID:       fmt.Sprintf("periodic-%d", time.Now().UnixNano()),
				Type:     "update",
				Title:    "Periodic Update",
				Message:  fmt.Sprintf("System update at %s", time.Now().Format(time.RFC3339)),
				UserID:   userID,
				Priority: "low",
				Read:     false,
			}

			if err := conn.Send(ctx, &forge.StreamMessage{
				Type: "notification",
				Data: periodicNotification,
				ID:   periodicNotification.ID,
			}); err != nil {
				l.Error("Failed to send periodic notification", logger.Error(err))
				return err
			}
		}
	}
}

// Live data SSE handler (public, no auth required)
func liveDataSSEHandler(ctx common.Context, conn streaming.Connection) error {
	l := ctx.Logger()

	l.Info("Live data SSE connection established",
		logger.String("connection_id", conn.ID()),
	)

	// Send initial data
	initialData := LiveDataEvent{
		Source:    "demo-source",
		Type:      "metric",
		Data:      map[string]interface{}{"value": 100, "unit": "requests/sec"},
		Timestamp: time.Now(),
		Version:   1,
		Metadata:  map[string]string{"region": "us-east-1", "service": "api"},
	}

	if err := conn.Send(ctx, &forge.StreamMessage{
		Type: "data_update",
		Data: initialData,
		ID:   fmt.Sprintf("data-%d", time.Now().UnixNano()),
	}); err != nil {
		l.Error("Failed to send initial data", logger.Error(err))
		return err
	}

	// Send live data updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	version := int64(1)
	for {
		select {
		case <-ctx.Done():
			l.Info("Live data SSE connection closed by context")
			return ctx.Err()
		case <-ticker.C:
			version++

			// Simulate random data
			value := 80 + (time.Now().UnixNano() % 40) // Random value between 80-120

			dataUpdate := LiveDataEvent{
				Source:    "demo-source",
				Type:      "metric",
				Data:      map[string]interface{}{"value": value, "unit": "requests/sec"},
				Timestamp: time.Now(),
				Version:   version,
				Metadata:  map[string]string{"region": "us-east-1", "service": "api"},
			}

			if err := conn.Send(ctx, &forge.StreamMessage{
				Type: "data_update",
				Data: dataUpdate,
				ID:   fmt.Sprintf("data-%d", time.Now().UnixNano()),
			}); err != nil {
				l.Error("Failed to send data update", logger.Error(err))
				return err
			}
		}
	}
}

// =============================================================================
// MAIN APPLICATION WITH ASYNCAPI DOCUMENTATION
// =============================================================================

func main() {
	// Create logger
	appLogger := logger.NewDevelopmentLogger()

	// Create application with streaming enabled
	app, err := forge.NewApplication("streaming-example", "1.0.0",
		forge.WithDescription("Complete streaming example with AsyncAPI documentation"),
		forge.WithLogger(appLogger),
		forge.WithStopTimeout(30*time.Second),
		forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "Streaming API",
			Description: "REST API with real-time streaming capabilities",
			EnableUI:    true,
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

	// Chat WebSocket with detailed message types
	if err := app.WebSocket("/ws/chat", chatWebSocketHandler,
		common.StreamingHandlerInfo{
			Protocol:          "websocket",
			Summary:           "Real-time chat WebSocket",
			Description:       "WebSocket endpoint for real-time chat communication with message history and presence updates",
			Tags:              []string{"chat", "websocket", "realtime"},
			Authentication:    true,
			ConnectionLimit:   1000,
			HeartbeatInterval: 30 * time.Second,
			MessageTypes: map[string]reflect.Type{
				"chat_message":  reflect.TypeOf(ChatMessage{}),
				"user_presence": reflect.TypeOf(UserPresenceMessage{}),
				"heartbeat":     reflect.TypeOf(map[string]interface{}{}),
			},
		},
	); err != nil {
		log.Fatal("Failed to register chat WebSocket:", err)
	}

	// Notification SSE with event types
	if err := app.EventStream("/sse/notifications", notificationSSEHandler,
		common.StreamingHandlerInfo{
			Protocol:        "sse",
			Summary:         "Real-time notifications",
			Description:     "Server-Sent Events endpoint for delivering real-time notifications to authenticated users",
			Tags:            []string{"notifications", "sse", "realtime"},
			Authentication:  true,
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
	fmt.Println()
	fmt.Println("  📡 Streaming:")
	fmt.Println("    WS   /ws/chat                 - Real-time chat (auth required)")
	fmt.Println("    SSE  /sse/notifications      - Notifications (auth required)")
	fmt.Println("    SSE  /sse/live-data          - Live data (public)")
	fmt.Println()
	fmt.Println("  📖 Documentation:")
	fmt.Println("    GET  /docs                   - OpenAPI UI (Swagger)")
	fmt.Println("    GET  /openapi.json           - OpenAPI specification")
	fmt.Println("    GET  /asyncapi               - AsyncAPI UI")
	fmt.Println("    GET  /asyncapi.json          - AsyncAPI specification")
	fmt.Println()
	fmt.Println("  📈 Monitoring:")
	fmt.Println("    GET  /metrics                - Application metrics")
	fmt.Println("    GET  /streaming/stats        - Streaming statistics")
	fmt.Println()
	fmt.Println("💡 Try these commands:")
	fmt.Println("  curl http://localhost:8080/api/v1/health")
	fmt.Println("  curl http://localhost:8080/asyncapi.json")
	fmt.Println("  curl http://localhost:8080/streaming/stats")
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
