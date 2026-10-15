package observability

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
)

func requireDocker(t *testing.T) {
	t.Helper()

	if os.Getenv("FORGE_OBS_DOCKER_TEST") != "1" {
		t.Skip("set FORGE_OBS_DOCKER_TEST=1 to run against " + Image)
	}

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
}

func TestPromtoolAcceptsGeneratedFiles(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	if _, err := WriteState(dir); err != nil {
		t.Fatal(err)
	}

	if err := WriteTarget(dir, Target{Name: "api", Address: "host.docker.internal:8080"}); err != nil {
		t.Fatal(err)
	}

	rules, err := RenderRules(DefaultRulesOptions())
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, RulesFile), rules, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"check", "config", "/forge/prometheus.yaml"}, {"check", "rules", "/forge/" + RulesFile}} {
		cmd := exec.CommandContext(t.Context(), "docker", append([]string{"run", "--rm", "--entrypoint", "/otel-lgtm/prometheus/promtool", "-v", dir + ":/forge:ro", Image}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("promtool %v: %v\n%s", args, err, out)
		}
	}
}

func TestStackRoundTrip(t *testing.T) {
	requireDocker(t)

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	s := &Stack{
		Runner: execx.System(), Dir: t.TempDir(), Name: "forge-lgtm-test-" + suffix, Volume: "forge-lgtm-test-" + suffix,
		Ports: Ports{Grafana: 23000, OTLP: 24318, Prometheus: 29090}, Out: os.Stderr, ReadyTimeout: 3 * time.Minute,
	}

	t.Cleanup(func() { _ = s.Down(context.Background(), true) })

	if err := WriteTarget(s.Dir, Target{Name: "nothing-here", Address: "host.docker.internal:1"}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Up(t.Context()); err != nil {
		t.Fatal(err)
	}

	st, err := s.Inspect(t.Context())
	if err != nil || !st.Running || st.Ports != s.Ports {
		t.Fatalf("inspect %+v, %v", st, err)
	}

	// file_sd refreshes every 5s.
	var targets []TargetHealth

	for range 30 {
		targets, err = Targets(t.Context(), http.DefaultClient, s.Ports.PrometheusURL())
		if err == nil && len(targets) == 1 {
			break
		}

		time.Sleep(time.Second)
	}

	if len(targets) != 1 || targets[0].Job != "nothing-here" {
		t.Fatalf("targets %+v, %v", targets, err)
	}

	resp, err := http.Get(s.Ports.GrafanaURL() + "/api/dashboards/uid/forge-service") //nolint:noctx // test
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard not provisioned: %v %v", resp, err)
	}

	_ = resp.Body.Close()

	if _, err := s.Up(t.Context()); err != nil {
		t.Fatalf("second Up should reuse the container: %v", err)
	}
}
