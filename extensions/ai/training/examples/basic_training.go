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

// BasicTrainingExample demonstrates the fundamental training workflow.
// This example shows:
// - Loading and preparing a dataset
// - Configuring a training job
// - Starting training and monitoring progress
// - Saving the trained model
func main() {
	fmt.Println("=== Basic Training Example ===\n")

	ctx := context.Background()

	// 1. Setup logging and metrics
	logger := forge.NewLogger(forge.LoggerConfig{
		Level:  "info",
		Format: "json",
	})

	fmt.Println("✓ Logger initialized")

	// 2. Create data manager
	dataManager := training.NewDataManager(logger)
	fmt.Println("✓ Data manager created")

	// 3. Prepare dataset
	fmt.Println("\n--- Preparing Dataset ---")

	// In a real application, you would implement your own DataSource
	// For this example, we'll use a mock data source
	dataSource := &MockDataSource{
		id:      "csv-source",
		name:    "Customer Churn CSV",
		records: generateMockCustomerData(1000),
	}

	// Register the data source
	if err := dataManager.RegisterDataSource(dataSource); err != nil {
		log.Fatalf("Failed to register data source: %v", err)
	}

	// Create dataset from source
	dataset, err := dataManager.CreateDataset(ctx, training.DatasetConfig{
		ID:          "customer-churn-dataset",
		Name:        "Customer Churn Dataset",
		Type:        training.DatasetTypeTabular,
		Description: "Customer data with churn labels",
		Source:      dataSource,
		Schema: training.DataSchema{
			Version: "1.0",
			Fields: map[string]training.FieldSchema{
				"customer_id":      {Name: "customer_id", Type: "string", Required: true},
				"tenure_months":    {Name: "tenure_months", Type: "int", Required: true},
				"monthly_charges":  {Name: "monthly_charges", Type: "float", Required: true},
				"total_charges":    {Name: "total_charges", Type: "float", Required: true},
				"contract_type":    {Name: "contract_type", Type: "string", Required: true},
				"payment_method":   {Name: "payment_method", Type: "string", Required: true},
				"internet_service": {Name: "internet_service", Type: "string", Required: true},
				"churn":            {Name: "churn", Type: "bool", Required: true},
			},
			Labels: map[string]training.FieldSchema{
				"churn": {Name: "churn", Type: "bool", Required: true},
			},
		},
		Validation: training.ValidationRules{
			Required: []string{"customer_id", "churn"},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	fmt.Printf("✓ Dataset created: %s (%d records)\n", dataset.ID(), dataset.RecordCount())

	// 4. Split dataset into train/validation/test
	fmt.Println("\n--- Splitting Dataset ---")

	splits, err := dataset.Split(ctx, []float64{0.7, 0.15, 0.15})
	if err != nil {
		log.Fatalf("Failed to split dataset: %v", err)
	}

	fmt.Printf("✓ Train set: %d records\n", splits[0].RecordCount())
	fmt.Printf("✓ Validation set: %d records\n", splits[1].RecordCount())
	fmt.Printf("✓ Test set: %d records\n", splits[2].RecordCount())

	// 5. Create model trainer
	trainer := training.NewModelTrainer(logger, metrics)
	fmt.Println("\n✓ Model trainer created")

	// 6. Configure and start training
	fmt.Println("\n--- Starting Training ---")

	trainingRequest := training.TrainingRequest{
		ID:        "churn-model-v1",
		ModelType: "classification",
		ModelConfig: training.ModelConfig{
			Architecture: "gradient_boosting",
			Framework:    "xgboost",
			Version:      "1.0",
			Parameters: map[string]any{
				"max_depth":        6,
				"n_estimators":     100,
				"subsample":        0.8,
				"colsample_bytree": 0.8,
			},
		},
		TrainingConfig: training.TrainingConfig{
			Epochs:       50,
			BatchSize:    32,
			LearningRate: 0.1,
			Optimizer:    "adam",
			LossFunction: "binary_crossentropy",
			Regularization: training.RegularizationConfig{
				L2:          0.01,
				Dropout:     0.2,
				WeightDecay: 0.0001,
			},
			EarlyStopping: training.EarlyStoppingConfig{
				Enabled:   true,
				Metric:    "val_loss",
				Mode:      "min",
				Patience:  10,
				Threshold: 0.001,
			},
			LRScheduler: training.LearningRateScheduler{
				Type: "step",
				Parameters: map[string]any{
					"step_size": 10,
					"gamma":     0.5,
				},
			},
			Checkpoints: training.CheckpointConfig{
				Enabled:    true,
				Frequency:  5,
				Path:       "./checkpoints/churn-model",
				SaveBest:   true,
				SaveLatest: true,
				MaxKeep:    5,
			},
		},
		ValidationConfig: training.ValidationConfig{
			SplitRatio:     0.15,
			Metrics:        []string{"accuracy", "precision", "recall", "f1", "auc"},
			EvaluationFreq: 1,
		},
		HyperParameters: map[string]any{
			"class_weight": "balanced",
		},
		Resources: training.ResourceConfig{
			CPU:        "4",
			Memory:     "8Gi",
			GPU:        0,
			Priority:   1,
			MaxRuntime: 2 * time.Hour,
		},
		Tags: map[string]string{
			"project":     "customer-retention",
			"model_type":  "churn-prediction",
			"environment": "development",
		},
	}

	job, err := trainer.StartTraining(ctx, trainingRequest)
	if err != nil {
		log.Fatalf("Failed to start training: %v", err)
	}

	fmt.Printf("✓ Training job started: %s\n", job.ID())
	fmt.Printf("  Status: %s\n", job.Status())
	fmt.Printf("  Started at: %s\n", job.StartedAt().Format(time.RFC3339))

	// 7. Monitor training progress
	fmt.Println("\n--- Monitoring Training Progress ---\n")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastEpoch := -1

	for {
		select {
		case <-ticker.C:
			// Get current status
			status, err := trainer.GetTrainingStatus(job.ID())
			if err != nil {
				log.Printf("Error getting status: %v", err)
				continue
			}

			// Check if training is complete
			if status == training.TrainingStatusCompleted {
				fmt.Println("\n✓ Training completed successfully!")
				goto trainingComplete
			} else if status == training.TrainingStatusFailed {
				fmt.Println("\n✗ Training failed")
				logs, _ := trainer.GetTrainingLogs(job.ID())
				if len(logs) > 0 {
					lastLog := logs[len(logs)-1]
					fmt.Printf("Last log: %s\n", lastLog.Message)
				}
				return
			}

			// Get training metrics
			metrics, err := trainer.GetTrainingMetrics(job.ID())
			if err != nil {
				log.Printf("Error getting metrics: %v", err)
				continue
			}

			// Display progress (only on new epoch)
			if metrics.Epoch != lastEpoch {
				lastEpoch = metrics.Epoch
				fmt.Printf("Epoch %3d/%d | ", metrics.Epoch, metrics.TotalEpochs)
				fmt.Printf("Loss: %.4f (val: %.4f) | ", metrics.TrainingLoss, metrics.ValidationLoss)
				fmt.Printf("Acc: %.2f%% | ", metrics.Accuracy*100)
				fmt.Printf("LR: %.6f | ", metrics.LearningRate)
				fmt.Printf("Time: %v\n", job.Duration())

				// Show best model info
				if metrics.BestEpoch > 0 {
					fmt.Printf("         Best: Epoch %d (Loss: %.4f)\n",
						metrics.BestEpoch, metrics.BestLoss)
				}
			}

		case <-ctx.Done():
			fmt.Println("\nTraining cancelled by user")
			trainer.StopTraining(ctx, job.ID())
			return
		}
	}

trainingComplete:

	// 8. Get final metrics
	fmt.Println("\n--- Final Training Results ---")

	finalMetrics, err := trainer.GetTrainingMetrics(job.ID())
	if err != nil {
		log.Fatalf("Failed to get final metrics: %v", err)
	}

	fmt.Printf("\nTraining Summary:\n")
	fmt.Printf("  Total Epochs:     %d\n", finalMetrics.TotalEpochs)
	fmt.Printf("  Training Time:    %v\n", job.Duration())
	fmt.Printf("  Best Epoch:       %d\n", finalMetrics.BestEpoch)
	fmt.Printf("  Best Loss:        %.4f\n", finalMetrics.BestLoss)
	fmt.Printf("\nFinal Metrics:\n")
	fmt.Printf("  Accuracy:         %.2f%%\n", finalMetrics.Accuracy*100)
	fmt.Printf("  Precision:        %.4f\n", finalMetrics.Precision)
	fmt.Printf("  Recall:           %.4f\n", finalMetrics.Recall)
	fmt.Printf("  F1 Score:         %.4f\n", finalMetrics.F1Score)
	fmt.Printf("  AUC:              %.4f\n", finalMetrics.AUC)
	fmt.Printf("\nResource Usage:\n")
	fmt.Printf("  CPU Usage:        %.1f%%\n", finalMetrics.ResourceUsage.CPUUsage*100)
	fmt.Printf("  Memory Used:      %d MB\n", finalMetrics.ResourceUsage.MemoryUsage/(1024*1024))
	fmt.Printf("  Samples/sec:      %.1f\n", finalMetrics.DataThroughput)

	// 9. Save trained model
	fmt.Println("\n--- Saving Trained Model ---")

	modelPath := "./models/churn_model_v1.pkl"
	if err := trainer.SaveModel(ctx, job.ID(), modelPath); err != nil {
		log.Fatalf("Failed to save model: %v", err)
	}

	fmt.Printf("✓ Model saved to: %s\n", modelPath)

	// 10. Export in different formats (optional)
	fmt.Println("\n--- Exporting Model ---")

	// Export as ONNX for framework-agnostic inference
	onnxData, err := trainer.ExportModel(ctx, job.ID(), "onnx")
	if err != nil {
		log.Printf("Warning: ONNX export failed: %v", err)
	} else {
		fmt.Printf("✓ ONNX export: %d bytes\n", len(onnxData))
	}

	// 11. Get training logs
	fmt.Println("\n--- Training Logs (last 5) ---")

	logs, err := trainer.GetTrainingLogs(job.ID())
	if err != nil {
		log.Printf("Failed to get logs: %v", err)
	} else {
		startIdx := 0
		if len(logs) > 5 {
			startIdx = len(logs) - 5
		}
		for _, entry := range logs[startIdx:] {
			fmt.Printf("[%s] %s: %s\n",
				entry.Timestamp.Format("15:04:05"),
				entry.Level,
				entry.Message)
		}
	}

	fmt.Println("\n=== Training Complete ===")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Validate model on test set")
	fmt.Println("  2. Load model in inference engine")
	fmt.Println("  3. Serve predictions via API")
	fmt.Println("\nSee train_to_inference.go for complete deployment workflow.")
}

// MockDataSource implements training.DataSource for demonstration
type MockDataSource struct {
	id      string
	name    string
	records []training.DataRecord
}

func (m *MockDataSource) ID() string {
	return m.id
}

func (m *MockDataSource) Name() string {
	return m.name
}

func (m *MockDataSource) Type() training.DataSourceType {
	return training.DataSourceTypeMemory
}

func (m *MockDataSource) Connect(ctx context.Context) error {
	return nil
}

func (m *MockDataSource) Disconnect(ctx context.Context) error {
	return nil
}

func (m *MockDataSource) Read(ctx context.Context, query training.DataQuery) (training.DataReader, error) {
	return &MockDataReader{records: m.records, position: 0}, nil
}

func (m *MockDataSource) Write(ctx context.Context, data training.DataWriter) error {
	return fmt.Errorf("write not supported in mock source")
}

func (m *MockDataSource) GetSchema() training.DataSchema {
	return training.DataSchema{}
}

func (m *MockDataSource) GetMetadata() map[string]any {
	return map[string]any{
		"record_count": len(m.records),
	}
}

// MockDataReader implements training.DataReader
type MockDataReader struct {
	records  []training.DataRecord
	position int
}

func (r *MockDataReader) Read(ctx context.Context) (training.DataRecord, error) {
	if r.position >= len(r.records) {
		return training.DataRecord{}, fmt.Errorf("end of data")
	}
	record := r.records[r.position]
	r.position++
	return record, nil
}

func (r *MockDataReader) ReadBatch(ctx context.Context, batchSize int) ([]training.DataRecord, error) {
	end := r.position + batchSize
	if end > len(r.records) {
		end = len(r.records)
	}
	batch := r.records[r.position:end]
	r.position = end
	return batch, nil
}

func (r *MockDataReader) ReadAll(ctx context.Context) ([]training.DataRecord, error) {
	return r.records, nil
}

func (r *MockDataReader) Close() error {
	return nil
}

func (r *MockDataReader) HasNext() bool {
	return r.position < len(r.records)
}

// generateMockCustomerData generates synthetic customer data
func generateMockCustomerData(count int) []training.DataRecord {
	records := make([]training.DataRecord, count)

	for i := 0; i < count; i++ {
		// Generate synthetic customer data
		tenureMonths := (i % 72) + 1
		monthlyCharges := 20.0 + float64(i%100)
		totalCharges := float64(tenureMonths) * monthlyCharges

		// Simulate churn based on simple rules
		churn := false
		if tenureMonths < 12 || monthlyCharges > 80 {
			churn = i%3 == 0 // Higher churn rate
		} else {
			churn = i%10 == 0 // Lower churn rate
		}

		records[i] = training.DataRecord{
			ID: fmt.Sprintf("customer_%d", i),
			Data: map[string]any{
				"customer_id":      fmt.Sprintf("CUST%06d", i),
				"tenure_months":    tenureMonths,
				"monthly_charges":  monthlyCharges,
				"total_charges":    totalCharges,
				"contract_type":    []string{"Month-to-month", "One year", "Two year"}[i%3],
				"payment_method":   []string{"Electronic check", "Mailed check", "Credit card"}[i%3],
				"internet_service": []string{"DSL", "Fiber optic", "No"}[i%3],
			},
			Labels: map[string]any{
				"churn": churn,
			},
			Version: "1.0",
		}
	}

	return records
}
