package plugins

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/observability"
)

// observabilityEnv returns the variables dev sets for an app with the local
// stack on, and the names it left alone because they were already set. The
// stack's Prometheus scrapes the app, which is what carries exemplars, so OTLP
// metrics push goes off; with both on every series would land twice.
func observabilityEnv(getenv func(string) string, endpoint, service string) (map[string]string, []string) {
	set := map[string]string{}
	kept := []string{}

	for key, value := range map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": endpoint,
		"OTEL_SERVICE_NAME":           service,
		"OTEL_METRICS_EXPORTER":       "none",
	} {
		if getenv(key) != "" {
			kept = append(kept, key)

			continue
		}

		set[key] = value
	}

	sort.Strings(kept)

	return set, kept
}

// appPort follows the app's own order: PORT, then dev.port, then 8080. An
// address set in code can't be seen from here; obs status shows that target
// as down with Prometheus's error.
func appPort(getenv func(string) string, app *AppInfo) int {
	if n, err := strconv.Atoi(getenv("PORT")); err == nil && n > 0 {
		return n
	}

	if app != nil && app.AppConfig != nil && app.AppConfig.Dev.GetPort() > 0 {
		return app.AppConfig.Dev.GetPort()
	}

	return 8080
}

// observabilityWiring says how an app and the stack reach each other.
type observabilityWiring struct {
	network  string // docker network for the dev container; empty outside docker mode
	connect  bool   // forge-lgtm must also join network
	endpoint string // OTLP endpoint as the app sees it
	target   string // scrape address as Prometheus sees it
}

// dockerObservabilityWiring wires a dev container to the stack. The default
// bridge has no container DNS, so the container joins the stack's network; a
// network you chose stays and the stack joins it; host networking shares the
// host's ports, like plain forge dev. Container names work on Linux and Docker
// Desktop alike, where host.docker.internal and 127.0.0.1-only ports don't.
func dockerObservabilityWiring(network, app string, port, otlpPort int) observabilityWiring {
	inNetwork := observabilityWiring{
		network:  network,
		endpoint: "http://" + observability.ContainerName + ":4318",
		target:   devContainerName(app) + ":" + strconv.Itoa(port),
	}

	switch network {
	case "", "bridge":
		inNetwork.network = observability.NetworkName
	case "host":
		return observabilityWiring{
			network:  network,
			endpoint: fmt.Sprintf("http://localhost:%d", otlpPort),
			target:   "host.docker.internal:" + strconv.Itoa(port),
		}
	default:
		inNetwork.connect = true
	}

	return inNetwork
}

// startObservability makes sure the stack runs, registers the app as a scrape
// target and returns the env the app needs, plus the docker network a dev
// container should use (empty outside docker mode). The cleanup removes the
// target and leaves the stack running.
func (p *DevPlugin) startObservability(ctx cli.CommandContext, app *AppInfo, inDocker bool) (map[string]string, string, func(), error) {
	if err := checkDockerAvailable(ctx.Context()); err != nil {
		return nil, "", nil, err
	}

	dir, err := observability.DefaultStateDir()
	if err != nil {
		return nil, "", nil, err
	}

	stack := &observability.Stack{Runner: execx.System(), Dir: dir, Ports: observability.DefaultPorts(), Out: cliWriter{ctx}}

	st, err := stack.Inspect(ctx.Context())
	if err != nil {
		return nil, "", nil, err
	}

	ports := st.Ports
	if !st.Running || ports == (observability.Ports{}) {
		ctx.Info("Starting the observability stack...")

		if ports, err = stack.Up(ctx.Context()); err != nil {
			return nil, "", nil, err
		}
	}

	port := appPort(os.Getenv, app)
	wiring := observabilityWiring{
		endpoint: ports.OTLPURL(),
		target:   "host.docker.internal:" + strconv.Itoa(port),
	}
	getenv := os.Getenv

	if inDocker {
		wiring = dockerObservabilityWiring(p.resolveDockerConfig(app, ctx.String("network")).GetNetwork(), app.Name, port, ports.OTLP)
		// The container doesn't inherit this shell's env, so nothing there is
		// set yet; runWithDocker keeps any key the docker config already has.
		getenv = func(string) string { return "" }

		if wiring.connect {
			out, err := dockerCommand(ctx.Context(), "network", "connect", wiring.network, observability.ContainerName).CombinedOutput()
			if err != nil && !strings.Contains(string(out), "already exists") {
				ctx.Warning(fmt.Sprintf("couldn't connect %s to network %s, so the app may not reach it: %s", observability.ContainerName, wiring.network, strings.TrimSpace(string(out))))
			}
		}
	}

	if err := observability.WriteTarget(dir, observability.Target{Name: app.Name, Address: wiring.target}); err != nil {
		return nil, "", nil, err
	}

	set, kept := observabilityEnv(getenv, wiring.endpoint, app.Name)

	ctx.Success("Grafana: " + ports.GrafanaURL())

	for _, key := range sortedKeys(set) {
		ctx.Info(fmt.Sprintf("Set %s=%s", key, set[key]))
	}

	for _, key := range kept {
		if key == "OTEL_METRICS_EXPORTER" {
			ctx.Warning("OTEL_METRICS_EXPORTER is already set, so dev left it alone; if the app pushes metrics to the stack too, every series lands twice")

			continue
		}

		if key == "OTEL_SERVICE_NAME" {
			ctx.Warning("OTEL_SERVICE_NAME is already set, so dev left it alone; Grafana's trace-to-metrics link needs it to match the Prometheus job, " + app.Name)

			continue
		}

		ctx.Warning(key + " is already set, so dev left it alone; telemetry goes where it points, not to the local stack")
	}

	cleanup := func() {
		if err := observability.RemoveTarget(dir, app.Name); err != nil {
			ctx.Warning(err.Error())
		}
	}

	return set, wiring.network, cleanup, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
