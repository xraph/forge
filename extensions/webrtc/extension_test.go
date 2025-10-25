package webrtc

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming"
)

// mockStreamingExtension for testing
type mockStreamingExtension struct{}

func (m *mockStreamingExtension) Name() string                     { return "streaming" }
func (m *mockStreamingExtension) Version() string                  { return "1.0.0" }
func (m *mockStreamingExtension) Description() string              { return "Mock streaming extension" }
func (m *mockStreamingExtension) Register(app any) error           { return nil }
func (m *mockStreamingExtension) Start(ctx context.Context) error  { return nil }
func (m *mockStreamingExtension) Stop(ctx context.Context) error   { return nil }
func (m *mockStreamingExtension) Health(ctx context.Context) error { return nil }
func (m *mockStreamingExtension) Dependencies() []string           { return []string{} }
func (m *mockStreamingExtension) Manager() streaming.Manager       { return nil }

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid mesh config",
			config: Config{
				Topology:    TopologyMesh,
				STUNServers: []string{"stun:stun.l.google.com:19302"},
			},
			wantErr: false,
		},
		{
			name: "valid SFU config",
			config: Config{
				Topology:    TopologySFU,
				STUNServers: []string{"stun:stun.l.google.com:19302"},
				SFUConfig:   &SFUConfig{WorkerCount: 4},
			},
			wantErr: false,
		},
		{
			name: "SFU without config",
			config: Config{
				Topology:    TopologySFU,
				STUNServers: []string{"stun:stun.l.google.com:19302"},
				SFUConfig:   nil, // Missing!
			},
			wantErr: true,
		},
		{
			name: "no ICE servers",
			config: Config{
				Topology: TopologyMesh,
				// No STUN/TURN servers
			},
			wantErr: true,
		},
	}

	// Create a real streaming extension for testing
	streamingExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
		streaming.WithFeatures(false, false, false, false, false), // Disable all features for testing
	).(*streaming.Extension)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext, err := New(streamingExt, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if ext == nil {
					t.Error("expected extension but got nil")
				}
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Topology:    TopologyMesh,
				STUNServers: []string{"stun:example.com"},
				MediaConfig: MediaConfig{
					MinVideoBitrate: 150,
					MaxVideoBitrate: 2500,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid bitrate range",
			config: Config{
				Topology:    TopologyMesh,
				STUNServers: []string{"stun:example.com"},
				MediaConfig: MediaConfig{
					MinVideoBitrate: 3000,
					MaxVideoBitrate: 2500, // Max < Min!
				},
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

func TestConfig_GetICEServers(t *testing.T) {
	config := Config{
		STUNServers: []string{
			"stun:stun1.example.com:3478",
			"stun:stun2.example.com:3478",
		},
		TURNServers: []TURNConfig{
			{
				URLs:       []string{"turn:turn.example.com:3478"},
				Username:   "user",
				Credential: "pass",
			},
		},
	}

	servers := config.GetICEServers()

	expectedCount := len(config.STUNServers) + len(config.TURNServers)
	if len(servers) != expectedCount {
		t.Errorf("expected %d ICE servers, got %d", expectedCount, len(servers))
	}

	// Verify STUN servers
	for i, stunURL := range config.STUNServers {
		if len(servers[i].URLs) != 1 || servers[i].URLs[0] != stunURL {
			t.Errorf("STUN server %d mismatch: expected %s, got %v", i, stunURL, servers[i].URLs)
		}
	}

	// Verify TURN server
	turnServer := servers[len(config.STUNServers)]
	if turnServer.Username != "user" || turnServer.Credential != "pass" {
		t.Error("TURN server credentials not set correctly")
	}
}

func TestConfigOptions(t *testing.T) {
	t.Run("WithTopology", func(t *testing.T) {
		config := DefaultConfig()
		WithTopology(TopologySFU)(&config)

		if config.Topology != TopologySFU {
			t.Errorf("expected topology %s, got %s", TopologySFU, config.Topology)
		}
	})

	t.Run("WithSTUNServers", func(t *testing.T) {
		config := DefaultConfig()
		servers := []string{"stun:example.com:3478"}
		WithSTUNServers(servers...)(&config)

		if len(config.STUNServers) != 1 || config.STUNServers[0] != servers[0] {
			t.Error("STUN servers not set correctly")
		}
	})

	t.Run("WithTURNServer", func(t *testing.T) {
		config := DefaultConfig()
		turn := TURNConfig{
			URLs:       []string{"turn:example.com:3478"},
			Username:   "user",
			Credential: "pass",
		}
		WithTURNServer(turn)(&config)

		if len(config.TURNServers) != 1 {
			t.Error("TURN server not added")
		}
		if config.TURNServers[0].Username != "user" {
			t.Error("TURN server not set correctly")
		}
	})

	t.Run("WithRecording", func(t *testing.T) {
		config := DefaultConfig()
		path := "/recordings"
		WithRecording(path)(&config)

		if !config.RecordingEnabled {
			t.Error("recording not enabled")
		}
		if config.RecordingPath != path {
			t.Errorf("recording path = %s, want %s", config.RecordingPath, path)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Verify defaults
	if !config.SignalingEnabled {
		t.Error("signaling should be enabled by default")
	}

	if config.SignalingTimeout != 30*time.Second {
		t.Errorf("signaling timeout = %v, want 30s", config.SignalingTimeout)
	}

	if config.Topology != TopologyMesh {
		t.Errorf("topology = %s, want %s", config.Topology, TopologyMesh)
	}

	if len(config.STUNServers) == 0 {
		t.Error("default config should have STUN servers")
	}

	if !config.MediaConfig.AudioEnabled {
		t.Error("audio should be enabled by default")
	}

	if !config.MediaConfig.VideoEnabled {
		t.Error("video should be enabled by default")
	}

	if config.QualityConfig.MonitorEnabled != true {
		t.Error("quality monitoring should be enabled by default")
	}

	if !config.RequireAuth {
		t.Error("auth should be required by default")
	}
}

func BenchmarkConfig_GetICEServers(b *testing.B) {
	config := Config{
		STUNServers: []string{
			"stun:stun1.example.com:3478",
			"stun:stun2.example.com:3478",
		},
		TURNServers: []TURNConfig{
			{URLs: []string{"turn:turn1.example.com:3478"}},
			{URLs: []string{"turn:turn2.example.com:3478"}},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.GetICEServers()
	}
}
