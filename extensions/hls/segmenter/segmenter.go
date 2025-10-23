package segmenter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Segmenter handles video segmentation for HLS
type Segmenter struct {
	config Config
	mu     sync.Mutex
}

// Config holds segmenter configuration
type Config struct {
	FFmpegPath     string
	TargetDuration int     // Segment duration in seconds
	SegmentPrefix  string  // Prefix for segment filenames
	InitialOffset  float64 // Initial time offset
}

// SegmentResult represents the output of segmentation
type SegmentResult struct {
	Segments  []Segment
	Duration  float64
	Bitrate   int64
	VideoInfo VideoInfo
}

// Segment represents a single video segment
type Segment struct {
	Index    int
	Duration float64
	Data     []byte
	Size     int64
}

// VideoInfo contains video metadata
type VideoInfo struct {
	Duration   float64
	Width      int
	Height     int
	Bitrate    int64
	FrameRate  float64
	VideoCodec string
	AudioCodec string
	Format     string
}

// NewSegmenter creates a new segmenter
func NewSegmenter(config Config) *Segmenter {
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}
	if config.TargetDuration == 0 {
		config.TargetDuration = 6
	}
	if config.SegmentPrefix == "" {
		config.SegmentPrefix = "segment"
	}

	return &Segmenter{
		config: config,
	}
}

// SegmentFile segments a video file into HLS segments
func (s *Segmenter) SegmentFile(ctx context.Context, inputPath string) (*SegmentResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// First, probe the video to get info
	videoInfo, err := s.probeVideo(ctx, inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to probe video: %w", err)
	}

	// Create FFmpeg command for segmentation
	args := []string{
		"-i", inputPath,
		"-c", "copy", // Copy codecs without re-encoding
		"-f", "segment", // Output format: segment
		"-segment_time", strconv.Itoa(s.config.TargetDuration), // Segment duration
		"-segment_list", "pipe:1", // Output playlist to stdout
		"-segment_format", "mpegts", // MPEG-TS format
		"-segment_list_type", "m3u8", // Playlist format
		"-segment_list_entry_prefix", s.config.SegmentPrefix + "_", // Segment prefix
		"-reset_timestamps", "1", // Reset timestamps for each segment
		"-", // Output to stdout
	}

	// Note: For production, you'd want to output segments to files
	// or memory buffers. This is a simplified version.
	cmd := exec.CommandContext(ctx, s.config.FFmpegPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg segmentation failed: %w, stderr: %s", err, stderr.String())
	}

	// Parse the result
	// In real implementation, you'd collect individual segment files
	result := &SegmentResult{
		Segments:  []Segment{},
		Duration:  videoInfo.Duration,
		Bitrate:   videoInfo.Bitrate,
		VideoInfo: *videoInfo,
	}

	return result, nil
}

// SegmentStream segments a live video stream
func (s *Segmenter) SegmentStream(ctx context.Context, input io.Reader, output chan<- Segment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// For live streaming, we'd use FFmpeg in streaming mode
	// This is a simplified implementation outline

	args := []string{
		"-i", "pipe:0", // Input from stdin
		"-c", "copy",
		"-f", "segment",
		"-segment_time", strconv.Itoa(s.config.TargetDuration),
		"-segment_format", "mpegts",
		"-reset_timestamps", "1",
		"-",
	}

	cmd := exec.CommandContext(ctx, s.config.FFmpegPath, args...)
	cmd.Stdin = input

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Read segments from stdout
	go func() {
		defer close(output)
		segmentIndex := 0

		// In real implementation, you'd parse the MPEG-TS stream
		// and detect segment boundaries
		buf := make([]byte, 1024*1024) // 1MB buffer
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				if err != io.EOF {
					// Log error
				}
				break
			}

			if n > 0 {
				segment := Segment{
					Index:    segmentIndex,
					Duration: float64(s.config.TargetDuration),
					Data:     make([]byte, n),
					Size:     int64(n),
				}
				copy(segment.Data, buf[:n])

				select {
				case output <- segment:
					segmentIndex++
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// probeVideo uses ffprobe to get video information
func (s *Segmenter) probeVideo(ctx context.Context, inputPath string) (*VideoInfo, error) {
	// Use ffprobe to get video metadata
	ffprobePath := strings.Replace(s.config.FFmpegPath, "ffmpeg", "ffprobe", 1)

	args := []string{
		"-v", "error",
		"-show_entries", "format=duration,bit_rate:stream=width,height,codec_name,r_frame_rate",
		"-of", "default=noprint_wrappers=1",
		inputPath,
	}

	cmd := exec.CommandContext(ctx, ffprobePath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w, stderr: %s", err, stderr.String())
	}

	// Parse ffprobe output
	info := &VideoInfo{
		Format: "mp4", // Default
	}

	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "duration=") {
			if d, err := strconv.ParseFloat(strings.TrimPrefix(line, "duration="), 64); err == nil {
				info.Duration = d
			}
		} else if strings.HasPrefix(line, "bit_rate=") {
			if b, err := strconv.ParseInt(strings.TrimPrefix(line, "bit_rate="), 10, 64); err == nil {
				info.Bitrate = b
			}
		} else if strings.HasPrefix(line, "width=") {
			if w, err := strconv.Atoi(strings.TrimPrefix(line, "width=")); err == nil {
				info.Width = w
			}
		} else if strings.HasPrefix(line, "height=") {
			if h, err := strconv.Atoi(strings.TrimPrefix(line, "height=")); err == nil {
				info.Height = h
			}
		} else if strings.HasPrefix(line, "codec_name=") {
			codec := strings.TrimPrefix(line, "codec_name=")
			if info.VideoCodec == "" {
				info.VideoCodec = codec
			} else if info.AudioCodec == "" {
				info.AudioCodec = codec
			}
		} else if strings.HasPrefix(line, "r_frame_rate=") {
			frameRateStr := strings.TrimPrefix(line, "r_frame_rate=")
			if parts := strings.Split(frameRateStr, "/"); len(parts) == 2 {
				num, _ := strconv.ParseFloat(parts[0], 64)
				den, _ := strconv.ParseFloat(parts[1], 64)
				if den != 0 {
					info.FrameRate = num / den
				}
			}
		}
	}

	return info, nil
}

// GetVideoInfo returns video information without segmenting
func (s *Segmenter) GetVideoInfo(ctx context.Context, inputPath string) (*VideoInfo, error) {
	return s.probeVideo(ctx, inputPath)
}
