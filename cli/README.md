# Forge v2 CLI Framework

Enterprise-grade CLI framework for building command-line tools with Forge. Features commands, subcommands, flags, middleware, prompts, tables, plugins, and seamless Forge App integration.

## Features

✅ **Commands & Subcommands** - Hierarchical command structure  
✅ **Comprehensive Flags** - String, int, bool, slice, duration with validation  
✅ **Interactive Prompts** - Input, confirm, select, multi-select with **arrow key navigation** ⬆️⬇️  
✅ **Space Bar Selection** - Toggle multi-select with spacebar (modern UX) ⎵  
✅ **Progress Indicators** - Progress bars and spinners  
✅ **Table Output** - Formatted, colored tables with multiple styles  
✅ **Middleware** - Before/after command hooks  
✅ **Plugin System** - Modular, composable commands  
✅ **CLI-Optimized Logger** - Color-coded, simple output  
✅ **Auto-Generated Help** - Automatic help text generation  
✅ **Shell Completion** - Bash, Zsh, Fish completion  
✅ **Forge Integration** - Access to DI container and services  

## Installation

```bash
go get github.com/xraph/forge/cli
```

## Quick Start

### Simple CLI

```go
package main

import (
    "fmt"
    "os"
    "github.com/xraph/forge/cli"
)

func main() {
    app := cli.New(cli.Config{
        Name:        "mytool",
        Version:     "1.0.0",
        Description: "My awesome CLI tool",
    })

    helloCmd := cli.NewCommand(
        "hello",
        "Say hello",
        func(ctx cli.CommandContext) error {
            name := ctx.String("name")
            if name == "" {
                name = "World"
            }
            ctx.Success(fmt.Sprintf("Hello, %s!", name))
            return nil
        },
        cli.WithFlag(cli.NewStringFlag("name", "n", "Name to greet", "")),
    )

    app.AddCommand(helloCmd)

    if err := app.Run(os.Args); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(cli.GetExitCode(err))
    }
}
```

Usage:
```bash
$ mytool hello --name=John
✓ Hello, John!
```

## Commands & Subcommands

```go
// Create parent command
projectCmd := cli.NewCommand("project", "Project management", nil)

// Add subcommands
newCmd := cli.NewCommand("new", "Create project", createProject)
listCmd := cli.NewCommand("list", "List projects", listProjects)
deleteCmd := cli.NewCommand("delete", "Delete project", deleteProject)

projectCmd.AddSubcommand(newCmd)
projectCmd.AddSubcommand(listCmd)
projectCmd.AddSubcommand(deleteCmd)

app.AddCommand(projectCmd)
```

Usage:
```bash
$ mytool project new --name=myapp
$ mytool project list
$ mytool project delete --name=myapp
```

## Flags

### Flag Types

```go
// String flag
cli.NewStringFlag("name", "n", "Your name", "default")

// Int flag
cli.NewIntFlag("port", "p", "Port number", 8080)

// Bool flag
cli.NewBoolFlag("verbose", "v", "Verbose output", false)

// String slice flag
cli.NewStringSliceFlag("tags", "t", "Tags", []string{})

// Duration flag
cli.NewDurationFlag("timeout", "", "Timeout", 30*time.Second)
```

### Flag Validation

```go
// Required flag
cli.NewStringFlag("api-key", "", "API key", "", cli.Required())

// Range validation
cli.NewIntFlag("port", "p", "Port", 8080, cli.ValidateRange(1, 65535))

// Enum validation
cli.NewStringFlag("env", "e", "Environment", "dev",
    cli.ValidateEnum("dev", "staging", "prod"),
)
```

### Using Flags

```go
func myCommand(ctx cli.CommandContext) error {
    name := ctx.String("name")
    port := ctx.Int("port")
    verbose := ctx.Bool("verbose")
    
    if ctx.Flag("name").IsSet() {
        // Name was explicitly provided
    }
    
    return nil
}
```

## Interactive Prompts

### Arrow Key Navigation 🎯

Select and multi-select now support **arrow key navigation** for a modern CLI experience!

```go
func interactive(ctx cli.CommandContext) error {
    // Simple prompt
    name, err := ctx.Prompt("What's your name?")
    if err != nil {
        return err
    }
    
    // Confirm prompt
    confirmed, err := ctx.Confirm("Are you sure?")
    if err != nil {
        return err
    }
    
    // Select with arrow keys ↑/↓
    // Navigate with arrows, Enter to select, Esc to cancel
    env, err := ctx.Select("Choose environment:", []string{
        "development",
        "staging",
        "production",
    })
    if err != nil {
        return err
    }
    
    // Multi-select with Space bar
    // Navigate with arrows, Space to toggle, Enter to confirm
    features, err := ctx.MultiSelect("Select features:", []string{
        "database",
        "cache",
        "events",
    })
    if err != nil {
        return err
    }
    
    return nil
}
```

**Interactive Controls:**
- **↑/↓** or **j/k** - Navigate options
- **Space** - Toggle selection (multi-select)
- **Enter** - Confirm selection
- **Esc/q** - Cancel

**Visual Example:**
```
Choose environment:
  ↑/↓: Navigate  │  Enter: Select  │  Esc/q: Cancel

    development
  ▸ staging        ← Current selection (bold + highlighted)
    production
```

**Multi-Select Example:**
```
Select features:
  ↑/↓: Navigate  │  Space: Select/Deselect  │  Enter: Confirm

  ▸ [✓] database   ← Cursor here, selected
    [ ] cache
    [✓] events     ← Selected but not at cursor
```

> **Note:** Automatically falls back to number input in non-interactive terminals

## Progress Indicators

### Progress Bar

```go
func download(ctx cli.CommandContext) error {
    total := 100
    progress := ctx.ProgressBar(total)
    
    for i := 0; i <= total; i++ {
        time.Sleep(50 * time.Millisecond)
        progress.Set(i)
    }
    
    progress.Finish("Download complete!")
    return nil
}
```

### Spinner

```go
func process(ctx cli.CommandContext) error {
    spinner := ctx.Spinner("Processing...")
    
    // Long-running task
    time.Sleep(5 * time.Second)
    
    spinner.Stop(cli.Green("✓ Processing complete!"))
    return nil
}
```

**Update spinner message:**
```go
spinner := ctx.Spinner("Starting...")
spinner.Update("Loading data...")
spinner.Update("Processing...")
spinner.Stop(cli.Green("✓ Done!"))
```

## Async Select & Multi-Select

Load options dynamically from APIs, databases, or any async source:

### Async Select

```go
func selectEnv(ctx cli.CommandContext) error {
    // Define loader function
    loader := func(ctx context.Context) ([]string, error) {
        // Fetch from API, database, etc.
        return fetchEnvironmentsFromAPI(ctx)
    }
    
    // Shows spinner while loading, then prompts
    env, err := ctx.SelectAsync("Choose environment:", loader)
    if err != nil {
        return err
    }
    
    ctx.Success("Selected: " + env)
    return nil
}
```

### Async Multi-Select

```go
func selectFeatures(ctx cli.CommandContext) error {
    loader := func(ctx context.Context) ([]string, error) {
        return fetchAvailableFeaturesFromAPI(ctx)
    }
    
    features, err := ctx.MultiSelectAsync("Select features:", loader)
    if err != nil {
        return err
    }
    
    return nil
}
```

### With Retry (Flaky Network)

```go
// Automatically retries on failure with exponential backoff
region, err := ctx.SelectWithRetry("Choose region:", loader, 3)
```

**Visual Flow:**
```
⠋ Loading options...    ← Spinner during load
✓ Options loaded

Choose environment:     ← Select prompt
↑/↓: Navigate  │  Enter: Select

▸ Production
  Staging
  Development
```

> See [ASYNC_SELECT_AND_SPINNER.md](../_impl_docs/cli/ASYNC_SELECT_AND_SPINNER.md) for complete guide

## Table Output

```go
func list(ctx cli.CommandContext) error {
    table := ctx.Table()
    
    table.SetHeader([]string{"ID", "Name", "Status"})
    table.AppendRow([]string{"1", "Project A", cli.Green("Active")})
    table.AppendRow([]string{"2", "Project B", cli.Yellow("Paused")})
    table.AppendRow([]string{"3", "Project C", cli.Red("Stopped")})
    
    table.Render()
    return nil
}
```

Output:
```
┌────┬───────────┬────────┐
│ ID │ Name      │ Status │
├────┼───────────┼────────┤
│ 1  │ Project A │ Active │
│ 2  │ Project B │ Paused │
│ 3  │ Project C │ Stopped│
└────┴───────────┴────────┘
```

## Plugin System

```go
// Define a plugin
type DatabasePlugin struct {
    *cli.BasePlugin
}

func NewDatabasePlugin() cli.Plugin {
    plugin := &DatabasePlugin{
        BasePlugin: cli.NewBasePlugin(
            "database",
            "1.0.0",
            "Database management commands",
        ),
    }
    
    migrateCmd := cli.NewCommand("db:migrate", "Run migrations", migrate)
    seedCmd := cli.NewCommand("db:seed", "Seed database", seed)
    
    plugin.AddCommand(migrateCmd)
    plugin.AddCommand(seedCmd)
    
    return plugin
}

// Register plugin
app := cli.New(cli.Config{Name: "myapp", Version: "1.0.0"})
app.RegisterPlugin(NewDatabasePlugin())
```

## Forge App Integration

```go
import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/cli"
)

func main() {
    // Create Forge app
    app := forge.NewApp(forge.AppConfig{
        Name:    "my-service",
        Version: "1.0.0",
    })
    
    // Register a service
    forge.RegisterSingleton(app.Container(), "database", func(c forge.Container) (*Database, error) {
        return NewDatabase(), nil
    })
    
    // Create CLI with Forge integration
    cliApp := cli.NewForgeIntegratedCLI(app, cli.Config{
        Name:    "my-service-cli",
        Version: "1.0.0",
    })
    
    // Add command that uses Forge services
    migrateCmd := cli.NewCommand(
        "migrate",
        "Run database migrations",
        func(ctx cli.CommandContext) error {
            // Access Forge service via DI
            db, err := cli.GetService[*Database](ctx, "database")
            if err != nil {
                return err
            }
            
            return db.Migrate()
        },
    )
    
    cliApp.AddCommand(migrateCmd)
    cliApp.Run(os.Args)
}
```

Built-in Forge commands:
- `info` - Show application information
- `health` - Check application health
- `extensions` - List registered extensions

## Middleware

```go
// Logging middleware
func loggingMiddleware(next cli.CommandHandler) cli.CommandHandler {
    return func(ctx cli.CommandContext) error {
        start := time.Now()
        ctx.Logger().Info("Command: %s", ctx.Command().Name())
        
        err := next(ctx)
        
        ctx.Logger().Info("Duration: %v", time.Since(start))
        return err
    }
}

// Auth middleware
func authMiddleware(next cli.CommandHandler) cli.CommandHandler {
    return func(ctx cli.CommandContext) error {
        token := ctx.String("token")
        if token == "" {
            return cli.NewError("authentication required", cli.ExitUnauthorized)
        }
        return next(ctx)
    }
}

// Use middleware
cmd := cli.NewCommand("deploy", "Deploy", deployHandler)
cmd.Before(loggingMiddleware)
cmd.Before(authMiddleware)
```

## CLI-Optimized Logger

```go
logger := cli.NewCLILogger(
    cli.WithColors(true),
    cli.WithLevel(cli.InfoLevel),
)

logger.Success("Operation completed!")  // Green ✓
logger.Info("Processing...")            // Blue [INFO]
logger.Warning("Slow response")         // Yellow [!]
logger.Error("Failed to connect")       // Red ✗
logger.Debug("Detailed info")           // Gray [DEBUG]
```

## Color Utilities

```go
// Color functions
cli.Green("Success!")
cli.Red("Error!")
cli.Yellow("Warning!")
cli.Blue("Info")
cli.Gray("Debug")

// Combined styles
cli.BoldGreen("Important!")
cli.Bold("Emphasis")
cli.Underline("Link")
```

## Shell Completion

Generate completion scripts:

```go
// Add completion command
completionCmd := cli.NewCommand(
    "completion",
    "Generate shell completion",
    func(ctx cli.CommandContext) error {
        shell := ctx.String("shell")
        
        switch shell {
        case "bash":
            return cli.GenerateBashCompletion(app, os.Stdout)
        case "zsh":
            return cli.GenerateZshCompletion(app, os.Stdout)
        case "fish":
            return cli.GenerateFishCompletion(app, os.Stdout)
        }
        return nil
    },
    cli.WithFlag(cli.NewStringFlag("shell", "s", "Shell type", "bash")),
)
```

Install:
```bash
# Bash
mytool completion --shell=bash > /etc/bash_completion.d/mytool

# Zsh
mytool completion --shell=zsh > ~/.zsh/completion/_mytool

# Fish
mytool completion --shell=fish > ~/.config/fish/completions/mytool.fish
```

## Examples

See the `examples/` directory for complete examples:

- **simple/** - Basic CLI with one command
- **subcommands/** - CLI with command hierarchy
- **interactive/** - CLI using prompts, progress, tables
- **plugin/** - CLI with custom plugins
- **forge_integration/** - CLI integrated with Forge App

## Testing

Run tests:
```bash
go test ./...
```

## Design

Follows the design specification in `v2/design/019-cli-framework.md`.

## License

See main Forge project license.

