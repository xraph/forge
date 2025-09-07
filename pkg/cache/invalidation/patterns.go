package invalidation

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// PatternMatcher handles pattern-based invalidation
type PatternMatcher struct {
	patterns      map[string]*CompiledPattern
	wildcardCache map[string]*WildcardPattern
	regexCache    map[string]*regexp.Regexp
	tagPatterns   map[string][]string
	logger        common.Logger
	metrics       common.Metrics
	mu            sync.RWMutex
	cacheTimeout  time.Duration
	maxCacheSize  int
}

// CompiledPattern represents a compiled invalidation pattern
type CompiledPattern struct {
	Original   cachecore.InvalidationPattern `json:"original"`
	Type       PatternType                   `json:"type"`
	Compiled   interface{}                   `json:"-"`
	Tags       []string                      `json:"tags"`
	Enabled    bool                          `json:"enabled"`
	Priority   int                           `json:"priority"`
	CreatedAt  time.Time                     `json:"created_at"`
	LastUsed   time.Time                     `json:"last_used"`
	MatchCount int64                         `json:"match_count"`
	FailCount  int64                         `json:"fail_count"`
}

// WildcardPattern represents a wildcard pattern
type WildcardPattern struct {
	Pattern       string    `json:"pattern"`
	Prefix        string    `json:"prefix"`
	Suffix        string    `json:"suffix"`
	HasWildcard   bool      `json:"has_wildcard"`
	CaseSensitive bool      `json:"case_sensitive"`
	CompiledAt    time.Time `json:"compiled_at"`
}

// PatternType represents the type of pattern
type PatternType string

const (
	PatternTypeExact    PatternType = "exact"
	PatternTypeWildcard PatternType = "wildcard"
	PatternTypeRegex    PatternType = "regex"
	PatternTypePrefix   PatternType = "prefix"
	PatternTypeSuffix   PatternType = "suffix"
	PatternTypeContains PatternType = "contains"
	PatternTypeTag      PatternType = "tag"
)

// PatternMatchResult represents the result of pattern matching
type PatternMatchResult struct {
	Matched     bool          `json:"matched"`
	Pattern     string        `json:"pattern"`
	Type        PatternType   `json:"type"`
	MatchedKeys []string      `json:"matched_keys"`
	Duration    time.Duration `json:"duration"`
	Error       error         `json:"error"`
}

// PatternStats contains statistics for pattern matching
type PatternStats struct {
	TotalPatterns     int                  `json:"total_patterns"`
	EnabledPatterns   int                  `json:"enabled_patterns"`
	CachedWildcards   int                  `json:"cached_wildcards"`
	CachedRegex       int                  `json:"cached_regex"`
	MatchesPerformed  int64                `json:"matches_performed"`
	MatchesSuccessful int64                `json:"matches_successful"`
	AverageMatchTime  time.Duration        `json:"average_match_time"`
	PatternBreakdown  map[PatternType]int  `json:"pattern_breakdown"`
	MostUsedPatterns  []string             `json:"most_used_patterns"`
	RecentMatches     []PatternMatchResult `json:"recent_matches"`
}

// NewPatternMatcher creates a new pattern matcher
func NewPatternMatcher(logger common.Logger, metrics common.Metrics) *PatternMatcher {
	return &PatternMatcher{
		patterns:      make(map[string]*CompiledPattern),
		wildcardCache: make(map[string]*WildcardPattern),
		regexCache:    make(map[string]*regexp.Regexp),
		tagPatterns:   make(map[string][]string),
		logger:        logger,
		metrics:       metrics,
		cacheTimeout:  time.Hour,
		maxCacheSize:  1000,
	}
}

// AddPattern adds a new invalidation pattern
func (pm *PatternMatcher) AddPattern(pattern cachecore.InvalidationPattern) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	compiled, err := pm.compilePattern(pattern)
	if err != nil {
		return fmt.Errorf("failed to compile pattern %s: %w", pattern.Name, err)
	}

	pm.patterns[pattern.Name] = compiled

	// Index tag patterns
	if len(pattern.Tags) > 0 {
		for _, tag := range pattern.Tags {
			pm.tagPatterns[tag] = append(pm.tagPatterns[tag], pattern.Name)
		}
	}

	pm.logger.Info("invalidation pattern added",
		logger.String("name", pattern.Name),
		logger.String("pattern", pattern.Pattern),
		logger.String("type", string(compiled.Type)),
		logger.Bool("enabled", pattern.Enabled),
	)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.cache.invalidation.patterns.added").Inc()
		pm.metrics.Gauge("forge.cache.invalidation.patterns.total").Set(float64(len(pm.patterns)))
	}

	return nil
}

// RemovePattern removes an invalidation pattern
func (pm *PatternMatcher) RemovePattern(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pattern, exists := pm.patterns[name]
	if !exists {
		return fmt.Errorf("pattern %s not found", name)
	}

	// Remove from tag index
	for _, tag := range pattern.Tags {
		if tagPatterns, exists := pm.tagPatterns[tag]; exists {
			for i, patternName := range tagPatterns {
				if patternName == name {
					pm.tagPatterns[tag] = append(tagPatterns[:i], tagPatterns[i+1:]...)
					break
				}
			}
			if len(pm.tagPatterns[tag]) == 0 {
				delete(pm.tagPatterns, tag)
			}
		}
	}

	delete(pm.patterns, name)

	pm.logger.Info("invalidation pattern removed", logger.String("name", name))

	if pm.metrics != nil {
		pm.metrics.Counter("forge.cache.invalidation.patterns.removed").Inc()
		pm.metrics.Gauge("forge.cache.invalidation.patterns.total").Set(float64(len(pm.patterns)))
	}

	return nil
}

// MatchKey checks if a key matches any patterns
func (pm *PatternMatcher) MatchKey(key string) []PatternMatchResult {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var results []PatternMatchResult
	start := time.Now()

	for name, pattern := range pm.patterns {
		if !pattern.Enabled {
			continue
		}

		result := pm.matchPattern(key, pattern)
		result.Pattern = name

		if result.Matched {
			results = append(results, result)
			pattern.MatchCount++
			pattern.LastUsed = time.Now()
		} else if result.Error != nil {
			pattern.FailCount++
		}
	}

	totalDuration := time.Since(start)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.cache.invalidation.patterns.matches").Inc()
		pm.metrics.Histogram("forge.cache.invalidation.patterns.match_duration").Observe(totalDuration.Seconds())

		if len(results) > 0 {
			pm.metrics.Counter("forge.cache.invalidation.patterns.matches.successful").Inc()
		}
	}

	return results
}

// MatchKeys checks if any keys match any patterns
func (pm *PatternMatcher) MatchKeys(keys []string) map[string][]PatternMatchResult {
	results := make(map[string][]PatternMatchResult)

	for _, key := range keys {
		matches := pm.MatchKey(key)
		if len(matches) > 0 {
			results[key] = matches
		}
	}

	return results
}

// MatchByTag finds patterns by tag
func (pm *PatternMatcher) MatchByTag(tag string) []*CompiledPattern {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var results []*CompiledPattern

	if patternNames, exists := pm.tagPatterns[tag]; exists {
		for _, name := range patternNames {
			if pattern, exists := pm.patterns[name]; exists && pattern.Enabled {
				results = append(results, pattern)
			}
		}
	}

	return results
}

// GetPattern returns a pattern by name
func (pm *PatternMatcher) GetPattern(name string) (*CompiledPattern, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pattern, exists := pm.patterns[name]
	if !exists {
		return nil, fmt.Errorf("pattern %s not found", name)
	}

	return pattern, nil
}

// ListPatterns returns all patterns
func (pm *PatternMatcher) ListPatterns() []*CompiledPattern {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	patterns := make([]*CompiledPattern, 0, len(pm.patterns))
	for _, pattern := range pm.patterns {
		patterns = append(patterns, pattern)
	}

	return patterns
}

// EnablePattern enables a pattern
func (pm *PatternMatcher) EnablePattern(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pattern, exists := pm.patterns[name]
	if !exists {
		return fmt.Errorf("pattern %s not found", name)
	}

	pattern.Enabled = true
	pm.logger.Info("pattern enabled", logger.String("name", name))

	return nil
}

// DisablePattern disables a pattern
func (pm *PatternMatcher) DisablePattern(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pattern, exists := pm.patterns[name]
	if !exists {
		return fmt.Errorf("pattern %s not found", name)
	}

	pattern.Enabled = false
	pm.logger.Info("pattern disabled", logger.String("name", name))

	return nil
}

// GetStats returns pattern matching statistics
func (pm *PatternMatcher) GetStats() PatternStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := PatternStats{
		TotalPatterns:    len(pm.patterns),
		CachedWildcards:  len(pm.wildcardCache),
		CachedRegex:      len(pm.regexCache),
		PatternBreakdown: make(map[PatternType]int),
		MostUsedPatterns: make([]string, 0),
	}

	var enabledCount int
	var totalMatches, successfulMatches int64
	var totalMatchTime time.Duration

	for name, pattern := range pm.patterns {
		if pattern.Enabled {
			enabledCount++
		}

		stats.PatternBreakdown[pattern.Type]++
		totalMatches += pattern.MatchCount + pattern.FailCount
		successfulMatches += pattern.MatchCount

		// Find most used patterns (simplified)
		if pattern.MatchCount > 10 {
			stats.MostUsedPatterns = append(stats.MostUsedPatterns, name)
		}
	}

	stats.EnabledPatterns = enabledCount
	stats.MatchesPerformed = totalMatches
	stats.MatchesSuccessful = successfulMatches

	if totalMatches > 0 {
		stats.AverageMatchTime = totalMatchTime / time.Duration(totalMatches)
	}

	return stats
}

// ClearCache clears pattern caches
func (pm *PatternMatcher) ClearCache() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.wildcardCache = make(map[string]*WildcardPattern)
	pm.regexCache = make(map[string]*regexp.Regexp)

	pm.logger.Info("pattern caches cleared")

	if pm.metrics != nil {
		pm.metrics.Counter("forge.cache.invalidation.patterns.cache.cleared").Inc()
	}
}

// compilePattern compiles a pattern for efficient matching
func (pm *PatternMatcher) compilePattern(pattern cachecore.InvalidationPattern) (*CompiledPattern, error) {
	compiled := &CompiledPattern{
		Original:  pattern,
		Tags:      pattern.Tags,
		Enabled:   pattern.Enabled,
		CreatedAt: time.Now(),
	}

	patternStr := pattern.Pattern

	// Determine pattern type and compile accordingly
	switch {
	case strings.Contains(patternStr, "*") || strings.Contains(patternStr, "?"):
		compiled.Type = PatternTypeWildcard
		wildcardPattern, err := pm.compileWildcardPattern(patternStr)
		if err != nil {
			return nil, err
		}
		compiled.Compiled = wildcardPattern

	case strings.HasPrefix(patternStr, "regex:"):
		compiled.Type = PatternTypeRegex
		regexStr := strings.TrimPrefix(patternStr, "regex:")
		regex, err := pm.compileRegexPattern(regexStr)
		if err != nil {
			return nil, err
		}
		compiled.Compiled = regex

	case strings.HasPrefix(patternStr, "prefix:"):
		compiled.Type = PatternTypePrefix
		compiled.Compiled = strings.TrimPrefix(patternStr, "prefix:")

	case strings.HasPrefix(patternStr, "suffix:"):
		compiled.Type = PatternTypeSuffix
		compiled.Compiled = strings.TrimPrefix(patternStr, "suffix:")

	case strings.HasPrefix(patternStr, "contains:"):
		compiled.Type = PatternTypeContains
		compiled.Compiled = strings.TrimPrefix(patternStr, "contains:")

	default:
		compiled.Type = PatternTypeExact
		compiled.Compiled = patternStr
	}

	return compiled, nil
}

// compileWildcardPattern compiles a wildcard pattern
func (pm *PatternMatcher) compileWildcardPattern(pattern string) (*WildcardPattern, error) {
	// Check cache first
	if cached, exists := pm.wildcardCache[pattern]; exists {
		return cached, nil
	}

	wildcard := &WildcardPattern{
		Pattern:       pattern,
		HasWildcard:   strings.Contains(pattern, "*") || strings.Contains(pattern, "?"),
		CaseSensitive: true,
		CompiledAt:    time.Now(),
	}

	// Simple wildcard pattern analysis
	if strings.HasSuffix(pattern, "*") && !strings.Contains(pattern[:len(pattern)-1], "*") {
		wildcard.Prefix = pattern[:len(pattern)-1]
	} else if strings.HasPrefix(pattern, "*") && !strings.Contains(pattern[1:], "*") {
		wildcard.Suffix = pattern[1:]
	}

	// Cache the compiled pattern
	if len(pm.wildcardCache) < pm.maxCacheSize {
		pm.wildcardCache[pattern] = wildcard
	}

	return wildcard, nil
}

// compileRegexPattern compiles a regex pattern
func (pm *PatternMatcher) compileRegexPattern(pattern string) (*regexp.Regexp, error) {
	// Check cache first
	if cached, exists := pm.regexCache[pattern]; exists {
		return cached, nil
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// Cache the compiled regex
	if len(pm.regexCache) < pm.maxCacheSize {
		pm.regexCache[pattern] = regex
	}

	return regex, nil
}

// matchPattern matches a key against a compiled pattern
func (pm *PatternMatcher) matchPattern(key string, pattern *CompiledPattern) PatternMatchResult {
	start := time.Now()
	result := PatternMatchResult{
		Type:     pattern.Type,
		Duration: 0,
	}

	defer func() {
		result.Duration = time.Since(start)
	}()

	switch pattern.Type {
	case PatternTypeExact:
		result.Matched = key == pattern.Compiled.(string)

	case PatternTypeWildcard:
		wildcard := pattern.Compiled.(*WildcardPattern)
		result.Matched = pm.matchWildcard(key, wildcard)

	case PatternTypeRegex:
		regex := pattern.Compiled.(*regexp.Regexp)
		result.Matched = regex.MatchString(key)

	case PatternTypePrefix:
		prefix := pattern.Compiled.(string)
		result.Matched = strings.HasPrefix(key, prefix)

	case PatternTypeSuffix:
		suffix := pattern.Compiled.(string)
		result.Matched = strings.HasSuffix(key, suffix)

	case PatternTypeContains:
		substring := pattern.Compiled.(string)
		result.Matched = strings.Contains(key, substring)

	default:
		result.Error = fmt.Errorf("unknown pattern type: %s", pattern.Type)
	}

	if result.Matched {
		result.MatchedKeys = []string{key}
	}

	return result
}

// matchWildcard matches a key against a wildcard pattern
func (pm *PatternMatcher) matchWildcard(key string, wildcard *WildcardPattern) bool {
	if !wildcard.HasWildcard {
		return key == wildcard.Pattern
	}

	// Optimized matching for common cases
	if wildcard.Prefix != "" {
		return strings.HasPrefix(key, wildcard.Prefix)
	}

	if wildcard.Suffix != "" {
		return strings.HasSuffix(key, wildcard.Suffix)
	}

	// General wildcard matching (simplified implementation)
	return pm.matchWildcardGeneral(key, wildcard.Pattern)
}

// matchWildcardGeneral performs general wildcard matching
func (pm *PatternMatcher) matchWildcardGeneral(key, pattern string) bool {
	// Convert wildcard pattern to regex for general matching
	regexPattern := strings.ReplaceAll(pattern, "*", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "?", ".")
	regexPattern = "^" + regexPattern + "$"

	if regex, err := regexp.Compile(regexPattern); err == nil {
		return regex.MatchString(key)
	}

	return false
}

// cleanupCache removes expired entries from caches
func (pm *PatternMatcher) cleanupCache() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()

	// Cleanup wildcard cache
	for pattern, wildcard := range pm.wildcardCache {
		if now.Sub(wildcard.CompiledAt) > pm.cacheTimeout {
			delete(pm.wildcardCache, pattern)
		}
	}

	// Cleanup regex cache
	for pattern := range pm.regexCache {
		// For regex, we don't have timestamp, so just limit size
		if len(pm.regexCache) > pm.maxCacheSize {
			delete(pm.regexCache, pattern)
			break
		}
	}
}

// StartMaintenance starts background maintenance for pattern matcher
func (pm *PatternMatcher) StartMaintenance(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pm.cleanupCache()
			}
		}
	}()
}
