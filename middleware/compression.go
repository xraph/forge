package middleware

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// CompressionMiddleware provides request/response compression
type CompressionMiddleware struct {
	*BaseMiddleware
	config CompressionConfig
	pools  *compressionPools
}

// CompressionConfig represents compression configuration
type CompressionConfig struct {
	// Compression types
	Types []CompressionType `json:"types"`

	// Compression levels
	GzipLevel    int `json:"gzip_level"`
	DeflateLevel int `json:"deflate_level"`

	// Content type filtering
	ContentTypes []string `json:"content_types"`
	ExcludeTypes []string `json:"exclude_types"`

	// Size thresholds
	MinSize int64 `json:"min_size"`
	MaxSize int64 `json:"max_size"`

	// Path filtering
	IncludePaths []string `json:"include_paths"`
	ExcludePaths []string `json:"exclude_paths"`

	// Vary header
	VaryHeader bool `json:"vary_header"`

	// Decompression
	AllowDecompression bool  `json:"allow_decompression"`
	MaxDecompressSize  int64 `json:"max_decompress_size"`

	// Performance
	PoolSize         int  `json:"pool_size"`
	BufferSize       int  `json:"buffer_size"`
	FlushInterval    int  `json:"flush_interval"`
	DisableStreaming bool `json:"disable_streaming"`
}

// CompressionType represents compression algorithms
type CompressionType string

const (
	CompressionGzip    CompressionType = "gzip"
	CompressionDeflate CompressionType = "deflate"
	CompressionBrotli  CompressionType = "br"
)

// compressionPools manages compression writer pools
type compressionPools struct {
	gzipPool    sync.Pool
	deflatePool sync.Pool
	bufferPool  sync.Pool
}

// NewCompressionMiddleware creates a new compression middleware
func NewCompressionMiddleware(config CompressionConfig) Middleware {
	pools := &compressionPools{
		gzipPool: sync.Pool{
			New: func() interface{} {
				writer, _ := gzip.NewWriterLevel(nil, config.GzipLevel)
				return writer
			},
		},
		deflatePool: sync.Pool{
			New: func() interface{} {
				writer, _ := flate.NewWriter(nil, config.DeflateLevel)
				return writer
			},
		},
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, config.BufferSize)
			},
		},
	}

	return &CompressionMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"compression",
			PriorityCompression,
			"Request/response compression middleware",
		),
		config: config,
		pools:  pools,
	}
}

// DefaultCompressionConfig returns default compression configuration
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Types:              []CompressionType{CompressionGzip, CompressionDeflate},
		GzipLevel:          gzip.DefaultCompression,
		DeflateLevel:       flate.DefaultCompression,
		ContentTypes:       DefaultCompressibleTypes(),
		ExcludeTypes:       DefaultNonCompressibleTypes(),
		MinSize:            1024,     // 1KB
		MaxSize:            10485760, // 10MB
		IncludePaths:       []string{},
		ExcludePaths:       []string{"/health", "/metrics"},
		VaryHeader:         true,
		AllowDecompression: true,
		MaxDecompressSize:  52428800, // 50MB
		PoolSize:           100,
		BufferSize:         32768, // 32KB
		FlushInterval:      1000,  // 1 second
		DisableStreaming:   false,
	}
}

// DefaultCompressibleTypes returns default compressible content types
func DefaultCompressibleTypes() []string {
	return []string{
		"text/html",
		"text/css",
		"text/plain",
		"text/javascript",
		"text/xml",
		"text/csv",
		"application/javascript",
		"application/json",
		"application/xml",
		"application/rss+xml",
		"application/atom+xml",
		"application/x-javascript",
		"application/x-web-app-manifest+json",
		"image/svg+xml",
		"font/woff",
		"font/woff2",
		"application/font-woff",
		"application/font-woff2",
		"application/vnd.ms-fontobject",
		"font/ttf",
		"font/otf",
		"font/eot",
	}
}

// DefaultNonCompressibleTypes returns default non-compressible content types
func DefaultNonCompressibleTypes() []string {
	return []string{
		"image/jpeg",
		"image/png",
		"image/gif",
		"image/webp",
		"image/bmp",
		"image/tiff",
		"video/mp4",
		"video/mpeg",
		"video/quicktime",
		"video/x-msvideo",
		"audio/mpeg",
		"audio/wav",
		"audio/x-wav",
		"audio/ogg",
		"application/pdf",
		"application/zip",
		"application/x-rar-compressed",
		"application/x-tar",
		"application/gzip",
		"application/x-bzip2",
		"application/x-7z-compressed",
	}
}

// Handle implements the Middleware interface
func (cm *CompressionMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle request decompression
		if cm.config.AllowDecompression {
			r = cm.handleRequestDecompression(r)
		}

		// Skip compression for certain paths
		if cm.shouldSkipCompression(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Check if client accepts compression
		acceptedEncodings := cm.parseAcceptEncoding(r.Header.Get("Accept-Encoding"))
		if len(acceptedEncodings) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		// Select best compression algorithm
		compressionType := cm.selectCompressionType(acceptedEncodings)
		if compressionType == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Create compression writer
		compressedWriter, err := cm.createCompressedWriter(w, compressionType)
		if err != nil {
			cm.logger.Error("Failed to create compression writer", logger.Error(err))
			next.ServeHTTP(w, r)
			return
		}
		defer compressedWriter.Close()

		// Set compression headers
		cm.setCompressionHeaders(w, compressionType)

		// Execute handler with compressed writer
		next.ServeHTTP(compressedWriter, r)
	})
}

// handleRequestDecompression handles decompression of incoming requests
func (cm *CompressionMiddleware) handleRequestDecompression(r *http.Request) *http.Request {
	contentEncoding := r.Header.Get("Content-Encoding")
	if contentEncoding == "" {
		return r
	}

	// Check content length limits
	if r.ContentLength > cm.config.MaxDecompressSize {
		return r
	}

	var reader io.Reader
	var err error

	switch contentEncoding {
	case "gzip":
		reader, err = gzip.NewReader(r.Body)
	case "deflate":
		reader = flate.NewReader(r.Body)
	default:
		// Unsupported compression
		return r
	}

	if err != nil {
		cm.logger.Error("Failed to create decompression reader", logger.Error(err))
		return r
	}

	// Create new request with decompressed body
	newRequest := *r
	newRequest.Body = io.NopCloser(reader)
	newRequest.Header.Del("Content-Encoding")
	newRequest.Header.Del("Content-Length")

	return &newRequest
}

// shouldSkipCompression checks if compression should be skipped
func (cm *CompressionMiddleware) shouldSkipCompression(r *http.Request) bool {
	path := r.URL.Path

	// Check exclude paths
	for _, excludePath := range cm.config.ExcludePaths {
		if strings.HasPrefix(path, excludePath) {
			return true
		}
	}

	// Check include paths
	if len(cm.config.IncludePaths) > 0 {
		included := false
		for _, includePath := range cm.config.IncludePaths {
			if strings.HasPrefix(path, includePath) {
				included = true
				break
			}
		}
		if !included {
			return true
		}
	}

	return false
}

// parseAcceptEncoding parses Accept-Encoding header
func (cm *CompressionMiddleware) parseAcceptEncoding(header string) map[string]float64 {
	encodings := make(map[string]float64)

	for _, encoding := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(encoding), ";")
		if len(parts) == 0 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		quality := 1.0

		// Parse quality value
		for _, part := range parts[1:] {
			if strings.HasPrefix(strings.TrimSpace(part), "q=") {
				qvalue := strings.TrimSpace(part[2:])
				if q, err := strconv.ParseFloat(qvalue, 64); err == nil {
					quality = q
				}
			}
		}

		if quality > 0 {
			encodings[name] = quality
		}
	}

	return encodings
}

// selectCompressionType selects the best compression algorithm
func (cm *CompressionMiddleware) selectCompressionType(acceptedEncodings map[string]float64) string {
	bestType := ""
	bestQuality := 0.0

	for _, compressionType := range cm.config.Types {
		typeStr := string(compressionType)
		if quality, exists := acceptedEncodings[typeStr]; exists && quality > bestQuality {
			bestType = typeStr
			bestQuality = quality
		}
	}

	return bestType
}

// createCompressedWriter creates a compressed response writer
func (cm *CompressionMiddleware) createCompressedWriter(w http.ResponseWriter, compressionType string) (*compressedResponseWriter, error) {
	var compressor io.WriteCloser
	var err error

	switch compressionType {
	case "gzip":
		gzipWriter := cm.pools.gzipPool.Get().(*gzip.Writer)
		gzipWriter.Reset(w)
		compressor = gzipWriter
	case "deflate":
		deflateWriter := cm.pools.deflatePool.Get().(*flate.Writer)
		deflateWriter.Reset(w)
		compressor = deflateWriter
	default:
		return nil, fmt.Errorf("unsupported compression type: %s", compressionType)
	}

	return &compressedResponseWriter{
		ResponseWriter:  w,
		compressor:      compressor,
		compressionType: compressionType,
		config:          cm.config,
		pools:           cm.pools,
		buffer:          cm.pools.bufferPool.Get().([]byte),
	}, err
}

// setCompressionHeaders sets compression-related headers
func (cm *CompressionMiddleware) setCompressionHeaders(w http.ResponseWriter, compressionType string) {
	w.Header().Set("Content-Encoding", compressionType)

	if cm.config.VaryHeader {
		vary := w.Header().Get("Vary")
		if vary == "" {
			w.Header().Set("Vary", "Accept-Encoding")
		} else if !strings.Contains(vary, "Accept-Encoding") {
			w.Header().Set("Vary", vary+", Accept-Encoding")
		}
	}
}

// compressedResponseWriter wraps http.ResponseWriter with compression
type compressedResponseWriter struct {
	http.ResponseWriter
	compressor      io.WriteCloser
	compressionType string
	config          CompressionConfig
	pools           *compressionPools
	buffer          []byte
	written         bool
	size            int64
}

// Write implements io.Writer
func (crw *compressedResponseWriter) Write(data []byte) (int, error) {
	if !crw.written {
		crw.written = true

		// Check if content should be compressed
		if !crw.shouldCompress() {
			return crw.ResponseWriter.Write(data)
		}

		// Remove content-length header since we're compressing
		crw.Header().Del("Content-Length")
	}

	crw.size += int64(len(data))
	return crw.compressor.Write(data)
}

// WriteHeader implements http.ResponseWriter
func (crw *compressedResponseWriter) WriteHeader(code int) {
	crw.ResponseWriter.WriteHeader(code)
}

// Header implements http.ResponseWriter
func (crw *compressedResponseWriter) Header() http.Header {
	return crw.ResponseWriter.Header()
}

// Flush implements http.Flusher
func (crw *compressedResponseWriter) Flush() {
	if flusher, ok := crw.compressor.(interface{ Flush() error }); ok {
		flusher.Flush()
	}
	if flusher, ok := crw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker
func (crw *compressedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := crw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("response writer does not support hijacking")
}

// Push implements http.Pusher
func (crw *compressedResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := crw.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return fmt.Errorf("response writer does not support push")
}

// shouldCompress checks if content should be compressed
func (crw *compressedResponseWriter) shouldCompress() bool {
	// Check content type
	contentType := crw.Header().Get("Content-Type")
	if contentType == "" {
		return false
	}

	// Remove charset from content type
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = contentType[:idx]
	}
	contentType = strings.TrimSpace(contentType)

	// Check if content type is in exclude list
	for _, excludeType := range crw.config.ExcludeTypes {
		if strings.HasPrefix(contentType, excludeType) {
			return false
		}
	}

	// Check if content type is in include list
	if len(crw.config.ContentTypes) > 0 {
		for _, includeType := range crw.config.ContentTypes {
			if strings.HasPrefix(contentType, includeType) {
				return true
			}
		}
		return false
	}

	return true
}

// Close closes the compressed writer
func (crw *compressedResponseWriter) Close() error {
	err := crw.compressor.Close()

	// Return objects to pools
	switch crw.compressionType {
	case "gzip":
		crw.pools.gzipPool.Put(crw.compressor)
	case "deflate":
		crw.pools.deflatePool.Put(crw.compressor)
	}

	crw.pools.bufferPool.Put(crw.buffer)

	return err
}

// Configure implements the Middleware interface
func (cm *CompressionMiddleware) Configure(config map[string]interface{}) error {
	if types, ok := config["types"].([]string); ok {
		cm.config.Types = make([]CompressionType, len(types))
		for i, t := range types {
			cm.config.Types[i] = CompressionType(t)
		}
	}
	if gzipLevel, ok := config["gzip_level"].(int); ok {
		cm.config.GzipLevel = gzipLevel
	}
	if deflateLevel, ok := config["deflate_level"].(int); ok {
		cm.config.DeflateLevel = deflateLevel
	}
	if contentTypes, ok := config["content_types"].([]string); ok {
		cm.config.ContentTypes = contentTypes
	}
	if excludeTypes, ok := config["exclude_types"].([]string); ok {
		cm.config.ExcludeTypes = excludeTypes
	}
	if minSize, ok := config["min_size"].(int64); ok {
		cm.config.MinSize = minSize
	}
	if maxSize, ok := config["max_size"].(int64); ok {
		cm.config.MaxSize = maxSize
	}
	if includePaths, ok := config["include_paths"].([]string); ok {
		cm.config.IncludePaths = includePaths
	}
	if excludePaths, ok := config["exclude_paths"].([]string); ok {
		cm.config.ExcludePaths = excludePaths
	}
	if varyHeader, ok := config["vary_header"].(bool); ok {
		cm.config.VaryHeader = varyHeader
	}
	if allowDecompression, ok := config["allow_decompression"].(bool); ok {
		cm.config.AllowDecompression = allowDecompression
	}

	return nil
}

// Health implements the Middleware interface
func (cm *CompressionMiddleware) Health(ctx context.Context) error {
	// Test compression pools
	if len(cm.config.Types) == 0 {
		return fmt.Errorf("no compression types configured")
	}

	// Test pool creation
	var buf bytes.Buffer
	for _, compressionType := range cm.config.Types {
		_, err := cm.createCompressedWriter(&responseWriterWrapper{&buf}, string(compressionType))
		if err != nil {
			return fmt.Errorf("failed to create %s compressor: %w", compressionType, err)
		}
	}

	return nil
}

// responseWriterWrapper wraps io.Writer to implement http.ResponseWriter
type responseWriterWrapper struct {
	io.Writer
}

func (rw *responseWriterWrapper) Header() http.Header {
	return make(http.Header)
}

func (rw *responseWriterWrapper) WriteHeader(statusCode int) {
	// No-op
}

// Utility functions

// CompressionMiddlewareFunc creates a simple compression middleware function
func CompressionMiddlewareFunc(config CompressionConfig) func(http.Handler) http.Handler {
	middleware := NewCompressionMiddleware(config)
	return middleware.Handle
}

// WithDefaultCompression creates compression middleware with default configuration
func WithDefaultCompression() func(http.Handler) http.Handler {
	return CompressionMiddlewareFunc(DefaultCompressionConfig())
}

// WithGzipCompression creates compression middleware with gzip only
func WithGzipCompression() func(http.Handler) http.Handler {
	config := DefaultCompressionConfig()
	config.Types = []CompressionType{CompressionGzip}
	return CompressionMiddlewareFunc(config)
}

// WithDeflateCompression creates compression middleware with deflate only
func WithDeflateCompression() func(http.Handler) http.Handler {
	config := DefaultCompressionConfig()
	config.Types = []CompressionType{CompressionDeflate}
	return CompressionMiddlewareFunc(config)
}

// WithHighCompression creates compression middleware with high compression levels
func WithHighCompression() func(http.Handler) http.Handler {
	config := DefaultCompressionConfig()
	config.GzipLevel = gzip.BestCompression
	config.DeflateLevel = flate.BestCompression
	return CompressionMiddlewareFunc(config)
}

// WithFastCompression creates compression middleware with fast compression levels
func WithFastCompression() func(http.Handler) http.Handler {
	config := DefaultCompressionConfig()
	config.GzipLevel = gzip.BestSpeed
	config.DeflateLevel = flate.BestSpeed
	return CompressionMiddlewareFunc(config)
}

// WithContentTypeFiltering creates compression middleware with specific content types
func WithContentTypeFiltering(contentTypes []string) func(http.Handler) http.Handler {
	config := DefaultCompressionConfig()
	config.ContentTypes = contentTypes
	return CompressionMiddlewareFunc(config)
}

// WithSizeThreshold creates compression middleware with size thresholds
func WithSizeThreshold(minSize, maxSize int64) func(http.Handler) http.Handler {
	config := DefaultCompressionConfig()
	config.MinSize = minSize
	config.MaxSize = maxSize
	return CompressionMiddlewareFunc(config)
}

// Compression testing utilities

// TestCompressionMiddleware provides utilities for testing compression
type TestCompressionMiddleware struct {
	*CompressionMiddleware
}

// NewTestCompressionMiddleware creates a test compression middleware
func NewTestCompressionMiddleware() *TestCompressionMiddleware {
	config := DefaultCompressionConfig()
	config.BufferSize = 1024 // Smaller buffer for testing
	return &TestCompressionMiddleware{
		CompressionMiddleware: NewCompressionMiddleware(config).(*CompressionMiddleware),
	}
}

// TestCompression tests compression with specific content
func (tcm *TestCompressionMiddleware) TestCompression(content string, contentType string) ([]byte, error) {
	var buf bytes.Buffer

	// Create compressed writer
	writer, err := tcm.createCompressedWriter(&responseWriterWrapper{&buf}, "gzip")
	if err != nil {
		return nil, err
	}
	defer writer.Close()

	// Set content type
	writer.Header().Set("Content-Type", contentType)

	// Write content
	_, err = writer.Write([]byte(content))
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GetCompressionRatio calculates compression ratio
func (tcm *TestCompressionMiddleware) GetCompressionRatio(original, compressed []byte) float64 {
	if len(original) == 0 {
		return 0
	}
	return float64(len(compressed)) / float64(len(original))
}

// Compression benchmarking utilities

// BenchmarkCompression benchmarks compression performance
func BenchmarkCompression(content []byte, iterations int) map[string]time.Duration {
	results := make(map[string]time.Duration)

	// Benchmark gzip
	start := time.Now()
	for i := 0; i < iterations; i++ {
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		writer.Write(content)
		writer.Close()
	}
	results["gzip"] = time.Since(start)

	// Benchmark deflate
	start = time.Now()
	for i := 0; i < iterations; i++ {
		var buf bytes.Buffer
		writer, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		writer.Write(content)
		writer.Close()
	}
	results["deflate"] = time.Since(start)

	return results
}

// Compression analysis utilities

// AnalyzeCompressionEfficiency analyzes compression efficiency for different content types
func AnalyzeCompressionEfficiency(samples map[string][]byte) map[string]CompressionAnalysis {
	results := make(map[string]CompressionAnalysis)

	for contentType, content := range samples {
		analysis := CompressionAnalysis{
			ContentType:    contentType,
			OriginalSize:   len(content),
			CompressedSize: make(map[string]int),
			Ratio:          make(map[string]float64),
		}

		// Test gzip
		var gzipBuf bytes.Buffer
		gzipWriter := gzip.NewWriter(&gzipBuf)
		gzipWriter.Write(content)
		gzipWriter.Close()
		analysis.CompressedSize["gzip"] = gzipBuf.Len()
		analysis.Ratio["gzip"] = float64(gzipBuf.Len()) / float64(len(content))

		// Test deflate
		var deflateBuf bytes.Buffer
		deflateWriter, _ := flate.NewWriter(&deflateBuf, flate.DefaultCompression)
		deflateWriter.Write(content)
		deflateWriter.Close()
		analysis.CompressedSize["deflate"] = deflateBuf.Len()
		analysis.Ratio["deflate"] = float64(deflateBuf.Len()) / float64(len(content))

		results[contentType] = analysis
	}

	return results
}

// CompressionAnalysis represents compression analysis results
type CompressionAnalysis struct {
	ContentType    string             `json:"content_type"`
	OriginalSize   int                `json:"original_size"`
	CompressedSize map[string]int     `json:"compressed_size"`
	Ratio          map[string]float64 `json:"ratio"`
}

// ShouldCompress determines if content should be compressed based on analysis
func (ca *CompressionAnalysis) ShouldCompress() bool {
	// Don't compress if any algorithm results in less than 10% reduction
	for _, ratio := range ca.Ratio {
		if ratio < 0.9 {
			return true
		}
	}
	return false
}

// BestAlgorithm returns the best compression algorithm
func (ca *CompressionAnalysis) BestAlgorithm() string {
	bestAlgo := ""
	bestRatio := 1.0

	for algo, ratio := range ca.Ratio {
		if ratio < bestRatio {
			bestAlgo = algo
			bestRatio = ratio
		}
	}

	return bestAlgo
}
