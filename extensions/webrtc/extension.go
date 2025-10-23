package webrtc

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
	"github.com/xraph/forge/internal/logger"
)

// Extension is the WebRTC extension
type Extension struct {
	config Config

	// Dependencies
	streaming *streaming.Extension
	container forge.Container
	logger    forge.Logger
	metrics   forge.Metrics

	// Components
	signaling SignalingManager
	rooms     map[string]CallRoom
	roomsMu   sync.RWMutex

	// SFU components (if enabled)
	sfuRouter SFURouter

	// Quality monitoring
	qualityMonitor QualityMonitor

	// Recording
	recorder Recorder

	// Lifecycle
	started bool
	mu      sync.RWMutex
}

// New creates a new WebRTC extension
func New(streamingExt *streaming.Extension, config Config, opts ...ConfigOption) (*Extension, error) {
	// Apply options
	for _, opt := range opts {
		opt(&config)
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("webrtc: config validation failed: %w", err)
	}

	ext := &Extension{
		config:    config,
		streaming: streamingExt,
		rooms:     make(map[string]CallRoom),
	}

	return ext, nil
}

// Name returns the extension name
func (e *Extension) Name() string {
	return "webrtc"
}

// Register registers the extension with the app
func (e *Extension) Register(app forge.App) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Get dependencies from DI container
	e.container = app.Container()

	// Get logger (optional) - TODO: Use proper DI resolution when available
	e.logger = nil // Will be set via dependency injection

	// Ensure we always have a logger (fallback to null logger)
	if e.logger == nil {
		e.logger = logger.GetGlobalLogger()
	}

	// Get metrics (optional)
	e.metrics = nil // Will be set via dependency injection

	// Initialize signaling manager
	e.signaling = NewSignalingManager(e.streaming, e.logger)

	// Initialize SFU router if enabled
	if e.config.Topology == TopologySFU {
		if e.config.SFUConfig == nil {
			return fmt.Errorf("webrtc: SFU enabled but no config provided")
		}
		e.sfuRouter = NewSFURouter(*e.config.SFUConfig, e.logger, e.metrics)
	}

	// Initialize quality monitor
	if e.config.QualityConfig.MonitorEnabled {
		e.qualityMonitor = NewQualityMonitor(e.config.QualityConfig, e.logger)
	}

	// Initialize recorder if enabled
	if e.config.RecordingEnabled {
		e.recorder = NewRecorder(e.config.RecordingPath, e.logger)
	}

	// Register in DI container
	// TODO: Proper DI registration when container API is finalized

	e.logger.Info("webrtc extension registered",
		forge.F("topology", e.config.Topology),
		forge.F("stun_servers", len(e.config.STUNServers)),
		forge.F("turn_servers", len(e.config.TURNServers)),
	)

	return nil
}

// Start starts the extension
func (e *Extension) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return nil
	}

	// Start signaling
	if err := e.signaling.Start(ctx); err != nil {
		return fmt.Errorf("webrtc: failed to start signaling: %w", err)
	}

	// Start quality monitor
	if e.qualityMonitor != nil {
		// Quality monitor starts monitoring per-peer
	}

	e.started = true
	e.logger.Info("webrtc extension started")

	return nil
}

// Stop stops the extension
func (e *Extension) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started {
		return nil
	}

	// Stop signaling
	if err := e.signaling.Stop(ctx); err != nil {
		e.logger.Error("webrtc: failed to stop signaling", forge.F("error", err))
	}

	// Close all rooms
	e.roomsMu.Lock()
	for roomID, room := range e.rooms {
		if err := room.Close(ctx); err != nil {
			e.logger.Error("webrtc: failed to close room",
				forge.F("room_id", roomID),
				forge.F("error", err),
			)
		}
	}
	e.rooms = make(map[string]CallRoom)
	e.roomsMu.Unlock()

	e.started = false
	e.logger.Info("webrtc extension stopped")

	return nil
}

// Health checks extension health
func (e *Extension) Health(ctx context.Context) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.started {
		return fmt.Errorf("webrtc: extension not started")
	}

	// Check dependencies
	if e.streaming == nil {
		return fmt.Errorf("webrtc: streaming extension not available")
	}

	// Check streaming health
	if err := e.streaming.Health(ctx); err != nil {
		return fmt.Errorf("webrtc: streaming extension unhealthy: %w", err)
	}

	// Check signaling
	if e.signaling == nil {
		return fmt.Errorf("webrtc: signaling manager not initialized")
	}

	// Check room count (warn if too many)
	e.roomsMu.RLock()
	roomCount := len(e.rooms)
	e.roomsMu.RUnlock()

	if roomCount > 10000 {
		e.logger.Warn("webrtc: high room count",
			forge.F("room_count", roomCount),
			forge.F("threshold", 10000),
		)
	}

	// Check SFU router health (if enabled)
	if e.config.Topology == TopologySFU && e.sfuRouter != nil {
		stats, err := e.sfuRouter.GetStats(ctx)
		if err != nil {
			return fmt.Errorf("webrtc: SFU router unhealthy: %w", err)
		}

		// Warn if router has traffic but no activity
		if stats.TotalBytesReceived == 0 && roomCount > 0 {
			e.logger.Warn("webrtc: SFU router not receiving traffic",
				forge.F("room_count", roomCount),
			)
		}
	}

	return nil
}

// CreateCallRoom creates a new call room
func (e *Extension) CreateCallRoom(ctx context.Context, roomID string, opts streaming.RoomOptions) (CallRoom, error) {
	e.roomsMu.Lock()
	defer e.roomsMu.Unlock()

	// Check if room already exists
	if _, exists := e.rooms[roomID]; exists {
		return nil, fmt.Errorf("webrtc: room %s already exists", roomID)
	}

	// Create underlying streaming room
	// TODO: This needs to be updated when streaming Room creation API is finalized
	// For now, we'll need to construct a Room and pass it to Manager.CreateRoom
	// The streaming extension should provide a factory method for this
	var streamingRoom streaming.Room = nil

	// Metrics: track room creation
	if e.metrics != nil {
		e.metrics.Inc("webrtc.rooms.create_attempts",
			forge.F("topology", e.config.Topology))
	}

	// Create call room
	var callRoom CallRoom

	switch e.config.Topology {
	case TopologyMesh:
		callRoom = NewMeshCallRoom(
			streamingRoom,
			e.config,
			e.signaling,
			e.logger,
			e.metrics,
		)

	case TopologySFU:
		callRoom = NewSFUCallRoom(
			streamingRoom,
			e.config,
			e.signaling,
			e.sfuRouter,
			e.logger,
			e.metrics,
		)

	default:
		return nil, fmt.Errorf("webrtc: unsupported topology: %s", e.config.Topology)
	}

	// Store room
	e.rooms[roomID] = callRoom

	e.logger.Info("call room created",
		forge.F("room_id", roomID),
		forge.F("topology", e.config.Topology),
		forge.F("max_members", opts.MaxMembers),
	)

	// Metrics: track successful creation
	if e.metrics != nil {
		e.metrics.Inc("webrtc.rooms.created",
			forge.F("topology", e.config.Topology))
		e.metrics.Gauge("webrtc.rooms.active", float64(len(e.rooms)))
	}

	return callRoom, nil
}

// GetCallRoom retrieves a call room
func (e *Extension) GetCallRoom(roomID string) (CallRoom, error) {
	e.roomsMu.RLock()
	defer e.roomsMu.RUnlock()

	room, exists := e.rooms[roomID]
	if !exists {
		return nil, ErrRoomNotFound
	}

	return room, nil
}

// DeleteCallRoom deletes a call room
func (e *Extension) DeleteCallRoom(ctx context.Context, roomID string) error {
	e.roomsMu.Lock()
	defer e.roomsMu.Unlock()

	room, exists := e.rooms[roomID]
	if !exists {
		return fmt.Errorf("webrtc: delete room failed: %w", ErrRoomNotFound)
	}

	// Close the room
	if err := room.Close(ctx); err != nil {
		return fmt.Errorf("webrtc: failed to close room %s: %w", roomID, err)
	}

	delete(e.rooms, roomID)

	e.logger.Info("call room deleted",
		forge.F("room_id", roomID),
		forge.F("remaining_rooms", len(e.rooms)),
	)

	// Metrics: track deletion
	if e.metrics != nil {
		e.metrics.Inc("webrtc.rooms.deleted")
		e.metrics.Gauge("webrtc.rooms.active", float64(len(e.rooms)))
	}

	return nil
}

// GetCallRooms returns all call rooms
func (e *Extension) GetCallRooms() []CallRoom {
	e.roomsMu.RLock()
	defer e.roomsMu.RUnlock()

	rooms := make([]CallRoom, 0, len(e.rooms))
	for _, room := range e.rooms {
		rooms = append(rooms, room)
	}

	return rooms
}

// JoinCall is a convenience method to join a call
func (e *Extension) JoinCall(ctx context.Context, roomID, userID string, opts *JoinOptions) (PeerConnection, error) {
	room, err := e.GetCallRoom(roomID)
	if err != nil {
		return nil, fmt.Errorf("webrtc: join call failed for room %s: %w", roomID, err)
	}

	// Metrics: track join attempts
	if e.metrics != nil {
		e.metrics.Inc("webrtc.calls.join_attempts",
			forge.F("room_id", roomID),
			forge.F("user_id", userID))
	}

	peer, err := room.JoinCall(ctx, userID, opts)
	if err != nil {
		// Metrics: track failures
		if e.metrics != nil {
			e.metrics.Inc("webrtc.calls.join_failures",
				forge.F("room_id", roomID),
				forge.F("error", err.Error()))
		}
		return nil, fmt.Errorf("webrtc: failed to join call in room %s: %w", roomID, err)
	}

	// Metrics: track successful joins
	if e.metrics != nil {
		e.metrics.Inc("webrtc.calls.joined",
			forge.F("room_id", roomID))
		e.metrics.Gauge("webrtc.peers.active", float64(len(room.GetPeers())))
	}

	return peer, nil
}

// LeaveCall is a convenience method to leave a call
func (e *Extension) LeaveCall(ctx context.Context, roomID, userID string) error {
	room, err := e.GetCallRoom(roomID)
	if err != nil {
		return fmt.Errorf("webrtc: leave call failed for room %s: %w", roomID, err)
	}

	err = room.Leave(ctx, userID)
	if err != nil {
		return fmt.Errorf("webrtc: failed to leave call in room %s: %w", roomID, err)
	}

	// Metrics: track leaves
	if e.metrics != nil {
		e.metrics.Inc("webrtc.calls.left",
			forge.F("room_id", roomID))
		e.metrics.Gauge("webrtc.peers.active", float64(len(room.GetPeers())))
	}

	return nil
}

// GetConfig returns the extension configuration
func (e *Extension) GetConfig() Config {
	return e.config
}

// GetSignalingManager returns the signaling manager
func (e *Extension) GetSignalingManager() SignalingManager {
	return e.signaling
}

// GetSFURouter returns the SFU router (if enabled)
func (e *Extension) GetSFURouter() SFURouter {
	return e.sfuRouter
}

// GetQualityMonitor returns the quality monitor
func (e *Extension) GetQualityMonitor() QualityMonitor {
	return e.qualityMonitor
}

// GetRecorder returns the recorder
func (e *Extension) GetRecorder() Recorder {
	return e.recorder
}

// RegisterRoutes registers WebRTC HTTP routes
func (e *Extension) RegisterRoutes(router forge.Router) error {
	// WebSocket endpoint for signaling
	return router.WebSocket("/webrtc/signal/{roomID}", func(ctx forge.Context, conn forge.Connection) error {
		roomID := ctx.Param("roomID")
		userID := ctx.Get("user_id").(string)

		// Handle signaling for this connection
		return e.handleSignaling(ctx, roomID, userID, conn)
	})
}

// handleSignaling handles WebRTC signaling for a connection
func (e *Extension) handleSignaling(fctx forge.Context, roomID, userID string, conn forge.Connection) error {
	ctx := fctx.Request().Context()

	// Get or create call room
	room, err := e.GetCallRoom(roomID)
	if err != nil {
		// Room doesn't exist, create it
		room, err = e.CreateCallRoom(ctx, roomID, streaming.RoomOptions{
			Name:       "Call " + roomID,
			MaxMembers: 50,
		})
		if err != nil {
			return err
		}
	}

	// Join the call
	peer, err := room.JoinCall(ctx, userID, &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
	})
	if err != nil {
		return err
	}
	defer room.Leave(ctx, userID)

	// Handle signaling messages via connection
	return e.handleSignalingLoop(ctx, roomID, userID, peer, conn)
}

func (e *Extension) handleSignalingLoop(ctx context.Context, roomID, userID string, peer PeerConnection, conn forge.Connection) error {
	// Listen for signaling messages
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return err
			}

			// Handle signaling message
			// TODO: Route to peer.HandleSignaling()
		}
	}
}
