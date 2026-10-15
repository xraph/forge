package kubernetes

import (
	"maps"
	"net/url"
	"strconv"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

// observabilityEnv is the OTEL_* env a target's observability block injects.
// applicationPod copies the service's own env over it, so what you set wins.
func observabilityEnv(d *model.Deployment, s model.Service) map[string]string {
	o := d.Target.Observability
	if o == nil || o.OTLPEndpoint == "" {
		return nil
	}

	env := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": o.OTLPEndpoint,
		"OTEL_SERVICE_NAME":           s.Name,
		"OTEL_RESOURCE_ATTRIBUTES":    "deployment.environment.name=" + d.Environment + ",k8s.namespace.name=" + namespace(d),
	}
	if o.Sampler != "" {
		env["OTEL_TRACES_SAMPLER"] = o.Sampler
	}

	if o.SampleRatio != nil {
		env["OTEL_TRACES_SAMPLER_ARG"] = strconv.FormatFloat(*o.SampleRatio, 'f', -1, 64)
	}

	return env
}

// metricsPort is the HTTP port forge serves /_/metrics on.
func metricsPort(s model.Service) (model.Port, bool) {
	for _, p := range s.Ports {
		if p.Protocol == "http" || p.Name == "http" {
			return p, true
		}
	}

	return model.Port{}, false
}

// serviceMonitorObject selects the service's Service by its labels. The
// Operator names the job after the Service, which matches OTEL_SERVICE_NAME.
func serviceMonitorObject(d *model.Deployment, s model.Service) (object, bool) {
	o := d.Target.Observability
	if !o.MonitorEnabled() {
		return nil, false
	}

	port, ok := metricsPort(s)
	if !ok {
		return nil, false
	}

	obj := makeObject(d, "monitoring.coreos.com/v1", "ServiceMonitor", s.Name, object{"spec": object{
		"selector":  object{"matchLabels": labels(d, s.Name, "app")},
		"endpoints": []any{object{"port": port.Name, "path": "/_/metrics", "interval": o.MonitorInterval()}},
	}})

	meta := obj["metadata"].(map[string]any)
	merged := object{}

	for k, v := range o.ServiceMonitor.Labels {
		merged[k] = v
	}

	// Ownership labels win: prune and destroy find objects by them.
	maps.Copy(merged, labels(d, s.Name, "app"))
	meta["labels"] = merged

	return obj, true
}

// otlpEgress allows traffic to the collector's port, toward otlp_namespace or
// the otlp entry of external_cidrs.
func otlpEgress(d *model.Deployment) (object, bool) {
	o := d.Target.Observability
	if o == nil || o.OTLPEndpoint == "" {
		return nil, false
	}

	u, err := url.Parse(o.OTLPEndpoint)
	if err != nil {
		return nil, false
	}

	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return nil, false
	}

	ports := []any{object{"protocol": "TCP", "port": n}}

	if o.OTLPNamespace != "" {
		return object{"to": []any{object{"namespaceSelector": object{"matchLabels": object{"kubernetes.io/metadata.name": o.OTLPNamespace}}}}, "ports": ports}, true
	}

	rule := cidrRule(d.Target.ExternalCIDRs["otlp"])
	rule["ports"] = ports

	return rule, true
}
