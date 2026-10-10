package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
)

var infraName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
var infraConfigPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\[\]-]{0,255}$`)

// WriteInfraRequirements registers a native Forge app's extensions, writes their
// metadata and returns without starting the application or migration hooks.
// Constructors and Register still execute your code. Use this only on a trusted app.
func WriteInfraRequirements(a App, w io.Writer) error {
	native, ok := a.(*app)
	if !ok || w == nil {
		return errors.New("infrastructure reporting requires a native Forge app and writer")
	}

	native.mu.RLock()
	started := native.started || native.starting
	native.mu.RUnlock()

	if started {
		return errors.New("infrastructure reporting requires an unstarted app")
	}

	_, extensions, order, err := native.buildExtensionGraph()
	if err != nil {
		return err
	}

	for _, name := range order {
		if ext := extensions[name]; ext != nil {
			if err := ext.Register(a); err != nil {
				return fmt.Errorf("infrastructure registration failed for %s", name)
			}
		}
	}

	report := InfraReport{Schema: InfraSchemaVersion, App: a.Name(), Requirements: []ExtensionInfraRequirement{}}
	for _, ext := range a.Extensions() {
		reporter, ok := ext.(InfraRequirer)
		if !ok {
			continue
		}

		for _, req := range reporter.InfraRequirements() {
			if !infraName.MatchString(ext.Name()) || !infraName.MatchString(req.Kind) || req.Instance != "" && !infraName.MatchString(req.Instance) || !infraConfigPath.MatchString(req.ConfigKey) {
				return errors.New("infrastructure requirement contains invalid metadata")
			}

			report.Requirements = append(report.Requirements, ExtensionInfraRequirement{Extension: ext.Name(), InfraRequirement: req})
			if len(report.Requirements) > 4096 {
				return errors.New("too many infrastructure requirements")
			}
		}
	}

	sort.SliceStable(report.Requirements, func(i, j int) bool {
		a, b := report.Requirements[i], report.Requirements[j]
		if a.Extension != b.Extension {
			return a.Extension < b.Extension
		}

		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}

		if a.Instance != b.Instance {
			return a.Instance < b.Instance
		}

		if a.ConfigKey != b.ConfigKey {
			return a.ConfigKey < b.ConfigKey
		}

		return !a.Optional && b.Optional
	})

	if report.App == "" || len(report.App) > 256 {
		return errors.New("invalid infrastructure report app name")
	}

	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}

	if len(raw) > 1<<20 {
		return errors.New("infrastructure report exceeds 1 MiB")
	}

	_, err = w.Write(append(raw, '\n'))

	return err
}
