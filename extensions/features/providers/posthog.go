package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// PostHogConfig holds configuration for PostHog feature flags
type PostHogConfig struct {
	// APIKey is your PostHog project API key
	APIKey string `yaml:"api_key" json:"api_key"`

	// Host is the PostHog host (default: https://app.posthog.com)
	Host string `yaml:"host" json:"host"`

	// PersonalAPIKey for admin operations (optional)
	PersonalAPIKey string `yaml:"personal_api_key" json:"personal_api_key"`

	// PollingInterval for flag refresh (default: 30s)
	PollingInterval time.Duration `yaml:"polling_interval" json:"polling_interval"`

	// HTTPClient allows custom HTTP client
	HTTPClient *http.Client `yaml:"-" json:"-"`

	// EnableLocalEvaluation enables local flag evaluation (faster)
	EnableLocalEvaluation bool `yaml:"enable_local_evaluation" json:"enable_local_evaluation"`
}

// PostHogProvider implements feature flags using PostHog
type PostHogProvider struct {
	config PostHogConfig
	client *http.Client
	logger forge.Logger

	// Local cache
	mu    sync.RWMutex
	flags map[string]*postHogFlag

	// Polling
	stopCh chan struct{}
	doneCh chan struct{}
}

// postHogFlag represents a PostHog feature flag
type postHogFlag struct {
	ID                         int            `json:"id"`
	Key                        string         `json:"key"`
	Name                       string         `json:"name"`
	Active                     bool           `json:"active"`
	Filters                    postHogFilters `json:"filters"`
	RolloutPercentage          int            `json:"rollout_percentage"`
	EnsureExperienceContinuity bool           `json:"ensure_experience_continuity"`
}

type postHogFilters struct {
	Groups       []postHogFilterGroup `json:"groups"`
	Multivariate *postHogMultivariate `json:"multivariate,omitempty"`
	PayloadRules []postHogPayloadRule `json:"payloads,omitempty"`
}

type postHogFilterGroup struct {
	Properties        []postHogProperty `json:"properties"`
	RolloutPercentage int               `json:"rollout_percentage"`
}

type postHogProperty struct {
	Key      string      `json:"key"`
	Type     string      `json:"type"`
	Value    interface{} `json:"value"`
	Operator string      `json:"operator"`
}

type postHogMultivariate struct {
	Variants []postHogVariant `json:"variants"`
}

type postHogVariant struct {
	Key               string `json:"key"`
	Name              string `json:"name"`
	RolloutPercentage int    `json:"rollout_percentage"`
}

type postHogPayloadRule struct {
	Value interface{} `json:"value"`
}

// PostHog API request/response types
type postHogDecideRequest struct {
	APIKey           string                 `json:"api_key"`
	DistinctID       string                 `json:"distinct_id"`
	Groups           map[string]string      `json:"groups,omitempty"`
	PersonProperties map[string]interface{} `json:"person_properties,omitempty"`
	GroupProperties  map[string]interface{} `json:"group_properties,omitempty"`
}

type postHogDecideResponse struct {
	FeatureFlags        map[string]interface{} `json:"featureFlags"`
	FeatureFlagPayloads map[string]interface{} `json:"featureFlagPayloads,omitempty"`
	Errors              []postHogError         `json:"errorsWhileComputingFlags,omitempty"`
}

type postHogError struct {
	FlagKey string `json:"flag_key"`
	Error   string `json:"error"`
}

// NewPostHogProvider creates a new PostHog provider
func NewPostHogProvider(config PostHogConfig, logger forge.Logger) (*PostHogProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("posthog api_key is required")
	}

	if config.Host == "" {
		config.Host = "https://app.posthog.com"
	}

	if config.PollingInterval == 0 {
		config.PollingInterval = 30 * time.Second
	}

	client := config.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	p := &PostHogProvider{
		config: config,
		client: client,
		logger: logger,
		flags:  make(map[string]*postHogFlag),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	// Load initial flags
	if config.EnableLocalEvaluation && config.PersonalAPIKey != "" {
		if err := p.refreshFlags(context.Background()); err != nil {
			logger.Warn("failed to load initial flags", forge.String("error", err.Error()))
		}
	}

	return p, nil
}

// Name returns the provider name
func (p *PostHogProvider) Name() string {
	return "posthog"
}

// IsEnabled checks if a feature flag is enabled
func (p *PostHogProvider) IsEnabled(ctx context.Context, key string, userCtx *UserContext) bool {
	value, _ := p.Evaluate(ctx, key, userCtx)

	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v != ""
	default:
		return false
	}
}

// GetValue returns the value of a feature flag
func (p *PostHogProvider) GetValue(ctx context.Context, key string, userCtx *UserContext, defaultValue interface{}) interface{} {
	value, err := p.Evaluate(ctx, key, userCtx)
	if err != nil {
		return defaultValue
	}
	if value == nil {
		return defaultValue
	}
	return value
}

// Evaluate evaluates a feature flag using PostHog's API
func (p *PostHogProvider) Evaluate(ctx context.Context, key string, userCtx *UserContext) (interface{}, error) {
	if userCtx == nil || userCtx.UserID == "" {
		return false, fmt.Errorf("user context with user_id is required")
	}

	// Try local evaluation first if enabled
	if p.config.EnableLocalEvaluation {
		if value := p.evaluateLocally(key, userCtx); value != nil {
			return value, nil
		}
	}

	// Fall back to API evaluation
	return p.evaluateRemote(ctx, key, userCtx)
}

// evaluateLocally evaluates flags using cached data
func (p *PostHogProvider) evaluateLocally(key string, userCtx *UserContext) interface{} {
	p.mu.RLock()
	flag, exists := p.flags[key]
	p.mu.RUnlock()

	if !exists || !flag.Active {
		return nil
	}

	// Simple active check (full evaluation would require more logic)
	return true
}

// evaluateRemote calls PostHog's decide API
func (p *PostHogProvider) evaluateRemote(ctx context.Context, key string, userCtx *UserContext) (interface{}, error) {
	reqBody := postHogDecideRequest{
		APIKey:           p.config.APIKey,
		DistinctID:       userCtx.UserID,
		PersonProperties: p.buildPersonProperties(userCtx),
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/decide/?v=2", p.config.Host)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call decide API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("decide API returned %d: %s", resp.StatusCode, string(body))
	}

	var decideResp postHogDecideResponse
	if err := json.NewDecoder(resp.Body).Decode(&decideResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for errors
	for _, errItem := range decideResp.Errors {
		if errItem.FlagKey == key {
			return nil, fmt.Errorf("flag evaluation error: %s", errItem.Error)
		}
	}

	// Get flag value
	value, exists := decideResp.FeatureFlags[key]
	if !exists {
		return false, nil
	}

	return value, nil
}

// buildPersonProperties converts UserContext to PostHog person properties
func (p *PostHogProvider) buildPersonProperties(userCtx *UserContext) map[string]interface{} {
	props := make(map[string]interface{})

	if userCtx.Email != "" {
		props["email"] = userCtx.Email
	}
	if userCtx.Name != "" {
		props["name"] = userCtx.Name
	}
	if userCtx.IP != "" {
		props["$ip"] = userCtx.IP
	}
	if userCtx.Country != "" {
		props["$geoip_country_code"] = userCtx.Country
	}
	if len(userCtx.Groups) > 0 {
		props["groups"] = userCtx.Groups
	}

	// Add custom attributes
	for k, v := range userCtx.Attributes {
		props[k] = v
	}

	return props
}

// GetAllFlags returns all feature flags
func (p *PostHogProvider) GetAllFlags(ctx context.Context, userCtx *UserContext) (map[string]interface{}, error) {
	if userCtx == nil || userCtx.UserID == "" {
		return nil, fmt.Errorf("user context with user_id is required")
	}

	reqBody := postHogDecideRequest{
		APIKey:           p.config.APIKey,
		DistinctID:       userCtx.UserID,
		PersonProperties: p.buildPersonProperties(userCtx),
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/decide/?v=2", p.config.Host)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call decide API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("decide API returned %d: %s", resp.StatusCode, string(body))
	}

	var decideResp postHogDecideResponse
	if err := json.NewDecoder(resp.Body).Decode(&decideResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return decideResp.FeatureFlags, nil
}

// refreshFlags fetches all flags from PostHog (requires Personal API Key)
func (p *PostHogProvider) refreshFlags(ctx context.Context) error {
	if p.config.PersonalAPIKey == "" {
		return fmt.Errorf("personal_api_key required for flag refresh")
	}

	url := fmt.Sprintf("%s/api/projects/@current/feature_flags/", p.config.Host)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.PersonalAPIKey))

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch flags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Results []postHogFlag `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Update cache
	p.mu.Lock()
	defer p.mu.Unlock()

	p.flags = make(map[string]*postHogFlag)
	for i := range response.Results {
		flag := &response.Results[i]
		p.flags[flag.Key] = flag
	}

	p.logger.Info("refreshed feature flags from PostHog",
		forge.Int("count", len(p.flags)))

	return nil
}

// Refresh manually refreshes flags from PostHog
func (p *PostHogProvider) Refresh(ctx context.Context) error {
	if !p.config.EnableLocalEvaluation {
		return fmt.Errorf("local evaluation not enabled")
	}
	return p.refreshFlags(ctx)
}

// Start starts the polling loop
func (p *PostHogProvider) Start(ctx context.Context) error {
	if !p.config.EnableLocalEvaluation || p.config.PersonalAPIKey == "" {
		p.logger.Info("PostHog provider started in API-only mode")
		return nil
	}

	go p.pollingLoop()
	p.logger.Info("PostHog provider started with local evaluation")
	return nil
}

// pollingLoop periodically refreshes flags
func (p *PostHogProvider) pollingLoop() {
	defer close(p.doneCh)

	ticker := time.NewTicker(p.config.PollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := p.refreshFlags(ctx); err != nil {
				p.logger.Error("failed to refresh flags", forge.String("error", err.Error()))
			}
			cancel()

		case <-p.stopCh:
			return
		}
	}
}

// Stop stops the provider
func (p *PostHogProvider) Stop(ctx context.Context) error {
	if p.config.EnableLocalEvaluation && p.config.PersonalAPIKey != "" {
		close(p.stopCh)

		// Wait for polling to finish or timeout
		select {
		case <-p.doneCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	p.logger.Info("PostHog provider stopped")
	return nil
}

// Health checks the provider health
func (p *PostHogProvider) Health(ctx context.Context) error {
	// Try a simple API call
	url := fmt.Sprintf("%s/decide/?v=2", p.config.Host)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBufferString(`{"api_key":"`+p.config.APIKey+`","distinct_id":"health-check"}`))
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("PostHog API returned %d", resp.StatusCode)
	}

	return nil
}

// Track sends an analytics event to PostHog
func (p *PostHogProvider) Track(ctx context.Context, userID string, event string, properties map[string]interface{}) error {
	eventData := map[string]interface{}{
		"api_key":     p.config.APIKey,
		"event":       event,
		"distinct_id": userID,
		"properties":  properties,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(eventData)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	url := fmt.Sprintf("%s/capture/", p.config.Host)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to track event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("track API returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
