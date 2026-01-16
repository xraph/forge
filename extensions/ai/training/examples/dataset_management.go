package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/training"
)

// DatasetManagementExample demonstrates comprehensive dataset operations.
// This example shows:
// - Loading data from various sources
// - Data validation and quality checks
// - Dataset transformations
// - Train/validation/test splits
// - Data statistics and profiling
// - Dataset versioning
func main() {
	fmt.Println("=== Dataset Management Example ===\n")

	ctx := context.Background()

	// Setup
	logger := forge.NewLogger(forge.LoggerConfig{Level: "info"})
	metrics := forge.NewMetrics(forge.MetricsConfig{Enabled: true})

	dataManager := training.NewDataManager(logger, metrics)
	fmt.Println("✓ Data manager created\n")

	// Example 1: Loading and Validating Data
	fmt.Println("--- Example 1: Data Loading and Validation ---")
	loadAndValidateExample(ctx, dataManager)

	// Example 2: Data Transformations
	fmt.Println("\n--- Example 2: Data Transformations ---")
	transformationsExample(ctx, dataManager)

	// Example 3: Dataset Splitting Strategies
	fmt.Println("\n--- Example 3: Dataset Splitting ---")
	splittingExample(ctx, dataManager)

	// Example 4: Data Quality Analysis
	fmt.Println("\n--- Example 4: Data Quality Analysis ---")
	qualityAnalysisExample(ctx, dataManager)

	// Example 5: Data Pipelines
	fmt.Println("\n--- Example 5: Data Processing Pipelines ---")
	dataPipelineExample(ctx, dataManager)

	fmt.Println("\n=== Dataset Management Complete ===")
}

// loadAndValidateExample demonstrates data loading and validation
func loadAndValidateExample(ctx context.Context, manager training.DataManager) {
	// Create data source
	dataSource := &MockCSVDataSource{
		filepath: "customer_data.csv",
		records:  generateCustomerData(1000),
	}

	// Register data source
	if err := manager.RegisterDataSource(dataSource); err != nil {
		log.Fatalf("Failed to register data source: %v", err)
	}

	fmt.Println("✓ Data source registered")

	// Create dataset with schema validation
	dataset, err := manager.CreateDataset(ctx, training.DatasetConfig{
		ID:          "customers-2024",
		Name:        "Customer Dataset 2024",
		Type:        training.DatasetTypeTabular,
		Description: "Customer demographics and purchase history",
		Version:     "1.0",
		Source:      dataSource,
		Schema: training.DataSchema{
			Version: "1.0",
			Fields: map[string]training.FieldSchema{
				"customer_id": {
					Name:     "customer_id",
					Type:     "string",
					Required: true,
				},
				"age": {
					Name:     "age",
					Type:     "int",
					Required: true,
					Constraints: map[string]any{
						"min": 18,
						"max": 100,
					},
				},
				"income": {
					Name:     "income",
					Type:     "float",
					Required: true,
					Constraints: map[string]any{
						"min": 0,
					},
				},
				"email": {
					Name:     "email",
					Type:     "string",
					Required: false,
					Format:   "email",
				},
				"country": {
					Name:     "country",
					Type:     "string",
					Required: true,
				},
			},
			Primary: []string{"customer_id"},
		},
		Validation: training.ValidationRules{
			Required: []string{"customer_id", "age", "income", "country"},
			Types: map[string]string{
				"age":    "int",
				"income": "float",
			},
			Ranges: map[string]training.Range{
				"age": {
					Min: 18,
					Max: 100,
				},
				"income": {
					Min: 0,
					Max: 1000000,
				},
			},
			Patterns: map[string]string{
				"email": `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create dataset: %v", err)
	}

	fmt.Printf("✓ Dataset created: %s\n", dataset.ID())
	fmt.Printf("  Records: %d\n", dataset.RecordCount())
	fmt.Printf("  Size:    %d bytes\n", dataset.Size())

	// Validate dataset
	if err := dataset.Validate(ctx); err != nil {
		log.Printf("Validation warnings: %v", err)
	} else {
		fmt.Println("✓ Dataset validation passed")
	}

	// Get schema info
	schema := dataset.GetSchema()
	fmt.Printf("\nSchema Information:\n")
	fmt.Printf("  Version: %s\n", schema.Version)
	fmt.Printf("  Fields:  %d\n", len(schema.Fields))
	fmt.Printf("  Primary Keys: %v\n", schema.Primary)
}

// transformationsExample shows various data transformations
func transformationsExample(ctx context.Context, manager training.DataManager) {
	// Create initial dataset
	dataset, _ := manager.CreateDataset(ctx, training.DatasetConfig{
		ID:   "raw-data",
		Name: "Raw Transaction Data",
		Type: training.DatasetTypeTabular,
		Source: &MockCSVDataSource{
			records: generateTransactionData(500),
		},
	})

	fmt.Printf("✓ Initial dataset: %d records\n", dataset.RecordCount())

	// 1. Filter transformation
	fmt.Println("\n1. Applying filter (amount > 100)...")

	filter := &AmountFilter{minAmount: 100.0}
	filteredDataset, err := dataset.Filter(ctx, filter)
	if err != nil {
		log.Fatalf("Filter failed: %v", err)
	}

	fmt.Printf("   After filter: %d records\n", filteredDataset.RecordCount())

	// 2. Transform - Add calculated field
	fmt.Println("\n2. Applying transformation (add tax field)...")

	transformer := &TaxTransformer{taxRate: 0.10}
	transformedDataset, err := dataset.Transform(ctx, transformer)
	if err != nil {
		log.Fatalf("Transform failed: %v", err)
	}

	fmt.Printf("   After transform: %d records with tax field\n", transformedDataset.RecordCount())

	// 3. Get sample to verify
	sample, _ := transformedDataset.GetSample(ctx, 3)
	fmt.Println("\n   Sample records:")
	for i, record := range sample {
		fmt.Printf("   %d. Amount: $%.2f, Tax: $%.2f\n",
			i+1,
			record.Data["amount"].(float64),
			record.Data["tax"].(float64))
	}
}

// splittingExample demonstrates various splitting strategies
func splittingExample(ctx context.Context, manager training.DataManager) {
	dataset, _ := manager.CreateDataset(ctx, training.DatasetConfig{
		ID:   "ml-data",
		Name: "ML Training Data",
		Type: training.DatasetTypeTabular,
		Source: &MockCSVDataSource{
			records: generateLabeledData(1000),
		},
	})

	fmt.Printf("✓ Dataset: %d records\n\n", dataset.RecordCount())

	// 1. Simple random split (70/15/15)
	fmt.Println("1. Random split (70/15/15):")

	splits, err := dataset.Split(ctx, []float64{0.70, 0.15, 0.15})
	if err != nil {
		log.Fatalf("Split failed: %v", err)
	}

	fmt.Printf("   Train:      %d records\n", splits[0].RecordCount())
	fmt.Printf("   Validation: %d records\n", splits[1].RecordCount())
	fmt.Printf("   Test:       %d records\n", splits[2].RecordCount())

	// 2. Stratified split (preserve class distribution)
	fmt.Println("\n2. Stratified split (preserving class distribution):")

	// Get class distribution
	stats := dataset.GetStatistics()
	fmt.Printf("   Original distribution:\n")
	if labelStats, ok := stats.Fields["label"]; ok {
		for class, count := range labelStats.Distribution {
			percentage := float64(count) / float64(dataset.RecordCount()) * 100
			fmt.Printf("     %s: %d (%.1f%%)\n", class, count, percentage)
		}
	}

	// 3. Time-based split (for time series)
	fmt.Println("\n3. Time-based split (chronological):")
	fmt.Println("   Ensuring no data leakage from future to past")
	fmt.Println("   Train: earliest 70%, Val: middle 15%, Test: latest 15%")
}

// qualityAnalysisExample shows data quality metrics
func qualityAnalysisExample(ctx context.Context, manager training.DataManager) {
	// Create dataset with some quality issues
	dataset, _ := manager.CreateDataset(ctx, training.DatasetConfig{
		ID:   "quality-check-data",
		Name: "Data with Quality Issues",
		Type: training.DatasetTypeTabular,
		Source: &MockCSVDataSource{
			records: generateDataWithIssues(500),
		},
	})

	fmt.Printf("✓ Dataset created: %d records\n\n", dataset.RecordCount())

	// Get detailed statistics
	stats := dataset.GetStatistics()

	fmt.Println("Data Statistics:")
	fmt.Printf("  Record Count: %d\n", stats.RecordCount)
	fmt.Printf("  Size:         %d bytes\n", stats.Size)
	fmt.Printf("  Last Updated: %s\n\n", stats.LastUpdated.Format(time.RFC3339))

	// Field-level statistics
	fmt.Println("Field Statistics:")
	for fieldName, fieldStats := range stats.Fields {
		fmt.Printf("\n  %s:\n", fieldName)
		fmt.Printf("    Total Count:  %d\n", fieldStats.Count)
		fmt.Printf("    Null Count:   %d (%.1f%%)\n",
			fieldStats.NullCount,
			float64(fieldStats.NullCount)/float64(fieldStats.Count)*100)
		fmt.Printf("    Unique Count: %d\n", fieldStats.UniqueCount)

		if fieldStats.Mean != nil {
			fmt.Printf("    Mean:         %.2f\n", *fieldStats.Mean)
		}
		if fieldStats.StdDev != nil {
			fmt.Printf("    Std Dev:      %.2f\n", *fieldStats.StdDev)
		}
	}

	// Data quality metrics
	quality := stats.Quality
	fmt.Println("\nData Quality Metrics:")
	fmt.Printf("  Completeness: %.2f%%\n", quality.Completeness*100)
	fmt.Printf("  Accuracy:     %.2f%%\n", quality.Accuracy*100)
	fmt.Printf("  Consistency:  %.2f%%\n", quality.Consistency*100)
	fmt.Printf("  Validity:     %.2f%%\n", quality.Validity*100)
	fmt.Printf("  Uniqueness:   %.2f%%\n", quality.Uniqueness*100)

	// Quality issues
	if len(quality.Issues) > 0 {
		fmt.Printf("\nQuality Issues Found: %d\n", len(quality.Issues))
		for i, issue := range quality.Issues {
			if i >= 5 {
				fmt.Printf("  ... and %d more issues\n", len(quality.Issues)-5)
				break
			}
			fmt.Printf("  %d. [%s] %s: %s (%d occurrences)\n",
				i+1,
				issue.Severity,
				issue.Type,
				issue.Message,
				issue.Count)
		}
	}

	// Dataset health check
	if err := dataset.HealthCheck(ctx); err != nil {
		fmt.Printf("\n⚠ Health Check: %v\n", err)
	} else {
		fmt.Println("\n✓ Health Check: Passed")
	}
}

// dataPipelineExample shows data processing pipelines
func dataPipelineExample(ctx context.Context, manager training.DataManager) {
	// Create data pipeline
	pipeline, err := manager.CreatePipeline(training.DataPipelineConfig{
		ID:          "etl-pipeline",
		Name:        "ETL Data Pipeline",
		Description: "Extract, Transform, Load pipeline for ML data",
		Stages: []training.DataPipelineStageConfig{
			{
				ID:   "extract",
				Name: "Extract Data",
				Type: "extraction",
				Parameters: map[string]any{
					"source": "database",
					"query":  "SELECT * FROM customers",
				},
			},
			{
				ID:   "clean",
				Name: "Clean Data",
				Type: "cleaning",
				Parameters: map[string]any{
					"remove_duplicates": true,
					"fill_missing":      "mean",
					"remove_outliers":   true,
				},
			},
			{
				ID:   "transform",
				Name: "Transform Features",
				Type: "transformation",
				Parameters: map[string]any{
					"normalize":    true,
					"encode_categorical": true,
					"feature_engineering": []string{"polynomial", "interaction"},
				},
			},
			{
				ID:   "validate",
				Name: "Validate Quality",
				Type: "validation",
				Parameters: map[string]any{
					"min_completeness": 0.95,
					"schema_check":     true,
				},
			},
			{
				ID:   "load",
				Name: "Load to Storage",
				Type: "loading",
				Parameters: map[string]any{
					"destination": "processed-data",
					"format":      "parquet",
				},
			},
		},
		Parameters: map[string]any{
			"batch_size": 1000,
			"parallel":   true,
		},
	})

	if err != nil {
		log.Fatalf("Failed to create pipeline: %v", err)
	}

	fmt.Printf("✓ Data pipeline created: %s\n", pipeline.ID())
	fmt.Printf("  Stages: %d\n", len(pipeline.GetConfig().Stages))

	// Execute pipeline
	fmt.Println("\nExecuting pipeline...")

	output, err := pipeline.Execute(ctx, training.DataPipelineInput{
		Data: map[string]any{
			"source_table": "customers",
		},
		Parameters: map[string]any{
			"start_date": "2024-01-01",
			"end_date":   "2024-12-31",
		},
	})

	if err != nil {
		log.Printf("Pipeline failed: %v", err)
	} else {
		fmt.Println("✓ Pipeline completed successfully")
		fmt.Printf("  Records processed: %v\n", output.Metadata["records_processed"])
		fmt.Printf("  Duration: %v\n", output.Metadata["duration"])
		fmt.Printf("  Metrics: %v\n", output.Metrics)
	}
}

// Mock implementations and helpers

type MockCSVDataSource struct {
	filepath string
	records  []map[string]any
}

func (m *MockCSVDataSource) ID() string                    { return "csv-" + m.filepath }
func (m *MockCSVDataSource) Name() string                  { return m.filepath }
func (m *MockCSVDataSource) Type() training.DataSourceType { return training.DataSourceTypeFile }
func (m *MockCSVDataSource) Connect(ctx context.Context) error               { return nil }
func (m *MockCSVDataSource) Disconnect(ctx context.Context) error            { return nil }
func (m *MockCSVDataSource) GetSchema() training.DataSchema                  { return training.DataSchema{} }
func (m *MockCSVDataSource) GetMetadata() map[string]any                     { return map[string]any{} }
func (m *MockCSVDataSource) Write(ctx context.Context, data training.DataWriter) error {
	return fmt.Errorf("write not supported")
}

func (m *MockCSVDataSource) Read(ctx context.Context, query training.DataQuery) (training.DataReader, error) {
	records := make([]training.DataRecord, len(m.records))
	for i, rec := range m.records {
		records[i] = training.DataRecord{
			ID:   fmt.Sprintf("record-%d", i),
			Data: rec,
		}
	}
	return &MockCSVReader{records: records}, nil
}

type MockCSVReader struct {
	records  []training.DataRecord
	position int
}

func (r *MockCSVReader) Read(ctx context.Context) (training.DataRecord, error) {
	if r.position >= len(r.records) {
		return training.DataRecord{}, fmt.Errorf("EOF")
	}
	record := r.records[r.position]
	r.position++
	return record, nil
}

func (r *MockCSVReader) ReadAll(ctx context.Context) ([]training.DataRecord, error) {
	return r.records, nil
}

func (r *MockCSVReader) ReadBatch(ctx context.Context, size int) ([]training.DataRecord, error) {
	end := r.position + size
	if end > len(r.records) {
		end = len(r.records)
	}
	batch := r.records[r.position:end]
	r.position = end
	return batch, nil
}

func (r *MockCSVReader) Close() error  { return nil }
func (r *MockCSVReader) HasNext() bool { return r.position < len(r.records) }

// AmountFilter filters records by amount
type AmountFilter struct {
	minAmount float64
}

func (f *AmountFilter) Name() string { return "amount-filter" }

func (f *AmountFilter) Filter(ctx context.Context, record training.DataRecord) (bool, error) {
	if amount, ok := record.Data["amount"].(float64); ok {
		return amount > f.minAmount, nil
	}
	return false, nil
}

func (f *AmountFilter) GetCriteria() map[string]any {
	return map[string]any{"min_amount": f.minAmount}
}

// TaxTransformer adds tax field
type TaxTransformer struct {
	taxRate float64
}

func (t *TaxTransformer) Name() string { return "tax-calculator" }

func (t *TaxTransformer) Transform(ctx context.Context, record training.DataRecord) (training.DataRecord, error) {
	if amount, ok := record.Data["amount"].(float64); ok {
		record.Data["tax"] = amount * t.taxRate
		record.Data["total"] = amount * (1 + t.taxRate)
	}
	return record, nil
}

func (t *TaxTransformer) BatchTransform(ctx context.Context, records []training.DataRecord) ([]training.DataRecord, error) {
	for i := range records {
		records[i], _ = t.Transform(ctx, records[i])
	}
	return records, nil
}

func (t *TaxTransformer) Fit(ctx context.Context, dataset training.Dataset) error {
	return nil
}

func (t *TaxTransformer) GetParams() map[string]any {
	return map[string]any{"tax_rate": t.taxRate}
}

func (t *TaxTransformer) SetParams(params map[string]any) error {
	if rate, ok := params["tax_rate"].(float64); ok {
		t.taxRate = rate
	}
	return nil
}

// Data generators
func generateCustomerData(count int) []map[string]any {
	records := make([]map[string]any, count)
	countries := []string{"USA", "UK", "Canada", "Germany", "France"}

	for i := 0; i < count; i++ {
		records[i] = map[string]any{
			"customer_id": fmt.Sprintf("CUST%06d", i),
			"age":         20 + (i % 60),
			"income":      30000.0 + float64(i%100000),
			"country":     countries[i%len(countries)],
			"email":       fmt.Sprintf("customer%d@example.com", i),
		}
	}
	return records
}

func generateTransactionData(count int) []map[string]any {
	records := make([]map[string]any, count)

	for i := 0; i < count; i++ {
		records[i] = map[string]any{
			"transaction_id": fmt.Sprintf("TXN%06d", i),
			"amount":         10.0 + float64(i%500),
			"currency":       "USD",
			"status":         []string{"completed", "pending", "failed"}[i%3],
		}
	}
	return records
}

func generateLabeledData(count int) []map[string]any {
	records := make([]map[string]any, count)
	labels := []string{"class_a", "class_b", "class_c"}

	for i := 0; i < count; i++ {
		records[i] = map[string]any{
			"id":      fmt.Sprintf("data-%d", i),
			"feature1": float64(i % 100),
			"feature2": float64(i % 50),
			"label":    labels[i%len(labels)],
		}
	}
	return records
}

func generateDataWithIssues(count int) []map[string]any {
	records := make([]map[string]any, count)

	for i := 0; i < count; i++ {
		record := map[string]any{
			"id":    fmt.Sprintf("record-%d", i),
			"value": float64(i),
		}

		// Introduce quality issues
		if i%10 == 0 {
			record["value"] = nil // Missing value
		}
		if i%7 == 0 {
			record["duplicate_field"] = record["id"] // Duplicate
		}

		records[i] = record
	}
	return records
}
