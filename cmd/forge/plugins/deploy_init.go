package plugins

import (
	"errors"
	"sort"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

func (p *DeployPlugin) init(ctx cli.CommandContext) error {
	e, mode, err := p.newEngine(ctx)
	if err != nil {
		return err
	}

	answers := map[string]string{}

	lastKey := ""
	for _, a := range ctx.StringSlice("answer") {
		if k, v, ok := strings.Cut(a, "="); ok {
			answers[k] = v
			lastKey = k
		} else if lastKey != "" {
			answers[lastKey] += "," + a
		} else {
			return output.Fail(output.ExitInvalidInput, "answer must be path=value")
		}
	}

	res, files, err := e.InitWithOptions(ctx.Context(), answers, engine.InitOptions{Force: ctx.Bool("force")})

	var oe *output.Error
	if errors.As(err, &oe) && oe.Code == output.ExitUnresolved && !mode.NonInteractive && !mode.JSON {
		for _, d := range oe.Diagnostics {
			options := strings.Split(strings.TrimPrefix(d.Fix, "--answer "+d.Field+"="), "|")

			choice, perr := ctx.Select(d.Message, options)
			if perr != nil {
				return perr
			}

			answers[d.Field] = choice
		}

		res, files, err = e.InitWithOptions(ctx.Context(), answers, engine.InitOptions{Force: ctx.Bool("force")})
	}

	if err != nil {
		return err
	}

	if !ctx.Bool("yes") && (mode.NonInteractive || mode.JSON) {
		return output.Fail(output.ExitInvalidInput, "pass --yes to write configuration")
	}

	if !ctx.Bool("yes") && !mode.NonInteractive && !mode.JSON {
		for path, data := range files {
			ctx.Println(path)
			ctx.Println(string(data))
		}

		ok, perr := ctx.Confirm("Write these files?")
		if perr != nil || !ok {
			return output.Fail(output.ExitInvalidInput, "init cancelled")
		}
	}

	if err := e.SaveInit(ctx.Context(), res, files); err != nil {
		return err
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	return output.Emit(ctx, mode, output.Envelope{Schema: output.SchemaVersion, Command: "init", OK: true,
		Data: map[string]any{"written": paths, "decisions": len(answers)}, Diagnostics: res.Diagnostics})
}
