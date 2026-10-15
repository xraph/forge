package plugins

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/observability"
)

// ObservabilityPlugin runs the local Grafana stack and exports its assets.
type ObservabilityPlugin struct {
	config *config.ForgeConfig
	runner execx.Runner
}

// NewObservabilityPlugin creates the observability plugin.
func NewObservabilityPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &ObservabilityPlugin{config: cfg, runner: execx.System()}
}

func (p *ObservabilityPlugin) Name() string           { return "observability" }
func (p *ObservabilityPlugin) Version() string        { return "1.0.0" }
func (p *ObservabilityPlugin) Description() string    { return "Local Grafana stack and dashboards" }
func (p *ObservabilityPlugin) Dependencies() []string { return nil }
func (p *ObservabilityPlugin) Initialize() error      { return nil }

func (p *ObservabilityPlugin) Commands() []cli.Command {
	root := cli.NewCommand("observability", "Run a local Grafana stack and export dashboards", p.status,
		cli.WithAliases("obs"))

	defaults := observability.DefaultPorts()
	portFlags := []cli.Flag{
		cli.NewIntFlag("grafana-port", "", "Host port for Grafana", defaults.Grafana),
		cli.NewIntFlag("otlp-port", "", "Host port for OTLP/HTTP", defaults.OTLP),
		cli.NewIntFlag("prometheus-port", "", "Host port for Prometheus", defaults.Prometheus),
	}

	mustAddSubcommand(root, cli.NewCommand("up", "Start the stack (Grafana, Tempo, Loki, Prometheus, collector)", p.up,
		cli.WithFlag(cli.NewStringSliceFlag("target", "t", "Extra scrape target, name=host:port (repeatable)", nil)),
		cli.WithFlag(cli.NewStringFlag("image", "", "Stack image", observability.Image)),
		cli.WithFlags(portFlags...),
	))
	mustAddSubcommand(root, cli.NewCommand("down", "Stop and remove the stack", p.down,
		cli.WithFlag(cli.NewBoolFlag("purge", "", "Also delete stored data and generated files", false)),
	))
	mustAddSubcommand(root, cli.NewCommand("status", "Show the stack and its scrape targets", p.status))
	mustAddSubcommand(root, cli.NewCommand("export", "Write the dashboard and Prometheus rules for your own Grafana", p.export,
		cli.WithFlag(cli.NewStringFlag("out", "o", "Output directory", "obs")),
		cli.WithFlag(cli.NewStringFlag("slo", "", "Availability objective in percent", "99.9")),
		cli.WithFlag(cli.NewStringFlag("p95-threshold", "", "p95 latency that raises ForgeHighLatencyP95", "1s")),
		cli.WithFlag(cli.NewBoolFlag("force", "f", "Overwrite existing files", false)),
	))

	return []cli.Command{root}
}

// stack builds the Stack. Only up reads the port and image flags; down and
// status act on whatever container exists.
func (p *ObservabilityPlugin) stack(ctx cli.CommandContext, fromFlags bool) (*observability.Stack, error) {
	dir, err := observability.DefaultStateDir()
	if err != nil {
		return nil, err
	}

	s := &observability.Stack{Runner: p.runner, Dir: dir, Ports: observability.DefaultPorts(), Out: cliWriter{ctx}}
	if fromFlags {
		s.Ports = observability.Ports{Grafana: ctx.Int("grafana-port"), OTLP: ctx.Int("otlp-port"), Prometheus: ctx.Int("prometheus-port")}
		s.Image = ctx.String("image")
	}

	return s, nil
}

func (p *ObservabilityPlugin) up(ctx cli.CommandContext) error {
	if err := checkDockerAvailable(ctx.Context()); err != nil {
		return err
	}

	s, err := p.stack(ctx, true)
	if err != nil {
		return err
	}

	for _, raw := range ctx.StringSlice("target") {
		t, err := observability.ParseTarget(raw)
		if err != nil {
			return err
		}

		if t.Rewritten {
			ctx.Info(fmt.Sprintf("%s: the container reaches your machine as %s", t.Name, t.Address))
		}

		if err := observability.WriteTarget(s.Dir, t); err != nil {
			return err
		}
	}

	ctx.Info("Starting the observability stack...")

	ports, err := s.Up(ctx.Context())
	if err != nil {
		return err
	}

	printStack(ctx, ports)

	return nil
}

func (p *ObservabilityPlugin) down(ctx cli.CommandContext) error {
	if err := checkDockerAvailable(ctx.Context()); err != nil {
		return err
	}

	s, err := p.stack(ctx, false)
	if err != nil {
		return err
	}

	if err := s.Down(ctx.Context(), ctx.Bool("purge")); err != nil {
		return err
	}

	ctx.Success("Observability stack removed")

	return nil
}

func (p *ObservabilityPlugin) status(ctx cli.CommandContext) error {
	if err := checkDockerAvailable(ctx.Context()); err != nil {
		return err
	}

	s, err := p.stack(ctx, false)
	if err != nil {
		return err
	}

	st, err := s.Inspect(ctx.Context())
	if err != nil {
		return err
	}

	if !st.Running {
		ctx.Info("The observability stack is not running. Start it with: forge obs up")

		return nil
	}

	printStack(ctx, st.Ports)

	client := &http.Client{Timeout: 3 * time.Second}

	targets, err := observability.Targets(ctx.Context(), client, "http://127.0.0.1:"+strconv.Itoa(st.Ports.Prometheus))
	switch {
	case err != nil:
		ctx.Warning(err.Error())
	case len(targets) == 0:
		ctx.Info("No scrape targets yet. Run forge dev --observability, or forge obs up --target name=host:port")
	default:
		table := ctx.Table()
		table.SetHeader([]string{"Job", "Target", "Health", "Last error"})

		for _, t := range targets {
			table.AppendRow([]string{t.Job, t.URL, t.Health, t.LastError})
		}

		table.Render()
	}

	ctx.Info("Logs reach Loki once your app exports them over OTLP.")

	return nil
}

func (p *ObservabilityPlugin) export(ctx cli.CommandContext) error {
	o, err := rulesOptions(ctx.String("slo"), ctx.String("p95-threshold"))
	if err != nil {
		return err
	}

	paths, err := observability.Export(ctx.String("out"), o, ctx.Bool("force"))
	if err != nil {
		return err
	}

	for _, path := range paths {
		ctx.Success("Wrote " + path)
	}

	return nil
}

func rulesOptions(slo, p95 string) (observability.RulesOptions, error) {
	v, err := strconv.ParseFloat(slo, 64)
	if err != nil {
		return observability.RulesOptions{}, fmt.Errorf("--slo %q is not a number", slo)
	}

	d, err := time.ParseDuration(p95)
	if err != nil {
		return observability.RulesOptions{}, fmt.Errorf("--p95-threshold %q is not a duration such as 500ms or 1s", p95)
	}

	return observability.RulesOptions{SLO: v, P95Threshold: d}, nil
}

func printStack(ctx cli.CommandContext, ports observability.Ports) {
	ctx.Success("Observability stack is up")
	ctx.Println("  Grafana:     " + ports.GrafanaURL())
	ctx.Println("  OTLP/HTTP:   " + ports.OTLPURL())
	ctx.Println("  Prometheus:  " + ports.PrometheusURL())
}

// cliWriter sends Stack progress lines through the CLI's output.
type cliWriter struct{ ctx cli.CommandContext }

func (w cliWriter) Write(b []byte) (int, error) {
	w.ctx.Printf("%s", b)

	return len(b), nil
}
