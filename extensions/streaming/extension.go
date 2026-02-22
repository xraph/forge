package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/streaming/backends"
	redisbackend "github.com/xraph/forge/extensions/streaming/backends/redis"
	"github.com/xraph/forge/extensions/streaming/coordinator"
	"github.com/xraph/forge/extensions/streaming/filters"
	"github.com/xraph/forge/extensions/streaming/lb"
	"github.com/xraph/forge/extensions/streaming/ratelimit"
	"github.com/xraph/forge/extensions/streaming/trackers"
	"github.com/xraph/forge/extensions/streaming/validation"
	"github.com/xraph/vessel"
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

	// Build manager options for message pipeline
	var managerOpts []ManagerOption

	// Create filter chain (always available, starts empty — users add filters via Manager)
	filterChain := filters.NewFilterChain()
	managerOpts = append(managerOpts, WithFilterChain(filterChain))

	// Create composite validator (always available, starts empty)
	validator := validation.NewCompositeValidator()
	managerOpts = append(managerOpts, WithValidator(validator))

	// Create rate limiter using config values
	rlConfig := ratelimit.DefaultRateLimitConfig()
	rlConfig.MessagesPerSecond = e.config.MaxMessagesPerSecond
	rlConfig.ConnectionsPerUser = e.config.MaxConnectionsPerUser
	rateLimiter := ratelimit.NewTokenBucket(rlConfig, nil) // in-memory mode
	managerOpts = append(managerOpts, WithRateLimiter(rateLimiter))

	// Create distributed coordinator if enabled
	if e.config.EnableDistributed && e.config.Backend == "redis" && len(e.config.BackendURLs) > 0 {
		redisClient, redisErr := redisbackend.NewClient(redisbackend.ClientConfig{
			URLs:        e.config.BackendURLs,
			Username:    e.config.BackendUsername,
			Password:    e.config.BackendPassword,
			TLSEnabled:  e.config.TLSEnabled,
			TLSCertFile: e.config.TLSCertFile,
			TLSKeyFile:  e.config.TLSKeyFile,
			TLSCAFile:   e.config.TLSCAFile,
			Prefix:      "streaming",
		})
		if redisErr != nil {
			e.Logger().Warn("streaming: failed to create coordinator redis client, distributed messaging disabled",
				forge.F("error", redisErr.Error()),
			)
		} else {
			coord := coordinator.NewRedisCoordinator(redisClient, e.config.NodeID)
			managerOpts = append(managerOpts, WithCoordinator(coord))
		}
		managerOpts = append(managerOpts, WithManagerNodeID(e.config.NodeID))
	}

	// Create load balancer if enabled (distributed mode only)
	if e.config.EnableLoadBalancer && e.config.EnableDistributed {
		balancer := createLoadBalancer(e.config)
		managerOpts = append(managerOpts, WithManagerLoadBalancer(balancer))

		// Create health checker
		if e.config.HealthCheckInterval > 0 {
			hcConfig := lb.HealthCheckConfig{
				Enabled:       true,
				Interval:      e.config.HealthCheckInterval,
				Timeout:       e.config.HealthCheckTimeout,
				FailThreshold: 3,
				PassThreshold: 2,
			}
			healthChecker := lb.NewHealthChecker(hcConfig, balancer)
			managerOpts = append(managerOpts, WithManagerHealthChecker(healthChecker))
		}

		e.Logger().Info("streaming load balancer configured",
			forge.F("strategy", e.config.LoadBalancerStrategy),
		)
	}

	// Create session store if session resumption is enabled
	if e.config.EnableSessionResumption {
		sessionStore := NewInMemorySessionStore()
		managerOpts = append(managerOpts, WithSessionStore(sessionStore))
		e.Logger().Info("session resumption enabled",
			forge.F("ttl", e.config.SessionResumptionTTL),
		)
	}

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
		managerOpts...,
	)

	// Register manager with DI container for backward compatibility
	manager := e.manager
	if err := vessel.ProvideConstructor(app.Container(), func() Manager {
		return manager
	}, vessel.WithAliases(ManagerKey)); err != nil {
		return fmt.Errorf("failed to register streaming manager: %w", err)
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
		return errors.New("streaming manager not initialized")
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

	// Register SSE subscription REST API
	subscribePath := strings.TrimSuffix(ssePath, "/") + "/subscribe"
	unsubscribePath := strings.TrimSuffix(ssePath, "/") + "/unsubscribe"

	if err := router.POST(subscribePath, e.handleSSESubscribe); err != nil {
		return fmt.Errorf("failed to register sse subscribe route: %w", err)
	}

	if err := router.POST(unsubscribePath, e.handleSSEUnsubscribe); err != nil {
		return fmt.Errorf("failed to register sse unsubscribe route: %w", err)
	}

	e.Logger().Info("streaming routes registered",
		forge.F("websocket", wsPath),
		forge.F("sse", ssePath),
		forge.F("sse_subscribe", subscribePath),
		forge.F("sse_unsubscribe", unsubscribePath),
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

	// Check for session resumption via query param
	sessionID := ctx.Request().URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Create enhanced connection
	enhanced := NewConnection(conn)
	enhanced.SetUserID(userID)
	enhanced.SetSessionID(sessionID)

	// Register connection
	if err := e.manager.Register(enhanced); err != nil {
		e.Logger().Error("failed to register connection",
			forge.F("conn_id", conn.ID()),
			forge.F("error", err),
		)

		return err
	}
	defer e.manager.Unregister(conn.ID())

	// Attempt session resumption if a session_id was provided
	if resumed, _ := e.manager.ResumeSession(ctx.Request().Context(), conn.ID(), sessionID); resumed {
		e.Logger().Debug("WebSocket connection resumed session",
			forge.F("conn_id", conn.ID()),
			forge.F("session_id", sessionID),
		)
	}

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
	// Get user ID from context (set by auth middleware)
	var userID string
	if uid := ctx.Get("user_id"); uid != nil {
		if uidStr, ok := uid.(string); ok {
			userID = uidStr
		}
	}

	// Check for session resumption via query param
	sessionID := ctx.Request().URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Create SSE connection adapter
	remoteAddr := ctx.Request().RemoteAddr
	localAddr := ""
	if ctx.Request().TLS != nil {
		localAddr = ctx.Request().Host
	}
	sseConn := NewSSEConnection(stream, remoteAddr, localAddr)

	// Wrap in enhanced connection
	enhanced := NewConnection(sseConn)
	enhanced.SetUserID(userID)
	enhanced.SetSessionID(sessionID)

	// Register with manager
	if err := e.manager.Register(enhanced); err != nil {
		e.Logger().Error("failed to register SSE connection",
			forge.F("conn_id", sseConn.ID()),
			forge.F("error", err),
		)
		return err
	}
	defer e.manager.Unregister(sseConn.ID())

	// Attempt session resumption
	resumed, _ := e.manager.ResumeSession(ctx.Request().Context(), sseConn.ID(), sessionID)

	// Send connection info as the first SSE event so the client knows its ID
	connInfo := map[string]any{
		"conn_id":    sseConn.ID(),
		"session_id": enhanced.GetSessionID(),
		"resumed":    resumed,
	}
	if err := stream.SendJSON("connected", connInfo); err != nil {
		return err
	}

	// Join rooms and channels from query params only if session was NOT resumed
	// (resumed sessions already have their rooms/channels restored)
	if !resumed {
		if roomsParam := ctx.Request().URL.Query().Get("rooms"); roomsParam != "" {
			for _, roomID := range strings.Split(roomsParam, ",") {
				roomID = strings.TrimSpace(roomID)
				if roomID != "" {
					if err := e.manager.JoinRoom(ctx.Request().Context(), sseConn.ID(), roomID); err != nil {
						e.Logger().Warn("SSE: failed to join room from query param",
							forge.F("room_id", roomID),
							forge.F("error", err),
						)
					}
				}
			}
		}

		if channelsParam := ctx.Request().URL.Query().Get("channels"); channelsParam != "" {
			for _, channelID := range strings.Split(channelsParam, ",") {
				channelID = strings.TrimSpace(channelID)
				if channelID != "" {
					if err := e.manager.Subscribe(ctx.Request().Context(), sseConn.ID(), channelID, nil); err != nil {
						e.Logger().Warn("SSE: failed to subscribe to channel from query param",
							forge.F("channel_id", channelID),
							forge.F("error", err),
						)
					}
				}
			}
		}
	}

	// Set user online
	if userID != "" && e.config.EnablePresence {
		_ = e.manager.SetPresence(ctx.Request().Context(), userID, StatusOnline)
		defer e.manager.SetPresence(ctx.Request().Context(), userID, StatusOffline)
	}

	e.Logger().Debug("SSE connection established",
		forge.F("conn_id", sseConn.ID()),
		forge.F("user_id", userID),
	)

	// Block until client disconnects — messages arrive via manager.WriteJSON()
	// through the SSE connection adapter's Write/WriteJSON methods
	<-stream.Context().Done()

	return nil
}

// sseSubscriptionRequest is the request body for SSE subscribe/unsubscribe endpoints.
type sseSubscriptionRequest struct {
	ConnID   string   `json:"conn_id"`
	Rooms    []string `json:"rooms,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// handleSSESubscribe handles POST requests to add SSE subscriptions.
func (e *Extension) handleSSESubscribe(ctx forge.Context) error {
	var req sseSubscriptionRequest
	if err := json.NewDecoder(ctx.Request().Body).Decode(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.ConnID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "conn_id is required"})
	}

	// Verify connection exists
	if _, err := e.manager.GetConnection(req.ConnID); err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "connection not found"})
	}

	reqCtx := ctx.Request().Context()
	var errs []string

	for _, roomID := range req.Rooms {
		if err := e.manager.JoinRoom(reqCtx, req.ConnID, roomID); err != nil {
			errs = append(errs, fmt.Sprintf("room %s: %s", roomID, err.Error()))
		}
	}

	for _, channelID := range req.Channels {
		if err := e.manager.Subscribe(reqCtx, req.ConnID, channelID, nil); err != nil {
			errs = append(errs, fmt.Sprintf("channel %s: %s", channelID, err.Error()))
		}
	}

	if len(errs) > 0 {
		return ctx.JSON(http.StatusMultiStatus, map[string]any{
			"status": "partial",
			"errors": errs,
		})
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// handleSSEUnsubscribe handles POST requests to remove SSE subscriptions.
func (e *Extension) handleSSEUnsubscribe(ctx forge.Context) error {
	var req sseSubscriptionRequest
	if err := json.NewDecoder(ctx.Request().Body).Decode(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.ConnID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "conn_id is required"})
	}

	// Verify connection exists
	if _, err := e.manager.GetConnection(req.ConnID); err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "connection not found"})
	}

	reqCtx := ctx.Request().Context()

	for _, roomID := range req.Rooms {
		_ = e.manager.LeaveRoom(reqCtx, req.ConnID, roomID)
	}

	for _, channelID := range req.Channels {
		_ = e.manager.Unsubscribe(reqCtx, req.ConnID, channelID)
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// handleMessage processes incoming messages.
func (e *Extension) handleMessage(ctx context.Context, conn Connection, msg *Message) error {
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
				return errors.New("invalid typing data")
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
				return errors.New("invalid presence data")
			}

			userID := conn.GetUserID()

			return e.manager.SetPresence(ctx, userID, status)
		}
	}

	return nil
}

// createLoadBalancer creates a load balancer based on config strategy.
func createLoadBalancer(config Config) lb.LoadBalancer {
	switch config.LoadBalancerStrategy {
	case "least_connections":
		return lb.NewLeastConnectionsBalancer(nil)
	case "consistent_hash":
		replicas := config.ConsistentHashReplicas
		if replicas <= 0 {
			replicas = 150
		}
		return lb.NewConsistentHashBalancer(replicas, nil)
	case "sticky":
		ttl := config.StickySessionTTL
		if ttl <= 0 {
			ttl = time.Hour
		}
		fallback := lb.NewLeastConnectionsBalancer(nil)
		return lb.NewStickyLoadBalancer(ttl, fallback, lb.NewInMemorySessionStore())
	default: // "round_robin" or unknown
		return lb.NewLeastConnectionsBalancer(nil)
	}
}
