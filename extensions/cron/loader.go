package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"gopkg.in/yaml.v3"
)

// JobsConfig represents a jobs configuration file.
type JobsConfig struct {
	Jobs []JobConfig `json:"jobs" yaml:"jobs"`
}

// JobConfig represents a job configuration from YAML/JSON.
type JobConfig struct {
	ID          string                 `json:"id" yaml:"id"`
	Name        string                 `json:"name" yaml:"name"`
	Schedule    string                 `json:"schedule" yaml:"schedule"`
	HandlerName string                 `json:"handlerName,omitempty" yaml:"handlerName,omitempty"`
	Handler     string                 `json:"handler,omitempty" yaml:"handler,omitempty"` // Alias for HandlerName
	Command     string                 `json:"command,omitempty" yaml:"command,omitempty"`
	Args        []string               `json:"args,omitempty" yaml:"args,omitempty"`
	Env         []string               `json:"env,omitempty" yaml:"env,omitempty"`
	WorkingDir  string                 `json:"workingDir,omitempty" yaml:"workingDir,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty" yaml:"payload,omitempty"`
	Enabled     bool                   `json:"enabled" yaml:"enabled"`
	Timezone    string                 `json:"timezone,omitempty" yaml:"timezone,omitempty"`
	MaxRetries  int                    `json:"maxRetries" yaml:"maxRetries"`
	Timeout     string                 `json:"timeout" yaml:"timeout"` // Duration string
	Metadata    map[string]string      `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Tags        []string               `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// JobLoader loads jobs from configuration files.
type JobLoader struct {
	logger   forge.Logger
	registry *JobRegistry
}

// NewJobLoader creates a new job loader.
func NewJobLoader(logger forge.Logger, registry *JobRegistry) *JobLoader {
	return &JobLoader{
		logger:   logger,
		registry: registry,
	}
}

// LoadFromFile loads jobs from a YAML or JSON file.
func (l *JobLoader) LoadFromFile(ctx context.Context, filePath string) ([]*Job, error) {
	l.logger.Info("loading jobs from file", forge.F("file", filePath))

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Determine format based on extension
	ext := filepath.Ext(filePath)
	var jobsConfig JobsConfig

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &jobsConfig); err != nil {
			return nil, fmt.Errorf("failed to parse YAML: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &jobsConfig); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported file format: %s (expected .yaml, .yml, or .json)", ext)
	}

	// Convert to Job objects
	jobs := make([]*Job, 0, len(jobsConfig.Jobs))
	for i, jobConfig := range jobsConfig.Jobs {
		job, err := l.convertJobConfig(jobConfig)
		if err != nil {
			l.logger.Error("failed to convert job config",
				forge.F("index", i),
				forge.F("id", jobConfig.ID),
				forge.F("error", err),
			)
			continue
		}
		jobs = append(jobs, job)
	}

	l.logger.Info("loaded jobs from file",
		forge.F("file", filePath),
		forge.F("count", len(jobs)),
	)

	return jobs, nil
}

// convertJobConfig converts a JobConfig to a Job.
func (l *JobLoader) convertJobConfig(config JobConfig) (*Job, error) {
	// Generate ID if not provided
	if config.ID == "" {
		config.ID = uuid.New().String()
	}

	// Use Handler alias if HandlerName is empty
	if config.HandlerName == "" && config.Handler != "" {
		config.HandlerName = config.Handler
	}

	// Validate job type
	hasHandler := config.HandlerName != ""
	hasCommand := config.Command != ""

	if !hasHandler && !hasCommand {
		return nil, fmt.Errorf("job must have either handlerName or command")
	}

	// Parse timezone
	var location *time.Location
	if config.Timezone != "" {
		loc, err := time.LoadLocation(config.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone '%s': %w", config.Timezone, err)
		}
		location = loc
	}

	// Parse timeout
	var timeout time.Duration
	if config.Timeout != "" {
		d, err := time.ParseDuration(config.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout '%s': %w", config.Timeout, err)
		}
		timeout = d
	}

	// Attach handler if it's a named handler
	var handler JobHandler
	if config.HandlerName != "" {
		h, err := l.registry.Get(config.HandlerName)
		if err != nil {
			l.logger.Warn("handler not found, will be resolved at runtime",
				forge.F("handler_name", config.HandlerName),
			)
		} else {
			handler = h
		}
	}

	// Create job
	now := time.Now()
	job := &Job{
		ID:          config.ID,
		Name:        config.Name,
		Schedule:    config.Schedule,
		Handler:     handler,
		HandlerName: config.HandlerName,
		Command:     config.Command,
		Args:        config.Args,
		Env:         config.Env,
		WorkingDir:  config.WorkingDir,
		Payload:     config.Payload,
		Enabled:     config.Enabled,
		Timezone:    location,
		MaxRetries:  config.MaxRetries,
		Timeout:     timeout,
		Metadata:    config.Metadata,
		Tags:        config.Tags,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	return job, nil
}

// LoadFromYAML loads jobs from YAML data.
func (l *JobLoader) LoadFromYAML(ctx context.Context, data []byte) ([]*Job, error) {
	var jobsConfig JobsConfig
	if err := yaml.Unmarshal(data, &jobsConfig); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	jobs := make([]*Job, 0, len(jobsConfig.Jobs))
	for i, jobConfig := range jobsConfig.Jobs {
		job, err := l.convertJobConfig(jobConfig)
		if err != nil {
			l.logger.Error("failed to convert job config",
				forge.F("index", i),
				forge.F("id", jobConfig.ID),
				forge.F("error", err),
			)
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// LoadFromJSON loads jobs from JSON data.
func (l *JobLoader) LoadFromJSON(ctx context.Context, data []byte) ([]*Job, error) {
	var jobsConfig JobsConfig
	if err := json.Unmarshal(data, &jobsConfig); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	jobs := make([]*Job, 0, len(jobsConfig.Jobs))
	for i, jobConfig := range jobsConfig.Jobs {
		job, err := l.convertJobConfig(jobConfig)
		if err != nil {
			l.logger.Error("failed to convert job config",
				forge.F("index", i),
				forge.F("id", jobConfig.ID),
				forge.F("error", err),
			)
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// ExportToFile exports jobs to a YAML or JSON file.
func (l *JobLoader) ExportToFile(ctx context.Context, jobs []*Job, filePath string) error {
	l.logger.Info("exporting jobs to file",
		forge.F("file", filePath),
		forge.F("count", len(jobs)),
	)

	// Convert to JobConfig
	jobConfigs := make([]JobConfig, 0, len(jobs))
	for _, job := range jobs {
		config := l.convertJobToConfig(job)
		jobConfigs = append(jobConfigs, config)
	}

	jobsConfig := JobsConfig{
		Jobs: jobConfigs,
	}

	// Determine format based on extension
	ext := filepath.Ext(filePath)
	var data []byte
	var err error

	switch ext {
	case ".yaml", ".yml":
		data, err = yaml.Marshal(jobsConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
	case ".json":
		data, err = json.MarshalIndent(jobsConfig, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
	default:
		return fmt.Errorf("unsupported file format: %s (expected .yaml, .yml, or .json)", ext)
	}

	// Write file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	l.logger.Info("exported jobs to file",
		forge.F("file", filePath),
		forge.F("count", len(jobs)),
	)

	return nil
}

// convertJobToConfig converts a Job to JobConfig.
func (l *JobLoader) convertJobToConfig(job *Job) JobConfig {
	var timezone string
	if job.Timezone != nil {
		timezone = job.Timezone.String()
	}

	var timeout string
	if job.Timeout > 0 {
		timeout = job.Timeout.String()
	}

	return JobConfig{
		ID:          job.ID,
		Name:        job.Name,
		Schedule:    job.Schedule,
		HandlerName: job.HandlerName,
		Command:     job.Command,
		Args:        job.Args,
		Env:         job.Env,
		WorkingDir:  job.WorkingDir,
		Payload:     job.Payload,
		Enabled:     job.Enabled,
		Timezone:    timezone,
		MaxRetries:  job.MaxRetries,
		Timeout:     timeout,
		Metadata:    job.Metadata,
		Tags:        job.Tags,
	}
}

// ValidateJobConfig validates a job configuration.
func (l *JobLoader) ValidateJobConfig(config JobConfig) error {
	if config.Name == "" {
		return fmt.Errorf("job name is required")
	}

	if config.Schedule == "" {
		return fmt.Errorf("job schedule is required")
	}

	hasHandler := config.HandlerName != "" || config.Handler != ""
	hasCommand := config.Command != ""

	if !hasHandler && !hasCommand {
		return fmt.Errorf("job must have either handlerName or command")
	}

	if config.Timezone != "" {
		if _, err := time.LoadLocation(config.Timezone); err != nil {
			return fmt.Errorf("invalid timezone '%s': %w", config.Timezone, err)
		}
	}

	if config.Timeout != "" {
		if _, err := time.ParseDuration(config.Timeout); err != nil {
			return fmt.Errorf("invalid timeout '%s': %w", config.Timeout, err)
		}
	}

	return nil
}
