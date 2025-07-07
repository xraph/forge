package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// RequestIDMiddleware provides request ID generation and tracking
type RequestIDMiddleware struct {
	*BaseMiddleware
	config    RequestIDConfig
	counter   uint64
	generator RequestIDGenerator
}

// RequestIDConfig represents request ID configuration
type RequestIDConfig struct {
	// Header name for request ID
	HeaderName string `json:"header_name"`

	// Response header name (if different from request)
	ResponseHeaderName string `json:"response_header_name"`

	// Context key for storing request ID
	ContextKey string `json:"context_key"`

	// Request ID generation method
	Generator RequestIDGeneratorType `json:"generator"`

	// Custom prefix for generated IDs
	Prefix string `json:"prefix"`

	// Length for random generators
	Length int `json:"length"`

	// Include timestamp in ID
	IncludeTimestamp bool `json:"include_timestamp"`

	// Include counter in ID
	IncludeCounter bool `json:"include_counter"`

	// Include hostname in ID
	IncludeHostname bool `json:"include_hostname"`

	// Custom generator function
	CustomGenerator RequestIDGenerator `json:"-"`

	// Validate existing request IDs
	ValidateExisting bool `json:"validate_existing"`

	// Skip paths
	SkipPaths []string `json:"skip_paths"`

	// Store in response header
	IncludeInResponse bool `json:"include_in_response"`

	// Log request ID generation
	LogGeneration bool `json:"log_generation"`
}

// RequestIDGeneratorType represents different ID generation methods
type RequestIDGeneratorType string

const (
	GeneratorUUID       RequestIDGeneratorType = "uuid"
	GeneratorULID       RequestIDGeneratorType = "ulid"
	GeneratorNanoID     RequestIDGeneratorType = "nanoid"
	GeneratorRandom     RequestIDGeneratorType = "random"
	GeneratorSequential RequestIDGeneratorType = "sequential"
	GeneratorTimestamp  RequestIDGeneratorType = "timestamp"
	GeneratorCustom     RequestIDGeneratorType = "custom"
)

// RequestIDGenerator interface for custom ID generation
type RequestIDGenerator interface {
	Generate(r *http.Request) string
}

// NewRequestIDMiddleware creates a new request ID middleware
func NewRequestIDMiddleware(config ...RequestIDConfig) Middleware {
	cfg := DefaultRequestIDConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	var generator RequestIDGenerator
	switch cfg.Generator {
	case GeneratorUUID:
		generator = &UUIDGenerator{}
	case GeneratorULID:
		generator = &ULIDGenerator{}
	case GeneratorNanoID:
		generator = &NanoIDGenerator{Length: cfg.Length}
	case GeneratorRandom:
		generator = &RandomGenerator{Length: cfg.Length}
	case GeneratorSequential:
		generator = &SequentialGenerator{Prefix: cfg.Prefix}
	case GeneratorTimestamp:
		generator = &TimestampGenerator{
			Prefix:          cfg.Prefix,
			IncludeCounter:  cfg.IncludeCounter,
			IncludeHostname: cfg.IncludeHostname,
		}
	case GeneratorCustom:
		generator = cfg.CustomGenerator
	default:
		generator = &UUIDGenerator{}
	}

	return &RequestIDMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"request_id",
			PriorityRequestID,
			"Request ID generation and tracking middleware",
		),
		config:    cfg,
		generator: generator,
	}
}

// DefaultRequestIDConfig returns default request ID configuration
func DefaultRequestIDConfig() RequestIDConfig {
	return RequestIDConfig{
		HeaderName:         "X-Request-ID",
		ResponseHeaderName: "X-Request-ID",
		ContextKey:         string(router.RequestIDKey),
		Generator:          GeneratorUUID,
		Prefix:             "",
		Length:             16,
		IncludeTimestamp:   false,
		IncludeCounter:     false,
		IncludeHostname:    false,
		ValidateExisting:   true,
		SkipPaths:          []string{},
		IncludeInResponse:  true,
		LogGeneration:      false,
	}
}

// Handle implements the Middleware interface
func (rim *RequestIDMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip for certain paths
		if rim.shouldSkipPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Get or generate request ID
		requestID := rim.getOrGenerateRequestID(r)

		// Add request ID to context
		ctx := context.WithValue(r.Context(), rim.config.ContextKey, requestID)
		r = r.WithContext(ctx)

		// Set response header if configured
		if rim.config.IncludeInResponse {
			w.Header().Set(rim.config.ResponseHeaderName, requestID)
		}

		// Log generation if enabled
		if rim.config.LogGeneration {
			rim.logger.Debug("Request ID generated",
				logger.String("request_id", requestID),
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
			)
		}

		next.ServeHTTP(w, r)
	})
}

// shouldSkipPath checks if request ID should be skipped for this path
func (rim *RequestIDMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range rim.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// getOrGenerateRequestID gets existing request ID or generates a new one
func (rim *RequestIDMiddleware) getOrGenerateRequestID(r *http.Request) string {
	// Check if request ID already exists in header
	existingID := r.Header.Get(rim.config.HeaderName)
	if existingID != "" {
		if rim.config.ValidateExisting {
			if rim.isValidRequestID(existingID) {
				return existingID
			}
		} else {
			return existingID
		}
	}

	// Generate new request ID
	return rim.generator.Generate(r)
}

// isValidRequestID validates an existing request ID
func (rim *RequestIDMiddleware) isValidRequestID(id string) bool {
	// Basic validation - check length and characters
	if len(id) < 8 || len(id) > 128 {
		return false
	}

	// Check for valid characters (alphanumeric, hyphens, underscores)
	for _, char := range id {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return false
		}
	}

	return true
}

// Configure implements the Middleware interface
func (rim *RequestIDMiddleware) Configure(config map[string]interface{}) error {
	if headerName, ok := config["header_name"].(string); ok {
		rim.config.HeaderName = headerName
	}
	if responseHeaderName, ok := config["response_header_name"].(string); ok {
		rim.config.ResponseHeaderName = responseHeaderName
	}
	if contextKey, ok := config["context_key"].(string); ok {
		rim.config.ContextKey = contextKey
	}
	if generator, ok := config["generator"].(string); ok {
		rim.config.Generator = RequestIDGeneratorType(generator)
	}
	if prefix, ok := config["prefix"].(string); ok {
		rim.config.Prefix = prefix
	}
	if length, ok := config["length"].(int); ok {
		rim.config.Length = length
	}
	if includeTimestamp, ok := config["include_timestamp"].(bool); ok {
		rim.config.IncludeTimestamp = includeTimestamp
	}
	if includeCounter, ok := config["include_counter"].(bool); ok {
		rim.config.IncludeCounter = includeCounter
	}
	if includeHostname, ok := config["include_hostname"].(bool); ok {
		rim.config.IncludeHostname = includeHostname
	}
	if validateExisting, ok := config["validate_existing"].(bool); ok {
		rim.config.ValidateExisting = validateExisting
	}
	if skipPaths, ok := config["skip_paths"].([]string); ok {
		rim.config.SkipPaths = skipPaths
	}
	if includeInResponse, ok := config["include_in_response"].(bool); ok {
		rim.config.IncludeInResponse = includeInResponse
	}
	if logGeneration, ok := config["log_generation"].(bool); ok {
		rim.config.LogGeneration = logGeneration
	}

	return nil
}

// Health implements the Middleware interface
func (rim *RequestIDMiddleware) Health(ctx context.Context) error {
	if rim.generator == nil {
		return fmt.Errorf("request ID generator not configured")
	}

	// Test generator
	testReq, _ := http.NewRequest("GET", "/test", nil)
	testID := rim.generator.Generate(testReq)
	if testID == "" {
		return fmt.Errorf("request ID generator returned empty ID")
	}

	return nil
}

// Request ID Generators

// UUIDGenerator generates UUID v4 request IDs
type UUIDGenerator struct{}

func (g *UUIDGenerator) Generate(r *http.Request) string {
	return generateUUID()
}

// ULIDGenerator generates ULID request IDs
type ULIDGenerator struct{}

func (g *ULIDGenerator) Generate(r *http.Request) string {
	return generateULID()
}

// NanoIDGenerator generates NanoID request IDs
type NanoIDGenerator struct {
	Length int
}

func (g *NanoIDGenerator) Generate(r *http.Request) string {
	return generateNanoID(g.Length)
}

// RandomGenerator generates random hex request IDs
type RandomGenerator struct {
	Length int
}

func (g *RandomGenerator) Generate(r *http.Request) string {
	return generateRandomHex(g.Length)
}

// SequentialGenerator generates sequential request IDs
type SequentialGenerator struct {
	Prefix  string
	counter uint64
}

func (g *SequentialGenerator) Generate(r *http.Request) string {
	count := atomic.AddUint64(&g.counter, 1)
	if g.Prefix != "" {
		return fmt.Sprintf("%s-%d", g.Prefix, count)
	}
	return strconv.FormatUint(count, 10)
}

// TimestampGenerator generates timestamp-based request IDs
type TimestampGenerator struct {
	Prefix          string
	IncludeCounter  bool
	IncludeHostname bool
	counter         uint64
}

func (g *TimestampGenerator) Generate(r *http.Request) string {
	timestamp := time.Now().Unix()

	var parts []string
	if g.Prefix != "" {
		parts = append(parts, g.Prefix)
	}

	parts = append(parts, strconv.FormatInt(timestamp, 10))

	if g.IncludeCounter {
		count := atomic.AddUint64(&g.counter, 1)
		parts = append(parts, strconv.FormatUint(count, 10))
	}

	if g.IncludeHostname {
		// Simple hostname - in production this could be more sophisticated
		parts = append(parts, "host")
	}

	return strings.Join(parts, "-")
}

// CustomRequestIDGenerator wraps a custom generator function
type CustomRequestIDGenerator struct {
	GeneratorFunc func(*http.Request) string
}

func (g *CustomRequestIDGenerator) Generate(r *http.Request) string {
	return g.GeneratorFunc(r)
}

// ID Generation Functions

// generateUUID generates a UUID v4
func generateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)

	// Set version (4) and variant bits
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

// generateULID generates a ULID (simplified implementation)
func generateULID() string {
	// Simplified ULID implementation
	// In production, use a proper ULID library
	timestamp := time.Now().UnixMilli()
	randomBytes := make([]byte, 10)
	rand.Read(randomBytes)

	return fmt.Sprintf("%013X%s", timestamp, hex.EncodeToString(randomBytes))
}

// generateNanoID generates a NanoID
func generateNanoID(length int) string {
	alphabet := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	bytes := make([]byte, length)
	rand.Read(bytes)

	for i, b := range bytes {
		bytes[i] = alphabet[b%byte(len(alphabet))]
	}

	return string(bytes)
}

// generateRandomHex generates a random hex string
func generateRandomHex(length int) string {
	bytes := make([]byte, length/2)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// Utility functions

// RequestIDMiddlewareFunc creates a simple request ID middleware function
func RequestIDMiddlewareFunc(config ...RequestIDConfig) func(http.Handler) http.Handler {
	middleware := NewRequestIDMiddleware(config...)
	return middleware.Handle
}

// WithRequestID creates request ID middleware with default configuration
func WithRequestID() func(http.Handler) http.Handler {
	return RequestIDMiddlewareFunc()
}

// WithCustomRequestID creates request ID middleware with custom generator
func WithCustomRequestID(generator RequestIDGenerator) func(http.Handler) http.Handler {
	config := DefaultRequestIDConfig()
	config.Generator = GeneratorCustom
	config.CustomGenerator = generator
	return RequestIDMiddlewareFunc(config)
}

// WithUUIDRequestID creates request ID middleware with UUID generator
func WithUUIDRequestID() func(http.Handler) http.Handler {
	config := DefaultRequestIDConfig()
	config.Generator = GeneratorUUID
	return RequestIDMiddlewareFunc(config)
}

// WithSequentialRequestID creates request ID middleware with sequential generator
func WithSequentialRequestID(prefix string) func(http.Handler) http.Handler {
	config := DefaultRequestIDConfig()
	config.Generator = GeneratorSequential
	config.Prefix = prefix
	return RequestIDMiddlewareFunc(config)
}

// WithTimestampRequestID creates request ID middleware with timestamp generator
func WithTimestampRequestID(prefix string, includeCounter bool) func(http.Handler) http.Handler {
	config := DefaultRequestIDConfig()
	config.Generator = GeneratorTimestamp
	config.Prefix = prefix
	config.IncludeCounter = includeCounter
	return RequestIDMiddlewareFunc(config)
}

// Request ID utilities

// GetRequestIDFromContext extracts request ID from context
func GetRequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(router.RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetRequestIDFromRequest extracts request ID from request
func GetRequestIDFromRequest(r *http.Request) string {
	return GetRequestIDFromContext(r.Context())
}

// SetRequestIDInContext sets request ID in context
func SetRequestIDInContext(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, router.RequestIDKey, requestID)
}

// RequestIDFromHeader extracts request ID from header
func RequestIDFromHeader(r *http.Request, headerName string) string {
	return r.Header.Get(headerName)
}

// RequestIDToHeader sets request ID in header
func RequestIDToHeader(w http.ResponseWriter, headerName, requestID string) {
	w.Header().Set(headerName, requestID)
}

// Request ID validation

// ValidateRequestID validates a request ID format
func ValidateRequestID(id string) error {
	if id == "" {
		return fmt.Errorf("request ID cannot be empty")
	}

	if len(id) < 8 {
		return fmt.Errorf("request ID too short")
	}

	if len(id) > 128 {
		return fmt.Errorf("request ID too long")
	}

	// Check for valid characters
	for _, char := range id {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return fmt.Errorf("request ID contains invalid characters")
		}
	}

	return nil
}

// IsValidRequestID checks if a request ID is valid
func IsValidRequestID(id string) bool {
	return ValidateRequestID(id) == nil
}

// Request ID testing utilities

// TestRequestIDMiddleware provides utilities for testing request IDs
type TestRequestIDMiddleware struct {
	*RequestIDMiddleware
}

// NewTestRequestIDMiddleware creates a test request ID middleware
func NewTestRequestIDMiddleware() *TestRequestIDMiddleware {
	config := DefaultRequestIDConfig()
	config.LogGeneration = false
	return &TestRequestIDMiddleware{
		RequestIDMiddleware: NewRequestIDMiddleware(config).(*RequestIDMiddleware),
	}
}

// TestRequestIDGeneration tests request ID generation
func (trim *TestRequestIDMiddleware) TestRequestIDGeneration() (string, error) {
	req, err := http.NewRequest("GET", "/test", nil)
	if err != nil {
		return "", err
	}

	return trim.generator.Generate(req), nil
}

// TestRequestIDUniqueness tests request ID uniqueness
func (trim *TestRequestIDMiddleware) TestRequestIDUniqueness(count int) (bool, error) {
	ids := make(map[string]bool)
	req, err := http.NewRequest("GET", "/test", nil)
	if err != nil {
		return false, err
	}

	for i := 0; i < count; i++ {
		id := trim.generator.Generate(req)
		if ids[id] {
			return false, fmt.Errorf("duplicate request ID: %s", id)
		}
		ids[id] = true
	}

	return true, nil
}

// Request ID benchmarking

// BenchmarkRequestIDGeneration benchmarks request ID generation
func BenchmarkRequestIDGeneration(generator RequestIDGenerator, iterations int) time.Duration {
	req, _ := http.NewRequest("GET", "/test", nil)

	start := time.Now()
	for i := 0; i < iterations; i++ {
		generator.Generate(req)
	}
	return time.Since(start)
}

// Request ID analysis

// AnalyzeRequestIDs analyzes request ID patterns
func AnalyzeRequestIDs(ids []string) RequestIDAnalysis {
	analysis := RequestIDAnalysis{
		Count:     len(ids),
		Unique:    0,
		MinLength: 999,
		MaxLength: 0,
		Patterns:  make(map[string]int),
	}

	uniqueIDs := make(map[string]bool)

	for _, id := range ids {
		// Check uniqueness
		if !uniqueIDs[id] {
			uniqueIDs[id] = true
			analysis.Unique++
		}

		// Check length
		if len(id) < analysis.MinLength {
			analysis.MinLength = len(id)
		}
		if len(id) > analysis.MaxLength {
			analysis.MaxLength = len(id)
		}

		// Analyze patterns
		if strings.Contains(id, "-") {
			analysis.Patterns["hyphenated"]++
		}
		if strings.Contains(id, "_") {
			analysis.Patterns["underscored"]++
		}
		if len(id) == 36 && strings.Count(id, "-") == 4 {
			analysis.Patterns["uuid"]++
		}
	}

	analysis.UniquenessRatio = float64(analysis.Unique) / float64(analysis.Count)

	return analysis
}

// RequestIDAnalysis represents request ID analysis results
type RequestIDAnalysis struct {
	Count           int            `json:"count"`
	Unique          int            `json:"unique"`
	UniquenessRatio float64        `json:"uniqueness_ratio"`
	MinLength       int            `json:"min_length"`
	MaxLength       int            `json:"max_length"`
	Patterns        map[string]int `json:"patterns"`
}

// Request ID correlation

// CorrelateRequestIDs correlates request IDs with other request data
func CorrelateRequestIDs(requests []RequestData) RequestIDCorrelation {
	correlation := RequestIDCorrelation{
		TotalRequests: len(requests),
		PathCounts:    make(map[string]int),
		MethodCounts:  make(map[string]int),
		StatusCounts:  make(map[int]int),
	}

	for _, req := range requests {
		correlation.PathCounts[req.Path]++
		correlation.MethodCounts[req.Method]++
		correlation.StatusCounts[req.Status]++
	}

	return correlation
}

// RequestData represents request data for correlation
type RequestData struct {
	ID     string
	Path   string
	Method string
	Status int
}

// RequestIDCorrelation represents correlation analysis results
type RequestIDCorrelation struct {
	TotalRequests int            `json:"total_requests"`
	PathCounts    map[string]int `json:"path_counts"`
	MethodCounts  map[string]int `json:"method_counts"`
	StatusCounts  map[int]int    `json:"status_counts"`
}
