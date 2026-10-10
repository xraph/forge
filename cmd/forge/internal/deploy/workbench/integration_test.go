//go:build integration

package workbench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

type livePage struct {
	client   *http.Client
	origin   string
	cmd      *exec.Cmd
	done     chan error
	stopOnce sync.Once
}

func startLivePage(t *testing.T, binary, root string, options ...string) *livePage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	t.Cleanup(cancel)

	args := append([]string{"deploy", "start", "--config", filepath.Join(root, ".forge.yml"), "--no-open", "--non-interactive", "--output", "json"}, options...)
	cmd := exec.CommandContext(ctx, binary, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	page := &livePage{cmd: cmd, done: make(chan error, 1)}
	go func() { page.done <- cmd.Wait() }()

	t.Cleanup(func() { page.stop(t) })

	addresses := make(chan string, 1)

	go func() {
		var envelope struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		}
		if json.NewDecoder(stdout).Decode(&envelope) == nil {
			addresses <- envelope.Data.URL
		}

		_, _ = io.Copy(io.Discard, stdout)
	}()

	var address string

	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()

	select {
	case address = <-addresses:
	case <-deadline.C:
		t.Fatal("workbench did not report its URL")
	case err := <-page.done:
		t.Fatal("workbench exited before startup", err)
	}

	if address == "" {
		t.Fatal("missing workbench URL")
	}

	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}

	page.origin = parsed.Scheme + "://" + parsed.Host

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	page.client = &http.Client{Jar: jar, Timeout: 30 * time.Second}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := page.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatal("page authentication failed", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Contains(body, []byte("Forge deployment workbench")) {
		t.Fatal("embedded interface missing", err)
	}

	return page
}

func (p *livePage) stop(t *testing.T) {
	t.Helper()
	p.stopOnce.Do(func() {
		_ = p.cmd.Process.Signal(os.Interrupt)
		select {
		case err := <-p.done:
			if err != nil {
				t.Errorf("workbench shutdown: %v", err)
			}
		case <-time.After(35 * time.Second):
			_ = p.cmd.Process.Kill()

			t.Error("workbench shutdown timed out")
		}
	})
}

func (p *livePage) call(t *testing.T, path string, body, out any) int {
	t.Helper()

	method := http.MethodGet

	var raw []byte

	if body != nil {
		method = http.MethodPost

		var err error

		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}

	request, err := http.NewRequestWithContext(t.Context(), method, p.origin+"/api/"+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("X-Forge-Workbench", "1")
	request.Header.Set("Origin", p.origin)
	request.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var envelope struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *APIError       `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if !envelope.OK {
			t.Fatal("unsuccessful API response", path)
		}

		if out != nil {
			if err := json.Unmarshal(envelope.Data, out); err != nil {
				t.Fatal(err)
			}
		}
	} else if response.StatusCode != http.StatusConflict {
		t.Fatalf("API %s: status %d, %v", path, response.StatusCode, envelope.Error)
	}

	return response.StatusCode
}

func (p *livePage) events(t *testing.T) <-chan Event {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.origin+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("X-Forge-Workbench", "1")
	request.Header.Set("Origin", p.origin)
	// Streaming clients have no total-response timeout; the context owns their lifetime.
	client := &http.Client{Jar: p.client.Jar}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatal("event stream denied", response.StatusCode)
	}

	t.Cleanup(func() { _ = response.Body.Close() })

	events := make(chan Event, 256)
	go func() {
		defer close(events)

		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			var event Event
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
				continue
			}

			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return events
}

func (p *livePage) retainedLogs(t *testing.T) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, p.origin+"/api/logs?target=local&env=dev&service=api&tail=20", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("X-Forge-Workbench", "1")
	request.Header.Set("Origin", p.origin)

	response, err := p.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || len(raw) == 0 {
		t.Fatal("retained service logs unavailable", response.StatusCode, err)
	}
}

func (p *livePage) cancelApply(t *testing.T, plan *plan.Plan) {
	t.Helper()

	var run Run
	p.call(t, "apply", map[string]any{"hash": plan.Hash, "approval": plan.Hash}, &run)
	p.call(t, "cancel", map[string]any{"run": run.ID}, nil)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var runs []Run
		p.call(t, "runs", nil, &runs)

		for _, current := range runs {
			if current.ID == run.ID && current.Status == "cancelled" {
				return
			}
		}

		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(50 * time.Millisecond):
		}
	}

	t.Fatal("real provider run did not cancel")
}

func livePlan(t *testing.T, page *livePage, services []string) *plan.Plan {
	t.Helper()

	var result struct {
		Plan *plan.Plan `json:"plan"`
	}
	page.call(t, "plan", map[string]any{"target": "local", "env": "dev", "services": services}, &result)

	if result.Plan == nil || len(result.Plan.Hash) != 64 {
		t.Fatal("missing immutable plan")
	}

	return result.Plan
}

func liveApply(t *testing.T, page *livePage, plan *plan.Plan) {
	t.Helper()

	var run Run
	page.call(t, "apply", map[string]any{"hash": plan.Hash, "approval": plan.Hash, "allow_destructive": false}, &run)

	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		var runs []Run
		page.call(t, "runs", nil, &runs)

		for _, current := range runs {
			if current.ID != run.ID {
				continue
			}

			switch current.Status {
			case "completed":
				return
			case "failed", "cancelled":
				t.Fatalf("deployment %s: %v", current.Status, current.Error)
			}
		}

		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(250 * time.Millisecond):
		}
	}

	t.Fatal("deployment did not complete")
}

func TestWorkbenchComposeLifecycle(t *testing.T) {
	binary := os.Getenv("FORGE_DEPLOY_CLI")
	if binary == "" {
		t.Fatal("set FORGE_DEPLOY_CLI to the freshly built CLI")
	}

	for _, backend := range []string{"files", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			root := testdata.Copy(t, "atlas-v2")
			project := fmt.Sprintf("forge-workbench-it-%d", time.Now().UnixNano())
			file := filepath.Join(root, ".forge.yml")

			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			raw = bytes.Replace(raw, []byte("name: atlas"), []byte("name: "+project), 1)
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}

			var config net.ListenConfig

			listener, err := config.Listen(t.Context(), "tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}

			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()

			doc, ds, err := spec.Parse(file)
			if err != nil || ds.HasErrors() {
				t.Fatal(err, ds)
			}

			files, err := doc.Patch([]spec.Op{{Path: "deploy.targets.local.project", Value: project}, {Path: "deploy.targets.local.build.builder", Value: "host"}, {Path: "deploy.services.gateway.ports.http.port", Value: port}})
			if err != nil {
				t.Fatal(err)
			}

			if err := spec.Write(file, doc.Hash, files[file]); err != nil {
				t.Fatal(err)
			}

			command := exec.CommandContext(t.Context(), "go", "mod", "tidy")
			command.Dir = root

			command.Env = append(os.Environ(), "GOWORK=off")
			if _, err := command.CombinedOutput(); err != nil {
				t.Fatal("fixture dependency resolution", err)
			}

			bundle := filepath.Join(root, "deployments", "local", "dev", "compose.yaml")
			envfile := filepath.Join(root, ".forge", "state", "local", "dev", "generated.env")

			var images []string

			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()

				cmd := exec.CommandContext(ctx, "docker", "compose", "-p", project, "-f", bundle, "--env-file", envfile, "down", "-v", "--remove-orphans")

				cmd.Dir = root
				if output, err := cmd.CombinedOutput(); err != nil && len(images) > 0 {
					t.Errorf("owned stack cleanup: %v (%d output bytes)", err, len(output))
				}

				for _, image := range images {
					_ = exec.CommandContext(ctx, "docker", "image", "rm", image).Run()
				}
			})

			options := []string{}
			if backend == "sqlite" {
				options = []string{"--store", "sqlite", "--store-ref", ".forge/deploy.db"}
			}

			page := startLivePage(t, binary, root, options...)

			var view engine.SettingsView
			page.call(t, "files", nil, &view)

			if view.Store.Backend != backend {
				t.Fatal("wrong authority", view.Store)
			}

			if backend == "files" {
				f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}

				_, err = f.WriteString("\n# concurrent editor\n")
				_ = f.Close()

				if err != nil {
					t.Fatal(err)
				}

				status := page.call(t, "files", map[string]any{"expected": view.Hash, "ops": []spec.Op{{Path: "deploy.environments.dev.services", Value: []string{"api"}}}}, nil)
				if status != http.StatusConflict {
					t.Fatal("concurrent edit accepted")
				}

				page.call(t, "files", nil, &view)
			}

			page.call(t, "files", map[string]any{"expected": view.Hash, "ops": []spec.Op{{Path: "deploy.environments.dev.services", Value: []string{"api", "gateway", "worker"}}}}, &view)
			p := livePlan(t, page, []string{"api", "gateway", "worker"})
			page.call(t, "export", map[string]any{"hash": p.Hash}, nil)

			for _, service := range p.Deployment.Services {
				images = append(images, service.Image.Repository+":"+service.Image.Tag)
			}

			progress := page.events(t)
			liveApply(t, page, p)

			observed := false
			for !observed {
				select {
				case event, ok := <-progress:
					if !ok {
						t.Fatal("progress stream closed before completion")
					}

					observed = event.Type == "completed"
				case <-time.After(5 * time.Second):
					t.Fatal("completed progress event missing")
				}
			}

			var status provider.Status
			page.call(t, "status?target=local&env=dev", nil, &status)

			if status.Overall != "healthy" || len(status.Services) != 3 {
				t.Fatal("stack not healthy", status.Overall, status.Services)
			}

			var history state.Snapshot
			page.call(t, "history?target=local&env=dev", nil, &history)

			if len(history.Releases) != 1 {
				t.Fatal("release missing")
			}

			page.call(t, "files", nil, &view)
			page.call(t, "files", map[string]any{"expected": view.Hash, "ops": []spec.Op{{Path: "deploy.environments.dev.services", Value: []string{"worker"}}}}, &view)

			subset := livePlan(t, page, []string{"worker"})
			for _, service := range subset.Deployment.Services {
				images = append(images, service.Image.Repository+":"+service.Image.Tag)
			}

			liveApply(t, page, subset)
			page.call(t, "status?target=local&env=dev", nil, &status)

			if status.Overall != "healthy" || len(status.Services) != 3 {
				t.Fatal("subset lost retained workloads", status.Overall, status.Services)
			}

			page.retainedLogs(t)
			page.stop(t)
			page = startLivePage(t, binary, root)
			page.call(t, "files", nil, &view)

			if view.Store.Backend != backend || len(view.Deploy.Environments["dev"].Services) != 1 {
				t.Fatal("settings not retained after restart")
			}

			page.call(t, "history?target=local&env=dev", nil, &history)

			if len(history.Releases) != 2 {
				t.Fatal("release history not retained")
			}

			page.call(t, "status?target=local&env=dev", nil, &status)

			if status.Overall != "healthy" || len(status.Services) != 3 {
				t.Fatal("restart lost live state")
			}

			page.cancelApply(t, livePlan(t, page, []string{"worker"}))
			t.Log("real CLI, authenticated page, CAS, full/subset rollout, retained logs, cancellation and restart qualified", backend)
		})
	}
}
