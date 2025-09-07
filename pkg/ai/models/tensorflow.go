package models

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// TensorFlowAdapter implements ModelAdapter for TensorFlow models
type TensorFlowAdapter struct {
	logger common.Logger
}

// NewTensorFlowAdapter creates a new TensorFlow adapter
func NewTensorFlowAdapter(logger common.Logger) ModelAdapter {
	return &TensorFlowAdapter{
		logger: logger,
	}
}

// Name returns the adapter name
func (a *TensorFlowAdapter) Name() string {
	return "tensorflow"
}

// Framework returns the ML framework
func (a *TensorFlowAdapter) Framework() MLFramework {
	return MLFrameworkTensorFlow
}

// SupportsModel checks if this adapter supports the given model configuration
func (a *TensorFlowAdapter) SupportsModel(config ModelConfig) bool {
	if config.Framework != MLFrameworkTensorFlow {
		return false
	}

	// Check if model path exists and has correct extension
	if config.ModelPath == "" {
		return false
	}

	// TensorFlow models can be in various formats
	validExtensions := []string{".pb", ".h5", ".savedmodel", ".tflite"}
	for _, ext := range validExtensions {
		if strings.HasSuffix(config.ModelPath, ext) {
			return true
		}
	}

	// Check if it's a SavedModel directory
	if strings.Contains(config.ModelPath, "saved_model") {
		return true
	}

	return false
}

// CreateModel creates a new TensorFlow model
func (a *TensorFlowAdapter) CreateModel(config ModelConfig) (Model, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, err
	}

	model := &TensorFlowModel{
		BaseModel: NewBaseModel(config, a.logger),
		adapter:   a,
	}

	// Set TensorFlow-specific input/output schemas
	model.SetInputSchema(a.getDefaultInputSchema(config))
	model.SetOutputSchema(a.getDefaultOutputSchema(config))

	// Set TensorFlow-specific metadata
	metadata := map[string]interface{}{
		"framework": "tensorflow",
		"version":   "2.x",
		"format":    a.detectModelFormat(config.ModelPath),
	}
	model.SetMetadata(metadata)

	return model, nil
}

// ValidateConfig validates the model configuration
func (a *TensorFlowAdapter) ValidateConfig(config ModelConfig) error {
	if config.Framework != MLFrameworkTensorFlow {
		return fmt.Errorf("invalid framework: expected %s, got %s", MLFrameworkTensorFlow, config.Framework)
	}

	if config.ModelPath == "" {
		return fmt.Errorf("model path is required")
	}

	if config.ID == "" {
		return fmt.Errorf("model ID is required")
	}

	if config.Name == "" {
		return fmt.Errorf("model name is required")
	}

	// Validate batch size
	if config.BatchSize < 0 {
		return fmt.Errorf("batch size must be non-negative")
	}

	// Validate timeout
	if config.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}

	return nil
}

// detectModelFormat detects the TensorFlow model format
func (a *TensorFlowAdapter) detectModelFormat(modelPath string) string {
	ext := filepath.Ext(modelPath)
	switch ext {
	case ".pb":
		return "frozen_graph"
	case ".h5":
		return "keras"
	case ".tflite":
		return "tflite"
	default:
		if strings.Contains(modelPath, "saved_model") {
			return "saved_model"
		}
		return "unknown"
	}
}

// getDefaultInputSchema returns default input schema for TensorFlow models
func (a *TensorFlowAdapter) getDefaultInputSchema(config ModelConfig) InputSchema {
	return InputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"inputs": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "number",
				},
			},
			"shape": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "integer",
				},
			},
		},
		Required: []string{"inputs"},
		Examples: []interface{}{
			map[string]interface{}{
				"inputs": []float64{1.0, 2.0, 3.0},
				"shape":  []int{1, 3},
			},
		},
		Constraints: map[string]interface{}{
			"max_batch_size": config.BatchSize,
		},
	}
}

// getDefaultOutputSchema returns default output schema for TensorFlow models
func (a *TensorFlowAdapter) getDefaultOutputSchema(config ModelConfig) OutputSchema {
	return OutputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"predictions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"label":       map[string]interface{}{"type": "string"},
						"value":       map[string]interface{}{"type": "number"},
						"confidence":  map[string]interface{}{"type": "number"},
						"probability": map[string]interface{}{"type": "number"},
					},
				},
			},
			"probabilities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "number",
				},
			},
		},
		Examples: []interface{}{
			map[string]interface{}{
				"predictions": []map[string]interface{}{
					{
						"label":       "class_0",
						"value":       0.8,
						"confidence":  0.9,
						"probability": 0.8,
					},
				},
				"probabilities": []float64{0.8, 0.2},
			},
		},
		Description: "TensorFlow model predictions with probabilities",
	}
}

// TensorFlowModel represents a TensorFlow model
type TensorFlowModel struct {
	*BaseModel
	adapter      *TensorFlowAdapter
	session      interface{} // TensorFlow session (would be actual TF session in real implementation)
	inputTensor  interface{} // Input tensor definition
	outputTensor interface{} // Output tensor definition

	// Model-specific configuration
	useCPU     bool
	useGPU     bool
	numThreads int
}

// Load loads the TensorFlow model
func (m *TensorFlowModel) Load(ctx context.Context) error {
	// Call base load first
	if err := m.BaseModel.Load(ctx); err != nil {
		return err
	}

	config := m.GetConfig()

	// Set hardware configuration
	m.useCPU = !config.GPU
	m.useGPU = config.GPU
	m.numThreads = 1 // Default to single thread, could be configurable

	// In a real implementation, this would:
	// 1. Load the TensorFlow model from the specified path
	// 2. Create a TensorFlow session
	// 3. Initialize input/output tensors
	// 4. Validate model structure

	// Simulated loading
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond): // Simulate loading time
		// Continue
	}

	// Mock session creation
	m.session = &tensorFlowSession{
		modelPath:  config.ModelPath,
		batchSize:  config.BatchSize,
		useGPU:     m.useGPU,
		numThreads: m.numThreads,
	}

	if m.adapter.logger != nil {
		m.adapter.logger.Info("TensorFlow model loaded successfully",
			logger.String("model_id", config.ID),
			logger.String("model_path", config.ModelPath),
			logger.Bool("use_gpu", m.useGPU),
			logger.Int("num_threads", m.numThreads),
		)
	}

	return nil
}

// Unload unloads the TensorFlow model
func (m *TensorFlowModel) Unload(ctx context.Context) error {
	// Clean up TensorFlow resources
	if m.session != nil {
		// In real implementation, close TensorFlow session
		m.session = nil
	}

	m.inputTensor = nil
	m.outputTensor = nil

	// Call base unload
	return m.BaseModel.Unload(ctx)
}

// Predict performs prediction using the TensorFlow model
func (m *TensorFlowModel) Predict(ctx context.Context, input ModelInput) (ModelOutput, error) {
	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.session == nil {
		return ModelOutput{}, fmt.Errorf("TensorFlow model %s is not loaded", m.ID())
	}

	// Validate input
	if err := m.ValidateInput(input); err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("invalid input: %w", err)
	}

	// Prepare input data for TensorFlow
	tfInput, err := m.prepareInput(input)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("failed to prepare input: %w", err)
	}

	// Run inference
	tfOutput, err := m.runInference(ctx, tfInput)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("inference failed: %w", err)
	}

	// Convert TensorFlow output to ModelOutput
	output, err := m.convertOutput(tfOutput, input.RequestID)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("failed to convert output: %w", err)
	}

	output.Latency = time.Since(startTime)
	output.Timestamp = time.Now()

	// Update metrics
	m.updateMetrics(time.Since(startTime), nil)

	if m.adapter.logger != nil {
		m.adapter.logger.Debug("TensorFlow prediction completed",
			logger.String("model_id", m.ID()),
			logger.String("request_id", input.RequestID),
			logger.Duration("latency", output.Latency),
			logger.Float64("confidence", output.Confidence),
		)
	}

	return output, nil
}

// BatchPredict performs batch prediction using the TensorFlow model
func (m *TensorFlowModel) BatchPredict(ctx context.Context, inputs []ModelInput) ([]ModelOutput, error) {
	if len(inputs) == 0 {
		return []ModelOutput{}, nil
	}

	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.session == nil {
		return nil, fmt.Errorf("TensorFlow model %s is not loaded", m.ID())
	}

	// Validate batch size
	config := m.GetConfig()
	if config.BatchSize > 0 && len(inputs) > config.BatchSize {
		return nil, fmt.Errorf("batch size %d exceeds maximum %d", len(inputs), config.BatchSize)
	}

	// Prepare batch input
	batchInput, err := m.prepareBatchInput(inputs)
	if err != nil {
		m.updateErrorCount()
		return nil, fmt.Errorf("failed to prepare batch input: %w", err)
	}

	// Run batch inference
	batchOutput, err := m.runInference(ctx, batchInput)
	if err != nil {
		m.updateErrorCount()
		return nil, fmt.Errorf("batch inference failed: %w", err)
	}

	// Convert batch output
	outputs, err := m.convertBatchOutput(batchOutput, inputs)
	if err != nil {
		m.updateErrorCount()
		return nil, fmt.Errorf("failed to convert batch output: %w", err)
	}

	// Update latency for all outputs
	latency := time.Since(startTime)
	for i := range outputs {
		outputs[i].Latency = latency
		outputs[i].Timestamp = time.Now()
	}

	// Update metrics
	m.updateMetrics(latency, nil)

	if m.adapter.logger != nil {
		m.adapter.logger.Debug("TensorFlow batch prediction completed",
			logger.String("model_id", m.ID()),
			logger.Int("batch_size", len(inputs)),
			logger.Duration("latency", latency),
		)
	}

	return outputs, nil
}

// ValidateInput validates TensorFlow-specific input
func (m *TensorFlowModel) ValidateInput(input ModelInput) error {
	// Call base validation
	if err := m.BaseModel.ValidateInput(input); err != nil {
		return err
	}

	// TensorFlow-specific validation
	if input.Data == nil {
		return fmt.Errorf("input data is required")
	}

	// Validate input format based on model type
	switch data := input.Data.(type) {
	case []float64:
		if len(data) == 0 {
			return fmt.Errorf("input array cannot be empty")
		}
	case [][]float64:
		if len(data) == 0 {
			return fmt.Errorf("input matrix cannot be empty")
		}
	case map[string]interface{}:
		if _, ok := data["inputs"]; !ok {
			return fmt.Errorf("input object must contain 'inputs' field")
		}
	default:
		return fmt.Errorf("unsupported input type: %T", data)
	}

	return nil
}

// prepareInput prepares input data for TensorFlow
func (m *TensorFlowModel) prepareInput(input ModelInput) (interface{}, error) {
	// In real implementation, this would:
	// 1. Convert input data to TensorFlow tensors
	// 2. Reshape data according to model requirements
	// 3. Normalize data if needed
	// 4. Handle different input formats

	// Mock implementation
	return &tensorFlowInput{
		data:      input.Data,
		features:  input.Features,
		metadata:  input.Metadata,
		requestID: input.RequestID,
	}, nil
}

// prepareBatchInput prepares batch input data for TensorFlow
func (m *TensorFlowModel) prepareBatchInput(inputs []ModelInput) (interface{}, error) {
	// In real implementation, this would:
	// 1. Concatenate input data into batch tensors
	// 2. Ensure consistent shapes
	// 3. Handle padding if needed

	// Mock implementation
	batchData := make([]interface{}, len(inputs))
	for i, input := range inputs {
		batchData[i] = input.Data
	}

	return &tensorFlowInput{
		data:      batchData,
		features:  nil,
		metadata:  map[string]interface{}{"batch_size": len(inputs)},
		requestID: "batch",
	}, nil
}

// runInference runs TensorFlow inference
func (m *TensorFlowModel) runInference(ctx context.Context, input interface{}) (interface{}, error) {
	// In real implementation, this would:
	// 1. Run TensorFlow session
	// 2. Feed input tensors
	// 3. Execute computation graph
	// 4. Extract output tensors

	// Mock implementation with timeout
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond): // Simulate inference time
		// Continue
	}

	// Mock output
	return &tensorFlowOutput{
		predictions:   []float64{0.8, 0.2},
		probabilities: []float64{0.8, 0.2},
		logits:        []float64{1.4, -1.4},
		metadata:      map[string]interface{}{"inference_time": 10},
	}, nil
}

// convertOutput converts TensorFlow output to ModelOutput
func (m *TensorFlowModel) convertOutput(tfOutput interface{}, requestID string) (ModelOutput, error) {
	output, ok := tfOutput.(*tensorFlowOutput)
	if !ok {
		return ModelOutput{}, fmt.Errorf("invalid TensorFlow output type")
	}

	// Convert predictions
	predictions := make([]Prediction, len(output.predictions))
	for i, pred := range output.predictions {
		predictions[i] = Prediction{
			Label:       fmt.Sprintf("class_%d", i),
			Value:       pred,
			Confidence:  pred,
			Probability: pred,
			Metadata:    map[string]interface{}{"index": i},
		}
	}

	// Calculate overall confidence
	confidence := 0.0
	if len(output.predictions) > 0 {
		confidence = output.predictions[0] // Highest prediction as confidence
	}

	return ModelOutput{
		Predictions:   predictions,
		Probabilities: output.probabilities,
		Confidence:    confidence,
		Metadata:      output.metadata,
		RequestID:     requestID,
	}, nil
}

// convertBatchOutput converts TensorFlow batch output to ModelOutput slice
func (m *TensorFlowModel) convertBatchOutput(tfOutput interface{}, inputs []ModelInput) ([]ModelOutput, error) {
	// In real implementation, this would split batch output into individual outputs
	// For now, simulate by calling convertOutput for each input

	outputs := make([]ModelOutput, len(inputs))
	for i, input := range inputs {
		output, err := m.convertOutput(tfOutput, input.RequestID)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}

	return outputs, nil
}

// Mock TensorFlow types for demonstration
type tensorFlowSession struct {
	modelPath  string
	batchSize  int
	useGPU     bool
	numThreads int
}

type tensorFlowInput struct {
	data      interface{}
	features  map[string]interface{}
	metadata  map[string]interface{}
	requestID string
}

type tensorFlowOutput struct {
	predictions   []float64
	probabilities []float64
	logits        []float64
	metadata      map[string]interface{}
}
