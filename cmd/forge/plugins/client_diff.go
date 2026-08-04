// v2/cmd/forge/plugins/client_diff.go
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/internal/client"
)

// diffUsage is rendered under USAGE: by `forge client diff --help`.
//
// The exit codes are the contract a CI job gates on, so they are documented
// where the person wiring up that job is already looking.
const diffUsage = `client diff <old-spec> <new-spec> [--format text|json]

Classifies every difference between two API specifications into three buckets:
compatible, breaking (API), and breaking (cache). The third has no equivalent in
other OpenAPI diff tools: renaming an entity or changing its id field breaks no
HTTP contract at all -- every request and response stays byte-identical -- while
repartitioning the generated client's normalized cache, so a persisted store
still holding the old keys becomes unreachable.

Changes this differ cannot prove are a widening or a narrowing are reported as
UNKNOWN rather than guessed at.

EXIT CODES:
  0  no changes, or compatible changes only
  1  breaking changes present (API contract or cache identity)
  2  usage error: wrong arguments, a spec that could not be read or parsed,
     or a report that could not be rendered
  3  no breaking changes, but changes this differ declined to classify
     (UNKNOWN) -- a human has to look`

func (p *ClientPlugin) diffSpecs(ctx cli.CommandContext) error {
	if ctx.NArgs() != 2 {
		return cli.NewError("usage: forge client diff <old-spec> <new-spec>", cli.ExitUsageError)
	}

	oldPath, newPath := ctx.Arg(0), ctx.Arg(1)

	format := strings.ToLower(strings.TrimSpace(ctx.String("format")))
	if format == "" {
		format = "text"
	}

	if format != "text" && format != "json" {
		return cli.NewError(fmt.Sprintf("invalid --format %q: must be text or json", ctx.String("format")), cli.ExitUsageError)
	}

	parseCtx := ctx.Context()
	if parseCtx == nil {
		parseCtx = context.Background()
	}

	parser := client.NewSpecParser()

	oldSpec, err := parser.ParseFile(parseCtx, oldPath)
	if err != nil {
		return cli.WrapError(err, "parse "+oldPath, cli.ExitUsageError)
	}

	newSpec, err := parser.ParseFile(parseCtx, newPath)
	if err != nil {
		return cli.WrapError(err, "parse "+newPath, cli.ExitUsageError)
	}

	report := client.DiffSpecs(oldSpec, newSpec)

	if format == "json" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			// Exit 2, not 3: 3 means "no breaking changes, but something went
			// unclassified", which a CI job may well choose to let through.
			// A report that could not be rendered has told the caller nothing
			// at all, and must not be mistaken for that.
			return cli.WrapError(err, "encode diff report", cli.ExitUsageError)
		}

		ctx.Println(string(encoded))
	} else {
		renderDiffText(ctx, oldPath, newPath, report)
	}

	// Exit code, in the order a CI job cares about: a break outranks an
	// unclassifiable change, which outranks everything being fine.
	switch {
	case report.HasBreaking():
		return cli.NewError(
			fmt.Sprintf("%d breaking change(s): %d API, %d cache",
				report.Summary.BreakingAPI+report.Summary.BreakingCache,
				report.Summary.BreakingAPI, report.Summary.BreakingCache),
			cli.ExitError,
		)

	case report.HasUnknown():
		return cli.NewError(
			fmt.Sprintf("%d change(s) could not be classified; review them before releasing", report.Summary.Unknown),
			cli.ExitInternalError,
		)

	default:
		return nil
	}
}

// diffKindOrder fixes the order sections appear in. Breaking first: the reader
// is scanning for a reason not to ship.
var diffKindOrder = []client.ChangeKind{
	client.ChangeBreakingAPI,
	client.ChangeBreakingCache,
	client.ChangeUnknown,
	client.ChangeCompatible,
}

// renderDiffText prints the report for a human. Readable first, parseable
// second: the sections are fixed, the rows are sorted, and two runs over the
// same specs produce byte-identical output so this can be pasted into a pull
// request and diffed against the previous run.
func renderDiffText(ctx cli.CommandContext, oldPath, newPath string, report client.DiffReport) {
	ctx.Println("")
	ctx.Println(cli.Bold(fmt.Sprintf("%s -> %s", oldPath, newPath)))

	if len(report.Changes) == 0 {
		ctx.Println("")
		ctx.Println("No changes.")

		return
	}

	byKind := make(map[client.ChangeKind][]client.Change, len(diffKindOrder))
	for _, change := range report.Changes {
		byKind[change.Kind] = append(byKind[change.Kind], change)
	}

	for _, kind := range diffKindOrder {
		changes := byKind[kind]
		if len(changes) == 0 {
			continue
		}

		width := 0

		for _, change := range changes {
			if len(change.Subject) > width {
				width = len(change.Subject)
			}
		}

		if width > 44 {
			width = 44
		}

		ctx.Println("")
		ctx.Println(cli.Bold(fmt.Sprintf("%s (%d)", kind, len(changes))))

		for _, change := range changes {
			ctx.Println(fmt.Sprintf("  %-*s  %s", width, change.Subject, change.Detail))
		}
	}

	ctx.Println("")
	ctx.Println(fmt.Sprintf("Summary: %d compatible, %d breaking (API), %d breaking (cache), %d unknown",
		report.Summary.Compatible, report.Summary.BreakingAPI, report.Summary.BreakingCache, report.Summary.Unknown))
}
