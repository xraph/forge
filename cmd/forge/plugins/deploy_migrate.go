package plugins

import (
	"fmt"
	"github.com/xraph/forge/cmd/forge/config"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

func (p *DeployPlugin) migrate(ctx cli.CommandContext) error {
	mode := output.ModeFromContext(ctx)

	path := ctx.String("config")
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		path = absolute
	} else {
		cfg := p.config
		if cfg == nil {
			var err error

			cfg, _, err = config.LoadForgeConfig()
			if err != nil {
				return output.Fail(output.ExitInvalidInput, err.Error())
			}
		}

		var diags output.Diagnostics

		var err error

		path, diags, err = spec.Locate(cfg.RootDir)
		if err != nil {
			return output.Fail(output.ExitInvalidInput, err.Error(), diags...)
		}
	}

	doc, pd, err := spec.Parse(path)
	if err != nil {
		return err
	}

	if pd.HasErrors() {
		return output.Fail(output.ExitInvalidInput, "config does not parse", pd...)
	}

	if !doc.IsV1 {
		if doc.Deploy != nil {
			return output.Fail(output.ExitInvalidInput, "deploy section is already version 2")
		}

		return output.Fail(output.ExitInvalidInput, "no deploy section to migrate; run forge deploy init")
	}

	var legacy config.DeployConfig
	if err := doc.Root.Content[0].Decode(&struct {
		Deploy *config.DeployConfig `yaml:"deploy"`
	}{Deploy: &legacy}); err != nil {
		return output.Fail(output.ExitInvalidInput, err.Error())
	}

	ops, md := spec.MigrateV1(&legacy)

	files, err := doc.Patch(ops)
	if err != nil {
		return err
	}

	diff := deployMigrationDiff(path, string(doc.RawBytes()), string(files[path]))

	if ctx.Bool("dry-run") {
		if !mode.JSON {
			ctx.Println(diff)
		}

		return output.Emit(ctx, mode, output.Envelope{Command: "migrate", OK: true, Data: map[string]any{"diff": diff, "written": false}, Diagnostics: md})
	}

	if !ctx.Bool("yes") {
		if mode.NonInteractive || mode.JSON {
			return output.Fail(output.ExitInvalidInput, "pass --yes to write in non-interactive mode")
		}

		if !mode.JSON {
			ctx.Println(diff)
		}

		ok, err := ctx.Confirm("Write " + path + "?")
		if err != nil || !ok {
			return output.Fail(output.ExitInvalidInput, "migrate cancelled")
		}
	}

	backup, err := os.OpenFile(path+".v1.bak", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return output.Fail(output.ExitConflict, fmt.Sprintf("backup already exists or cannot be created: %v", err))
	}

	if _, err := backup.Write(doc.RawBytes()); err != nil {
		_ = backup.Close()

		return err
	}

	if err := backup.Sync(); err != nil {
		_ = backup.Close()

		return err
	}

	if err := backup.Close(); err != nil {
		return err
	}

	if err := spec.Write(path, doc.Hash, files[path]); err != nil {
		return output.Fail(output.ExitConflict, err.Error())
	}

	return output.Emit(ctx, mode, output.Envelope{Command: "migrate", OK: true, Data: map[string]any{"written": true, "backup": path + ".v1.bak"}, Diagnostics: md})
}

// deployMigrationDiff is a small line diff: common prefix and suffix are context,
// the middle is shown as removed and added lines. It is enough for a
// review prompt; the workbench renders a real diff in plan 05.
func deployMigrationDiff(path, before, after string) string {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")

	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}

	j := 0
	for j < len(a)-i && j < len(b)-i && a[len(a)-1-j] == b[len(b)-1-j] {
		j++
	}

	var sb strings.Builder
	sb.WriteString("--- " + path + "\n+++ " + path + "\n")

	for _, l := range a[i : len(a)-j] {
		sb.WriteString("-" + l + "\n")
	}

	for _, l := range b[i : len(b)-j] {
		sb.WriteString("+" + l + "\n")
	}

	return sb.String()
}
