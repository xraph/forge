package webrtc

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// sfuRouter implements SFURouter for selective forwarding.
type sfuRouter struct {
	id      string
	roomID  string
	logger  forge.Logger
	metrics forge.Metrics

	// Track forwarding
	publishers  map[string]*sfuPublisher  // userID -> publisher
	subscribers map[string]*sfuSubscriber // userID -> subscriber
	tracks      map[string]*sfuTrack      // trackID -> track metadata

	// Simulcast layers
	simulcastLayers map[string][]*SimulcastLayer // trackID -> layers

	mu sync.RWMutex
}

// sfuPublisher represents a publishing peer.
type sfuPublisher struct {
	userID string
	peer   PeerConnection
	tracks map[string]*sfuTrack // trackID -> track
}

// sfuSubscriber represents a subscribing peer.
type sfuSubscriber struct {
	userID        string
	peer          PeerConnection
	subscriptions map[string]*sfuSubscription // trackID -> subscription
}

// sfuSubscription represents a track subscription.
type sfuSubscription struct {
	trackID          string
	publisherID      string
	selectedLayer    int // Simulcast layer index
	lastPacketTime   time.Time
	packetsForwarded uint64
}

// sfuTrack represents a track in the SFU.
type sfuTrack struct {
	id          string
	publisherID string
	kind        TrackKind
	remoteTrack *webrtc.TrackRemote
	localTrack  webrtc.TrackLocal

	// RTP forwarding
	rtpSender *webrtc.RTPSender

	// Simulcast support
	simulcastLayers []*SimulcastLayer

	// Stats
	packetsReceived uint64
	bytesReceived   uint64
	packetsDropped  uint64

	mu sync.RWMutex
}

// NewSFURouter creates a new SFU router.
func NewSFURouter(roomID string, logger forge.Logger, metrics forge.Metrics) SFURouter {
	return &sfuRouter{
		id:              "sfu-" + roomID,
		roomID:          roomID,
		logger:          logger,
		metrics:         metrics,
		publishers:      make(map[string]*sfuPublisher),
		subscribers:     make(map[string]*sfuSubscriber),
		tracks:          make(map[string]*sfuTrack),
		simulcastLayers: make(map[string][]*SimulcastLayer),
	}
}

// RouteTrack routes a track from sender to receivers.
func (s *sfuRouter) RouteTrack(ctx context.Context, senderID string, track MediaTrack, receiverIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the sender
	sender, exists := s.publishers[senderID]
	if !exists {
		return fmt.Errorf("sender not found: %s", senderID)
	}

	// Store the track
	sfuTrack := &sfuTrack{
		id:          track.ID(),
		publisherID: senderID,
		kind:        track.Kind(),
	}

	sender.tracks[track.ID()] = sfuTrack
	s.tracks[track.ID()] = sfuTrack

	// Route to all receivers
	for _, receiverID := range receiverIDs {
		if err := s.AddReceiver(ctx, track.ID(), receiverID); err != nil {
			s.logger.Error("failed to add receiver",
				forge.F("track_id", track.ID()),
				forge.F("receiver_id", receiverID),
				forge.F("error", err),
			)
		}
	}

	s.logger.Info("routed track to receivers",
		forge.F("track_id", track.ID()),
		forge.F("sender_id", senderID),
		forge.F("receiver_count", len(receiverIDs)),
	)

	return nil
}

// AddPublisher adds a publishing peer.
func (s *sfuRouter) AddPublisher(ctx context.Context, userID string, peer PeerConnection) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.publishers[userID]; exists {
		return fmt.Errorf("publisher already exists: %s", userID)
	}

	publisher := &sfuPublisher{
		userID: userID,
		peer:   peer,
		tracks: make(map[string]*sfuTrack),
	}

	s.publishers[userID] = publisher

	// Setup track handler to receive tracks from this publisher
	peer.OnTrack(func(track MediaTrack, receiver *TrackReceiver) {
		s.handlePublisherTrack(ctx, userID, track, receiver)
	})

	s.logger.Info("added SFU publisher",
		forge.F("room_id", s.roomID),
		forge.F("user_id", userID),
		forge.F("total_publishers", len(s.publishers)),
	)

	if s.metrics != nil {
		s.metrics.Gauge("webrtc.sfu.publishers", forge.WithLabel("count", strconv.Itoa(len(s.publishers)))).Set(float64(len(s.publishers)))
	}

	return nil
}

// AddSubscriber adds a subscribing peer.
func (s *sfuRouter) AddSubscriber(ctx context.Context, userID string, peer PeerConnection) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscribers[userID]; exists {
		return fmt.Errorf("subscriber already exists: %s", userID)
	}

	subscriber := &sfuSubscriber{
		userID:        userID,
		peer:          peer,
		subscriptions: make(map[string]*sfuSubscription),
	}

	s.subscribers[userID] = subscriber

	s.logger.Info("added SFU subscriber",
		forge.F("room_id", s.roomID),
		forge.F("user_id", userID),
		forge.F("total_subscribers", len(s.subscribers)),
	)

	if s.metrics != nil {
		s.metrics.Gauge("webrtc.sfu.subscribers", forge.WithLabel("count", strconv.Itoa(len(s.subscribers)))).Set(float64(len(s.subscribers)))
	}

	return nil
}

// RemovePublisher removes a publishing peer.
func (s *sfuRouter) RemovePublisher(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	publisher, exists := s.publishers[userID]
	if !exists {
		return fmt.Errorf("publisher not found: %s", userID)
	}

	// Remove all tracks from this publisher
	for trackID := range publisher.tracks {
		delete(s.tracks, trackID)
		delete(s.simulcastLayers, trackID)
	}

	delete(s.publishers, userID)

	s.logger.Info("removed SFU publisher",
		forge.F("room_id", s.roomID),
		forge.F("user_id", userID),
		forge.F("tracks_removed", len(publisher.tracks)),
	)

	if s.metrics != nil {
		s.metrics.Gauge("webrtc.sfu.publishers", forge.WithLabel("count", strconv.Itoa(len(s.publishers)))).Set(float64(len(s.publishers)))
	}

	return nil
}

// RemoveSubscriber removes a subscribing peer.
func (s *sfuRouter) RemoveSubscriber(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriber, exists := s.subscribers[userID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", userID)
	}

	delete(s.subscribers, userID)

	s.logger.Info("removed SFU subscriber",
		forge.F("room_id", s.roomID),
		forge.F("user_id", userID),
		forge.F("subscriptions", len(subscriber.subscriptions)),
	)

	if s.metrics != nil {
		s.metrics.Gauge("webrtc.sfu.subscribers", forge.WithLabel("count", strconv.Itoa(len(s.subscribers)))).Set(float64(len(s.subscribers)))
	}

	return nil
}

// SubscribeToTrack subscribes a peer to a publisher's track.
func (s *sfuRouter) SubscribeToTrack(ctx context.Context, subscriberID, publisherID, trackID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriber, exists := s.subscribers[subscriberID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", subscriberID)
	}

	track, exists := s.tracks[trackID]
	if !exists {
		return fmt.Errorf("track not found: %s", trackID)
	}

	if track.publisherID != publisherID {
		return errors.New("track does not belong to publisher")
	}

	// Check if already subscribed
	if _, exists := subscriber.subscriptions[trackID]; exists {
		return fmt.Errorf("already subscribed to track: %s", trackID)
	}

	// Create subscription
	subscription := &sfuSubscription{
		trackID:        trackID,
		publisherID:    publisherID,
		selectedLayer:  0, // Start with lowest quality layer
		lastPacketTime: time.Now(),
	}

	subscriber.subscriptions[trackID] = subscription

	// Forward the track to the subscriber
	if err := s.forwardTrack(ctx, track, subscriber.peer); err != nil {
		delete(subscriber.subscriptions, trackID)

		return fmt.Errorf("failed to forward track: %w", err)
	}

	s.logger.Info("subscribed to track",
		forge.F("subscriber_id", subscriberID),
		forge.F("publisher_id", publisherID),
		forge.F("track_id", trackID),
	)

	if s.metrics != nil {
		s.metrics.Counter("webrtc.sfu.subscriptions", forge.WithLabel("subscriber_id", subscriberID), forge.WithLabel("track_id", trackID)).Inc()
	}

	return nil
}

// UnsubscribeFromTrack unsubscribes a peer from a track.
func (s *sfuRouter) UnsubscribeFromTrack(ctx context.Context, subscriberID, trackID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriber, exists := s.subscribers[subscriberID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", subscriberID)
	}

	if _, exists := subscriber.subscriptions[trackID]; !exists {
		return fmt.Errorf("not subscribed to track: %s", trackID)
	}

	delete(subscriber.subscriptions, trackID)

	s.logger.Info("unsubscribed from track",
		forge.F("subscriber_id", subscriberID),
		forge.F("track_id", trackID),
	)

	if s.metrics != nil {
		s.metrics.Counter("webrtc.sfu.unsubscriptions", forge.WithLabel("subscriber_id", subscriberID), forge.WithLabel("track_id", trackID)).Inc()
	}

	return nil
}

// AddReceiver adds a receiver for a track.
func (s *sfuRouter) AddReceiver(ctx context.Context, trackID, receiverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the track
	track, exists := s.tracks[trackID]
	if !exists {
		return fmt.Errorf("track not found: %s", trackID)
	}

	// Find the subscriber
	subscriber, exists := s.subscribers[receiverID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", receiverID)
	}

	// Create subscription
	subscription := &sfuSubscription{
		trackID:        trackID,
		publisherID:    track.publisherID,
		selectedLayer:  0,
		lastPacketTime: time.Now(),
	}

	subscriber.subscriptions[trackID] = subscription

	s.logger.Info("added receiver for track",
		forge.F("track_id", trackID),
		forge.F("receiver_id", receiverID),
	)

	return nil
}

// RemoveReceiver removes a receiver.
func (s *sfuRouter) RemoveReceiver(ctx context.Context, trackID, receiverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriber, exists := s.subscribers[receiverID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", receiverID)
	}

	if _, exists := subscriber.subscriptions[trackID]; !exists {
		return fmt.Errorf("not subscribed to track: %s", trackID)
	}

	delete(subscriber.subscriptions, trackID)

	s.logger.Info("removed receiver for track",
		forge.F("track_id", trackID),
		forge.F("receiver_id", receiverID),
	)

	return nil
}

// GetReceivers returns all receivers for a track.
func (s *sfuRouter) GetReceivers(trackID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var receivers []string

	for subscriberID, subscriber := range s.subscribers {
		if _, exists := subscriber.subscriptions[trackID]; exists {
			receivers = append(receivers, subscriberID)
		}
	}

	return receivers
}

// SetQuality sets quality layer for receiver.
func (s *sfuRouter) SetQuality(ctx context.Context, trackID, receiverID, quality string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriber, exists := s.subscribers[receiverID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", receiverID)
	}

	subscription, exists := subscriber.subscriptions[trackID]
	if !exists {
		return fmt.Errorf("not subscribed to track: %s", trackID)
	}

	// Map quality string to layer index
	var layerIndex int

	switch quality {
	case "low":
		layerIndex = 0
	case "medium":
		layerIndex = 1
	case "high":
		layerIndex = 2
	default:
		return fmt.Errorf("invalid quality: %s", quality)
	}

	subscription.selectedLayer = layerIndex

	s.logger.Info("set quality for receiver",
		forge.F("track_id", trackID),
		forge.F("receiver_id", receiverID),
		forge.F("quality", quality),
	)

	return nil
}

// GetAvailableTracks returns all available tracks.
func (s *sfuRouter) GetAvailableTracks() []TrackInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tracks := make([]TrackInfo, 0, len(s.tracks))
	for _, track := range s.tracks {
		tracks = append(tracks, TrackInfo{
			TrackID:     track.id,
			PublisherID: track.publisherID,
			Kind:        track.kind,
			// Additional metadata would go here
		})
	}

	return tracks
}

// GetStats returns SFU statistics.
func (s *sfuRouter) GetStats(ctx context.Context) (*RouterStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Count total subscriptions
	totalSubscriptions := 0
	for _, subscriber := range s.subscribers {
		totalSubscriptions += len(subscriber.subscriptions)
	}

	// Aggregate track stats
	var totalBytesReceived uint64

	for _, track := range s.tracks {
		track.mu.RLock()
		totalBytesReceived += track.bytesReceived
		track.mu.RUnlock()
	}

	stats := &RouterStats{
		TotalTracks:        len(s.tracks),
		ActiveReceivers:    totalSubscriptions,
		TotalBytesSent:     0, // TODO: Track bytes sent
		TotalBytesReceived: totalBytesReceived,
		AverageBitrate:     int(float64(totalBytesReceived) / time.Since(time.Now()).Seconds()),
	}

	return stats, nil
}

// handlePublisherTrack handles a new track from a publisher.
func (s *sfuRouter) handlePublisherTrack(ctx context.Context, userID string, track MediaTrack, receiver *TrackReceiver) {
	s.mu.Lock()
	defer s.mu.Unlock()

	publisher, exists := s.publishers[userID]
	if !exists {
		s.logger.Error("publisher not found for track",
			forge.F("user_id", userID),
			forge.F("track_id", track.ID()),
		)

		return
	}

	// Create SFU track
	sfuTrack := &sfuTrack{
		id:          track.ID(),
		publisherID: userID,
		kind:        track.Kind(),
	}

	// Store track
	publisher.tracks[track.ID()] = sfuTrack
	s.tracks[track.ID()] = sfuTrack

	s.logger.Info("received publisher track",
		forge.F("user_id", userID),
		forge.F("track_id", track.ID()),
		forge.F("kind", track.Kind()),
	)

	if s.metrics != nil {
		s.metrics.Gauge("webrtc.sfu.tracks", forge.WithLabel("count", strconv.Itoa(len(s.tracks)))).Set(float64(len(s.tracks)))
	}

	// TODO: Start RTP packet forwarding loop
	// This would read from track.ReadRTP() and forward to all subscribers
}

// forwardTrack forwards a track to a subscriber's peer connection.
func (s *sfuRouter) forwardTrack(ctx context.Context, track *sfuTrack, peer PeerConnection) error {
	// Create a local track to send to the subscriber
	var (
		pionTrack webrtc.TrackLocal
		err       error
	)

	if track.kind == TrackKindAudio {
		pionTrack, err = webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
			track.id,
			"stream-"+track.publisherID,
		)
	} else {
		pionTrack, err = webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
			track.id,
			"stream-"+track.publisherID,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to create local track: %w", err)
	}

	fmt.Println("pionTrack", pionTrack)

	// Add track to subscriber's peer connection
	// Note: This requires casting peer to *peerConnection to access the underlying webrtc.PeerConnection
	// In production, you'd want a more elegant way to do this

	s.logger.Debug("forwarding track to subscriber",
		forge.F("track_id", track.id),
		forge.F("kind", track.kind),
	)

	// TODO: Start packet forwarding from publisher's track to this local track
	// This involves reading RTP packets from the publisher and writing to this track

	return nil
}

// Close closes the SFU router.
func (s *sfuRouter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("closing SFU router",
		forge.F("room_id", s.roomID),
		forge.F("publishers", len(s.publishers)),
		forge.F("subscribers", len(s.subscribers)),
	)

	// Clear all maps
	s.publishers = make(map[string]*sfuPublisher)
	s.subscribers = make(map[string]*sfuSubscriber)
	s.tracks = make(map[string]*sfuTrack)
	s.simulcastLayers = make(map[string][]*SimulcastLayer)

	return nil
}

// Helper types for SFU stats.
type TrackInfo struct {
	TrackID     string
	PublisherID string
	Kind        TrackKind
}
