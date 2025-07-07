package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/database"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/plugins"
)

// CommandsFactory creates built-in commands
type CommandsFactory struct {
	app       interface{}
	container core.Container
	logger    logger.Logger
}

// NewCommandsFactory creates a new commands factory
func NewCommandsFactory(app interface{}, container core.Container, logger logger.Logger) *CommandsFactory {
	return &CommandsFactory{
		app:       app,
		container: container,
		logger:    logger.Named("commands"),
	}
}

// CreateServerCommand creates the server command
func (f *CommandsFactory) CreateServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the HTTP server",
		Long: `Start the HTTP server with the configured settings.

This command starts the main application server, initializes all components,
and begins listening for HTTP requests.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.logger.Info("Starting server...")

			// Get application with Run method
			if app, ok := f.app.(interface{ Run() error }); ok {
				return app.Run()
			}

			return fmt.Errorf("application does not support server mode")
		},
	}

	// Add server-specific flags
	cmd.Flags().String("host", "", "server host (overrides config)")
	cmd.Flags().Int("port", 0, "server port (overrides config)")
	cmd.Flags().Bool("tls", false, "enable TLS")
	cmd.Flags().String("cert", "", "TLS certificate file")
	cmd.Flags().String("key", "", "TLS key file")

	return cmd
}

// CreateDatabaseCommand creates the database command
func (f *CommandsFactory) CreateDatabaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database management commands",
		Long:  `Manage database connections, schemas, and data.`,
	}

	// Add subcommands
	cmd.AddCommand(f.createDatabaseStatusCommand())
	cmd.AddCommand(f.createDatabasePingCommand())
	cmd.AddCommand(f.createDatabaseSchemaCommand())
	cmd.AddCommand(f.createDatabaseSeedCommand())

	return cmd
}

// createDatabaseStatusCommand creates the database status command
func (f *CommandsFactory) createDatabaseStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check database status",
		Long:  `Check the status of all configured databases.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.logger.Info("Checking database status...")

			// Get databases from container
			databases := f.getAllDatabases()
			if len(databases) == 0 {
				fmt.Println("No databases configured")
				return nil
			}

			fmt.Printf("Database Status:\n")
			fmt.Printf("================\n\n")

			for name, db := range databases {
				fmt.Printf("Database: %s\n", name)
				fmt.Printf("Type: %s\n", f.getDatabaseType(db))

				// Check connection
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := db.Ping(ctx); err != nil {
					fmt.Printf("Status: ❌ FAILED - %v\n", err)
				} else {
					fmt.Printf("Status: ✅ OK\n")
				}

				fmt.Printf("---\n")
			}

			return nil
		},
	}
}

// createDatabasePingCommand creates the database ping command
func (f *CommandsFactory) createDatabasePingCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ping [database-name]",
		Short: "Ping a database connection",
		Long:  `Test connectivity to a specific database or all databases.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if len(args) > 0 {
				// Ping specific database
				dbName := args[0]
				db := f.getDatabase(dbName)
				if db == nil {
					return fmt.Errorf("database '%s' not found", dbName)
				}

				fmt.Printf("Pinging database '%s'...\n", dbName)
				if err := db.Ping(ctx); err != nil {
					fmt.Printf("❌ FAILED: %v\n", err)
					return err
				}
				fmt.Printf("✅ OK\n")
			} else {
				// Ping all databases
				databases := f.getAllDatabases()
				for name, db := range databases {
					fmt.Printf("Pinging '%s'... ", name)
					if err := db.Ping(ctx); err != nil {
						fmt.Printf("❌ FAILED: %v\n", err)
					} else {
						fmt.Printf("✅ OK\n")
					}
				}
			}

			return nil
		},
	}
}

// createDatabaseSchemaCommand creates the database schema command
func (f *CommandsFactory) createDatabaseSchemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema [database-name]",
		Short: "Show database schema information",
		Long:  `Display schema information for databases.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbName := "default"
			if len(args) > 0 {
				dbName = args[0]
			}

			db := f.getSQLDatabase(dbName)
			if db == nil {
				return fmt.Errorf("SQL database '%s' not found", dbName)
			}

			// ctx := context.Background()

			// Get schema information
			fmt.Printf("Schema for database '%s':\n", dbName)
			fmt.Printf("========================\n\n")

			// This would need to be implemented based on the specific database type
			// For now, we'll show a placeholder
			fmt.Printf("Schema information not yet implemented for this database type\n")

			return nil
		},
	}
}

// createDatabaseSeedCommand creates the database seed command
func (f *CommandsFactory) createDatabaseSeedCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "seed [database-name]",
		Short: "Seed database with test data",
		Long:  `Populate database with test/sample data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbName := "default"
			if len(args) > 0 {
				dbName = args[0]
			}

			f.logger.Info("Seeding database", logger.String("database", dbName))
			fmt.Printf("Seeding database '%s'...\n", dbName)

			// This would be implemented based on application needs
			fmt.Printf("Database seeding not yet implemented\n")

			return nil
		},
	}
}

// CreateMigrateCommand creates the migrate command
func (f *CommandsFactory) CreateMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration commands",
		Long:  `Manage database migrations.`,
	}

	// Add subcommands
	cmd.AddCommand(f.createMigrateUpCommand())
	cmd.AddCommand(f.createMigrateDownCommand())
	cmd.AddCommand(f.createMigrateStatusCommand())
	cmd.AddCommand(f.createMigrateCreateCommand())

	return cmd
}

// createMigrateUpCommand creates the migrate up command
func (f *CommandsFactory) createMigrateUpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply pending migrations",
		Long:  `Apply all pending database migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.logger.Info("Running migrations...")

			// Get all SQL databases
			databases := f.getAllSQLDatabases()
			if len(databases) == 0 {
				fmt.Println("No SQL databases configured")
				return nil
			}

			ctx := context.Background()
			for name, db := range databases {
				fmt.Printf("Migrating database '%s'...\n", name)

				// This would use the actual migration system
				if migrator, ok := db.(interface{ Migrate(context.Context) error }); ok {
					if err := migrator.Migrate(ctx); err != nil {
						return fmt.Errorf("migration failed for database '%s': %w", name, err)
					}
					fmt.Printf("✅ Migrations applied to '%s'\n", name)
				} else {
					fmt.Printf("⚠️  Database '%s' does not support migrations\n", name)
				}
			}

			return nil
		},
	}
}

// createMigrateDownCommand creates the migrate down command
func (f *CommandsFactory) createMigrateDownCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Rollback migrations",
		Long:  `Rollback database migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.logger.Info("Rolling back migrations...")

			steps, _ := cmd.Flags().GetInt("steps")
			if steps <= 0 {
				steps = 1
			}

			fmt.Printf("Rolling back %d migration(s)...\n", steps)

			// This would use the actual migration system
			fmt.Printf("Migration rollback not yet implemented\n")

			return nil
		},
	}
}

// createMigrateStatusCommand creates the migrate status command
func (f *CommandsFactory) createMigrateStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Long:  `Display the current migration status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Migration Status:\n")
			fmt.Printf("================\n\n")

			// This would show actual migration status
			fmt.Printf("Migration status not yet implemented\n")

			return nil
		},
	}
}

// createMigrateCreateCommand creates the migrate create command
func (f *CommandsFactory) createMigrateCreateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new migration",
		Long:  `Create a new migration file with the specified name.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			f.logger.Info("Creating migration", logger.String("name", name))

			// This would create actual migration files
			fmt.Printf("Creating migration '%s'...\n", name)
			fmt.Printf("Migration creation not yet implemented\n")

			return nil
		},
	}
}

// CreateHealthCommand creates the health command
func (f *CommandsFactory) CreateHealthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check application health",
		Long:  `Check the health of the application and its components.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.logger.Info("Checking application health...")

			// Get health checker from container
			var healthChecker observability.Health
			if f.container != nil {
				if hc, err := f.container.Resolve("health"); err == nil {
					healthChecker = hc.(observability.Health)
				}
			}

			if healthChecker == nil {
				fmt.Println("Health checker not configured")
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Check health
			status := healthChecker.Check(ctx)

			fmt.Printf("Application Health Status:\n")
			fmt.Printf("=========================\n\n")
			fmt.Printf("Overall Status: %s\n", status.Status)
			fmt.Printf("Timestamp: %s\n", status.Timestamp.Format(time.RFC3339))
			fmt.Printf("Duration: %s\n", status.Duration)

			if len(status.Checks) > 0 {
				fmt.Printf("\nComponent Status:\n")
				for name, check := range status.Checks {
					statusIcon := "✅"
					if check.Status != observability.HealthStatusUp {
						statusIcon = "❌"
					}
					fmt.Printf("%s %s: %s", statusIcon, name, check.Status)
					if check.Message != "" {
						fmt.Printf(" - %s", check.Message)
					}
					fmt.Printf("\n")
				}
			}

			// Exit with error code if unhealthy
			if status.Status != observability.HealthStatusUp {
				os.Exit(1)
			}

			return nil
		},
	}
}

// CreateConfigCommand creates the config command
func (f *CommandsFactory) CreateConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management commands",
		Long:  `Manage application configuration.`,
	}

	// Add subcommands
	cmd.AddCommand(f.createConfigShowCommand())
	cmd.AddCommand(f.createConfigValidateCommand())
	cmd.AddCommand(f.createConfigInitCommand())

	return cmd
}

// createConfigShowCommand creates the config show command
func (f *CommandsFactory) createConfigShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		Long:  `Display the current application configuration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get configuration from container
			var config core.Config
			if f.container != nil {
				if c, err := f.container.Resolve("config"); err == nil {
					config = c.(core.Config)
				}
			}

			if config == nil {
				fmt.Println("Configuration not available")
				return nil
			}

			fmt.Printf("Current Configuration:\n")
			fmt.Printf("=====================\n\n")

			// Show configuration (sanitized for security)
			configData := f.sanitizeConfig(config.All())
			jsonData, err := json.MarshalIndent(configData, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}

			fmt.Println(string(jsonData))
			return nil
		},
	}
}

// createConfigValidateCommand creates the config validate command
func (f *CommandsFactory) createConfigValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration",
		Long:  `Validate the current configuration for errors.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.logger.Info("Validating configuration...")

			// Get configuration from container
			var config core.Config
			if f.container != nil {
				if c, err := f.container.Resolve("config"); err == nil {
					config = c.(core.Config)
				}
			}

			if config == nil {
				return fmt.Errorf("configuration not available")
			}

			// Validate configuration
			if err := config.Validate(); err != nil {
				fmt.Printf("❌ Configuration validation failed: %v\n", err)
				return err
			}

			fmt.Printf("✅ Configuration is valid\n")
			return nil
		},
	}
}

// createConfigInitCommand creates the config init command
func (f *CommandsFactory) createConfigInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration file",
		Long:  `Create a new configuration file with default values.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			filename := "config.yaml"
			if len(args) > 0 {
				filename = args[0]
			}

			f.logger.Info("Initializing configuration file", logger.String("filename", filename))

			// Check if file exists
			if _, err := os.Stat(filename); err == nil {
				overwrite, _ := cmd.Flags().GetBool("overwrite")
				if !overwrite {
					return fmt.Errorf("configuration file '%s' already exists, use --overwrite to replace", filename)
				}
			}

			// Create default configuration
			defaultConfig := f.createDefaultConfig()
			yamlData, err := yaml.Marshal(defaultConfig)
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}

			// Write to file
			if err := os.WriteFile(filename, yamlData, 0644); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}

			fmt.Printf("✅ Configuration file '%s' created\n", filename)
			return nil
		},
	}
}

// CreateJobsCommand creates the jobs command
func (f *CommandsFactory) CreateJobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Job management commands",
		Long:  `Manage background jobs and queues.`,
	}

	// Add subcommands
	cmd.AddCommand(f.createJobsStatusCommand())
	cmd.AddCommand(f.createJobsRunCommand())
	cmd.AddCommand(f.createJobsWorkerCommand())

	return cmd
}

// createJobsStatusCommand creates the jobs status command
func (f *CommandsFactory) createJobsStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show job queue status",
		Long:  `Display the current status of job queues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get jobs processor from container
			var processor jobs.Processor
			if f.container != nil {
				if p, err := f.container.Resolve("jobs"); err == nil {
					processor = p.(jobs.Processor)
				}
			}

			if processor == nil {
				fmt.Println("Job processor not configured")
				return nil
			}

			ctx := context.Background()
			stats, _ := processor.GetStats(ctx)

			fmt.Printf("Job Queue Status:\n")
			fmt.Printf("================\n\n")
			fmt.Printf("Queued Jobs: %d\n", stats.QueuedJobs)
			fmt.Printf("Running Jobs: %d\n", stats.RunningJobs)
			fmt.Printf("Completed Jobs: %d\n", stats.CompletedJobs)
			fmt.Printf("Failed Jobs: %d\n", stats.FailedJobs)
			fmt.Printf("Workers: %d\n", stats.TotalWorkers)

			return nil
		},
	}
}

// createJobsRunCommand creates the jobs run command
func (f *CommandsFactory) createJobsRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run [job-type]",
		Short: "Run a specific job",
		Long:  `Execute a specific job type immediately.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobType := args[0]
			f.logger.Info("Running job", logger.String("type", jobType))

			// Get jobs processor from container
			var processor jobs.Processor
			if f.container != nil {
				if p, err := f.container.Resolve("jobs"); err == nil {
					processor = p.(jobs.Processor)
				}
			}

			if processor == nil {
				return fmt.Errorf("job processor not configured")
			}

			ctx := context.Background()
			job := jobs.Job{
				Type:    jobType,
				Payload: map[string]interface{}{},
			}

			fmt.Printf("Running job '%s'...\n", jobType)
			if err := processor.Enqueue(ctx, job); err != nil {
				return fmt.Errorf("failed to enqueue job: %w", err)
			}

			fmt.Printf("✅ Job '%s' enqueued\n", jobType)
			return nil
		},
	}
}

// createJobsWorkerCommand creates the jobs worker command
func (f *CommandsFactory) createJobsWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Start job worker",
		Long:  `Start a background job worker.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.logger.Info("Starting job worker...")

			// Get jobs processor from container
			var processor jobs.Processor
			if f.container != nil {
				if p, err := f.container.Resolve("jobs"); err == nil {
					processor = p.(jobs.Processor)
				}
			}

			if processor == nil {
				return fmt.Errorf("job processor not configured")
			}

			ctx := context.Background()
			fmt.Printf("Starting job worker...\n")

			return processor.Start(ctx)
		},
	}
}

// CreatePluginCommand creates the plugin command
func (f *CommandsFactory) CreatePluginCommand(pluginManager plugins.Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Plugin management commands",
		Long:  `Manage application plugins.`,
	}

	// Add subcommands
	cmd.AddCommand(f.createPluginListCommand(pluginManager))
	cmd.AddCommand(f.createPluginEnableCommand(pluginManager))
	cmd.AddCommand(f.createPluginDisableCommand(pluginManager))
	cmd.AddCommand(f.createPluginStatusCommand(pluginManager))

	return cmd
}

// createPluginListCommand creates the plugin list command
func (f *CommandsFactory) createPluginListCommand(pluginManager plugins.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all plugins",
		Long:  `List all available plugins and their status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			plugins := pluginManager.List()

			fmt.Printf("Available Plugins:\n")
			fmt.Printf("==================\n\n")

			if len(plugins) == 0 {
				fmt.Println("No plugins available")
				return nil
			}

			for _, plugin := range plugins {
				status := "❌ Disabled"
				if plugin.IsEnabled() {
					status = "✅ Enabled"
				}

				fmt.Printf("Name: %s\n", plugin.Name())
				fmt.Printf("Version: %s\n", plugin.Version())
				fmt.Printf("Description: %s\n", plugin.Description())
				fmt.Printf("Author: %s\n", plugin.Author())
				fmt.Printf("Status: %s\n", status)
				fmt.Printf("---\n")
			}

			return nil
		},
	}
}

// createPluginEnableCommand creates the plugin enable command
func (f *CommandsFactory) createPluginEnableCommand(pluginManager plugins.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "enable [plugin-name]",
		Short: "Enable a plugin",
		Long:  `Enable a specific plugin.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginName := args[0]
			f.logger.Info("Enabling plugin", logger.String("name", pluginName))

			if err := pluginManager.Enable(pluginName); err != nil {
				return fmt.Errorf("failed to enable plugin '%s': %w", pluginName, err)
			}

			fmt.Printf("✅ Plugin '%s' enabled\n", pluginName)
			return nil
		},
	}
}

// createPluginDisableCommand creates the plugin disable command
func (f *CommandsFactory) createPluginDisableCommand(pluginManager plugins.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "disable [plugin-name]",
		Short: "Disable a plugin",
		Long:  `Disable a specific plugin.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginName := args[0]
			f.logger.Info("Disabling plugin", logger.String("name", pluginName))

			if err := pluginManager.Disable(pluginName); err != nil {
				return fmt.Errorf("failed to disable plugin '%s': %w", pluginName, err)
			}

			fmt.Printf("✅ Plugin '%s' disabled\n", pluginName)
			return nil
		},
	}
}

// createPluginStatusCommand creates the plugin status command
func (f *CommandsFactory) createPluginStatusCommand(pluginManager plugins.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "status [plugin-name]",
		Short: "Show plugin status",
		Long:  `Show the status of a specific plugin or all plugins.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				// Show specific plugin status
				pluginName := args[0]
				plugin, err := pluginManager.Get(pluginName)
				if err != nil {
					return fmt.Errorf("plugin '%s' not found: %w", pluginName, err)
				}

				status := plugin.Status()
				fmt.Printf("Plugin Status for '%s':\n", pluginName)
				fmt.Printf("======================\n\n")
				fmt.Printf("Name: %s\n", plugin.Name())
				fmt.Printf("Version: %s\n", plugin.Version())
				fmt.Printf("Enabled: %t\n", status.Enabled)
				fmt.Printf("Initialized: %t\n", status.Initialized)
				fmt.Printf("Started: %t\n", status.Started)
				fmt.Printf("Health: %s\n", status.Health)
				if status.Error != "" {
					fmt.Printf("Error: %s\n", status.Error)
				}
			} else {
				// Show all plugin status
				statusMap := pluginManager.Status()
				fmt.Printf("Plugin Status:\n")
				fmt.Printf("==============\n\n")

				for name, status := range statusMap {
					fmt.Printf("%s: %s\n", name, status.Health)
				}
			}

			return nil
		},
	}
}

// CreateVersionCommand creates the version command
func (f *CommandsFactory) CreateVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Long:  `Display version information about the application and its components.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get application version info
			var appName, appVersion string
			if app, ok := f.app.(interface{ Name() string }); ok {
				appName = app.Name()
			}
			if app, ok := f.app.(interface{ Version() string }); ok {
				appVersion = app.Version()
			}

			if appName == "" {
				appName = "forge-app"
			}
			if appVersion == "" {
				appVersion = "unknown"
			}

			fmt.Printf("%s version %s\n", appName, appVersion)
			fmt.Printf("Built with Forge Framework\n")

			// Show additional version info if available
			if f.container != nil {
				// Show component versions
				fmt.Printf("\nComponent Versions:\n")
				fmt.Printf("==================\n")

				// This could be expanded to show versions of all components
				fmt.Printf("Go Runtime: %s\n", "1.21+")
				fmt.Printf("Forge Core: %s\n", "1.0.0")
			}

			return nil
		},
	}
}

// CreateCompletionCommand creates the completion command
func (f *CommandsFactory) CreateCompletionCommand(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate completion script",
		Long: `Generate shell completion scripts for the CLI.

To load completions:

Bash:
  $ source <(your-app completion bash)

Zsh:
  $ source <(your-app completion zsh)

Fish:
  $ your-app completion fish | source

PowerShell:
  PS> your-app completion powershell | Out-String | Invoke-Expression

To load completions for each session, execute once:

Linux:
  $ your-app completion bash > /etc/bash_completion.d/your-app

macOS:
  $ your-app completion bash > /usr/local/etc/bash_completion.d/your-app

Zsh:
  $ your-app completion zsh > "${fpath[1]}/_your-app"`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactValidArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletion(os.Stdout)
			}
			return nil
		},
	}

	return cmd
}

// Helper methods

// getAllDatabases gets all databases from the container
func (f *CommandsFactory) getAllDatabases() map[string]database.Database {
	databases := make(map[string]database.Database)

	// This would need to be implemented based on how databases are stored in the container
	// For now, return empty map
	return databases
}

// getDatabase gets a specific database by name
func (f *CommandsFactory) getDatabase(name string) database.Database {
	if f.container == nil {
		return nil
	}

	// Try to get from container
	if db, err := f.container.Resolve("database:" + name); err == nil {
		return db.(database.Database)
	}

	return nil
}

// getAllSQLDatabases gets all SQL databases from the container
func (f *CommandsFactory) getAllSQLDatabases() map[string]database.SQLDatabase {
	databases := make(map[string]database.SQLDatabase)

	// This would need to be implemented based on how databases are stored in the container
	// For now, return empty map
	return databases
}

// getSQLDatabase gets a specific SQL database by name
func (f *CommandsFactory) getSQLDatabase(name string) database.SQLDatabase {
	if f.container == nil {
		return nil
	}

	// Try to get from container
	if db, err := f.container.Resolve("database:" + name); err == nil {
		if sqlDB, ok := db.(database.SQLDatabase); ok {
			return sqlDB
		}
	}

	return nil
}

// getDatabaseType returns the type of a database
func (f *CommandsFactory) getDatabaseType(db database.Database) string {
	// This would need to be implemented based on database interface
	// For now, return generic type
	return "unknown"
}

// sanitizeConfig removes sensitive information from configuration
func (f *CommandsFactory) sanitizeConfig(config map[string]interface{}) map[string]interface{} {
	sanitized := make(map[string]interface{})

	for key, value := range config {
		lowerKey := strings.ToLower(key)

		// Skip sensitive keys
		if strings.Contains(lowerKey, "password") ||
			strings.Contains(lowerKey, "secret") ||
			strings.Contains(lowerKey, "token") ||
			strings.Contains(lowerKey, "key") && !strings.Contains(lowerKey, "public") {
			sanitized[key] = "***REDACTED***"
			continue
		}

		// Recursively sanitize nested maps
		if nestedMap, ok := value.(map[string]interface{}); ok {
			sanitized[key] = f.sanitizeConfig(nestedMap)
		} else {
			sanitized[key] = value
		}
	}

	return sanitized
}

// createDefaultConfig creates a default configuration structure
func (f *CommandsFactory) createDefaultConfig() *core.ConfigFile {
	return &core.ConfigFile{
		App: core.AppConfig{
			Name:        "forge-app",
			Version:     "1.0.0",
			Environment: "development",
			Debug:       true,
		},
		Server: core.ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		Database: map[string]interface{}{
			"driver":   "postgres",
			"host":     "localhost",
			"port":     5432,
			"name":     "forge_db",
			"user":     "forge",
			"password": "password",
		},
		Logging: logger.LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
		Observability: core.ObservabilityConfig{
			Tracing: core.TracingConfig{
				Enabled: false,
			},
			Metrics: core.MetricsConfig{
				Enabled: true,
				Port:    9090,
			},
		},
	}
}

// Package-level YAML marshaling (simplified)
// In a real implementation, you'd import "gopkg.in/yaml.v3"
type yaml struct{}

func (y yaml) Marshal(v interface{}) ([]byte, error) {
	// This is a placeholder - in real implementation, use yaml.Marshal
	return json.MarshalIndent(v, "", "  ")
}
