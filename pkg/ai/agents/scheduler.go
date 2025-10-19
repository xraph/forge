package agents

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/xraph/forge/pkg/ai"
	"github.com/xraph/forge/pkg/logger"
)

// SchedulerAgent optimizes job scheduling and resource allocation
type SchedulerAgent struct {
	*ai.BaseAgent
	cronManager          interface{} // Cron manager from Phase 4
	resourceThresholds   SchedulerResourceThresholds
	optimizationPolicies []OptimizationPolicy
	schedulingStats      SchedulingStats
	jobPredictions       map[string]JobPrediction
}

// SchedulerResourceThresholds defines resource utilization thresholds
type SchedulerResourceThresholds struct {
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	Disk    float64 `json:"disk"`
	Network float64 `json:"network"`
}

// OptimizationPolicy defines job scheduling optimization policies
type OptimizationPolicy struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // priority, load_balance, cost_optimize, sla_optimize
	Weight      float64                `json:"weight"`
	Conditions  []string               `json:"conditions"`
	Parameters  map[string]interface{} `json:"parameters"`
	Enabled     bool                   `json:"enabled"`
	SuccessRate float64                `json:"success_rate"`
	LastUsed    time.Time              `json:"last_used"`
}

// SchedulingStats tracks scheduling optimization metrics
type SchedulingStats struct {
	TotalOptimizations  int64            `json:"total_optimizations"`
	SuccessfulSchedules int64            `json:"successful_schedules"`
	FailedSchedules     int64            `json:"failed_schedules"`
	AverageOptimization time.Duration    `json:"average_optimization"`
	ResourceSavings     float64          `json:"resource_savings"`
	SLAComplianceRate   float64          `json:"sla_compliance_rate"`
	LastOptimization    time.Time        `json:"last_optimization"`
	OptimizationsByType map[string]int64 `json:"optimizations_by_type"`
}

// JobPrediction contains job execution predictions
type JobPrediction struct {
	JobID              string        `json:"job_id"`
	EstimatedRuntime   time.Duration `json:"estimated_runtime"`
	ResourceUsage      ResourceUsage `json:"resource_usage"`
	SuccessProbability float64       `json:"success_probability"`
	OptimalStartTime   time.Time     `json:"optimal_start_time"`
	Dependencies       []string      `json:"dependencies"`
	Priority           int           `json:"priority"`
	LastUpdated        time.Time     `json:"last_updated"`
}

// SchedulerInput represents job scheduling optimization input
type SchedulerInput struct {
	Jobs              []Job                `json:"jobs"`
	Resources         ResourceAvailability `json:"resources"`
	Constraints       []Constraint         `json:"constraints"`
	SLARequirements   []SLARequirement     `json:"sla_requirements"`
	HistoricalData    HistoricalJobData    `json:"historical_data"`
	TimeWindow        TimeWindow           `json:"time_window"`
	OptimizationGoals []OptimizationGoal   `json:"optimization_goals"`
	SystemLoad        SystemLoad           `json:"system_load"`
}

// Job represents a job to be scheduled
type Job struct {
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name"`
	Type                 string                 `json:"type"`
	Priority             int                    `json:"priority"`
	EstimatedRuntime     time.Duration          `json:"estimated_runtime"`
	ResourceRequirements ResourceRequirements   `json:"resource_requirements"`
	Dependencies         []string               `json:"dependencies"`
	Schedule             string                 `json:"schedule"` // cron expression
	Deadline             *time.Time             `json:"deadline"`
	Retries              int                    `json:"retries"`
	Status               string                 `json:"status"`
	LastRun              *time.Time             `json:"last_run"`
	LastDuration         time.Duration          `json:"last_duration"`
	SuccessRate          float64                `json:"success_rate"`
	Metadata             map[string]interface{} `json:"metadata"`
}

// ResourceRequirements defines job resource requirements
type ResourceRequirements struct {
	CPU     float64 `json:"cpu"`
	Memory  int64   `json:"memory"`
	Disk    int64   `json:"disk"`
	Network int64   `json:"network"`
	GPU     int     `json:"gpu"`
}

// ResourceAvailability defines available system resources
type ResourceAvailability struct {
	CPU     float64 `json:"cpu"`
	Memory  int64   `json:"memory"`
	Disk    int64   `json:"disk"`
	Network int64   `json:"network"`
	GPU     int     `json:"gpu"`
	Nodes   int     `json:"nodes"`
}

// Constraint defines scheduling constraints
type Constraint struct {
	Type       string                 `json:"type"`
	Target     string                 `json:"target"`
	Operator   string                 `json:"operator"`
	Value      interface{}            `json:"value"`
	Priority   int                    `json:"priority"`
	Soft       bool                   `json:"soft"`
	Parameters map[string]interface{} `json:"parameters"`
}

// SLARequirement defines service level agreement requirements
type SLARequirement struct {
	JobID              string        `json:"job_id"`
	MaxRuntime         time.Duration `json:"max_runtime"`
	MinSuccessRate     float64       `json:"min_success_rate"`
	MaxRetries         int           `json:"max_retries"`
	AvailabilityTarget float64       `json:"availability_target"`
	ResponseTime       time.Duration `json:"response_time"`
	Priority           int           `json:"priority"`
}

// HistoricalJobData contains historical job execution data
type HistoricalJobData struct {
	JobExecutions      []JobExecution      `json:"job_executions"`
	ResourceUsage      []ResourceUsage     `json:"resource_usage"`
	PerformanceMetrics []PerformanceMetric `json:"performance_metrics"`
	Failures           []JobFailure        `json:"failures"`
	Trends             []Trend             `json:"trends"`
}

// JobExecution contains job execution information
type JobExecution struct {
	JobID      string        `json:"job_id"`
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	Duration   time.Duration `json:"duration"`
	Status     string        `json:"status"`
	Resources  ResourceUsage `json:"resources"`
	ExitCode   int           `json:"exit_code"`
	RetryCount int           `json:"retry_count"`
}

// PerformanceMetric contains performance metrics
type PerformanceMetric struct {
	JobID     string    `json:"job_id"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
	NodeID    string    `json:"node_id"`
}

// JobFailure contains job failure information
type JobFailure struct {
	JobID      string    `json:"job_id"`
	Timestamp  time.Time `json:"timestamp"`
	Reason     string    `json:"reason"`
	ErrorCode  int       `json:"error_code"`
	NodeID     string    `json:"node_id"`
	RetryCount int       `json:"retry_count"`
}

// SystemLoad contains current system load information
type SystemLoad struct {
	CPU        float64   `json:"cpu"`
	Memory     float64   `json:"memory"`
	Disk       float64   `json:"disk"`
	Network    float64   `json:"network"`
	ActiveJobs int       `json:"active_jobs"`
	QueuedJobs int       `json:"queued_jobs"`
	Timestamp  time.Time `json:"timestamp"`
}

// SchedulerOutput represents job scheduling optimization output
type SchedulerOutput struct {
	Schedule          OptimizedSchedule          `json:"schedule"`
	ResourcePlan      ResourcePlan               `json:"resource_plan"`
	Recommendations   []SchedulingRecommendation `json:"recommendations"`
	PredictedOutcomes []PredictedOutcome         `json:"predicted_outcomes"`
	Optimizations     []SchedulingOptimization   `json:"optimizations"`
	Actions           []SchedulingAction         `json:"actions"`
	Conflicts         []ConflictResolution       `json:"conflicts"`
	Warnings          []string                   `json:"warnings"`
}

// OptimizedSchedule contains the optimized job schedule
type OptimizedSchedule struct {
	TimeSlots     []TimeSlot            `json:"time_slots"`
	JobPlacements []JobPlacement        `json:"job_placements"`
	LoadBalancing LoadBalancingStrategy `json:"load_balancing"`
	TotalDuration time.Duration         `json:"total_duration"`
	Efficiency    float64               `json:"efficiency"`
	SLACompliance float64               `json:"sla_compliance"`
}

// TimeSlot represents a time slot in the schedule
type TimeSlot struct {
	Start     time.Time     `json:"start"`
	End       time.Time     `json:"end"`
	JobIDs    []string      `json:"job_ids"`
	Resources ResourceUsage `json:"resources"`
	Load      float64       `json:"load"`
	Priority  int           `json:"priority"`
}

// JobPlacement contains job placement information
type JobPlacement struct {
	JobID        string             `json:"job_id"`
	StartTime    time.Time          `json:"start_time"`
	EndTime      time.Time          `json:"end_time"`
	NodeID       string             `json:"node_id"`
	Resources    ResourceAllocation `json:"resources"`
	Priority     int                `json:"priority"`
	Dependencies []string           `json:"dependencies"`
	Constraints  []Constraint       `json:"constraints"`
}

// ResourceAllocation contains resource allocation information
type ResourceAllocation struct {
	CPU     float64 `json:"cpu"`
	Memory  int64   `json:"memory"`
	Disk    int64   `json:"disk"`
	Network int64   `json:"network"`
	GPU     int     `json:"gpu"`
	NodeID  string  `json:"node_id"`
}

// LoadBalancingStrategy defines load balancing strategy
type LoadBalancingStrategy struct {
	Type       string                 `json:"type"`
	Parameters map[string]interface{} `json:"parameters"`
	Nodes      []NodeAllocation       `json:"nodes"`
	Efficiency float64                `json:"efficiency"`
}

// NodeAllocation contains node allocation information
type NodeAllocation struct {
	NodeID      string        `json:"node_id"`
	JobIDs      []string      `json:"job_ids"`
	Resources   ResourceUsage `json:"resources"`
	Utilization float64       `json:"utilization"`
	Load        float64       `json:"load"`
}

// ResourcePlan contains resource allocation plan
type ResourcePlan struct {
	Allocations []ResourceAllocation `json:"allocations"`
	Utilization ResourceUtilization  `json:"utilization"`
	Scaling     ScalingPlan          `json:"scaling"`
	Cost        CostEstimate         `json:"cost"`
	Efficiency  float64              `json:"efficiency"`
}

// ScalingPlan contains scaling recommendations
type ScalingPlan struct {
	ScaleUp   []ScalingAction  `json:"scale_up"`
	ScaleDown []ScalingAction  `json:"scale_down"`
	Triggers  []ScalingTrigger `json:"triggers"`
}

// ScalingAction contains scaling action information
type ScalingAction struct {
	Type      string    `json:"type"`
	Resource  string    `json:"resource"`
	Amount    float64   `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
	Priority  int       `json:"priority"`
}

// ScalingTrigger contains scaling trigger information
type ScalingTrigger struct {
	Metric    string        `json:"metric"`
	Threshold float64       `json:"threshold"`
	Duration  time.Duration `json:"duration"`
	Action    string        `json:"action"`
}

// CostEstimate contains cost estimation
type CostEstimate struct {
	Total      float64            `json:"total"`
	ByResource map[string]float64 `json:"by_resource"`
	ByJob      map[string]float64 `json:"by_job"`
	Savings    float64            `json:"savings"`
}

// SchedulingRecommendation contains scheduling recommendations
type SchedulingRecommendation struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Impact      string                 `json:"impact"`
	Confidence  float64                `json:"confidence"`
	Priority    int                    `json:"priority"`
	Parameters  map[string]interface{} `json:"parameters"`
	Rationale   string                 `json:"rationale"`
}

// PredictedOutcome contains predicted scheduling outcomes
type PredictedOutcome struct {
	JobID              string        `json:"job_id"`
	StartTime          time.Time     `json:"start_time"`
	EndTime            time.Time     `json:"end_time"`
	SuccessProbability float64       `json:"success_probability"`
	ResourceUsage      ResourceUsage `json:"resource_usage"`
	SLACompliance      bool          `json:"sla_compliance"`
	Risks              []string      `json:"risks"`
}

// SchedulingOptimization contains optimization details
type SchedulingOptimization struct {
	Type        string                 `json:"type"`
	Target      string                 `json:"target"`
	Improvement float64                `json:"improvement"`
	Method      string                 `json:"method"`
	Parameters  map[string]interface{} `json:"parameters"`
	Confidence  float64                `json:"confidence"`
}

// SchedulingAction contains scheduling actions
type SchedulingAction struct {
	Type       string                 `json:"type"`
	Target     string                 `json:"target"`
	Parameters map[string]interface{} `json:"parameters"`
	Priority   int                    `json:"priority"`
	Condition  string                 `json:"condition"`
	Timeout    time.Duration          `json:"timeout"`
	Rollback   bool                   `json:"rollback"`
}

// ConflictResolution contains conflict resolution information
type ConflictResolution struct {
	ConflictType string                 `json:"conflict_type"`
	JobIDs       []string               `json:"job_ids"`
	Resolution   string                 `json:"resolution"`
	Parameters   map[string]interface{} `json:"parameters"`
	Impact       string                 `json:"impact"`
}

// NewSchedulerAgent creates a new job scheduler agent
func NewSchedulerAgent() ai.AIAgent {
	capabilities := []ai.Capability{
		{
			Name:        "intelligent-scheduling",
			Description: "Optimize job scheduling based on resource availability and constraints",
			InputType:   reflect.TypeOf(SchedulerInput{}),
			OutputType:  reflect.TypeOf(SchedulerOutput{}),
			Metadata: map[string]interface{}{
				"optimization_level": "high",
				"sla_compliance":     0.95,
			},
		},
		{
			Name:        "resource-optimization",
			Description: "Optimize resource allocation for scheduled jobs",
			InputType:   reflect.TypeOf(SchedulerInput{}),
			OutputType:  reflect.TypeOf(SchedulerOutput{}),
			Metadata: map[string]interface{}{
				"efficiency":   0.90,
				"cost_savings": 0.25,
			},
		},
		{
			Name:        "predictive-scheduling",
			Description: "Predict job execution outcomes and optimize accordingly",
			InputType:   reflect.TypeOf(SchedulerInput{}),
			OutputType:  reflect.TypeOf(SchedulerOutput{}),
			Metadata: map[string]interface{}{
				"accuracy":  0.88,
				"lookahead": "24h",
			},
		},
		{
			Name:        "load-balancing",
			Description: "Balance job loads across available resources",
			InputType:   reflect.TypeOf(SchedulerInput{}),
			OutputType:  reflect.TypeOf(SchedulerOutput{}),
			Metadata: map[string]interface{}{
				"balancing_efficiency": 0.92,
				"fairness":             "high",
			},
		},
	}

	baseAgent := ai.NewBaseAgent("job-scheduler", "Job Scheduler Agent", ai.AgentTypeJobScheduler, capabilities)

	return &SchedulerAgent{
		BaseAgent: baseAgent,
		resourceThresholds: SchedulerResourceThresholds{
			CPU:     0.80,
			Memory:  0.85,
			Disk:    0.90,
			Network: 0.75,
		},
		optimizationPolicies: []OptimizationPolicy{},
		schedulingStats:      SchedulingStats{OptimizationsByType: make(map[string]int64)},
		jobPredictions:       make(map[string]JobPrediction),
	}
}

// Initialize initializes the scheduler agent
func (a *SchedulerAgent) Initialize(ctx context.Context, config ai.AgentConfig) error {
	if err := a.BaseAgent.Initialize(ctx, config); err != nil {
		return err
	}

	// Initialize scheduler-specific configuration
	if schedulerConfig, ok := config.Metadata["scheduler"]; ok {
		if configMap, ok := schedulerConfig.(map[string]interface{}); ok {
			if thresholds, ok := configMap["resource_thresholds"].(map[string]interface{}); ok {
				if cpu, ok := thresholds["cpu"].(float64); ok {
					a.resourceThresholds.CPU = cpu
				}
				if memory, ok := thresholds["memory"].(float64); ok {
					a.resourceThresholds.Memory = memory
				}
				if disk, ok := thresholds["disk"].(float64); ok {
					a.resourceThresholds.Disk = disk
				}
				if network, ok := thresholds["network"].(float64); ok {
					a.resourceThresholds.Network = network
				}
			}
		}
	}

	// Initialize optimization policies
	a.optimizationPolicies = []OptimizationPolicy{
		{
			Name:       "priority-based",
			Type:       "priority",
			Weight:     0.4,
			Conditions: []string{"job_priority > 0"},
			Parameters: map[string]interface{}{
				"priority_weight": 0.6,
				"fairness_weight": 0.4,
			},
			Enabled:     true,
			SuccessRate: 0.0,
		},
		{
			Name:       "load-balancing",
			Type:       "load_balance",
			Weight:     0.3,
			Conditions: []string{"resource_utilization < 0.9"},
			Parameters: map[string]interface{}{
				"balance_threshold": 0.1,
				"migration_cost":    0.05,
			},
			Enabled:     true,
			SuccessRate: 0.0,
		},
		{
			Name:       "sla-optimization",
			Type:       "sla_optimize",
			Weight:     0.3,
			Conditions: []string{"sla_compliance < 0.95"},
			Parameters: map[string]interface{}{
				"compliance_target": 0.98,
				"penalty_weight":    0.2,
			},
			Enabled:     true,
			SuccessRate: 0.0,
		},
	}

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Info("scheduler agent initialized",
			logger.String("agent_id", a.ID()),
			logger.Float64("cpu_threshold", a.resourceThresholds.CPU),
			logger.Float64("memory_threshold", a.resourceThresholds.Memory),
			logger.Int("policies", len(a.optimizationPolicies)),
		)
	}

	return nil
}

// Process processes job scheduling optimization input
func (a *SchedulerAgent) Process(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
	startTime := time.Now()

	// Convert input to scheduler-specific input
	schedulerInput, ok := input.Data.(SchedulerInput)
	if !ok {
		return ai.AgentOutput{}, fmt.Errorf("invalid input type for scheduler agent")
	}

	// Analyze current system state
	analysis := a.analyzeSystemState(schedulerInput)

	// Generate job predictions
	predictions := a.generateJobPredictions(schedulerInput)

	// Optimize job schedule
	schedule := a.optimizeJobSchedule(schedulerInput, analysis, predictions)

	// Create resource plan
	resourcePlan := a.createResourcePlan(schedulerInput, schedule)

	// Generate recommendations
	recommendations := a.generateRecommendations(schedulerInput, analysis, schedule)

	// Predict outcomes
	outcomes := a.predictOutcomes(schedulerInput, schedule, predictions)

	// Create optimizations
	optimizations := a.createOptimizations(schedulerInput, schedule, analysis)

	// Resolve conflicts
	conflicts := a.resolveConflicts(schedulerInput, schedule)

	// Generate warnings
	warnings := a.generateWarnings(schedulerInput, schedule, analysis)

	// Create actions
	actions := a.createActions(recommendations, schedule, resourcePlan)

	// Create output
	output := SchedulerOutput{
		Schedule:          schedule,
		ResourcePlan:      resourcePlan,
		Recommendations:   recommendations,
		PredictedOutcomes: outcomes,
		Optimizations:     optimizations,
		Actions:           actions,
		Conflicts:         conflicts,
		Warnings:          warnings,
	}

	// Update statistics
	a.updateSchedulingStats(output)

	// Create agent output
	agentOutput := ai.AgentOutput{
		Type:        "job-scheduling",
		Data:        output,
		Confidence:  a.calculateConfidence(schedulerInput, analysis),
		Explanation: a.generateExplanation(output),
		Actions:     a.convertToAgentActions(actions),
		Metadata: map[string]interface{}{
			"processing_time":     time.Since(startTime),
			"jobs_scheduled":      len(schedule.JobPlacements),
			"sla_compliance":      schedule.SLACompliance,
			"resource_efficiency": resourcePlan.Efficiency,
		},
		Timestamp: time.Now(),
	}

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Debug("job scheduling processed",
			logger.String("agent_id", a.ID()),
			logger.String("request_id", input.RequestID),
			logger.Int("jobs_count", len(schedulerInput.Jobs)),
			logger.Float64("sla_compliance", schedule.SLACompliance),
			logger.Duration("processing_time", time.Since(startTime)),
		)
	}

	return agentOutput, nil
}

// analyzeSystemState analyzes the current system state
func (a *SchedulerAgent) analyzeSystemState(input SchedulerInput) SystemStateAnalysis {
	analysis := SystemStateAnalysis{
		ResourceUtilization: ResourceUtilization{
			CPU:     input.SystemLoad.CPU,
			Memory:  input.SystemLoad.Memory,
			Disk:    input.SystemLoad.Disk,
			Network: input.SystemLoad.Network,
			Overall: (input.SystemLoad.CPU + input.SystemLoad.Memory + input.SystemLoad.Disk + input.SystemLoad.Network) / 4,
		},
		JobQueue: JobQueueAnalysis{
			ActiveJobs:   input.SystemLoad.ActiveJobs,
			QueuedJobs:   input.SystemLoad.QueuedJobs,
			TotalJobs:    len(input.Jobs),
			HighPriority: 0,
			LowPriority:  0,
		},
		Bottlenecks: []string{},
		Trends:      []TrendAnalysis{},
	}

	// Analyze job priorities
	for _, job := range input.Jobs {
		if job.Priority > 5 {
			analysis.JobQueue.HighPriority++
		} else {
			analysis.JobQueue.LowPriority++
		}
	}

	// Identify bottlenecks
	if analysis.ResourceUtilization.CPU > a.resourceThresholds.CPU {
		analysis.Bottlenecks = append(analysis.Bottlenecks, "cpu")
	}
	if analysis.ResourceUtilization.Memory > a.resourceThresholds.Memory {
		analysis.Bottlenecks = append(analysis.Bottlenecks, "memory")
	}
	if analysis.ResourceUtilization.Disk > a.resourceThresholds.Disk {
		analysis.Bottlenecks = append(analysis.Bottlenecks, "disk")
	}
	if analysis.ResourceUtilization.Network > a.resourceThresholds.Network {
		analysis.Bottlenecks = append(analysis.Bottlenecks, "network")
	}

	// Analyze trends
	for _, trend := range input.HistoricalData.Trends {
		analysis.Trends = append(analysis.Trends, TrendAnalysis{
			Type:       trend.Type,
			Direction:  trend.Direction,
			Confidence: trend.Confidence,
			Impact:     trend.Impact,
		})
	}

	return analysis
}

// SystemStateAnalysis contains system state analysis
type SystemStateAnalysis struct {
	ResourceUtilization ResourceUtilization `json:"resource_utilization"`
	JobQueue            JobQueueAnalysis    `json:"job_queue"`
	Bottlenecks         []string            `json:"bottlenecks"`
	Trends              []TrendAnalysis     `json:"trends"`
}

// JobQueueAnalysis contains job queue analysis
type JobQueueAnalysis struct {
	ActiveJobs   int `json:"active_jobs"`
	QueuedJobs   int `json:"queued_jobs"`
	TotalJobs    int `json:"total_jobs"`
	HighPriority int `json:"high_priority"`
	LowPriority  int `json:"low_priority"`
}

// TrendAnalysis contains trend analysis
type TrendAnalysis struct {
	Type       string  `json:"type"`
	Direction  string  `json:"direction"`
	Confidence float64 `json:"confidence"`
	Impact     string  `json:"impact"`
}

// generateJobPredictions generates job execution predictions
func (a *SchedulerAgent) generateJobPredictions(input SchedulerInput) map[string]JobPrediction {
	predictions := make(map[string]JobPrediction)

	for _, job := range input.Jobs {
		prediction := JobPrediction{
			JobID:              job.ID,
			EstimatedRuntime:   a.predictJobRuntime(job, input.HistoricalData),
			ResourceUsage:      a.predictResourceUsage(job, input.HistoricalData),
			SuccessProbability: a.calculateSuccessProbability(job, input.HistoricalData),
			OptimalStartTime:   a.findOptimalStartTime(job, input),
			Dependencies:       job.Dependencies,
			Priority:           job.Priority,
			LastUpdated:        time.Now(),
		}

		predictions[job.ID] = prediction
		a.jobPredictions[job.ID] = prediction
	}

	return predictions
}

// predictJobRuntime predicts job execution runtime
func (a *SchedulerAgent) predictJobRuntime(job Job, historical HistoricalJobData) time.Duration {
	if job.EstimatedRuntime > 0 {
		return job.EstimatedRuntime
	}

	// Analyze historical executions
	var totalDuration time.Duration
	var count int

	for _, execution := range historical.JobExecutions {
		if execution.JobID == job.ID {
			totalDuration += execution.Duration
			count++
		}
	}

	if count > 0 {
		avgDuration := totalDuration / time.Duration(count)
		// Add 20% buffer for safety
		return time.Duration(float64(avgDuration) * 1.2)
	}

	// Default estimation based on job type
	switch job.Type {
	case "batch":
		return 30 * time.Minute
	case "etl":
		return 1 * time.Hour
	case "backup":
		return 2 * time.Hour
	case "report":
		return 15 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// predictResourceUsage predicts job resource usage
func (a *SchedulerAgent) predictResourceUsage(job Job, historical HistoricalJobData) ResourceUsage {
	// Start with job requirements
	usage := ResourceUsage{
		CPU:     job.ResourceRequirements.CPU,
		Memory:  float64(job.ResourceRequirements.Memory),
		Disk:    float64(job.ResourceRequirements.Disk),
		Network: float64(job.ResourceRequirements.Network),
	}

	// Adjust based on historical data
	var totalUsage ResourceUsage
	var count int

	for _, historical := range historical.ResourceUsage {
		// Assuming ResourceUsage has a JobID field (should be added)
		totalUsage.CPU += historical.CPU
		totalUsage.Memory += historical.Memory
		totalUsage.Disk += historical.Disk
		totalUsage.Network += historical.Network
		count++
	}

	if count > 0 {
		avgUsage := ResourceUsage{
			CPU:     totalUsage.CPU / float64(count),
			Memory:  totalUsage.Memory / float64(count),
			Disk:    totalUsage.Disk / float64(count),
			Network: totalUsage.Network / float64(count),
		}

		// Use historical average if available
		if avgUsage.CPU > 0 {
			usage.CPU = avgUsage.CPU
		}
		if avgUsage.Memory > 0 {
			usage.Memory = avgUsage.Memory
		}
		if avgUsage.Disk > 0 {
			usage.Disk = avgUsage.Disk
		}
		if avgUsage.Network > 0 {
			usage.Network = avgUsage.Network
		}
	}

	return usage
}

// calculateSuccessProbability calculates job success probability
func (a *SchedulerAgent) calculateSuccessProbability(job Job, historical HistoricalJobData) float64 {
	if job.SuccessRate > 0 {
		return job.SuccessRate
	}

	// Analyze historical success rate
	var successCount, totalCount int

	for _, execution := range historical.JobExecutions {
		if execution.JobID == job.ID {
			totalCount++
			if execution.Status == "success" {
				successCount++
			}
		}
	}

	if totalCount > 0 {
		return float64(successCount) / float64(totalCount)
	}

	// Default success probability based on job type
	switch job.Type {
	case "batch":
		return 0.95
	case "etl":
		return 0.90
	case "backup":
		return 0.98
	case "report":
		return 0.92
	default:
		return 0.85
	}
}

// findOptimalStartTime finds the optimal start time for a job
func (a *SchedulerAgent) findOptimalStartTime(job Job, input SchedulerInput) time.Time {
	// Consider resource availability and constraints
	startTime := input.TimeWindow.StartTime

	// Check resource requirements
	if job.ResourceRequirements.CPU > input.Resources.CPU*0.8 {
		// Schedule during off-peak hours
		if startTime.Hour() >= 8 && startTime.Hour() <= 18 {
			// Move to night time
			startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 22, 0, 0, 0, startTime.Location())
		}
	}

	// Consider dependencies
	if len(job.Dependencies) > 0 {
		// Add buffer time for dependencies
		startTime = startTime.Add(30 * time.Minute)
	}

	// Consider deadline
	if job.Deadline != nil {
		estimatedRuntime := a.predictJobRuntime(job, input.HistoricalData)
		if startTime.Add(estimatedRuntime).After(*job.Deadline) {
			// Schedule earlier to meet deadline
			startTime = job.Deadline.Add(-estimatedRuntime).Add(-10 * time.Minute)
		}
	}

	return startTime
}

// optimizeJobSchedule optimizes the job schedule
func (a *SchedulerAgent) optimizeJobSchedule(input SchedulerInput, analysis SystemStateAnalysis, predictions map[string]JobPrediction) OptimizedSchedule {
	// Sort jobs by priority and dependencies
	sortedJobs := a.sortJobsByPriority(input.Jobs)

	// Create time slots
	timeSlots := a.createTimeSlots(input.TimeWindow, 30*time.Minute)

	// Place jobs in time slots
	jobPlacements := a.placeJobs(sortedJobs, timeSlots, input.Resources, predictions)

	// Create load balancing strategy
	loadBalancing := a.createLoadBalancingStrategy(jobPlacements, input.Resources)

	// Calculate metrics
	totalDuration := a.calculateTotalDuration(jobPlacements)
	efficiency := a.calculateScheduleEfficiency(jobPlacements, input.Resources)
	slaCompliance := a.calculateSLACompliance(jobPlacements, input.SLARequirements)

	return OptimizedSchedule{
		TimeSlots:     timeSlots,
		JobPlacements: jobPlacements,
		LoadBalancing: loadBalancing,
		TotalDuration: totalDuration,
		Efficiency:    efficiency,
		SLACompliance: slaCompliance,
	}
}

// sortJobsByPriority sorts jobs by priority and dependencies
func (a *SchedulerAgent) sortJobsByPriority(jobs []Job) []Job {
	sortedJobs := make([]Job, len(jobs))
	copy(sortedJobs, jobs)

	sort.Slice(sortedJobs, func(i, j int) bool {
		// First sort by priority
		if sortedJobs[i].Priority != sortedJobs[j].Priority {
			return sortedJobs[i].Priority > sortedJobs[j].Priority
		}

		// Then by number of dependencies (fewer dependencies first)
		if len(sortedJobs[i].Dependencies) != len(sortedJobs[j].Dependencies) {
			return len(sortedJobs[i].Dependencies) < len(sortedJobs[j].Dependencies)
		}

		// Finally by estimated runtime (shorter jobs first)
		return sortedJobs[i].EstimatedRuntime < sortedJobs[j].EstimatedRuntime
	})

	return sortedJobs
}

// createTimeSlots creates time slots for scheduling
func (a *SchedulerAgent) createTimeSlots(timeWindow TimeWindow, slotDuration time.Duration) []TimeSlot {
	slots := []TimeSlot{}

	current := timeWindow.StartTime
	slotIndex := 0

	for current.Before(timeWindow.EndTime) {
		end := current.Add(slotDuration)
		if end.After(timeWindow.EndTime) {
			end = timeWindow.EndTime
		}

		slot := TimeSlot{
			Start:     current,
			End:       end,
			JobIDs:    []string{},
			Resources: ResourceUsage{},
			Load:      0.0,
			Priority:  slotIndex,
		}

		slots = append(slots, slot)
		current = end
		slotIndex++
	}

	return slots
}

// placeJobs places jobs in time slots
func (a *SchedulerAgent) placeJobs(jobs []Job, timeSlots []TimeSlot, resources ResourceAvailability, predictions map[string]JobPrediction) []JobPlacement {
	placements := []JobPlacement{}
	resourceUsage := make(map[int]ResourceUsage) // Usage per time slot

	for _, job := range jobs {
		placement := a.findBestPlacement(job, timeSlots, resources, resourceUsage, predictions)
		if placement != nil {
			placements = append(placements, *placement)

			// Update resource usage
			startSlot := a.findTimeSlotIndex(timeSlots, placement.StartTime)
			endSlot := a.findTimeSlotIndex(timeSlots, placement.EndTime)

			for i := startSlot; i <= endSlot; i++ {
				if i < len(timeSlots) {
					usage := resourceUsage[i]
					usage.CPU += placement.Resources.CPU
					usage.Memory += float64(placement.Resources.Memory)
					usage.Disk += float64(placement.Resources.Disk)
					usage.Network += float64(placement.Resources.Network)
					resourceUsage[i] = usage
				}
			}
		}
	}

	return placements
}

// findBestPlacement finds the best placement for a job
func (a *SchedulerAgent) findBestPlacement(job Job, timeSlots []TimeSlot, resources ResourceAvailability, resourceUsage map[int]ResourceUsage, predictions map[string]JobPrediction) *JobPlacement {
	prediction, exists := predictions[job.ID]
	if !exists {
		return nil
	}

	// Find available time slots
	for i, slot := range timeSlots {
		// Check if slot has enough resources
		currentUsage := resourceUsage[i]
		if a.canFitJob(job, currentUsage, resources) {
			// Check dependencies
			if a.areDependenciesMet(job, slot.Start, resourceUsage) {
				// Create placement
				placement := &JobPlacement{
					JobID:     job.ID,
					StartTime: slot.Start,
					EndTime:   slot.Start.Add(prediction.EstimatedRuntime),
					NodeID:    "auto", // Let system decide
					Resources: ResourceAllocation{
						CPU:     job.ResourceRequirements.CPU,
						Memory:  job.ResourceRequirements.Memory,
						Disk:    job.ResourceRequirements.Disk,
						Network: job.ResourceRequirements.Network,
						GPU:     job.ResourceRequirements.GPU,
					},
					Priority:     job.Priority,
					Dependencies: job.Dependencies,
					Constraints:  []Constraint{},
				}

				return placement
			}
		}
	}

	return nil
}

// canFitJob checks if a job can fit in the current resource usage
func (a *SchedulerAgent) canFitJob(job Job, currentUsage ResourceUsage, resources ResourceAvailability) bool {
	if currentUsage.CPU+job.ResourceRequirements.CPU > resources.CPU {
		return false
	}
	if currentUsage.Memory+float64(job.ResourceRequirements.Memory) > float64(resources.Memory) {
		return false
	}
	if currentUsage.Disk+float64(job.ResourceRequirements.Disk) > float64(resources.Disk) {
		return false
	}
	if currentUsage.Network+float64(job.ResourceRequirements.Network) > float64(resources.Network) {
		return false
	}

	return true
}

// areDependenciesMet checks if job dependencies are met
func (a *SchedulerAgent) areDependenciesMet(job Job, startTime time.Time, resourceUsage map[int]ResourceUsage) bool {
	// Simplified dependency check
	// In a real implementation, this would check if dependent jobs are scheduled before this time
	return len(job.Dependencies) == 0 || startTime.After(time.Now().Add(time.Hour))
}

// findTimeSlotIndex finds the time slot index for a given time
func (a *SchedulerAgent) findTimeSlotIndex(timeSlots []TimeSlot, t time.Time) int {
	for i, slot := range timeSlots {
		if t.Before(slot.End) {
			return i
		}
	}
	return len(timeSlots) - 1
}

// createLoadBalancingStrategy creates a load balancing strategy
func (a *SchedulerAgent) createLoadBalancingStrategy(placements []JobPlacement, resources ResourceAvailability) LoadBalancingStrategy {
	// Simple round-robin node allocation
	nodeCount := resources.Nodes
	if nodeCount == 0 {
		nodeCount = 1
	}

	nodes := make([]NodeAllocation, nodeCount)
	for i := range nodes {
		nodes[i] = NodeAllocation{
			NodeID:      fmt.Sprintf("node-%d", i),
			JobIDs:      []string{},
			Resources:   ResourceUsage{},
			Utilization: 0.0,
			Load:        0.0,
		}
	}

	// Distribute jobs across nodes
	for i, placement := range placements {
		nodeIndex := i % nodeCount
		nodes[nodeIndex].JobIDs = append(nodes[nodeIndex].JobIDs, placement.JobID)
		nodes[nodeIndex].Resources.CPU += placement.Resources.CPU
		nodes[nodeIndex].Resources.Memory += float64(placement.Resources.Memory)
		nodes[nodeIndex].Resources.Disk += float64(placement.Resources.Disk)
		nodes[nodeIndex].Resources.Network += float64(placement.Resources.Network)
	}

	// Calculate utilization
	for i := range nodes {
		nodes[i].Utilization = nodes[i].Resources.CPU / (resources.CPU / float64(nodeCount))
		nodes[i].Load = nodes[i].Utilization
	}

	// Calculate efficiency
	totalUtilization := 0.0
	for _, node := range nodes {
		totalUtilization += node.Utilization
	}
	efficiency := totalUtilization / float64(nodeCount)

	return LoadBalancingStrategy{
		Type:       "round-robin",
		Parameters: map[string]interface{}{"node_count": nodeCount},
		Nodes:      nodes,
		Efficiency: efficiency,
	}
}

// calculateTotalDuration calculates total schedule duration
func (a *SchedulerAgent) calculateTotalDuration(placements []JobPlacement) time.Duration {
	if len(placements) == 0 {
		return 0
	}

	var earliestStart, latestEnd time.Time
	for i, placement := range placements {
		if i == 0 {
			earliestStart = placement.StartTime
			latestEnd = placement.EndTime
		} else {
			if placement.StartTime.Before(earliestStart) {
				earliestStart = placement.StartTime
			}
			if placement.EndTime.After(latestEnd) {
				latestEnd = placement.EndTime
			}
		}
	}

	return latestEnd.Sub(earliestStart)
}

// calculateScheduleEfficiency calculates schedule efficiency
func (a *SchedulerAgent) calculateScheduleEfficiency(placements []JobPlacement, resources ResourceAvailability) float64 {
	if len(placements) == 0 {
		return 0.0
	}

	// Calculate resource utilization
	totalCPU := 0.0
	totalMemory := 0.0
	totalDisk := 0.0
	totalNetwork := 0.0

	for _, placement := range placements {
		totalCPU += placement.Resources.CPU
		totalMemory += float64(placement.Resources.Memory)
		totalDisk += float64(placement.Resources.Disk)
		totalNetwork += float64(placement.Resources.Network)
	}

	// Calculate efficiency as percentage of resource utilization
	cpuEfficiency := totalCPU / resources.CPU
	memoryEfficiency := totalMemory / float64(resources.Memory)
	diskEfficiency := totalDisk / float64(resources.Disk)
	networkEfficiency := totalNetwork / float64(resources.Network)

	// Average efficiency
	efficiency := (cpuEfficiency + memoryEfficiency + diskEfficiency + networkEfficiency) / 4.0

	// Cap at 100%
	if efficiency > 1.0 {
		efficiency = 1.0
	}

	return efficiency
}

// calculateSLACompliance calculates SLA compliance
func (a *SchedulerAgent) calculateSLACompliance(placements []JobPlacement, slaRequirements []SLARequirement) float64 {
	if len(slaRequirements) == 0 {
		return 1.0
	}

	compliantJobs := 0
	totalJobs := len(slaRequirements)

	for _, sla := range slaRequirements {
		// Find corresponding placement
		for _, placement := range placements {
			if placement.JobID == sla.JobID {
				// Check if runtime meets SLA
				runtime := placement.EndTime.Sub(placement.StartTime)
				if runtime <= sla.MaxRuntime {
					compliantJobs++
				}
				break
			}
		}
	}

	return float64(compliantJobs) / float64(totalJobs)
}

// createResourcePlan creates a resource allocation plan
func (a *SchedulerAgent) createResourcePlan(input SchedulerInput, schedule OptimizedSchedule) ResourcePlan {
	// Calculate total resource allocations
	allocations := []ResourceAllocation{}

	for _, placement := range schedule.JobPlacements {
		allocations = append(allocations, placement.Resources)
	}

	// Calculate utilization
	utilization := ResourceUtilization{
		CPU:     0.0,
		Memory:  0.0,
		Disk:    0.0,
		Network: 0.0,
		GPU:     0.0,
		Overall: 0.0,
	}

	for _, allocation := range allocations {
		utilization.CPU += allocation.CPU
		utilization.Memory += float64(allocation.Memory)
		utilization.Disk += float64(allocation.Disk)
		utilization.Network += float64(allocation.Network)
		utilization.GPU += float64(allocation.GPU)
	}

	// Normalize by available resources
	utilization.CPU /= input.Resources.CPU
	utilization.Memory /= float64(input.Resources.Memory)
	utilization.Disk /= float64(input.Resources.Disk)
	utilization.Network /= float64(input.Resources.Network)
	if input.Resources.GPU > 0 {
		utilization.GPU /= float64(input.Resources.GPU)
	}

	utilization.Overall = (utilization.CPU + utilization.Memory + utilization.Disk + utilization.Network) / 4.0

	// Create scaling plan
	scalingPlan := a.createScalingPlan(utilization, input.Resources)

	// Estimate costs
	costEstimate := a.estimateCosts(allocations, schedule.TotalDuration)

	return ResourcePlan{
		Allocations: allocations,
		Utilization: utilization,
		Scaling:     scalingPlan,
		Cost:        costEstimate,
		Efficiency:  schedule.Efficiency,
	}
}

// createScalingPlan creates a scaling plan
func (a *SchedulerAgent) createScalingPlan(utilization ResourceUtilization, resources ResourceAvailability) ScalingPlan {
	scaleUp := []ScalingAction{}
	scaleDown := []ScalingAction{}

	// Check if scaling is needed
	if utilization.CPU > 0.9 {
		scaleUp = append(scaleUp, ScalingAction{
			Type:      "scale_up",
			Resource:  "cpu",
			Amount:    resources.CPU * 0.5,
			Timestamp: time.Now(),
			Reason:    "High CPU utilization",
			Priority:  1,
		})
	}

	if utilization.Memory > 0.9 {
		scaleUp = append(scaleUp, ScalingAction{
			Type:      "scale_up",
			Resource:  "memory",
			Amount:    float64(resources.Memory) * 0.5,
			Timestamp: time.Now(),
			Reason:    "High memory utilization",
			Priority:  1,
		})
	}

	if utilization.Overall < 0.3 {
		scaleDown = append(scaleDown, ScalingAction{
			Type:      "scale_down",
			Resource:  "overall",
			Amount:    0.2,
			Timestamp: time.Now(),
			Reason:    "Low overall utilization",
			Priority:  2,
		})
	}

	// Create scaling triggers
	triggers := []ScalingTrigger{
		{
			Metric:    "cpu_utilization",
			Threshold: 0.9,
			Duration:  5 * time.Minute,
			Action:    "scale_up",
		},
		{
			Metric:    "memory_utilization",
			Threshold: 0.9,
			Duration:  5 * time.Minute,
			Action:    "scale_up",
		},
		{
			Metric:    "overall_utilization",
			Threshold: 0.3,
			Duration:  15 * time.Minute,
			Action:    "scale_down",
		},
	}

	return ScalingPlan{
		ScaleUp:   scaleUp,
		ScaleDown: scaleDown,
		Triggers:  triggers,
	}
}

// estimateCosts estimates resource costs
func (a *SchedulerAgent) estimateCosts(allocations []ResourceAllocation, duration time.Duration) CostEstimate {
	// Simple cost estimation (would be more sophisticated in real implementation)
	cpuCost := 0.0
	memoryCost := 0.0
	diskCost := 0.0
	networkCost := 0.0

	hours := duration.Hours()

	// Cost per hour per unit
	cpuRate := 0.10     // $0.10 per CPU hour
	memoryRate := 0.01  // $0.01 per GB hour
	diskRate := 0.001   // $0.001 per GB hour
	networkRate := 0.05 // $0.05 per GB hour

	byJob := make(map[string]float64)

	for _, allocation := range allocations {
		jobCPUCost := allocation.CPU * cpuRate * hours
		jobMemoryCost := float64(allocation.Memory) / 1024 / 1024 / 1024 * memoryRate * hours
		jobDiskCost := float64(allocation.Disk) / 1024 / 1024 / 1024 * diskRate * hours
		jobNetworkCost := float64(allocation.Network) / 1024 / 1024 / 1024 * networkRate * hours

		jobTotal := jobCPUCost + jobMemoryCost + jobDiskCost + jobNetworkCost
		byJob[allocation.NodeID] = jobTotal

		cpuCost += jobCPUCost
		memoryCost += jobMemoryCost
		diskCost += jobDiskCost
		networkCost += jobNetworkCost
	}

	total := cpuCost + memoryCost + diskCost + networkCost

	return CostEstimate{
		Total: total,
		ByResource: map[string]float64{
			"cpu":     cpuCost,
			"memory":  memoryCost,
			"disk":    diskCost,
			"network": networkCost,
		},
		ByJob:   byJob,
		Savings: 0.0, // Would calculate based on optimization
	}
}

// generateRecommendations generates scheduling recommendations
func (a *SchedulerAgent) generateRecommendations(input SchedulerInput, analysis SystemStateAnalysis, schedule OptimizedSchedule) []SchedulingRecommendation {
	recommendations := []SchedulingRecommendation{}

	// Resource optimization recommendations
	if analysis.ResourceUtilization.Overall < 0.5 {
		recommendations = append(recommendations, SchedulingRecommendation{
			Type:        "resource-optimization",
			Description: "Consider reducing allocated resources to optimize costs",
			Impact:      "medium",
			Confidence:  0.80,
			Priority:    2,
			Parameters: map[string]interface{}{
				"suggested_reduction": 0.3,
				"estimated_savings":   0.25,
			},
			Rationale: "Low resource utilization indicates over-provisioning",
		})
	}

	// SLA compliance recommendations
	if schedule.SLACompliance < 0.95 {
		recommendations = append(recommendations, SchedulingRecommendation{
			Type:        "sla-improvement",
			Description: "Adjust job scheduling to improve SLA compliance",
			Impact:      "high",
			Confidence:  0.85,
			Priority:    1,
			Parameters: map[string]interface{}{
				"target_compliance": 0.98,
				"buffer_time":       "15m",
			},
			Rationale: "SLA compliance is below target threshold",
		})
	}

	// Load balancing recommendations
	if schedule.LoadBalancing.Efficiency < 0.8 {
		recommendations = append(recommendations, SchedulingRecommendation{
			Type:        "load-balancing",
			Description: "Improve load balancing across nodes",
			Impact:      "medium",
			Confidence:  0.75,
			Priority:    3,
			Parameters: map[string]interface{}{
				"strategy": "weighted-round-robin",
				"weights":  map[string]float64{"cpu": 0.4, "memory": 0.4, "disk": 0.2},
			},
			Rationale: "Load balancing efficiency is below optimal",
		})
	}

	// Bottleneck recommendations
	for _, bottleneck := range analysis.Bottlenecks {
		recommendations = append(recommendations, SchedulingRecommendation{
			Type:        "bottleneck-resolution",
			Description: fmt.Sprintf("Address %s bottleneck", bottleneck),
			Impact:      "high",
			Confidence:  0.90,
			Priority:    1,
			Parameters: map[string]interface{}{
				"resource":  bottleneck,
				"threshold": a.getResourceThreshold(bottleneck),
				"current":   a.getCurrentResourceUsage(bottleneck, analysis),
			},
			Rationale: fmt.Sprintf("%s utilization exceeds threshold", bottleneck),
		})
	}

	return recommendations
}

// getResourceThreshold gets the threshold for a resource
func (a *SchedulerAgent) getResourceThreshold(resource string) float64 {
	switch resource {
	case "cpu":
		return a.resourceThresholds.CPU
	case "memory":
		return a.resourceThresholds.Memory
	case "disk":
		return a.resourceThresholds.Disk
	case "network":
		return a.resourceThresholds.Network
	default:
		return 0.8
	}
}

// getCurrentResourceUsage gets current resource usage
func (a *SchedulerAgent) getCurrentResourceUsage(resource string, analysis SystemStateAnalysis) float64 {
	switch resource {
	case "cpu":
		return analysis.ResourceUtilization.CPU
	case "memory":
		return analysis.ResourceUtilization.Memory
	case "disk":
		return analysis.ResourceUtilization.Disk
	case "network":
		return analysis.ResourceUtilization.Network
	default:
		return 0.0
	}
}

// predictOutcomes predicts scheduling outcomes
func (a *SchedulerAgent) predictOutcomes(input SchedulerInput, schedule OptimizedSchedule, predictions map[string]JobPrediction) []PredictedOutcome {
	outcomes := []PredictedOutcome{}

	for _, placement := range schedule.JobPlacements {
		prediction, exists := predictions[placement.JobID]
		if !exists {
			continue
		}

		// Find SLA requirement
		var slaCompliant bool = true
		for _, sla := range input.SLARequirements {
			if sla.JobID == placement.JobID {
				runtime := placement.EndTime.Sub(placement.StartTime)
				slaCompliant = runtime <= sla.MaxRuntime
				break
			}
		}

		// Identify risks
		risks := []string{}
		if prediction.SuccessProbability < 0.9 {
			risks = append(risks, "low-success-probability")
		}
		if placement.Resources.CPU > 0.8 {
			risks = append(risks, "high-cpu-usage")
		}
		if len(placement.Dependencies) > 0 {
			risks = append(risks, "dependency-risk")
		}

		outcome := PredictedOutcome{
			JobID:              placement.JobID,
			StartTime:          placement.StartTime,
			EndTime:            placement.EndTime,
			SuccessProbability: prediction.SuccessProbability,
			ResourceUsage:      prediction.ResourceUsage,
			SLACompliance:      slaCompliant,
			Risks:              risks,
		}

		outcomes = append(outcomes, outcome)
	}

	return outcomes
}

// createOptimizations creates optimization details
func (a *SchedulerAgent) createOptimizations(input SchedulerInput, schedule OptimizedSchedule, analysis SystemStateAnalysis) []SchedulingOptimization {
	optimizations := []SchedulingOptimization{}

	// Efficiency optimization
	if schedule.Efficiency > 0.8 {
		optimizations = append(optimizations, SchedulingOptimization{
			Type:        "efficiency",
			Target:      "overall",
			Improvement: schedule.Efficiency - 0.5, // Assuming baseline of 0.5
			Method:      "intelligent-placement",
			Parameters: map[string]interface{}{
				"algorithm":      "priority-based",
				"load_balancing": true,
			},
			Confidence: 0.85,
		})
	}

	// SLA optimization
	if schedule.SLACompliance > 0.95 {
		optimizations = append(optimizations, SchedulingOptimization{
			Type:        "sla",
			Target:      "compliance",
			Improvement: schedule.SLACompliance - 0.9, // Assuming baseline of 0.9
			Method:      "deadline-aware-scheduling",
			Parameters: map[string]interface{}{
				"buffer_time":        "15m",
				"priority_weighting": 0.7,
			},
			Confidence: 0.90,
		})
	}

	// Resource optimization
	if analysis.ResourceUtilization.Overall < 0.9 {
		optimizations = append(optimizations, SchedulingOptimization{
			Type:        "resource",
			Target:      "utilization",
			Improvement: 0.9 - analysis.ResourceUtilization.Overall,
			Method:      "resource-aware-placement",
			Parameters: map[string]interface{}{
				"utilization_target":      0.85,
				"fragmentation_avoidance": true,
			},
			Confidence: 0.80,
		})
	}

	return optimizations
}

// resolveConflicts resolves scheduling conflicts
func (a *SchedulerAgent) resolveConflicts(input SchedulerInput, schedule OptimizedSchedule) []ConflictResolution {
	conflicts := []ConflictResolution{}

	// Check for resource conflicts
	resourceConflicts := a.checkResourceConflicts(schedule.JobPlacements)
	for _, conflict := range resourceConflicts {
		resolution := ConflictResolution{
			ConflictType: "resource",
			JobIDs:       conflict.JobIDs,
			Resolution:   "reschedule-lower-priority",
			Parameters: map[string]interface{}{
				"delay":                 "30m",
				"alternative_resources": true,
			},
			Impact: "medium",
		}
		conflicts = append(conflicts, resolution)
	}

	// Check for dependency conflicts
	dependencyConflicts := a.checkDependencyConflicts(schedule.JobPlacements)
	for _, conflict := range dependencyConflicts {
		resolution := ConflictResolution{
			ConflictType: "dependency",
			JobIDs:       conflict.JobIDs,
			Resolution:   "adjust-start-time",
			Parameters: map[string]interface{}{
				"dependency_buffer":  "10m",
				"parallel_execution": false,
			},
			Impact: "high",
		}
		conflicts = append(conflicts, resolution)
	}

	// Check for SLA conflicts
	slaConflicts := a.checkSLAConflicts(schedule.JobPlacements, input.SLARequirements)
	for _, conflict := range slaConflicts {
		resolution := ConflictResolution{
			ConflictType: "sla",
			JobIDs:       conflict.JobIDs,
			Resolution:   "increase-priority",
			Parameters: map[string]interface{}{
				"priority_boost":       2,
				"resource_reservation": true,
			},
			Impact: "high",
		}
		conflicts = append(conflicts, resolution)
	}

	return conflicts
}

// checkResourceConflicts checks for resource conflicts
func (a *SchedulerAgent) checkResourceConflicts(placements []JobPlacement) []ResourceConflict {
	conflicts := []ResourceConflict{}

	// Group placements by time and check for resource over-allocation
	timeGroups := make(map[string][]JobPlacement)

	for _, placement := range placements {
		timeKey := placement.StartTime.Format("2006-01-02T15:04")
		timeGroups[timeKey] = append(timeGroups[timeKey], placement)
	}

	for timeKey, group := range timeGroups {
		if len(group) > 1 {
			// Check if total resource requirements exceed availability
			totalCPU := 0.0
			totalMemory := int64(0)
			jobIDs := []string{}

			for _, placement := range group {
				totalCPU += placement.Resources.CPU
				totalMemory += placement.Resources.Memory
				jobIDs = append(jobIDs, placement.JobID)
			}

			// Simple conflict detection (would be more sophisticated in real implementation)
			if totalCPU > 1.0 || totalMemory > 8*1024*1024*1024 { // 8GB
				conflicts = append(conflicts, ResourceConflict{
					TimeKey: timeKey,
					JobIDs:  jobIDs,
					Type:    "over-allocation",
				})
			}
		}
	}

	return conflicts
}

// ResourceConflict represents a resource conflict
type ResourceConflict struct {
	TimeKey string   `json:"time_key"`
	JobIDs  []string `json:"job_ids"`
	Type    string   `json:"type"`
}

// checkDependencyConflicts checks for dependency conflicts
func (a *SchedulerAgent) checkDependencyConflicts(placements []JobPlacement) []DependencyConflict {
	conflicts := []DependencyConflict{}

	// Create job timing map
	jobTiming := make(map[string]JobPlacement)
	for _, placement := range placements {
		jobTiming[placement.JobID] = placement
	}

	// Check dependency ordering
	for _, placement := range placements {
		for _, depID := range placement.Dependencies {
			if depPlacement, exists := jobTiming[depID]; exists {
				if depPlacement.EndTime.After(placement.StartTime) {
					conflicts = append(conflicts, DependencyConflict{
						JobID:        placement.JobID,
						DependencyID: depID,
						Type:         "ordering",
						JobIDs:       []string{placement.JobID, depID},
					})
				}
			}
		}
	}

	return conflicts
}

// DependencyConflict represents a dependency conflict
type DependencyConflict struct {
	JobID        string   `json:"job_id"`
	DependencyID string   `json:"dependency_id"`
	Type         string   `json:"type"`
	JobIDs       []string `json:"job_ids"`
}

// checkSLAConflicts checks for SLA conflicts
func (a *SchedulerAgent) checkSLAConflicts(placements []JobPlacement, slaRequirements []SLARequirement) []SLAConflict {
	conflicts := []SLAConflict{}

	for _, sla := range slaRequirements {
		for _, placement := range placements {
			if placement.JobID == sla.JobID {
				runtime := placement.EndTime.Sub(placement.StartTime)
				if runtime > sla.MaxRuntime {
					conflicts = append(conflicts, SLAConflict{
						JobID:         placement.JobID,
						Type:          "runtime-exceeded",
						MaxRuntime:    sla.MaxRuntime,
						ActualRuntime: runtime,
						JobIDs:        []string{placement.JobID},
					})
				}
			}
		}
	}

	return conflicts
}

// SLAConflict represents an SLA conflict
type SLAConflict struct {
	JobID         string        `json:"job_id"`
	Type          string        `json:"type"`
	MaxRuntime    time.Duration `json:"max_runtime"`
	ActualRuntime time.Duration `json:"actual_runtime"`
	JobIDs        []string      `json:"job_ids"`
}

// generateWarnings generates scheduling warnings
func (a *SchedulerAgent) generateWarnings(input SchedulerInput, schedule OptimizedSchedule, analysis SystemStateAnalysis) []string {
	warnings := []string{}

	// High resource utilization warning
	if analysis.ResourceUtilization.Overall > 0.95 {
		warnings = append(warnings, "System resource utilization is very high (>95%)")
	}

	// Low SLA compliance warning
	if schedule.SLACompliance < 0.90 {
		warnings = append(warnings, fmt.Sprintf("SLA compliance is below 90%% (%.1f%%)", schedule.SLACompliance*100))
	}

	// Job queue overflow warning
	if len(input.Jobs) > 100 {
		warnings = append(warnings, fmt.Sprintf("Large job queue detected (%d jobs)", len(input.Jobs)))
	}

	// Dependency chain warning
	maxDepth := a.calculateMaxDependencyDepth(input.Jobs)
	if maxDepth > 5 {
		warnings = append(warnings, fmt.Sprintf("Deep dependency chain detected (depth: %d)", maxDepth))
	}

	// Resource fragmentation warning
	if a.detectResourceFragmentation(schedule.JobPlacements) {
		warnings = append(warnings, "Resource fragmentation detected")
	}

	return warnings
}

// calculateMaxDependencyDepth calculates maximum dependency depth
func (a *SchedulerAgent) calculateMaxDependencyDepth(jobs []Job) int {
	maxDepth := 0

	for _, job := range jobs {
		depth := a.calculateJobDependencyDepth(job.ID, jobs, make(map[string]bool))
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	return maxDepth
}

// calculateJobDependencyDepth calculates dependency depth for a job
func (a *SchedulerAgent) calculateJobDependencyDepth(jobID string, jobs []Job, visited map[string]bool) int {
	if visited[jobID] {
		return 0 // Circular dependency protection
	}

	visited[jobID] = true

	// Find job
	var job *Job
	for _, j := range jobs {
		if j.ID == jobID {
			job = &j
			break
		}
	}

	if job == nil || len(job.Dependencies) == 0 {
		return 0
	}

	maxDepth := 0
	for _, depID := range job.Dependencies {
		depth := a.calculateJobDependencyDepth(depID, jobs, visited)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	return maxDepth + 1
}

// detectResourceFragmentation detects resource fragmentation
func (a *SchedulerAgent) detectResourceFragmentation(placements []JobPlacement) bool {
	// Simple fragmentation detection - could be more sophisticated
	// Check if there are many small allocations
	smallAllocations := 0
	for _, placement := range placements {
		if placement.Resources.CPU < 0.5 && placement.Resources.Memory < 1024*1024*1024 {
			smallAllocations++
		}
	}

	return smallAllocations > len(placements)/2
}

// createActions creates scheduling actions
func (a *SchedulerAgent) createActions(recommendations []SchedulingRecommendation, schedule OptimizedSchedule, resourcePlan ResourcePlan) []SchedulingAction {
	actions := []SchedulingAction{}

	// Create actions from recommendations
	for _, rec := range recommendations {
		action := SchedulingAction{
			Type:       rec.Type,
			Target:     "scheduler",
			Parameters: rec.Parameters,
			Priority:   rec.Priority,
			Condition:  fmt.Sprintf("confidence > 0.7"),
			Timeout:    5 * time.Minute,
			Rollback:   true,
		}
		actions = append(actions, action)
	}

	// Add scheduling action
	actions = append(actions, SchedulingAction{
		Type:   "apply-schedule",
		Target: "cron-manager",
		Parameters: map[string]interface{}{
			"schedule":   schedule,
			"placements": schedule.JobPlacements,
			"efficiency": schedule.Efficiency,
		},
		Priority:  1,
		Condition: "sla_compliance > 0.9",
		Timeout:   10 * time.Minute,
		Rollback:  true,
	})

	// Add resource scaling actions
	for _, scaleAction := range resourcePlan.Scaling.ScaleUp {
		actions = append(actions, SchedulingAction{
			Type:   "scale-up",
			Target: "resource-manager",
			Parameters: map[string]interface{}{
				"resource": scaleAction.Resource,
				"amount":   scaleAction.Amount,
				"reason":   scaleAction.Reason,
			},
			Priority:  scaleAction.Priority,
			Condition: "resource_utilization > 0.9",
			Timeout:   15 * time.Minute,
			Rollback:  true,
		})
	}

	for _, scaleAction := range resourcePlan.Scaling.ScaleDown {
		actions = append(actions, SchedulingAction{
			Type:   "scale-down",
			Target: "resource-manager",
			Parameters: map[string]interface{}{
				"resource": scaleAction.Resource,
				"amount":   scaleAction.Amount,
				"reason":   scaleAction.Reason,
			},
			Priority:  scaleAction.Priority,
			Condition: "resource_utilization < 0.3",
			Timeout:   15 * time.Minute,
			Rollback:  true,
		})
	}

	return actions
}

// calculateConfidence calculates overall confidence in scheduling
func (a *SchedulerAgent) calculateConfidence(input SchedulerInput, analysis SystemStateAnalysis) float64 {
	confidence := 0.5 // Base confidence

	// Increase confidence based on data quality
	if len(input.Jobs) > 0 && len(input.HistoricalData.JobExecutions) > 10 {
		confidence += 0.2
	}

	// Increase confidence based on system stability
	if analysis.ResourceUtilization.Overall < 0.8 {
		confidence += 0.1
	}

	// Increase confidence based on SLA requirements clarity
	if len(input.SLARequirements) > 0 {
		confidence += 0.1
	}

	// Decrease confidence based on bottlenecks
	confidence -= float64(len(analysis.Bottlenecks)) * 0.05

	// Increase confidence based on trend predictability
	predictableTrends := 0
	for _, trend := range analysis.Trends {
		if trend.Confidence > 0.8 {
			predictableTrends++
		}
	}
	confidence += float64(predictableTrends) * 0.02

	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// generateExplanation generates human-readable explanation
func (a *SchedulerAgent) generateExplanation(output SchedulerOutput) string {
	explanation := fmt.Sprintf("Job scheduling optimization completed. ")

	if len(output.Schedule.JobPlacements) > 0 {
		explanation += fmt.Sprintf("Successfully scheduled %d jobs with %.1f%% efficiency. ",
			len(output.Schedule.JobPlacements), output.Schedule.Efficiency*100)
	}

	if output.Schedule.SLACompliance > 0 {
		explanation += fmt.Sprintf("SLA compliance: %.1f%%. ", output.Schedule.SLACompliance*100)
	}

	if len(output.Recommendations) > 0 {
		explanation += fmt.Sprintf("Generated %d optimization recommendations. ", len(output.Recommendations))
	}

	if len(output.Conflicts) > 0 {
		explanation += fmt.Sprintf("Resolved %d scheduling conflicts. ", len(output.Conflicts))
	}

	if len(output.Warnings) > 0 {
		explanation += fmt.Sprintf("Issued %d warnings for attention. ", len(output.Warnings))
	}

	explanation += fmt.Sprintf("Resource efficiency: %.1f%%. ", output.ResourcePlan.Efficiency*100)
	explanation += fmt.Sprintf("Estimated cost: $%.2f.", output.ResourcePlan.Cost.Total)

	return explanation
}

// convertToAgentActions converts scheduling actions to agent actions
func (a *SchedulerAgent) convertToAgentActions(schedulingActions []SchedulingAction) []ai.AgentAction {
	actions := []ai.AgentAction{}

	for _, schedulingAction := range schedulingActions {
		action := ai.AgentAction{
			Type:       schedulingAction.Type,
			Target:     schedulingAction.Target,
			Parameters: schedulingAction.Parameters,
			Priority:   schedulingAction.Priority,
			Condition:  schedulingAction.Condition,
			Timeout:    schedulingAction.Timeout,
		}
		actions = append(actions, action)
	}

	return actions
}

// updateSchedulingStats updates scheduling statistics
func (a *SchedulerAgent) updateSchedulingStats(output SchedulerOutput) {
	a.schedulingStats.TotalOptimizations++
	a.schedulingStats.LastOptimization = time.Now()

	if output.Schedule.SLACompliance > 0.9 {
		a.schedulingStats.SuccessfulSchedules++
	} else {
		a.schedulingStats.FailedSchedules++
	}

	a.schedulingStats.SLAComplianceRate = output.Schedule.SLACompliance
	a.schedulingStats.ResourceSavings += output.ResourcePlan.Cost.Savings

	// Update optimizations by type
	for _, optimization := range output.Optimizations {
		a.schedulingStats.OptimizationsByType[optimization.Type]++
	}
}

// Learn learns from scheduling feedback
func (a *SchedulerAgent) Learn(ctx context.Context, feedback ai.AgentFeedback) error {
	if err := a.BaseAgent.Learn(ctx, feedback); err != nil {
		return err
	}

	// Update optimization policy success rates
	if policyType, ok := feedback.Context["policy_type"].(string); ok {
		for i, policy := range a.optimizationPolicies {
			if policy.Type == policyType {
				if feedback.Success {
					a.optimizationPolicies[i].SuccessRate = a.optimizationPolicies[i].SuccessRate*0.9 + 0.1
				} else {
					a.optimizationPolicies[i].SuccessRate = a.optimizationPolicies[i].SuccessRate * 0.9
				}
				a.optimizationPolicies[i].LastUsed = time.Now()
				break
			}
		}
	}

	// Adjust resource thresholds based on feedback
	if resourceType, ok := feedback.Context["resource_type"].(string); ok {
		if _, ok := feedback.Metrics["utilization"]; ok {
			adjustment := 0.01
			if !feedback.Success {
				adjustment = -0.01
			}

			switch resourceType {
			case "cpu":
				a.resourceThresholds.CPU = math.Max(0.5, math.Min(0.95, a.resourceThresholds.CPU+adjustment))
			case "memory":
				a.resourceThresholds.Memory = math.Max(0.5, math.Min(0.95, a.resourceThresholds.Memory+adjustment))
			case "disk":
				a.resourceThresholds.Disk = math.Max(0.5, math.Min(0.95, a.resourceThresholds.Disk+adjustment))
			case "network":
				a.resourceThresholds.Network = math.Max(0.5, math.Min(0.95, a.resourceThresholds.Network+adjustment))
			}
		}
	}

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Debug("scheduler agent learned from feedback",
			logger.String("agent_id", a.ID()),
			logger.String("action_id", feedback.ActionID),
			logger.Bool("success", feedback.Success),
			logger.Float64("cpu_threshold", a.resourceThresholds.CPU),
			logger.Float64("memory_threshold", a.resourceThresholds.Memory),
		)
	}

	return nil
}

// GetSchedulingStats returns scheduling statistics
func (a *SchedulerAgent) GetSchedulingStats() SchedulingStats {
	return a.schedulingStats
}

// GetOptimizationPolicies returns optimization policies
func (a *SchedulerAgent) GetOptimizationPolicies() []OptimizationPolicy {
	return a.optimizationPolicies
}

// GetJobPredictions returns job predictions
func (a *SchedulerAgent) GetJobPredictions() map[string]JobPrediction {
	return a.jobPredictions
}

// UpdateOptimizationPolicy updates an optimization policy
func (a *SchedulerAgent) UpdateOptimizationPolicy(name string, policy OptimizationPolicy) error {
	for i, p := range a.optimizationPolicies {
		if p.Name == name {
			a.optimizationPolicies[i] = policy
			return nil
		}
	}
	return fmt.Errorf("optimization policy %s not found", name)
}

// GetSchedulerResourceThresholds returns resource thresholds
func (a *SchedulerAgent) GetSchedulerResourceThresholds() SchedulerResourceThresholds {
	return a.resourceThresholds
}

// SetSchedulerResourceThresholds sets resource thresholds
func (a *SchedulerAgent) SetSchedulerResourceThresholds(thresholds SchedulerResourceThresholds) {
	a.resourceThresholds = thresholds
}
