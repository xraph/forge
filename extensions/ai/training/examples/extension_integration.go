//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai"
)

// ExtensionIntegrationExample demonstrates using training with the AI extension.
// This example shows:
// - Enabling training in extension configuration
// - Resolving training services from DI container
// - Using training services through the extension
// - Configuring training resources and storage
func main() {
	fmt.Println("=== AI Extension Training Integration ===\n")

	ctx := context.Background()

	// Example 1: Enable Training via Configuration
	fmt.Println("--- Example 1: Enable Training via Config ---")
	configuredExample(ctx)

	// Example 2: Access Training Services via DI
	fmt.Println("\n--- Example 2: Access Services via DI ---")
	diExample(ctx)

	// Example 3: Custom Training Configuration
	fmt.Println("\n--- Example 3: Custom Training Config ---")
	customConfigExample(ctx)

	fmt.Println("\n=== Integration Complete ===")
}

// configuredExample shows enabling training via extension config
func configuredExample(ctx context.Context) {
	// Create Forge app
	app := forge.NewApp(forge.Config{
		Name:    "training-app",
		Version: "1.0.0",
	})

	// Configure AI extension with training enabled
	aiExt := ai.NewExtension(
		ai.WithEnableTraining(true),
		ai.WithTrainingConfig(ai.TrainingConfiguration{
			Enabled:           true,
			CheckpointPath:    "./checkpoints",
			ModelPath:         "./models",
			DataPath:          "./data",
			MaxConcurrentJobs: 3,
			DefaultResources: ai.ResourcesConfig{
				CPU:      "8",
				Memory:   "16Gi",
				GPU:      1,
				Timeout:  6 * time.Hour,
				Priority: 2,
			},
			Storage: ai.StorageConfig{
				Type: "local",
				Local: &ai.LocalStorageConfig{
					BasePath: "./training-storage",
				},
			},
		}),
	)

	// Register extension
	if err := app.UseExtension(aiExt); err != nil {
		log.Fatalf("Failed to register AI extension: %v", err)
	}

	fmt.Println("✓ AI extension registered with training enabled")
	fmt.Println("  Checkpoint path: ./checkpoints")
	fmt.Println("  Model path: ./models")
	fmt.Println("  Max concurrent jobs: 3")
	fmt.Println("  Default GPU: 1")
}

// diExample shows accessing training services via dependency injection
func diExample(ctx context.Context) {
	// Create and configure app
	app := forge.NewApp(forge.Config{
		Name:    "training-di-app",
		Version: "1.0.0",
	})

	// Register AI extension with training
	aiExt := ai.NewExtension(
		ai.WithEnableTraining(true),
	)

	if err := app.UseExtension(aiExt); err != nil {
		log.Fatalf("Failed to register extension: %v", err)
	}

	// Start app to initialize services
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}

	fmt.Println("✓ App started with training services")

	// Resolve training services from DI container

	// Method 1: Resolve by type (preferred)
	modelTrainer, err := forge.InjectType[ai.ModelTrainer](app.Container())
	if err != nil {
		log.Fatalf("Failed to inject ModelTrainer: %v", err)
	}
	fmt.Println("✓ ModelTrainer resolved from DI container")

	dataManager, err := forge.InjectType[ai.DataManager](app.Container())
	if err != nil {
		log.Fatalf("Failed to inject DataManager: %v", err)
	}
	fmt.Println("✓ DataManager resolved from DI container")

	pipelineManager, err := forge.InjectType[ai.PipelineManager](app.Container())
	if err != nil {
		log.Fatalf("Failed to inject PipelineManager: %v", err)
	}
	fmt.Println("✓ PipelineManager resolved from DI container")

	// Method 2: Resolve by key (backward compatible)
	trainerByKey, err := app.Container().Resolve(ai.ModelTrainerKey)
	if err != nil {
		log.Fatalf("Failed to resolve by key: %v", err)
	}
	trainer, ok := trainerByKey.(ai.ModelTrainer)
	if !ok {
		log.Fatal("Invalid trainer type")
	}
	fmt.Println("✓ ModelTrainer resolved by string key")

	// Use the services
	fmt.Println("\n  Using services:")

	// Create dataset
	dataset, err := dataManager.CreateDataset(ctx, ai.DatasetConfig{
		ID:   "test-dataset",
		Name: "Test Dataset",
		Type: ai.DatasetTypeTabular,
	})
	if err != nil {
		log.Printf("  Dataset creation failed (expected in example): %v", err)
	} else {
		fmt.Printf("  ✓ Dataset created: %s\n", dataset.ID())
	}

	// List training jobs
	jobs := modelTrainer.ListTrainingJobs()
	fmt.Printf("  ✓ Training jobs: %d\n", len(jobs))

	// List pipelines
	pipelines := pipelineManager.ListPipelines()
	fmt.Printf("  ✓ Pipelines: %d\n", len(pipelines))

	// Cleanup
	if err := app.Stop(ctx); err != nil {
		log.Printf("Failed to stop app: %v", err)
	}

	// Use the resolved services
	_ = trainer
}

// customConfigExample shows custom training configuration options
func customConfigExample(ctx context.Context) {
	// Example 1: Production configuration with S3 storage
	fmt.Println("\n1. Production Config with S3:")

	prodConfig := ai.TrainingConfiguration{
		Enabled:           true,
		CheckpointPath:    "/mnt/checkpoints",
		ModelPath:         "/mnt/models",
		DataPath:          "/mnt/data",
		MaxConcurrentJobs: 10,
		DefaultResources: ai.ResourcesConfig{
			CPU:      "16",
			Memory:   "32Gi",
			GPU:      2,
			Timeout:  24 * time.Hour,
			Priority: 3,
		},
		Storage: ai.StorageConfig{
			Type: "s3",
			S3: &ai.S3StorageConfig{
				Bucket:    "ml-models-prod",
				Region:    "us-west-2",
				AccessKey: "AWS_ACCESS_KEY",
				SecretKey: "AWS_SECRET_KEY",
				Prefix:    "training/",
			},
		},
	}

	fmt.Printf("  ✓ Storage: S3 (bucket: %s)\n", prodConfig.Storage.S3.Bucket)
	fmt.Printf("  ✓ Max jobs: %d\n", prodConfig.MaxConcurrentJobs)
	fmt.Printf("  ✓ Default GPU: %d\n", prodConfig.DefaultResources.GPU)

	// Example 2: Development configuration with local storage
	fmt.Println("\n2. Development Config with Local Storage:")

	devConfig := ai.TrainingConfiguration{
		Enabled:           true,
		CheckpointPath:    "./dev-checkpoints",
		ModelPath:         "./dev-models",
		DataPath:          "./dev-data",
		MaxConcurrentJobs: 2,
		DefaultResources: ai.ResourcesConfig{
			CPU:      "4",
			Memory:   "8Gi",
			GPU:      0, // No GPU for dev
			Timeout:  2 * time.Hour,
			Priority: 1,
		},
		Storage: ai.StorageConfig{
			Type: "local",
			Local: &ai.LocalStorageConfig{
				BasePath: "./dev-training",
			},
		},
	}

	fmt.Printf("  ✓ Storage: Local (path: %s)\n", devConfig.Storage.Local.BasePath)
	fmt.Printf("  ✓ Max jobs: %d\n", devConfig.MaxConcurrentJobs)
	fmt.Printf("  ✓ GPU: %d (CPU only)\n", devConfig.DefaultResources.GPU)

	// Example 3: GCS storage configuration
	fmt.Println("\n3. Google Cloud Storage Config:")

	gcsConfig := ai.TrainingConfiguration{
		Enabled: true,
		Storage: ai.StorageConfig{
			Type: "gcs",
			GCS: &ai.GCSStorageConfig{
				Bucket:          "ml-training-gcs",
				ProjectID:       "my-ml-project",
				CredentialsFile: "/path/to/credentials.json",
				Prefix:          "models/",
			},
		},
	}

	fmt.Printf("  ✓ Storage: GCS (bucket: %s)\n", gcsConfig.Storage.GCS.Bucket)
	fmt.Printf("  ✓ Project: %s\n", gcsConfig.Storage.GCS.ProjectID)

	// Example 4: Disable training
	fmt.Println("\n4. Training Disabled:")

	disabledConfig := ai.TrainingConfiguration{
		Enabled: false,
	}

	fmt.Printf("  ✓ Training: %v (LLM and inference only)\n", disabledConfig.Enabled)
}

// ========================================
// Complete Usage Example
// ========================================

// CompleteExample shows the full workflow with extension integration
func CompleteExample() {
	ctx := context.Background()

	// 1. Create app with AI extension
	app := forge.NewApp(forge.Config{
		Name:    "ml-platform",
		Version: "1.0.0",
	})

	// 2. Configure and register AI extension
	aiExt := ai.NewExtension(
		ai.WithEnableTraining(true),
		ai.WithEnableLLM(true),
		ai.WithEnableAgents(true),
		ai.WithTrainingConfig(ai.TrainingConfiguration{
			Enabled:           true,
			CheckpointPath:    "./checkpoints",
			ModelPath:         "./models",
			MaxConcurrentJobs: 5,
			DefaultResources: ai.ResourcesConfig{
				CPU:    "8",
				Memory: "16Gi",
				GPU:    1,
			},
		}),
	)

	if err := app.UseExtension(aiExt); err != nil {
		log.Fatalf("Failed to register extension: %v", err)
	}

	// 3. Start app
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// 4. Resolve training services via DI
	trainer, _ := forge.InjectType[ai.ModelTrainer](app.Container())
	dataManager, _ := forge.InjectType[ai.DataManager](app.Container())

	// 5. Use training services
	dataset, _ := dataManager.CreateDataset(ctx, ai.DatasetConfig{
		ID:   "customer-data",
		Name: "Customer Dataset",
		Type: ai.DatasetTypeTabular,
	})

	job, _ := trainer.StartTraining(ctx, ai.TrainingRequest{
		ID:        "churn-model",
		ModelType: "classification",
		ModelConfig: ai.ModelConfig{
			Architecture: "xgboost",
			Framework:    "xgboost",
		},
	})

	fmt.Printf("Training job started: %s\n", job.ID())
}

// ========================================
// Configuration Best Practices
// ========================================

func ConfigurationBestPractices() {
	fmt.Println("\n=== Configuration Best Practices ===\n")

	fmt.Println("1. Development Environment:")
	fmt.Println("   - Use local storage")
	fmt.Println("   - Disable GPU (CPU only)")
	fmt.Println("   - Lower concurrent jobs (1-2)")
	fmt.Println("   - Shorter timeouts")

	fmt.Println("\n2. Staging Environment:")
	fmt.Println("   - Use cloud storage (S3/GCS)")
	fmt.Println("   - Enable 1 GPU")
	fmt.Println("   - Medium concurrent jobs (3-5)")
	fmt.Println("   - Standard timeouts")

	fmt.Println("\n3. Production Environment:")
	fmt.Println("   - Use highly available storage")
	fmt.Println("   - Multiple GPUs")
	fmt.Println("   - Higher concurrent jobs (10+)")
	fmt.Println("   - Extended timeouts")
	fmt.Println("   - Resource quotas and priorities")

	fmt.Println("\n4. Optional Training:")
	fmt.Println("   - Set enabled: false by default")
	fmt.Println("   - Only enable when needed")
	fmt.Println("   - Use separate training clusters")
	fmt.Println("   - Keep inference always available")
}
