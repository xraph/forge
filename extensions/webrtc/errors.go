package webrtc

import "errors"

// Common errors.
var (
	// Configuration errors.
	ErrInvalidConfig       = errors.New("webrtc: invalid configuration")
	ErrNoICEServers        = errors.New("webrtc: no ICE servers configured")
	ErrInvalidBitrateRange = errors.New("webrtc: invalid bitrate range")

	// Connection errors.
	ErrPeerNotFound           = errors.New("webrtc: peer not found")
	ErrPeerAlreadyExists      = errors.New("webrtc: peer already exists")
	ErrConnectionFailed       = errors.New("webrtc: connection failed")
	ErrConnectionClosed       = errors.New("webrtc: connection closed")
	ErrInvalidConnectionState = errors.New("webrtc: invalid connection state")

	// Signaling errors.
	ErrSignalingFailed  = errors.New("webrtc: signaling failed")
	ErrInvalidSDP       = errors.New("webrtc: invalid SDP")
	ErrInvalidCandidate = errors.New("webrtc: invalid ICE candidate")
	ErrSignalingTimeout = errors.New("webrtc: signaling timeout")

	// Media errors.
	ErrTrackNotFound     = errors.New("webrtc: track not found")
	ErrInvalidTrackKind  = errors.New("webrtc: invalid track kind")
	ErrMediaNotSupported = errors.New("webrtc: media type not supported")
	ErrCodecNotSupported = errors.New("webrtc: codec not supported")

	// Room errors.
	ErrRoomNotFound  = errors.New("webrtc: room not found")
	ErrRoomFull      = errors.New("webrtc: room is full")
	ErrNotInRoom     = errors.New("webrtc: not in room")
	ErrAlreadyInRoom = errors.New("webrtc: already in room")

	// Data channel errors.
	ErrDataChannelClosed = errors.New("webrtc: data channel closed")
	ErrDataChannelFailed = errors.New("webrtc: data channel failed")

	// Recording errors.
	ErrRecordingFailed  = errors.New("webrtc: recording failed")
	ErrNotRecording     = errors.New("webrtc: not recording")
	ErrAlreadyRecording = errors.New("webrtc: already recording")

	// SFU errors.
	ErrSFUNotEnabled    = errors.New("webrtc: SFU not enabled")
	ErrRoutingFailed    = errors.New("webrtc: media routing failed")
	ErrReceiverNotFound = errors.New("webrtc: receiver not found")

	// Auth errors.
	ErrUnauthorized = errors.New("webrtc: unauthorized")
	ErrForbidden    = errors.New("webrtc: forbidden")

	// General errors.
	ErrNotImplemented = errors.New("webrtc: not implemented - requires additional setup")
)

// SignalingError wraps signaling errors with context.
type SignalingError struct {
	Op     string // Operation that failed
	PeerID string
	RoomID string
	Err    error
}

func (e *SignalingError) Error() string {
	return "webrtc signaling: " + e.Op + ": " + e.Err.Error()
}

func (e *SignalingError) Unwrap() error {
	return e.Err
}

// ConnectionError wraps connection errors with context.
type ConnectionError struct {
	PeerID string
	State  ConnectionState
	Err    error
}

func (e *ConnectionError) Error() string {
	return "webrtc connection: peer=" + e.PeerID + " state=" + string(e.State) + ": " + e.Err.Error()
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}

// MediaError wraps media errors with context.
type MediaError struct {
	TrackID string
	Kind    TrackKind
	Err     error
}

func (e *MediaError) Error() string {
	return "webrtc media: track=" + e.TrackID + " kind=" + string(e.Kind) + ": " + e.Err.Error()
}

func (e *MediaError) Unwrap() error {
	return e.Err
}
