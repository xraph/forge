package security

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	common2 "github.com/xraph/forge/pkg/pluginengine/common"
)

// Config contains security configuration for plugins
type Config struct {
	Enabled                bool                   `yaml:"enabled" json:"enabled"`
	StrictMode             bool                   `yaml:"strict_mode" json:"strict_mode"`
	ScanningEnabled        bool                   `yaml:"scanning_enabled" json:"scanning_enabled"`
	IsolationEnabled       bool                   `yaml:"isolation_enabled" json:"isolation_enabled"`
	PolicyEnforcementLevel PolicyEnforcementLevel `yaml:"policy_enforcement_level" json:"policy_enforcement_level"`
	DefaultLimits          DefaultResourceLimits  `yaml:"default_limits" json:"default_limits"`
	TrustedPlugins         []string               `yaml:"trusted_plugins" json:"trusted_plugins"`
	BlockedPlugins         []string               `yaml:"blocked_plugins" json:"blocked_plugins"`
	ScanConfig             ScanConfig             `yaml:"scan_config" json:"scan_config"`
	VulnerabilityDB        VulnerabilityDBConfig  `yaml:"vulnerability_db" json:"vulnerability_db"`
}

type PolicyEnforcementLevel string

const (
	PolicyEnforcementLevelNone    PolicyEnforcementLevel = "none"
	PolicyEnforcementLevelWarn    PolicyEnforcementLevel = "warn"
	PolicyEnforcementLevelEnforce PolicyEnforcementLevel = "enforce"
	PolicyEnforcementLevelStrict  PolicyEnforcementLevel = "strict"
)

type DefaultResourceLimits struct {
	MaxMemoryMB    int64         `yaml:"max_memory_mb" json:"max_memory_mb"`
	MaxCPUPercent  float64       `yaml:"max_cpu_percent" json:"max_cpu_percent"`
	MaxDiskSpaceMB int64         `yaml:"max_disk_space_mb" json:"max_disk_space_mb"`
	MaxConnections int           `yaml:"max_connections" json:"max_connections"`
	MaxFiles       int           `yaml:"max_files" json:"max_files"`
	MaxProcesses   int           `yaml:"max_processes" json:"max_processes"`
	Timeout        time.Duration `yaml:"timeout" json:"timeout"`
}

type ScanConfig struct {
	AutoScan              bool          `yaml:"auto_scan" json:"auto_scan"`
	ScanOnLoad            bool          `yaml:"scan_on_load" json:"scan_on_load"`
	ScanOnUpdate          bool          `yaml:"scan_on_update" json:"scan_on_update"`
	MaxScanTime           time.Duration `yaml:"max_scan_time" json:"max_scan_time"`
	AllowCriticalRisk     bool          `yaml:"allow_critical_risk" json:"allow_critical_risk"`
	AllowHighRisk         bool          `yaml:"allow_high_risk" json:"allow_high_risk"`
	QuarantineOnViolation bool          `yaml:"quarantine_on_violation" json:"quarantine_on_violation"`
}

type VulnerabilityDBConfig struct {
	UpdateInterval time.Duration `yaml:"update_interval" json:"update_interval"`
	Sources        []string      `yaml:"sources" json:"sources"`
	CacheEnabled   bool          `yaml:"cache_enabled" json:"cache_enabled"`
	CacheTTL       time.Duration `yaml:"cache_ttl" json:"cache_ttl"`
}

// SecurityManager coordinates all security components
type SecurityManager struct {
	config            Config
	permissionManager PermissionManager
	policyEnforcer    PolicyEnforcer
	scanner           SecurityScanner
	isolationManager  IsolationManager
	sandboxManager    *SandboxManager
	logger            common.Logger
	metrics           common.Metrics
}

// NewSecurityManager creates a new security manager
func NewSecurityManager(config Config, logger common.Logger, metrics common.Metrics) *SecurityManager {
	sm := &SecurityManager{
		config:  config,
		logger:  logger,
		metrics: metrics,
	}

	if config.Enabled {
		sm.permissionManager = NewPermissionManager(logger, metrics)
		sm.policyEnforcer = NewPolicyEnforcer(sm.permissionManager, logger, metrics)
		sm.scanner = NewSecurityScanner(logger, metrics)
		sm.isolationManager = NewIsolationManager(logger, metrics)
		sm.sandboxManager = NewSandboxManager(logger, metrics)
	}

	return sm
}

// Initialize initializes all security components
func (sm *SecurityManager) Initialize(ctx context.Context) error {
	if !sm.config.Enabled {
		sm.logger.Info("security manager disabled")
		return nil
	}

	// Initialize vulnerability database
	if sm.config.VulnerabilityDB.UpdateInterval > 0 {
		if err := sm.scanner.UpdateVulnerabilityDatabase(ctx); err != nil {
			sm.logger.Warn("failed to update vulnerability database", logger.Error(err))
		}
	}

	// Setup default security policies
	if err := sm.setupDefaultPolicies(ctx); err != nil {
		return fmt.Errorf("failed to setup default policies: %w", err)
	}

	// Setup default roles and permissions
	if err := sm.setupDefaultRoles(ctx); err != nil {
		return fmt.Errorf("failed to setup default roles: %w", err)
	}

	sm.logger.Info("security manager initialized",
		logger.Bool("scanning_enabled", sm.config.ScanningEnabled),
		logger.Bool("isolation_enabled", sm.config.IsolationEnabled),
		logger.String("enforcement_level", string(sm.config.PolicyEnforcementLevel)),
	)

	return nil
}

// ValidatePluginSecurity performs comprehensive security validation
func (sm *SecurityManager) ValidatePluginSecurity(ctx context.Context, pluginID string, pkg common2.PluginEnginePackage) (SecurityValidationResult, error) {
	result := SecurityValidationResult{
		PluginID:  pluginID,
		Valid:     true,
		CheckedAt: time.Now(),
		Checks:    make(map[string]SecurityCheckResult),
	}

	if !sm.config.Enabled {
		result.Checks["security_disabled"] = SecurityCheckResult{
			Name:    "Security Disabled",
			Passed:  true,
			Message: "Security validation is disabled",
		}
		return result, nil
	}

	// Check if plugin is blocked
	if sm.isPluginBlocked(pluginID) {
		result.Valid = false
		result.Checks["blocked"] = SecurityCheckResult{
			Name:    "Plugin Blocked",
			Passed:  false,
			Message: "Plugin is in the blocked list",
		}
		return result, nil
	}

	// Check if plugin is trusted (skip some checks)
	trusted := sm.isPluginTrusted(pluginID)
	result.Trusted = trusted

	// Perform security scan
	if sm.config.ScanningEnabled && sm.config.ScanConfig.ScanOnLoad {
		scanResult, err := sm.scanner.ScanPackage(ctx, pkg)
		if err != nil {
			result.Valid = false
			result.Checks["scan_failed"] = SecurityCheckResult{
				Name:    "Security Scan",
				Passed:  false,
				Message: fmt.Sprintf("Security scan failed: %v", err),
			}
		} else {
			result.ScanResult = &scanResult

			// Check risk level
			if !trusted && !sm.isRiskLevelAcceptable(scanResult.RiskLevel) {
				result.Valid = false
				result.Checks["risk_too_high"] = SecurityCheckResult{
					Name:    "Risk Level Check",
					Passed:  false,
					Message: fmt.Sprintf("Risk level %s is not acceptable", scanResult.RiskLevel),
				}
			} else {
				result.Checks["risk_level"] = SecurityCheckResult{
					Name:    "Risk Level Check",
					Passed:  true,
					Message: fmt.Sprintf("Risk level %s is acceptable", scanResult.RiskLevel),
				}
			}
		}
	}

	// Validate package integrity
	if err := sm.validatePackageIntegrity(pkg); err != nil {
		result.Valid = false
		result.Checks["integrity"] = SecurityCheckResult{
			Name:    "Package Integrity",
			Passed:  false,
			Message: fmt.Sprintf("Package integrity check failed: %v", err),
		}
	} else {
		result.Checks["integrity"] = SecurityCheckResult{
			Name:    "Package Integrity",
			Passed:  true,
			Message: "Package integrity verified",
		}
	}

	// Check resource requirements
	if err := sm.validateResourceRequirements(pkg); err != nil {
		if sm.config.StrictMode {
			result.Valid = false
		}
		result.Checks["resources"] = SecurityCheckResult{
			Name:    "Resource Requirements",
			Passed:  false,
			Message: fmt.Sprintf("Resource requirements validation failed: %v", err),
		}
	} else {
		result.Checks["resources"] = SecurityCheckResult{
			Name:    "Resource Requirements",
			Passed:  true,
			Message: "Resource requirements are acceptable",
		}
	}

	return result, nil
}

// CreateSecurePluginEnvironment sets up a secure environment for a plugin
func (sm *SecurityManager) CreateSecurePluginEnvironment(ctx context.Context, pluginID string, config PluginSecurityConfig) (*SecurePluginEnvironment, error) {
	if !sm.config.Enabled {
		return &SecurePluginEnvironment{PluginID: pluginID}, nil
	}

	env := &SecurePluginEnvironment{
		PluginID:  pluginID,
		CreatedAt: time.Now(),
		Config:    config,
	}

	// Create sandbox
	if sm.config.IsolationEnabled {
		sandboxConfig := sm.createSandboxConfig(pluginID, config)
		sandbox, err := sm.sandboxManager.CreateSandbox(ctx, sandboxConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create sandbox: %w", err)
		}
		env.Sandbox = sandbox
	}

	// Create isolated process
	if sm.config.IsolationEnabled {
		isolationConfig := sm.createIsolationConfig(pluginID, config)
		process, err := sm.isolationManager.CreateIsolatedProcess(ctx, isolationConfig)
		if err != nil {
			if env.Sandbox != nil {
				env.Sandbox.Destroy(ctx)
			}
			return nil, fmt.Errorf("failed to create isolated process: %w", err)
		}
		env.IsolatedProcess = process
	}

	// Assign permissions
	if err := sm.assignPluginPermissions(ctx, pluginID, config); err != nil {
		sm.cleanupEnvironment(ctx, env)
		return nil, fmt.Errorf("failed to assign permissions: %w", err)
	}

	return env, nil
}

// MonitorPluginSecurity continuously monitors plugin security
func (sm *SecurityManager) MonitorPluginSecurity(ctx context.Context, pluginID string) error {
	if !sm.config.Enabled {
		return nil
	}

	// Start monitoring goroutine
	go sm.securityMonitorLoop(ctx, pluginID)
	return nil
}

// IsolationManager retrieves and returns the current IsolationManager instance associated with the SecurityManager.
func (sm *SecurityManager) IsolationManager() IsolationManager {
	return sm.isolationManager
}

// SandboxManager returns the currently associated SandboxManager instance from the SecurityManager.
func (sm *SecurityManager) SandboxManager() *SandboxManager {
	return sm.sandboxManager
}

// PolicyEnforcer returns the PolicyEnforcer instance associated with the SecurityManager.
func (sm *SecurityManager) PolicyEnforcer() PolicyEnforcer {
	return sm.policyEnforcer
}

// Scanner returns the SecurityScanner instance associated with the SecurityManager.
func (sm *SecurityManager) Scanner() SecurityScanner {
	return sm.scanner
}

// PermissionManager returns the PermissionManager instance associated with the SecurityManager.
func (sm *SecurityManager) PermissionManager() PermissionManager {
	return sm.permissionManager
}

// Helper methods

func (sm *SecurityManager) setupDefaultPolicies(ctx context.Context) error {
	policies := []SecurityPolicy{
		{
			ID:          "default-file-access",
			Name:        "Default File Access Policy",
			Description: "Controls file system access for plugins",
			Category:    PolicyCategoryAccess,
			Severity:    PolicySeverityMedium,
			Rules: []PolicyRule{
				{
					ID:          "deny-system-files",
					Name:        "Deny System Files Access",
					Description: "Prevent access to system files",
					Type:        RuleTypeDeny,
					Conditions: []RuleCondition{
						{
							Field:    "operation.resource",
							Operator: OperatorStartsWith,
							Value:    "/etc/",
						},
					},
					Effect:   EffectDeny,
					Priority: 100,
					Enabled:  true,
				},
			},
			Enabled:   true,
			CreatedAt: time.Now(),
			CreatedBy: "system",
		},
		{
			ID:          "plugin-resource-access",
			Name:        "Plugin Resource Access Policy",
			Description: "Controls plugin access to system resources",
			Category:    PolicyCategoryAccess,
			Severity:    PolicySeverityHigh,
			Rules: []PolicyRule{
				{
					ID:          "file-system-access",
					Name:        "File System Access Rule",
					Description: "Restrict plugin access to sensitive file paths",
					Type:        RuleTypeDeny,
					Conditions: []RuleCondition{
						{
							Field:    "operation.resource",
							Operator: OperatorStartsWith,
							Value:    "/etc",
						},
					},
					Effect:   EffectDeny,
					Priority: 100,
					Enabled:  true,
				},
			},
			Scope: PolicyScope{
				Operations: []string{"filesystem", "read", "write"},
			},
			Enabled:   true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			CreatedBy: "system",
		},
		{
			ID:          "plugin-network-policy",
			Name:        "Plugin Network Policy",
			Description: "Controls plugin network access",
			Category:    PolicyCategoryNetwork,
			Severity:    PolicySeverityMedium,
			Rules: []PolicyRule{
				{
					ID:          "outbound-connections",
					Name:        "Outbound Connection Rule",
					Description: "Limit plugin outbound connections",
					Type:        RuleTypeLimit,
					Conditions: []RuleCondition{
						{
							Field:    "operation.type",
							Operator: OperatorEquals,
							Value:    "network",
						},
					},
					Effect:   EffectThrottle,
					Priority: 50,
					Enabled:  true,
				},
			},
			Scope: PolicyScope{
				Operations: []string{"network"},
			},
			Enabled:   true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			CreatedBy: "system",
		},
	}

	for _, policy := range policies {
		if err := sm.policyEnforcer.UpdatePolicy(ctx, policy); err != nil {
			return err
		}
	}

	return nil
}

func (sm *SecurityManager) setupDefaultRoles(ctx context.Context) error {
	roles := []Role{
		{
			Name:        "plugin-basic",
			Description: "Basic plugin permissions",
			Permissions: []Permission{
				{
					Name:     "read-config",
					Resource: "config/*",
					Action:   "read",
					Scope:    PermissionScope{Type: ScopeTypeResource},
				},
				{
					Name:     "write-logs",
					Resource: "logs/*",
					Action:   "write",
					Scope:    PermissionScope{Type: ScopeTypeResource},
				},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			CreatedBy: "system",
		},
		{
			Name:        "plugin-advanced",
			Description: "Advanced plugin permissions",
			Permissions: []Permission{
				{
					Name:     "network-access",
					Resource: "network/*",
					Action:   "*",
					Scope:    PermissionScope{Type: ScopeTypeResource},
				},
				{
					Name:     "file-system-access",
					Resource: "filesystem/user/*",
					Action:   "*",
					Scope:    PermissionScope{Type: ScopeTypeResource},
				},
			},
			Inherits:  []string{"plugin-basic"},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			CreatedBy: "system",
		},
	}

	for _, role := range roles {
		if err := sm.permissionManager.CreateRole(ctx, role); err != nil {
			return err
		}
	}

	return nil
}

func (sm *SecurityManager) isPluginBlocked(pluginID string) bool {
	for _, blocked := range sm.config.BlockedPlugins {
		if blocked == pluginID {
			return true
		}
	}
	return false
}

func (sm *SecurityManager) isPluginTrusted(pluginID string) bool {
	for _, trusted := range sm.config.TrustedPlugins {
		if trusted == pluginID {
			return true
		}
	}
	return false
}

func (sm *SecurityManager) isRiskLevelAcceptable(riskLevel RiskLevel) bool {
	switch riskLevel {
	case RiskLevelCritical:
		return sm.config.ScanConfig.AllowCriticalRisk
	case RiskLevelHigh:
		return sm.config.ScanConfig.AllowHighRisk
	case RiskLevelMedium, RiskLevelLow, RiskLevelInfo:
		return true
	default:
		return false
	}
}

func (sm *SecurityManager) validatePackageIntegrity(pkg common2.PluginEnginePackage) error {
	// Implement package integrity validation
	// This could include signature verification, checksum validation, etc.
	return nil
}

func (sm *SecurityManager) validateResourceRequirements(pkg common2.PluginEnginePackage) error {
	// Check if plugin resource requirements are within acceptable limits
	// This would examine the plugin's resource declarations
	return nil
}

func (sm *SecurityManager) createSandboxConfig(pluginID string, config PluginSecurityConfig) SandboxConfig {
	return SandboxConfig{
		PluginID:     pluginID,
		Isolated:     true,
		MaxMemory:    sm.config.DefaultLimits.MaxMemoryMB * 1024 * 1024,
		MaxCPU:       sm.config.DefaultLimits.MaxCPUPercent / 100.0,
		MaxDiskSpace: sm.config.DefaultLimits.MaxDiskSpaceMB * 1024 * 1024,
		Timeout:      sm.config.DefaultLimits.Timeout,
		NetworkAccess: NetworkPolicy{
			Allowed:  config.NetworkAccess,
			MaxConns: sm.config.DefaultLimits.MaxConnections,
		},
		FileSystemAccess: FileSystemPolicy{
			ReadOnly:    config.ReadOnlyFileSystem,
			MaxFiles:    sm.config.DefaultLimits.MaxFiles,
			NoDevAccess: true,
		},
	}
}

func (sm *SecurityManager) createIsolationConfig(pluginID string, config PluginSecurityConfig) IsolationConfig {
	return IsolationConfig{
		ProcessID:  pluginID,
		Command:    "/usr/bin/plugin-runner",
		Args:       []string{"--plugin", pluginID},
		WorkingDir: fmt.Sprintf("/tmp/plugins/%s", pluginID),
		Resources: ResourceLimits{
			Memory: MemoryLimits{
				Limit: sm.config.DefaultLimits.MaxMemoryMB * 1024 * 1024,
			},
			CPU: CPULimits{
				Cores: sm.config.DefaultLimits.MaxCPUPercent / 100.0,
			},
			Processes: ProcessLimits{
				MaxProcesses: sm.config.DefaultLimits.MaxProcesses,
			},
		},
		Timeout: sm.config.DefaultLimits.Timeout,
	}
}

func (sm *SecurityManager) assignPluginPermissions(ctx context.Context, pluginID string, config PluginSecurityConfig) error {
	// Assign role based on plugin requirements
	role := "plugin-basic"
	if config.RequiresElevatedPermissions {
		role = "plugin-advanced"
	}

	return sm.permissionManager.AssignRole(ctx, pluginID, role)
}

func (sm *SecurityManager) cleanupEnvironment(ctx context.Context, env *SecurePluginEnvironment) {
	if env.Sandbox != nil {
		env.Sandbox.Destroy(ctx)
	}
	if env.IsolatedProcess != nil {
		env.IsolatedProcess.Stop(ctx)
	}
}

func (sm *SecurityManager) securityMonitorLoop(ctx context.Context, pluginID string) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Perform periodic security checks
			sm.performSecurityCheck(ctx, pluginID)
		}
	}
}

func (sm *SecurityManager) performSecurityCheck(ctx context.Context, pluginID string) {
	// Check compliance
	if report, err := sm.policyEnforcer.CheckCompliance(ctx, pluginID); err == nil {
		if report.Status != ComplianceStatusCompliant {
			sm.logger.Warn("plugin compliance issue detected",
				logger.String("plugin_id", pluginID),
				logger.String("status", string(report.Status)),
				logger.Float64("score", report.OverallScore),
			)
		}
	}

	// Check for violations
	if violations, err := sm.policyEnforcer.GetViolations(ctx, pluginID); err == nil {
		for _, violation := range violations {
			if !violation.Resolved {
				sm.logger.Warn("unresolved security violation",
					logger.String("plugin_id", pluginID),
					logger.String("violation_id", violation.ID),
					logger.String("severity", string(violation.Severity)),
				)
			}
		}
	}
}

// Supporting types

type SecurityValidationResult struct {
	PluginID   string                         `json:"plugin_id"`
	Valid      bool                           `json:"valid"`
	Trusted    bool                           `json:"trusted"`
	CheckedAt  time.Time                      `json:"checked_at"`
	ScanResult *ScanResult                    `json:"scan_result,omitempty"`
	Checks     map[string]SecurityCheckResult `json:"checks"`
}

type SecurityCheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

type PluginSecurityConfig struct {
	NetworkAccess               bool `json:"network_access"`
	ReadOnlyFileSystem          bool `json:"readonly_filesystem"`
	RequiresElevatedPermissions bool `json:"requires_elevated_permissions"`
}

type SecurePluginEnvironment struct {
	PluginID        string               `json:"plugin_id"`
	CreatedAt       time.Time            `json:"created_at"`
	Config          PluginSecurityConfig `json:"config"`
	Sandbox         PluginSandbox        `json:"-"`
	IsolatedProcess IsolatedProcess      `json:"-"`
}

// DefaultSecurityConfig returns a default security configuration
func DefaultSecurityConfig() Config {
	return Config{
		Enabled:                true,
		StrictMode:             false,
		ScanningEnabled:        true,
		IsolationEnabled:       true,
		PolicyEnforcementLevel: PolicyEnforcementLevelEnforce,
		DefaultLimits: DefaultResourceLimits{
			MaxMemoryMB:    1024, // 1GB
			MaxCPUPercent:  50.0, // 50%
			MaxDiskSpaceMB: 100,  // 100MB
			MaxConnections: 10,
			MaxFiles:       100,
			MaxProcesses:   5,
			Timeout:        30 * time.Second,
		},
		ScanConfig: ScanConfig{
			AutoScan:              true,
			ScanOnLoad:            true,
			ScanOnUpdate:          true,
			MaxScanTime:           5 * time.Minute,
			AllowCriticalRisk:     false,
			AllowHighRisk:         false,
			QuarantineOnViolation: true,
		},
		VulnerabilityDB: VulnerabilityDBConfig{
			UpdateInterval: 24 * time.Hour,
			Sources:        []string{"nvd", "github-advisory"},
			CacheEnabled:   true,
			CacheTTL:       1 * time.Hour,
		},
	}
}
