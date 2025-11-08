package hls

import (
	"context"
	"io"
	"time"
)

// HLS represents the core HLS streaming interface
type HLS interface {
	// Stream Management
	CreateStream(ctx context.Context, opts StreamOptions) (*Stream, error)
	GetStream(ctx context.Context, streamID string) (*Stream, error)
	DeleteStream(ctx context.Context, streamID string) error
	ListStreams(ctx context.Context) ([]*Stream, error)

	// Live Streaming
	StartLiveStream(ctx context.Context, streamID string) error
	StopLiveStream(ctx context.Context, streamID string) error
	IngestSegment(ctx context.Context, streamID string, segment *Segment) error

	// VOD (Video on Demand)
	CreateVOD(ctx context.Context, source io.Reader, opts VODOptions) (*Stream, error)
	TranscodeVideo(ctx context.Context, streamID string, profiles []TranscodeProfile) error

	// Manifest Generation
	GetMasterPlaylist(ctx context.Context, streamID string) (*MasterPlaylist, error)
	GetMediaPlaylist(ctx context.Context, streamID string, variantID string) (*MediaPlaylist, error)

	// Segment Management
	GetSegment(ctx context.Context, streamID, variantID string, segmentNum int) (io.ReadCloser, error)
	GetSegmentURL(ctx context.Context, streamID, variantID string, segmentNum int) (string, error)

	// Stats and Monitoring
	GetStreamStats(ctx context.Context, streamID string) (*StreamStats, error)
	GetActiveViewers(ctx context.Context, streamID string) (int, error)
}

// Stream represents an HLS stream
type Stream struct {
	ID          string
	Type        StreamType
	Status      StreamStatus
	Title       string
	Description string

	// Variants (different quality levels)
	Variants []*Variant

	// Metadata
	Duration  time.Duration
	CreatedAt time.Time
	UpdatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time

	// Configuration
	TargetDuration int // Segment duration in seconds
	DVRWindowSize  int // Number of segments to keep in DVR window

	// Stats
	ViewerCount    int
	TotalViews     int64
	TotalBandwidth int64
}

// Variant represents a quality variant of the stream
type Variant struct {
	ID         string
	Bandwidth  int    // Bits per second
	Resolution string // e.g., "1920x1080"
	Codecs     string // e.g., "avc1.4d401f,mp4a.40.2"
	FrameRate  float64

	// Playlist info
	PlaylistURL  string
	SegmentCount int
	LastSegment  int
}

// Segment represents an HLS segment
type Segment struct {
	StreamID    string
	VariantID   string
	SequenceNum int
	Duration    float64
	Data        []byte
	URL         string
	Size        int64
	CreatedAt   time.Time
}

// MasterPlaylist represents an HLS master playlist
type MasterPlaylist struct {
	StreamID string
	Variants []*VariantInfo
	Content  string // M3U8 content
}

// VariantInfo contains information about a variant in the master playlist
type VariantInfo struct {
	Bandwidth  int
	Resolution string
	Codecs     string
	FrameRate  float64
	URI        string
}

// MediaPlaylist represents an HLS media playlist
type MediaPlaylist struct {
	StreamID       string
	VariantID      string
	TargetDuration int
	MediaSequence  int
	Segments       []*SegmentInfo
	Ended          bool
	Content        string // M3U8 content
}

// SegmentInfo contains information about a segment in the media playlist
type SegmentInfo struct {
	Duration float64
	URI      string
	Index    int
}

// StreamStats contains statistics about a stream
type StreamStats struct {
	StreamID       string
	CurrentViewers int
	TotalViews     int64
	TotalBandwidth int64
	AverageLatency time.Duration
	SegmentsServed int64
	ErrorRate      float64

	// Per-variant stats
	VariantStats map[string]*VariantStats
}

// VariantStats contains statistics about a specific variant
type VariantStats struct {
	VariantID      string
	RequestCount   int64
	BytesServed    int64
	CurrentViewers int
	AverageLatency time.Duration
}

// StreamOptions contains options for creating a stream
type StreamOptions struct {
	Title          string
	Description    string
	Type           StreamType
	TargetDuration int // Segment duration in seconds
	DVRWindowSize  int // Number of segments to keep for DVR

	// Transcoding
	TranscodeProfiles []TranscodeProfile

	// Storage
	StorageBackend string
	StoragePath    string
}

// VODOptions contains options for creating VOD content
type VODOptions struct {
	Title          string
	Description    string
	TargetDuration int

	// Transcoding
	TranscodeProfiles []TranscodeProfile

	// Storage
	StorageBackend string
	StoragePath    string
}

// TranscodeProfile defines a transcoding profile
type TranscodeProfile struct {
	Name       string
	Width      int
	Height     int
	Bitrate    int // Bits per second
	FrameRate  float64
	VideoCodec string // e.g., "h264", "h265"
	AudioCodec string // e.g., "aac"
	Preset     string // e.g., "fast", "medium", "slow"
}

// StreamType represents the type of stream
type StreamType string

const (
	StreamTypeLive  StreamType = "live"
	StreamTypeVOD   StreamType = "vod"
	StreamTypeEvent StreamType = "event" // Live stream that becomes VOD
)

// StreamStatus represents the status of a stream
type StreamStatus string

const (
	StreamStatusCreated StreamStatus = "created"
	StreamStatusActive  StreamStatus = "active"
	StreamStatusStopped StreamStatus = "stopped"
	StreamStatusError   StreamStatus = "error"
	StreamStatusEnded   StreamStatus = "ended"
)

// Common transcoding profiles
var (
	// Mobile profiles
	Profile360p = TranscodeProfile{
		Name:       "360p",
		Width:      640,
		Height:     360,
		Bitrate:    800000, // 800 Kbps
		FrameRate:  30,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Preset:     "fast",
	}

	Profile480p = TranscodeProfile{
		Name:       "480p",
		Width:      854,
		Height:     480,
		Bitrate:    1400000, // 1.4 Mbps
		FrameRate:  30,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Preset:     "medium",
	}

	// HD profiles
	Profile720p = TranscodeProfile{
		Name:       "720p",
		Width:      1280,
		Height:     720,
		Bitrate:    2800000, // 2.8 Mbps
		FrameRate:  30,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Preset:     "medium",
	}

	Profile1080p = TranscodeProfile{
		Name:       "1080p",
		Width:      1920,
		Height:     1080,
		Bitrate:    5000000, // 5 Mbps
		FrameRate:  30,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Preset:     "medium",
	}

	// 4K profile
	Profile4K = TranscodeProfile{
		Name:       "4K",
		Width:      3840,
		Height:     2160,
		Bitrate:    20000000, // 20 Mbps
		FrameRate:  30,
		VideoCodec: "h265",
		AudioCodec: "aac",
		Preset:     "slow",
	}
)

// DefaultProfiles returns common transcoding profiles
func DefaultProfiles() []TranscodeProfile {
	return []TranscodeProfile{
		Profile360p,
		Profile480p,
		Profile720p,
		Profile1080p,
	}
}
