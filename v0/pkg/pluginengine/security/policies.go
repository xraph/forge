package security

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/pluginengine/common"
)

// PolicyEnforcer enforces security policies for plugins
type PolicyEnforcer interface {
	// Policy enforcement
	EnforcePolicy(ctx context.Context, pluginID string, operation PolicyOperation) error
	ValidateOperation(ctx context.Context, pluginID string, operation PolicyOperation) (ValidationResult, error)

	// Policy management
	LoadPolicies(ctx context.Context, policySource string) error
	UpdatePolicy(ctx context.Context, policy SecurityPolicy) error
	DeletePolicy(ctx context.Context, policyID string) error
	GetPolicy(ctx context.Context, policyID string) (SecurityPolicy, error)
	ListPolicies() []SecurityPolicy

	// Compliance and auditing
	CheckCompliance(ctx context.Context, pluginID string) (ComplianceReport, error)
	GetViolations(ctx context.Context, pluginID string) ([]PolicyViolation, error)
	GenerateComplianceReport(ctx context.Context) (GlobalComplianceReport, error)

	// Policy templates
	GetPolicyTemplates() []PolicyTemplate
	CreatePolicyFromTemplate(ctx context.Context, templateID string, config map[string]interface{}) (SecurityPolicy, error)

	// Statistics
	GetPolicyStats() PolicyStats
}

// SecurityPolicy represents a security policy
type SecurityPolicy struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Version     string                 `json:"version"`
	Category    PolicyCategory         `json:"category"`
	Severity    PolicySeverity         `json:"severity"`
	Rules       []PolicyRule           `json:"rules"`
	Conditions  []PolicyCondition      `json:"conditions"`
	Actions     []PolicyAction         `json:"actions"`
	Scope       PolicyScope            `json:"scope"`
	Enabled     bool                   `json:"enabled"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CreatedBy   string                 `json:"created_by"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// PolicyCategory defines policy categories
type PolicyCategory string

const (
	PolicyCategoryAccess     PolicyCategory = "access"
	PolicyCategoryResource   PolicyCategory = "resource"
	PolicyCategoryNetwork    PolicyCategory = "network"
	PolicyCategoryData       PolicyCategory = "data"
	PolicyCategoryCrypto     PolicyCategory = "crypto"
	PolicyCategoryCompliance PolicyCategory = "compliance"
	PolicyCategoryGeneral    PolicyCategory = "general"
)

// PolicySeverity defines policy severity levels
type PolicySeverity string

const (
	PolicySeverityInfo     PolicySeverity = "info"
	PolicySeverityLow      PolicySeverity = "low"
	PolicySeverityMedium   PolicySeverity = "medium"
	PolicySeverityHigh     PolicySeverity = "high"
	PolicySeverityCritical PolicySeverity = "critical"
)

// PermissionPolicyRule defines a specific policy rule
type PolicyRule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        RuleType               `json:"type"`
	Conditions  []RuleCondition        `json:"conditions"`
	Effect      RuleEffect             `json:"effect"`
	Priority    int                    `json:"priority"`
	Enabled     bool                   `json:"enabled"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// RuleType defines types of policy rules
type RuleType string

const (
	RuleTypeAllow     RuleType = "allow"
	RuleTypeDeny      RuleType = "deny"
	RuleTypeLimit     RuleType = "limit"
	RuleTypeMonitor   RuleType = "monitor"
	RuleTypeTransform RuleType = "transform"
	RuleTypeValidate  RuleType = "validate"
)

// RuleCondition defines conditions for rule evaluation
type RuleCondition struct {
	Field         string                 `json:"field"`
	Operator      ConditionOperator      `json:"operator"`
	Value         interface{}            `json:"value"`
	Negated       bool                   `json:"negated"`
	CaseSensitive bool                   `json:"case_sensitive"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// ConditionOperator defines condition operators
type ConditionOperator string

const (
	OperatorEquals       ConditionOperator = "eq"
	OperatorNotEquals    ConditionOperator = "ne"
	OperatorGreater      ConditionOperator = "gt"
	OperatorLess         ConditionOperator = "lt"
	OperatorGreaterEqual ConditionOperator = "gte"
	OperatorLessEqual    ConditionOperator = "lte"
	OperatorContains     ConditionOperator = "contains"
	OperatorStartsWith   ConditionOperator = "starts_with"
	OperatorEndsWith     ConditionOperator = "ends_with"
	OperatorRegex        ConditionOperator = "regex"
	OperatorIn           ConditionOperator = "in"
	OperatorNotIn        ConditionOperator = "not_in"
)

// RuleEffect defines the effect of a rule
type RuleEffect string

const (
	EffectAllow    RuleEffect = "allow"
	EffectDeny     RuleEffect = "deny"
	EffectWarn     RuleEffect = "warn"
	EffectLog      RuleEffect = "log"
	EffectThrottle RuleEffect = "throttle"
	EffectBlock    RuleEffect = "block"
)

// PolicyCondition defines global policy conditions
type PolicyCondition struct {
	Type     ConditionType          `json:"type"`
	Config   map[string]interface{} `json:"config"`
	Enabled  bool                   `json:"enabled"`
	Metadata map[string]interface{} `json:"metadata"`
}

// ConditionType defines types of policy conditions
type ConditionType string

const (
	ConditionTypeTime        ConditionType = "time"
	ConditionTypeLocation    ConditionType = "location"
	ConditionTypeUser        ConditionType = "user"
	ConditionTypePlugin      ConditionType = "plugin"
	ConditionTypeResource    ConditionType = "resource"
	ConditionTypeEnvironment ConditionType = "environment"
)

// PolicyAction defines actions to take when policy is triggered
type PolicyAction struct {
	Type     ActionType             `json:"type"`
	Config   map[string]interface{} `json:"config"`
	Enabled  bool                   `json:"enabled"`
	Priority int                    `json:"priority"`
	Metadata map[string]interface{} `json:"metadata"`
}

// ActionType defines types of policy actions
type ActionType string

const (
	ActionTypeLog        ActionType = "log"
	ActionTypeAlert      ActionType = "alert"
	ActionTypeBlock      ActionType = "block"
	ActionTypeThrottle   ActionType = "throttle"
	ActionTypeTerminate  ActionType = "terminate"
	ActionTypeQuarantine ActionType = "quarantine"
	ActionTypeNotify     ActionType = "notify"
)

// PolicyScope defines the scope of policy application
type PolicyScope struct {
	PluginTypes []plugins.PluginEngineType `json:"plugin_types"`
	PluginIDs   []string                   `json:"plugin_ids"`
	Operations  []string                   `json:"operations"`
	Resources   []string                   `json:"resources"`
	Tags        []string                   `json:"tags"`
	Metadata    map[string]interface{}     `json:"metadata"`
}

// PolicyOperation represents an operation subject to policy enforcement
type PolicyOperation struct {
	Type       OperationType          `json:"type"`
	PluginID   string                 `json:"plugin_id"`
	Resource   string                 `json:"resource"`
	Action     string                 `json:"action"`
	Parameters map[string]interface{} `json:"parameters"`
	Context    map[string]interface{} `json:"context"`
	Timestamp  time.Time              `json:"timestamp"`
	RequestID  string                 `json:"request_id"`
	UserID     string                 `json:"user_id"`
	IP         string                 `json:"ip"`
	UserAgent  string                 `json:"user_agent"`
}

// OperationType defines types of operations
type OperationType string

const (
	OperationTypeLoad       OperationType = "load"
	OperationTypeStart      OperationType = "start"
	OperationTypeStop       OperationType = "stop"
	OperationTypeExecute    OperationType = "execute"
	OperationTypeRead       OperationType = "read"
	OperationTypeWrite      OperationType = "write"
	OperationTypeDelete     OperationType = "delete"
	OperationTypeNetwork    OperationType = "network"
	OperationTypeFileSystem OperationType = "filesystem"
)

// ValidationResult represents the result of operation validation
type ValidationResult struct {
	Valid           bool                   `json:"valid"`
	Violations      []PolicyViolation      `json:"violations"`
	Warnings        []PolicyWarning        `json:"warnings"`
	AppliedPolicies []string               `json:"applied_policies"`
	Decision        PolicyDecision         `json:"decision"`
	Reason          string                 `json:"reason"`
	Recommendations []string               `json:"recommendations"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// PolicyViolation represents a policy violation
type PolicyViolation struct {
	ID         string                 `json:"id"`
	PolicyID   string                 `json:"policy_id"`
	RuleID     string                 `json:"rule_id"`
	PluginID   string                 `json:"plugin_id"`
	Operation  PolicyOperation        `json:"operation"`
	Severity   ViolationSeverity      `json:"severity"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details"`
	Timestamp  time.Time              `json:"timestamp"`
	Resolved   bool                   `json:"resolved"`
	ResolvedAt *time.Time             `json:"resolved_at,omitempty"`
	ResolvedBy string                 `json:"resolved_by,omitempty"`
	Actions    []string               `json:"actions"`
}

// PolicyWarning represents a policy warning
type PolicyWarning struct {
	PolicyID  string                 `json:"policy_id"`
	RuleID    string                 `json:"rule_id"`
	Message   string                 `json:"message"`
	Severity  WarningSeverity        `json:"severity"`
	Details   map[string]interface{} `json:"details"`
	Timestamp time.Time              `json:"timestamp"`
}

// // ViolationSeverity defines violation severity levels
// type ViolationSeverity string
//
// const (
// 	ViolationSeverityLow      ViolationSeverity = "low"
// 	ViolationSeverityMedium   ViolationSeverity = "medium"
// 	ViolationSeverityHigh     ViolationSeverity = "high"
// 	ViolationSeverityCritical ViolationSeverity = "critical"
// )

// WarningSeverity defines warning severity levels
type WarningSeverity string

const (
	WarningSeverityInfo   WarningSeverity = "info"
	WarningSeverityLow    WarningSeverity = "low"
	WarningSeverityMedium WarningSeverity = "medium"
	WarningSeverityHigh   WarningSeverity = "high"
)

// ComplianceReport represents compliance status for a plugin
type ComplianceReport struct {
	PluginID        string             `json:"plugin_id"`
	OverallScore    float64            `json:"overall_score"`
	Status          ComplianceStatus   `json:"status"`
	Categories      map[string]float64 `json:"categories"`
	Violations      []PolicyViolation  `json:"violations"`
	Recommendations []string           `json:"recommendations"`
	LastChecked     time.Time          `json:"last_checked"`
	ValidUntil      time.Time          `json:"valid_until"`
}

// GlobalComplianceReport represents overall compliance status
type GlobalComplianceReport struct {
	OverallScore    float64                     `json:"overall_score"`
	Status          ComplianceStatus            `json:"status"`
	PluginReports   map[string]ComplianceReport `json:"plugin_reports"`
	CategoryScores  map[PolicyCategory]float64  `json:"category_scores"`
	TopViolations   []PolicyViolation           `json:"top_violations"`
	TrendData       []ComplianceTrend           `json:"trend_data"`
	Recommendations []string                    `json:"recommendations"`
	GeneratedAt     time.Time                   `json:"generated_at"`
}

// ComplianceStatus defines compliance status levels
type ComplianceStatus string

const (
	ComplianceStatusCompliant    ComplianceStatus = "compliant"
	ComplianceStatusPartial      ComplianceStatus = "partial"
	ComplianceStatusNonCompliant ComplianceStatus = "non_compliant"
	ComplianceStatusUnknown      ComplianceStatus = "unknown"
)

// ComplianceTrend represents compliance trend data
type ComplianceTrend struct {
	Date  time.Time `json:"date"`
	Score float64   `json:"score"`
}

// PolicyTemplate represents a policy template
type PolicyTemplate struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Category    PolicyCategory         `json:"category"`
	Template    SecurityPolicy         `json:"template"`
	Parameters  []TemplateParameter    `json:"parameters"`
	Examples    []TemplateExample      `json:"examples"`
	Tags        []string               `json:"tags"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Version     string                 `json:"version"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// TemplateParameter defines configurable parameters for templates
type TemplateParameter struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Required    bool                   `json:"required"`
	Default     interface{}            `json:"default"`
	Options     []interface{}          `json:"options,omitempty"`
	Validation  map[string]interface{} `json:"validation,omitempty"`
}

// TemplateExample provides usage examples for templates
type TemplateExample struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Config      map[string]interface{} `json:"config"`
	Expected    SecurityPolicy         `json:"expected"`
}

// PolicyStats contains policy enforcement statistics
type PolicyStats struct {
	TotalPolicies        int                       `json:"total_policies"`
	ActivePolicies       int                       `json:"active_policies"`
	TotalViolations      int                       `json:"total_violations"`
	RecentViolations     int                       `json:"recent_violations"`
	ComplianceScore      float64                   `json:"compliance_score"`
	ViolationsByCategory map[PolicyCategory]int    `json:"violations_by_category"`
	ViolationsBySeverity map[ViolationSeverity]int `json:"violations_by_severity"`
	PolicyByCategory     map[PolicyCategory]int    `json:"policy_by_category"`
	EnforcementRate      float64                   `json:"enforcement_rate"`
	LastUpdated          time.Time                 `json:"last_updated"`
}

// PolicyEnforcerImpl implements the PolicyEnforcer interface
type PolicyEnforcerImpl struct {
	policies      map[string]SecurityPolicy
	violations    []PolicyViolation
	templates     map[string]PolicyTemplate
	stats         PolicyStats
	permissionMgr PermissionManager
	logger        common.Logger
	metrics       common.Metrics
	mu            sync.RWMutex
}

// NewPolicyEnforcer creates a new policy enforcer
func NewPolicyEnforcer(permissionMgr PermissionManager, logger common.Logger, metrics common.Metrics) PolicyEnforcer {
	enforcer := &PolicyEnforcerImpl{
		policies:      make(map[string]SecurityPolicy),
		violations:    make([]PolicyViolation, 0),
		templates:     make(map[string]PolicyTemplate),
		stats:         PolicyStats{},
		permissionMgr: permissionMgr,
		logger:        logger,
		metrics:       metrics,
	}

	// Load default policies and templates
	enforcer.loadDefaultPolicies()
	enforcer.loadDefaultTemplates()

	return enforcer
}

// EnforcePolicy enforces policies for a plugin operation
func (pe *PolicyEnforcerImpl) EnforcePolicy(ctx context.Context, pluginID string, operation PolicyOperation) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	// Validate the operation first
	result, err := pe.validateOperationInternal(ctx, pluginID, operation)
	if err != nil {
		return fmt.Errorf("failed to validate operation: %w", err)
	}

	// Handle violations
	if !result.Valid {
		for _, violation := range result.Violations {
			pe.handleViolation(ctx, violation)
		}

		if result.Decision == PolicyDecisionDeny {
			return fmt.Errorf("operation denied by security policy: %s", result.Reason)
		}
	}

	// Log enforcement
	pe.logger.Info("policy enforced",
		logger.String("plugin_id", pluginID),
		logger.String("operation", string(operation.Type)),
		logger.String("decision", string(result.Decision)),
		logger.Int("violations", len(result.Violations)),
		logger.Int("warnings", len(result.Warnings)),
	)

	// Update metrics
	if pe.metrics != nil {
		pe.metrics.Counter("forge.plugins.policy.enforced").Inc()
		pe.metrics.Counter("forge.plugins.policy.violations").Add(float64(len(result.Violations)))
		pe.metrics.Counter("forge.plugins.policy.warnings").Add(float64(len(result.Warnings)))
	}

	return nil
}

// ValidateOperation validates an operation against policies
func (pe *PolicyEnforcerImpl) ValidateOperation(ctx context.Context, pluginID string, operation PolicyOperation) (ValidationResult, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	return pe.validateOperationInternal(ctx, pluginID, operation)
}

// LoadPolicies loads policies from a source
func (pe *PolicyEnforcerImpl) LoadPolicies(ctx context.Context, policySource string) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	// This would load policies from various sources (file, database, etc.)
	// For now, just log the operation
	pe.logger.Info("loading policies",
		logger.String("source", policySource),
	)

	return nil
}

// UpdatePolicy updates a policy
func (pe *PolicyEnforcerImpl) UpdatePolicy(ctx context.Context, policy SecurityPolicy) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	policy.UpdatedAt = time.Now()
	pe.policies[policy.ID] = policy

	pe.updateStats()

	pe.logger.Info("policy updated",
		logger.String("policy_id", policy.ID),
		logger.String("name", policy.Name),
		logger.String("version", policy.Version),
	)

	return nil
}

// DeletePolicy deletes a policy
func (pe *PolicyEnforcerImpl) DeletePolicy(ctx context.Context, policyID string) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if _, exists := pe.policies[policyID]; !exists {
		return fmt.Errorf("policy %s not found", policyID)
	}

	delete(pe.policies, policyID)
	pe.updateStats()

	pe.logger.Info("policy deleted",
		logger.String("policy_id", policyID),
	)

	return nil
}

// GetPolicy returns a policy by ID
func (pe *PolicyEnforcerImpl) GetPolicy(ctx context.Context, policyID string) (SecurityPolicy, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	policy, exists := pe.policies[policyID]
	if !exists {
		return SecurityPolicy{}, fmt.Errorf("policy %s not found", policyID)
	}

	return policy, nil
}

// ListPolicies returns all policies
func (pe *PolicyEnforcerImpl) ListPolicies() []SecurityPolicy {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	policies := make([]SecurityPolicy, 0, len(pe.policies))
	for _, policy := range pe.policies {
		policies = append(policies, policy)
	}

	return policies
}

// CheckCompliance checks compliance for a plugin
func (pe *PolicyEnforcerImpl) CheckCompliance(ctx context.Context, pluginID string) (ComplianceReport, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	// Get plugin violations
	var pluginViolations []PolicyViolation
	for _, violation := range pe.violations {
		if violation.PluginID == pluginID {
			pluginViolations = append(pluginViolations, violation)
		}
	}

	// Calculate compliance score
	score := pe.calculateComplianceScore(pluginViolations)

	// Determine status
	var status ComplianceStatus
	if score >= 90 {
		status = ComplianceStatusCompliant
	} else if score >= 70 {
		status = ComplianceStatusPartial
	} else {
		status = ComplianceStatusNonCompliant
	}

	// Generate recommendations
	recommendations := pe.generateRecommendations(pluginViolations)

	return ComplianceReport{
		PluginID:        pluginID,
		OverallScore:    score,
		Status:          status,
		Categories:      pe.calculateCategoryScores(pluginViolations),
		Violations:      pluginViolations,
		Recommendations: recommendations,
		LastChecked:     time.Now(),
		ValidUntil:      time.Now().Add(24 * time.Hour),
	}, nil
}

// GetViolations returns violations for a plugin
func (pe *PolicyEnforcerImpl) GetViolations(ctx context.Context, pluginID string) ([]PolicyViolation, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	var pluginViolations []PolicyViolation
	for _, violation := range pe.violations {
		if violation.PluginID == pluginID {
			pluginViolations = append(pluginViolations, violation)
		}
	}

	return pluginViolations, nil
}

// GenerateComplianceReport generates a global compliance report
func (pe *PolicyEnforcerImpl) GenerateComplianceReport(ctx context.Context) (GlobalComplianceReport, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	// Get all plugin IDs
	pluginIDs := pe.getAllPluginIDs()

	// Generate reports for each plugin
	pluginReports := make(map[string]ComplianceReport)
	totalScore := 0.0

	for _, pluginID := range pluginIDs {
		report, err := pe.CheckCompliance(ctx, pluginID)
		if err == nil {
			pluginReports[pluginID] = report
			totalScore += report.OverallScore
		}
	}

	// Calculate overall score
	overallScore := 0.0
	if len(pluginReports) > 0 {
		overallScore = totalScore / float64(len(pluginReports))
	}

	// Determine overall status
	var status ComplianceStatus
	if overallScore >= 90 {
		status = ComplianceStatusCompliant
	} else if overallScore >= 70 {
		status = ComplianceStatusPartial
	} else {
		status = ComplianceStatusNonCompliant
	}

	return GlobalComplianceReport{
		OverallScore:    overallScore,
		Status:          status,
		PluginReports:   pluginReports,
		CategoryScores:  pe.calculateGlobalCategoryScores(),
		TopViolations:   pe.getTopViolations(10),
		TrendData:       pe.getComplianceTrend(),
		Recommendations: pe.generateGlobalRecommendations(),
		GeneratedAt:     time.Now(),
	}, nil
}

// GetPolicyTemplates returns all policy templates
func (pe *PolicyEnforcerImpl) GetPolicyTemplates() []PolicyTemplate {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	templates := make([]PolicyTemplate, 0, len(pe.templates))
	for _, template := range pe.templates {
		templates = append(templates, template)
	}

	return templates
}

// CreatePolicyFromTemplate creates a policy from a template
func (pe *PolicyEnforcerImpl) CreatePolicyFromTemplate(ctx context.Context, templateID string, config map[string]interface{}) (SecurityPolicy, error) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	template, exists := pe.templates[templateID]
	if !exists {
		return SecurityPolicy{}, fmt.Errorf("template %s not found", templateID)
	}

	// Create policy from template
	policy := template.Template
	policy.ID = pe.generatePolicyID()
	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()

	// Apply configuration
	if err := pe.applyTemplateConfig(&policy, config, template.Parameters); err != nil {
		return SecurityPolicy{}, fmt.Errorf("failed to apply template config: %w", err)
	}

	// Store policy
	pe.policies[policy.ID] = policy
	pe.updateStats()

	pe.logger.Info("policy created from template",
		logger.String("policy_id", policy.ID),
		logger.String("template_id", templateID),
		logger.String("name", policy.Name),
	)

	return policy, nil
}

// GetPolicyStats returns policy statistics
func (pe *PolicyEnforcerImpl) GetPolicyStats() PolicyStats {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	pe.updateStats()
	return pe.stats
}

// Helper methods

func (pe *PolicyEnforcerImpl) validateOperationInternal(ctx context.Context, pluginID string, operation PolicyOperation) (ValidationResult, error) {
	var violations []PolicyViolation
	var warnings []PolicyWarning
	var appliedPolicies []string

	decision := PolicyDecisionAllow
	reason := "No violations found"

	// Evaluate each policy
	for _, policy := range pe.policies {
		if !policy.Enabled {
			continue
		}

		// Check if policy applies to this operation
		if !pe.policyApplies(policy, pluginID, operation) {
			continue
		}

		appliedPolicies = append(appliedPolicies, policy.ID)

		// Evaluate policy rules
		for _, rule := range policy.Rules {
			if !rule.Enabled {
				continue
			}

			if pe.ruleMatches(rule, operation) {
				switch rule.Effect {
				case EffectDeny:
					violation := PolicyViolation{
						ID:        pe.generateViolationID(),
						PolicyID:  policy.ID,
						RuleID:    rule.ID,
						PluginID:  pluginID,
						Operation: operation,
						Severity:  pe.mapPolicySeverityToViolationSeverity(policy.Severity),
						Message:   fmt.Sprintf("Policy %s, Rule %s: %s", policy.Name, rule.Name, rule.Description),
						Details:   map[string]interface{}{"rule": rule, "policy": policy},
						Timestamp: time.Now(),
						Resolved:  false,
						Actions:   pe.getPolicyActions(policy),
					}
					violations = append(violations, violation)
					decision = PolicyDecisionDeny
					reason = violation.Message

				case EffectWarn:
					warning := PolicyWarning{
						PolicyID:  policy.ID,
						RuleID:    rule.ID,
						Message:   fmt.Sprintf("Policy %s, Rule %s: %s", policy.Name, rule.Name, rule.Description),
						Severity:  pe.mapPolicySeverityToWarningSeverity(policy.Severity),
						Details:   map[string]interface{}{"rule": rule, "policy": policy},
						Timestamp: time.Now(),
					}
					warnings = append(warnings, warning)
					if decision == PolicyDecisionAllow {
						decision = PolicyDecisionWarn
					}

				case EffectLog:
					pe.logger.Info("policy log action triggered",
						logger.String("plugin_id", pluginID),
						logger.String("policy", policy.Name),
						logger.String("rule", rule.Name),
					)
				}
			}
		}
	}

	return ValidationResult{
		Valid:           len(violations) == 0,
		Violations:      violations,
		Warnings:        warnings,
		AppliedPolicies: appliedPolicies,
		Decision:        decision,
		Reason:          reason,
		Recommendations: pe.generateOperationRecommendations(violations),
		Metadata:        map[string]interface{}{},
	}, nil
}

func (pe *PolicyEnforcerImpl) policyApplies(policy SecurityPolicy, pluginID string, operation PolicyOperation) bool {
	// Check plugin ID scope
	if len(policy.Scope.PluginIDs) > 0 {
		found := false
		for _, id := range policy.Scope.PluginIDs {
			if id == pluginID || id == "*" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check operation scope
	if len(policy.Scope.Operations) > 0 {
		found := false
		for _, op := range policy.Scope.Operations {
			if op == string(operation.Type) || op == "*" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check resource scope
	if len(policy.Scope.Resources) > 0 {
		found := false
		for _, resource := range policy.Scope.Resources {
			if resource == operation.Resource || resource == "*" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (pe *PolicyEnforcerImpl) ruleMatches(rule PolicyRule, operation PolicyOperation) bool {
	for _, condition := range rule.Conditions {
		if !pe.evaluateRuleCondition(condition, operation) {
			return false
		}
	}
	return true
}

func (pe *PolicyEnforcerImpl) evaluateRuleCondition(condition RuleCondition, operation PolicyOperation) bool {
	// Get the field value from the operation
	fieldValue := pe.getFieldValue(condition.Field, operation)

	// Evaluate the condition
	result := pe.evaluateCondition(fieldValue, condition.Operator, condition.Value, condition.CaseSensitive)

	// Apply negation if needed
	if condition.Negated {
		result = !result
	}

	return result
}

func (pe *PolicyEnforcerImpl) getFieldValue(field string, operation PolicyOperation) interface{} {
	switch field {
	case "operation.type":
		return operation.Type
	case "operation.resource":
		return operation.Resource
	case "operation.action":
		return operation.Action
	case "operation.plugin_id":
		return operation.PluginID
	case "operation.user_id":
		return operation.UserID
	case "operation.ip":
		return operation.IP
	case "operation.user_agent":
		return operation.UserAgent
	default:
		// Check in parameters
		if strings.HasPrefix(field, "parameters.") {
			key := strings.TrimPrefix(field, "parameters.")
			if value, exists := operation.Parameters[key]; exists {
				return value
			}
		}
		// Check in context
		if strings.HasPrefix(field, "context.") {
			key := strings.TrimPrefix(field, "context.")
			if value, exists := operation.Context[key]; exists {
				return value
			}
		}
	}
	return nil
}

func (pe *PolicyEnforcerImpl) evaluateCondition(fieldValue interface{}, operator ConditionOperator, expectedValue interface{}, caseSensitive bool) bool {
	if fieldValue == nil {
		return false
	}

	switch operator {
	case OperatorEquals:
		return pe.compareValues(fieldValue, expectedValue, caseSensitive) == 0
	case OperatorNotEquals:
		return pe.compareValues(fieldValue, expectedValue, caseSensitive) != 0
	case OperatorContains:
		if fieldStr, ok := fieldValue.(string); ok {
			if expectedStr, ok := expectedValue.(string); ok {
				if !caseSensitive {
					fieldStr = strings.ToLower(fieldStr)
					expectedStr = strings.ToLower(expectedStr)
				}
				return strings.Contains(fieldStr, expectedStr)
			}
		}
	case OperatorStartsWith:
		if fieldStr, ok := fieldValue.(string); ok {
			if expectedStr, ok := expectedValue.(string); ok {
				if !caseSensitive {
					fieldStr = strings.ToLower(fieldStr)
					expectedStr = strings.ToLower(expectedStr)
				}
				return strings.HasPrefix(fieldStr, expectedStr)
			}
		}
	case OperatorEndsWith:
		if fieldStr, ok := fieldValue.(string); ok {
			if expectedStr, ok := expectedValue.(string); ok {
				if !caseSensitive {
					fieldStr = strings.ToLower(fieldStr)
					expectedStr = strings.ToLower(expectedStr)
				}
				return strings.HasSuffix(fieldStr, expectedStr)
			}
		}
	}

	return false
}

func (pe *PolicyEnforcerImpl) compareValues(a, b interface{}, caseSensitive bool) int {
	if aStr, ok := a.(string); ok {
		if bStr, ok := b.(string); ok {
			if !caseSensitive {
				aStr = strings.ToLower(aStr)
				bStr = strings.ToLower(bStr)
			}
			return strings.Compare(aStr, bStr)
		}
	}
	return 0
}

func (pe *PolicyEnforcerImpl) handleViolation(ctx context.Context, violation PolicyViolation) {
	pe.violations = append(pe.violations, violation)

	// Execute policy actions
	for _, action := range violation.Actions {
		pe.executePolicyAction(ctx, action, violation)
	}

	pe.logger.Warn("policy violation detected",
		logger.String("plugin_id", violation.PluginID),
		logger.String("policy_id", violation.PolicyID),
		logger.String("rule_id", violation.RuleID),
		logger.String("severity", string(violation.Severity)),
		logger.String("message", violation.Message),
	)
}

func (pe *PolicyEnforcerImpl) executePolicyAction(ctx context.Context, actionType string, violation PolicyViolation) {
	switch ActionType(actionType) {
	case ActionTypeLog:
		pe.logger.Warn("policy action: log",
			logger.String("violation_id", violation.ID),
			logger.String("message", violation.Message),
		)
	case ActionTypeAlert:
		pe.logger.Error("policy action: alert",
			logger.String("violation_id", violation.ID),
			logger.String("message", violation.Message),
		)
	case ActionTypeBlock:
		pe.logger.Error("policy action: block",
			logger.String("violation_id", violation.ID),
			logger.String("plugin_id", violation.PluginID),
		)
		// Implementation would block the plugin
	case ActionTypeTerminate:
		pe.logger.Error("policy action: terminate",
			logger.String("violation_id", violation.ID),
			logger.String("plugin_id", violation.PluginID),
		)
		// Implementation would terminate the plugin
	}
}

func (pe *PolicyEnforcerImpl) loadDefaultPolicies() {
	// Load default security policies
	defaultPolicies := []SecurityPolicy{
		{
			ID:          "default-resource-access",
			Name:        "Resource Access Policy",
			Description: "Controls access to system resources",
			Version:     "1.0.0",
			Category:    PolicyCategoryAccess,
			Severity:    PolicySeverityMedium,
			Rules: []PolicyRule{
				{
					ID:          "resource-access-rule",
					Name:        "Resource Access Rule",
					Description: "Deny access to restricted resources",
					Type:        RuleTypeDeny,
					Conditions: []RuleCondition{
						{
							Field:    "operation.resource",
							Operator: OperatorContains,
							Value:    "/system",
						},
					},
					Effect:   EffectDeny,
					Priority: 100,
					Enabled:  true,
				},
			},
			Actions: []PolicyAction{
				{
					Type:     ActionTypeLog,
					Enabled:  true,
					Priority: 1,
				},
			},
			Scope: PolicyScope{
				Operations: []string{"*"},
			},
			Enabled:   true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			CreatedBy: "system",
		},
	}

	for _, policy := range defaultPolicies {
		pe.policies[policy.ID] = policy
	}
}

func (pe *PolicyEnforcerImpl) loadDefaultTemplates() {
	// Load default policy templates
	defaultTemplates := []PolicyTemplate{
		{
			ID:          "resource-access-template",
			Name:        "Resource Access Template",
			Description: "Template for controlling resource access",
			Category:    PolicyCategoryAccess,
			Template: SecurityPolicy{
				Name:        "Resource Access Policy",
				Description: "Controls access to specified resources",
				Category:    PolicyCategoryAccess,
				Severity:    PolicySeverityMedium,
				Rules: []PolicyRule{
					{
						ID:          "resource-rule",
						Name:        "Resource Rule",
						Description: "Control resource access",
						Type:        RuleTypeDeny,
						Conditions: []RuleCondition{
							{
								Field:    "operation.resource",
								Operator: OperatorContains,
								Value:    "${resource_pattern}",
							},
						},
						Effect:   EffectDeny,
						Priority: 100,
						Enabled:  true,
					},
				},
			},
			Parameters: []TemplateParameter{
				{
					Name:        "resource_pattern",
					Type:        "string",
					Description: "Pattern to match against resources",
					Required:    true,
					Default:     "/restricted",
				},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Version:   "1.0.0",
		},
	}

	for _, template := range defaultTemplates {
		pe.templates[template.ID] = template
	}
}

func (pe *PolicyEnforcerImpl) updateStats() {
	pe.stats.TotalPolicies = len(pe.policies)
	pe.stats.ActivePolicies = 0
	pe.stats.TotalViolations = len(pe.violations)
	pe.stats.RecentViolations = 0
	pe.stats.ViolationsByCategory = make(map[PolicyCategory]int)
	pe.stats.ViolationsBySeverity = make(map[ViolationSeverity]int)
	pe.stats.PolicyByCategory = make(map[PolicyCategory]int)

	// Count active policies and categories
	for _, policy := range pe.policies {
		if policy.Enabled {
			pe.stats.ActivePolicies++
		}
		pe.stats.PolicyByCategory[policy.Category]++
	}

	// Count recent violations (last 24 hours)
	recentTime := time.Now().Add(-24 * time.Hour)
	for _, violation := range pe.violations {
		if violation.Timestamp.After(recentTime) {
			pe.stats.RecentViolations++
		}
		pe.stats.ViolationsBySeverity[violation.Severity]++
	}

	// Calculate enforcement rate
	if pe.stats.TotalViolations > 0 {
		pe.stats.EnforcementRate = float64(pe.stats.TotalViolations-pe.stats.RecentViolations) / float64(pe.stats.TotalViolations) * 100
	}

	pe.stats.LastUpdated = time.Now()
}

// Helper methods for compliance reporting

func (pe *PolicyEnforcerImpl) calculateComplianceScore(violations []PolicyViolation) float64 {
	if len(violations) == 0 {
		return 100.0
	}

	// Score calculation based on violation severity
	totalPenalty := 0.0
	for _, violation := range violations {
		switch violation.Severity {
		case ViolationSeverityCritical:
			totalPenalty += 25.0
		case ViolationSeverityHigh:
			totalPenalty += 15.0
		case ViolationSeverityMedium:
			totalPenalty += 10.0
		case ViolationSeverityLow:
			totalPenalty += 5.0
		}
	}

	score := 100.0 - totalPenalty
	if score < 0 {
		score = 0
	}

	return score
}

func (pe *PolicyEnforcerImpl) calculateCategoryScores(violations []PolicyViolation) map[string]float64 {
	categoryViolations := make(map[PolicyCategory][]PolicyViolation)

	// Group violations by category
	for _, violation := range violations {
		if policy, exists := pe.policies[violation.PolicyID]; exists {
			categoryViolations[policy.Category] = append(categoryViolations[policy.Category], violation)
		}
	}

	// Calculate score for each category
	scores := make(map[string]float64)
	for category, categoryViolations := range categoryViolations {
		scores[string(category)] = pe.calculateComplianceScore(categoryViolations)
	}

	// Add categories with no violations
	for _, policy := range pe.policies {
		if _, exists := scores[string(policy.Category)]; !exists {
			scores[string(policy.Category)] = 100.0
		}
	}

	return scores
}

func (pe *PolicyEnforcerImpl) generateRecommendations(violations []PolicyViolation) []string {
	var recommendations []string
	severityCounts := make(map[ViolationSeverity]int)

	for _, violation := range violations {
		severityCounts[violation.Severity]++
	}

	if severityCounts[ViolationSeverityCritical] > 0 {
		recommendations = append(recommendations, "Address critical security violations immediately")
	}
	if severityCounts[ViolationSeverityHigh] > 3 {
		recommendations = append(recommendations, "High priority: Review and fix high severity violations")
	}
	if len(violations) > 10 {
		recommendations = append(recommendations, "Consider reviewing plugin security practices")
	}

	return recommendations
}

func (pe *PolicyEnforcerImpl) generateOperationRecommendations(violations []PolicyViolation) []string {
	var recommendations []string

	for _, violation := range violations {
		switch violation.Severity {
		case ViolationSeverityCritical:
			recommendations = append(recommendations, "This operation violates critical security policies and should be blocked")
		case ViolationSeverityHigh:
			recommendations = append(recommendations, "Consider alternative approaches that don't violate security policies")
		case ViolationSeverityMedium:
			recommendations = append(recommendations, "Review the operation for potential security improvements")
		}
	}

	return recommendations
}

func (pe *PolicyEnforcerImpl) getAllPluginIDs() []string {
	pluginIDs := make(map[string]bool)

	for _, violation := range pe.violations {
		pluginIDs[violation.PluginID] = true
	}

	var result []string
	for pluginID := range pluginIDs {
		result = append(result, pluginID)
	}

	return result
}

func (pe *PolicyEnforcerImpl) calculateGlobalCategoryScores() map[PolicyCategory]float64 {
	scores := make(map[PolicyCategory]float64)

	// This would calculate scores across all plugins
	// Simplified implementation for now
	scores[PolicyCategoryAccess] = 85.0
	scores[PolicyCategoryResource] = 90.0
	scores[PolicyCategoryNetwork] = 88.0
	scores[PolicyCategoryData] = 92.0

	return scores
}

func (pe *PolicyEnforcerImpl) getTopViolations(count int) []PolicyViolation {
	// Sort violations by severity and timestamp
	violations := make([]PolicyViolation, len(pe.violations))
	copy(violations, pe.violations)

	// Simple sorting - in practice would be more sophisticated
	var topViolations []PolicyViolation
	for _, violation := range violations {
		if violation.Severity == ViolationSeverityCritical {
			topViolations = append(topViolations, violation)
		}
	}

	if len(topViolations) >= count {
		return topViolations[:count]
	}

	return topViolations
}

func (pe *PolicyEnforcerImpl) getComplianceTrend() []ComplianceTrend {
	// Generate sample trend data
	// In practice, this would come from historical data
	var trends []ComplianceTrend

	now := time.Now()
	for i := 30; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		score := 85.0 + float64(i%10) // Sample varying scores
		trends = append(trends, ComplianceTrend{
			Date:  date,
			Score: score,
		})
	}

	return trends
}

func (pe *PolicyEnforcerImpl) generateGlobalRecommendations() []string {
	return []string{
		"Review and update security policies regularly",
		"Implement automated policy compliance checks",
		"Provide security training for plugin developers",
		"Establish incident response procedures for policy violations",
	}
}

func (pe *PolicyEnforcerImpl) applyTemplateConfig(policy *SecurityPolicy, config map[string]interface{}, parameters []TemplateParameter) error {
	// Apply configuration values to template placeholders
	// This is a simplified implementation
	policyStr := fmt.Sprintf("%+v", policy)

	for _, param := range parameters {
		placeholder := fmt.Sprintf("${%s}", param.Name)
		var value interface{}

		if configValue, exists := config[param.Name]; exists {
			value = configValue
		} else if param.Default != nil {
			value = param.Default
		} else if param.Required {
			return fmt.Errorf("required parameter %s not provided", param.Name)
		}

		if value != nil {
			valueStr := fmt.Sprintf("%v", value)
			policyStr = strings.ReplaceAll(policyStr, placeholder, valueStr)
		}
	}

	return nil
}

func (pe *PolicyEnforcerImpl) generatePolicyID() string {
	return fmt.Sprintf("policy_%d", time.Now().UnixNano())
}

func (pe *PolicyEnforcerImpl) generateViolationID() string {
	return fmt.Sprintf("violation_%d", time.Now().UnixNano())
}

func (pe *PolicyEnforcerImpl) mapPolicySeverityToViolationSeverity(severity PolicySeverity) ViolationSeverity {
	switch severity {
	case PolicySeverityCritical:
		return ViolationSeverityCritical
	case PolicySeverityHigh:
		return ViolationSeverityHigh
	case PolicySeverityMedium:
		return ViolationSeverityMedium
	case PolicySeverityLow:
		return ViolationSeverityLow
	default:
		return ViolationSeverityLow
	}
}

func (pe *PolicyEnforcerImpl) mapPolicySeverityToWarningSeverity(severity PolicySeverity) WarningSeverity {
	switch severity {
	case PolicySeverityHigh, PolicySeverityCritical:
		return WarningSeverityHigh
	case PolicySeverityMedium:
		return WarningSeverityMedium
	case PolicySeverityLow:
		return WarningSeverityLow
	default:
		return WarningSeverityInfo
	}
}

func (pe *PolicyEnforcerImpl) getPolicyActions(policy SecurityPolicy) []string {
	var actions []string
	for _, action := range policy.Actions {
		if action.Enabled {
			actions = append(actions, string(action.Type))
		}
	}
	return actions
}
