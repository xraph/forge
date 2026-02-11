//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/training"
)

// TrainingPipelineExample demonstrates multi-stage training workflows.
// This example shows:
// - Creating a multi-stage training pipeline
// - Defining stage dependencies
// - Executing pipeline with error handling
// - Using pipeline templates
// - Monitoring pipeline progress
func main() {
	fmt.Println("=== Training Pipeline Example ===\n")

	ctx := context.Background()

	// Setup
	logger := forge.NewLogger(forge.LoggerConfig{Level: "info"})
	metrics := forge.NewMetrics(forge.MetricsConfig{Enabled: true})

	// Create pipeline manager
	pipelineManager := training.NewPipelineManager(logger, metrics)
	fmt.Println("✓ Pipeline manager created")

	// Example 1: Basic Pipeline
	fmt.Println("\n--- Example 1: Basic Training Pipeline ---")
	basicPipelineExample(ctx, pipelineManager)

	// Example 2: Advanced Pipeline with Conditionals
	fmt.Println("\n--- Example 2: Advanced Pipeline with Conditionals ---")
	advancedPipelineExample(ctx, pipelineManager)

	// Example 3: Pipeline Templates
	fmt.Println("\n--- Example 3: Pipeline Templates ---")
	templatePipelineExample(ctx, pipelineManager)

	// Example 4: Scheduled Pipeline
	fmt.Println("\n--- Example 4: Scheduled Pipeline ---")
	scheduledPipelineExample(ctx, pipelineManager)

	fmt.Println("\n=== Pipeline Examples Complete ===")
}

// basicPipelineExample shows a simple linear pipeline
func basicPipelineExample(ctx context.Context, manager training.PipelineManager) {
	// Define pipeline configuration
	config := training.PipelineConfig{
		ID:          "basic-ml-pipeline",
		Name:        "Basic ML Training Pipeline",
		Description: "Data preparation → Training → Validation → Deployment",
		Version:     "1.0",
		Tags: map[string]string{
			"type":        "training",
			"environment": "development",
		},
		Stages: []training.StageConfig{
			{
				ID:   "data-prep",
				Name: "Data Preparation",
				Type: training.StageTypeDataPreparation,
				Parameters: map[string]any{
					"source":      "s3://my-bucket/raw-data",
					"output_path": "./prepared-data",
					"clean":       true,
					"normalize":   true,
				},
				Resources: training.ResourceConfig{
					CPU:     "2",
					Memory:  "4Gi",
					Timeout: 30 * time.Minute,
				},
				Retry: training.RetryConfig{
					Enabled:     true,
					MaxAttempts: 3,
					BackoffType: "exponential",
					Delay:       10 * time.Second,
					MaxDelay:    5 * time.Minute,
				},
			},
			{
				ID:           "data-validation",
				Name:         "Data Validation",
				Type:         training.StageTypeDataValidation,
				Dependencies: []string{"data-prep"},
				Parameters: map[string]any{
					"schema_path":      "./schemas/customer-schema.json",
					"quality_checks":   true,
					"min_completeness": 0.95,
				},
				Resources: training.ResourceConfig{
					CPU:     "1",
					Memory:  "2Gi",
					Timeout: 10 * time.Minute,
				},
			},
			{
				ID:           "feature-extraction",
				Name:         "Feature Engineering",
				Type:         training.StageTypeFeatureExtraction,
				Dependencies: []string{"data-validation"},
				Parameters: map[string]any{
					"feature_set": "v1",
					"techniques":  []string{"polynomial", "interaction", "encoding"},
					"target_var":  "churn",
				},
				Resources: training.ResourceConfig{
					CPU:     "4",
					Memory:  "8Gi",
					Timeout: 1 * time.Hour,
				},
			},
			{
				ID:           "model-training",
				Name:         "Model Training",
				Type:         training.StageTypeModelTraining,
				Dependencies: []string{"feature-extraction"},
				Parameters: map[string]any{
					"model_type":    "xgboost",
					"epochs":        100,
					"batch_size":    32,
					"learning_rate": 0.01,
					"early_stopping": map[string]any{
						"patience": 10,
						"metric":   "val_loss",
					},
				},
				Resources: training.ResourceConfig{
					CPU:        "8",
					Memory:     "16Gi",
					GPU:        1,
					Priority:   2,
					MaxRuntime: 4 * time.Hour,
				},
			},
			{
				ID:           "model-validation",
				Name:         "Model Validation",
				Type:         training.StageTypeModelValidation,
				Dependencies: []string{"model-training"},
				Parameters: map[string]any{
					"test_set": "test_split",
					"metrics":  []string{"accuracy", "precision", "recall", "f1", "auc"},
					"threshold": map[string]float64{
						"accuracy": 0.80,
						"auc":      0.75,
					},
				},
			},
			{
				ID:           "model-deployment",
				Name:         "Model Deployment",
				Type:         training.StageTypeModelDeployment,
				Dependencies: []string{"model-validation"},
				Parameters: map[string]any{
					"environment": "staging",
					"endpoint":    "/api/predict/churn",
					"version":     "v1",
				},
			},
		},
		Parameters: map[string]any{
			"project":       "customer-retention",
			"experiment_id": "exp-001",
		},
		Resources: training.ResourceConfig{
			Priority: 1,
		},
		Timeouts: training.TimeoutConfig{
			Pipeline: 8 * time.Hour,
			Stages: map[string]time.Duration{
				"model-training": 4 * time.Hour,
				"data-prep":      30 * time.Minute,
			},
		},
	}

	// Create pipeline
	pipeline, err := manager.CreatePipeline(config)
	if err != nil {
		log.Fatalf("Failed to create pipeline: %v", err)
	}

	fmt.Printf("✓ Pipeline created: %s\n", pipeline.ID())
	fmt.Printf("  Stages: %d\n", len(pipeline.GetStages()))
	fmt.Printf("  Status: %s\n", pipeline.Status())

	// Execute pipeline
	fmt.Println("\nExecuting pipeline...")

	input := training.PipelineInput{
		Data: map[string]any{
			"dataset_id": "customer-churn-2024",
			"version":    "1.0",
		},
		Parameters: map[string]any{
			"force_retrain":      false,
			"notify_on_complete": true,
		},
		Context: map[string]any{
			"user_id":    "analyst-123",
			"request_id": "req-456",
		},
	}

	output, err := pipeline.Execute(ctx, input)
	if err != nil {
		log.Printf("Pipeline execution failed: %v", err)
	} else {
		fmt.Println("\n✓ Pipeline completed successfully!")
		fmt.Printf("  Status: %s\n", output.Status)
		fmt.Printf("  Artifacts: %d\n", len(output.Artifacts))
		fmt.Printf("  Metrics: %v\n", output.Metrics)
	}

	// Get pipeline metrics
	pipelineMetrics := pipeline.GetMetrics()
	fmt.Printf("\nPipeline Metrics:\n")
	fmt.Printf("  Total Executions: %d\n", pipelineMetrics.TotalExecutions)
	fmt.Printf("  Success Rate:     %.1f%%\n", pipelineMetrics.SuccessRate*100)
	fmt.Printf("  Average Duration: %v\n", pipelineMetrics.AverageDuration)
}

// advancedPipelineExample shows conditional stages and parallel execution
func advancedPipelineExample(ctx context.Context, manager training.PipelineManager) {
	config := training.PipelineConfig{
		ID:          "advanced-ml-pipeline",
		Name:        "Advanced ML Pipeline with Conditionals",
		Description: "Pipeline with conditional stages and parallel execution",
		Stages: []training.StageConfig{
			{
				ID:   "data-quality-check",
				Name: "Data Quality Assessment",
				Type: training.StageTypeDataValidation,
				Parameters: map[string]any{
					"min_quality_score": 0.9,
				},
			},
			{
				ID:           "data-cleaning",
				Name:         "Data Cleaning",
				Type:         training.StageTypeDataPreparation,
				Dependencies: []string{"data-quality-check"},
				Conditional: training.ConditionalConfig{
					Condition: "data_quality_score < 0.95",
					OnTrue: map[string]any{
						"intensive_cleaning": true,
					},
					OnFalse: map[string]any{
						"intensive_cleaning": false,
					},
				},
			},
			{
				ID:           "feature-selection",
				Name:         "Feature Selection",
				Type:         training.StageTypeFeatureExtraction,
				Dependencies: []string{"data-cleaning"},
				Parameters: map[string]any{
					"method":    "mutual_info",
					"top_k":     50,
					"threshold": 0.01,
				},
			},
			// Parallel model training stages
			{
				ID:           "train-xgboost",
				Name:         "Train XGBoost Model",
				Type:         training.StageTypeModelTraining,
				Dependencies: []string{"feature-selection"},
				Parameters: map[string]any{
					"model_type":   "xgboost",
					"n_estimators": 100,
				},
			},
			{
				ID:           "train-random-forest",
				Name:         "Train Random Forest Model",
				Type:         training.StageTypeModelTraining,
				Dependencies: []string{"feature-selection"},
				Parameters: map[string]any{
					"model_type":   "random_forest",
					"n_estimators": 200,
				},
			},
			{
				ID:           "train-neural-net",
				Name:         "Train Neural Network",
				Type:         training.StageTypeModelTraining,
				Dependencies: []string{"feature-selection"},
				Parameters: map[string]any{
					"model_type": "neural_network",
					"layers":     []int{128, 64, 32},
				},
			},
			// Model ensemble/selection
			{
				ID:           "model-selection",
				Name:         "Select Best Model",
				Type:         training.StageTypeModelEvaluation,
				Dependencies: []string{"train-xgboost", "train-random-forest", "train-neural-net"},
				Parameters: map[string]any{
					"metric":   "auc",
					"strategy": "best_single",
				},
			},
		},
	}

	pipeline, err := manager.CreatePipeline(config)
	if err != nil {
		log.Fatalf("Failed to create advanced pipeline: %v", err)
	}

	fmt.Printf("✓ Advanced pipeline created: %s\n", pipeline.ID())
	fmt.Printf("  Parallel stages: 3 (train-xgboost, train-random-forest, train-neural-net)\n")
	fmt.Printf("  Conditional stages: 1 (data-cleaning)\n")
}

// templatePipelineExample shows using pipeline templates
func templatePipelineExample(ctx context.Context, manager training.PipelineManager) {
	// Create a reusable template
	template := training.PipelineTemplate{
		ID:          "classification-template",
		Name:        "Standard Classification Pipeline",
		Description: "Reusable template for binary classification tasks",
		Version:     "1.0",
		Category:    "classification",
		Tags:        []string{"supervised", "classification", "standard"},
		Config: training.PipelineConfig{
			ID:   "{{pipeline_id}}",
			Name: "{{pipeline_name}}",
			Stages: []training.StageConfig{
				{
					ID:   "load-data",
					Type: training.StageTypeDataPreparation,
					Parameters: map[string]any{
						"source": "{{data_source}}",
					},
				},
				{
					ID:           "train",
					Type:         training.StageTypeModelTraining,
					Dependencies: []string{"load-data"},
					Parameters: map[string]any{
						"model_type": "{{model_type}}",
						"epochs":     "{{epochs}}",
					},
				},
				{
					ID:           "validate",
					Type:         training.StageTypeModelValidation,
					Dependencies: []string{"train"},
				},
			},
		},
		Parameters: map[string]training.Parameter{
			"pipeline_id": {
				Name:        "pipeline_id",
				Type:        "string",
				Description: "Unique pipeline identifier",
				Required:    true,
			},
			"pipeline_name": {
				Name:        "pipeline_name",
				Type:        "string",
				Description: "Pipeline display name",
				Required:    true,
			},
			"data_source": {
				Name:        "data_source",
				Type:        "string",
				Description: "Data source path or URL",
				Required:    true,
			},
			"model_type": {
				Name:        "model_type",
				Type:        "string",
				Description: "Type of model to train",
				Default:     "xgboost",
				Required:    false,
			},
			"epochs": {
				Name:        "epochs",
				Type:        "int",
				Description: "Number of training epochs",
				Default:     100,
				Required:    false,
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Register template
	if err := manager.CreateTemplate(template); err != nil {
		log.Fatalf("Failed to create template: %v", err)
	}

	fmt.Printf("✓ Template created: %s\n", template.ID)

	// Use template to create pipeline
	pipeline, err := manager.CreateFromTemplate("classification-template", map[string]any{
		"pipeline_id":   "fraud-detection-pipeline",
		"pipeline_name": "Fraud Detection Model",
		"data_source":   "s3://fraud-data/transactions",
		"model_type":    "random_forest",
		"epochs":        50,
	})
	if err != nil {
		log.Fatalf("Failed to create pipeline from template: %v", err)
	}

	fmt.Printf("✓ Pipeline created from template: %s\n", pipeline.ID())

	// List all templates
	templates := manager.ListTemplates()
	fmt.Printf("\nAvailable Templates: %d\n", len(templates))
	for _, t := range templates {
		fmt.Printf("  - %s (%s): %s\n", t.Name, t.Category, t.Description)
	}
}

// scheduledPipelineExample shows scheduled pipeline execution
func scheduledPipelineExample(ctx context.Context, manager training.PipelineManager) {
	// Create pipeline with schedule
	config := training.PipelineConfig{
		ID:   "scheduled-retraining",
		Name: "Automated Model Retraining",
		Schedule: training.ScheduleConfig{
			Enabled:  true,
			Cron:     "0 2 * * 0", // Every Sunday at 2 AM
			Timezone: "UTC",
			Triggers: []training.TriggerConfig{
				{
					Type: "cron",
					Parameters: map[string]any{
						"expression": "0 2 * * 0",
					},
				},
				{
					Type: "event",
					Parameters: map[string]any{
						"event_type": "new_data_available",
					},
					Condition: "data_volume > 1000",
				},
			},
		},
		Stages: []training.StageConfig{
			{
				ID:   "check-data-drift",
				Type: training.StageTypeDataValidation,
				Parameters: map[string]any{
					"drift_threshold": 0.1,
				},
			},
			{
				ID:           "retrain-model",
				Type:         training.StageTypeModelTraining,
				Dependencies: []string{"check-data-drift"},
				Conditional: training.ConditionalConfig{
					Condition: "drift_detected == true",
				},
			},
			{
				ID:           "deploy-if-better",
				Type:         training.StageTypeModelDeployment,
				Dependencies: []string{"retrain-model"},
				Conditional: training.ConditionalConfig{
					Condition: "new_model_auc > current_model_auc + 0.01",
				},
			},
		},
		Outputs: []training.OutputConfig{
			{
				Type:        "notification",
				Destination: "slack://ml-team",
				Format:      "json",
				Parameters: map[string]any{
					"channel":    "#ml-alerts",
					"on_failure": true,
					"on_success": true,
				},
			},
			{
				Type:        "database",
				Destination: "postgres://metrics-db",
				Format:      "structured",
				Parameters: map[string]any{
					"table": "pipeline_runs",
				},
			},
		},
	}

	pipeline, err := manager.CreatePipeline(config)
	if err != nil {
		log.Fatalf("Failed to create scheduled pipeline: %v", err)
	}

	fmt.Printf("✓ Scheduled pipeline created: %s\n", pipeline.ID())
	fmt.Printf("  Schedule: %s\n", config.Schedule.Cron)
	fmt.Printf("  Triggers: %d\n", len(config.Schedule.Triggers))
	fmt.Printf("  Outputs: %d\n", len(config.Outputs))

	// Start pipeline (will run on schedule)
	if err := pipeline.Start(ctx); err != nil {
		log.Fatalf("Failed to start pipeline: %v", err)
	}

	fmt.Println("  Status: Running (scheduled)")

	// List all executions
	executions := pipeline.ListExecutions()
	fmt.Printf("\nPipeline Executions: %d\n", len(executions))
	for i, exec := range executions {
		fmt.Printf("  %d. %s - Status: %s, Started: %s\n",
			i+1,
			exec.ID(),
			exec.Status(),
			exec.StartedAt().Format(time.RFC3339))
	}
}
