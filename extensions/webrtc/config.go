package webrtc

import (
	"time"
)

// Config holds WebRTC extension configuration.
type Config struct {
	// Signaling
	SignalingEnabled bool
	SignalingTimeout time.Duration

	// Topology
	Topology Topology

	// STUN servers for NAT traversal
	STUNServers []string

	// TURN servers for relaying when P2P fails
	TURNServers []TURNConfig

	// Media configuration
	MediaConfig MediaConfig

	// SFU configuration (if topology is SFU)
	SFUConfig *SFUConfig

	// Quality settings
	QualityConfig QualityConfig

	// Recording
	RecordingEnabled bool
	RecordingPath    string

	// Metrics
	MetricsEnabled bool

	// Security
	RequireAuth bool
	AllowGuests bool
}

// Topology represents the WebRTC connection topology.
type Topology string

const (
	// TopologyMesh - Peer-to-peer mesh (each peer connects to all others).
	TopologyMesh Topology = "mesh"

	// TopologySFU - Selective Forwarding Unit (server routes media).
	TopologySFU Topology = "sfu"

	// TopologyMCU - Multipoint Control Unit (server mixes media).
	TopologyMCU Topology = "mcu"
)

// TURNConfig holds TURN server configuration.
type TURNConfig struct {
	URLs       []string
	Username   string
	Credential string

	// TLS configuration
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
}

// MediaConfig holds media stream configuration.
type MediaConfig struct {
	// Audio
	AudioEnabled bool
	AudioCodecs  []string // ["opus", "pcmu", "pcma"]

	// Video
	VideoEnabled bool
	VideoCodecs  []string // ["VP8", "VP9", "H264", "AV1"]

	// Screen sharing
	ScreenShareEnabled bool

	// Data channels
	DataChannelsEnabled bool

	// Bitrate limits (kbps)
	MaxAudioBitrate int
	MaxVideoBitrate int
	MinVideoBitrate int

	// Resolution constraints
	MaxWidth  int
	MaxHeight int
	MaxFPS    int
}

// SFUConfig holds SFU-specific configuration.
type SFUConfig struct {
	// Worker configuration
	WorkerCount int

	// Bandwidth management
	MaxBandwidthMbps int
	AdaptiveBitrate  bool
	SimulcastEnabled bool

	// Quality layers for simulcast
	QualityLayers []QualityLayer

	// Recording
	RecordingEnabled bool
	RecordingFormat  string // "webm", "mp4"
}

// QualityLayer represents a simulcast quality layer.
type QualityLayer struct {
	RID       string // "f" (full), "h" (half), "q" (quarter)
	MaxWidth  int
	MaxHeight int
	MaxFPS    int
	Bitrate   int
}

// QualityConfig holds quality monitoring configuration.
type QualityConfig struct {
	// Monitoring
	MonitorEnabled  bool
	MonitorInterval time.Duration

	// Thresholds for quality warnings
	MaxPacketLoss float64 // Percentage
	MaxJitter     time.Duration
	MinBitrate    int // kbps

	// Adaptive quality
	AdaptiveQuality      bool
	QualityCheckInterval time.Duration
}

// DefaultConfig returns default WebRTC configuration.
func DefaultConfig() Config {
	return Config{
		SignalingEnabled: true,
		SignalingTimeout: 30 * time.Second,
		Topology:         TopologyMesh,

		STUNServers: []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
		},

		MediaConfig: MediaConfig{
			AudioEnabled:    true,
			AudioCodecs:     []string{"opus"},
			VideoEnabled:    true,
			VideoCodecs:     []string{"VP8", "H264"},
			MaxAudioBitrate: 128,
			MaxVideoBitrate: 2500,
			MinVideoBitrate: 150,
			MaxWidth:        1920,
			MaxHeight:       1080,
			MaxFPS:          30,
		},

		QualityConfig: QualityConfig{
			MonitorEnabled:       true,
			MonitorInterval:      5 * time.Second,
			MaxPacketLoss:        5.0,
			MaxJitter:            30 * time.Millisecond,
			MinBitrate:           100,
			AdaptiveQuality:      true,
			QualityCheckInterval: 10 * time.Second,
		},

		RequireAuth: true,
		AllowGuests: false,
	}
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithTopology sets the connection topology.
func WithTopology(topology Topology) ConfigOption {
	return func(c *Config) {
		c.Topology = topology
	}
}

// WithSTUNServers sets STUN servers.
func WithSTUNServers(servers ...string) ConfigOption {
	return func(c *Config) {
		c.STUNServers = servers
	}
}

// WithTURNServer adds a TURN server.
func WithTURNServer(turn TURNConfig) ConfigOption {
	return func(c *Config) {
		c.TURNServers = append(c.TURNServers, turn)
	}
}

// WithAudioCodecs sets audio codecs.
func WithAudioCodecs(codecs ...string) ConfigOption {
	return func(c *Config) {
		c.MediaConfig.AudioCodecs = codecs
	}
}

// WithVideoCodecs sets video codecs.
func WithVideoCodecs(codecs ...string) ConfigOption {
	return func(c *Config) {
		c.MediaConfig.VideoCodecs = codecs
	}
}

// WithSFU enables SFU mode with configuration.
func WithSFU(config SFUConfig) ConfigOption {
	return func(c *Config) {
		c.Topology = TopologySFU
		c.SFUConfig = &config
	}
}

// WithRecording enables recording.
func WithRecording(path string) ConfigOption {
	return func(c *Config) {
		c.RecordingEnabled = true
		c.RecordingPath = path
	}
}

// WithQualityMonitoring enables quality monitoring.
func WithQualityMonitoring(config QualityConfig) ConfigOption {
	return func(c *Config) {
		c.QualityConfig = config
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Topology == TopologySFU && c.SFUConfig == nil {
		return ErrInvalidConfig
	}

	if len(c.STUNServers) == 0 && len(c.TURNServers) == 0 {
		return ErrNoICEServers
	}

	if c.MediaConfig.MaxVideoBitrate < c.MediaConfig.MinVideoBitrate {
		return ErrInvalidBitrateRange
	}

	return nil
}

// GetICEServers returns all configured ICE servers.
func (c *Config) GetICEServers() []ICEServer {
	servers := make([]ICEServer, 0, len(c.STUNServers)+len(c.TURNServers))

	// Add STUN servers
	for _, url := range c.STUNServers {
		servers = append(servers, ICEServer{
			URLs: []string{url},
		})
	}

	// Add TURN servers
	for _, turn := range c.TURNServers {
		servers = append(servers, ICEServer{
			URLs:       turn.URLs,
			Username:   turn.Username,
			Credential: turn.Credential,
		})
	}

	return servers
}
