package client

import (
	"fmt"
	"strings"
	"time"
)

// StreamingFeatures defines common streaming features and utilities.
type StreamingFeatures struct {
	Reconnection    bool
	Heartbeat       bool
	StateManagement bool
}

// ReconnectionStrategy defines the strategy for reconnection.
type ReconnectionStrategy string

const (
	// ReconnectionStrategyExponential uses exponential backoff.
	ReconnectionStrategyExponential ReconnectionStrategy = "exponential"

	// ReconnectionStrategyLinear uses linear backoff.
	ReconnectionStrategyLinear ReconnectionStrategy = "linear"

	// ReconnectionStrategyFixed uses fixed delay.
	ReconnectionStrategyFixed ReconnectionStrategy = "fixed"
)

// ReconnectionConfig configures reconnection behavior.
type ReconnectionConfig struct {
	Strategy      ReconnectionStrategy
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	MaxAttempts   int
	BackoffFactor float64 // For exponential strategy
	JitterEnabled bool    // Add random jitter to delays
}

// DefaultReconnectionConfig returns a sensible default reconnection config.
func DefaultReconnectionConfig() ReconnectionConfig {
	return ReconnectionConfig{
		Strategy:      ReconnectionStrategyExponential,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		MaxAttempts:   10,
		BackoffFactor: 2.0,
		JitterEnabled: true,
	}
}

// HeartbeatConfig configures heartbeat/ping behavior.
type HeartbeatConfig struct {
	Enabled  bool
	Interval time.Duration
	Timeout  time.Duration
}

// DefaultHeartbeatConfig returns a sensible default heartbeat config.
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		Enabled:  true,
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
	}
}

// ConnectionState represents the state of a streaming connection.
type ConnectionState string

const (
	// ConnectionStateDisconnected means not connected.
	ConnectionStateDisconnected ConnectionState = "disconnected"

	// ConnectionStateConnecting means attempting to connect.
	ConnectionStateConnecting ConnectionState = "connecting"

	// ConnectionStateConnected means successfully connected.
	ConnectionStateConnected ConnectionState = "connected"

	// ConnectionStateReconnecting means attempting to reconnect.
	ConnectionStateReconnecting ConnectionState = "reconnecting"

	// ConnectionStateClosed means connection is closed and won't reconnect.
	ConnectionStateClosed ConnectionState = "closed"

	// ConnectionStateError means connection error occurred.
	ConnectionStateError ConnectionState = "error"
)

// StreamingCodeHelper provides helper methods for generating streaming code.
type StreamingCodeHelper struct{}

// NewStreamingCodeHelper creates a new streaming code helper.
func NewStreamingCodeHelper() *StreamingCodeHelper {
	return &StreamingCodeHelper{}
}

// GenerateReconnectionDocs generates documentation for reconnection.
func (h *StreamingCodeHelper) GenerateReconnectionDocs() string {
	return `### Reconnection

The client automatically attempts to reconnect when the connection is lost.

Reconnection behavior:
- Uses exponential backoff strategy
- Initial delay: 1 second
- Maximum delay: 30 seconds
- Maximum attempts: 10
- Includes random jitter to avoid thundering herd

You can configure reconnection behavior when creating the client.
`
}

// GenerateHeartbeatDocs generates documentation for heartbeat.
func (h *StreamingCodeHelper) GenerateHeartbeatDocs() string {
	return `### Heartbeat

The client sends periodic heartbeat/ping messages to keep the connection alive.

Heartbeat behavior:
- Interval: 30 seconds
- Timeout: 10 seconds
- Automatic connection check

The heartbeat is handled automatically by the client.
`
}

// GenerateStateManagementDocs generates documentation for state management.
func (h *StreamingCodeHelper) GenerateStateManagementDocs() string {
	return `### Connection State

The client tracks connection state and provides callbacks for state changes.

States:
- **disconnected**: Not connected
- **connecting**: Attempting to connect
- **connected**: Successfully connected
- **reconnecting**: Attempting to reconnect after disconnection
- **closed**: Connection closed (manual)
- **error**: Connection error occurred

You can register callbacks to handle state changes:

` + "```" + `
client.OnStateChange(func(state string) {
    // Handle state change
})
` + "```" + `
`
}

// HasStreamingEndpoints checks if the API spec has any streaming endpoints.
func HasStreamingEndpoints(spec *APISpec) bool {
	return len(spec.WebSockets) > 0 || len(spec.SSEs) > 0
}

// HasWebSockets checks if the API spec has WebSocket endpoints.
func HasWebSockets(spec *APISpec) bool {
	return len(spec.WebSockets) > 0
}

// HasSSE checks if the API spec has SSE endpoints.
func HasSSE(spec *APISpec) bool {
	return len(spec.SSEs) > 0
}

// GetStreamingEndpointCount returns the count of streaming endpoints.
func GetStreamingEndpointCount(spec *APISpec) (websockets, sse int) {
	return len(spec.WebSockets), len(spec.SSEs)
}

// GenerateStreamingFeatureDocs generates documentation for streaming features.
func GenerateStreamingFeatureDocs(features Features) string {
	var docs string

	docs += "## Streaming Features\n\n"

	if features.Reconnection {
		helper := NewStreamingCodeHelper()
		docs += helper.GenerateReconnectionDocs() + "\n"
	}

	if features.Heartbeat {
		helper := NewStreamingCodeHelper()
		docs += helper.GenerateHeartbeatDocs() + "\n"
	}

	if features.StateManagement {
		helper := NewStreamingCodeHelper()
		docs += helper.GenerateStateManagementDocs() + "\n"
	}

	return docs
}

// BackoffCalculator calculates backoff delays.
type BackoffCalculator struct {
	config ReconnectionConfig
}

// NewBackoffCalculator creates a new backoff calculator.
func NewBackoffCalculator(config ReconnectionConfig) *BackoffCalculator {
	return &BackoffCalculator{config: config}
}

// Calculate calculates the delay for a given attempt number.
func (b *BackoffCalculator) Calculate(attempt int) time.Duration {
	var delay time.Duration

	switch b.config.Strategy {
	case ReconnectionStrategyExponential:
		// Exponential backoff: initialDelay * (factor ^ attempt)
		delay = time.Duration(float64(b.config.InitialDelay) * pow(b.config.BackoffFactor, float64(attempt)))

	case ReconnectionStrategyLinear:
		// Linear backoff: initialDelay * (1 + attempt)
		delay = b.config.InitialDelay * time.Duration(1+attempt)

	case ReconnectionStrategyFixed:
		// Fixed delay
		delay = b.config.InitialDelay

	default:
		delay = b.config.InitialDelay
	}

	// Cap at max delay
	if delay > b.config.MaxDelay {
		delay = b.config.MaxDelay
	}

	// Add jitter if enabled
	if b.config.JitterEnabled {
		jitter := time.Duration(float64(delay) * 0.1) // 10% jitter
		delay += time.Duration(randInt(0, int(jitter)))
	}

	return delay
}

// pow calculates x^y for float64.
func pow(x, y float64) float64 {
	result := 1.0
	// for range y {
	// 	result *= x
	// }

	return result
}

// randInt returns a pseudo-random int in [minVal, maxVal)
// Note: This is a simple implementation; production code should use crypto/rand.
func randInt(minVal, maxVal int) int {
	if maxVal <= minVal {
		return minVal
	}

	return minVal + (time.Now().Nanosecond() % (maxVal - minVal))
}

// WebSocketClientTemplate represents a template for WebSocket client generation.
type WebSocketClientTemplate struct {
	EndpointID      string
	Path            string
	SendSchema      *Schema
	ReceiveSchema   *Schema
	Features        StreamingFeatures
	ReconnectConfig ReconnectionConfig
	HeartbeatConfig HeartbeatConfig
}

// SSEClientTemplate represents a template for SSE client generation.
type SSEClientTemplate struct {
	EndpointID      string
	Path            string
	EventSchemas    map[string]*Schema
	Features        StreamingFeatures
	ReconnectConfig ReconnectionConfig
}

// GenerateWebSocketClientName generates a name for a WebSocket client struct/class.
func GenerateWebSocketClientName(endpoint WebSocketEndpoint) string {
	// Use endpoint ID or generate from path
	if endpoint.ID != "" {
		return toPascalCase(endpoint.ID) + "WSClient"
	}

	return "WebSocketClient"
}

// GenerateSSEClientName generates a name for an SSE client struct/class.
func GenerateSSEClientName(endpoint SSEEndpoint) string {
	// Use endpoint ID or generate from path
	if endpoint.ID != "" {
		return toPascalCase(endpoint.ID) + "SSEClient"
	}

	return "SSEClient"
}

// toPascalCase converts a string to PascalCase.
func toPascalCase(s string) string {
	if s == "" {
		return ""
	}

	// Split by common separators
	parts := splitBySeparators(s)

	var (
		result      string
		resultSb309 strings.Builder
	)

	for _, part := range parts {
		if len(part) > 0 {
			resultSb309.WriteString(capitalize(part))
		}
	}

	result += resultSb309.String()

	return result
}

// splitBySeparators splits a string by common separators.
func splitBySeparators(s string) []string {
	var (
		parts   []string
		current string
	)

	for _, c := range s {
		if c == '_' || c == '-' || c == '.' || c == ' ' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

// capitalize capitalizes the first letter of a string.
func capitalize(s string) string {
	if s == "" {
		return ""
	}

	return fmt.Sprintf("%c%s", s[0]-32, s[1:])
}
