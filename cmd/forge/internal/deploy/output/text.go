package output

import (
	"github.com/xraph/forge/cli"
	"strconv"
)

// renderers draw Envelope.Data for a command in text mode. Commands register
// theirs in init() of the file that owns the data type.
var renderers = map[string]func(ctx cli.CommandContext, mode Mode, data any){}

func RegisterRenderer(command string, fn func(ctx cli.CommandContext, mode Mode, data any)) {
	renderers[command] = fn
}

func renderText(ctx cli.CommandContext, mode Mode, env Envelope) {
	if fn, ok := renderers[env.Command]; ok && env.Data != nil {
		fn(ctx, mode, env.Data)
	}

	if len(env.Diagnostics) == 0 {
		if env.OK {
			ctx.Success("OK")
		}

		return
	}

	table := ctx.Table()
	table.SetHeader([]string{"Severity", "Code", "Where", "Message", "Fix"})

	for _, d := range env.Diagnostics.Sorted() {
		where := d.File
		if d.Line > 0 {
			where += ":" + itoa(d.Line)
		}

		if d.Field != "" {
			if where != "" {
				where += " "
			}

			where += d.Field
		}

		sev := string(d.Severity)
		if !mode.NoColor {
			switch d.Severity {
			case SeverityError:
				sev = cli.Red(sev)
			case SeverityWarning:
				sev = cli.Yellow(sev)
			}
		}

		table.AppendRow([]string{sev, d.Code, where, d.Message, d.Fix})
	}

	table.Render()
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
