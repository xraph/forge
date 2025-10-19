package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/pkg/cli/output"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/config"
	"github.com/xraph/forge/pkg/config/sources"
	"github.com/xraph/forge/pkg/di"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
)

// CLIApp represents the main CLI application with Forge integration
type CLIApp interface {

	// AddCommand adds a new command to the CLI application. It returns an error if the command cannot be added.
	AddCommand(cmd *Command) error

	// AddPlugin integrates a CLIPlugin into the application, registering its commands, middleware, and lifecycle hooks.
	AddPlugin(plugin CLIPlugin) error

	// UseMiddleware adds the specified CLIMiddleware to the application pipeline for execution during command processing.
	UseMiddleware(middleware CLIMiddleware) error

	// RegisterService registers a service instance with the dependency injection container for use across the CLI application.
	RegisterService(service interface{}) error

	// Execute runs the CLI application, processing commands and executing the appropriate logic defined within the application.
	Execute() error

	// ExecuteWithArgs runs the CLI application using the specified command-line arguments and returns any execution error.
	ExecuteWithArgs(args []string) error

	// SetConfig applies the given CLIConfig to configure the application and initializes its settings.
	SetConfig(config *CLIConfig) error

	// GetConfig retrieves the current CLI configuration for the application and returns it as a *CLIConfig instance.
	GetConfig() *CLIConfig

	// SetOutput sets the output writer for the CLI application, allowing customization of where output is directed.
	SetOutput(output io.Writer)

	// SetInput sets the input stream for the CLI application, typically used for reading user input or test automation.
	SetInput(input io.Reader)

	// EnableCompletion enables shell completion for the CLI application by registering necessary handlers and configurations.
	EnableCompletion() error

	// GenerateCompletion generates shell-specific autocompletion scripts for the CLI commands. Returns an error if generation fails.
	GenerateCompletion(shell string) error

	// Container provides access to the dependency injection container used for service registration and resolution.
	Container() common.Container

	// Logger returns the application logger instance for structured logging.
	Logger() common.Logger

	// Metrics returns the metrics interface for collecting and managing application metrics.
	Metrics() common.Metrics

	// Config returns the configuration manager for managing application settings and configurations.
	Config() common.ConfigManager
}

// cliApp implements CLIApp with full Forge integration
type cliApp struct {
	*cobra.Command
	name    string
	version string

	// Forge integration
	container common.Container
	logger    common.Logger
	metrics   common.Metrics
	config    common.ConfigManager

	// CLI-specific
	middleware []CLIMiddleware
	plugins    []CLIPlugin
	commands   map[string]*commandWrapper

	// Execution hooks
	beforeRun []func(CLIContext) error
	afterRun  []func(CLIContext) error

	// Configuration
	cliConfig *CLIConfig

	// Output and interaction
	output io.Writer
	input  io.Reader

	// State
	started bool
	mutex   sync.RWMutex
}

// NewCLIApp creates a new CLI application with Forge integration
func NewCLIApp(name, description string) CLIApp {
	rootCmd := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  description,
	}

	app := &cliApp{
		name:      "Forge",
		version:   "0.0.0",
		Command:   rootCmd,
		commands:  make(map[string]*commandWrapper),
		beforeRun: make([]func(CLIContext) error, 0),
		afterRun:  make([]func(CLIContext) error, 0),
		cliConfig: &CLIConfig{
			Name:        name,
			Description: description,
		},
		output: os.Stdout,
		input:  os.Stdin,
	}

	// Initialize default Forge components
	app.initializeForgeComponents()

	// Set up root command execution
	app.setupRootCommand()

	return app
}

// NewCLIAppWithContainer creates a CLI app with existing Forge container
func NewCLIAppWithContainer(name, description string, container common.Container) CLIApp {
	app := NewCLIApp(name, description).(*cliApp)
	app.container = container

	// Resolve Forge components from container
	if logger, err := container.Resolve((*common.Logger)(nil)); err == nil {
		app.logger = logger.(common.Logger)
	}
	if metrics, err := container.Resolve((*common.Metrics)(nil)); err == nil {
		app.metrics = metrics.(common.Metrics)
	}
	if config, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		app.config = config.(common.ConfigManager)
	}

	return app
}

// initializeForgeComponents initializes default Forge components
func (app *cliApp) initializeForgeComponents() {
	// Create default implementations if not provided
	if app.config == nil {
		app.config = app.createDefaultConfig()
	}
	if app.logger == nil {
		app.logger = app.createDefaultLogger()
	}
	if app.container == nil {
		app.container = app.createDefaultContainer()
	}
	if app.metrics == nil {
		app.metrics = app.createDefaultMetrics()
	}
}

// createDefaultContainer creates a default DI container
func (app *cliApp) createDefaultContainer() common.Container {
	return di.NewContainer(di.ContainerConfig{
		Logger:                    app.logger,
		Metrics:                   app.metrics,
		Config:                    app.config,
		CollisionHandlingStrategy: di.CollisionStrategyFail,
	})
}

// createDefaultLogger creates a default logger
func (app *cliApp) createDefaultLogger() common.Logger {
	env := app.config.GetString("environment", "development")
	if env == "development" {
		return output.NewDevelopmentLogger(app.config.Name())
	} else if env == "cli" {
		return output.NewCLILogger(app.config.Name())
	}
	return output.NewDefaultConsoleLogger()
}

// createDefaultMetrics creates a default metrics collector
func (app *cliApp) createDefaultMetrics() common.Metrics {
	collector := metrics.NewCollector(metrics.DefaultCollectorConfig(), app.logger)
	return collector
}

// createDefaultConfig creates a default config manager
func (app *cliApp) createDefaultConfig() common.ConfigManager {
	cfm := config.NewManager(config.ManagerConfig{
		Logger:          output.NewSilentCLILogger("Forge"),
		Metrics:         app.metrics,
		ValidationMode:  config.ValidationModePermissive,
		WatchInterval:   30 * time.Second,
		ReloadOnChange:  true,
		MetricsEnabled:  true,
		ErrorRetryCount: 3,
		ErrorRetryDelay: 5 * time.Second,
	})

	forgeSource, err := sources.NewFileSource("./.forge.yaml", sources.FileSourceOptions{
		Logger:       app.logger,
		RequireFile:  false,
		Priority:     1,
		WatchEnabled: true,
	})
	if err != nil {
		panic(fmt.Errorf("failed to create forge.yaml source: %w", err))
	}

	err = cfm.LoadFrom(forgeSource)
	if err != nil {
		panic(fmt.Errorf("failed to load forge.yaml: %w", err))
	}

	return cfm
}

func (app *cliApp) showStartupBanner() {
	showBanner := app.config.GetBool("enableBanner", true)
	env := app.config.GetString("environment", "development")

	if showBanner {
		switch env {
		case "development":
			config := output.DeveloperBannerConfig(app.name, app.version)
			config.Environment = env
			banner := output.NewBanner(config)
			banner.Render()
		case "production":
			output.DisplayProductionBanner(app.name, app.version, "production", map[string]string{})
		default:
			output.DisplayCLIStartupBanner(app.name, app.version, map[string]string{})
		}
	}
}

// setupRootCommand configures the root command with middleware execution
func (app *cliApp) setupRootCommand() {
	originalRunE := app.Command.RunE

	app.Command.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := app.createContext(cmd, args)

		// Execute middleware chain
		chain := &middlewareChain{
			middleware: app.middleware,
			handler: func() error {
				if originalRunE != nil {
					return originalRunE(cmd, args)
				}

				// Only show help if we have subcommands and this isn't a cancellation
				if len(cmd.Commands()) > 0 {
					return cmd.Help()
				}

				// For commands without RunE, just return nil (don't show help)
				return nil
			},
		}

		return chain.Execute(ctx)
	}
}

// AddCommand adds a command to the CLI application
func (app *cliApp) AddCommand(cmd *Command) error {
	app.mutex.Lock()
	defer app.mutex.Unlock()

	if app.started {
		return common.ErrLifecycleError("add_command", common.NewForgeError("CLI_STARTED", "cannot add command after CLI has started", nil))
	}

	// Create command wrapper
	wrapper := &commandWrapper{
		Command:    cmd,
		app:        app,
		middleware: make([]CLIMiddleware, 0),
	}

	// Add command-specific middleware
	wrapper.middleware = append(wrapper.middleware, cmd.Middleware...)

	// Convert to Cobra command
	cobraCmd, err := wrapper.toCobraCommand()
	if err != nil {
		return common.ErrInvalidConfig("command", err)
	}

	wrapper.cobraCommand = cobraCmd
	app.commands[cmd.Use] = wrapper
	app.Command.AddCommand(cobraCmd)

	if app.logger != nil {
		app.logger.Debug("command added",
			logger.String("command", cmd.Use),
			logger.String("description", cmd.Short),
			logger.Int("middleware_count", len(cmd.Middleware)),
		)
	}

	return nil
}

// AddPlugin adds a plugin to the CLI application
func (app *cliApp) AddPlugin(plugin CLIPlugin) error {
	// Check if started without holding lock for too long
	app.mutex.RLock()
	started := app.started
	app.mutex.RUnlock()

	if started {
		return common.ErrLifecycleError("add_plugin", common.NewForgeError("CLI_STARTED", "cannot add plugin after CLI has started", nil))
	}

	// Initialize plugin BEFORE acquiring write lock
	// This prevents deadlock if plugin.Initialize calls back into app
	if err := plugin.Initialize(app); err != nil {
		return common.ErrPluginInitFailed(plugin.Name(), err)
	}

	// Now safely add to collections with write lock
	app.mutex.Lock()
	defer app.mutex.Unlock()

	// Double-check started flag after acquiring write lock
	if app.started {
		return common.ErrLifecycleError("add_plugin", common.NewForgeError("CLI_STARTED", "cannot add plugin after CLI has started", nil))
	}

	// Add plugin middleware
	for _, middleware := range plugin.Middleware() {
		app.middleware = append(app.middleware, middleware)
	}

	// Add plugin commands
	for _, cmd := range plugin.Commands() {
		if err := app.addCommandInternal(cmd); err != nil {
			return err
		}
	}

	app.plugins = append(app.plugins, plugin)

	if app.logger != nil {
		// app.logger.Info("plugin added",
		// 	logger.String("plugin", plugin.Name()),
		// 	logger.String("version", plugin.Version()),
		// 	logger.Int("commands", len(plugin.Commands())),
		// 	logger.Int("middleware", len(plugin.Middleware())),
		// )
	}

	return nil
}

// addCommandInternal adds a command without additional locking (internal use)
func (app *cliApp) addCommandInternal(cmd *Command) error {
	// This assumes the caller already holds the necessary locks
	wrapper := &commandWrapper{
		Command:    cmd,
		app:        app,
		middleware: make([]CLIMiddleware, 0),
	}

	wrapper.middleware = append(wrapper.middleware, cmd.Middleware...)

	cobraCmd, err := wrapper.toCobraCommand()
	if err != nil {
		return common.ErrInvalidConfig("command", err)
	}

	wrapper.cobraCommand = cobraCmd
	app.commands[cmd.Use] = wrapper
	app.Command.AddCommand(cobraCmd)

	return nil
}

// UseMiddleware adds middleware to the CLI application
func (app *cliApp) UseMiddleware(middleware CLIMiddleware) error {
	app.mutex.Lock()
	defer app.mutex.Unlock()

	if app.started {
		return common.ErrLifecycleError("use_middleware", common.NewForgeError("CLI_STARTED", "cannot add middleware after CLI has started", nil))
	}

	app.middleware = append(app.middleware, middleware)

	// Sort middleware by priority
	sort.Slice(app.middleware, func(i, j int) bool {
		return app.middleware[i].Priority() < app.middleware[j].Priority()
	})

	if app.logger != nil {
		app.logger.Debug("middleware added",
			logger.String("middleware", middleware.Name()),
			logger.Int("priority", middleware.Priority()),
		)
	}

	return nil
}

// RegisterService registers a service with the DI container
func (app *cliApp) RegisterService(service interface{}) error {
	if app.container == nil {
		return common.ErrContainerError("register_service", common.NewForgeError("NO_CONTAINER", "no DI container available", nil))
	}

	serviceType := reflect.TypeOf(service)
	if serviceType.Kind() == reflect.Ptr {
		serviceType = serviceType.Elem()
	}

	definition := common.ServiceDefinition{
		Name:        serviceType.Name(),
		Type:        service,
		Constructor: func() interface{} { return service },
		Singleton:   true,
	}

	return app.container.Register(definition)
}

// Execute executes the CLI application
func (app *cliApp) Execute() error {
	app.mutex.Lock()
	app.started = true
	app.mutex.Unlock()

	// Start container if it's a service
	if service, ok := app.container.(common.Service); ok {
		if err := service.Start(context.Background()); err != nil {
			return err
		}
		defer service.Stop(context.Background())
	}

	start := time.Now()
	err := app.Command.Execute()
	duration := time.Since(start)

	// Record metrics
	if app.metrics != nil {
		app.metrics.Counter("cli.commands.executed").Inc()
		app.metrics.Timer("cli.commands.duration").Record(duration)
		if err != nil {
			app.metrics.Counter("cli.commands.errors").Inc()
		}
	}

	return err
}

// ExecuteWithArgs executes with specific arguments
func (app *cliApp) ExecuteWithArgs(args []string) error {
	app.Command.SetArgs(args)
	return app.Execute()
}

// SetConfig sets the CLI configuration
func (app *cliApp) SetConfig(config *CLIConfig) error {
	app.mutex.Lock()
	defer app.mutex.Unlock()

	app.cliConfig = config

	// Apply configuration
	if config.Name != "" {
		app.Command.Use = config.Name
	}
	if config.Description != "" {
		app.Command.Short = config.Description
		app.Command.Long = config.Description
	}
	if config.Version != "" {
		app.Command.Version = config.Version
	}

	return nil
}

// GetConfig returns the CLI configuration
func (app *cliApp) GetConfig() *CLIConfig {
	app.mutex.RLock()
	defer app.mutex.RUnlock()
	return app.cliConfig
}

// SetOutput sets the output writer
func (app *cliApp) SetOutput(output io.Writer) {
	app.output = output
	app.Command.SetOut(output)
}

// SetInput sets the input reader
func (app *cliApp) SetInput(input io.Reader) {
	app.input = input
	app.Command.SetIn(input)
}

// EnableCompletion enables shell completion
func (app *cliApp) EnableCompletion() error {
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate completion script",
		Long: `To load completions:

Bash:
  $ source <(yourprogram completion bash)

Zsh:
  $ source <(yourprogram completion zsh)

Fish:
  $ yourprogram completion fish | source

PowerShell:
  PS> yourprogram completion powershell | Out-String | Invoke-Expression
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactValidArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "bash":
				cmd.Root().GenBashCompletion(app.output)
			case "zsh":
				cmd.Root().GenZshCompletion(app.output)
			case "fish":
				cmd.Root().GenFishCompletion(app.output, true)
			case "powershell":
				cmd.Root().GenPowerShellCompletionWithDesc(app.output)
			}
		},
	}

	app.Command.AddCommand(completionCmd)
	return nil
}

// GenerateCompletion generates completion for specific shell
func (app *cliApp) GenerateCompletion(shell string) error {
	switch shell {
	case "bash":
		return app.Command.GenBashCompletion(app.output)
	case "zsh":
		return app.Command.GenZshCompletion(app.output)
	case "fish":
		return app.Command.GenFishCompletion(app.output, true)
	case "powershell":
		return app.Command.GenPowerShellCompletionWithDesc(app.output)
	default:
		return common.ErrInvalidConfig("shell", common.NewForgeError("INVALID_SHELL", "unsupported shell: "+shell, nil))
	}
}

// Container returns the DI container
func (app *cliApp) Container() common.Container {
	return app.container
}

// Logger returns the logger
func (app *cliApp) Logger() common.Logger {
	return app.logger
}

// Metrics returns the metrics collector
func (app *cliApp) Metrics() common.Metrics {
	return app.metrics
}

// Config returns the configuration manager
func (app *cliApp) Config() common.ConfigManager {
	return app.config
}

// createContext creates a new CLI context
func (app *cliApp) createContext(cmd *cobra.Command, args []string) CLIContext {
	return &cliContext{
		cobra:     cmd,
		container: app.container,
		logger:    app.logger,
		metrics:   app.metrics,
		config:    app.config,
		app:       app,
		values:    make(map[string]interface{}),
		args:      args,
	}
}
