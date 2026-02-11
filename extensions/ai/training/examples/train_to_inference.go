//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/inference"
	"github.com/xraph/forge/extensions/ai/models"
	"github.com/xraph/forge/extensions/ai/training"
)

// TrainToInferenceExample demonstrates the complete ML lifecycle.
// This example shows the end-to-end workflow:
// 1. Prepare and validate dataset
// 2. Train a custom model
// 3. Save and export the trained model
// 4. Load model in inference engine
// 5. Serve predictions via API
//
// This is the most important example as it shows how training and inference work together.
func main() {
	fmt.Println("=== Complete ML Lifecycle: Training → Inference ===\n")

	ctx := context.Background()

	// Setup
	logger := forge.NewLogger(forge.LoggerConfig{Level: "info"})
	metrics := forge.NewMetrics(forge.MetricsConfig{Enabled: true})

	// =====================================================================
	// PHASE 1: DATA PREPARATION
	// =====================================================================
	fmt.Println("### PHASE 1: DATA PREPARATION ###\n")

	dataManager := training.NewDataManager(logger, metrics)

	// Create dataset
	dataset, err := dataManager.CreateDataset(ctx, training.DatasetConfig{
		ID:   "sentiment-analysis-data",
		Name: "Product Reviews Sentiment Dataset",
		Type: training.DatasetTypeText,
		Source: &MockTextDataSource{
			reviews: generateMockReviews(5000),
		},
	})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	fmt.Printf("✓ Dataset created: %d reviews\n", dataset.RecordCount())

	// Validate dataset quality
	if err := dataManager.ValidateDataset(ctx, dataset); err != nil {
		log.Fatalf("Dataset validation failed: %v", err)
	}

	stats := dataset.GetStatistics()
	fmt.Printf("✓ Dataset validated\n")
	fmt.Printf("  Quality Score: %.2f%%\n", stats.Quality.Completeness*100)
	fmt.Printf("  Completeness:  %.2f%%\n", stats.Quality.Completeness*100)
	fmt.Printf("  Validity:      %.2f%%\n", stats.Quality.Validity*100)

	// Split into train/val/test
	splits, err := dataset.Split(ctx, []float64{0.7, 0.15, 0.15})
	if err != nil {
		log.Fatalf("Failed to split dataset: %v", err)
	}

	fmt.Printf("\n✓ Dataset split:\n")
	fmt.Printf("  Training:   %d reviews\n", splits[0].RecordCount())
	fmt.Printf("  Validation: %d reviews\n", splits[1].RecordCount())
	fmt.Printf("  Test:       %d reviews\n", splits[2].RecordCount())

	// =====================================================================
	// PHASE 2: MODEL TRAINING
	// =====================================================================
	fmt.Println("\n### PHASE 2: MODEL TRAINING ###\n")

	trainer := training.NewModelTrainer(logger, metrics)

	// Configure training job
	trainingRequest := training.TrainingRequest{
		ID:        "sentiment-model-v1",
		ModelType: "text_classification",
		ModelConfig: training.ModelConfig{
			Architecture: "transformer",
			Framework:    "pytorch",
			Version:      "1.0",
			Parameters: map[string]any{
				"base_model":     "distilbert-base-uncased",
				"num_labels":     3, // positive, negative, neutral
				"hidden_dropout": 0.1,
			},
		},
		TrainingConfig: training.TrainingConfig{
			Epochs:       10,
			BatchSize:    16,
			LearningRate: 2e-5,
			Optimizer:    "adamw",
			LossFunction: "cross_entropy",
			EarlyStopping: training.EarlyStoppingConfig{
				Enabled:   true,
				Patience:  3,
				Metric:    "val_loss",
				Mode:      "min",
				Threshold: 0.001,
			},
			Checkpoints: training.CheckpointConfig{
				Enabled:    true,
				Frequency:  1,
				Path:       "./checkpoints/sentiment-model",
				SaveBest:   true,
				SaveLatest: true,
				MaxKeep:    3,
			},
		},
		ValidationConfig: training.ValidationConfig{
			Metrics:        []string{"accuracy", "precision", "recall", "f1"},
			EvaluationFreq: 1,
		},
		Resources: training.ResourceConfig{
			CPU:        "4",
			Memory:     "8Gi",
			GPU:        1,
			MaxRuntime: 2 * time.Hour,
		},
	}

	fmt.Println("Starting training job...")
	job, err := trainer.StartTraining(ctx, trainingRequest)
	if err != nil {
		log.Fatalf("Failed to start training: %v", err)
	}

	fmt.Printf("✓ Training job started: %s\n", job.ID())

	// Monitor training progress
	fmt.Println("\nMonitoring training progress...\n")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	lastEpoch := -1

trainingLoop:
	for {
		select {
		case <-ticker.C:
			status, _ := trainer.GetTrainingStatus(job.ID())

			if status == training.TrainingStatusCompleted {
				fmt.Println("\n✓ Training completed!")
				break trainingLoop
			} else if status == training.TrainingStatusFailed {
				log.Fatal("Training failed")
			}

			metrics, _ := trainer.GetTrainingMetrics(job.ID())
			if metrics.Epoch != lastEpoch {
				lastEpoch = metrics.Epoch
				fmt.Printf("Epoch %2d/%d | Loss: %.4f (val: %.4f) | Acc: %.2f%% | F1: %.4f\n",
					metrics.Epoch,
					metrics.TotalEpochs,
					metrics.TrainingLoss,
					metrics.ValidationLoss,
					metrics.Accuracy*100,
					metrics.F1Score)
			}
		}
	}

	// Get final metrics
	finalMetrics, _ := trainer.GetTrainingMetrics(job.ID())
	fmt.Printf("\nFinal Training Metrics:\n")
	fmt.Printf("  Accuracy:  %.2f%%\n", finalMetrics.Accuracy*100)
	fmt.Printf("  Precision: %.4f\n", finalMetrics.Precision)
	fmt.Printf("  Recall:    %.4f\n", finalMetrics.Recall)
	fmt.Printf("  F1 Score:  %.4f\n", finalMetrics.F1Score)
	fmt.Printf("  Duration:  %v\n", job.Duration())

	// =====================================================================
	// PHASE 3: MODEL EXPORT
	// =====================================================================
	fmt.Println("\n### PHASE 3: MODEL EXPORT ###\n")

	// Save model for inference
	modelPath := "./models/sentiment_model_v1.pkl"
	if err := trainer.SaveModel(ctx, job.ID(), modelPath); err != nil {
		log.Fatalf("Failed to save model: %v", err)
	}
	fmt.Printf("✓ Model saved to: %s\n", modelPath)

	// Export in ONNX format for production
	onnxData, err := trainer.ExportModel(ctx, job.ID(), "onnx")
	if err != nil {
		log.Printf("Warning: ONNX export not available in this example")
	} else {
		fmt.Printf("✓ ONNX export: %d bytes\n", len(onnxData))
	}

	// Load the trained model
	trainedModel, err := trainer.LoadModel(ctx, modelPath)
	if err != nil {
		log.Fatalf("Failed to load model: %v", err)
	}
	fmt.Printf("✓ Model loaded for inference\n")

	// =====================================================================
	// PHASE 4: INFERENCE ENGINE SETUP
	// =====================================================================
	fmt.Println("\n### PHASE 4: INFERENCE ENGINE SETUP ###\n")

	// Create inference engine with production configuration
	inferenceEngine := inference.NewInferenceEngine(inference.InferenceConfig{
		Workers:        4,
		BatchSize:      16,
		BatchTimeout:   50 * time.Millisecond,
		EnableBatching: true,
		EnableCaching:  true,
		EnableScaling:  false, // Fixed workers for predictable performance
		CacheSize:      1000,
		CacheTTL:       10 * time.Minute,
		RequestTimeout: 5 * time.Second,
	})

	fmt.Println("✓ Inference engine created")
	fmt.Println("  Workers:  4")
	fmt.Println("  Batching: enabled (size: 16)")
	fmt.Println("  Caching:  enabled (1000 entries)")

	// Wrap trained model for inference
	inferenceModel := &SentimentInferenceModel{
		trainedModel: trainedModel,
		name:         "sentiment-analyzer-v1",
	}

	// Register model with inference engine
	inferenceEngine.RegisterModel("sentiment-analyzer", inferenceModel)
	fmt.Printf("\n✓ Model registered: %s\n", inferenceModel.Name())

	// Start inference engine
	if err := inferenceEngine.Start(ctx); err != nil {
		log.Fatalf("Failed to start inference engine: %v", err)
	}

	fmt.Println("✓ Inference engine started and ready")

	// =====================================================================
	// PHASE 5: SERVE PREDICTIONS
	// =====================================================================
	fmt.Println("\n### PHASE 5: SERVING PREDICTIONS ###\n")

	// Test cases
	testReviews := []string{
		"This product is absolutely amazing! Best purchase ever!",
		"Terrible quality. Very disappointed and want a refund.",
		"It's okay, nothing special but does the job.",
		"Love it! Exactly what I needed. Highly recommend!",
		"Waste of money. Do not buy this product.",
	}

	fmt.Println("Making predictions...\n")

	for i, review := range testReviews {
		// Make inference request
		result, err := inferenceEngine.Infer(ctx, inference.InferenceRequest{
			ModelID: "sentiment-analyzer",
			Input: models.ModelInput{
				Data: map[string]any{
					"text": review,
				},
			},
		})

		if err != nil {
			log.Printf("Prediction %d failed: %v", i+1, err)
			continue
		}

		// Parse prediction
		predictions := result.Output.Predictions
		if len(predictions) > 0 {
			pred := predictions[0]
			sentiment := pred.Label
			confidence := pred.Score

			fmt.Printf("Review %d:\n", i+1)
			fmt.Printf("  Text:       \"%s\"\n", truncateString(review, 50))
			fmt.Printf("  Sentiment:  %s\n", sentiment)
			fmt.Printf("  Confidence: %.2f%%\n", confidence*100)
			fmt.Printf("  Latency:    %v\n", result.Latency)
			fmt.Println()
		}
	}

	// =====================================================================
	// PHASE 6: BATCH INFERENCE
	// =====================================================================
	fmt.Println("### PHASE 6: BATCH INFERENCE ###\n")

	// Simulate batch prediction workload
	fmt.Println("Processing batch of 100 reviews...")

	batchStart := time.Now()
	batchCount := 100
	successCount := 0

	for i := 0; i < batchCount; i++ {
		review := fmt.Sprintf("Sample review number %d with random content", i)

		go func(idx int, text string) {
			_, err := inferenceEngine.Infer(ctx, inference.InferenceRequest{
				ModelID: "sentiment-analyzer",
				Input: models.ModelInput{
					Data: map[string]any{
						"text": text,
					},
				},
			})
			if err == nil {
				successCount++
			}
		}(i, review)
	}

	// Wait for batch to complete
	time.Sleep(2 * time.Second)

	batchDuration := time.Since(batchStart)

	fmt.Printf("\n✓ Batch inference completed\n")
	fmt.Printf("  Total:      %d requests\n", batchCount)
	fmt.Printf("  Successful: %d\n", successCount)
	fmt.Printf("  Duration:   %v\n", batchDuration)
	fmt.Printf("  Throughput: %.1f req/s\n", float64(batchCount)/batchDuration.Seconds())

	// =====================================================================
	// PHASE 7: PERFORMANCE METRICS
	// =====================================================================
	fmt.Println("\n### PHASE 7: PERFORMANCE METRICS ###\n")

	// Get inference engine statistics
	stats2 := inferenceEngine.Stats()
	fmt.Printf("Inference Engine Stats:\n")
	fmt.Printf("  Requests Processed: %d\n", stats2.RequestsProcessed)
	fmt.Printf("  Batches Processed:  %d\n", stats2.BatchesProcessed)
	fmt.Printf("  Average Batch Size: %.1f\n", stats2.AverageBatchSize)
	fmt.Printf("  Cache Hit Rate:     %.2f%%\n", stats2.CacheHitRate*100)
	fmt.Printf("  Average Latency:    %v\n", stats2.AverageLatency)
	fmt.Printf("  Throughput:         %.1f req/s\n", stats2.ThroughputPerSec)

	// Health check
	health := inferenceEngine.Health(ctx)
	fmt.Printf("\nInference Engine Health:\n")
	fmt.Printf("  Status:         %s\n", health.Status)
	fmt.Printf("  Active Workers: %d\n", health.ActiveWorkers)
	fmt.Printf("  Queue Size:     %d\n", health.QueueSize)

	// =====================================================================
	// CLEANUP
	// =====================================================================
	fmt.Println("\n### CLEANUP ###\n")

	// Stop inference engine
	if err := inferenceEngine.Stop(ctx); err != nil {
		log.Printf("Error stopping inference engine: %v", err)
	}
	fmt.Println("✓ Inference engine stopped")

	// Summary
	fmt.Println("\n=== ML Lifecycle Complete ===")
	fmt.Println("\nSummary:")
	fmt.Println("  1. ✓ Dataset prepared and validated")
	fmt.Println("  2. ✓ Model trained with early stopping")
	fmt.Println("  3. ✓ Model saved and exported")
	fmt.Println("  4. ✓ Inference engine configured")
	fmt.Println("  5. ✓ Predictions served via API")
	fmt.Println("  6. ✓ Batch inference tested")
	fmt.Println("  7. ✓ Performance metrics collected")

	fmt.Println("\nNext Steps:")
	fmt.Println("  - Deploy inference engine to production")
	fmt.Println("  - Set up monitoring and alerting")
	fmt.Println("  - Schedule periodic model retraining")
	fmt.Println("  - Implement A/B testing for new models")
}

// MockTextDataSource implements training.DataSource for text data
type MockTextDataSource struct {
	reviews []map[string]any
}

func (m *MockTextDataSource) ID() string                           { return "text-source" }
func (m *MockTextDataSource) Name() string                         { return "Mock Text Reviews" }
func (m *MockTextDataSource) Type() training.DataSourceType        { return training.DataSourceTypeMemory }
func (m *MockTextDataSource) Connect(ctx context.Context) error    { return nil }
func (m *MockTextDataSource) Disconnect(ctx context.Context) error { return nil }
func (m *MockTextDataSource) GetSchema() training.DataSchema       { return training.DataSchema{} }
func (m *MockTextDataSource) GetMetadata() map[string]any          { return map[string]any{} }
func (m *MockTextDataSource) Write(ctx context.Context, data training.DataWriter) error {
	return fmt.Errorf("write not supported")
}

func (m *MockTextDataSource) Read(ctx context.Context, query training.DataQuery) (training.DataReader, error) {
	records := make([]training.DataRecord, len(m.reviews))
	for i, review := range m.reviews {
		records[i] = training.DataRecord{
			ID:   fmt.Sprintf("review-%d", i),
			Data: review,
			Labels: map[string]any{
				"sentiment": review["sentiment"],
			},
		}
	}
	return &MockTextReader{records: records}, nil
}

type MockTextReader struct {
	records  []training.DataRecord
	position int
}

func (r *MockTextReader) Read(ctx context.Context) (training.DataRecord, error) {
	if r.position >= len(r.records) {
		return training.DataRecord{}, fmt.Errorf("EOF")
	}
	record := r.records[r.position]
	r.position++
	return record, nil
}

func (r *MockTextReader) ReadAll(ctx context.Context) ([]training.DataRecord, error) {
	return r.records, nil
}

func (r *MockTextReader) ReadBatch(ctx context.Context, size int) ([]training.DataRecord, error) {
	end := r.position + size
	if end > len(r.records) {
		end = len(r.records)
	}
	batch := r.records[r.position:end]
	r.position = end
	return batch, nil
}

func (r *MockTextReader) Close() error  { return nil }
func (r *MockTextReader) HasNext() bool { return r.position < len(r.records) }

// SentimentInferenceModel wraps trained model for inference
type SentimentInferenceModel struct {
	trainedModel training.TrainedModel
	name         string
}

func (m *SentimentInferenceModel) ID() string                    { return m.trainedModel.ID() }
func (m *SentimentInferenceModel) Name() string                  { return m.name }
func (m *SentimentInferenceModel) Version() string               { return m.trainedModel.Version() }
func (m *SentimentInferenceModel) Type() models.ModelType        { return models.ModelTypeNLP }
func (m *SentimentInferenceModel) Framework() models.MLFramework { return models.MLFrameworkPyTorch }

func (m *SentimentInferenceModel) Predict(ctx context.Context, input models.ModelInput) (models.ModelOutput, error) {
	// Simulate prediction
	text := input.Data["text"].(string)

	// Simple rule-based prediction for demo
	sentiment := "neutral"
	confidence := 0.7

	if containsPositiveWords(text) {
		sentiment = "positive"
		confidence = 0.85
	} else if containsNegativeWords(text) {
		sentiment = "negative"
		confidence = 0.80
	}

	return models.ModelOutput{
		Predictions: []models.Prediction{
			{
				Label: sentiment,
				Score: confidence,
			},
		},
		Confidence: confidence,
		Metadata: map[string]any{
			"model_version": m.Version(),
			"text_length":   len(text),
		},
	}, nil
}

func (m *SentimentInferenceModel) BatchPredict(ctx context.Context, inputs []models.ModelInput) ([]models.ModelOutput, error) {
	outputs := make([]models.ModelOutput, len(inputs))
	for i, input := range inputs {
		output, err := m.Predict(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

// Stub implementations for Model interface
func (m *SentimentInferenceModel) Load(ctx context.Context) error   { return nil }
func (m *SentimentInferenceModel) Unload(ctx context.Context) error { return nil }
func (m *SentimentInferenceModel) IsLoaded() bool                   { return true }
func (m *SentimentInferenceModel) GetMetrics() models.ModelMetrics  { return models.ModelMetrics{} }
func (m *SentimentInferenceModel) GetHealth() models.ModelHealth    { return models.ModelHealth{} }
func (m *SentimentInferenceModel) GetConfig() models.ModelConfig    { return models.ModelConfig{} }
func (m *SentimentInferenceModel) GetInfo() models.ModelInfo        { return models.ModelInfo{} }
func (m *SentimentInferenceModel) GetMetadata() map[string]any      { return map[string]any{} }
func (m *SentimentInferenceModel) ValidateInput(input models.ModelInput) error {
	return nil
}
func (m *SentimentInferenceModel) GetInputSchema() models.InputSchema   { return models.InputSchema{} }
func (m *SentimentInferenceModel) GetOutputSchema() models.OutputSchema { return models.OutputSchema{} }

// Helper functions
func generateMockReviews(count int) []map[string]any {
	reviews := make([]map[string]any, count)
	positiveWords := []string{"amazing", "excellent", "love", "great", "perfect", "best"}
	negativeWords := []string{"terrible", "awful", "hate", "worst", "disappointed", "bad"}

	for i := 0; i < count; i++ {
		sentiment := "neutral"
		text := fmt.Sprintf("This is review number %d.", i)

		if i%3 == 0 {
			sentiment = "positive"
			word := positiveWords[i%len(positiveWords)]
			text = fmt.Sprintf("This product is %s! Review number %d.", word, i)
		} else if i%3 == 1 {
			sentiment = "negative"
			word := negativeWords[i%len(negativeWords)]
			text = fmt.Sprintf("This product is %s. Review number %d.", word, i)
		}

		reviews[i] = map[string]any{
			"text":      text,
			"sentiment": sentiment,
		}
	}
	return reviews
}

func containsPositiveWords(text string) bool {
	positive := []string{"amazing", "excellent", "love", "great", "perfect", "best", "recommend"}
	for _, word := range positive {
		if contains(text, word) {
			return true
		}
	}
	return false
}

func containsNegativeWords(text string) bool {
	negative := []string{"terrible", "awful", "hate", "worst", "disappointed", "bad", "refund"}
	for _, word := range negative {
		if contains(text, word) {
			return true
		}
	}
	return false
}

func contains(text, word string) bool {
	return len(text) > 0 && len(word) > 0 // Simplified
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
