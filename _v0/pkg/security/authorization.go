package security

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AuthzManager provides centralized authorization management
type AuthzManager struct {
	config      AuthzConfig
	policies    map[string]*Policy
	roles       map[string]*Role
	permissions map[string]*Permission
	mu          sync.RWMutex
}

// AuthzConfig contains authorization configuration
type AuthzConfig struct {
	EnableRBAC     bool          `yaml:"enable_rbac" default:"true"`
	EnableABAC     bool          `yaml:"enable_abac" default:"false"`
	EnablePolicies bool          `yaml:"enable_policies" default:"true"`
	PolicyEngine   string        `yaml:"policy_engine" default:"builtin"` // "builtin", "opa", "cedar"
	CacheTimeout   time.Duration `yaml:"cache_timeout" default:"5m"`
	MaxCacheSize   int           `yaml:"max_cache_size" default:"10000"`
	EnableAudit    bool          `yaml:"enable_audit" default:"true"`
	AuditRetention time.Duration `yaml:"audit_retention" default:"30d"`
}

// Policy represents an authorization policy
type Policy struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Rules       []Rule            `json:"rules"`
	Subjects    []string          `json:"subjects"`   // users, roles, groups
	Resources   []string          `json:"resources"`  // resources this policy applies to
	Actions     []string          `json:"actions"`    // actions this policy allows/denies
	Effect      string            `json:"effect"`     // "allow" or "deny"
	Conditions  map[string]string `json:"conditions"` // additional conditions
	Priority    int               `json:"priority"`
	Enabled     bool              `json:"enabled"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Rule represents a policy rule
type Rule struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`       // "rbac", "abac", "ip", "time", "custom"
	Expression string            `json:"expression"` // policy expression
	Conditions map[string]string `json:"conditions"`
	Effect     string            `json:"effect"` // "allow" or "deny"
	Priority   int               `json:"priority"`
	Enabled    bool              `json:"enabled"`
}

// Role represents a user role
type Role struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Permissions []string          `json:"permissions"`
	Inherits    []string          `json:"inherits"` // parent roles
	Attributes  map[string]string `json:"attributes"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Permission represents a permission
type Permission struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Resource    string            `json:"resource"`
	Action      string            `json:"action"`
	Attributes  map[string]string `json:"attributes"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// AuthzRequest represents an authorization request
type AuthzRequest struct {
	Subject    string            `json:"subject"`    // user ID
	Resource   string            `json:"resource"`   // resource being accessed
	Action     string            `json:"action"`     // action being performed
	Context    map[string]string `json:"context"`    // additional context
	Attributes map[string]string `json:"attributes"` // subject attributes
}

// AuthzResult represents the result of authorization
type AuthzResult struct {
	Allowed    bool              `json:"allowed"`
	Reason     string            `json:"reason"`
	Policy     string            `json:"policy,omitempty"`
	Rule       string            `json:"rule,omitempty"`
	Attributes map[string]string `json:"attributes"`
	Timestamp  time.Time         `json:"timestamp"`
}

// AuditEntry represents an audit log entry
type AuditEntry struct {
	ID        string            `json:"id"`
	Subject   string            `json:"subject"`
	Resource  string            `json:"resource"`
	Action    string            `json:"action"`
	Result    string            `json:"result"` // "allow" or "deny"
	Reason    string            `json:"reason"`
	Policy    string            `json:"policy,omitempty"`
	Rule      string            `json:"rule,omitempty"`
	Context   map[string]string `json:"context"`
	Timestamp time.Time         `json:"timestamp"`
	IP        string            `json:"ip"`
	UserAgent string            `json:"user_agent"`
}

// NewAuthzManager creates a new authorization manager
func NewAuthzManager(config AuthzConfig) *AuthzManager {
	return &AuthzManager{
		config:      config,
		policies:    make(map[string]*Policy),
		roles:       make(map[string]*Role),
		permissions: make(map[string]*Permission),
	}
}

// Authorize checks if a subject is authorized to perform an action on a resource
func (az *AuthzManager) Authorize(ctx context.Context, req AuthzRequest) (*AuthzResult, error) {
	az.mu.RLock()
	defer az.mu.RUnlock()

	// Check if authorization is enabled
	if !az.config.EnableRBAC && !az.config.EnableABAC && !az.config.EnablePolicies {
		return &AuthzResult{
			Allowed:   true,
			Reason:    "authorization disabled",
			Timestamp: time.Now(),
		}, nil
	}

	// Apply policies first (highest priority)
	if az.config.EnablePolicies {
		result, err := az.evaluatePolicies(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("policy evaluation failed: %w", err)
		}
		if result != nil {
			az.auditDecision(req, result)
			return result, nil
		}
	}

	// Apply RBAC
	if az.config.EnableRBAC {
		result, err := az.evaluateRBAC(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("RBAC evaluation failed: %w", err)
		}
		if result != nil {
			az.auditDecision(req, result)
			return result, nil
		}
	}

	// Apply ABAC
	if az.config.EnableABAC {
		result, err := az.evaluateABAC(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("ABAC evaluation failed: %w", err)
		}
		if result != nil {
			az.auditDecision(req, result)
			return result, nil
		}
	}

	// Default deny
	result := &AuthzResult{
		Allowed:   false,
		Reason:    "no matching policy found",
		Timestamp: time.Now(),
	}
	az.auditDecision(req, result)
	return result, nil
}

// evaluatePolicies evaluates policies for the request
func (az *AuthzManager) evaluatePolicies(ctx context.Context, req AuthzRequest) (*AuthzResult, error) {
	for _, policy := range az.policies {
		if !policy.Enabled {
			continue
		}

		// Check if policy applies to this request
		if !az.policyApplies(policy, req) {
			continue
		}

		// Evaluate policy rules
		for _, rule := range policy.Rules {
			if !rule.Enabled {
				continue
			}

			allowed, reason, err := az.evaluateRule(ctx, rule, req)
			if err != nil {
				return nil, fmt.Errorf("rule evaluation failed: %w", err)
			}

			if allowed {
				return &AuthzResult{
					Allowed:   true,
					Reason:    reason,
					Policy:    policy.ID,
					Rule:      rule.ID,
					Timestamp: time.Now(),
				}, nil
			}
		}
	}

	return nil, nil
}

// evaluateRBAC evaluates RBAC for the request
func (az *AuthzManager) evaluateRBAC(ctx context.Context, req AuthzRequest) (*AuthzResult, error) {
	// Get user roles from context or user service
	roles, err := az.getUserRoles(ctx, req.Subject)
	if err != nil {
		return nil, fmt.Errorf("failed to get user roles: %w", err)
	}

	// Check if any role has the required permission
	for _, roleName := range roles {
		role, exists := az.roles[roleName]
		if !exists {
			continue
		}

		// Check if role has permission for this resource and action
		permission := fmt.Sprintf("%s:%s", req.Resource, req.Action)
		for _, perm := range role.Permissions {
			if perm == permission || perm == "*" {
				return &AuthzResult{
					Allowed:   true,
					Reason:    fmt.Sprintf("role %s has permission", roleName),
					Timestamp: time.Now(),
				}, nil
			}
		}
	}

	return nil, nil
}

// evaluateABAC evaluates ABAC for the request
func (az *AuthzManager) evaluateABAC(ctx context.Context, req AuthzRequest) (*AuthzResult, error) {
	// ABAC evaluation would typically involve:
	// 1. Collecting subject attributes
	// 2. Collecting resource attributes
	// 3. Collecting environment attributes
	// 4. Evaluating attribute-based rules

	// For now, implement a simple attribute-based check
	if req.Attributes["department"] == "admin" {
		return &AuthzResult{
			Allowed:   true,
			Reason:    "admin department access",
			Timestamp: time.Now(),
		}, nil
	}

	return nil, nil
}

// policyApplies checks if a policy applies to the request
func (az *AuthzManager) policyApplies(policy *Policy, req AuthzRequest) bool {
	// Check subjects
	if len(policy.Subjects) > 0 {
		found := false
		for _, subject := range policy.Subjects {
			if subject == req.Subject || subject == "*" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check resources
	if len(policy.Resources) > 0 {
		found := false
		for _, resource := range policy.Resources {
			if resource == req.Resource || resource == "*" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check actions
	if len(policy.Actions) > 0 {
		found := false
		for _, action := range policy.Actions {
			if action == req.Action || action == "*" {
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

// evaluateRule evaluates a single rule
func (az *AuthzManager) evaluateRule(ctx context.Context, rule Rule, req AuthzRequest) (bool, string, error) {
	switch rule.Type {
	case "rbac":
		return az.evaluateRBACRule(ctx, rule, req)
	case "abac":
		return az.evaluateABACRule(ctx, rule, req)
	case "ip":
		return az.evaluateIPRule(ctx, rule, req)
	case "time":
		return az.evaluateTimeRule(ctx, rule, req)
	case "custom":
		return az.evaluateCustomRule(ctx, rule, req)
	default:
		return false, "unknown rule type", nil
	}
}

// evaluateRBACRule evaluates an RBAC rule
func (az *AuthzManager) evaluateRBACRule(ctx context.Context, rule Rule, req AuthzRequest) (bool, string, error) {
	// Simple RBAC rule evaluation
	// In a real implementation, this would be more sophisticated
	return rule.Effect == "allow", "RBAC rule evaluation", nil
}

// evaluateABACRule evaluates an ABAC rule
func (az *AuthzManager) evaluateABACRule(ctx context.Context, rule Rule, req AuthzRequest) (bool, string, error) {
	// Simple ABAC rule evaluation
	// In a real implementation, this would evaluate attribute conditions
	return rule.Effect == "allow", "ABAC rule evaluation", nil
}

// evaluateIPRule evaluates an IP-based rule
func (az *AuthzManager) evaluateIPRule(ctx context.Context, rule Rule, req AuthzRequest) (bool, string, error) {
	// Simple IP rule evaluation
	// In a real implementation, this would check IP ranges, CIDR blocks, etc.
	return rule.Effect == "allow", "IP rule evaluation", nil
}

// evaluateTimeRule evaluates a time-based rule
func (az *AuthzManager) evaluateTimeRule(ctx context.Context, rule Rule, req AuthzRequest) (bool, string, error) {
	// Simple time rule evaluation
	// In a real implementation, this would check time ranges, business hours, etc.
	return rule.Effect == "allow", "Time rule evaluation", nil
}

// evaluateCustomRule evaluates a custom rule
func (az *AuthzManager) evaluateCustomRule(ctx context.Context, rule Rule, req AuthzRequest) (bool, string, error) {
	// Custom rule evaluation would typically involve:
	// 1. Parsing the expression
	// 2. Evaluating conditions
	// 3. Returning the result
	return rule.Effect == "allow", "Custom rule evaluation", nil
}

// getUserRoles gets roles for a user
func (az *AuthzManager) getUserRoles(ctx context.Context, userID string) ([]string, error) {
	// In a real implementation, this would query a user service or database
	// For now, return a default role
	return []string{"user"}, nil
}

// auditDecision logs an authorization decision
func (az *AuthzManager) auditDecision(req AuthzRequest, result *AuthzResult) {
	if !az.config.EnableAudit {
		return
	}

	entry := &AuditEntry{
		ID:        generateID(),
		Subject:   req.Subject,
		Resource:  req.Resource,
		Action:    req.Action,
		Result:    result.Reason,
		Policy:    result.Policy,
		Rule:      result.Rule,
		Context:   req.Context,
		Timestamp: result.Timestamp,
	}

	// In a real implementation, this would write to an audit log
	_ = entry
}

// AddPolicy adds a new policy
func (az *AuthzManager) AddPolicy(policy *Policy) error {
	az.mu.Lock()
	defer az.mu.Unlock()

	if policy.ID == "" {
		policy.ID = generateID()
	}

	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()
	az.policies[policy.ID] = policy

	return nil
}

// RemovePolicy removes a policy
func (az *AuthzManager) RemovePolicy(policyID string) error {
	az.mu.Lock()
	defer az.mu.Unlock()

	delete(az.policies, policyID)
	return nil
}

// GetPolicy returns a policy by ID
func (az *AuthzManager) GetPolicy(policyID string) (*Policy, error) {
	az.mu.RLock()
	defer az.mu.RUnlock()

	policy, exists := az.policies[policyID]
	if !exists {
		return nil, errors.New("policy not found")
	}

	return policy, nil
}

// ListPolicies returns all policies
func (az *AuthzManager) ListPolicies() []*Policy {
	az.mu.RLock()
	defer az.mu.RUnlock()

	policies := make([]*Policy, 0, len(az.policies))
	for _, policy := range az.policies {
		policies = append(policies, policy)
	}

	return policies
}

// AddRole adds a new role
func (az *AuthzManager) AddRole(role *Role) error {
	az.mu.Lock()
	defer az.mu.Unlock()

	if role.ID == "" {
		role.ID = generateID()
	}

	role.CreatedAt = time.Now()
	role.UpdatedAt = time.Now()
	az.roles[role.ID] = role

	return nil
}

// RemoveRole removes a role
func (az *AuthzManager) RemoveRole(roleID string) error {
	az.mu.Lock()
	defer az.mu.Unlock()

	delete(az.roles, roleID)
	return nil
}

// GetRole returns a role by ID
func (az *AuthzManager) GetRole(roleID string) (*Role, error) {
	az.mu.RLock()
	defer az.mu.RUnlock()

	role, exists := az.roles[roleID]
	if !exists {
		return nil, errors.New("role not found")
	}

	return role, nil
}

// ListRoles returns all roles
func (az *AuthzManager) ListRoles() []*Role {
	az.mu.RLock()
	defer az.mu.RUnlock()

	roles := make([]*Role, 0, len(az.roles))
	for _, role := range az.roles {
		roles = append(roles, role)
	}

	return roles
}

// AddPermission adds a new permission
func (az *AuthzManager) AddPermission(permission *Permission) error {
	az.mu.Lock()
	defer az.mu.Unlock()

	if permission.ID == "" {
		permission.ID = generateID()
	}

	permission.CreatedAt = time.Now()
	permission.UpdatedAt = time.Now()
	az.permissions[permission.ID] = permission

	return nil
}

// RemovePermission removes a permission
func (az *AuthzManager) RemovePermission(permissionID string) error {
	az.mu.Lock()
	defer az.mu.Unlock()

	delete(az.permissions, permissionID)
	return nil
}

// GetPermission returns a permission by ID
func (az *AuthzManager) GetPermission(permissionID string) (*Permission, error) {
	az.mu.RLock()
	defer az.mu.RUnlock()

	permission, exists := az.permissions[permissionID]
	if !exists {
		return nil, errors.New("permission not found")
	}

	return permission, nil
}

// ListPermissions returns all permissions
func (az *AuthzManager) ListPermissions() []*Permission {
	az.mu.RLock()
	defer az.mu.RUnlock()

	permissions := make([]*Permission, 0, len(az.permissions))
	for _, permission := range az.permissions {
		permissions = append(permissions, permission)
	}

	return permissions
}

// generateID generates a unique ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
