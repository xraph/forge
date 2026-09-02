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

	cfg.StripPrefixes = clientStripPrefixes(entry, p.clients, p.config.StripPrefixes)

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

// clientStripPrefixes returns every service prefix this client should strip.
//
// A client strips more than its own prefix because a service describes more
// than its own types. The auth service that fronts the others re-describes what
// it fronts, so identity's document declares `Portal_WorkspaceResponse` for the
// same record portal's own client calls `WorkspaceResponse`. Strip only
// identity's prefix and those stay two names for one record: a consumer that
// unions the generated entity tables -- which is the point of the tables, and
// what makes normalization worth anything on a screen touching two services --
// gets two cache entries where it asked for one, and neither invalidates the
// other.
//
// The set is derived rather than declared because a clients: block already
// states it. Every entry names the prefix its own service was merged under, so
// the union of those is exactly the set of prefixes the gateway is using, and
// asking each client to restate its siblings' prefixes would be a list that
// drifts the first time a service is added.
//
// An explicit strip_prefixes on a client REPLACES the derived siblings rather
// than adding to them. Additive would make the knob useless in the case it
// exists for: it exists to escape a collision between two services whose types
// strip to the same name, and adding can only ever widen the set that produced
// the collision. The cost is that a client naming one unowned prefix has to
// name the siblings it still wants, which is the rarer edit and a visible one.
func clientStripPrefixes(entry ClientGenConfig, siblings []ClientGenConfig, extra []string) []string {
	// The client's own prefix is always in the set, including when an explicit
	// list is given. Losing it is the one mistake this cannot be asked to make:
	// a client that stops stripping its own prefix regenerates every typename
	// in the package with the stutter back in, and nothing fails to say so.
	prefixes := []string{entry.StripPrefix}

	if len(entry.StripPrefixes) > 0 {
		prefixes = append(prefixes, entry.StripPrefixes...)
	} else {
		for _, sibling := range siblings {
			prefixes = append(prefixes, sibling.StripPrefix)
		}
	}

	// defaults.strip_prefixes is for the service the gateway fronts that no
	// client is generated for, so it applies whichever branch ran above.
	return append(prefixes, extra...)
}
