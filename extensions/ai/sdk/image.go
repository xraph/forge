package sdk

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/xraph/forge"
)

// ImageSize represents image dimensions.
type ImageSize string

const (
	Size256x256   ImageSize = "256x256"
	Size512x512   ImageSize = "512x512"
	Size1024x1024 ImageSize = "1024x1024"
	Size1792x1024 ImageSize = "1792x1024"
	Size1024x1792 ImageSize = "1024x1792"
)

// ImageStyle represents image style.
type ImageStyle string

const (
	StyleVivid   ImageStyle = "vivid"
	StyleNatural ImageStyle = "natural"
)

// ImageQuality represents image quality.
type ImageQuality string

const (
	QualityStandard ImageQuality = "standard"
	QualityHD       ImageQuality = "hd"
)

// ImageResponseFormat represents how the image is returned.
type ImageResponseFormat string

const (
	ResponseFormatURL     ImageResponseFormat = "url"
	ResponseFormatB64JSON ImageResponseFormat = "b64_json"
)

// ImageGenerator generates images from text prompts.
type ImageGenerator interface {
	Generate(ctx context.Context, request ImageRequest) (*ImageResponse, error)
	Edit(ctx context.Context, request ImageEditRequest) (*ImageResponse, error)
	CreateVariation(ctx context.Context, request ImageVariationRequest) (*ImageResponse, error)
}

// ImageRequest represents an image generation request.
type ImageRequest struct {
	Prompt         string              `json:"prompt"`
	Model          string              `json:"model,omitempty"`
	N              int                 `json:"n,omitempty"`
	Size           ImageSize           `json:"size,omitempty"`
	Style          ImageStyle          `json:"style,omitempty"`
	Quality        ImageQuality        `json:"quality,omitempty"`
	ResponseFormat ImageResponseFormat `json:"response_format,omitempty"`
	User           string              `json:"user,omitempty"`
}

// ImageEditRequest represents an image edit request.
type ImageEditRequest struct {
	Image          []byte              `json:"-"`
	Mask           []byte              `json:"-"`
	Prompt         string              `json:"prompt"`
	Model          string              `json:"model,omitempty"`
	N              int                 `json:"n,omitempty"`
	Size           ImageSize           `json:"size,omitempty"`
	ResponseFormat ImageResponseFormat `json:"response_format,omitempty"`
	User           string              `json:"user,omitempty"`
}

// ImageVariationRequest represents an image variation request.
type ImageVariationRequest struct {
	Image          []byte              `json:"-"`
	Model          string              `json:"model,omitempty"`
	N              int                 `json:"n,omitempty"`
	Size           ImageSize           `json:"size,omitempty"`
	ResponseFormat ImageResponseFormat `json:"response_format,omitempty"`
	User           string              `json:"user,omitempty"`
}

// ImageResponse represents an image generation response.
type ImageResponse struct {
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`
	Usage   *ImageUsage `json:"usage,omitempty"`
}

// ImageData represents a single generated image.
type ImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ImageUsage represents image generation usage.
type ImageUsage struct {
	PromptTokens int     `json:"prompt_tokens,omitempty"`
	TotalTokens  int     `json:"total_tokens,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
}

// GetBytes returns the image as bytes.
func (d *ImageData) GetBytes() ([]byte, error) {
	if d.B64JSON != "" {
		return base64.StdEncoding.DecodeString(d.B64JSON)
	}

	if d.URL != "" {
		resp, err := http.Get(d.URL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		return io.ReadAll(resp.Body)
	}

	return nil, errors.New("no image data available")
}

// ImageBuilder provides a fluent API for image generation.
type ImageBuilder struct {
	ctx       context.Context
	generator ImageGenerator
	logger    forge.Logger
	metrics   forge.Metrics

	// Request configuration
	prompt         string
	model          string
	n              int
	size           ImageSize
	style          ImageStyle
	quality        ImageQuality
	responseFormat ImageResponseFormat
	user           string
	timeout        time.Duration

	// Callbacks
	onStart    func()
	onComplete func(*ImageResponse)
	onError    func(error)
}

// NewImageBuilder creates a new image builder.
func NewImageBuilder(ctx context.Context, generator ImageGenerator, logger forge.Logger, metrics forge.Metrics) *ImageBuilder {
	return &ImageBuilder{
		ctx:            ctx,
		generator:      generator,
		logger:         logger,
		metrics:        metrics,
		model:          "dall-e-3",
		n:              1,
		size:           Size1024x1024,
		style:          StyleVivid,
		quality:        QualityStandard,
		responseFormat: ResponseFormatURL,
		timeout:        60 * time.Second,
	}
}

// WithPrompt sets the prompt.
func (b *ImageBuilder) WithPrompt(prompt string) *ImageBuilder {
	b.prompt = prompt

	return b
}

// WithModel sets the model.
func (b *ImageBuilder) WithModel(model string) *ImageBuilder {
	b.model = model

	return b
}

// WithCount sets the number of images to generate.
func (b *ImageBuilder) WithCount(n int) *ImageBuilder {
	b.n = n

	return b
}

// WithSize sets the image size.
func (b *ImageBuilder) WithSize(size ImageSize) *ImageBuilder {
	b.size = size

	return b
}

// WithStyle sets the image style.
func (b *ImageBuilder) WithStyle(style ImageStyle) *ImageBuilder {
	b.style = style

	return b
}

// WithQuality sets the image quality.
func (b *ImageBuilder) WithQuality(quality ImageQuality) *ImageBuilder {
	b.quality = quality

	return b
}

// WithResponseFormat sets the response format.
func (b *ImageBuilder) WithResponseFormat(format ImageResponseFormat) *ImageBuilder {
	b.responseFormat = format

	return b
}

// WithUser sets the user identifier.
func (b *ImageBuilder) WithUser(user string) *ImageBuilder {
	b.user = user

	return b
}

// WithTimeout sets the timeout.
func (b *ImageBuilder) WithTimeout(timeout time.Duration) *ImageBuilder {
	b.timeout = timeout

	return b
}

// OnStart registers a callback for start.
func (b *ImageBuilder) OnStart(fn func()) *ImageBuilder {
	b.onStart = fn

	return b
}

// OnComplete registers a callback for completion.
func (b *ImageBuilder) OnComplete(fn func(*ImageResponse)) *ImageBuilder {
	b.onComplete = fn

	return b
}

// OnError registers a callback for errors.
func (b *ImageBuilder) OnError(fn func(error)) *ImageBuilder {
	b.onError = fn

	return b
}

// Generate generates images.
func (b *ImageBuilder) Generate() (*ImageResponse, error) {
	if b.prompt == "" {
		return nil, errors.New("prompt is required")
	}

	if b.onStart != nil {
		b.onStart()
	}

	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	if b.logger != nil {
		b.logger.Debug("Generating image",
			F("model", b.model),
			F("size", b.size),
			F("style", b.style),
		)
	}

	startTime := time.Now()

	response, err := b.generator.Generate(ctx, ImageRequest{
		Prompt:         b.prompt,
		Model:          b.model,
		N:              b.n,
		Size:           b.size,
		Style:          b.style,
		Quality:        b.quality,
		ResponseFormat: b.responseFormat,
		User:           b.user,
	})

	duration := time.Since(startTime)

	if err != nil {
		if b.onError != nil {
			b.onError(err)
		}

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.image.errors", "model", b.model).Inc()
		}

		return nil, err
	}

	if b.logger != nil {
		b.logger.Info("Image generation completed",
			F("model", b.model),
			F("count", len(response.Data)),
			F("duration", duration),
		)
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.image.success", "model", b.model).Inc()
		b.metrics.Histogram("forge.ai.sdk.image.duration", "model", b.model).Observe(duration.Seconds())
	}

	if b.onComplete != nil {
		b.onComplete(response)
	}

	return response, nil
}

// ImageEditBuilder provides a fluent API for image editing.
type ImageEditBuilder struct {
	ctx       context.Context
	generator ImageGenerator
	logger    forge.Logger
	metrics   forge.Metrics

	image          []byte
	mask           []byte
	prompt         string
	model          string
	n              int
	size           ImageSize
	responseFormat ImageResponseFormat
	user           string
	timeout        time.Duration
}

// NewImageEditBuilder creates a new image edit builder.
func NewImageEditBuilder(ctx context.Context, generator ImageGenerator, logger forge.Logger, metrics forge.Metrics) *ImageEditBuilder {
	return &ImageEditBuilder{
		ctx:            ctx,
		generator:      generator,
		logger:         logger,
		metrics:        metrics,
		model:          "dall-e-2",
		n:              1,
		size:           Size1024x1024,
		responseFormat: ResponseFormatURL,
		timeout:        60 * time.Second,
	}
}

// WithImage sets the image to edit.
func (b *ImageEditBuilder) WithImage(image []byte) *ImageEditBuilder {
	b.image = image

	return b
}

// WithMask sets the mask for editing.
func (b *ImageEditBuilder) WithMask(mask []byte) *ImageEditBuilder {
	b.mask = mask

	return b
}

// WithPrompt sets the prompt.
func (b *ImageEditBuilder) WithPrompt(prompt string) *ImageEditBuilder {
	b.prompt = prompt

	return b
}

// WithModel sets the model.
func (b *ImageEditBuilder) WithModel(model string) *ImageEditBuilder {
	b.model = model

	return b
}

// WithCount sets the number of images.
func (b *ImageEditBuilder) WithCount(n int) *ImageEditBuilder {
	b.n = n

	return b
}

// WithSize sets the size.
func (b *ImageEditBuilder) WithSize(size ImageSize) *ImageEditBuilder {
	b.size = size

	return b
}

// WithResponseFormat sets the response format.
func (b *ImageEditBuilder) WithResponseFormat(format ImageResponseFormat) *ImageEditBuilder {
	b.responseFormat = format

	return b
}

// Edit edits the image.
func (b *ImageEditBuilder) Edit() (*ImageResponse, error) {
	if b.image == nil {
		return nil, errors.New("image is required")
	}

	if b.prompt == "" {
		return nil, errors.New("prompt is required")
	}

	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	return b.generator.Edit(ctx, ImageEditRequest{
		Image:          b.image,
		Mask:           b.mask,
		Prompt:         b.prompt,
		Model:          b.model,
		N:              b.n,
		Size:           b.size,
		ResponseFormat: b.responseFormat,
		User:           b.user,
	})
}

// ImageVariationBuilder provides a fluent API for image variations.
type ImageVariationBuilder struct {
	ctx       context.Context
	generator ImageGenerator
	logger    forge.Logger
	metrics   forge.Metrics

	image          []byte
	model          string
	n              int
	size           ImageSize
	responseFormat ImageResponseFormat
	user           string
	timeout        time.Duration
}

// NewImageVariationBuilder creates a new variation builder.
func NewImageVariationBuilder(ctx context.Context, generator ImageGenerator, logger forge.Logger, metrics forge.Metrics) *ImageVariationBuilder {
	return &ImageVariationBuilder{
		ctx:            ctx,
		generator:      generator,
		logger:         logger,
		metrics:        metrics,
		model:          "dall-e-2",
		n:              1,
		size:           Size1024x1024,
		responseFormat: ResponseFormatURL,
		timeout:        60 * time.Second,
	}
}

// WithImage sets the source image.
func (b *ImageVariationBuilder) WithImage(image []byte) *ImageVariationBuilder {
	b.image = image

	return b
}

// WithModel sets the model.
func (b *ImageVariationBuilder) WithModel(model string) *ImageVariationBuilder {
	b.model = model

	return b
}

// WithCount sets the number of variations.
func (b *ImageVariationBuilder) WithCount(n int) *ImageVariationBuilder {
	b.n = n

	return b
}

// WithSize sets the size.
func (b *ImageVariationBuilder) WithSize(size ImageSize) *ImageVariationBuilder {
	b.size = size

	return b
}

// WithResponseFormat sets the response format.
func (b *ImageVariationBuilder) WithResponseFormat(format ImageResponseFormat) *ImageVariationBuilder {
	b.responseFormat = format

	return b
}

// CreateVariation creates image variations.
func (b *ImageVariationBuilder) CreateVariation() (*ImageResponse, error) {
	if b.image == nil {
		return nil, errors.New("image is required")
	}

	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	return b.generator.CreateVariation(ctx, ImageVariationRequest{
		Image:          b.image,
		Model:          b.model,
		N:              b.n,
		Size:           b.size,
		ResponseFormat: b.responseFormat,
		User:           b.user,
	})
}
