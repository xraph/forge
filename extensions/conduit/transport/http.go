// Package transport binds standard API clients to logical service names.
package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

// Services resolves logical destinations for standard HTTP and gRPC clients.
type Services struct {
	Resolver     core.Resolver
	ResolverFunc func() core.Resolver
	Identity     core.Identity
	IdentityFunc func() core.Identity
	counter      atomic.Uint64
}

func (s *Services) endpoints(ctx context.Context, service string, protocols ...string) ([]core.Endpoint, error) {
	resolver := s.Resolver
	if s.ResolverFunc != nil {
		resolver = s.ResolverFunc()
	}

	if resolver == nil {
		return nil, errors.New("conduit: service resolver is required")
	}

	identity := s.identity()

	instances, err := resolver.Resolve(ctx, identity.Namespace, service)
	if err != nil {
		return nil, err
	}

	endpoints := make([]core.Endpoint, 0)

	for _, instance := range instances {
		if !instance.Ready || instance.Identity.Namespace != identity.Namespace || instance.Identity.ServiceID != service {
			continue
		}

		for _, endpoint := range instance.Endpoints {
			if err := endpoint.Validate(); err != nil {
				return nil, err
			}

			if slices.Contains(protocols, endpoint.Protocol) {
				endpoints = append(endpoints, endpoint)
			}
		}
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("%w: no ready %s endpoint for %s", core.ErrNotFound, strings.Join(protocols, "/"), service)
	}

	return endpoints, nil
}

type serviceTransport struct {
	services *Services
	service  string
	next     http.RoundTripper
}

// HTTP returns a standard client that resolves its service on every request.
// Requests use http://<service>/<path>; the registered endpoint chooses HTTP or HTTPS.
func (s *Services) HTTP(service string, next http.RoundTripper) *http.Client {
	if next == nil {
		next = http.DefaultTransport
	}

	return &http.Client{Transport: &serviceTransport{services: s, service: service, next: next}, Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func (t *serviceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != t.service {
		return nil, errors.New("conduit: request destination differs from configured service")
	}

	endpoints, err := t.services.endpoints(request.Context(), t.service, "http", "https")
	if err != nil {
		return nil, err
	}

	endpoint := endpoints[(t.services.counter.Add(1)-1)%uint64(len(endpoints))]

	base, err := url.Parse(endpoint.URL)
	if err != nil || base.Host == "" || base.User != nil || base.Scheme != endpoint.Protocol {
		return nil, errors.New("conduit: invalid advertised HTTP endpoint")
	}

	clone := request.Clone(request.Context())
	clone.URL.Scheme, clone.URL.Host = base.Scheme, base.Host
	clone.URL.Path = strings.TrimRight(base.Path, "/") + request.URL.Path
	clone.URL.RawPath = strings.TrimRight(base.EscapedPath(), "/") + request.URL.EscapedPath()
	clone.Host = base.Host
	clone.Header.Set("X-Forge-Service-Id", t.services.identity().ServiceID)
	clone.Header.Set("X-Forge-Instance-Id", t.services.identity().InstanceID)

	return t.next.RoundTrip(clone)
}

func (s *Services) identity() core.Identity {
	if s.IdentityFunc != nil {
		return s.IdentityFunc()
	}

	return s.Identity
}
