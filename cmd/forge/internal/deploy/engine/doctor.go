package engine

import (
	"context"
	"fmt"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

var toolsByProvider = map[string][]string{
	"compose":      {"docker"},
	"kubernetes":   {"kubectl"},
	"render":       {"render"},
	"digitalocean": {"doctl"},
}

// Doctor runs preflight. Offline checks: config, decisions, tools on PATH,
// secret resolution through resolvers that need no network. online adds
// provider checks in plan 03 and later.
func (e *Engine) Doctor(ctx context.Context, target, env string, online bool) (output.Diagnostics, error) {
	res, err := e.inspect(ctx, target, env, online)
	if err != nil {
		return nil, err
	}

	diags := res.Diagnostics

	provider := ""
	if res.Doc.Deploy != nil {
		provider = res.Doc.Deploy.Targets[res.Target].Provider
	}

	for _, tool := range toolsByProvider[provider] {
		if _, err := e.runner.LookPath(tool); err != nil {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_TOOL_MISSING", Severity: output.SeverityError,
				Message: fmt.Sprintf("%s is not on PATH; target %q needs it", tool, res.Target), Fix: "install " + tool})
		}
	}

	if res.Deployment != nil {
		if adapter, ok := e.registry.Get(provider); ok {
			diags = append(diags, adapter.Validate(ctx, res.Deployment)...)
		}
	}

	if online && !diags.HasErrors() && provider == "compose" {
		args := []string{"info"}
		if t := res.Doc.Deploy.Targets[res.Target]; t.DockerContext != "" {
			args = append([]string{"--context", t.DockerContext}, args...)
		}

		if _, err := e.runner.Run(ctx, execx.Command{Name: "docker", Args: args, Dir: e.cfg.RootDir}); err != nil {
			diags = append(diags, output.Diagnostic{Code: "DEPLOY_CONTEXT_UNREACHABLE", Severity: output.SeverityError, Message: "Docker daemon is unavailable for this target", Fix: "start Docker or select an available Docker context"})
		}
	}

	if !diags.HasErrors() {
		diags = append(diags, output.Diagnostic{Code: "DEPLOY_CONFIG_VALIDATED", Severity: output.SeverityInfo, Message: "Local deployment configuration checks passed"})
	}

	return diags.Sorted(), nil
}
