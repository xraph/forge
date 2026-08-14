package plugins

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/internal/client"
)

// expandClients turns one resolved plan into the list of clients to generate.
//
// A specification is not always one consumer's worth of API. A gateway that
// fronts several services publishes their routes as one document, and the
// client each frontend wants is one service's surface under its own package
// name -- not the union under a name that belongs to none of them. The clients:
// block is how that is declared, and this is where a plan carrying it becomes
// the several plans it describes.
//
// With no clients: block the result is the base plan alone, which is what makes
// this safe to call unconditionally from generate, check and watch: a config
// that never mentions clients: takes exactly the path it always did.
func expandClients(base *generationPlan) ([]*generationPlan, error) {
	if len(base.clients) == 0 {
		return []*generationPlan{base}, nil
	}

	// A pinned --output cannot be honoured for more than one client, and the
	// failure mode if it were ignored is quiet and destructive: every client
	// generated into the same directory, each overwriting the last, leaving a
	// tree that looks like a successful generation of whichever ran final.
	if base.pinnedOutput {
		return nil, cli.NewError(
			"--output cannot be combined with a clients: block, which names an output per client; "+
				"drop the flag to generate them all, or select one with --client",
			cli.ExitUsageError,
		)
	}

	plans := make([]*generationPlan, 0, len(base.clients))
	seenName := make(map[string]struct{}, len(base.clients))
	seenOutput := make(map[string]string, len(base.clients))

	for i, entry := range base.clients {
		name := entry.Name
		if name == "" {
			name = fmt.Sprintf("client %d", i+1)
		}

		if _, dup := seenName[name]; dup {
			return nil, cli.NewError(
				fmt.Sprintf("clients: declares %q twice; names identify a client on the command line and in output, so they must be unique", name),
				cli.ExitUsageError,
			)
		}

		seenName[name] = struct{}{}

		if entry.Output == "" {
			return nil, cli.NewError(
				fmt.Sprintf("client %q declares no output; every client in a clients: block needs its own directory", name),
				cli.ExitUsageError,
			)
		}

		// Two clients sharing an output directory is the same silent overwrite
		// the pinned --output check above refuses, just spelled in config.
		// Compared cleaned so ./a and ./a/ are not treated as different.
		out := filepath.Clean(entry.Output)
		if other, dup := seenOutput[out]; dup {
			return nil, cli.NewError(
				fmt.Sprintf("clients %q and %q both generate into %s; each client needs its own directory", other, name, entry.Output),
				cli.ExitUsageError,
			)
		}

		seenOutput[out] = name

		plans = append(plans, base.derive(name, entry))
	}

	return plans, nil
}

// derive builds the plan for one entry of a clients: block, starting from the
// base plan's fully resolved configuration.
//
// Starting from the base rather than from zero is what keeps a clients: block
// short: everything a client does not mention -- field naming, streaming
// features, the whole feature-flag set, and the sources themselves -- it
// inherits, so a per-service split is a name, an output and a filter.
func (p *generationPlan) derive(name string, entry ClientGenConfig) *generationPlan {
	cfg := p.config

	cfg.OutputDir = entry.Output

	if entry.Language != "" {
		cfg.Language = entry.Language
	}

	if entry.Package != "" {
		cfg.PackageName = entry.Package
	}

	if entry.BaseURL != "" {
		cfg.BaseURL = entry.BaseURL
	}

	if entry.Module != "" {
		cfg.Module = entry.Module
	}

	if entry.Hooks != nil {
		cfg.Hooks = *entry.Hooks
	}

	if entry.StripPrefix != "" {
		cfg.StripPrefix = entry.StripPrefix
	}

	if entry.Auth != nil {
		cfg.IncludeAuth = *entry.Auth
	}

	if entry.Streaming != nil {
		cfg.IncludeStreaming = *entry.Streaming
	}

	// Replace rather than append: see ClientGenConfig.Include. A client that
	// names neither keeps the base filter, which is the inherited default.
	if len(entry.Include) > 0 || len(entry.Exclude) > 0 {
		cfg.PathFilter = client.PathFilter{
			Include: entry.Include,
			Exclude: entry.Exclude,
		}
	}

	derived := *p
	derived.name = name
	derived.config = cfg
	derived.outputDir = entry.Output

	// cleanup releases the temp files holding downloaded specs, which every
	// derived plan shares with the base. Releasing them per client would pull
	// the sources out from under the clients still to be generated, so the
	// base's own deferred cleanup remains the only one.
	derived.cleanup = func() {}

	return &derived
}

// selectClients narrows an expanded plan list to the names given, which is what
// --client takes. An empty selection means all of them.
//
// This exists for the regenerate-one-service case: a spec change that touched
// only /studio should not have to rewrite, and put through review, the other
// clients' output as well.
func selectClients(plans []*generationPlan, names []string) ([]*generationPlan, error) {
	if len(names) == 0 {
		return plans, nil
	}

	byName := make(map[string]*generationPlan, len(plans))

	available := make([]string, 0, len(plans))

	for _, plan := range plans {
		byName[plan.name] = plan

		available = append(available, plan.name)
	}

	selected := make([]*generationPlan, 0, len(names))

	for _, name := range names {
		plan, ok := byName[name]
		if !ok {
			return nil, cli.NewError(
				fmt.Sprintf("no client named %q; this config declares: %s", name, strings.Join(available, ", ")),
				cli.ExitUsageError,
			)
		}

		selected = append(selected, plan)
	}

	return selected, nil
}
