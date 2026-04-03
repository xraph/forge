package cli

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
)

// --- Test RunAppOption functions ---

func TestWithAutoMigrate(t *testing.T) {
	r := NewAppRunner(nil)
	WithAutoMigrate()(r)
	if !r.autoMigrateOnServe {
		t.Error("expected autoMigrateOnServe to be true")
	}
}

func TestWithExtraCommands(t *testing.T) {
	r := NewAppRunner(nil)
	cmd := NewCommand("test", "Test", nil)
	WithExtraCommands(cmd)(r)
	if len(r.extraCommands) != 1 {
		t.Fatalf("expected 1 extra command, got %d", len(r.extraCommands))
	}
	if r.extraCommands[0].Name() != "test" {
		t.Errorf("expected command name 'test', got '%s'", r.extraCommands[0].Name())
	}
}

func TestWithDisableMigrationCommands(t *testing.T) {
	r := NewAppRunner(nil)
	WithDisableMigrationCommands()(r)
	if !r.disableMigrationCommands {
		t.Error("expected disableMigrationCommands to be true")
	}
}

func TestWithDisableServeCommand(t *testing.T) {
	r := NewAppRunner(nil)
	WithDisableServeCommand()(r)
	if !r.disableServeCommand {
		t.Error("expected disableServeCommand to be true")
	}
}

func TestWithCLIName(t *testing.T) {
	r := NewAppRunner(nil)
	WithCLIName("my-app")(r)
	if r.name != "my-app" {
		t.Errorf("expected name 'my-app', got '%s'", r.name)
	}
}

func TestWithCLIVersion(t *testing.T) {
	r := NewAppRunner(nil)
	WithCLIVersion("2.0.0")(r)
	if r.version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", r.version)
	}
}

func TestWithCLIDescription(t *testing.T) {
	r := NewAppRunner(nil)
	WithCLIDescription("My cool app")(r)
	if r.description != "My cool app" {
		t.Errorf("expected description 'My cool app', got '%s'", r.description)
	}
}

func TestWithGlobalFlags(t *testing.T) {
	r := NewAppRunner(nil)
	WithGlobalFlags(
		NewStringFlag("port", "p", "port", "8080"),
		NewStringFlag("env", "e", "env", "dev"),
	)(r)
	if len(r.globalFlags) != 2 {
		t.Fatalf("expected 2 global flags, got %d", len(r.globalFlags))
	}
	if r.globalFlags[0].Name() != "port" {
		t.Errorf("expected first flag 'port', got '%s'", r.globalFlags[0].Name())
	}
}

// --- Test AppRunner builder methods ---

func TestNewAppRunner_Methods(t *testing.T) {
	r := NewAppRunner(nil).
		Name("test").
		Version("2.0.0").
		Description("desc").
		AutoMigrate().
		DisableMigrationCommands().
		DisableServeCommand().
		WithGlobalFlags(NewStringFlag("x", "", "x", "")).
		WithExtraCommands(NewCommand("cmd", "cmd", nil))

	if r.name != "test" {
		t.Errorf("expected name 'test', got '%s'", r.name)
	}
	if r.version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", r.version)
	}
	if r.description != "desc" {
		t.Errorf("expected description 'desc', got '%s'", r.description)
	}
	if !r.autoMigrateOnServe {
		t.Error("expected autoMigrateOnServe")
	}
	if !r.disableMigrationCommands {
		t.Error("expected disableMigrationCommands")
	}
	if !r.disableServeCommand {
		t.Error("expected disableServeCommand")
	}
	if len(r.globalFlags) != 1 {
		t.Errorf("expected 1 global flag, got %d", len(r.globalFlags))
	}
	if len(r.extraCommands) != 1 {
		t.Errorf("expected 1 extra command, got %d", len(r.extraCommands))
	}
}

// --- Test collectMigratableExtensions ---

func TestCollectMigratableExtensions_NoMigratable(t *testing.T) {
	app := &mockApp{
		extensions: []forge.Extension{
			&plainExt{name: "ext1"},
			&plainExt{name: "ext2"},
		},
	}

	result := collectMigratableExtensions(app)
	if len(result) != 0 {
		t.Errorf("expected 0 migratable extensions, got %d", len(result))
	}
}

func TestCollectMigratableExtensions_MixedExtensions(t *testing.T) {
	app := &mockApp{
		extensions: []forge.Extension{
			&plainExt{name: "plain"},
			&migratableExt{name: "grove"},
			&plainExt{name: "another-plain"},
			&migratableExt{name: "custom-db"},
		},
	}

	result := collectMigratableExtensions(app)
	if len(result) != 2 {
		t.Fatalf("expected 2 migratable extensions, got %d", len(result))
	}

	// Verify we got the right ones
	ext0 := result[0].(forge.Extension)
	ext1 := result[1].(forge.Extension)
	if ext0.Name() != "grove" {
		t.Errorf("expected first migratable to be 'grove', got '%s'", ext0.Name())
	}
	if ext1.Name() != "custom-db" {
		t.Errorf("expected second migratable to be 'custom-db', got '%s'", ext1.Name())
	}
}

func TestCollectMigratableExtensions_Empty(t *testing.T) {
	app := &mockApp{extensions: nil}
	result := collectMigratableExtensions(app)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

// --- Test buildServeCommand ---

func TestBuildServeCommand_Basic(t *testing.T) {
	cmd := buildServeCommand(false)

	if cmd.Name() != "serve" {
		t.Errorf("expected command name 'serve', got '%s'", cmd.Name())
	}

	aliases := cmd.Aliases()
	if len(aliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(aliases))
	}
	hasStart := false
	hasRun := false
	for _, a := range aliases {
		if a == "start" {
			hasStart = true
		}
		if a == "run" {
			hasRun = true
		}
	}
	if !hasStart || !hasRun {
		t.Errorf("expected aliases 'start' and 'run', got %v", aliases)
	}
}

// --- Test buildMigrateCommand ---

func TestBuildMigrateCommand_Subcommands(t *testing.T) {
	cmd := buildMigrateCommand()

	if cmd.Name() != "migrate" {
		t.Errorf("expected command name 'migrate', got '%s'", cmd.Name())
	}

	subs := cmd.Subcommands()
	if len(subs) != 3 {
		t.Fatalf("expected 3 subcommands, got %d", len(subs))
	}

	names := map[string]bool{}
	for _, sub := range subs {
		names[sub.Name()] = true
	}

	for _, expected := range []string{"up", "down", "status"} {
		if !names[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestBuildMigrateCommand_DownHasForceFlag(t *testing.T) {
	cmd := buildMigrateCommand()

	down, ok := cmd.FindSubcommand("down")
	if !ok {
		t.Fatal("expected to find 'down' subcommand")
	}

	flags := down.Flags()
	found := false
	for _, f := range flags {
		if f.Name() == "force" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'force' flag on 'down' command")
	}
}

// --- Test registerExtensionCommands ---

func TestRegisterExtensionCommands(t *testing.T) {
	cmd1 := NewCommand("seed", "Seed data", nil)
	cmd2 := NewCommand("dump", "Dump schema", nil)

	app := &mockApp{
		extensions: []forge.Extension{
			&commandProviderExt{
				name:     "db-ext",
				commands: []any{cmd1, cmd2},
			},
			&plainExt{name: "no-commands"},
		},
		logger: forge.NewNoopLogger(),
	}

	c := New(Config{Name: "test", App: app})
	registerExtensionCommands(c, app)

	cmds := c.Commands()
	names := map[string]bool{}
	for _, cmd := range cmds {
		names[cmd.Name()] = true
	}

	if !names["seed"] {
		t.Error("expected 'seed' command from extension")
	}
	if !names["dump"] {
		t.Error("expected 'dump' command from extension")
	}
}

func TestRegisterExtensionCommands_InvalidCommand(t *testing.T) {
	// Extension returns a non-Command value — should log warning, not panic.
	app := &mockApp{
		extensions: []forge.Extension{
			&commandProviderExt{
				name:     "bad-ext",
				commands: []any{"not-a-command"},
			},
		},
		logger: forge.NewNoopLogger(),
	}

	c := New(Config{Name: "test", App: app})
	// Should not panic
	registerExtensionCommands(c, app)
}

// --- Test lazy app resolution ---

func TestLazyAppResolution(t *testing.T) {
	setupCalled := false
	testApp := &mockApp{name: "lazy-app"}

	c := New(Config{
		Name: "test",
		AppProvider: func(ctx CommandContext) (forge.App, error) {
			setupCalled = true
			return testApp, nil
		},
	})

	var capturedApp forge.App
	_ = c.AddCommand(NewCommand("check", "test", func(ctx CommandContext) error {
		capturedApp = ctx.App()
		return nil
	}))

	err := c.Run([]string{"test", "check"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !setupCalled {
		t.Error("expected setup to be called")
	}
	if capturedApp == nil {
		t.Error("expected app to be set on context")
	}
	if capturedApp.Name() != "lazy-app" {
		t.Errorf("expected app name 'lazy-app', got '%s'", capturedApp.Name())
	}
}

func TestGlobalFlagInjection(t *testing.T) {
	testApp := &mockApp{name: "flag-test"}

	c := New(Config{
		Name: "test",
		AppProvider: func(ctx CommandContext) (forge.App, error) {
			return testApp, nil
		},
	})

	cImpl := c.(*cli)
	cImpl.globalFlags = append(cImpl.globalFlags,
		NewStringFlag("port", "p", "port", "8080"),
	)

	var capturedPort string
	_ = c.AddCommand(NewCommand("check", "test", func(ctx CommandContext) error {
		capturedPort = ctx.String("port")
		return nil
	}))

	err := c.Run([]string{"test", "check", "--port", "9000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPort != "9000" {
		t.Errorf("expected port '9000', got '%s'", capturedPort)
	}
}

func TestGlobalFlagDefault(t *testing.T) {
	testApp := &mockApp{name: "flag-test"}

	c := New(Config{
		Name: "test",
		AppProvider: func(ctx CommandContext) (forge.App, error) {
			return testApp, nil
		},
	})

	cImpl := c.(*cli)
	cImpl.globalFlags = append(cImpl.globalFlags,
		NewStringFlag("port", "p", "port", "8080"),
	)

	var capturedPort string
	_ = c.AddCommand(NewCommand("check", "test", func(ctx CommandContext) error {
		capturedPort = ctx.String("port")
		return nil
	}))

	err := c.Run([]string{"test", "check"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPort != "8080" {
		t.Errorf("expected port default '8080', got '%s'", capturedPort)
	}
}

// --- Mock types ---

type mockApp struct {
	name       string
	version    string
	extensions []forge.Extension
	logger     forge.Logger
	lm         forge.LifecycleManager
	startErr   error
	stopErr    error
	runErr     error
}

func (a *mockApp) Name() string {
	if a.name == "" {
		return "mock-app"
	}
	return a.name
}
func (a *mockApp) Version() string {
	if a.version == "" {
		return "1.0.0"
	}
	return a.version
}
func (a *mockApp) Environment() string         { return "test" }
func (a *mockApp) Description() string         { return "" }
func (a *mockApp) StartTime() time.Time        { return time.Time{} }
func (a *mockApp) Uptime() time.Duration       { return 0 }
func (a *mockApp) Container() forge.Container  { return nil }
func (a *mockApp) Router() forge.Router        { return nil }
func (a *mockApp) Config() forge.ConfigManager { return nil }
func (a *mockApp) Logger() forge.Logger {
	if a.logger != nil {
		return a.logger
	}
	return forge.NewNoopLogger()
}
func (a *mockApp) Metrics() forge.Metrics             { return nil }
func (a *mockApp) HealthManager() forge.HealthManager { return nil }
func (a *mockApp) LifecycleManager() forge.LifecycleManager {
	if a.lm != nil {
		return a.lm
	}
	return forge.NewLifecycleManager(nil)
}
func (a *mockApp) Start(_ context.Context) error { return a.startErr }
func (a *mockApp) Stop(_ context.Context) error  { return a.stopErr }
func (a *mockApp) Run() error                    { return a.runErr }
func (a *mockApp) RegisterService(_ string, _ forge.Factory, _ ...forge.RegisterOption) error {
	return nil
}
func (a *mockApp) RegisterController(_ forge.Controller) error { return nil }
func (a *mockApp) RegisterExtension(_ forge.Extension) error   { return nil }
func (a *mockApp) RegisterHook(phase forge.LifecyclePhase, hook forge.LifecycleHook, opts forge.LifecycleHookOptions) error {
	if a.lm != nil {
		return a.lm.RegisterHook(phase, hook, opts)
	}
	return nil
}
func (a *mockApp) RegisterHookFn(phase forge.LifecyclePhase, name string, hook forge.LifecycleHook) error {
	if a.lm != nil {
		return a.lm.RegisterHookFn(phase, name, hook)
	}
	return nil
}
func (a *mockApp) Extensions() []forge.Extension {
	return a.extensions
}
func (a *mockApp) GetExtension(name string) (forge.Extension, error) {
	for _, ext := range a.extensions {
		if ext.Name() == name {
			return ext, nil
		}
	}
	return nil, nil
}
func (a *mockApp) MigrationsDisabled() bool { return false }

// plainExt is a minimal extension that does NOT implement MigratableExtension.
type plainExt struct {
	name string
}

func (e *plainExt) Name() string                   { return e.name }
func (e *plainExt) Version() string                { return "1.0.0" }
func (e *plainExt) Description() string            { return "" }
func (e *plainExt) Register(_ forge.App) error     { return nil }
func (e *plainExt) Start(_ context.Context) error  { return nil }
func (e *plainExt) Stop(_ context.Context) error   { return nil }
func (e *plainExt) Health(_ context.Context) error { return nil }
func (e *plainExt) Dependencies() []string         { return nil }

// migratableExt implements both Extension and MigratableExtension.
type migratableExt struct {
	name string
}

func (e *migratableExt) Name() string                   { return e.name }
func (e *migratableExt) Version() string                { return "1.0.0" }
func (e *migratableExt) Description() string            { return "" }
func (e *migratableExt) Register(_ forge.App) error     { return nil }
func (e *migratableExt) Start(_ context.Context) error  { return nil }
func (e *migratableExt) Stop(_ context.Context) error   { return nil }
func (e *migratableExt) Health(_ context.Context) error { return nil }
func (e *migratableExt) Dependencies() []string         { return nil }
func (e *migratableExt) Migrate(_ context.Context) (*forge.MigrationResult, error) {
	return &forge.MigrationResult{}, nil
}
func (e *migratableExt) Rollback(_ context.Context) (*forge.MigrationResult, error) {
	return &forge.MigrationResult{}, nil
}
func (e *migratableExt) MigrationStatus(_ context.Context) ([]*forge.MigrationGroupInfo, error) {
	return nil, nil
}

// commandProviderExt implements Extension and CLICommandProvider.
type commandProviderExt struct {
	name     string
	commands []any
}

func (e *commandProviderExt) Name() string                   { return e.name }
func (e *commandProviderExt) Version() string                { return "1.0.0" }
func (e *commandProviderExt) Description() string            { return "" }
func (e *commandProviderExt) Register(_ forge.App) error     { return nil }
func (e *commandProviderExt) Start(_ context.Context) error  { return nil }
func (e *commandProviderExt) Stop(_ context.Context) error   { return nil }
func (e *commandProviderExt) Health(_ context.Context) error { return nil }
func (e *commandProviderExt) Dependencies() []string         { return nil }
func (e *commandProviderExt) CLICommands() []any             { return e.commands }
