package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// LLMManager manages Large Language Model providers and operations
type LLMManager struct {
	providers   map[string]LLMProvider
	config      LLMManagerConfig
	logger      common.Logger
	metrics     common.Metrics
	stats       LLMStats
	started     bool
	mu          sync.RWMutex
	statsUpdate time.Time
}

// LLMManagerConfig contains configuration for the LLM manager
type LLMManagerConfig struct {
	Logger          common.Logger
	Metrics         common.Metrics
	DefaultProvider string               `yaml:"default_provider" default:"openai"`
	MaxRetries      int                  `yaml:"max_retries" default:"3"`
	RetryDelay      time.Duration        `yaml:"retry_delay" default:"1s"`
	Timeout         time.Duration        `yaml:"timeout" default:"30s"`
	RateLimits      map[string]RateLimit `yaml:"rate_limits"`
}

// RateLimit defines rate limiting for LLM providers
type RateLimit struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
	TokensPerMinute   int `yaml:"tokens_per_minute"`
}

// LLMStats contains statistics about LLM operations
type LLMStats struct {
	TotalRequests      int64                       `json:"total_requests"`
	SuccessfulRequests int64                       `json:"successful_requests"`
	FailedRequests     int64                       `json:"failed_requests"`
	AverageLatency     time.Duration               `json:"average_latency"`
	TotalTokens        int64                       `json:"total_tokens"`
	ProviderStats      map[string]LLMProviderStats `json:"provider_stats"`
	LastUpdated        time.Time                   `json:"last_updated"`
}

// LLMProviderStats contains statistics for a specific provider
type LLMProviderStats struct {
	Name               string        `json:"name"`
	TotalRequests      int64         `json:"total_requests"`
	SuccessfulRequests int64         `json:"successful_requests"`
	FailedRequests     int64         `json:"failed_requests"`
	AverageLatency     time.Duration `json:"average_latency"`
	TotalTokens        int64         `json:"total_tokens"`
	Usage              LLMUsage      `json:"usage"`
	IsHealthy          bool          `json:"is_healthy"`
	LastUsed           time.Time     `json:"last_used"`
}

// NewLLMManager creates a new LLM manager
func NewLLMManager(config LLMManagerConfig) (*LLMManager, error) {
	return &LLMManager{
		providers: make(map[string]LLMProvider),
		config:    config,
		logger:    config.Logger,
		metrics:   config.Metrics,
		stats: LLMStats{
			ProviderStats: make(map[string]LLMProviderStats),
			LastUpdated:   time.Now(),
		},
		statsUpdate: time.Now(),
	}, nil
}

// Start initializes the LLM manager
func (m *LLMManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("LLM manager already started")
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("LLM manager started",
			logger.String("default_provider", m.config.DefaultProvider),
			logger.Int("max_retries", m.config.MaxRetries),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.llm.manager_started").Inc()
	}

	return nil
}

// Stop shuts down the LLM manager
func (m *LLMManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("LLM manager not started")
	}

	// Stop all providers
	for _, provider := range m.providers {
		if stoppable, ok := provider.(interface{ Stop(context.Context) error }); ok {
			if err := stoppable.Stop(ctx); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to stop LLM provider",
						logger.String("provider", provider.Name()),
						logger.Error(err),
					)
				}
			}
		}
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("LLM manager stopped")
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.llm.manager_stopped").Inc()
	}

	return nil
}

// RegisterProvider registers a new LLM provider
func (m *LLMManager) RegisterProvider(provider LLMProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := provider.Name()
	if _, exists := m.providers[name]; exists {
		return fmt.Errorf("provider %s already registered", name)
	}

	m.providers[name] = provider

	// Initialize provider stats
	m.stats.ProviderStats[name] = LLMProviderStats{
		Name:      name,
		Usage:     LLMUsage{LastReset: time.Now()},
		IsHealthy: true,
	}

	if m.logger != nil {
		m.logger.Info("LLM provider registered",
			logger.String("provider", name),
			logger.String("models", fmt.Sprintf("%v", provider.Models())),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.llm.provider_registered").Inc()
		m.metrics.Gauge("forge.ai.llm.providers_count").Set(float64(len(m.providers)))
	}

	return nil
}

// GetProvider retrieves an LLM provider by name
func (m *LLMManager) GetProvider(name string) (LLMProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, exists := m.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", name)
	}

	return provider, nil
}

// Chat performs a chat completion request
func (m *LLMManager) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	startTime := time.Now()

	// Get provider
	providerName := request.Provider
	if providerName == "" {
		providerName = m.config.DefaultProvider
	}

	provider, err := m.GetProvider(providerName)
	if err != nil {
		return ChatResponse{}, err
	}

	// Apply timeout
	if m.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.config.Timeout)
		defer cancel()
	}

	// Execute chat request with retries
	var response ChatResponse
	var lastErr error

	for attempt := 0; attempt <= m.config.MaxRetries; attempt++ {
		response, lastErr = provider.Chat(ctx, request)
		if lastErr == nil {
			break
		}

		if attempt < m.config.MaxRetries {
			select {
			case <-ctx.Done():
				return ChatResponse{}, ctx.Err()
			case <-time.After(m.config.RetryDelay):
				continue
			}
		}
	}

	// Update statistics
	latency := time.Since(startTime)
	m.updateStats(providerName, "chat", latency, lastErr, response.Usage)

	if lastErr != nil {
		return ChatResponse{}, lastErr
	}

	return response, nil
}

// Complete performs a text completion request
func (m *LLMManager) Complete(ctx context.Context, request CompletionRequest) (CompletionResponse, error) {
	startTime := time.Now()

	// Get provider
	providerName := request.Provider
	if providerName == "" {
		providerName = m.config.DefaultProvider
	}

	provider, err := m.GetProvider(providerName)
	if err != nil {
		return CompletionResponse{}, err
	}

	// Apply timeout
	if m.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.config.Timeout)
		defer cancel()
	}

	// Execute completion request with retries
	var response CompletionResponse
	var lastErr error

	for attempt := 0; attempt <= m.config.MaxRetries; attempt++ {
		response, lastErr = provider.Complete(ctx, request)
		if lastErr == nil {
			break
		}

		if attempt < m.config.MaxRetries {
			select {
			case <-ctx.Done():
				return CompletionResponse{}, ctx.Err()
			case <-time.After(m.config.RetryDelay):
				continue
			}
		}
	}

	// Update statistics
	latency := time.Since(startTime)
	m.updateStats(providerName, "completion", latency, lastErr, response.Usage)

	if lastErr != nil {
		return CompletionResponse{}, lastErr
	}

	return response, nil
}

// Embed performs an embedding request
func (m *LLMManager) Embed(ctx context.Context, request EmbeddingRequest) (EmbeddingResponse, error) {
	startTime := time.Now()

	// Get provider
	providerName := request.Provider
	if providerName == "" {
		providerName = m.config.DefaultProvider
	}

	provider, err := m.GetProvider(providerName)
	if err != nil {
		return EmbeddingResponse{}, err
	}

	// Apply timeout
	if m.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.config.Timeout)
		defer cancel()
	}

	// Execute embedding request with retries
	var response EmbeddingResponse
	var lastErr error

	for attempt := 0; attempt <= m.config.MaxRetries; attempt++ {
		response, lastErr = provider.Embed(ctx, request)
		if lastErr == nil {
			break
		}

		if attempt < m.config.MaxRetries {
			select {
			case <-ctx.Done():
				return EmbeddingResponse{}, ctx.Err()
			case <-time.After(m.config.RetryDelay):
				continue
			}
		}
	}

	// Update statistics
	latency := time.Since(startTime)
	m.updateStats(providerName, "embedding", latency, lastErr, response.Usage)

	if lastErr != nil {
		return EmbeddingResponse{}, lastErr
	}

	return response, nil
}

// GetStats returns LLM statistics
func (m *LLMManager) GetStats() LLMStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Update provider stats
	for name, provider := range m.providers {
		if stats, exists := m.stats.ProviderStats[name]; exists {
			stats.Usage = provider.GetUsage()
			stats.LastUsed = time.Now()
			m.stats.ProviderStats[name] = stats
		}
	}

	m.stats.LastUpdated = time.Now()
	return m.stats
}

// updateStats updates statistics for a provider
func (m *LLMManager) updateStats(providerName, operation string, latency time.Duration, err error, usage *LLMUsage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update overall stats
	m.stats.TotalRequests++
	if err != nil {
		m.stats.FailedRequests++
	} else {
		m.stats.SuccessfulRequests++
	}

	// Update average latency
	if m.stats.TotalRequests > 0 {
		m.stats.AverageLatency = time.Duration((int64(m.stats.AverageLatency)*m.stats.TotalRequests + int64(latency)) / (m.stats.TotalRequests + 1))
	}

	// Update provider stats
	if providerStats, exists := m.stats.ProviderStats[providerName]; exists {
		providerStats.TotalRequests++
		if err != nil {
			providerStats.FailedRequests++
			providerStats.IsHealthy = false
		} else {
			providerStats.SuccessfulRequests++
			providerStats.IsHealthy = true
		}

		// Update average latency
		if providerStats.TotalRequests > 0 {
			providerStats.AverageLatency = time.Duration((int64(providerStats.AverageLatency)*providerStats.TotalRequests + int64(latency)) / (providerStats.TotalRequests + 1))
		}

		// Update usage if provided
		if usage != nil {
			providerStats.Usage.InputTokens += usage.InputTokens
			providerStats.Usage.OutputTokens += usage.OutputTokens
			providerStats.Usage.TotalTokens += usage.TotalTokens
			providerStats.Usage.RequestCount++
			providerStats.Usage.Cost += usage.Cost
		}

		providerStats.LastUsed = time.Now()
		m.stats.ProviderStats[providerName] = providerStats
	}

	// Update tokens
	if usage != nil {
		m.stats.TotalTokens += usage.TotalTokens
	}

	// Update metrics
	if m.metrics != nil {
		m.metrics.Counter("forge.ai.llm.requests_total", "provider", providerName, "operation", operation).Inc()
		if err != nil {
			m.metrics.Counter("forge.ai.llm.requests_failed", "provider", providerName, "operation", operation).Inc()
		} else {
			m.metrics.Counter("forge.ai.llm.requests_successful", "provider", providerName, "operation", operation).Inc()
		}
		m.metrics.Histogram("forge.ai.llm.request_duration", "provider", providerName, "operation", operation).Observe(latency.Seconds())

		if usage != nil {
			m.metrics.Counter("forge.ai.llm.tokens_total", "provider", providerName).Add(float64(usage.TotalTokens))
			m.metrics.Counter("forge.ai.llm.tokens_input", "provider", providerName).Add(float64(usage.InputTokens))
			m.metrics.Counter("forge.ai.llm.tokens_output", "provider", providerName).Add(float64(usage.OutputTokens))
		}
	}
}

// GetProviders returns all registered providers
func (m *LLMManager) GetProviders() map[string]LLMProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := make(map[string]LLMProvider)
	for name, provider := range m.providers {
		providers[name] = provider
	}

	return providers
}

// HealthCheck performs a health check on all providers
func (m *LLMManager) HealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return fmt.Errorf("LLM manager not started")
	}

	unhealthyProviders := 0
	for name, provider := range m.providers {
		if checker, ok := provider.(interface{ HealthCheck(context.Context) error }); ok {
			if err := checker.HealthCheck(ctx); err != nil {
				unhealthyProviders++
				if m.logger != nil {
					m.logger.Warn("LLM provider health check failed",
						logger.String("provider", name),
						logger.Error(err),
					)
				}
			}
		}
	}

	if unhealthyProviders > 0 {
		return fmt.Errorf("%d out of %d LLM providers are unhealthy", unhealthyProviders, len(m.providers))
	}

	return nil
}

// IsStarted returns true if the LLM manager is started
func (m *LLMManager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}
