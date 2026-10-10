package kubernetes

import (
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"net/url"
	"sort"
)

func internalEdge(d *model.Deployment, edge model.Connection) bool {
	if edge.Address == "" {
		return true
	}

	address, err := url.Parse(edge.Address)
	if err != nil {
		return false
	}

	host := address.Hostname()

	return host == edge.To || host == edge.To+"."+namespace(d)+".svc.cluster.local"
}

func routeObject(d *model.Deployment, s model.Service, r model.Route) (object, error) {
	p, err := namedPort(s, r.Port)
	if err != nil {
		return nil, err
	}

	if p.Exposure != spec.ExposurePublic || (p.Protocol != "http" && p.Protocol != "") {
		return nil, fmt.Errorf("%s route requires a public HTTP port", s.Name)
	}

	if d.Target.GatewayAPI {
		return makeObject(d, "gateway.networking.k8s.io/v1", "HTTPRoute", s.Name+"-"+r.Port, object{"spec": object{"parentRefs": []any{object{"name": d.Target.Gateway}}, "hostnames": []string{r.Host}, "rules": []any{object{"matches": []any{object{"path": object{"type": "PathPrefix", "value": r.Path}}}, "backendRefs": []any{object{"name": s.Name, "port": p.Port}}}}}}), nil
	}

	body := object{"rules": []any{object{"host": r.Host, "http": object{"paths": []any{object{"path": r.Path, "pathType": "Prefix", "backend": object{"service": object{"name": s.Name, "port": object{"name": p.Name}}}}}}}}}
	if d.Target.IngressClass != "" {
		body["ingressClassName"] = d.Target.IngressClass
	}

	if r.TLS != "" {
		body["tls"] = []any{object{"hosts": []string{r.Host}, "secretName": r.TLS}}
	}

	return makeObject(d, "networking.k8s.io/v1", "Ingress", s.Name+"-"+r.Port, object{"spec": body}), nil
}
func peer(d *model.Deployment, name string) object {
	v := owner(d)
	v["forge.xraph.io/component"] = name

	return object{"podSelector": object{"matchLabels": v}}
}
func dnsRule() object {
	return object{"to": []any{object{"namespaceSelector": object{"matchLabels": object{"kubernetes.io/metadata.name": "kube-system"}}, "podSelector": object{"matchLabels": object{"k8s-app": "kube-dns"}}}}, "ports": []any{object{"protocol": "UDP", "port": 53}, object{"protocol": "TCP", "port": 53}}}
}
func cidrRule(cidrs []string) object {
	to := []any{}
	for _, cidr := range cidrs {
		to = append(to, object{"ipBlock": object{"cidr": cidr}})
	}

	return object{"to": to}
}
func networkObjects(d *model.Deployment) []object {
	ingress := map[string][]any{}
	egress := map[string][]any{}

	names := map[string]bool{}
	for _, s := range d.Services {
		names[s.Name] = true

		egress[s.Name] = []any{dnsRule()}
		if s.Discovery {
			rule := cidrRule(d.Target.APIServerCIDRs)
			rule["ports"] = []any{object{"protocol": "TCP", "port": 443}, object{"protocol": "TCP", "port": 6443}}
			egress[s.Name] = append(egress[s.Name], rule)
		}
	}

	for _, r := range d.Resources {
		if r.Lifecycle == spec.LifecycleContainer {
			names[r.Name] = true

			egress[r.Name] = []any{dnsRule()}
			if len(r.RuntimeRecipe.Init) > 0 {
				ingress[r.Name] = append(ingress[r.Name], object{"from": []any{peer(d, r.Name)}, "ports": []any{object{"protocol": "TCP", "port": r.RuntimeRecipe.Port}}})
				egress[r.Name] = append(egress[r.Name], object{"to": []any{peer(d, r.Name)}, "ports": []any{object{"protocol": "TCP", "port": r.RuntimeRecipe.Port}}})
			}
		}
	}

	services := map[string]model.Service{}
	for _, s := range d.Services {
		services[s.Name] = s
	}

	for _, edge := range d.Connections {
		if s, ok := services[edge.To]; ok && internalEdge(d, edge) {
			p, _ := namedPort(s, edge.Port)
			ports := []any{object{"protocol": transport(p.Protocol), "port": p.Port}}
			ingress[edge.To] = append(ingress[edge.To], object{"from": []any{peer(d, edge.From)}, "ports": ports})
			egress[edge.From] = append(egress[edge.From], object{"to": []any{peer(d, edge.To)}, "ports": ports})
		} else {
			egress[edge.From] = append(egress[edge.From], cidrRule(d.Target.ExternalCIDRs[edge.To]))
		}
	}

	for _, s := range d.Services {
		for _, binding := range s.Bindings {
			for _, r := range d.Resources {
				if binding.Resource != r.Name {
					continue
				}

				if r.Lifecycle == spec.LifecycleContainer {
					ports := []any{object{"protocol": "TCP", "port": r.RuntimeRecipe.Port}}
					ingress[r.Name] = append(ingress[r.Name], object{"from": []any{peer(d, s.Name)}, "ports": ports})
					egress[s.Name] = append(egress[s.Name], object{"to": []any{peer(d, r.Name)}, "ports": ports})
				} else {
					egress[s.Name] = append(egress[s.Name], cidrRule(d.Target.ExternalCIDRs[r.Name]))
				}
			}
		}
	}

	for _, r := range d.Routes {
		if r.Host == "" {
			continue
		}

		p, _ := namedPort(services[r.Service], r.Port)
		ingress[r.Service] = append(ingress[r.Service], object{"from": []any{object{"namespaceSelector": object{"matchLabels": object{"kubernetes.io/metadata.name": d.Target.IngressNamespace}}}}, "ports": []any{object{"protocol": "TCP", "port": p.Port}}})
	}

	keys := []string{}
	for name := range names {
		keys = append(keys, name)
	}

	sort.Strings(keys)

	out := []object{}

	for _, name := range keys {
		in, outbound := ingress[name], egress[name]
		if in == nil {
			in = []any{}
		}

		selector := owner(d)
		selector["forge.xraph.io/component"] = name
		out = append(out, makeObject(d, "networking.k8s.io/v1", "NetworkPolicy", name, object{"spec": object{"podSelector": object{"matchLabels": selector}, "policyTypes": []string{"Ingress", "Egress"}, "ingress": in, "egress": outbound}}))
	}

	return out
}
