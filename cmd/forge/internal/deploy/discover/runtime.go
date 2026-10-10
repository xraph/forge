package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
)

const runtimeLimit = 1 << 20

var runtimeName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
var runtimeConfigPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\[\]-]{0,255}$`)

type RuntimeRequirement struct {
	Extension string `json:"extension"`
	Kind      string `json:"kind"`
	Instance  string `json:"instance,omitempty"`
	ConfigKey string `json:"config_key"`
	Optional  bool   `json:"optional,omitempty"`
}
type RuntimeReport struct {
	Service      string               `json:"service,omitempty"`
	Schema       string               `json:"schema"`
	App          string               `json:"app"`
	Requirements []RuntimeRequirement `json:"requirements"`
}

func decodeRuntime(raw []byte) (RuntimeReport, error) {
	var report RuntimeReport

	invalid := errors.New("invalid infrastructure report; update the app's Forge dependency and emit forge.infra/v1 metadata only")
	if len(raw) > runtimeLimit {
		return report, invalid
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&report); err != nil {
		return RuntimeReport{}, invalid
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return RuntimeReport{}, invalid
	}

	if report.Schema != "forge.infra/v1" || report.App == "" || len(report.App) > 256 || len(report.Requirements) > 4096 || report.Service != "" {
		return RuntimeReport{}, invalid
	}

	for _, r := range report.Requirements {
		if !runtimeName.MatchString(r.Extension) || !runtimeName.MatchString(r.Kind) || r.Instance != "" && !runtimeName.MatchString(r.Instance) || !runtimeConfigPath.MatchString(r.ConfigKey) {
			return RuntimeReport{}, invalid
		}
	}

	return report, nil
}

type boundedReport struct {
	bytes.Buffer

	oversized bool
}

func (w *boundedReport) Write(p []byte) (int, error) {
	total := len(p)

	left := runtimeLimit - w.Len()
	if left < total {
		w.oversized = true
		p = p[:max(0, left)]
	}

	_, _ = w.Buffer.Write(p)

	return total, nil
}

// ExecuteRuntime executes only explicitly selected, trusted project apps. Ordinary
// discovery, planning and the workbench never call this method.
func ExecuteRuntime(ctx context.Context, root string, apps []App, selected string, runner execx.Runner) ([]RuntimeReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if runner == nil {
		runner = execx.System()
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}

	chosen := []App{}

	for _, app := range apps {
		if selected == "" || app.Name == selected {
			chosen = append(chosen, app)
		}
	}

	if len(chosen) == 0 {
		return nil, errors.New("select a discovered app for runtime inspection")
	}

	for _, app := range chosen {
		realDir, err := filepath.EvalSymlinks(app.Dir)
		if err != nil {
			return nil, errors.New("runtime inspection app directory is unavailable")
		}

		relative, err := filepath.Rel(realRoot, realDir)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, errors.New("runtime inspection app escapes project root")
		}
	}

	if _, err := runner.LookPath("go"); err != nil {
		return nil, errors.New("runtime inspection requires Go")
	}

	temporary, err := os.MkdirTemp("", "forge-introspect-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temporary)

	reports := []RuntimeReport{}

	for i, app := range chosen {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		module := app.Dir
		for {
			if _, err := os.Stat(filepath.Join(module, "go.mod")); err == nil {
				break
			}

			if module == root || module == filepath.Dir(module) {
				return nil, errors.New("runtime inspection requires an app module")
			}

			module = filepath.Dir(module)
		}

		rel, err := filepath.Rel(module, app.Dir)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, errors.New("invalid runtime app package")
		}

		binary := filepath.Join(temporary, "app-"+strconv.Itoa(i))
		if _, err := runner.Run(ctx, execx.Command{Name: "go", Args: []string{"build", "-o", binary, "./" + filepath.ToSlash(rel)}, Dir: module, Env: []string{"GOFLAGS=-mod=readonly"}, Stdout: io.Discard, Stderr: io.Discard}); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			return nil, errors.New("runtime inspection build failed; verify the selected app builds without changing its module")
		}

		var out boundedReport
		if _, err := runner.Run(ctx, execx.Command{Name: binary, Dir: app.Dir, Env: []string{"FORGE_INTROSPECT=1"}, Stdout: &out, Stderr: io.Discard}); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			return nil, errors.New("runtime inspection failed before a valid infrastructure report")
		}

		if out.oversized {
			return nil, errors.New("runtime infrastructure report exceeds 1 MiB")
		}

		report, err := decodeRuntime(out.Bytes())
		if err != nil {
			return nil, err
		}

		report.Service = app.Name
		reports = append(reports, report)
	}

	return reports, nil
}
