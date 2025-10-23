package framework

import (
	"context"
	"fmt"

	"github.com/xraph/forge/v0/internal/services"
)

// FrameworkIntegrator handles integration of Forge with existing frameworks
type FrameworkIntegrator struct {
	analyzer *FrameworkAnalyzer
}

// NewFrameworkIntegrator creates a new framework integrator
func NewFrameworkIntegrator() *FrameworkIntegrator {
	return &FrameworkIntegrator{
		analyzer: NewFrameworkAnalyzer(),
	}
}

// IntegrateFramework integrates Forge with an existing framework
func (fi *FrameworkIntegrator) IntegrateFramework(ctx context.Context, config services.IntegrationConfig) (*services.IntegrationResult, error) {
	result := &services.IntegrationResult{
		Framework:       config.Framework,
		Mode:            config.Mode,
		ModifiedFiles:   make([]string, 0),
		EnabledFeatures: make([]string, 0),
	}

	// Analyze existing project
	analysis, err := fi.analyzer.AnalyzeProject(ctx, config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze project: %w", err)
	}

	// Generate integration plan
	plan, err := fi.generateIntegrationPlan(analysis, config)
	if err != nil {
		return nil, fmt.Errorf("failed to generate integration plan: %w", err)
	}

	// Execute integration steps
	for _, step := range plan.Steps {
		if config.OnProgress != nil {
			config.OnProgress(step.Description, step.Order, len(plan.Steps))
		}

		stepResult, err := fi.executeIntegrationStep(step, config)
		if err != nil {
			return nil, fmt.Errorf("integration step failed: %w", err)
		}

		result.ModifiedFiles = append(result.ModifiedFiles, stepResult.ModifiedFiles...)
		result.EnabledFeatures = append(result.EnabledFeatures, stepResult.EnabledFeatures...)
	}

	return result, nil
}

// generateIntegrationPlan generates an integration plan
func (fi *FrameworkIntegrator) generateIntegrationPlan(analysis *services.FrameworkAnalysisResult, config services.IntegrationConfig) (*services.IntegrationPlan, error) {
	plan := &services.IntegrationPlan{
		Steps: make([]services.IntegrationStep, 0),
	}

	// Add steps based on requested features
	for _, feature := range config.Features {
		switch feature {
		case "di":
			plan.Steps = append(plan.Steps, services.IntegrationStep{
				Order:       len(plan.Steps) + 1,
				Description: "Integrate dependency injection",
				Type:        "di",
				Feature:     feature,
			})
		case "middleware":
			plan.Steps = append(plan.Steps, services.IntegrationStep{
				Order:       len(plan.Steps) + 1,
				Description: "Integrate Forge middleware",
				Type:        "middleware",
				Feature:     feature,
			})
		case "health":
			plan.Steps = append(plan.Steps, services.IntegrationStep{
				Order:       len(plan.Steps) + 1,
				Description: "Add health checks",
				Type:        "health",
				Feature:     feature,
			})
		case "metrics":
			plan.Steps = append(plan.Steps, services.IntegrationStep{
				Order:       len(plan.Steps) + 1,
				Description: "Add metrics collection",
				Type:        "metrics",
				Feature:     feature,
			})
		}
	}

	return plan, nil
}

// executeIntegrationStep executes an integration step
func (fi *FrameworkIntegrator) executeIntegrationStep(step services.IntegrationStep, config services.IntegrationConfig) (*services.IntegrationStepResult, error) {
	result := &services.IntegrationStepResult{
		ModifiedFiles:   make([]string, 0),
		EnabledFeatures: []string{step.Feature},
	}

	switch step.Type {
	case "di":
		files, err := fi.integrateDI(config)
		if err != nil {
			return nil, err
		}
		result.ModifiedFiles = files
	case "middleware":
		files, err := fi.integrateMiddleware(config)
		if err != nil {
			return nil, err
		}
		result.ModifiedFiles = files
	case "health":
		files, err := fi.integrateHealth(config)
		if err != nil {
			return nil, err
		}
		result.ModifiedFiles = files
	case "metrics":
		files, err := fi.integrateMetrics(config)
		if err != nil {
			return nil, err
		}
		result.ModifiedFiles = files
	}

	return result, nil
}

// integrateDI integrates dependency injection
func (fi *FrameworkIntegrator) integrateDI(config services.IntegrationConfig) ([]string, error) {
	// Implementation would integrate DI container
	return []string{"main.go", "internal/di/container.go"}, nil
}

// integrateMiddleware integrates Forge middleware
func (fi *FrameworkIntegrator) integrateMiddleware(config services.IntegrationConfig) ([]string, error) {
	// Implementation would integrate middleware
	return []string{"internal/middleware/forge.go"}, nil
}

// integrateHealth integrates health checks
func (fi *FrameworkIntegrator) integrateHealth(config services.IntegrationConfig) ([]string, error) {
	// Implementation would integrate health checks
	return []string{"internal/health/health.go"}, nil
}

// integrateMetrics integrates metrics collection
func (fi *FrameworkIntegrator) integrateMetrics(config services.IntegrationConfig) ([]string, error) {
	// Implementation would integrate metrics
	return []string{"internal/metrics/metrics.go"}, nil
}
