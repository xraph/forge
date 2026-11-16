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
	"net"
	"net/http"
	"net/url"
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

// DefaultMaxResponseBytes caps how much of an upstream's response the
// forwarding dispatcher reads. A contract response is a JSON envelope; a
// larger body is refused rather than buffered without bound.
const DefaultMaxResponseBytes int64 = 10 << 20

// ForwardedUserHeader carries the subject of the user the dashboard
// authenticated to the upstream. It is a claim made by the dashboard, so an
// upstream should only believe it on a request that also authenticated as
// the dashboard (RemoteEndpoint.APIKey).
const ForwardedUserHeader = "X-Forwarded-User"

// CheckTransport refuses a plain http baseURL whose host is not a loopback
// address unless allowInsecure is set. Envelopes carry the dashboard's API
// key, the user's identity and whatever the intent returns, none of which
// should cross a network in clear text by default.
func CheckTransport(baseURL string, allowInsecure bool) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("remote: invalid base URL %q: %w", baseURL, err)
	}

	if !strings.EqualFold(u.Scheme, "http") || allowInsecure || isLoopbackHost(u.Hostname()) {
		return nil
	}

	return fmt.Errorf("remote: refusing plain http to non-loopback contributor %s; use https or allow insecure remotes explicitly", u.Host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

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
	// maxResponse caps the upstream response body; always > 0.
	maxResponse int64
}

// Option configures a ForwardingDispatcher.
type Option func(*ForwardingDispatcher)

// WithDispatchPath overrides the path appended to a RemoteEndpoint.BaseURL
// when forwarding envelopes. Useful when the upstream service mounts the
// contract server at a non-default location.
func WithDispatchPath(path string) Option {
	return func(f *ForwardingDispatcher) { f.dispatchPath = path }
}

// WithMaxResponseBytes overrides DefaultMaxResponseBytes. n <= 0 keeps the
// default; there is no way to switch the cap off.
func WithMaxResponseBytes(n int64) Option {
	return func(f *ForwardingDispatcher) {
		if n > 0 {
			f.maxResponse = n
		}
	}
}

// NewForwardingDispatcher returns a dispatcher that reads endpoints from
// the supplied registry. The registry is the source of truth for which
// contributors are remote — local contributors fall through with
// CodeNotFound so the host dispatcher's own NotFound handling kicks in.
func NewForwardingDispatcher(reg contract.Registry, opts ...Option) *ForwardingDispatcher {
	f := &ForwardingDispatcher{reg: reg, dispatchPath: DefaultDispatchPath, maxResponse: DefaultMaxResponseBytes}
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

	// Checked per dispatch as well as at registration, so an endpoint put
	// in the registry by some other route is held to the same rule.
	if err := CheckTransport(endpoint.BaseURL, endpoint.AllowInsecureHTTP); err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeUnavailable, Message: err.Error()}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeInternal, Message: "encode envelope: " + err.Error()}
	}

	target := strings.TrimRight(endpoint.BaseURL, "/") + f.dispatchPath

	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeInternal, Message: "build request: " + err.Error()}
	}

	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "application/json")

	// The upstream learns who the user is from the subject the dashboard
	// already authenticated, not from the user's own credentials. Raw
	// Cookie and Authorization are host credentials; forwarding them to
	// every contributor would let any contributor replay them against the
	// host. Headers named in ForwardHeaders are the explicit exception.
	if user := dashauth.UserFromContext(ctx); user.Authenticated() {
		hr.Header.Set(ForwardedUserHeader, user.Subject)
	}

	if in := dashauth.RequestFromContext(ctx); in != nil {
		for _, name := range endpoint.ForwardHeaders {
			if v := in.Header.Get(name); v != "" {
				hr.Header.Set("X-Forwarded-"+http.CanonicalHeaderKey(name), v)
			}
		}
	}
	// The dashboard's own credential authenticates the dashboard-to-service
	// hop. The user's identity goes in X-Forwarded-User above.
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

	rb, err := io.ReadAll(io.LimitReader(resp.Body, f.maxResponse+1))
	if err != nil {
		return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeInternal, Message: "read upstream response: " + err.Error()}
	}

	if int64(len(rb)) > f.maxResponse {
		return nil, contract.ResponseMeta{}, &contract.Error{
			Code:    contract.CodeInternal,
			Message: fmt.Sprintf("upstream %s response exceeds %d bytes", req.Contributor, f.maxResponse),
		}
	}

	// An upstream's Set-Cookie lands on the dashboard's own origin when
	// re-emitted, so it only happens for endpoints that opted in. The case
	// it exists for is an upstream serving auth.login: its cookie is set on
	// its own response, and without the re-emit the browser never sees it.
	//
	// Done before decoding the envelope so cookies propagate even when the
	// upstream returns an error envelope (a logout that clears the session
	// on the way out, say).
	if endpoint.PropagateSetCookie {
		if hostW := dashauth.ResponseWriterFromContext(ctx); hostW != nil {
			for _, c := range resp.Cookies() {
				http.SetCookie(hostW, c)
			}
		}
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
