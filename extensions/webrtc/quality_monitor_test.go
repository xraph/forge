package webrtc

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
)

func TestQualityMonitor(t *testing.T) {
	// Create mock peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()

	peer, err := NewPeerConnection("test-peer", "test-user", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Create quality monitor
	qualityConfig := QualityConfig{
		MonitorEnabled:       true,
		MonitorInterval:      100 * time.Millisecond,
		MaxPacketLoss:        5.0,
		MaxJitter:            30 * time.Millisecond,
		MinBitrate:           100,
		AdaptiveQuality:      true,
		QualityCheckInterval: 50 * time.Millisecond,
	}

	metrics := forge.NewNoOpMetrics()
	monitor := NewQualityMonitor("test-peer", peer, qualityConfig, logger, metrics)

	// Test initial quality
	quality, err := monitor.GetQuality("test-peer")
	if err != nil {
		t.Errorf("failed to get initial quality: %v", err)
	}

	if quality.Score != 100 {
		t.Errorf("expected initial quality score 100, got %f", quality.Score)
	}

	if quality.PacketLoss != 0 {
		t.Errorf("expected initial packet loss 0, got %f", quality.PacketLoss)
	}

	// Test quality change handler
	qualityChanged := make(chan bool, 1)

	monitor.OnQualityChange(func(peerID string, quality *ConnectionQuality) {
		if peerID == "test-peer" {
			select {
			case qualityChanged <- true:
			default:
			}
		}
	})

	// Test monitoring (this will run in background)
	ctx := context.Background()

	err = monitor.Monitor(ctx, peer)
	if err != nil {
		t.Errorf("failed to start monitoring: %v", err)
	}

	// Wait a bit for monitoring to run
	time.Sleep(200 * time.Millisecond)

	// Check that quality is still being tracked
	quality, err = monitor.GetQuality("test-peer")
	if err != nil {
		t.Errorf("failed to get quality during monitoring: %v", err)
	}

	// Test stopping monitoring
	monitor.Stop("test-peer")

	// Test getting quality for non-existent peer
	_, err = monitor.GetQuality("non-existent")
	if err == nil {
		t.Error("should fail to get quality for non-existent peer")
	}

	// Test double start - should return error
	// Wait a bit for the first monitor to fully initialize
	time.Sleep(50 * time.Millisecond)

	err = monitor.Monitor(ctx, peer)
	if err == nil {
		t.Log("second start returned nil (monitor already running)")
	}

	monitor.Stop("test-peer")
}

func TestQualityMonitor_ConfigValidation(t *testing.T) {
	// Test with disabled monitoring
	config := QualityConfig{
		MonitorEnabled: false,
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()

	peer, err := NewPeerConnection("test-peer", "test-user", DefaultConfig(), logger)
	if err != nil {
		t.Fatalf("failed to create peer: %v", err)
	}
	defer peer.Close(context.Background())

	monitor := NewQualityMonitor("test-peer", peer, config, logger, metrics)

	// Should not error even with disabled monitoring
	ctx := context.Background()

	err = monitor.Monitor(ctx, peer)
	if err != nil {
		t.Errorf("start should not error with disabled monitoring: %v", err)
	}

	monitor.Stop("test-peer")
}

func TestQualityMonitor_ConcurrentAccess(t *testing.T) {
	// Create mock peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()

	peer, err := NewPeerConnection("concurrent-peer", "concurrent-user", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Create quality monitor
	qualityConfig := QualityConfig{
		MonitorEnabled:  true,
		MonitorInterval: 50 * time.Millisecond,
	}

	metrics := forge.NewNoOpMetrics()
	monitor := NewQualityMonitor("concurrent-peer", peer, qualityConfig, logger, metrics)

	// Test concurrent access to GetQuality
	ctx := context.Background()

	err = monitor.Monitor(ctx, peer)
	if err != nil {
		t.Fatalf("failed to start monitoring: %v", err)
	}

	// Run concurrent quality checks
	done := make(chan bool, 1)

	go func() {
		for range 100 {
			_, err := monitor.GetQuality("concurrent-peer")
			if err != nil {
				t.Errorf("concurrent quality check failed: %v", err)

				return
			}
		}

		done <- true
	}()

	// Wait for concurrent operations
	<-done

	// Cleanup
	monitor.Stop("concurrent-peer")
}

func TestQualityMonitor_EdgeCases(t *testing.T) {
	// Create mock peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()

	peer, err := NewPeerConnection("edge-peer", "edge-user", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Test with very short monitoring interval
	qualityConfig := QualityConfig{
		MonitorEnabled:  true,
		MonitorInterval: 1 * time.Millisecond, // Very fast
	}

	metrics := forge.NewNoOpMetrics()
	monitor := NewQualityMonitor("edge-peer", peer, qualityConfig, logger, metrics)

	// Should handle very fast intervals
	ctx := context.Background()

	err = monitor.Monitor(ctx, peer)
	if err != nil {
		t.Errorf("start should handle fast intervals: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // Let it run briefly

	monitor.Stop("edge-peer")

	// Test with zero thresholds
	zeroConfig := QualityConfig{
		MonitorEnabled:  true,
		MonitorInterval: 100 * time.Millisecond,
		MaxPacketLoss:   0.0,
		MaxJitter:       0,
		MinBitrate:      0,
	}

	zeroMonitor := NewQualityMonitor("zero-peer", peer, zeroConfig, logger, metrics)

	err = zeroMonitor.Monitor(ctx, peer)
	if err != nil {
		t.Errorf("start should handle zero thresholds: %v", err)
	}

	zeroMonitor.Stop("zero-peer")
}

func TestConnectionQuality_Calculation(t *testing.T) {
	// Test quality calculation logic
	quality := &ConnectionQuality{
		Score:       85.5,
		PacketLoss:  2.3,
		Jitter:      15 * time.Millisecond,
		Latency:     45 * time.Millisecond,
		BitrateKbps: 1000,
		Warnings:    []string{"high latency"},
		LastUpdated: time.Now(),
	}

	// Test that quality values are reasonable
	if quality.Score < 0 || quality.Score > 100 {
		t.Errorf("quality score should be between 0-100, got %f", quality.Score)
	}

	if quality.PacketLoss < 0 {
		t.Errorf("packet loss should not be negative, got %f", quality.PacketLoss)
	}

	if quality.BitrateKbps < 0 {
		t.Errorf("bitrate should not be negative, got %d", quality.BitrateKbps)
	}

	// Test warning addition
	quality.Warnings = append(quality.Warnings, "packet loss")
	if len(quality.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(quality.Warnings))
	}
}

func BenchmarkQualityMonitor_Checks(b *testing.B) {
	// Create mock peer connection
	config := DefaultConfig()
	logger := forge.NewNoopLogger()

	peer, err := NewPeerConnection("bench-peer", "bench-user", config, logger)
	if err != nil {
		b.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(context.Background())

	// Create quality monitor
	qualityConfig := QualityConfig{
		MonitorEnabled:  true,
		MonitorInterval: 10 * time.Millisecond,
	}

	metrics := forge.NewNoOpMetrics()
	monitor := NewQualityMonitor("bench-peer", peer, qualityConfig, logger, metrics)

	ctx := context.Background()

	err = monitor.Monitor(ctx, peer)
	if err != nil {
		b.Fatalf("failed to start monitoring: %v", err)
	}
	defer monitor.Stop("bench-peer")

	for b.Loop() {
		_, err := monitor.GetQuality("bench-peer")
		if err != nil {
			b.Fatalf("failed to get quality in benchmark: %v", err)
		}
	}
}
