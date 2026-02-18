package contributor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RemoteContributor wraps a remote service that exposes dashboard pages and widgets
// via HTTP fragment endpoints. It fetches HTML fragments from the remote service
// and returns them as raw HTML nodes for embedding in the dashboard shell.
type RemoteContributor struct {
	manifest  *Manifest
	baseURL   string
	apiKey    string
	client    *http.Client
	lastFetch time.Time
}

// RemoteContributorOption configures a RemoteContributor.
type RemoteContributorOption func(*RemoteContributor)

// WithAPIKey sets the API key for authenticating with the remote service.
func WithAPIKey(key string) RemoteContributorOption {
	return func(rc *RemoteContributor) {
		rc.apiKey = key
	}
}

// WithHTTPClient sets a custom HTTP client for the remote contributor.
func WithHTTPClient(client *http.Client) RemoteContributorOption {
	return func(rc *RemoteContributor) {
		rc.client = client
	}
}

// NewRemoteContributor creates a remote contributor from a base URL and manifest.
func NewRemoteContributor(baseURL string, manifest *Manifest, opts ...RemoteContributorOption) *RemoteContributor {
	rc := &RemoteContributor{
		manifest: manifest,
		baseURL:  baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(rc)
	}

	return rc
}

// Manifest returns the contributor's manifest.
func (rc *RemoteContributor) Manifest() *Manifest {
	return rc.manifest
}

// BaseURL returns the remote service base URL.
func (rc *RemoteContributor) BaseURL() string {
	return rc.baseURL
}

// FetchPage fetches an HTML page fragment from the remote service.
func (rc *RemoteContributor) FetchPage(ctx context.Context, route string) ([]byte, error) {
	url := rc.baseURL + "/_forge/dashboard/pages" + route
	return rc.fetch(ctx, url)
}

// FetchWidget fetches an HTML widget fragment from the remote service.
func (rc *RemoteContributor) FetchWidget(ctx context.Context, widgetID string) ([]byte, error) {
	url := rc.baseURL + "/_forge/dashboard/widgets/" + widgetID
	return rc.fetch(ctx, url)
}

// FetchSettings fetches an HTML settings form from the remote service.
func (rc *RemoteContributor) FetchSettings(ctx context.Context, settingID string) ([]byte, error) {
	url := rc.baseURL + "/_forge/dashboard/settings/" + settingID
	return rc.fetch(ctx, url)
}

// fetch makes an authenticated HTTP request and returns the response body.
func (rc *RemoteContributor) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set dashboard headers
	req.Header.Set("X-Forge-Dashboard", "true")
	req.Header.Set("Accept", "text/html")

	if rc.apiKey != "" {
		req.Header.Set("X-Forge-API-Key", rc.apiKey)
	}

	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote returned status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	rc.lastFetch = time.Now()

	return body, nil
}

// FetchManifest fetches the dashboard manifest from a remote service URL.
// This is used during discovery to get the remote contributor's manifest before registration.
func FetchManifest(ctx context.Context, baseURL string, timeout time.Duration, apiKey string) (*Manifest, error) {
	client := &http.Client{Timeout: timeout}

	url := baseURL + "/_forge/dashboard/manifest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest request: %w", err)
	}

	req.Header.Set("X-Forge-Dashboard", "true")
	req.Header.Set("Accept", "application/json")

	if apiKey != "" {
		req.Header.Set("X-Forge-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest endpoint returned status %d", resp.StatusCode)
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	return &manifest, nil
}
