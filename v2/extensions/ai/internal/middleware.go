package internal

import "time"

// AIMiddlewareType defines the type of AI middleware
type AIMiddlewareType string

const (
	AIMiddlewareTypeLoadBalance          AIMiddlewareType = "load_balance"
	AIMiddlewareTypeAnomalyDetection     AIMiddlewareType = "anomaly_detection"
	AIMiddlewareTypeRateLimit            AIMiddlewareType = "rate_limit"
	AIMiddlewareTypeResponseOptimization AIMiddlewareType = "response_optimization"
	AIMiddlewareTypePersonalization      AIMiddlewareType = "personalization"
	AIMiddlewareTypeOptimization         AIMiddlewareType = "optimization"
	AIMiddlewareTypeSecurity             AIMiddlewareType = "security"
)

// AIMiddlewareConfig contains configuration for AI middleware
type AIMiddlewareConfig struct {
	Enabled    bool                   `json:"enabled"`
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    time.Duration          `json:"timeout"`
	Retries    int                    `json:"retries"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// AIMiddlewareStats contains statistics for AI middleware
type AIMiddlewareStats struct {
	Name              string                 `json:"name"`
	Type              string                 `json:"type"`
	RequestsTotal     int64                  `json:"requests_total"`
	RequestsProcessed int64                  `json:"requests_processed"`
	RequestsBlocked   int64                  `json:"requests_blocked"`
	AverageLatency    time.Duration          `json:"average_latency"`
	ErrorRate         float64                `json:"error_rate"`
	LearningEnabled   bool                   `json:"learning_enabled"`
	AdaptiveChanges   int64                  `json:"adaptive_changes"`
	LastUpdated       time.Time              `json:"last_updated"`
	CustomMetrics     map[string]interface{} `json:"custom_metrics"`
}
