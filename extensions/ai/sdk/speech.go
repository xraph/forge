package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/xraph/forge"
)

// SpeechSynthesizer generates speech from text.
type SpeechSynthesizer interface {
	Synthesize(ctx context.Context, request SpeechRequest) (*SpeechResponse, error)
}

// SpeechVoice represents available voices.
type SpeechVoice string

const (
	VoiceAlloy   SpeechVoice = "alloy"
	VoiceEcho    SpeechVoice = "echo"
	VoiceFable   SpeechVoice = "fable"
	VoiceOnyx    SpeechVoice = "onyx"
	VoiceNova    SpeechVoice = "nova"
	VoiceShimmer SpeechVoice = "shimmer"
)

// SpeechFormat represents audio output format.
type SpeechFormat string

const (
	SpeechFormatMP3  SpeechFormat = "mp3"
	SpeechFormatOpus SpeechFormat = "opus"
	SpeechFormatAAC  SpeechFormat = "aac"
	SpeechFormatFLAC SpeechFormat = "flac"
	SpeechFormatWAV  SpeechFormat = "wav"
	SpeechFormatPCM  SpeechFormat = "pcm"
)

// SpeechRequest represents a speech synthesis request.
type SpeechRequest struct {
	Model          string       `json:"model"`
	Input          string       `json:"input"`
	Voice          SpeechVoice  `json:"voice"`
	ResponseFormat SpeechFormat `json:"response_format,omitempty"`
	Speed          float64      `json:"speed,omitempty"`
}

// SpeechResponse represents a speech synthesis response.
type SpeechResponse struct {
	Audio     io.ReadCloser `json:"-"`
	Format    SpeechFormat  `json:"format"`
	SizeBytes int64         `json:"size_bytes,omitempty"`
}

// GetBytes reads all audio data.
func (r *SpeechResponse) GetBytes() ([]byte, error) {
	if r.Audio == nil {
		return nil, errors.New("no audio data")
	}
	defer r.Audio.Close()

	return io.ReadAll(r.Audio)
}

// SaveTo saves the audio to a file.
func (r *SpeechResponse) SaveTo(path string) error {
	if r.Audio == nil {
		return errors.New("no audio data")
	}
	defer r.Audio.Close()

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, r.Audio)

	return err
}

// SpeechBuilder provides a fluent API for speech synthesis.
type SpeechBuilder struct {
	ctx         context.Context
	synthesizer SpeechSynthesizer
	logger      forge.Logger
	metrics     forge.Metrics

	// Request configuration
	model          string
	input          string
	voice          SpeechVoice
	responseFormat SpeechFormat
	speed          float64
	timeout        time.Duration

	// Output configuration
	outputPath string

	// Callbacks
	onStart    func()
	onComplete func(*SpeechResponse)
	onError    func(error)
}

// NewSpeechBuilder creates a new speech builder.
func NewSpeechBuilder(ctx context.Context, synthesizer SpeechSynthesizer, logger forge.Logger, metrics forge.Metrics) *SpeechBuilder {
	return &SpeechBuilder{
		ctx:            ctx,
		synthesizer:    synthesizer,
		logger:         logger,
		metrics:        metrics,
		model:          "tts-1",
		voice:          VoiceAlloy,
		responseFormat: SpeechFormatMP3,
		speed:          1.0,
		timeout:        120 * time.Second,
	}
}

// WithModel sets the model.
func (b *SpeechBuilder) WithModel(model string) *SpeechBuilder {
	b.model = model

	return b
}

// WithHDModel uses the HD model.
func (b *SpeechBuilder) WithHDModel() *SpeechBuilder {
	b.model = "tts-1-hd"

	return b
}

// WithInput sets the text input.
func (b *SpeechBuilder) WithInput(input string) *SpeechBuilder {
	b.input = input

	return b
}

// WithVoice sets the voice.
func (b *SpeechBuilder) WithVoice(voice SpeechVoice) *SpeechBuilder {
	b.voice = voice

	return b
}

// WithFormat sets the output format.
func (b *SpeechBuilder) WithFormat(format SpeechFormat) *SpeechBuilder {
	b.responseFormat = format

	return b
}

// WithSpeed sets the speech speed (0.25 to 4.0).
func (b *SpeechBuilder) WithSpeed(speed float64) *SpeechBuilder {
	if speed < 0.25 {
		speed = 0.25
	}

	if speed > 4.0 {
		speed = 4.0
	}

	b.speed = speed

	return b
}

// SaveTo sets the output file path.
func (b *SpeechBuilder) SaveTo(path string) *SpeechBuilder {
	b.outputPath = path

	return b
}

// WithTimeout sets the timeout.
func (b *SpeechBuilder) WithTimeout(timeout time.Duration) *SpeechBuilder {
	b.timeout = timeout

	return b
}

// OnStart registers a callback for start.
func (b *SpeechBuilder) OnStart(fn func()) *SpeechBuilder {
	b.onStart = fn

	return b
}

// OnComplete registers a callback for completion.
func (b *SpeechBuilder) OnComplete(fn func(*SpeechResponse)) *SpeechBuilder {
	b.onComplete = fn

	return b
}

// OnError registers a callback for errors.
func (b *SpeechBuilder) OnError(fn func(error)) *SpeechBuilder {
	b.onError = fn

	return b
}

// Generate generates speech.
func (b *SpeechBuilder) Generate() (*SpeechResponse, error) {
	if b.input == "" {
		return nil, errors.New("input text is required")
	}

	if b.onStart != nil {
		b.onStart()
	}

	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	if b.logger != nil {
		b.logger.Debug("Generating speech",
			F("model", b.model),
			F("voice", b.voice),
			F("format", b.responseFormat),
			F("input_length", len(b.input)),
		)
	}

	startTime := time.Now()

	response, err := b.synthesizer.Synthesize(ctx, SpeechRequest{
		Model:          b.model,
		Input:          b.input,
		Voice:          b.voice,
		ResponseFormat: b.responseFormat,
		Speed:          b.speed,
	})

	duration := time.Since(startTime)

	if err != nil {
		if b.onError != nil {
			b.onError(err)
		}

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.speech.errors", "model", b.model, "voice", string(b.voice)).Inc()
		}

		return nil, err
	}

	// Save to file if path specified
	if b.outputPath != "" {
		if err := response.SaveTo(b.outputPath); err != nil {
			if b.onError != nil {
				b.onError(err)
			}

			return nil, fmt.Errorf("failed to save audio: %w", err)
		}
	}

	if b.logger != nil {
		b.logger.Info("Speech generation completed",
			F("model", b.model),
			F("voice", b.voice),
			F("duration", duration),
		)
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.speech.success", "model", b.model, "voice", string(b.voice)).Inc()
		b.metrics.Histogram("forge.ai.sdk.speech.duration", "model", b.model).Observe(duration.Seconds())
	}

	if b.onComplete != nil {
		b.onComplete(response)
	}

	return response, nil
}

// ExecuteAndSave generates speech and saves to the configured path.
func (b *SpeechBuilder) ExecuteAndSave() error {
	if b.outputPath == "" {
		return errors.New("output path not configured")
	}

	_, err := b.Generate()

	return err
}

// SpeechChunker breaks text into speakable chunks.
type SpeechChunker struct {
	maxChunkSize int
	breakPoints  []string
}

// NewSpeechChunker creates a new speech chunker.
func NewSpeechChunker(maxChunkSize int) *SpeechChunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 4096 // Default OpenAI limit
	}

	return &SpeechChunker{
		maxChunkSize: maxChunkSize,
		breakPoints:  []string{"\n\n", "\n", ". ", "! ", "? ", "; ", ", ", " "},
	}
}

// Chunk splits text into speakable chunks.
func (c *SpeechChunker) Chunk(text string) []string {
	if len(text) <= c.maxChunkSize {
		return []string{text}
	}

	var chunks []string

	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= c.maxChunkSize {
			chunks = append(chunks, remaining)

			break
		}

		// Find best break point
		chunk := remaining[:c.maxChunkSize]
		breakIdx := -1

		for _, bp := range c.breakPoints {
			idx := lastIndex(chunk, bp)
			if idx > 0 {
				breakIdx = idx + len(bp)

				break
			}
		}

		if breakIdx > 0 {
			chunks = append(chunks, remaining[:breakIdx])
			remaining = remaining[breakIdx:]
		} else {
			// Force break at max size
			chunks = append(chunks, chunk)
			remaining = remaining[c.maxChunkSize:]
		}
	}

	return chunks
}

func lastIndex(s, substr string) int {
	for i := len(s) - len(substr); i >= 0; i-- {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}

// BatchSpeechBuilder generates speech for multiple chunks.
type BatchSpeechBuilder struct {
	builder *SpeechBuilder
	chunks  []string
	onChunk func(int, *SpeechResponse)
}

// NewBatchSpeechBuilder creates a batch speech builder.
func NewBatchSpeechBuilder(builder *SpeechBuilder) *BatchSpeechBuilder {
	return &BatchSpeechBuilder{
		builder: builder,
		chunks:  make([]string, 0),
	}
}

// WithText sets the full text to be chunked.
func (b *BatchSpeechBuilder) WithText(text string, maxChunkSize int) *BatchSpeechBuilder {
	chunker := NewSpeechChunker(maxChunkSize)
	b.chunks = chunker.Chunk(text)

	return b
}

// WithChunks sets pre-chunked text.
func (b *BatchSpeechBuilder) WithChunks(chunks []string) *BatchSpeechBuilder {
	b.chunks = chunks

	return b
}

// OnChunk registers a callback for each chunk.
func (b *BatchSpeechBuilder) OnChunk(fn func(int, *SpeechResponse)) *BatchSpeechBuilder {
	b.onChunk = fn

	return b
}

// Generate generates speech for all chunks.
func (b *BatchSpeechBuilder) Generate() ([]*SpeechResponse, error) {
	responses := make([]*SpeechResponse, 0, len(b.chunks))

	for i, chunk := range b.chunks {
		b.builder.input = chunk

		response, err := b.builder.Generate()
		if err != nil {
			return responses, fmt.Errorf("chunk %d failed: %w", i, err)
		}

		if b.onChunk != nil {
			b.onChunk(i, response)
		}

		responses = append(responses, response)
	}

	return responses, nil
}

// VoicePreset represents a voice configuration preset.
type VoicePreset struct {
	Name        string
	Voice       SpeechVoice
	Speed       float64
	Model       string
	Format      SpeechFormat
	Description string
}

// Common voice presets.
var (
	PresetNarrator = VoicePreset{
		Name:        "narrator",
		Voice:       VoiceFable,
		Speed:       0.9,
		Model:       "tts-1-hd",
		Format:      SpeechFormatMP3,
		Description: "Clear, dramatic narration voice",
	}

	PresetConversational = VoicePreset{
		Name:        "conversational",
		Voice:       VoiceAlloy,
		Speed:       1.1,
		Model:       "tts-1",
		Format:      SpeechFormatMP3,
		Description: "Natural, conversational voice",
	}

	PresetNews = VoicePreset{
		Name:        "news",
		Voice:       VoiceOnyx,
		Speed:       1.0,
		Model:       "tts-1-hd",
		Format:      SpeechFormatMP3,
		Description: "Professional news reader voice",
	}

	PresetAssistant = VoicePreset{
		Name:        "assistant",
		Voice:       VoiceNova,
		Speed:       1.0,
		Model:       "tts-1",
		Format:      SpeechFormatMP3,
		Description: "Friendly assistant voice",
	}

	PresetAudiobook = VoicePreset{
		Name:        "audiobook",
		Voice:       VoiceShimmer,
		Speed:       0.85,
		Model:       "tts-1-hd",
		Format:      SpeechFormatFLAC,
		Description: "High-quality audiobook voice",
	}
)

// ApplyPreset applies a voice preset to the builder.
func (b *SpeechBuilder) ApplyPreset(preset VoicePreset) *SpeechBuilder {
	b.voice = preset.Voice
	b.speed = preset.Speed
	b.model = preset.Model
	b.responseFormat = preset.Format

	return b
}
