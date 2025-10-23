package streaming

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/backends"
	"github.com/xraph/forge/extensions/streaming/trackers"
)

// Extension implements forge.Extension for streaming functionality.
type Extension struct {
	*forge.BaseExtension
	config  Config
	manager Manager
}

// NewExtension creates a new streaming extension with functional options.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("streaming", "2.0.0", "Real-time streaming with WebSocket/SSE, rooms, channels, presence")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new streaming extension with a complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the streaming extension with the app.
func (e *Extension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config from ConfigManager
	programmaticConfig := e.config
	finalConfig := DefaultConfig()
	if err := e.LoadConfig("streaming", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("streaming: failed to load required config: %w", err)
		}
		e.Logger().Warn("streaming: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}
	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("streaming config validation failed: %w", err)
	}

	// Generate node ID if not set
	if e.config.EnableDistributed && e.config.NodeID == "" {
		e.config.NodeID = uuid.New().String()
	}

	// Create stores based on backend
	backendConfig := backends.BackendConfig{
		Type:        e.config.Backend,
		URLs:        e.config.BackendURLs,
		Username:    e.config.BackendUsername,
		Password:    e.config.BackendPassword,
		NodeID:      e.config.NodeID,
		Prefix:      "streaming",
		TLSEnabled:  e.config.TLSEnabled,
		TLSCertFile: e.config.TLSCertFile,
		TLSKeyFile:  e.config.TLSKeyFile,
		TLSCAFile:   e.config.TLSCAFile,
	}

	roomStore, channelStore, messageStore, presenceStore, typingStore, distributed, err := backends.CreateStores(backendConfig)
	if err != nil {
		return fmt.Errorf("failed to create stores: %w", err)
	}

	// Create trackers
	presenceOpts := DefaultPresenceOptions()
	presenceOpts.OfflineTimeout = e.config.PresenceTimeout
	presenceOpts.CleanupInterval = e.config.PresenceCleanup

	typingOpts := DefaultTypingOptions()
	typingOpts.TypingTimeout = e.config.TypingTimeout
	typingOpts.CleanupInterval = e.config.TypingCleanup
	typingOpts.MaxTypingUsers = e.config.MaxTypingUsersPerRoom

	presenceTracker := trackers.NewPresenceTracker(
		presenceStore,
		presenceOpts,
		e.Logger(),
		e.Metrics(),
	)

	typingTracker := trackers.NewTypingTracker(
		typingStore,
		typingOpts,
		e.Logger(),
		e.Metrics(),
	)

	// Create manager
	e.manager = NewManager(
		e.config,
		roomStore,
		channelStore,
		messageStore,
		presenceTracker,
		typingTracker,
		distributed,
		e.Logger(),
		e.Metrics(),
	)

	// Register manager with DI container
	if err := forge.RegisterSingleton(app.Container(), "streaming", func(c forge.Container) (Manager, error) {
		return e.manager, nil
	}); err != nil {
		return fmt.Errorf("failed to register streaming manager: %w", err)
	}

	// Also register the extension itself
	if err := forge.RegisterSingleton(app.Container(), "streaming:extension", func(c forge.Container) (*Extension, error) {
		return e, nil
	}); err != nil {
		return fmt.Errorf("failed to register streaming extension: %w", err)
	}

	e.Logger().Info("streaming extension registered",
		forge.F("backend", e.config.Backend),
		forge.F("rooms", e.config.EnableRooms),
		forge.F("channels", e.config.EnableChannels),
		forge.F("presence", e.config.EnablePresence),
		forge.F("typing", e.config.EnableTypingIndicators),
		forge.F("history", e.config.EnableMessageHistory),
		forge.F("distributed", e.config.EnableDistributed),
	)

	return nil
}

// Start starts the streaming extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting streaming extension",
		forge.F("backend", e.config.Backend),
	)

	if err := e.manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start streaming manager: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("streaming extension started")

	return nil
}

// Stop stops the streaming extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping streaming extension")

	if e.manager != nil {
		if err := e.manager.Stop(ctx); err != nil {
			e.Logger().Error("failed to stop streaming manager",
				forge.F("error", err),
			)
		}
	}

	e.MarkStopped()
	e.Logger().Info("streaming extension stopped")

	return nil
}

// Health checks if the streaming extension is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.manager == nil {
		return fmt.Errorf("streaming manager not initialized")
	}

	if err := e.manager.Health(ctx); err != nil {
		return fmt.Errorf("streaming health check failed: %w", err)
	}

	return nil
}

// Manager returns the streaming manager (for advanced usage).
func (e *Extension) Manager() Manager {
	return e.manager
}

// RegisterRoutes is a helper to register WebSocket and SSE routes with the router.
func (e *Extension) RegisterRoutes(router forge.Router, wsPath, ssePath string) error {
	// Register WebSocket handler
	if err := router.WebSocket(wsPath, e.handleWebSocket); err != nil {
		return fmt.Errorf("failed to register websocket route: %w", err)
	}

	// Register SSE handler
	if err := router.EventStream(ssePath, e.handleSSE); err != nil {
		return fmt.Errorf("failed to register sse route: %w", err)
	}

	e.Logger().Info("streaming routes registered",
		forge.F("websocket", wsPath),
		forge.F("sse", ssePath),
	)

	return nil
}

// handleWebSocket is the default WebSocket handler.
func (e *Extension) handleWebSocket(ctx forge.Context, conn forge.Connection) error {
	// Get user ID from context (set by auth middleware)
	var userID string
	if uid := ctx.Get("user_id"); uid != nil {
		if uidStr, ok := uid.(string); ok {
			userID = uidStr
		}
	}

	// Create enhanced connection
	enhanced := NewConnection(conn)
	enhanced.SetUserID(userID)
	enhanced.SetSessionID(uuid.New().String())

	// Register connection
	if err := e.manager.Register(enhanced); err != nil {
		e.Logger().Error("failed to register connection",
			forge.F("conn_id", conn.ID()),
			forge.F("error", err),
		)
		return err
	}
	defer e.manager.Unregister(conn.ID())

	// Set user online
	if userID != "" && e.config.EnablePresence {
		_ = e.manager.SetPresence(ctx.Request().Context(), userID, StatusOnline)
		defer e.manager.SetPresence(ctx.Request().Context(), userID, StatusOffline)
	}

	// Message loop
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			// Connection closed or error
			return err
		}

		// Update activity
		enhanced.UpdateActivity()
		if userID != "" && e.config.EnablePresence {
			_ = e.manager.TrackActivity(ctx.Request().Context(), userID)
		}

		// Handle different message types
		if err := e.handleMessage(ctx.Request().Context(), enhanced, &msg); err != nil {
			e.Logger().Error("failed to handle message",
				forge.F("conn_id", conn.ID()),
				forge.F("type", msg.Type),
				forge.F("error", err),
			)
		}
	}
}

// handleSSE is the default SSE handler.
func (e *Extension) handleSSE(ctx forge.Context, stream forge.Stream) error {
	// Simple SSE implementation - send periodic updates
	// In a real implementation, this would subscribe to events

	// Get user ID from context
	var userID string
	if uid := ctx.Get("user_id"); uid != nil {
		if uidStr, ok := uid.(string); ok {
			userID = uidStr
		}
	}

	e.Logger().Debug("SSE connection established",
		forge.F("user_id", userID),
	)

	// Keep connection alive
	<-stream.Context().Done()
	return stream.Context().Err()
}

// handleMessage processes incoming messages.
func (e *Extension) handleMessage(ctx context.Context, conn EnhancedConnection, msg *Message) error {
	switch msg.Type {
	case MessageTypeMessage:
		// Regular message
		if msg.RoomID != "" {
			// Save to history
			if e.config.EnableMessageHistory {
				_ = e.manager.SaveMessage(ctx, msg)
			}
			// Broadcast to room
			return e.manager.BroadcastToRoom(ctx, msg.RoomID, msg)
		} else if msg.ChannelID != "" {
			// Broadcast to channel
			return e.manager.BroadcastToChannel(ctx, msg.ChannelID, msg)
		}

	case MessageTypeJoin:
		// Join room
		if msg.RoomID != "" {
			return e.manager.JoinRoom(ctx, conn.ID(), msg.RoomID)
		}

	case MessageTypeLeave:
		// Leave room
		if msg.RoomID != "" {
			return e.manager.LeaveRoom(ctx, conn.ID(), msg.RoomID)
		}

	case MessageTypeTyping:
		// Typing indicator
		if msg.RoomID != "" && e.config.EnableTypingIndicators {
			isTyping, ok := msg.Data.(bool)
			if !ok {
				return fmt.Errorf("invalid typing data")
			}

			userID := conn.GetUserID()
			if isTyping {
				return e.manager.StartTyping(ctx, userID, msg.RoomID)
			} else {
				return e.manager.StopTyping(ctx, userID, msg.RoomID)
			}
		}

	case MessageTypePresence:
		// Presence update
		if e.config.EnablePresence {
			status, ok := msg.Data.(string)
			if !ok {
				return fmt.Errorf("invalid presence data")
			}

			userID := conn.GetUserID()
			return e.manager.SetPresence(ctx, userID, status)
		}
	}

	return nil
}
