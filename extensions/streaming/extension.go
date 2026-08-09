package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contributor"
	streamauth "github.com/xraph/forge/extensions/streaming/auth"
	"github.com/xraph/forge/extensions/streaming/backends"
	redisbackend "github.com/xraph/forge/extensions/streaming/backends/redis"
	streamingcontract "github.com/xraph/forge/extensions/streaming/contract"
	"github.com/xraph/forge/extensions/streaming/coordinator"
	"github.com/xraph/forge/extensions/streaming/dashboard"
	"github.com/xraph/forge/extensions/streaming/filters"
	"github.com/xraph/forge/extensions/streaming/lb"
	"github.com/xraph/forge/extensions/streaming/ratelimit"
	"github.com/xraph/forge/extensions/streaming/trackers"
	"github.com/xraph/forge/extensions/streaming/validation"
	"github.com/xraph/forgeui/bridge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for streaming functionality.
type Extension struct {
	*forge.BaseExtension

	config  Config
	manager Manager
	hooks   *HookRegistry
	codecs  *CodecRegistry
}

// NewExtension creates a new streaming extension with functional options.
//
// Returns the concrete *Extension rather than forge.Extension because the type
// carries surface the interface does not — RegisterRoutes, RegisterHook,
// RegisterCodec, Manager — and every one of those is needed in ordinary use.
// Returning the interface made the documented quick-start unable to compile.
//
// This widens the return type rather than narrowing it: *Extension satisfies
// forge.Extension, so `var e forge.Extension = streaming.NewExtension(...)`
// still works.
func NewExtension(opts ...ConfigOption) *Extension {
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
func NewExtensionWithConfig(config Config) *Extension {
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

	// The membership resolver reads the room store directly rather than going
	// through the manager, which does not exist yet at this point. It lets
	// GetOnlineUsersInRoom filter by membership instead of reporting every
	// online user on the node.
	presenceTracker := trackers.NewPresenceTracker(
		presenceStore,
		presenceOpts,
		e.Logger(),
		e.Metrics(),
		trackers.WithRoomMembers(func(ctx context.Context, roomID string) ([]string, error) {
			members, err := roomStore.GetMembers(ctx, roomID)
			if err != nil {
				return nil, err
			}

			userIDs := make([]string, 0, len(members))
			for _, member := range members {
				userIDs = append(userIDs, member.GetUserID())
			}

			return userIDs, nil
		}),
	)

	typingTracker := trackers.NewTypingTracker(
		typingStore,
		typingOpts,
		e.Logger(),
		e.Metrics(),
	)

	// Initialize hooks and codecs
	e.hooks = NewHookRegistry()
	e.codecs = NewCodecRegistry()

	// Build manager options for message pipeline
	var managerOpts []ManagerOption
	managerOpts = append(managerOpts, WithHookRegistry(e.hooks))
	managerOpts = append(managerOpts, WithCodecRegistry(e.codecs))

	// Create filter chain (always available, starts empty — users add filters via Manager)
	filterChain := filters.NewFilterChain()
	managerOpts = append(managerOpts, WithFilterChain(filterChain))

	// Create composite validator (always available, starts empty)
	validator := validation.NewCompositeValidator()
	managerOpts = append(managerOpts, WithValidator(validator))

	// Wire authorization. These are constructed unconditionally rather than
	// offered as an opt-in: the authorizers existed in extensions/streaming/auth
	// but were imported by nothing, so every deployment ran with no room or
	// message authorization at all. A security control that must be switched on
	// is a security control that is off in production.
	//
	// The default RoomAuthorizer admits members and anyone to a public room, and
	// refuses non-members entry to a private one. Applications needing a
	// different policy replace it with WithRoomAuthorizer.
	roomAuthorizer := streamauth.NewRoomAuthorizer(roomStore)
	managerOpts = append(managerOpts, WithRoomAuthorizer(roomAuthorizer))
	managerOpts = append(managerOpts,
		WithMessageAuthorizer(streamauth.NewMessageAuthorizer(roomAuthorizer, newAuthMessageStore(messageStore))))

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
//
// Connections are drained before the manager is torn down. Drain was
// implemented but never called from anywhere, so shutdown dropped every live
// socket without notice — clients saw an abrupt close and could not tell a
// deploy from a crash, which matters because the two deserve different
// reconnect behaviour.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping streaming extension")

	if e.manager != nil {
		drainCtx, cancel := context.WithTimeout(ctx, e.config.DrainTimeout)
		if err := e.manager.Drain(drainCtx); err != nil {
			e.Logger().Warn("streaming drain incomplete",
				forge.F("error", err),
			)
		}

		cancel()

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

	// Check for content type preference via query param or header
	preferredContentType := ctx.Request().URL.Query().Get("content_type")
	if preferredContentType == "" {
		preferredContentType = ctx.Request().Header.Get("X-Content-Type")
	}

	// Create enhanced connection
	enhanced := NewConnection(conn)
	enhanced.SetUserID(userID)
	enhanced.SetSessionID(sessionID)

	if preferredContentType != "" {
		enhanced.SetContentType(preferredContentType)
	}

	// Fire connection hooks (before registration)
	if e.hooks != nil {
		if err := e.hooks.FireOnConnect(ctx.Request().Context(), enhanced); err != nil {
			e.Logger().Debug("connection rejected by hook",
				forge.F("conn_id", conn.ID()),
				forge.F("error", err),
			)

			return err
		}
	}

	// Register connection
	if err := e.manager.Register(enhanced); err != nil {
		e.Logger().Error("failed to register connection",
			forge.F("conn_id", conn.ID()),
			forge.F("error", err),
		)

		return err
	}
	defer func() {
		e.manager.Unregister(conn.ID())

		// Fire disconnect hooks (after unregistration)
		if e.hooks != nil {
			e.hooks.FireOnDisconnect(ctx.Request().Context(), enhanced)
		}
	}()

	// Attempt session resumption if a session_id was provided
	if resumed, _ := e.manager.ResumeSession(ctx.Request().Context(), conn.ID(), sessionID); resumed {
		e.Logger().Debug("WebSocket connection resumed session",
			forge.F("conn_id", conn.ID()),
			forge.F("session_id", sessionID),
		)
	}

	// Apply the configured inbound message cap to this socket. The router
	// defaults to 1 MiB; MaxMessageSize was never pushed down to it, so the
	// configured value (64 KB by default) did nothing.
	if limiter, ok := conn.(interface{ SetReadLimit(int64) }); ok && e.config.MaxMessageSize > 0 {
		limiter.SetReadLimit(int64(e.config.MaxMessageSize))
	}

	// Set user online.
	//
	// Going offline is conditional on this being the user's last connection.
	// Unconditionally marking offline on any disconnect made a user with two
	// tabs open appear offline the moment either one closed — and the default
	// MaxConnectionsPerUser of 5 says multiple connections are the expected case,
	// not an edge one.
	if userID != "" && e.config.EnablePresence {
		_ = e.manager.SetPresence(ctx.Request().Context(), userID, StatusOnline)

		defer func() {
			// Detached from the request context on purpose: by the time this
			// runs the request context is cancelled, so a presence write to a
			// Redis backend would fail with "context canceled" and the user
			// would be stuck online forever.
			presenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx.Request().Context()), 5*time.Second)
			defer cancel()

			// Unregister has already run (deferred later, so it fires first),
			// meaning this connection is out of the registry and any remaining
			// entries are genuinely other live sockets.
			if len(e.manager.GetUserConnections(userID)) == 0 {
				_ = e.manager.SetPresence(presenceCtx, userID, StatusOffline)
			}
		}()
	}

	// Heartbeat, scoped to the connection's lifetime.
	hbCtx, stopHeartbeat := context.WithCancel(conn.Context())
	defer stopHeartbeat()

	go e.heartbeat(hbCtx, enhanced, enhanced)

	// Message loop — read raw bytes for multi-datatype support
	for {
		data, err := conn.Read()
		if err != nil {
			// Connection closed or error
			return err
		}

		// Update activity
		enhanced.UpdateActivity()

		if userID != "" && e.config.EnablePresence {
			_ = e.manager.TrackActivity(ctx.Request().Context(), userID)
		}

		reqCtx := ctx.Request().Context()

		// Fire raw message hooks (can transform bytes or reject)
		if e.hooks != nil {
			data, err = e.hooks.FireOnRawMessage(reqCtx, enhanced, data)
			if err != nil {
				e.Logger().Debug("raw message rejected by hook",
					forge.F("conn_id", conn.ID()),
					forge.F("error", err),
				)

				continue
			}
		}

		// Decode message using codec
		var msg Message

		if err := e.codecs.Decode(data, &msg); err != nil {
			// JSON decode failed — check if this is binary data
			msg = Message{
				Type:        MessageTypeMessage,
				ContentType: ContentTypeBinary,
				RawData:     data,
				UserID:      userID,
				Timestamp:   time.Now(),
			}
		}

		// Stamp the authenticated identity over whatever the client sent.
		//
		// This is an overwrite, not a default. Filling it in only when empty
		// let a client set "user_id" to any value it liked and have the server
		// broadcast it and write it to history under that name — impersonation
		// with no special tooling required. The connection's identity is the
		// only identity that can be trusted here.
		msg.UserID = userID

		// Likewise the timestamp: a client-supplied one is unverifiable and
		// orders history however the client pleases.
		msg.Timestamp = time.Now()

		// The message ID is what dedup and client-side ordering key on, so it
		// must not be attacker-chosen either — a client could otherwise
		// suppress another node's message by claiming its ID first.
		msg.ID = uuid.New().String()

		// Fire message received hooks (can transform or block)
		processMsg := &msg
		if e.hooks != nil {
			processMsg, err = e.hooks.FireOnMessageReceived(reqCtx, enhanced, &msg)
			if err != nil {
				e.Logger().Debug("message rejected by hook",
					forge.F("conn_id", conn.ID()),
					forge.F("error", err),
				)

				continue
			}

			if processMsg == nil {
				continue // message blocked
			}
		}

		// The gate: size, rate limit, target authorization, content validation.
		// Nothing reaches a broadcast without passing through here.
		gated, err := e.manager.ProcessInbound(reqCtx, processMsg, enhanced)
		if err != nil {
			e.Logger().Debug("inbound message rejected",
				forge.F("conn_id", conn.ID()),
				forge.F("type", processMsg.Type),
				forge.F("error", err),
			)

			// Tell the client why. A silently dropped message leaves a web
			// client with no signal to back off against, so it retries into
			// the same limit forever.
			e.sendError(enhanced, processMsg, err)

			continue
		}

		// Handle different message types
		if err := e.handleMessage(reqCtx, enhanced, gated); err != nil {
			e.Logger().Error("failed to handle message",
				forge.F("conn_id", conn.ID()),
				forge.F("type", gated.Type),
				forge.F("error", err),
			)

			e.sendError(enhanced, gated, err)

			// Fire error hooks
			if e.hooks != nil {
				e.hooks.FireOnError(reqCtx, enhanced, err)
			}
		}
	}
}

// sendError reports a rejected message back to its sender.
//
// A rejection the client cannot see is a rejection the client cannot respond
// to: a rate-limited web client with no error frame simply retries into the
// same limit. The frame carries a machine-readable code plus, for rate limits,
// how long to wait — which is the whole point of the exercise.
func (e *Extension) sendError(conn Connection, cause *Message, err error) {
	frame := &Message{
		ID:        uuid.New().String(),
		Type:      MessageTypeError,
		Event:     "message.rejected",
		Timestamp: time.Now(),
		Data: map[string]any{
			"code":    errorCode(err),
			"message": err.Error(),
		},
	}

	if cause != nil {
		frame.RoomID = cause.RoomID
		frame.ChannelID = cause.ChannelID

		if data, ok := frame.Data.(map[string]any); ok {
			data["rejected_id"] = cause.ID
		}
	}

	// Attach retry guidance when the rejection was a rate limit, so the client
	// backs off by the server's schedule rather than guessing at one.
	if errors.Is(err, ErrRateLimitExceeded) {
		if status, sErr := e.manager.GetRateLimitStatus(context.Background(), conn.GetUserID()); sErr == nil {
			if data, ok := frame.Data.(map[string]any); ok {
				data["retry_after_ms"] = status.RetryAfter.Milliseconds()
				data["reset_at"] = status.ResetAt
			}
		}
	}

	if writeErr := conn.WriteJSON(frame); writeErr != nil {
		e.Logger().Debug("failed to deliver error frame",
			forge.F("conn_id", conn.ID()),
			forge.F("error", writeErr),
		)
	}
}

// errorCode maps an internal error to a stable string a client can branch on.
// Clients must not have to match on error prose, which changes.
func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrRateLimitExceeded):
		return "rate_limited"
	case errors.Is(err, ErrMessageTooLarge):
		return "message_too_large"
	case errors.Is(err, ErrSendDenied):
		return "send_denied"
	case errors.Is(err, ErrUserMuted):
		return "muted"
	case errors.Is(err, ErrUserBanned):
		return "banned"
	case errors.Is(err, ErrRoomAccessDenied):
		return "room_access_denied"
	case errors.Is(err, ErrChannelAccessDenied):
		return "channel_access_denied"
	case errors.Is(err, ErrRoomLimitReached):
		return "room_limit_reached"
	case errors.Is(err, ErrChannelLimitReached):
		return "channel_limit_reached"
	case errors.Is(err, ErrRoomsDisabled),
		errors.Is(err, ErrChannelsDisabled),
		errors.Is(err, ErrHistoryDisabled),
		errors.Is(err, ErrPresenceDisabled),
		errors.Is(err, ErrTypingDisabled):
		return "feature_disabled"
	case errors.Is(err, ErrInvalidMessage):
		return "invalid_message"
	default:
		return "error"
	}
}

// heartbeat pings the client on an interval and closes the connection when a
// pong does not arrive in time.
//
// PingInterval, PongTimeout and WriteTimeout were configuration fields that
// nothing read — they appeared only in the dashboard's settings table and the
// contract manifest. So the dashboard reported a 30s ping interval on a server
// that had never sent a ping. Without one, a connection dropped by an
// intermediary stays open in the server's tables indefinitely: the read blocks
// forever on a socket the peer has already forgotten, holding its room index
// entries and its slot against the connection limits.
func (e *Extension) heartbeat(ctx context.Context, conn Connection, enhanced Connection) {
	interval := e.config.PingInterval
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Liveness is judged on read activity, not on a pong frame:
			// UpdateActivity is called for every inbound message, and a client
			// that is talking is unambiguously alive. A separate pong channel
			// would be a second liveness signal that can disagree with the first.
			idle := time.Since(enhanced.GetLastActivity())
			if idle > interval+e.config.PongTimeout {
				e.Logger().Debug("closing unresponsive connection",
					forge.F("conn_id", conn.ID()),
					forge.F("idle", idle),
				)

				_ = conn.Close()

				return
			}

			ping := &Message{
				ID:        uuid.New().String(),
				Type:      MessageTypeSystem,
				Event:     "ping",
				Timestamp: time.Now(),
			}

			if err := conn.WriteJSON(ping); err != nil {
				// The write failed, so the peer is gone. Closing here rather
				// than waiting for the read side to notice frees the slot now.
				_ = conn.Close()

				return
			}
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

	// Wrap in enhanced connection with SSE transport type
	enhanced := NewConnectionWithTransport(sseConn, TransportSSE)
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

	// Fill the gap.
	//
	// Runs AFTER rooms are joined, because Replay intersects the client's cursor
	// with the rooms this connection actually holds — before the joins there is
	// nothing to intersect and the replay would be silently empty.
	//
	// The cursor comes from Last-Event-ID, which EventSource sets automatically
	// on reconnect from the last `id:` it saw. A malformed one is logged and
	// ignored rather than failing the connection: the client still gets a live
	// stream, and falls back to its own recovery path.
	if lastEventID := lastEventIDOf(stream, ctx); lastEventID != "" && e.config.EnableMessageHistory {
		replayed, replayErr := e.manager.Replay(ctx.Request().Context(), sseConn.ID(), lastEventID)
		if replayErr != nil {
			e.Logger().Debug("SSE replay skipped",
				forge.F("conn_id", sseConn.ID()),
				forge.F("error", replayErr),
			)

			// Tell the client its cursor was not usable so it can fall back to
			// a full resync instead of assuming it is caught up — the one
			// outcome worse than replaying too much is believing a gap is closed
			// when it is not.
			_ = stream.SendJSON("resync_required", map[string]any{
				"reason": "cursor could not be honoured",
			})
		} else if replayed > 0 {
			e.Logger().Debug("SSE replayed missed messages",
				forge.F("conn_id", sseConn.ID()),
				forge.F("count", replayed),
			)
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

// maxSubscriptionBodyBytes caps the SSE subscription request body. The handler
// decodes into a slice-bearing struct, so an unbounded body is an unbounded
// allocation from an unauthenticated caller.
const maxSubscriptionBodyBytes = 64 * 1024

// lastEventIDOf resolves the client's resume cursor.
//
// The header is the standard: EventSource sets Last-Event-ID itself on
// reconnect, with no application code involved. The query parameter is the
// fallback for clients that cannot set headers — a browser EventSource cannot
// set any — which is the common case rather than an exotic one, so it is a
// first-class path and not a workaround.
func lastEventIDOf(stream forge.Stream, ctx forge.Context) string {
	if reader, ok := stream.(interface{ LastEventID() string }); ok {
		if id := reader.LastEventID(); id != "" {
			return id
		}
	}

	if id := ctx.Request().Header.Get("Last-Event-ID"); id != "" {
		return id
	}

	return ctx.Request().URL.Query().Get("last_event_id")
}

// sseSubscriptionRequest is the request body for SSE subscribe/unsubscribe endpoints.
type sseSubscriptionRequest struct {
	ConnID   string   `json:"conn_id"`
	Rooms    []string `json:"rooms,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// authorizeConnAccess resolves the connection a request names and verifies the
// caller owns it.
//
// Both SSE subscription endpoints take a conn_id from the request body. They
// previously checked only that such a connection existed, so any caller could
// name somebody else's connection id and subscribe it to arbitrary rooms — or
// unsubscribe it from all of them. That is a direct object reference with no
// ownership check on either side.
//
// Ownership is by user id. An anonymous connection cannot be addressed through
// these endpoints at all: with no identity on either side there is nothing to
// compare, and "both are empty" is not a match, it is an absence of evidence.
func (e *Extension) authorizeConnAccess(ctx forge.Context, connID string) (Connection, error) {
	conn, err := e.manager.GetConnection(connID)
	if err != nil {
		// Deliberately the same response as an ownership failure below, so the
		// endpoint cannot be used to probe which connection ids are live.
		return nil, errConnAccessDenied
	}

	var callerID string
	if uid := ctx.Get("user_id"); uid != nil {
		if uidStr, ok := uid.(string); ok {
			callerID = uidStr
		}
	}

	if callerID == "" || conn.GetUserID() != callerID {
		e.Logger().Warn("SSE subscription access denied",
			forge.F("conn_id", connID),
			forge.F("caller", callerID),
		)

		return nil, errConnAccessDenied
	}

	return conn, nil
}

// errConnAccessDenied is returned for both "no such connection" and "not
// yours", so the two are indistinguishable to a caller.
var errConnAccessDenied = errors.New("connection not found or access denied")

// handleSSESubscribe handles POST requests to add SSE subscriptions.
func (e *Extension) handleSSESubscribe(ctx forge.Context) error {
	var req sseSubscriptionRequest
	if err := json.NewDecoder(io.LimitReader(ctx.Request().Body, maxSubscriptionBodyBytes)).Decode(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.ConnID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "conn_id is required"})
	}

	if _, err := e.authorizeConnAccess(ctx, req.ConnID); err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
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
	if err := json.NewDecoder(io.LimitReader(ctx.Request().Body, maxSubscriptionBodyBytes)).Decode(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.ConnID == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "conn_id is required"})
	}

	if _, err := e.authorizeConnAccess(ctx, req.ConnID); err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
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
//
// Identity is stamped here rather than only at the socket read loop. The read
// loop does it too, but this function is reachable on its own — from a test,
// from a second transport, from any later refactor — and a security property
// that holds only because of where the one caller happens to put it is not a
// property, it is a coincidence. Stamping at the point of use makes spoofing
// unrepresentable regardless of how the message arrived.
func (e *Extension) handleMessage(ctx context.Context, conn Connection, msg *Message) error {
	msg.UserID = conn.GetUserID()

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

// DashboardContributor implements dashboard.DashboardAware.
// Returns a streaming dashboard contributor for auto-registration.
// Uses resolver closures so the manager/config are resolved at render time,
// not at discovery time (when they may not yet be initialized).
func (e *Extension) DashboardContributor() contributor.LocalContributor {
	return dashboard.NewStreamingContributor(
		func() Manager { return e.manager },
		func() Config { return e.config },
	)
}

// RegisterDashboardBridge implements dashboard.BridgeAware.
// Registers streaming bridge functions for Go↔JS communication.
// Uses resolver closures so the manager/config are resolved at request time.
func (e *Extension) RegisterDashboardBridge(b *bridge.Bridge) error {
	return dashboard.RegisterBridge(b,
		func() Manager { return e.manager },
		func() Config { return e.config },
	)
}

// RegisterContractContributor implements dashboard.ContractContributorAware.
// Wires the streaming-contract handlers (slice f migration target) into the
// dashboard's contract dispatcher and registers the embedded YAML manifest.
// Coexists with DashboardContributor: both are registered during dashboard
// startup, so the legacy /dashboard/ext/streaming/* and the new
// /dashboard/contract/streaming-contract/* paths both stay live during the
// migration window. See extensions/dashboard/contract/SLICE_F_DESIGN.md.
func (e *Extension) RegisterContractContributor(
	disp *dispatcher.Dispatcher,
	reg dashcontract.Registry,
	wreg dashcontract.WardenRegistry,
) error {
	return streamingcontract.Register(disp, reg, wreg, streamingcontract.Deps{
		Manager: func() Manager { return e.manager },
		Config:  func() Config { return e.config },
	})
}

// RegisterHook adds a streaming hook for lifecycle events.
// Hooks can implement one or more hook interfaces (ConnectionHook, MessageHook,
// RawMessageHook, RoomHook, PresenceHook, ErrorHook).
func (e *Extension) RegisterHook(hook StreamingHook) {
	e.hooks.Add(hook)
}

// UnregisterHook removes a streaming hook by name.
func (e *Extension) UnregisterHook(name string) {
	e.hooks.Remove(name)
}

// RegisterCodec adds a message codec for a specific content type.
func (e *Extension) RegisterCodec(codec Codec) {
	e.codecs.Register(codec)
}

// Hooks returns the hook registry for direct access.
func (e *Extension) Hooks() *HookRegistry {
	return e.hooks
}

// Codecs returns the codec registry for direct access.
func (e *Extension) Codecs() *CodecRegistry {
	return e.codecs
}
