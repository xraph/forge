package plugins

import (
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
)

func TestDeployOldSubcommandsAreGone(t *testing.T) {
	for _, sub := range []string{"docker", "k8s"} {
		_, err := runCLI(t, NewDeployPlugin(nil), "deploy", sub)
		if err == nil || cli.GetExitCode(err) == 0 {
			t.Fatalf("forge deploy %s must not exist", sub)
		}
	}
}

func TestDeployStatusRequiresProject(t *testing.T) {
	out, err := runCLI(t, NewDeployPlugin(nil), "deploy", "status")
	if cli.GetExitCode(err) != 2 || strings.Contains(out, "api-gateway") {
		t.Fatalf("exit %d out %q", cli.GetExitCode(err), out)
	}
}

func TestDeployHelpListsPlannedCommands(t *testing.T) {
	out, err := runCLI(t, NewDeployPlugin(nil), "deploy")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"init", "inspect", "doctor", "plan", "apply", "status"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help misses %s:\n%s", want, out)
		}
	}
}

func TestDeployGlobalFlagsParse(t *testing.T) {
	out, err := runCLI(t, NewDeployPlugin(nil), "deploy", "status", "--output", "json", "--non-interactive", "--env", "dev")
	if cli.GetExitCode(err) != 2 {
		t.Fatalf("flags must parse on every subcommand: %v %q", err, out)
	}
}
