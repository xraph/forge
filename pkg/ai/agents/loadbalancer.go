package agents

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/xraph/forge/pkg/ai"
)

// LoadBalancerAgent provides intelligent load balancing decisions
type LoadBalancerAgent struct {
	*ai.BaseAgent

	// Load balancing state
	serverMetrics      map[string]*ServerMetrics
	loadBalancingRules []LoadBalancingRule
	algorithms         map[string]LoadBalancingAlgorithm

	// Prediction models
	loadPredictor     *LoadPredictor
	capacityEstimator *CapacityEstimator
	healthMonitor     *HealthMonitor

	// Configuration
	responseTimeWeight float64
	cpuWeight          float64
	memoryWeight       float64
	connectionWeight   float64
	healthWeight       float64

	// Adaptive parameters
	adaptationEnabled  bool
	learningRate       float64
	rebalanceThreshold float64
}

// ServerMetrics represents metrics for a server
type ServerMetrics struct {
	ServerID      string            `json:"server_id"`
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	ResponseTime  time.Duration     `json:"response_time"`
	CPUUsage      float64           `json:"cpu_usage"`
	MemoryUsage   float64           `json:"memory_usage"`
	ActiveConns   int               `json:"active_connections"`
	TotalRequests int64             `json:"total_requests"`
	ErrorRate     float64           `json:"error_rate"`
	Capacity      int               `json:"capacity"`
	HealthStatus  HealthStatus      `json:"health_status"`
	LastUpdate    time.Time         `json:"last_update"`
	LoadScore     float64           `json:"load_score"`
	Weight        float64           `json:"weight"`
	Metadata      map[string]string `json:"metadata"`
}

// HealthStatus represents the health status of a server
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// LoadBalancingRule represents a rule for load balancing
type LoadBalancingRule struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Condition  string                 `json:"condition"`
	Action     string                 `json:"action"`
	Priority   int                    `json:"priority"`
	Enabled    bool                   `json:"enabled"`
	Parameters map[string]interface{} `json:"parameters"`
}

// LoadBalancingAlgorithm represents a load balancing algorithm
type LoadBalancingAlgorithm interface {
	Name() string
	SelectServer(servers []*ServerMetrics, request *RequestContext) (*ServerMetrics, error)
	CalculateWeights(servers []*ServerMetrics) error
	GetScore(server *ServerMetrics) float64
}

// RequestContext provides context for load balancing decisions
type RequestContext struct {
	UserID        string            `json:"user_id"`
	SessionID     string            `json:"session_id"`
	RequestType   string            `json:"request_type"`
	Priority      int               `json:"priority"`
	StickySession bool              `json:"sticky_session"`
	Metadata      map[string]string `json:"metadata"`
}

// LoadBalancingDecision represents a load balancing decision
type LoadBalancingDecision struct {
	SelectedServer *ServerMetrics         `json:"selected_server"`
	Algorithm      string                 `json:"algorithm"`
	Score          float64                `json:"score"`
	Confidence     float64                `json:"confidence"`
	Reasoning      string                 `json:"reasoning"`
	Alternatives   []*ServerMetrics       `json:"alternatives"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// NewLoadBalancerAgent creates a new load balancer agent
func NewLoadBalancerAgent(id, name string) ai.AIAgent {
	capabilities := []ai.Capability{
		{
			Name:        "load-balancing",
			Description: "Make intelligent load balancing decisions",
			InputType:   reflect.TypeOf(LoadBalancingInput{}),
			OutputType:  reflect.TypeOf(LoadBalancingOutput{}),
			Metadata: map[string]interface{}{
				"category": "load_balancing",
				"priority": "high",
			},
		},
		{
			Name:        "capacity-planning",
			Description: "Plan server capacity based on load predictions",
			InputType:   reflect.TypeOf(CapacityPlanningInput{}),
			OutputType:  reflect.TypeOf(CapacityPlanningOutput{}),
			Metadata: map[string]interface{}{
				"category": "planning",
				"priority": "medium",
			},
		},
		{
			Name:        "health-monitoring",
			Description: "Monitor and assess server health",
			InputType:   reflect.TypeOf(HealthMonitoringInput{}),
			OutputType:  reflect.TypeOf(HealthMonitoringOutput{}),
			Metadata: map[string]interface{}{
				"category": "monitoring",
				"priority": "high",
			},
		},
	}

	agent := &LoadBalancerAgent{
		BaseAgent:          ai.NewBaseAgent(id, name, ai.AgentTypeLoadBalancer, capabilities),
		serverMetrics:      make(map[string]*ServerMetrics),
		loadBalancingRules: make([]LoadBalancingRule, 0),
		algorithms:         make(map[string]LoadBalancingAlgorithm),
		loadPredictor:      NewLoadPredictor(),
		capacityEstimator:  NewCapacityEstimator(),
		healthMonitor:      NewHealthMonitor(),
		responseTimeWeight: 0.3,
		cpuWeight:          0.2,
		memoryWeight:       0.2,
		connectionWeight:   0.15,
		healthWeight:       0.15,
		adaptationEnabled:  true,
		learningRate:       0.1,
		rebalanceThreshold: 0.8,
	}

	// Initialize algorithms
	agent.initializeAlgorithms()

	// Initialize default rules
	agent.initializeDefaultRules()

	return agent
}

// Process processes load balancing requests
func (a *LoadBalancerAgent) Process(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
	// Call base implementation first
	output, err := a.BaseAgent.Process(ctx, input)
	if err != nil {
		return output, err
	}

	// Handle specific load balancing types
	switch input.Type {
	case "load-balancing":
		return a.processLoadBalancing(ctx, input)
	case "capacity-planning":
		return a.processCapacityPlanning(ctx, input)
	case "health-monitoring":
		return a.processHealthMonitoring(ctx, input)
	default:
		return output, fmt.Errorf("unsupported load balancing type: %s", input.Type)
	}
}

// processLoadBalancing processes load balancing requests
func (a *LoadBalancerAgent) processLoadBalancing(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
	// Parse input
	data, ok := input.Data.(map[string]interface{})
	if !ok {
		return ai.AgentOutput{}, fmt.Errorf("invalid input data format")
	}

	// Extract server metrics and request context
	servers, err := a.extractServerMetrics(data)
	if err != nil {
		return ai.AgentOutput{}, fmt.Errorf("failed to extract server metrics: %w", err)
	}

	requestContext := a.extractRequestContext(data)

	// Update internal server metrics
	a.updateServerMetrics(servers)

	// Calculate load scores for all servers
	a.calculateLoadScores(servers)

	// Select the best algorithm based on current conditions
	algorithm := a.selectBestAlgorithm(servers, requestContext)

	// Make load balancing decision
	decision, err := a.makeLoadBalancingDecision(servers, requestContext, algorithm)
	if err != nil {
		return ai.AgentOutput{}, fmt.Errorf("failed to make load balancing decision: %w", err)
	}

	// Generate load balancing actions
	actions := a.generateLoadBalancingActions(decision, servers)

	// Learn from the decision if adaptation is enabled
	if a.adaptationEnabled {
		go a.learnFromDecision(decision, servers, requestContext)
	}

	return ai.AgentOutput{
		Type:        "load-balancing-result",
		Data:        decision,
		Confidence:  decision.Confidence,
		Explanation: decision.Reasoning,
		Actions:     actions,
		Metadata: map[string]interface{}{
			"algorithm":       algorithm.Name(),
			"servers_count":   len(servers),
			"selected_server": decision.SelectedServer.ServerID,
		},
		Timestamp: time.Now(),
	}, nil
}

// processCapacityPlanning processes capacity planning requests
func (a *LoadBalancerAgent) processCapacityPlanning(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
	// Parse input
	data, ok := input.Data.(map[string]interface{})
	if !ok {
		return ai.AgentOutput{}, fmt.Errorf("invalid input data format")
	}

	// Extract current load and prediction horizon
	currentLoad := a.extractCurrentLoad(data)
	horizon := a.extractHorizon(data)

	// Predict future load
	loadPrediction, err := a.loadPredictor.PredictLoad(currentLoad, horizon)
	if err != nil {
		return ai.AgentOutput{}, fmt.Errorf("failed to predict load: %w", err)
	}

	// Estimate required capacity
	capacityRecommendation, err := a.capacityEstimator.EstimateCapacity(loadPrediction, a.serverMetrics)
	if err != nil {
		return ai.AgentOutput{}, fmt.Errorf("failed to estimate capacity: %w", err)
	}

	// Generate capacity planning actions
	actions := a.generateCapacityActions(capacityRecommendation)

	return ai.AgentOutput{
		Type:        "capacity-planning-result",
		Data:        capacityRecommendation,
		Confidence:  capacityRecommendation.Confidence,
		Explanation: fmt.Sprintf("Capacity planning for %s horizon", horizon),
		Actions:     actions,
		Metadata: map[string]interface{}{
			"horizon":           horizon.String(),
			"current_capacity":  capacityRecommendation.CurrentCapacity,
			"required_capacity": capacityRecommendation.RequiredCapacity,
		},
		Timestamp: time.Now(),
	}, nil
}

// processHealthMonitoring processes health monitoring requests
func (a *LoadBalancerAgent) processHealthMonitoring(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
	// Parse input
	data, ok := input.Data.(map[string]interface{})
	if !ok {
		return ai.AgentOutput{}, fmt.Errorf("invalid input data format")
	}

	// Extract servers for health monitoring
	servers, err := a.extractServerMetrics(data)
	if err != nil {
		return ai.AgentOutput{}, fmt.Errorf("failed to extract server metrics: %w", err)
	}

	// Perform health assessment
	healthAssessment := a.healthMonitor.AssessHealth(servers)

	// Generate health monitoring actions
	actions := a.generateHealthActions(healthAssessment)

	return ai.AgentOutput{
		Type:        "health-monitoring-result",
		Data:        healthAssessment,
		Confidence:  healthAssessment.Confidence,
		Explanation: fmt.Sprintf("Health assessment for %d servers", len(servers)),
		Actions:     actions,
		Metadata: map[string]interface{}{
			"servers_count":     len(servers),
			"healthy_servers":   healthAssessment.HealthyServers,
			"degraded_servers":  healthAssessment.DegradedServers,
			"unhealthy_servers": healthAssessment.UnhealthyServers,
		},
		Timestamp: time.Now(),
	}, nil
}

// extractServerMetrics extracts server metrics from input data
func (a *LoadBalancerAgent) extractServerMetrics(data map[string]interface{}) ([]*ServerMetrics, error) {
	servers := make([]*ServerMetrics, 0)

	serversData, ok := data["servers"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("servers data not found or invalid format")
	}

	for _, serverData := range serversData {
		serverMap, ok := serverData.(map[string]interface{})
		if !ok {
			continue
		}

		server := &ServerMetrics{
			ServerID:      a.getStringValue(serverMap, "server_id"),
			Host:          a.getStringValue(serverMap, "host"),
			Port:          int(a.getFloatValue(serverMap, "port")),
			ResponseTime:  time.Duration(a.getFloatValue(serverMap, "response_time")) * time.Millisecond,
			CPUUsage:      a.getFloatValue(serverMap, "cpu_usage"),
			MemoryUsage:   a.getFloatValue(serverMap, "memory_usage"),
			ActiveConns:   int(a.getFloatValue(serverMap, "active_connections")),
			TotalRequests: int64(a.getFloatValue(serverMap, "total_requests")),
			ErrorRate:     a.getFloatValue(serverMap, "error_rate"),
			Capacity:      int(a.getFloatValue(serverMap, "capacity")),
			HealthStatus:  HealthStatus(a.getStringValue(serverMap, "health_status")),
			LastUpdate:    time.Now(),
			Weight:        a.getFloatValue(serverMap, "weight"),
			Metadata:      a.getStringMap(serverMap, "metadata"),
		}

		servers = append(servers, server)
	}

	return servers, nil
}

// extractRequestContext extracts request context from input data
func (a *LoadBalancerAgent) extractRequestContext(data map[string]interface{}) *RequestContext {
	requestData, ok := data["request"].(map[string]interface{})
	if !ok {
		return &RequestContext{}
	}

	return &RequestContext{
		UserID:        a.getStringValue(requestData, "user_id"),
		SessionID:     a.getStringValue(requestData, "session_id"),
		RequestType:   a.getStringValue(requestData, "request_type"),
		Priority:      int(a.getFloatValue(requestData, "priority")),
		StickySession: a.getBoolValue(requestData, "sticky_session"),
		Metadata:      a.getStringMap(requestData, "metadata"),
	}
}

// updateServerMetrics updates the internal server metrics
func (a *LoadBalancerAgent) updateServerMetrics(servers []*ServerMetrics) {
	for _, server := range servers {
		a.serverMetrics[server.ServerID] = server
	}
}

// calculateLoadScores calculates load scores for all servers
func (a *LoadBalancerAgent) calculateLoadScores(servers []*ServerMetrics) {
	for _, server := range servers {
		score := a.calculateLoadScore(server)
		server.LoadScore = score
	}
}

// calculateLoadScore calculates a load score for a server
func (a *LoadBalancerAgent) calculateLoadScore(server *ServerMetrics) float64 {
	// Weighted score based on multiple factors
	score := 0.0

	// Response time score (lower is better)
	if server.ResponseTime > 0 {
		responseTimeScore := math.Max(0, 1.0-float64(server.ResponseTime.Milliseconds())/1000.0)
		score += a.responseTimeWeight * responseTimeScore
	}

	// CPU usage score (lower is better)
	cpuScore := math.Max(0, 1.0-server.CPUUsage)
	score += a.cpuWeight * cpuScore

	// Memory usage score (lower is better)
	memoryScore := math.Max(0, 1.0-server.MemoryUsage)
	score += a.memoryWeight * memoryScore

	// Connection score (based on capacity)
	connectionScore := 1.0
	if server.Capacity > 0 {
		connectionScore = math.Max(0, 1.0-float64(server.ActiveConns)/float64(server.Capacity))
	}
	score += a.connectionWeight * connectionScore

	// Health score
	healthScore := a.getHealthScore(server.HealthStatus)
	score += a.healthWeight * healthScore

	return math.Max(0, math.Min(1, score))
}

// getHealthScore returns a numeric score for health status
func (a *LoadBalancerAgent) getHealthScore(status HealthStatus) float64 {
	switch status {
	case HealthStatusHealthy:
		return 1.0
	case HealthStatusDegraded:
		return 0.5
	case HealthStatusUnhealthy:
		return 0.0
	default:
		return 0.3
	}
}

// selectBestAlgorithm selects the best load balancing algorithm
func (a *LoadBalancerAgent) selectBestAlgorithm(servers []*ServerMetrics, requestContext *RequestContext) LoadBalancingAlgorithm {
	// Default to weighted round robin
	algorithm := a.algorithms["weighted_round_robin"]

	// Select algorithm based on conditions
	if requestContext.StickySession {
		algorithm = a.algorithms["sticky_session"]
	} else if a.hasHighVariance(servers) {
		algorithm = a.algorithms["least_connections"]
	} else if a.hasCapacityConstraints(servers) {
		algorithm = a.algorithms["capacity_aware"]
	}

	return algorithm
}

// makeLoadBalancingDecision makes a load balancing decision
func (a *LoadBalancerAgent) makeLoadBalancingDecision(servers []*ServerMetrics, requestContext *RequestContext, algorithm LoadBalancingAlgorithm) (*LoadBalancingDecision, error) {
	// Filter healthy servers
	healthyServers := a.filterHealthyServers(servers)
	if len(healthyServers) == 0 {
		return nil, fmt.Errorf("no healthy servers available")
	}

	// Select server using the algorithm
	selectedServer, err := algorithm.SelectServer(healthyServers, requestContext)
	if err != nil {
		return nil, fmt.Errorf("algorithm failed to select server: %w", err)
	}

	// Calculate confidence based on server load score and health
	confidence := a.calculateDecisionConfidence(selectedServer, healthyServers)

	// Generate reasoning
	reasoning := fmt.Sprintf("Selected server %s using %s algorithm (score: %.2f, confidence: %.2f)",
		selectedServer.ServerID, algorithm.Name(), selectedServer.LoadScore, confidence)

	// Get alternative servers
	alternatives := a.getAlternativeServers(healthyServers, selectedServer, 3)

	return &LoadBalancingDecision{
		SelectedServer: selectedServer,
		Algorithm:      algorithm.Name(),
		Score:          selectedServer.LoadScore,
		Confidence:     confidence,
		Reasoning:      reasoning,
		Alternatives:   alternatives,
		Metadata: map[string]interface{}{
			"total_servers":   len(servers),
			"healthy_servers": len(healthyServers),
			"algorithm":       algorithm.Name(),
		},
	}, nil
}

// generateLoadBalancingActions generates actions for load balancing
func (a *LoadBalancerAgent) generateLoadBalancingActions(decision *LoadBalancingDecision, servers []*ServerMetrics) []ai.AgentAction {
	actions := make([]ai.AgentAction, 0)

	// Primary action: route to selected server
	actions = append(actions, ai.AgentAction{
		Type:   "route_request",
		Target: decision.SelectedServer.ServerID,
		Parameters: map[string]interface{}{
			"server_id": decision.SelectedServer.ServerID,
			"host":      decision.SelectedServer.Host,
			"port":      decision.SelectedServer.Port,
			"weight":    decision.SelectedServer.Weight,
		},
		Priority: 1,
		Timeout:  30 * time.Second,
	})

	// Check if rebalancing is needed
	if a.needsRebalancing(servers) {
		actions = append(actions, ai.AgentAction{
			Type:   "rebalance_load",
			Target: "load_balancer",
			Parameters: map[string]interface{}{
				"reason":    "load_imbalance_detected",
				"threshold": a.rebalanceThreshold,
			},
			Priority: 5,
			Timeout:  60 * time.Second,
		})
	}

	// Check for unhealthy servers
	for _, server := range servers {
		if server.HealthStatus == HealthStatusUnhealthy {
			actions = append(actions, ai.AgentAction{
				Type:   "remove_server",
				Target: server.ServerID,
				Parameters: map[string]interface{}{
					"server_id": server.ServerID,
					"reason":    "unhealthy_server",
				},
				Priority: 2,
				Timeout:  30 * time.Second,
			})
		}
	}

	return actions
}

// Helper methods

func (a *LoadBalancerAgent) filterHealthyServers(servers []*ServerMetrics) []*ServerMetrics {
	healthy := make([]*ServerMetrics, 0)
	for _, server := range servers {
		if server.HealthStatus == HealthStatusHealthy || server.HealthStatus == HealthStatusDegraded {
			healthy = append(healthy, server)
		}
	}
	return healthy
}

func (a *LoadBalancerAgent) hasHighVariance(servers []*ServerMetrics) bool {
	if len(servers) < 2 {
		return false
	}

	// Calculate variance in load scores
	mean := 0.0
	for _, server := range servers {
		mean += server.LoadScore
	}
	mean /= float64(len(servers))

	variance := 0.0
	for _, server := range servers {
		variance += math.Pow(server.LoadScore-mean, 2)
	}
	variance /= float64(len(servers))

	return variance > 0.1 // High variance threshold
}

func (a *LoadBalancerAgent) hasCapacityConstraints(servers []*ServerMetrics) bool {
	for _, server := range servers {
		if server.Capacity > 0 && float64(server.ActiveConns)/float64(server.Capacity) > 0.8 {
			return true
		}
	}
	return false
}

func (a *LoadBalancerAgent) calculateDecisionConfidence(selectedServer *ServerMetrics, healthyServers []*ServerMetrics) float64 {
	// Base confidence on server load score
	confidence := selectedServer.LoadScore

	// Adjust based on health status
	if selectedServer.HealthStatus == HealthStatusHealthy {
		confidence *= 1.0
	} else if selectedServer.HealthStatus == HealthStatusDegraded {
		confidence *= 0.8
	}

	// Adjust based on server capacity
	if selectedServer.Capacity > 0 {
		utilizationRate := float64(selectedServer.ActiveConns) / float64(selectedServer.Capacity)
		confidence *= 1.0 - utilizationRate
	}

	return math.Max(0.1, math.Min(0.99, confidence))
}

func (a *LoadBalancerAgent) getAlternativeServers(servers []*ServerMetrics, selected *ServerMetrics, count int) []*ServerMetrics {
	alternatives := make([]*ServerMetrics, 0)

	// Sort servers by load score (descending)
	sortedServers := make([]*ServerMetrics, len(servers))
	copy(sortedServers, servers)
	sort.Slice(sortedServers, func(i, j int) bool {
		return sortedServers[i].LoadScore > sortedServers[j].LoadScore
	})

	// Add alternatives (excluding selected server)
	for _, server := range sortedServers {
		if server.ServerID != selected.ServerID && len(alternatives) < count {
			alternatives = append(alternatives, server)
		}
	}

	return alternatives
}

func (a *LoadBalancerAgent) needsRebalancing(servers []*ServerMetrics) bool {
	if len(servers) < 2 {
		return false
	}

	// Check if any server is above the rebalance threshold
	for _, server := range servers {
		if server.LoadScore < (1.0 - a.rebalanceThreshold) {
			return true
		}
	}

	return false
}

func (a *LoadBalancerAgent) learnFromDecision(decision *LoadBalancingDecision, servers []*ServerMetrics, requestContext *RequestContext) {
	// Implement learning logic based on decision outcomes
	// This would typically involve collecting feedback about the decision
	// and adjusting algorithm weights or parameters
}

// Utility methods for data extraction

func (a *LoadBalancerAgent) getStringValue(data map[string]interface{}, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

func (a *LoadBalancerAgent) getFloatValue(data map[string]interface{}, key string) float64 {
	if value, ok := data[key].(float64); ok {
		return value
	}
	if value, ok := data[key].(int); ok {
		return float64(value)
	}
	return 0.0
}

func (a *LoadBalancerAgent) getBoolValue(data map[string]interface{}, key string) bool {
	if value, ok := data[key].(bool); ok {
		return value
	}
	return false
}

func (a *LoadBalancerAgent) getStringMap(data map[string]interface{}, key string) map[string]string {
	if value, ok := data[key].(map[string]interface{}); ok {
		result := make(map[string]string)
		for k, v := range value {
			if str, ok := v.(string); ok {
				result[k] = str
			}
		}
		return result
	}
	return make(map[string]string)
}

func (a *LoadBalancerAgent) extractCurrentLoad(data map[string]interface{}) *LoadMetrics {
	// Extract current load metrics
	return &LoadMetrics{
		RequestsPerSecond:   a.getFloatValue(data, "requests_per_second"),
		AverageResponseTime: time.Duration(a.getFloatValue(data, "average_response_time")) * time.Millisecond,
		ErrorRate:           a.getFloatValue(data, "error_rate"),
		ConcurrentUsers:     int(a.getFloatValue(data, "concurrent_users")),
	}
}

func (a *LoadBalancerAgent) extractHorizon(data map[string]interface{}) time.Duration {
	if horizonStr, ok := data["horizon"].(string); ok {
		if duration, err := time.ParseDuration(horizonStr); err == nil {
			return duration
		}
	}
	return 1 * time.Hour // Default horizon
}

func (a *LoadBalancerAgent) generateCapacityActions(recommendation *CapacityRecommendation) []ai.AgentAction {
	actions := make([]ai.AgentAction, 0)

	if recommendation.RequiredCapacity > recommendation.CurrentCapacity {
		actions = append(actions, ai.AgentAction{
			Type:   "scale_up",
			Target: "server_pool",
			Parameters: map[string]interface{}{
				"current_capacity":  recommendation.CurrentCapacity,
				"required_capacity": recommendation.RequiredCapacity,
				"scale_factor":      recommendation.ScaleFactor,
			},
			Priority: 3,
			Timeout:  5 * time.Minute,
		})
	}

	return actions
}

func (a *LoadBalancerAgent) generateHealthActions(assessment *HealthAssessment) []ai.AgentAction {
	actions := make([]ai.AgentAction, 0)

	for _, server := range assessment.UnhealthyServers {
		actions = append(actions, ai.AgentAction{
			Type:   "health_check",
			Target: server.ServerID,
			Parameters: map[string]interface{}{
				"server_id": server.ServerID,
				"action":    "restart_health_check",
			},
			Priority: 2,
			Timeout:  60 * time.Second,
		})
	}

	return actions
}

// Initialize algorithms and rules

func (a *LoadBalancerAgent) initializeAlgorithms() {
	a.algorithms["weighted_round_robin"] = NewWeightedRoundRobinAlgorithm()
	a.algorithms["least_connections"] = NewLeastConnectionsAlgorithm()
	a.algorithms["capacity_aware"] = NewCapacityAwareAlgorithm()
	a.algorithms["sticky_session"] = NewStickySessionAlgorithm()
}

func (a *LoadBalancerAgent) initializeDefaultRules() {
	a.loadBalancingRules = []LoadBalancingRule{
		{
			ID:        "high_cpu_usage",
			Name:      "High CPU Usage",
			Condition: "cpu_usage > 0.8",
			Action:    "reduce_weight",
			Priority:  1,
			Enabled:   true,
		},
		{
			ID:        "high_error_rate",
			Name:      "High Error Rate",
			Condition: "error_rate > 0.05",
			Action:    "remove_server",
			Priority:  2,
			Enabled:   true,
		},
	}
}

// Input/Output types

type LoadBalancingInput struct {
	Servers []*ServerMetrics       `json:"servers"`
	Request *RequestContext        `json:"request"`
	Options map[string]interface{} `json:"options"`
}

type LoadBalancingOutput struct {
	Decision   *LoadBalancingDecision `json:"decision"`
	Confidence float64                `json:"confidence"`
}

type CapacityPlanningInput struct {
	CurrentLoad *LoadMetrics           `json:"current_load"`
	Horizon     time.Duration          `json:"horizon"`
	Options     map[string]interface{} `json:"options"`
}

type CapacityPlanningOutput struct {
	Recommendation *CapacityRecommendation `json:"recommendation"`
	Confidence     float64                 `json:"confidence"`
}

type HealthMonitoringInput struct {
	Servers []*ServerMetrics       `json:"servers"`
	Options map[string]interface{} `json:"options"`
}

type HealthMonitoringOutput struct {
	Assessment *HealthAssessment `json:"assessment"`
	Confidence float64           `json:"confidence"`
}

// Supporting types

type LoadMetrics struct {
	RequestsPerSecond   float64       `json:"requests_per_second"`
	AverageResponseTime time.Duration `json:"average_response_time"`
	ErrorRate           float64       `json:"error_rate"`
	ConcurrentUsers     int           `json:"concurrent_users"`
}

type CapacityRecommendation struct {
	CurrentCapacity  int     `json:"current_capacity"`
	RequiredCapacity int     `json:"required_capacity"`
	ScaleFactor      float64 `json:"scale_factor"`
	Confidence       float64 `json:"confidence"`
	Reasoning        string  `json:"reasoning"`
}

type HealthAssessment struct {
	HealthyServers   []*ServerMetrics `json:"healthy_servers"`
	DegradedServers  []*ServerMetrics `json:"degraded_servers"`
	UnhealthyServers []*ServerMetrics `json:"unhealthy_servers"`
	OverallHealth    float64          `json:"overall_health"`
	Confidence       float64          `json:"confidence"`
}

// Placeholder implementations for supporting components

type LoadPredictor struct{}

func NewLoadPredictor() *LoadPredictor {
	return &LoadPredictor{}
}

func (lp *LoadPredictor) PredictLoad(currentLoad *LoadMetrics, horizon time.Duration) (*LoadPrediction, error) {
	// Simplified prediction logic
	return &LoadPrediction{
		PredictedRPS: currentLoad.RequestsPerSecond * 1.1, // 10% increase
		Confidence:   0.7,
	}, nil
}

type LoadPrediction struct {
	PredictedRPS float64 `json:"predicted_rps"`
	Confidence   float64 `json:"confidence"`
}

type CapacityEstimator struct{}

func NewCapacityEstimator() *CapacityEstimator {
	return &CapacityEstimator{}
}

func (ce *CapacityEstimator) EstimateCapacity(prediction *LoadPrediction, servers map[string]*ServerMetrics) (*CapacityRecommendation, error) {
	currentCapacity := len(servers)
	requiredCapacity := int(math.Ceil(prediction.PredictedRPS / 1000.0)) // 1000 RPS per server

	return &CapacityRecommendation{
		CurrentCapacity:  currentCapacity,
		RequiredCapacity: requiredCapacity,
		ScaleFactor:      float64(requiredCapacity) / float64(currentCapacity),
		Confidence:       prediction.Confidence,
		Reasoning:        "Based on load prediction and server capacity",
	}, nil
}

type HealthMonitor struct{}

func NewHealthMonitor() *HealthMonitor {
	return &HealthMonitor{}
}

func (hm *HealthMonitor) AssessHealth(servers []*ServerMetrics) *HealthAssessment {
	var healthy, degraded, unhealthy []*ServerMetrics

	for _, server := range servers {
		switch server.HealthStatus {
		case HealthStatusHealthy:
			healthy = append(healthy, server)
		case HealthStatusDegraded:
			degraded = append(degraded, server)
		case HealthStatusUnhealthy:
			unhealthy = append(unhealthy, server)
		}
	}

	overallHealth := float64(len(healthy)) / float64(len(servers))

	return &HealthAssessment{
		HealthyServers:   healthy,
		DegradedServers:  degraded,
		UnhealthyServers: unhealthy,
		OverallHealth:    overallHealth,
		Confidence:       0.9,
	}
}

// Simple algorithm implementations

type WeightedRoundRobinAlgorithm struct{}

func NewWeightedRoundRobinAlgorithm() *WeightedRoundRobinAlgorithm {
	return &WeightedRoundRobinAlgorithm{}
}

func (wrr *WeightedRoundRobinAlgorithm) Name() string {
	return "weighted_round_robin"
}

func (wrr *WeightedRoundRobinAlgorithm) SelectServer(servers []*ServerMetrics, request *RequestContext) (*ServerMetrics, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers available")
	}

	// Select server with highest load score
	var bestServer *ServerMetrics
	for _, server := range servers {
		if bestServer == nil || server.LoadScore > bestServer.LoadScore {
			bestServer = server
		}
	}

	return bestServer, nil
}

func (wrr *WeightedRoundRobinAlgorithm) CalculateWeights(servers []*ServerMetrics) error {
	for _, server := range servers {
		server.Weight = server.LoadScore
	}
	return nil
}

func (wrr *WeightedRoundRobinAlgorithm) GetScore(server *ServerMetrics) float64 {
	return server.LoadScore
}

type LeastConnectionsAlgorithm struct{}

func NewLeastConnectionsAlgorithm() *LeastConnectionsAlgorithm {
	return &LeastConnectionsAlgorithm{}
}

func (lc *LeastConnectionsAlgorithm) Name() string {
	return "least_connections"
}

func (lc *LeastConnectionsAlgorithm) SelectServer(servers []*ServerMetrics, request *RequestContext) (*ServerMetrics, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers available")
	}

	// Select server with least connections
	var bestServer *ServerMetrics
	for _, server := range servers {
		if bestServer == nil || server.ActiveConns < bestServer.ActiveConns {
			bestServer = server
		}
	}

	return bestServer, nil
}

func (lc *LeastConnectionsAlgorithm) CalculateWeights(servers []*ServerMetrics) error {
	return nil
}

func (lc *LeastConnectionsAlgorithm) GetScore(server *ServerMetrics) float64 {
	if server.Capacity > 0 {
		return 1.0 - float64(server.ActiveConns)/float64(server.Capacity)
	}
	return 1.0
}

type CapacityAwareAlgorithm struct{}

func NewCapacityAwareAlgorithm() *CapacityAwareAlgorithm {
	return &CapacityAwareAlgorithm{}
}

func (ca *CapacityAwareAlgorithm) Name() string {
	return "capacity_aware"
}

func (ca *CapacityAwareAlgorithm) SelectServer(servers []*ServerMetrics, request *RequestContext) (*ServerMetrics, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers available")
	}

	// Select server with best capacity score
	var bestServer *ServerMetrics
	bestScore := -1.0

	for _, server := range servers {
		score := ca.GetScore(server)
		if score > bestScore {
			bestScore = score
			bestServer = server
		}
	}

	return bestServer, nil
}

func (ca *CapacityAwareAlgorithm) CalculateWeights(servers []*ServerMetrics) error {
	return nil
}

func (ca *CapacityAwareAlgorithm) GetScore(server *ServerMetrics) float64 {
	if server.Capacity > 0 {
		return math.Max(0, 1.0-float64(server.ActiveConns)/float64(server.Capacity))
	}
	return server.LoadScore
}

type StickySessionAlgorithm struct{}

func NewStickySessionAlgorithm() *StickySessionAlgorithm {
	return &StickySessionAlgorithm{}
}

func (ss *StickySessionAlgorithm) Name() string {
	return "sticky_session"
}

func (ss *StickySessionAlgorithm) SelectServer(servers []*ServerMetrics, request *RequestContext) (*ServerMetrics, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers available")
	}

	// For sticky sessions, we'd normally hash the session ID
	// For now, just return the first server
	return servers[0], nil
}

func (ss *StickySessionAlgorithm) CalculateWeights(servers []*ServerMetrics) error {
	return nil
}

func (ss *StickySessionAlgorithm) GetScore(server *ServerMetrics) float64 {
	return server.LoadScore
}
