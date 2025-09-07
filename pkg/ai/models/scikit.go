package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ScikitAdapter implements ModelAdapter for Scikit-learn models
type ScikitAdapter struct {
	logger common.Logger
}

// NewScikitAdapter creates a new Scikit-learn adapter
func NewScikitAdapter(logger common.Logger) ModelAdapter {
	return &ScikitAdapter{
		logger: logger,
	}
}

// Name returns the adapter name
func (a *ScikitAdapter) Name() string {
	return "scikit"
}

// Framework returns the ML framework
func (a *ScikitAdapter) Framework() MLFramework {
	return MLFrameworkSciKit
}

// SupportsModel checks if this adapter supports the given model configuration
func (a *ScikitAdapter) SupportsModel(config ModelConfig) bool {
	if config.Framework != MLFrameworkSciKit {
		return false
	}

	// Check if model path exists and has correct extension
	if config.ModelPath == "" {
		return false
	}

	// Scikit-learn models are typically saved as pickle files
	validExtensions := []string{".pkl", ".pickle", ".joblib"}
	for _, ext := range validExtensions {
		if strings.HasSuffix(config.ModelPath, ext) {
			return true
		}
	}

	return false
}

// CreateModel creates a new Scikit-learn model
func (a *ScikitAdapter) CreateModel(config ModelConfig) (Model, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, err
	}

	model := &ScikitModel{
		BaseModel: NewBaseModel(config, a.logger),
		adapter:   a,
	}

	// Set Scikit-learn-specific input/output schemas
	model.SetInputSchema(a.getDefaultInputSchema(config))
	model.SetOutputSchema(a.getDefaultOutputSchema(config))

	// Set Scikit-learn-specific metadata
	metadata := map[string]interface{}{
		"framework":  "scikit-learn",
		"version":    "1.x",
		"format":     a.detectModelFormat(config.ModelPath),
		"model_type": config.Type,
		"features":   nil, // Will be set after loading
		"classes":    nil, // Will be set after loading
	}
	model.SetMetadata(metadata)

	return model, nil
}

// ValidateConfig validates the model configuration
func (a *ScikitAdapter) ValidateConfig(config ModelConfig) error {
	if config.Framework != MLFrameworkSciKit {
		return fmt.Errorf("invalid framework: expected %s, got %s", MLFrameworkSciKit, config.Framework)
	}

	if config.ModelPath == "" {
		return fmt.Errorf("model path is required")
	}

	// Check for valid extensions
	validExtensions := []string{".pkl", ".pickle", ".joblib"}
	hasValidExtension := false
	for _, ext := range validExtensions {
		if strings.HasSuffix(config.ModelPath, ext) {
			hasValidExtension = true
			break
		}
	}

	if !hasValidExtension {
		return fmt.Errorf("model path must have one of the following extensions: %s", strings.Join(validExtensions, ", "))
	}

	if config.ID == "" {
		return fmt.Errorf("model ID is required")
	}

	if config.Name == "" {
		return fmt.Errorf("model name is required")
	}

	// Validate model type for Scikit-learn
	validTypes := []ModelType{
		ModelTypeClassification,
		ModelTypeRegression,
		ModelTypeClustering,
		ModelTypeAnomalyDetect,
	}

	isValidType := false
	for _, validType := range validTypes {
		if config.Type == validType {
			isValidType = true
			break
		}
	}

	if !isValidType {
		return fmt.Errorf("invalid model type for Scikit-learn: %s", config.Type)
	}

	return nil
}

// detectModelFormat detects the Scikit-learn model format
func (a *ScikitAdapter) detectModelFormat(modelPath string) string {
	if strings.HasSuffix(modelPath, ".joblib") {
		return "joblib"
	} else if strings.HasSuffix(modelPath, ".pkl") || strings.HasSuffix(modelPath, ".pickle") {
		return "pickle"
	}
	return "unknown"
}

// getDefaultInputSchema returns default input schema for Scikit-learn models
func (a *ScikitAdapter) getDefaultInputSchema(config ModelConfig) InputSchema {
	return InputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"features": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "number",
				},
			},
			"feature_names": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		Required: []string{"features"},
		Examples: []interface{}{
			map[string]interface{}{
				"features":      []float64{1.0, 2.0, 3.0, 4.0},
				"feature_names": []string{"feature1", "feature2", "feature3", "feature4"},
			},
		},
		Constraints: map[string]interface{}{
			"max_features": 1000,
			"model_type":   config.Type,
		},
	}
}

// getDefaultOutputSchema returns default output schema for Scikit-learn models
func (a *ScikitAdapter) getDefaultOutputSchema(config ModelConfig) OutputSchema {
	schema := OutputSchema{
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
		},
		Description: "Scikit-learn model predictions",
	}

	// Add type-specific properties
	switch config.Type {
	case ModelTypeClassification:
		schema.Properties["probabilities"] = map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "number",
			},
		}
		schema.Properties["classes"] = map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "string",
			},
		}
	case ModelTypeRegression:
		schema.Properties["prediction"] = map[string]interface{}{
			"type": "number",
		}
	case ModelTypeClustering:
		schema.Properties["cluster"] = map[string]interface{}{
			"type": "integer",
		}
		schema.Properties["distance"] = map[string]interface{}{
			"type": "number",
		}
	case ModelTypeAnomalyDetect:
		schema.Properties["is_anomaly"] = map[string]interface{}{
			"type": "boolean",
		}
		schema.Properties["anomaly_score"] = map[string]interface{}{
			"type": "number",
		}
	}

	return schema
}

// ScikitModel represents a Scikit-learn model
type ScikitModel struct {
	*BaseModel
	adapter *ScikitAdapter
	model   interface{} // Scikit-learn model object

	// Model information
	featureNames []string
	classes      []string
	numFeatures  int

	// Model-specific properties
	modelType  ModelType
	algorithm  string
	parameters map[string]interface{}
}

// Load loads the Scikit-learn model
func (m *ScikitModel) Load(ctx context.Context) error {
	// Call base load first
	if err := m.BaseModel.Load(ctx); err != nil {
		return err
	}

	config := m.GetConfig()
	m.modelType = config.Type

	// In a real implementation, this would:
	// 1. Load the model using pickle or joblib
	// 2. Extract model metadata (feature names, classes, etc.)
	// 3. Validate model compatibility
	// 4. Set up preprocessing pipelines if needed

	// Simulated loading
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond): // Simulate loading time
		// Continue
	}

	// Mock model creation
	m.model = &scikitModel{
		modelPath:  config.ModelPath,
		modelType:  m.modelType,
		algorithm:  m.inferAlgorithm(config.Name),
		parameters: m.getDefaultParameters(),
	}

	// Mock model metadata
	m.featureNames = []string{"feature1", "feature2", "feature3", "feature4"}
	m.numFeatures = len(m.featureNames)

	// Set classes for classification models
	if m.modelType == ModelTypeClassification {
		m.classes = []string{"class_0", "class_1"}
	}

	// Update metadata
	metadata := m.GetMetadata()
	metadata["features"] = m.featureNames
	metadata["classes"] = m.classes
	metadata["algorithm"] = m.algorithm
	metadata["num_features"] = m.numFeatures
	m.SetMetadata(metadata)

	if m.adapter.logger != nil {
		m.adapter.logger.Info("Scikit-learn model loaded successfully",
			logger.String("model_id", config.ID),
			logger.String("model_path", config.ModelPath),
			logger.String("model_type", string(m.modelType)),
			logger.String("algorithm", m.algorithm),
			logger.Int("num_features", m.numFeatures),
		)
	}

	return nil
}

// Unload unloads the Scikit-learn model
func (m *ScikitModel) Unload(ctx context.Context) error {
	// Clean up model resources
	if m.model != nil {
		m.model = nil
	}

	m.featureNames = nil
	m.classes = nil
	m.numFeatures = 0
	m.algorithm = ""
	m.parameters = nil

	// Call base unload
	return m.BaseModel.Unload(ctx)
}

// Predict performs prediction using the Scikit-learn model
func (m *ScikitModel) Predict(ctx context.Context, input ModelInput) (ModelOutput, error) {
	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.model == nil {
		return ModelOutput{}, fmt.Errorf("Scikit-learn model %s is not loaded", m.ID())
	}

	// Validate input
	if err := m.ValidateInput(input); err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("invalid input: %w", err)
	}

	// Prepare input features
	features, err := m.prepareFeatures(input)
	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("failed to prepare features: %w", err)
	}

	// Run prediction based on model type
	var output ModelOutput
	switch m.modelType {
	case ModelTypeClassification:
		output, err = m.predictClassification(features, input.RequestID)
	case ModelTypeRegression:
		output, err = m.predictRegression(features, input.RequestID)
	case ModelTypeClustering:
		output, err = m.predictClustering(features, input.RequestID)
	case ModelTypeAnomalyDetect:
		output, err = m.predictAnomaly(features, input.RequestID)
	default:
		err = fmt.Errorf("unsupported model type: %s", m.modelType)
	}

	if err != nil {
		m.updateErrorCount()
		return ModelOutput{}, fmt.Errorf("prediction failed: %w", err)
	}

	output.Latency = time.Since(startTime)
	output.Timestamp = time.Now()

	// Update metrics
	m.updateMetrics(time.Since(startTime), nil)

	if m.adapter.logger != nil {
		m.adapter.logger.Debug("Scikit-learn prediction completed",
			logger.String("model_id", m.ID()),
			logger.String("request_id", input.RequestID),
			logger.String("model_type", string(m.modelType)),
			logger.Duration("latency", output.Latency),
			logger.Float64("confidence", output.Confidence),
		)
	}

	return output, nil
}

// BatchPredict performs batch prediction using the Scikit-learn model
func (m *ScikitModel) BatchPredict(ctx context.Context, inputs []ModelInput) ([]ModelOutput, error) {
	if len(inputs) == 0 {
		return []ModelOutput{}, nil
	}

	startTime := time.Now()

	// Check if model is loaded
	if !m.IsLoaded() || m.model == nil {
		return nil, fmt.Errorf("Scikit-learn model %s is not loaded", m.ID())
	}

	// Prepare batch features
	batchFeatures, err := m.prepareBatchFeatures(inputs)
	if err != nil {
		m.updateErrorCount()
		return nil, fmt.Errorf("failed to prepare batch features: %w", err)
	}

	// Run batch prediction
	outputs, err := m.predictBatch(batchFeatures, inputs)
	if err != nil {
		m.updateErrorCount()
		return nil, fmt.Errorf("batch prediction failed: %w", err)
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
		m.adapter.logger.Debug("Scikit-learn batch prediction completed",
			logger.String("model_id", m.ID()),
			logger.String("model_type", string(m.modelType)),
			logger.Int("batch_size", len(inputs)),
			logger.Duration("latency", latency),
		)
	}

	return outputs, nil
}

// ValidateInput validates Scikit-learn-specific input
func (m *ScikitModel) ValidateInput(input ModelInput) error {
	// Call base validation
	if err := m.BaseModel.ValidateInput(input); err != nil {
		return err
	}

	// Scikit-learn-specific validation
	if input.Data == nil {
		return fmt.Errorf("input data is required")
	}

	// Validate input format
	switch data := input.Data.(type) {
	case []float64:
		if len(data) == 0 {
			return fmt.Errorf("feature array cannot be empty")
		}
		if m.numFeatures > 0 && len(data) != m.numFeatures {
			return fmt.Errorf("expected %d features, got %d", m.numFeatures, len(data))
		}
	case map[string]interface{}:
		if features, ok := data["features"]; ok {
			if featureSlice, ok := features.([]interface{}); ok {
				if len(featureSlice) == 0 {
					return fmt.Errorf("feature array cannot be empty")
				}
				if m.numFeatures > 0 && len(featureSlice) != m.numFeatures {
					return fmt.Errorf("expected %d features, got %d", m.numFeatures, len(featureSlice))
				}
			} else {
				return fmt.Errorf("features must be an array")
			}
		} else {
			return fmt.Errorf("input object must contain 'features' field")
		}
	default:
		return fmt.Errorf("unsupported input type: %T", data)
	}

	return nil
}

// prepareFeatures prepares feature data for Scikit-learn
func (m *ScikitModel) prepareFeatures(input ModelInput) ([]float64, error) {
	switch data := input.Data.(type) {
	case []float64:
		return data, nil
	case []interface{}:
		features := make([]float64, len(data))
		for i, val := range data {
			if f, ok := val.(float64); ok {
				features[i] = f
			} else if f, ok := val.(int); ok {
				features[i] = float64(f)
			} else {
				return nil, fmt.Errorf("feature at index %d is not a number", i)
			}
		}
		return features, nil
	case map[string]interface{}:
		if featureData, ok := data["features"]; ok {
			if featureSlice, ok := featureData.([]interface{}); ok {
				features := make([]float64, len(featureSlice))
				for i, val := range featureSlice {
					if f, ok := val.(float64); ok {
						features[i] = f
					} else if f, ok := val.(int); ok {
						features[i] = float64(f)
					} else {
						return nil, fmt.Errorf("feature at index %d is not a number", i)
					}
				}
				return features, nil
			}
		}
		return nil, fmt.Errorf("invalid feature format in input object")
	default:
		return nil, fmt.Errorf("unsupported input type: %T", data)
	}
}

// prepareBatchFeatures prepares batch feature data
func (m *ScikitModel) prepareBatchFeatures(inputs []ModelInput) ([][]float64, error) {
	batchFeatures := make([][]float64, len(inputs))
	for i, input := range inputs {
		features, err := m.prepareFeatures(input)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare features for input %d: %w", i, err)
		}
		batchFeatures[i] = features
	}
	return batchFeatures, nil
}

// predictClassification performs classification prediction
func (m *ScikitModel) predictClassification(features []float64, requestID string) (ModelOutput, error) {
	// Mock classification prediction
	probabilities := []float64{0.8, 0.2} // Mock probabilities
	predictedClass := 0                  // Mock predicted class

	predictions := make([]Prediction, len(m.classes))
	for i, class := range m.classes {
		predictions[i] = Prediction{
			Label:       class,
			Value:       probabilities[i],
			Confidence:  probabilities[i],
			Probability: probabilities[i],
			Metadata: map[string]interface{}{
				"class_index": i,
			},
		}
	}

	return ModelOutput{
		Predictions:   predictions,
		Probabilities: probabilities,
		Confidence:    probabilities[predictedClass],
		Metadata: map[string]interface{}{
			"predicted_class": m.classes[predictedClass],
			"algorithm":       m.algorithm,
		},
		RequestID: requestID,
	}, nil
}

// predictRegression performs regression prediction
func (m *ScikitModel) predictRegression(features []float64, requestID string) (ModelOutput, error) {
	// Mock regression prediction
	prediction := 42.5 // Mock prediction value
	confidence := 0.9  // Mock confidence

	return ModelOutput{
		Predictions: []Prediction{
			{
				Label:      "regression_value",
				Value:      prediction,
				Confidence: confidence,
				Metadata: map[string]interface{}{
					"prediction_type": "regression",
				},
			},
		},
		Confidence: confidence,
		Metadata: map[string]interface{}{
			"prediction": prediction,
			"algorithm":  m.algorithm,
		},
		RequestID: requestID,
	}, nil
}

// predictClustering performs clustering prediction
func (m *ScikitModel) predictClustering(features []float64, requestID string) (ModelOutput, error) {
	// Mock clustering prediction
	cluster := 1     // Mock cluster assignment
	distance := 0.15 // Mock distance to cluster center

	return ModelOutput{
		Predictions: []Prediction{
			{
				Label:      fmt.Sprintf("cluster_%d", cluster),
				Value:      float64(cluster),
				Confidence: 1.0 - distance, // Closer to center = higher confidence
				Metadata: map[string]interface{}{
					"cluster":  cluster,
					"distance": distance,
				},
			},
		},
		Confidence: 1.0 - distance,
		Metadata: map[string]interface{}{
			"cluster":   cluster,
			"distance":  distance,
			"algorithm": m.algorithm,
		},
		RequestID: requestID,
	}, nil
}

// predictAnomaly performs anomaly detection prediction
func (m *ScikitModel) predictAnomaly(features []float64, requestID string) (ModelOutput, error) {
	// Mock anomaly detection prediction
	isAnomaly := false  // Mock anomaly detection
	anomalyScore := 0.3 // Mock anomaly score

	return ModelOutput{
		Predictions: []Prediction{
			{
				Label:      fmt.Sprintf("is_anomaly_%t", isAnomaly),
				Value:      anomalyScore,
				Confidence: 1.0 - anomalyScore,
				Metadata: map[string]interface{}{
					"is_anomaly":    isAnomaly,
					"anomaly_score": anomalyScore,
					"threshold":     0.5,
				},
			},
		},
		Confidence: 1.0 - anomalyScore,
		Metadata: map[string]interface{}{
			"is_anomaly":    isAnomaly,
			"anomaly_score": anomalyScore,
			"algorithm":     m.algorithm,
		},
		RequestID: requestID,
	}, nil
}

// predictBatch performs batch prediction
func (m *ScikitModel) predictBatch(batchFeatures [][]float64, inputs []ModelInput) ([]ModelOutput, error) {
	outputs := make([]ModelOutput, len(inputs))

	for i, features := range batchFeatures {
		var output ModelOutput
		var err error

		switch m.modelType {
		case ModelTypeClassification:
			output, err = m.predictClassification(features, inputs[i].RequestID)
		case ModelTypeRegression:
			output, err = m.predictRegression(features, inputs[i].RequestID)
		case ModelTypeClustering:
			output, err = m.predictClustering(features, inputs[i].RequestID)
		case ModelTypeAnomalyDetect:
			output, err = m.predictAnomaly(features, inputs[i].RequestID)
		default:
			return nil, fmt.Errorf("unsupported model type: %s", m.modelType)
		}

		if err != nil {
			return nil, err
		}

		outputs[i] = output
	}

	return outputs, nil
}

// inferAlgorithm infers the algorithm from model name
func (m *ScikitModel) inferAlgorithm(modelName string) string {
	name := strings.ToLower(modelName)

	if strings.Contains(name, "svm") {
		return "support_vector_machine"
	} else if strings.Contains(name, "forest") {
		return "random_forest"
	} else if strings.Contains(name, "tree") {
		return "decision_tree"
	} else if strings.Contains(name, "logistic") {
		return "logistic_regression"
	} else if strings.Contains(name, "linear") {
		return "linear_regression"
	} else if strings.Contains(name, "kmeans") {
		return "k_means"
	} else if strings.Contains(name, "naive") {
		return "naive_bayes"
	} else if strings.Contains(name, "knn") {
		return "k_nearest_neighbors"
	}

	return "unknown"
}

// getDefaultParameters returns default parameters for the model
func (m *ScikitModel) getDefaultParameters() map[string]interface{} {
	return map[string]interface{}{
		"n_estimators": 100,
		"max_depth":    nil,
		"random_state": 42,
	}
}

// GetFeatureNames returns the feature names
func (m *ScikitModel) GetFeatureNames() []string {
	return m.featureNames
}

// GetClasses returns the class names for classification models
func (m *ScikitModel) GetClasses() []string {
	return m.classes
}

// GetAlgorithm returns the algorithm name
func (m *ScikitModel) GetAlgorithm() string {
	return m.algorithm
}

// GetParameters returns the model parameters
func (m *ScikitModel) GetParameters() map[string]interface{} {
	return m.parameters
}

// Mock Scikit-learn types for demonstration
type scikitModel struct {
	modelPath  string
	modelType  ModelType
	algorithm  string
	parameters map[string]interface{}
}
