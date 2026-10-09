package plugins

import (
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
)

func minimalConfig(t *testing.T) *config.ForgeConfig {
	t.Helper()

	cfg := config.DefaultConfigMinimal()
	cfg.Project.Name = "atlas"
	cfg.Project.Module = "example.com/atlas"
	cfg.RootDir = t.TempDir()
	cfg.Build.Apps = []config.BuildApp{{Name: "api", Cmd: "./cmd/api"}}

	return cfg
}

func TestManagedPlatformDeployHandsOff(t *testing.T) {
	for _, provider := range []string{"do", "render"} {
		out, err := runCLI(t, NewInfraPlugin(minimalConfig(t)), "infra", provider, "deploy", "--service", "api")
		if cli.GetExitCode(err) != 4 {
			t.Fatalf("%s: exit %d %q", provider, cli.GetExitCode(err), out)
		}

		if strings.Contains(out, "Deployed successfully") {
			t.Fatalf("%s: fixture success leaked: %q", provider, out)
		}

		if !strings.Contains(err.Error(), "forge infra "+provider+" export") {
			t.Fatalf("%s: handoff must name the export command: %v", provider, err)
		}
	}
}
func TestComposeDeployRunsDockerThroughRunner(t *testing.T) {
	cfg := minimalConfig(t)
	writeFile(t, cfg.RootDir, "cmd/api/main.go", "package main\nfunc main(){}\n")
	f := execx.NewFake(t)
	f.Available["docker"] = true
	f.Script("docker compose -f", execx.Result{})

	_, err := runCLI(t, NewInfraPluginWithRunner(cfg, f), "infra", "docker", "deploy", "--service", "api", "--build")
	if err != nil {
		t.Fatal(err)
	}

	lines := f.CallLines()
	if len(lines) != 2 || !strings.HasSuffix(lines[0], " build api") || !strings.Contains(lines[1], " up -d api") {
		t.Fatalf("%v", lines)
	}
}

func TestK8sDeployRejectsServiceFilter(t *testing.T) {
	cfg := minimalConfig(t)
	f := execx.NewFake(t)
	f.Available["kubectl"] = true

	_, err := runCLI(t, NewInfraPluginWithRunner(cfg, f), "infra", "k8s", "deploy", "--service", "api")
	if cli.GetExitCode(err) != 2 || !strings.Contains(err.Error(), "--service") {
		t.Fatalf("%v", err)
	}

	if len(f.Calls) != 0 {
		t.Fatalf("nothing must run: %v", f.CallLines())
	}
}

func TestK8sDeployValidatesWithKustomizeAndNeverFallsBack(t *testing.T) {
	cfg := minimalConfig(t)
	writeFile(t, cfg.RootDir, "cmd/api/main.go", "package main\nfunc main(){}\n")
	f := execx.NewFake(t)
	f.Available["kubectl"] = true
	f.Script("kubectl kustomize", execx.Result{ExitCode: 1, Stderr: "bad overlay"})

	_, err := runCLI(t, NewInfraPluginWithRunner(cfg, f), "infra", "k8s", "deploy", "--env", "dev")
	if err == nil || !strings.Contains(err.Error(), "bad overlay") {
		t.Fatalf("expected the kustomize error, got %v", err)
	}

	for _, line := range f.CallLines() {
		if strings.Contains(line, "apply -f") {
			t.Fatalf("raw apply fallback must not run: %v", f.CallLines())
		}
	}
}

func TestK8sDeployAppliesOverlayWhenKustomizeRenders(t *testing.T) {
	cfg := minimalConfig(t)
	writeFile(t, cfg.RootDir, "cmd/api/main.go", "package main\nfunc main(){}\n")
	f := execx.NewFake(t)
	f.Available["kubectl"] = true
	f.Script("kubectl kustomize", execx.Result{Stdout: "apiVersion: v1\n"})
	f.Script("kubectl apply -k", execx.Result{})

	_, err := runCLI(t, NewInfraPluginWithRunner(cfg, f), "infra", "k8s", "deploy", "--env", "dev", "--namespace", "x")
	if err != nil {
		t.Fatal(err)
	}

	lines := f.CallLines()
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "kubectl kustomize ") || !strings.Contains(lines[1], "apply -k ") || !strings.HasSuffix(lines[1], " -n x") {
		t.Fatalf("%v", lines)
	}
}
