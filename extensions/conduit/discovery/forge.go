package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/discovery/backends"
)

// Forge adapts an existing Forge discovery backend without owning its lifecycle.
type Forge struct {
	backend func() (backends.Backend, error)
}

// NewForge resolves the existing backend lazily after Forge registers its services.
func NewForge(backend func() (backends.Backend, error)) *Forge { return &Forge{backend: backend} }

func registryID(identity core.Identity) string {
	binding := core.Binding{Identity: identity, Subscription: core.SubscriptionConfig{ID: identity.InstanceID}}

	return "conduit-" + binding.ConsumerID()
}

// Register renews one namespaced Conduit member in the shared Forge backend.
func (f *Forge) Register(ctx context.Context, instance core.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}

	backend, err := f.backend()
	if err != nil {
		return err
	}

	endpoints, err := json.Marshal(instance.Endpoints)
	if err != nil {
		return err
	}

	host := ""
	port := 0

	if len(instance.Endpoints) > 0 {
		parsed, _ := url.Parse(instance.Endpoints[0].URL)
		host = parsed.Hostname()

		port, _ = strconv.Atoi(parsed.Port())
		if port == 0 {
			if parsed.Scheme == "https" {
				port = 443
			} else {
				port = 80
			}
		}
	}

	status := backends.HealthStatusCritical
	if instance.Ready {
		status = backends.HealthStatusPassing
	}

	return backend.Register(ctx, &backends.ServiceInstance{ID: registryID(instance.Identity), Name: instance.Identity.ServiceID, Version: instance.Version, Address: host, Port: port, Status: status, LastHeartbeat: time.Now().Unix(), Metadata: map[string]string{"conduit.namespace": instance.Identity.Namespace, "conduit.instance": instance.Identity.InstanceID, "conduit.endpoints": string(endpoints)}})
}

// Deregister removes only the stopping member's dedicated record.
func (f *Forge) Deregister(ctx context.Context, identity core.Identity) error {
	backend, err := f.backend()
	if err != nil {
		return err
	}

	return backend.Deregister(ctx, registryID(identity))
}

func convertMember(member *backends.ServiceInstance, namespace string) (core.Instance, bool, error) {
	if member == nil || member.Metadata["conduit.namespace"] != namespace {
		return core.Instance{}, false, nil
	}

	instance := core.Instance{Identity: core.Identity{Namespace: namespace, ServiceID: member.Name, InstanceID: member.Metadata["conduit.instance"]}, Version: member.Version, Ready: member.IsHealthy()}
	if data := member.Metadata["conduit.endpoints"]; data != "" {
		if err := json.Unmarshal([]byte(data), &instance.Endpoints); err != nil {
			return core.Instance{}, false, errors.New("conduit: invalid Forge discovery endpoints")
		}
	} else {
		scheme := member.Metadata["scheme"]
		if scheme == "" {
			scheme = "http"
		}

		instance.Endpoints = []core.Endpoint{{Protocol: scheme, URL: scheme + "://" + net.JoinHostPort(member.Address, strconv.Itoa(member.Port))}}
	}

	if err := instance.Validate(); err != nil {
		return core.Instance{}, false, errors.New("conduit: invalid Forge discovery record")
	}

	return instance, true, nil
}

// Resolve selects only healthy members carrying the matching namespace.
func (f *Forge) Resolve(ctx context.Context, namespace, service string) ([]core.Instance, error) {
	backend, err := f.backend()
	if err != nil {
		return nil, err
	}

	members, err := backend.Discover(ctx, service)
	if err != nil {
		return nil, err
	}

	result := []core.Instance{}

	for _, member := range members {
		instance, ok, err := convertMember(member, namespace)
		if err != nil {
			return nil, err
		}

		if ok && instance.Ready && instance.Identity.ServiceID == service {
			result = append(result, instance)
		}
	}

	return result, nil
}

// List includes unhealthy members without mixing namespaces or legacy unscoped records.
func (f *Forge) List(ctx context.Context, namespace string) ([]core.Instance, error) {
	backend, err := f.backend()
	if err != nil {
		return nil, err
	}

	services, err := backend.ListServices(ctx)
	if err != nil {
		return nil, err
	}

	result := []core.Instance{}

	for _, name := range services {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		members, err := backend.Discover(ctx, name)
		if err != nil {
			return nil, err
		}

		for _, member := range members {
			instance, ok, err := convertMember(member, namespace)
			if err != nil {
				return nil, err
			}

			if ok {
				result = append(result, instance)
			}
		}
	}

	slices.SortFunc(result, func(a, b core.Instance) int {
		return strings.Compare(a.Identity.ServiceID+"/"+a.Identity.InstanceID, b.Identity.ServiceID+"/"+b.Identity.InstanceID)
	})

	return result, nil
}
