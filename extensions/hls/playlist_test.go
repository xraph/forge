package hls

import (
	"strings"
	"testing"
)

func TestPlaylistGenerator(t *testing.T) {
	generator := NewPlaylistGenerator("http://localhost:8080/hls")

	if generator == nil {
		t.Fatal("expected generator to be created")
	}
}

func TestGenerateMasterPlaylist(t *testing.T) {
	generator := NewPlaylistGenerator("http://localhost:8080/hls")

	stream := &Stream{
		ID:             "test-stream",
		Type:           StreamTypeLive,
		TargetDuration: 6,
		Variants: []*Variant{
			{
				ID:         "variant-720p",
				Bandwidth:  2800000,
				Resolution: "1280x720",
				Codecs:     "avc1.4d401f,mp4a.40.2",
				FrameRate:  30,
			},
			{
				ID:         "variant-1080p",
				Bandwidth:  5000000,
				Resolution: "1920x1080",
				Codecs:     "avc1.4d401f,mp4a.40.2",
				FrameRate:  30,
			},
		},
	}

	content := generator.GenerateMasterPlaylist(stream)

	// Check required tags
	if !strings.Contains(content, "#EXTM3U") {
		t.Error("expected #EXTM3U tag")
	}

	if !strings.Contains(content, "#EXT-X-VERSION:3") {
		t.Error("expected #EXT-X-VERSION:3 tag")
	}

	// Check variants
	if !strings.Contains(content, "BANDWIDTH=2800000") {
		t.Error("expected 720p bandwidth")
	}

	if !strings.Contains(content, "BANDWIDTH=5000000") {
		t.Error("expected 1080p bandwidth")
	}

	if !strings.Contains(content, "RESOLUTION=1280x720") {
		t.Error("expected 720p resolution")
	}

	if !strings.Contains(content, "RESOLUTION=1920x1080") {
		t.Error("expected 1080p resolution")
	}

	// Check playlist URLs
	if !strings.Contains(content, "variant-720p/playlist.m3u8") {
		t.Error("expected 720p playlist URL")
	}

	if !strings.Contains(content, "variant-1080p/playlist.m3u8") {
		t.Error("expected 1080p playlist URL")
	}
}

func TestGenerateMediaPlaylist(t *testing.T) {
	generator := NewPlaylistGenerator("http://localhost:8080/hls")

	stream := &Stream{
		ID:             "test-stream",
		Type:           StreamTypeVOD,
		TargetDuration: 10,
	}

	variant := &Variant{
		ID:        "variant-720p",
		Bandwidth: 2800000,
	}

	segments := []*SegmentInfo{
		{Index: 0, Duration: 10.0, URI: "segment_0.ts"},
		{Index: 1, Duration: 10.0, URI: "segment_1.ts"},
		{Index: 2, Duration: 9.5, URI: "segment_2.ts"},
	}

	content := generator.GenerateMediaPlaylist(stream, variant, segments, true)

	// Check required tags
	if !strings.Contains(content, "#EXTM3U") {
		t.Error("expected #EXTM3U tag")
	}

	if !strings.Contains(content, "#EXT-X-VERSION:3") {
		t.Error("expected #EXT-X-VERSION:3 tag")
	}

	if !strings.Contains(content, "#EXT-X-TARGETDURATION:10") {
		t.Error("expected target duration tag")
	}

	if !strings.Contains(content, "#EXT-X-MEDIA-SEQUENCE:0") {
		t.Error("expected media sequence tag")
	}

	if !strings.Contains(content, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Error("expected VOD playlist type")
	}

	// Check segments
	if !strings.Contains(content, "#EXTINF:10.000") {
		t.Error("expected segment duration")
	}

	if !strings.Contains(content, "segment_0.ts") {
		t.Error("expected segment URI")
	}

	// Check end tag for VOD
	if !strings.Contains(content, "#EXT-X-ENDLIST") {
		t.Error("expected endlist tag for VOD")
	}
}

func TestGenerateLiveMediaPlaylist(t *testing.T) {
	generator := NewPlaylistGenerator("http://localhost:8080/hls")

	stream := &Stream{
		ID:             "test-stream",
		Type:           StreamTypeLive,
		TargetDuration: 6,
		DVRWindowSize:  5,
	}

	variant := &Variant{
		ID: "variant-720p",
	}

	// Simulate 10 segments (should only include last 5 for DVR)
	segments := make([]*SegmentInfo, 10)
	for i := 0; i < 10; i++ {
		segments[i] = &SegmentInfo{
			Index:    i,
			Duration: 6.0,
			URI:      "",
		}
	}

	content := generator.GenerateLiveMediaPlaylist(stream, variant, segments)

	// Should not have endlist tag for live
	if strings.Contains(content, "#EXT-X-ENDLIST") {
		t.Error("live playlist should not have endlist tag")
	}

	// Media sequence should be 5 (first segment of DVR window)
	if !strings.Contains(content, "#EXT-X-MEDIA-SEQUENCE:5") {
		t.Error("expected media sequence 5 for DVR window")
	}
}

func TestFormatResolution(t *testing.T) {
	tests := []struct {
		width    int
		height   int
		expected string
	}{
		{1920, 1080, "1920x1080"},
		{1280, 720, "1280x720"},
		{640, 360, "640x360"},
	}

	for _, tt := range tests {
		result := FormatResolution(tt.width, tt.height)
		if result != tt.expected {
			t.Errorf("FormatResolution(%d, %d) = %s, expected %s",
				tt.width, tt.height, result, tt.expected)
		}
	}
}

func TestParseResolution(t *testing.T) {
	tests := []struct {
		resolution string
		width      int
		height     int
		wantErr    bool
	}{
		{"1920x1080", 1920, 1080, false},
		{"1280x720", 1280, 720, false},
		{"640x360", 640, 360, false},
		{"invalid", 0, 0, true},
		{"1920", 0, 0, true},
	}

	for _, tt := range tests {
		width, height, err := ParseResolution(tt.resolution)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseResolution(%s) error = %v, wantErr %v",
				tt.resolution, err, tt.wantErr)
			continue
		}

		if !tt.wantErr {
			if width != tt.width || height != tt.height {
				t.Errorf("ParseResolution(%s) = (%d, %d), expected (%d, %d)",
					tt.resolution, width, height, tt.width, tt.height)
			}
		}
	}
}

func TestGenerateCodecsString(t *testing.T) {
	tests := []struct {
		videoCodec string
		audioCodec string
		expected   string
	}{
		{"h264", "aac", "avc1.4d401f,mp4a.40.2"},
		{"h265", "aac", "hev1.1.6.L93.B0,mp4a.40.2"},
		{"h264", "", "avc1.4d401f"},
		{"", "aac", "mp4a.40.2"},
	}

	for _, tt := range tests {
		result := GenerateCodecsString(tt.videoCodec, tt.audioCodec)
		if result != tt.expected {
			t.Errorf("GenerateCodecsString(%s, %s) = %s, expected %s",
				tt.videoCodec, tt.audioCodec, result, tt.expected)
		}
	}
}
