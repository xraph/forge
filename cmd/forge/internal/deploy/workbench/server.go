// Package workbench serves the authenticated local deployment interface.
package workbench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

type engineAPI interface {
	Root() string
	Inspect(ctx context.Context, target, env string) (*engine.InspectResult, error)
	Doctor(ctx context.Context, target, env string, online bool) (output.Diagnostics, error)
	Files(ctx context.Context) (engine.SettingsView, error)
	Save(ctx context.Context, expected string, ops []spec.Op) error
	ConfigureStore(ctx context.Context, options persistence.Options, expected string) error
	PlanWithOptions(ctx context.Context, target, env string, options engine.PlanOptions) (*plan.Plan, *render.Bundle, error)
	LoadPlan(ctx context.Context, hash string) (*plan.Plan, error)
	Export(ctx context.Context, p *plan.Plan, b *render.Bundle, dir string, force bool) (render.WriteResult, error)
	Apply(ctx context.Context, p *plan.Plan, approval string, destructive bool, events chan<- provider.Event) error
	Status(ctx context.Context, target, env string) (provider.Status, error)
	Logs(ctx context.Context, target, env, service string, options provider.LogOptions) (io.ReadCloser, error)
	Connections(ctx context.Context) ([]engine.Connection, error)
	ConnectRegistry(ctx context.Context, name, host, username, token string) error
	Providers(ctx context.Context) []engine.ProviderInfo
	InitWithOptions(ctx context.Context, answers map[string]string, options engine.InitOptions) (*engine.InspectResult, map[string][]byte, error)
	History(ctx context.Context, target, env string) (state.Snapshot, error)
	LifecycleState(ctx context.Context, target, env string) (engine.LifecycleState, error)
	RollbackApproved(ctx context.Context, target, env, release, approval string) error
	DestroyApproved(ctx context.Context, target, env string, deleteData bool, approval string) error
}

type Options struct {
	Engine  *engine.Engine
	Root    string
	Port    int
	Token   string
	Timeout time.Duration
	Assets  http.Handler
}
type Server struct {
	engine     engineAPI
	root       string
	timeout    time.Duration
	listener   net.Listener
	http       *http.Server
	host       string
	origin     string
	token      string
	session    string
	cookieName string
	authMu     sync.Mutex
	exchanged  bool
	assets     http.Handler
	done       chan struct{}
	closeOnce  sync.Once
	mutations  sync.Mutex
	operations sync.Mutex
	active     *Run
	cancel     context.CancelFunc
	proofs     map[string]lifecycleProof
	runs       map[string]Run
	runOrder   []string
	wg         sync.WaitGroup
	events     *eventBuffer
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return hex.EncodeToString(raw), nil
}
func New(options Options) (*Server, error) {
	if options.Engine == nil {
		return nil, errors.New("workbench requires a deployment engine")
	}

	root, err := filepath.Abs(options.Engine.Root())
	if err != nil {
		return nil, err
	}

	if options.Root != "" {
		provided, err := filepath.Abs(options.Root)
		if err != nil || provided != root {
			return nil, errors.New("workbench root must match its deployment engine")
		}
	}

	if options.Port < 0 || options.Port > 65535 {
		return nil, errors.New("workbench port must be between 0 and 65535")
	}

	if options.Timeout == 0 {
		options.Timeout = 10 * time.Minute
	}

	if options.Timeout < time.Second || options.Timeout > 24*time.Hour {
		return nil, errors.New("operation timeout must be between one second and 24 hours")
	}

	token := options.Token
	if token == "" {
		token, err = randomToken()
		if err != nil {
			return nil, err
		}
	} else if len(token) < 32 {
		return nil, errors.New("workbench token must have at least 32 characters")
	}

	session, err := randomToken()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp4", "127.0.0.1:"+strconv.Itoa(options.Port))
	if err != nil {
		return nil, err
	}

	host := listener.Addr().String()

	_, port, err := net.SplitHostPort(host)
	if err != nil {
		_ = listener.Close()

		return nil, err
	}

	if options.Assets == nil {
		options.Assets = Assets()
	}

	s := &Server{engine: options.Engine, root: root, timeout: options.Timeout, listener: listener, host: host, origin: "http://" + host, token: token, session: session, cookieName: "forge_workbench_" + port, assets: options.Assets, done: make(chan struct{}), events: newEvents(), runs: map[string]Run{}, proofs: map[string]lifecycleProof{}}
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 8192}

	return s, nil
}
func (s *Server) URL() string           { return s.origin + "/?t=" + url.QueryEscape(s.token) }
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.handle) }
func (s *Server) Close() error {
	var result error

	s.closeOnce.Do(func() {
		close(s.done)
		s.operations.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.operations.Unlock()
		result = s.http.Close()
		_ = s.listener.Close()
	})

	return result
}
func (s *Server) Serve(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()

	err := s.http.Serve(s.listener)
	_ = s.Close()
	joined := make(chan struct{})

	go func() { s.wg.Wait(); close(joined) }()

	select {
	case <-joined:
	case <-time.After(30 * time.Second):
		return errors.New("deployment operations did not stop before shutdown deadline")
	}

	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}
