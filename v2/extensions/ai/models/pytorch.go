package models

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/v2/internal/logger"
)

// PyTorchAdapter implements ModelAdapter for PyTorch models
type PyTorchAdapter struct {
	logger logger.Logger
}

// NewPyTorchAdapter creates a new PyTorch adapter
func NewPyTorchAdapter(logger logger.Logger) ModelAdapter {
	return &PyTorchAdapter{
		logger: logger,
	}
}

// Name returns the adapter name
func (a *PyTorchAdapter) Name() string {
	return "pytorch"
}

// Framework returns the ML framework
func (a *PyTorchAdapter) Framework() MLFramework {
	return MLFrameworkPyTorch
}

// SupportsModel checks if this adapter supports the given model configuration
func (a *PyTorchAdapter) SupportsModel(config ModelConfig) bool {
	if config.Framework != MLFrameworkPyTorch {
		return false
	}

	// Check if model path exists and has correct extension
	if config.ModelPath == "" {
		return false
	}

	// PyTorch models can be in various formats
	validExtensions := []string{".pt", ".pth", ".pkl", ".zip"}
	for _, ext := range validExtensions {
		if strings.HasSuffix(config.ModelPath, ext) {
			return true
		}
	}

	// Check for TorchScript models
	if strings.Contains(config.ModelPath, "torchscript") {
		return true
	}

	return false
}

// CreateModel creates a new PyTorch model
func (a *PyTorchAdapter) CreateModel(config ModelConfig) (Model, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, err
	}

	model := &PyTorchModel{
		BaseModel: NewBaseModel(config, a.logger),
		adapter:   a,
	}

	// Set PyTorch-specific input/output schemas
	model.SetInputSchema(a.getDefaultInputSchema(config))
	model.SetOutputSchema(a.getDefaultOutputSchema(config))

	// Set PyTorch-specific metadata
	metadata := map[string]interface{}{
		"framework": "pytorch",
		"version":   "1.x",
		"format":    a.detectModelFormat(config.ModelPath),
		"device":    a.getDeviceType(config.GPU),
	}
	model.SetMetadata(metadata)

	return model, nil
}

// ValidateConfig validates the model configuration
func (a *PyTorchAdapter) ValidateConfig(config ModelConfig) error {
	if config.Framework != MLFrameworkPyTorch {
		return fmt.Errorf("invalid framework: expected %s, got %s", MLFrameworkPyTorch, config.Framework)
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

// detectModelFormat detects the PyTorch model format
func (a *PyTorchAdapter) detectModelFormat(modelPath string) string {
	ext := filepath.Ext(modelPath)
	switch ext {
	case ".pt":
		return "pytorch_state_dict"
	case ".pth":
		return "pytorch_model"
	case ".pkl":
		return "pickle"
	case ".zip":
		return "torchscript_zip"
	default:
		if strings.Contains(modelPath, "torchscript") {
			return "torchscript"
		}
		return "unknown"
	}
}

// getDeviceType returns the device type based on GPU availability
func (a *PyTorchAdapter) getDeviceType(useGPU bool) string {
	if useGPU {
		return "cuda"
	}
	return "cpu"
}

// getDefaultInputSchema returns default input schema for PyTorch models
func (a *PyTorchAdapter) getDefaultInputSchema(config ModelConfig) InputSchema {
	return InputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"tensor": map[string]interface{}{
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
			"dtype": map[string]interface{}{
				"type": "string",
				"enum": []string{"float32", "float64", "int32", "int64"},
			},
		},
		Required: []string{"tensor"},
		Examples: []interface{}{
			map[string]interface{}{
				"tensor": []float64{1.0, 2.0, 3.0, 4.0},
				"shape":  []int{1, 4},
				"dtype":  "float32",
			},
		},
		Constraints: map[string]interface{}{
			"max_batch_size": config.BatchSize,
			"device":         a.getDeviceType(config.GPU),
		},
	}
}

// getDefaultOutputSchema returns default output schema for PyTorch models
func (a *PyTorchAdapter) getDefaultOutputSchema(config ModelConfig) OutputSchema {
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
			"tensor": map[string]interface{}{
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
		Examples: []interface{}{
			map[string]interface{}{
				"predictions": []map[string]interface{}{
					{
						"label":       "class_0",
						"value":       0.85,
						"confidence":  0.92,
						"probability": 0.85,
					},
				},
				"tensor": []float64{0.85, 0.15},
				"shape":  []int{1, 2},
			},
		},
		Description: "PyTorch model predictions with tensor outputs",
	}
}

// PyTorchModel represents a PyTorch model
type PyTorchModel struct {
	*BaseModel
	adapter    *PyTorchAdapter
	model      interface{} // PyTorch model (would be actual torch.Module in real implementation)
	device     string      // Device type (cpu/cuda)
	scriptMode bool        // Whether model is in TorchScript mode

	// Model-specific configuration
	dtype      string // Data type (float32, float64, etc.)
	numThreads int    // Number of threads for CPU inference
}

// Load loads the PyTorch model
func (m *PyTorchModel) Load(ctx context.Context) error {
	// Call base load first
	if err := m.BaseModel.Load(ctx); err != nil {
		return err
	}

	config := m.GetConfig()

	// Set device configuration
	m.device = m.adapter.getDeviceType(config.GPU)
	m.dtype = "float32" // Default dtype
	m.numThreads = 1    // Default to single thread

	// Detect if model is TorchScript
	format := m.adapter.detectModelFormat(config.ModelPath)
	m.scriptMode = strings.Contains(format, "torchscript")

	// In a real implementation, this would:
	// 1. Load the PyTorch model using torch.load() or torch.jit.load()
	// 2. Move model to appropriate device (CPU/GPU)
	// 3. Set model to evaluation mode
	// 4. Compile model for optimization if needed

	// Simulated loading
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(300 * time.Millisecond): // Simulate loading time
		// Continue
	}

	// Mock model creation
	m.model = &pyTorchModel{
		modelPath:  config.ModelPath,
		device:     m.device,
		batchSize:  config.BatchSize,
		scriptMode: m.scriptMode,
		dtype:      m.dtype,
	}

	if m.adapter.logger != nil {
		m.adapter.logger.Info("PyTorch model loaded successfully",
			logger.String("model_id", config.ID),
			logger.String("model_path", config.ModelPath),
			logger.String("device", m.device),
			logger.Bool("script_mode", m.scriptMode),
			logger.String("dtype", m.dtype),
		)
	}

	return nil
}

// Unload unloads the PyTorch model
func (m *PyTorchModel) Unload(ctx context.Context) error {
	// Clean up PyTorch resources
	if m.model != nil {
		// In real implementation, free GPU memory and model resources
		m.model = nil
	}

	// Call base unload
	return m.BaseModel.Unload(ctx)
}

// Predict performs prediction using the PyTorch model
func (m *PyTorchModel) Predict(ctx context.Context, input ModelInput) (ModelOutput, error) {
	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.model == nil {
		return ModelOutput{}, fmt.Errorf("PyTorch model %s is not loaded", m.ID())
	}

	// Validate input
	if err := m.ValidateInput(input); err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("invalid input: %w", err)
	}

	// Prepare input tensor for PyTorch
	torchInput, err := m.prepareInput(input)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("failed to prepare input: %w", err)
	}

	// Run inference
	torchOutput, err := m.runInference(ctx, torchInput)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("inference failed: %w", err)
	}

	// Convert PyTorch output to ModelOutput
	output, err := m.convertOutput(torchOutput, input.RequestID)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("failed to convert output: %w", err)
	}

	output.Latency = time.Since(startTime)
	output.Timestamp = time.Now()

	// Update metrics
	m.updateMetrics(time.Since(startTime), nil)

	if m.adapter.logger != nil {
		m.adapter.logger.Debug("PyTorch prediction completed",
			logger.String("model_id", m.ID()),
			logger.String("request_id", input.RequestID),
			logger.Duration("latency", output.Latency),
			logger.Float64("confidence", output.Confidence),
			logger.String("device", m.device),
		)
	}

	return output, nil
}

// BatchPredict performs batch prediction using the PyTorch model
func (m *PyTorchModel) BatchPredict(ctx context.Context, inputs []ModelInput) ([]ModelOutput, error) {
	if len(inputs) == 0 {
		return []ModelOutput{}, nil
	}

	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.model == nil {
		return nil, fmt.Errorf("PyTorch model %s is not loaded", m.ID())
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
		m.adapter.logger.Debug("PyTorch batch prediction completed",
			logger.String("model_id", m.ID()),
			logger.Int("batch_size", len(inputs)),
			logger.Duration("latency", latency),
			logger.String("device", m.device),
		)
	}

	return outputs, nil
}

// ValidateInput validates PyTorch-specific input
func (m *PyTorchModel) ValidateInput(input ModelInput) error {
	// Call base validation
	if err := m.BaseModel.ValidateInput(input); err != nil {
		return err
	}

	// PyTorch-specific validation
	if input.Data == nil {
		return fmt.Errorf("input data is required")
	}

	// Validate input format based on model type
	switch data := input.Data.(type) {
	case []float64:
		if len(data) == 0 {
			return fmt.Errorf("input tensor cannot be empty")
		}
	case [][]float64:
		if len(data) == 0 {
			return fmt.Errorf("input tensor cannot be empty")
		}
	case map[string]interface{}:
		if _, ok := data["tensor"]; !ok {
			return fmt.Errorf("input object must contain 'tensor' field")
		}
	default:
		return fmt.Errorf("unsupported input type: %T", data)
	}

	return nil
}

// prepareInput prepares input data for PyTorch
func (m *PyTorchModel) prepareInput(input ModelInput) (interface{}, error) {
	// In real implementation, this would:
	// 1. Convert input data to PyTorch tensors
	// 2. Move tensors to appropriate device (CPU/GPU)
	// 3. Set correct dtype and shape
	// 4. Handle different input formats

	// Mock implementation
	return &pyTorchInput{
		tensor:    input.Data,
		device:    m.device,
		dtype:     m.dtype,
		features:  input.Features,
		metadata:  input.Metadata,
		requestID: input.RequestID,
	}, nil
}

// prepareBatchInput prepares batch input data for PyTorch
func (m *PyTorchModel) prepareBatchInput(inputs []ModelInput) (interface{}, error) {
	// In real implementation, this would:
	// 1. Stack input tensors into batch tensor
	// 2. Ensure consistent shapes and dtypes
	// 3. Move to appropriate device

	// Mock implementation
	batchData := make([]interface{}, len(inputs))
	for i, input := range inputs {
		batchData[i] = input.Data
	}

	return &pyTorchInput{
		tensor:    batchData,
		device:    m.device,
		dtype:     m.dtype,
		features:  nil,
		metadata:  map[string]interface{}{"batch_size": len(inputs)},
		requestID: "batch",
	}, nil
}

// runInference runs PyTorch inference
func (m *PyTorchModel) runInference(ctx context.Context, input interface{}) (interface{}, error) {
	// In real implementation, this would:
	// 1. Run forward pass through PyTorch model
	// 2. Handle both regular and TorchScript models
	// 3. Apply post-processing if needed
	// 4. Move output tensors to CPU if needed

	// Mock implementation with timeout
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(15 * time.Millisecond): // Simulate inference time
		// Continue
	}

	// Mock output
	return &pyTorchOutput{
		tensor:   []float64{0.85, 0.15},
		shape:    []int{1, 2},
		dtype:    m.dtype,
		device:   m.device,
		logits:   []float64{1.7, -1.7},
		metadata: map[string]interface{}{"inference_time": 15, "device": m.device},
	}, nil
}

// convertOutput converts PyTorch output to ModelOutput
func (m *PyTorchModel) convertOutput(torchOutput interface{}, requestID string) (ModelOutput, error) {
	output, ok := torchOutput.(*pyTorchOutput)
	if !ok {
		return ModelOutput{}, fmt.Errorf("invalid PyTorch output type")
	}

	// Convert tensor to predictions
	predictions := make([]Prediction, len(output.tensor))
	for i, val := range output.tensor {
		predictions[i] = Prediction{
			Label:       fmt.Sprintf("class_%d", i),
			Value:       val,
			Confidence:  val,
			Probability: val,
			Metadata: map[string]interface{}{
				"index": i,
				"logit": output.logits[i],
			},
		}
	}

	// Calculate overall confidence
	confidence := 0.0
	if len(output.tensor) > 0 {
		confidence = output.tensor[0] // Highest prediction as confidence
	}

	return ModelOutput{
		Predictions:   predictions,
		Probabilities: output.tensor,
		Confidence:    confidence,
		Metadata: map[string]interface{}{
			"tensor_shape": output.shape,
			"dtype":        output.dtype,
			"device":       output.device,
			"logits":       output.logits,
		},
		RequestID: requestID,
	}, nil
}

// convertBatchOutput converts PyTorch batch output to ModelOutput slice
func (m *PyTorchModel) convertBatchOutput(torchOutput interface{}, inputs []ModelInput) ([]ModelOutput, error) {
	// In real implementation, this would split batch tensor into individual outputs
	// For now, simulate by calling convertOutput for each input

	outputs := make([]ModelOutput, len(inputs))
	for i, input := range inputs {
		output, err := m.convertOutput(torchOutput, input.RequestID)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}

	return outputs, nil
}

// GetDeviceInfo returns information about the device used by the model
func (m *PyTorchModel) GetDeviceInfo() map[string]interface{} {
	return map[string]interface{}{
		"device":      m.device,
		"dtype":       m.dtype,
		"script_mode": m.scriptMode,
		"num_threads": m.numThreads,
	}
}

// SetDevice changes the device used by the model
func (m *PyTorchModel) SetDevice(device string) error {
	if !m.IsLoaded() {
		return fmt.Errorf("model must be loaded before changing device")
	}

	// In real implementation, this would move the model to the new device
	m.device = device

	if m.adapter.logger != nil {
		m.adapter.logger.Info("PyTorch model device changed",
			logger.String("model_id", m.ID()),
			logger.String("new_device", device),
		)
	}

	return nil
}

// Mock PyTorch types for demonstration
type pyTorchModel struct {
	modelPath  string
	device     string
	batchSize  int
	scriptMode bool
	dtype      string
}

type pyTorchInput struct {
	tensor    interface{}
	device    string
	dtype     string
	features  map[string]interface{}
	metadata  map[string]interface{}
	requestID string
}

type pyTorchOutput struct {
	tensor   []float64
	shape    []int
	dtype    string
	device   string
	logits   []float64
	metadata map[string]interface{}
}
