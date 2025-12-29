package sdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// AIGateway provides intelligent routing across multiple LLM providers
// with fallbacks, load balancing, and cost optimization.
//
// Example:
//
//	gateway := sdk.NewAIGateway(logger, metrics).
//	    AddProvider("openai", openaiManager).
//	    AddProvider("anthropic", anthropicManager).
//	    WithRouter(sdk.NewCostBasedRouter(budget)).
//	    WithRouter(sdk.NewCapabilityRouter()).
//	    WithFallback("gpt-4", "claude-3-opus").
//	    WithLoadBalancing(sdk.LoadBalanceRoundRobin).
//	    Build()
//
//	result, err := gateway.Chat(ctx, request)
type AIGateway struct {
	providers      map[string]StreamingLLMManager
	routers        []ModelRouter
	fallbacks      map[string][]string
	loadBalancer   LoadBalancer
	costOptimizer  *CostOptimizer
	healthChecker  *ProviderHealthChecker
	logger         forge.Logger
	metrics        forge.Metrics
	defaultTimeout time.Duration

	mu sync.RWMutex
}

// GatewayConfig configures the AI Gateway.
type GatewayConfig struct {
	DefaultTimeout time.Duration
	MaxRetries     int
	RetryDelay     time.Duration
	EnableHealth   bool
	HealthInterval time.Duration
}

// RouteDecision represents a routing decision made by the gateway.
type RouteDecision struct {
	Provider     string
	Model        string
	Reason       string
	Confidence   float64
	Alternatives []RouteAlternative
	Metadata     map[string]any
}

// RouteAlternative represents an alternative route option.
type RouteAlternative struct {
	Provider   string
	Model      string
	Reason     string
	Confidence float64
}

// GatewayRequest extends ChatRequest with gateway-specific options.
type GatewayRequest struct {
	llm.ChatRequest
	PreferredProviders []string       // Prefer these providers
	ExcludeProviders   []string       // Exclude these providers
	RequiredCaps       []string       // Required capabilities
	MaxCost            float64        // Maximum cost per request
	Timeout            time.Duration  // Request-specific timeout
	Metadata           map[string]any // Request metadata
}

// GatewayResponse extends ChatResponse with routing information.
type GatewayResponse struct {
	llm.ChatResponse
	RouteDecision RouteDecision
	Latency       time.Duration
	Retries       int
	FinalProvider string
	FinalModel    string
}

// NewAIGateway creates a new AI Gateway.
func NewAIGateway(logger forge.Logger, metrics forge.Metrics) *AIGateway {
	return &AIGateway{
		providers:      make(map[string]StreamingLLMManager),
		routers:        make([]ModelRouter, 0),
		fallbacks:      make(map[string][]string),
		logger:         logger,
		metrics:        metrics,
		defaultTimeout: 60 * time.Second,
	}
}

// AddProvider registers an LLM provider with the gateway.
func (g *AIGateway) AddProvider(name string, manager StreamingLLMManager) *AIGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[name] = manager
	return g
}

// WithRouter adds a router to the routing chain.
func (g *AIGateway) WithRouter(router ModelRouter) *AIGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.routers = append(g.routers, router)
	return g
}

// WithFallback configures fallback models for a primary model.
func (g *AIGateway) WithFallback(primary string, fallbacks ...string) *AIGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fallbacks[primary] = fallbacks
	return g
}

// WithLoadBalancing sets the load balancing strategy.
func (g *AIGateway) WithLoadBalancing(lb LoadBalancer) *AIGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.loadBalancer = lb
	return g
}

// WithCostOptimizer sets the cost optimizer.
func (g *AIGateway) WithCostOptimizer(optimizer *CostOptimizer) *AIGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.costOptimizer = optimizer
	return g
}

// WithHealthChecker sets the health checker.
func (g *AIGateway) WithHealthChecker(checker *ProviderHealthChecker) *AIGateway {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.healthChecker = checker
	return g
}

// WithTimeout sets the default timeout.
func (g *AIGateway) WithTimeout(timeout time.Duration) *AIGateway {
	g.defaultTimeout = timeout
	return g
}

// Build finalizes gateway configuration.
func (g *AIGateway) Build() *AIGateway {
	// Initialize default routers if none specified
	if len(g.routers) == 0 {
		g.routers = append(g.routers, NewDefaultRouter())
	}

	// Initialize default load balancer if none specified
	if g.loadBalancer == nil {
		g.loadBalancer = NewRoundRobinBalancer()
	}

	return g
}

// Chat routes a chat request through the gateway.
func (g *AIGateway) Chat(ctx context.Context, request GatewayRequest) (*GatewayResponse, error) {
	startTime := time.Now()

	// Set timeout
	timeout := g.defaultTimeout
	if request.Timeout > 0 {
		timeout = request.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Make routing decision
	decision, err := g.route(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	// Log routing decision
	if g.logger != nil {
		g.logger.Debug("Gateway routing decision",
			F("provider", decision.Provider),
			F("model", decision.Model),
			F("reason", decision.Reason),
		)
	}

	// Try primary route and fallbacks
	var lastErr error
	retries := 0

	routes := g.buildRouteList(decision)

	for _, route := range routes {
		// Check if provider is healthy
		if g.healthChecker != nil && !g.healthChecker.IsHealthy(route.Provider) {
			if g.logger != nil {
				g.logger.Debug("Skipping unhealthy provider", F("provider", route.Provider))
			}
			continue
		}

		// Get provider manager
		g.mu.RLock()
		manager, exists := g.providers[route.Provider]
		g.mu.RUnlock()

		if !exists {
			continue
		}

		// Execute request
		request.ChatRequest.Provider = route.Provider
		request.ChatRequest.Model = route.Model

		response, err := manager.Chat(ctx, request.ChatRequest)
		if err != nil {
			lastErr = err
			retries++
			if g.logger != nil {
				g.logger.Warn("Gateway request failed, trying fallback",
					F("provider", route.Provider),
					F("error", err.Error()),
				)
			}
			continue
		}

		// Success
		return &GatewayResponse{
			ChatResponse:  response,
			RouteDecision: *decision,
			Latency:       time.Since(startTime),
			Retries:       retries,
			FinalProvider: route.Provider,
			FinalModel:    route.Model,
		}, nil
	}

	return nil, fmt.Errorf("all routes failed, last error: %w", lastErr)
}

// ChatStream routes a streaming chat request through the gateway.
func (g *AIGateway) ChatStream(ctx context.Context, request GatewayRequest, handler func(llm.ChatStreamEvent) error) error {
	// Set timeout
	timeout := g.defaultTimeout
	if request.Timeout > 0 {
		timeout = request.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Make routing decision
	decision, err := g.route(ctx, request)
	if err != nil {
		return fmt.Errorf("routing failed: %w", err)
	}

	// Try primary route and fallbacks
	var lastErr error
	routes := g.buildRouteList(decision)

	for _, route := range routes {
		// Check if provider is healthy
		if g.healthChecker != nil && !g.healthChecker.IsHealthy(route.Provider) {
			continue
		}

		// Get provider manager
		g.mu.RLock()
		manager, exists := g.providers[route.Provider]
		g.mu.RUnlock()

		if !exists {
			continue
		}

		// Execute streaming request
		request.ChatRequest.Provider = route.Provider
		request.ChatRequest.Model = route.Model

		err := manager.ChatStream(ctx, request.ChatRequest, handler)
		if err != nil {
			lastErr = err
			if g.logger != nil {
				g.logger.Warn("Gateway stream failed, trying fallback",
					F("provider", route.Provider),
					F("error", err.Error()),
				)
			}
			continue
		}

		return nil
	}

	return fmt.Errorf("all routes failed, last error: %w", lastErr)
}

// route makes a routing decision for the request.
func (g *AIGateway) route(ctx context.Context, request GatewayRequest) (*RouteDecision, error) {
	// Collect route decisions from all routers
	var bestDecision *RouteDecision

	for _, router := range g.routers {
		decision, err := router.Route(ctx, &RouteRequest{
			ChatRequest:        request.ChatRequest,
			PreferredProviders: request.PreferredProviders,
			ExcludeProviders:   request.ExcludeProviders,
			RequiredCaps:       request.RequiredCaps,
			MaxCost:            request.MaxCost,
			AvailableProviders: g.getAvailableProviders(),
		})

		if err != nil {
			continue
		}

		if bestDecision == nil || decision.Confidence > bestDecision.Confidence {
			bestDecision = decision
		}
	}

	if bestDecision == nil {
		// Fallback to first available provider
		for provider := range g.providers {
			return &RouteDecision{
				Provider:   provider,
				Model:      request.Model,
				Reason:     "default fallback",
				Confidence: 0.5,
			}, nil
		}
		return nil, fmt.Errorf("no providers available")
	}

	return bestDecision, nil
}

// buildRouteList creates a list of routes to try including fallbacks.
func (g *AIGateway) buildRouteList(decision *RouteDecision) []RouteAlternative {
	routes := []RouteAlternative{
		{
			Provider:   decision.Provider,
			Model:      decision.Model,
			Reason:     decision.Reason,
			Confidence: decision.Confidence,
		},
	}

	// Add configured fallbacks
	modelKey := decision.Model
	if fallbacks, ok := g.fallbacks[modelKey]; ok {
		for _, fb := range fallbacks {
			// Parse provider:model format
			provider, model := parseModelSpec(fb)
			if provider == "" {
				provider = decision.Provider
			}
			routes = append(routes, RouteAlternative{
				Provider: provider,
				Model:    model,
				Reason:   "configured fallback",
			})
		}
	}

	// Add alternatives from decision
	routes = append(routes, decision.Alternatives...)

	return routes
}

// getAvailableProviders returns list of registered providers.
func (g *AIGateway) getAvailableProviders() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	providers := make([]string, 0, len(g.providers))
	for p := range g.providers {
		providers = append(providers, p)
	}
	return providers
}

// parseModelSpec parses a "provider:model" string.
func parseModelSpec(spec string) (provider, model string) {
	for i, c := range spec {
		if c == ':' {
			return spec[:i], spec[i+1:]
		}
	}
	return "", spec
}

// GetProviders returns list of registered provider names.
func (g *AIGateway) GetProviders() []string {
	return g.getAvailableProviders()
}

// GetProvider returns a specific provider manager.
func (g *AIGateway) GetProvider(name string) (StreamingLLMManager, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	manager, exists := g.providers[name]
	return manager, exists
}

// ProviderHealthChecker monitors provider health.
type ProviderHealthChecker struct {
	status    map[string]*ProviderHealth
	mu        sync.RWMutex
	checkFunc func(provider string) bool
	interval  time.Duration
	stopChan  chan struct{}
}

// ProviderHealth represents the health status of a provider.
type ProviderHealth struct {
	Provider    string
	Healthy     bool
	LastCheck   time.Time
	LastError   string
	SuccessRate float64
	AvgLatency  time.Duration
	Failures    int64
	Successes   int64
}

// NewProviderHealthChecker creates a new health checker.
func NewProviderHealthChecker(checkFunc func(provider string) bool, interval time.Duration) *ProviderHealthChecker {
	return &ProviderHealthChecker{
		status:    make(map[string]*ProviderHealth),
		checkFunc: checkFunc,
		interval:  interval,
		stopChan:  make(chan struct{}),
	}
}

// Start begins health checking.
func (h *ProviderHealthChecker) Start(providers []string) {
	for _, p := range providers {
		h.status[p] = &ProviderHealth{
			Provider: p,
			Healthy:  true,
		}
	}

	go h.runChecks()
}

// Stop stops health checking.
func (h *ProviderHealthChecker) Stop() {
	close(h.stopChan)
}

// IsHealthy checks if a provider is healthy.
func (h *ProviderHealthChecker) IsHealthy(provider string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if status, ok := h.status[provider]; ok {
		return status.Healthy
	}
	return true // Assume healthy if unknown
}

// GetHealth returns health status for a provider.
func (h *ProviderHealthChecker) GetHealth(provider string) *ProviderHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status[provider]
}

// RecordSuccess records a successful request.
func (h *ProviderHealthChecker) RecordSuccess(provider string, latency time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if status, ok := h.status[provider]; ok {
		status.Successes++
		status.Healthy = true
		total := float64(status.Successes + status.Failures)
		status.SuccessRate = float64(status.Successes) / total
		// Simple moving average for latency
		status.AvgLatency = (status.AvgLatency + latency) / 2
	}
}

// RecordFailure records a failed request.
func (h *ProviderHealthChecker) RecordFailure(provider string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if status, ok := h.status[provider]; ok {
		status.Failures++
		status.LastError = err.Error()
		total := float64(status.Successes + status.Failures)
		status.SuccessRate = float64(status.Successes) / total

		// Mark unhealthy if too many failures
		if status.SuccessRate < 0.5 && status.Failures > 3 {
			status.Healthy = false
		}
	}
}

// runChecks periodically checks provider health.
func (h *ProviderHealthChecker) runChecks() {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.mu.Lock()
			for provider, status := range h.status {
				status.LastCheck = time.Now()
				if h.checkFunc != nil {
					status.Healthy = h.checkFunc(provider)
				}
			}
			h.mu.Unlock()
		case <-h.stopChan:
			return
		}
	}
}

// CostOptimizer optimizes routing based on cost.
type CostOptimizer struct {
	budgetLimit  float64
	currentSpend float64
	costPerModel map[string]float64
	mu           sync.RWMutex
}

// NewCostOptimizer creates a new cost optimizer.
func NewCostOptimizer(budgetLimit float64) *CostOptimizer {
	return &CostOptimizer{
		budgetLimit:  budgetLimit,
		costPerModel: make(map[string]float64),
	}
}

// SetModelCost sets the cost per 1K tokens for a model.
func (c *CostOptimizer) SetModelCost(model string, costPer1KTokens float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.costPerModel[model] = costPer1KTokens
}

// GetModelCost returns the cost per 1K tokens for a model.
func (c *CostOptimizer) GetModelCost(model string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.costPerModel[model]
}

// RecordSpend records spending.
func (c *CostOptimizer) RecordSpend(amount float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentSpend += amount
}

// GetCurrentSpend returns current spending.
func (c *CostOptimizer) GetCurrentSpend() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSpend
}

// IsWithinBudget checks if spending is within budget.
func (c *CostOptimizer) IsWithinBudget() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSpend < c.budgetLimit
}

// RemainingBudget returns remaining budget.
func (c *CostOptimizer) RemainingBudget() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.budgetLimit - c.currentSpend
}

// ResetSpend resets spending to zero.
func (c *CostOptimizer) ResetSpend() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentSpend = 0
}

// SuggestModel suggests the most cost-effective model for a request.
func (c *CostOptimizer) SuggestModel(models []string, maxCost float64) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var cheapest string
	cheapestCost := float64(0)

	for _, model := range models {
		cost := c.costPerModel[model]
		if maxCost > 0 && cost > maxCost {
			continue
		}
		if cheapest == "" || cost < cheapestCost {
			cheapest = model
			cheapestCost = cost
		}
	}

	return cheapest
}
