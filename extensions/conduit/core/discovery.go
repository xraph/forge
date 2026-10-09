package core

import (
	"context"
	"errors"
	"net/url"
)

// Endpoint is an advertised address, separate from the instance's bind address.
type Endpoint struct {
	Protocol string `json:"protocol" yaml:"protocol"`
	URL      string `json:"url"      yaml:"url"`
}

// Validate accepts transport addresses without embedded credentials or query tokens.
func (e Endpoint) Validate() error {
	parsed, err := url.Parse(e.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != e.Protocol {
		return errors.New("conduit: invalid advertised endpoint")
	}

	switch e.Protocol {
	case "http", "https":
		return nil
	case "grpc", "grpcs":
		if parsed.Path == "" || parsed.Path == "/" {
			return nil
		}
	}

	return errors.New("conduit: unsupported advertised endpoint protocol or path")
}

// Instance describes one running member of a logical service.
type Instance struct {
	Identity  Identity   `json:"identity"`
	Version   string     `json:"version"`
	Endpoints []Endpoint `json:"endpoints"`
	Ready     bool       `json:"ready"`
}

// Validate checks registration identity and advertised transport addresses.
func (i Instance) Validate() error {
	if err := i.Identity.Validate(); err != nil {
		return err
	}

	for _, endpoint := range i.Endpoints {
		if err := endpoint.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Resolver finds replicas by logical service identity within a namespace.
type Resolver interface {
	Resolve(ctx context.Context, namespace string, service string) ([]Instance, error)
}

// Registry adds instance registration without requiring it from DNS resolvers.
type Registry interface {
	Resolver
	Register(ctx context.Context, instance Instance) error
	Deregister(ctx context.Context, identity Identity) error
}

// InstanceLister lists members within the configured namespace for dashboard inspection.
type InstanceLister interface {
	List(ctx context.Context, namespace string) ([]Instance, error)
}
