package webrtc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)

// signalingManager implements SignalingManager using streaming extension
type signalingManager struct {
	streaming streaming.Manager
	logger    forge.Logger

	// Callbacks
	offerHandler        OfferHandler
	answerHandler       AnswerHandler
	iceCandidateHandler ICECandidateReceivedHandler

	mu      sync.RWMutex
	started bool
}

// NewSignalingManager creates a new signaling manager
func NewSignalingManager(streamingExt *streaming.Extension, logger forge.Logger) SignalingManager {
	return &signalingManager{
		streaming: streamingExt.Manager(),
		logger:    logger,
	}
}

// Message types for signaling
const (
	MessageTypeOffer        = "webrtc.offer"
	MessageTypeAnswer       = "webrtc.answer"
	MessageTypeICECandidate = "webrtc.ice_candidate"
)

// SendOffer sends an SDP offer to a peer via streaming
func (s *signalingManager) SendOffer(ctx context.Context, roomID, peerID string, offer *SessionDescription) error {
	msg := &streaming.Message{
		Type: MessageTypeOffer,
		Data: offer,
		Metadata: map[string]any{
			"peer_id": peerID,
		},
	}

	// TODO: Send via streaming - needs actual manager method
	if err := s.streaming.BroadcastToRoom(ctx, roomID, msg); err != nil {
		return &SignalingError{
			Op:     "send_offer",
			PeerID: peerID,
			RoomID: roomID,
			Err:    err,
		}
	}

	s.logger.Debug("sent offer",
		forge.F("room_id", roomID),
		forge.F("peer_id", peerID),
	)

	return nil
}

// SendAnswer sends an SDP answer to a peer via streaming
func (s *signalingManager) SendAnswer(ctx context.Context, roomID, peerID string, answer *SessionDescription) error {
	msg := &streaming.Message{
		Type: MessageTypeAnswer,
		Data: answer,
		Metadata: map[string]any{
			"peer_id": peerID,
		},
	}

	// TODO: Send via streaming - needs actual manager method
	if err := s.streaming.BroadcastToRoom(ctx, roomID, msg); err != nil {
		return &SignalingError{
			Op:     "send_answer",
			PeerID: peerID,
			RoomID: roomID,
			Err:    err,
		}
	}

	s.logger.Debug("sent answer",
		forge.F("room_id", roomID),
		forge.F("peer_id", peerID),
	)

	return nil
}

// SendICECandidate sends an ICE candidate to a peer via streaming
func (s *signalingManager) SendICECandidate(ctx context.Context, roomID, peerID string, candidate *ICECandidate) error {
	msg := &streaming.Message{
		Type: MessageTypeICECandidate,
		Data: candidate,
		Metadata: map[string]any{
			"peer_id": peerID,
		},
	}

	// TODO: Send via streaming - needs actual manager method
	if err := s.streaming.BroadcastToRoom(ctx, roomID, msg); err != nil {
		return &SignalingError{
			Op:     "send_ice_candidate",
			PeerID: peerID,
			RoomID: roomID,
			Err:    err,
		}
	}

	s.logger.Debug("sent ICE candidate",
		forge.F("room_id", roomID),
		forge.F("peer_id", peerID),
	)

	return nil
}

// OnOffer registers offer received callback
func (s *signalingManager) OnOffer(handler OfferHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offerHandler = handler
}

// OnAnswer registers answer received callback
func (s *signalingManager) OnAnswer(handler AnswerHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answerHandler = handler
}

// OnICECandidate registers ICE candidate received callback
func (s *signalingManager) OnICECandidate(handler ICECandidateReceivedHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iceCandidateHandler = handler
}

// Start begins listening for signaling messages
func (s *signalingManager) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	// Subscribe to signaling message types
	// This would integrate with streaming's message routing

	s.started = true
	s.logger.Info("signaling manager started")

	return nil
}

// Stop stops listening for signaling messages
func (s *signalingManager) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.started = false
	s.logger.Info("signaling manager stopped")

	return nil
}

// HandleConnection handles signaling for a specific connection
func (s *signalingManager) HandleConnection(ctx context.Context, roomID, userID string, peer PeerConnection, conn forge.Connection) error {
	s.logger.Debug("starting signaling handler",
		forge.F("room_id", roomID),
		forge.F("user_id", userID),
		forge.F("peer_id", peer.ID()),
	)

	// Listen for signaling messages from this connection with context cancellation
	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("signaling handler stopped",
				forge.F("room_id", roomID),
				forge.F("user_id", userID),
				forge.F("reason", ctx.Err()),
			)
			return ctx.Err()

		default:
			// TODO: Set read deadline based on context when Connection interface supports it
			// if deadline, ok := ctx.Deadline(); ok {
			// 	conn.SetReadDeadline(deadline)
			// }

			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				// Check if context was cancelled
				if ctx.Err() != nil {
					return ctx.Err()
				}

				s.logger.Error("failed to read signaling message",
					forge.F("room_id", roomID),
					forge.F("user_id", userID),
					forge.F("error", err),
				)
				return err
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				s.logger.Warn("signaling message missing type field",
					forge.F("room_id", roomID),
					forge.F("user_id", userID),
				)
				continue
			}

			// Handle message based on type
			switch msgType {
			case MessageTypeOffer:
				if err := s.handleOffer(ctx, roomID, userID, msg, peer); err != nil {
					s.logger.Error("failed to handle offer",
						forge.F("room_id", roomID),
						forge.F("user_id", userID),
						forge.F("error", err),
					)
				}

			case MessageTypeAnswer:
				if err := s.handleAnswer(ctx, roomID, userID, msg, peer); err != nil {
					s.logger.Error("failed to handle answer",
						forge.F("room_id", roomID),
						forge.F("user_id", userID),
						forge.F("error", err),
					)
				}

			case MessageTypeICECandidate:
				if err := s.handleICECandidate(ctx, roomID, userID, msg, peer); err != nil {
					s.logger.Error("failed to handle ICE candidate",
						forge.F("room_id", roomID),
						forge.F("user_id", userID),
						forge.F("error", err),
					)
				}

			default:
				s.logger.Warn("unknown signaling message type",
					forge.F("room_id", roomID),
					forge.F("user_id", userID),
					forge.F("type", msgType),
				)
			}
		}
	}
}

func (s *signalingManager) handleOffer(ctx context.Context, roomID, userID string, msg map[string]any, peer PeerConnection) error {
	// Parse SDP offer
	sdpData, ok := msg["sdp"].(map[string]any)
	if !ok {
		return fmt.Errorf("webrtc: invalid SDP format in offer: %w", ErrInvalidSDP)
	}

	sdpString, ok := sdpData["sdp"].(string)
	if !ok || sdpString == "" {
		return fmt.Errorf("webrtc: missing or invalid SDP string in offer: %w", ErrInvalidSDP)
	}

	offer := &SessionDescription{
		Type: SessionDescriptionTypeOffer,
		SDP:  sdpString,
	}

	s.logger.Debug("processing SDP offer",
		forge.F("room_id", roomID),
		forge.F("user_id", userID),
		forge.F("sdp_length", len(sdpString)),
	)

	// Set remote description
	if err := peer.SetRemoteDescription(ctx, offer); err != nil {
		return fmt.Errorf("webrtc: failed to set remote description for offer: %w", err)
	}

	// Create answer
	answer, err := peer.CreateAnswer(ctx)
	if err != nil {
		return fmt.Errorf("webrtc: failed to create answer: %w", err)
	}

	// Set local description
	if err := peer.SetLocalDescription(ctx, answer); err != nil {
		return fmt.Errorf("webrtc: failed to set local description for answer: %w", err)
	}

	s.logger.Debug("sending SDP answer",
		forge.F("room_id", roomID),
		forge.F("user_id", userID),
	)

	// Send answer back
	return s.SendAnswer(ctx, roomID, userID, answer)
}

func (s *signalingManager) handleAnswer(ctx context.Context, roomID, userID string, msg map[string]any, peer PeerConnection) error {
	// Parse SDP answer
	sdpData, ok := msg["sdp"].(map[string]any)
	if !ok {
		return fmt.Errorf("webrtc: invalid SDP format in answer: %w", ErrInvalidSDP)
	}

	sdpString, ok := sdpData["sdp"].(string)
	if !ok || sdpString == "" {
		return fmt.Errorf("webrtc: missing or invalid SDP string in answer: %w", ErrInvalidSDP)
	}

	answer := &SessionDescription{
		Type: SessionDescriptionTypeAnswer,
		SDP:  sdpString,
	}

	s.logger.Debug("processing SDP answer",
		forge.F("room_id", roomID),
		forge.F("user_id", userID),
		forge.F("sdp_length", len(sdpString)),
	)

	// Set remote description
	if err := peer.SetRemoteDescription(ctx, answer); err != nil {
		return fmt.Errorf("webrtc: failed to set remote description for answer: %w", err)
	}

	return nil
}

func (s *signalingManager) handleICECandidate(ctx context.Context, roomID, userID string, msg map[string]any, peer PeerConnection) error {
	// Parse ICE candidate
	candidateData, ok := msg["candidate"].(map[string]any)
	if !ok {
		return fmt.Errorf("webrtc: invalid candidate format: %w", ErrInvalidCandidate)
	}

	candidateStr, ok := candidateData["candidate"].(string)
	if !ok {
		return fmt.Errorf("webrtc: missing candidate string: %w", ErrInvalidCandidate)
	}

	sdpMid, _ := candidateData["sdpMid"].(string)
	sdpMLineIndex, _ := candidateData["sdpMLineIndex"].(float64)
	usernameFragment, _ := candidateData["usernameFragment"].(string)

	candidate := &ICECandidate{
		Candidate:        candidateStr,
		SDPMid:           sdpMid,
		SDPMLineIndex:    int(sdpMLineIndex),
		UsernameFragment: usernameFragment,
	}

	s.logger.Debug("processing ICE candidate",
		forge.F("room_id", roomID),
		forge.F("user_id", userID),
		forge.F("candidate", candidateStr[:min(len(candidateStr), 50)]), // Log first 50 chars
	)

	// Add ICE candidate
	if err := peer.AddICECandidate(ctx, candidate); err != nil {
		return fmt.Errorf("webrtc: failed to add ICE candidate: %w", err)
	}

	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseSessionDescription parses a session description from raw data
func parseSessionDescription(data any) (*SessionDescription, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var sdp SessionDescription
	if err := json.Unmarshal(jsonData, &sdp); err != nil {
		return nil, err
	}

	return &sdp, nil
}

// parseICECandidate parses an ICE candidate from raw data
func parseICECandidate(data any) (*ICECandidate, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var candidate ICECandidate
	if err := json.Unmarshal(jsonData, &candidate); err != nil {
		return nil, err
	}

	return &candidate, nil
}
