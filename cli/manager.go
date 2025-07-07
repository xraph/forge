package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/plugins"
)

// Manager represents the CLI command manager
type Manager struct {
	rootCmd     *cobra.Command
	app         interface{} // Application interface
	config      *Config
	logger      logger.Logger
	container   core.Container
	plugins     plugins.Manager
	commands    map[string]*cobra.Command
	initialized bool
}

// NewManager creates a new CLI manager
func NewManager(app interface{}, config *Config) (*Manager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Create logger
	logConfig := logger.LoggingConfig{
		Level:  config.LogLevel,
		Format: "console",
		Output: "stderr",
	}
	if config.LogLevel == "" {
		logConfig.Level = "info"
	}
	cliLogger := logger.NewLogger(logConfig).Named("cli")

	// Create root command
	rootCmd := &cobra.Command{
		Use:     config.AppName,
		Short:   config.Description,
		Version: config.Version,
		Long: fmt.Sprintf(`%s

%s is a Forge-based application with a comprehensive CLI interface.
Configure via environment variables, config files, or command line flags.

Find more information at: https://github.com/xraph/forge`, config.Description, config.AppName),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initializeConfig(config.ConfigFile)
		},
	}

	// Add global flags
	rootCmd.PersistentFlags().StringVar(&config.ConfigFile, "config", "", "config file (default is $HOME/.forge.yaml)")
	rootCmd.PersistentFlags().StringVar(&config.LogLevel, "log-level", "info", "log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&config.EnableColors, "colors", true, "enable colored output")
	rootCmd.PersistentFlags().BoolVar(&config.EnableTiming, "timing", false, "show command execution time")
	rootCmd.PersistentFlags().DurationVar(&config.Timeout, "timeout", 30*time.Second, "command timeout")

	// Set version template
	rootCmd.SetVersionTemplate(fmt.Sprintf(`%s version %s
Built with Forge Framework
`, config.AppName, config.Version))

	manager := &Manager{
		rootCmd:   rootCmd,
		app:       app,
		config:    config,
		logger:    cliLogger,
		commands:  make(map[string]*cobra.Command),
		container: extractContainer(app),
	}

	// Extract plugins manager if available
	if pluginsManager := extractPluginsManager(app); pluginsManager != nil {
		manager.plugins = pluginsManager
	}

	return manager, nil
}

// Initialize initializes the CLI manager
func (m *Manager) Initialize() error {
	if m.initialized {
		return nil
	}

	m.logger.Debug("Initializing CLI manager")

	// Add built-in commands
	if m.config.EnableBuiltins {
		if err := m.addBuiltinCommands(); err != nil {
			return fmt.Errorf("failed to add builtin commands: %w", err)
		}
	}

	// Add plugin commands
	if m.config.EnablePlugins && m.plugins != nil {
		if err := m.addPluginCommands(); err != nil {
			return fmt.Errorf("failed to add plugin commands: %w", err)
		}
	}

	// Set default command if specified
	if m.config.DefaultCommand != "" {
		m.setDefaultCommand(m.config.DefaultCommand)
	}

	m.initialized = true
	m.logger.Debug("CLI manager initialized successfully")
	return nil
}

// Execute executes the CLI
func (m *Manager) Execute() error {
	if !m.initialized {
		if err := m.Initialize(); err != nil {
			return fmt.Errorf("failed to initialize CLI: %w", err)
		}
	}

	// Setup error handling
	m.rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		m.logger.Error("Flag error", logger.Error(err))
		cmd.PrintErrln(err)
		cmd.PrintErrln(cmd.UsageString())
		return err
	})

	// Setup execution timing
	if m.config.EnableTiming {
		m.setupTiming()
	}

	// Execute command
	return m.rootCmd.Execute()
}

// ExecuteWithContext executes the CLI with context
func (m *Manager) ExecuteWithContext(ctx context.Context) error {
	return m.rootCmd.ExecuteContext(ctx)
}

// AddCommand adds a custom command
func (m *Manager) AddCommand(cmd *cobra.Command) {
	m.rootCmd.AddCommand(cmd)
	m.commands[cmd.Name()] = cmd
	m.logger.Debug("Added custom command", logger.String("name", cmd.Name()))
}

// AddCommands adds multiple commands
func (m *Manager) AddCommands(commands ...*cobra.Command) {
	for _, cmd := range commands {
		m.AddCommand(cmd)
	}
}

// RemoveCommand removes a command
func (m *Manager) RemoveCommand(name string) {
	if cmd, exists := m.commands[name]; exists {
		m.rootCmd.RemoveCommand(cmd)
		delete(m.commands, name)
		m.logger.Debug("Removed command", logger.String("name", name))
	}
}

// GetCommand gets a command by name
func (m *Manager) GetCommand(name string) *cobra.Command {
	return m.commands[name]
}

// GetRootCommand returns the root command
func (m *Manager) GetRootCommand() *cobra.Command {
	return m.rootCmd
}

// ListCommands returns all registered commands
func (m *Manager) ListCommands() map[string]*cobra.Command {
	result := make(map[string]*cobra.Command)
	for name, cmd := range m.commands {
		result[name] = cmd
	}
	return result
}

// Private helper methods

// addBuiltinCommands adds built-in commands
func (m *Manager) addBuiltinCommands() error {
	m.logger.Debug("Adding built-in commands")

	// Create commands factory
	factory := NewCommandsFactory(m.app, m.container, m.logger)

	// Add server command
	serverCmd := factory.CreateServerCommand()
	m.AddCommand(serverCmd)

	// Add database commands
	dbCmd := factory.CreateDatabaseCommand()
	m.AddCommand(dbCmd)

	// Add migration commands
	migrateCmd := factory.CreateMigrateCommand()
	m.AddCommand(migrateCmd)

	// Add health check command
	healthCmd := factory.CreateHealthCommand()
	m.AddCommand(healthCmd)

	// Add config commands
	configCmd := factory.CreateConfigCommand()
	m.AddCommand(configCmd)

	// Add jobs commands
	jobsCmd := factory.CreateJobsCommand()
	m.AddCommand(jobsCmd)

	// Add plugin commands
	if m.plugins != nil {
		pluginCmd := factory.CreatePluginCommand(m.plugins)
		m.AddCommand(pluginCmd)
	}

	// Add version command (enhanced)
	versionCmd := factory.CreateVersionCommand()
	m.AddCommand(versionCmd)

	// Add completion command
	completionCmd := factory.CreateCompletionCommand(m.rootCmd)
	m.AddCommand(completionCmd)

	m.logger.Debug("Built-in commands added successfully")
	return nil
}

// addPluginCommands adds plugin commands
func (m *Manager) addPluginCommands() error {
	if m.plugins == nil {
		return nil
	}

	m.logger.Debug("Adding plugin commands")

	plugins := m.plugins.List()
	for _, plugin := range plugins {
		if !plugin.IsEnabled() {
			continue
		}

		// Get plugin commands
		commands := plugin.Commands()
		for _, cmdDef := range commands {
			cobraCmd := m.convertPluginCommand(cmdDef, plugin)
			m.AddCommand(cobraCmd)
		}
	}

	m.logger.Debug("Plugin commands added successfully")
	return nil
}

// convertPluginCommand converts a plugin command definition to cobra command
func (m *Manager) convertPluginCommand(cmdDef plugins.CommandDefinition, plugin plugins.Plugin) *cobra.Command {
	cmd := &cobra.Command{
		Use:    cmdDef.Name,
		Short:  cmdDef.Description,
		Long:   cmdDef.Usage,
		Hidden: cmdDef.Hidden,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			ctx := context.Background()
			if m.config.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, m.config.Timeout)
				defer cancel()
			}

			// Execute plugin command
			if err := cmdDef.Handler.Execute(ctx, args); err != nil {
				m.logger.Error("Plugin command failed",
					logger.String("plugin", plugin.Name()),
					logger.String("command", cmdDef.Name),
					logger.Error(err),
				)
				return err
			}

			return nil
		},
	}

	// Add aliases
	cmd.Aliases = cmdDef.Aliases

	// Add flags
	for _, flagDef := range cmdDef.Flags {
		m.addFlag(cmd, flagDef)
	}

	// Add subcommands
	for _, subCmdDef := range cmdDef.Subcommands {
		subCmd := m.convertPluginCommand(subCmdDef, plugin)
		cmd.AddCommand(subCmd)
	}

	return cmd
}

// addFlag adds a flag to a command based on flag definition
func (m *Manager) addFlag(cmd *cobra.Command, flagDef plugins.FlagDefinition) {
	switch flagDef.Type {
	case "string":
		defaultVal := ""
		if flagDef.Default != nil {
			defaultVal = flagDef.Default.(string)
		}
		if flagDef.Short != "" {
			cmd.Flags().StringP(flagDef.Name, flagDef.Short, defaultVal, flagDef.Description)
		} else {
			cmd.Flags().String(flagDef.Name, defaultVal, flagDef.Description)
		}
		if flagDef.Required {
			cmd.MarkFlagRequired(flagDef.Name)
		}

	case "int":
		defaultVal := 0
		if flagDef.Default != nil {
			defaultVal = flagDef.Default.(int)
		}
		if flagDef.Short != "" {
			cmd.Flags().IntP(flagDef.Name, flagDef.Short, defaultVal, flagDef.Description)
		} else {
			cmd.Flags().Int(flagDef.Name, defaultVal, flagDef.Description)
		}
		if flagDef.Required {
			cmd.MarkFlagRequired(flagDef.Name)
		}

	case "bool":
		defaultVal := false
		if flagDef.Default != nil {
			defaultVal = flagDef.Default.(bool)
		}
		if flagDef.Short != "" {
			cmd.Flags().BoolP(flagDef.Name, flagDef.Short, defaultVal, flagDef.Description)
		} else {
			cmd.Flags().Bool(flagDef.Name, defaultVal, flagDef.Description)
		}

	case "duration":
		defaultVal := time.Duration(0)
		if flagDef.Default != nil {
			defaultVal = flagDef.Default.(time.Duration)
		}
		if flagDef.Short != "" {
			cmd.Flags().DurationP(flagDef.Name, flagDef.Short, defaultVal, flagDef.Description)
		} else {
			cmd.Flags().Duration(flagDef.Name, defaultVal, flagDef.Description)
		}
		if flagDef.Required {
			cmd.MarkFlagRequired(flagDef.Name)
		}
	}
}

// setDefaultCommand sets the default command to run
func (m *Manager) setDefaultCommand(defaultCmd string) {
	// Find the command
	for _, cmd := range m.rootCmd.Commands() {
		if cmd.Name() == defaultCmd {
			// Set up the root command to run the default command
			m.rootCmd.RunE = func(cobraCmd *cobra.Command, args []string) error {
				return cmd.RunE(cmd, args)
			}
			break
		}
	}
}

// setupTiming sets up command execution timing
func (m *Manager) setupTiming() {
	originalPersistentPreRun := m.rootCmd.PersistentPreRunE
	m.rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Store start time in context
		ctx := context.WithValue(cmd.Context(), "startTime", time.Now())
		cmd.SetContext(ctx)

		// Run original pre-run
		if originalPersistentPreRun != nil {
			return originalPersistentPreRun(cmd, args)
		}
		return nil
	}

	// Add timing to all commands
	for _, cmd := range m.rootCmd.Commands() {
		m.wrapCommandWithTiming(cmd)
	}
}

// wrapCommandWithTiming wraps a command with timing
func (m *Manager) wrapCommandWithTiming(cmd *cobra.Command) {
	if cmd.RunE == nil {
		return
	}

	originalRunE := cmd.RunE
	cmd.RunE = func(cobraCmd *cobra.Command, args []string) error {
		start := time.Now()
		err := originalRunE(cobraCmd, args)
		duration := time.Since(start)

		if m.config.EnableColors {
			fmt.Fprintf(os.Stderr, "\n\033[90m⏱ Command completed in %v\033[0m\n", duration)
		} else {
			fmt.Fprintf(os.Stderr, "\nCommand completed in %v\n", duration)
		}

		return err
	}
}

// Helper functions for extracting components from application

// extractContainer extracts the container from an application
func extractContainer(app interface{}) core.Container {
	if appWithContainer, ok := app.(interface{ Container() core.Container }); ok {
		return appWithContainer.Container()
	}
	return nil
}

// extractPluginsManager extracts the plugins manager from an application
func extractPluginsManager(app interface{}) plugins.Manager {
	if appWithPlugins, ok := app.(interface{ Plugins() plugins.Manager }); ok {
		return appWithPlugins.Plugins()
	}
	return nil
}

// initializeConfig initializes configuration using viper
func initializeConfig(cfgFile string) error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".forge")
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix("FORGE")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Read config file if it exists
	if err := viper.ReadInConfig(); err == nil {
		return nil
	}

	return nil
}

// Utility methods

// GetVersion returns the application version
func (m *Manager) GetVersion() string {
	return m.config.Version
}

// GetAppName returns the application name
func (m *Manager) GetAppName() string {
	return m.config.AppName
}

// SetLogger sets the logger
func (m *Manager) SetLogger(logger logger.Logger) {
	m.logger = logger.Named("cli")
}

// GetLogger returns the logger
func (m *Manager) GetLogger() logger.Logger {
	return m.logger
}

// IsInitialized returns whether the manager is initialized
func (m *Manager) IsInitialized() bool {
	return m.initialized
}
