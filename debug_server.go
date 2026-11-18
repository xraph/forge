//go:build forge_debug

package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// debugState holds the debug server state for a running forge app.
type debugState struct {
	a       *app
	addr    string
	hub     *debugHub
	httpSrv *http.Server
	stopCh  chan struct{}
}

// initDebugServer starts the debug HTTP/WebSocket server when FORGE_DEBUG_PORT
// is set in the environment. Called from newApp(); no-op in release builds
// (debug_server_stub.go provides the stub).
func initDebugServer(a *app) {
	port := os.Getenv("FORGE_DEBUG_PORT")
	if port == "" {
		return
	}
	addr := "127.0.0.1:" + port
	workspaceDir := os.Getenv("FORGE_DEBUG_WORKSPACE")

	ds := &debugState{
		a:      a,
		addr:   addr,
		hub:    newDebugHub(),
		stopCh: make(chan struct{}),
	}

	ds.httpSrv = &http.Server{
		Addr:        addr,
		Handler:     ds.handler(),
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	// Start the listener before registering the hook so the port is bound
	// before the app finishes starting.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		a.logger.Warn("forge debug: failed to start debug server",
			F("addr", addr),
			F("error", err),
		)
		return
	}

	// Register process in ~/.forge/debug-servers.json.
	appAddr := os.Getenv("PORT")
	if appAddr == "" {
		appAddr = a.config.HTTPAddress
	}
	if appAddr != "" && appAddr[0] != ':' {
		appAddr = ":" + appAddr
	}
	entry := DebugServerEntry{
		AppName:      a.config.Name,
		AppVersion:   a.config.Version,
		DebugAddr:    addr,
		AppAddr:      "localhost" + appAddr,
		WorkspaceDir: workspaceDir,
	}
	if regErr := debugRegisterServer(entry); regErr != nil {
		a.logger.Warn("forge debug: failed to write server registry",
			F("error", regErr),
		)
	}

	// Register a lifecycle hook to stop the debug server gracefully.
	_ = a.RegisterHookFn(PhaseBeforeStop, "forge-debug-stop", func(ctx context.Context, _ App) error {
		close(ds.stopCh)
		shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = ds.httpSrv.Shutdown(shutCtx)
		debugUnregisterServer(addr)
		return nil
	})

	// Register a lifecycle hook to start the tick goroutine after the app is running.
	_ = a.RegisterHookFn(PhaseAfterRun, "forge-debug-tick", func(ctx context.Context, _ App) error {
		go ds.tickLoop(ctx, "localhost"+appAddr)
		return nil
	})

	go func() {
		if err := ds.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case <-ds.stopCh:
			default:
				a.logger.Error("forge debug server error", F("error", err))
			}
		}
	}()

	a.logger.Info("forge debug server started",
		F("addr", addr),
	)
}

// maxDebugFrame caps a WebSocket frame read from a debug client. The debug
// protocol only sends to clients; what they send is pings and closes, so
// anything larger is not a debug client.
const maxDebugFrame = 64 << 10

// handler returns the debug server's routes behind the local-only checks.
func (ds *debugState) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/state", ds.handleState)
	mux.HandleFunc("/ws", ds.handleWebSocket)

	return localOnly(mux)
}

// localOnly rejects requests that did not come from a local tool.
//
// The server listens on 127.0.0.1, but a browser on the same machine can
// still reach it: any page can open a WebSocket to localhost (browsers do
// not apply CORS to upgrades), and DNS rebinding lets a page fetch /state
// through a hostname that resolves to 127.0.0.1. Both carry the page's own
// Origin or Host, so requiring a loopback Host and an empty or loopback
// Origin keeps the snapshot (routes, config, services) away from web pages
// while the CLI and the editor extension, which send neither a foreign
// Host nor an Origin, keep working.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)

			return
		}

		if origin := r.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin) {
			http.Error(w, "forbidden origin", http.StatusForbidden)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether a Host header names this machine:
// localhost, 127.0.0.1 or ::1, with or without a port.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// isLoopbackOrigin reports whether an Origin is an http or https origin on a
// loopback host.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}

	return isLoopbackHost(u.Host)
}

func (ds *debugState) handleState(w http.ResponseWriter, r *http.Request) {
	snap := ds.buildSnapshot(r.Context())
	msg := DebugMessage{
		Type:      DebugMsgSnapshot,
		Timestamp: time.Now().UnixMilli(),
		AppName:   ds.a.config.Name,
		Payload:   snap,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msg)
}

func (ds *debugState) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}
	ds.hub.add(conn)

	// Send initial snapshot.
	snap := ds.buildSnapshot(r.Context())
	msg := DebugMessage{
		Type:      DebugMsgSnapshot,
		Timestamp: time.Now().UnixMilli(),
		AppName:   ds.a.config.Name,
		Payload:   snap,
	}
	if data, err := json.Marshal(msg); err == nil {
		_ = wsutil.WriteServerMessage(conn, ws.OpText, data)
	}

	// Read loop: drain frames until the client disconnects.
	for {
		hdr, err := ws.ReadHeader(conn)
		if err != nil {
			break
		}

		// The length is the client's to choose, up to 2^63, so it is never
		// used to size an allocation. Payloads are discarded as they are
		// read, and a frame over the cap ends the connection.
		if hdr.Length > maxDebugFrame {
			break
		}

		if hdr.Length > 0 {
			if _, err := io.CopyN(io.Discard, conn, hdr.Length); err != nil {
				break
			}
		}

		if hdr.OpCode == ws.OpPing {
			_ = ws.WriteHeader(conn, ws.Header{OpCode: ws.OpPong, Fin: true})
		}

		if hdr.OpCode == ws.OpClose {
			break
		}
	}
	ds.hub.remove(conn)
}

func (ds *debugState) tickLoop(ctx context.Context, appAddr string) {
	interval := 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ds.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ds.hub.count() == 0 {
				continue
			}
			ds.broadcastMetrics(ctx, appAddr)
			ds.broadcastHealth(ctx)
		}
	}
}

func (ds *debugState) broadcastMetrics(ctx context.Context, appAddr string) {
	raw := fetchDebugMetrics(ctx, appAddr)
	ds.broadcast(DebugMsgMetrics, DebugMetrics{Raw: raw})
}

func (ds *debugState) broadcastHealth(ctx context.Context) {
	health := ds.buildHealth(ctx)
	if health != nil {
		ds.broadcast(DebugMsgHealth, health)
	}
}

func (ds *debugState) broadcast(msgType DebugMessageType, payload any) {
	msg := DebugMessage{
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
		AppName:   ds.a.config.Name,
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	ds.hub.broadcast(data)
}

func (ds *debugState) buildSnapshot(ctx context.Context) DebugSnapshot {
	return DebugSnapshot{
		App:        ds.buildAppInfo(),
		Config:     ds.buildConfigMap(),
		Services:   ds.a.container.Services(),
		Routes:     ds.buildRoutes(),
		Extensions: ds.buildExtensions(ctx),
		Health:     ds.buildHealth(ctx),
	}
}

func (ds *debugState) buildAppInfo() DebugAppInfo {
	return DebugAppInfo{
		Name:        ds.a.config.Name,
		Version:     ds.a.config.Version,
		Environment: ds.a.config.Environment,
		HTTPAddr:    ds.a.config.HTTPAddress,
		DebugAddr:   ds.addr,
		UptimeMs:    ds.a.Uptime().Milliseconds(),
	}
}

func (ds *debugState) buildConfigMap() map[string]any {
	cfg := ds.a.configManager
	if cfg == nil {
		return nil
	}
	keys := []string{"name", "version", "environment", "http.address", "metrics.enabled", "health.enabled"}
	result := make(map[string]any, len(keys))
	for _, k := range keys {
		if v := cfg.Get(k); v != nil {
			result[k] = v
		}
	}
	return result
}

func (ds *debugState) buildRoutes() []DebugRoute {
	raw := ds.a.router.Routes()
	out := make([]DebugRoute, 0, len(raw))
	for _, r := range raw {
		out = append(out, DebugRoute{
			Name:        r.Name,
			Method:      r.Method,
			Path:        r.Path,
			Tags:        r.Tags,
			Summary:     r.Summary,
			Description: r.Description,
		})
	}
	return out
}

func (ds *debugState) buildExtensions(ctx context.Context) []DebugExtInfo {
	exts := ds.a.extensions
	out := make([]DebugExtInfo, 0, len(exts))
	for _, e := range exts {
		healthy := e.Health(ctx) == nil
		out = append(out, DebugExtInfo{
			Name:         e.Name(),
			Version:      e.Version(),
			Description:  e.Description(),
			Dependencies: e.Dependencies(),
			Healthy:      healthy,
		})
	}
	return out
}

func (ds *debugState) buildHealth(ctx context.Context) *DebugHealth {
	hm := ds.a.healthManager
	if hm == nil {
		return nil
	}
	report := hm.Check(ctx)
	if report == nil {
		return nil
	}
	checks := make(map[string]DebugCheckResult, len(report.Services))
	for name, res := range report.Services {
		checks[name] = DebugCheckResult{
			Status:     string(res.Status),
			Message:    res.Message,
			ResponseMs: res.Duration.Milliseconds(),
		}
	}
	return &DebugHealth{
		Overall: string(report.Overall),
		Checks:  checks,
	}
}

// fetchDebugMetrics performs an in-process HTTP GET against the app's /_/metrics
// endpoint to obtain the canonical Prometheus text payload.
func fetchDebugMetrics(ctx context.Context, appAddr string) string {
	if appAddr == "" {
		return ""
	}
	url := fmt.Sprintf("http://%s/_/metrics", appAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}
