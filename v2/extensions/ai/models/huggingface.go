package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/v2/internal/logger"
)

// HuggingFaceAdapter implements ModelAdapter for Hugging Face models
type HuggingFaceAdapter struct {
	config HuggingFaceConfig
	client *http.Client
	logger logger.Logger
}

// HuggingFaceConfig contains configuration for Hugging Face models
type HuggingFaceConfig struct {
	APIKey     string        `json:"api_key"`
	BaseURL    string        `json:"base_url"`
	Timeout    time.Duration `json:"timeout"`
	MaxRetries int           `json:"max_retries"`
	UseCache   bool          `json:"use_cache"`
	CacheTTL   time.Duration `json:"cache_ttl"`
	TaskType   string        `json:"task_type"` // text-generation, text-classification, etc.
}

// HuggingFaceModel implements Model for Hugging Face models
type HuggingFaceModel struct {
	*BaseModel
	adapter     *HuggingFaceAdapter
	modelID     string
	task        string
	apiEndpoint string
	pipeline    string
}

// HuggingFaceRequest represents a request to Hugging Face API
type HuggingFaceRequest struct {
	Inputs     interface{}            `json:"inputs"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Options    map[string]interface{} `json:"options,omitempty"`
}

// HuggingFaceResponse represents a response from Hugging Face API
type HuggingFaceResponse struct {
	GeneratedText string                 `json:"generated_text,omitempty"`
	Label         string                 `json:"label,omitempty"`
	Score         float64                `json:"score,omitempty"`
	Embeddings    []float64              `json:"embeddings,omitempty"`
	Error         string                 `json:"error,omitempty"`
	Warnings      []string               `json:"warnings,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// NewHuggingFaceAdapter creates a new Hugging Face adapter
func NewHuggingFaceAdapter(config HuggingFaceConfig, logger logger.Logger) ModelAdapter {
	if config.BaseURL == "" {
		config.BaseURL = "https://api-inference.huggingface.co"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 1 * time.Hour
	}

	return &HuggingFaceAdapter{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		logger: logger,
	}
}

// Name returns the adapter name
func (a *HuggingFaceAdapter) Name() string {
	return "huggingface"
}

// Framework returns the ML framework
func (a *HuggingFaceAdapter) Framework() MLFramework {
	return MLFrameworkHuggingFace
}

// SupportsModel checks if the adapter supports the model configuration
func (a *HuggingFaceAdapter) SupportsModel(config ModelConfig) bool {
	return config.Framework == MLFrameworkHuggingFace ||
		strings.HasPrefix(config.ModelPath, "huggingface://") ||
		a.isHuggingFaceModel(config.ModelPath)
}

// CreateModel creates a new Hugging Face model
func (a *HuggingFaceAdapter) CreateModel(config ModelConfig) (Model, error) {
	if !a.SupportsModel(config) {
		return nil, fmt.Errorf("model configuration not supported by Hugging Face adapter")
	}

	// Extract model ID from path
	modelID := a.extractModelID(config.ModelPath)
	if modelID == "" {
		return nil, fmt.Errorf("invalid Hugging Face model path: %s", config.ModelPath)
	}

	// Determine task type and pipeline
	task := a.config.TaskType
	if task == "" {
		task = a.inferTaskType(config.Type)
	}

	pipeline := a.getPipeline(task)
	apiEndpoint := fmt.Sprintf("%s/models/%s", a.config.BaseURL, modelID)

	// Create base model
	baseModel := NewBaseModel(config, a.logger)

	// Set input/output schemas based on task type
	a.setModelSchemas(baseModel, task)

	model := &HuggingFaceModel{
		BaseModel:   baseModel,
		adapter:     a,
		modelID:     modelID,
		task:        task,
		apiEndpoint: apiEndpoint,
		pipeline:    pipeline,
	}

	return model, nil
}

// ValidateConfig validates the model configuration
func (a *HuggingFaceAdapter) ValidateConfig(config ModelConfig) error {
	if config.Framework != MLFrameworkHuggingFace {
		return fmt.Errorf("framework must be 'huggingface'")
	}

	if config.ModelPath == "" {
		return fmt.Errorf("model path is required")
	}

	if a.config.APIKey == "" {
		return fmt.Errorf("Hugging Face API key is required")
	}

	// Validate model ID format
	modelID := a.extractModelID(config.ModelPath)
	if modelID == "" {
		return fmt.Errorf("invalid model path format")
	}

	return nil
}

// Load loads the Hugging Face model
func (m *HuggingFaceModel) Load(ctx context.Context) error {
	// Call base Load first
	if err := m.BaseModel.Load(ctx); err != nil {
		return err
	}

	// Test model availability with a simple request
	if err := m.testModelAvailability(ctx); err != nil {
		return fmt.Errorf("model not available: %w", err)
	}

	if m.adapter.logger != nil {
		m.adapter.logger.Info("Hugging Face model loaded successfully",
			logger.String("model_id", m.modelID),
			logger.String("task", m.task),
			logger.String("pipeline", m.pipeline),
		)
	}

	return nil
}

// Predict performs prediction using Hugging Face API
func (m *HuggingFaceModel) Predict(ctx context.Context, input ModelInput) (ModelOutput, error) {
	startTime := time.Now()

	// Validate input
	if err := m.ValidateInput(input); err != nil {
		return ModelOutput{}, err
	}

	// Prepare request
	request := m.prepareRequest(input)

	// Make API call with retries
	response, err := m.makeAPICall(ctx, request)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, err
	}

	// Process response
	output := m.processResponse(response, input, time.Since(startTime))

	// Update metrics
	m.updateMetrics(time.Since(startTime), nil)

	return output, nil
}

// BatchPredict performs batch prediction
func (m *HuggingFaceModel) BatchPredict(ctx context.Context, inputs []ModelInput) ([]ModelOutput, error) {
	if len(inputs) == 0 {
		return []ModelOutput{}, nil
	}

	// Process inputs in parallel with rate limiting
	outputs := make([]ModelOutput, len(inputs))
	errors := make([]error, len(inputs))

	// Simple parallel processing with goroutines
	const maxConcurrency = 5
	sem := make(chan struct{}, maxConcurrency)

	for i, input := range inputs {
		sem <- struct{}{}
		go func(idx int, inp ModelInput) {
			defer func() { <-sem }()
			output, err := m.Predict(ctx, inp)
			outputs[idx] = output
			errors[idx] = err
		}(i, input)
	}

	// Wait for all to complete
	for i := 0; i < maxConcurrency; i++ {
		sem <- struct{}{}
	}

	// Check for errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("batch prediction failed at index %d: %w", i, err)
		}
	}

	return outputs, nil
}

// testModelAvailability tests if the model is available
func (m *HuggingFaceModel) testModelAvailability(ctx context.Context) error {
	// Create a simple test request
	testInput := ModelInput{
		Data: "test",
		Metadata: map[string]interface{}{
			"test": true,
		},
	}

	request := m.prepareRequest(testInput)

	// Make test API call
	_, err := m.makeAPICall(ctx, request)
	return err
}

// prepareRequest prepares the API request
func (m *HuggingFaceModel) prepareRequest(input ModelInput) HuggingFaceRequest {
	request := HuggingFaceRequest{
		Inputs:     input.Data,
		Parameters: make(map[string]interface{}),
		Options:    make(map[string]interface{}),
	}

	// Add task-specific parameters
	switch m.task {
	case "text-generation":
		if maxLength, ok := input.Features["max_length"]; ok {
			request.Parameters["max_length"] = maxLength
		}
		if temperature, ok := input.Features["temperature"]; ok {
			request.Parameters["temperature"] = temperature
		}
		if topK, ok := input.Features["top_k"]; ok {
			request.Parameters["top_k"] = topK
		}
		if topP, ok := input.Features["top_p"]; ok {
			request.Parameters["top_p"] = topP
		}

	case "text-classification":
		if returnAllScores, ok := input.Features["return_all_scores"]; ok {
			request.Parameters["return_all_scores"] = returnAllScores
		}

	case "question-answering":
		if context, ok := input.Features["context"]; ok {
			request.Inputs = map[string]interface{}{
				"question": input.Data,
				"context":  context,
			}
		}

	case "summarization":
		if maxLength, ok := input.Features["max_length"]; ok {
			request.Parameters["max_length"] = maxLength
		}
		if minLength, ok := input.Features["min_length"]; ok {
			request.Parameters["min_length"] = minLength
		}
	}

	// Add caching options if enabled
	if m.adapter.config.UseCache {
		request.Options["use_cache"] = true
		request.Options["wait_for_model"] = true
	}

	return request
}

// makeAPICall makes the API call to Hugging Face
func (m *HuggingFaceModel) makeAPICall(ctx context.Context, request HuggingFaceRequest) (*HuggingFaceResponse, error) {
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < m.adapter.config.MaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", m.apiEndpoint, strings.NewReader(string(requestBody)))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+m.adapter.config.APIKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := m.adapter.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("API returned status %d", resp.StatusCode)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
		}

		var response HuggingFaceResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if response.Error != "" {
			return nil, fmt.Errorf("API error: %s", response.Error)
		}

		return &response, nil
	}

	return nil, fmt.Errorf("API call failed after %d attempts: %w", m.adapter.config.MaxRetries, lastErr)
}

// processResponse processes the API response
func (m *HuggingFaceModel) processResponse(response *HuggingFaceResponse, input ModelInput, latency time.Duration) ModelOutput {
	output := ModelOutput{
		Predictions: []Prediction{},
		Confidence:  response.Score,
		Metadata:    make(map[string]interface{}),
		RequestID:   input.RequestID,
		Timestamp:   time.Now(),
		Latency:     latency,
	}

	// Process based on task type
	switch m.task {
	case "text-generation":
		if response.GeneratedText != "" {
			output.Predictions = append(output.Predictions, Prediction{
				Label:      "generated_text",
				Value:      response.GeneratedText,
				Confidence: 1.0,
			})
		}

	case "text-classification":
		if response.Label != "" {
			output.Predictions = append(output.Predictions, Prediction{
				Label:       response.Label,
				Value:       response.Label,
				Confidence:  response.Score,
				Probability: response.Score,
			})
		}

	case "feature-extraction":
		if len(response.Embeddings) > 0 {
			output.Predictions = append(output.Predictions, Prediction{
				Label:      "embeddings",
				Value:      response.Embeddings,
				Confidence: 1.0,
			})
		}

	default:
		// Generic response handling
		if response.Label != "" {
			output.Predictions = append(output.Predictions, Prediction{
				Label:      response.Label,
				Value:      response.Label,
				Confidence: response.Score,
			})
		}
	}

	// Add metadata
	if response.Metadata != nil {
		for k, v := range response.Metadata {
			output.Metadata[k] = v
		}
	}

	if len(response.Warnings) > 0 {
		output.Metadata["warnings"] = response.Warnings
	}

	output.Metadata["model_id"] = m.modelID
	output.Metadata["task"] = m.task
	output.Metadata["pipeline"] = m.pipeline

	return output
}

// Helper methods

func (a *HuggingFaceAdapter) extractModelID(modelPath string) string {
	// Handle different formats:
	// - huggingface://bert-base-uncased
	// - bert-base-uncased
	// - https://huggingface.co/bert-base-uncased

	if strings.HasPrefix(modelPath, "huggingface://") {
		return strings.TrimPrefix(modelPath, "huggingface://")
	}

	if strings.HasPrefix(modelPath, "https://huggingface.co/") {
		return strings.TrimPrefix(modelPath, "https://huggingface.co/")
	}

	// Assume it's already a model ID
	return modelPath
}

func (a *HuggingFaceAdapter) isHuggingFaceModel(modelPath string) bool {
	return strings.Contains(modelPath, "huggingface") ||
		strings.Contains(modelPath, "/") && !strings.Contains(modelPath, ".")
}

func (a *HuggingFaceAdapter) inferTaskType(modelType ModelType) string {
	switch modelType {
	case ModelTypeClassification:
		return "text-classification"
	case ModelTypeNLP:
		return "text-generation"
	case ModelTypeLLM:
		return "text-generation"
	default:
		return "text-generation"
	}
}

func (a *HuggingFaceAdapter) getPipeline(task string) string {
	switch task {
	case "text-generation":
		return "text-generation"
	case "text-classification":
		return "text-classification"
	case "question-answering":
		return "question-answering"
	case "summarization":
		return "summarization"
	case "feature-extraction":
		return "feature-extraction"
	case "token-classification":
		return "token-classification"
	case "fill-mask":
		return "fill-mask"
	case "translation":
		return "translation"
	default:
		return "text-generation"
	}
}

func (a *HuggingFaceAdapter) setModelSchemas(model *BaseModel, task string) {
	switch task {
	case "text-generation":
		model.SetInputSchema(InputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Input text for generation",
				},
				"max_length": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum length of generated text",
					"default":     100,
				},
				"temperature": map[string]interface{}{
					"type":        "number",
					"description": "Sampling temperature",
					"minimum":     0.1,
					"maximum":     2.0,
					"default":     1.0,
				},
			},
			Required: []string{"text"},
		})

		model.SetOutputSchema(OutputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"generated_text": map[string]interface{}{
					"type":        "string",
					"description": "Generated text",
				},
			},
		})

	case "text-classification":
		model.SetInputSchema(InputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Input text for classification",
				},
			},
			Required: []string{"text"},
		})

		model.SetOutputSchema(OutputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"label": map[string]interface{}{
					"type":        "string",
					"description": "Predicted label",
				},
				"score": map[string]interface{}{
					"type":        "number",
					"description": "Confidence score",
				},
			},
		})

	case "feature-extraction":
		model.SetInputSchema(InputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Input text for feature extraction",
				},
			},
			Required: []string{"text"},
		})

		model.SetOutputSchema(OutputSchema{
			Type: "array",
			Properties: map[string]interface{}{
				"embeddings": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "number"},
					"description": "Feature embeddings",
				},
			},
		})
	}
}
