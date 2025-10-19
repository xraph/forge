package security

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// PermissionManager manages plugin permissions
type PermissionManager interface {

	// GrantPermission grants the specified permission to a plugin, updating its permission set in the permission manager.
	GrantPermission(ctx context.Context, pluginID string, permission Permission) error

	// RevokePermission revokes a specific permission from the given plugin by its permission name.
	RevokePermission(ctx context.Context, pluginID string, permissionName string) error

	// HasPermission verifies if a plugin has the specified permission and returns the result along with any potential error.
	HasPermission(ctx context.Context, pluginID string, permission Permission) (bool, error)

	// GetPermissions retrieves the list of permissions associated with the specified plugin ID.
	GetPermissions(ctx context.Context, pluginID string) ([]Permission, error)

	// CheckPermission verifies if the specified plugin has permission to perform the given action on the provided resource.
	CheckPermission(ctx context.Context, pluginID string, resource string, action string) error

	// CheckMultiplePermissions verifies a batch of permission checks for a specified plugin and returns an error if any fail.
	CheckMultiplePermissions(ctx context.Context, pluginID string, checks []PermissionCheck) error

	// CreateRole creates a new role with the specified permissions and metadata. It returns an error if the operation fails.
	CreateRole(ctx context.Context, role Role) error

	// AssignRole assigns a role to the specified plugin by its pluginID. Returns an error if the operation fails.
	AssignRole(ctx context.Context, pluginID string, roleName string) error

	// RemoveRole removes the specified role from a plugin, ensuring the role no longer applies to the given plugin.
	RemoveRole(ctx context.Context, pluginID string, roleName string) error

	// GetRoles retrieves all roles assigned to a plugin identified by pluginID from the context.
	GetRoles(ctx context.Context, pluginID string) ([]Role, error)

	// CreatePolicy creates a new permission policy in the system using the provided policy details. Returns an error if it fails.
	CreatePolicy(ctx context.Context, policy Policy) error

	// UpdatePolicy updates an existing policy in the system based on the provided policy object and context.
	UpdatePolicy(ctx context.Context, policy Policy) error

	// DeletePolicy removes a policy by its name from the system. Returns an error if the operation fails.
	DeletePolicy(ctx context.Context, policyName string) error

	// EvaluatePolicy evaluates a policy request and returns the decision, associated policies, and optional context or an error.
	EvaluatePolicy(ctx context.Context, request PolicyRequest) (PolicyResult, error)

	// GetAuditLog retrieves the audit log entries for the specified plugin ID and returns them as a slice of AuditEntry.
	GetAuditLog(ctx context.Context, pluginID string) ([]AuditEntry, error)

	// GetPermissionStats retrieves aggregated permission statistics, including counts of permissions, roles, policies, and audit logs.
	GetPermissionStats() PermissionStats
}

// Permission represents a specific permission
type Permission struct {
	Name       string                 `json:"name"`
	Resource   string                 `json:"resource"`
	Action     string                 `json:"action"`
	Conditions []PermissionCondition  `json:"conditions"`
	Scope      PermissionScope        `json:"scope"`
	GrantedAt  time.Time              `json:"granted_at"`
	ExpiresAt  *time.Time             `json:"expires_at,omitempty"`
	GrantedBy  string                 `json:"granted_by"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// PermissionCondition defines conditions for permission evaluation
type PermissionCondition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
	Negated  bool        `json:"negated"`
}

// PermissionScope defines the scope of a permission
type PermissionScope struct {
	Type       ScopeType              `json:"type"`
	Resources  []string               `json:"resources"`
	Attributes map[string]interface{} `json:"attributes"`
}

// ScopeType defines the type of permission scope
type ScopeType string

const (
	ScopeTypeGlobal      ScopeType = "global"
	ScopeTypeService     ScopeType = "service"
	ScopeTypeResource    ScopeType = "resource"
	ScopeTypeAttribute   ScopeType = "attribute"
	ScopeTypeConditional ScopeType = "conditional"
)

// PermissionCheck represents a permission check request
type PermissionCheck struct {
	Resource string                 `json:"resource"`
	Action   string                 `json:"action"`
	Context  map[string]interface{} `json:"context"`
}

// Role represents a role with associated permissions
type Role struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Permissions []Permission           `json:"permissions"`
	Inherits    []string               `json:"inherits"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CreatedBy   string                 `json:"created_by"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// Policy represents a permission policy
type Policy struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Version     string                 `json:"version"`
	Rules       []PermissionPolicyRule `json:"rules"`
	Effect      PolicyEffect           `json:"effect"`
	Priority    int                    `json:"priority"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CreatedBy   string                 `json:"created_by"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// PermissionPolicyRule defines a rule within a policy
type PermissionPolicyRule struct {
	Name       string                 `json:"name"`
	Resources  []string               `json:"resources"`
	Actions    []string               `json:"actions"`
	Conditions []PermissionCondition  `json:"conditions"`
	Effect     PolicyEffect           `json:"effect"`
	Priority   int                    `json:"priority"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// PolicyEffect defines the effect of a policy rule
type PolicyEffect string

const (
	PolicyEffectAllow PolicyEffect = "allow"
	PolicyEffectDeny  PolicyEffect = "deny"
)

// PolicyRequest represents a policy evaluation request
type PolicyRequest struct {
	PluginID  string                 `json:"plugin_id"`
	Resource  string                 `json:"resource"`
	Action    string                 `json:"action"`
	Context   map[string]interface{} `json:"context"`
	Timestamp time.Time              `json:"timestamp"`
}

// PolicyResult represents the result of a policy evaluation
type PolicyResult struct {
	Decision  PolicyDecision         `json:"decision"`
	Reason    string                 `json:"reason"`
	Policies  []string               `json:"policies"`
	Context   map[string]interface{} `json:"context"`
	Timestamp time.Time              `json:"timestamp"`
}

// PolicyDecision represents a policy decision
type PolicyDecision string

const (
	PolicyDecisionAllow   PolicyDecision = "allow"
	PolicyDecisionDeny    PolicyDecision = "deny"
	PolicyDecisionWarn    PolicyDecision = "warn"
	PolicyDecisionMonitor PolicyDecision = "monitor"
)

// AuditEntry represents an audit log entry
type AuditEntry struct {
	ID        string                 `json:"id"`
	PluginID  string                 `json:"plugin_id"`
	Action    string                 `json:"action"`
	Resource  string                 `json:"resource"`
	Result    string                 `json:"result"`
	Reason    string                 `json:"reason"`
	Context   map[string]interface{} `json:"context"`
	Timestamp time.Time              `json:"timestamp"`
	UserAgent string                 `json:"user_agent"`
	IP        string                 `json:"ip"`
}

// PermissionStats contains permission statistics
type PermissionStats struct {
	TotalPermissions      int            `json:"total_permissions"`
	ActivePermissions     int            `json:"active_permissions"`
	ExpiredPermissions    int            `json:"expired_permissions"`
	TotalRoles            int            `json:"total_roles"`
	TotalPolicies         int            `json:"total_policies"`
	PermissionsByResource map[string]int `json:"permissions_by_resource"`
	PermissionsByAction   map[string]int `json:"permissions_by_action"`
	AuditEntries          int            `json:"audit_entries"`
	LastUpdated           time.Time      `json:"last_updated"`
	SecurityViolations    int            `json:"security_violations"`
}

// PermissionManagerImpl implements the PermissionManager interface
type PermissionManagerImpl struct {
	pluginPermissions map[string][]Permission
	pluginRoles       map[string][]string
	roles             map[string]Role
	policies          map[string]Policy
	auditLog          []AuditEntry
	stats             PermissionStats
	logger            common.Logger
	metrics           common.Metrics
	mu                sync.RWMutex
}

// NewPermissionManager creates a new permission manager
func NewPermissionManager(logger common.Logger, metrics common.Metrics) PermissionManager {
	return &PermissionManagerImpl{
		pluginPermissions: make(map[string][]Permission),
		pluginRoles:       make(map[string][]string),
		roles:             make(map[string]Role),
		policies:          make(map[string]Policy),
		auditLog:          make([]AuditEntry, 0),
		stats:             PermissionStats{},
		logger:            logger,
		metrics:           metrics,
	}
}

// GrantPermission grants a permission to a plugin
func (pm *PermissionManagerImpl) GrantPermission(ctx context.Context, pluginID string, permission Permission) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if permission already exists
	if permissions, exists := pm.pluginPermissions[pluginID]; exists {
		for _, p := range permissions {
			if p.Name == permission.Name {
				return fmt.Errorf("permission %s already granted to plugin %s", permission.Name, pluginID)
			}
		}
	}

	// Set grant metadata
	permission.GrantedAt = time.Now()
	permission.GrantedBy = pm.getGrantedBy(ctx)

	// Add permission
	pm.pluginPermissions[pluginID] = append(pm.pluginPermissions[pluginID], permission)

	// Log audit entry
	pm.logAuditEntry(AuditEntry{
		ID:        pm.generateAuditID(),
		PluginID:  pluginID,
		Action:    "grant_permission",
		Resource:  permission.Resource,
		Result:    "success",
		Reason:    fmt.Sprintf("Permission %s granted", permission.Name),
		Context:   map[string]interface{}{"permission": permission},
		Timestamp: time.Now(),
	})

	// Update stats
	pm.updateStats()

	pm.logger.Info("permission granted",
		logger.String("plugin_id", pluginID),
		logger.String("permission", permission.Name),
		logger.String("resource", permission.Resource),
		logger.String("action", permission.Action),
	)

	return nil
}

// RevokePermission revokes a permission from a plugin
func (pm *PermissionManagerImpl) RevokePermission(ctx context.Context, pluginID string, permissionName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	permissions, exists := pm.pluginPermissions[pluginID]
	if !exists {
		return fmt.Errorf("no permissions found for plugin %s", pluginID)
	}

	// Find and remove permission
	for i, p := range permissions {
		if p.Name == permissionName {
			pm.pluginPermissions[pluginID] = append(permissions[:i], permissions[i+1:]...)

			// Log audit entry
			pm.logAuditEntry(AuditEntry{
				ID:        pm.generateAuditID(),
				PluginID:  pluginID,
				Action:    "revoke_permission",
				Resource:  p.Resource,
				Result:    "success",
				Reason:    fmt.Sprintf("Permission %s revoked", permissionName),
				Context:   map[string]interface{}{"permission": p},
				Timestamp: time.Now(),
			})

			// Update stats
			pm.updateStats()

			pm.logger.Info("permission revoked",
				logger.String("plugin_id", pluginID),
				logger.String("permission", permissionName),
			)

			return nil
		}
	}

	return fmt.Errorf("permission %s not found for plugin %s", permissionName, pluginID)
}

// HasPermission checks if a plugin has a specific permission
func (pm *PermissionManagerImpl) HasPermission(ctx context.Context, pluginID string, permission Permission) (bool, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Check direct permissions
	if permissions, exists := pm.pluginPermissions[pluginID]; exists {
		for _, p := range permissions {
			if pm.permissionMatches(p, permission) {
				// Check if permission is expired
				if p.ExpiresAt != nil && time.Now().After(*p.ExpiresAt) {
					return false, nil
				}
				return true, nil
			}
		}
	}

	// Check role-based permissions
	if roles, exists := pm.pluginRoles[pluginID]; exists {
		for _, roleName := range roles {
			if role, exists := pm.roles[roleName]; exists {
				for _, p := range role.Permissions {
					if pm.permissionMatches(p, permission) {
						return true, nil
					}
				}
			}
		}
	}

	return false, nil
}

// GetPermissions returns all permissions for a plugin
func (pm *PermissionManagerImpl) GetPermissions(ctx context.Context, pluginID string) ([]Permission, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var allPermissions []Permission

	// Add direct permissions
	if permissions, exists := pm.pluginPermissions[pluginID]; exists {
		allPermissions = append(allPermissions, permissions...)
	}

	// Add role-based permissions
	if roles, exists := pm.pluginRoles[pluginID]; exists {
		for _, roleName := range roles {
			if role, exists := pm.roles[roleName]; exists {
				allPermissions = append(allPermissions, role.Permissions...)
			}
		}
	}

	return allPermissions, nil
}

// CheckPermission checks if a plugin has permission for a specific resource and action
func (pm *PermissionManagerImpl) CheckPermission(ctx context.Context, pluginID string, resource string, action string) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Create permission check
	permission := Permission{
		Resource: resource,
		Action:   action,
	}

	hasPermission, err := pm.HasPermission(ctx, pluginID, permission)
	if err != nil {
		return err
	}

	if !hasPermission {
		// Log audit entry
		pm.logAuditEntry(AuditEntry{
			ID:        pm.generateAuditID(),
			PluginID:  pluginID,
			Action:    "check_permission",
			Resource:  resource,
			Result:    "denied",
			Reason:    fmt.Sprintf("Plugin %s does not have permission for %s:%s", pluginID, resource, action),
			Context:   map[string]interface{}{"resource": resource, "action": action},
			Timestamp: time.Now(),
		})

		return fmt.Errorf("permission denied: plugin %s does not have permission for %s:%s", pluginID, resource, action)
	}

	return nil
}

// CheckMultiplePermissions checks multiple permissions at once
func (pm *PermissionManagerImpl) CheckMultiplePermissions(ctx context.Context, pluginID string, checks []PermissionCheck) error {
	for _, check := range checks {
		if err := pm.CheckPermission(ctx, pluginID, check.Resource, check.Action); err != nil {
			return err
		}
	}
	return nil
}

// CreateRole creates a new role
func (pm *PermissionManagerImpl) CreateRole(ctx context.Context, role Role) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.roles[role.Name]; exists {
		return fmt.Errorf("role %s already exists", role.Name)
	}

	role.CreatedAt = time.Now()
	role.UpdatedAt = time.Now()
	role.CreatedBy = pm.getGrantedBy(ctx)

	pm.roles[role.Name] = role

	// Update stats
	pm.updateStats()

	pm.logger.Info("role created",
		logger.String("role", role.Name),
		logger.String("description", role.Description),
		logger.Int("permissions", len(role.Permissions)),
	)

	return nil
}

// AssignRole assigns a role to a plugin
func (pm *PermissionManagerImpl) AssignRole(ctx context.Context, pluginID string, roleName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if role exists
	if _, exists := pm.roles[roleName]; !exists {
		return fmt.Errorf("role %s does not exist", roleName)
	}

	// Check if role already assigned
	if roles, exists := pm.pluginRoles[pluginID]; exists {
		for _, r := range roles {
			if r == roleName {
				return fmt.Errorf("role %s already assigned to plugin %s", roleName, pluginID)
			}
		}
	}

	// Assign role
	pm.pluginRoles[pluginID] = append(pm.pluginRoles[pluginID], roleName)

	// Log audit entry
	pm.logAuditEntry(AuditEntry{
		ID:        pm.generateAuditID(),
		PluginID:  pluginID,
		Action:    "assign_role",
		Resource:  roleName,
		Result:    "success",
		Reason:    fmt.Sprintf("Role %s assigned to plugin %s", roleName, pluginID),
		Context:   map[string]interface{}{"role": roleName},
		Timestamp: time.Now(),
	})

	pm.logger.Info("role assigned",
		logger.String("plugin_id", pluginID),
		logger.String("role", roleName),
	)

	return nil
}

// RemoveRole removes a role from a plugin
func (pm *PermissionManagerImpl) RemoveRole(ctx context.Context, pluginID string, roleName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	roles, exists := pm.pluginRoles[pluginID]
	if !exists {
		return fmt.Errorf("no roles found for plugin %s", pluginID)
	}

	// Find and remove role
	for i, r := range roles {
		if r == roleName {
			pm.pluginRoles[pluginID] = append(roles[:i], roles[i+1:]...)

			// Log audit entry
			pm.logAuditEntry(AuditEntry{
				ID:        pm.generateAuditID(),
				PluginID:  pluginID,
				Action:    "remove_role",
				Resource:  roleName,
				Result:    "success",
				Reason:    fmt.Sprintf("Role %s removed from plugin %s", roleName, pluginID),
				Context:   map[string]interface{}{"role": roleName},
				Timestamp: time.Now(),
			})

			pm.logger.Info("role removed",
				logger.String("plugin_id", pluginID),
				logger.String("role", roleName),
			)

			return nil
		}
	}

	return fmt.Errorf("role %s not found for plugin %s", roleName, pluginID)
}

// GetRoles returns all roles for a plugin
func (pm *PermissionManagerImpl) GetRoles(ctx context.Context, pluginID string) ([]Role, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var roles []Role
	if roleNames, exists := pm.pluginRoles[pluginID]; exists {
		for _, roleName := range roleNames {
			if role, exists := pm.roles[roleName]; exists {
				roles = append(roles, role)
			}
		}
	}

	return roles, nil
}

// CreatePolicy creates a new policy
func (pm *PermissionManagerImpl) CreatePolicy(ctx context.Context, policy Policy) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.policies[policy.Name]; exists {
		return fmt.Errorf("policy %s already exists", policy.Name)
	}

	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()
	policy.CreatedBy = pm.getGrantedBy(ctx)

	pm.policies[policy.Name] = policy

	// Update stats
	pm.updateStats()

	pm.logger.Info("policy created",
		logger.String("policy", policy.Name),
		logger.String("description", policy.Description),
		logger.String("version", policy.Version),
	)

	return nil
}

// UpdatePolicy updates an existing policy
func (pm *PermissionManagerImpl) UpdatePolicy(ctx context.Context, policy Policy) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.policies[policy.Name]; !exists {
		return fmt.Errorf("policy %s does not exist", policy.Name)
	}

	policy.UpdatedAt = time.Now()
	pm.policies[policy.Name] = policy

	pm.logger.Info("policy updated",
		logger.String("policy", policy.Name),
		logger.String("version", policy.Version),
	)

	return nil
}

// DeletePolicy deletes a policy
func (pm *PermissionManagerImpl) DeletePolicy(ctx context.Context, policyName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.policies[policyName]; !exists {
		return fmt.Errorf("policy %s does not exist", policyName)
	}

	delete(pm.policies, policyName)

	// Update stats
	pm.updateStats()

	pm.logger.Info("policy deleted",
		logger.String("policy", policyName),
	)

	return nil
}

// EvaluatePolicy evaluates a policy request
func (pm *PermissionManagerImpl) EvaluatePolicy(ctx context.Context, request PolicyRequest) (PolicyResult, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Evaluate all applicable policies
	var applicablePolicies []Policy
	for _, policy := range pm.policies {
		if pm.policyApplies(policy, request) {
			applicablePolicies = append(applicablePolicies, policy)
		}
	}

	// Determine decision based on policy priority and effects
	decision := PolicyDecisionDeny // Default deny
	var reason string
	var policyNames []string

	for _, policy := range applicablePolicies {
		policyNames = append(policyNames, policy.Name)

		for _, rule := range policy.Rules {
			if pm.ruleApplies(rule, request) {
				if rule.Effect == PolicyEffectAllow {
					decision = PolicyDecisionAllow
					reason = fmt.Sprintf("Allowed by policy %s, rule %s", policy.Name, rule.Name)
				} else if rule.Effect == PolicyEffectDeny {
					decision = PolicyDecisionDeny
					reason = fmt.Sprintf("Denied by policy %s, rule %s", policy.Name, rule.Name)
					break // Deny takes precedence
				}
			}
		}
	}

	if reason == "" {
		reason = "No applicable policies found"
	}

	result := PolicyResult{
		Decision:  decision,
		Reason:    reason,
		Policies:  policyNames,
		Context:   request.Context,
		Timestamp: time.Now(),
	}

	// Log audit entry
	pm.logAuditEntry(AuditEntry{
		ID:        pm.generateAuditID(),
		PluginID:  request.PluginID,
		Action:    "evaluate_policy",
		Resource:  request.Resource,
		Result:    string(decision),
		Reason:    reason,
		Context:   map[string]interface{}{"request": request, "result": result},
		Timestamp: time.Now(),
	})

	return result, nil
}

// GetAuditLog returns the audit log for a plugin
func (pm *PermissionManagerImpl) GetAuditLog(ctx context.Context, pluginID string) ([]AuditEntry, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var entries []AuditEntry
	for _, entry := range pm.auditLog {
		if entry.PluginID == pluginID {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// GetPermissionStats returns permission statistics
func (pm *PermissionManagerImpl) GetPermissionStats() PermissionStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pm.updateStats()
	return pm.stats
}

// Helper methods

func (pm *PermissionManagerImpl) permissionMatches(granted Permission, requested Permission) bool {
	// Check resource match
	if !pm.resourceMatches(granted.Resource, requested.Resource) {
		return false
	}

	// Check action match
	if !pm.actionMatches(granted.Action, requested.Action) {
		return false
	}

	// Check conditions
	if !pm.conditionsMatch(granted.Conditions, requested) {
		return false
	}

	return true
}

func (pm *PermissionManagerImpl) resourceMatches(granted string, requested string) bool {
	// Support wildcards
	if granted == "*" {
		return true
	}

	// Support prefix matching
	if strings.HasSuffix(granted, "*") {
		prefix := strings.TrimSuffix(granted, "*")
		return strings.HasPrefix(requested, prefix)
	}

	return granted == requested
}

func (pm *PermissionManagerImpl) actionMatches(granted string, requested string) bool {
	// Support wildcards
	if granted == "*" {
		return true
	}

	// Support prefix matching
	if strings.HasSuffix(granted, "*") {
		prefix := strings.TrimSuffix(granted, "*")
		return strings.HasPrefix(requested, prefix)
	}

	return granted == requested
}

func (pm *PermissionManagerImpl) conditionsMatch(conditions []PermissionCondition, requested Permission) bool {
	// For now, always return true
	// In a real implementation, this would evaluate conditions
	return true
}

func (pm *PermissionManagerImpl) policyApplies(policy Policy, request PolicyRequest) bool {
	// Check if policy applies to the request
	// This is a simplified implementation
	return true
}

func (pm *PermissionManagerImpl) ruleApplies(rule PermissionPolicyRule, request PolicyRequest) bool {
	// Check if rule applies to the request
	for _, resource := range rule.Resources {
		if pm.resourceMatches(resource, request.Resource) {
			for _, action := range rule.Actions {
				if pm.actionMatches(action, request.Action) {
					return true
				}
			}
		}
	}
	return false
}

func (pm *PermissionManagerImpl) logAuditEntry(entry AuditEntry) {
	pm.auditLog = append(pm.auditLog, entry)

	// Limit audit log size
	if len(pm.auditLog) > 10000 {
		pm.auditLog = pm.auditLog[1000:]
	}
}

func (pm *PermissionManagerImpl) generateAuditID() string {
	return fmt.Sprintf("audit_%d", time.Now().UnixNano())
}

func (pm *PermissionManagerImpl) getGrantedBy(ctx context.Context) string {
	// Extract user from context
	if userID := ctx.Value("user_id"); userID != nil {
		return userID.(string)
	}
	return "system"
}

func (pm *PermissionManagerImpl) updateStats() {
	pm.stats.TotalPermissions = 0
	pm.stats.ActivePermissions = 0
	pm.stats.ExpiredPermissions = 0
	pm.stats.PermissionsByResource = make(map[string]int)
	pm.stats.PermissionsByAction = make(map[string]int)

	now := time.Now()

	// Count plugin permissions
	for _, permissions := range pm.pluginPermissions {
		for _, p := range permissions {
			pm.stats.TotalPermissions++

			if p.ExpiresAt == nil || now.Before(*p.ExpiresAt) {
				pm.stats.ActivePermissions++
			} else {
				pm.stats.ExpiredPermissions++
			}

			pm.stats.PermissionsByResource[p.Resource]++
			pm.stats.PermissionsByAction[p.Action]++
		}
	}

	// Count role permissions
	for _, role := range pm.roles {
		for _, p := range role.Permissions {
			pm.stats.PermissionsByResource[p.Resource]++
			pm.stats.PermissionsByAction[p.Action]++
		}
	}

	pm.stats.TotalRoles = len(pm.roles)
	pm.stats.TotalPolicies = len(pm.policies)
	pm.stats.AuditEntries = len(pm.auditLog)
	pm.stats.LastUpdated = time.Now()
}
