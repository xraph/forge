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

// forwardedHeadersKey is the context key the proxy uses to pass through
// inbound-request headers (typically `Authorization` and `Cookie`) on
// the server-to-server call to a remote contributor. Without this, the
// remote sees an unauthenticated S2S request and cannot identify the
// end user — that breaks any contributor handler that needs the
// caller's identity (e.g. authsome's dashboard "Create API Key" form,
// which binds the new key to the dashboard user's UserID).
type forwardedHeadersKey struct{}

// WithForwardedHeaders returns a derived context that carries the
// supplied headers; RemoteContributor.send forwards a fixed allowlist
// of them (Authorization, Cookie) on the outbound HTTP request. Pass
// the inbound *http.Request.Header verbatim — only the allowlisted
// keys are forwarded, so unrelated headers (Host, Content-Length, …)
// are not leaked.
//
// Trust model: the host explicitly registers a remote contributor via
// WatchRemoteContributor / AddRemoteContributor, so forwarding the
// end user's auth to that registered URL is in-scope of an existing
// trust boundary. Do NOT forward to arbitrary URLs.
func WithForwardedHeaders(ctx context.Context, h http.Header) context.Context {
	if h == nil {
		return ctx
	}

	return context.WithValue(ctx, forwardedHeadersKey{}, h.Clone())
}

// forwardedHeadersFrom returns the forwarded headers attached to ctx,
// or nil if none were set.
func forwardedHeadersFrom(ctx context.Context) http.Header {
	v, _ := ctx.Value(forwardedHeadersKey{}).(http.Header) //nolint:errcheck // type-safe via key

	return v
}

// forwardableHeaderNames lists the inbound headers RemoteContributor
// will forward on S2S calls. Authorization carries Bearer tokens (JWT
// or session), Cookie carries the host-side session cookie when the
// dashboard uses cookie auth. Anything else is dropped to avoid
// leaking unrelated request metadata to the remote.
var forwardableHeaderNames = []string{"Authorization", "Cookie"}

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

// FetchPage fetches an HTML page fragment from the remote service. rawQuery
// is the request's URL query string (without the leading "?") and is forwarded
// verbatim — detail pages and any other route relying on query parameters
// (e.g. /users/detail?id=…) will 404 if it's dropped.
func (rc *RemoteContributor) FetchPage(ctx context.Context, route, rawQuery string) ([]byte, error) {
	url := rc.baseURL + "/_forge/dashboard/pages" + route
	if rawQuery != "" {
		url += "?" + rawQuery
	}

	return rc.fetch(ctx, url)
}

// PostPage forwards a POST to the remote contributor's page endpoint.
// Used to proxy form submissions from the host dashboard. The remote handler
// re-renders the page with FormData populated from the parsed body. body and
// contentType come straight from the inbound request — typically
// application/x-www-form-urlencoded or multipart/form-data.
func (rc *RemoteContributor) PostPage(ctx context.Context, route, rawQuery string, body io.Reader, contentType string) ([]byte, error) {
	url := rc.baseURL + "/_forge/dashboard/pages" + route
	if rawQuery != "" {
		url += "?" + rawQuery
	}

	return rc.send(ctx, http.MethodPost, url, body, contentType)
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

// fetch makes an authenticated GET request and returns the response body.
func (rc *RemoteContributor) fetch(ctx context.Context, url string) ([]byte, error) {
	return rc.send(ctx, http.MethodGet, url, nil, "")
}

// send executes an authenticated request against the remote contributor's
// protocol with the given method/body. Returns the response body bytes.
func (rc *RemoteContributor) send(ctx context.Context, method, url string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set dashboard headers
	req.Header.Set("X-Forge-Dashboard", "true")
	req.Header.Set("Accept", "text/html")

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if rc.apiKey != "" {
		req.Header.Set("X-Forge-Api-Key", rc.apiKey)
	}

	// Forward the allowlisted inbound headers (e.g. Authorization,
	// Cookie) when the host has populated them via
	// WithForwardedHeaders. The remote contributor's auth middleware
	// reads these to resolve the end user — without it, S2S calls
	// from the host arrive unauthenticated and any handler that
	// needs a UserID has no choice but to fail or fabricate one.
	if fwd := forwardedHeadersFrom(ctx); fwd != nil {
		for _, name := range forwardableHeaderNames {
			if v := fwd.Get(name); v != "" {
				req.Header.Set(name, v)
			}
		}
	}

	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote returned status %d for %s", resp.StatusCode, url)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	rc.lastFetch = time.Now()

	return respBody, nil
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
		req.Header.Set("X-Forge-Api-Key", apiKey)
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
