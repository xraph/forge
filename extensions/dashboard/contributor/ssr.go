package contributor

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	g "maragu.dev/gomponents"
)

// SSRContributor manages a Node.js SSR sidecar process that renders dashboard
// pages and widgets on the server. It starts a Node.js process, waits for it
// to be ready, and delegates rendering via HTTP to the running server.
//
// The SSR server must expose the standard Forge dashboard fragment endpoints:
//   - GET /_forge/dashboard/pages/*   → HTML page fragments
//   - GET /_forge/dashboard/widgets/* → HTML widget fragments
//   - GET /_forge/dashboard/settings/* → HTML settings fragments
//
// SSRContributor is framework-agnostic — it works with Astro SSR, Next.js,
// or any Node.js server that implements the endpoint convention.
type SSRContributor struct {
	manifest    *Manifest
	entryScript string
	workDir     string
	env         map[string]string
	port        int
	cmd         *exec.Cmd
	remote      *RemoteContributor
	mu          sync.Mutex
	started     bool
}

// SSROption configures an SSRContributor.
type SSROption func(*SSRContributor)

// WithSSRWorkDir sets the working directory for the Node.js process.
func WithSSRWorkDir(dir string) SSROption {
	return func(sc *SSRContributor) {
		sc.workDir = dir
	}
}

// WithSSREnv sets additional environment variables for the Node.js process.
func WithSSREnv(env map[string]string) SSROption {
	return func(sc *SSRContributor) {
		sc.env = env
	}
}

// NewSSRContributor creates an SSR contributor that will launch a Node.js
// process from the given entry script. Call Start() to launch the process.
func NewSSRContributor(manifest *Manifest, entryScript string, opts ...SSROption) *SSRContributor {
	sc := &SSRContributor{
		manifest:    manifest,
		entryScript: entryScript,
		env:         make(map[string]string),
	}
	for _, opt := range opts {
		opt(sc)
	}

	return sc
}

// Manifest returns the contributor's manifest.
func (sc *SSRContributor) Manifest() *Manifest {
	return sc.manifest
}

// Port returns the port the SSR server is listening on. Returns 0 if not started.
func (sc *SSRContributor) Port() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	return sc.port
}

// Started returns true if the SSR server is running.
func (sc *SSRContributor) Started() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	return sc.started
}

// Start launches the Node.js SSR server. It picks a free port, starts the
// process, and waits for the port to be accepting connections (max 10s).
func (sc *SSRContributor) Start(ctx context.Context) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.started {
		return nil
	}

	port, err := freePort()
	if err != nil {
		return fmt.Errorf("ssr contributor %q: get free port: %w", sc.manifest.Name, err)
	}

	sc.port = port

	cmd := exec.CommandContext(ctx, "node", sc.entryScript) //nolint:gosec // entryScript is operator-configured, not user input
	if sc.workDir != "" {
		cmd.Dir = sc.workDir
	}

	// Build environment
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))

	cmd.Env = append(cmd.Env, "HOST=127.0.0.1")
	for k, v := range sc.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssr contributor %q: start node: %w", sc.manifest.Name, err)
	}

	sc.cmd = cmd

	// Wait for the server to be ready
	if err := waitForPort(port, 10*time.Second); err != nil {
		// Kill the process if it didn't start in time
		_ = cmd.Process.Kill()

		return fmt.Errorf("ssr contributor %q: server did not start: %w", sc.manifest.Name, err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	sc.remote = NewRemoteContributor(baseURL, sc.manifest)
	sc.started = true

	return nil
}

// Stop kills the Node.js SSR server process.
func (sc *SSRContributor) Stop() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.started {
		return nil
	}

	sc.started = false
	sc.remote = nil

	if sc.cmd != nil && sc.cmd.Process != nil {
		if err := sc.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("ssr contributor %q: kill: %w", sc.manifest.Name, err)
		}
		// Wait to avoid zombie processes
		_ = sc.cmd.Wait()
	}

	return nil
}

// RenderPage renders a page fragment by delegating to the SSR server.
func (sc *SSRContributor) RenderPage(ctx context.Context, route string, _ Params) (g.Node, error) {
	sc.mu.Lock()
	remote := sc.remote
	sc.mu.Unlock()

	if remote == nil {
		return nil, fmt.Errorf("ssr contributor %q: not started", sc.manifest.Name)
	}

	data, err := remote.FetchPage(ctx, route)
	if err != nil {
		return nil, err
	}

	fragment := ExtractBodyFragment(data)

	return g.Raw(string(fragment)), nil
}

// RenderWidget renders a widget fragment by delegating to the SSR server.
func (sc *SSRContributor) RenderWidget(ctx context.Context, widgetID string) (g.Node, error) {
	sc.mu.Lock()
	remote := sc.remote
	sc.mu.Unlock()

	if remote == nil {
		return nil, fmt.Errorf("ssr contributor %q: not started", sc.manifest.Name)
	}

	data, err := remote.FetchWidget(ctx, widgetID)
	if err != nil {
		return nil, err
	}

	fragment := ExtractBodyFragment(data)

	return g.Raw(string(fragment)), nil
}

// RenderSettings renders a settings fragment by delegating to the SSR server.
func (sc *SSRContributor) RenderSettings(ctx context.Context, settingID string) (g.Node, error) {
	sc.mu.Lock()
	remote := sc.remote
	sc.mu.Unlock()

	if remote == nil {
		return nil, fmt.Errorf("ssr contributor %q: not started", sc.manifest.Name)
	}

	data, err := remote.FetchSettings(ctx, settingID)
	if err != nil {
		return nil, err
	}

	fragment := ExtractBodyFragment(data)

	return g.Raw(string(fragment)), nil
}

// freePort asks the OS for an available port on localhost.
func freePort() (int, error) {
	lc := &net.ListenConfig{}

	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitForPort waits for a TCP port to accept connections within the timeout.
func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	for time.Now().Before(deadline) {
		d := &net.Dialer{Timeout: 200 * time.Millisecond}

		conn, err := d.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			conn.Close()

			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("port %d did not open within %s", port, timeout)
}
