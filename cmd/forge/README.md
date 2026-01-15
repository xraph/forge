# Forge CLI

Enterprise-grade command-line interface for Forge framework.

## Module Structure

The Forge CLI (`cmd/forge`) is a **separate Go module** from the main Forge framework. This separation is necessary because the CLI depends on `github.com/xraph/forge/extensions/database`, which creates a circular dependency if included in the main module.

The module structure:
- **Main module**: `github.com/xraph/forge` - Core framework
- **Database extension**: `github.com/xraph/forge/extensions/database` - Separate module
- **Forge CLI**: `github.com/xraph/forge/cmd/forge` - Separate module (depends on database extension)

## Installation

```bash
go install github.com/xraph/forge/cmd/forge@latest
```

Or build from source:

```bash
cd cmd/forge
go build -o forge .
```

## Quick Start

```bash
# Initialize a new project
forge init

# Generate an app
forge generate app --name=my-app

# Start development server
forge dev

# Build for production
forge build --production

# Run database migrations
forge db migrate

# Deploy to Kubernetes
forge deploy --env=staging
```

## Project Layouts

Forge CLI supports two project layouts:

### Single-Module (Recommended for most projects)

Traditional Go layout with one `go.mod` file:

```
my-project/
├── .forge.yaml
├── go.mod
├── cmd/              # Entry points
├── apps/             # App-specific code
├── pkg/              # Shared libraries
├── internal/         # Private shared code
└── database/         # Migrations & seeds
```

**Best for:**
- Small to medium projects
- Single team
- Shared dependencies
- Faster development cycle

### Multi-Module (For large teams)

Microservices layout with multiple `go.mod` files:

```
my-project/
├── .forge.yaml
├── go.work
├── apps/             # Independent apps
│   ├── api-gateway/
│   │   └── go.mod
│   └── auth-service/
│       └── go.mod
├── services/         # Shared services
└── pkg/              # Common libraries
```

**Best for:**
- Large projects (10+ services)
- Multiple teams
- Independent versioning
- Different release cycles

## Commands

### Project Management

```bash
# Initialize new project
forge init
forge init --layout=single-module --template=api

# Check system requirements
forge doctor
forge doctor --verbose
```

### Development

```bash
# Start development server
forge dev                    # Interactive app selection
forge dev -a api-gateway     # Run specific app
forge dev -a api-gateway -p 8080 --watch

# List available apps
forge dev list

# Build for development
forge dev build -a my-app
```

### Code Generation

```bash
# Generate application
forge generate app --name=api-gateway --template=api
forge gen app -n auth-service
forge g app -n auth-service  # Short alias

# Generate service
forge generate service --name=users
forge gen service -n billing
forge g service -n billing  # Short alias

# Generate extension
forge generate extension --name=payment
forge gen ext -n notifications
forge g ext -n notifications  # Short alias

# Generate controller/handler
forge generate controller --name=users --app=api-gateway
forge gen ctrl -n products -a api-gateway
forge g ctrl -n products -a api-gateway  # Short alias

# Generate model
forge generate model --name=User --fields=name:string,email:string,age:int
forge gen model -n Product -f name:string -f price:float64
forge g model -n Product -f name:string -f price:float64  # Short alias
```

### Database

```bash
# Run migrations
forge db migrate                 # All migrations
forge db migrate --env=staging
forge db migrate --steps=1       # Run one migration

# Rollback migrations
forge db rollback
forge db rollback --steps=2

# Show migration status
forge db status
forge db status --env=production

# Create new migration
forge db create --name=add_users_table
forge db create -n create_products

# Seed database
forge db seed
forge db seed --file=dev_users.sql

# Reset database
forge db reset --env=dev
forge db reset --env=production --force
```

### Build

```bash
# Build all apps
forge build

# Build specific app
forge build -a api-gateway

# Build for specific platform
forge build --platform=linux/amd64
forge build --platform=darwin/arm64

# Production build
forge build --production

# Custom output directory
forge build -o ./dist
```

### Deployment

```bash
# Full deployment
forge deploy -a api-gateway -e staging -t v1.2.3

# Build and push Docker image
forge deploy docker -a api-gateway -t latest
forge deploy docker -a auth-service -t v2.0.0

# Deploy to Kubernetes
forge deploy k8s -e production
forge deploy k8s -e staging --namespace=my-namespace

# Show deployment status
forge deploy status
forge deploy status --env=production
```

### Extensions

```bash
# List available extensions
forge extension list
forge ext list

# Show extension info
forge extension info --name=cache
forge ext info -n database
```

## Configuration

### `.forge.yaml`

The `.forge.yaml` file configures your Forge project. It's automatically searched up the directory tree, so you can run `forge` commands from any subdirectory.

See example configurations:
- [single-module.forge.yaml](./examples/single-module.forge.yaml)
- [multi-module.forge.yaml](./examples/multi-module.forge.yaml)

### Key Configuration Sections

#### Project

```yaml
project:
  name: "my-project"
  version: "1.0.0"
  layout: "single-module"  # or "multi-module"
  module: "github.com/myorg/my-project"
```

#### Development

```yaml
dev:
  auto_discover: true
  default_app: "api-gateway"
  watch:
    enabled: true
    paths:
      - "./apps/**/*.go"
```

#### Database

```yaml
database:
  driver: "postgres"
  migrations_path: "./database/migrations"
  connections:
    dev:
      url: "postgres://localhost:5432/mydb"
```

#### Build

```yaml
build:
  output_dir: "./bin"
  apps:
    - name: "api-gateway"
      cmd: "./cmd/api-gateway"
      output: "api-gateway"
```

#### Extensions

```yaml
extensions:
  cache:
    driver: "redis"
    url: "redis://localhost:6379"
  
  database:
    driver: "postgres"
    url: "${DATABASE_URL}"
```

## Environment Variables

Forge CLI supports environment variable substitution in `.forge.yaml`:

```yaml
database:
  connections:
    production:
      url: "${DATABASE_URL}"  # Replaced with env var

extensions:
  auth:
    jwt_secret: "${JWT_SECRET}"
```

## Plugin System

Forge CLI is built on a plugin architecture. Current plugins:

- **Init Plugin** - Project initialization
- **Dev Plugin** - Development server
- **Generate Plugin** - Code generation
- **Database Plugin** - Database management
- **Build Plugin** - Application building
- **Deploy Plugin** - Deployment automation
- **Extension Plugin** - Extension management
- **Doctor Plugin** - System diagnostics

## Examples

### Create a new API project

```bash
# Initialize project
forge init --layout=single-module --template=api

# Generate API app
forge generate app --name=api-gateway

# Generate controllers
forge generate controller --name=users --app=api-gateway
forge generate controller --name=products --app=api-gateway

# Generate models
forge generate model --name=User --fields=name:string,email:string
forge generate model --name=Product --fields=name:string,price:float64

# Create database migrations
forge db create --name=create_users_table
forge db create --name=create_products_table

# Run migrations
forge db migrate

# Start development
forge dev -a api-gateway
```

### Create a microservices project

```bash
# Initialize with multi-module layout
forge init --layout=multi-module --template=microservices

# Generate services
forge generate app --name=api-gateway
forge generate app --name=auth-service
forge generate app --name=user-service
forge generate app --name=order-service

# Start a service
forge dev -a auth-service

# Build all services
forge build --production

# Deploy to staging
forge deploy k8s --env=staging
```

## Troubleshooting

### Command not found

Make sure `$GOPATH/bin` is in your PATH:

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

### No .forge.yaml found

Run `forge init` in your project root to create the configuration file.

### Permission denied

Ensure the forge binary is executable:

```bash
chmod +x $(which forge)
```

### Import errors

For single-module projects, run:

```bash
go mod tidy
```

For multi-module projects, run:

```bash
go work sync
```

## Contributing

Contributions welcome! See the [main repository](https://github.com/xraph/forge) for details.

## License

MIT License - see LICENSE file for details.

## Support

- Documentation: https://forge.dev
- Issues: https://github.com/xraph/forge/issues
- Discord: https://discord.gg/forge

---

**Forge** - Enterprise-grade backend framework for Go

