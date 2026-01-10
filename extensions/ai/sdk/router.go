package sdk

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

// ModelRouter routes requests to appropriate models/providers.
type ModelRouter interface {
	// Route makes a routing decision for the given request.
	Route(ctx context.Context, request *RouteRequest) (*RouteDecision, error)
	// SupportedModels returns models this router can handle.
	SupportedModels() []string
	// Name returns the router name.
	Name() string
}

// RouteRequest contains information for routing decisions.
type RouteRequest struct {
	llm.ChatRequest

	PreferredProviders []string
	ExcludeProviders   []string
	RequiredCaps       []string
	MaxCost            float64
	AvailableProviders []string
}

// LoadBalancer distributes load across providers.
type LoadBalancer interface {
	// Select selects a provider from the available options.
	Select(providers []string) string
	// RecordLatency records latency for a provider.
	RecordLatency(provider string, latency time.Duration)
	// RecordError records an error for a provider.
	RecordError(provider string)
}

// ModelCapability represents a model's capabilities.
type ModelCapability struct {
	Model            string
	Provider         string
	MaxTokens        int
	ContextWindow    int
	SupportsVision   bool
	SupportsTools    bool
	SupportsJSON     bool
	SupportStreaming bool
	CostPer1KInput   float64
	CostPer1KOutput  float64
	Tags             []string
}

// DefaultRouter provides basic routing logic.
type DefaultRouter struct {
	capabilities map[string]ModelCapability
	mu           sync.RWMutex
}

// NewDefaultRouter creates a new default router.
func NewDefaultRouter() *DefaultRouter {
	return &DefaultRouter{
		capabilities: make(map[string]ModelCapability),
	}
}

// RegisterModel registers a model's capabilities.
func (r *DefaultRouter) RegisterModel(cap ModelCapability) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := cap.Provider + ":" + cap.Model
	r.capabilities[key] = cap
}

// Route implements ModelRouter.
func (r *DefaultRouter) Route(ctx context.Context, request *RouteRequest) (*RouteDecision, error) {
	// If model is specified, use it directly
	if request.Model != "" {
		provider := request.Provider
		if provider == "" && len(request.AvailableProviders) > 0 {
			provider = request.AvailableProviders[0]
		}

		return &RouteDecision{
			Provider:   provider,
			Model:      request.Model,
			Reason:     "explicit model selection",
			Confidence: 1.0,
		}, nil
	}

	// Find best match from available providers
	for _, provider := range request.AvailableProviders {
		if isExcluded(provider, request.ExcludeProviders) {
			continue
		}

		if len(request.PreferredProviders) > 0 && !isPreferred(provider, request.PreferredProviders) {
			continue
		}

		return &RouteDecision{
			Provider:   provider,
			Model:      getDefaultModel(provider),
			Reason:     "default selection",
			Confidence: 0.7,
		}, nil
	}

	// Fallback to first available
	if len(request.AvailableProviders) > 0 {
		return &RouteDecision{
			Provider:   request.AvailableProviders[0],
			Model:      getDefaultModel(request.AvailableProviders[0]),
			Reason:     "fallback selection",
			Confidence: 0.5,
		}, nil
	}

	return nil, nil
}

// SupportedModels implements ModelRouter.
func (r *DefaultRouter) SupportedModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.capabilities))
	for _, cap := range r.capabilities {
		models = append(models, cap.Model)
	}

	return models
}

// Name implements ModelRouter.
func (r *DefaultRouter) Name() string {
	return "default"
}

// CostBasedRouter routes based on cost optimization.
type CostBasedRouter struct {
	optimizer  *CostOptimizer
	modelCosts map[string]float64
	mu         sync.RWMutex
}

// NewCostBasedRouter creates a cost-based router.
func NewCostBasedRouter(optimizer *CostOptimizer) *CostBasedRouter {
	return &CostBasedRouter{
		optimizer:  optimizer,
		modelCosts: make(map[string]float64),
	}
}

// SetModelCost sets cost for a model.
func (r *CostBasedRouter) SetModelCost(model string, cost float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.modelCosts[model] = cost
}

// Route implements ModelRouter.
func (r *CostBasedRouter) Route(ctx context.Context, request *RouteRequest) (*RouteDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check budget
	if r.optimizer != nil && !r.optimizer.IsWithinBudget() {
		// Route to cheapest available model
		cheapest := r.findCheapestModel(request.AvailableProviders)
		if cheapest != nil {
			return cheapest, nil
		}
	}

	// Route based on max cost constraint
	if request.MaxCost > 0 {
		for _, provider := range request.AvailableProviders {
			model := getDefaultModel(provider)

			key := provider + ":" + model
			if cost, ok := r.modelCosts[key]; ok && cost <= request.MaxCost {
				return &RouteDecision{
					Provider:   provider,
					Model:      model,
					Reason:     "within cost constraint",
					Confidence: 0.8,
				}, nil
			}
		}
	}

	return nil, nil
}

// findCheapestModel finds the cheapest available model.
func (r *CostBasedRouter) findCheapestModel(providers []string) *RouteDecision {
	var cheapest *RouteDecision

	cheapestCost := float64(0)

	for _, provider := range providers {
		model := getDefaultModel(provider)

		key := provider + ":" + model
		if cost, ok := r.modelCosts[key]; ok {
			if cheapest == nil || cost < cheapestCost {
				cheapest = &RouteDecision{
					Provider:   provider,
					Model:      model,
					Reason:     "lowest cost",
					Confidence: 0.9,
				}
				cheapestCost = cost
			}
		}
	}

	return cheapest
}

// SupportedModels implements ModelRouter.
func (r *CostBasedRouter) SupportedModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.modelCosts))
	for key := range r.modelCosts {
		_, model := parseModelSpec(key)
		models = append(models, model)
	}

	return models
}

// Name implements ModelRouter.
func (r *CostBasedRouter) Name() string {
	return "cost-based"
}

// CapabilityRouter routes based on required capabilities.
type CapabilityRouter struct {
	capabilities map[string]ModelCapability
	mu           sync.RWMutex
}

// NewCapabilityRouter creates a capability-based router.
func NewCapabilityRouter() *CapabilityRouter {
	return &CapabilityRouter{
		capabilities: make(map[string]ModelCapability),
	}
}

// RegisterCapability registers a model's capabilities.
func (r *CapabilityRouter) RegisterCapability(cap ModelCapability) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := cap.Provider + ":" + cap.Model
	r.capabilities[key] = cap
}

// Route implements ModelRouter.
func (r *CapabilityRouter) Route(ctx context.Context, request *RouteRequest) (*RouteDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check required capabilities
	if len(request.RequiredCaps) == 0 {
		return nil, nil
	}

	var alternatives []RouteAlternative

	for key, cap := range r.capabilities {
		if isExcluded(cap.Provider, request.ExcludeProviders) {
			continue
		}

		score := r.scoreCapabilities(cap, request.RequiredCaps)
		if score > 0 {
			alt := RouteAlternative{
				Provider:   cap.Provider,
				Model:      cap.Model,
				Reason:     "capability match",
				Confidence: score,
			}
			alternatives = append(alternatives, alt)
		}

		_ = key // avoid unused warning
	}

	if len(alternatives) == 0 {
		return nil, nil
	}

	// Sort by confidence and pick best
	best := alternatives[0]
	for _, alt := range alternatives[1:] {
		if alt.Confidence > best.Confidence {
			best = alt
		}
	}

	return &RouteDecision{
		Provider:     best.Provider,
		Model:        best.Model,
		Reason:       "best capability match",
		Confidence:   best.Confidence,
		Alternatives: alternatives[1:],
	}, nil
}

// scoreCapabilities scores how well a model matches required capabilities.
func (r *CapabilityRouter) scoreCapabilities(cap ModelCapability, required []string) float64 {
	matched := 0

	for _, req := range required {
		switch req {
		case "vision":
			if cap.SupportsVision {
				matched++
			}
		case "tools", "function_calling":
			if cap.SupportsTools {
				matched++
			}
		case "json", "json_mode":
			if cap.SupportsJSON {
				matched++
			}
		case "streaming":
			if cap.SupportStreaming {
				matched++
			}
		default:
			// Check tags
			for _, tag := range cap.Tags {
				if tag == req {
					matched++

					break
				}
			}
		}
	}

	if len(required) == 0 {
		return 0
	}

	return float64(matched) / float64(len(required))
}

// SupportedModels implements ModelRouter.
func (r *CapabilityRouter) SupportedModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.capabilities))
	for _, cap := range r.capabilities {
		models = append(models, cap.Model)
	}

	return models
}

// Name implements ModelRouter.
func (r *CapabilityRouter) Name() string {
	return "capability"
}

// LatencyBasedRouter routes based on historical latency.
type LatencyBasedRouter struct {
	latencies map[string]*latencyStats
	mu        sync.RWMutex
}

type latencyStats struct {
	avg        time.Duration
	count      int64
	lastUpdate time.Time
}

// NewLatencyBasedRouter creates a latency-based router.
func NewLatencyBasedRouter() *LatencyBasedRouter {
	return &LatencyBasedRouter{
		latencies: make(map[string]*latencyStats),
	}
}

// RecordLatency records latency for a provider/model.
func (r *LatencyBasedRouter) RecordLatency(provider, model string, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := provider + ":" + model

	stats, ok := r.latencies[key]
	if !ok {
		stats = &latencyStats{}
		r.latencies[key] = stats
	}

	// Exponential moving average
	stats.count++
	if stats.count == 1 {
		stats.avg = latency
	} else {
		stats.avg = (stats.avg + latency) / 2
	}

	stats.lastUpdate = time.Now()
}

// Route implements ModelRouter.
func (r *LatencyBasedRouter) Route(ctx context.Context, request *RouteRequest) (*RouteDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var fastest *RouteDecision

	fastestLatency := time.Duration(0)

	for _, provider := range request.AvailableProviders {
		if isExcluded(provider, request.ExcludeProviders) {
			continue
		}

		model := request.Model
		if model == "" {
			model = getDefaultModel(provider)
		}

		key := provider + ":" + model
		if stats, ok := r.latencies[key]; ok {
			if fastest == nil || stats.avg < fastestLatency {
				fastest = &RouteDecision{
					Provider:   provider,
					Model:      model,
					Reason:     "lowest latency",
					Confidence: 0.8,
				}
				fastestLatency = stats.avg
			}
		}
	}

	return fastest, nil
}

// SupportedModels implements ModelRouter.
func (r *LatencyBasedRouter) SupportedModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.latencies))
	for key := range r.latencies {
		_, model := parseModelSpec(key)
		models = append(models, model)
	}

	return models
}

// Name implements ModelRouter.
func (r *LatencyBasedRouter) Name() string {
	return "latency"
}

// RoundRobinBalancer implements round-robin load balancing.
type RoundRobinBalancer struct {
	counter uint64
}

// NewRoundRobinBalancer creates a round-robin load balancer.
func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

// Select implements LoadBalancer.
func (b *RoundRobinBalancer) Select(providers []string) string {
	if len(providers) == 0 {
		return ""
	}

	idx := atomic.AddUint64(&b.counter, 1) % uint64(len(providers))

	return providers[idx]
}

// RecordLatency implements LoadBalancer (no-op for round-robin).
func (b *RoundRobinBalancer) RecordLatency(provider string, latency time.Duration) {}

// RecordError implements LoadBalancer (no-op for round-robin).
func (b *RoundRobinBalancer) RecordError(provider string) {}

// WeightedBalancer implements weighted load balancing.
type WeightedBalancer struct {
	weights map[string]int
	mu      sync.RWMutex
}

// NewWeightedBalancer creates a weighted load balancer.
func NewWeightedBalancer() *WeightedBalancer {
	return &WeightedBalancer{
		weights: make(map[string]int),
	}
}

// SetWeight sets weight for a provider.
func (b *WeightedBalancer) SetWeight(provider string, weight int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.weights[provider] = weight
}

// Select implements LoadBalancer.
func (b *WeightedBalancer) Select(providers []string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(providers) == 0 {
		return ""
	}

	// Build weighted list
	totalWeight := 0

	for _, p := range providers {
		w := b.weights[p]
		if w <= 0 {
			w = 1
		}

		totalWeight += w
	}

	// Random selection based on weights
	r := rand.Intn(totalWeight)
	cumulative := 0

	for _, p := range providers {
		w := b.weights[p]
		if w <= 0 {
			w = 1
		}

		cumulative += w
		if r < cumulative {
			return p
		}
	}

	return providers[0]
}

// RecordLatency implements LoadBalancer (no-op for weighted).
func (b *WeightedBalancer) RecordLatency(provider string, latency time.Duration) {}

// RecordError implements LoadBalancer (no-op for weighted).
func (b *WeightedBalancer) RecordError(provider string) {}

// LeastConnectionsBalancer implements least connections load balancing.
type LeastConnectionsBalancer struct {
	connections map[string]int64
	mu          sync.RWMutex
}

// NewLeastConnectionsBalancer creates a least connections load balancer.
func NewLeastConnectionsBalancer() *LeastConnectionsBalancer {
	return &LeastConnectionsBalancer{
		connections: make(map[string]int64),
	}
}

// Select implements LoadBalancer.
func (b *LeastConnectionsBalancer) Select(providers []string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(providers) == 0 {
		return ""
	}

	// Find provider with least connections
	var selected string

	minConns := int64(-1)

	for _, p := range providers {
		conns := b.connections[p]
		if minConns < 0 || conns < minConns {
			selected = p
			minConns = conns
		}
	}

	// Increment connection count
	if selected != "" {
		b.connections[selected]++
	}

	return selected
}

// Release decrements connection count for a provider.
func (b *LeastConnectionsBalancer) Release(provider string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.connections[provider] > 0 {
		b.connections[provider]--
	}
}

// RecordLatency implements LoadBalancer (no-op).
func (b *LeastConnectionsBalancer) RecordLatency(provider string, latency time.Duration) {}

// RecordError implements LoadBalancer (no-op).
func (b *LeastConnectionsBalancer) RecordError(provider string) {}

// Helper functions

func isExcluded(provider string, excluded []string) bool {
	for _, e := range excluded {
		if e == provider {
			return true
		}
	}

	return false
}

func isPreferred(provider string, preferred []string) bool {
	for _, p := range preferred {
		if p == provider {
			return true
		}
	}

	return false
}

func getDefaultModel(provider string) string {
	// Default models for common providers
	defaults := map[string]string{
		"openai":    "gpt-4-turbo",
		"anthropic": "claude-3-opus-20240229",
		"google":    "gemini-pro",
		"cohere":    "command",
		"mistral":   "mistral-large-latest",
		"ollama":    "llama2",
	}

	if model, ok := defaults[provider]; ok {
		return model
	}

	return "default"
}

// LoadBalanceType represents load balancing strategy types.
type LoadBalanceType string

const (
	LoadBalanceRoundRobin       LoadBalanceType = "round_robin"
	LoadBalanceWeighted         LoadBalanceType = "weighted"
	LoadBalanceLeastConnections LoadBalanceType = "least_connections"
	LoadBalanceRandom           LoadBalanceType = "random"
)

// RandomBalancer implements random load balancing.
type RandomBalancer struct{}

// NewRandomBalancer creates a random load balancer.
func NewRandomBalancer() *RandomBalancer {
	return &RandomBalancer{}
}

// Select implements LoadBalancer.
func (b *RandomBalancer) Select(providers []string) string {
	if len(providers) == 0 {
		return ""
	}

	return providers[rand.Intn(len(providers))]
}

// RecordLatency implements LoadBalancer (no-op).
func (b *RandomBalancer) RecordLatency(provider string, latency time.Duration) {}

// RecordError implements LoadBalancer (no-op).
func (b *RandomBalancer) RecordError(provider string) {}
