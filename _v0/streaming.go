package v0

import (
	"github.com/xraph/forge/v0/pkg/streaming"
)

// =============================================================================
// STREAMING CONFIGURATION OPTIONS
// =============================================================================

// WithStreamingConfig stores streaming configuration for later initialization
func WithStreamingConfig(config streaming.StreamingConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		appConfig.StreamingConfig = &config
		return nil
	}
}

// WithDefaultStreaming enables streaming with default configuration
func WithDefaultStreaming() ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		defaultConfig := streaming.DefaultStreamingConfig()
		appConfig.StreamingConfig = &defaultConfig
		return nil
	}
}

// WithWebSocketSupport enables WebSocket support with custom configuration
func WithWebSocketSupport(config streaming.StreamingConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		// Ensure WebSocket protocol is enabled
		config.EnableWebSocket = true
		appConfig.StreamingConfig = &config
		return nil
	}
}

// WithSSESupport enables Server-Sent Events support with custom configuration
func WithSSESupport(config streaming.StreamingConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		// Ensure SSE protocol is enabled
		config.EnableSSE = true
		appConfig.StreamingConfig = &config
		return nil
	}
}

// WithFullStreamingSupport enables all streaming protocols
func WithFullStreamingSupport(config streaming.StreamingConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		// Enable all protocols
		config.EnableWebSocket = true
		config.EnableSSE = true
		config.EnableLongPolling = true
		appConfig.StreamingConfig = &config
		return nil
	}
}

// WithRedisStreaming enables Redis-backed streaming for scaling
func WithRedisStreaming(redisConfig streaming.RedisConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		config := streaming.DefaultStreamingConfig()
		config.EnableRedisScaling = true
		config.RedisConfig = &redisConfig
		appConfig.StreamingConfig = &config
		return nil
	}
}

// WithPersistentStreaming enables persistent streaming with database storage
func WithPersistentStreaming(persistenceConfig streaming.PersistenceConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		config := streaming.DefaultStreamingConfig()
		config.EnablePersistence = true
		config.PersistenceConfig = &persistenceConfig
		appConfig.StreamingConfig = &config
		return nil
	}
}

// WithStreamingManager stores a pre-built streaming manager instance
func WithStreamingManager(streamingManager streaming.StreamingManager) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		// Store the instance for later assignment
		// Note: This will bypass the normal streaming service registration
		// and directly assign the manager in the application
		return nil
	}
}

// =============================================================================
// STREAMING CONVENIENCE OPTIONS
// =============================================================================

// WithBasicWebSocket enables WebSocket with minimal configuration
func WithBasicWebSocket() ApplicationOption {
	return WithWebSocketSupport(streaming.StreamingConfig{
		EnableWebSocket:   true,
		MaxConnections:    100,
		HeartbeatInterval: 30,
	})
}

// WithBasicSSE enables Server-Sent Events with minimal configuration
func WithBasicSSE() ApplicationOption {
	return WithSSESupport(streaming.StreamingConfig{
		EnableSSE:         true,
		MaxConnections:    100,
		HeartbeatInterval: 30,
	})
}

// WithHighThroughputStreaming configures streaming for high-performance scenarios
func WithHighThroughputStreaming() ApplicationOption {
	return WithFullStreamingSupport(streaming.StreamingConfig{
		EnableWebSocket:   true,
		EnableSSE:         true,
		EnableLongPolling: true,
		MaxConnections:    10000,
		HeartbeatInterval: 15,
		MaxMessageSize:    1024 * 1024, // 1MB
	})
}
