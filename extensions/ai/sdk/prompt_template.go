package sdk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/xraph/forge"
)

// PromptTemplateManager manages prompt templates with versioning
type PromptTemplateManager struct {
	logger  forge.Logger
	metrics forge.Metrics

	mu        sync.RWMutex
	templates map[string]map[string]*PromptTemplate // name -> version -> template
	activeABTests map[string]*ABTest
}

// PromptTemplate represents a versioned prompt template
type PromptTemplate struct {
	Name        string
	Version     string
	Description string
	Template    string
	Variables   []string
	Tags        []string
	Metadata    map[string]interface{}
	CreatedAt   time.Time
	UpdatedAt   time.Time
	IsActive    bool
	
	compiled *template.Template
}

// ABTest represents an A/B test configuration
type ABTest struct {
	Name        string
	Description string
	Variants    []ABVariant
	StartDate   time.Time
	EndDate     time.Time
	IsActive    bool
	
	mu sync.RWMutex
	results map[string]*ABTestResults
}

// ABVariant represents a variant in an A/B test
type ABVariant struct {
	Name           string
	TemplateVersion string
	TrafficWeight  float64 // 0.0 to 1.0
}

// ABTestResults tracks results for an A/B test
type ABTestResults struct {
	VariantName string
	Impressions int
	Successes   int
	Failures    int
	AvgLatency  float64
	AvgCost     float64
}

// NewPromptTemplateManager creates a new template manager
func NewPromptTemplateManager(logger forge.Logger, metrics forge.Metrics) *PromptTemplateManager {
	return &PromptTemplateManager{
		logger:        logger,
		metrics:       metrics,
		templates:     make(map[string]map[string]*PromptTemplate),
		activeABTests: make(map[string]*ABTest),
	}
}

// RegisterTemplate registers a new prompt template
func (ptm *PromptTemplateManager) RegisterTemplate(tmpl *PromptTemplate) error {
	if tmpl.Name == "" {
		return errors.New("template name is required")
	}

	if tmpl.Version == "" {
		tmpl.Version = "1.0.0"
	}

	if tmpl.Template == "" {
		return errors.New("template content is required")
	}

	// Compile template
	compiled, err := template.New(tmpl.Name).Parse(tmpl.Template)
	if err != nil {
		return fmt.Errorf("template compilation failed: %w", err)
	}
	tmpl.compiled = compiled

	// Extract variables
	tmpl.Variables = extractTemplateVariables(tmpl.Template)

	tmpl.CreatedAt = time.Now()
	tmpl.UpdatedAt = time.Now()
	tmpl.IsActive = true

	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	if _, exists := ptm.templates[tmpl.Name]; !exists {
		ptm.templates[tmpl.Name] = make(map[string]*PromptTemplate)
	}

	ptm.templates[tmpl.Name][tmpl.Version] = tmpl

	if ptm.logger != nil {
		ptm.logger.Info("Prompt template registered",
			F("name", tmpl.Name),
			F("version", tmpl.Version),
		)
	}

	if ptm.metrics != nil {
		ptm.metrics.Counter("forge.ai.sdk.prompt_templates.registered",
			"name", tmpl.Name,
			"version", tmpl.Version,
		).Inc()
	}

	return nil
}

// GetTemplate retrieves a specific template version
func (ptm *PromptTemplateManager) GetTemplate(name, version string) (*PromptTemplate, error) {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()

	versions, exists := ptm.templates[name]
	if !exists {
		return nil, fmt.Errorf("template %s not found", name)
	}

	if version == "" {
		// Get latest version
		version = ptm.getLatestVersion(versions)
	}

	tmpl, exists := versions[version]
	if !exists {
		return nil, fmt.Errorf("template %s version %s not found", name, version)
	}

	return tmpl, nil
}

// getLatestVersion returns the latest version string
func (ptm *PromptTemplateManager) getLatestVersion(versions map[string]*PromptTemplate) string {
	var latest string
	var latestTime time.Time

	for version, tmpl := range versions {
		if tmpl.CreatedAt.After(latestTime) {
			latest = version
			latestTime = tmpl.CreatedAt
		}
	}

	return latest
}

// ListTemplates returns all templates
func (ptm *PromptTemplateManager) ListTemplates() []*PromptTemplate {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()

	templates := make([]*PromptTemplate, 0)
	for _, versions := range ptm.templates {
		for _, tmpl := range versions {
			templates = append(templates, tmpl)
		}
	}

	return templates
}

// ListVersions returns all versions of a template
func (ptm *PromptTemplateManager) ListVersions(name string) ([]string, error) {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()

	versions, exists := ptm.templates[name]
	if !exists {
		return nil, fmt.Errorf("template %s not found", name)
	}

	versionList := make([]string, 0, len(versions))
	for version := range versions {
		versionList = append(versionList, version)
	}

	return versionList, nil
}

// Render renders a template with the given variables
func (ptm *PromptTemplateManager) Render(name, version string, vars map[string]interface{}) (string, error) {
	tmpl, err := ptm.GetTemplate(name, version)
	if err != nil {
		return "", err
	}

	// Check for A/B test
	if abTest, exists := ptm.activeABTests[name]; exists && abTest.IsActive {
		tmpl, err = ptm.selectABVariant(abTest)
		if err != nil {
			// Fallback to default template
			if ptm.logger != nil {
				ptm.logger.Warn("A/B test variant selection failed, using default", F("error", err))
			}
		}
	}

	var buf bytes.Buffer
	if err := tmpl.compiled.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template rendering failed: %w", err)
	}

	rendered := buf.String()

	if ptm.metrics != nil {
		ptm.metrics.Counter("forge.ai.sdk.prompt_templates.renders",
			"name", tmpl.Name,
			"version", tmpl.Version,
		).Inc()
	}

	return rendered, nil
}

// UpdateTemplate updates an existing template (creates new version)
func (ptm *PromptTemplateManager) UpdateTemplate(name string, newTemplate string) (string, error) {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	versions, exists := ptm.templates[name]
	if !exists {
		return "", fmt.Errorf("template %s not found", name)
	}

	// Get latest version
	latestVersion := ptm.getLatestVersion(versions)
	oldTmpl := versions[latestVersion]

	// Generate new version
	newVersion := ptm.incrementVersion(latestVersion)

	// Create new template
	newTmpl := &PromptTemplate{
		Name:        name,
		Version:     newVersion,
		Description: oldTmpl.Description,
		Template:    newTemplate,
		Tags:        oldTmpl.Tags,
		Metadata:    oldTmpl.Metadata,
		IsActive:    true,
	}

	// Compile
	compiled, err := template.New(name).Parse(newTemplate)
	if err != nil {
		return "", fmt.Errorf("template compilation failed: %w", err)
	}
	newTmpl.compiled = compiled
	newTmpl.Variables = extractTemplateVariables(newTemplate)
	newTmpl.CreatedAt = time.Now()
	newTmpl.UpdatedAt = time.Now()

	// Deactivate old version
	oldTmpl.IsActive = false
	oldTmpl.UpdatedAt = time.Now()

	versions[newVersion] = newTmpl

	if ptm.logger != nil {
		ptm.logger.Info("Template updated",
			F("name", name),
			F("old_version", latestVersion),
			F("new_version", newVersion),
		)
	}

	return newVersion, nil
}

// incrementVersion increments a semantic version
func (ptm *PromptTemplateManager) incrementVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "1.0.1"
	}

	var major, minor, patch int
	fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)
	patch++

	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// DeleteTemplate deletes a template version
func (ptm *PromptTemplateManager) DeleteTemplate(name, version string) error {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	versions, exists := ptm.templates[name]
	if !exists {
		return fmt.Errorf("template %s not found", name)
	}

	if version == "" {
		// Delete all versions
		delete(ptm.templates, name)
	} else {
		delete(versions, version)
		if len(versions) == 0 {
			delete(ptm.templates, name)
		}
	}

	if ptm.logger != nil {
		ptm.logger.Info("Template deleted", F("name", name), F("version", version))
	}

	return nil
}

// CreateABTest creates a new A/B test
func (ptm *PromptTemplateManager) CreateABTest(test *ABTest) error {
	if test.Name == "" {
		return errors.New("A/B test name is required")
	}

	if len(test.Variants) < 2 {
		return errors.New("A/B test must have at least 2 variants")
	}

	// Validate traffic weights sum to 1.0
	totalWeight := 0.0
	for _, variant := range test.Variants {
		totalWeight += variant.TrafficWeight
	}
	if totalWeight < 0.99 || totalWeight > 1.01 {
		return fmt.Errorf("traffic weights must sum to 1.0, got %.2f", totalWeight)
	}

	test.IsActive = true
	test.StartDate = time.Now()
	test.results = make(map[string]*ABTestResults)

	for _, variant := range test.Variants {
		test.results[variant.Name] = &ABTestResults{
			VariantName: variant.Name,
		}
	}

	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	ptm.activeABTests[test.Name] = test

	if ptm.logger != nil {
		ptm.logger.Info("A/B test created",
			F("name", test.Name),
			F("variants", len(test.Variants)),
		)
	}

	return nil
}

// selectABVariant selects a variant based on traffic weights
func (ptm *PromptTemplateManager) selectABVariant(test *ABTest) (*PromptTemplate, error) {
	r := rand.Float64()
	cumulative := 0.0

	for _, variant := range test.Variants {
		cumulative += variant.TrafficWeight
		if r <= cumulative {
			// Record impression
			test.mu.Lock()
			if result, exists := test.results[variant.Name]; exists {
				result.Impressions++
			}
			test.mu.Unlock()

			// Get template for this variant
			tmpl, err := ptm.GetTemplate(test.Name, variant.TemplateVersion)
			if err != nil {
				return nil, err
			}

			if ptm.metrics != nil {
				ptm.metrics.Counter("forge.ai.sdk.prompt_templates.ab_test.variant_selected",
					"test", test.Name,
					"variant", variant.Name,
				).Inc()
			}

			return tmpl, nil
		}
	}

	// Fallback to first variant
	return ptm.GetTemplate(test.Name, test.Variants[0].TemplateVersion)
}

// RecordABTestResult records the result of an A/B test variant
func (ptm *PromptTemplateManager) RecordABTestResult(testName, variantName string, success bool, latency time.Duration, cost float64) error {
	ptm.mu.RLock()
	test, exists := ptm.activeABTests[testName]
	ptm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("A/B test %s not found", testName)
	}

	test.mu.Lock()
	defer test.mu.Unlock()

	result, exists := test.results[variantName]
	if !exists {
		return fmt.Errorf("variant %s not found in test %s", variantName, testName)
	}

	if success {
		result.Successes++
	} else {
		result.Failures++
	}

	// Update averages
	totalRequests := float64(result.Successes + result.Failures)
	result.AvgLatency = (result.AvgLatency*(totalRequests-1) + latency.Seconds()) / totalRequests
	result.AvgCost = (result.AvgCost*(totalRequests-1) + cost) / totalRequests

	return nil
}

// GetABTestResults retrieves results for an A/B test
func (ptm *PromptTemplateManager) GetABTestResults(testName string) (map[string]*ABTestResults, error) {
	ptm.mu.RLock()
	test, exists := ptm.activeABTests[testName]
	ptm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("A/B test %s not found", testName)
	}

	test.mu.RLock()
	defer test.mu.RUnlock()

	// Return a copy
	results := make(map[string]*ABTestResults)
	for name, result := range test.results {
		resultCopy := *result
		results[name] = &resultCopy
	}

	return results, nil
}

// StopABTest stops an active A/B test
func (ptm *PromptTemplateManager) StopABTest(testName string) error {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	test, exists := ptm.activeABTests[testName]
	if !exists {
		return fmt.Errorf("A/B test %s not found", testName)
	}

	test.IsActive = false
	test.EndDate = time.Now()

	if ptm.logger != nil {
		ptm.logger.Info("A/B test stopped", F("name", testName))
	}

	return nil
}

// CompareTemplates compares two template versions
func (ptm *PromptTemplateManager) CompareTemplates(name, version1, version2 string) (*TemplateDiff, error) {
	tmpl1, err := ptm.GetTemplate(name, version1)
	if err != nil {
		return nil, err
	}

	tmpl2, err := ptm.GetTemplate(name, version2)
	if err != nil {
		return nil, err
	}

	diff := &TemplateDiff{
		Name:     name,
		Version1: version1,
		Version2: version2,
		IsSame:   tmpl1.Template == tmpl2.Template,
	}

	// Calculate hash
	hash1 := sha256.Sum256([]byte(tmpl1.Template))
	hash2 := sha256.Sum256([]byte(tmpl2.Template))
	diff.Hash1 = fmt.Sprintf("%x", hash1)
	diff.Hash2 = fmt.Sprintf("%x", hash2)

	// Simple diff (in production, use proper diff algorithm)
	if !diff.IsSame {
		diff.Changes = fmt.Sprintf("Template content changed from version %s to %s", version1, version2)
	}

	return diff, nil
}

// TemplateDiff represents the difference between two templates
type TemplateDiff struct {
	Name     string
	Version1 string
	Version2 string
	IsSame   bool
	Hash1    string
	Hash2    string
	Changes  string
}

// ExportTemplate exports a template as JSON
func (ptm *PromptTemplateManager) ExportTemplate(name, version string) (string, error) {
	tmpl, err := ptm.GetTemplate(name, version)
	if err != nil {
		return "", err
	}

	// Don't export compiled template
	export := struct {
		Name        string
		Version     string
		Description string
		Template    string
		Variables   []string
		Tags        []string
		Metadata    map[string]interface{}
		CreatedAt   time.Time
	}{
		Name:        tmpl.Name,
		Version:     tmpl.Version,
		Description: tmpl.Description,
		Template:    tmpl.Template,
		Variables:   tmpl.Variables,
		Tags:        tmpl.Tags,
		Metadata:    tmpl.Metadata,
		CreatedAt:   tmpl.CreatedAt,
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return "", fmt.Errorf("export failed: %w", err)
	}

	return string(data), nil
}

// ImportTemplate imports a template from JSON
func (ptm *PromptTemplateManager) ImportTemplate(jsonData string) error {
	var tmpl PromptTemplate
	if err := json.Unmarshal([]byte(jsonData), &tmpl); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	return ptm.RegisterTemplate(&tmpl)
}

// extractTemplateVariables extracts variable names from a template
func extractTemplateVariables(tmplStr string) []string {
	re := regexp.MustCompile(`\{\{\.(\w+)\}\}`)
	matches := re.FindAllStringSubmatch(tmplStr, -1)

	vars := make([]string, 0)
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) > 1 {
			varName := match[1]
			if !seen[varName] {
				vars = append(vars, varName)
				seen[varName] = true
			}
		}
	}

	return vars
}

// ValidateVariables checks if all required variables are provided
func (ptm *PromptTemplateManager) ValidateVariables(name, version string, vars map[string]interface{}) error {
	tmpl, err := ptm.GetTemplate(name, version)
	if err != nil {
		return err
	}

	missingVars := make([]string, 0)
	for _, varName := range tmpl.Variables {
		if _, exists := vars[varName]; !exists {
			missingVars = append(missingVars, varName)
		}
	}

	if len(missingVars) > 0 {
		return fmt.Errorf("missing required variables: %v", missingVars)
	}

	return nil
}

