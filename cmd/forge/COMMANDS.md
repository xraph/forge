# Forge CLI Commands Reference

Complete reference for all Forge CLI commands with examples and aliases.

## Command Structure

Forge CLI uses a hierarchical subcommand structure for better organization:

```bash
forge <command> <subcommand> [flags]
```

Most commands support aliases for faster typing:

```bash
forge generate app       # Full command
forge gen app            # Alias
forge g app              # Short alias
```

---

## Core Commands

### `forge init`

Initialize a new Forge project.

```bash
forge init
forge init --layout=single-module
forge init --name=my-app --module=github.com/me/my-app
forge init --template=api --git
```

**Flags:**
- `-n, --name` - Project name
- `-m, --module` - Go module path
- `-l, --layout` - Project layout (single-module, multi-module)
- `-t, --template` - Project template (basic, api, microservices, fullstack)
- `-g, --git` - Initialize git repository
- `-f, --force` - Force init even if directory is not empty

---

## Development Commands

### `forge dev`

Start development server with hot reload support.

```bash
forge dev                    # Interactive app selection with hot reload
forge dev -a api-gateway     # Run specific app with hot reload
forge dev -a api -p 8080     # Run on specific port with hot reload
forge dev --watch=false      # Disable hot reload
```

**Flags:**
- `-a, --app` - App to run
- `-w, --watch` - Watch for changes and auto-reload (default: true)
- `-p, --port` - Port number

**Hot Reload Features:**
- Automatically watches `.go` files in app, `internal/`, and `pkg/` directories
- Intelligently filters changes (ignores test files, temporary files, hidden files)
- Debounces rapid file changes (300ms window) to prevent excessive restarts
- Gracefully terminates and restarts processes on code changes
- Clean shutdown on Ctrl+C with proper resource cleanup

**What Triggers Reload:**
- Modifications to `.go` files (write, create, delete)
- Files in watched directories: `cmd/`, `internal/`, `pkg/`

**What's Ignored:**
- Test files (`*_test.go`)
- Hidden files and directories (`.git`, `.idea`, etc.)
- Vendor and node_modules
- Build artifacts (`bin/`, `dist/`, `tmp/`)
- Editor temporary files (`.swp`, `~`, etc.)

For detailed information about hot reload implementation, see [HOT_RELOAD.md](./HOT_RELOAD.md).

#### Subcommands

**`forge dev list`** (aliases: `ls`)

List all available apps.

```bash
forge dev list
forge dev ls
```

**`forge dev build`**

Build app for development.

```bash
forge dev build -a api-gateway
```

---

## Generation Commands

### `forge generate` (aliases: `gen`, `g`)

Generate code from templates.

```bash
forge generate app --name=my-app
forge gen service -n auth
forge g model -n User --fields=name:string
```

#### Subcommands

**`forge generate app`**

Generate a new application.

```bash
forge generate app --name=api-gateway --template=api
forge gen app -n auth-service
forge g app -n my-app
```

**Flags:**
- `-n, --name` - App name (required)
- `-t, --template` - Template (basic, api, grpc, worker, cli)

**`forge generate service`** (aliases: `svc`)

Generate a new service with interactive directory and target selection.

```bash
forge generate service --name=users
forge gen service -n billing
forge gen svc -n auth
```

**Features:**

The service generation command now provides interactive selection for:

1. **Service Location** - Choose where to place the service:
   - `pkg` - Main package directory
   - `internal` - Internal packages directory
   - Any extension directory (e.g., `cache`, `database`, `auth`)

2. **Optional Controller** - After service creation, you can add a controller to:
   - Any app in your project
   - Any extension in your project

**Flags:**
- `-n, --name` - Service name (required)

**Example Interactive Session:**

```bash
$ forge gen service -n UserService
? Service name: UserService
? Select directory for service: [pkg | internal | auth-ext | cache-ext]
✓ Service UserService created in pkg!
? Add controller to an app or extension? [y/n]
? Select app or extension to add controller: [📦 api | 📦 web | 🔌 auth]
? Controller name: users
✓ Controller users added to api!
```

**`forge generate extension`** (aliases: `ext`)

Generate a new extension.

```bash
forge generate extension --name=payment
forge gen ext -n notifications
```

**Flags:**
- `-n, --name` - Extension name (required)

**`forge generate controller`** (aliases: `ctrl`, `handler`)

Generate a controller for an app or extension with flexible placement options.

The controller generation command now provides interactive selection for:

1. **Target Selection** - Choose where to add the controller:
   - Any app in your project (📦)
   - Any extension in your project (🔌)

2. **Placement Options** - For extensions, choose where to place the controller:
   - `internal/controllers` (recommended) - Follows standard project structure
   - `root of extension` - Places controller directly in extension directory

Apps always use `internal/controllers` (no choice needed).

```bash
# Interactive mode - prompts for all options
forge generate controller

# With name specified
forge generate controller --name=users

# With target specified (works for both apps and extensions)
forge gen ctrl -n products -a api-gateway
forge gen controller -n cache -a cache-extension
```

**Flags:**
- `-n, --name` - Controller name (required if not prompted)
- `-a, --app` - Target app or extension name (optional, prompts if not provided)

**Example Interactive Session:**

```bash
$ forge gen controller -n UserController
? Controller name: UserController
? Select app or extension: [📦 api | 📦 web | 🔌 cache | 🔌 auth]
  > 🔌 cache (extension)
? Where to place the controller in extension: [internal/controllers (recommended) | root of extension]
  > internal/controllers (recommended)
✓ Controller UserController created at extensions/cache/internal/controllers/user_controller.go
```

**`forge generate model`**

Generate a database model.

```bash
forge generate model --name=User --fields=name:string,email:string,age:int
forge gen model -n Product -f name:string -f price:float64 -f stock:int
```

**Flags:**
- `-n, --name` - Model name (required)
- `-f, --fields` - Fields in format name:type (can be repeated)

---

## Database Commands

### `forge db` (aliases: `database`)

Database management tools.

```bash
forge db migrate
forge db status
forge database migrate --env=staging
```

#### Subcommands

**`forge db migrate`** (aliases: `up`)

Run database migrations.

```bash
forge db migrate                 # All pending migrations
forge db migrate --env=staging   # Specific environment
forge db migrate --steps=1       # Run one migration
forge db up --steps=2            # Using alias
```

**Flags:**
- `-e, --env` - Environment (default: dev)
- `-s, --steps` - Number of steps (0 = all)

**`forge db rollback`** (aliases: `down`)

Rollback database migrations.

```bash
forge db rollback                # Rollback one migration
forge db rollback --steps=2      # Rollback two migrations
forge db down                    # Using alias
```

**Flags:**
- `-s, --steps` - Steps to rollback (default: 1)

**`forge db status`**

Show migration status.

```bash
forge db status
forge db status --env=production
```

**Flags:**
- `-e, --env` - Environment (default: dev)

**`forge db create`** (aliases: `new`)

Create a new migration.

```bash
forge db create --name=add_users_table
forge db create -n create_products
forge db new -n add_index_to_users
```

**Flags:**
- `-n, --name` - Migration name (required)

**`forge db seed`**

Seed the database.

```bash
forge db seed                    # All seed files
forge db seed --file=users.sql   # Specific file
forge db seed --env=dev          # Specific environment
```

**Flags:**
- `-e, --env` - Environment (default: dev)
- `-f, --file` - Specific seed file

**`forge db reset`**

Reset the database.

```bash
forge db reset --env=dev                 # Development environment
forge db reset --env=production --force  # Force production reset
```

**Flags:**
- `-e, --env` - Environment (default: dev)
- `--force` - Force reset (required for production)

---

## Build Commands

### `forge build`

Build applications.

```bash
forge build                         # Build all apps
forge build -a api-gateway          # Build specific app
forge build --platform=linux/amd64  # Cross-platform build
forge build --production            # Production build
forge build -o ./dist               # Custom output
```

**Flags:**
- `-a, --app` - App to build (empty = all)
- `-p, --platform` - Target platform (os/arch)
- `--production` - Production build
- `-o, --output` - Output directory

---

## Deployment Commands

### `forge deploy`

Deploy applications.

```bash
forge deploy -a api-gateway -e staging -t v1.2.3
forge deploy -a auth-service -e production --tag=latest
```

**Flags:**
- `-a, --app` - App to deploy
- `-e, --env` - Environment
- `-t, --tag` - Image tag

#### Subcommands

**`forge deploy docker`**

Build and push Docker image.

```bash
forge deploy docker -a api-gateway -t v1.0.0
forge deploy docker -a auth-service --tag=latest
```

**Flags:**
- `-a, --app` - App to build (required)
- `-t, --tag` - Image tag

**`forge deploy k8s`** (aliases: `kubernetes`)

Deploy to Kubernetes.

```bash
forge deploy k8s -e staging
forge deploy k8s -e production --namespace=prod
forge deploy kubernetes -e dev
```

**Flags:**
- `-e, --env` - Environment
- `-n, --namespace` - Kubernetes namespace

**`forge deploy status`**

Show deployment status.

```bash
forge deploy status
forge deploy status --env=production
```

**Flags:**
- `-e, --env` - Environment

---

## Extension Commands

### `forge extension` (aliases: `ext`)

Extension management tools.

```bash
forge extension list
forge ext info --name=cache
```

#### Subcommands

**`forge extension list`** (aliases: `ls`)

List available Forge extensions.

```bash
forge extension list
forge ext list
forge ext ls
```

**`forge extension info`**

Show extension information.

```bash
forge extension info --name=cache
forge ext info -n database
forge ext info -n mcp
```

**Flags:**
- `-n, --name` - Extension name (required)

---

## System Commands

### `forge doctor`

Check system requirements and project health.

```bash
forge doctor              # Standard check
forge doctor --verbose    # Detailed output
```

**Flags:**
- `-v, --verbose` - Show verbose output

### `forge version`

Show version information.

```bash
forge version
```

---

## Global Flags

Available for all commands:

- `-h, --help` - Show help
- `-v, --version` - Show version

---

## Aliases Quick Reference

| Full Command | Aliases | Example |
|-------------|---------|---------|
| `generate` | `gen`, `g` | `forge gen app` |
| `database` | `db` | `forge db migrate` |
| `extension` | `ext` | `forge ext list` |
| `dev list` | `dev ls` | `forge dev ls` |
| `db migrate` | `db up` | `forge db up` |
| `db rollback` | `db down` | `forge db down` |
| `db create` | `db new` | `forge db new` |
| `deploy k8s` | `deploy kubernetes` | `forge deploy k8s` |
| `extension list` | `ext ls` | `forge ext ls` |
| `generate service` | `gen svc` | `forge gen svc` |
| `generate extension` | `gen ext` | `forge gen ext` |
| `generate controller` | `gen ctrl`, `gen handler` | `forge gen ctrl` |

---

## Example Workflows

### Create a New API

```bash
forge init --layout=single-module --template=api
forge gen app -n api-gateway
forge gen ctrl -n users -a api-gateway
forge gen ctrl -n products -a api-gateway
forge gen model -n User --fields=name:string,email:string
forge gen model -n Product --fields=name:string,price:float64
forge db create -n create_users_table
forge db create -n create_products_table
forge db migrate
forge dev -a api-gateway
```

### Microservices Deployment

```bash
forge init --layout=multi-module
forge gen app -n api-gateway
forge gen app -n auth-service
forge gen app -n user-service
forge build --production
forge deploy docker -a api-gateway -t v1.0.0
forge deploy docker -a auth-service -t v1.0.0
forge deploy k8s -e staging
forge deploy status --env=staging
```

### Database Management

```bash
forge db status
forge db create -n add_users_table
forge db migrate
forge db seed
forge db status
forge db rollback --steps=1
```

---

## Infrastructure Deployment Commands

### `forge infra`

Deploy applications using various infrastructure providers.

See [INFRASTRUCTURE_DEPLOYMENT.md](./INFRASTRUCTURE_DEPLOYMENT.md) for complete guide.

#### Docker Commands

**`forge infra docker deploy`**

Deploy using Docker and Docker Compose.

```bash
forge infra docker deploy                    # Deploy all services
forge infra docker deploy -s api-service     # Deploy specific service
forge infra docker deploy -e prod            # Deploy to production
forge infra docker deploy -b                 # Force rebuild images
```

**Flags:**
- `-s, --service` - Service to deploy (default: all)
- `-e, --env` - Environment (default: dev)
- `-b, --build` - Force rebuild images

**`forge infra docker export`**

Export Docker configuration to deployments folder.

```bash
forge infra docker export                    # Export to deployments/docker/
forge infra docker export -o ./infra/docker  # Export to custom path
forge infra docker export -f                 # Force overwrite
```

**Flags:**
- `-o, --output` - Output directory
- `-f, --force` - Force overwrite existing files

#### Kubernetes Commands

**`forge infra k8s deploy`** (aliases: `kubernetes`)

Deploy to Kubernetes cluster.

```bash
forge infra k8s deploy                       # Deploy all services
forge infra k8s deploy -s api-service        # Deploy specific service
forge infra k8s deploy -e prod               # Deploy to production
forge infra k8s deploy -n production         # Deploy to namespace
forge infra k8s deploy --dry-run             # Preview changes
```

**Flags:**
- `-s, --service` - Service to deploy (default: all)
- `-e, --env` - Environment (default: dev)
- `-n, --namespace` - Kubernetes namespace
- `--dry-run` - Perform a dry run

**`forge infra k8s export`**

Export Kubernetes manifests.

```bash
forge infra k8s export                       # Export to deployments/k8s/
forge infra k8s export -o ./k8s              # Export to custom path
forge infra k8s export -f                    # Force overwrite
```

**Flags:**
- `-o, --output` - Output directory
- `-f, --force` - Force overwrite existing files

#### Digital Ocean Commands

**`forge infra do deploy`** (aliases: `digitalocean`)

Deploy to Digital Ocean App Platform.

```bash
forge infra do deploy                        # Deploy all services
forge infra do deploy -s api-service         # Deploy specific service
forge infra do deploy -e prod                # Deploy to production
forge infra do deploy -r nyc1                # Deploy to region
```

**Flags:**
- `-s, --service` - Service to deploy (default: all)
- `-e, --env` - Environment (default: dev)
- `-r, --region` - Digital Ocean region

**`forge infra do export`**

Export Digital Ocean configuration.

```bash
forge infra do export                        # Export to deployments/do/
forge infra do export -f                     # Force overwrite
```

**Flags:**
- `-o, --output` - Output directory
- `-f, --force` - Force overwrite existing files

#### Render Commands

**`forge infra render deploy`**

Deploy to Render.com.

```bash
forge infra render deploy                    # Deploy all services
forge infra render deploy -s api-service     # Deploy specific service
forge infra render deploy -e prod            # Deploy to production
```

**Flags:**
- `-s, --service` - Service to deploy (default: all)
- `-e, --env` - Environment (default: dev)

**`forge infra render export`**

Export Render.com configuration.

```bash
forge infra render export                    # Export to deployments/render/
forge infra render export -f                 # Force overwrite
```

**Flags:**
- `-o, --output` - Output directory
- `-f, --force` - Force overwrite existing files

---

## Forge Cloud Commands

### `forge cloud`

Deploy and manage applications on Forge Cloud platform.

See [INFRASTRUCTURE_DEPLOYMENT.md](./INFRASTRUCTURE_DEPLOYMENT.md) for complete guide.

#### Authentication

**`forge cloud login`**

Authenticate with Forge Cloud.

```bash
forge cloud login                            # Login with browser
forge cloud login -t YOUR_TOKEN              # Login with token
```

**Flags:**
- `-t, --token` - API token

**`forge cloud logout`**

Log out from Forge Cloud.

```bash
forge cloud logout
```

#### Deployment

**`forge cloud deploy`**

Deploy services to Forge Cloud.

```bash
forge cloud deploy                           # Deploy all services
forge cloud deploy -s api-service            # Deploy specific service
forge cloud deploy -e prod                   # Deploy to production
forge cloud deploy -r us-east-1              # Deploy to region
forge cloud deploy -w                        # Watch deployment progress
```

**Flags:**
- `-s, --service` - Service to deploy (default: all)
- `-e, --env` - Environment (default: dev)
- `-r, --region` - Deployment region
- `-w, --watch` - Watch deployment progress

#### Status & Monitoring

**`forge cloud status`**

Show deployment status.

```bash
forge cloud status                           # View all services
forge cloud status -e prod                   # Check production status
forge cloud status -s api-service            # Filter by service
forge cloud status -w                        # Watch status updates
```

**Flags:**
- `-e, --env` - Environment (default: dev)
- `-s, --service` - Filter by service
- `-w, --watch` - Watch status updates

**`forge cloud logs`**

View service logs.

```bash
forge cloud logs -s api-service              # View service logs
forge cloud logs -s api-service -f           # Follow logs
forge cloud logs -s api-service -n 500       # Last 500 lines
forge cloud logs -s api -e prod -f           # Follow prod logs
```

**Flags:**
- `-s, --service` - Service name (required)
- `-e, --env` - Environment (default: dev)
- `-f, --follow` - Follow log output
- `-n, --tail` - Number of lines to show (default: 100)

#### Management

**`forge cloud rollback`**

Rollback to previous deployment.

```bash
forge cloud rollback -s api-service          # Rollback to previous
forge cloud rollback -s api -v v1.2.3        # Rollback to version
forge cloud rollback -s api -e prod          # Rollback in production
```

**Flags:**
- `-s, --service` - Service to rollback (required)
- `-e, --env` - Environment (default: dev)
- `-v, --version` - Version to rollback to

**`forge cloud scale`**

Scale service instances.

```bash
forge cloud scale -s api-service -r 3        # Scale to 3 instances
forge cloud scale -s api -e prod -r 5        # Scale prod to 5 instances
```

**Flags:**
- `-s, --service` - Service to scale (required)
- `-e, --env` - Environment (default: dev)
- `-r, --replicas` - Number of replicas (default: 1)

---

## Tips

1. **Tab Completion**: Most shells support tab completion. Run `forge completion bash` or `forge completion zsh` to enable it.

2. **Help Everywhere**: Add `--help` to any command to see detailed usage:
   ```bash
   forge --help
   forge generate --help
   forge generate app --help
   ```

3. **Interactive Mode**: Most commands prompt for missing required arguments:
   ```bash
   forge gen app    # Will prompt for name
   forge dev        # Will show app selector
   ```

4. **Aliases**: Use shorter aliases for faster typing:
   ```bash
   forge g app -n my-app     # Instead of forge generate app
   forge db up               # Instead of forge db migrate
   forge ext ls              # Instead of forge extension list
   ```

5. **Config Search**: Run commands from any subdirectory - `.forge.yaml` is found automatically.

---

For more information, see the [README.md](./README.md) or [QUICK_START.md](./QUICK_START.md).

