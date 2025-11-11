package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
)

// ONNXAdapter implements ModelAdapter for ONNX models.
type ONNXAdapter struct {
	logger logger.Logger
}

// NewONNXAdapter creates a new ONNX adapter.
func NewONNXAdapter(logger logger.Logger) ModelAdapter {
	return &ONNXAdapter{
		logger: logger,
	}
}

// Name returns the adapter name.
func (a *ONNXAdapter) Name() string {
	return "onnx"
}

// Framework returns the ML framework.
func (a *ONNXAdapter) Framework() MLFramework {
	return MLFrameworkONNX
}

// SupportsModel checks if this adapter supports the given model configuration.
func (a *ONNXAdapter) SupportsModel(config ModelConfig) bool {
	if config.Framework != MLFrameworkONNX {
		return false
	}

	// Check if model path exists and has correct extension
	if config.ModelPath == "" {
		return false
	}

	// ONNX models have .onnx extension
	return strings.HasSuffix(config.ModelPath, ".onnx")
}

// CreateModel creates a new ONNX model.
func (a *ONNXAdapter) CreateModel(config ModelConfig) (Model, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, err
	}

	model := &ONNXModel{
		BaseModel: NewBaseModel(config, a.logger),
		adapter:   a,
	}

	// Set ONNX-specific input/output schemas
	model.SetInputSchema(a.getDefaultInputSchema(config))
	model.SetOutputSchema(a.getDefaultOutputSchema(config))

	// Set ONNX-specific metadata
	metadata := map[string]any{
		"framework": "onnx",
		"version":   "1.x",
		"format":    "onnx",
		"providers": a.getAvailableProviders(config.GPU),
	}
	model.SetMetadata(metadata)

	return model, nil
}

// ValidateConfig validates the model configuration.
func (a *ONNXAdapter) ValidateConfig(config ModelConfig) error {
	if config.Framework != MLFrameworkONNX {
		return fmt.Errorf("invalid framework: expected %s, got %s", MLFrameworkONNX, config.Framework)
	}

	if config.ModelPath == "" {
		return errors.New("model path is required")
	}

	if !strings.HasSuffix(config.ModelPath, ".onnx") {
		return errors.New("model path must have .onnx extension")
	}

	if config.ID == "" {
		return errors.New("model ID is required")
	}

	if config.Name == "" {
		return errors.New("model name is required")
	}

	// Validate batch size
	if config.BatchSize < 0 {
		return errors.New("batch size must be non-negative")
	}

	// Validate timeout
	if config.Timeout < 0 {
		return errors.New("timeout must be non-negative")
	}

	return nil
}

// getAvailableProviders returns available ONNX Runtime providers.
func (a *ONNXAdapter) getAvailableProviders(useGPU bool) []string {
	providers := []string{"CPUExecutionProvider"}

	if useGPU {
		// In real implementation, check for GPU availability
		providers = append(providers, "CUDAExecutionProvider")
	}

	return providers
}

// getDefaultInputSchema returns default input schema for ONNX models.
func (a *ONNXAdapter) getDefaultInputSchema(config ModelConfig) InputSchema {
	return InputSchema{
		Type: "object",
		Properties: map[string]any{
			"input": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "number",
				},
			},
			"shape": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "integer",
				},
			},
			"dtype": map[string]any{
				"type": "string",
				"enum": []string{"float32", "float64", "int32", "int64"},
			},
		},
		Required: []string{"input"},
		Examples: []any{
			map[string]any{
				"input": []float64{1.0, 2.0, 3.0, 4.0},
				"shape": []int{1, 4},
				"dtype": "float32",
			},
		},
		Constraints: map[string]any{
			"max_batch_size": config.BatchSize,
			"providers":      a.getAvailableProviders(config.GPU),
		},
	}
}

// getDefaultOutputSchema returns default output schema for ONNX models.
func (a *ONNXAdapter) getDefaultOutputSchema(config ModelConfig) OutputSchema {
	return OutputSchema{
		Type: "object",
		Properties: map[string]any{
			"predictions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":       map[string]any{"type": "string"},
						"value":       map[string]any{"type": "number"},
						"confidence":  map[string]any{"type": "number"},
						"probability": map[string]any{"type": "number"},
					},
				},
			},
			"output": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "number",
				},
			},
			"shape": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "integer",
				},
			},
		},
		Examples: []any{
			map[string]any{
				"predictions": []map[string]any{
					{
						"label":       "class_0",
						"value":       0.9,
						"confidence":  0.95,
						"probability": 0.9,
					},
				},
				"output": []float64{0.9, 0.1},
				"shape":  []int{1, 2},
			},
		},
		Description: "ONNX model predictions with raw outputs",
	}
}

// ONNXModel represents an ONNX model.
type ONNXModel struct {
	*BaseModel

	adapter   *ONNXAdapter
	session   any      // ONNX Runtime session
	providers []string // Execution providers

	// Model information
	inputNames   []string
	outputNames  []string
	inputShapes  map[string][]int
	outputShapes map[string][]int

	// Configuration
	optimization  ONNXOptimization
	graphOptLevel string
	parallelism   int
}

// ONNXOptimization represents ONNX optimization settings.
type ONNXOptimization struct {
	EnableMemPattern       bool
	EnableCPUMemArena      bool
	EnableProfiling        bool
	InterOpNumThreads      int
	IntraOpNumThreads      int
	ExecutionMode          string
	GraphOptimizationLevel string
}

// Load loads the ONNX model.
func (m *ONNXModel) Load(ctx context.Context) error {
	// Call base load first
	if err := m.BaseModel.Load(ctx); err != nil {
		return err
	}

	config := m.GetConfig()

	// Set providers based on GPU availability
	m.providers = m.adapter.getAvailableProviders(config.GPU)

	// Set optimization settings
	m.optimization = ONNXOptimization{
		EnableMemPattern:       true,
		EnableCPUMemArena:      true,
		EnableProfiling:        false,
		InterOpNumThreads:      1,
		IntraOpNumThreads:      1,
		ExecutionMode:          "sequential",
		GraphOptimizationLevel: "basic",
	}

	// In a real implementation, this would:
	// 1. Create ONNX Runtime session
	// 2. Set execution providers
	// 3. Apply optimization settings
	// 4. Extract model metadata (input/output names and shapes)

	// Simulated loading
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(200 * time.Millisecond): // Simulate loading time
		// Continue
	}

	// Mock session creation
	m.session = &onnxSession{
		modelPath:    config.ModelPath,
		providers:    m.providers,
		batchSize:    config.BatchSize,
		optimization: m.optimization,
	}

	// Mock model metadata
	m.inputNames = []string{"input"}
	m.outputNames = []string{"output"}
	m.inputShapes = map[string][]int{
		"input": {1, 4}, // Example shape
	}
	m.outputShapes = map[string][]int{
		"output": {1, 2}, // Example shape
	}

	if m.adapter.logger != nil {
		m.adapter.logger.Info("ONNX model loaded successfully",
			logger.String("model_id", config.ID),
			logger.String("model_path", config.ModelPath),
			logger.String("providers", strings.Join(m.providers, ", ")),
			logger.String("input_names", strings.Join(m.inputNames, ", ")),
			logger.String("output_names", strings.Join(m.outputNames, ", ")),
		)
	}

	return nil
}

// Unload unloads the ONNX model.
func (m *ONNXModel) Unload(ctx context.Context) error {
	// Clean up ONNX Runtime resources
	if m.session != nil {
		// In real implementation, release ONNX Runtime session
		m.session = nil
	}

	m.inputNames = nil
	m.outputNames = nil
	m.inputShapes = nil
	m.outputShapes = nil

	// Call base unload
	return m.BaseModel.Unload(ctx)
}

// Predict performs prediction using the ONNX model.
func (m *ONNXModel) Predict(ctx context.Context, input ModelInput) (ModelOutput, error) {
	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.session == nil {
		return ModelOutput{}, fmt.Errorf("ONNX model %s is not loaded", m.ID())
	}

	// Validate input
	if err := m.ValidateInput(input); err != nil {
		m.updateErrorCount()

		return ModelOutput{}, fmt.Errorf("invalid input: %w", err)
	}

	// Prepare input for ONNX Runtime
	onnxInput, err := m.prepareInput(input)
	if err != nil {
		m.updateErrorCount()

		return ModelOutput{}, fmt.Errorf("failed to prepare input: %w", err)
	}

	// Run inference
	onnxOutput, err := m.runInference(ctx, onnxInput)
	if err != nil {
		m.updateErrorCount()

		return ModelOutput{}, fmt.Errorf("inference failed: %w", err)
	}

	// Convert ONNX output to ModelOutput
	output, err := m.convertOutput(onnxOutput, input.RequestID)
	if err != nil {
		m.updateErrorCount()

		return ModelOutput{}, fmt.Errorf("failed to convert output: %w", err)
	}

	output.Latency = time.Since(startTime)
	output.Timestamp = time.Now()

	// Update metrics
	m.updateMetrics(time.Since(startTime), nil)

	if m.adapter.logger != nil {
		m.adapter.logger.Debug("ONNX prediction completed",
			logger.String("model_id", m.ID()),
			logger.String("request_id", input.RequestID),
			logger.Duration("latency", output.Latency),
			logger.Float64("confidence", output.Confidence),
			logger.String("providers", strings.Join(m.providers, ", ")),
		)
	}

	return output, nil
}

// BatchPredict performs batch prediction using the ONNX model.
func (m *ONNXModel) BatchPredict(ctx context.Context, inputs []ModelInput) ([]ModelOutput, error) {
	if len(inputs) == 0 {
		return []ModelOutput{}, nil
	}

	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.session == nil {
		return nil, fmt.Errorf("ONNX model %s is not loaded", m.ID())
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
		m.adapter.logger.Debug("ONNX batch prediction completed",
			logger.String("model_id", m.ID()),
			logger.Int("batch_size", len(inputs)),
			logger.Duration("latency", latency),
			logger.String("providers", strings.Join(m.providers, ", ")),
		)
	}

	return outputs, nil
}

// ValidateInput validates ONNX-specific input.
func (m *ONNXModel) ValidateInput(input ModelInput) error {
	// Call base validation
	if err := m.BaseModel.ValidateInput(input); err != nil {
		return err
	}

	// ONNX-specific validation
	if input.Data == nil {
		return errors.New("input data is required")
	}

	// Validate input format
	switch data := input.Data.(type) {
	case []float64:
		if len(data) == 0 {
			return errors.New("input array cannot be empty")
		}
	case [][]float64:
		if len(data) == 0 {
			return errors.New("input matrix cannot be empty")
		}
	case map[string]any:
		if _, ok := data["input"]; !ok {
			return errors.New("input object must contain 'input' field")
		}
	default:
		return fmt.Errorf("unsupported input type: %T", data)
	}

	return nil
}

// prepareInput prepares input data for ONNX Runtime.
func (m *ONNXModel) prepareInput(input ModelInput) (any, error) {
	// In real implementation, this would:
	// 1. Convert input data to ONNX value format
	// 2. Validate input shapes match model expectations
	// 3. Handle different data types
	// 4. Create named input tensors

	// Mock implementation
	return &onnxInput{
		name:      m.inputNames[0],
		data:      input.Data,
		shape:     m.inputShapes[m.inputNames[0]],
		dtype:     "float32",
		features:  input.Features,
		metadata:  input.Metadata,
		requestID: input.RequestID,
	}, nil
}

// prepareBatchInput prepares batch input data for ONNX Runtime.
func (m *ONNXModel) prepareBatchInput(inputs []ModelInput) (any, error) {
	// In real implementation, this would:
	// 1. Concatenate inputs into batch tensors
	// 2. Update batch dimension in shape
	// 3. Handle dynamic batch sizes

	// Mock implementation
	batchData := make([]any, len(inputs))
	for i, input := range inputs {
		batchData[i] = input.Data
	}

	// Update shape for batch
	batchShape := make([]int, len(m.inputShapes[m.inputNames[0]]))
	copy(batchShape, m.inputShapes[m.inputNames[0]])
	batchShape[0] = len(inputs) // Set batch dimension

	return &onnxInput{
		name:      m.inputNames[0],
		data:      batchData,
		shape:     batchShape,
		dtype:     "float32",
		features:  nil,
		metadata:  map[string]any{"batch_size": len(inputs)},
		requestID: "batch",
	}, nil
}

// runInference runs ONNX Runtime inference.
func (m *ONNXModel) runInference(ctx context.Context, input any) (any, error) {
	// In real implementation, this would:
	// 1. Run ONNX Runtime session
	// 2. Handle input/output tensor management
	// 3. Apply post-processing if needed

	// Mock implementation with timeout
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(8 * time.Millisecond): // Simulate inference time
		// Continue
	}

	// Mock output
	return &onnxOutput{
		name:     m.outputNames[0],
		data:     []float64{0.9, 0.1},
		shape:    m.outputShapes[m.outputNames[0]],
		dtype:    "float32",
		metadata: map[string]any{"inference_time": 8, "provider": m.providers[0]},
	}, nil
}

// convertOutput converts ONNX output to ModelOutput.
func (m *ONNXModel) convertOutput(onnxOut any, requestID string) (ModelOutput, error) {
	output, ok := onnxOut.(*onnxOutput)
	if !ok {
		return ModelOutput{}, errors.New("invalid ONNX output type")
	}

	// Convert raw output to predictions
	predictions := make([]Prediction, len(output.data))
	for i, val := range output.data {
		predictions[i] = Prediction{
			Label:       fmt.Sprintf("class_%d", i),
			Value:       val,
			Confidence:  val,
			Probability: val,
			Metadata: map[string]any{
				"index":     i,
				"raw_value": val,
			},
		}
	}

	// Calculate overall confidence
	confidence := 0.0
	if len(output.data) > 0 {
		confidence = output.data[0] // Highest prediction as confidence
	}

	return ModelOutput{
		Predictions:   predictions,
		Probabilities: output.data,
		Confidence:    confidence,
		Metadata: map[string]any{
			"output_name":  output.name,
			"output_shape": output.shape,
			"dtype":        output.dtype,
			"provider":     m.providers[0],
		},
		RequestID: requestID,
	}, nil
}

// convertBatchOutput converts ONNX batch output to ModelOutput slice.
func (m *ONNXModel) convertBatchOutput(onnxOutput any, inputs []ModelInput) ([]ModelOutput, error) {
	// In real implementation, this would split batch output into individual outputs
	// For now, simulate by calling convertOutput for each input
	outputs := make([]ModelOutput, len(inputs))
	for i, input := range inputs {
		output, err := m.convertOutput(onnxOutput, input.RequestID)
		if err != nil {
			return nil, err
		}

		outputs[i] = output
	}

	return outputs, nil
}

// GetModelMetadata returns ONNX model metadata.
func (m *ONNXModel) GetModelMetadata() map[string]any {
	return map[string]any{
		"input_names":   m.inputNames,
		"output_names":  m.outputNames,
		"input_shapes":  m.inputShapes,
		"output_shapes": m.outputShapes,
		"providers":     m.providers,
		"optimization":  m.optimization,
	}
}

// SetOptimization updates ONNX optimization settings.
func (m *ONNXModel) SetOptimization(optimization ONNXOptimization) error {
	if m.IsLoaded() {
		return errors.New("cannot change optimization settings while model is loaded")
	}

	m.optimization = optimization

	return nil
}

// GetProviders returns available execution providers.
func (m *ONNXModel) GetProviders() []string {
	return m.providers
}

// SetProviders sets the execution providers.
func (m *ONNXModel) SetProviders(providers []string) error {
	if m.IsLoaded() {
		return errors.New("cannot change providers while model is loaded")
	}

	m.providers = providers

	return nil
}

// Mock ONNX types for demonstration.
type onnxSession struct {
	modelPath    string
	providers    []string
	batchSize    int
	optimization ONNXOptimization
}

type onnxInput struct {
	name      string
	data      any
	shape     []int
	dtype     string
	features  map[string]any
	metadata  map[string]any
	requestID string
}

type onnxOutput struct {
	name     string
	data     []float64
	shape    []int
	dtype    string
	metadata map[string]any
}
