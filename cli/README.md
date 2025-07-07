# Forge CLI Module Documentation

The Forge CLI module provides a powerful, extensible command-line interface for managing Forge applications. It offers built-in commands for common operations, plugin integration, and a fluent API for creating custom commands.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Basic Usage](#basic-usage)
- [Built-in Commands](#built-in-commands)
- [Custom Commands](#custom-commands)
- [Plugin Integration](#plugin-integration)
- [Configuration](#configuration)
- [Advanced Features](#advanced-features)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Installation

The CLI module is included with the Forge framework. Import it in your application:

```go
import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/cli"
)
```

## Quick Start

### Basic Setup

```go
package main

import (
    "log"
    "github.com/xraph/forge"
    "github.com/xraph/forge/cli"
)

func main() {
    // Create Forge application
    app := forge.New("my-app").
        WithDatabase(forge.PostgreSQLConfig("primary", "localhost", "user", "pass", "db")).
        WithTracing(forge.TracingConfig{ServiceName: "my-app"}).
        MustBuild()

    // Create CLI manager
    cliManager, err := cli.NewManager(app, cli.DefaultConfig())
    if err != nil {
        log.Fatal(err)
    }

    // Initialize and execute
    if err := cliManager.Initialize(); err != nil {
        log.Fatal(err)
    }

    if err := cliManager.Execute(); err != nil {
        log.Fatal(err)
    }
}
```

### Using the Builder Pattern

```go
func main() {
    app := forge.New("my-app").MustBuild()
    
    // Create CLI with builder
    cliManager := cli.NewBuilder().
        WithApplication(app).
        WithConfig(&cli.Config{
            AppName:     "my-app",
            Version:     "1.0.0",
            Description: "My awesome application",
        }).
        WithBuiltinCommands().
        WithPluginCommands().
        MustBuild()

    cliManager.Execute()
}
```

## Basic Usage

### Command Structure

```bash
# Basic command structure
./app [global-flags] command [command-flags] [arguments]

# Examples
./app --config config.yaml server --port 8080
./app --log-level debug db status
./app --timeout 60s migrate up
```

### Global Flags

All commands support these global flags:

```bash
--config string      Config file path (default: .forge.yaml)
--log-level string   Log level (debug, info, warn, error) (default: info)
--colors             Enable colored output (default: true)
--timing             Show command execution time
--timeout duration   Command timeout (default: 30s)
--help              Show help information
--version           Show version information
```

### Getting Help

```bash
# Show general help
./app --help

# Show command-specific help
./app server --help
./app db --help
./app migrate --help

# Show subcommand help
./app db ping --help
./app plugin list --help
```

## Built-in Commands

### Server Command

Start the HTTP server with your application.

```bash
# Start server with default settings
./app server

# Start with custom host and port
./app server --host 0.0.0.0 --port 8080

# Start with TLS
./app server --tls --cert cert.pem --key key.pem

# Available flags:
--host string    Server host (overrides config)
--port int       Server port (overrides config)
--tls            Enable TLS
--cert string    TLS certificate file
--key string     TLS key file
```

### Database Commands

Manage database connections and operations.

```bash
# Check status of all databases
./app db status

# Ping specific database
./app db ping primary
./app db ping cache

# Show database schema
./app db schema primary

# Seed database with test data
./app db seed primary
```

**Example Output:**
```
Database Status:
================

Database: primary
Type: postgresql
Status: ✅ OK
---
Database: cache
Type: redis
Status: ✅ OK
---
```

### Migration Commands

Manage database migrations.

```bash
# Apply all pending migrations
./app migrate up

# Rollback migrations
./app migrate down --steps 1

# Show migration status
./app migrate status

# Create new migration
./app migrate create add_users_table
```

**Example Migration Status:**
```
Migration Status:
================

✅ 001_create_users_table.sql        (Applied: 2024-01-01 12:00:00)
✅ 002_add_user_indexes.sql          (Applied: 2024-01-01 12:01:00)
⏳ 003_add_user_profiles.sql         (Pending)
```

### Health Commands

Check application and component health.

```bash
# Check overall health
./app health

# Health check with JSON output
./app health --format json
```

**Example Output:**
```
Application Health Status:
=========================

Overall Status: UP
Timestamp: 2024-01-01T12:00:00Z
Duration: 45ms

Component Status:
✅ database: UP
✅ cache: UP
✅ jobs: UP
⚠️  external-api: WARNING - High response time
```

### Configuration Commands

Manage application configuration.

```bash
# Show current configuration (sanitized)
./app config show

# Show configuration in JSON format
./app config show --format json

# Validate configuration
./app config validate

# Initialize new configuration file
./app config init
./app config init myconfig.yaml --overwrite
```

### Jobs Commands

Manage background jobs and workers.

```bash
# Show job queue status
./app jobs status

# Start job worker
./app jobs worker

# Run specific job
./app jobs run email-notifications
./app jobs run data-sync --data '{"table": "users"}'
```

**Example Job Status:**
```
Job Queue Status:
================

Queued Jobs: 23
Processing Jobs: 2
Completed Jobs: 1,245
Failed Jobs: 12
Workers: 4
```

### Plugin Commands

Manage application plugins.

```bash
# List all plugins
./app plugin list

# Enable plugin
./app plugin enable auth-plugin

# Disable plugin
./app plugin disable auth-plugin

# Show plugin status
./app plugin status
./app plugin status auth-plugin
```

**Example Plugin List:**
```
Available Plugins:
==================

Name: auth-plugin
Version: 1.2.0
Description: Authentication and authorization plugin
Author: Forge Team
Status: ✅ Enabled
---
Name: metrics-plugin
Version: 1.0.0
Description: Enhanced metrics collection
Author: Community
Status: ❌ Disabled
---
```

### Version Command

Show version information.

```bash
# Show version
./app version

# Show detailed version information
./app version --detailed
```

### Completion Command

Generate shell completion scripts.

```bash
# Generate bash completion
./app completion bash > /etc/bash_completion.d/my-app

# Generate zsh completion
./app completion zsh > "${fpath[1]}/_my-app"

# Generate fish completion
./app completion fish > ~/.config/fish/completions/my-app.fish

# Generate PowerShell completion
./app completion powershell > my-app.ps1
```

## Custom Commands

### Creating Custom Commands

```go
package main

import (
    "fmt"
    "github.com/spf13/cobra"
    "github.com/xraph/forge/cli"
)

func main() {
    app := forge.New("my-app").MustBuild()
    cliManager, _ := cli.NewManager(app, cli.DefaultConfig())

    // Create custom command
    customCmd := &cobra.Command{
        Use:   "custom",
        Short: "Custom command example",
        Long:  "A custom command that demonstrates CLI extensibility",
        RunE: func(cmd *cobra.Command, args []string) error {
            fmt.Println("Executing custom command!")
            
            // Access application services
            if app, ok := cliManager.GetApp().(forge.Application); ok {
                db := app.Database()
                logger := app.Logger()
                
                logger.Info("Custom command executed")
                // Use database, cache, etc.
            }
            
            return nil
        },
    }

    // Add flags
    customCmd.Flags().String("input", "", "Input parameter")
    customCmd.Flags().Bool("verbose", false, "Verbose output")

    // Add command to manager
    cliManager.AddCommand(customCmd)
    cliManager.Execute()
}
```

### Command Groups

Create organized command groups:

```go
// Create command group
userCmd := &cobra.Command{
    Use:   "user",
    Short: "User management commands",
}

// Add subcommands
userCmd.AddCommand(&cobra.Command{
    Use:   "create",
    Short: "Create a new user",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Create user logic
        return nil
    },
})

userCmd.AddCommand(&cobra.Command{
    Use:   "list",
    Short: "List users",
    RunE: func(cmd *cobra.Command, args []string) error {
        // List users logic
        return nil
    },
})

cliManager.AddCommand(userCmd)
```

### Using Command Factory

```go
func main() {
    app := forge.New("my-app").MustBuild()
    cliManager, _ := cli.NewManager(app, cli.DefaultConfig())
    
    // Create command factory
    factory := cli.NewCommandsFactory(app, app.Container(), app.Logger())
    
    // Create custom command using factory
    customCmd := factory.CreateCustomCommand(cli.CommandDefinition{
        Name:  "backup",
        Short: "Backup application data",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Backup logic
            return nil
        },
        Flags: []cli.FlagDefinition{
            {
                Name:  "output",
                Type:  cli.FlagTypeString,
                Usage: "Output file path",
                Required: true,
            },
            {
                Name:  "compress",
                Type:  cli.FlagTypeBool,
                Usage: "Compress backup file",
                DefaultValue: true,
            },
        },
    })
    
    cliManager.AddCommand(customCmd)
    cliManager.Execute()
}
```

## Plugin Integration

### Creating CLI-Enabled Plugins

```go
package myplugin

import (
    "context"
    "github.com/xraph/forge/plugins"
    "github.com/xraph/forge/cli"
)

type MyPlugin struct {
    plugins.BasePlugin
}

func (p *MyPlugin) Commands() []plugins.CommandDefinition {
    return []plugins.CommandDefinition{
        {
            Name:        "my-command",
            Description: "My custom plugin command",
            Handler:     &MyCommandHandler{},
            Flags: []plugins.FlagDefinition{
                {
                    Name:        "param",
                    Type:        "string",
                    Description: "Parameter for the command",
                    Required:    true,
                },
            },
        },
    }
}

type MyCommandHandler struct{}

func (h *MyCommandHandler) Execute(ctx context.Context, args []string) error {
    // Command implementation
    return nil
}
```

### Auto-Discovery of Plugin Commands

When you enable plugin commands, the CLI manager automatically discovers and registers all commands from enabled plugins:

```go
cliManager := cli.NewBuilder().
    WithApplication(app).
    WithPluginCommands().  // Automatically discovers plugin commands
    MustBuild()
```

## Configuration

### Configuration File

Create a `.forge.yaml` file in your project root:

```yaml
# Application configuration
app:
  name: my-app
  version: 1.0.0
  environment: development

# CLI configuration
cli:
  app_name: my-app
  version: 1.0.0
  description: "My awesome application"
  default_command: server
  log_level: info
  timeout: 30s
  enable_colors: true
  enable_timing: false
  enable_builtins: true
  enable_plugins: true
  enable_completion: true

# Server configuration
server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 30s
  write_timeout: 30s

# Database configuration
database:
  driver: postgres
  host: localhost
  port: 5432
  name: myapp_db
  user: myapp
  password: password

# Logging configuration
logging:
  level: info
  format: json
  output: stdout
```

### Environment Variables

Override configuration using environment variables:

```bash
# Application settings
export FORGE_APP_NAME=my-app
export FORGE_ENVIRONMENT=production

# CLI settings
export FORGE_CLI_LOG_LEVEL=debug
export FORGE_CLI_ENABLE_COLORS=false
export FORGE_CLI_TIMEOUT=60s

# Server settings
export FORGE_SERVER_HOST=0.0.0.0
export FORGE_SERVER_PORT=8080

# Database settings
export FORGE_DATABASE_HOST=db.example.com
export FORGE_DATABASE_PASSWORD=secret
```

### Programmatic Configuration

```go
config := &cli.Config{
    AppName:        "my-app",
    Version:        "1.0.0",
    Description:    "My application",
    DefaultCommand: "server",
    LogLevel:       "info",
    Timeout:        30 * time.Second,
    EnableColors:   true,
    EnableTiming:   true,
    EnableBuiltins: true,
    EnablePlugins:  true,
    RequireConfirmation: []string{"delete", "drop"},
}

cliManager, err := cli.NewManager(app, config)
```

## Advanced Features

### Middleware

Add middleware to intercept and modify command execution:

```go
type TimingMiddleware struct{}

func (m *TimingMiddleware) Execute(ctx *cli.ExecutionContext, next func(*cli.ExecutionContext) error) error {
    start := time.Now()
    err := next(ctx)
    duration := time.Since(start)
    
    fmt.Printf("Command executed in %v\n", duration)
    return err
}

func (m *TimingMiddleware) Name() string { return "timing" }
func (m *TimingMiddleware) Priority() int { return 100 }
func (m *TimingMiddleware) Enabled() bool { return true }

// Add middleware to CLI
cliManager := cli.NewBuilder().
    WithApplication(app).
    WithMiddleware(&TimingMiddleware{}).
    MustBuild()
```

### Custom Output Formatting

```go
// Custom command with formatted output
customCmd := &cobra.Command{
    Use:   "users",
    Short: "List users",
    RunE: func(cmd *cobra.Command, args []string) error {
        users := getUsersFromDatabase()
        format, _ := cmd.Flags().GetString("format")
        
        switch format {
        case "json":
            return outputJSON(users)
        case "table":
            return outputTable(users)
        default:
            return outputText(users)
        }
    },
}

customCmd.Flags().String("format", "table", "Output format (json|table|text)")
```

### Interactive Commands

```go
import "github.com/manifoldco/promptui"

interactiveCmd := &cobra.Command{
    Use:   "setup",
    Short: "Interactive setup",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Text input
        namePrompt := promptui.Prompt{
            Label: "Application Name",
            Default: "my-app",
        }
        name, err := namePrompt.Run()
        if err != nil {
            return err
        }
        
        // Selection
        envPrompt := promptui.Select{
            Label: "Environment",
            Items: []string{"development", "staging", "production"},
        }
        _, env, err := envPrompt.Run()
        if err != nil {
            return err
        }
        
        // Confirmation
        confirmPrompt := promptui.Prompt{
            Label:     "Continue with setup",
            IsConfirm: true,
        }
        _, err = confirmPrompt.Run()
        if err != nil {
            return err
        }
        
        // Proceed with setup
        return performSetup(name, env)
    },
}
```

### Command Validation

```go
validatedCmd := &cobra.Command{
    Use:   "deploy [environment]",
    Short: "Deploy application",
    Args:  cobra.ExactArgs(1),
    ValidArgs: []string{"development", "staging", "production"},
    PreRunE: func(cmd *cobra.Command, args []string) error {
        // Custom validation
        env := args[0]
        if env == "production" {
            confirm, _ := cmd.Flags().GetBool("confirm")
            if !confirm {
                return fmt.Errorf("production deployment requires --confirm flag")
            }
        }
        return nil
    },
    RunE: func(cmd *cobra.Command, args []string) error {
        env := args[0]
        return deployToEnvironment(env)
    },
}

validatedCmd.Flags().Bool("confirm", false, "Confirm production deployment")
```

## Best Practices

### 1. Command Organization

```go
// Group related commands
authCmd := &cobra.Command{Use: "auth", Short: "Authentication commands"}
authCmd.AddCommand(loginCmd, logoutCmd, statusCmd)

userCmd := &cobra.Command{Use: "user", Short: "User management commands"}
userCmd.AddCommand(createUserCmd, listUsersCmd, deleteUserCmd)

// Add to CLI manager
cliManager.AddCommands(authCmd, userCmd)
```

### 2. Error Handling

```go
cmd := &cobra.Command{
    Use:   "risky-operation",
    Short: "Performs a risky operation",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Wrap errors with context
        if err := performOperation(); err != nil {
            return fmt.Errorf("operation failed: %w", err)
        }
        
        // Use appropriate exit codes
        if !success {
            os.Exit(1)
        }
        
        return nil
    },
}
```

### 3. Configuration Management

```go
// Use consistent configuration keys
const (
    ConfigKeyDBHost = "database.host"
    ConfigKeyDBPort = "database.port"
    ConfigKeyDBName = "database.name"
)

// Provide sensible defaults
func getDBConfig() DatabaseConfig {
    return DatabaseConfig{
        Host: config.GetString(ConfigKeyDBHost, "localhost"),
        Port: config.GetInt(ConfigKeyDBPort, 5432),
        Name: config.GetString(ConfigKeyDBName, "app_db"),
    }
}
```

### 4. Logging

```go
cmd := &cobra.Command{
    Use:   "data-import",
    Short: "Import data from file",
    RunE: func(cmd *cobra.Command, args []string) error {
        logger := app.Logger().Named("data-import")
        
        logger.Info("Starting data import",
            logger.String("file", filename),
            logger.Int("batch_size", batchSize),
        )
        
        // Log progress
        for i, record := range records {
            if i%100 == 0 {
                logger.Debug("Import progress",
                    logger.Int("processed", i),
                    logger.Int("total", len(records)),
                )
            }
        }
        
        logger.Info("Data import completed",
            logger.Int("total_records", len(records)),
            logger.Duration("duration", time.Since(start)),
        )
        
        return nil
    },
}
```

### 5. Testing CLI Commands

```go
func TestUserCreateCommand(t *testing.T) {
    // Create test application
    app := forge.NewTestApplication("test-app")
    defer app.Cleanup()
    
    // Create CLI manager
    cliManager, err := cli.NewManager(app, cli.DefaultConfig())
    require.NoError(t, err)
    
    // Test command execution
    cmd := cliManager.GetCommand("user")
    require.NotNil(t, cmd)
    
    // Set up test environment
    os.Setenv("FORGE_DATABASE_HOST", "localhost")
    defer os.Unsetenv("FORGE_DATABASE_HOST")
    
    // Execute command
    err = cmd.Execute()
    assert.NoError(t, err)
}
```

## Troubleshooting

### Common Issues

#### 1. Command Not Found
```bash
Error: unknown command "migrate" for "my-app"
```
**Solution**: Ensure built-in commands are enabled:
```go
config := cli.DefaultConfig()
config.EnableBuiltins = true
```

#### 2. Database Connection Failed
```bash
❌ FAILED: dial tcp [::1]:5432: connect: connection refused
```
**Solution**: Check database configuration and ensure database is running:
```bash
./app config show
./app db ping
```

#### 3. Plugin Commands Not Available
```bash
Error: unknown command "plugin-command" for "my-app"
```
**Solution**: Enable plugin commands and ensure plugin is loaded:
```go
config.EnablePlugins = true
```
```bash
./app plugin list
./app plugin enable my-plugin
```

#### 4. Configuration File Not Found
```bash
Error: Config File ".forge" Not Found
```
**Solution**: Create configuration file or specify path:
```bash
./app config init
# or
./app --config /path/to/config.yaml command
```

### Debugging

Enable debug logging to troubleshoot issues:

```bash
# Enable debug logging
./app --log-level debug command

# Show timing information
./app --timing command

# Use verbose output
./app command --verbose
```

### Performance Issues

For performance debugging:

```bash
# Set timeout for long-running commands
./app --timeout 5m migrate up

# Monitor command execution
./app --timing --log-level debug jobs worker
```

### Configuration Validation

Validate your configuration:

```bash
# Check configuration syntax
./app config validate

# Show resolved configuration
./app config show

# Test database connectivity
./app db ping
```

## Examples

### Complete Application with CLI

```go
package main

import (
    "log"
    "os"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/cli"
    "github.com/xraph/forge/plugins"
)

func main() {
    // Create application
    app := forge.New("my-awesome-app").
        WithVersion("1.0.0").
        WithEnvironment("production").
        WithPostgreSQL("primary", "host=localhost user=myapp dbname=myapp_db sslmode=disable").
        WithRedis("cache", "localhost:6379").
        WithTracing(forge.TracingConfig{
            ServiceName: "my-awesome-app",
            Endpoint:    "http://jaeger:14268/api/traces",
        }).
        WithMetrics(forge.MetricsConfig{
            ServiceName: "my-awesome-app",
            Port:        9090,
        }).
        EnableJobs().
        EnablePlugins().
        MustBuild()

    // Create CLI manager
    cliManager := cli.NewBuilder().
        WithApplication(app).
        WithConfig(&cli.Config{
            AppName:     "my-awesome-app",
            Version:     "1.0.0",
            Description: "My awesome application with CLI",
            EnableColors: true,
            EnableTiming: true,
            Timeout:     60 * time.Second,
        }).
        WithBuiltinCommands().
        WithPluginCommands().
        MustBuild()

    // Execute CLI
    if err := cliManager.Execute(); err != nil {
        log.Fatal(err)
    }
}
```

### Custom Plugin with CLI Commands

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/xraph/forge/plugins"
)

type BackupPlugin struct {
    plugins.BasePlugin
}

func (p *BackupPlugin) Name() string { return "backup" }
func (p *BackupPlugin) Version() string { return "1.0.0" }
func (p *BackupPlugin) Description() string { return "Database backup plugin" }

func (p *BackupPlugin) Commands() []plugins.CommandDefinition {
    return []plugins.CommandDefinition{
        {
            Name:        "backup",
            Description: "Create database backup",
            Handler:     &BackupHandler{},
            Flags: []plugins.FlagDefinition{
                {
                    Name:        "output",
                    Type:        "string",
                    Description: "Output file path",
                    Required:    true,
                },
                {
                    Name:        "compress",
                    Type:        "bool",
                    Description: "Compress backup file",
                    Default:     true,
                },
            },
        },
        {
            Name:        "restore",
            Description: "Restore database from backup",
            Handler:     &RestoreHandler{},
            Flags: []plugins.FlagDefinition{
                {
                    Name:        "file",
                    Type:        "string",
                    Description: "Backup file path",
                    Required:    true,
                },
            },
        },
    }
}

type BackupHandler struct{}

func (h *BackupHandler) Execute(ctx context.Context, args []string) error {
    fmt.Println("Creating database backup...")
    // Backup implementation
    return nil
}

type RestoreHandler struct{}

func (h *RestoreHandler) Execute(ctx context.Context, args []string) error {
    fmt.Println("Restoring database from backup...")
    // Restore implementation
    return nil
}
```

The Forge CLI module provides a comprehensive, extensible command-line interface that grows with your application. It combines the power of the Cobra CLI framework with the Forge application architecture to create a seamless development and operations experience.