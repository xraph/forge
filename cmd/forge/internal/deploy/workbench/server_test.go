package workbench

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func serverFixture(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	root := testdata.Copy(t, "atlas-v2")

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Available["docker"] = true
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps", execx.Result{Stdout: "net/http\n"})
	f.Script("docker", execx.Result{})

	e, err := engine.New(engine.Options{Config: cfg, Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	s, err := New(Options{Engine: e, Root: root, Timeout: time.Minute, Token: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Close() })

	response := request(s, nil, "GET", s.URL(), "")
	if response.Code != http.StatusSeeOther {
		t.Fatal("token exchange failed", response.Code, response.Body.String())
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}

	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("weak session cookie", cookie)
	}

	return s, cookie
}
func request(s *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	parsed, _ := url.Parse(s.URL())
	if strings.HasPrefix(path, "/") {
		path = parsed.Scheme + "://" + parsed.Host + path
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if cookie != nil {
		req.AddCookie(cookie)
	}

	req.Header.Set("Origin", parsed.Scheme+"://"+parsed.Host)
	req.Header.Set("X-Forge-Workbench", "1")
	req.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)

	return response
}
func TestAuthBoundaryAndTokenReplay(t *testing.T) {
	s, cookie := serverFixture(t)
	if response := request(s, nil, "GET", s.URL(), ""); response.Code != 401 {
		t.Fatal("one-time token replay accepted", response.Code)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"missing cookie", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"wrong cookie", func(r *http.Request) { r.Header.Set("Cookie", cookie.Name+"=wrong") }, 401},
		{"missing header", func(r *http.Request) { r.Header.Del("X-Forge-Workbench") }, 403},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"foreign host", func(r *http.Request) { r.Host = "evil.example" }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := url.Parse(s.URL())
			req := httptest.NewRequestWithContext(context.Background(), "GET", u.Scheme+"://"+u.Host+"/api/files", nil)
			req.AddCookie(cookie)
			req.Header.Set("Origin", u.Scheme+"://"+u.Host)
			req.Header.Set("X-Forge-Workbench", "1")
			tc.mutate(req)

			response := httptest.NewRecorder()
			s.Handler().ServeHTTP(response, req)

			if response.Code != tc.status {
				t.Fatal(response.Code, response.Body.String())
			}
		})
	}

	if response := request(s, cookie, "GET", "/api/files", ""); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
}
func TestAPIRejectsMalformedAndEscapedRequests(t *testing.T) {
	s, cookie := serverFixture(t)
	for _, body := range []string{`{"target":"local","env":"dev","command":"touch outside"}`, `{} {}`, strings.Repeat(" ", 1<<20) + `{}`} {
		if response := request(s, cookie, "POST", "/api/plan", body); response.Code != 400 && response.Code != 413 {
			t.Fatal("invalid body accepted", response.Code)
		}
	}

	response := request(s, cookie, "POST", "/api/files", `{"expected":"stale","ops":[{"Path":"deploy.services.api.replicas","Value":2}]}`)
	if response.Code != 409 {
		t.Fatal("stale CAS accepted", response.Code, response.Body.String())
	}

	response = request(s, cookie, "POST", "/api/files", `{"path":"../../outside.yml","expected":"stale","ops":[]}`)
	if response.Code != 400 {
		t.Fatal("arbitrary browser path accepted", response.Code)
	}

	response = request(s, cookie, "POST", "/api/apply", `{"hash":"bad","approval":"different"}`)
	if response.Code != 409 {
		t.Fatal("wrong plan approval accepted", response.Code)
	}
}
func TestEventsRequireFetchHeader(t *testing.T) {
	s, cookie := serverFixture(t)
	u, _ := url.Parse(s.URL())
	req := httptest.NewRequestWithContext(context.Background(), "GET", u.Scheme+"://"+u.Host+"/api/events", nil)
	req.AddCookie(cookie)

	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)

	if response.Code != 403 {
		t.Fatal("EventSource without header accepted", response.Code)
	}
}
func TestCloseCancelsServerLifetime(t *testing.T) {
	s, _ := serverFixture(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server ignored cancellation")
	}
}

type blockingEngine struct {
	engineAPI

	started chan struct{}
}

func (b blockingEngine) LoadPlan(context.Context, string) (*plan.Plan, error) {
	return &plan.Plan{Hash: "approved", TargetName: "local", Environment: "dev"}, nil
}
func (b blockingEngine) Apply(ctx context.Context, _ *plan.Plan, _ string, _ bool, events chan<- provider.Event) error {
	close(b.started)

	events <- provider.Event{Op: "build", Message: "working"}

	<-ctx.Done()

	return ctx.Err()
}
func TestApplySerializesMutationsAndCancellation(t *testing.T) {
	s, cookie := serverFixture(t)
	started := make(chan struct{})
	s.engine = blockingEngine{s.engine, started}

	response := request(s, cookie, "POST", "/api/apply", `{"hash":"approved","approval":"approved"}`)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("operation did not start")
	}

	var result struct {
		Data Run `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	for _, route := range []string{"/api/apply", "/api/files"} {
		body := `{"hash":"approved","approval":"approved"}`
		if route == "/api/files" {
			body = `{"expected":"old","ops":[]}`
		}

		if response := request(s, cookie, "POST", route, body); response.Code != 409 {
			t.Fatal("parallel mutation accepted", response.Code, response.Body.String())
		}
	}

	if response := request(s, cookie, "POST", "/api/cancel", `{"run":"`+result.Data.ID+`"}`); response.Code != 200 {
		t.Fatal(response.Code)
	}

	s.wg.Wait()

	runs := s.currentRuns()
	if len(runs) != 1 || runs[0].Status != "cancelled" {
		t.Fatalf("cancelled run: %+v", runs)
	}

	events, _, _ := s.events.after(0)
	if len(events) < 3 || events[len(events)-1].Type != "cancelled" {
		t.Fatalf("missing progress %+v", events)
	}
}
func TestShutdownCancelsActiveOperation(t *testing.T) {
	s, _ := serverFixture(t)
	started := make(chan struct{})
	s.engine = blockingEngine{s.engine, started}

	_, err := s.startRun("apply", "local", "dev", func(ctx context.Context, events chan<- provider.Event) error {
		return s.engine.Apply(ctx, nil, "", false, events)
	})
	if err != nil {
		t.Fatal(err)
	}

	<-started

	_ = s.Close()
	done := make(chan struct{})

	go func() { s.wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel deployment")
	}
}
func TestEventReplayBoundsAndFutureCursor(t *testing.T) {
	buffer := newEvents()
	for range 300 {
		buffer.append(Event{Type: "operation"})
	}

	items, _, gap := buffer.after(1)
	if !gap || len(items) != 256 || items[0].ID != 45 {
		t.Fatal("event history was not bounded", gap, len(items))
	}

	items, _, gap = buffer.after(298)
	if gap || len(items) != 2 || items[0].ID != 299 {
		t.Fatal("replay cursor", items, gap)
	}

	if _, _, gap := buffer.after(999); !gap {
		t.Fatal("future cursor did not request reset")
	}
}
func TestPublicSettingsExcludeRuntimeValues(t *testing.T) {
	view := engine.SettingsView{Files: []engine.FileView{{Path: ".forge.yml", Content: "project:\n  name: atlas\nconfig:\n  password: runtime-secret\ndeploy:\n  version: 2 # preserve this comment\n"}}}

	public := publicSettings(view)
	if strings.Contains(public.Files[0].Content, "runtime-secret") || !strings.Contains(public.Files[0].Content, "preserve this comment") {
		t.Fatal("runtime configuration leaked or comments lost", public.Files)
	}

	if !strings.Contains(view.Files[0].Content, "runtime-secret") {
		t.Fatal("public view mutated engine authority")
	}
}
func TestLifecycleApprovalRejectsUnissuedProof(t *testing.T) {
	s, cookie := serverFixture(t)
	if response := request(s, cookie, "POST", "/api/lifecycle/apply", `{"hash":"fake","approval":"fake"}`); response.Code != 409 {
		t.Fatal(response.Code, response.Body.String())
	}

	if response := request(s, cookie, "POST", "/api/lifecycle/plan", `{"action":"shell","target":"local","env":"dev"}`); response.Code != 400 {
		t.Fatal(response.Code)
	}
}
func TestEventStreamReplaysWithFetchHeader(t *testing.T) {
	s, cookie := serverFixture(t)
	s.events.append(Event{Type: "started"})
	s.events.append(Event{Type: "completed"})

	server := httptest.NewServer(s.Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events?after=1", nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Host = s.host
	req.Header.Set("Origin", s.origin)
	req.Header.Set("X-Forge-Workbench", "1")
	req.AddCookie(cookie)

	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode)
	}

	reader := bufio.NewReader(response.Body)

	id, err := reader.ReadString('\n')
	if err != nil || id != "id: 2\n" {
		t.Fatal("replay failed", id, err)
	}

	kind, err := reader.ReadString('\n')
	if err != nil || kind != "event: progress\n" {
		t.Fatal(kind, err)
	}

	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, `"type":"completed"`) {
		t.Fatal(line, err)
	}

	cancel()
}
func TestSavedSettingsReloadAndCAS(t *testing.T) {
	s, cookie := serverFixture(t)

	view, err := s.engine.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	body := `{"expected":"` + view.Hash + `","ops":[{"path":"deploy.services.api.replicas","value":2}]}`

	response := request(s, cookie, "POST", "/api/files", body)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}

	if response = request(s, cookie, "POST", "/api/files", body); response.Code != 409 {
		t.Fatal("stale browser save", response.Code)
	}

	response = request(s, cookie, "GET", "/api/project", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"replicas":2`) {
		t.Fatal("saved settings missing after reload", response.Code, response.Body.String())
	}
}
