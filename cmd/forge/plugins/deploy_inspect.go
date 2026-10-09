package plugins

import (
	"errors"
	"fmt"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/discover"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

func (p *DeployPlugin) newEngine(ctx cli.CommandContext) (*engine.Engine, output.Mode, error) {
	cfg := p.config

	if path := ctx.String("config"); path != "" {
		root, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return nil, output.Mode{}, err
		}

		cfg, err = config.LoadForgeConfigFrom(root)
		if err != nil {
			return nil, output.Mode{}, err
		}
	}

	if cfg == nil {
		return nil, output.Mode{}, output.Fail(output.ExitInvalidInput, "no .forge.yml found in this directory or any parent",
			output.Diagnostic{Code: "DEPLOY_CONFIG_INVALID", Severity: output.SeverityError, Message: "not a Forge project", Fix: "run forge init"})
	}

	mode := output.ModeFromContext(ctx)
	e, err := engine.New(engine.Options{Config: cfg, Mode: mode})

	return e, mode, err
}

type inspectData struct {
	Project     string `json:"project"`
	Target      string `json:"target"`
	Environment string `json:"environment"`
	Apps        []any  `json:"apps"`
	Services    []any  `json:"services"`
	Resources   []any  `json:"resources"`
	Connections []any  `json:"connections"`
	Suggestions []any  `json:"suggestions"`
}

func (p *DeployPlugin) inspect(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	res, err := e.Inspect(ctx.Context(), ctx.String("target"), ctx.String("env"))
	if err != nil {
		return output.Fail(output.ExitAccess, err.Error())
	}

	data := inspectData{Project: res.Project, Target: res.Target, Environment: res.Environment}
	for _, a := range res.Discovery.Apps {
		data.Apps = append(data.Apps, a)
	}

	for _, s := range res.Discovery.Suggestions {
		data.Suggestions = append(data.Suggestions, s)
	}

	if res.Deployment != nil {
		for _, s := range res.Deployment.Services {
			data.Services = append(data.Services, s)
		}

		for _, r := range res.Deployment.Resources {
			data.Resources = append(data.Resources, r)
		}

		for _, c := range res.Deployment.Connections {
			data.Connections = append(data.Connections, c)
		}
	}

	env := output.Envelope{Schema: output.SchemaVersion, Command: "inspect", OK: !res.Diagnostics.HasErrors(), Data: data, Diagnostics: res.Diagnostics}
	if err := output.Emit(ctx, mode, env); err != nil {
		return err
	}

	if !env.OK {
		code := output.ExitInvalidInput

		for _, d := range res.Diagnostics.Errors() {
			if d.Code == "DEPLOY_VERSION_MISSING" || d.Code == "DEPLOY_DECISION_OPEN" {
				code = output.ExitUnresolved
			}
		}

		return emittedDeployError(code, fmt.Sprintf("%d problems", len(res.Diagnostics.Errors())))
	}

	return nil
}

func emittedDeployError(code int, message string) error {
	err := output.Fail(code, message)

	var e *output.Error
	if errors.As(err, &e) {
		e.Emitted = true
	}

	return err
}

var deployRendererOnce sync.Once

func registerDeployRenderers() {
	deployRendererOnce.Do(func() {
		output.RegisterRenderer("inspect", func(ctx cli.CommandContext, _ output.Mode, value any) {
			data, ok := value.(inspectData)
			if !ok {
				return
			}

			ctx.Println(data.Project + " / " + data.Target + " / " + data.Environment)
			table := ctx.Table()
			table.SetHeader([]string{"Service", "Kind", "Replicas", "Bindings"})

			for _, v := range data.Services {
				s := v.(model.Service)
				table.AppendRow([]string{s.Name, string(s.Kind), strconv.Itoa(s.Replicas), strconv.Itoa(len(s.Bindings))})
			}

			table.Render()

			for _, v := range data.Suggestions {
				s := v.(discover.Suggestion)
				ctx.Println(s.Path + " (" + string(s.Confidence) + ", " + s.Source + ")")
			}
		})
	})
}
