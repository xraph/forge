package plugins

import (
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

	for _, c := range planned {
		name := c.name

		err := deployCmd.AddSubcommand(cli.NewCommand(name, c.desc, func(ctx cli.CommandContext) error {
			return output.Unsupported("forge deploy "+name, "This subcommand lands in a later release. Run forge deploy for what works today.")
		}, deployFlags()...))
		if err != nil {
			panic(err)
		}
	}

	return []cli.Command{deployCmd}
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
