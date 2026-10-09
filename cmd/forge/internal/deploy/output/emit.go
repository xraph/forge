package output

import (
	"encoding/json"
	"os"

	"github.com/xraph/forge/cli"
)

type Mode struct {
	JSON           bool
	NonInteractive bool
	NoColor        bool
}

func ModeFromContext(ctx cli.CommandContext) Mode {
	return Mode{
		JSON:           ctx.String("output") == "json",
		NonInteractive: ctx.Bool("non-interactive"),
		NoColor:        ctx.Bool("no-color"),
	}
}

// Emit writes the envelope. JSON mode prints one document through
// ctx.Println so tests can capture it through cli.SetOutput.
func Emit(ctx cli.CommandContext, mode Mode, env Envelope) error {
	if env.Diagnostics == nil {
		env.Diagnostics = Diagnostics{}
	}

	if env.Schema == "" {
		env.Schema = SchemaVersion
	}

	if mode.JSON {
		data, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return err
		}

		ctx.Println(string(data))

		return nil
	}

	renderText(ctx, mode, env)

	return nil
}

// Progress goes to stderr in JSON mode and through ctx.Info otherwise.
func Progress(ctx cli.CommandContext, mode Mode, msg string) {
	if mode.JSON {
		_, _ = os.Stderr.WriteString(msg + "\n")

		return
	}

	ctx.Info(msg)
}
