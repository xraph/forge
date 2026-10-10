package resolve

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"sort"
)

func gatewayConnections(sp *spec.Deploy, d *model.Deployment) ([]model.Connection, output.Diagnostics) {
	names := []string{}

	for name, s := range sp.Services {
		if s.Kind == spec.KindGateway {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	if len(names) == 0 && d.Target.Gateway == "" {
		return nil, nil
	}

	needed := false

	for _, s := range d.Services {
		if s.Discovery && s.Kind != spec.KindGateway {
			needed = true
		}
	}

	if !needed {
		return nil, nil
	}

	fail := func(message string) ([]model.Connection, output.Diagnostics) {
		return nil, output.Diagnostics{{Code: "DEPLOY_GATEWAY_SELECTION", Severity: output.SeverityError, Message: message, Field: "deploy.targets." + d.TargetName + ".gateway"}}
	}

	name := d.Target.Gateway
	if name == "" {
		if len(names) != 1 {
			return fail("select a gateway explicitly when several gateway services are declared")
		}

		name = names[0]
	}

	gateway, ok := sp.Services[name]
	if !ok || gateway.Kind != spec.KindGateway {
		return fail("gateway must name a declared gateway service")
	}

	ports := []string{}

	for key, p := range gateway.Ports {
		if p.Protocol == "" || p.Protocol == "http" || p.Protocol == "https" {
			ports = append(ports, key)
		}
	}

	sort.Strings(ports)

	if len(ports) != 1 {
		return fail("gateway discovery requires exactly one named HTTP port")
	}

	connections := []model.Connection{}

	for _, s := range d.Services {
		if s.Discovery && s.Name != name && s.Kind != spec.KindGateway {
			connections = append(connections, model.Connection{From: s.Name, To: name, Port: ports[0], ConfigKey: "extensions.discovery.farp.gateway_url", EnvVar: "FARP_GATEWAY_URL"})
		}
	}

	return connections, nil
}
