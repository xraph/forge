package webrtc

import (
	"context"
	"fmt"
	"sync"

	"github.com/pion/webrtc/v3"
	"github.com/xraph/forge"
)

// peerConnection implements PeerConnection using pion/webrtc
type peerConnection struct {
	id     string
	userID string
	pc     *webrtc.PeerConnection

	// Tracks
	tracks   []MediaTrack
	tracksMu sync.RWMutex

	// Event handlers
	iceHandler         ICECandidateHandler
	trackHandler       TrackHandler
	stateHandler       ConnectionStateHandler
	dataChannelHandler DataChannelHandler

	// State
	state  ConnectionState
	logger forge.Logger

	mu sync.RWMutex
}

// NewPeerConnection creates a new peer connection
func NewPeerConnection(id, userID string, config Config, logger forge.Logger) (PeerConnection, error) {
	// Build pion config
	pionConfig := webrtc.Configuration{
		ICEServers: convertICEServers(config.GetICEServers()),
	}

	// Create pion peer connection
	pc, err := webrtc.NewPeerConnection(pionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	peer := &peerConnection{
		id:     id,
		userID: userID,
		pc:     pc,
		tracks: make([]MediaTrack, 0),
		state:  ConnectionStateNew,
		logger: logger,
	}

	// Setup pion event handlers
	peer.setupEventHandlers()

	return peer, nil
}

// setupEventHandlers sets up pion event handlers
func (p *peerConnection) setupEventHandlers() {
	// ICE candidate handler
	p.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil && p.iceHandler != nil {
			iceCandidate := &ICECandidate{
				Candidate:        candidate.ToJSON().Candidate,
				SDPMid:           *candidate.ToJSON().SDPMid,
				SDPMLineIndex:    int(*candidate.ToJSON().SDPMLineIndex),
				UsernameFragment: candidate.ToJSON().UsernameFragment,
			}
			p.iceHandler(iceCandidate)
		}
	})

	// Connection state change handler
	p.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.mu.Lock()
		p.state = convertPionConnectionState(state)
		p.mu.Unlock()

		if p.stateHandler != nil {
			p.stateHandler(p.state)
		}

		p.logger.Debug("peer connection state changed",
			forge.F("peer_id", p.id),
			forge.F("user_id", p.userID),
			forge.F("state", state.String()),
		)
	})

	// Track handler
	p.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		p.logger.Debug("received remote track",
			forge.F("peer_id", p.id),
			forge.F("track_id", track.ID()),
			forge.F("kind", track.Kind().String()),
		)

		if p.trackHandler != nil {
			mediaTrack := &remoteMediaTrack{
				track:  track,
				logger: p.logger,
			}

			trackReceiver := &TrackReceiver{
				TrackID:  track.ID(),
				PeerID:   p.id,
				Kind:     TrackKind(track.Kind().String()),
				Settings: TrackSettings{
					// Will be populated from track capabilities
				},
			}

			p.trackHandler(mediaTrack, trackReceiver)
		}
	})

	// Data channel handler
	p.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		p.logger.Debug("received data channel",
			forge.F("peer_id", p.id),
			forge.F("channel_id", *dc.ID()),
			forge.F("label", dc.Label()),
		)

		if p.dataChannelHandler != nil {
			dataChannel := &dataChannel{
				dc:     dc,
				logger: p.logger,
			}
			p.dataChannelHandler(dataChannel)
		}
	})
}

// ID returns the peer connection ID
func (p *peerConnection) ID() string {
	return p.id
}

// UserID returns the user ID
func (p *peerConnection) UserID() string {
	return p.userID
}

// State returns current connection state
func (p *peerConnection) State() ConnectionState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// CreateOffer creates an SDP offer
func (p *peerConnection) CreateOffer(ctx context.Context) (*SessionDescription, error) {
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create offer: %w", err)
	}

	return &SessionDescription{
		Type: SessionDescriptionTypeOffer,
		SDP:  offer.SDP,
	}, nil
}

// CreateAnswer creates an SDP answer
func (p *peerConnection) CreateAnswer(ctx context.Context) (*SessionDescription, error) {
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	return &SessionDescription{
		Type: SessionDescriptionTypeAnswer,
		SDP:  answer.SDP,
	}, nil
}

// SetLocalDescription sets local SDP
func (p *peerConnection) SetLocalDescription(ctx context.Context, sdp *SessionDescription) error {
	desc := webrtc.SessionDescription{
		Type: convertToWebRTCSDPType(sdp.Type),
		SDP:  sdp.SDP,
	}

	if err := p.pc.SetLocalDescription(desc); err != nil {
		return fmt.Errorf("failed to set local description: %w", err)
	}

	p.logger.Debug("set local description",
		forge.F("peer_id", p.id),
		forge.F("type", sdp.Type),
	)

	return nil
}

// SetRemoteDescription sets remote SDP
func (p *peerConnection) SetRemoteDescription(ctx context.Context, sdp *SessionDescription) error {
	desc := webrtc.SessionDescription{
		Type: convertToWebRTCSDPType(sdp.Type),
		SDP:  sdp.SDP,
	}

	if err := p.pc.SetRemoteDescription(desc); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	p.logger.Debug("set remote description",
		forge.F("peer_id", p.id),
		forge.F("type", sdp.Type),
	)

	return nil
}

// AddICECandidate adds an ICE candidate
func (p *peerConnection) AddICECandidate(ctx context.Context, candidate *ICECandidate) error {
	iceCandidate := webrtc.ICECandidateInit{
		Candidate:        candidate.Candidate,
		SDPMid:           &candidate.SDPMid,
		SDPMLineIndex:    uint16Ptr(uint16(candidate.SDPMLineIndex)),
		UsernameFragment: &candidate.UsernameFragment,
	}

	if err := p.pc.AddICECandidate(iceCandidate); err != nil {
		return fmt.Errorf("failed to add ICE candidate: %w", err)
	}

	return nil
}

// AddTrack adds a media track
func (p *peerConnection) AddTrack(ctx context.Context, track MediaTrack) error {
	p.tracksMu.Lock()
	defer p.tracksMu.Unlock()

	// For local tracks, we need to add them to pion
	if localTrack, ok := track.(*localMediaTrack); ok {
		sender, err := p.pc.AddTrack(localTrack.track)
		if err != nil {
			return fmt.Errorf("failed to add track: %w", err)
		}

		localTrack.sender = sender
		p.tracks = append(p.tracks, track)

		p.logger.Debug("added track",
			forge.F("peer_id", p.id),
			forge.F("track_id", track.ID()),
			forge.F("kind", track.Kind()),
		)

		return nil
	}

	return fmt.Errorf("can only add local tracks")
}

// RemoveTrack removes a media track
func (p *peerConnection) RemoveTrack(ctx context.Context, trackID string) error {
	p.tracksMu.Lock()
	defer p.tracksMu.Unlock()

	for i, track := range p.tracks {
		if track.ID() == trackID {
			if localTrack, ok := track.(*localMediaTrack); ok && localTrack.sender != nil {
				if err := p.pc.RemoveTrack(localTrack.sender); err != nil {
					return fmt.Errorf("failed to remove track: %w", err)
				}
			}

			// Remove from slice
			p.tracks = append(p.tracks[:i], p.tracks[i+1:]...)

			p.logger.Debug("removed track",
				forge.F("peer_id", p.id),
				forge.F("track_id", trackID),
			)

			return nil
		}
	}

	return fmt.Errorf("track not found: %s", trackID)
}

// GetTracks returns all media tracks
func (p *peerConnection) GetTracks() []MediaTrack {
	p.tracksMu.RLock()
	defer p.tracksMu.RUnlock()

	tracks := make([]MediaTrack, len(p.tracks))
	copy(tracks, p.tracks)
	return tracks
}

// GetStats returns connection statistics
func (p *peerConnection) GetStats(ctx context.Context) (*PeerStats, error) {
	stats := p.pc.GetStats()

	peerStats := &PeerStats{
		PeerID:          p.id,
		ConnectionState: p.State(),
	}

	// Parse stats from pion
	for _, stat := range stats {
		switch s := stat.(type) {
		case *webrtc.ICECandidatePairStats:
			if s.State == webrtc.StatsICECandidatePairStateSucceeded {
				peerStats.LocalCandidateType = s.LocalCandidateID
				peerStats.RemoteCandidateType = s.RemoteCandidateID
			}

		case *webrtc.TransportStats:
			peerStats.BytesSent = s.BytesSent
			peerStats.BytesReceived = s.BytesReceived
			peerStats.PacketsSent = uint64(s.PacketsSent)
			peerStats.PacketsReceived = uint64(s.PacketsReceived)
		}
	}

	return peerStats, nil
}

// Close closes the peer connection
func (p *peerConnection) Close(ctx context.Context) error {
	if err := p.pc.Close(); err != nil {
		return fmt.Errorf("failed to close peer connection: %w", err)
	}

	p.logger.Debug("closed peer connection",
		forge.F("peer_id", p.id),
		forge.F("user_id", p.userID),
	)

	return nil
}

// OnICECandidate sets ICE candidate callback
func (p *peerConnection) OnICECandidate(handler ICECandidateHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.iceHandler = handler
}

// OnTrack sets track received callback
func (p *peerConnection) OnTrack(handler TrackHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trackHandler = handler
}

// OnConnectionStateChange sets state change callback
func (p *peerConnection) OnConnectionStateChange(handler ConnectionStateHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stateHandler = handler
}

// OnDataChannel sets data channel callback
func (p *peerConnection) OnDataChannel(handler DataChannelHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dataChannelHandler = handler
}

// Helper functions

func convertICEServers(servers []ICEServer) []webrtc.ICEServer {
	result := make([]webrtc.ICEServer, len(servers))
	for i, server := range servers {
		result[i] = webrtc.ICEServer{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: server.Credential,
		}
	}
	return result
}

func convertPionConnectionState(state webrtc.PeerConnectionState) ConnectionState {
	switch state {
	case webrtc.PeerConnectionStateNew:
		return ConnectionStateNew
	case webrtc.PeerConnectionStateConnecting:
		return ConnectionStateConnecting
	case webrtc.PeerConnectionStateConnected:
		return ConnectionStateConnected
	case webrtc.PeerConnectionStateDisconnected:
		return ConnectionStateDisconnected
	case webrtc.PeerConnectionStateFailed:
		return ConnectionStateFailed
	case webrtc.PeerConnectionStateClosed:
		return ConnectionStateClosed
	default:
		return ConnectionStateNew
	}
}

func convertToWebRTCSDPType(sdpType SessionDescriptionType) webrtc.SDPType {
	switch sdpType {
	case SessionDescriptionTypeOffer:
		return webrtc.SDPTypeOffer
	case SessionDescriptionTypeAnswer:
		return webrtc.SDPTypeAnswer
	default:
		return webrtc.SDPTypeOffer
	}
}

func uint16Ptr(v uint16) *uint16 {
	return &v
}
