package plugins

import (
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
)

func TestDeployStartValidatesLoopbackOptions(t *testing.T) {
	for _, args := range [][]string{{"--port", "-1"}, {"--port", "65536"}, {"--token", "short"}, {"--store-ref", "env:DSN"}} {
		out, err := runCLI(t, NewDeployPlugin(nil), append([]string{"deploy", "start", "--no-open", "--non-interactive"}, args...)...)
		if cli.GetExitCode(err) != 2 || (strings.Contains(err.Error(), "not available yet") || strings.Contains(err.Error(), "unknown flag")) {
			t.Fatalf("start flags are not implemented: %v %s", err, out)
		}

		expected := "port must"

		if args[0] == "--token" {
			expected = "token must"
		}

		if args[0] == "--store-ref" {
			expected = "store-ref requires"
		}

		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("validation did not run: %v", err)
		}
	}

	out, err := runCLI(t, NewDeployPlugin(nil), "deploy", "--non-interactive")
	if err != nil || !strings.Contains(out, "start") {
		t.Fatalf("noninteractive help: %v %s", err, out)
	}
}
