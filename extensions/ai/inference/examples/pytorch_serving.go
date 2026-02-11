//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge/extensions/ai/inference"
	"github.com/xraph/forge/extensions/ai/models"
)

// PyTorchServingExample demonstrates serving a PyTorch model with auto-scaling.
// This example shows:
// - Loading a PyTorch model (via ONNX or TorchScript)
// - Dynamic batching with model-aware strategies
// - Auto-scaling based on load
// - Custom preprocessing/postprocessing
func main() {
	fmt.Println("=== PyTorch Model Serving Example ===\n")

	ctx := context.Background()

	// 1. Create inference engine optimized for PyTorch
	engine := inference.NewInferenceEngine(inference.InferenceConfig{
		Workers:          6,
		BatchSize:        32, // Larger batch for GPU efficiency
		BatchTimeout:     100 * time.Millisecond,
		EnableBatching:   true,
		EnableCaching:    false, // Disable for non-deterministic models
		EnableScaling:    true,
		RequestTimeout:   10 * time.Second,
		ScalingThreshold: 0.7, // Scale up earlier
		MaxWorkers:       16,
		MinWorkers:       4,
	})

	fmt.Println("✓ Inference engine configured for PyTorch:")
	fmt.Println("  - 6 initial workers (scales 4-16)")
	fmt.Println("  - Batch size: 32 (GPU optimized)")
	fmt.Println("  - Auto-scaling at 70% load")

	// 2. Load PyTorch model (converted to ONNX for production)
	model := loadPyTorchModel("./models/text_classifier.onnx")
	if model == nil {
		log.Fatal("Failed to load model")
	}

	engine.RegisterModel("text-classifier", model)
	fmt.Println("\n✓ Registered PyTorch/ONNX model: text-classifier")

	// 3. Add text preprocessing pipeline
	engine.AddPreprocessor(func(ctx context.Context, req inference.InferenceRequest) (inference.InferenceRequest, error) {
		fmt.Printf("  → Tokenizing input for request %s\n", req.ID)

		// Text preprocessing:
		// - Tokenization
		// - Padding/truncation
		// - Convert to tensor format
		req.Input = tokenizeText(req.Input)

		return req, nil
	})

	// 4. Add output formatting
	engine.AddPostprocessor(func(ctx context.Context, resp inference.InferenceResponse) (inference.InferenceResponse, error) {
		fmt.Printf("  ← Formatting output for response %s\n", resp.ID)

		// Output postprocessing:
		// - Apply softmax
		// - Get top-k predictions
		// - Format as human-readable labels
		resp.Output = formatClassificationOutput(resp.Output)

		return resp, nil
	})

	fmt.Println("\n✓ Added NLP preprocessing and postprocessing stages")

	// 5. Configure custom batching strategy
	// Model-aware batching optimizes for GPU throughput
	engine.SetBatchingStrategy(&ModelAwareBatchingStrategy{
		modelType:        "text-classification",
		optimalBatchSize: 32,
		maxWaitTime:      100 * time.Millisecond,
	})

	fmt.Println("\n✓ Configured model-aware batching strategy")

	// 6. Start the engine
	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop(ctx)

	fmt.Println("\n✓ Inference engine started")

	// 7. Simulate varying load to trigger auto-scaling
	fmt.Println("\n--- Simulating Variable Load ---\n")

	// Low load
	fmt.Println("Phase 1: Low load (10 req/s)")
	simulateLoad(ctx, engine, 10, 1*time.Second)
	time.Sleep(2 * time.Second)
	displayStats(engine, "Low Load")

	// Medium load
	fmt.Println("\nPhase 2: Medium load (50 req/s)")
	simulateLoad(ctx, engine, 50, 1*time.Second)
	time.Sleep(2 * time.Second)
	displayStats(engine, "Medium Load")

	// High load (should trigger auto-scaling)
	fmt.Println("\nPhase 3: High load (200 req/s)")
	simulateLoad(ctx, engine, 200, 1*time.Second)
	time.Sleep(3 * time.Second)
	displayStats(engine, "High Load")

	// Cool down
	fmt.Println("\nPhase 4: Cooldown (10 req/s)")
	simulateLoad(ctx, engine, 10, 1*time.Second)
	time.Sleep(5 * time.Second)
	displayStats(engine, "After Cooldown")

	fmt.Println("\n=== Example Complete ===")
}

// loadPyTorchModel loads a PyTorch model (via ONNX Runtime)
func loadPyTorchModel(path string) models.Model {
	// Example stub - replace with actual ONNX loading:
	//
	// import "github.com/yalue/onnxruntime_go"
	//
	// session, err := onnxruntime_go.NewSession(path)
	// if err != nil {
	//     return nil
	// }
	//
	// return &ONNXModelWrapper{session: session}

	return &MockPyTorchModel{name: "text-classifier"}
}

// MockPyTorchModel simulates a PyTorch model
type MockPyTorchModel struct {
	name string
}

func (m *MockPyTorchModel) Predict(ctx context.Context, input interface{}) (interface{}, error) {
	// Simulate model inference with GPU latency
	time.Sleep(20 * time.Millisecond)

	return map[string]float64{
		"positive": 0.78,
		"negative": 0.15,
		"neutral":  0.07,
	}, nil
}

func (m *MockPyTorchModel) Name() string {
	return m.name
}

func (m *MockPyTorchModel) Type() string {
	return "pytorch"
}

// ModelAwareBatchingStrategy optimizes batching for specific model types
type ModelAwareBatchingStrategy struct {
	modelType        string
	optimalBatchSize int
	maxWaitTime      time.Duration
}

func (s *ModelAwareBatchingStrategy) Name() string {
	return "model-aware"
}

func (s *ModelAwareBatchingStrategy) ShouldBatch(req inference.InferenceRequest, batch *inference.RequestBatch) bool {
	// Only batch requests for the same model
	if batch.ModelID != req.ModelID {
		return false
	}

	// Batch if we haven't reached optimal size
	return batch.Size < s.optimalBatchSize
}

func (s *ModelAwareBatchingStrategy) OptimalBatchSize(modelID string, queueSize int) int {
	// Adjust batch size based on queue depth
	if queueSize > 100 {
		return s.optimalBatchSize * 2 // Larger batches under load
	}
	return s.optimalBatchSize
}

func (s *ModelAwareBatchingStrategy) BatchTimeout(modelID string, batchSize int) time.Duration {
	// Shorter timeout for smaller batches
	if batchSize < s.optimalBatchSize/2 {
		return s.maxWaitTime / 2
	}
	return s.maxWaitTime
}

// Helper functions
func tokenizeText(input interface{}) interface{} {
	// Implement text tokenization
	return input
}

func formatClassificationOutput(output interface{}) interface{} {
	// Format classification results
	return output
}

func simulateLoad(ctx context.Context, engine *inference.InferenceEngine, reqPerSec int, duration time.Duration) {
	ticker := time.NewTicker(time.Second / time.Duration(reqPerSec))
	defer ticker.Stop()

	done := time.After(duration)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			go func() {
				_, _ = engine.Infer(ctx, inference.InferenceRequest{
					ModelID: "text-classifier",
					Input:   "Sample text for classification",
				})
			}()
		}
	}
}

func displayStats(engine *inference.InferenceEngine, phase string) {
	stats := engine.Stats()
	fmt.Printf("\n--- %s Statistics ---\n", phase)
	fmt.Printf("Active Workers:     %d\n", stats.ActiveWorkers)
	fmt.Printf("Requests Processed: %d\n", stats.RequestsProcessed)
	fmt.Printf("Average Batch Size: %.1f\n", stats.AverageBatchSize)
	fmt.Printf("Queue Size:         %d\n", stats.QueueSize)
	fmt.Printf("Throughput:         %.1f req/s\n", stats.ThroughputPerSec)
	fmt.Printf("Average Latency:    %v\n", stats.AverageLatency)
}
