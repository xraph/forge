package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge/extensions/ai/inference"
	"github.com/xraph/forge/extensions/ai/models"
)

// TensorFlowServingExample demonstrates serving a TensorFlow model with the Forge Inference Engine.
// This example shows:
// - Loading a TensorFlow model
// - Configuring batching and caching
// - Adding preprocessing pipeline
// - Monitoring inference stats
func main() {
	fmt.Println("=== TensorFlow Model Serving Example ===\n")

	ctx := context.Background()

	// 1. Create inference engine with production config
	engine := inference.NewInferenceEngine(inference.InferenceConfig{
		Workers:          4,
		BatchSize:        16,
		BatchTimeout:     50 * time.Millisecond,
		EnableBatching:   true,
		EnableCaching:    true,
		EnableScaling:    true,
		CacheSize:        1000,
		CacheTTL:         30 * time.Minute,
		RequestTimeout:   5 * time.Second,
		ScalingThreshold: 0.75,
		MaxWorkers:       12,
		MinWorkers:       2,
	})

	fmt.Println("✓ Inference engine created with:")
	fmt.Println("  - 4 initial workers (auto-scales 2-12)")
	fmt.Println("  - Batch size: 16 (50ms timeout)")
	fmt.Println("  - Cache: 1000 entries (30min TTL)")

	// 2. Load TensorFlow model
	// In a real application, replace with your actual model loading
	model := loadTensorFlowModel("./models/image_classifier")
	if model == nil {
		log.Fatal("Failed to load model")
	}

	// Register the model
	engine.RegisterModel("image-classifier", model)
	fmt.Println("\n✓ Registered TensorFlow model: image-classifier")

	// 3. Add preprocessing pipeline
	engine.AddPreprocessor(func(ctx context.Context, req inference.InferenceRequest) (inference.InferenceRequest, error) {
		// Example preprocessing:
		// - Resize images to 224x224
		// - Normalize pixel values to [0, 1]
		// - Convert to model input format
		
		fmt.Printf("  → Preprocessing request %s\n", req.ID)
		
		// Your preprocessing logic here
		req.Input = preprocessImage(req.Input)
		
		return req, nil
	})

	// 4. Add postprocessing pipeline
	engine.AddPostprocessor(func(ctx context.Context, resp inference.InferenceResponse) (inference.InferenceResponse, error) {
		// Example postprocessing:
		// - Convert logits to probabilities
		// - Apply confidence threshold
		// - Format output labels
		
		fmt.Printf("  ← Postprocessing response %s\n", resp.ID)
		
		// Your postprocessing logic here
		resp.Output = postprocessOutput(resp.Output)
		
		return resp, nil
	})

	fmt.Println("\n✓ Added preprocessing and postprocessing stages")

	// 5. Start the engine
	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop(ctx)

	fmt.Println("\n✓ Inference engine started and ready")

	// 6. Simulate inference requests
	fmt.Println("\n--- Simulating Inference Requests ---\n")

	// Single request
	result, err := engine.Infer(ctx, inference.InferenceRequest{
		ModelID: "image-classifier",
		Input:   loadImage("test_image.jpg"),
		Options: map[string]interface{}{
			"confidence_threshold": 0.5,
		},
	})
	if err != nil {
		log.Printf("Inference error: %v", err)
	} else {
		fmt.Printf("Single request result: %v\n", result.Output)
	}

	// Batch of requests (will be automatically batched)
	fmt.Println("\nSending batch of 20 requests...")
	start := time.Now()
	
	for i := 0; i < 20; i++ {
		go func(idx int) {
			result, err := engine.Infer(ctx, inference.InferenceRequest{
				ModelID: "image-classifier",
				Input:   generateRandomImage(224, 224),
			})
			if err != nil {
				log.Printf("Request %d error: %v", idx, err)
			} else {
				fmt.Printf("  Request %d: %v\n", idx, result.Output)
			}
		}(i)
	}
	
	time.Sleep(2 * time.Second) // Wait for batch processing
	
	fmt.Printf("\nBatch completed in %v\n", time.Since(start))

	// 7. Display statistics
	fmt.Println("\n--- Engine Statistics ---\n")
	
	stats := engine.Stats()
	fmt.Printf("Requests Processed: %d\n", stats.RequestsProcessed)
	fmt.Printf("Batches Processed:  %d\n", stats.BatchesProcessed)
	fmt.Printf("Cache Hit Rate:     %.2f%%\n", stats.CacheHitRate*100)
	fmt.Printf("Average Latency:    %v\n", stats.AverageLatency)
	fmt.Printf("Average Batch Size: %.1f\n", stats.AverageBatchSize)
	fmt.Printf("Throughput:         %.1f req/s\n", stats.ThroughputPerSec)
	fmt.Printf("Active Workers:     %d\n", stats.ActiveWorkers)

	// 8. Health check
	health := engine.Health(ctx)
	fmt.Printf("\nEngine Health: %s\n", health.Status)
	fmt.Printf("Queue Size:    %d\n", health.QueueSize)

	fmt.Println("\n=== Example Complete ===")
}

// loadTensorFlowModel loads a TensorFlow SavedModel
// Replace with actual TensorFlow loading logic
func loadTensorFlowModel(path string) models.Model {
	// Example stub - replace with actual TensorFlow model loading:
	// 
	// import tf "github.com/tensorflow/tensorflow/tensorflow/go"
	// 
	// model, err := tf.LoadSavedModel(path, []string{"serve"}, nil)
	// if err != nil {
	//     return nil
	// }
	// 
	// return &TensorFlowModelWrapper{model: model}
	
	return &MockModel{name: "image-classifier"}
}

// MockModel is a placeholder for demonstration
type MockModel struct {
	name string
}

func (m *MockModel) Predict(ctx context.Context, input interface{}) (interface{}, error) {
	// Simulate model inference
	time.Sleep(10 * time.Millisecond)
	return map[string]float64{
		"cat":  0.85,
		"dog":  0.12,
		"bird": 0.03,
	}, nil
}

func (m *MockModel) Name() string {
	return m.name
}

func (m *MockModel) Type() string {
	return "tensorflow"
}

// Helper functions
func preprocessImage(input interface{}) interface{} {
	// Implement image preprocessing:
	// - Resize to model input size
	// - Normalize pixel values
	// - Convert color space if needed
	return input
}

func postprocessOutput(output interface{}) interface{} {
	// Implement output postprocessing:
	// - Apply softmax
	// - Filter by confidence
	// - Format labels
	return output
}

func loadImage(path string) interface{} {
	// Load image from file
	return []byte("image data")
}

func generateRandomImage(width, height int) interface{} {
	// Generate random image data for testing
	return make([]byte, width*height*3)
}
