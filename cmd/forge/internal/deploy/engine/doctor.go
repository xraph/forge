package engine

import (
	"context"
	"fmt"

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

	if online {
		diags = append(diags, output.Diagnostic{Code: "DEPLOY_UNSUPPORTED_COMMAND", Severity: output.SeverityInfo,
			Message: "online checks arrive with the first provider; run with --offline for now"})
	}

	return diags.Sorted(), nil
}
