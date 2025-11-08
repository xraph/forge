package webrtc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// qualityMonitor implements QualityMonitor for connection quality tracking.
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

	// Callbacks
	qualityHandler QualityChangeHandler

	mu sync.RWMutex
}

// QualitySample represents a quality measurement at a point in time.
type QualitySample struct {
	Timestamp        time.Time
	PacketLoss       float64
	Jitter           time.Duration
	RTT              time.Duration
	Bitrate          uint64
	AvailableBitrate uint64
}

// QualityConfig is defined in config.go

// DefaultQualityConfig returns default quality monitoring configuration.
func DefaultQualityConfig() QualityConfig {
	return QualityConfig{
		MonitorEnabled:       true,
		MonitorInterval:      time.Second,
		MaxPacketLoss:        5.0, // 5%
		MaxJitter:            30 * time.Millisecond,
		MinBitrate:           500, // 500 kbps
		AdaptiveQuality:      true,
		QualityCheckInterval: time.Second,
	}
}

// NewQualityMonitor creates a new quality monitor.
func NewQualityMonitor(peerID string, peer PeerConnection, config QualityConfig, logger forge.Logger, metrics forge.Metrics) QualityMonitor {
	return &qualityMonitor{
		peerID:  peerID,
		peer:    peer,
		logger:  logger,
		metrics: metrics,
		config:  config,
		currentQuality: &ConnectionQuality{
			Score:       100,
			PacketLoss:  0,
			Jitter:      0,
			Latency:     0,
			BitrateKbps: 0,
			Warnings:    []string{},
			LastUpdated: time.Now(),
		},
		samples:    make([]QualitySample, 0, 60), // Default to 60 samples
		maxSamples: 60,
		stopCh:     make(chan struct{}),
	}
}

// Start begins quality monitoring.
func (q *qualityMonitor) Start(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.running {
		return errors.New("quality monitor already running")
	}

	// Check if monitoring is enabled
	if !q.config.MonitorEnabled {
		q.logger.Debug("quality monitor disabled, not starting",
			forge.F("peer_id", q.peerID))

		return nil
	}

	if q.config.MonitorInterval <= 0 {
		return fmt.Errorf("invalid monitor interval: %v", q.config.MonitorInterval)
	}

	q.running = true
	q.sampleTicker = time.NewTicker(q.config.MonitorInterval)

	go q.monitorLoop(ctx)

	q.logger.Debug("started quality monitor", forge.F("peer_id", q.peerID))

	return nil
}

// Stop stops quality monitoring.
func (q *qualityMonitor) Stop(peerID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if this monitor is for the requested peer
	if q.peerID != peerID {
		return
	}

	if !q.running {
		return
	}

	q.running = false
	if q.sampleTicker != nil {
		q.sampleTicker.Stop()
	}

	select {
	case <-q.stopCh:
		// Channel already closed
	default:
		close(q.stopCh)
	}

	q.logger.Debug("stopped quality monitor", forge.F("peer_id", q.peerID))
}

// Monitor starts monitoring a peer connection.
func (q *qualityMonitor) Monitor(ctx context.Context, peer PeerConnection) error {
	// Start monitoring the peer
	return q.Start(ctx)
}

// OnQualityChange sets quality change callback.
func (q *qualityMonitor) OnQualityChange(handler QualityChangeHandler) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.qualityHandler = handler
}

// GetQuality returns current connection quality.
func (q *qualityMonitor) GetQuality(peerID string) (*ConnectionQuality, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Check if this monitor is for the requested peer
	if q.peerID != peerID {
		return nil, fmt.Errorf("quality monitor not found for peer %s", peerID)
	}

	// Return a deep copy to avoid race conditions
	quality := ConnectionQuality{
		Score:       q.currentQuality.Score,
		PacketLoss:  q.currentQuality.PacketLoss,
		Jitter:      q.currentQuality.Jitter,
		Latency:     q.currentQuality.Latency,
		BitrateKbps: q.currentQuality.BitrateKbps,
		Warnings:    append([]string{}, q.currentQuality.Warnings...),
		LastUpdated: q.currentQuality.LastUpdated,
	}

	return &quality, nil
}

// GetHistory returns quality history.
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

// monitorLoop runs the quality monitoring loop.
func (q *qualityMonitor) monitorLoop(ctx context.Context) {
	// Get the ticker channel under read lock to avoid race condition
	q.mu.RLock()
	ticker := q.sampleTicker
	q.mu.RUnlock()

	if ticker == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stopCh:
			return
		case <-ticker.C:
			if err := q.collectSample(ctx); err != nil {
				q.logger.Error("failed to collect quality sample",
					forge.F("peer_id", q.peerID),
					forge.F("error", err),
				)
			}
		}
	}
}

// collectSample collects a quality measurement sample.
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
		scoreGauge := q.metrics.Gauge("webrtc.quality.score")
		scoreGauge.Set(float64(quality.Score))

		packetLossGauge := q.metrics.Gauge("webrtc.quality.packet_loss")
		packetLossGauge.Set(quality.PacketLoss)

		jitterGauge := q.metrics.Gauge("webrtc.quality.jitter_ms")
		jitterGauge.Set(float64(quality.Jitter.Milliseconds()))

		latencyGauge := q.metrics.Gauge("webrtc.quality.latency_ms")
		latencyGauge.Set(float64(quality.Latency.Milliseconds()))

		bitrateGauge := q.metrics.Gauge("webrtc.quality.bitrate")
		bitrateGauge.Set(float64(quality.BitrateKbps))
	}

	return nil
}

// calculateSample calculates quality metrics from peer stats.
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
	bitrate := uint64(float64(stats.BytesSent+stats.BytesReceived) / q.config.MonitorInterval.Seconds())

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

// assessQuality calculates a quality score from a sample.
func (q *qualityMonitor) assessQuality(sample QualitySample) *ConnectionQuality {
	score := 100

	// Deduct points for packet loss
	if sample.PacketLoss > q.config.MaxPacketLoss/100 {
		lossScore := int((sample.PacketLoss - q.config.MaxPacketLoss/100) * 100)
		score -= lossScore
	}

	// Deduct points for high jitter
	if sample.Jitter > q.config.MaxJitter {
		excessJitter := sample.Jitter - q.config.MaxJitter
		jitterScore := int(excessJitter.Milliseconds() / 10)
		score -= jitterScore
	}

	// Deduct points for high RTT
	if sample.RTT > q.config.MaxJitter { // Using MaxJitter as RTT threshold
		excessRTT := sample.RTT - q.config.MaxJitter
		rttScore := int(excessRTT.Milliseconds() / 50)
		score -= rttScore
	}

	// Deduct points for low bitrate
	if sample.Bitrate < uint64(q.config.MinBitrate*1024) {
		bitrateDef := uint64(q.config.MinBitrate*1024) - sample.Bitrate
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
		Score:       float64(score),
		PacketLoss:  sample.PacketLoss,
		Jitter:      sample.Jitter,
		Latency:     sample.RTT,
		BitrateKbps: int(sample.Bitrate / 1024),
		Warnings:    []string{},
		LastUpdated: time.Now(),
	}
}

// GetRecommendedLayer returns recommended simulcast layer based on quality.
func (q *qualityMonitor) GetRecommendedLayer(peerID string) (int, error) {
	quality, err := q.GetQuality(peerID)
	if err != nil {
		return 0, err
	}

	// Recommend layer based on quality score
	if quality.Score >= 80 {
		return 2, nil // High quality
	} else if quality.Score >= 50 {
		return 1, nil // Medium quality
	}

	return 0, nil // Low quality
}
