package webrtc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)

// Basic implementation of CallRoom interface
// This provides a foundation that can be extended for specific topologies

// baseCallRoom provides common functionality for call rooms
type baseCallRoom struct {
	id        string
	name      string
	room      streaming.Room
	config    Config
	signaling SignalingManager
	logger    forge.Logger
	metrics   forge.Metrics

	// Room state
	peers   map[string]PeerConnection
	peersMu sync.RWMutex

	// Quality monitoring
	qualityMonitors map[string]QualityMonitor
	monitorsMu      sync.RWMutex

	// Recording (if enabled)
	recorder Recorder

	// Lifecycle
	started bool
	closed  bool
	mu      sync.RWMutex
}

// newBaseCallRoom creates a new base call room
func newBaseCallRoom(
	id string,
	name string,
	streamingRoom streaming.Room,
	config Config,
	signaling SignalingManager,
	logger forge.Logger,
	metrics forge.Metrics,
) *baseCallRoom {
	return &baseCallRoom{
		id:              id,
		name:            name,
		room:            streamingRoom,
		config:          config,
		signaling:       signaling,
		logger:          logger,
		metrics:         metrics,
		peers:           make(map[string]PeerConnection),
		qualityMonitors: make(map[string]QualityMonitor),
	}
}

// ID returns room ID
func (r *baseCallRoom) ID() string {
	return r.id
}

// Name returns room name
func (r *baseCallRoom) Name() string {
	return r.name
}

// GetPeers returns all peer connections
func (r *baseCallRoom) GetPeers() []PeerConnection {
	r.peersMu.RLock()
	defer r.peersMu.RUnlock()

	peers := make([]PeerConnection, 0, len(r.peers))
	for _, peer := range r.peers {
		peers = append(peers, peer)
	}
	return peers
}

// GetParticipants returns participant information
func (r *baseCallRoom) GetParticipants() []Participant {
	r.peersMu.RLock()
	defer r.peersMu.RUnlock()

	participants := make([]Participant, 0, len(r.peers))
	for userID, peer := range r.peers {
		// Get quality if available
		var quality *ConnectionQuality
		if monitor, exists := r.qualityMonitors[userID]; exists {
			quality, _ = monitor.GetQuality(peer.ID())
		}

		participants = append(participants, Participant{
			UserID:   userID,
			Quality:  quality,
			JoinedAt: time.Now(), // Would need to track actual join time
		})
	}
	return participants
}

// GetPeer returns peer connection for user
func (r *baseCallRoom) GetPeer(userID string) (PeerConnection, error) {
	r.peersMu.RLock()
	defer r.peersMu.RUnlock()

	peer, exists := r.peers[userID]
	if !exists {
		return nil, fmt.Errorf("webrtc: peer not found for user %s: %w", userID, ErrPeerNotFound)
	}
	return peer, nil
}

// Leave leaves the call
func (r *baseCallRoom) Leave(ctx context.Context, userID string) error {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	peer, exists := r.peers[userID]
	if !exists {
		return fmt.Errorf("webrtc: user %s not in call: %w", userID, ErrNotInRoom)
	}

	// Stop quality monitoring
	r.monitorsMu.Lock()
	if monitor, exists := r.qualityMonitors[userID]; exists {
		monitor.Stop(peer.ID())
		delete(r.qualityMonitors, userID)
	}
	r.monitorsMu.Unlock()

	// Close peer connection
	if err := peer.Close(ctx); err != nil {
		r.logger.Error("failed to close peer connection",
			forge.F("user_id", userID),
			forge.F("error", err),
		)
	}

	// Remove from peers
	delete(r.peers, userID)

	r.logger.Info("user left call",
		forge.F("room_id", r.id),
		forge.F("user_id", userID),
		forge.F("remaining_peers", len(r.peers)),
	)

	// Track metrics
	if r.metrics != nil {
		r.metrics.Gauge("webrtc.room.peers", "count", fmt.Sprintf("%d", len(r.peers))).Set(float64(len(r.peers)))
	}

	return nil
}

// Close closes the call room
func (r *baseCallRoom) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.logger.Info("closing call room",
		forge.F("room_id", r.id),
		forge.F("peers", len(r.peers)),
	)

	// Close all peer connections
	r.peersMu.Lock()
	for userID, peer := range r.peers {
		if err := peer.Close(ctx); err != nil {
			r.logger.Error("failed to close peer connection during room close",
				forge.F("user_id", userID),
				forge.F("error", err),
			)
		}
	}
	r.peersMu.Unlock()

	// Stop quality monitors
	r.monitorsMu.Lock()
	for userID, monitor := range r.qualityMonitors {
		monitor.Stop(userID)
	}
	r.qualityMonitors = make(map[string]QualityMonitor)
	r.monitorsMu.Unlock()

	// Stop recording if active
	if r.recorder != nil {
		if err := r.recorder.Stop(ctx, r.id); err != nil {
			r.logger.Error("failed to stop recording during room close",
				forge.F("error", err),
			)
		}
	}

	r.closed = true

	r.logger.Info("call room closed",
		forge.F("room_id", r.id),
	)

	return nil
}

// MuteUser mutes a user's audio (stub implementation)
func (r *baseCallRoom) MuteUser(ctx context.Context, userID string) error {
	return ErrNotImplemented
}

// UnmuteUser unmutes a user's audio (stub implementation)
func (r *baseCallRoom) UnmuteUser(ctx context.Context, userID string) error {
	return ErrNotImplemented
}

// EnableVideo enables user's video (stub implementation)
func (r *baseCallRoom) EnableVideo(ctx context.Context, userID string) error {
	return ErrNotImplemented
}

// DisableVideo disables user's video (stub implementation)
func (r *baseCallRoom) DisableVideo(ctx context.Context, userID string) error {
	return ErrNotImplemented
}

// StartScreenShare starts screen sharing (stub implementation)
func (r *baseCallRoom) StartScreenShare(ctx context.Context, userID string, track MediaTrack) error {
	return ErrNotImplemented
}

// StopScreenShare stops screen sharing (stub implementation)
func (r *baseCallRoom) StopScreenShare(ctx context.Context, userID string) error {
	return ErrNotImplemented
}

// GetQuality returns call quality metrics (stub implementation)
func (r *baseCallRoom) GetQuality(ctx context.Context) (*CallQuality, error) {
	return nil, ErrNotImplemented
}

// MeshCallRoom implements CallRoom with mesh topology
type MeshCallRoom struct {
	*baseCallRoom
}

// NewMeshCallRoom creates a call room with mesh topology
func NewMeshCallRoom(
	streamingRoom streaming.Room,
	config Config,
	signaling SignalingManager,
	logger forge.Logger,
	metrics forge.Metrics,
) CallRoom {
	roomID := fmt.Sprintf("mesh-%s", streamingRoom.GetID())

	base := newBaseCallRoom(
		roomID,
		"Mesh Call Room",
		streamingRoom,
		config,
		signaling,
		logger,
		metrics,
	)

	return &MeshCallRoom{baseCallRoom: base}
}

// JoinCall joins the mesh call
func (r *MeshCallRoom) JoinCall(ctx context.Context, userID string, opts *JoinOptions) (PeerConnection, error) {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	// Check if user already in call
	if _, exists := r.peers[userID]; exists {
		return nil, fmt.Errorf("webrtc: user %s already in call", userID)
	}

	// Generate peer ID
	peerID := fmt.Sprintf("mesh-peer-%s-%d", userID, time.Now().UnixNano())

	// Create peer connection
	peer, err := NewPeerConnection(peerID, userID, r.config, r.logger)
	if err != nil {
		return nil, fmt.Errorf("webrtc: failed to create peer connection: %w", err)
	}

	// Add local media tracks if requested
	if opts != nil {
		if opts.AudioEnabled {
			audioTrack, err := NewLocalTrack(TrackKindAudio, fmt.Sprintf("audio-%s", peerID), "audio", r.logger)
			if err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to create audio track: %w", err)
			}
			if err := peer.AddTrack(ctx, audioTrack); err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to add audio track: %w", err)
			}
		}

		if opts.VideoEnabled {
			videoTrack, err := NewLocalTrack(TrackKindVideo, fmt.Sprintf("video-%s", peerID), "video", r.logger)
			if err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to create video track: %w", err)
			}
			if err := peer.AddTrack(ctx, videoTrack); err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to add video track: %w", err)
			}
		}
	}

	// Setup ICE candidate handler to forward via signaling
	peer.OnICECandidate(func(candidate *ICECandidate) {
		r.logger.Debug("ICE candidate generated",
			forge.F("peer_id", peerID),
			forge.F("user_id", userID),
		)
		// Forward to signaling manager
		if err := r.signaling.SendICECandidate(ctx, r.ID(), userID, candidate); err != nil {
			r.logger.Error("failed to send ICE candidate", forge.F("error", err))
		}
	})

	// Setup track handler for receiving remote tracks
	peer.OnTrack(func(track MediaTrack, receiver *TrackReceiver) {
		r.logger.Info("received remote track",
			forge.F("peer_id", peerID),
			forge.F("track_id", track.ID()),
			forge.F("kind", track.Kind()),
		)
	})

	// Setup connection state handler
	peer.OnConnectionStateChange(func(state ConnectionState) {
		r.logger.Info("peer connection state changed",
			forge.F("peer_id", peerID),
			forge.F("state", state),
		)

		// Track metrics
		if r.metrics != nil {
			r.metrics.Gauge("webrtc.peer.state",
				"state", fmt.Sprintf("%d", stateToInt(state)),
				"peer_id", peerID,
			).Set(float64(stateToInt(state)))
		}
	})

	// Store peer
	r.peers[userID] = peer

	r.logger.Info("user joined mesh call",
		forge.F("room_id", r.ID()),
		forge.F("user_id", userID),
		forge.F("peer_id", peerID),
		forge.F("total_peers", len(r.peers)),
	)

	// Track metrics
	if r.metrics != nil {
		r.metrics.Gauge("webrtc.mesh.peers", "count", fmt.Sprintf("%d", len(r.peers))).Set(float64(len(r.peers)))
	}

	return peer, nil
}

func stateToInt(state ConnectionState) int {
	switch state {
	case ConnectionStateNew:
		return 0
	case ConnectionStateConnecting:
		return 1
	case ConnectionStateConnected:
		return 2
	case ConnectionStateDisconnected:
		return 3
	case ConnectionStateFailed:
		return 4
	case ConnectionStateClosed:
		return 5
	default:
		return -1
	}
}

// SFUCallRoom implements CallRoom with SFU topology
type SFUCallRoom struct {
	*baseCallRoom
	router SFURouter
}

// NewSFUCallRoom creates a call room with SFU topology
func NewSFUCallRoom(
	streamingRoom streaming.Room,
	config Config,
	signaling SignalingManager,
	router SFURouter,
	logger forge.Logger,
	metrics forge.Metrics,
) CallRoom {
	roomID := fmt.Sprintf("sfu-%s", streamingRoom.GetID())

	base := newBaseCallRoom(
		roomID,
		"SFU Call Room",
		streamingRoom,
		config,
		signaling,
		logger,
		metrics,
	)

	return &SFUCallRoom{
		baseCallRoom: base,
		router:       router,
	}
}

// JoinCall joins the SFU call
func (r *SFUCallRoom) JoinCall(ctx context.Context, userID string, opts *JoinOptions) (PeerConnection, error) {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	// Check if user already in call
	if _, exists := r.peers[userID]; exists {
		return nil, fmt.Errorf("webrtc: user %s already in call", userID)
	}

	// Generate peer ID
	peerID := fmt.Sprintf("sfu-peer-%s-%d", userID, time.Now().UnixNano())

	// Create peer connection
	peer, err := NewPeerConnection(peerID, userID, r.config, r.logger)
	if err != nil {
		return nil, fmt.Errorf("webrtc: failed to create peer connection: %w", err)
	}

	// Determine if this user is publisher or subscriber
	isPublisher := opts != nil && (opts.AudioEnabled || opts.VideoEnabled)

	if isPublisher {
		// Add as publisher to SFU
		if err := r.router.AddPublisher(ctx, userID, peer); err != nil {
			peer.Close(ctx)
			return nil, fmt.Errorf("webrtc: failed to add publisher: %w", err)
		}

		// Add local media tracks
		if opts.AudioEnabled {
			audioTrack, err := NewLocalTrack(TrackKindAudio, fmt.Sprintf("audio-%s", peerID), "audio", r.logger)
			if err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to create audio track: %w", err)
			}
			if err := peer.AddTrack(ctx, audioTrack); err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to add audio track: %w", err)
			}
		}

		if opts.VideoEnabled {
			videoTrack, err := NewLocalTrack(TrackKindVideo, fmt.Sprintf("video-%s", peerID), "video", r.logger)
			if err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to create video track: %w", err)
			}
			if err := peer.AddTrack(ctx, videoTrack); err != nil {
				peer.Close(ctx)
				return nil, fmt.Errorf("webrtc: failed to add video track: %w", err)
			}
		}
	} else {
		// Add as subscriber to SFU
		if err := r.router.AddSubscriber(ctx, userID, peer); err != nil {
			peer.Close(ctx)
			return nil, fmt.Errorf("webrtc: failed to add subscriber: %w", err)
		}

		// Subscribe to all available tracks
		availableTracks := r.router.GetAvailableTracks()
		for _, trackInfo := range availableTracks {
			if err := r.router.SubscribeToTrack(ctx, userID, trackInfo.PublisherID, trackInfo.TrackID); err != nil {
				r.logger.Error("failed to subscribe to track",
					forge.F("user_id", userID),
					forge.F("track_id", trackInfo.TrackID),
					forge.F("error", err),
				)
			}
		}
	}

	// Setup quality monitoring
	qualityConfig := DefaultQualityConfig()
	monitor := NewQualityMonitor(peerID, peer, qualityConfig, r.logger, r.metrics)
	if err := monitor.Monitor(ctx, peer); err != nil {
		r.logger.Error("failed to start quality monitor", forge.F("error", err))
	} else {
		r.monitorsMu.Lock()
		r.qualityMonitors[userID] = monitor
		r.monitorsMu.Unlock()
	}

	// Store peer
	r.peers[userID] = peer

	r.logger.Info("user joined SFU call",
		forge.F("room_id", r.ID()),
		forge.F("user_id", userID),
		forge.F("peer_id", peerID),
		forge.F("is_publisher", isPublisher),
		forge.F("total_peers", len(r.peers)),
	)

	// Track metrics
	if r.metrics != nil {
		r.metrics.Gauge("webrtc.sfu.peers", "count", fmt.Sprintf("%d", len(r.peers))).Set(float64(len(r.peers)))
	}

	return peer, nil
}
