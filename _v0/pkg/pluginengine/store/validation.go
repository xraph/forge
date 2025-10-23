package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// PluginValidator validates plugin packages and metadata
type PluginValidator struct {
	config      StoreConfig
	rules       []ValidationRule
	logger      common.Logger
	metrics     common.Metrics
	initialized bool
}

// ValidationRule defines a validation rule
type ValidationRule interface {
	Name() string
	Description() string
	Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult
	Category() ValidationCategory
	Severity() ValidationSeverity
}

// ValidationCategory defines the category of validation
type ValidationCategory string

const (
	CategorySecurity     ValidationCategory = "security"
	CategoryStructure    ValidationCategory = "structure"
	CategoryMetadata     ValidationCategory = "metadata"
	CategoryDependencies ValidationCategory = "dependencies"
	CategoryAPI          ValidationCategory = "api"
	CategoryPerformance  ValidationCategory = "performance"
)

// ValidationSeverity defines the severity of validation issues
type ValidationSeverity string

const (
	SeverityInfo     ValidationSeverity = "info"
	SeverityWarning  ValidationSeverity = "warning"
	SeverityError    ValidationSeverity = "error"
	SeverityCritical ValidationSeverity = "critical"
)

// ValidationResult contains the result of a validation
type ValidationResult struct {
	Rule        string                 `json:"rule"`
	Category    ValidationCategory     `json:"category"`
	Severity    ValidationSeverity     `json:"severity"`
	Passed      bool                   `json:"passed"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Suggestions []string               `json:"suggestions,omitempty"`
}

// ValidationReport contains the complete validation report
type ValidationReport struct {
	PluginID  string             `json:"plugin_id"`
	Version   string             `json:"version"`
	Timestamp time.Time          `json:"timestamp"`
	Duration  time.Duration      `json:"duration"`
	Passed    bool               `json:"passed"`
	Results   []ValidationResult `json:"results"`
	Summary   ValidationSummary  `json:"summary"`
	Checksum  string             `json:"checksum"`
}

// ValidationSummary provides a summary of validation results
type ValidationSummary struct {
	TotalRules    int `json:"total_rules"`
	PassedRules   int `json:"passed_rules"`
	FailedRules   int `json:"failed_rules"`
	InfoCount     int `json:"info_count"`
	WarningCount  int `json:"warning_count"`
	ErrorCount    int `json:"error_count"`
	CriticalCount int `json:"critical_count"`
}

// NewPluginValidator creates a new plugin validator
func NewPluginValidator(config StoreConfig, logger common.Logger, metrics common.Metrics) *PluginValidator {
	return &PluginValidator{
		config:  config,
		rules:   []ValidationRule{},
		logger:  logger,
		metrics: metrics,
	}
}

// Initialize initializes the plugin validator
func (pv *PluginValidator) Initialize(ctx context.Context) error {
	if pv.initialized {
		return nil
	}

	// Register default validation rules
	pv.registerDefaultRules()

	pv.initialized = true

	pv.logger.Info("plugin validator initialized",
		logger.Int("validation_rules", len(pv.rules)),
	)

	if pv.metrics != nil {
		pv.metrics.Counter("forge.plugins.validator_initialized").Inc()
	}

	return nil
}

// Stop stops the plugin validator
func (pv *PluginValidator) Stop(ctx context.Context) error {
	if !pv.initialized {
		return nil
	}

	pv.initialized = false

	pv.logger.Info("plugin validator stopped")

	if pv.metrics != nil {
		pv.metrics.Counter("forge.plugins.validator_stopped").Inc()
	}

	return nil
}

// ValidatePackage validates a plugin package
func (pv *PluginValidator) ValidatePackage(ctx context.Context, pkg plugins.PluginEnginePackage) error {
	report := pv.ValidatePackageDetailed(ctx, pkg)

	if !report.Passed {
		// Count critical and error issues
		criticalCount := 0
		errorCount := 0

		for _, result := range report.Results {
			if result.Severity == SeverityCritical {
				criticalCount++
			} else if result.Severity == SeverityError {
				errorCount++
			}
		}

		if criticalCount > 0 {
			return fmt.Errorf("plugin validation failed: %d critical issues found", criticalCount)
		}

		if errorCount > 0 {
			return fmt.Errorf("plugin validation failed: %d error issues found", errorCount)
		}
	}

	return nil
}

// ValidatePackageDetailed validates a plugin package and returns detailed report
func (pv *PluginValidator) ValidatePackageDetailed(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationReport {
	startTime := time.Now()

	report := ValidationReport{
		PluginID:  pkg.Info.ID,
		Version:   pkg.Info.Version,
		Timestamp: startTime,
		Results:   []ValidationResult{},
		Summary: ValidationSummary{
			TotalRules: len(pv.rules),
		},
	}

	// Calculate package checksum
	hasher := sha256.New()
	hasher.Write(pkg.Binary)
	report.Checksum = hex.EncodeToString(hasher.Sum(nil))

	// Run all validation rules
	for _, rule := range pv.rules {
		result := rule.Validate(ctx, pkg)
		report.Results = append(report.Results, result)

		// Update summary
		if result.Passed {
			report.Summary.PassedRules++
		} else {
			report.Summary.FailedRules++
		}

		switch result.Severity {
		case SeverityInfo:
			report.Summary.InfoCount++
		case SeverityWarning:
			report.Summary.WarningCount++
		case SeverityError:
			report.Summary.ErrorCount++
		case SeverityCritical:
			report.Summary.CriticalCount++
		}
	}

	// Determine overall pass/fail
	report.Passed = report.Summary.CriticalCount == 0 && report.Summary.ErrorCount == 0

	report.Duration = time.Since(startTime)

	pv.logger.Debug("plugin validation completed",
		logger.String("plugin_id", pkg.Info.ID),
		logger.String("version", pkg.Info.Version),
		logger.Bool("passed", report.Passed),
		logger.Int("total_rules", report.Summary.TotalRules),
		logger.Int("failed_rules", report.Summary.FailedRules),
		logger.Duration("duration", report.Duration),
	)

	if pv.metrics != nil {
		pv.metrics.Counter("forge.plugins.validation_performed").Inc()
		pv.metrics.Histogram("forge.plugins.validation_duration").Observe(report.Duration.Seconds())

		if report.Passed {
			pv.metrics.Counter("forge.plugins.validation_passed").Inc()
		} else {
			pv.metrics.Counter("forge.plugins.validation_failed").Inc()
		}
	}

	return report
}

// AddRule adds a validation rule
func (pv *PluginValidator) AddRule(rule ValidationRule) {
	pv.rules = append(pv.rules, rule)
}

// RemoveRule removes a validation rule
func (pv *PluginValidator) RemoveRule(ruleName string) {
	for i, rule := range pv.rules {
		if rule.Name() == ruleName {
			pv.rules = append(pv.rules[:i], pv.rules[i+1:]...)
			break
		}
	}
}

// GetRules returns all validation rules
func (pv *PluginValidator) GetRules() []ValidationRule {
	return pv.rules
}

// registerDefaultRules registers default validation rules
func (pv *PluginValidator) registerDefaultRules() {
	// Security rules
	pv.AddRule(&ChecksumValidationRule{})
	pv.AddRule(&FileSizeValidationRule{maxSize: pv.config.MaxDownloadSize})
	pv.AddRule(&MalwareDetectionRule{})

	// Structure rules
	pv.AddRule(&PluginInfoValidationRule{})
	pv.AddRule(&BinaryFormatValidationRule{})
	pv.AddRule(&ConfigSchemaValidationRule{})

	// Metadata rules
	pv.AddRule(&VersionValidationRule{})
	pv.AddRule(&LicenseValidationRule{})
	pv.AddRule(&AuthorValidationRule{})

	// Dependency rules
	pv.AddRule(&DependencyValidationRule{})
	pv.AddRule(&CircularDependencyRule{})

	// API rules
	pv.AddRule(&APICompatibilityRule{})
	pv.AddRule(&InterfaceValidationRule{})

	// Performance rules
	pv.AddRule(&PerformanceAnalysisRule{})
}

// Built-in validation rules

// ChecksumValidationRule validates package checksum
type ChecksumValidationRule struct{}

func (r *ChecksumValidationRule) Name() string                 { return "checksum_validation" }
func (r *ChecksumValidationRule) Description() string          { return "Validates package checksum integrity" }
func (r *ChecksumValidationRule) Category() ValidationCategory { return CategorySecurity }
func (r *ChecksumValidationRule) Severity() ValidationSeverity { return SeverityCritical }

func (r *ChecksumValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	if pkg.Checksum == "" {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Package checksum is missing",
		}
	}

	// Calculate actual checksum
	hasher := sha256.New()
	hasher.Write(pkg.Binary)
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))

	if actualChecksum != pkg.Checksum {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Package checksum mismatch",
			Details: map[string]interface{}{
				"expected": pkg.Checksum,
				"actual":   actualChecksum,
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Package checksum is valid",
	}
}

// FileSizeValidationRule validates file size limits
type FileSizeValidationRule struct {
	maxSize int64
}

func (r *FileSizeValidationRule) Name() string                 { return "file_size_validation" }
func (r *FileSizeValidationRule) Description() string          { return "Validates package file size limits" }
func (r *FileSizeValidationRule) Category() ValidationCategory { return CategorySecurity }
func (r *FileSizeValidationRule) Severity() ValidationSeverity { return SeverityError }

func (r *FileSizeValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	size := int64(len(pkg.Binary))

	if size > r.maxSize {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  fmt.Sprintf("Package size exceeds limit: %d bytes > %d bytes", size, r.maxSize),
			Details: map[string]interface{}{
				"size":     size,
				"max_size": r.maxSize,
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Package size is within limits",
	}
}

// MalwareDetectionRule basic malware detection
type MalwareDetectionRule struct{}

func (r *MalwareDetectionRule) Name() string                 { return "malware_detection" }
func (r *MalwareDetectionRule) Description() string          { return "Basic malware detection" }
func (r *MalwareDetectionRule) Category() ValidationCategory { return CategorySecurity }
func (r *MalwareDetectionRule) Severity() ValidationSeverity { return SeverityCritical }

func (r *MalwareDetectionRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	// Basic signature-based detection
	suspicious := []string{
		"eval(",
		"exec(",
		"system(",
		"os.system",
		"subprocess.call",
		"Runtime.getRuntime",
	}

	content := string(pkg.Binary)
	for _, sig := range suspicious {
		if strings.Contains(content, sig) {
			return ValidationResult{
				Rule:     r.Name(),
				Category: r.Category(),
				Severity: r.Severity(),
				Passed:   false,
				Message:  fmt.Sprintf("Suspicious code pattern detected: %s", sig),
				Details: map[string]interface{}{
					"pattern": sig,
				},
			}
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "No suspicious patterns detected",
	}
}

// PluginInfoValidationRule validates plugin metadata
type PluginInfoValidationRule struct{}

func (r *PluginInfoValidationRule) Name() string                 { return "plugin_info_validation" }
func (r *PluginInfoValidationRule) Description() string          { return "Validates plugin metadata" }
func (r *PluginInfoValidationRule) Category() ValidationCategory { return CategoryMetadata }
func (r *PluginInfoValidationRule) Severity() ValidationSeverity { return SeverityError }

func (r *PluginInfoValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	info := pkg.Info

	if info.ID == "" {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin ID is required",
		}
	}

	if info.Name == "" {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin name is required",
		}
	}

	if info.Version == "" {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin version is required",
		}
	}

	if info.Author == "" {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin author is required",
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin metadata is valid",
	}
}

// BinaryFormatValidationRule validates binary format
type BinaryFormatValidationRule struct{}

func (r *BinaryFormatValidationRule) Name() string                 { return "binary_format_validation" }
func (r *BinaryFormatValidationRule) Description() string          { return "Validates binary format" }
func (r *BinaryFormatValidationRule) Category() ValidationCategory { return CategoryStructure }
func (r *BinaryFormatValidationRule) Severity() ValidationSeverity { return SeverityError }

func (r *BinaryFormatValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	if len(pkg.Binary) == 0 {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin binary is empty",
		}
	}

	// Check for valid binary format (simplified check)
	if len(pkg.Binary) < 4 {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin binary is too small",
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin binary format is valid",
	}
}

// ConfigSchemaValidationRule validates configuration schema
type ConfigSchemaValidationRule struct{}

func (r *ConfigSchemaValidationRule) Name() string                 { return "config_schema_validation" }
func (r *ConfigSchemaValidationRule) Description() string          { return "Validates configuration schema" }
func (r *ConfigSchemaValidationRule) Category() ValidationCategory { return CategoryStructure }
func (r *ConfigSchemaValidationRule) Severity() ValidationSeverity { return SeverityWarning }

func (r *ConfigSchemaValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	// This would validate the configuration schema
	// For now, just check if config is present
	if len(pkg.Config) == 0 {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin configuration is missing",
			Suggestions: []string{
				"Add a configuration schema to make the plugin more configurable",
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin configuration is present",
	}
}

// VersionValidationRule validates version format
type VersionValidationRule struct{}

func (r *VersionValidationRule) Name() string                 { return "version_validation" }
func (r *VersionValidationRule) Description() string          { return "Validates version format" }
func (r *VersionValidationRule) Category() ValidationCategory { return CategoryMetadata }
func (r *VersionValidationRule) Severity() ValidationSeverity { return SeverityError }

func (r *VersionValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	version := pkg.Info.Version

	// Check for semantic versioning
	semverPattern := `^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`
	matched, _ := regexp.MatchString(semverPattern, version)

	if !matched {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Version does not follow semantic versioning format",
			Details: map[string]interface{}{
				"version": version,
			},
			Suggestions: []string{
				"Use semantic versioning format (e.g., 1.0.0, 2.1.3-beta.1)",
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Version format is valid",
	}
}

// LicenseValidationRule validates license information
type LicenseValidationRule struct{}

func (r *LicenseValidationRule) Name() string                 { return "license_validation" }
func (r *LicenseValidationRule) Description() string          { return "Validates license information" }
func (r *LicenseValidationRule) Category() ValidationCategory { return CategoryMetadata }
func (r *LicenseValidationRule) Severity() ValidationSeverity { return SeverityWarning }

func (r *LicenseValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	license := pkg.Info.License

	if license == "" {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin license is missing",
			Suggestions: []string{
				"Add a license to clarify usage rights",
			},
		}
	}

	// Check for common license formats
	commonLicenses := []string{
		"MIT", "Apache-2.0", "GPL-3.0", "BSD-3-Clause", "ISC", "LGPL-2.1",
	}

	for _, commonLicense := range commonLicenses {
		if strings.EqualFold(license, commonLicense) {
			return ValidationResult{
				Rule:     r.Name(),
				Category: r.Category(),
				Severity: r.Severity(),
				Passed:   true,
				Message:  "Plugin license is valid",
			}
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   false,
		Message:  "Plugin license is not a recognized format",
		Details: map[string]interface{}{
			"license": license,
		},
		Suggestions: []string{
			"Use a standard license identifier (MIT, Apache-2.0, etc.)",
		},
	}
}

// AuthorValidationRule validates author information
type AuthorValidationRule struct{}

func (r *AuthorValidationRule) Name() string                 { return "author_validation" }
func (r *AuthorValidationRule) Description() string          { return "Validates author information" }
func (r *AuthorValidationRule) Category() ValidationCategory { return CategoryMetadata }
func (r *AuthorValidationRule) Severity() ValidationSeverity { return SeverityWarning }

func (r *AuthorValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	author := pkg.Info.Author

	if author == "" {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin author is missing",
		}
	}

	// Basic validation - check for suspicious authors
	if strings.Contains(strings.ToLower(author), "anonymous") ||
		strings.Contains(strings.ToLower(author), "unknown") {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin author appears to be anonymous or unknown",
			Details: map[string]interface{}{
				"author": author,
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin author is valid",
	}
}

// DependencyValidationRule validates plugin dependencies
type DependencyValidationRule struct{}

func (r *DependencyValidationRule) Name() string                 { return "dependency_validation" }
func (r *DependencyValidationRule) Description() string          { return "Validates plugin dependencies" }
func (r *DependencyValidationRule) Category() ValidationCategory { return CategoryDependencies }
func (r *DependencyValidationRule) Severity() ValidationSeverity { return SeverityError }

func (r *DependencyValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	dependencies := pkg.Info.Dependencies

	for _, dep := range dependencies {
		if dep.Name == "" {
			return ValidationResult{
				Rule:     r.Name(),
				Category: r.Category(),
				Severity: r.Severity(),
				Passed:   false,
				Message:  "Dependency name is missing",
			}
		}

		if dep.Version == "" && dep.Required {
			return ValidationResult{
				Rule:     r.Name(),
				Category: r.Category(),
				Severity: r.Severity(),
				Passed:   false,
				Message:  fmt.Sprintf("Required dependency '%s' is missing version", dep.Name),
			}
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin dependencies are valid",
	}
}

// CircularDependencyRule checks for circular dependencies
type CircularDependencyRule struct{}

func (r *CircularDependencyRule) Name() string                 { return "circular_dependency" }
func (r *CircularDependencyRule) Description() string          { return "Checks for circular dependencies" }
func (r *CircularDependencyRule) Category() ValidationCategory { return CategoryDependencies }
func (r *CircularDependencyRule) Severity() ValidationSeverity { return SeverityError }

func (r *CircularDependencyRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	// This would need access to the plugin registry to check for circular dependencies
	// For now, just check if plugin depends on itself
	pluginID := pkg.Info.ID

	for _, dep := range pkg.Info.Dependencies {
		if dep.Name == pluginID {
			return ValidationResult{
				Rule:     r.Name(),
				Category: r.Category(),
				Severity: r.Severity(),
				Passed:   false,
				Message:  "Plugin has circular dependency on itself",
			}
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "No circular dependencies detected",
	}
}

// APICompatibilityRule validates API compatibility
type APICompatibilityRule struct{}

func (r *APICompatibilityRule) Name() string                 { return "api_compatibility" }
func (r *APICompatibilityRule) Description() string          { return "Validates API compatibility" }
func (r *APICompatibilityRule) Category() ValidationCategory { return CategoryAPI }
func (r *APICompatibilityRule) Severity() ValidationSeverity { return SeverityError }

func (r *APICompatibilityRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	compatibility := pkg.Info.Compatibility

	if len(compatibility) == 0 {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin compatibility information is missing",
			Suggestions: []string{
				"Add compatibility information to specify supported framework versions",
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin compatibility information is present",
	}
}

// InterfaceValidationRule validates plugin interfaces
type InterfaceValidationRule struct{}

func (r *InterfaceValidationRule) Name() string                 { return "interface_validation" }
func (r *InterfaceValidationRule) Description() string          { return "Validates plugin interfaces" }
func (r *InterfaceValidationRule) Category() ValidationCategory { return CategoryAPI }
func (r *InterfaceValidationRule) Severity() ValidationSeverity { return SeverityError }

func (r *InterfaceValidationRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	// This would validate that the plugin implements required interfaces
	// For now, just check if capabilities are defined
	capabilities := pkg.Info.Capabilities

	if len(capabilities) == 0 {
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin capabilities are not defined",
			Suggestions: []string{
				"Define plugin capabilities to specify what the plugin can do",
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin capabilities are defined",
	}
}

// PerformanceAnalysisRule analyzes plugin performance characteristics
type PerformanceAnalysisRule struct{}

func (r *PerformanceAnalysisRule) Name() string { return "performance_analysis" }
func (r *PerformanceAnalysisRule) Description() string {
	return "Analyzes plugin performance characteristics"
}
func (r *PerformanceAnalysisRule) Category() ValidationCategory { return CategoryPerformance }
func (r *PerformanceAnalysisRule) Severity() ValidationSeverity { return SeverityInfo }

func (r *PerformanceAnalysisRule) Validate(ctx context.Context, pkg plugins.PluginEnginePackage) ValidationResult {
	// Basic performance analysis based on binary size
	size := len(pkg.Binary)

	if size > 50*1024*1024 { // 50MB
		return ValidationResult{
			Rule:     r.Name(),
			Category: r.Category(),
			Severity: r.Severity(),
			Passed:   false,
			Message:  "Plugin is very large and may impact performance",
			Details: map[string]interface{}{
				"size": size,
			},
			Suggestions: []string{
				"Consider optimizing the plugin binary size",
				"Remove unnecessary dependencies",
			},
		}
	}

	return ValidationResult{
		Rule:     r.Name(),
		Category: r.Category(),
		Severity: r.Severity(),
		Passed:   true,
		Message:  "Plugin size is reasonable",
	}
}
