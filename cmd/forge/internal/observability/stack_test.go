package observability

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
)

const inspectRunning = `[{"State":{"Running":true},"Config":{"Labels":{"forge.config-hash":"%s","forge.ports":"3000,4318,9090"}}}]`

func newStack(t *testing.T, f *execx.Fake) *Stack {
	t.Helper()

	return &Stack{
		Runner: f, Dir: t.TempDir(), Ports: DefaultPorts(), Out: &strings.Builder{},
		ReadyTimeout: 50 * time.Millisecond, PollInterval: 5 * time.Millisecond,
		PortFree: func(context.Context, int) bool { return true },
	}
}

func noSuch() execx.Result {
	return execx.Result{ExitCode: 1, Stderr: "Error response from daemon: No such container: forge-lgtm"}
}

func TestUpCreatesContainer(t *testing.T) {
	f := execx.NewFake(t)
	f.Script("docker container inspect forge-lgtm", noSuch())
	f.Script("docker image inspect "+Image, execx.Result{})
	f.Script("docker run", execx.Result{Stdout: "abc"})
	f.Script("docker exec forge-lgtm test -f /tmp/ready", execx.Result{})

	s := newStack(t, f)

	ports, err := s.Up(context.Background())
	if err != nil || ports != DefaultPorts() {
		t.Fatalf("Up = %+v, %v", ports, err)
	}

	var run []string

	for _, c := range f.Calls {
		if len(c.Args) > 0 && c.Args[0] == "run" {
			run = c.Args
		}
	}

	line := strings.Join(run, " ")
	for _, want := range []string{
		"--name forge-lgtm",
		"--add-host=host.docker.internal:host-gateway",
		"-p 127.0.0.1:3000:3000", "-p 127.0.0.1:4318:4318", "-p 127.0.0.1:9090:9090",
		"-v forge-lgtm-data:/data",
		":/otel-lgtm/prometheus.yaml:ro",
		":/otel-lgtm/grafana/conf/provisioning/datasources/grafana-datasources.yaml:ro",
		":/otel-lgtm/grafana/conf/provisioning/dashboards/forge.yaml:ro",
		":/forge/dashboards:ro", ":/forge/targets:ro",
		"GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH=/forge/dashboards/forge-service.json",
		"--label forge.ports=3000,4318,9090",
		Image,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("run args lack %q:\n%s", want, line)
		}
	}

	if strings.Contains(line, "4317") {
		t.Error("4317 must not be published")
	}
}

func TestUpReusesMatchingContainer(t *testing.T) {
	f := execx.NewFake(t)
	s := newStack(t, f)

	files, err := WriteState(s.Dir)
	if err != nil {
		t.Fatal(err)
	}

	f.Script("docker container inspect forge-lgtm", execx.Result{Stdout: strings.Replace(inspectRunning, "%s", configHash(files, Image, DefaultPorts()), 1)})
	f.Script("docker exec forge-lgtm test -f /tmp/ready", execx.Result{})

	if _, err := s.Up(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, line := range f.CallLines() {
		if strings.HasPrefix(line, "docker run") || strings.HasPrefix(line, "docker rm") {
			t.Fatalf("matching container was touched: %s", line)
		}
	}
}

func TestUpRecreatesChangedContainerBeforePortCheck(t *testing.T) {
	f := execx.NewFake(t)
	f.Script("docker container inspect forge-lgtm", execx.Result{Stdout: `[{"State":{"Running":true},"Config":{"Labels":{}}}]`})
	f.Script("docker rm -f forge-lgtm", execx.Result{})
	f.Script("docker image inspect "+Image, execx.Result{})
	f.Script("docker run", execx.Result{})
	f.Script("docker exec forge-lgtm test -f /tmp/ready", execx.Result{})

	s := newStack(t, f)
	removed := false
	s.PortFree = func(context.Context, int) bool { return removed }

	// The fake records calls; mark the ports free once rm has been called.
	s.Runner = runnerFunc(func(ctx context.Context, c execx.Command) (execx.Result, error) {
		if c.String() == "docker rm -f forge-lgtm" {
			removed = true
		}

		return f.Run(ctx, c)
	}, f)

	if _, err := s.Up(context.Background()); err != nil {
		t.Fatal(err)
	}

	lines := f.CallLines()
	if slices.Index(lines, "docker rm -f forge-lgtm") < 0 || !strings.Contains(s.Out.(*strings.Builder).String(), "data is kept") {
		t.Fatalf("calls %v out %q", lines, s.Out)
	}

	for _, l := range lines {
		if strings.HasPrefix(l, "docker volume") {
			t.Fatal("recreate must keep the volume")
		}
	}
}

func TestUpReportsBusyPort(t *testing.T) {
	f := execx.NewFake(t)
	f.Script("docker container inspect forge-lgtm", noSuch())

	s := newStack(t, f)
	s.PortFree = func(_ context.Context, port int) bool { return port != 4318 }

	_, err := s.Up(context.Background())
	if !errors.Is(err, ErrPortInUse) || !strings.Contains(err.Error(), "forge obs up --otlp-port") || !strings.Contains(err.Error(), "4318") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpAnnouncesPull(t *testing.T) {
	f := execx.NewFake(t)
	f.Script("docker container inspect forge-lgtm", noSuch())
	f.Script("docker image inspect "+Image, execx.Result{ExitCode: 1, Stderr: "No such image"})
	f.Script("docker run", execx.Result{})
	f.Script("docker exec forge-lgtm test -f /tmp/ready", execx.Result{})

	s := newStack(t, f)
	if _, err := s.Up(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(s.Out.(*strings.Builder).String(), "Pulling "+Image) {
		t.Fatalf("out = %q", s.Out)
	}
}

func TestUpTimesOutWithLogs(t *testing.T) {
	f := execx.NewFake(t)
	f.Script("docker container inspect forge-lgtm", noSuch())
	f.Script("docker image inspect "+Image, execx.Result{})
	f.Script("docker run", execx.Result{})
	f.Script("docker exec forge-lgtm test -f /tmp/ready", execx.Result{ExitCode: 1})
	f.Script("docker logs --tail 40 forge-lgtm", execx.Result{Stdout: "tempo failed to start"})

	s := newStack(t, f)

	_, err := s.Up(context.Background())
	if !errors.Is(err, ErrNotReady) || !strings.Contains(s.Out.(*strings.Builder).String(), "tempo failed to start") {
		t.Fatalf("err = %v, out = %q", err, s.Out)
	}
}

func TestDown(t *testing.T) {
	f := execx.NewFake(t)
	f.Script("docker rm -f forge-lgtm", noSuch())
	f.Script("docker volume rm forge-lgtm-data", execx.Result{ExitCode: 1, Stderr: "Error response from daemon: get forge-lgtm-data: no such volume"})

	s := newStack(t, f)
	if err := s.Down(context.Background(), false); err != nil {
		t.Fatal(err)
	}

	if err := s.Down(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteState(s.Dir); err != nil {
		t.Fatal(err) // purge removed the dir; WriteState recreates it
	}
}

func TestTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/targets" {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[
			{"labels":{"job":"web"},"scrapeUrl":"http://host.docker.internal:8081/_/metrics","health":"down","lastError":"connection refused"},
			{"labels":{"job":"api"},"scrapeUrl":"http://host.docker.internal:8080/_/metrics","health":"up","lastError":""}]}}`))
	}))
	defer srv.Close()

	got, err := Targets(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Job != "api" || got[1].Health != "down" || got[1].LastError != "connection refused" {
		t.Fatalf("got %+v", got)
	}
}

func TestTargetsReportsUnreachablePrometheus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := Targets(context.Background(), http.DefaultClient, url)
	if err == nil || !strings.Contains(err.Error(), "Prometheus is not answering") {
		t.Fatalf("err = %v", err)
	}
}

// runnerFunc wraps Run while keeping the Fake's other methods.
type runnerWrap struct {
	*execx.Fake

	run func(context.Context, execx.Command) (execx.Result, error)
}

func (r runnerWrap) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	return r.run(ctx, c)
}

func runnerFunc(run func(context.Context, execx.Command) (execx.Result, error), f *execx.Fake) execx.Runner {
	return runnerWrap{Fake: f, run: run}
}

func TestPortFreeSeesWildcardListener(t *testing.T) {
	// Docker Desktop publishes ports on 0.0.0.0. A probe that only binds
	// 127.0.0.1 can succeed next to it on macOS and miss the conflict.
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port
	if portFree(t.Context(), port) {
		t.Fatalf("port %d is held on 0.0.0.0 but portFree says it is free", port)
	}
}

func TestUpRemovesContainerWhenRunFails(t *testing.T) {
	f := execx.NewFake(t)
	f.Script("docker container inspect forge-lgtm", noSuch())
	f.Script("docker image inspect "+Image, execx.Result{})
	f.Script("docker run", execx.Result{ExitCode: 125, Stderr: "docker: Error response from daemon: Bind for 0.0.0.0:9090 failed: port is already allocated"})
	f.Script("docker rm -f forge-lgtm", execx.Result{})

	_, err := newStack(t, f).Up(context.Background())
	if err == nil || !strings.Contains(err.Error(), "port is already allocated") {
		t.Fatalf("err = %v", err)
	}

	if strings.Contains(err.Error(), "--add-host") {
		t.Errorf("error repeats the whole docker command: %v", err)
	}

	if !slices.Contains(f.CallLines(), "docker rm -f forge-lgtm") {
		t.Fatalf("a failed run left the created container behind: %v", f.CallLines())
	}
}
