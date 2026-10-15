package plugins

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/observability"
)

// observabilityEnv returns the variables dev sets for an app with the local
// stack on, and the names it left alone because they were already set.
func observabilityEnv(getenv func(string) string, endpoint, service string) (map[string]string, []string) {
	set := map[string]string{}
	kept := []string{}

	for key, value := range map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": endpoint,
		"OTEL_SERVICE_NAME":           service,
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

// startObservability makes sure the stack runs, registers the app as a scrape
// target and returns the env the app needs. The cleanup removes the target and
// leaves the stack running.
func (p *DevPlugin) startObservability(ctx cli.CommandContext, app *AppInfo, inDocker bool) (map[string]string, func(), error) {
	if err := checkDockerAvailable(ctx.Context()); err != nil {
		return nil, nil, err
	}

	dir, err := observability.DefaultStateDir()
	if err != nil {
		return nil, nil, err
	}

	stack := &observability.Stack{Runner: execx.System(), Dir: dir, Ports: observability.DefaultPorts(), Out: cliWriter{ctx}}

	st, err := stack.Inspect(ctx.Context())
	if err != nil {
		return nil, nil, err
	}

	ports := st.Ports
	if !st.Running || ports == (observability.Ports{}) {
		ctx.Info("Starting the observability stack...")

		if ports, err = stack.Up(ctx.Context()); err != nil {
			return nil, nil, err
		}
	}

	target := observability.Target{Name: app.Name, Address: "host.docker.internal:" + strconv.Itoa(appPort(os.Getenv, app))}
	if err := observability.WriteTarget(dir, target); err != nil {
		return nil, nil, err
	}

	host, getenv := "localhost", os.Getenv
	if inDocker {
		// The container doesn't inherit this shell's env, so nothing there is
		// set yet; runWithDocker keeps any key the docker config already has.
		host, getenv = "host.docker.internal", func(string) string { return "" }
	}

	set, kept := observabilityEnv(getenv, fmt.Sprintf("http://%s:%d", host, ports.OTLP), app.Name)

	ctx.Success("Grafana: " + ports.GrafanaURL())

	for _, key := range sortedKeys(set) {
		ctx.Info(fmt.Sprintf("Set %s=%s", key, set[key]))
	}

	for _, key := range kept {
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

	return set, cleanup, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
