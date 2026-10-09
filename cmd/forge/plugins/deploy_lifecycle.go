package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

func (p *DeployPlugin) timeoutContext(ctx cli.CommandContext) (context.Context, context.CancelFunc) {
	duration := time.Duration(ctx.Duration("timeout"))
	if duration <= 0 {
		duration = 10 * time.Minute
	}

	return context.WithTimeout(ctx.Context(), duration)
}
func (p *DeployPlugin) makePlan(ctx cli.CommandContext, e *engine.Engine) (*plan.Plan, error) {
	timeout, cancel := p.timeoutContext(ctx)
	defer cancel()

	pl, _, err := e.PlanWithOptions(timeout, ctx.String("target"), ctx.String("env"), engine.PlanOptions{Services: ctx.StringSlice("services")})

	return pl, err
}
func (p *DeployPlugin) plan(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	pl, err := p.makePlan(ctx, e)
	if err != nil {
		return err
	}

	file := e.PlanPath(pl)
	if path := ctx.String("out"); path != "" {
		raw, err := json.MarshalIndent(pl, "", "  ")
		if err != nil {
			return err
		}

		if err := os.WriteFile(path, raw, 0600); err != nil {
			return err
		}

		if err := os.Chmod(path, 0600); err != nil {
			return err
		}

		file = path
	}

	if !mode.JSON {
		ctx.Println(planSummary(pl))
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "plan", OK: true, Data: map[string]any{"hash": pl.Hash, "file": file, "operations": pl.Operations, "images": pl.Images}, Diagnostics: pl.Diagnostics})
}
func (p *DeployPlugin) export(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	var pl *plan.Plan
	if path := ctx.String("plan"); path != "" {
		pl, err = plan.Load(path)
	} else {
		pl, err = p.makePlan(ctx, e)
	}

	if err != nil {
		return err
	}

	result, err := e.Export(ctx.Context(), pl, nil, ctx.String("output-dir"), ctx.Bool("force"))
	if err != nil {
		return err
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "export", OK: true, Data: result, Diagnostics: pl.Diagnostics})
}
func (p *DeployPlugin) apply(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	approve := ctx.String("approve-plan")
	if mode.NonInteractive && approve == "" {
		return output.Fail(output.ExitInvalidInput, "non-interactive apply needs --approve-plan <hash>")
	}

	var pl *plan.Plan
	if path := ctx.String("plan"); path != "" {
		pl, err = plan.Load(path)
	} else {
		pl, err = p.makePlan(ctx, e)
	}

	if err != nil {
		return err
	}

	if approve == "" {
		ctx.Println(planSummary(pl))

		ok, err := ctx.Confirm("Apply this plan?")
		if err != nil {
			return err
		}

		if !ok {
			return output.Fail(output.ExitInvalidInput, "apply cancelled")
		}

		approve = pl.Hash
	}

	return p.runApply(ctx, e, mode, pl, approve, "apply")
}
func (p *DeployPlugin) runApply(ctx cli.CommandContext, e *engine.Engine, mode output.Mode, pl *plan.Plan, approve, command string) error {
	timeout, cancel := p.timeoutContext(ctx)
	defer cancel()

	events := make(chan provider.Event, 128)

	var journal []provider.Event

	var done sync.WaitGroup

	done.Go(func() {
		for event := range events {
			journal = append(journal, event)
			if !mode.JSON {
				ctx.Println(event.Op + " " + string(event.Status) + " " + event.Message)
			}
		}
	})

	err := e.Apply(timeout, pl, approve, ctx.Bool("allow-destructive"), events)
	close(events)
	done.Wait()

	if err != nil {
		return err
	}

	return output.Emit(ctx, mode, output.Envelope{Command: command, OK: true, Data: map[string]any{"hash": pl.Hash, "journal": journal}, Diagnostics: pl.Diagnostics})
}
func (p *DeployPlugin) up(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	timeout, cancel := p.timeoutContext(ctx)
	defer cancel()

	ds, err := e.Doctor(timeout, ctx.String("target"), ctx.String("env"), true)
	if err != nil {
		return err
	}

	if ds.HasErrors() {
		return output.Fail(output.ExitInvalidInput, "deployment preflight failed", ds...)
	}

	if mode.NonInteractive && !ctx.Bool("yes") && ctx.String("approve-plan") == "" {
		return output.Fail(output.ExitInvalidInput, "non-interactive up needs --yes or --approve-plan")
	}

	var pl *plan.Plan
	if path := ctx.String("plan"); path != "" {
		pl, err = plan.Load(path)
	} else {
		pl, err = p.makePlan(ctx, e)
	}

	if err != nil {
		return err
	}

	approve := ctx.String("approve-plan")
	if approve == "" {
		if !ctx.Bool("yes") {
			ctx.Println(planSummary(pl))

			ok, err := ctx.Confirm("Apply this plan?")
			if err != nil {
				return err
			}

			if !ok {
				return output.Fail(output.ExitInvalidInput, "up cancelled")
			}
		}

		approve = pl.Hash
	}

	return p.runApply(ctx, e, mode, pl, approve, "up")
}
func (p *DeployPlugin) status(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	status, err := e.Status(ctx.Context(), ctx.String("target"), ctx.String("env"))
	if err != nil {
		return output.Fail(output.ExitAccess, err.Error())
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "status", OK: true, Data: status})
}
func (p *DeployPlugin) logs(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	if ctx.NArgs() != 1 {
		return output.Fail(output.ExitInvalidInput, "usage: forge deploy logs <service>")
	}

	if mode.JSON && ctx.Bool("follow") {
		return output.Fail(output.ExitInvalidInput, "JSON logs need a finite --tail; use text output to follow")
	}

	reader, err := e.Logs(ctx.Context(), ctx.String("target"), ctx.String("env"), ctx.Arg(0), provider.LogOptions{Follow: ctx.Bool("follow"), Tail: ctx.Int("tail")})
	if err != nil {
		return err
	}
	defer reader.Close()

	if mode.JSON {
		raw, err := io.ReadAll(io.LimitReader(reader, 4*1024*1024))
		if err != nil {
			return err
		}

		return output.Emit(ctx, mode, output.Envelope{Command: "logs", OK: true, Data: map[string]string{"service": ctx.Arg(0), "logs": string(raw)}})
	}

	_, err = io.Copy(os.Stdout, reader)

	return err
}
func (p *DeployPlugin) rollback(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	if !ctx.Bool("yes") {
		if mode.NonInteractive {
			return output.Fail(output.ExitInvalidInput, "rollback needs --yes in non-interactive mode")
		}

		ok, err := ctx.Confirm("Restore the previous release?")
		if err != nil {
			return err
		}

		if !ok {
			return output.Fail(output.ExitInvalidInput, "rollback cancelled")
		}
	}

	if err := e.Rollback(ctx.Context(), ctx.String("target"), ctx.String("env"), ctx.String("release")); err != nil {
		return err
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "rollback", OK: true})
}
func (p *DeployPlugin) destroy(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	env := ctx.String("env")
	if env == "" {
		res, err := e.Inspect(ctx.Context(), ctx.String("target"), "")
		if err != nil {
			return err
		}

		env = res.Environment
		if env == "" {
			return output.Fail(output.ExitInvalidInput, "an environment is required")
		}
	}

	if !ctx.Bool("yes") {
		if mode.NonInteractive {
			return output.Fail(output.ExitInvalidInput, "destroy needs --yes in non-interactive mode")
		}

		prompt := "Type " + env + " to remove its recorded workloads"
		if ctx.Bool("delete-data") {
			prompt += " and stored data"
		}

		answer, err := ctx.Prompt(prompt + ":")
		if err != nil {
			return err
		}

		if answer != env {
			return output.Fail(output.ExitInvalidInput, "destroy cancelled")
		}
	}

	if err := e.Destroy(ctx.Context(), ctx.String("target"), env, ctx.Bool("delete-data")); err != nil {
		return err
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "destroy", OK: true, Data: map[string]bool{"deleted_data": ctx.Bool("delete-data")}})
}
func (p *DeployPlugin) providers(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "providers", OK: true, Data: e.Providers(ctx.Context())})
}
func (p *DeployPlugin) catalog(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	res, err := e.Inspect(ctx.Context(), "", "")
	if err != nil {
		return err
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "catalog", OK: true, Data: map[string]any{"descriptors": res.Catalog.Descriptors, "recipes": res.Catalog.Recipes}})
}
func planSummary(p *plan.Plan) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Plan %s: %s on %s (%s)\n", p.Hash, p.Environment, p.TargetName, p.Target.Provider)

	for _, op := range p.Operations {
		out.WriteString("  " + op.ID + "  " + op.Detail + "\n")
	}

	return out.String()
}
