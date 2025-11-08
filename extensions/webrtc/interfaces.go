package webrtc

import (
	"context"
	"time"
)

// PeerConnection represents a WebRTC peer connection.
type PeerConnection interface {
	// ID returns the peer connection ID
	ID() string

	// UserID returns the user ID associated with this peer
	UserID() string

	// State returns the current connection state
	State() ConnectionState

	// CreateOffer creates an SDP offer
	CreateOffer(ctx context.Context) (*SessionDescription, error)

	// CreateAnswer creates an SDP answer
	CreateAnswer(ctx context.Context) (*SessionDescription, error)

	// SetLocalDescription sets local SDP
	SetLocalDescription(ctx context.Context, sdp *SessionDescription) error

	// SetRemoteDescription sets remote SDP
	SetRemoteDescription(ctx context.Context, sdp *SessionDescription) error

	// AddICECandidate adds an ICE candidate
	AddICECandidate(ctx context.Context, candidate *ICECandidate) error

	// AddTrack adds a media track
	AddTrack(ctx context.Context, track MediaTrack) error

	// RemoveTrack removes a media track
	RemoveTrack(ctx context.Context, trackID string) error

	// GetTracks returns all media tracks
	GetTracks() []MediaTrack

	// GetStats returns connection statistics
	GetStats(ctx context.Context) (*PeerStats, error)

	// Close closes the peer connection
	Close(ctx context.Context) error

	// OnICECandidate sets ICE candidate callback
	OnICECandidate(handler ICECandidateHandler)

	// OnTrack sets track received callback
	OnTrack(handler TrackHandler)

	// OnConnectionStateChange sets state change callback
	OnConnectionStateChange(handler ConnectionStateHandler)

	// OnDataChannel sets data channel callback
	OnDataChannel(handler DataChannelHandler)
}

// MediaTrack represents a media track (audio/video).
type MediaTrack interface {
	// ID returns the track ID
	ID() string

	// Kind returns track kind ("audio" or "video")
	Kind() TrackKind

	// Label returns track label
	Label() string

	// Enabled returns if track is enabled
	Enabled() bool

	// SetEnabled enables/disables track
	SetEnabled(enabled bool)

	// GetSettings returns track settings
	GetSettings() TrackSettings

	// GetStats returns track statistics
	GetStats(ctx context.Context) (*TrackStats, error)

	// Close stops the track
	Close() error
}

// DataChannel represents a WebRTC data channel.
type DataChannel interface {
	// ID returns channel ID
	ID() string

	// Label returns channel label
	Label() string

	// State returns channel state
	State() DataChannelState

	// Send sends data
	Send(data []byte) error

	// SendText sends text
	SendText(text string) error

	// OnMessage sets message callback
	OnMessage(handler DataChannelMessageHandler)

	// OnOpen sets open callback
	OnOpen(handler DataChannelStateHandler)

	// OnClose sets close callback
	OnClose(handler DataChannelStateHandler)

	// Close closes the channel
	Close() error
}

// SignalingManager handles WebRTC signaling via streaming.
type SignalingManager interface {
	// SendOffer sends SDP offer to peer
	SendOffer(ctx context.Context, roomID, peerID string, offer *SessionDescription) error

	// SendAnswer sends SDP answer to peer
	SendAnswer(ctx context.Context, roomID, peerID string, answer *SessionDescription) error

	// SendICECandidate sends ICE candidate to peer
	SendICECandidate(ctx context.Context, roomID, peerID string, candidate *ICECandidate) error

	// OnOffer sets offer received callback
	OnOffer(handler OfferHandler)

	// OnAnswer sets answer received callback
	OnAnswer(handler AnswerHandler)

	// OnICECandidate sets ICE candidate received callback
	OnICECandidate(handler ICECandidateReceivedHandler)

	// Start begins listening for signaling messages
	Start(ctx context.Context) error

	// Stop stops listening
	Stop(ctx context.Context) error
}

// CallRoom represents a WebRTC call room.
type CallRoom interface {
	// ID returns room ID
	ID() string

	// Name returns room name
	Name() string

	// Join joins the call
	JoinCall(ctx context.Context, userID string, options *JoinOptions) (PeerConnection, error)

	// Leave leaves the call
	Leave(ctx context.Context, userID string) error

	// GetPeer returns peer connection for user
	GetPeer(userID string) (PeerConnection, error)

	// GetPeers returns all peer connections
	GetPeers() []PeerConnection

	// GetParticipants returns participant information
	GetParticipants() []Participant

	// MuteUser mutes a user's audio
	MuteUser(ctx context.Context, userID string) error

	// UnmuteUser unmutes a user's audio
	UnmuteUser(ctx context.Context, userID string) error

	// EnableVideo enables user's video
	EnableVideo(ctx context.Context, userID string) error

	// DisableVideo disables user's video
	DisableVideo(ctx context.Context, userID string) error

	// StartScreenShare starts screen sharing
	StartScreenShare(ctx context.Context, userID string, track MediaTrack) error

	// StopScreenShare stops screen sharing
	StopScreenShare(ctx context.Context, userID string) error

	// GetQuality returns call quality metrics
	GetQuality(ctx context.Context) (*CallQuality, error)

	// Close closes the call room
	Close(ctx context.Context) error
}

// SFURouter handles media routing in SFU mode.
type SFURouter interface {
	// RouteTrack routes a track from sender to receivers
	RouteTrack(ctx context.Context, senderID string, track MediaTrack, receiverIDs []string) error

	// AddPublisher adds a publishing peer
	AddPublisher(ctx context.Context, userID string, peer PeerConnection) error

	// AddSubscriber adds a subscribing peer
	AddSubscriber(ctx context.Context, userID string, peer PeerConnection) error

	// RemovePublisher removes a publishing peer
	RemovePublisher(ctx context.Context, userID string) error

	// RemoveSubscriber removes a subscribing peer
	RemoveSubscriber(ctx context.Context, userID string) error

	// SubscribeToTrack subscribes a peer to a publisher's track
	SubscribeToTrack(ctx context.Context, subscriberID, publisherID, trackID string) error

	// UnsubscribeFromTrack unsubscribes a peer from a track
	UnsubscribeFromTrack(ctx context.Context, subscriberID, trackID string) error

	// AddReceiver adds a receiver for a track
	AddReceiver(ctx context.Context, trackID, receiverID string) error

	// RemoveReceiver removes a receiver
	RemoveReceiver(ctx context.Context, trackID, receiverID string) error

	// GetReceivers returns all receivers for a track
	GetReceivers(trackID string) []string

	// SetQuality sets quality layer for receiver
	SetQuality(ctx context.Context, trackID, receiverID, quality string) error

	// GetStats returns routing statistics
	GetStats(ctx context.Context) (*RouterStats, error)

	// GetAvailableTracks returns all available tracks
	GetAvailableTracks() []TrackInfo
}

// Recorder handles call recording.
type Recorder interface {
	// Start starts recording
	Start(ctx context.Context, roomID string, options *RecordingOptions) error

	// Stop stops recording
	Stop(ctx context.Context, roomID string) error

	// Pause pauses recording
	Pause(ctx context.Context, roomID string) error

	// Resume resumes recording
	Resume(ctx context.Context, roomID string) error

	// GetStatus returns recording status
	GetStatus(roomID string) (*RecordingStatus, error)
}

// QualityMonitor monitors connection quality.
type QualityMonitor interface {
	// Monitor starts monitoring peer connection
	Monitor(ctx context.Context, peer PeerConnection) error

	// Stop stops monitoring
	Stop(peerID string)

	// GetQuality returns current quality metrics
	GetQuality(peerID string) (*ConnectionQuality, error)

	// OnQualityChange sets quality change callback
	OnQualityChange(handler QualityChangeHandler)
}

// Callback handlers.
type (
	ICECandidateHandler         func(candidate *ICECandidate)
	TrackHandler                func(track MediaTrack, receiver *TrackReceiver)
	ConnectionStateHandler      func(state ConnectionState)
	DataChannelHandler          func(channel DataChannel)
	DataChannelMessageHandler   func(data []byte)
	DataChannelStateHandler     func()
	OfferHandler                func(peerID string, offer *SessionDescription)
	AnswerHandler               func(peerID string, answer *SessionDescription)
	ICECandidateReceivedHandler func(peerID string, candidate *ICECandidate)
	QualityChangeHandler        func(peerID string, quality *ConnectionQuality)
)

// Types

// SessionDescription represents SDP.
type SessionDescription struct {
	Type SessionDescriptionType
	SDP  string
}

// SessionDescriptionType is the type of SDP.
type SessionDescriptionType string

const (
	SessionDescriptionTypeOffer  SessionDescriptionType = "offer"
	SessionDescriptionTypeAnswer SessionDescriptionType = "answer"
)

// ICECandidate represents an ICE candidate.
type ICECandidate struct {
	Candidate        string
	SDPMid           string
	SDPMLineIndex    int
	UsernameFragment string
}

// ICEServer represents a STUN/TURN server.
type ICEServer struct {
	URLs       []string
	Username   string
	Credential string
}

// ConnectionState represents peer connection state.
type ConnectionState string

const (
	ConnectionStateNew          ConnectionState = "new"
	ConnectionStateConnecting   ConnectionState = "connecting"
	ConnectionStateConnected    ConnectionState = "connected"
	ConnectionStateDisconnected ConnectionState = "disconnected"
	ConnectionStateFailed       ConnectionState = "failed"
	ConnectionStateClosed       ConnectionState = "closed"
)

// DataChannelState represents data channel state.
type DataChannelState string

const (
	DataChannelStateConnecting DataChannelState = "connecting"
	DataChannelStateOpen       DataChannelState = "open"
	DataChannelStateClosing    DataChannelState = "closing"
	DataChannelStateClosed     DataChannelState = "closed"
)

// TrackKind represents track type.
type TrackKind string

const (
	TrackKindAudio TrackKind = "audio"
	TrackKindVideo TrackKind = "video"
)

// TrackSettings represents track settings.
type TrackSettings struct {
	Width      int
	Height     int
	FrameRate  int
	Bitrate    int
	SampleRate int
	Channels   int
}

// Participant represents a call participant.
type Participant struct {
	UserID        string
	DisplayName   string
	AudioEnabled  bool
	VideoEnabled  bool
	ScreenSharing bool
	Quality       *ConnectionQuality
	JoinedAt      time.Time
}

// JoinOptions holds options for joining a call.
type JoinOptions struct {
	AudioEnabled bool
	VideoEnabled bool
	DisplayName  string
	Metadata     map[string]any
}

// RecordingOptions holds recording options.
type RecordingOptions struct {
	Format      string // "webm", "mp4"
	VideoCodec  string
	AudioCodec  string
	OutputPath  string
	IncludeChat bool
}

// RecordingStatus represents recording status.
type RecordingStatus struct {
	RoomID     string
	Recording  bool
	Paused     bool
	StartedAt  time.Time
	Duration   time.Duration
	FileSize   int64
	OutputPath string
}

// TrackReceiver receives a track.
type TrackReceiver struct {
	TrackID  string
	PeerID   string
	Kind     TrackKind
	Settings TrackSettings
}

// Stats types

// PeerStats holds peer connection statistics.
type PeerStats struct {
	PeerID              string
	ConnectionState     ConnectionState
	LocalCandidateType  string
	RemoteCandidateType string
	BytesSent           uint64
	BytesReceived       uint64
	PacketsSent         uint64
	PacketsReceived     uint64
	PacketsLost         uint64
	Jitter              time.Duration
	RoundTripTime       time.Duration
	AvailableBitrate    int
}

// TrackStats holds media track statistics.
type TrackStats struct {
	TrackID         string
	Kind            TrackKind
	BytesSent       uint64
	BytesReceived   uint64
	PacketsSent     uint64
	PacketsReceived uint64
	PacketsLost     uint64
	Bitrate         int
	FrameRate       int
	Jitter          time.Duration
}

// RouterStats holds SFU router statistics.
type RouterStats struct {
	TotalTracks        int
	ActiveReceivers    int
	TotalBytesSent     uint64
	TotalBytesReceived uint64
	AverageBitrate     int
}

// CallQuality holds overall call quality metrics.
type CallQuality struct {
	RoomID           string
	ParticipantCount int
	AverageQuality   float64 // 0-100
	PacketLoss       float64
	Jitter           time.Duration
	Latency          time.Duration
	Participants     map[string]*ConnectionQuality
}

// ConnectionQuality holds connection quality metrics.
type ConnectionQuality struct {
	Score       float64 // 0-100
	PacketLoss  float64 // Percentage
	Jitter      time.Duration
	Latency     time.Duration
	BitrateKbps int
	Warnings    []string
	LastUpdated time.Time
}

// SimulcastLayer represents a simulcast layer.
type SimulcastLayer struct {
	RID     string
	Active  bool
	Width   int
	Height  int
	Bitrate int
}

// SFUStats holds SFU router statistics.
type SFUStats struct {
	TotalTracks        int
	ActiveReceivers    int
	TotalBytesSent     uint64
	TotalBytesReceived uint64
	AverageBitrate     int
	Timestamp          time.Time
}

// RecordOptions holds recording configuration options.
type RecordOptions struct {
	Format      string
	Quality     string
	AudioOnly   bool
	VideoOnly   bool
	MaxDuration time.Duration
}

// RecordingStats holds recording statistics.
type RecordingStats struct {
	Duration    time.Duration
	FileSize    uint64
	Bitrate     int
	FrameRate   int
	AudioTracks int
	VideoTracks int
	StartTime   time.Time
	EndTime     time.Time
}
