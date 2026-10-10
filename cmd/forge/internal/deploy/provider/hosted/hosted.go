// Package hosted exports ctrlplane-compatible workload contracts without account-side mutation.
package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"maps"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Hosted struct{ provider.ExportOnly }

func New(_ string) *Hosted                                  { return &Hosted{ExportOnly: provider.ExportOnly{ProviderName: "hosted"}} }
func Factory(_ execx.Runner, root string) provider.Provider { return New(root) }
func (*Hosted) Capabilities(context.Context, spec.Target) (model.Capabilities, error) {
	resources := map[model.ResourceType][]model.Lifecycle{}
	for _, kind := range []model.ResourceType{model.Postgres, model.MySQL, model.SQLite, model.MongoDB, model.ClickHouse, model.Turso, model.Redis, model.Memcached, model.NATS, model.Kafka, model.RabbitMQ, model.ObjectStorage, model.SMTP, model.MQTT, model.Meilisearch, model.Elasticsearch, model.Typesense} {
		resources[kind] = []model.Lifecycle{spec.LifecycleExternal}
	}

	return model.Capabilities{Level: model.LevelRenderable, Resources: resources, FileMounts: true}, nil
}

var immutable = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
var variable = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`)
var vaultKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]{0,255}$`)

func (p *Hosted) Validate(ctx context.Context, d *model.Deployment) output.Diagnostics {
	var ds output.Diagnostics

	fail := func(message string) {
		ds = append(ds, output.Diagnostic{Code: output.CodeUnsupportedCommand, Severity: output.SeverityError, Message: message})
	}
	if d == nil {
		fail("deployment is required")

		return ds
	}

	if d.Target.Build.Delivery != "registry" || (d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci") {
		fail("hosted export requires existing or CI registry images")
	}

	if d.Target.Release.Mode == "gitops" || d.Target.NetworkPolicy || d.Target.GatewayAPI || d.Target.Gateway != "" {
		fail("hosted networking and controller settings require an account-side mapping")
	}

	if d.Target.Build.Registry.Visibility == "private" {
		fail("private hosted registry credentials require an account-side qualification")
	}

	if len(d.Migrations) > 0 {
		fail("hosted migration execution is not qualified")
	}

	for _, r := range d.Resources {
		if r.Lifecycle != spec.LifecycleExternal {
			fail("hosted resources must be provisioned externally")
		}

		if r.Secret.Name == "" || !vaultKey.MatchString(d.Target.SecretKeys[r.Secret.Name]) {
			fail("map each hosted resource secret reference to an existing vault key in target.secret_keys")
		}
	}

	for _, c := range d.Connections {
		u, e := url.Parse(c.Address)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			fail("hosted connections require explicit credential-free external URLs until hosted service routing is qualified")
		}
	}

	for _, s := range d.Services {
		if !namePattern.MatchString(s.Name) {
			fail("hosted service names must be DNS-safe")
		}

		if !immutable.MatchString(s.Image.Digest) || s.Image.Repository == "" {
			fail("hosted images require immutable registry digests")
		}

		if s.Kind != spec.KindWeb && s.Kind != spec.KindWorker && s.Kind != spec.KindGateway {
			fail("hosted job and cron lifecycle is not qualified")
		}

		if s.Discovery || len(s.Migrate) > 0 || len(s.ConfigFiles) > 0 {
			fail("hosted discovery, migrations and arbitrary config mounts are not qualified")
		}

		if s.Health.Heartbeat {
			fail("hosted heartbeat observation is not qualified")
		}

		if _, e := compute(s); e != nil {
			fail(e.Error())
		}

		for _, key := range []string{"FORGE_SERVICE_ID", "FORGE_INSTANCE_ID"} {
			if _, ok := s.Env[key]; ok {
				fail("service identity fields are supplied by the deployment authority")
			}
		}

		for _, port := range s.Ports {
			if port.Port < 1 || port.Port > 65535 || (port.Protocol != "" && port.Protocol != "http" && port.Protocol != "https" && port.Protocol != "tcp" && port.Protocol != "udp") {
				fail("hosted ports require a supported transport and valid container port")
			}
		}

		if _, e := workload(ctx, d, s); e != nil {
			fail(e.Error())
		}
	}

	return ds
}
func compute(s model.Service) (Resources, error) {
	invalid := errors.New("hosted compute requires positive CPU cores or millicores and integer Mi/Gi/M/G memory; distinct CPU/memory limits are not mapped")
	if s.Resources.CPULimit != "" || s.Resources.MemoryLimit != "" {
		return Resources{}, invalid
	}

	cpu := s.Resources.CPU
	if cpu == "" {
		cpu = "100m"
	}

	multiplier := 1000.0

	if before, ok := strings.CutSuffix(cpu, "m"); ok {
		cpu = before
		multiplier = 1
	}

	v, e := strconv.ParseFloat(cpu, 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v*multiplier > 1000000 {
		return Resources{}, invalid
	}

	millis := int(math.Ceil(v * multiplier))

	memory := s.Resources.Memory
	if memory == "" {
		memory = "128Mi"
	}

	parts := regexp.MustCompile(`^([1-9][0-9]*)(Mi|Gi|M|G)$`).FindStringSubmatch(memory)
	if len(parts) != 3 {
		return Resources{}, invalid
	}

	size, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || size > 1000000 {
		return Resources{}, invalid
	}

	units := map[string]float64{"Mi": 1, "Gi": 1024, "M": 1000000.0 / 1048576, "G": 1000000000.0 / 1048576}

	mb := int(math.Ceil(float64(size) * units[parts[2]]))
	if mb > 1000000 {
		return Resources{}, invalid
	}

	replicas := s.Replicas
	if replicas == 0 {
		replicas = 1
	}

	if replicas < 1 || replicas > 1000 {
		return Resources{}, invalid
	}

	return Resources{CPUMillis: millis, MemoryMB: mb, Replicas: replicas}, nil
}
func workload(ctx context.Context, d *model.Deployment, s model.Service) (Workload, error) {
	if e := ctx.Err(); e != nil {
		return Workload{}, e
	}

	resources, e := compute(s)
	if e != nil {
		return Workload{}, e
	}

	overlay, e := resolve.Overlay(d, &s)
	if e != nil {
		return Workload{}, errors.New("hosted overlay cannot be rendered")
	}

	refs := map[string]SecretBinding{}

	for _, r := range d.Resources {
		for _, consumer := range r.UsedBy {
			if consumer == s.Name {
				refs[r.Secret.EnvVar] = SecretBinding{VarName: r.Secret.Name, EnvKey: r.Secret.EnvVar, Ref: SecretRef{Key: d.Target.SecretKeys[r.Secret.Name], Type: "env"}}
			}
		}
	}

	env := maps.Clone(s.Env)
	if env == nil {
		env = map[string]string{}
	}

	port := 8080

	for _, p := range s.Ports {
		if p.Protocol == "http" || p.Protocol == "https" || p.Protocol == "" {
			port = p.Port

			break
		}
	}

	env["PORT"] = strconv.Itoa(port)
	env["FORGE_HTTP_PORT"] = strconv.Itoa(port)
	env["FORGE_SERVICE_ID"] = s.Name

	mount := "/etc/forge/overlay.yaml"
	if d.Overlay == model.OverlayFallback {
		mount = "/app/config.local.yaml"
	} else {
		env["FORGE_CONFIG_OVERLAY"] = mount
	}

	needed := map[string]SecretBinding{}

	for _, match := range variable.FindAllStringSubmatch(string(overlay), -1) {
		ref, ok := refs[match[1]]
		if !ok || !vaultKey.MatchString(ref.Ref.Key) {
			return Workload{}, errors.New("hosted overlay variable needs a resource vault binding")
		}

		needed[match[1]] = ref
	}

	for key, value := range s.Env {
		matches := variable.FindAllStringSubmatch(value, -1)
		if len(matches) == 0 {
			continue
		}

		if len(matches) != 1 || value != matches[0][0] {
			return Workload{}, errors.New("hosted environment aliases must reference one vault-backed resource variable")
		}

		ref, ok := refs[matches[0][1]]
		if !ok || !vaultKey.MatchString(ref.Ref.Key) {
			return Workload{}, errors.New("hosted environment variable needs a resource vault binding")
		}

		ref.EnvKey = key
		needed[key] = ref
		delete(env, key)
	}

	bindings := []SecretBinding{}

	keys := []string{}
	for key := range needed {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	secretRefs := []SecretRef{}
	seen := map[string]bool{}

	for _, key := range keys {
		binding := needed[key]

		bindings = append(bindings, binding)
		if !seen[binding.Ref.Key] {
			secretRefs = append(secretRefs, binding.Ref)
			seen[binding.Ref.Key] = true
		}
	}

	service := Service{Name: s.Name, Image: images.Ref(s.Image), Role: "main", Resources: resources, Env: env, Secrets: secretRefs, ConfigFiles: []model.ConfigFile{{Name: "forge-overlay", Path: mount, Format: "yaml", Content: string(overlay)}}, Annotations: map[string]string{}}
	for _, p := range s.Ports {
		protocol := "tcp"
		if p.Protocol == "udp" {
			protocol = "udp"
		}

		service.Ports = append(service.Ports, Port{Container: p.Port, Protocol: protocol})
	}

	if !s.Health.None && s.Health.Readiness != "" {
		service.HealthCheck = &HealthCheck{Path: s.Health.Readiness, Port: port, Interval: int64(5 * time.Second), Timeout: int64(2 * time.Second), Retries: 3}
	}

	if s.Health.Liveness != "" {
		service.Annotations["forge.xraph.io/liveness-path"] = s.Health.Liveness
	}

	if s.Health.Startup != "" {
		service.Annotations["forge.xraph.io/startup-path"] = s.Health.Startup
	}

	return Workload{Name: s.Name, Kind: "deployment", Services: []Service{service}, SecretBindings: bindings}, nil
}
func (p *Hosted) Render(ctx context.Context, d *model.Deployment) (*render.Bundle, error) {
	if ds := p.Validate(ctx, d); ds.HasErrors() {
		return nil, fmt.Errorf("invalid hosted handoff: %v", ds)
	}

	out := Export{Schema: "forge.hosted/v1", Project: d.Project, Environment: d.Environment, Workloads: []Workload{}}
	for _, s := range d.Services {
		w, e := workload(ctx, d, s)
		if e != nil {
			return nil, e
		}

		out.Workloads = append(out.Workloads, w)
	}

	sort.Slice(out.Workloads, func(i, j int) bool { return out.Workloads[i].Name < out.Workloads[j].Name })

	raw, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		return nil, e
	}

	b := render.New(d.TargetName, d.Environment)
	b.Add("forge-hosted.json", append(raw, '\n'))
	b.Add("HANDOFF.md", []byte("# Hosted workload export\n\nImport forge-hosted.json through a trusted hosted authority. It contains one main service per workload and separate vault-backed secret bindings. The authority supplies tenant and instance IDs, authorizes every vault key, materializes each binding at its EnvKey, and provisions workloads. The export does not call a hosted API.\n\nOnly existing external resources and immutable images are supported. Service routing, discovery, private image pulls, migrations and arbitrary mounts need account-side qualification. The provider contract has one readiness health check; liveness and startup paths are retained as annotations and require account-side probe configuration. Configure public routing and replica identity through the authority before rollout.\n"))

	return b, nil
}
func (*Hosted) Operations(context.Context, *model.Deployment, *render.Bundle, state.Snapshot) ([]plan.Operation, error) {
	return []plan.Operation{{ID: "hosted-handoff", Kind: plan.OpDeliver, Detail: "Import workload contracts and authorize vault bindings through the hosted authority"}}, nil
}
