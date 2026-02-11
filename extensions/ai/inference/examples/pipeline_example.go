//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/ai/inference"
	"github.com/xraph/forge/extensions/ai/models"
)

// PipelineExample demonstrates complex pre/post processing pipelines.
// This example shows:
// - Multi-stage preprocessing
// - Complex data transformations
// - Pipeline interceptors
// - Error handling in pipelines
func main() {
	fmt.Println("=== Inference Pipeline Example ===\n")

	ctx := context.Background()

	// 1. Create engine with pipeline support
	engine := inference.NewInferenceEngine(inference.InferenceConfig{
		Workers:        4,
		BatchSize:      8,
		EnableBatching: true,
		EnableCaching:  true,
	})

	// 2. Register model
	model := &SentimentAnalysisModel{name: "sentiment"}
	engine.RegisterModel("sentiment", model)

	fmt.Println("✓ Registered sentiment analysis model\n")

	// 3. Build preprocessing pipeline
	fmt.Println("--- Building Preprocessing Pipeline ---\n")

	// Stage 1: Input validation
	engine.AddPreprocessor(func(ctx context.Context, req inference.InferenceRequest) (inference.InferenceRequest, error) {
		fmt.Println("  [Stage 1] Validating input...")

		text, ok := req.Input.(string)
		if !ok {
			return req, fmt.Errorf("input must be string")
		}

		if len(text) == 0 {
			return req, fmt.Errorf("input cannot be empty")
		}

		if len(text) > 5000 {
			return req, fmt.Errorf("input too long (max 5000 chars)")
		}

		return req, nil
	})

	// Stage 2: Text cleaning
	engine.AddPreprocessor(func(ctx context.Context, req inference.InferenceRequest) (inference.InferenceRequest, error) {
		fmt.Println("  [Stage 2] Cleaning text...")

		text := req.Input.(string)

		// Remove extra whitespace
		text = strings.TrimSpace(text)
		text = strings.Join(strings.Fields(text), " ")

		// Convert to lowercase
		text = strings.ToLower(text)

		// Remove special characters
		text = removeSpecialChars(text)

		req.Input = text
		return req, nil
	})

	// Stage 3: Tokenization
	engine.AddPreprocessor(func(ctx context.Context, req inference.InferenceRequest) (inference.InferenceRequest, error) {
		fmt.Println("  [Stage 3] Tokenizing...")

		text := req.Input.(string)
		tokens := tokenize(text)

		// Store both text and tokens
		req.Metadata = map[string]interface{}{
			"original_text": text,
			"tokens":        tokens,
			"token_count":   len(tokens),
		}

		req.Input = tokens
		return req, nil
	})

	// Stage 4: Feature extraction
	engine.AddPreprocessor(func(ctx context.Context, req inference.InferenceRequest) (inference.InferenceRequest, error) {
		fmt.Println("  [Stage 4] Extracting features...")

		tokens := req.Input.([]string)
		features := extractFeatures(tokens)

		req.Metadata["features"] = features
		req.Input = features

		return req, nil
	})

	// 4. Build postprocessing pipeline
	fmt.Println("\n--- Building Postprocessing Pipeline ---\n")

	// Stage 1: Score normalization
	engine.AddPostprocessor(func(ctx context.Context, resp inference.InferenceResponse) (inference.InferenceResponse, error) {
		fmt.Println("  [Stage 1] Normalizing scores...")

		scores := resp.Output.(map[string]float64)
		normalized := normalizeScores(scores)

		resp.Output = normalized
		return resp, nil
	})

	// Stage 2: Apply confidence threshold
	engine.AddPostprocessor(func(ctx context.Context, resp inference.InferenceResponse) (inference.InferenceResponse, error) {
		fmt.Println("  [Stage 2] Applying confidence threshold...")

		scores := resp.Output.(map[string]float64)
		threshold := 0.5

		// Find highest score
		maxScore := 0.0
		maxLabel := ""
		for label, score := range scores {
			if score > maxScore {
				maxScore = score
				maxLabel = label
			}
		}

		// Apply threshold
		if maxScore < threshold {
			resp.Output = map[string]interface{}{
				"prediction": "uncertain",
				"confidence": maxScore,
				"scores":     scores,
			}
		} else {
			resp.Output = map[string]interface{}{
				"prediction": maxLabel,
				"confidence": maxScore,
				"scores":     scores,
			}
		}

		return resp, nil
	})

	// Stage 3: Add explanations
	engine.AddPostprocessor(func(ctx context.Context, resp inference.InferenceResponse) (inference.InferenceResponse, error) {
		fmt.Println("  [Stage 3] Generating explanation...")

		output := resp.Output.(map[string]interface{})
		prediction := output["prediction"].(string)
		confidence := output["confidence"].(float64)

		// Add human-readable explanation
		explanation := generateExplanation(prediction, confidence)
		output["explanation"] = explanation

		resp.Output = output
		return resp, nil
	})

	// Stage 4: Format output
	engine.AddPostprocessor(func(ctx context.Context, resp inference.InferenceResponse) (inference.InferenceResponse, error) {
		fmt.Println("  [Stage 4] Formatting output...")

		// Convert to pretty JSON
		jsonBytes, _ := json.MarshalIndent(resp.Output, "", "  ")

		resp.Metadata["json_output"] = string(jsonBytes)
		return resp, nil
	})

	// 5. Add pipeline interceptor for logging
	engine.AddInterceptor(func(ctx context.Context, req inference.InferenceRequest) (inference.InferenceRequest, error) {
		fmt.Printf("\n→ Processing request %s\n", req.ID)
		fmt.Printf("  Model: %s\n", req.ModelID)
		fmt.Printf("  Input type: %T\n", req.Input)
		return req, nil
	})

	// 6. Start engine
	if err := engine.Start(ctx); err != nil {
		log.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop(ctx)

	fmt.Println("\n✓ Pipeline configured and engine started\n")

	// 7. Process sample requests
	fmt.Println("--- Processing Sample Requests ---\n")

	samples := []string{
		"This product is absolutely amazing! I love it so much!",
		"Terrible experience. Worst purchase ever. Very disappointed.",
		"It's okay, I guess. Nothing special but not bad either.",
		"", // Empty (will fail validation)
		"The quick brown fox jumps over the lazy dog.", // Neutral
	}

	for i, text := range samples {
		fmt.Printf("\n=== Request %d ===\n", i+1)
		fmt.Printf("Input: %q\n\n", text)

		result, err := engine.Infer(ctx, inference.InferenceRequest{
			ID:      fmt.Sprintf("req-%d", i+1),
			ModelID: "sentiment",
			Input:   text,
		})

		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			continue
		}

		fmt.Println("\nResult:")
		jsonOutput := result.Metadata["json_output"].(string)
		fmt.Println(jsonOutput)

		fmt.Println("\n" + strings.Repeat("-", 60))
	}

	// 8. Display pipeline statistics
	fmt.Println("\n--- Pipeline Statistics ---\n")

	stats := engine.Stats()
	fmt.Printf("Total Requests:     %d\n", stats.RequestsReceived)
	fmt.Printf("Successful:         %d\n", stats.RequestsProcessed)
	fmt.Printf("Failed:             %d\n", stats.RequestsError)
	fmt.Printf("Average Latency:    %v\n", stats.AverageLatency)

	fmt.Println("\n=== Example Complete ===")
}

// SentimentAnalysisModel simulates a sentiment analysis model
type SentimentAnalysisModel struct {
	name string
}

func (m *SentimentAnalysisModel) Predict(ctx context.Context, input interface{}) (interface{}, error) {
	// Simulate model inference
	time.Sleep(10 * time.Millisecond)

	// Simple rule-based sentiment for demo
	features := input.(map[string]float64)

	return map[string]float64{
		"positive": features["positive_score"],
		"negative": features["negative_score"],
		"neutral":  features["neutral_score"],
	}, nil
}

func (m *SentimentAnalysisModel) Name() string {
	return m.name
}

func (m *SentimentAnalysisModel) Type() string {
	return "sentiment-analysis"
}

// Helper functions
func removeSpecialChars(text string) string {
	// Keep only alphanumeric and spaces
	result := ""
	for _, char := range text {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == ' ' {
			result += string(char)
		}
	}
	return result
}

func tokenize(text string) []string {
	// Simple whitespace tokenization
	return strings.Fields(text)
}

func extractFeatures(tokens []string) map[string]float64 {
	// Extract simple sentiment features
	positiveWords := map[string]bool{
		"amazing": true, "love": true, "great": true, "excellent": true,
		"wonderful": true, "fantastic": true, "good": true,
	}

	negativeWords := map[string]bool{
		"terrible": true, "hate": true, "bad": true, "awful": true,
		"worst": true, "disappointed": true, "poor": true,
	}

	posCount := 0.0
	negCount := 0.0

	for _, token := range tokens {
		if positiveWords[token] {
			posCount++
		}
		if negativeWords[token] {
			negCount++
		}
	}

	total := float64(len(tokens))
	if total == 0 {
		total = 1
	}

	return map[string]float64{
		"positive_score": (posCount / total) * 100,
		"negative_score": (negCount / total) * 100,
		"neutral_score":  ((total - posCount - negCount) / total) * 100,
	}
}

func normalizeScores(scores map[string]float64) map[string]float64 {
	// Normalize scores to sum to 1.0
	sum := 0.0
	for _, score := range scores {
		sum += score
	}

	if sum == 0 {
		return scores
	}

	normalized := make(map[string]float64)
	for label, score := range scores {
		normalized[label] = score / sum
	}

	return normalized
}

func generateExplanation(prediction string, confidence float64) string {
	switch prediction {
	case "positive":
		return fmt.Sprintf("The text expresses a positive sentiment with %.0f%% confidence.", confidence*100)
	case "negative":
		return fmt.Sprintf("The text expresses a negative sentiment with %.0f%% confidence.", confidence*100)
	case "neutral":
		return fmt.Sprintf("The text expresses a neutral sentiment with %.0f%% confidence.", confidence*100)
	case "uncertain":
		return fmt.Sprintf("Unable to determine sentiment with sufficient confidence (%.0f%%).", confidence*100)
	default:
		return "Unknown prediction."
	}
}
