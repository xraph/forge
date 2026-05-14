// Package remote implements the contract dispatcher's HTTP forwarding layer.
// When a contributor's handlers live in another service, the host dashboard
// records its RemoteEndpoint via Registry.RegisterRemote; the
// ForwardingDispatcher reads that endpoint at dispatch time and forwards
// the verbatim envelope over HTTP. Slice (m) introduced this so a single
// dashboard can aggregate contributors from multiple upstream services
// without the transport layer caring whether a handler is local or remote.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// DefaultDispatchPath is appended to a RemoteEndpoint.BaseURL when forwarding
// a request envelope.
const DefaultDispatchPath = "/_forge/contract/dispatch"

// DefaultManifestPath is appended to a RemoteEndpoint.BaseURL when fetching
// a contributor manifest during registration.
const DefaultManifestPath = "/_forge/contract/manifest"

// DefaultTimeout is the HTTP client timeout used when RemoteEndpoint.Client
// is nil. Chosen to stay well below typical proxy / load-balancer idle cuts
// while leaving room for cold-path handlers.
const DefaultTimeout = 10 * time.Second

// ForwardingDispatcher implements dispatcher.RemoteDispatcher by POSTing
// the verbatim contract envelope to the contributor's RemoteEndpoint.
//
// It looks the endpoint up via Registry.Remote, so registering a contributor
// (Registry.RegisterRemote) and wiring this dispatcher into the dispatcher
// (Dispatcher.SetRemoteDispatcher) are the two halves of the integration —
// nothing further is needed at the transport layer.
type ForwardingDispatcher struct {
	reg contract.Registry
	// dispatchPath overrides DefaultDispatchPath; primarily for tests.
	dispatchPath string
}

// Option configures a ForwardingDispatcher.
type Option func(*ForwardingDispatcher)

// WithDispatchPath overrides the path appended to a RemoteEndpoint.BaseURL
// when forwarding envelopes. Useful when the upstream service mounts the
// contract server at a non-default location.
func WithDispatchPath(path string) Option {
	return func(f *ForwardingDispatcher) { f.dispatchPath = path }
}

// NewForwardingDispatcher returns a dispatcher that reads endpoints from
// the supplied registry. The registry is the source of truth for which
// contributors are remote — local contributors fall through with
// CodeNotFound so the host dispatcher's own NotFound handling kicks in.
func NewForwardingDispatcher(reg contract.Registry, opts ...Option) *ForwardingDispatcher {
	f := &ForwardingDispatcher{reg: reg, dispatchPath: DefaultDispatchPath}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Dispatch implements dispatcher.RemoteDispatcher. Returns CodeNotFound
// when the contributor is not registered as a remote (the host dispatcher
// then surfaces its own NotFound). Network and decode failures are
// reported as CodeInternal; upstream-returned error envelopes are
// surfaced verbatim with their original code/message.
func (f *ForwardingDispatcher) Dispatch(
	ctx context.Context,
	req contract.Request,
	_ contract.Principal,
) (json.RawMessage, contract.ResponseMeta, error) {
	if f.reg == nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeInternal, Message: "forwarding dispatcher: nil registry"}
	}
	endpoint, ok := f.reg.Remote(req.Contributor)
	if !ok {
		return nil, contract.ResponseMeta{}, &contract.Error{
			Code:    contract.CodeNotFound,
			Message: fmt.Sprintf("contributor %q has no remote endpoint", req.Contributor),
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeInternal, Message: "encode envelope: " + err.Error()}
	}

	url := strings.TrimRight(endpoint.BaseURL, "/") + f.dispatchPath
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeInternal, Message: "build request: " + err.Error()}
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "application/json")

	// Forward inbound auth headers so the upstream sees the end-user
	// identity. Mirrors the legacy RemoteContributor.WithForwardedHeaders.
	if in := dashauth.RequestFromContext(ctx); in != nil {
		if auth := in.Header.Get("Authorization"); auth != "" {
			hr.Header.Set("X-Forwarded-Authorization", auth)
		}
		if cookie := in.Header.Get("Cookie"); cookie != "" {
			hr.Header.Set("X-Forwarded-Cookie", cookie)
		}
	}
	// The dashboard's own credential authenticates the dashboard-to-service
	// hop. Upstream identity goes in X-Forwarded-Authorization above.
	if endpoint.APIKey != "" {
		hr.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}

	client := endpoint.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}

	resp, err := client.Do(hr)
	if err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeUnavailable, Message: "forward to " + req.Contributor + ": " + err.Error()}
	}
	defer resp.Body.Close()

	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeInternal, Message: "read upstream response: " + err.Error()}
	}

	// First try the success envelope. On failure fall back to the error
	// envelope shape — the upstream may return either with the same status
	// per the contract.
	var ok2 contract.Response
	if jsonErr := json.Unmarshal(rb, &ok2); jsonErr == nil && ok2.OK {
		return ok2.Data, ok2.Meta, nil
	}
	var er contract.ErrorResponse
	if jsonErr := json.Unmarshal(rb, &er); jsonErr == nil && er.Error != nil {
		// Surface upstream error code/message verbatim.
		return nil, contract.ResponseMeta{}, er.Error
	}
	return nil, contract.ResponseMeta{}, &contract.Error{
		Code:    contract.CodeInternal,
		Message: fmt.Sprintf("upstream %s returned %d with undecodable body", req.Contributor, resp.StatusCode),
	}
}
