// v2/cmd/forge/plugins/extension.go
package plugins

import (
	"fmt"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/internal/errors"
)

// ExtensionPlugin handles extension operations.
type ExtensionPlugin struct {
	config *config.ForgeConfig
}

// NewExtensionPlugin creates a new extension plugin.
func NewExtensionPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &ExtensionPlugin{config: cfg}
}

func (p *ExtensionPlugin) Name() string           { return "extension" }
func (p *ExtensionPlugin) Version() string        { return "1.0.0" }
func (p *ExtensionPlugin) Description() string    { return "Extension management tools" }
func (p *ExtensionPlugin) Dependencies() []string { return nil }
func (p *ExtensionPlugin) Initialize() error      { return nil }

func (p *ExtensionPlugin) Commands() []cli.Command {
	// Create main extension command with subcommands
	extensionCmd := cli.NewCommand(
		"extension",
		"Extension management tools",
		nil, // No handler, requires subcommand
		cli.WithAliases("ext"),
	)

	// Add subcommands
	extensionCmd.AddSubcommand(cli.NewCommand(
		"list",
		"List available Forge extensions",
		p.listExtensions,
		cli.WithAliases("ls"),
	))

	extensionCmd.AddSubcommand(cli.NewCommand(
		"info",
		"Show extension information",
		p.extensionInfo,
		cli.WithFlag(cli.NewStringFlag("name", "n", "Extension name", "")),
	))

	return []cli.Command{extensionCmd}
}

func (p *ExtensionPlugin) listExtensions(ctx cli.CommandContext) error {
	ctx.Info("Available Forge v2 Extensions:")
	ctx.Println("")

	table := ctx.Table()
	table.SetHeader([]string{"Extension", "Version", "Description", "Status"})

	extensions := []struct {
		name        string
		version     string
		description string
		installed   bool
	}{
		{"cache", "2.0.0", "Caching layer (Redis, Memcached, In-Memory)", true},
		{"database", "2.0.0", "Database integration (Postgres, MySQL, SQLite)", true},
		{"auth", "2.0.0", "Authentication & Authorization", true},
		{"mcp", "2.0.0", "Model Context Protocol integration", true},
		{"search", "2.0.0", "Full-text search (Elasticsearch, Meilisearch)", true},
		{"storage", "2.0.0", "Object storage (S3, GCS, Azure)", true},
		{"queue", "2.0.0", "Job queues (Redis, NATS, RabbitMQ)", true},
		{"email", "2.0.0", "Email sending (SMTP, SendGrid, Mailgun)", true},
		{"sms", "2.0.0", "SMS sending (Twilio, AWS SNS)", false},
		{"payment", "2.0.0", "Payment processing (Stripe, PayPal)", false},
		{"websocket", "2.0.0", "WebSocket connections", true},
		{"graphql", "2.0.0", "GraphQL API support", true},
		{"grpc", "2.0.0", "gRPC service support", true},
		{"orpc", "2.0.0", "oRPC protocol support", true},
		{"dashboard", "2.0.0", "Admin dashboard UI", true},
	}

	for _, ext := range extensions {
		status := cli.Green("✓ Included")
		if !ext.installed {
			status = cli.Yellow("○ Coming Soon")
		}

		table.AppendRow([]string{
			ext.name,
			ext.version,
			ext.description,
			status,
		})
	}

	table.Render()

	ctx.Println("")
	ctx.Info("To use an extension in your app:")
	ctx.Println("  import \"github.com/xraph/forge/extensions/{extension}\"")
	ctx.Println("")
	ctx.Info("Configure in .forge.yaml:")
	ctx.Println("  extensions:")
	ctx.Println("    cache:")
	ctx.Println("      driver: redis")
	ctx.Println("      url: redis://localhost:6379")

	return nil
}

func (p *ExtensionPlugin) extensionInfo(ctx cli.CommandContext) error {
	name := ctx.String("name")
	if name == "" {
		return errors.New("--name flag is required")
	}

	extensionInfo := map[string]struct {
		description string
		features    []string
		config      string
	}{
		"cache": {
			description: "Caching layer for improved performance",
			features: []string{
				"Multiple drivers: Redis, Memcached, In-Memory",
				"TTL support",
				"Tag-based invalidation",
				"Cache stampede prevention",
			},
			config: `extensions:
  cache:
    driver: redis
    url: redis://localhost:6379
    ttl: 1h`,
		},
		"database": {
			description: "Database integration with multiple drivers",
			features: []string{
				"PostgreSQL, MySQL, SQLite support",
				"Connection pooling",
				"Query builder",
				"Migration support",
			},
			config: `extensions:
  database:
    driver: postgres
    url: postgres://localhost:5432/mydb
    max_connections: 20`,
		},
		"mcp": {
			description: "Model Context Protocol for AI integration",
			features: []string{
				"Tools and resources support",
				"Multiple AI provider integration",
				"Context management",
				"Streaming responses",
			},
			config: `extensions:
  mcp:
    enabled: true
    base_path: /_/mcp`,
		},
	}

	info, exists := extensionInfo[name]
	if !exists {
		return fmt.Errorf("extension not found: %s. Use 'forge extension:list' to see available extensions", name)
	}

	ctx.Info("Extension: " + name)
	ctx.Println("")
	ctx.Println(info.description)
	ctx.Println("")

	ctx.Info("Features:")

	for _, feature := range info.features {
		ctx.Println("  •", feature)
	}

	ctx.Println("")
	ctx.Info("Configuration:")
	ctx.Println(info.config)

	ctx.Println("")
	ctx.Info("Import:")
	ctx.Println(fmt.Sprintf("  import \"github.com/xraph/forge/extensions/%s\"", name))

	return nil
}
