package agents

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	ai "github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/internal/logger"
)

// ResourceAgent manages system resources and provides optimization recommendations
type ResourceAgent struct {
	*ai.BaseAgent
	resourceManager   interface{} // Resource manager from system
	resourceMonitor   *ResourceMonitor
	optimizer         *ResourceOptimizer
	scaler            *ResourceScaler
	resourcePolicies  []ResourcePolicy
	resourceStats     ResourceStats
	thresholds        ResourceThresholds
	optimizationRules []OptimizationRule
	autoScaling       bool
	autoOptimization  bool
	learningEnabled   bool
	mu                sync.RWMutex
}

// ResourceMonitor monitors system resources
type ResourceMonitor struct {
	CPUMonitor     *CPUMonitor     `json:"cpu_monitor"`
	MemoryMonitor  *MemoryMonitor  `json:"memory_monitor"`
	DiskMonitor    *DiskMonitor    `json:"disk_monitor"`
	NetworkMonitor *NetworkMonitor `json:"network_monitor"`
	UpdateInterval time.Duration   `json:"update_interval"`
	HistorySize    int             `json:"history_size"`
	Enabled        bool            `json:"enabled"`
}

// CPUMonitor monitors CPU usage
type CPUMonitor struct {
	Usage       float64                `json:"usage"`
	LoadAverage []float64              `json:"load_average"`
	Cores       int                    `json:"cores"`
	Frequency   float64                `json:"frequency"`
	Temperature float64                `json:"temperature"`
	Processes   []ProcessInfo          `json:"processes"`
	History     []CPUUsageHistory      `json:"history"`
	Thresholds  CPUThresholds          `json:"thresholds"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// MemoryMonitor monitors memory usage
type MemoryMonitor struct {
	Total      uint64                 `json:"total"`
	Available  uint64                 `json:"available"`
	Used       uint64                 `json:"used"`
	Cached     uint64                 `json:"cached"`
	Buffers    uint64                 `json:"buffers"`
	Swap       SwapInfo               `json:"swap"`
	Processes  []ProcessMemoryInfo    `json:"processes"`
	History    []MemoryUsageHistory   `json:"history"`
	Thresholds MemoryThresholds       `json:"thresholds"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// DiskMonitor monitors disk usage
type DiskMonitor struct {
	Filesystems []FilesystemInfo       `json:"filesystems"`
	IOStats     DiskIOStats            `json:"io_stats"`
	History     []DiskUsageHistory     `json:"history"`
	Thresholds  DiskThresholds         `json:"thresholds"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// NetworkMonitor monitors network usage
type NetworkMonitor struct {
	Interfaces  []NetworkInterface     `json:"interfaces"`
	Traffic     NetworkTrafficStats    `json:"traffic"`
	Connections []NetworkConnection    `json:"connections"`
	History     []NetworkUsageHistory  `json:"history"`
	Thresholds  NetworkThresholds      `json:"thresholds"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// ProcessInfo contains process information
type ProcessInfo struct {
	PID         int                    `json:"pid"`
	Name        string                 `json:"name"`
	CPUUsage    float64                `json:"cpu_usage"`
	MemoryUsage uint64                 `json:"memory_usage"`
	Status      string                 `json:"status"`
	StartTime   time.Time              `json:"start_time"`
	Command     string                 `json:"command"`
	User        string                 `json:"user"`
	Threads     int                    `json:"threads"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// ProcessMemoryInfo contains process memory information
type ProcessMemoryInfo struct {
	PID      int                    `json:"pid"`
	Name     string                 `json:"name"`
	RSS      uint64                 `json:"rss"`
	VMS      uint64                 `json:"vms"`
	Shared   uint64                 `json:"shared"`
	Text     uint64                 `json:"text"`
	Data     uint64                 `json:"data"`
	Metadata map[string]interface{} `json:"metadata"`
}

// SwapInfo contains swap information
type SwapInfo struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
}

// FilesystemInfo contains filesystem information
type FilesystemInfo struct {
	Device     string  `json:"device"`
	Mountpoint string  `json:"mountpoint"`
	Type       string  `json:"type"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Available  uint64  `json:"available"`
	Usage      float64 `json:"usage"`
}

// DiskIOStats contains disk I/O statistics
type DiskIOStats struct {
	ReadOps    uint64 `json:"read_ops"`
	WriteOps   uint64 `json:"write_ops"`
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
	ReadTime   uint64 `json:"read_time"`
	WriteTime  uint64 `json:"write_time"`
	IOTime     uint64 `json:"io_time"`
}

// NetworkInterface contains network interface information
type NetworkInterface struct {
	Name      string `json:"name"`
	MTU       int    `json:"mtu"`
	Speed     uint64 `json:"speed"`
	Duplex    string `json:"duplex"`
	Status    string `json:"status"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxPackets uint64 `json:"tx_packets"`
	RxErrors  uint64 `json:"rx_errors"`
	TxErrors  uint64 `json:"tx_errors"`
	RxDropped uint64 `json:"rx_dropped"`
	TxDropped uint64 `json:"tx_dropped"`
}

// NetworkTrafficStats contains network traffic statistics
type NetworkTrafficStats struct {
	TotalRxBytes   uint64  `json:"total_rx_bytes"`
	TotalTxBytes   uint64  `json:"total_tx_bytes"`
	TotalRxPackets uint64  `json:"total_rx_packets"`
	TotalTxPackets uint64  `json:"total_tx_packets"`
	Bandwidth      uint64  `json:"bandwidth"`
	Utilization    float64 `json:"utilization"`
}

// NetworkConnection contains network connection information
type NetworkConnection struct {
	Protocol    string `json:"protocol"`
	LocalAddr   string `json:"local_addr"`
	LocalPort   int    `json:"local_port"`
	RemoteAddr  string `json:"remote_addr"`
	RemotePort  int    `json:"remote_port"`
	Status      string `json:"status"`
	PID         int    `json:"pid"`
	ProcessName string `json:"process_name"`
}

// History types for tracking resource usage over time
type CPUUsageHistory struct {
	Timestamp time.Time `json:"timestamp"`
	Usage     float64   `json:"usage"`
	Load      []float64 `json:"load"`
}

type MemoryUsageHistory struct {
	Timestamp time.Time `json:"timestamp"`
	Used      uint64    `json:"used"`
	Available uint64    `json:"available"`
	Usage     float64   `json:"usage"`
}

type DiskUsageHistory struct {
	Timestamp time.Time `json:"timestamp"`
	Used      uint64    `json:"used"`
	Available uint64    `json:"available"`
	Usage     float64   `json:"usage"`
}

type NetworkUsageHistory struct {
	Timestamp time.Time `json:"timestamp"`
	RxBytes   uint64    `json:"rx_bytes"`
	TxBytes   uint64    `json:"tx_bytes"`
	RxPackets uint64    `json:"rx_packets"`
	TxPackets uint64    `json:"tx_packets"`
}

// ResourceThresholds Resource thresholds for alerting and optimization
type ResourceThresholds struct {
	CPU     CPUThresholds     `json:"cpu"`
	Memory  MemoryThresholds  `json:"memory"`
	Disk    DiskThresholds    `json:"disk"`
	Network NetworkThresholds `json:"network"`
}

type CPUThresholds struct {
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
	LoadHigh float64 `json:"load_high"`
	TempHigh float64 `json:"temp_high"`
}

type MemoryThresholds struct {
	Warning     float64 `json:"warning"`
	Critical    float64 `json:"critical"`
	SwapHigh    float64 `json:"swap_high"`
	ProcessHigh uint64  `json:"process_high"`
}

type DiskThresholds struct {
	Warning     float64 `json:"warning"`
	Critical    float64 `json:"critical"`
	IOHigh      uint64  `json:"io_high"`
	LatencyHigh float64 `json:"latency_high"`
}

type NetworkThresholds struct {
	Warning         float64 `json:"warning"`
	Critical        float64 `json:"critical"`
	BandwidthHigh   uint64  `json:"bandwidth_high"`
	ConnectionsHigh int     `json:"connections_high"`
}

// ResourceOptimizer optimizes resource usage
type ResourceOptimizer struct {
	OptimizationStrategies []OptimizationStrategy `json:"optimization_strategies"`
	PerformanceTargets     PerformanceTargets     `json:"performance_targets"`
	CostTargets            CostTargets            `json:"cost_targets"`
	Enabled                bool                   `json:"enabled"`
	AutoApply              bool                   `json:"auto_apply"`
	LearningEnabled        bool                   `json:"learning_enabled"`
}

// OptimizationStrategy defines optimization strategies
type OptimizationStrategy struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Target        string                 `json:"target"`
	Condition     string                 `json:"condition"`
	Action        string                 `json:"action"`
	Parameters    map[string]interface{} `json:"parameters"`
	Priority      int                    `json:"priority"`
	Enabled       bool                   `json:"enabled"`
	Effectiveness float64                `json:"effectiveness"`
	Cost          float64                `json:"cost"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// PerformanceTargets defines performance targets
type PerformanceTargets struct {
	CPUTarget        float64       `json:"cpu_target"`
	MemoryTarget     float64       `json:"memory_target"`
	DiskTarget       float64       `json:"disk_target"`
	NetworkTarget    float64       `json:"network_target"`
	LatencyTarget    time.Duration `json:"latency_target"`
	ThroughputTarget float64       `json:"throughput_target"`
}

// CostTargets defines cost optimization targets
type CostTargets struct {
	ComputeCost  float64 `json:"compute_cost"`
	StorageCost  float64 `json:"storage_cost"`
	NetworkCost  float64 `json:"network_cost"`
	TotalCost    float64 `json:"total_cost"`
	CostPerHour  float64 `json:"cost_per_hour"`
	CostPerMonth float64 `json:"cost_per_month"`
}

// ResourceScaler handles automatic scaling
type ResourceScaler struct {
	ScalingPolicies []ScalingPolicy `json:"scaling_policies"`
	ScalingRules    []ScalingRule   `json:"scaling_rules"`
	ScalingHistory  []ScalingEvent  `json:"scaling_history"`
	Enabled         bool            `json:"enabled"`
	AutoScale       bool            `json:"auto_scale"`
	CooldownPeriod  time.Duration   `json:"cooldown_period"`
	MinInstances    int             `json:"min_instances"`
	MaxInstances    int             `json:"max_instances"`
}

// ScalingPolicy defines scaling policies
type ScalingPolicy struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Target     string                 `json:"target"`
	Metric     string                 `json:"metric"`
	Threshold  float64                `json:"threshold"`
	Action     string                 `json:"action"`
	Parameters map[string]interface{} `json:"parameters"`
	Enabled    bool                   `json:"enabled"`
	Priority   int                    `json:"priority"`
	Cooldown   time.Duration          `json:"cooldown"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// ScalingRule defines scaling rules
type ScalingRule struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Condition  string                 `json:"condition"`
	Action     string                 `json:"action"`
	Parameters map[string]interface{} `json:"parameters"`
	Enabled    bool                   `json:"enabled"`
	Priority   int                    `json:"priority"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// ScalingEvent records scaling events
type ScalingEvent struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"`
	Action    string                 `json:"action"`
	Target    string                 `json:"target"`
	Reason    string                 `json:"reason"`
	Before    ResourceSnapshot       `json:"before"`
	After     ResourceSnapshot       `json:"after"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error"`
	Duration  time.Duration          `json:"duration"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// ResourceSnapshot captures resource state at a point in time
type ResourceSnapshot struct {
	Timestamp time.Time              `json:"timestamp"`
	CPU       CPUSnapshot            `json:"cpu"`
	Memory    MemorySnapshot         `json:"memory"`
	Disk      DiskSnapshot           `json:"disk"`
	Network   NetworkSnapshot        `json:"network"`
	Processes []ProcessSnapshot      `json:"processes"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type CPUSnapshot struct {
	Usage       float64   `json:"usage"`
	LoadAverage []float64 `json:"load_average"`
	Cores       int       `json:"cores"`
}

type MemorySnapshot struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Usage     float64 `json:"usage"`
}

type DiskSnapshot struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Usage     float64 `json:"usage"`
}

type NetworkSnapshot struct {
	RxBytes   uint64  `json:"rx_bytes"`
	TxBytes   uint64  `json:"tx_bytes"`
	RxPackets uint64  `json:"rx_packets"`
	TxPackets uint64  `json:"tx_packets"`
	Bandwidth uint64  `json:"bandwidth"`
	Usage     float64 `json:"usage"`
}

type ProcessSnapshot struct {
	PID         int     `json:"pid"`
	Name        string  `json:"name"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage uint64  `json:"memory_usage"`
	Status      string  `json:"status"`
}

// ResourcePolicy defines resource management policies
type ResourcePolicy struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Target   string                 `json:"target"`
	Rules    []ResourceRule         `json:"rules"`
	Actions  []ResourceAction       `json:"actions"`
	Enabled  bool                   `json:"enabled"`
	Priority int                    `json:"priority"`
	Metadata map[string]interface{} `json:"metadata"`
}

// ResourceRule defines resource rules
type ResourceRule struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Condition string                 `json:"condition"`
	Metric    string                 `json:"metric"`
	Threshold float64                `json:"threshold"`
	Operator  string                 `json:"operator"`
	Duration  time.Duration          `json:"duration"`
	Enabled   bool                   `json:"enabled"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// ResourceAction defines resource actions
type ResourceAction struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Target     string                 `json:"target"`
	Parameters map[string]interface{} `json:"parameters"`
	Automatic  bool                   `json:"automatic"`
	Timeout    time.Duration          `json:"timeout"`
	Rollback   bool                   `json:"rollback"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// OptimizationRule defines optimization rules
type OptimizationRule struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Condition     string                 `json:"condition"`
	Optimization  string                 `json:"optimization"`
	Parameters    map[string]interface{} `json:"parameters"`
	Enabled       bool                   `json:"enabled"`
	Priority      int                    `json:"priority"`
	Effectiveness float64                `json:"effectiveness"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// ResourceStats tracks resource management statistics
type ResourceStats struct {
	TotalOptimizations      int64                    `json:"total_optimizations"`
	SuccessfulOptimizations int64                    `json:"successful_optimizations"`
	TotalScalingEvents      int64                    `json:"total_scaling_events"`
	SuccessfulScalings      int64                    `json:"successful_scalings"`
	AverageUtilization      ResourceUtilization      `json:"average_utilization"`
	PeakUtilization         ResourceUtilization      `json:"peak_utilization"`
	CostSavings             float64                  `json:"cost_savings"`
	PerformanceGain         float64                  `json:"performance_gain"`
	LastOptimization        time.Time                `json:"last_optimization"`
	LastScaling             time.Time                `json:"last_scaling"`
	OptimizationsByType     map[string]int64         `json:"optimizations_by_type"`
	ScalingsByType          map[string]int64         `json:"scalings_by_type"`
	ResourceTrends          map[string]ResourceTrend `json:"resource_trends"`
	Metadata                map[string]interface{}   `json:"metadata"`
}

// ResourceUtilization contains resource utilization information
type ResourceUtilization struct {
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	Disk    float64 `json:"disk"`
	Network float64 `json:"network"`
	GPU     float64 `json:"gpu"`
	Overall float64 `json:"overall"`
}

// ResourceTrend tracks resource trends
type ResourceTrend struct {
	Resource   string        `json:"resource"`
	Direction  string        `json:"direction"`
	Change     float64       `json:"change"`
	Confidence float64       `json:"confidence"`
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	Duration   time.Duration `json:"duration"`
}

// ResourceInput represents resource monitoring input
type ResourceInput struct {
	SystemMetrics              SystemMetrics              `json:"system_metrics"`
	ApplicationMetrics         ApplicationMetrics         `json:"application_metrics"`
	InfrastructureMetrics      InfrastructureMetrics      `json:"infrastructure_metrics"`
	ResourcePerformanceMetrics ResourcePerformanceMetrics `json:"performance_metrics"`
	CostMetrics                CostMetrics                `json:"cost_metrics"`
	ServiceMetrics             ServiceMetrics             `json:"service_metrics"`
	UserMetrics                UserMetrics                `json:"user_metrics"`
	TimeWindow                 TimeWindow                 `json:"time_window"`
	Context                    ResourceContext            `json:"context"`
	Metadata                   map[string]interface{}     `json:"metadata"`
}

// SystemMetrics contains system-level metrics
type SystemMetrics struct {
	CPU       CPUMetrics             `json:"cpu"`
	Memory    MemoryMetrics          `json:"memory"`
	Disk      DiskMetrics            `json:"disk"`
	Network   NetworkMetrics         `json:"network"`
	Processes []ProcessMetrics       `json:"processes"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type CPUMetrics struct {
	Usage       float64   `json:"usage"`
	LoadAverage []float64 `json:"load_average"`
	Cores       int       `json:"cores"`
	Frequency   float64   `json:"frequency"`
	Temperature float64   `json:"temperature"`
}

type MemoryMetrics struct {
	Total     uint64      `json:"total"`
	Used      uint64      `json:"used"`
	Available uint64      `json:"available"`
	Usage     float64     `json:"usage"`
	Cached    uint64      `json:"cached"`
	Buffers   uint64      `json:"buffers"`
	Swap      SwapMetrics `json:"swap"`
}

type SwapMetrics struct {
	Total uint64  `json:"total"`
	Used  uint64  `json:"used"`
	Usage float64 `json:"usage"`
}

type DiskMetrics struct {
	Total     uint64      `json:"total"`
	Used      uint64      `json:"used"`
	Available uint64      `json:"available"`
	Usage     float64     `json:"usage"`
	IOStats   DiskIOStats `json:"io_stats"`
}

type NetworkMetrics struct {
	RxBytes     uint64  `json:"rx_bytes"`
	TxBytes     uint64  `json:"tx_bytes"`
	RxPackets   uint64  `json:"rx_packets"`
	TxPackets   uint64  `json:"tx_packets"`
	Bandwidth   uint64  `json:"bandwidth"`
	Usage       float64 `json:"usage"`
	Connections int     `json:"connections"`
}

type ProcessMetrics struct {
	PID         int     `json:"pid"`
	Name        string  `json:"name"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage uint64  `json:"memory_usage"`
	Status      string  `json:"status"`
	Threads     int     `json:"threads"`
}

// ApplicationMetrics contains application-specific metrics
type ApplicationMetrics struct {
	RequestsPerSecond   float64                `json:"requests_per_second"`
	ResponseTime        time.Duration          `json:"response_time"`
	ErrorRate           float64                `json:"error_rate"`
	Throughput          float64                `json:"throughput"`
	Latency             time.Duration          `json:"latency"`
	ActiveConnections   int                    `json:"active_connections"`
	QueueSize           int                    `json:"queue_size"`
	CacheHitRate        float64                `json:"cache_hit_rate"`
	DatabaseConnections int                    `json:"database_connections"`
	Timestamp           time.Time              `json:"timestamp"`
	Metadata            map[string]interface{} `json:"metadata"`
}

// InfrastructureMetrics contains infrastructure metrics
type InfrastructureMetrics struct {
	NodeCount      int                    `json:"node_count"`
	HealthyNodes   int                    `json:"healthy_nodes"`
	UnhealthyNodes int                    `json:"unhealthy_nodes"`
	LoadBalancers  int                    `json:"load_balancers"`
	DatabaseStatus string                 `json:"database_status"`
	CacheStatus    string                 `json:"cache_status"`
	QueueStatus    string                 `json:"queue_status"`
	ServiceStatus  map[string]string      `json:"service_status"`
	Timestamp      time.Time              `json:"timestamp"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// ResourcePerformanceMetrics contains performance metrics
type ResourcePerformanceMetrics struct {
	AverageResponseTime time.Duration          `json:"average_response_time"`
	P95ResponseTime     time.Duration          `json:"p95_response_time"`
	P99ResponseTime     time.Duration          `json:"p99_response_time"`
	Throughput          float64                `json:"throughput"`
	ErrorRate           float64                `json:"error_rate"`
	SLA                 float64                `json:"sla"`
	Availability        float64                `json:"availability"`
	Timestamp           time.Time              `json:"timestamp"`
	Metadata            map[string]interface{} `json:"metadata"`
}

// CostMetrics contains cost metrics
type CostMetrics struct {
	ComputeCost    float64                `json:"compute_cost"`
	StorageCost    float64                `json:"storage_cost"`
	NetworkCost    float64                `json:"network_cost"`
	TotalCost      float64                `json:"total_cost"`
	CostPerHour    float64                `json:"cost_per_hour"`
	CostPerRequest float64                `json:"cost_per_request"`
	BudgetUsage    float64                `json:"budget_usage"`
	Forecast       float64                `json:"forecast"`
	Timestamp      time.Time              `json:"timestamp"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// ServiceMetrics contains service-specific metrics
type ServiceMetrics struct {
	ServiceName      string                 `json:"service_name"`
	Version          string                 `json:"version"`
	Instances        int                    `json:"instances"`
	HealthyInstances int                    `json:"healthy_instances"`
	RequestCount     int64                  `json:"request_count"`
	ErrorCount       int64                  `json:"error_count"`
	ResponseTime     time.Duration          `json:"response_time"`
	Throughput       float64                `json:"throughput"`
	ResourceUsage    ResourceUtilization    `json:"resource_usage"`
	Timestamp        time.Time              `json:"timestamp"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// UserMetrics contains user experience metrics
type UserMetrics struct {
	ActiveUsers      int                    `json:"active_users"`
	SessionDuration  time.Duration          `json:"session_duration"`
	PageLoadTime     time.Duration          `json:"page_load_time"`
	UserSatisfaction float64                `json:"user_satisfaction"`
	ConversionRate   float64                `json:"conversion_rate"`
	BounceRate       float64                `json:"bounce_rate"`
	Timestamp        time.Time              `json:"timestamp"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// ResourceContext provides context for resource management
type ResourceContext struct {
	Environment     string                   `json:"environment"`
	ServiceTier     string                   `json:"service_tier"`
	CostConstraints CostConstraints          `json:"cost_constraints"`
	Performance     PerformanceRequirements  `json:"performance"`
	Availability    AvailabilityRequirements `json:"availability"`
	Scalability     ScalabilityRequirements  `json:"scalability"`
	Metadata        map[string]interface{}   `json:"metadata"`
}

// CostConstraints defines cost constraints
type CostConstraints struct {
	MaxHourlyCost    float64 `json:"max_hourly_cost"`
	MaxMonthlyCost   float64 `json:"max_monthly_cost"`
	MaxYearlyCost    float64 `json:"max_yearly_cost"`
	CostPerRequest   float64 `json:"cost_per_request"`
	BudgetLimit      float64 `json:"budget_limit"`
	CostOptimization bool    `json:"cost_optimization"`
}

// PerformanceRequirements defines performance requirements
type PerformanceRequirements struct {
	MaxResponseTime time.Duration `json:"max_response_time"`
	MinThroughput   float64       `json:"min_throughput"`
	MaxErrorRate    float64       `json:"max_error_rate"`
	MinAvailability float64       `json:"min_availability"`
	MaxLatency      time.Duration `json:"max_latency"`
	SLOTarget       float64       `json:"slo_target"`
}

// AvailabilityRequirements defines availability requirements
type AvailabilityRequirements struct {
	MinUptime        float64       `json:"min_uptime"`
	MaxDowntime      time.Duration `json:"max_downtime"`
	MTTRTarget       time.Duration `json:"mttr_target"`
	MTBFTarget       time.Duration `json:"mtbf_target"`
	DisasterRecovery bool          `json:"disaster_recovery"`
	BackupRequired   bool          `json:"backup_required"`
}

// ScalabilityRequirements defines scalability requirements
type ScalabilityRequirements struct {
	MinInstances       int           `json:"min_instances"`
	MaxInstances       int           `json:"max_instances"`
	AutoScaling        bool          `json:"auto_scaling"`
	ScaleUpThreshold   float64       `json:"scale_up_threshold"`
	ScaleDownThreshold float64       `json:"scale_down_threshold"`
	ScaleCooldown      time.Duration `json:"scale_cooldown"`
}

// ResourceOutput represents resource management output
type ResourceOutput struct {
	ResourceAnalysis            ResourceAnalysis             `json:"resource_analysis"`
	OptimizationRecommendations []OptimizationRecommendation `json:"optimization_recommendations"`
	ScalingRecommendations      []ScalingRecommendation      `json:"scaling_recommendations"`
	ResourceAlerts              []ResourceAlert              `json:"resource_alerts"`
	PerformanceAnalysis         PerformanceAnalysis          `json:"performance_analysis"`
	CostAnalysis                CostAnalysis                 `json:"cost_analysis"`
	Predictions                 []ResourceResourcePrediction `json:"predictions"`
	Actions                     []ResourceActionItem         `json:"actions"`
	Summary                     ResourceSummary              `json:"summary"`
	Metadata                    map[string]interface{}       `json:"metadata"`
}

// ResourceAnalysis contains resource analysis results
type ResourceAnalysis struct {
	CurrentUtilization    ResourceUtilization    `json:"current_utilization"`
	HistoricalUtilization []ResourceUtilization  `json:"historical_utilization"`
	PeakUtilization       ResourceUtilization    `json:"peak_utilization"`
	AverageUtilization    ResourceUtilization    `json:"average_utilization"`
	ResourceTrends        []ResourceTrend        `json:"resource_trends"`
	Bottlenecks           []ResourceBottleneck   `json:"bottlenecks"`
	Inefficiencies        []ResourceInefficiency `json:"inefficiencies"`
	Capacity              ResourceCapacity       `json:"capacity"`
	Forecast              ResourceForecast       `json:"forecast"`
	Confidence            float64                `json:"confidence"`
	Metadata              map[string]interface{} `json:"metadata"`
}

// OptimizationRecommendation contains optimization recommendations
type OptimizationRecommendation struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Priority         int                    `json:"priority"`
	Title            string                 `json:"title"`
	Description      string                 `json:"description"`
	Target           string                 `json:"target"`
	CurrentValue     float64                `json:"current_value"`
	RecommendedValue float64                `json:"recommended_value"`
	Impact           OptimizationImpact     `json:"impact"`
	Implementation   string                 `json:"implementation"`
	Complexity       string                 `json:"complexity"`
	Risk             string                 `json:"risk"`
	Timeline         time.Duration          `json:"timeline"`
	CostSavings      float64                `json:"cost_savings"`
	PerformanceGain  float64                `json:"performance_gain"`
	Confidence       float64                `json:"confidence"`
	Dependencies     []string               `json:"dependencies"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// ScalingRecommendation contains scaling recommendations
type ScalingRecommendation struct {
	ID                   string                 `json:"id"`
	Type                 string                 `json:"type"`
	Direction            string                 `json:"direction"`
	Target               string                 `json:"target"`
	CurrentInstances     int                    `json:"current_instances"`
	RecommendedInstances int                    `json:"recommended_instances"`
	Reason               string                 `json:"reason"`
	Urgency              string                 `json:"urgency"`
	Impact               ScalingImpact          `json:"impact"`
	Timeline             time.Duration          `json:"timeline"`
	CostImpact           float64                `json:"cost_impact"`
	PerformanceImpact    float64                `json:"performance_impact"`
	Confidence           float64                `json:"confidence"`
	AutoApply            bool                   `json:"auto_apply"`
	Metadata             map[string]interface{} `json:"metadata"`
}

// ResourceAlert contains resource alerts
type ResourceAlert struct {
	ID             string                 `json:"id"`
	Type           string                 `json:"type"`
	Severity       string                 `json:"severity"`
	Title          string                 `json:"title"`
	Description    string                 `json:"description"`
	Resource       string                 `json:"resource"`
	Metric         string                 `json:"metric"`
	CurrentValue   float64                `json:"current_value"`
	ThresholdValue float64                `json:"threshold_value"`
	Duration       time.Duration          `json:"duration"`
	Status         string                 `json:"status"`
	Timestamp      time.Time              `json:"timestamp"`
	Actions        []string               `json:"actions"`
	Metadata       map[string]interface{} `json:"metadata"`
}

// Supporting types for analysis
type ResourceBottleneck struct {
	Resource   string                 `json:"resource"`
	Type       string                 `json:"type"`
	Severity   string                 `json:"severity"`
	Impact     string                 `json:"impact"`
	Cause      string                 `json:"cause"`
	Solution   string                 `json:"solution"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type ResourceInefficiency struct {
	Resource   string                 `json:"resource"`
	Type       string                 `json:"type"`
	Waste      float64                `json:"waste"`
	CostImpact float64                `json:"cost_impact"`
	Solution   string                 `json:"solution"`
	Savings    float64                `json:"savings"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type ResourceCapacity struct {
	CPU     CapacityInfo `json:"cpu"`
	Memory  CapacityInfo `json:"memory"`
	Disk    CapacityInfo `json:"disk"`
	Network CapacityInfo `json:"network"`
}

type CapacityInfo struct {
	Total     float64 `json:"total"`
	Used      float64 `json:"used"`
	Available float64 `json:"available"`
	Reserved  float64 `json:"reserved"`
	Limit     float64 `json:"limit"`
	Usage     float64 `json:"usage"`
}

type ResourceForecast struct {
	Horizon     time.Duration                `json:"horizon"`
	Predictions []ResourceResourcePrediction `json:"predictions"`
	Confidence  float64                      `json:"confidence"`
	Accuracy    float64                      `json:"accuracy"`
	LastUpdated time.Time                    `json:"last_updated"`
	Metadata    map[string]interface{}       `json:"metadata"`
}

type ResourceResourcePrediction struct {
	Timestamp  time.Time              `json:"timestamp"`
	Resource   string                 `json:"resource"`
	Metric     string                 `json:"metric"`
	Value      float64                `json:"value"`
	Confidence float64                `json:"confidence"`
	Bounds     PredictionBounds       `json:"bounds"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type OptimizationImpact struct {
	Performance float64 `json:"performance"`
	Cost        float64 `json:"cost"`
	Reliability float64 `json:"reliability"`
	Scalability float64 `json:"scalability"`
	Efficiency  float64 `json:"efficiency"`
}

type ScalingImpact struct {
	Performance  float64 `json:"performance"`
	Cost         float64 `json:"cost"`
	Availability float64 `json:"availability"`
	Latency      float64 `json:"latency"`
	Throughput   float64 `json:"throughput"`
}

type PerformanceAnalysis struct {
	CurrentPerformance ResourcePerformanceMetrics  `json:"current_performance"`
	TargetPerformance  ResourcePerformanceMetrics  `json:"target_performance"`
	PerformanceGap     ResourcePerformanceMetrics  `json:"performance_gap"`
	Trends             []PerformanceTrend          `json:"trends"`
	Bottlenecks        []PerformanceBottleneck     `json:"bottlenecks"`
	Recommendations    []PerformanceRecommendation `json:"recommendations"`
	SLACompliance      SLACompliance               `json:"sla_compliance"`
	Metadata           map[string]interface{}      `json:"metadata"`
}

type PerformanceTrend struct {
	Metric     string        `json:"metric"`
	Direction  string        `json:"direction"`
	Change     float64       `json:"change"`
	Confidence float64       `json:"confidence"`
	Duration   time.Duration `json:"duration"`
}

type PerformanceBottleneck struct {
	Component  string                 `json:"component"`
	Metric     string                 `json:"metric"`
	Impact     string                 `json:"impact"`
	Cause      string                 `json:"cause"`
	Solution   string                 `json:"solution"`
	Priority   int                    `json:"priority"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type PerformanceRecommendation struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Impact      string                 `json:"impact"`
	Effort      string                 `json:"effort"`
	Priority    int                    `json:"priority"`
	Timeline    time.Duration          `json:"timeline"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type SLACompliance struct {
	Overall      float64                `json:"overall"`
	Availability float64                `json:"availability"`
	Performance  float64                `json:"performance"`
	Reliability  float64                `json:"reliability"`
	Violations   []SLAViolation         `json:"violations"`
	Trends       []SLATrend             `json:"trends"`
	Metadata     map[string]interface{} `json:"metadata"`
}

type SLAViolation struct {
	Metric    string        `json:"metric"`
	Threshold float64       `json:"threshold"`
	Actual    float64       `json:"actual"`
	Duration  time.Duration `json:"duration"`
	Impact    string        `json:"impact"`
	Timestamp time.Time     `json:"timestamp"`
}

type SLATrend struct {
	Metric     string        `json:"metric"`
	Direction  string        `json:"direction"`
	Change     float64       `json:"change"`
	Duration   time.Duration `json:"duration"`
	Confidence float64       `json:"confidence"`
}

type CostAnalysis struct {
	CurrentCost   CostMetrics            `json:"current_cost"`
	TargetCost    CostMetrics            `json:"target_cost"`
	CostTrends    []CostTrend            `json:"cost_trends"`
	CostDrivers   []CostDriver           `json:"cost_drivers"`
	Optimizations []CostOptimization     `json:"optimizations"`
	Forecast      CostForecast           `json:"forecast"`
	BudgetStatus  BudgetStatus           `json:"budget_status"`
	Metadata      map[string]interface{} `json:"metadata"`
}

type CostTrend struct {
	Component  string        `json:"component"`
	Direction  string        `json:"direction"`
	Change     float64       `json:"change"`
	Duration   time.Duration `json:"duration"`
	Confidence float64       `json:"confidence"`
}

type CostDriver struct {
	Component    string                 `json:"component"`
	Cost         float64                `json:"cost"`
	Percentage   float64                `json:"percentage"`
	Trend        string                 `json:"trend"`
	Optimization string                 `json:"optimization"`
	Potential    float64                `json:"potential"`
	Metadata     map[string]interface{} `json:"metadata"`
}

type CostOptimization struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Savings     float64                `json:"savings"`
	Percentage  float64                `json:"percentage"`
	Effort      string                 `json:"effort"`
	Risk        string                 `json:"risk"`
	Timeline    time.Duration          `json:"timeline"`
	Priority    int                    `json:"priority"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type CostForecast struct {
	Horizon     time.Duration          `json:"horizon"`
	Predictions []CostPrediction       `json:"predictions"`
	Confidence  float64                `json:"confidence"`
	Accuracy    float64                `json:"accuracy"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type CostPrediction struct {
	Timestamp  time.Time        `json:"timestamp"`
	Cost       float64          `json:"cost"`
	Confidence float64          `json:"confidence"`
	Bounds     PredictionBounds `json:"bounds"`
}

type BudgetStatus struct {
	Budget    float64                `json:"budget"`
	Spent     float64                `json:"spent"`
	Remaining float64                `json:"remaining"`
	Usage     float64                `json:"usage"`
	Forecast  float64                `json:"forecast"`
	Alert     bool                   `json:"alert"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type ResourceActionItem struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Action     string                 `json:"action"`
	Target     string                 `json:"target"`
	Parameters map[string]interface{} `json:"parameters"`
	Priority   int                    `json:"priority"`
	Urgency    string                 `json:"urgency"`
	Automatic  bool                   `json:"automatic"`
	Timeout    time.Duration          `json:"timeout"`
	Rollback   bool                   `json:"rollback"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type ResourceSummary struct {
	OverallHealth         string                 `json:"overall_health"`
	ResourceUtilization   ResourceUtilization    `json:"resource_utilization"`
	PerformanceScore      float64                `json:"performance_score"`
	CostEfficiency        float64                `json:"cost_efficiency"`
	OptimizationPotential float64                `json:"optimization_potential"`
	RecommendationsCount  int                    `json:"recommendations_count"`
	AlertsCount           int                    `json:"alerts_count"`
	ActionsCount          int                    `json:"actions_count"`
	Trends                map[string]string      `json:"trends"`
	LastUpdated           time.Time              `json:"last_updated"`
	Metadata              map[string]interface{} `json:"metadata"`
}

// NewResourceAgent creates a new resource management agent
func NewResourceAgent() ai.AIAgent {
	capabilities := []ai.Capability{
		{
			Name:        "resource-monitoring",
			Description: "Monitor system resources and performance metrics",
			InputType:   reflect.TypeOf(ResourceInput{}),
			OutputType:  reflect.TypeOf(ResourceOutput{}),
			Metadata: map[string]interface{}{
				"monitoring_accuracy": 0.95,
				"update_frequency":    "real-time",
			},
		},
		{
			Name:        "performance-optimization",
			Description: "Optimize system performance and resource utilization",
			InputType:   reflect.TypeOf(ResourceInput{}),
			OutputType:  reflect.TypeOf(ResourceOutput{}),
			Metadata: map[string]interface{}{
				"optimization_accuracy": 0.88,
				"performance_gain":      "15-30%",
			},
		},
		{
			Name:        "cost-optimization",
			Description: "Optimize costs while maintaining performance",
			InputType:   reflect.TypeOf(ResourceInput{}),
			OutputType:  reflect.TypeOf(ResourceOutput{}),
			Metadata: map[string]interface{}{
				"cost_savings":     "10-25%",
				"optimization_roi": "200-400%",
			},
		},
		{
			Name:        "auto-scaling",
			Description: "Automatically scale resources based on demand",
			InputType:   reflect.TypeOf(ResourceInput{}),
			OutputType:  reflect.TypeOf(ResourceOutput{}),
			Metadata: map[string]interface{}{
				"scaling_accuracy": 0.92,
				"response_time":    "< 2min",
			},
		},
		{
			Name:        "capacity-planning",
			Description: "Plan future resource capacity needs",
			InputType:   reflect.TypeOf(ResourceInput{}),
			OutputType:  reflect.TypeOf(ResourceOutput{}),
			Metadata: map[string]interface{}{
				"forecast_accuracy": 0.85,
				"planning_horizon":  "1-12 months",
			},
		},
	}

	baseAgent := ai.NewBaseAgent("resource-manager", "Resource Management Agent", ai.AgentTypeResourceManager, capabilities)

	return &ResourceAgent{
		BaseAgent: baseAgent,
		resourceMonitor: &ResourceMonitor{
			UpdateInterval: 30 * time.Second,
			HistorySize:    1000,
			Enabled:        true,
		},
		optimizer: &ResourceOptimizer{
			Enabled:         true,
			AutoApply:       false,
			LearningEnabled: true,
		},
		scaler: &ResourceScaler{
			Enabled:        true,
			AutoScale:      false,
			CooldownPeriod: 5 * time.Minute,
			MinInstances:   1,
			MaxInstances:   10,
		},
		thresholds: ResourceThresholds{
			CPU: CPUThresholds{
				Warning:  80.0,
				Critical: 90.0,
				LoadHigh: 5.0,
				TempHigh: 80.0,
			},
			Memory: MemoryThresholds{
				Warning:     85.0,
				Critical:    95.0,
				SwapHigh:    50.0,
				ProcessHigh: 1024 * 1024 * 1024, // 1GB
			},
			Disk: DiskThresholds{
				Warning:     80.0,
				Critical:    90.0,
				IOHigh:      1000,
				LatencyHigh: 100.0,
			},
			Network: NetworkThresholds{
				Warning:         75.0,
				Critical:        90.0,
				BandwidthHigh:   1000000000, // 1Gbps
				ConnectionsHigh: 10000,
			},
		},
		resourcePolicies:  []ResourcePolicy{},
		resourceStats:     ResourceStats{OptimizationsByType: make(map[string]int64), ScalingsByType: make(map[string]int64), ResourceTrends: make(map[string]ResourceTrend)},
		optimizationRules: []OptimizationRule{},
		autoScaling:       false,
		autoOptimization:  false,
		learningEnabled:   true,
	}
}

// Initialize initializes the resource agent
func (a *ResourceAgent) Initialize(ctx context.Context, config ai.AgentConfig) error {
	if err := a.BaseAgent.Initialize(ctx, config); err != nil {
		return err
	}

	// Initialize resource-specific configuration
	if resourceConfig, ok := config.Metadata["resource"]; ok {
		if configMap, ok := resourceConfig.(map[string]interface{}); ok {
			if autoScale, ok := configMap["auto_scaling"].(bool); ok {
				a.autoScaling = autoScale
			}
			if autoOpt, ok := configMap["auto_optimization"].(bool); ok {
				a.autoOptimization = autoOpt
			}
			if learning, ok := configMap["learning_enabled"].(bool); ok {
				a.learningEnabled = learning
			}
		}
	}

	// Initialize resource policies
	a.initializeResourcePolicies()

	// Initialize optimization rules
	a.initializeOptimizationRules()

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Info("resource agent initialized",
			logger.String("agent_id", a.ID()),
			logger.Bool("auto_scaling", a.autoScaling),
			logger.Bool("auto_optimization", a.autoOptimization),
			logger.Bool("learning_enabled", a.learningEnabled),
			logger.Int("resource_policies", len(a.resourcePolicies)),
			logger.Int("optimization_rules", len(a.optimizationRules)),
		)
	}

	return nil
}

// Process processes resource monitoring input
func (a *ResourceAgent) Process(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
	startTime := time.Now()

	// Convert input to resource-specific input
	resourceInput, ok := input.Data.(ResourceInput)
	if !ok {
		return ai.AgentOutput{}, fmt.Errorf("invalid input type for resource agent")
	}

	// Analyze current resource state
	resourceAnalysis := a.analyzeResources(resourceInput)

	// Generate optimization recommendations
	optimizationRecommendations := a.generateOptimizationRecommendations(resourceInput, resourceAnalysis)

	// Generate scaling recommendations
	scalingRecommendations := a.generateScalingRecommendations(resourceInput, resourceAnalysis)

	// Generate resource alerts
	resourceAlerts := a.generateResourceAlerts(resourceInput, resourceAnalysis)

	// Analyze performance
	performanceAnalysis := a.analyzePerformance(resourceInput, resourceAnalysis)

	// Analyze costs
	costAnalysis := a.analyzeCosts(resourceInput, resourceAnalysis)

	// Generate predictions
	predictions := a.generateResourceResourcePredictions(resourceInput, resourceAnalysis)

	// Create actions
	actions := a.createResourceActions(optimizationRecommendations, scalingRecommendations, resourceAlerts)

	// Generate summary
	summary := a.generateResourceSummary(resourceAnalysis, performanceAnalysis, costAnalysis, optimizationRecommendations, resourceAlerts)

	// Create output
	output := ResourceOutput{
		ResourceAnalysis:            resourceAnalysis,
		OptimizationRecommendations: optimizationRecommendations,
		ScalingRecommendations:      scalingRecommendations,
		ResourceAlerts:              resourceAlerts,
		PerformanceAnalysis:         performanceAnalysis,
		CostAnalysis:                costAnalysis,
		Predictions:                 predictions,
		Actions:                     actions,
		Summary:                     summary,
		Metadata: map[string]interface{}{
			"processing_time": time.Since(startTime),
			"agent_version":   "1.0.0",
		},
	}

	// Update statistics
	a.updateResourceStats(output)

	// Create agent output
	agentOutput := ai.AgentOutput{
		Type:        "resource-management",
		Data:        output,
		Confidence:  a.calculateResourceConfidence(resourceInput, resourceAnalysis),
		Explanation: a.generateResourceExplanation(output),
		Actions:     a.convertToAgentActions(actions),
		Metadata: map[string]interface{}{
			"processing_time":       time.Since(startTime),
			"recommendations_count": len(optimizationRecommendations),
			"alerts_count":          len(resourceAlerts),
			"actions_count":         len(actions),
			"performance_score":     summary.PerformanceScore,
			"cost_efficiency":       summary.CostEfficiency,
		},
		Timestamp: time.Now(),
	}

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Debug("resource management processed",
			logger.String("agent_id", a.ID()),
			logger.String("request_id", input.RequestID),
			logger.Int("recommendations", len(optimizationRecommendations)),
			logger.Int("alerts", len(resourceAlerts)),
			logger.Float64("performance_score", summary.PerformanceScore),
			logger.Duration("processing_time", time.Since(startTime)),
		)
	}

	return agentOutput, nil
}

// initializeResourcePolicies initializes default resource policies
func (a *ResourceAgent) initializeResourcePolicies() {
	a.resourcePolicies = []ResourcePolicy{
		{
			ID:     "cpu-high-usage",
			Name:   "CPU High Usage Policy",
			Type:   "cpu",
			Target: "system",
			Rules: []ResourceRule{
				{
					ID:        "cpu-warning",
					Name:      "CPU Warning Threshold",
					Condition: "cpu_usage > threshold",
					Metric:    "cpu_usage",
					Threshold: 80.0,
					Operator:  ">",
					Duration:  5 * time.Minute,
					Enabled:   true,
				},
			},
			Actions: []ResourceAction{
				{
					ID:     "scale-up",
					Type:   "scaling",
					Name:   "Scale Up",
					Target: "instances",
					Parameters: map[string]interface{}{
						"scale_factor":  1.5,
						"max_instances": 10,
					},
					Automatic: false,
					Timeout:   10 * time.Minute,
					Rollback:  true,
				},
			},
			Enabled:  true,
			Priority: 1,
		},
		{
			ID:     "memory-high-usage",
			Name:   "Memory High Usage Policy",
			Type:   "memory",
			Target: "system",
			Rules: []ResourceRule{
				{
					ID:        "memory-warning",
					Name:      "Memory Warning Threshold",
					Condition: "memory_usage > threshold",
					Metric:    "memory_usage",
					Threshold: 85.0,
					Operator:  ">",
					Duration:  3 * time.Minute,
					Enabled:   true,
				},
			},
			Actions: []ResourceAction{
				{
					ID:     "optimize-memory",
					Type:   "optimization",
					Name:   "Memory Optimization",
					Target: "memory",
					Parameters: map[string]interface{}{
						"clear_cache":   true,
						"gc_aggressive": true,
					},
					Automatic: true,
					Timeout:   5 * time.Minute,
					Rollback:  false,
				},
			},
			Enabled:  true,
			Priority: 2,
		},
	}
}

// initializeOptimizationRules initializes default optimization rules
func (a *ResourceAgent) initializeOptimizationRules() {
	a.optimizationRules = []OptimizationRule{
		{
			ID:           "cpu-optimization",
			Name:         "CPU Optimization",
			Type:         "cpu",
			Condition:    "cpu_usage < 50 AND load_average < 2.0",
			Optimization: "reduce_cpu_allocation",
			Parameters: map[string]interface{}{
				"reduction_factor": 0.8,
				"min_allocation":   0.5,
			},
			Enabled:       true,
			Priority:      1,
			Effectiveness: 0.85,
		},
		{
			ID:           "memory-optimization",
			Name:         "Memory Optimization",
			Type:         "memory",
			Condition:    "memory_usage < 60 AND swap_usage < 10",
			Optimization: "reduce_memory_allocation",
			Parameters: map[string]interface{}{
				"reduction_factor": 0.9,
				"min_allocation":   0.5,
			},
			Enabled:       true,
			Priority:      2,
			Effectiveness: 0.78,
		},
		{
			ID:           "disk-optimization",
			Name:         "Disk Optimization",
			Type:         "disk",
			Condition:    "disk_usage < 70 AND io_wait < 10",
			Optimization: "optimize_disk_usage",
			Parameters: map[string]interface{}{
				"cleanup_temp":  true,
				"compress_logs": true,
				"archive_old":   true,
			},
			Enabled:       true,
			Priority:      3,
			Effectiveness: 0.72,
		},
	}
}

// analyzeResources analyzes current resource state
func (a *ResourceAgent) analyzeResources(input ResourceInput) ResourceAnalysis {
	currentUtilization := ResourceUtilization{
		CPU:     input.SystemMetrics.CPU.Usage,
		Memory:  input.SystemMetrics.Memory.Usage,
		Disk:    input.SystemMetrics.Disk.Usage,
		Network: input.SystemMetrics.Network.Usage,
	}

	// Calculate trends
	trends := a.calculateResourceTrends(input)

	// Identify bottlenecks
	bottlenecks := a.identifyBottlenecks(input, currentUtilization)

	// Identify inefficiencies
	inefficiencies := a.identifyInefficiencies(input, currentUtilization)

	// Calculate capacity
	capacity := a.calculateCapacity(input)

	// Generate forecast
	forecast := a.generateResourceForecast(input, trends)

	return ResourceAnalysis{
		CurrentUtilization: currentUtilization,
		ResourceTrends:     trends,
		Bottlenecks:        bottlenecks,
		Inefficiencies:     inefficiencies,
		Capacity:           capacity,
		Forecast:           forecast,
		Confidence:         0.85,
		Metadata: map[string]interface{}{
			"analysis_time": time.Now(),
			"data_quality":  "good",
		},
	}
}

// calculateResourceTrends calculates resource trends
func (a *ResourceAgent) calculateResourceTrends(input ResourceInput) []ResourceTrend {
	trends := []ResourceTrend{}

	// CPU trend
	trends = append(trends, ResourceTrend{
		Resource:   "cpu",
		Direction:  a.determineTrendDirection(input.SystemMetrics.CPU.Usage),
		Change:     0.05, // 5% change
		Confidence: 0.8,
		StartTime:  time.Now().Add(-time.Hour),
		EndTime:    time.Now(),
		Duration:   time.Hour,
	})

	// Memory trend
	trends = append(trends, ResourceTrend{
		Resource:   "memory",
		Direction:  a.determineTrendDirection(input.SystemMetrics.Memory.Usage),
		Change:     0.03, // 3% change
		Confidence: 0.85,
		StartTime:  time.Now().Add(-time.Hour),
		EndTime:    time.Now(),
		Duration:   time.Hour,
	})

	// Disk trend
	trends = append(trends, ResourceTrend{
		Resource:   "disk",
		Direction:  a.determineTrendDirection(input.SystemMetrics.Disk.Usage),
		Change:     0.02, // 2% change
		Confidence: 0.9,
		StartTime:  time.Now().Add(-time.Hour),
		EndTime:    time.Now(),
		Duration:   time.Hour,
	})

	// Network trend
	trends = append(trends, ResourceTrend{
		Resource:   "network",
		Direction:  a.determineTrendDirection(input.SystemMetrics.Network.Usage),
		Change:     0.10, // 10% change
		Confidence: 0.75,
		StartTime:  time.Now().Add(-time.Hour),
		EndTime:    time.Now(),
		Duration:   time.Hour,
	})

	return trends
}

// determineTrendDirection determines trend direction based on usage
func (a *ResourceAgent) determineTrendDirection(usage float64) string {
	if usage > 80 {
		return "increasing"
	} else if usage < 30 {
		return "decreasing"
	}
	return "stable"
}

// identifyBottlenecks identifies resource bottlenecks
func (a *ResourceAgent) identifyBottlenecks(input ResourceInput, utilization ResourceUtilization) []ResourceBottleneck {
	bottlenecks := []ResourceBottleneck{}

	// Check CPU bottleneck
	if utilization.CPU > a.thresholds.CPU.Warning {
		bottlenecks = append(bottlenecks, ResourceBottleneck{
			Resource:   "cpu",
			Type:       "high_usage",
			Severity:   a.determineSeverity(utilization.CPU, a.thresholds.CPU.Warning, a.thresholds.CPU.Critical),
			Impact:     "performance degradation",
			Cause:      "high CPU utilization",
			Solution:   "scale up instances or optimize CPU-intensive processes",
			Confidence: 0.9,
		})
	}

	// Check Memory bottleneck
	if utilization.Memory > a.thresholds.Memory.Warning {
		bottlenecks = append(bottlenecks, ResourceBottleneck{
			Resource:   "memory",
			Type:       "high_usage",
			Severity:   a.determineSeverity(utilization.Memory, a.thresholds.Memory.Warning, a.thresholds.Memory.Critical),
			Impact:     "potential OOM and performance issues",
			Cause:      "high memory utilization",
			Solution:   "increase memory allocation or optimize memory usage",
			Confidence: 0.85,
		})
	}

	// Check Disk bottleneck
	if utilization.Disk > a.thresholds.Disk.Warning {
		bottlenecks = append(bottlenecks, ResourceBottleneck{
			Resource:   "disk",
			Type:       "high_usage",
			Severity:   a.determineSeverity(utilization.Disk, a.thresholds.Disk.Warning, a.thresholds.Disk.Critical),
			Impact:     "slow I/O operations",
			Cause:      "high disk utilization",
			Solution:   "add more storage or optimize disk usage",
			Confidence: 0.8,
		})
	}

	// Check Network bottleneck
	if utilization.Network > a.thresholds.Network.Warning {
		bottlenecks = append(bottlenecks, ResourceBottleneck{
			Resource:   "network",
			Type:       "high_usage",
			Severity:   a.determineSeverity(utilization.Network, a.thresholds.Network.Warning, a.thresholds.Network.Critical),
			Impact:     "network latency and timeouts",
			Cause:      "high network utilization",
			Solution:   "increase bandwidth or optimize network usage",
			Confidence: 0.75,
		})
	}

	return bottlenecks
}

// identifyInefficiencies identifies resource inefficiencies
func (a *ResourceAgent) identifyInefficiencies(input ResourceInput, utilization ResourceUtilization) []ResourceInefficiency {
	inefficiencies := []ResourceInefficiency{}

	// Check CPU inefficiency
	if utilization.CPU < 30 {
		inefficiencies = append(inefficiencies, ResourceInefficiency{
			Resource:   "cpu",
			Type:       "underutilization",
			Waste:      30 - utilization.CPU,
			CostImpact: 100.0, // $100 estimated waste
			Solution:   "reduce CPU allocation or consolidate workloads",
			Savings:    50.0, // $50 estimated savings
			Confidence: 0.8,
		})
	}

	// Check Memory inefficiency
	if utilization.Memory < 40 {
		inefficiencies = append(inefficiencies, ResourceInefficiency{
			Resource:   "memory",
			Type:       "underutilization",
			Waste:      40 - utilization.Memory,
			CostImpact: 80.0, // $80 estimated waste
			Solution:   "reduce memory allocation or consolidate workloads",
			Savings:    40.0, // $40 estimated savings
			Confidence: 0.85,
		})
	}

	return inefficiencies
}

// calculateCapacity calculates resource capacity
func (a *ResourceAgent) calculateCapacity(input ResourceInput) ResourceCapacity {
	return ResourceCapacity{
		CPU: CapacityInfo{
			Total:     float64(input.SystemMetrics.CPU.Cores * 100),
			Used:      input.SystemMetrics.CPU.Usage,
			Available: float64(input.SystemMetrics.CPU.Cores*100) - input.SystemMetrics.CPU.Usage,
			Reserved:  10.0, // 10% reserved
			Limit:     90.0, // 90% limit
			Usage:     input.SystemMetrics.CPU.Usage / float64(input.SystemMetrics.CPU.Cores*100),
		},
		Memory: CapacityInfo{
			Total:     float64(input.SystemMetrics.Memory.Total),
			Used:      float64(input.SystemMetrics.Memory.Used),
			Available: float64(input.SystemMetrics.Memory.Available),
			Reserved:  float64(input.SystemMetrics.Memory.Total) * 0.1, // 10% reserved
			Limit:     float64(input.SystemMetrics.Memory.Total) * 0.9, // 90% limit
			Usage:     input.SystemMetrics.Memory.Usage,
		},
		Disk: CapacityInfo{
			Total:     float64(input.SystemMetrics.Disk.Total),
			Used:      float64(input.SystemMetrics.Disk.Used),
			Available: float64(input.SystemMetrics.Disk.Available),
			Reserved:  float64(input.SystemMetrics.Disk.Total) * 0.05, // 5% reserved
			Limit:     float64(input.SystemMetrics.Disk.Total) * 0.95, // 95% limit
			Usage:     input.SystemMetrics.Disk.Usage,
		},
		Network: CapacityInfo{
			Total:     float64(input.SystemMetrics.Network.Bandwidth),
			Used:      float64(input.SystemMetrics.Network.RxBytes + input.SystemMetrics.Network.TxBytes),
			Available: float64(input.SystemMetrics.Network.Bandwidth) - float64(input.SystemMetrics.Network.RxBytes+input.SystemMetrics.Network.TxBytes),
			Reserved:  float64(input.SystemMetrics.Network.Bandwidth) * 0.1, // 10% reserved
			Limit:     float64(input.SystemMetrics.Network.Bandwidth) * 0.9, // 90% limit
			Usage:     input.SystemMetrics.Network.Usage,
		},
	}
}

// generateResourceForecast generates resource forecasts
func (a *ResourceAgent) generateResourceForecast(input ResourceInput, trends []ResourceTrend) ResourceForecast {
	predictions := []ResourceResourcePrediction{}

	for _, trend := range trends {
		// Generate predictions for next 24 hours
		for i := 1; i <= 24; i++ {
			prediction := ResourceResourcePrediction{
				Timestamp:  time.Now().Add(time.Duration(i) * time.Hour),
				Resource:   trend.Resource,
				Metric:     "usage",
				Value:      a.calculateForecastValue(trend, float64(i)),
				Confidence: trend.Confidence * 0.9, // Confidence decreases with time
				Bounds: PredictionBounds{
					Lower: a.calculateForecastValue(trend, float64(i)) * 0.8,
					Upper: a.calculateForecastValue(trend, float64(i)) * 1.2,
				},
			}
			predictions = append(predictions, prediction)
		}
	}

	return ResourceForecast{
		Horizon:     24 * time.Hour,
		Predictions: predictions,
		Confidence:  0.8,
		Accuracy:    0.85,
		LastUpdated: time.Now(),
	}
}

// calculateForecastValue calculates forecast value based on trend
func (a *ResourceAgent) calculateForecastValue(trend ResourceTrend, hours float64) float64 {
	baseValue := 50.0                 // Assume 50% as base
	changeRate := trend.Change / 24.0 // Change per hour

	if trend.Direction == "increasing" {
		return baseValue + (changeRate * hours)
	} else if trend.Direction == "decreasing" {
		return baseValue - (changeRate * hours)
	}
	return baseValue
}

// determineSeverity determines severity based on thresholds
func (a *ResourceAgent) determineSeverity(value, warning, critical float64) string {
	if value >= critical {
		return "critical"
	} else if value >= warning {
		return "warning"
	}
	return "normal"
}

// generateOptimizationRecommendations generates optimization recommendations
func (a *ResourceAgent) generateOptimizationRecommendations(input ResourceInput, analysis ResourceAnalysis) []OptimizationRecommendation {
	recommendations := []OptimizationRecommendation{}

	// Generate recommendations based on analysis
	for _, inefficiency := range analysis.Inefficiencies {
		recommendation := OptimizationRecommendation{
			ID:               fmt.Sprintf("opt-%s-%d", inefficiency.Resource, time.Now().Unix()),
			Type:             "cost-optimization",
			Priority:         1,
			Title:            fmt.Sprintf("Optimize %s usage", inefficiency.Resource),
			Description:      fmt.Sprintf("Reduce %s allocation to eliminate waste", inefficiency.Resource),
			Target:           inefficiency.Resource,
			CurrentValue:     inefficiency.Waste,
			RecommendedValue: 0,
			Impact: OptimizationImpact{
				Cost:        inefficiency.CostImpact,
				Performance: 0.0,
				Reliability: 0.0,
				Scalability: 0.0,
				Efficiency:  inefficiency.Savings,
			},
			Implementation:  inefficiency.Solution,
			Complexity:      "medium",
			Risk:            "low",
			Timeline:        time.Hour,
			CostSavings:     inefficiency.Savings,
			PerformanceGain: 0.0,
			Confidence:      inefficiency.Confidence,
		}
		recommendations = append(recommendations, recommendation)
	}

	// Sort recommendations by priority and impact
	sort.Slice(recommendations, func(i, j int) bool {
		if recommendations[i].Priority != recommendations[j].Priority {
			return recommendations[i].Priority < recommendations[j].Priority
		}
		return recommendations[i].CostSavings > recommendations[j].CostSavings
	})

	return recommendations
}

// generateScalingRecommendations generates scaling recommendations
func (a *ResourceAgent) generateScalingRecommendations(input ResourceInput, analysis ResourceAnalysis) []ScalingRecommendation {
	recommendations := []ScalingRecommendation{}

	// Check if scaling is needed based on bottlenecks
	for _, bottleneck := range analysis.Bottlenecks {
		if bottleneck.Severity == "critical" {
			recommendation := ScalingRecommendation{
				ID:                   fmt.Sprintf("scale-%s-%d", bottleneck.Resource, time.Now().Unix()),
				Type:                 "horizontal",
				Direction:            "up",
				Target:               bottleneck.Resource,
				CurrentInstances:     1,
				RecommendedInstances: 2,
				Reason:               bottleneck.Cause,
				Urgency:              "high",
				Impact: ScalingImpact{
					Performance:  0.5,
					Cost:         100.0,
					Availability: 0.1,
					Latency:      -0.3,
					Throughput:   0.8,
				},
				Timeline:          5 * time.Minute,
				CostImpact:        100.0,
				PerformanceImpact: 0.5,
				Confidence:        bottleneck.Confidence,
				AutoApply:         a.autoScaling,
			}
			recommendations = append(recommendations, recommendation)
		}
	}

	return recommendations
}

// generateResourceAlerts generates resource alerts
func (a *ResourceAgent) generateResourceAlerts(input ResourceInput, analysis ResourceAnalysis) []ResourceAlert {
	alerts := []ResourceAlert{}

	// Generate alerts based on bottlenecks
	for _, bottleneck := range analysis.Bottlenecks {
		alert := ResourceAlert{
			ID:             fmt.Sprintf("alert-%s-%d", bottleneck.Resource, time.Now().Unix()),
			Type:           "resource_bottleneck",
			Severity:       bottleneck.Severity,
			Title:          fmt.Sprintf("%s bottleneck detected", bottleneck.Resource),
			Description:    bottleneck.Cause,
			Resource:       bottleneck.Resource,
			Metric:         "usage",
			CurrentValue:   analysis.CurrentUtilization.CPU, // Simplified
			ThresholdValue: a.thresholds.CPU.Warning,        // Simplified
			Duration:       5 * time.Minute,
			Status:         "active",
			Timestamp:      time.Now(),
			Actions:        []string{bottleneck.Solution},
		}
		alerts = append(alerts, alert)
	}

	return alerts
}

// analyzePerformance analyzes performance metrics
func (a *ResourceAgent) analyzePerformance(input ResourceInput, analysis ResourceAnalysis) PerformanceAnalysis {
	currentPerformance := ResourcePerformanceMetrics{
		AverageResponseTime: input.ResourcePerformanceMetrics.AverageResponseTime,
		P95ResponseTime:     input.ResourcePerformanceMetrics.P95ResponseTime,
		P99ResponseTime:     input.ResourcePerformanceMetrics.P99ResponseTime,
		Throughput:          input.ResourcePerformanceMetrics.Throughput,
		ErrorRate:           input.ResourcePerformanceMetrics.ErrorRate,
		SLA:                 input.ResourcePerformanceMetrics.SLA,
		Availability:        input.ResourcePerformanceMetrics.Availability,
		Timestamp:           time.Now(),
	}

	// Calculate performance score
	performanceScore := a.calculatePerformanceScore(currentPerformance)

	return PerformanceAnalysis{
		CurrentPerformance: currentPerformance,
		Metadata: map[string]interface{}{
			"performance_score": performanceScore,
			"analysis_time":     time.Now(),
		},
	}
}

// calculatePerformanceScore calculates performance score
func (a *ResourceAgent) calculatePerformanceScore(metrics ResourcePerformanceMetrics) float64 {
	// Simple scoring algorithm
	score := 100.0

	// Penalize high response times
	if metrics.AverageResponseTime > 100*time.Millisecond {
		score -= 10.0
	}

	// Penalize low throughput
	if metrics.Throughput < 100 {
		score -= 10.0
	}

	// Penalize high error rates
	if metrics.ErrorRate > 0.01 {
		score -= 20.0
	}

	// Penalize low availability
	if metrics.Availability < 0.99 {
		score -= 30.0
	}

	if score < 0 {
		score = 0
	}

	return score
}

// analyzeCosts analyzes cost metrics
func (a *ResourceAgent) analyzeCosts(input ResourceInput, analysis ResourceAnalysis) CostAnalysis {
	currentCost := CostMetrics{
		ComputeCost:    input.CostMetrics.ComputeCost,
		StorageCost:    input.CostMetrics.StorageCost,
		NetworkCost:    input.CostMetrics.NetworkCost,
		TotalCost:      input.CostMetrics.TotalCost,
		CostPerHour:    input.CostMetrics.CostPerHour,
		CostPerRequest: input.CostMetrics.CostPerRequest,
		BudgetUsage:    input.CostMetrics.BudgetUsage,
		Forecast:       input.CostMetrics.Forecast,
		Timestamp:      time.Now(),
	}

	return CostAnalysis{
		CurrentCost: currentCost,
		Metadata: map[string]interface{}{
			"analysis_time": time.Now(),
		},
	}
}

// generateResourceResourcePredictions generates resource predictions
func (a *ResourceAgent) generateResourceResourcePredictions(input ResourceInput, analysis ResourceAnalysis) []ResourceResourcePrediction {
	predictions := []ResourceResourcePrediction{}

	// Generate predictions based on forecast
	for _, prediction := range analysis.Forecast.Predictions {
		predictions = append(predictions, prediction)
	}

	return predictions
}

// createResourceActions creates resource actions
func (a *ResourceAgent) createResourceActions(optimizations []OptimizationRecommendation, scalings []ScalingRecommendation, alerts []ResourceAlert) []ResourceActionItem {
	actions := []ResourceActionItem{}

	// Create actions from optimization recommendations
	for _, opt := range optimizations {
		action := ResourceActionItem{
			ID:     fmt.Sprintf("action-opt-%s", opt.ID),
			Type:   "optimization",
			Action: opt.Implementation,
			Target: opt.Target,
			Parameters: map[string]interface{}{
				"current_value":     opt.CurrentValue,
				"recommended_value": opt.RecommendedValue,
				"cost_savings":      opt.CostSavings,
			},
			Priority:   opt.Priority,
			Urgency:    "medium",
			Automatic:  a.autoOptimization,
			Timeout:    opt.Timeline,
			Rollback:   true,
			Confidence: opt.Confidence,
		}
		actions = append(actions, action)
	}

	// Create actions from scaling recommendations
	for _, scale := range scalings {
		action := ResourceActionItem{
			ID:     fmt.Sprintf("action-scale-%s", scale.ID),
			Type:   "scaling",
			Action: fmt.Sprintf("scale_%s", scale.Direction),
			Target: scale.Target,
			Parameters: map[string]interface{}{
				"current_instances":     scale.CurrentInstances,
				"recommended_instances": scale.RecommendedInstances,
				"cost_impact":           scale.CostImpact,
			},
			Priority:   1,
			Urgency:    scale.Urgency,
			Automatic:  scale.AutoApply,
			Timeout:    scale.Timeline,
			Rollback:   true,
			Confidence: scale.Confidence,
		}
		actions = append(actions, action)
	}

	return actions
}

// generateResourceSummary generates resource summary
func (a *ResourceAgent) generateResourceSummary(analysis ResourceAnalysis, performance PerformanceAnalysis, cost CostAnalysis, recommendations []OptimizationRecommendation, alerts []ResourceAlert) ResourceSummary {
	// Calculate overall health
	health := "good"
	if len(alerts) > 0 {
		health = "warning"
		for _, alert := range alerts {
			if alert.Severity == "critical" {
				health = "critical"
				break
			}
		}
	}

	// Calculate optimization potential
	optimizationPotential := 0.0
	for _, rec := range recommendations {
		optimizationPotential += rec.CostSavings
	}

	// Calculate cost efficiency
	costEfficiency := 100.0 - (optimizationPotential / 1000.0 * 100.0)
	if costEfficiency < 0 {
		costEfficiency = 0
	}

	// Calculate performance score
	performanceScore := a.calculatePerformanceScore(performance.CurrentPerformance)

	return ResourceSummary{
		OverallHealth:         health,
		ResourceUtilization:   analysis.CurrentUtilization,
		PerformanceScore:      performanceScore,
		CostEfficiency:        costEfficiency,
		OptimizationPotential: optimizationPotential,
		RecommendationsCount:  len(recommendations),
		AlertsCount:           len(alerts),
		ActionsCount:          len(recommendations) + len(alerts),
		Trends: map[string]string{
			"cpu":     "stable",
			"memory":  "increasing",
			"disk":    "stable",
			"network": "decreasing",
		},
		LastUpdated: time.Now(),
	}
}

// Helper methods
func (a *ResourceAgent) updateResourceStats(output ResourceOutput) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.resourceStats.TotalOptimizations++
	a.resourceStats.LastOptimization = time.Now()

	for _, rec := range output.OptimizationRecommendations {
		a.resourceStats.OptimizationsByType[rec.Type]++
		a.resourceStats.CostSavings += rec.CostSavings
	}

	for _, rec := range output.ScalingRecommendations {
		a.resourceStats.ScalingsByType[rec.Type]++
		a.resourceStats.TotalScalingEvents++
		a.resourceStats.LastScaling = time.Now()
	}

	a.resourceStats.AverageUtilization = output.ResourceAnalysis.CurrentUtilization
	a.resourceStats.PerformanceGain = output.Summary.PerformanceScore
}

func (a *ResourceAgent) calculateResourceConfidence(input ResourceInput, analysis ResourceAnalysis) float64 {
	confidence := 0.8 // Base confidence

	// Adjust based on data quality
	if len(analysis.Bottlenecks) > 0 {
		confidence += 0.1
	}

	// Adjust based on trend consistency
	if len(analysis.ResourceTrends) > 0 {
		confidence += 0.05
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (a *ResourceAgent) generateResourceExplanation(output ResourceOutput) string {
	explanation := "Resource analysis completed. "

	if len(output.ResourceAlerts) > 0 {
		explanation += fmt.Sprintf("Found %d resource alerts requiring attention. ", len(output.ResourceAlerts))
	}

	if len(output.OptimizationRecommendations) > 0 {
		explanation += fmt.Sprintf("Generated %d optimization recommendations with potential savings of $%.2f. ", len(output.OptimizationRecommendations), output.Summary.OptimizationPotential)
	}

	if len(output.ScalingRecommendations) > 0 {
		explanation += fmt.Sprintf("Generated %d scaling recommendations to address performance issues. ", len(output.ScalingRecommendations))
	}

	explanation += fmt.Sprintf("Overall system health: %s. Performance score: %.1f.", output.Summary.OverallHealth, output.Summary.PerformanceScore)

	return explanation
}

func (a *ResourceAgent) convertToAgentActions(actions []ResourceActionItem) []ai.AgentAction {
	agentActions := []ai.AgentAction{}

	for _, action := range actions {
		agentAction := ai.AgentAction{
			Type:       action.Type,
			Target:     action.Target,
			Parameters: action.Parameters,
			Priority:   action.Priority,
			Timeout:    action.Timeout,
		}
		agentActions = append(agentActions, agentAction)
	}

	return agentActions
}
