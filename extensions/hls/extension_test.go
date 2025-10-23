package hls

import (
	"testing"
	"time"
)

func TestNewExtension(t *testing.T) {
	ext := NewExtension(
		WithBasePath("/test"),
		WithTargetDuration(10),
	)

	if ext == nil {
		t.Fatal("expected extension to be created")
	}
}

func TestExtensionMetadata(t *testing.T) {
	ext := NewExtension().(*Extension)

	if ext.Name() != "hls" {
		t.Errorf("expected name 'hls', got '%s'", ext.Name())
	}

	if ext.Version() != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", ext.Version())
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid target duration",
			config: Config{
				TargetDuration:      0,
				DVRWindowSize:       10,
				MaxSegmentSize:      1024,
				MaxStreams:          100,
				MaxViewersPerStream: 1000,
			},
			wantErr: true,
		},
		{
			name: "negative dvr window",
			config: Config{
				TargetDuration:      6,
				DVRWindowSize:       -1,
				MaxSegmentSize:      1024,
				MaxStreams:          100,
				MaxViewersPerStream: 1000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigOptions(t *testing.T) {
	config := DefaultConfig()

	// Test WithBasePath
	WithBasePath("/custom")(&config)
	if config.BasePath != "/custom" {
		t.Errorf("expected base path '/custom', got '%s'", config.BasePath)
	}

	// Test WithTargetDuration
	WithTargetDuration(10)(&config)
	if config.TargetDuration != 10 {
		t.Errorf("expected target duration 10, got %d", config.TargetDuration)
	}

	// Test WithDVRWindow
	WithDVRWindow(20)(&config)
	if config.DVRWindowSize != 20 {
		t.Errorf("expected DVR window 20, got %d", config.DVRWindowSize)
	}

	// Test WithTranscoding
	profiles := []TranscodeProfile{Profile720p}
	WithTranscoding(true, profiles...)(&config)
	if !config.EnableTranscoding {
		t.Error("expected transcoding to be enabled")
	}
	if len(config.TranscodeProfiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(config.TranscodeProfiles))
	}

	// Test WithCORS
	WithCORS(true, "http://example.com")(&config)
	if !config.EnableCORS {
		t.Error("expected CORS to be enabled")
	}
	if len(config.AllowedOrigins) != 1 || config.AllowedOrigins[0] != "http://example.com" {
		t.Errorf("expected origin 'http://example.com', got %v", config.AllowedOrigins)
	}

	// Test WithCleanup
	WithCleanup(time.Hour, 30*time.Minute)(&config)
	if config.SegmentRetention != time.Hour {
		t.Errorf("expected retention 1 hour, got %v", config.SegmentRetention)
	}
	if config.CleanupInterval != 30*time.Minute {
		t.Errorf("expected cleanup interval 30 minutes, got %v", config.CleanupInterval)
	}
}

func TestDefaultProfiles(t *testing.T) {
	profiles := DefaultProfiles()

	if len(profiles) != 4 {
		t.Errorf("expected 4 default profiles, got %d", len(profiles))
	}

	// Check that standard profiles are included
	names := make(map[string]bool)
	for _, p := range profiles {
		names[p.Name] = true
	}

	expected := []string{"360p", "480p", "720p", "1080p"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected profile %s not found", name)
		}
	}
}

func TestStreamTypes(t *testing.T) {
	if StreamTypeLive != "live" {
		t.Errorf("expected 'live', got '%s'", StreamTypeLive)
	}
	if StreamTypeVOD != "vod" {
		t.Errorf("expected 'vod', got '%s'", StreamTypeVOD)
	}
	if StreamTypeEvent != "event" {
		t.Errorf("expected 'event', got '%s'", StreamTypeEvent)
	}
}

func TestStreamStatus(t *testing.T) {
	statuses := []StreamStatus{
		StreamStatusCreated,
		StreamStatusActive,
		StreamStatusStopped,
		StreamStatusError,
		StreamStatusEnded,
	}

	for _, status := range statuses {
		if status == "" {
			t.Error("expected non-empty status")
		}
	}
}
