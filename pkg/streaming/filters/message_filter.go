package filters

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	streaming "github.com/xraph/forge/pkg/streaming/core"
)

// FilterAction represents what action to take when a filter matches
type FilterAction string

const (
	FilterActionAllow      FilterAction = "allow"      // Allow the message through
	FilterActionBlock      FilterAction = "block"      // Block the message completely
	FilterActionModify     FilterAction = "modify"     // Modify the message content
	FilterActionTransform  FilterAction = "transform"  // Transform the message
	FilterActionFlag       FilterAction = "flag"       // Flag for review but allow through
	FilterActionQuarantine FilterAction = "quarantine" // Quarantine for admin review
)

// FilterResult represents the result of applying filters to a message
type FilterResult struct {
	Action          FilterAction           `json:"action"`
	Allowed         bool                   `json:"allowed"`
	Modified        bool                   `json:"modified"`
	OriginalMessage *streaming.Message     `json:"original_message,omitempty"`
	FilteredMessage *streaming.Message     `json:"filtered_message,omitempty"`
	MatchedFilters  []string               `json:"matched_filters"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	Reason          string                 `json:"reason,omitempty"`
}

// MessageFilter represents a filter that can be applied to messages
type MessageFilter interface {
	// Name returns the filter name
	Name() string

	// Priority returns the filter priority (lower numbers = higher priority)
	Priority() int

	// Apply applies the filter to a message and returns the result
	Apply(ctx context.Context, message *streaming.Message, context *FilterContext) *FilterResult

	// IsEnabled returns true if the filter is enabled
	IsEnabled() bool

	// SetEnabled enables or disables the filter
	SetEnabled(enabled bool)

	// GetConfig returns the filter configuration
	GetConfig() map[string]interface{}

	// UpdateConfig updates the filter configuration
	UpdateConfig(config map[string]interface{}) error
}

// FilterContext provides context for filter operations
type FilterContext struct {
	RoomID       string                 `json:"room_id"`
	UserID       string                 `json:"user_id"`
	ConnectionID string                 `json:"connection_id"`
	UserRoles    []string               `json:"user_roles,omitempty"`
	UserMetadata map[string]interface{} `json:"user_metadata,omitempty"`
	RoomMetadata map[string]interface{} `json:"room_metadata,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
}

// MessageFilterManager manages multiple message filters
type MessageFilterManager interface {
	// Filter management
	AddFilter(filter MessageFilter) error
	RemoveFilter(name string) error
	GetFilter(name string) (MessageFilter, error)
	GetFilters() []MessageFilter
	EnableFilter(name string) error
	DisableFilter(name string) error

	// Apply filters to a message
	ApplyFilters(ctx context.Context, message *streaming.Message, context *FilterContext) *FilterResult

	// Batch operations
	ApplyFiltersToMessages(ctx context.Context, messages []*streaming.Message, context *FilterContext) []*FilterResult

	// Configuration
	UpdateFilterConfig(name string, config map[string]interface{}) error
	GetFilterStats() map[string]*FilterStats

	// Presets and templates
	AddFilterPreset(name string, filters []MessageFilter) error
	ApplyFilterPreset(name string) error
	GetFilterPresets() map[string][]string
}

// FilterStats represents statistics for a filter
type FilterStats struct {
	Name           string        `json:"name"`
	AppliedCount   int64         `json:"applied_count"`
	MatchCount     int64         `json:"match_count"`
	BlockCount     int64         `json:"block_count"`
	ModifyCount    int64         `json:"modify_count"`
	ErrorCount     int64         `json:"error_count"`
	AverageLatency time.Duration `json:"average_latency"`
	LastApplied    time.Time     `json:"last_applied"`
}

// DefaultMessageFilterManager implements MessageFilterManager
type DefaultMessageFilterManager struct {
	filters     map[string]MessageFilter
	filterStats map[string]*FilterStats
	presets     map[string][]string
	mu          sync.RWMutex
	logger      common.Logger
	metrics     common.Metrics
}

// NewDefaultMessageFilterManager creates a new default message filter manager
func NewDefaultMessageFilterManager(logger common.Logger, metrics common.Metrics) MessageFilterManager {
	return &DefaultMessageFilterManager{
		filters:     make(map[string]MessageFilter),
		filterStats: make(map[string]*FilterStats),
		presets:     make(map[string][]string),
		logger:      logger,
		metrics:     metrics,
	}
}

// AddFilter adds a filter to the manager
func (fm *DefaultMessageFilterManager) AddFilter(filter MessageFilter) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	name := filter.Name()
	if _, exists := fm.filters[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	fm.filters[name] = filter
	fm.filterStats[name] = &FilterStats{
		Name: name,
	}

	if fm.logger != nil {
		fm.logger.Info("message filter added",
			logger.String("filter", name),
			logger.Int("priority", filter.Priority()),
			logger.Bool("enabled", filter.IsEnabled()),
		)
	}

	if fm.metrics != nil {
		fm.metrics.Counter("streaming.filters.added").Inc()
		fm.metrics.Gauge("streaming.filters.count").Set(float64(len(fm.filters)))
	}

	return nil
}

// RemoveFilter removes a filter from the manager
func (fm *DefaultMessageFilterManager) RemoveFilter(name string) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if _, exists := fm.filters[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(fm.filters, name)
	delete(fm.filterStats, name)

	if fm.logger != nil {
		fm.logger.Info("message filter removed", logger.String("filter", name))
	}

	if fm.metrics != nil {
		fm.metrics.Counter("streaming.filters.removed").Inc()
		fm.metrics.Gauge("streaming.filters.count").Set(float64(len(fm.filters)))
	}

	return nil
}

// GetFilter gets a filter by name
func (fm *DefaultMessageFilterManager) GetFilter(name string) (MessageFilter, error) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	filter, exists := fm.filters[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return filter, nil
}

// GetFilters returns all filters sorted by priority
func (fm *DefaultMessageFilterManager) GetFilters() []MessageFilter {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	filters := make([]MessageFilter, 0, len(fm.filters))
	for _, filter := range fm.filters {
		filters = append(filters, filter)
	}

	// Sort by priority (lower numbers = higher priority)
	for i := 0; i < len(filters); i++ {
		for j := i + 1; j < len(filters); j++ {
			if filters[i].Priority() > filters[j].Priority() {
				filters[i], filters[j] = filters[j], filters[i]
			}
		}
	}

	return filters
}

// ApplyFilters applies all enabled filters to a message
func (fm *DefaultMessageFilterManager) ApplyFilters(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *FilterResult {
	filters := fm.GetFilters()

	result := &FilterResult{
		Action:          FilterActionAllow,
		Allowed:         true,
		Modified:        false,
		OriginalMessage: message,
		FilteredMessage: fm.cloneMessage(message),
		MatchedFilters:  make([]string, 0),
		Metadata:        make(map[string]interface{}),
	}

	for _, filter := range filters {
		if !filter.IsEnabled() {
			continue
		}

		start := time.Now()
		filterResult := filter.Apply(ctx, result.FilteredMessage, filterContext)
		latency := time.Since(start)

		// Update filter statistics
		fm.updateFilterStats(filter.Name(), filterResult != nil, latency)

		if filterResult == nil {
			continue
		}

		// Record matched filter
		result.MatchedFilters = append(result.MatchedFilters, filter.Name())

		// Process filter result
		switch filterResult.Action {
		case FilterActionBlock:
			result.Action = FilterActionBlock
			result.Allowed = false
			result.Reason = filterResult.Reason
			if fm.metrics != nil {
				fm.metrics.Counter("streaming.messages.blocked", "filter", filter.Name()).Inc()
			}
			return result // OnStop processing on block

		case FilterActionModify:
			if filterResult.FilteredMessage != nil {
				result.FilteredMessage = filterResult.FilteredMessage
				result.Modified = true
			}
			if fm.metrics != nil {
				fm.metrics.Counter("streaming.messages.modified", "filter", filter.Name()).Inc()
			}

		case FilterActionTransform:
			if filterResult.FilteredMessage != nil {
				result.FilteredMessage = filterResult.FilteredMessage
				result.Modified = true
			}
			if fm.metrics != nil {
				fm.metrics.Counter("streaming.messages.transformed", "filter", filter.Name()).Inc()
			}

		case FilterActionFlag:
			// Add flag metadata but continue processing
			result.Metadata["flagged"] = true
			result.Metadata["flag_reason"] = filterResult.Reason
			result.Metadata["flagged_by"] = filter.Name()
			if fm.metrics != nil {
				fm.metrics.Counter("streaming.messages.flagged", "filter", filter.Name()).Inc()
			}

		case FilterActionQuarantine:
			result.Action = FilterActionQuarantine
			result.Allowed = false
			result.Reason = filterResult.Reason
			result.Metadata["quarantined"] = true
			result.Metadata["quarantine_reason"] = filterResult.Reason
			if fm.metrics != nil {
				fm.metrics.Counter("streaming.messages.quarantined", "filter", filter.Name()).Inc()
			}
			return result // OnStop processing on quarantine

		case FilterActionAllow:
			// Continue processing
			continue
		}

		// Merge filter metadata
		for k, v := range filterResult.Metadata {
			result.Metadata[k] = v
		}
	}

	if fm.metrics != nil {
		if result.Allowed {
			fm.metrics.Counter("streaming.messages.allowed").Inc()
		}
		if result.Modified {
			fm.metrics.Counter("streaming.messages.total_modified").Inc()
		}
	}

	return result
}

// ApplyFiltersToMessages applies filters to multiple messages
func (fm *DefaultMessageFilterManager) ApplyFiltersToMessages(ctx context.Context, messages []*streaming.Message, filterContext *FilterContext) []*FilterResult {
	results := make([]*FilterResult, len(messages))

	for i, message := range messages {
		results[i] = fm.ApplyFilters(ctx, message, filterContext)
	}

	return results
}

// EnableFilter enables a filter
func (fm *DefaultMessageFilterManager) EnableFilter(name string) error {
	filter, err := fm.GetFilter(name)
	if err != nil {
		return err
	}

	filter.SetEnabled(true)

	if fm.logger != nil {
		fm.logger.Info("filter enabled", logger.String("filter", name))
	}

	return nil
}

// DisableFilter disables a filter
func (fm *DefaultMessageFilterManager) DisableFilter(name string) error {
	filter, err := fm.GetFilter(name)
	if err != nil {
		return err
	}

	filter.SetEnabled(false)

	if fm.logger != nil {
		fm.logger.Info("filter disabled", logger.String("filter", name))
	}

	return nil
}

// UpdateFilterConfig updates a filter's configuration
func (fm *DefaultMessageFilterManager) UpdateFilterConfig(name string, config map[string]interface{}) error {
	filter, err := fm.GetFilter(name)
	if err != nil {
		return err
	}

	if err := filter.UpdateConfig(config); err != nil {
		return fmt.Errorf("failed to update filter config: %w", err)
	}

	if fm.logger != nil {
		fm.logger.Info("filter config updated", logger.String("filter", name))
	}

	return nil
}

// GetFilterStats returns statistics for all filters
func (fm *DefaultMessageFilterManager) GetFilterStats() map[string]*FilterStats {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	stats := make(map[string]*FilterStats)
	for name, stat := range fm.filterStats {
		// Return copies to avoid race conditions
		statCopy := *stat
		stats[name] = &statCopy
	}

	return stats
}

// AddFilterPreset adds a filter preset
func (fm *DefaultMessageFilterManager) AddFilterPreset(name string, filters []MessageFilter) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	filterNames := make([]string, len(filters))
	for i, filter := range filters {
		filterNames[i] = filter.Name()
		// Add filter if not already present
		if _, exists := fm.filters[filter.Name()]; !exists {
			fm.filters[filter.Name()] = filter
			fm.filterStats[filter.Name()] = &FilterStats{
				Name: filter.Name(),
			}
		}
	}

	fm.presets[name] = filterNames

	if fm.logger != nil {
		fm.logger.Info("filter preset added",
			logger.String("preset", name),
			logger.Int("filter_count", len(filters)),
		)
	}

	return nil
}

// ApplyFilterPreset applies a filter preset
func (fm *DefaultMessageFilterManager) ApplyFilterPreset(name string) error {
	fm.mu.RLock()
	filterNames, exists := fm.presets[name]
	fm.mu.RUnlock()

	if !exists {
		return common.ErrServiceNotFound(name)
	}

	// Enable all filters in the preset
	for _, filterName := range filterNames {
		if err := fm.EnableFilter(filterName); err != nil {
			return fmt.Errorf("failed to enable filter %s in preset %s: %w", filterName, name, err)
		}
	}

	if fm.logger != nil {
		fm.logger.Info("filter preset applied",
			logger.String("preset", name),
			logger.Int("filter_count", len(filterNames)),
		)
	}

	return nil
}

// GetFilterPresets returns all filter presets
func (fm *DefaultMessageFilterManager) GetFilterPresets() map[string][]string {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	presets := make(map[string][]string)
	for name, filters := range fm.presets {
		// Return copies to avoid race conditions
		filtersCopy := make([]string, len(filters))
		copy(filtersCopy, filters)
		presets[name] = filtersCopy
	}

	return presets
}

// Helper methods

// cloneMessage creates a deep copy of a message
func (fm *DefaultMessageFilterManager) cloneMessage(message *streaming.Message) *streaming.Message {
	clone := &streaming.Message{
		ID:        message.ID,
		Type:      message.Type,
		From:      message.From,
		To:        message.To,
		RoomID:    message.RoomID,
		Data:      message.Data, // Shallow copy - might need deep copy depending on data types
		Timestamp: message.Timestamp,
		TTL:       message.TTL,
		Metadata:  make(map[string]interface{}),
	}

	// Copy metadata
	for k, v := range message.Metadata {
		clone.Metadata[k] = v
	}

	return clone
}

// updateFilterStats updates statistics for a filter
func (fm *DefaultMessageFilterManager) updateFilterStats(filterName string, matched bool, latency time.Duration) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	stats := fm.filterStats[filterName]
	if stats == nil {
		return
	}

	stats.AppliedCount++
	stats.LastApplied = time.Now()

	if matched {
		stats.MatchCount++
	}

	// Update average latency
	if stats.AppliedCount == 1 {
		stats.AverageLatency = latency
	} else {
		total := time.Duration(stats.AppliedCount-1) * stats.AverageLatency
		stats.AverageLatency = (total + latency) / time.Duration(stats.AppliedCount)
	}
}

// Built-in Filter Implementations

// BaseFilter provides common functionality for filters
type BaseFilter struct {
	name     string
	priority int
	enabled  bool
	config   map[string]interface{}
	mu       sync.RWMutex
}

// NewBaseFilter creates a new base filter
func NewBaseFilter(name string, priority int) *BaseFilter {
	return &BaseFilter{
		name:     name,
		priority: priority,
		enabled:  true,
		config:   make(map[string]interface{}),
	}
}

// Name returns the filter name
func (bf *BaseFilter) Name() string {
	return bf.name
}

// Priority returns the filter priority
func (bf *BaseFilter) Priority() int {
	return bf.priority
}

// IsEnabled returns true if the filter is enabled
func (bf *BaseFilter) IsEnabled() bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return bf.enabled
}

// SetEnabled enables or disables the filter
func (bf *BaseFilter) SetEnabled(enabled bool) {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.enabled = enabled
}

// GetConfig returns the filter configuration
func (bf *BaseFilter) GetConfig() map[string]interface{} {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	config := make(map[string]interface{})
	for k, v := range bf.config {
		config[k] = v
	}
	return config
}

// UpdateConfig updates the filter configuration
func (bf *BaseFilter) UpdateConfig(config map[string]interface{}) error {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for k, v := range config {
		bf.config[k] = v
	}

	return nil
}

// ProfanityFilter filters out profanity
type ProfanityFilter struct {
	*BaseFilter
	words       []string
	replacement string
	regex       *regexp.Regexp
}

// NewProfanityFilter creates a new profanity filter
func NewProfanityFilter(words []string, replacement string) *ProfanityFilter {
	filter := &ProfanityFilter{
		BaseFilter:  NewBaseFilter("profanity", 10),
		words:       words,
		replacement: replacement,
	}

	// Build regex pattern
	if len(words) > 0 {
		pattern := `\b(` + strings.Join(words, "|") + `)\b`
		filter.regex = regexp.MustCompile(`(?i)` + pattern)
	}

	return filter
}

// Apply applies the profanity filter
func (pf *ProfanityFilter) Apply(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *FilterResult {
	if pf.regex == nil {
		return nil
	}

	// Check if message data contains text
	data, ok := message.Data.(map[string]interface{})
	if !ok {
		return nil
	}

	text, ok := data["text"].(string)
	if !ok {
		return nil
	}

	// Check for profanity
	if pf.regex.MatchString(text) {
		// Replace profanity
		cleanText := pf.regex.ReplaceAllString(text, pf.replacement)

		// Create modified message
		modifiedData := make(map[string]interface{})
		for k, v := range data {
			modifiedData[k] = v
		}
		modifiedData["text"] = cleanText

		modifiedMessage := &streaming.Message{
			ID:        message.ID,
			Type:      message.Type,
			From:      message.From,
			To:        message.To,
			RoomID:    message.RoomID,
			Data:      modifiedData,
			Metadata:  message.Metadata,
			Timestamp: message.Timestamp,
			TTL:       message.TTL,
		}

		return &FilterResult{
			Action:          FilterActionModify,
			Allowed:         true,
			Modified:        true,
			FilteredMessage: modifiedMessage,
			Reason:          "profanity detected and replaced",
			Metadata: map[string]interface{}{
				"profanity_detected": true,
				"original_text":      text,
			},
		}
	}

	return nil
}

// SpamFilter detects and blocks spam messages
type SpamFilter struct {
	*BaseFilter
	maxLength      int
	maxRepeated    int
	bannedPatterns []*regexp.Regexp
}

// NewSpamFilter creates a new spam filter
func NewSpamFilter(maxLength, maxRepeated int, bannedPatterns []string) *SpamFilter {
	filter := &SpamFilter{
		BaseFilter:  NewBaseFilter("spam", 5),
		maxLength:   maxLength,
		maxRepeated: maxRepeated,
	}

	// Compile regex patterns
	for _, pattern := range bannedPatterns {
		if regex, err := regexp.Compile(pattern); err == nil {
			filter.bannedPatterns = append(filter.bannedPatterns, regex)
		}
	}

	return filter
}

// Apply applies the spam filter
func (sf *SpamFilter) Apply(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *FilterResult {
	// Check if message data contains text
	data, ok := message.Data.(map[string]interface{})
	if !ok {
		return nil
	}

	text, ok := data["text"].(string)
	if !ok {
		return nil
	}

	// Check message length
	if len(text) > sf.maxLength {
		return &FilterResult{
			Action:  FilterActionBlock,
			Allowed: false,
			Reason:  fmt.Sprintf("message too long (%d characters, max %d)", len(text), sf.maxLength),
		}
	}

	// Check for repeated characters
	if sf.hasRepeatedCharacters(text, sf.maxRepeated) {
		return &FilterResult{
			Action:  FilterActionBlock,
			Allowed: false,
			Reason:  "excessive repeated characters detected",
		}
	}

	// Check banned patterns
	for _, pattern := range sf.bannedPatterns {
		if pattern.MatchString(text) {
			return &FilterResult{
				Action:  FilterActionBlock,
				Allowed: false,
				Reason:  "message matches banned pattern",
			}
		}
	}

	return nil
}

// hasRepeatedCharacters checks if text has too many repeated characters
func (sf *SpamFilter) hasRepeatedCharacters(text string, maxRepeated int) bool {
	if len(text) == 0 || maxRepeated <= 0 {
		return false
	}

	count := 1
	for i := 1; i < len(text); i++ {
		if text[i] == text[i-1] {
			count++
			if count > maxRepeated {
				return true
			}
		} else {
			count = 1
		}
	}

	return false
}

// PermissionFilter checks user permissions
type PermissionFilter struct {
	*BaseFilter
	requiredRoles       []string
	blockedRoles        []string
	adminBypass         bool
	requireVerification bool
}

// NewPermissionFilter creates a new permission filter
func NewPermissionFilter(requiredRoles, blockedRoles []string, adminBypass, requireVerification bool) *PermissionFilter {
	return &PermissionFilter{
		BaseFilter:          NewBaseFilter("permission", 1),
		requiredRoles:       requiredRoles,
		blockedRoles:        blockedRoles,
		adminBypass:         adminBypass,
		requireVerification: requireVerification,
	}
}

// Apply applies the permission filter
func (pf *PermissionFilter) Apply(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *FilterResult {
	// Check if user has admin bypass
	if pf.adminBypass && pf.hasRole(filterContext.UserRoles, "admin") {
		return nil
	}

	// Check blocked roles
	for _, blockedRole := range pf.blockedRoles {
		if pf.hasRole(filterContext.UserRoles, blockedRole) {
			return &FilterResult{
				Action:  FilterActionBlock,
				Allowed: false,
				Reason:  fmt.Sprintf("user has blocked role: %s", blockedRole),
			}
		}
	}

	// Check required roles
	if len(pf.requiredRoles) > 0 {
		hasRequired := false
		for _, requiredRole := range pf.requiredRoles {
			if pf.hasRole(filterContext.UserRoles, requiredRole) {
				hasRequired = true
				break
			}
		}

		if !hasRequired {
			return &FilterResult{
				Action:  FilterActionBlock,
				Allowed: false,
				Reason:  "user does not have required role",
			}
		}
	}

	// Check verification requirement
	if pf.requireVerification {
		if verified, ok := filterContext.UserMetadata["verified"].(bool); !ok || !verified {
			return &FilterResult{
				Action:  FilterActionBlock,
				Allowed: false,
				Reason:  "user is not verified",
			}
		}
	}

	return nil
}

// hasRole checks if user has a specific role
func (pf *PermissionFilter) hasRole(userRoles []string, role string) bool {
	for _, userRole := range userRoles {
		if userRole == role {
			return true
		}
	}
	return false
}

// FilterAction helpers

// IsTerminal returns true if the action stops further processing
func (fa FilterAction) IsTerminal() bool {
	return fa == FilterActionBlock || fa == FilterActionQuarantine
}

// String returns the string representation of the filter action
func (fa FilterAction) String() string {
	return string(fa)
}
