package security

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// SecurityScanner provides security scanning capabilities for plugins
type SecurityScanner interface {
	// Package scanning
	ScanPackage(ctx context.Context, pkg plugins.PluginEnginePackage) (ScanResult, error)
	ScanBinary(ctx context.Context, binary []byte) (ScanResult, error)
	ScanSource(ctx context.Context, sourcePath string) (ScanResult, error)

	// Vulnerability database
	UpdateVulnerabilityDatabase(ctx context.Context) error
	GetVulnerabilityDatabase() VulnerabilityDatabase

	// Scanning rules
	AddScanRule(rule ScanRule) error
	RemoveScanRule(ruleID string) error
	GetScanRules() []ScanRule

	// Results management
	GetScanHistory(pluginID string) ([]ScanResult, error)
	GetScanResult(scanID string) (ScanResult, error)

	// Statistics
	GetScannerStats() ScannerStats
}

// ScanResult represents the result of a security scan
type ScanResult struct {
	ID              string                 `json:"id"`
	PluginID        string                 `json:"plugin_id"`
	ScanType        ScanType               `json:"scan_type"`
	Status          ScanStatus             `json:"status"`
	RiskLevel       RiskLevel              `json:"risk_level"`
	Score           float64                `json:"score"`
	Vulnerabilities []Vulnerability        `json:"vulnerabilities"`
	Warnings        []SecurityWarning      `json:"warnings"`
	Metadata        map[string]interface{} `json:"metadata"`
	StartTime       time.Time              `json:"start_time"`
	EndTime         time.Time              `json:"end_time"`
	Duration        time.Duration          `json:"duration"`
	ScannerInfo     ScannerInfo            `json:"scanner_info"`
}

// ScanType defines the type of security scan
type ScanType string

const (
	ScanTypePackage       ScanType = "package"
	ScanTypeBinary        ScanType = "binary"
	ScanTypeSource        ScanType = "source"
	ScanTypeDependency    ScanType = "dependency"
	ScanTypeRuntime       ScanType = "runtime"
	ScanTypeConfiguration ScanType = "configuration"
)

// ScanStatus defines the status of a security scan
type ScanStatus string

const (
	ScanStatusPending   ScanStatus = "pending"
	ScanStatusRunning   ScanStatus = "running"
	ScanStatusCompleted ScanStatus = "completed"
	ScanStatusFailed    ScanStatus = "failed"
	ScanStatusCancelled ScanStatus = "cancelled"
)

// RiskLevel defines the risk level of a vulnerability
type RiskLevel string

const (
	RiskLevelCritical RiskLevel = "critical"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelLow      RiskLevel = "low"
	RiskLevelInfo     RiskLevel = "info"
)

// Vulnerability represents a security vulnerability
type Vulnerability struct {
	ID            string                 `json:"id"`
	Type          VulnerabilityType      `json:"type"`
	Severity      Severity               `json:"severity"`
	Title         string                 `json:"title"`
	Description   string                 `json:"description"`
	Location      Location               `json:"location"`
	CWE           string                 `json:"cwe"`
	CVE           string                 `json:"cve"`
	CVSS          CVSSScore              `json:"cvss"`
	References    []string               `json:"references"`
	Remediation   Remediation            `json:"remediation"`
	FirstSeen     time.Time              `json:"first_seen"`
	LastSeen      time.Time              `json:"last_seen"`
	FalsePositive bool                   `json:"false_positive"`
	Suppressed    bool                   `json:"suppressed"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// VulnerabilityType defines the type of vulnerability
type VulnerabilityType string

const (
	VulnerabilityTypeCodeInjection           VulnerabilityType = "code_injection"
	VulnerabilityTypeSQLInjection            VulnerabilityType = "sql_injection"
	VulnerabilityTypeXSS                     VulnerabilityType = "xss"
	VulnerabilityTypeCSRF                    VulnerabilityType = "csrf"
	VulnerabilityTypeAuthBypass              VulnerabilityType = "auth_bypass"
	VulnerabilityTypePrivilegeEscalation     VulnerabilityType = "privilege_escalation"
	VulnerabilityTypeBufferOverflow          VulnerabilityType = "buffer_overflow"
	VulnerabilityTypeMemoryLeak              VulnerabilityType = "memory_leak"
	VulnerabilityTypeRaceCondition           VulnerabilityType = "race_condition"
	VulnerabilityTypeInformationDisclosure   VulnerabilityType = "information_disclosure"
	VulnerabilityTypeWeakCryptography        VulnerabilityType = "weak_cryptography"
	VulnerabilityTypeInsecureDeserialization VulnerabilityType = "insecure_deserialization"
	VulnerabilityTypeMaliciousCode           VulnerabilityType = "malicious_code"
	VulnerabilityTypeOutdatedDependency      VulnerabilityType = "outdated_dependency"
)

// Severity defines the severity of a vulnerability
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Location represents the location of a vulnerability
type Location struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Function string `json:"function"`
	Method   string `json:"method"`
	Class    string `json:"class"`
}

// CVSSScore represents a CVSS score
type CVSSScore struct {
	Version string  `json:"version"`
	Vector  string  `json:"vector"`
	Score   float64 `json:"score"`
}

// Remediation provides remediation information
type Remediation struct {
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	References  []string `json:"references"`
	Effort      string   `json:"effort"`
	Priority    string   `json:"priority"`
}

// SecurityWarning represents a security warning
type SecurityWarning struct {
	ID          string                 `json:"id"`
	Type        WarningType            `json:"type"`
	Severity    Severity               `json:"severity"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Location    Location               `json:"location"`
	Suggestion  string                 `json:"suggestion"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// WarningType defines the type of security warning
type WarningType string

const (
	WarningTypeWeakConfiguration     WarningType = "weak_configuration"
	WarningTypeInsecurePractice      WarningType = "insecure_practice"
	WarningTypeDeprecatedAPI         WarningType = "deprecated_api"
	WarningTypeWeakPermissions       WarningType = "weak_permissions"
	WarningTypeUnencryptedData       WarningType = "unencrypted_data"
	WarningTypeHardcodedSecrets      WarningType = "hardcoded_secrets"
	WarningTypeInsecureDefaults      WarningType = "insecure_defaults"
	WarningTypeInformationDisclosure WarningType = "information_disclosure"
	WarningTypeMaliciousCode         WarningType = "malicious_code"
	WarningTypeOutdatedDependency    WarningType = "outdated_dependency"
)

// ScannerInfo provides information about the scanner
type ScannerInfo struct {
	Name     string    `json:"name"`
	Version  string    `json:"version"`
	Database string    `json:"database"`
	Updated  time.Time `json:"updated"`
}

// ScanRule defines a custom scanning rule
type ScanRule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        ScanRuleType           `json:"type"`
	Pattern     string                 `json:"pattern"`
	Severity    Severity               `json:"severity"`
	Category    string                 `json:"category"`
	Enabled     bool                   `json:"enabled"`
	FileTypes   []string               `json:"file_types"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// ScanRuleType defines the type of scan rule
type ScanRuleType string

const (
	ScanRuleTypeRegex     ScanRuleType = "regex"
	ScanRuleTypeSignature ScanRuleType = "signature"
	ScanRuleTypeHash      ScanRuleType = "hash"
	ScanRuleTypeYara      ScanRuleType = "yara"
	ScanRuleTypeCustom    ScanRuleType = "custom"
)

// VulnerabilityDatabase represents a vulnerability database
type VulnerabilityDatabase interface {
	GetVulnerabilities(ctx context.Context) ([]VulnerabilityEntry, error)
	GetVulnerability(ctx context.Context, id string) (VulnerabilityEntry, error)
	SearchVulnerabilities(ctx context.Context, query VulnerabilityQuery) ([]VulnerabilityEntry, error)
	UpdateDatabase(ctx context.Context) error
	GetLastUpdated() time.Time
}

// VulnerabilityEntry represents an entry in the vulnerability database
type VulnerabilityEntry struct {
	ID          string                 `json:"id"`
	CVE         string                 `json:"cve"`
	CWE         string                 `json:"cwe"`
	Severity    Severity               `json:"severity"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	References  []string               `json:"references"`
	Signatures  []string               `json:"signatures"`
	Patterns    []string               `json:"patterns"`
	Metadata    map[string]interface{} `json:"metadata"`
	PublishedAt time.Time              `json:"published_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// VulnerabilityQuery represents a query for vulnerabilities
type VulnerabilityQuery struct {
	Severity  []Severity `json:"severity"`
	Type      []string   `json:"type"`
	Keywords  []string   `json:"keywords"`
	DateRange DateRange  `json:"date_range"`
	Limit     int        `json:"limit"`
	Offset    int        `json:"offset"`
}

// DateRange represents a date range for queries
type DateRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ScannerStats contains scanner statistics
type ScannerStats struct {
	TotalScans           int               `json:"total_scans"`
	CompletedScans       int               `json:"completed_scans"`
	FailedScans          int               `json:"failed_scans"`
	VulnerabilitiesFound int               `json:"vulnerabilities_found"`
	WarningsFound        int               `json:"warnings_found"`
	ScansByType          map[ScanType]int  `json:"scans_by_type"`
	ScansByRisk          map[RiskLevel]int `json:"scans_by_risk"`
	AverageScanTime      time.Duration     `json:"average_scan_time"`
	LastScanTime         time.Time         `json:"last_scan_time"`
	DatabaseLastUpdated  time.Time         `json:"database_last_updated"`
}

// SecurityScannerImpl implements the SecurityScanner interface
type SecurityScannerImpl struct {
	vulnDB      VulnerabilityDatabase
	scanRules   map[string]ScanRule
	scanHistory map[string][]ScanResult
	scanResults map[string]ScanResult
	stats       ScannerStats
	logger      common.Logger
	metrics     common.Metrics
	mu          sync.RWMutex
}

// NewSecurityScanner creates a new security scanner
func NewSecurityScanner(logger common.Logger, metrics common.Metrics) SecurityScanner {
	scanner := &SecurityScannerImpl{
		scanRules:   make(map[string]ScanRule),
		scanHistory: make(map[string][]ScanResult),
		scanResults: make(map[string]ScanResult),
		stats:       ScannerStats{},
		logger:      logger,
		metrics:     metrics,
	}

	// Initialize vulnerability database
	scanner.vulnDB = NewVulnerabilityDatabaseImpl(logger, metrics)

	// Load default scan rules
	scanner.loadDefaultScanRules()

	return scanner
}

// ScanPackage scans a plugin package for security vulnerabilities
func (s *SecurityScannerImpl) ScanPackage(ctx context.Context, pkg plugins.PluginEnginePackage) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scanID := s.generateScanID()
	result := ScanResult{
		ID:          scanID,
		PluginID:    pkg.Info.ID,
		ScanType:    ScanTypePackage,
		Status:      ScanStatusRunning,
		StartTime:   time.Now(),
		ScannerInfo: s.getScannerInfo(),
		Metadata:    make(map[string]interface{}),
	}

	s.scanResults[scanID] = result
	s.updateStats()

	s.logger.Info("starting package security scan",
		logger.String("scan_id", scanID),
		logger.String("plugin_id", pkg.Info.ID),
	)

	// Perform the scan
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("panic during security scan",
					logger.String("scan_id", scanID),
					logger.Any("panic", r),
				)
				s.markScanFailed(scanID, fmt.Errorf("scan panic: %v", r))
			}
		}()

		// Scan binary
		binaryVulns, binaryWarnings, err := s.scanBinaryInternal(ctx, pkg.Binary)
		if err != nil {
			s.markScanFailed(scanID, err)
			return
		}

		// Scan configuration
		configVulns, configWarnings, err := s.scanConfiguration(ctx, pkg.Config)
		if err != nil {
			s.logger.Warn("failed to scan configuration", logger.Error(err))
		}

		// Scan documentation
		docVulns, docWarnings, err := s.scanDocumentation(ctx, pkg.Docs)
		if err != nil {
			s.logger.Warn("failed to scan documentation", logger.Error(err))
		}

		// Combine results
		allVulns := append(binaryVulns, configVulns...)
		allVulns = append(allVulns, docVulns...)

		allWarnings := append(binaryWarnings, configWarnings...)
		allWarnings = append(allWarnings, docWarnings...)

		// Calculate risk level and score
		riskLevel, score := s.calculateRiskScore(allVulns, allWarnings)

		// Update result
		s.mu.Lock()
		result = s.scanResults[scanID]
		result.Status = ScanStatusCompleted
		result.RiskLevel = riskLevel
		result.Score = score
		result.Vulnerabilities = allVulns
		result.Warnings = allWarnings
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		s.scanResults[scanID] = result
		s.mu.Unlock()

		// Add to history
		s.addToHistory(pkg.Info.ID, result)

		s.logger.Info("package security scan completed",
			logger.String("scan_id", scanID),
			logger.String("plugin_id", pkg.Info.ID),
			logger.String("risk_level", string(riskLevel)),
			logger.Float64("score", score),
			logger.Int("vulnerabilities", len(allVulns)),
			logger.Int("warnings", len(allWarnings)),
		)
	}()

	return result, nil
}

// ScanBinary scans a binary for security vulnerabilities
func (s *SecurityScannerImpl) ScanBinary(ctx context.Context, binary []byte) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scanID := s.generateScanID()
	result := ScanResult{
		ID:          scanID,
		PluginID:    "binary-scan",
		ScanType:    ScanTypeBinary,
		Status:      ScanStatusRunning,
		StartTime:   time.Now(),
		ScannerInfo: s.getScannerInfo(),
		Metadata:    make(map[string]interface{}),
	}

	s.scanResults[scanID] = result

	vulns, warnings, err := s.scanBinaryInternal(ctx, binary)
	if err != nil {
		s.markScanFailed(scanID, err)
		return result, err
	}

	// Calculate risk level and score
	riskLevel, score := s.calculateRiskScore(vulns, warnings)

	// Update result
	result.Status = ScanStatusCompleted
	result.RiskLevel = riskLevel
	result.Score = score
	result.Vulnerabilities = vulns
	result.Warnings = warnings
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	s.scanResults[scanID] = result

	return result, nil
}

// ScanSource scans source code for security vulnerabilities
func (s *SecurityScannerImpl) ScanSource(ctx context.Context, sourcePath string) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scanID := s.generateScanID()
	result := ScanResult{
		ID:          scanID,
		PluginID:    "source-scan",
		ScanType:    ScanTypeSource,
		Status:      ScanStatusRunning,
		StartTime:   time.Now(),
		ScannerInfo: s.getScannerInfo(),
		Metadata:    make(map[string]interface{}),
	}

	s.scanResults[scanID] = result

	vulns, warnings, err := s.scanSourceCode(ctx, sourcePath)
	if err != nil {
		s.markScanFailed(scanID, err)
		return result, err
	}

	// Calculate risk level and score
	riskLevel, score := s.calculateRiskScore(vulns, warnings)

	// Update result
	result.Status = ScanStatusCompleted
	result.RiskLevel = riskLevel
	result.Score = score
	result.Vulnerabilities = vulns
	result.Warnings = warnings
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	s.scanResults[scanID] = result

	return result, nil
}

// UpdateVulnerabilityDatabase updates the vulnerability database
func (s *SecurityScannerImpl) UpdateVulnerabilityDatabase(ctx context.Context) error {
	s.logger.Info("updating vulnerability database")

	if err := s.vulnDB.UpdateDatabase(ctx); err != nil {
		return fmt.Errorf("failed to update vulnerability database: %w", err)
	}

	s.stats.DatabaseLastUpdated = time.Now()

	s.logger.Info("vulnerability database updated")
	return nil
}

// GetVulnerabilityDatabase returns the vulnerability database
func (s *SecurityScannerImpl) GetVulnerabilityDatabase() VulnerabilityDatabase {
	return s.vulnDB
}

// AddScanRule adds a custom scan rule
func (s *SecurityScannerImpl) AddScanRule(rule ScanRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.scanRules[rule.ID]; exists {
		return fmt.Errorf("scan rule %s already exists", rule.ID)
	}

	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	s.scanRules[rule.ID] = rule

	s.logger.Info("scan rule added",
		logger.String("rule_id", rule.ID),
		logger.String("name", rule.Name),
		logger.String("type", string(rule.Type)),
	)

	return nil
}

// RemoveScanRule removes a scan rule
func (s *SecurityScannerImpl) RemoveScanRule(ruleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.scanRules[ruleID]; !exists {
		return fmt.Errorf("scan rule %s not found", ruleID)
	}

	delete(s.scanRules, ruleID)

	s.logger.Info("scan rule removed",
		logger.String("rule_id", ruleID),
	)

	return nil
}

// GetScanRules returns all scan rules
func (s *SecurityScannerImpl) GetScanRules() []ScanRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := make([]ScanRule, 0, len(s.scanRules))
	for _, rule := range s.scanRules {
		rules = append(rules, rule)
	}

	return rules
}

// GetScanHistory returns scan history for a plugin
func (s *SecurityScannerImpl) GetScanHistory(pluginID string) ([]ScanResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history, exists := s.scanHistory[pluginID]
	if !exists {
		return []ScanResult{}, nil
	}

	return history, nil
}

// GetScanResult returns a scan result by ID
func (s *SecurityScannerImpl) GetScanResult(scanID string) (ScanResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result, exists := s.scanResults[scanID]
	if !exists {
		return ScanResult{}, fmt.Errorf("scan result %s not found", scanID)
	}

	return result, nil
}

// GetScannerStats returns scanner statistics
func (s *SecurityScannerImpl) GetScannerStats() ScannerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.updateStats()
	return s.stats
}

// Helper methods

func (s *SecurityScannerImpl) scanBinaryInternal(ctx context.Context, binary []byte) ([]Vulnerability, []SecurityWarning, error) {
	var vulnerabilities []Vulnerability
	var warnings []SecurityWarning

	// Calculate hash
	hash := sha256.Sum256(binary)
	hashStr := hex.EncodeToString(hash[:])

	// Check for known malicious hashes
	if s.isKnownMalicious(hashStr) {
		vulnerabilities = append(vulnerabilities, Vulnerability{
			ID:          s.generateVulnID(),
			Type:        VulnerabilityTypeMaliciousCode,
			Severity:    SeverityCritical,
			Title:       "Known Malicious Binary",
			Description: "Binary matches known malicious file signature",
			Location:    Location{File: "binary"},
			Metadata:    map[string]interface{}{"hash": hashStr},
		})
	}

	// Scan for signatures
	sigVulns, sigWarnings := s.scanSignatures(binary)
	vulnerabilities = append(vulnerabilities, sigVulns...)
	warnings = append(warnings, sigWarnings...)

	// Scan for patterns
	patternVulns, patternWarnings := s.scanPatterns(binary)
	vulnerabilities = append(vulnerabilities, patternVulns...)
	warnings = append(warnings, patternWarnings...)

	return vulnerabilities, warnings, nil
}

func (s *SecurityScannerImpl) scanConfiguration(ctx context.Context, config []byte) ([]Vulnerability, []SecurityWarning, error) {
	var vulnerabilities []Vulnerability
	var warnings []SecurityWarning

	if len(config) == 0 {
		return vulnerabilities, warnings, nil
	}

	// Parse configuration
	var configData map[string]interface{}
	if err := json.Unmarshal(config, &configData); err != nil {
		return vulnerabilities, warnings, fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Check for insecure configurations
	if s.hasInsecureConfig(configData) {
		warnings = append(warnings, SecurityWarning{
			ID:          s.generateWarningID(),
			Type:        WarningTypeWeakConfiguration,
			Severity:    SeverityMedium,
			Title:       "Insecure Configuration",
			Description: "Configuration contains potentially insecure settings",
			Location:    Location{File: "config.json"},
			Suggestion:  "Review and harden configuration settings",
		})
	}

	// Check for hardcoded secrets
	if s.hasHardcodedSecrets(configData) {
		vulnerabilities = append(vulnerabilities, Vulnerability{
			ID:          s.generateVulnID(),
			Type:        VulnerabilityTypeInformationDisclosure,
			Severity:    SeverityHigh,
			Title:       "Hardcoded Secrets",
			Description: "Configuration contains hardcoded secrets or credentials",
			Location:    Location{File: "config.json"},
		})
	}

	return vulnerabilities, warnings, nil
}

func (s *SecurityScannerImpl) scanDocumentation(ctx context.Context, docs []byte) ([]Vulnerability, []SecurityWarning, error) {
	var vulnerabilities []Vulnerability
	var warnings []SecurityWarning

	if len(docs) == 0 {
		return vulnerabilities, warnings, nil
	}

	// Simple text-based scanning for documentation
	docText := string(docs)

	// Check for sensitive information in documentation
	if s.containsSensitiveInfo(docText) {
		warnings = append(warnings, SecurityWarning{
			ID:          s.generateWarningID(),
			Type:        WarningTypeInformationDisclosure,
			Severity:    SeverityLow,
			Title:       "Sensitive Information in Documentation",
			Description: "Documentation may contain sensitive information",
			Location:    Location{File: "documentation"},
			Suggestion:  "Review documentation for sensitive information",
		})
	}

	return vulnerabilities, warnings, nil
}

func (s *SecurityScannerImpl) scanSourceCode(ctx context.Context, sourcePath string) ([]Vulnerability, []SecurityWarning, error) {
	var vulnerabilities []Vulnerability
	var warnings []SecurityWarning

	// Walk through source files
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Skip non-source files
		if !s.isSourceFile(path) {
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Scan file content
		fileVulns, fileWarnings := s.scanFileContent(path, content)
		vulnerabilities = append(vulnerabilities, fileVulns...)
		warnings = append(warnings, fileWarnings...)

		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to walk source directory: %w", err)
	}

	return vulnerabilities, warnings, nil
}

func (s *SecurityScannerImpl) scanSignatures(binary []byte) ([]Vulnerability, []SecurityWarning) {
	var vulnerabilities []Vulnerability
	var warnings []SecurityWarning

	// Scan for known vulnerability signatures
	for _, rule := range s.scanRules {
		if !rule.Enabled || rule.Type != ScanRuleTypeSignature {
			continue
		}

		if s.matchesSignature(binary, rule.Pattern) {
			vulnerability := Vulnerability{
				ID:          s.generateVulnID(),
				Type:        VulnerabilityTypeMaliciousCode,
				Severity:    rule.Severity,
				Title:       rule.Name,
				Description: rule.Description,
				Location:    Location{File: "binary"},
				Metadata:    map[string]interface{}{"rule_id": rule.ID},
			}

			if rule.Severity == SeverityCritical || rule.Severity == SeverityHigh {
				vulnerabilities = append(vulnerabilities, vulnerability)
			} else {
				warnings = append(warnings, SecurityWarning{
					ID:          s.generateWarningID(),
					Type:        WarningTypeInsecurePractice,
					Severity:    rule.Severity,
					Title:       rule.Name,
					Description: rule.Description,
					Location:    Location{File: "binary"},
				})
			}
		}
	}

	return vulnerabilities, warnings
}

func (s *SecurityScannerImpl) scanPatterns(binary []byte) ([]Vulnerability, []SecurityWarning) {
	var vulnerabilities []Vulnerability
	var warnings []SecurityWarning

	// Scan for regex patterns
	for _, rule := range s.scanRules {
		if !rule.Enabled || rule.Type != ScanRuleTypeRegex {
			continue
		}

		regex, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}

		if regex.Match(binary) {
			vulnerability := Vulnerability{
				ID:          s.generateVulnID(),
				Type:        VulnerabilityTypeMaliciousCode,
				Severity:    rule.Severity,
				Title:       rule.Name,
				Description: rule.Description,
				Location:    Location{File: "binary"},
				Metadata:    map[string]interface{}{"rule_id": rule.ID},
			}

			if rule.Severity == SeverityCritical || rule.Severity == SeverityHigh {
				vulnerabilities = append(vulnerabilities, vulnerability)
			} else {
				warnings = append(warnings, SecurityWarning{
					ID:          s.generateWarningID(),
					Type:        WarningTypeInsecurePractice,
					Severity:    rule.Severity,
					Title:       rule.Name,
					Description: rule.Description,
					Location:    Location{File: "binary"},
				})
			}
		}
	}

	return vulnerabilities, warnings
}

func (s *SecurityScannerImpl) scanFileContent(path string, content []byte) ([]Vulnerability, []SecurityWarning) {
	var vulnerabilities []Vulnerability
	var warnings []SecurityWarning

	contentStr := string(content)

	// Check for common security issues
	if s.containsSQLInjection(contentStr) {
		vulnerabilities = append(vulnerabilities, Vulnerability{
			ID:          s.generateVulnID(),
			Type:        VulnerabilityTypeSQLInjection,
			Severity:    SeverityHigh,
			Title:       "Potential SQL Injection",
			Description: "Code may be vulnerable to SQL injection attacks",
			Location:    Location{File: path},
		})
	}

	if s.containsXSS(contentStr) {
		vulnerabilities = append(vulnerabilities, Vulnerability{
			ID:          s.generateVulnID(),
			Type:        VulnerabilityTypeXSS,
			Severity:    SeverityMedium,
			Title:       "Potential XSS Vulnerability",
			Description: "Code may be vulnerable to cross-site scripting attacks",
			Location:    Location{File: path},
		})
	}

	if s.containsHardcodedCredentials(contentStr) {
		vulnerabilities = append(vulnerabilities, Vulnerability{
			ID:          s.generateVulnID(),
			Type:        VulnerabilityTypeInformationDisclosure,
			Severity:    SeverityHigh,
			Title:       "Hardcoded Credentials",
			Description: "Source code contains hardcoded credentials",
			Location:    Location{File: path},
		})
	}

	return vulnerabilities, warnings
}

func (s *SecurityScannerImpl) calculateRiskScore(vulnerabilities []Vulnerability, warnings []SecurityWarning) (RiskLevel, float64) {
	var score float64

	// Calculate score based on vulnerabilities
	for _, vuln := range vulnerabilities {
		switch vuln.Severity {
		case SeverityCritical:
			score += 10.0
		case SeverityHigh:
			score += 7.0
		case SeverityMedium:
			score += 4.0
		case SeverityLow:
			score += 1.0
		}
	}

	// Add warnings to score
	for _, warning := range warnings {
		switch warning.Severity {
		case SeverityCritical:
			score += 5.0
		case SeverityHigh:
			score += 3.0
		case SeverityMedium:
			score += 2.0
		case SeverityLow:
			score += 0.5
		}
	}

	// Determine risk level
	var riskLevel RiskLevel
	if score >= 30.0 {
		riskLevel = RiskLevelCritical
	} else if score >= 20.0 {
		riskLevel = RiskLevelHigh
	} else if score >= 10.0 {
		riskLevel = RiskLevelMedium
	} else if score >= 5.0 {
		riskLevel = RiskLevelLow
	} else {
		riskLevel = RiskLevelInfo
	}

	return riskLevel, score
}

func (s *SecurityScannerImpl) loadDefaultScanRules() {
	// Load default security scanning rules
	defaultRules := []ScanRule{
		{
			ID:          "hardcoded-password",
			Name:        "Hardcoded Password",
			Description: "Detects hardcoded passwords in source code",
			Type:        ScanRuleTypeRegex,
			Pattern:     `(?i)(password|pwd|passwd)\s*[:=]\s*["'][^"']{3,}["']`,
			Severity:    SeverityHigh,
			Category:    "credentials",
			Enabled:     true,
			FileTypes:   []string{".go", ".js", ".py", ".java", ".cpp", ".c"},
		},
		{
			ID:          "sql-injection",
			Name:        "SQL Injection",
			Description: "Detects potential SQL injection vulnerabilities",
			Type:        ScanRuleTypeRegex,
			Pattern:     `(?i)(select|insert|update|delete|drop|create|alter)\s+.*\+.*["']`,
			Severity:    SeverityHigh,
			Category:    "injection",
			Enabled:     true,
			FileTypes:   []string{".go", ".js", ".py", ".java", ".php"},
		},
		{
			ID:          "xss-vulnerability",
			Name:        "XSS Vulnerability",
			Description: "Detects potential cross-site scripting vulnerabilities",
			Type:        ScanRuleTypeRegex,
			Pattern:     `(?i)(document\.write|innerHTML|outerHTML)\s*=.*\+`,
			Severity:    SeverityMedium,
			Category:    "xss",
			Enabled:     true,
			FileTypes:   []string{".js", ".html", ".php", ".jsp"},
		},
		{
			ID:          "weak-crypto",
			Name:        "Weak Cryptography",
			Description: "Detects use of weak cryptographic algorithms",
			Type:        ScanRuleTypeRegex,
			Pattern:     `(?i)(md5|sha1|des|rc4|md4)`,
			Severity:    SeverityMedium,
			Category:    "crypto",
			Enabled:     true,
			FileTypes:   []string{".go", ".js", ".py", ".java", ".cpp", ".c"},
		},
	}

	for _, rule := range defaultRules {
		rule.CreatedAt = time.Now()
		rule.UpdatedAt = time.Now()
		s.scanRules[rule.ID] = rule
	}
}

func (s *SecurityScannerImpl) generateScanID() string {
	return fmt.Sprintf("scan_%d", time.Now().UnixNano())
}

func (s *SecurityScannerImpl) generateVulnID() string {
	return fmt.Sprintf("vuln_%d", time.Now().UnixNano())
}

func (s *SecurityScannerImpl) generateWarningID() string {
	return fmt.Sprintf("warn_%d", time.Now().UnixNano())
}

func (s *SecurityScannerImpl) getScannerInfo() ScannerInfo {
	return ScannerInfo{
		Name:     "Forge Security Scanner",
		Version:  "1.0.0",
		Database: "forge-vuln-db",
		Updated:  s.vulnDB.GetLastUpdated(),
	}
}

func (s *SecurityScannerImpl) markScanFailed(scanID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if result, exists := s.scanResults[scanID]; exists {
		result.Status = ScanStatusFailed
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.Metadata["error"] = err.Error()
		s.scanResults[scanID] = result
	}

	s.updateStats()
}

func (s *SecurityScannerImpl) addToHistory(pluginID string, result ScanResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scanHistory[pluginID] = append(s.scanHistory[pluginID], result)

	// Limit history size
	if len(s.scanHistory[pluginID]) > 100 {
		s.scanHistory[pluginID] = s.scanHistory[pluginID][1:]
	}
}

func (s *SecurityScannerImpl) updateStats() {
	s.stats.TotalScans = len(s.scanResults)
	s.stats.CompletedScans = 0
	s.stats.FailedScans = 0
	s.stats.VulnerabilitiesFound = 0
	s.stats.WarningsFound = 0
	s.stats.ScansByType = make(map[ScanType]int)
	s.stats.ScansByRisk = make(map[RiskLevel]int)

	var totalDuration time.Duration
	completedCount := 0

	for _, result := range s.scanResults {
		s.stats.ScansByType[result.ScanType]++

		if result.Status == ScanStatusCompleted {
			s.stats.CompletedScans++
			s.stats.ScansByRisk[result.RiskLevel]++
			s.stats.VulnerabilitiesFound += len(result.Vulnerabilities)
			s.stats.WarningsFound += len(result.Warnings)

			if result.Duration > 0 {
				totalDuration += result.Duration
				completedCount++
			}

			if result.EndTime.After(s.stats.LastScanTime) {
				s.stats.LastScanTime = result.EndTime
			}
		} else if result.Status == ScanStatusFailed {
			s.stats.FailedScans++
		}
	}

	if completedCount > 0 {
		s.stats.AverageScanTime = totalDuration / time.Duration(completedCount)
	}
}

// Helper functions for vulnerability detection

func (s *SecurityScannerImpl) isKnownMalicious(hash string) bool {
	// Check against known malicious file database
	// This is a simplified implementation
	return false
}

func (s *SecurityScannerImpl) matchesSignature(binary []byte, signature string) bool {
	// Simple signature matching
	return bytes.Contains(binary, []byte(signature))
}

func (s *SecurityScannerImpl) hasInsecureConfig(config map[string]interface{}) bool {
	// Check for insecure configuration patterns
	insecureKeys := []string{"debug", "unsafe", "disable_ssl", "allow_all"}

	for _, key := range insecureKeys {
		if val, exists := config[key]; exists {
			if boolVal, ok := val.(bool); ok && boolVal {
				return true
			}
		}
	}

	return false
}

func (s *SecurityScannerImpl) hasHardcodedSecrets(config map[string]interface{}) bool {
	// Check for hardcoded secrets in configuration
	secretKeys := []string{"password", "secret", "token", "key", "api_key"}

	for _, key := range secretKeys {
		if val, exists := config[key]; exists {
			if strVal, ok := val.(string); ok && len(strVal) > 0 {
				return true
			}
		}
	}

	return false
}

func (s *SecurityScannerImpl) containsSensitiveInfo(text string) bool {
	// Check for sensitive information patterns
	sensitivePatterns := []string{
		"password", "secret", "token", "key", "credential",
		"private", "confidential", "internal", "proprietary",
	}

	lowerText := strings.ToLower(text)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lowerText, pattern) {
			return true
		}
	}

	return false
}

func (s *SecurityScannerImpl) isSourceFile(path string) bool {
	sourceExtensions := []string{".go", ".js", ".py", ".java", ".cpp", ".c", ".cs", ".php", ".rb", ".rs"}

	ext := strings.ToLower(filepath.Ext(path))
	for _, srcExt := range sourceExtensions {
		if ext == srcExt {
			return true
		}
	}

	return false
}

func (s *SecurityScannerImpl) containsSQLInjection(code string) bool {
	// Simple SQL injection detection
	patterns := []string{
		`(?i)(select|insert|update|delete|drop|create|alter)\s+.*\+.*["']`,
		`(?i)query\s*\+\s*["']`,
		`(?i)execute\s*\(\s*["'].*\+`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, code); matched {
			return true
		}
	}

	return false
}

func (s *SecurityScannerImpl) containsXSS(code string) bool {
	// Simple XSS detection
	patterns := []string{
		`(?i)(document\.write|innerHTML|outerHTML)\s*=.*\+`,
		`(?i)eval\s*\(\s*.*\+`,
		`(?i)setTimeout\s*\(\s*["'].*\+`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, code); matched {
			return true
		}
	}

	return false
}

func (s *SecurityScannerImpl) containsHardcodedCredentials(code string) bool {
	// Simple hardcoded credentials detection
	patterns := []string{
		`(?i)(password|pwd|passwd|secret|token|key)\s*[:=]\s*["'][^"']{3,}["']`,
		`(?i)(api_key|apikey|access_token)\s*[:=]\s*["'][^"']{10,}["']`,
		`(?i)(username|user)\s*[:=]\s*["'][^"']{3,}["'].*\n.*(?i)(password|pwd)\s*[:=]\s*["'][^"']{3,}["']`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, code); matched {
			return true
		}
	}

	return false
}

// VulnerabilityDatabaseImpl implements the VulnerabilityDatabase interface
type VulnerabilityDatabaseImpl struct {
	vulnerabilities map[string]VulnerabilityEntry
	lastUpdated     time.Time
	logger          common.Logger
	metrics         common.Metrics
	mu              sync.RWMutex
}

// NewVulnerabilityDatabaseImpl creates a new vulnerability database implementation
func NewVulnerabilityDatabaseImpl(logger common.Logger, metrics common.Metrics) *VulnerabilityDatabaseImpl {
	return &VulnerabilityDatabaseImpl{
		vulnerabilities: make(map[string]VulnerabilityEntry),
		logger:          logger,
		metrics:         metrics,
	}
}

// GetVulnerabilities returns all vulnerabilities
func (vdb *VulnerabilityDatabaseImpl) GetVulnerabilities(ctx context.Context) ([]VulnerabilityEntry, error) {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	vulnerabilities := make([]VulnerabilityEntry, 0, len(vdb.vulnerabilities))
	for _, vuln := range vdb.vulnerabilities {
		vulnerabilities = append(vulnerabilities, vuln)
	}

	return vulnerabilities, nil
}

// GetVulnerability returns a specific vulnerability
func (vdb *VulnerabilityDatabaseImpl) GetVulnerability(ctx context.Context, id string) (VulnerabilityEntry, error) {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	vuln, exists := vdb.vulnerabilities[id]
	if !exists {
		return VulnerabilityEntry{}, fmt.Errorf("vulnerability %s not found", id)
	}

	return vuln, nil
}

// SearchVulnerabilities searches for vulnerabilities
func (vdb *VulnerabilityDatabaseImpl) SearchVulnerabilities(ctx context.Context, query VulnerabilityQuery) ([]VulnerabilityEntry, error) {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	var results []VulnerabilityEntry
	count := 0

	for _, vuln := range vdb.vulnerabilities {
		if count >= query.Limit && query.Limit > 0 {
			break
		}

		if vdb.matchesQuery(vuln, query) {
			if count >= query.Offset {
				results = append(results, vuln)
			}
			count++
		}
	}

	return results, nil
}

// UpdateDatabase updates the vulnerability database
func (vdb *VulnerabilityDatabaseImpl) UpdateDatabase(ctx context.Context) error {
	vdb.mu.Lock()
	defer vdb.mu.Unlock()

	// This would typically fetch from external sources like CVE databases
	// For now, we'll just update the timestamp
	vdb.lastUpdated = time.Now()

	return nil
}

// GetLastUpdated returns the last update time
func (vdb *VulnerabilityDatabaseImpl) GetLastUpdated() time.Time {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()
	return vdb.lastUpdated
}

func (vdb *VulnerabilityDatabaseImpl) matchesQuery(vuln VulnerabilityEntry, query VulnerabilityQuery) bool {
	// Check severity filter
	if len(query.Severity) > 0 {
		found := false
		for _, severity := range query.Severity {
			if vuln.Severity == severity {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check keywords
	if len(query.Keywords) > 0 {
		lowerTitle := strings.ToLower(vuln.Title)
		lowerDesc := strings.ToLower(vuln.Description)

		for _, keyword := range query.Keywords {
			lowerKeyword := strings.ToLower(keyword)
			if strings.Contains(lowerTitle, lowerKeyword) || strings.Contains(lowerDesc, lowerKeyword) {
				return true
			}
		}
		return false
	}

	// Check date range
	if !query.DateRange.Start.IsZero() && vuln.PublishedAt.Before(query.DateRange.Start) {
		return false
	}
	if !query.DateRange.End.IsZero() && vuln.PublishedAt.After(query.DateRange.End) {
		return false
	}

	return true
}
