package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xraph/forge/extensions/ai/inference"
	"github.com/xraph/forge/extensions/ai/models"
)

// BatchInferenceExample demonstrates high-throughput batch processing.
// This example shows:
// - Processing large batches of requests
// - Maximizing throughput with optimal batching
// - Monitoring performance metrics
// - Handling backpressure
func main() {
	fmt.Println("=== Batch Inference Example ===\n")

	ctx := context.Background()

	// 1. Create engine optimized for maximum throughput
	engine := inference.NewInferenceEngine(inference.InferenceConfig{
		Workers:        8,
		BatchSize:      64, // Large batch size for throughput
		BatchTimeout:   200 * time.Millisecond,
		EnableBatching: true,
		EnableCaching:  true,
		EnableScaling:  false, // Fixed workers for predictable performance
		CacheSize:      5000,
		CacheTTL:       10 * time.Minute,
		MaxQueueSize:   50000, // Large queue for batch jobs
		RequestTimeout: 30 * time.Second,
	})

	fmt.Println("✓ Inference engine configured for batch processing:")
	fmt.Println("  - 8 workers (fixed)")
	fmt.Println("  - Batch size: 64 (200ms timeout)")
	fmt.Println("  - Queue size: 50,000")
	fmt.Println("  - Cache: 5,000 entries")

	// 2. Load model
	model := &BatchOptimizedModel{name: "batch-classifier"}
	engine.RegisterModel("batch-model", model)
	fmt.Println("\n✓ Registered batch-optimized model")

	// 3. Start engine
	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop(ctx)

	// 4. Process large batch job
	fmt.Println("\n--- Processing Large Batch Job ---\n")

	totalRequests := 10000
	fmt.Printf("Submitting %d requests...\n\n", totalRequests)

	startTime := time.Now()
	
	// Submit requests in parallel
	var wg sync.WaitGroup
	results := make(chan inference.InferenceResponse, totalRequests)
	errors := make(chan error, totalRequests)

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			
			result, err := engine.Infer(ctx, inference.InferenceRequest{
				ID:      fmt.Sprintf("req-%d", idx),
				ModelID: "batch-model",
				Input:   generateBatchInput(idx),
			})
			
			if err != nil {
				errors <- err
			} else {
				results <- result
			}
		}(i)
	}

	// Monitor progress
	go monitorProgress(engine, totalRequests, startTime)

	// Wait for completion
	wg.Wait()
	close(results)
	close(errors)

	duration := time.Since(startTime)

	// 5. Report results
	fmt.Println("\n--- Batch Job Results ---\n")

	successCount := len(results)
	errorCount := len(errors)

	fmt.Printf("Total Requests:     %d\n", totalRequests)
	fmt.Printf("Successful:         %d (%.2f%%)\n", successCount, float64(successCount)/float64(totalRequests)*100)
	fmt.Printf("Failed:             %d\n", errorCount)
	fmt.Printf("Total Duration:     %v\n", duration)
	fmt.Printf("Throughput:         %.1f req/s\n", float64(totalRequests)/duration.Seconds())
	fmt.Printf("Average Latency:    %v\n", duration/time.Duration(totalRequests))

	// 6. Display final statistics
	fmt.Println("\n--- Engine Statistics ---\n")
	
	stats := engine.Stats()
	fmt.Printf("Batches Processed:  %d\n", stats.BatchesProcessed)
	fmt.Printf("Average Batch Size: %.1f\n", stats.AverageBatchSize)
	fmt.Printf("Cache Hit Rate:     %.2f%%\n", stats.CacheHitRate*100)
	fmt.Printf("Error Rate:         %.2f%%\n", stats.ErrorRate*100)

	// 7. Batch size distribution
	fmt.Println("\n--- Batch Size Distribution ---")
	displayBatchHistogram(stats.BatchSizeHistogram)

	// 8. Demonstrate streaming batch results
	fmt.Println("\n--- Streaming Batch Processing ---\n")
	
	streamingBatchDemo(ctx, engine, 1000)

	fmt.Println("\n=== Example Complete ===")
}

// BatchOptimizedModel simulates a model optimized for batch processing
type BatchOptimizedModel struct {
	name string
}

func (m *BatchOptimizedModel) Predict(ctx context.Context, input interface{}) (interface{}, error) {
	// Simulate batch-optimized inference
	// Real batched operations are much faster per item
	time.Sleep(5 * time.Millisecond)
	
	return map[string]interface{}{
		"class":      "positive",
		"confidence": 0.89,
		"embeddings": []float64{0.1, 0.2, 0.3},
	}, nil
}

func (m *BatchOptimizedModel) Name() string {
	return m.name
}

func (m *BatchOptimizedModel) Type() string {
	return "batch-optimized"
}

func (m *BatchOptimizedModel) BatchPredict(ctx context.Context, inputs []interface{}) ([]interface{}, error) {
	// Batch prediction is more efficient
	results := make([]interface{}, len(inputs))
	
	// Simulate efficient batch processing
	time.Sleep(time.Duration(len(inputs)) * time.Millisecond)
	
	for i := range inputs {
		results[i] = map[string]interface{}{
			"class":      "positive",
			"confidence": 0.89,
		}
	}
	
	return results, nil
}

// Helper functions
func generateBatchInput(idx int) interface{} {
	return map[string]interface{}{
		"id":   idx,
		"data": fmt.Sprintf("input-%d", idx),
	}
}

func monitorProgress(engine *inference.InferenceEngine, total int, startTime time.Time) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastProcessed := int64(0)
	
	for range ticker.C {
		stats := engine.Stats()
		processed := stats.RequestsProcessed
		
		if processed >= int64(total) {
			return
		}
		
		throughput := float64(processed-lastProcessed) // per second
		elapsed := time.Since(startTime)
		remaining := time.Duration(float64(total-int(processed)) / throughput * float64(time.Second))
		
		fmt.Printf("\rProgress: %d/%d (%.1f%%) | Throughput: %.0f req/s | Queue: %d | ETA: %v",
			processed,
			total,
			float64(processed)/float64(total)*100,
			throughput,
			stats.QueueSize,
			remaining.Round(time.Second),
		)
		
		lastProcessed = processed
	}
}

func displayBatchHistogram(histogram map[int]int64) {
	if len(histogram) == 0 {
		fmt.Println("  No data available")
		return
	}
	
	// Find max count for scaling
	maxCount := int64(0)
	for _, count := range histogram {
		if count > maxCount {
			maxCount = count
		}
	}
	
	// Display histogram
	for batchSize := 1; batchSize <= 64; batchSize++ {
		count := histogram[batchSize]
		if count == 0 {
			continue
		}
		
		// Scale bar to 50 characters
		barLength := int(float64(count) / float64(maxCount) * 50)
		bar := ""
		for i := 0; i < barLength; i++ {
			bar += "█"
		}
		
		fmt.Printf("  Size %2d: %-50s %d\n", batchSize, bar, count)
	}
}

func streamingBatchDemo(ctx context.Context, engine *inference.InferenceEngine, count int) {
	fmt.Printf("Processing %d requests with streaming results...\n", count)
	
	resultChan := make(chan inference.InferenceResponse, 100)
	errorChan := make(chan error, 100)
	
	// Producer: submit requests
	go func() {
		for i := 0; i < count; i++ {
			go func(idx int) {
				result, err := engine.Infer(ctx, inference.InferenceRequest{
					ModelID: "batch-model",
					Input:   generateBatchInput(idx),
				})
				
				if err != nil {
					errorChan <- err
				} else {
					resultChan <- result
				}
			}(i)
		}
	}()
	
	// Consumer: process results as they arrive
	processed := 0
	errors := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for processed+errors < count {
		select {
		case <-resultChan:
			processed++
		case <-errorChan:
			errors++
		case <-ticker.C:
			fmt.Printf("\r  Processed: %d | Errors: %d", processed, errors)
		}
	}
	
	fmt.Printf("\r  Completed: %d successful, %d errors\n", processed, errors)
}
