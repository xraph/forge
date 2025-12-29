package sdk

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/xraph/forge"
)

// AudioTranscriberAPI defines the audio transcription API interface.
type AudioTranscriberAPI interface {
	Transcribe(ctx context.Context, request TranscriptionRequest) (*TranscriptionResponse, error)
	TranslateAudio(ctx context.Context, request TranslationRequest) (*TranslationResponse, error)
}

// TranscriptionRequest represents a transcription request.
type TranscriptionRequest struct {
	File                   io.Reader `json:"-"`
	Filename               string    `json:"filename,omitempty"`
	Model                  string    `json:"model"`
	Language               string    `json:"language,omitempty"`
	Prompt                 string    `json:"prompt,omitempty"`
	ResponseFormat         string    `json:"response_format,omitempty"`
	Temperature            float64   `json:"temperature,omitempty"`
	TimestampGranularities []string  `json:"timestamp_granularities,omitempty"`
}

// TranscriptionResponse represents a transcription response.
type TranscriptionResponse struct {
	Task     string    `json:"task,omitempty"`
	Language string    `json:"language,omitempty"`
	Duration float64   `json:"duration,omitempty"`
	Text     string    `json:"text"`
	Words    []Word    `json:"words,omitempty"`
	Segments []Segment `json:"segments,omitempty"`
}

// Word represents a transcribed word with timing.
type Word struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Segment represents a transcribed segment.
type Segment struct {
	ID               int     `json:"id"`
	Seek             int     `json:"seek"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens"`
	Temperature      float64 `json:"temperature"`
	AvgLogprob       float64 `json:"avg_logprob"`
	CompressionRatio float64 `json:"compression_ratio"`
	NoSpeechProb     float64 `json:"no_speech_prob"`
}

// TranslationRequest represents an audio translation request.
type TranslationRequest struct {
	File           io.Reader `json:"-"`
	Filename       string    `json:"filename,omitempty"`
	Model          string    `json:"model"`
	Prompt         string    `json:"prompt,omitempty"`
	ResponseFormat string    `json:"response_format,omitempty"`
	Temperature    float64   `json:"temperature,omitempty"`
}

// TranslationResponse represents a translation response.
type TranslationResponse struct {
	Task     string    `json:"task,omitempty"`
	Language string    `json:"language,omitempty"`
	Duration float64   `json:"duration,omitempty"`
	Text     string    `json:"text"`
	Segments []Segment `json:"segments,omitempty"`
}

// TranscriptionFormat represents output format for transcription.
type TranscriptionFormat string

const (
	FormatJSON        TranscriptionFormat = "json"
	FormatText        TranscriptionFormat = "text"
	FormatSRT         TranscriptionFormat = "srt"
	FormatVerboseJSON TranscriptionFormat = "verbose_json"
	FormatVTT         TranscriptionFormat = "vtt"
)

// TranscriptionBuilder provides a fluent API for audio transcription.
type TranscriptionBuilder struct {
	ctx         context.Context
	transcriber AudioTranscriberAPI
	logger      forge.Logger
	metrics     forge.Metrics

	// Request configuration
	file                   io.Reader
	filename               string
	model                  string
	language               string
	prompt                 string
	responseFormat         TranscriptionFormat
	temperature            float64
	timestampGranularities []string
	timeout                time.Duration

	// Callbacks
	onStart    func()
	onComplete func(*TranscriptionResponse)
	onError    func(error)
}

// NewTranscriptionBuilder creates a new transcription builder.
func NewTranscriptionBuilder(ctx context.Context, transcriber AudioTranscriberAPI, logger forge.Logger, metrics forge.Metrics) *TranscriptionBuilder {
	return &TranscriptionBuilder{
		ctx:            ctx,
		transcriber:    transcriber,
		logger:         logger,
		metrics:        metrics,
		model:          "whisper-1",
		responseFormat: FormatJSON,
		timeout:        300 * time.Second, // 5 minutes for audio
	}
}

// FromFile sets the audio from a file path.
func (b *TranscriptionBuilder) FromFile(path string) *TranscriptionBuilder {
	file, err := os.Open(path)
	if err == nil {
		b.file = file
		b.filename = path
	}
	return b
}

// FromReader sets the audio from a reader.
func (b *TranscriptionBuilder) FromReader(reader io.Reader, filename string) *TranscriptionBuilder {
	b.file = reader
	b.filename = filename
	return b
}

// WithModel sets the model.
func (b *TranscriptionBuilder) WithModel(model string) *TranscriptionBuilder {
	b.model = model
	return b
}

// WithLanguage sets the language hint.
func (b *TranscriptionBuilder) WithLanguage(language string) *TranscriptionBuilder {
	b.language = language
	return b
}

// WithPrompt sets the prompt hint.
func (b *TranscriptionBuilder) WithPrompt(prompt string) *TranscriptionBuilder {
	b.prompt = prompt
	return b
}

// WithResponseFormat sets the response format.
func (b *TranscriptionBuilder) WithResponseFormat(format TranscriptionFormat) *TranscriptionBuilder {
	b.responseFormat = format
	return b
}

// WithTemperature sets the temperature.
func (b *TranscriptionBuilder) WithTemperature(temp float64) *TranscriptionBuilder {
	b.temperature = temp
	return b
}

// WithTimestamps enables word/segment timestamps.
func (b *TranscriptionBuilder) WithTimestamps(granularities ...string) *TranscriptionBuilder {
	b.timestampGranularities = granularities
	return b
}

// WithTimeout sets the timeout.
func (b *TranscriptionBuilder) WithTimeout(timeout time.Duration) *TranscriptionBuilder {
	b.timeout = timeout
	return b
}

// OnStart registers a callback for start.
func (b *TranscriptionBuilder) OnStart(fn func()) *TranscriptionBuilder {
	b.onStart = fn
	return b
}

// OnComplete registers a callback for completion.
func (b *TranscriptionBuilder) OnComplete(fn func(*TranscriptionResponse)) *TranscriptionBuilder {
	b.onComplete = fn
	return b
}

// OnError registers a callback for errors.
func (b *TranscriptionBuilder) OnError(fn func(error)) *TranscriptionBuilder {
	b.onError = fn
	return b
}

// Execute performs the transcription.
func (b *TranscriptionBuilder) Execute() (*TranscriptionResponse, error) {
	if b.file == nil {
		return nil, fmt.Errorf("audio file is required")
	}

	if b.onStart != nil {
		b.onStart()
	}

	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	if b.logger != nil {
		b.logger.Debug("Transcribing audio",
			F("model", b.model),
			F("language", b.language),
			F("format", b.responseFormat),
		)
	}

	startTime := time.Now()

	response, err := b.transcriber.Transcribe(ctx, TranscriptionRequest{
		File:                   b.file,
		Filename:               b.filename,
		Model:                  b.model,
		Language:               b.language,
		Prompt:                 b.prompt,
		ResponseFormat:         string(b.responseFormat),
		Temperature:            b.temperature,
		TimestampGranularities: b.timestampGranularities,
	})

	duration := time.Since(startTime)

	if err != nil {
		if b.onError != nil {
			b.onError(err)
		}
		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.transcription.errors", "model", b.model).Inc()
		}
		return nil, err
	}

	if b.logger != nil {
		b.logger.Info("Transcription completed",
			F("model", b.model),
			F("duration", duration),
			F("audio_duration", response.Duration),
		)
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.transcription.success", "model", b.model).Inc()
		b.metrics.Histogram("forge.ai.sdk.transcription.duration", "model", b.model).Observe(duration.Seconds())
	}

	if b.onComplete != nil {
		b.onComplete(response)
	}

	return response, nil
}

// TranslationBuilder provides a fluent API for audio translation.
type TranslationBuilder struct {
	ctx         context.Context
	transcriber AudioTranscriberAPI
	logger      forge.Logger
	metrics     forge.Metrics

	file           io.Reader
	filename       string
	model          string
	prompt         string
	responseFormat TranscriptionFormat
	temperature    float64
	timeout        time.Duration
}

// NewTranslationBuilder creates a new translation builder.
func NewTranslationBuilder(ctx context.Context, transcriber AudioTranscriberAPI, logger forge.Logger, metrics forge.Metrics) *TranslationBuilder {
	return &TranslationBuilder{
		ctx:            ctx,
		transcriber:    transcriber,
		logger:         logger,
		metrics:        metrics,
		model:          "whisper-1",
		responseFormat: FormatJSON,
		timeout:        300 * time.Second,
	}
}

// FromFile sets the audio from a file path.
func (b *TranslationBuilder) FromFile(path string) *TranslationBuilder {
	file, err := os.Open(path)
	if err == nil {
		b.file = file
		b.filename = path
	}
	return b
}

// FromReader sets the audio from a reader.
func (b *TranslationBuilder) FromReader(reader io.Reader, filename string) *TranslationBuilder {
	b.file = reader
	b.filename = filename
	return b
}

// WithModel sets the model.
func (b *TranslationBuilder) WithModel(model string) *TranslationBuilder {
	b.model = model
	return b
}

// WithPrompt sets the prompt hint.
func (b *TranslationBuilder) WithPrompt(prompt string) *TranslationBuilder {
	b.prompt = prompt
	return b
}

// WithResponseFormat sets the response format.
func (b *TranslationBuilder) WithResponseFormat(format TranscriptionFormat) *TranslationBuilder {
	b.responseFormat = format
	return b
}

// WithTemperature sets the temperature.
func (b *TranslationBuilder) WithTemperature(temp float64) *TranslationBuilder {
	b.temperature = temp
	return b
}

// WithTimeout sets the timeout.
func (b *TranslationBuilder) WithTimeout(timeout time.Duration) *TranslationBuilder {
	b.timeout = timeout
	return b
}

// Execute performs the translation.
func (b *TranslationBuilder) Execute() (*TranslationResponse, error) {
	if b.file == nil {
		return nil, fmt.Errorf("audio file is required")
	}

	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	if b.logger != nil {
		b.logger.Debug("Translating audio",
			F("model", b.model),
			F("format", b.responseFormat),
		)
	}

	startTime := time.Now()

	response, err := b.transcriber.TranslateAudio(ctx, TranslationRequest{
		File:           b.file,
		Filename:       b.filename,
		Model:          b.model,
		Prompt:         b.prompt,
		ResponseFormat: string(b.responseFormat),
		Temperature:    b.temperature,
	})

	duration := time.Since(startTime)

	if err != nil {
		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.translation.errors", "model", b.model).Inc()
		}
		return nil, err
	}

	if b.logger != nil {
		b.logger.Info("Translation completed",
			F("model", b.model),
			F("duration", duration),
		)
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.translation.success", "model", b.model).Inc()
		b.metrics.Histogram("forge.ai.sdk.translation.duration", "model", b.model).Observe(duration.Seconds())
	}

	return response, nil
}

// TimestampedTranscript represents a transcript with timestamps.
type TimestampedTranscript struct {
	Text     string            `json:"text"`
	Language string            `json:"language"`
	Duration float64           `json:"duration"`
	Entries  []TranscriptEntry `json:"entries"`
}

// TranscriptEntry represents a single transcript entry with timing.
type TranscriptEntry struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// ToTimestampedTranscript converts a transcription response to a timestamped transcript.
func (r *TranscriptionResponse) ToTimestampedTranscript() *TimestampedTranscript {
	transcript := &TimestampedTranscript{
		Text:     r.Text,
		Language: r.Language,
		Duration: r.Duration,
		Entries:  make([]TranscriptEntry, 0),
	}

	// Use segments if available
	if len(r.Segments) > 0 {
		for _, seg := range r.Segments {
			transcript.Entries = append(transcript.Entries, TranscriptEntry{
				Start: seg.Start,
				End:   seg.End,
				Text:  seg.Text,
			})
		}
	} else if len(r.Words) > 0 {
		// Fall back to words
		for _, word := range r.Words {
			transcript.Entries = append(transcript.Entries, TranscriptEntry{
				Start: word.Start,
				End:   word.End,
				Text:  word.Word,
			})
		}
	}

	return transcript
}

// ToSRT converts the transcript to SRT format.
func (t *TimestampedTranscript) ToSRT() string {
	var result string
	for i, entry := range t.Entries {
		startTime := formatSRTTime(entry.Start)
		endTime := formatSRTTime(entry.End)
		result += fmt.Sprintf("%d\n%s --> %s\n%s\n\n", i+1, startTime, endTime, entry.Text)
	}
	return result
}

// ToVTT converts the transcript to WebVTT format.
func (t *TimestampedTranscript) ToVTT() string {
	result := "WEBVTT\n\n"
	for _, entry := range t.Entries {
		startTime := formatVTTTime(entry.Start)
		endTime := formatVTTTime(entry.End)
		result += fmt.Sprintf("%s --> %s\n%s\n\n", startTime, endTime, entry.Text)
	}
	return result
}

func formatSRTTime(seconds float64) string {
	hours := int(seconds) / 3600
	minutes := (int(seconds) % 3600) / 60
	secs := int(seconds) % 60
	millis := int((seconds - float64(int(seconds))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, secs, millis)
}

func formatVTTTime(seconds float64) string {
	hours := int(seconds) / 3600
	minutes := (int(seconds) % 3600) / 60
	secs := int(seconds) % 60
	millis := int((seconds - float64(int(seconds))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, millis)
}
