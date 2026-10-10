package kubernetes

import (
	"context"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func (*Kubernetes) Validate(_ context.Context, d *model.Deployment) output.Diagnostics {
	var ds output.Diagnostics

	fail := func(field, msg string) {
		ds = append(ds, output.Diagnostic{Code: output.CodeLifecycleUnsupported, Severity: output.SeverityError, Field: field, Message: msg})
	}
	if d.Target.Context == "" {
		fail("context", "select an explicit Kubernetes context")
	}

	if !dnsLabel.MatchString(namespace(d)) {
		fail("namespace", "use a DNS-safe Kubernetes namespace with at most 63 characters")
	}

	for field, name := range map[string]string{"project": d.Project, "target": d.TargetName, "environment": d.Environment} {
		if !dnsLabel.MatchString(name) {
			fail(field, "Kubernetes ownership labels require DNS-safe names")
		}
	}

	services := map[string]model.Service{}
	names := map[string]bool{}
	generated := map[string]bool{}
	reserve := func(kind, name string) {
		key := kind + "/" + name
		if generated[key] {
			fail(name, "duplicate generated Kubernetes object "+key)
		}

		generated[key] = true
	}

	for _, s := range d.Services {
		if !dnsLabel.MatchString(s.Name) || len(s.Name) > 42 {
			fail(s.Name, "service name must be DNS-safe and at most 42 characters to reserve generated suffixes")
		}

		if names[s.Name] {
			fail(s.Name, "duplicate generated Service name")
		}

		names[s.Name] = true

		services[s.Name] = s
		if s.Kind == spec.KindJob {
			reserve("Job", jobName(d, s.Name))
		}

		if len(s.Migrate) > 0 {
			reserve("Job", jobName(d, s.Name+"-migrate"))
		}

		if s.Replicas < 1 {
			fail(s.Name, "replicas must be positive")
		}

		if s.Health.Heartbeat {
			fail(s.Name, "Kubernetes needs an HTTP health endpoint or health.none; heartbeat observation is unsupported")
		}

		for _, path := range []string{s.Health.Readiness, s.Health.Liveness, s.Health.Startup} {
			if path != "" && !strings.HasPrefix(path, "/") {
				fail(s.Name, "HTTP probe paths must begin with /")
			}
		}

		ports := map[string]bool{}
		numbers := map[string]bool{}

		for _, p := range s.Ports {
			if !dnsLabel.MatchString(p.Name) || len(p.Name) > 15 || ports[p.Name] {
				fail(s.Name, "container port names must be unique DNS labels with at most 15 characters")
			}

			ports[p.Name] = true

			key := strconv.Itoa(p.Port) + "/" + transport(p.Protocol)
			if p.Port < 1 || p.Port > 65535 || numbers[key] {
				fail(s.Name, "ports must be valid and unique per protocol")
			}

			numbers[key] = true

			if p.Protocol != "" && p.Protocol != "http" && p.Protocol != "grpc" && p.Protocol != "tcp" && p.Protocol != "udp" {
				fail(s.Name, "unsupported service protocol")
			}
		}

		if environmentCycle(s.Env) {
			fail(s.Name, "environment references contain a cycle")
		}

		for key := range s.Env {
			if key == "FORGE_SERVICE_ID" || key == "FORGE_INSTANCE_ID" {
				fail(s.Name, "service identity fields are supplied by the deployment authority")
			}

			if key == "POD_IP" {
				fail(s.Name, "POD_IP is supplied by the Kubernetes Downward API")
			}
		}
	}

	for _, r := range d.Resources {
		if names[r.Name] {
			fail(r.Name, "resource and application share a generated Service name")
		}

		names[r.Name] = true
		if !dnsLabel.MatchString(r.Name) || len(r.Name) > 42 {
			fail(r.Name, "resource name must be DNS-safe and at most 42 characters")
		}

		if r.Lifecycle == spec.LifecycleManaged {
			fail(r.Name, "Kubernetes adapter does not provision managed resources")
		}

		if r.Lifecycle == spec.LifecycleContainer && (r.RuntimeRecipe == nil || r.RuntimeRecipe.Image == "") {
			fail(r.Name, "container resource requires a frozen runtime recipe")
		}

		if r.Lifecycle == spec.LifecycleContainer && r.RuntimeRecipe != nil && len(r.RuntimeRecipe.Init) > 0 {
			reserve("Job", jobName(d, r.Name+"-init"))
		}

		if d.Target.NetworkPolicy && r.Lifecycle == spec.LifecycleExternal && len(d.Target.ExternalCIDRs[r.Name]) == 0 {
			fail(r.Name, "network policy requires external_cidrs for this resource")
		}
	}

	for _, edge := range d.Connections {
		if s, ok := services[edge.To]; ok && internalEdge(d, edge) {
			if _, err := namedPort(s, edge.Port); err != nil {
				fail(edge.To, err.Error())
			}
		} else if d.Target.NetworkPolicy && len(d.Target.ExternalCIDRs[edge.To]) == 0 {
			fail(edge.To, "network policy requires external_cidrs for an external service")
		}
	}

	routeNames := map[string]bool{}

	for _, route := range d.Routes {
		if route.Host == "" {
			continue
		}

		key := route.Service + "-" + route.Port
		if routeNames[key] {
			fail(key, "duplicate generated route name")
		}

		routeNames[key] = true
		s, ok := services[route.Service]

		p, err := namedPort(s, route.Port)
		if !ok || err != nil || p.Exposure != spec.ExposurePublic || (p.Protocol != "http" && p.Protocol != "") {
			fail(route.Service, "routes require an explicit public HTTP port")
		}

		if route.Host == "" || !strings.HasPrefix(route.Path, "/") {
			fail(route.Service, "route needs a host and an absolute path")
		}

		if d.Target.NetworkPolicy && d.Target.IngressNamespace == "" {
			fail(route.Service, "network policy requires ingress_namespace for HTTP routes")
		}
	}

	if d.Target.GatewayAPI && len(d.Routes) > 0 && d.Target.Gateway == "" {
		fail("gateway", "Gateway API routes need an existing gateway name")
	}

	for name, cidrs := range d.Target.ExternalCIDRs {
		for _, cidr := range cidrs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				fail(name, "invalid external CIDR: "+cidr)
			}
		}
	}

	for _, cidr := range d.Target.APIServerCIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			fail("api_server_cidrs", "invalid API-server CIDR")
		}
	}

	for _, s := range d.Services {
		if d.Target.NetworkPolicy && s.Discovery && len(d.Target.APIServerCIDRs) == 0 {
			fail(s.Name, "discovery with network policy requires api_server_cidrs")
		}
	}

	switch d.Target.Build.Source {
	case "", "local", "remote", "existing", "ci":
	default:
		fail("build.source", "Kubernetes uses local/remote builds or immutable CI/existing images")
	}

	if d.Target.LocalCluster != "" && d.Target.Context != "kind-"+d.Target.LocalCluster {
		fail("local_cluster", "local image delivery requires context kind-"+d.Target.LocalCluster)
	}

	ds = append(ds, validateGitOps(d)...)

	return ds
}

func environmentCycle(values map[string]string) bool {
	visited := map[string]bool{}
	active := map[string]bool{}

	var visit func(string) bool

	visit = func(key string) bool {
		if active[key] {
			return true
		}

		if visited[key] {
			return false
		}

		active[key] = true

		value := values[key]
		if value != "${"+key+"}" {
			for _, ref := range variable.FindAllStringSubmatch(value, -1) {
				if _, ok := values[ref[1]]; ok && visit(ref[1]) {
					return true
				}
			}
		}

		delete(active, key)
		visited[key] = true

		return false
	}
	for key := range values {
		if visit(key) {
			return true
		}
	}

	return false
}
