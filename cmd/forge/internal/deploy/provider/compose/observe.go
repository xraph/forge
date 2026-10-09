package compose

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"strconv"

	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"io"
	"strings"
)

type psRow struct {
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

func parseRows(raw string) ([]psRow, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var rows []psRow
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return nil, errors.New("invalid Compose status response")
		}

		return rows, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)

	for scanner.Scan() {
		var row psRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil || row.Service == "" {
			return nil, errors.New("invalid Compose status response")
		}

		rows = append(rows, row)
	}

	return rows, scanner.Err()
}
func (c *Compose) Observe(ctx context.Context, _ provider.EnvRef, st *state.Store) (provider.Status, error) {
	status := provider.Status{Overall: state.StatusUnknown, Services: map[string]provider.ServiceStatus{}, Resources: map[string]state.Status{}}

	snap, err := st.Snapshot()
	if err != nil {
		return status, err
	}

	hash := snap.ActivePlanHash
	if hash == "" && len(snap.Releases) > 0 {
		hash = snap.Releases[len(snap.Releases)-1].PlanHash
	}

	if hash == "" {
		return status, errors.New("no recorded deployment for this environment")
	}

	p, err := c.loadPlan(st, hash)
	if err != nil {
		return status, err
	}

	d := p.Deployment

	res, err := c.run(ctx, d, "ps", "-a", "--format", "json")
	if err != nil {
		return status, errors.New("docker status could not be read")
	}

	rows, err := parseRows(res.Stdout)
	if err != nil {
		return status, err
	}

	indexed := map[string][]psRow{}
	for _, row := range rows {
		indexed[row.Service] = append(indexed[row.Service], row)
	}

	allReady, accepted, failed := true, false, false

	for _, s := range d.Services {
		ss := provider.ServiceStatus{Desired: s.Replicas, Image: s.Image}
		if ss.Desired < 1 {
			ss.Desired = 1
		}

		running := 0

		for _, row := range indexed[s.Name] {
			if s.Kind == spec.KindJob && row.State == "exited" && row.ExitCode == 0 {
				ss.Ready++

				continue
			}

			if row.State == "running" {
				running++

				if row.Health == "healthy" {
					ss.Ready++
				}
			}

			if row.State == "exited" && row.ExitCode != 0 || row.Health == "unhealthy" {
				failed = true
				ss.Message = "workload failed"
			}
		}

		if ss.Ready < ss.Desired {
			if s.Health.None && running >= ss.Desired {
				accepted = true
				ss.Message = "running; readiness was disabled"
			} else {
				allReady = false

				if ss.Message == "" {
					ss.Message = "waiting for readiness"
				}
			}
		}

		status.Services[s.Name] = ss
	}

	for _, r := range d.Resources {
		if r.Lifecycle != spec.LifecycleContainer {
			status.Resources[r.Name] = state.StatusAccepted
			accepted = true

			continue
		}

		ready := 0

		for _, row := range indexed[r.Name] {
			if row.State == "running" && row.Health == "healthy" {
				ready++
			}

			if row.Health == "unhealthy" || row.State == "exited" && row.ExitCode != 0 {
				failed = true
			}
		}

		if ready == 1 {
			status.Resources[r.Name] = state.StatusHealthy
		} else {
			status.Resources[r.Name] = state.StatusPartial
			allReady = false
		}
	}

	switch {
	case failed:
		status.Overall = state.StatusFailed
	case allReady && accepted:
		status.Overall = state.StatusAccepted
	case allReady:
		status.Overall = state.StatusHealthy
	default:
		status.Overall = state.StatusPartial
	}

	// Routes are exposed endpoints; this status does not claim an external network reachability probe.
	for _, route := range d.Routes {
		for _, s := range d.Services {
			if route.Service == s.Name {
				status.Routes = append(status.Routes, fmt.Sprintf("http://localhost:%d", firstPort(s)))
			}
		}
	}

	return status, nil
}

type logReader struct {
	*io.PipeReader

	process execx.Process
}

func (r *logReader) Close() error {
	_ = r.process.Kill()

	return r.PipeReader.Close()
}
func (c *Compose) Logs(ctx context.Context, ref provider.ServiceRef, opts provider.LogOptions) (io.ReadCloser, error) {
	st, err := state.Open(c.root, ref.Target, ref.Env)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	snap, err := st.Snapshot()
	if err != nil {
		return nil, err
	}

	p, err := c.loadPlan(st, snap.ActivePlanHash)
	if err != nil {
		return nil, err
	}

	found := false

	for _, s := range p.Deployment.Services {
		if s.Name == ref.Service {
			found = true
		}
	}

	if !found {
		return nil, errors.New("service is not in the recorded deployment")
	}

	tail := opts.Tail
	if tail <= 0 {
		tail = 200
	}

	if tail > 10000 {
		tail = 10000
	}

	args := []string{"logs", "--no-color", "--tail", strconv.Itoa(tail)}
	if opts.Follow {
		args = append(args, "--follow")
	}

	args = append(args, ref.Service)
	reader, writer := io.Pipe()

	process, err := c.runner.Start(ctx, execx.Command{Name: "docker", Args: c.composeArgs(p.Deployment, args...), Dir: c.root, Stdout: writer, Stderr: writer})
	if err != nil {
		reader.Close()
		writer.Close()

		return nil, err
	}

	go func() {
		_, err := process.Wait()
		_ = writer.CloseWithError(err)
	}()

	return &logReader{PipeReader: reader, process: process}, nil
}

var _ provider.Provider = (*Compose)(nil)
