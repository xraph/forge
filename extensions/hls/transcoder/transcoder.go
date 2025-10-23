package transcoder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
)

// Transcoder handles video transcoding for HLS
type Transcoder struct {
	config       Config
	activeJobs   map[string]*Job
	activeJobsMu sync.RWMutex
	semaphore    chan struct{} // Limits concurrent transcodes
}

// Config holds transcoder configuration
type Config struct {
	FFmpegPath          string
	FFprobePath         string
	MaxConcurrentJobs   int
	TempDir             string
	EnableHardwareAccel bool
	HardwareAccelType   string // "cuda", "qsv", "videotoolbox", etc.
}

// Profile defines a transcoding profile
type Profile struct {
	Name       string
	Width      int
	Height     int
	Bitrate    int // Bits per second
	FrameRate  float64
	VideoCodec string // "h264", "h265", "vp9"
	AudioCodec string // "aac", "opus", "mp3"
	Preset     string // FFmpeg preset: "ultrafast", "fast", "medium", "slow", "veryslow"
	CRF        int    // Constant Rate Factor (quality): 18-28 for h264
}

// Job represents a transcoding job
type Job struct {
	ID         string
	InputPath  string
	OutputPath string
	Profile    Profile
	Status     JobStatus
	Progress   float64
	Error      error
	cancel     context.CancelFunc
	mu         sync.RWMutex
}

// JobStatus represents the status of a transcoding job
type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// TranscodeResult represents the result of transcoding
type TranscodeResult struct {
	OutputPath string
	Duration   float64
	Size       int64
	Bitrate    int64
}

// NewTranscoder creates a new transcoder
func NewTranscoder(config Config) *Transcoder {
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}
	if config.FFprobePath == "" {
		config.FFprobePath = "ffprobe"
	}
	if config.MaxConcurrentJobs <= 0 {
		config.MaxConcurrentJobs = 4
	}
	if config.TempDir == "" {
		config.TempDir = os.TempDir()
	}

	return &Transcoder{
		config:     config,
		activeJobs: make(map[string]*Job),
		semaphore:  make(chan struct{}, config.MaxConcurrentJobs),
	}
}

// TranscodeFile transcodes a video file to a specific profile
func (t *Transcoder) TranscodeFile(ctx context.Context, inputPath string, profile Profile) (*TranscodeResult, error) {
	// Acquire semaphore to limit concurrency
	select {
	case t.semaphore <- struct{}{}:
		defer func() { <-t.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Generate output path
	outputDir := filepath.Join(t.config.TempDir, "hls_transcode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	outputPath := filepath.Join(outputDir, fmt.Sprintf("%s_%s.mp4", filepath.Base(inputPath), profile.Name))

	// Build FFmpeg command
	args := t.buildTranscodeArgs(inputPath, outputPath, profile)

	cmd := exec.CommandContext(ctx, t.config.FFmpegPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Run transcoding
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg transcoding failed: %w, stderr: %s", err, stderr.String())
	}

	// Get output file info
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat output file: %w", err)
	}

	result := &TranscodeResult{
		OutputPath: outputPath,
		Size:       fileInfo.Size(),
	}

	return result, nil
}

// TranscodeStream transcodes a live stream
func (t *Transcoder) TranscodeStream(ctx context.Context, input io.Reader, output io.Writer, profile Profile) error {
	// Acquire semaphore
	select {
	case t.semaphore <- struct{}{}:
		defer func() { <-t.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}

	args := []string{
		"-i", "pipe:0", // Input from stdin
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov", // For streaming output
	}

	// Add profile-specific args
	args = append(args, t.buildProfileArgs(profile)...)
	args = append(args, "pipe:1") // Output to stdout

	cmd := exec.CommandContext(ctx, t.config.FFmpegPath, args...)
	cmd.Stdin = input
	cmd.Stdout = output

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg streaming transcode failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// StartJob starts a transcoding job asynchronously
func (t *Transcoder) StartJob(ctx context.Context, jobID, inputPath, outputPath string, profile Profile) (*Job, error) {
	ctx, cancel := context.WithCancel(ctx)

	job := &Job{
		ID:         jobID,
		InputPath:  inputPath,
		OutputPath: outputPath,
		Profile:    profile,
		Status:     JobStatusQueued,
		cancel:     cancel,
	}

	t.activeJobsMu.Lock()
	t.activeJobs[jobID] = job
	t.activeJobsMu.Unlock()

	go t.runJob(ctx, job)

	return job, nil
}

// runJob executes a transcoding job
func (t *Transcoder) runJob(ctx context.Context, job *Job) {
	defer func() {
		t.activeJobsMu.Lock()
		delete(t.activeJobs, job.ID)
		t.activeJobsMu.Unlock()
	}()

	// Update status
	job.mu.Lock()
	job.Status = JobStatusRunning
	job.mu.Unlock()

	// Run transcoding
	result, err := t.TranscodeFile(ctx, job.InputPath, job.Profile)

	job.mu.Lock()
	if err != nil {
		job.Status = JobStatusFailed
		job.Error = err
	} else {
		job.Status = JobStatusCompleted
		// Move output to final path if different
		if result.OutputPath != job.OutputPath {
			if err := os.Rename(result.OutputPath, job.OutputPath); err != nil {
				job.Status = JobStatusFailed
				job.Error = fmt.Errorf("failed to move output file: %w", err)
			}
		}
	}
	job.mu.Unlock()
}

// GetJob returns a transcoding job by ID
func (t *Transcoder) GetJob(jobID string) (*Job, bool) {
	t.activeJobsMu.RLock()
	defer t.activeJobsMu.RUnlock()

	job, exists := t.activeJobs[jobID]
	return job, exists
}

// CancelJob cancels a transcoding job
func (t *Transcoder) CancelJob(jobID string) error {
	t.activeJobsMu.RLock()
	job, exists := t.activeJobs[jobID]
	t.activeJobsMu.RUnlock()

	if !exists {
		return fmt.Errorf("job not found")
	}

	job.mu.Lock()
	if job.cancel != nil {
		job.cancel()
	}
	job.Status = JobStatusCancelled
	job.mu.Unlock()

	return nil
}

// buildTranscodeArgs builds FFmpeg arguments for transcoding
func (t *Transcoder) buildTranscodeArgs(inputPath, outputPath string, profile Profile) []string {
	args := []string{
		"-i", inputPath,
		"-y", // Overwrite output file
	}

	// Hardware acceleration
	if t.config.EnableHardwareAccel {
		switch t.config.HardwareAccelType {
		case "cuda":
			args = append(args, "-hwaccel", "cuda", "-hwaccel_output_format", "cuda")
		case "qsv":
			args = append(args, "-hwaccel", "qsv")
		case "videotoolbox":
			args = append(args, "-hwaccel", "videotoolbox")
		}
	}

	// Add profile-specific args
	args = append(args, t.buildProfileArgs(profile)...)

	// Output file
	args = append(args, outputPath)

	return args
}

// buildProfileArgs builds profile-specific FFmpeg arguments
func (t *Transcoder) buildProfileArgs(profile Profile) []string {
	args := []string{}

	// Video codec
	switch profile.VideoCodec {
	case "h264":
		if t.config.EnableHardwareAccel {
			switch t.config.HardwareAccelType {
			case "cuda":
				args = append(args, "-c:v", "h264_nvenc")
			case "qsv":
				args = append(args, "-c:v", "h264_qsv")
			case "videotoolbox":
				args = append(args, "-c:v", "h264_videotoolbox")
			default:
				args = append(args, "-c:v", "libx264")
			}
		} else {
			args = append(args, "-c:v", "libx264")
		}
	case "h265":
		if t.config.EnableHardwareAccel {
			switch t.config.HardwareAccelType {
			case "cuda":
				args = append(args, "-c:v", "hevc_nvenc")
			case "qsv":
				args = append(args, "-c:v", "hevc_qsv")
			case "videotoolbox":
				args = append(args, "-c:v", "hevc_videotoolbox")
			default:
				args = append(args, "-c:v", "libx265")
			}
		} else {
			args = append(args, "-c:v", "libx265")
		}
	case "vp9":
		args = append(args, "-c:v", "libvpx-vp9")
	default:
		args = append(args, "-c:v", "libx264")
	}

	// Resolution
	if profile.Width > 0 && profile.Height > 0 {
		args = append(args, "-s", fmt.Sprintf("%dx%d", profile.Width, profile.Height))
	}

	// Bitrate
	if profile.Bitrate > 0 {
		args = append(args, "-b:v", strconv.Itoa(profile.Bitrate))
	}

	// Frame rate
	if profile.FrameRate > 0 {
		args = append(args, "-r", fmt.Sprintf("%.2f", profile.FrameRate))
	}

	// Preset
	if profile.Preset != "" {
		args = append(args, "-preset", profile.Preset)
	}

	// CRF (Constant Rate Factor)
	if profile.CRF > 0 {
		args = append(args, "-crf", strconv.Itoa(profile.CRF))
	}

	// Audio codec
	switch profile.AudioCodec {
	case "aac":
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", "128k")
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-b:a", "128k")
	default:
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}

	// Additional H.264 flags for HLS compatibility
	if profile.VideoCodec == "h264" {
		args = append(args,
			"-profile:v", "main",
			"-level", "4.0",
			"-pix_fmt", "yuv420p",
		)
	}

	return args
}

// Stop stops all active transcoding jobs
func (t *Transcoder) Stop() {
	t.activeJobsMu.Lock()
	defer t.activeJobsMu.Unlock()

	for _, job := range t.activeJobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
}
