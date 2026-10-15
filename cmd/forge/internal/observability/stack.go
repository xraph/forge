package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
)

var (
	// ErrPortInUse reports a host port another process holds.
	ErrPortInUse = errors.New("observability: port in use")
	// ErrNotReady reports a stack that didn't finish starting in time.
	ErrNotReady = errors.New("observability: stack did not become ready")
)

const grafanaProvisioning = "/otel-lgtm/grafana/conf/provisioning"

// Stack runs grafana/otel-lgtm as one long-lived container with the files
// from WriteState mounted into it.
type Stack struct {
	Runner       execx.Runner
	Dir          string // state directory, see DefaultStateDir
	Name         string // container name, ContainerName when empty
	Volume       string // data volume, VolumeName when empty
	Network      string // user network, NetworkName when empty
	Image        string // Image when empty
	Ports        Ports
	Out          io.Writer     // progress lines, discarded when nil
	ReadyTimeout time.Duration // 90s when zero
	PollInterval time.Duration // 1s when zero
	// PortFree reports whether a host port is free. It checks 127.0.0.1 when nil.
	PortFree func(ctx context.Context, port int) bool
}

// ContainerState is what docker reports about the stack's container.
type ContainerState struct {
	Exists  bool
	Running bool
	Hash    string
	Ports   Ports
}

func (s *Stack) name() string   { return or(s.Name, ContainerName) }
func (s *Stack) volume() string { return or(s.Volume, VolumeName) }
func (s *Stack) image() string  { return or(s.Image, Image) }

func (s *Stack) network() string { return or(s.Network, NetworkName) }

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}

	return v
}

func (s *Stack) out() io.Writer {
	if s.Out == nil {
		return io.Discard
	}

	return s.Out
}

func (s *Stack) sayf(format string, args ...any) {
	_, _ = fmt.Fprintf(s.out(), format+"\n", args...)
}

func (s *Stack) docker(ctx context.Context, args ...string) (execx.Result, error) {
	return s.Runner.Run(ctx, execx.Command{Name: "docker", Args: args})
}

// notFound reports docker's "No such container/volume/image" and "network
// ... not found" answers.
func notFound(err error) bool {
	var exit *execx.ExitError
	if !errors.As(err, &exit) {
		return false
	}

	msg := strings.ToLower(exit.Result.Stderr)

	return strings.Contains(msg, "no such") || strings.Contains(msg, "not found")
}

// configHash identifies a container's configuration: the generated files,
// the image, the published ports and the network.
func configHash(files, image string, ports Ports, network string) string {
	h := sha256.Sum256([]byte(files + "\n" + image + "\n" + ports.String() + "\n" + network))

	return hex.EncodeToString(h[:])[:16]
}

// Inspect reports the container's state. A missing container is not an error.
func (s *Stack) Inspect(ctx context.Context) (ContainerState, error) {
	res, err := s.docker(ctx, "container", "inspect", s.name())
	if notFound(err) {
		return ContainerState{}, nil
	}

	if err != nil {
		return ContainerState{}, fmt.Errorf("observability: inspect %s: %w", s.name(), err)
	}

	var items []struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &items); err != nil || len(items) == 0 {
		return ContainerState{}, fmt.Errorf("observability: unreadable inspect output for %s", s.name())
	}

	st := ContainerState{Exists: true, Running: items[0].State.Running, Hash: items[0].Config.Labels[labelHash]}
	if p, err := ParsePorts(items[0].Config.Labels[labelPorts]); err == nil {
		st.Ports = p
	}

	return st, nil
}

// Up writes the state directory and makes sure the container runs with it.
// A container whose configuration differs is recreated; the data volume stays.
func (s *Stack) Up(ctx context.Context) (Ports, error) {
	files, err := WriteState(s.Dir)
	if err != nil {
		return Ports{}, err
	}

	want := configHash(files, s.image(), s.Ports, s.network())

	st, err := s.Inspect(ctx)
	if err != nil {
		return Ports{}, err
	}

	switch {
	case st.Exists && st.Hash == want && st.Running:
		return s.Ports, s.waitReady(ctx)
	case st.Exists && st.Hash == want:
		if _, err := s.docker(ctx, "start", s.name()); err != nil {
			return Ports{}, fmt.Errorf("observability: start %s: %w", s.name(), err)
		}

		return s.Ports, s.waitReady(ctx)
	case st.Exists:
		s.sayf("Configuration changed, recreating %s (your data is kept)", s.name())

		if _, err := s.docker(ctx, "rm", "-f", s.name()); err != nil {
			return Ports{}, fmt.Errorf("observability: remove %s: %w", s.name(), err)
		}
	}

	if err := s.checkPorts(ctx); err != nil {
		return Ports{}, err
	}

	if _, err := s.docker(ctx, "image", "inspect", s.image()); err != nil {
		s.sayf("Pulling %s (about 3.5 GB, first run only)", s.image())
	}

	if err := s.ensureNetwork(ctx); err != nil {
		return Ports{}, err
	}

	if _, err := s.docker(ctx, s.runArgs(want)...); err != nil {
		// docker run can fail after it has created the container, for
		// example when a port is taken. Left behind, the next Up would find
		// it with a matching hash and try to start it forever.
		_, _ = s.docker(ctx, "rm", "-f", s.name())

		return Ports{}, fmt.Errorf("observability: start %s: %s", s.name(), dockerMessage(err))
	}

	return s.Ports, s.waitReady(ctx)
}

func (s *Stack) runArgs(hash string) []string {
	mount := func(host, container string) string { return host + ":" + container + ":ro" }
	publish := func(host, container int) string {
		return "127.0.0.1:" + strconv.Itoa(host) + ":" + strconv.Itoa(container)
	}

	return []string{
		"run", "-d", "--name", s.name(),
		"--add-host=host.docker.internal:host-gateway",
		"--label", labelHash + "=" + hash,
		"--label", labelPorts + "=" + s.Ports.String(),
		"--network", s.network(),
		"-p", publish(s.Ports.Grafana, 3000),
		"-p", publish(s.Ports.OTLP, 4318),
		"-p", publish(s.Ports.Prometheus, 9090),
		"-v", s.volume() + ":/data",
		"-v", mount(filepath.Join(s.Dir, "prometheus.yaml"), "/otel-lgtm/prometheus.yaml"),
		"-v", mount(filepath.Join(s.Dir, "grafana-datasources.yaml"), grafanaProvisioning+"/datasources/grafana-datasources.yaml"),
		"-v", mount(filepath.Join(s.Dir, "grafana-dashboards-forge.yaml"), grafanaProvisioning+"/dashboards/forge.yaml"),
		"-v", mount(filepath.Join(s.Dir, "dashboards"), "/forge/dashboards"),
		"-v", mount(filepath.Join(s.Dir, "targets"), "/forge/targets"),
		"-e", "GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH=/forge/dashboards/forge-service.json",
		s.image(),
	}
}

// ensureNetwork creates the stack's user network when it doesn't exist yet.
func (s *Stack) ensureNetwork(ctx context.Context) error {
	_, err := s.docker(ctx, "network", "inspect", s.network())
	if err == nil {
		return nil
	}

	if !notFound(err) {
		return fmt.Errorf("observability: inspect network %s: %w", s.network(), err)
	}

	if _, err := s.docker(ctx, "network", "create", s.network()); err != nil {
		return fmt.Errorf("observability: create network %s: %s", s.network(), dockerMessage(err))
	}

	return nil
}

func (s *Stack) checkPorts(ctx context.Context) error {
	free := s.PortFree
	if free == nil {
		free = portFree
	}

	for _, p := range []struct {
		port       int
		what, flag string
	}{
		{s.Ports.Grafana, "Grafana", "--grafana-port"},
		{s.Ports.OTLP, "OTLP/HTTP", "--otlp-port"},
		{s.Ports.Prometheus, "Prometheus", "--prometheus-port"},
	} {
		if !free(ctx, p.port) {
			return fmt.Errorf("%w: %d (%s); stop whatever holds it, or start the stack with forge obs up %s <port>", ErrPortInUse, p.port, p.what, p.flag)
		}
	}

	return nil
}

// portFree probes the loopback address the stack binds and the wildcard
// address. Docker Desktop publishes on 0.0.0.0, and on macOS a loopback-only
// probe can bind next to that listener and miss the conflict.
func portFree(ctx context.Context, port int) bool {
	for _, host := range []string{"127.0.0.1", "0.0.0.0"} {
		l, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return false
		}

		_ = l.Close()
	}

	return true
}

// dockerMessage is docker's own stderr for a failed command, without the
// command line, which for docker run is long enough to bury the reason.
func dockerMessage(err error) string {
	var exit *execx.ExitError
	if errors.As(err, &exit) {
		if msg := strings.TrimSpace(exit.Result.Stderr); msg != "" {
			return msg
		}
	}

	return err.Error()
}

func (s *Stack) waitReady(ctx context.Context) error {
	timeout, poll := s.ReadyTimeout, s.PollInterval
	if timeout == 0 {
		timeout = 90 * time.Second
	}

	if poll == 0 {
		poll = time.Second
	}

	deadline := time.Now().Add(timeout)

	for {
		if _, err := s.docker(ctx, "exec", s.name(), "test", "-f", "/tmp/ready"); err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}

	if res, err := s.docker(ctx, "logs", "--tail", "40", s.name()); err == nil {
		_, _ = io.WriteString(s.out(), res.Stdout+res.Stderr)
	}

	return fmt.Errorf("%w within %s; the container is still running, see: docker logs %s", ErrNotReady, timeout, s.name())
}

// Down removes the container. With purge it also removes the data volume and
// the state directory.
func (s *Stack) Down(ctx context.Context, purge bool) error {
	if _, err := s.docker(ctx, "rm", "-f", s.name()); err != nil && !notFound(err) {
		return fmt.Errorf("observability: remove %s: %w", s.name(), err)
	}

	if !purge {
		return nil
	}

	if _, err := s.docker(ctx, "volume", "rm", s.volume()); err != nil && !notFound(err) {
		return fmt.Errorf("observability: remove volume %s: %w", s.volume(), err)
	}

	return os.RemoveAll(s.Dir)
}

// TargetHealth is one scrape target as Prometheus sees it.
type TargetHealth struct {
	Job       string
	URL       string
	Health    string
	LastError string
}

// Targets asks Prometheus at prometheusURL for its active scrape targets,
// sorted by job.
func Targets(ctx context.Context, client *http.Client, prometheusURL string) ([]TargetHealth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, prometheusURL+"/api/v1/targets?state=active", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("observability: Prometheus is not answering at %s yet: %w", prometheusURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("observability: Prometheus is not answering at %s yet: %s", prometheusURL, resp.Status)
	}

	var body struct {
		Data struct {
			ActiveTargets []struct {
				Labels    map[string]string `json:"labels"`
				ScrapeURL string            `json:"scrapeUrl"`
				Health    string            `json:"health"`
				LastError string            `json:"lastError"`
			} `json:"activeTargets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("observability: unreadable Prometheus targets: %w", err)
	}

	out := make([]TargetHealth, 0, len(body.Data.ActiveTargets))
	for _, t := range body.Data.ActiveTargets {
		out = append(out, TargetHealth{Job: t.Labels["job"], URL: t.ScrapeURL, Health: t.Health, LastError: t.LastError})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Job < out[j].Job })

	return out, nil
}
