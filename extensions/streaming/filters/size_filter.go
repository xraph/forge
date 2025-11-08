package filters

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// SizeFilterConfig configures size-based filtering.
type SizeFilterConfig struct {
	// Max message size in bytes
	MaxMessageSize int

	// Truncation
	EnableTruncation bool
	TruncateLength   int

	// Compression
	EnableCompression     bool
	CompressionThreshold  int     // Compress if larger than this
	CompressionMinSavings float64 // Only use compression if saves this % (0.0-1.0)
}

// sizeFilter enforces message size limits.
type sizeFilter struct {
	config SizeFilterConfig
}

// NewSizeFilter creates a size filter.
func NewSizeFilter(config SizeFilterConfig) MessageFilter {
	return &sizeFilter{
		config: config,
	}
}

func (sf *sizeFilter) Name() string {
	return "size_filter"
}

func (sf *sizeFilter) Priority() int {
	return 30 // Late in chain (after content and metadata)
}

func (sf *sizeFilter) Filter(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
	// Calculate message size
	size, err := sf.calculateSize(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate message size: %w", err)
	}

	// Check max size
	if sf.config.MaxMessageSize > 0 && size > sf.config.MaxMessageSize {
		// Try truncation
		if sf.config.EnableTruncation {
			truncated, err := sf.truncateMessage(msg)
			if err != nil {
				return nil, err
			}

			msg = truncated
			size, _ = sf.calculateSize(msg)
		} else {
			return nil, nil // Block oversized message
		}
	}

	// Apply compression if beneficial
	if sf.config.EnableCompression && size > sf.config.CompressionThreshold {
		compressed, err := sf.compressMessage(msg)
		if err != nil {
			// Compression failed, use original
			return msg, nil
		}

		compressedSize, _ := sf.calculateSize(compressed)
		savings := 1.0 - (float64(compressedSize) / float64(size))

		if savings >= sf.config.CompressionMinSavings {
			return compressed, nil
		}
	}

	return msg, nil
}

func (sf *sizeFilter) calculateSize(msg *streaming.Message) (int, error) {
	// Serialize to JSON to get size
	data, err := json.Marshal(msg)
	if err != nil {
		return 0, err
	}

	return len(data), nil
}

func (sf *sizeFilter) truncateMessage(msg *streaming.Message) (*streaming.Message, error) {
	truncated := *msg

	// Truncate text data
	if text, ok := msg.Data.(string); ok {
		if len(text) > sf.config.TruncateLength {
			truncated.Data = text[:sf.config.TruncateLength] + "..."
			if truncated.Metadata == nil {
				truncated.Metadata = make(map[string]any)
			}

			truncated.Metadata["truncated"] = true
			truncated.Metadata["original_length"] = len(text)
		}
	}

	return &truncated, nil
}

func (sf *sizeFilter) compressMessage(msg *streaming.Message) (*streaming.Message, error) {
	// Serialize message
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return nil, err
	}

	// Compress with gzip
	var buf bytes.Buffer

	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(data); err != nil {
		return nil, err
	}

	if err := gzWriter.Close(); err != nil {
		return nil, err
	}

	// Create compressed message
	compressed := *msg

	compressed.Data = buf.Bytes()
	if compressed.Metadata == nil {
		compressed.Metadata = make(map[string]any)
	}

	compressed.Metadata["compressed"] = true
	compressed.Metadata["compression"] = "gzip"
	compressed.Metadata["original_size"] = len(data)

	return &compressed, nil
}
