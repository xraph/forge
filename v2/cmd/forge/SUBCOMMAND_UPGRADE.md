# Forge CLI - Subcommand Structure Upgrade

**Date:** October 23, 2025  
**Status:** ✅ **COMPLETE**

## Overview

Upgraded the Forge CLI from flat colon-separated commands to a proper hierarchical subcommand structure with full alias support.

## Changes Made

### Before (Flat Structure)

```bash
forge dev:list
forge dev:build
forge generate:app
forge generate:service
forge generate:extension
forge generate:controller
forge generate:model
forge db:migrate
forge db:rollback
forge db:status
forge db:create
forge db:seed
forge db:reset
forge deploy:docker
forge deploy:k8s
forge deploy:status
forge extension:list
forge extension:info
```

### After (Hierarchical Structure)

```bash
# Development
forge dev list
forge dev build

# Generation
forge generate app
forge generate service
forge generate extension
forge generate controller
forge generate model

# Database
forge db migrate
forge db rollback
forge db status
forge db create
forge db seed
forge db reset

# Deployment
forge deploy docker
forge deploy k8s
forge deploy status

# Extensions
forge extension list
forge extension info
```

## Alias Support

### Command-Level Aliases

```bash
# Generate command
forge generate app      # Full
forge gen app           # Alias
forge g app             # Short alias

# Database command
forge database migrate  # Full
forge db migrate        # Alias

# Extension command
forge extension list    # Full
forge ext list          # Alias
```

### Subcommand-Level Aliases

```bash
# List commands
forge dev list          # Full
forge dev ls            # Alias

forge extension list    # Full
forge ext ls            # Alias

# Migration commands
forge db migrate        # Full
forge db up             # Alias

forge db rollback       # Full
forge db down           # Alias

# Create commands
forge db create         # Full
forge db new            # Alias

# Generation subcommands
forge gen service       # Full
forge gen svc           # Alias

forge gen extension     # Full
forge gen ext           # Alias

forge gen controller    # Full
forge gen ctrl          # Alias
forge gen handler       # Alias (second alias)

# Kubernetes deployment
forge deploy k8s        # Full
forge deploy kubernetes # Alias
```

## Complete Alias Reference

| Command | Aliases | Usage Examples |
|---------|---------|----------------|
| `generate` | `gen`, `g` | `forge generate app` <br> `forge gen app` <br> `forge g app` |
| `database` | `db` | `forge database migrate` <br> `forge db migrate` |
| `extension` | `ext` | `forge extension list` <br> `forge ext list` |
| `dev list` | `dev ls` | `forge dev list` <br> `forge dev ls` |
| `db migrate` | `db up` | `forge db migrate` <br> `forge db up` |
| `db rollback` | `db down` | `forge db rollback` <br> `forge db down` |
| `db create` | `db new` | `forge db create` <br> `forge db new` |
| `gen service` | `gen svc` | `forge gen service` <br> `forge gen svc` |
| `gen extension` | `gen ext` | `forge gen extension` <br> `forge gen ext` |
| `gen controller` | `gen ctrl`, `gen handler` | `forge gen controller` <br> `forge gen ctrl` <br> `forge gen handler` |
| `deploy k8s` | `deploy kubernetes` | `forge deploy k8s` <br> `forge deploy kubernetes` |
| `ext list` | `ext ls` | `forge extension list` <br> `forge ext ls` |

## Benefits

### 1. **More Conventional**
Matches modern CLI tools like `kubectl`, `docker`, `git`, etc.

```bash
kubectl get pods       # Not: kubectl:get:pods
docker compose up      # Not: docker:compose:up
git branch list        # Not: git:branch:list
```

### 2. **Better Organization**
Clear hierarchy shows relationships between commands:

```bash
forge generate         # Parent command
  ├── app             # Subcommand
  ├── service         # Subcommand
  ├── extension       # Subcommand
  ├── controller      # Subcommand
  └── model           # Subcommand
```

### 3. **Improved Help System**
Help is now hierarchical:

```bash
forge --help           # Shows main commands
forge generate --help  # Shows generate subcommands
forge gen app --help   # Shows app-specific flags
```

### 4. **Flexible Usage**
Multiple ways to run the same command:

```bash
# Explicit and clear
forge generate app --name=my-app

# Quick with aliases
forge gen app -n my-app

# Super quick
forge g app -n my-app
```

### 5. **Easy to Remember**
Natural language structure:

```bash
forge db migrate      # "forge, database, migrate"
forge gen app         # "forge, generate, app"
forge dev list        # "forge, development, list"
```

## Implementation Details

### Plugin Changes

Each plugin now returns a single parent command with subcommands:

**Before:**
```go
func (p *DevPlugin) Commands() []cli.Command {
	return []cli.Command{
		cli.NewCommand("dev", ...),
		cli.NewCommand("dev:list", ...),
		cli.NewCommand("dev:build", ...),
	}
}
```

**After:**
```go
func (p *DevPlugin) Commands() []cli.Command {
	devCmd := cli.NewCommand(
		"dev",
		"Development server and tools",
		p.runDev,
	)
	
	devCmd.AddSubcommand(cli.NewCommand(
		"list",
		"List available apps",
		p.listApps,
		cli.WithAliases("ls"),
	))
	
	devCmd.AddSubcommand(cli.NewCommand(
		"build",
		"Build app for development",
		p.buildDev,
	))
	
	return []cli.Command{devCmd}
}
```

### Testing Results

All command variations tested and working:

```bash
✓ forge generate app --help
✓ forge gen app --help
✓ forge g app --help
✓ forge dev list
✓ forge dev ls
✓ forge db migrate
✓ forge db up
✓ forge ext list
✓ forge ext ls
✓ forge gen ctrl --help
✓ forge gen handler --help
✓ forge deploy k8s
✓ forge deploy kubernetes
```

## Migration Guide

For users of the previous version:

### Old Command → New Command

```bash
# Development
forge dev:list        → forge dev list
forge dev:build       → forge dev build

# Generation
forge generate:app    → forge generate app (or: forge gen app)
forge gen:service     → forge gen service
forge gen:ext         → forge gen extension
forge gen:ctrl        → forge gen controller

# Database
forge db:migrate      → forge db migrate (or: forge db up)
forge db:rollback     → forge db rollback (or: forge db down)
forge db:status       → forge db status
forge db:create       → forge db create (or: forge db new)

# Deployment
forge deploy:docker   → forge deploy docker
forge deploy:k8s      → forge deploy k8s
forge deploy:status   → forge deploy status

# Extensions
forge extension:list  → forge extension list (or: forge ext list)
forge ext:info        → forge ext info
```

## Documentation Updates

All documentation has been updated:

- ✅ [README.md](./README.md) - Updated all examples
- ✅ [QUICK_START.md](./QUICK_START.md) - New command structure
- ✅ [COMMANDS.md](./COMMANDS.md) - Complete command reference
- ✅ [IMPLEMENTATION_SUMMARY.md](./IMPLEMENTATION_SUMMARY.md) - Technical details

## Backward Compatibility

**Note:** The old colon-separated commands are no longer supported. This is a breaking change but aligns with CLI best practices and modern conventions.

If you have scripts using the old format, update them:

```bash
# Old (will not work)
forge generate:app --name=my-app

# New (works)
forge generate app --name=my-app
forge gen app -n my-app
forge g app -n my-app
```

## Examples

### Creating a New Project

```bash
# All these work:
forge generate app --name=api-gateway
forge gen app --name=api-gateway
forge g app -n api-gateway
```

### Database Migrations

```bash
# All these work:
forge db migrate
forge db up
forge database migrate
```

### Listing Extensions

```bash
# All these work:
forge extension list
forge ext list
forge ext ls
```

### Building for Dev

```bash
# Standard
forge dev build -a api-gateway

# With list first
forge dev list
forge dev build -a api-gateway
```

## Conclusion

The hierarchical subcommand structure with comprehensive alias support provides:

✅ **Better UX** - More intuitive and conventional  
✅ **Flexibility** - Multiple ways to run commands  
✅ **Discoverability** - Clear hierarchy in help system  
✅ **Modern** - Matches industry standards  
✅ **Powerful** - Full alias support at all levels  

The Forge CLI is now more polished, professional, and user-friendly! 🎉

---

**Next:** Try the new commands with `forge --help` or check the [COMMANDS.md](./COMMANDS.md) reference.

