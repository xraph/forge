package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// MaxManifestBytes caps the manifest FetchManifest reads. A larger one is an
// error rather than being cut short, since a truncated manifest either fails
// to decode with a misleading message or, worse, decodes.
const MaxManifestBytes int64 = 1 << 20

// ErrManifestTooLarge is returned by FetchManifest when the upstream's
// manifest exceeds MaxManifestBytes.
var ErrManifestTooLarge = errors.New("remote: manifest too large")

// FetchManifest pulls a contributor's contract manifest from the upstream
// service's manifest endpoint. The caller typically follows this with
// Registry.RegisterRemote(manifest, RemoteEndpoint{BaseURL: baseURL, ...}).
//
// baseURL is the service root (e.g. https://svc:8443/svc); the function
// appends DefaultManifestPath. apiKey, when non-empty, is sent as
// Authorization: Bearer, but only over a secure transport (see
// IsSecureTransport): over plain http to another host it would cross the
// network in clear text, so it is left off. FetchManifest does not decide
// whether plain http is acceptable at all; callers check that with
// CheckTransport first. A nil client falls back to a 10s-timeout client.
//
// On non-2xx the upstream's status + body excerpt are surfaced so config
// mistakes (wrong port, wrong path) are diagnosable. A manifest over
// MaxManifestBytes is refused with ErrManifestTooLarge.
func FetchManifest(ctx context.Context, baseURL, apiKey string, client *http.Client) (*contract.ContractManifest, error) {
	if baseURL == "" {
		return nil, errors.New("remote: baseURL is required")
	}

	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}

	url := strings.TrimRight(baseURL, "/") + DefaultManifestPath

	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("remote: build manifest request: %w", err)
	}

	hr.Header.Set("Accept", "application/json")

	if apiKey != "" && IsSecureTransport(baseURL) {
		hr.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("remote: fetch manifest from %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("remote: read manifest from %s: %w", url, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := strings.TrimSpace(string(body))
		if len(excerpt) > 200 {
			excerpt = excerpt[:200] + "…"
		}

		return nil, fmt.Errorf("remote: manifest endpoint %s returned %d: %s", url, resp.StatusCode, excerpt)
	}

	if int64(len(body)) > MaxManifestBytes {
		return nil, fmt.Errorf("%w: %s returned more than %d bytes", ErrManifestTooLarge, url, MaxManifestBytes)
	}

	var m contract.ContractManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("remote: decode manifest from %s: %w", url, err)
	}

	if m.Contributor.Name == "" {
		return nil, fmt.Errorf("remote: manifest from %s is missing contributor.name", url)
	}

	return &m, nil
}

// fetchWithDeadline is a small helper used by callers that want a per-call
// timeout shorter than the client's. Returns a derived context whose cancel
// must be called by the caller.
func fetchWithDeadline(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, d)
}

// Keep the helper referenced so unused-symbol lint doesn't strip it; callers
// outside this package can opt in via WithDeadline-style wrappers later.
var _ = fetchWithDeadline
