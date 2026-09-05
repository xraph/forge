package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// statusExt is a Forge extension that both registers a contract contributor
// and reports a dashboard status, with a setter so a test can flip Configured
// after discovery the way a real extension does when it is configured at
// runtime. contributor is the name it registers; when empty it registers
// nothing, which is the un-attributable case.
type statusExt struct {
	*forge.BaseExtension

	contributor string
	// legacy, when set, makes this extension a DashboardAware one whose
	// legacy manifest carries a contract manifest — the mirror registration
	// path, which is a second way a contributor reaches the contract
	// registry.
	legacy *legacyContributor

	mu     sync.Mutex
	status DashboardStatus
}

func newStatusExt(name, contributorName string, st DashboardStatus) *statusExt {
	return &statusExt{
		BaseExtension: forge.NewBaseExtension(name, "test", "test"),
		contributor:   contributorName,
		status:        st,
	}
}

// DashboardContributor makes this extension DashboardAware. Returning nil is
// an explicit opt-out the discovery loop handles, which is what extensions
// without a legacy contributor do here.
func (s *statusExt) DashboardContributor() contributor.LocalContributor {
	if s.legacy == nil {
		return nil
	}

	return s.legacy
}

// legacyContributor is a LocalContributor whose legacy manifest publishes a
// contract manifest alongside it.
type legacyContributor struct {
	m *contributor.Manifest
}

func newLegacyContributor(t *testing.T, name string) *legacyContributor {
	t.Helper()

	src := `
schemaVersion: 1
contributor: { name: ` + name + `, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: ` + name + `.list, kind: query, version: 1, capability: read }
`

	var cm contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &cm); err != nil {
		t.Fatal(err)
	}

	return &legacyContributor{m: &contributor.Manifest{
		Name:        name,
		DisplayName: name,
		Version:     "1.0.0",
		Contract:    &cm,
	}}
}

func (l *legacyContributor) Manifest() *contributor.Manifest { return l.m }

func (l *legacyContributor) RenderPage(context.Context, string, contributor.Params) (templ.Component, error) {
	return nil, nil
}

func (l *legacyContributor) RenderWidget(context.Context, string) (templ.Component, error) {
	return nil, nil
}

func (l *legacyContributor) RenderSettings(context.Context, string) (templ.Component, error) {
	return nil, nil
}

func (s *statusExt) DashboardStatus() DashboardStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status
}

func (s *statusExt) setStatus(st DashboardStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status = st
}

// RegisterContractContributor registers through the registry it is handed,
// exactly as a real extension does — which is the only reason the host can
// learn which contributor name belongs to this extension.
func (s *statusExt) RegisterContractContributor(
	_ *dispatcher.Dispatcher,
	reg contract.Registry,
	_ contract.WardenRegistry,
) error {
	if s.contributor == "" {
		return nil
	}

	src := `
schemaVersion: 1
contributor: { name: ` + s.contributor + `, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: ` + s.contributor + `.list, kind: query, version: 1, capability: read }
`

	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		return err
	}

	return reg.Register(&m)
}

// warnLogger records Warn messages so a test can assert on the diagnostic the
// host emits when it cannot attribute an extension's status.
type warnLogger struct {
	forge.Logger

	mu    sync.Mutex
	warns []string
}

func (l *warnLogger) Warn(msg string, fields ...forge.Field) {
	l.mu.Lock()
	l.warns = append(l.warns, msg)
	l.mu.Unlock()
	l.Logger.Warn(msg, fields...)
}

func (l *warnLogger) warned(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, w := range l.warns {
		if strings.Contains(w, substr) {
			return true
		}
	}

	return false
}

// statusTestApp is a forge.App whose only meaningful method is Extensions().
type statusTestApp struct {
	exts []forge.Extension
}

func (a *statusTestApp) Extensions() []forge.Extension { return a.exts }

func (a *statusTestApp) Name() string                                   { return "test-app" }
func (a *statusTestApp) Version() string                                { return "1.0.0" }
func (a *statusTestApp) Environment() string                            { return "test" }
func (a *statusTestApp) Container() forge.Container                     { return nil }
func (a *statusTestApp) Router() forge.Router                           { return nil }
func (a *statusTestApp) Config() forge.ConfigManager                    { return nil }
func (a *statusTestApp) Logger() forge.Logger                           { return forge.NewNoopLogger() }
func (a *statusTestApp) Metrics() forge.Metrics                         { return forge.NewNoOpMetrics() }
func (a *statusTestApp) HealthManager() forge.HealthManager             { return nil }
func (a *statusTestApp) LifecycleManager() forge.LifecycleManager       { return nil }
func (a *statusTestApp) Start(context.Context) error                    { return nil }
func (a *statusTestApp) Stop(context.Context) error                     { return nil }
func (a *statusTestApp) Run() error                                     { return nil }
func (a *statusTestApp) RegisterController(forge.Controller) error      { return nil }
func (a *statusTestApp) RegisterExtension(forge.Extension) error        { return nil }
func (a *statusTestApp) GetExtension(string) (forge.Extension, error)   { return nil, nil }
func (a *statusTestApp) StartTime() time.Time                           { return time.Now() }
func (a *statusTestApp) Uptime() time.Duration                          { return 0 }
func (a *statusTestApp) MigrationsDisabled() bool                       { return false }
func (a *statusTestApp) SetMigrationsDisabled(bool)                     {}
func (a *statusTestApp) CentralMigrationsEnabled() bool                 { return false }
func (a *statusTestApp) CentralMigrator() (forge.CentralMigrator, bool) { return nil, false }
func (a *statusTestApp) RegisterService(string, forge.Factory, ...forge.RegisterOption) error {
	return nil
}

func (a *statusTestApp) RegisterHook(forge.LifecyclePhase, forge.LifecycleHook, forge.LifecycleHookOptions) error {
	return nil
}

func (a *statusTestApp) RegisterHookFn(forge.LifecyclePhase, string, forge.LifecycleHook) error {
	return nil
}

// newDiscoveryTestExt builds a dashboard Extension with the contract track
// wired and the given extensions visible to discovery.
func newDiscoveryTestExt(t *testing.T, log forge.Logger, exts ...forge.Extension) *Extension {
	t.Helper()

	e := newTestDashboardExt(t)
	if log != nil {
		e.BaseExtension.SetLogger(log)
	}

	e.contractRegistry = contract.NewRegistry()
	e.wardenRegistry = contract.NewWardenRegistry()
	e.dispatcher = dispatcher.New(nil)
	e.app = &statusTestApp{exts: exts}

	return e
}

// capabilityStatus serves the real capabilities endpoint off e and returns each
// contributor's raw JSON keyed by name.
func capabilityStatus(t *testing.T, e *Extension) map[string]map[string]json.RawMessage {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/capabilities", nil)
	w := httptest.NewRecorder()
	e.handleContractCapabilities()(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}

	var raw struct {
		Contributors []map[string]json.RawMessage `json:"contributors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal %s: %v", w.Body.Bytes(), err)
	}

	out := make(map[string]map[string]json.RawMessage, len(raw.Contributors))

	for _, c := range raw.Contributors {
		var name string
		if err := json.Unmarshal(c["name"], &name); err != nil {
			t.Fatal(err)
		}

		out[name] = c
	}

	return out
}

func field(t *testing.T, c map[string]json.RawMessage, key string) string {
	t.Helper()

	return string(c[key])
}

// TestDiscovery_AttributesStatusPerExtension drives the whole path the running
// server uses: the real discovery loop registers two extensions' contract
// contributors, and the real capabilities endpoint is asked what it knows.
// Nothing here reaches past the seams into the attribution helpers, so
// unwiring any link in the chain — the recorder, the record call, the handler
// argument — shows up as a failure.
//
// Two extensions, not one, because cross-attribution is the failure this
// design exists to prevent: one extension's status showing up on another's
// contributor is invisible with a single extension under test.
func TestDiscovery_AttributesStatusPerExtension(t *testing.T) {
	billing := newStatusExt("billing", "billing", DashboardStatus{
		Version: "1.0.0", Configured: false, Message: "set SMTP host",
	})
	analytics := newStatusExt("analytics", "analytics", DashboardStatus{
		Version: "3.4.5", Configured: true,
	})

	e := newDiscoveryTestExt(t, nil, billing, analytics)
	e.discoverExtensionContributors(context.Background())

	got := capabilityStatus(t, e)
	if len(got) != 2 {
		t.Fatalf("contributors = %v, want billing and analytics", got)
	}

	if v := field(t, got["billing"], "version"); v != `"1.0.0"` {
		t.Errorf("billing version = %s, want \"1.0.0\" — the billing extension's "+
			"status did not reach the capabilities endpoint", v)
	}

	if v := field(t, got["billing"], "configured"); v != "false" {
		t.Errorf("billing configured = %q, want false", v)
	}

	if v := field(t, got["billing"], "message"); v != `"set SMTP host"` {
		t.Errorf("billing message = %s, want \"set SMTP host\"", v)
	}

	if v := field(t, got["analytics"], "version"); v != `"3.4.5"` {
		t.Errorf("analytics version = %s, want \"3.4.5\" — a version of 1.0.0 here "+
			"means billing's status was attributed to analytics", v)
	}

	if v := field(t, got["analytics"], "configured"); v != "true" {
		t.Errorf("analytics configured = %q, want true — false here means "+
			"billing's status was attributed to analytics", v)
	}

	// Configured flips once the extension is set up. The endpoint must read
	// the live answer, not one snapshotted during discovery.
	billing.setStatus(DashboardStatus{Version: "1.0.0", Configured: true})

	got = capabilityStatus(t, e)
	if v := field(t, got["billing"], "configured"); v != "true" {
		t.Errorf("billing configured = %q after the extension flipped it, want true; "+
			"the status is being snapshotted instead of read live", v)
	}
}

// TestDiscovery_WarnsWhenStatusCannotBeAttributed covers the diagnostic for the
// registration paths attribution cannot reach — RegisterContributor takes a
// LocalContributor, so there is no extension to attribute to. Such an
// extension's contributors report the permissive default, which renders a
// broken setup as ready. The warning is the only thing that makes that visible.
func TestDiscovery_WarnsWhenStatusCannotBeAttributed(t *testing.T) {
	log := &warnLogger{Logger: forge.NewNoopLogger()}

	// Reports a status, registers nothing through the recorded path.
	orphan := newStatusExt("orphan", "", DashboardStatus{Version: "1.0.0", Configured: false})
	e := newDiscoveryTestExt(t, log, orphan)
	e.discoverExtensionContributors(context.Background())

	if !log.warned("registered no contract contributor") {
		t.Errorf("no warning for an extension whose status could not be attributed; warns = %v", log.warns)
	}
}

// TestDiscovery_NoWarningWhenAttributionSucceeds discriminates the warning
// above: it must fire on the un-attributable case only, not on every
// DashboardStatusAware extension.
func TestDiscovery_NoWarningWhenAttributionSucceeds(t *testing.T) {
	log := &warnLogger{Logger: forge.NewNoopLogger()}

	billing := newStatusExt("billing", "billing", DashboardStatus{Version: "1.0.0", Configured: true})
	e := newDiscoveryTestExt(t, log, billing)
	e.discoverExtensionContributors(context.Background())

	if log.warned("registered no contract contributor") {
		t.Errorf("warned about an extension whose contributor was attributed fine; warns = %v", log.warns)
	}
}

// TestUnregisterRemoteContractContributor_ForgetsStatus pins the prune. The map keys on
// contributor name, so a name released by an unregister and later claimed by
// someone else would otherwise serve the previous owner's live status.
func TestUnregisterRemoteContractContributor_ForgetsStatus(t *testing.T) {
	billing := newStatusExt("billing", "billing", DashboardStatus{Version: "1.0.0", Configured: false})
	e := newDiscoveryTestExt(t, nil, billing)
	e.discoverExtensionContributors(context.Background())

	if _, ok := e.contributorStatusFor("billing"); !ok {
		t.Fatalf("billing has no status after discovery")
	}

	e.UnregisterRemoteContractContributor("billing")

	if st, ok := e.contributorStatusFor("billing"); ok {
		t.Errorf("billing still has status %+v after unregister; the next owner of "+
			"the name would serve the previous owner's status", st)
	}
}

// TestRecordingRegistry_UnregisterForgetsStatus is the sibling of the test
// above, taking the other of the two routes that free a contributor name. An
// extension keeps the registry it was handed and can unregister through it at
// any time; if that route leaves the attribution behind, the next party to
// claim the name serves this extension's live status.
func TestRecordingRegistry_UnregisterForgetsStatus(t *testing.T) {
	e := newDiscoveryTestExt(t, nil)
	sa := newStatusExt("billing", "", DashboardStatus{Version: "1.0.0", Configured: false})
	reg := &recordingRegistry{
		Registry: e.contractRegistry,
		rec:      &contributorStatusRecorder{ext: sa, host: e},
	}

	if err := reg.Register(testManifest(t, "billing")); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, ok := e.contributorStatusFor("billing"); !ok {
		t.Fatalf("billing has no status after registering through the recording registry")
	}

	reg.Unregister("billing")

	if st, ok := e.contributorStatusFor("billing"); ok {
		t.Errorf("billing still has status %+v after unregistering through the "+
			"recording registry; the next owner of the name would serve the "+
			"previous owner's status", st)
	}
}

// TestDiscovery_AttributesStatusFromMirroredContractManifest covers the second
// way a contract contributor reaches the registry during discovery: a legacy
// DashboardAware contributor whose manifest publishes a contract manifest
// alongside it. That path never calls RegisterContractContributor, so the
// recording registry never sees it and it has to be attributed on its own.
func TestDiscovery_AttributesStatusFromMirroredContractManifest(t *testing.T) {
	ext := newStatusExt("reports", "", DashboardStatus{
		Version: "9.9.9", Configured: false, Message: "pick a warehouse",
	})
	ext.legacy = newLegacyContributor(t, "reports")

	e := newDiscoveryTestExt(t, nil, ext)
	e.discoverExtensionContributors(context.Background())

	got := capabilityStatus(t, e)

	reports, ok := got["reports"]
	if !ok {
		t.Fatalf("no reports contributor in %v", got)
	}

	if v := field(t, reports, "version"); v != `"9.9.9"` {
		t.Errorf("reports version = %s, want \"9.9.9\" — a contract manifest "+
			"mirrored from a legacy contributor is not being attributed", v)
	}

	if v := field(t, reports, "configured"); v != "false" {
		t.Errorf("reports configured = %q, want false", v)
	}
}

// TestContributorStatus_ConcurrentRegistrationAndServe pins the lock. An
// extension may keep the registry it was handed and register more contributors
// later from its own goroutine, while the capabilities endpoint is already
// serving. Under -race, dropping contributorStatusMu fails here.
func TestContributorStatus_ConcurrentRegistrationAndServe(t *testing.T) {
	e := newDiscoveryTestExt(t, nil)
	sa := newStatusExt("late", "", DashboardStatus{Version: "1.0.0", Configured: true})
	reg := &recordingRegistry{
		Registry: e.contractRegistry,
		rec:      &contributorStatusRecorder{ext: sa, host: e},
	}

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := range 20 {
			if err := reg.Register(testManifest(t, fmt.Sprintf("late%d", i))); err != nil {
				t.Errorf("register: %v", err)

				return
			}
		}
	}()

	go func() {
		defer wg.Done()

		h := e.handleContractCapabilities()

		for range 20 {
			w := httptest.NewRecorder()
			h(w, httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/capabilities", nil))

			if w.Code != http.StatusOK {
				t.Errorf("status = %d", w.Code)

				return
			}
		}
	}()

	wg.Wait()

	if _, ok := e.contributorStatusFor("late19"); !ok {
		t.Errorf("late19 has no status after concurrent registration")
	}
}

// testManifest builds a one-intent contract manifest for the named contributor.
func testManifest(t *testing.T, name string) *contract.ContractManifest {
	t.Helper()

	src := `
schemaVersion: 1
contributor: { name: ` + name + `, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: ` + name + `.list, kind: query, version: 1, capability: read }
`

	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}

	return &m
}
