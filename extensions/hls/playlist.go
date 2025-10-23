package hls

import (
	"fmt"
	"strings"
)

// PlaylistGenerator generates HLS playlists
type PlaylistGenerator struct {
	baseURL string
}

// NewPlaylistGenerator creates a new playlist generator
func NewPlaylistGenerator(baseURL string) *PlaylistGenerator {
	return &PlaylistGenerator{
		baseURL: baseURL,
	}
}

// GenerateMasterPlaylist generates a master playlist
func (g *PlaylistGenerator) GenerateMasterPlaylist(stream *Stream) string {
	var b strings.Builder

	// Header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("\n")

	// Add variants
	for _, variant := range stream.Variants {
		// Stream info
		b.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d", variant.Bandwidth))

		if variant.Resolution != "" {
			b.WriteString(fmt.Sprintf(",RESOLUTION=%s", variant.Resolution))
		}

		if variant.Codecs != "" {
			b.WriteString(fmt.Sprintf(",CODECS=\"%s\"", variant.Codecs))
		}

		if variant.FrameRate > 0 {
			b.WriteString(fmt.Sprintf(",FRAME-RATE=%.3f", variant.FrameRate))
		}

		b.WriteString("\n")

		// Variant playlist URL
		playlistURL := fmt.Sprintf("%s/%s/variants/%s/playlist.m3u8", g.baseURL, stream.ID, variant.ID)
		b.WriteString(playlistURL + "\n")
		b.WriteString("\n")
	}

	return b.String()
}

// GenerateMediaPlaylist generates a media playlist for a variant
func (g *PlaylistGenerator) GenerateMediaPlaylist(stream *Stream, variant *Variant, segments []*SegmentInfo, ended bool) string {
	var b strings.Builder

	// Header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", stream.TargetDuration))

	// Media sequence (first segment number)
	mediaSequence := 0
	if len(segments) > 0 {
		mediaSequence = segments[0].Index
	}
	b.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", mediaSequence))

	// Playlist type
	switch stream.Type {
	case StreamTypeLive:
		// No playlist type tag for live
	case StreamTypeVOD:
		b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	case StreamTypeEvent:
		b.WriteString("#EXT-X-PLAYLIST-TYPE:EVENT\n")
	}

	b.WriteString("\n")

	// Add segments
	for _, segment := range segments {
		// Segment duration
		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", segment.Duration))

		// Segment URL
		segmentURL := fmt.Sprintf("%s/%s/variants/%s/segment_%d.ts",
			g.baseURL, stream.ID, variant.ID, segment.Index)
		b.WriteString(segmentURL + "\n")
	}

	// End tag for VOD
	if ended {
		b.WriteString("#EXT-X-ENDLIST\n")
	}

	return b.String()
}

// GenerateLiveMediaPlaylist generates a media playlist for live streaming
func (g *PlaylistGenerator) GenerateLiveMediaPlaylist(stream *Stream, variant *Variant, segments []*SegmentInfo) string {
	var b strings.Builder

	// Header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", stream.TargetDuration))

	// Media sequence (first segment number in window)
	mediaSequence := 0
	if len(segments) > 0 {
		mediaSequence = segments[0].Index
	}
	b.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", mediaSequence))

	b.WriteString("\n")

	// Add segments (only last N segments based on DVR window)
	startIdx := 0
	if len(segments) > stream.DVRWindowSize {
		startIdx = len(segments) - stream.DVRWindowSize
	}

	for i := startIdx; i < len(segments); i++ {
		segment := segments[i]

		// Segment duration
		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", segment.Duration))

		// Segment URL
		segmentURL := fmt.Sprintf("%s/%s/variants/%s/segment_%d.ts",
			g.baseURL, stream.ID, variant.ID, segment.Index)
		b.WriteString(segmentURL + "\n")
	}

	return b.String()
}

// GenerateVODMediaPlaylist generates a media playlist for VOD content
func (g *PlaylistGenerator) GenerateVODMediaPlaylist(stream *Stream, variant *Variant, segments []*SegmentInfo) string {
	playlist := g.GenerateMediaPlaylist(stream, variant, segments, true)
	return playlist
}

// ParseResolution parses a resolution string (e.g., "1920x1080") into width and height
func ParseResolution(resolution string) (width, height int, err error) {
	parts := strings.Split(resolution, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid resolution format: %s", resolution)
	}

	_, err = fmt.Sscanf(parts[0], "%d", &width)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid width: %w", err)
	}

	_, err = fmt.Sscanf(parts[1], "%d", &height)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid height: %w", err)
	}

	return width, height, nil
}

// FormatResolution formats width and height into a resolution string
func FormatResolution(width, height int) string {
	return fmt.Sprintf("%dx%d", width, height)
}

// GenerateCodecsString generates a codecs string for HLS
func GenerateCodecsString(videoCodec, audioCodec string) string {
	var codecs []string

	switch videoCodec {
	case "h264":
		codecs = append(codecs, "avc1.4d401f") // H.264 Main Profile Level 3.1
	case "h265":
		codecs = append(codecs, "hev1.1.6.L93.B0") // H.265 Main Profile
	}

	switch audioCodec {
	case "aac":
		codecs = append(codecs, "mp4a.40.2") // AAC-LC
	}

	return strings.Join(codecs, ",")
}
