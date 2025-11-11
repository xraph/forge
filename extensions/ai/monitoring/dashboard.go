package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	ai "github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/internal/logger"
)

// AIDashboard provides comprehensive analytics and visualization for AI systems.
type AIDashboard struct {
	aiManager        ai.AI
	healthMonitor    *AIHealthMonitor
	metricsCollector *AIMetricsCollector
	alertManager     *AIAlertManager
	logger           forge.Logger
	metrics          forge.Metrics

	// Dashboard state
	config        DashboardConfig
	widgets       map[string]*Widget
	layouts       map[string]*Layout
	customQueries map[string]*CustomQuery

	// Real-time data
	realtimeData *RealtimeData
	dataStreams  map[string]*DataStream

	// HTTP server for dashboard UI
	server *http.Server
	router *http.ServeMux

	mu      sync.RWMutex
	started bool
}

// DashboardConfig contains configuration for the AI dashboard.
type DashboardConfig struct {
	Port                     int           `default:"8090"                       yaml:"port"`
	RefreshInterval          time.Duration `default:"30s"                        yaml:"refresh_interval"`
	DataRetentionPeriod      time.Duration `default:"24h"                        yaml:"data_retention_period"`
	MaxDataPoints            int           `default:"1000"                       yaml:"max_data_points"`
	EnableRealtime           bool          `default:"true"                       yaml:"enable_realtime"`
	EnableAuthentication     bool          `default:"true"                       yaml:"enable_authentication"`
	CustomDashboards         bool          `default:"true"                       yaml:"custom_dashboards"`
	ExportFormats            []string      `default:"[\"json\",\"csv\",\"pdf\"]" yaml:"export_formats"`
	WebSocketTimeout         time.Duration `default:"5m"                         yaml:"websocket_timeout"`
	MaxConcurrentConnections int           `default:"100"                        yaml:"max_concurrent_connections"`
}

// Widget represents a dashboard widget.
type Widget struct {
	ID          string        `json:"id"`
	Type        WidgetType    `json:"type"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Position    Position      `json:"position"`
	Size        Size          `json:"size"`
	Config      WidgetConfig  `json:"config"`
	DataSource  string        `json:"data_source"`
	Query       string        `json:"query"`
	RefreshRate time.Duration `json:"refresh_rate"`
	Visible     bool          `json:"visible"`
	Permissions []string      `json:"permissions"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Data        any           `json:"data,omitempty"`
}

// WidgetType defines types of dashboard widgets.
type WidgetType string

const (
	WidgetTypeChart        WidgetType = "chart"
	WidgetTypeTable        WidgetType = "table"
	WidgetTypeMetric       WidgetType = "metric"
	WidgetTypeGauge        WidgetType = "gauge"
	WidgetTypeHeatmap      WidgetType = "heatmap"
	WidgetTypeTimeline     WidgetType = "timeline"
	WidgetTypeAlertList    WidgetType = "alert_list"
	WidgetTypeHealthStatus WidgetType = "health_status"
	WidgetTypeTopology     WidgetType = "topology"
	WidgetTypeLog          WidgetType = "log"
	WidgetTypeCustom       WidgetType = "custom"
)

// Position defines widget position on dashboard.
type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Size defines widget dimensions.
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WidgetConfig contains widget-specific configuration.
type WidgetConfig struct {
	ChartType   string           `json:"chart_type,omitempty"`  // line, bar, pie, area, scatter
	Aggregation string           `json:"aggregation,omitempty"` // sum, avg, max, min, count
	TimeWindow  string           `json:"time_window,omitempty"` // 1h, 6h, 24h, 7d
	Threshold   *ThresholdConfig `json:"threshold,omitempty"`
	Colors      []string         `json:"colors,omitempty"`
	ShowLegend  bool             `json:"show_legend"`
	ShowGrid    bool             `json:"show_grid"`
	Stacked     bool             `json:"stacked"`
	Normalized  bool             `json:"normalized"`
	MaxRows     int              `json:"max_rows,omitempty"`
	Columns     []ColumnConfig   `json:"columns,omitempty"`
	CustomHTML  string           `json:"custom_html,omitempty"`
	Parameters  map[string]any   `json:"parameters,omitempty"`
}

// ThresholdConfig defines threshold lines for charts.
type ThresholdConfig struct {
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
}

// ColumnConfig defines table column configuration.
type ColumnConfig struct {
	Name       string `json:"name"`
	Field      string `json:"field"`
	Type       string `json:"type"` // string, number, date, boolean
	Width      int    `json:"width"`
	Sortable   bool   `json:"sortable"`
	Filterable bool   `json:"filterable"`
}

// Layout represents a dashboard layout.
type Layout struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Widgets     []string  `json:"widgets"`
	Layout      string    `json:"layout"` // grid, flow, custom
	Columns     int       `json:"columns"`
	IsDefault   bool      `json:"is_default"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CustomQuery represents a custom data query.
type CustomQuery struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Query       string         `json:"query"`
	Parameters  map[string]any `json:"parameters"`
	DataSource  string         `json:"data_source"`
	Schedule    string         `json:"schedule,omitempty"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"created_at"`
}

// RealtimeData manages real-time dashboard data.
type RealtimeData struct {
	connections map[string]*WebSocketConnection
	streams     map[string]*DataStream
	mu          sync.RWMutex
}

// DataStream represents a real-time data stream.
type DataStream struct {
	ID             string                          `json:"id"`
	Type           string                          `json:"type"`
	Source         string                          `json:"source"`
	UpdateInterval time.Duration                   `json:"update_interval"`
	LastUpdate     time.Time                       `json:"last_update"`
	Data           any                             `json:"data"`
	Subscribers    map[string]*WebSocketConnection `json:"-"`
	Active         bool                            `json:"active"`
	mu             sync.RWMutex
}

// WebSocketConnection represents a WebSocket connection for real-time updates.
type WebSocketConnection struct {
	ID            string
	UserID        string
	ConnectedAt   time.Time
	LastPing      time.Time
	Subscriptions []string
	Connection    any // Would be *websocket.Conn in real implementation
}

// DashboardData contains all dashboard data for API responses.
type DashboardData struct {
	Overview        *OverviewData      `json:"overview"`
	SystemMetrics   *SystemMetricsData `json:"system_metrics"`
	AgentMetrics    *AgentMetricsData  `json:"agent_metrics"`
	ModelMetrics    *ModelMetricsData  `json:"model_metrics"`
	HealthStatus    *HealthStatusData  `json:"health_status"`
	AlertsData      *AlertsData        `json:"alerts_data"`
	PerformanceData *PerformanceData   `json:"performance_data"`
	Topology        *TopologyData      `json:"topology"`
	Insights        *InsightsData      `json:"insights"`
	LastUpdated     time.Time          `json:"last_updated"`
}

// Data structures for different dashboard sections

type OverviewData struct {
	TotalAgents    int                `json:"total_agents"`
	ActiveAgents   int                `json:"active_agents"`
	HealthyAgents  int                `json:"healthy_agents"`
	ModelsLoaded   int                `json:"models_loaded"`
	TotalRequests  int64              `json:"total_requests"`
	ErrorRate      float64            `json:"error_rate"`
	AverageLatency time.Duration      `json:"average_latency"`
	SystemHealth   string             `json:"system_health"`
	Uptime         time.Duration      `json:"uptime"`
	LastRestart    time.Time          `json:"last_restart"`
	ResourceUsage  *ResourceUsageData `json:"resource_usage"`
	Alerts         *AlertSummaryData  `json:"alerts"`
}

type ResourceUsageData struct {
	CPU     float64           `json:"cpu"`
	Memory  float64           `json:"memory"`
	Disk    float64           `json:"disk"`
	GPU     float64           `json:"gpu,omitempty"`
	Network *NetworkUsageData `json:"network"`
}

type NetworkUsageData struct {
	BytesIn    int64 `json:"bytes_in"`
	BytesOut   int64 `json:"bytes_out"`
	PacketsIn  int64 `json:"packets_in"`
	PacketsOut int64 `json:"packets_out"`
}

type AlertSummaryData struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
	New      int `json:"new"`
}

type SystemMetricsData struct {
	Timeline    []TimePoint               `json:"timeline"`
	Current     *CurrentMetrics           `json:"current"`
	Trends      map[string]TrendData      `json:"trends"`
	Predictions map[string]PredictionData `json:"predictions"`
	Benchmarks  map[string]float64        `json:"benchmarks"`
}

type TimePoint struct {
	Timestamp time.Time          `json:"timestamp"`
	Metrics   map[string]float64 `json:"metrics"`
}

type CurrentMetrics struct {
	ErrorRate         float64       `json:"error_rate"`
	Latency           time.Duration `json:"latency"`
	Throughput        float64       `json:"throughput"`
	CPUUsage          float64       `json:"cpu_usage"`
	MemoryUsage       float64       `json:"memory_usage"`
	ActiveConnections int           `json:"active_connections"`
}

type TrendData struct {
	Direction  string  `json:"direction"` // up, down, stable
	Change     float64 `json:"change"`    // percentage change
	Confidence float64 `json:"confidence"`
	Period     string  `json:"period"` // 1h, 24h, 7d
}

type PredictionData struct {
	Value      float64   `json:"value"`
	Confidence float64   `json:"confidence"`
	Horizon    string    `json:"horizon"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type AgentMetricsData struct {
	Agents           []AgentSummary `json:"agents"`
	TypeDistribution map[string]int `json:"type_distribution"`
	PerformanceGrid  [][]float64    `json:"performance_grid"`
	TopPerformers    []AgentSummary `json:"top_performers"`
	Issues           []AgentIssue   `json:"issues"`
}

type AgentSummary struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Type           string        `json:"type"`
	Status         string        `json:"status"`
	Health         string        `json:"health"`
	RequestsPerSec float64       `json:"requests_per_sec"`
	ErrorRate      float64       `json:"error_rate"`
	AverageLatency time.Duration `json:"average_latency"`
	Confidence     float64       `json:"confidence"`
	LastActive     time.Time     `json:"last_active"`
}

type AgentIssue struct {
	AgentID     string    `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	Issue       string    `json:"issue"`
	Severity    string    `json:"severity"`
	Description string    `json:"description"`
	FirstSeen   time.Time `json:"first_seen"`
	Count       int       `json:"count"`
}

type ModelMetricsData struct {
	Models           []ModelSummary               `json:"models"`
	TypeDistribution map[string]int               `json:"type_distribution"`
	AccuracyMetrics  map[string]float64           `json:"accuracy_metrics"`
	LatencyMetrics   map[string]time.Duration     `json:"latency_metrics"`
	ResourceUsage    map[string]ResourceUsageData `json:"resource_usage"`
	ModelComparison  []ModelComparison            `json:"model_comparison"`
}

type ModelSummary struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Type             string        `json:"type"`
	Framework        string        `json:"framework"`
	Version          string        `json:"version"`
	Status           string        `json:"status"`
	Accuracy         float64       `json:"accuracy"`
	InferenceLatency time.Duration `json:"inference_latency"`
	ThroughputRPS    float64       `json:"throughput_rps"`
	MemoryUsage      int64         `json:"memory_usage"`
	LoadedAt         time.Time     `json:"loaded_at"`
}

type ModelComparison struct {
	ModelA       string  `json:"model_a"`
	ModelB       string  `json:"model_b"`
	Metric       string  `json:"metric"`
	Difference   float64 `json:"difference"`
	Significance string  `json:"significance"`
}

type HealthStatusData struct {
	OverallStatus   string                     `json:"overall_status"`
	ComponentHealth map[string]ComponentHealth `json:"component_health"`
	HealthTimeline  []HealthTimePoint          `json:"health_timeline"`
	Issues          []HealthIssue              `json:"issues"`
	Recommendations []HealthRecommendation     `json:"recommendations"`
}

type ComponentHealth struct {
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Message     string         `json:"message"`
	LastChecked time.Time      `json:"last_checked"`
	Details     map[string]any `json:"details"`
}

type HealthTimePoint struct {
	Timestamp       time.Time         `json:"timestamp"`
	OverallStatus   string            `json:"overall_status"`
	ComponentStatus map[string]string `json:"component_status"`
}

type HealthIssue struct {
	Component      string    `json:"component"`
	Severity       string    `json:"severity"`
	Message        string    `json:"message"`
	FirstSeen      time.Time `json:"first_seen"`
	Impact         string    `json:"impact"`
	Recommendation string    `json:"recommendation"`
}

type AlertsData struct {
	ActiveAlerts     []AlertSummary       `json:"active_alerts"`
	RecentAlerts     []AlertSummary       `json:"recent_alerts"`
	AlertTimeline    []AlertTimePoint     `json:"alert_timeline"`
	AlertsByCategory map[string]int       `json:"alerts_by_category"`
	AlertTrends      map[string]TrendData `json:"alert_trends"`
}

type AlertSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Severity    string    `json:"severity"`
	Status      string    `json:"status"`
	Component   string    `json:"component"`
	CreatedAt   time.Time `json:"created_at"`
	Count       int       `json:"count"`
	Description string    `json:"description"`
}

type AlertTimePoint struct {
	Timestamp  time.Time      `json:"timestamp"`
	AlertCount int            `json:"alert_count"`
	BySeverity map[string]int `json:"by_severity"`
	ByCategory map[string]int `json:"by_category"`
}

type PerformanceData struct {
	Throughput    *MetricTimeSeries        `json:"throughput"`
	Latency       *MetricTimeSeries        `json:"latency"`
	ErrorRate     *MetricTimeSeries        `json:"error_rate"`
	ResourceUsage *MetricTimeSeries        `json:"resource_usage"`
	SLAMetrics    map[string]SLAData       `json:"sla_metrics"`
	Benchmarks    map[string]BenchmarkData `json:"benchmarks"`
	Anomalies     []AnomalyData            `json:"anomalies"`
}

type MetricTimeSeries struct {
	Name       string            `json:"name"`
	Unit       string            `json:"unit"`
	DataPoints []DataPoint       `json:"data_points"`
	Statistics *MetricStatistics `json:"statistics"`
}

type DataPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags,omitempty"`
}

type MetricStatistics struct {
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
	StdDev float64 `json:"std_dev"`
}

type SLAData struct {
	Target       float64       `json:"target"`
	Current      float64       `json:"current"`
	Status       string        `json:"status"` // "met", "at_risk", "breached"
	TimeToTarget time.Duration `json:"time_to_target,omitempty"`
}

type BenchmarkData struct {
	Name        string    `json:"name"`
	Current     float64   `json:"current"`
	Target      float64   `json:"target"`
	Baseline    float64   `json:"baseline"`
	Status      string    `json:"status"`
	LastUpdated time.Time `json:"last_updated"`
}

type AnomalyData struct {
	ID          string        `json:"id"`
	Metric      string        `json:"metric"`
	Type        string        `json:"type"`
	Severity    string        `json:"severity"`
	Value       float64       `json:"value"`
	Expected    float64       `json:"expected"`
	Deviation   float64       `json:"deviation"`
	DetectedAt  time.Time     `json:"detected_at"`
	Duration    time.Duration `json:"duration"`
	Description string        `json:"description"`
}

type TopologyData struct {
	Nodes    []TopologyNode    `json:"nodes"`
	Edges    []TopologyEdge    `json:"edges"`
	Clusters []TopologyCluster `json:"clusters"`
	Layout   string            `json:"layout"`
	Metadata map[string]any    `json:"metadata"`
}

type TopologyNode struct {
	ID       string             `json:"id"`
	Label    string             `json:"label"`
	Type     string             `json:"type"`
	Status   string             `json:"status"`
	Metrics  map[string]float64 `json:"metrics"`
	Position *Position          `json:"position,omitempty"`
	Size     int                `json:"size"`
	Color    string             `json:"color"`
	Metadata map[string]any     `json:"metadata"`
}

type TopologyEdge struct {
	ID       string         `json:"id"`
	Source   string         `json:"source"`
	Target   string         `json:"target"`
	Type     string         `json:"type"`
	Weight   float64        `json:"weight"`
	Status   string         `json:"status"`
	Metadata map[string]any `json:"metadata"`
}

type TopologyCluster struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Nodes []string `json:"nodes"`
	Color string   `json:"color"`
}

type InsightsData struct {
	KeyInsights         []Insight           `json:"key_insights"`
	Recommendations     []Recommendation    `json:"recommendations"`
	PredictiveAnalytics []PredictiveInsight `json:"predictive_analytics"`
	AnomalyInsights     []AnomalyInsight    `json:"anomaly_insights"`
	OptimizationTips    []OptimizationTip   `json:"optimization_tips"`
	TrendAnalysis       []TrendInsight      `json:"trend_analysis"`
}

type Insight struct {
	ID          string         `json:"id"`
	Category    string         `json:"category"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Impact      string         `json:"impact"`
	Confidence  float64        `json:"confidence"`
	CreatedAt   time.Time      `json:"created_at"`
	Metadata    map[string]any `json:"metadata"`
}

type Recommendation struct {
	ID              string    `json:"id"`
	Priority        string    `json:"priority"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Actions         []string  `json:"actions"`
	ExpectedBenefit string    `json:"expected_benefit"`
	Effort          string    `json:"effort"`
	CreatedAt       time.Time `json:"created_at"`
}

type PredictiveInsight struct {
	Metric      string  `json:"metric"`
	Prediction  float64 `json:"prediction"`
	Confidence  float64 `json:"confidence"`
	Timeframe   string  `json:"timeframe"`
	Trend       string  `json:"trend"`
	RiskLevel   string  `json:"risk_level"`
	Description string  `json:"description"`
}

type AnomalyInsight struct {
	Type        string    `json:"type"`
	Severity    string    `json:"severity"`
	Component   string    `json:"component"`
	Description string    `json:"description"`
	FirstSeen   time.Time `json:"first_seen"`
	Frequency   int       `json:"frequency"`
	Impact      string    `json:"impact"`
}

type OptimizationTip struct {
	Category    string  `json:"category"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Benefit     string  `json:"benefit"`
	Difficulty  string  `json:"difficulty"`
	Priority    float64 `json:"priority"`
}

type TrendInsight struct {
	Metric       string  `json:"metric"`
	Trend        string  `json:"trend"`
	Change       float64 `json:"change"`
	Period       string  `json:"period"`
	Significance string  `json:"significance"`
	Forecast     string  `json:"forecast"`
}

// NewAIDashboard creates a new AI analytics dashboard.
func NewAIDashboard(aiManager ai.AI, healthMonitor *AIHealthMonitor, metricsCollector *AIMetricsCollector, alertManager *AIAlertManager, logger forge.Logger, metrics forge.Metrics, config DashboardConfig) *AIDashboard {
	dashboard := &AIDashboard{
		aiManager:        aiManager,
		healthMonitor:    healthMonitor,
		metricsCollector: metricsCollector,
		alertManager:     alertManager,
		logger:           logger,
		metrics:          metrics,
		config:           config,
		widgets:          make(map[string]*Widget),
		layouts:          make(map[string]*Layout),
		customQueries:    make(map[string]*CustomQuery),
		dataStreams:      make(map[string]*DataStream),
		router:           http.NewServeMux(),
	}

	// Initialize real-time data if enabled
	if config.EnableRealtime {
		dashboard.realtimeData = &RealtimeData{
			connections: make(map[string]*WebSocketConnection),
			streams:     make(map[string]*DataStream),
		}
	}

	// Initialize default widgets and layouts
	dashboard.initializeDefaults()
	dashboard.setupRoutes()

	return dashboard
}

// Start starts the dashboard HTTP server.
func (d *AIDashboard) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return errors.New("AI dashboard already started")
	}

	// Create HTTP server
	addr := fmt.Sprintf(":%d", d.config.Port)
	d.server = &http.Server{
		Addr:    addr,
		Handler: d.router,
	}

	// Start real-time data streams if enabled
	if d.config.EnableRealtime {
		d.startRealtimeStreams(ctx)
	}

	d.started = true

	// Start server in goroutine
	go func() {
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if d.logger != nil {
				d.logger.Error("dashboard server error", logger.Error(err))
			}
		}
	}()

	if d.logger != nil {
		d.logger.Info("AI dashboard started",
			logger.Int("port", d.config.Port),
			logger.Bool("realtime_enabled", d.config.EnableRealtime),
			logger.Bool("authentication_enabled", d.config.EnableAuthentication),
		)
	}

	return nil
}

// Stop stops the dashboard HTTP server.
func (d *AIDashboard) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.started {
		return errors.New("AI dashboard not started")
	}

	// Shutdown HTTP server
	if err := d.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown dashboard server: %w", err)
	}

	// Stop real-time streams
	if d.realtimeData != nil {
		d.stopRealtimeStreams(ctx)
	}

	d.started = false

	if d.logger != nil {
		d.logger.Info("AI dashboard stopped")
	}

	return nil
}

// setupRoutes sets up HTTP routes for the dashboard.
func (d *AIDashboard) setupRoutes() {
	// API routes
	d.router.HandleFunc("/api/dashboard", d.handleDashboardData)
	d.router.HandleFunc("/api/widgets", d.handleWidgets)
	d.router.HandleFunc("/api/layouts", d.handleLayouts)
	d.router.HandleFunc("/api/health", d.handleHealthData)
	d.router.HandleFunc("/api/metrics", d.handleMetricsData)
	d.router.HandleFunc("/api/alerts", d.handleAlertsData)
	d.router.HandleFunc("/api/topology", d.handleTopologyData)
	d.router.HandleFunc("/api/insights", d.handleInsightsData)
	d.router.HandleFunc("/api/export", d.handleDataExport)

	// WebSocket route for real-time updates
	if d.config.EnableRealtime {
		d.router.HandleFunc("/ws", d.handleWebSocket)
	}

	// Static files and UI
	d.router.HandleFunc("/", d.handleDashboardUI)
	d.router.HandleFunc("/static/", d.handleStaticFiles)
}

// handleDashboardData handles dashboard data API requests.
func (d *AIDashboard) handleDashboardData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Collect comprehensive dashboard data
	dashboardData, err := d.collectDashboardData(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to collect dashboard data: %v", err), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(dashboardData); err != nil {
		if d.logger != nil {
			d.logger.Error("failed to encode dashboard data", logger.Error(err))
		}

		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}

	// Record metrics
	if d.metrics != nil {
		d.metrics.Counter("forge.ai.dashboard.data_requests").Inc()
	}
}

// collectDashboardData collects all dashboard data.
func (d *AIDashboard) collectDashboardData(ctx context.Context) (*DashboardData, error) {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	data := &DashboardData{
		LastUpdated: time.Now(),
	}

	// Collect data concurrently
	wg.Add(8)

	// Overview data
	go func() {
		defer wg.Done()

		if overview, err := d.collectOverviewData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("overview data: %w", err))

			mu.Unlock()
		} else {
			data.Overview = overview
		}
	}()

	// System metrics
	go func() {
		defer wg.Done()

		if systemMetrics, err := d.collectSystemMetricsData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("system metrics: %w", err))

			mu.Unlock()
		} else {
			data.SystemMetrics = systemMetrics
		}
	}()

	// Agent metrics
	go func() {
		defer wg.Done()

		if agentMetrics, err := d.collectAgentMetricsData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("agent metrics: %w", err))

			mu.Unlock()
		} else {
			data.AgentMetrics = agentMetrics
		}
	}()

	// Model metrics
	go func() {
		defer wg.Done()

		if modelMetrics, err := d.collectModelMetricsData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("model metrics: %w", err))

			mu.Unlock()
		} else {
			data.ModelMetrics = modelMetrics
		}
	}()

	// Health status
	go func() {
		defer wg.Done()

		if healthStatus, err := d.collectHealthStatusData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("health status: %w", err))

			mu.Unlock()
		} else {
			data.HealthStatus = healthStatus
		}
	}()

	// Alerts data
	go func() {
		defer wg.Done()

		if alertsData, err := d.collectAlertsData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("alerts data: %w", err))

			mu.Unlock()
		} else {
			data.AlertsData = alertsData
		}
	}()

	// Performance data
	go func() {
		defer wg.Done()

		if performanceData, err := d.collectPerformanceData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("performance data: %w", err))

			mu.Unlock()
		} else {
			data.PerformanceData = performanceData
		}
	}()

	// Insights
	go func() {
		defer wg.Done()

		if insights, err := d.collectInsightsData(ctx); err != nil {
			mu.Lock()

			errs = append(errs, fmt.Errorf("insights: %w", err))

			mu.Unlock()
		} else {
			data.Insights = insights
		}
	}()

	wg.Wait()

	// Return first error if any
	if len(errs) > 0 {
		return nil, errs[0]
	}

	return data, nil
}

// Data collection methods - these would be fully implemented

func (d *AIDashboard) collectOverviewData(ctx context.Context) (*OverviewData, error) {
	stats := d.aiManager.GetStats()

	// Get health data
	var healthStatus = "unknown"

	if d.healthMonitor != nil {
		if healthReport, err := d.healthMonitor.GetHealthReport(ctx); err == nil {
			healthStatus = string(healthReport.OverallStatus)
		}
	}

	overview := &OverviewData{
		TotalAgents:    getIntFromMap(stats, "TotalAgents"),
		ActiveAgents:   getIntFromMap(stats, "ActiveAgents"),
		HealthyAgents:  getIntFromMap(stats, "ActiveAgents"), // Simplified
		ModelsLoaded:   getIntFromMap(stats, "ModelsLoaded"),
		TotalRequests:  getInt64FromMap(stats, "InferenceRequests"),
		ErrorRate:      getFloat64FromMap(stats, "ErrorRate"),
		AverageLatency: getDurationFromMap(stats, "AverageLatency"),
		SystemHealth:   healthStatus,
		Uptime:         time.Since(time.Now().Add(-24 * time.Hour)), // Placeholder
		LastRestart:    time.Now().Add(-24 * time.Hour),             // Placeholder
		ResourceUsage: &ResourceUsageData{
			CPU:    50.0, // Would be collected from system metrics
			Memory: 60.0,
			Disk:   30.0,
			Network: &NetworkUsageData{
				BytesIn:    1000000,
				BytesOut:   2000000,
				PacketsIn:  10000,
				PacketsOut: 15000,
			},
		},
		Alerts: &AlertSummaryData{
			Total:    10, // Would be collected from alert manager
			Critical: 1,
			Warning:  4,
			Info:     5,
			New:      3,
		},
	}

	return overview, nil
}

func (d *AIDashboard) collectSystemMetricsData(ctx context.Context) (*SystemMetricsData, error) {
	if d.metricsCollector == nil {
		return &SystemMetricsData{}, nil
	}

	metricsReport, err := d.metricsCollector.GetMetricsReport(ctx)
	if err != nil {
		return nil, err
	}

	// Create timeline data from historical metrics
	timeline := make([]TimePoint, 0)

	if metricsReport.HistoricalData != nil {
		for _, point := range metricsReport.HistoricalData.DataPoints {
			timeline = append(timeline, TimePoint{
				Timestamp: point.Timestamp,
				Metrics: map[string]float64{
					"error_rate":      point.ErrorRate,
					"average_latency": point.AverageLatency,
					"throughput":      point.Throughput,
					"cpu_usage":       point.CPUUsage,
					"memory_usage":    point.MemoryUsage,
					"active_agents":   float64(point.ActiveAgents),
				},
			})
		}
	}

	systemMetrics := &SystemMetricsData{
		Timeline: timeline,
		Current: &CurrentMetrics{
			ErrorRate:         metricsReport.SystemMetrics.OverallErrorRate,
			Latency:           metricsReport.SystemMetrics.OverallLatency,
			Throughput:        metricsReport.SystemMetrics.OverallThroughput,
			CPUUsage:          metricsReport.SystemMetrics.SystemCPUUsage,
			MemoryUsage:       metricsReport.SystemMetrics.SystemMemoryUsage,
			ActiveConnections: metricsReport.SystemMetrics.ActiveAgents,
		},
		Trends:      make(map[string]TrendData),
		Predictions: make(map[string]PredictionData),
		Benchmarks: map[string]float64{
			"target_error_rate": 0.01,
			"target_latency":    1.0,
			"target_throughput": 1000.0,
		},
	}

	return systemMetrics, nil
}

func (d *AIDashboard) collectAgentMetricsData(ctx context.Context) (*AgentMetricsData, error) {
	agents := d.aiManager.GetAgents()
	agentSummaries := make([]AgentSummary, 0, len(agents))
	typeDistribution := make(map[string]int)
	issues := make([]AgentIssue, 0)

	for _, agent := range agents {
		stats := agent.GetStats()
		health := agent.GetHealth()

		summary := AgentSummary{
			ID:             agent.ID(),
			Name:           agent.Name(),
			Type:           string(agent.Type()),
			Status:         "active", // Simplified
			Health:         string(health.Status),
			RequestsPerSec: float64(stats.TotalProcessed) / 3600, // Simplified calculation
			ErrorRate:      stats.ErrorRate,
			AverageLatency: stats.AverageLatency,
			Confidence:     stats.Confidence,
			LastActive:     time.Now(), // Would track actual last activity
		}

		agentSummaries = append(agentSummaries, summary)
		typeDistribution[string(agent.Type())]++

		// Check for issues
		if health.Status != ai.AgentHealthStatusHealthy {
			issues = append(issues, AgentIssue{
				AgentID:     agent.ID(),
				AgentName:   agent.Name(),
				Issue:       health.Message,
				Severity:    string(health.Status),
				Description: health.Message,
				FirstSeen:   health.CheckedAt,
				Count:       1,
			})
		}
	}

	// Sort agents by performance
	sort.Slice(agentSummaries, func(i, j int) bool {
		return agentSummaries[i].RequestsPerSec > agentSummaries[j].RequestsPerSec
	})

	topPerformers := agentSummaries
	if len(topPerformers) > 10 {
		topPerformers = topPerformers[:10]
	}

	return &AgentMetricsData{
		Agents:           agentSummaries,
		TypeDistribution: typeDistribution,
		TopPerformers:    topPerformers,
		Issues:           issues,
	}, nil
}

// Additional collection methods would be implemented here...
// collectModelMetricsData, collectHealthStatusData, collectAlertsData,
// collectPerformanceData, collectInsightsData

// Helper methods for widget and layout management.
func (d *AIDashboard) initializeDefaults() {
	// Create default widgets
	defaultWidgets := []*Widget{
		{
			ID:          "system_overview",
			Type:        WidgetTypeMetric,
			Title:       "System Overview",
			Description: "High-level system metrics",
			Position:    Position{X: 0, Y: 0},
			Size:        Size{Width: 3, Height: 2},
			DataSource:  "overview",
			Visible:     true,
			RefreshRate: 30 * time.Second,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "error_rate_chart",
			Type:        WidgetTypeChart,
			Title:       "Error Rate",
			Description: "System error rate over time",
			Position:    Position{X: 3, Y: 0},
			Size:        Size{Width: 6, Height: 3},
			Config: WidgetConfig{
				ChartType:  "line",
				TimeWindow: "24h",
				ShowLegend: true,
				ShowGrid:   true,
			},
			DataSource:  "metrics",
			Query:       "error_rate",
			Visible:     true,
			RefreshRate: 60 * time.Second,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "agent_health",
			Type:        WidgetTypeTable,
			Title:       "Agent Health",
			Description: "Current status of all agents",
			Position:    Position{X: 9, Y: 0},
			Size:        Size{Width: 3, Height: 4},
			Config: WidgetConfig{
				MaxRows: 20,
				Columns: []ColumnConfig{
					{Name: "Name", Field: "name", Type: "string", Sortable: true},
					{Name: "Status", Field: "status", Type: "string"},
					{Name: "Health", Field: "health", Type: "string"},
					{Name: "Error Rate", Field: "error_rate", Type: "number"},
				},
			},
			DataSource:  "agents",
			Visible:     true,
			RefreshRate: 30 * time.Second,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "active_alerts",
			Type:        WidgetTypeAlertList,
			Title:       "Active Alerts",
			Description: "Current active alerts",
			Position:    Position{X: 0, Y: 3},
			Size:        Size{Width: 6, Height: 3},
			DataSource:  "alerts",
			Query:       "status:active",
			Visible:     true,
			RefreshRate: 15 * time.Second,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	for _, widget := range defaultWidgets {
		d.widgets[widget.ID] = widget
	}

	// Create default layout
	defaultLayout := &Layout{
		ID:          "default",
		Name:        "Default Dashboard",
		Description: "Default AI monitoring dashboard layout",
		Widgets:     []string{"system_overview", "error_rate_chart", "agent_health", "active_alerts"},
		Layout:      "grid",
		Columns:     12,
		IsDefault:   true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	d.layouts["default"] = defaultLayout
}

// Additional handler methods would be implemented here...
// handleWidgets, handleLayouts, handleHealthData, handleMetricsData,
// handleAlertsData, handleTopologyData, handleInsightsData,
// handleDataExport, handleWebSocket, handleDashboardUI, handleStaticFiles

// Placeholder methods for handlers.
func (d *AIDashboard) handleWidgets(w http.ResponseWriter, r *http.Request) {
	// Implementation for widget management API
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d.widgets)
}

func (d *AIDashboard) handleLayouts(w http.ResponseWriter, r *http.Request) {
	// Implementation for layout management API
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d.layouts)
}

func (d *AIDashboard) handleDashboardUI(w http.ResponseWriter, r *http.Request) {
	// Serve dashboard HTML UI
	html := d.generateDashboardHTML()

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func (d *AIDashboard) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	// Serve static files (CSS, JS, images)
	http.NotFound(w, r) // Placeholder
}

func (d *AIDashboard) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Handle WebSocket connections for real-time updates
	// Implementation would use gorilla/websocket or similar
}

func (d *AIDashboard) generateDashboardHTML() string {
	// Generate HTML for dashboard UI
	return `
<!DOCTYPE html>
<html>
<head>
    <title>AI Analytics Dashboard</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .header { background: #2c3e50; color: white; padding: 20px; margin: -20px -20px 20px; }
        .widget { background: white; padding: 20px; margin: 10px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; }
        .metric { font-size: 2em; font-weight: bold; color: #3498db; }
        .status-healthy { color: #27ae60; }
        .status-warning { color: #f39c12; }
        .status-critical { color: #e74c3c; }
    </style>
</head>
<body>
    <div class="header">
        <h1>AI Analytics Dashboard</h1>
        <p>Real-time monitoring and analytics for AI systems</p>
    </div>
    
    <div class="grid">
        <div class="widget">
            <h3>System Status</h3>
            <div id="system-status">Loading...</div>
        </div>
        
        <div class="widget">
            <h3>Active Agents</h3>
            <div id="agent-count" class="metric">-</div>
        </div>
        
        <div class="widget">
            <h3>Error Rate</h3>
            <div id="error-rate" class="metric">-</div>
        </div>
        
        <div class="widget">
            <h3>Average Latency</h3>
            <div id="latency" class="metric">-</div>
        </div>
        
        <div class="widget">
            <h3>Recent Alerts</h3>
            <div id="alerts">Loading...</div>
        </div>
        
        <div class="widget">
            <h3>Performance Chart</h3>
            <div id="performance-chart" style="height: 200px; background: #ecf0f1; display: flex; align-items: center; justify-content: center;">
                Chart would be rendered here
            </div>
        </div>
    </div>

    <script>
        // Dashboard JavaScript would be included here
        // This would handle real-time updates, chart rendering, etc.
        
        function updateDashboard() {
            fetch('/api/dashboard')
                .then(response => response.json())
                .then(data => {
                    document.getElementById('agent-count').textContent = data.overview.active_agents;
                    document.getElementById('error-rate').textContent = (data.overview.error_rate * 100).toFixed(2) + '%';
                    document.getElementById('latency').textContent = Math.round(data.overview.average_latency / 1000000) + 'ms';
                    
                    // Update system status
                    const statusElement = document.getElementById('system-status');
                    statusElement.textContent = data.overview.system_health;
                    statusElement.className = 'status-' + data.overview.system_health;
                })
                .catch(error => console.error('Error updating dashboard:', error));
        }
        
        // Update dashboard every 30 seconds
        updateDashboard();
        setInterval(updateDashboard, 30000);
    </script>
</body>
</html>`
}

// Real-time streaming methods.
func (d *AIDashboard) startRealtimeStreams(ctx context.Context) {
	// Start data streams for real-time updates
	streams := []string{"system_metrics", "agent_status", "alerts", "health_status"}

	for _, streamName := range streams {
		stream := &DataStream{
			ID:             streamName,
			Type:           "metrics",
			Source:         streamName,
			UpdateInterval: d.config.RefreshInterval,
			Active:         true,
			Subscribers:    make(map[string]*WebSocketConnection),
		}

		d.dataStreams[streamName] = stream

		// Start stream processor
		go d.processDataStream(ctx, stream)
	}
}

func (d *AIDashboard) stopRealtimeStreams(ctx context.Context) {
	for _, stream := range d.dataStreams {
		stream.Active = false
	}
}

func (d *AIDashboard) processDataStream(ctx context.Context, stream *DataStream) {
	ticker := time.NewTicker(stream.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !stream.Active {
				return
			}

			// Update stream data and notify subscribers
			d.updateStreamData(stream)
			d.notifyStreamSubscribers(stream)
		}
	}
}

func (d *AIDashboard) updateStreamData(stream *DataStream) {
	// Update stream with latest data based on stream type
	switch stream.ID {
	case "system_metrics":
		if d.metricsCollector != nil {
			if report, err := d.metricsCollector.GetMetricsReport(context.Background()); err == nil {
				stream.Data = report.SystemMetrics
				stream.LastUpdate = time.Now()
			}
		}
	case "agent_status":
		agents := d.aiManager.GetAgents()

		agentData := make([]map[string]any, 0, len(agents))
		for _, agent := range agents {
			stats := agent.GetStats()
			health := agent.GetHealth()
			agentData = append(agentData, map[string]any{
				"id":         agent.ID(),
				"name":       agent.Name(),
				"type":       string(agent.Type()),
				"status":     string(health.Status),
				"error_rate": stats.ErrorRate,
				"latency":    stats.AverageLatency,
				"confidence": stats.Confidence,
			})
		}

		stream.Data = agentData
		stream.LastUpdate = time.Now()
	}
}

func (d *AIDashboard) notifyStreamSubscribers(stream *DataStream) {
	stream.mu.RLock()
	defer stream.mu.RUnlock()

	// Notify all subscribers about data update
	for _, conn := range stream.Subscribers {
		// Send update to WebSocket connection
		// Implementation would depend on WebSocket library used
		_ = conn // Placeholder
	}
}

// Placeholder implementations for missing methods.
func (d *AIDashboard) collectModelMetricsData(ctx context.Context) (*ModelMetricsData, error) {
	return &ModelMetricsData{}, nil
}

func (d *AIDashboard) collectHealthStatusData(ctx context.Context) (*HealthStatusData, error) {
	return &HealthStatusData{}, nil
}

func (d *AIDashboard) collectAlertsData(ctx context.Context) (*AlertsData, error) {
	return &AlertsData{}, nil
}

func (d *AIDashboard) collectPerformanceData(ctx context.Context) (*PerformanceData, error) {
	return &PerformanceData{}, nil
}

func (d *AIDashboard) collectInsightsData(ctx context.Context) (*InsightsData, error) {
	return &InsightsData{}, nil
}

// handleHealthData handles health data requests.
func (d *AIDashboard) handleHealthData(w http.ResponseWriter, r *http.Request) {
	// Implementation for handling health data requests
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "healthy"}`))
}

// handleMetricsData handles metrics data requests.
func (d *AIDashboard) handleMetricsData(w http.ResponseWriter, r *http.Request) {
	// Implementation for handling metrics data requests
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"metrics": []}`))
}

// handleAlertsData handles alerts data requests.
func (d *AIDashboard) handleAlertsData(w http.ResponseWriter, r *http.Request) {
	// Implementation for handling alerts data requests
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"alerts": []}`))
}

// handleTopologyData handles topology data requests.
func (d *AIDashboard) handleTopologyData(w http.ResponseWriter, r *http.Request) {
	// Implementation for handling topology data requests
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"topology": {}}`))
}

// handleInsightsData handles insights data requests.
func (d *AIDashboard) handleInsightsData(w http.ResponseWriter, r *http.Request) {
	// Implementation for handling insights data requests
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"insights": []}`))
}

// handleDataExport handles data export requests.
func (d *AIDashboard) handleDataExport(w http.ResponseWriter, r *http.Request) {
	// Implementation for handling data export requests
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"export": "data"}`))
}

// Helper functions for map access.
func getIntFromMap(data map[string]any, key string) int {
	if val, ok := data[key].(int); ok {
		return val
	}

	return 0
}

func getInt64FromMap(data map[string]any, key string) int64 {
	if val, ok := data[key].(int64); ok {
		return val
	}

	return 0
}

func getFloat64FromMap(data map[string]any, key string) float64 {
	if val, ok := data[key].(float64); ok {
		return val
	}

	return 0.0
}

func getDurationFromMap(data map[string]any, key string) time.Duration {
	if val, ok := data[key].(time.Duration); ok {
		return val
	}

	return 0
}
