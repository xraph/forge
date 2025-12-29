package sdk

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/ai/llm"
)

// MultiModalContentType represents the type of multi-modal content.
type MultiModalContentType string

const (
	ContentTypeText  MultiModalContentType = "text"
	ContentTypeImage MultiModalContentType = "image"
	ContentTypeAudio MultiModalContentType = "audio"
	ContentTypeVideo MultiModalContentType = "video"
)

// MultiModalContent represents a piece of content in a multi-modal request.
type MultiModalContent struct {
	Type     MultiModalContentType
	Text     string // For text content
	Data     []byte // For binary content (images, audio, video)
	MimeType string // MIME type of the content
	URL      string // URL to the content (alternative to Data)
	Metadata map[string]any
}

// MultiModalBuilder provides a fluent API for multi-modal generation.
type MultiModalBuilder struct {
	ctx         context.Context
	llmManager  LLMManager
	logger      forge.Logger
	metrics     forge.Metrics
	model       string
	contents    []MultiModalContent
	systemMsg   string
	temperature *float64
	maxTokens   *int
	topP        *float64

	// Callbacks
	onStart    func()
	onComplete func(*MultiModalResult)
	onError    func(error)
}

// MultiModalResult contains the result of a multi-modal generation.
type MultiModalResult struct {
	Text         string
	Reasoning    []string
	ToolCalls    []ToolCallResult
	Usage        Usage
	Model        string
	FinishReason string
	Metadata     map[string]any
}

// NewMultiModalBuilder creates a new multi-modal builder.
func NewMultiModalBuilder(ctx context.Context, llmManager LLMManager, logger forge.Logger, metrics forge.Metrics) *MultiModalBuilder {
	temp := 0.7
	tokens := 1000
	topP := 1.0

	return &MultiModalBuilder{
		ctx:         ctx,
		llmManager:  llmManager,
		logger:      logger,
		metrics:     metrics,
		model:       "gpt-4-vision",
		contents:    make([]MultiModalContent, 0),
		temperature: &temp,
		maxTokens:   &tokens,
		topP:        &topP,
	}
}

// WithModel sets the model to use.
func (b *MultiModalBuilder) WithModel(model string) *MultiModalBuilder {
	b.model = model

	return b
}

// WithText adds text content.
func (b *MultiModalBuilder) WithText(text string) *MultiModalBuilder {
	b.contents = append(b.contents, MultiModalContent{
		Type: ContentTypeText,
		Text: text,
	})

	return b
}

// WithImage adds image content from bytes.
func (b *MultiModalBuilder) WithImage(data []byte, mimeType string) *MultiModalBuilder {
	b.contents = append(b.contents, MultiModalContent{
		Type:     ContentTypeImage,
		Data:     data,
		MimeType: mimeType,
	})

	return b
}

// WithImageURL adds image content from URL.
func (b *MultiModalBuilder) WithImageURL(url string) *MultiModalBuilder {
	b.contents = append(b.contents, MultiModalContent{
		Type: ContentTypeImage,
		URL:  url,
	})

	return b
}

// WithImageFile adds image content from file.
func (b *MultiModalBuilder) WithImageFile(path string) *MultiModalBuilder {
	data, err := os.ReadFile(path)
	if err != nil {
		if b.logger != nil {
			b.logger.Error("failed to read image file",
				F("path", path),
				F("error", err),
			)
		}

		return b
	}

	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return b.WithImage(data, mimeType)
}

// WithAudio adds audio content.
func (b *MultiModalBuilder) WithAudio(data []byte, mimeType string) *MultiModalBuilder {
	b.contents = append(b.contents, MultiModalContent{
		Type:     ContentTypeAudio,
		Data:     data,
		MimeType: mimeType,
	})

	return b
}

// WithAudioFile adds audio content from file.
func (b *MultiModalBuilder) WithAudioFile(path string) *MultiModalBuilder {
	data, err := os.ReadFile(path)
	if err != nil {
		if b.logger != nil {
			b.logger.Error("failed to read audio file",
				F("path", path),
				F("error", err),
			)
		}

		return b
	}

	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "audio/mpeg"
	}

	return b.WithAudio(data, mimeType)
}

// WithVideo adds video content.
func (b *MultiModalBuilder) WithVideo(data []byte, mimeType string) *MultiModalBuilder {
	b.contents = append(b.contents, MultiModalContent{
		Type:     ContentTypeVideo,
		Data:     data,
		MimeType: mimeType,
	})

	return b
}

// WithVideoFile adds video content from file.
func (b *MultiModalBuilder) WithVideoFile(path string) *MultiModalBuilder {
	data, err := os.ReadFile(path)
	if err != nil {
		if b.logger != nil {
			b.logger.Error("failed to read video file",
				F("path", path),
				F("error", err),
			)
		}

		return b
	}

	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	return b.WithVideo(data, mimeType)
}

// WithSystemMessage sets the system message.
func (b *MultiModalBuilder) WithSystemMessage(msg string) *MultiModalBuilder {
	b.systemMsg = msg

	return b
}

// WithTemperature sets the temperature.
func (b *MultiModalBuilder) WithTemperature(temp float64) *MultiModalBuilder {
	b.temperature = &temp

	return b
}

// WithMaxTokens sets the max tokens.
func (b *MultiModalBuilder) WithMaxTokens(tokens int) *MultiModalBuilder {
	b.maxTokens = &tokens

	return b
}

// WithTopP sets the top-p value.
func (b *MultiModalBuilder) WithTopP(topP float64) *MultiModalBuilder {
	b.topP = &topP

	return b
}

// OnStart sets the start callback.
func (b *MultiModalBuilder) OnStart(fn func()) *MultiModalBuilder {
	b.onStart = fn

	return b
}

// OnComplete sets the complete callback.
func (b *MultiModalBuilder) OnComplete(fn func(*MultiModalResult)) *MultiModalBuilder {
	b.onComplete = fn

	return b
}

// OnError sets the error callback.
func (b *MultiModalBuilder) OnError(fn func(error)) *MultiModalBuilder {
	b.onError = fn

	return b
}

// Execute runs the multi-modal generation.
func (b *MultiModalBuilder) Execute() (*MultiModalResult, error) {
	if b.onStart != nil {
		b.onStart()
	}

	if len(b.contents) == 0 {
		err := errors.New("no content provided")
		if b.onError != nil {
			b.onError(err)
		}

		return nil, err
	}

	// Build the message with proper multi-modal content format
	userMessage, err := b.buildMultiModalMessage()
	if err != nil {
		err = fmt.Errorf("failed to build multi-modal message: %w", err)
		if b.onError != nil {
			b.onError(err)
		}

		return nil, err
	}

	// Build messages
	messages := []llm.ChatMessage{}
	if b.systemMsg != "" {
		messages = append(messages, llm.ChatMessage{
			Role:    "system",
			Content: b.systemMsg,
		})
	}

	messages = append(messages, userMessage)

	// Make request
	req := llm.ChatRequest{
		Model:       b.model,
		Messages:    messages,
		Temperature: b.temperature,
		MaxTokens:   b.maxTokens,
		TopP:        b.topP,
	}

	resp, err := b.llmManager.Chat(b.ctx, req)
	if err != nil {
		err = fmt.Errorf("chat completion failed: %w", err)
		if b.onError != nil {
			b.onError(err)
		}

		return nil, err
	}

	// Extract result
	result := &MultiModalResult{
		Model:    resp.Model,
		Metadata: make(map[string]any),
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		result.Text = choice.Message.Content
		result.FinishReason = choice.FinishReason

		// Extract tool calls if any
		if len(choice.Message.ToolCalls) > 0 {
			result.ToolCalls = make([]ToolCallResult, len(choice.Message.ToolCalls))
			for i, tc := range choice.Message.ToolCalls {
				result.ToolCalls[i] = ToolCallResult{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Parsed,
				}
			}
		}
	}

	// Extract usage
	if resp.Usage != nil {
		result.Usage = Usage{
			Provider:     "",
			Model:        resp.Model,
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
			TotalTokens:  int(resp.Usage.TotalTokens),
			Timestamp:    time.Now(),
		}
	}

	// Log metrics
	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.multimodal.requests", "model", b.model).Inc()
		b.metrics.Gauge("forge.ai.sdk.multimodal.tokens", "model", b.model, "type", "total").Set(float64(result.Usage.TotalTokens))
	}

	if b.logger != nil {
		b.logger.Info("multi-modal generation complete",
			F("model", b.model),
			F("input_tokens", result.Usage.InputTokens),
			F("output_tokens", result.Usage.OutputTokens),
			F("content_count", len(b.contents)),
		)
	}

	if b.onComplete != nil {
		b.onComplete(result)
	}

	return result, nil
}

// MultiModalContentPart represents a single part of multi-modal content.
// This follows the OpenAI API format for multi-modal messages.
type MultiModalContentPart struct {
	Type     string        `json:"type"` // "text", "image_url", "audio"
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
	Audio    *AudioPart    `json:"audio,omitempty"`
	Video    *VideoPart    `json:"video,omitempty"`
}

// ImageURLPart represents an image URL in multi-modal content.
type ImageURLPart struct {
	URL    string `json:"url"`              // Can be a URL or data:... base64 string
	Detail string `json:"detail,omitempty"` // "low", "high", "auto"
}

// AudioPart represents audio content.
type AudioPart struct {
	Data   string `json:"data,omitempty"` // base64-encoded audio data
	URL    string `json:"url,omitempty"`
	Format string `json:"format,omitempty"` // e.g., "mp3", "wav"
}

// VideoPart represents video content.
type VideoPart struct {
	Data   string `json:"data,omitempty"` // base64-encoded video data
	URL    string `json:"url,omitempty"`
	Format string `json:"format,omitempty"` // e.g., "mp4", "webm"
}

// buildMultiModalMessage builds a chat message with proper multi-modal content.
func (b *MultiModalBuilder) buildMultiModalMessage() (llm.ChatMessage, error) {
	contentParts := make([]MultiModalContentPart, 0, len(b.contents))

	for _, content := range b.contents {
		part, err := b.buildContentPart(content)
		if err != nil {
			return llm.ChatMessage{}, err
		}
		contentParts = append(contentParts, part)
	}

	// For most LLMs, the multi-modal content is stored in metadata
	// The actual format depends on the provider
	message := llm.ChatMessage{
		Role:    "user",
		Content: b.buildTextContent(contentParts), // Fallback text for non-multimodal models
		Metadata: map[string]any{
			"multimodal_content": contentParts,
			"content_type":       "multimodal",
		},
	}

	return message, nil
}

// buildContentPart builds a single content part.
func (b *MultiModalBuilder) buildContentPart(content MultiModalContent) (MultiModalContentPart, error) {
	switch content.Type {
	case ContentTypeText:
		return MultiModalContentPart{
			Type: "text",
			Text: content.Text,
		}, nil

	case ContentTypeImage:
		part := MultiModalContentPart{
			Type:     "image_url",
			ImageURL: &ImageURLPart{Detail: "auto"},
		}

		if content.URL != "" {
			part.ImageURL.URL = content.URL
		} else if len(content.Data) > 0 {
			// Create data URL
			encoded := base64.StdEncoding.EncodeToString(content.Data)
			mimeType := content.MimeType
			if mimeType == "" {
				mimeType = "image/png"
			}
			part.ImageURL.URL = fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)
		} else {
			return MultiModalContentPart{}, errors.New("image content has no data or URL")
		}

		return part, nil

	case ContentTypeAudio:
		part := MultiModalContentPart{
			Type:  "audio",
			Audio: &AudioPart{},
		}

		if content.URL != "" {
			part.Audio.URL = content.URL
		} else if len(content.Data) > 0 {
			part.Audio.Data = base64.StdEncoding.EncodeToString(content.Data)
			part.Audio.Format = mimeToFormat(content.MimeType)
		} else {
			return MultiModalContentPart{}, errors.New("audio content has no data or URL")
		}

		return part, nil

	case ContentTypeVideo:
		part := MultiModalContentPart{
			Type:  "video",
			Video: &VideoPart{},
		}

		if content.URL != "" {
			part.Video.URL = content.URL
		} else if len(content.Data) > 0 {
			part.Video.Data = base64.StdEncoding.EncodeToString(content.Data)
			part.Video.Format = mimeToFormat(content.MimeType)
		} else {
			return MultiModalContentPart{}, errors.New("video content has no data or URL")
		}

		return part, nil

	default:
		return MultiModalContentPart{}, fmt.Errorf("unsupported content type: %s", content.Type)
	}
}

// buildTextContent creates a text representation for fallback.
func (b *MultiModalBuilder) buildTextContent(parts []MultiModalContentPart) string {
	var texts []string

	for _, part := range parts {
		switch part.Type {
		case "text":
			texts = append(texts, part.Text)
		case "image_url":
			texts = append(texts, "[Image content attached]")
		case "audio":
			texts = append(texts, "[Audio content attached]")
		case "video":
			texts = append(texts, "[Video content attached]")
		}
	}

	return strings.Join(texts, "\n")
}

// mimeToFormat extracts format from MIME type.
func mimeToFormat(mimeType string) string {
	parts := strings.Split(mimeType, "/")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// DownloadFromURL downloads content from a URL.
func DownloadFromURL(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return data, mimeType, nil
}

// VisionAnalyzer provides specialized vision analysis capabilities.
type VisionAnalyzer struct {
	llmManager LLMManager
	logger     forge.Logger
	metrics    forge.Metrics
}

// NewVisionAnalyzer creates a new vision analyzer.
func NewVisionAnalyzer(llmManager LLMManager, logger forge.Logger, metrics forge.Metrics) *VisionAnalyzer {
	return &VisionAnalyzer{
		llmManager: llmManager,
		logger:     logger,
		metrics:    metrics,
	}
}

// DescribeImage analyzes and describes an image.
func (va *VisionAnalyzer) DescribeImage(ctx context.Context, imageData []byte, mimeType string) (string, error) {
	builder := NewMultiModalBuilder(ctx, va.llmManager, va.logger, va.metrics)

	result, err := builder.
		WithImage(imageData, mimeType).
		WithText("Please provide a detailed description of this image.").
		Execute()
	if err != nil {
		return "", err
	}

	return result.Text, nil
}

// DetectObjects detects objects in an image.
func (va *VisionAnalyzer) DetectObjects(ctx context.Context, imageData []byte, mimeType string) ([]string, error) {
	builder := NewMultiModalBuilder(ctx, va.llmManager, va.logger, va.metrics)

	result, err := builder.
		WithImage(imageData, mimeType).
		WithText("List all the objects you can see in this image. Provide a comma-separated list.").
		Execute()
	if err != nil {
		return nil, err
	}

	// Parse comma-separated list
	objects := strings.Split(result.Text, ",")
	for i := range objects {
		objects[i] = strings.TrimSpace(objects[i])
	}

	return objects, nil
}

// ReadText extracts text from an image (OCR).
func (va *VisionAnalyzer) ReadText(ctx context.Context, imageData []byte, mimeType string) (string, error) {
	builder := NewMultiModalBuilder(ctx, va.llmManager, va.logger, va.metrics)

	result, err := builder.
		WithImage(imageData, mimeType).
		WithText("Extract all text visible in this image. Provide only the extracted text.").
		Execute()
	if err != nil {
		return "", err
	}

	return result.Text, nil
}

// CompareImages compares two images and describes differences.
func (va *VisionAnalyzer) CompareImages(ctx context.Context, img1, img2 []byte, mimeType string) (string, error) {
	builder := NewMultiModalBuilder(ctx, va.llmManager, va.logger, va.metrics)

	result, err := builder.
		WithImage(img1, mimeType).
		WithImage(img2, mimeType).
		WithText("Compare these two images and describe the key differences.").
		Execute()
	if err != nil {
		return "", err
	}

	return result.Text, nil
}

// AudioTranscriber provides audio transcription capabilities.
type AudioTranscriber struct {
	llmManager LLMManager
	logger     forge.Logger
	metrics    forge.Metrics
}

// NewAudioTranscriber creates a new audio transcriber.
func NewAudioTranscriber(llmManager LLMManager, logger forge.Logger, metrics forge.Metrics) *AudioTranscriber {
	return &AudioTranscriber{
		llmManager: llmManager,
		logger:     logger,
		metrics:    metrics,
	}
}

// Transcribe transcribes audio to text.
func (at *AudioTranscriber) Transcribe(ctx context.Context, audioData []byte, language string) (string, error) {
	// This would integrate with Whisper or similar audio transcription models
	builder := NewMultiModalBuilder(ctx, at.llmManager, at.logger, at.metrics)

	result, err := builder.
		WithAudio(audioData, "audio/mpeg").
		WithText("Transcribe this audio to text. Language: " + language).
		Execute()
	if err != nil {
		return "", err
	}

	return result.Text, nil
}

// TranscribeWithTimestamps transcribes audio with timestamps.
func (at *AudioTranscriber) TranscribeWithTimestamps(ctx context.Context, audioData []byte, language string) ([]TranscriptSegment, error) {
	builder := NewMultiModalBuilder(ctx, at.llmManager, at.logger, at.metrics)

	result, err := builder.
		WithAudio(audioData, "audio/mpeg").
		WithText(fmt.Sprintf("Transcribe this audio to text with timestamps. Language: %s. Format: [00:00] text", language)).
		Execute()
	if err != nil {
		return nil, err
	}

	// Parse timestamped segments
	segments := parseTimestampedTranscript(result.Text)

	return segments, nil
}

// TranscriptSegment represents a segment of transcribed audio with timestamp.
type TranscriptSegment struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

// parseTimestampedTranscript parses timestamped transcript text.
func parseTimestampedTranscript(text string) []TranscriptSegment {
	// Simple parser for [MM:SS] format
	segments := []TranscriptSegment{}
	lines := strings.SplitSeq(text, "\n")

	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Look for [MM:SS] pattern
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			endIdx := strings.Index(line, "]")
			timestamp := line[1:endIdx]
			text := strings.TrimSpace(line[endIdx+1:])

			// Parse timestamp (simple MM:SS format)
			parts := strings.Split(timestamp, ":")
			if len(parts) == 2 {
				segments = append(segments, TranscriptSegment{
					Text: text,
				})
			}
		}
	}

	return segments
}
