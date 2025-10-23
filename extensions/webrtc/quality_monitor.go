package webrtc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// qualityMonitor implements QualityMonitor for connection quality tracking
type qualityMonitor struct {
	peerID  string
	peer    PeerConnection
	logger  forge.Logger
	metrics forge.Metrics

	// Quality thresholds
	config QualityConfig

	// Current quality
	currentQuality *ConnectionQuality

	// Historical samples
	samples      []QualitySample
	maxSamples   int
	sampleTicker *time.Ticker

	// State
	running bool
	stopCh  chan struct{}

	mu sync.RWMutex
}

// QualitySample represents a quality measurement at a point in time
type QualitySample struct {
	Timestamp        time.Time
	PacketLoss       float64
	Jitter           time.Duration
	RTT              time.Duration
	Bitrate          uint64
	AvailableBitrate uint64
}

// QualityConfig holds thresholds for quality assessment
type QualityConfig struct {
	SampleInterval      time.Duration
	MaxSamples          int
	PacketLossThreshold float64 // 0.0-1.0, e.g., 0.05 = 5%
	JitterThreshold     time.Duration
	RTTThreshold        time.Duration
	BitrateThreshold    uint64
}

// DefaultQualityConfig returns default quality monitoring configuration
func DefaultQualityConfig() QualityConfig {
	return QualityConfig{
		SampleInterval:      time.Second,
		MaxSamples:          60, // Keep 1 minute of samples
		PacketLossThreshold: 0.05,
		JitterThreshold:     30 * time.Millisecond,
		RTTThreshold:        200 * time.Millisecond,
		BitrateThreshold:    500 * 1024, // 500 Kbps
	}
}

// NewQualityMonitor creates a new quality monitor
func NewQualityMonitor(peerID string, peer PeerConnection, config QualityConfig, logger forge.Logger, metrics forge.Metrics) QualityMonitor {
	return &qualityMonitor{
		peerID:  peerID,
		peer:    peer,
		logger:  logger,
		metrics: metrics,
		config:  config,
		currentQuality: &ConnectionQuality{
			Score:      100,
			PacketLoss: 0,
			Jitter:     0,
			RTT:        0,
			Bitrate:    0,
		},
		samples:    make([]QualitySample, 0, config.MaxSamples),
		maxSamples: config.MaxSamples,
		stopCh:     make(chan struct{}),
	}
}

// Start begins quality monitoring
func (q *qualityMonitor) Start(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.running {
		return fmt.Errorf("quality monitor already running")
	}

	q.running = true
	q.sampleTicker = time.NewTicker(q.config.SampleInterval)

	go q.monitorLoop(ctx)

	q.logger.Debug("started quality monitor", forge.F("peer_id", q.peerID))

	return nil
}

// Stop stops quality monitoring
func (q *qualityMonitor) Stop() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.running {
		return nil
	}

	q.running = false
	if q.sampleTicker != nil {
		q.sampleTicker.Stop()
	}
	close(q.stopCh)

	q.logger.Debug("stopped quality monitor", forge.F("peer_id", q.peerID))

	return nil
}

// GetQuality returns current connection quality
func (q *qualityMonitor) GetQuality(ctx context.Context) (*ConnectionQuality, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Return a copy
	quality := *q.currentQuality
	return &quality, nil
}

// GetHistory returns quality history
func (q *qualityMonitor) GetHistory(ctx context.Context, duration time.Duration) ([]QualitySample, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	samples := make([]QualitySample, 0)

	for _, sample := range q.samples {
		if sample.Timestamp.After(cutoff) {
			samples = append(samples, sample)
		}
	}

	return samples, nil
}

// monitorLoop runs the quality monitoring loop
func (q *qualityMonitor) monitorLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stopCh:
			return
		case <-q.sampleTicker.C:
			if err := q.collectSample(ctx); err != nil {
				q.logger.Error("failed to collect quality sample",
					forge.F("peer_id", q.peerID),
					forge.F("error", err),
				)
			}
		}
	}
}

// collectSample collects a quality measurement sample
func (q *qualityMonitor) collectSample(ctx context.Context) error {
	// Get stats from peer connection
	stats, err := q.peer.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get peer stats: %w", err)
	}

	// Calculate metrics
	sample := q.calculateSample(stats)

	// Update current quality
	quality := q.assessQuality(sample)

	q.mu.Lock()
	defer q.mu.Unlock()

	// Store sample
	q.samples = append(q.samples, sample)
	if len(q.samples) > q.maxSamples {
		q.samples = q.samples[1:]
	}

	// Update current quality
	q.currentQuality = quality

	// Report metrics
	if q.metrics != nil {
		q.metrics.Gauge("webrtc.quality.score", float64(quality.Score),
			forge.F("peer_id", q.peerID))
		q.metrics.Gauge("webrtc.quality.packet_loss", quality.PacketLoss,
			forge.F("peer_id", q.peerID))
		q.metrics.Gauge("webrtc.quality.jitter_ms", float64(quality.Jitter.Milliseconds()),
			forge.F("peer_id", q.peerID))
		q.metrics.Gauge("webrtc.quality.rtt_ms", float64(quality.RTT.Milliseconds()),
			forge.F("peer_id", q.peerID))
		q.metrics.Gauge("webrtc.quality.bitrate", float64(quality.Bitrate),
			forge.F("peer_id", q.peerID))
	}

	return nil
}

// calculateSample calculates quality metrics from peer stats
func (q *qualityMonitor) calculateSample(stats *PeerStats) QualitySample {
	// Calculate packet loss
	var packetLoss float64
	totalPackets := stats.PacketsSent + stats.PacketsReceived
	if totalPackets > 0 {
		// This is a simplified calculation
		// Real packet loss would require tracking sequence numbers
		packetLoss = 0.0 // Placeholder
	}

	// Calculate bitrate (bytes per second over sample interval)
	bitrate := uint64(float64(stats.BytesSent+stats.BytesReceived) / q.config.SampleInterval.Seconds())

	sample := QualitySample{
		Timestamp:        time.Now(),
		PacketLoss:       packetLoss,
		Jitter:           0, // Would need RTCP stats
		RTT:              0, // Would need RTCP stats
		Bitrate:          bitrate,
		AvailableBitrate: bitrate, // Simplified
	}

	return sample
}

// assessQuality calculates a quality score from a sample
func (q *qualityMonitor) assessQuality(sample QualitySample) *ConnectionQuality {
	score := 100

	// Deduct points for packet loss
	if sample.PacketLoss > q.config.PacketLossThreshold {
		lossScore := int((sample.PacketLoss - q.config.PacketLossThreshold) * 100)
		score -= lossScore
	}

	// Deduct points for high jitter
	if sample.Jitter > q.config.JitterThreshold {
		excessJitter := sample.Jitter - q.config.JitterThreshold
		jitterScore := int(excessJitter.Milliseconds() / 10)
		score -= jitterScore
	}

	// Deduct points for high RTT
	if sample.RTT > q.config.RTTThreshold {
		excessRTT := sample.RTT - q.config.RTTThreshold
		rttScore := int(excessRTT.Milliseconds() / 50)
		score -= rttScore
	}

	// Deduct points for low bitrate
	if sample.Bitrate < q.config.BitrateThreshold {
		bitrateDef := q.config.BitrateThreshold - sample.Bitrate
		bitrateScore := int(bitrateDef / (50 * 1024))
		score -= bitrateScore
	}

	// Clamp score
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return &ConnectionQuality{
		Score:      score,
		PacketLoss: sample.PacketLoss,
		Jitter:     sample.Jitter,
		RTT:        sample.RTT,
		Bitrate:    sample.Bitrate,
	}
}

// GetRecommendedLayer returns recommended simulcast layer based on quality
func (q *qualityMonitor) GetRecommendedLayer(ctx context.Context) (int, error) {
	quality, err := q.GetQuality(ctx)
	if err != nil {
		return 0, err
	}

	// Recommend layer based on quality score
	if quality.Score >= 80 {
		return 2 // High quality
	} else if quality.Score >= 50 {
		return 1 // Medium quality
	}
	return 0 // Low quality
}
