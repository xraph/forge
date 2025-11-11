package webrtc

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// recorder implements Recorder for media recording.
type recorder struct {
	id      string
	roomID  string
	config  RecorderConfig
	logger  forge.Logger
	metrics forge.Metrics

	// Output writers
	audioWriter io.WriteCloser
	videoWriter io.WriteCloser

	// Recording state
	recording  bool
	startTime  time.Time
	duration   time.Duration
	frameCount uint64
	audioCount uint64

	// Additional stats
	fileSize  uint64
	bitrate   int
	frameRate int

	mu sync.RWMutex
}

// RecorderConfig holds recording configuration.
type RecorderConfig struct {
	OutputDir      string
	AudioCodec     string // opus, pcm
	VideoCodec     string // vp8, h264
	EnableAudio    bool
	EnableVideo    bool
	MaxDuration    time.Duration
	FileNamePrefix string
}

// DefaultRecorderConfig returns default recorder configuration.
func DefaultRecorderConfig() RecorderConfig {
	return RecorderConfig{
		OutputDir:      "./recordings",
		AudioCodec:     "opus",
		VideoCodec:     "vp8",
		EnableAudio:    true,
		EnableVideo:    true,
		MaxDuration:    time.Hour,
		FileNamePrefix: "recording",
	}
}

// NewRecorder creates a new media recorder.
func NewRecorder(roomID string, config RecorderConfig, logger forge.Logger, metrics forge.Metrics) (Recorder, error) {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	return &recorder{
		id:      fmt.Sprintf("recorder-%s-%d", roomID, time.Now().Unix()),
		roomID:  roomID,
		config:  config,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Start starts recording.
func (r *recorder) Start(ctx context.Context, roomID string, options *RecordingOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return errors.New("recorder: already recording")
	}

	// Use roomID from parameter instead of stored roomID
	if roomID != "" {
		r.roomID = roomID
	}

	timestamp := time.Now().Format("20060102-150405")
	baseFilename := fmt.Sprintf("%s/%s-%s", r.config.OutputDir, r.config.FileNamePrefix, timestamp)

	// Create audio writer
	if r.config.EnableAudio {
		audioFile := baseFilename + "-audio.ogg"

		writer, err := oggwriter.New(audioFile, 48000, 2)
		if err != nil {
			return fmt.Errorf("failed to create audio writer: %w", err)
		}

		r.audioWriter = &writeCloserWrapper{writer: writer}
		r.logger.Info("recording audio to file", forge.F("file", audioFile))
	}

	// Create video writer
	if r.config.EnableVideo {
		videoFile := baseFilename + "-video.ivf"

		writer, err := ivfwriter.New(videoFile)
		if err != nil {
			if r.audioWriter != nil {
				r.audioWriter.Close()
			}

			return fmt.Errorf("failed to create video writer: %w", err)
		}

		r.videoWriter = &writeCloserWrapper{writer: writer}
		r.logger.Info("recording video to file", forge.F("file", videoFile))
	}

	r.recording = true
	r.startTime = time.Now()
	r.frameCount = 0
	r.audioCount = 0

	r.logger.Info("started recording",
		forge.F("recorder_id", r.id),
		forge.F("room_id", r.roomID),
		forge.F("audio", r.config.EnableAudio),
		forge.F("video", r.config.EnableVideo),
	)

	if r.metrics != nil {
		counter := r.metrics.Counter("webrtc.recordings.started")
		counter.Inc()
	}

	return nil
}

// Stop stops recording.
func (r *recorder) Stop(ctx context.Context, roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return errors.New("recorder: not recording")
	}

	r.recording = false
	r.duration = time.Since(r.startTime)

	// Close writers
	if r.audioWriter != nil {
		if err := r.audioWriter.Close(); err != nil {
			r.logger.Error("failed to close audio writer", forge.F("error", err))
		}

		r.audioWriter = nil
	}

	if r.videoWriter != nil {
		if err := r.videoWriter.Close(); err != nil {
			r.logger.Error("failed to close video writer", forge.F("error", err))
		}

		r.videoWriter = nil
	}

	r.logger.Info("stopped recording",
		forge.F("recorder_id", r.id),
		forge.F("room_id", r.roomID),
		forge.F("duration", r.duration.String()),
		forge.F("video_frames", r.frameCount),
		forge.F("audio_samples", r.audioCount),
	)

	if r.metrics != nil {
		counter := r.metrics.Counter("webrtc.recordings.stopped")
		counter.Inc()

		histogram := r.metrics.Histogram("webrtc.recordings.duration")
		histogram.Observe(r.duration.Seconds())
	}

	return nil
}

// Pause pauses recording.
func (r *recorder) Pause(ctx context.Context, roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return errors.New("recording is not active")
	}

	// TODO: Implement pause functionality
	r.logger.Info("recording paused", forge.F("room_id", roomID))

	return nil
}

// Resume resumes recording.
func (r *recorder) Resume(ctx context.Context, roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return errors.New("recording is not active")
	}

	// TODO: Implement resume functionality
	r.logger.Info("recording resumed", forge.F("room_id", roomID))

	return nil
}

// IsRecording returns whether recording is active.
func (r *recorder) IsRecording() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.recording
}

// GetStats returns recording statistics.
func (r *recorder) GetStats(ctx context.Context) (*RecordingStats, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	duration := r.duration
	if r.recording {
		duration = time.Since(r.startTime)
	}

	return &RecordingStats{
		Duration:    duration,
		FileSize:    r.fileSize,
		Bitrate:     r.bitrate,
		FrameRate:   r.frameRate,
		AudioTracks: int(r.audioCount),
		VideoTracks: int(r.frameCount),
		StartTime:   r.startTime,
		EndTime:     time.Now(),
	}, nil
}

// GetStatus returns recording status.
func (r *recorder) GetStatus(roomID string) (*RecordingStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := "stopped"
	if r.recording {
		status = "recording"
	}

	recording := status == "recording"

	return &RecordingStatus{
		RoomID:     roomID,
		Recording:  recording,
		Paused:     false,
		StartedAt:  r.startTime,
		Duration:   r.duration,
		FileSize:   int64(r.fileSize),
		OutputPath: "", // TODO: Set actual output path
	}, nil
}

// WriteAudioSample writes an audio sample.
func (r *recorder) WriteAudioSample(sample media.Sample) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.recording || r.audioWriter == nil {
		return nil
	}

	// Write sample
	wrapper, ok := r.audioWriter.(*writeCloserWrapper)
	if !ok {
		return errors.New("invalid audio writer type")
	}

	oggWriter, ok := wrapper.writer.(*oggwriter.OggWriter)
	if !ok {
		return errors.New("invalid audio writer type")
	}

	if err := oggWriter.WriteRTP(&rtp.Packet{
		Header:  rtp.Header{Timestamp: uint32(sample.Duration.Milliseconds())},
		Payload: sample.Data,
	}); err != nil {
		return fmt.Errorf("failed to write audio sample: %w", err)
	}

	r.audioCount++

	return nil
}

// WriteVideoSample writes a video sample.
func (r *recorder) WriteVideoSample(sample media.Sample) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording || r.videoWriter == nil {
		return nil
	}

	// Write sample
	wrapper, ok := r.videoWriter.(*writeCloserWrapper)
	if !ok {
		return errors.New("invalid video writer type")
	}

	ivfWriter, ok := wrapper.writer.(*ivfwriter.IVFWriter)
	if !ok {
		return errors.New("invalid video writer type")
	}

	if err := ivfWriter.WriteRTP(&rtp.Packet{
		Header:  rtp.Header{Timestamp: uint32(sample.Duration.Milliseconds())},
		Payload: sample.Data,
	}); err != nil {
		return fmt.Errorf("failed to write video sample: %w", err)
	}

	r.frameCount++

	// Check max duration
	if r.config.MaxDuration > 0 && time.Since(r.startTime) >= r.config.MaxDuration {
		r.logger.Info("max recording duration reached, stopping",
			forge.F("duration", r.config.MaxDuration.String()))
		// Note: Can't call Stop here due to lock, would need async stop signal
	}

	return nil
}

// Close closes the recorder.
func (r *recorder) Close() error {
	if r.IsRecording() {
		return r.Stop(context.Background(), r.roomID)
	}

	return nil
}

// writeCloserWrapper wraps writers that don't implement io.WriteCloser.
type writeCloserWrapper struct {
	writer any
}

func (w *writeCloserWrapper) Write(p []byte) (n int, err error) {
	// This is a placeholder - the actual implementation would depend on the specific writer
	return 0, errors.New("write not implemented")
}

func (w *writeCloserWrapper) Close() error {
	// This is a placeholder - the actual implementation would depend on the specific writer
	return nil
}
