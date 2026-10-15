package plugins

import (
	"slices"
	"testing"
)

func TestObservabilityCommandTree(t *testing.T) {
	cmds := NewObservabilityPlugin(nil).Commands()
	if len(cmds) != 1 || cmds[0].Name() != "observability" || !slices.Contains(cmds[0].Aliases(), "obs") {
		t.Fatalf("commands = %v", cmds)
	}

	names := []string{}
	for _, sub := range cmds[0].Subcommands() {
		names = append(names, sub.Name())
	}

	for _, want := range []string{"up", "down", "status", "export"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing subcommand %q in %v", want, names)
		}
	}

	up, _ := cmds[0].FindSubcommand("up")

	flags := []string{}
	for _, f := range up.Flags() {
		flags = append(flags, f.Name())
	}

	for _, want := range []string{"target", "image", "grafana-port", "otlp-port", "prometheus-port"} {
		if !slices.Contains(flags, want) {
			t.Errorf("up lacks --%s (%v)", want, flags)
		}
	}
}

func TestParseSLOFlag(t *testing.T) {
	o, err := rulesOptions("99.5", "750ms")
	if err != nil || o.SLO != 99.5 || o.P95Threshold.Milliseconds() != 750 {
		t.Fatalf("got %+v, %v", o, err)
	}

	for _, bad := range [][2]string{{"abc", "1s"}, {"99.9", "soon"}} {
		if _, err := rulesOptions(bad[0], bad[1]); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
