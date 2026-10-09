package plugins

import (
	"errors"
	"time"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

// DeployPlugin wires forge deploy to the deploy engine.
type DeployPlugin struct {
	config *config.ForgeConfig
}

func NewDeployPlugin(cfg *config.ForgeConfig) cli.Plugin {
	registerDeployRenderers()

	return &DeployPlugin{config: cfg}
}

func (p *DeployPlugin) Name() string           { return "deploy" }
func (p *DeployPlugin) Version() string        { return "2.0.0" }
func (p *DeployPlugin) Description() string    { return "Describe, plan and apply deployments" }
func (p *DeployPlugin) Dependencies() []string { return nil }
func (p *DeployPlugin) Initialize() error      { return nil }

// deployFlags are registered on every deploy subcommand.
func deployFlags() []cli.CommandOption {
	return []cli.CommandOption{
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output format: text or json", "text")),
		cli.WithFlag(cli.NewBoolFlag("non-interactive", "", "Never prompt; fail where a prompt would appear", false)),
		cli.WithFlag(cli.NewStringFlag("config", "", "Path to .forge.yml", "")),
		cli.WithFlag(cli.NewStringFlag("target", "t", "Target name from deploy.targets", "")),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment name from deploy.environments", "")),
		cli.WithFlag(cli.NewDurationFlag("timeout", "", "Overall timeout", 10*time.Minute)),
		cli.WithFlag(cli.NewBoolFlag("no-color", "", "Disable colour", false)),
	}
}

// planned lists subcommands that later releases add. Help shows them so the
// surface is stable; each returns exit 4 until it lands.
var planned = []struct{ name, desc string }{
	{"migrate", "Migrate a legacy deployment configuration to version 2"},
	{"schema", "Print the deployment JSON Schema"},
	{"init", "Write a suggested deploy section from what the project declares"},
	{"start", "Open the local deployment page"},
	{"inspect", "Show the resolved deployment, suggestions and open decisions"},
	{"doctor", "Check the project, tools and secrets for a target"},
	{"plan", "Write an immutable plan and print its hash"},
	{"export", "Render provider artifacts to deployments/<target>/<env>"},
	{"apply", "Apply an approved plan"},
	{"up", "Plan and apply the default environment"},
	{"status", "Show the state of an environment"},
	{"logs", "Stream logs for a service"},
	{"rollback", "Restore a previous release"},
	{"destroy", "Remove an environment's workloads, keeping data by default"},
	{"providers", "List adapters and their support level"},
	{"catalog", "List descriptors and recipes"},
}

func (p *DeployPlugin) Commands() []cli.Command {
	deployCmd := cli.NewCommand("deploy", "Describe, plan and apply deployments", p.help)

	handlers := map[string]cli.CommandHandler{"migrate": p.migrate, "schema": p.schema, "inspect": p.inspect, "init": p.init, "doctor": p.doctor}
	for _, c := range planned {
		name := c.name

		handler, ok := handlers[name]
		if !ok {
			handler = func(ctx cli.CommandContext) error {
				return output.Unsupported("forge deploy "+name, "This command is not available yet.")
			}
		}

		opts := deployFlags()
		if name == "init" {
			opts = append(opts, cli.WithFlag(cli.NewBoolFlag("force", "", "Add missing deployment keys without replacing existing values", false)))
			opts = append(opts, cli.WithFlag(cli.NewBoolFlag("yes", "y", "Write without confirming", false)), cli.WithFlag(cli.NewStringSliceFlag("answer", "", "Answer a decision as path=value", nil)))
		}

		if name == "doctor" {
			opts = append(opts, cli.WithFlag(cli.NewBoolFlag("offline", "", "Skip target checks", false)))
		}
		if name == "migrate" {
			opts = append(opts, cli.WithFlag(cli.NewBoolFlag("dry-run", "", "Preview changes", false)), cli.WithFlag(cli.NewBoolFlag("yes", "y", "Write the migration", false)))
		}

		if err := deployCmd.AddSubcommand(cli.NewCommand(name, c.desc, deployOutputHandler(name, handler), opts...)); err != nil {
			panic(err)
		}
	}

	return []cli.Command{deployCmd}
}

func deployOutputHandler(name string, handler cli.CommandHandler) cli.CommandHandler {
	return func(ctx cli.CommandContext) error {
		format := ctx.String("output")
		if format != "text" && format != "json" {
			return output.Fail(output.ExitInvalidInput, "output must be text or json")
		}

		err := handler(ctx)
		if err == nil || format != "json" {
			return err
		}

		var deployErr *output.Error

		var diagnostics output.Diagnostics
		if errors.As(err, &deployErr) {
			if deployErr.Emitted {
				return err
			}
			diagnostics = deployErr.Diagnostics
		}

		if len(diagnostics) == 0 {
			diagnostics = output.Diagnostics{{Code: output.CodeConfigInvalid, Severity: output.SeverityError, Message: err.Error()}}
		}

		if emitErr := output.Emit(ctx, output.ModeFromContext(ctx), output.Envelope{Command: name, OK: false, Diagnostics: diagnostics}); emitErr != nil {
			return emitErr
		}

		return err
	}
}

func (p *DeployPlugin) help(ctx cli.CommandContext) error {
	if ctx.NArgs() != 0 {
		return output.Fail(output.ExitInvalidInput, "unknown deploy command: "+ctx.Arg(0))
	}

	ctx.Info("forge deploy: describe, plan and apply deployments\n")
	ctx.Println("Usage: forge deploy <command> [flags]")
	ctx.Println("")

	for _, c := range planned {
		ctx.Println("  " + padRight(c.name, 10) + c.desc)
	}

	ctx.Println("")
	ctx.Println("Flags on every command: --output json|text, --non-interactive, --config, --target, --env, --timeout, --no-color")

	return nil
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}

	return s
}
