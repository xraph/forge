# Forge CLI - Quick Start Guide

Get started with the Forge CLI in minutes!

## Installation

```bash
cd v2/cmd/forge
make build

# The binary is now at: v2/bin/forge
```

Or install system-wide:
```bash
cd v2/cmd/forge
make install

# Now you can run: forge from anywhere
```

## Verify Installation

```bash
forge --help
forge version
forge doctor    # Check system requirements
```

## Create Your First Project

### Option 1: Interactive (Recommended)

```bash
# Create a new directory for your project
mkdir my-awesome-app
cd my-awesome-app

# Initialize (will prompt you for details)
forge init
```

### Option 2: Non-Interactive

```bash
# Single-module layout (traditional Go)
forge init --layout=single-module --name=my-app --module=github.com/me/my-app

# Multi-module layout (microservices)
forge init --layout=multi-module --name=my-services --module=github.com/me/my-services
```

## Generate Your First App

```bash
# Generate a new application
forge generate:app --name=api-gateway

# Or with alias
forge gen:app -n api-gateway
```

## Add Components

```bash
# Generate a controller
forge generate:controller --name=users --app=api-gateway

# Generate a model
forge generate:model --name=User --fields=name:string,email:string,age:int

# Generate a service
forge generate:service --name=auth
```

## Database Setup

```bash
# Create a migration
forge db:create --name=create_users_table

# Run migrations
forge db:migrate

# Check status
forge db:status
```

## Start Development

```bash
# Start the dev server (interactive selection)
forge dev

# Or specify the app
forge dev -a api-gateway

# With custom port
forge dev -a api-gateway -p 8080

# List available apps
forge dev:list
```

## Build and Deploy

```bash
# Build all apps
forge build

# Build specific app
forge build -a api-gateway

# Production build
forge build --production

# Deploy to staging
forge deploy -a api-gateway -e staging -t v1.0.0
```

## Common Workflows

### API Server Project

```bash
# Initialize
forge init --layout=single-module --template=api

# Generate app
forge gen:app -n api-gateway

# Add controllers
forge gen:controller -n users -a api-gateway
forge gen:controller -n products -a api-gateway

# Add models
forge gen:model -n User --fields=name:string,email:string
forge gen:model -n Product --fields=name:string,price:float64

# Setup database
forge db:create -n create_users_table
forge db:create -n create_products_table
forge db:migrate

# Start developing
forge dev -a api-gateway
```

### Microservices Project

```bash
# Initialize with multi-module layout
forge init --layout=multi-module

# Generate multiple services
forge gen:app -n api-gateway
forge gen:app -n auth-service
forge gen:app -n user-service
forge gen:app -n order-service

# Add shared services
forge gen:service -n email
forge gen:service -n notifications

# Build all
forge build --production

# Deploy to kubernetes
forge deploy:k8s -e staging
```

## Project Structure

### After `forge init --layout=single-module`

```
my-app/
├── .forge.yaml       # Your project config
├── go.mod
├── cmd/              # Will contain app entry points
├── apps/             # Will contain app-specific code
├── pkg/              # For shared libraries
├── internal/         # For private code
├── database/
│   ├── migrations/
│   └── seeds/
├── config/
└── deployments/
```

### After `forge generate:app --name=api-gateway`

```
my-app/
├── cmd/
│   └── api-gateway/
│       └── main.go   # ← Generated!
└── apps/
    └── api-gateway/
        ├── .forge.yaml
        └── internal/
            └── handlers/
```

## Tips & Tricks

### Run from Anywhere

The CLI automatically searches for `.forge.yaml` up the directory tree, so you can run commands from any subdirectory:

```bash
cd apps/api-gateway/internal
forge dev           # Still works!
```

### Interactive Features

Most commands support interactive mode when arguments are missing:

```bash
forge gen:app       # Will prompt for name
forge dev           # Will show app selector
```

### Check System Health

```bash
forge doctor        # Check Go, Docker, Git, etc.
forge doctor --verbose  # Detailed output
```

### View Available Extensions

```bash
forge extension:list
forge extension:info --name=cache
```

### Help is Always Available

```bash
forge --help
forge dev --help
forge generate:app --help
```

## Configuration

Your `.forge.yaml` file controls everything:

```yaml
project:
  name: "my-app"
  layout: "single-module"
  module: "github.com/me/my-app"

dev:
  auto_discover: true
  default_app: "api-gateway"

database:
  driver: "postgres"
  migrations_path: "./database/migrations"

extensions:
  cache:
    driver: "redis"
    url: "redis://localhost:6379"
```

See `examples/` directory for complete config examples.

## Troubleshooting

### Command not found

Make sure the binary is in your PATH:

```bash
export PATH=$PATH:$(pwd)/v2/bin
```

Or use the full path:

```bash
./v2/bin/forge --help
```

### No .forge.yaml found

Some commands require a `.forge.yaml` file. Run:

```bash
forge init
```

### Import errors

For single-module:
```bash
go mod tidy
```

For multi-module:
```bash
go work sync
```

## Next Steps

1. Read the full [README.md](./README.md)
2. Check example configs in `examples/`
3. Run `forge doctor` to ensure your system is ready
4. Start building! 🚀

## Get Help

- Run any command with `--help`
- Check `forge doctor` for diagnostics
- Read the comprehensive [README.md](./README.md)

---

**Happy coding with Forge!** 🔥

