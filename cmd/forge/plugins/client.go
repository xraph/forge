// v2/cmd/forge/plugins/client.go
package plugins

import (
	"context"
	"fmt"
	"os"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

// ClientPlugin handles client code generation
type ClientPlugin struct {
	config *config.ForgeConfig
}

// NewClientPlugin creates a new client plugin
func NewClientPlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &ClientPlugin{config: cfg}
}

func (p *ClientPlugin) Name() string           { return "client" }
func (p *ClientPlugin) Version() string        { return "1.0.0" }
func (p *ClientPlugin) Description() string    { return "Client code generation tools" }
func (p *ClientPlugin) Dependencies() []string { return nil }
func (p *ClientPlugin) Initialize() error      { return nil }

func (p *ClientPlugin) Commands() []cli.Command {
	// Create main client command
	clientCmd := cli.NewCommand(
		"client",
		"Client generation commands",
		nil, // No handler, requires subcommand
	)

	// Add subcommands
	clientCmd.AddSubcommand(cli.NewCommand(
		"generate",
		"Generate a client from API specification",
		p.generateClient,
		cli.WithAliases("gen", "g"),
		cli.WithFlag(cli.NewStringFlag("from-spec", "s", "Path to OpenAPI/AsyncAPI spec file", "")),
		cli.WithFlag(cli.NewStringFlag("language", "l", "Target language (go, typescript)", "go")),
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "./client")),
		cli.WithFlag(cli.NewStringFlag("package", "p", "Package/module name", "client")),
		cli.WithFlag(cli.NewStringFlag("base-url", "b", "API base URL", "")),
		cli.WithFlag(cli.NewStringFlag("module", "m", "Go module path (for Go only)", "")),
		cli.WithFlag(cli.NewBoolFlag("auth", "", "Include authentication", true)),
		cli.WithFlag(cli.NewBoolFlag("streaming", "", "Include streaming (WebSocket/SSE)", true)),
		cli.WithFlag(cli.NewBoolFlag("reconnection", "", "Enable reconnection", true)),
		cli.WithFlag(cli.NewBoolFlag("heartbeat", "", "Enable heartbeat", true)),
		cli.WithFlag(cli.NewBoolFlag("state-management", "", "Enable state management", true)),
	))

	clientCmd.AddSubcommand(cli.NewCommand(
		"list",
		"List endpoints from specification",
		p.listEndpoints,
		cli.WithFlag(cli.NewStringFlag("from-spec", "s", "Path to OpenAPI/AsyncAPI spec file", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Filter by type (rest, ws, sse)", "")),
	))

	clientCmd.AddSubcommand(cli.NewCommand(
		"init",
		"Initialize client generation configuration",
		p.initConfig,
	))

	return []cli.Command{clientCmd}
}

func (p *ClientPlugin) generateClient(ctx cli.CommandContext) error {
	// Get flags
	fromSpec := ctx.String("from-spec")
	language := ctx.String("language")
	outputDir := ctx.String("output")
	packageName := ctx.String("package")
	baseURL := ctx.String("base-url")
	module := ctx.String("module")
	includeAuth := ctx.Bool("auth")
	includeStreaming := ctx.Bool("streaming")
	reconnection := ctx.Bool("reconnection")
	heartbeat := ctx.Bool("heartbeat")
	stateManagement := ctx.Bool("state-management")

	// Validate required flags
	if fromSpec == "" {
		return cli.NewError("--from-spec is required", cli.ExitUsageError)
	}

	// Create generator
	gen := client.NewGenerator()

	// Register language generators
	if err := gen.Register(golang.NewGenerator()); err != nil {
		return fmt.Errorf("register Go generator: %w", err)
	}
	if err := gen.Register(typescript.NewGenerator()); err != nil {
		return fmt.Errorf("register TypeScript generator: %w", err)
	}

	// Create config
	genConfig := client.GeneratorConfig{
		Language:         language,
		OutputDir:        outputDir,
		PackageName:      packageName,
		APIName:          "Client",
		BaseURL:          baseURL,
		Module:           module,
		IncludeAuth:      includeAuth,
		IncludeStreaming: includeStreaming,
		Version:          "1.0.0",
		Features: client.Features{
			Reconnection:    reconnection,
			Heartbeat:       heartbeat,
			StateManagement: stateManagement,
			TypedErrors:     true,
			RequestRetry:    false,
			Timeout:         true,
		},
	}

	// Validate config
	if err := genConfig.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	ctx.Info(fmt.Sprintf("Generating %s client from %s...", language, fromSpec))
	spinner := ctx.Spinner("Parsing specification...")

	// Generate from file
	generatedClient, err := gen.GenerateFromFile(context.Background(), fromSpec, genConfig)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("generate client: %w", err)
	}

	spinner.Stop(cli.Green("✓ Specification parsed"))

	// Write files
	spinner = ctx.Spinner("Writing client files...")
	outputMgr := client.NewOutputManager()
	if err := outputMgr.WriteClient(generatedClient, outputDir); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("write client: %w", err)
	}

	spinner.Stop(cli.Green(fmt.Sprintf("✓ Client generated in %s", outputDir)))

	// Show summary
	ctx.Println("")
	ctx.Success("Client generation complete!")
	ctx.Println("")
	ctx.Println(cli.Bold("Generated files:"))
	for filename := range generatedClient.Files {
		ctx.Println(fmt.Sprintf("  - %s", filename))
	}

	if len(generatedClient.Dependencies) > 0 {
		ctx.Println("")
		ctx.Println(cli.Bold("Dependencies:"))
		for _, dep := range generatedClient.Dependencies {
			ctx.Println(fmt.Sprintf("  - %s %s", dep.Name, dep.Version))
		}
	}

	ctx.Println("")
	ctx.Info("Next steps:")
	switch language {
	case "go":
		ctx.Println(fmt.Sprintf("  cd %s", outputDir))
		if module != "" {
			ctx.Println("  go mod tidy")
		}
		ctx.Println("  # Import and use the client in your code")

	case "typescript":
		ctx.Println(fmt.Sprintf("  cd %s", outputDir))
		ctx.Println("  npm install")
		ctx.Println("  npm run build")
	}

	return nil
}

func (p *ClientPlugin) listEndpoints(ctx cli.CommandContext) error {
	fromSpec := ctx.String("from-spec")
	filterType := ctx.String("type")

	if fromSpec == "" {
		return cli.NewError("--from-spec is required", cli.ExitUsageError)
	}

	// Parse spec
	parser := client.NewSpecParser()
	spec, err := parser.ParseFile(context.Background(), fromSpec)
	if err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	// Filter endpoints
	var endpoints []endpointInfo

	if filterType == "" || filterType == "rest" {
		for _, ep := range spec.Endpoints {
			endpoints = append(endpoints, endpointInfo{
				Type:    "REST",
				Method:  ep.Method,
				Path:    ep.Path,
				Auth:    len(ep.Security) > 0,
				Summary: ep.Summary,
			})
		}
	}

	if filterType == "" || filterType == "ws" {
		for _, ws := range spec.WebSockets {
			endpoints = append(endpoints, endpointInfo{
				Type:    "WebSocket",
				Method:  "WS",
				Path:    ws.Path,
				Auth:    len(ws.Security) > 0,
				Summary: ws.Summary,
			})
		}
	}

	if filterType == "" || filterType == "sse" {
		for _, sse := range spec.SSEs {
			endpoints = append(endpoints, endpointInfo{
				Type:    "SSE",
				Method:  "SSE",
				Path:    sse.Path,
				Auth:    len(sse.Security) > 0,
				Summary: sse.Summary,
			})
		}
	}

	// Display table
	if len(endpoints) == 0 {
		ctx.Info("No endpoints found")
		return nil
	}

	ctx.Println("")
	ctx.Println(cli.Bold(fmt.Sprintf("API: %s v%s", spec.Info.Title, spec.Info.Version)))
	ctx.Println("")

	table := ctx.Table()
	table.SetHeader([]string{"Type", "Method", "Path", "Auth", "Summary"})

	for _, ep := range endpoints {
		authStr := "No"
		if ep.Auth {
			authStr = cli.Green("Yes")
		}

		table.AppendRow([]string{
			ep.Type,
			ep.Method,
			ep.Path,
			authStr,
			truncate(ep.Summary, 50),
		})
	}

	table.Render()

	// Show statistics
	ctx.Println("")
	stats := spec.GetStats()
	ctx.Println(cli.Bold("Statistics:"))
	ctx.Println(fmt.Sprintf("  Total endpoints: %d", stats.TotalEndpoints))
	ctx.Println(fmt.Sprintf("  REST: %d, WebSocket: %d, SSE: %d", stats.RESTEndpoints, stats.WebSocketCount, stats.SSECount))
	ctx.Println(fmt.Sprintf("  Secured: %d", stats.SecuredEndpoints))

	return nil
}

func (p *ClientPlugin) initConfig(ctx cli.CommandContext) error {
	ctx.Info("Initializing client generation configuration...")

	// Prompt for language
	language, err := ctx.Select("Select target language:", []string{"go", "typescript"})
	if err != nil {
		return err
	}

	// Prompt for output directory
	outputDir, err := ctx.Prompt("Output directory [./client]:")
	if err != nil {
		return err
	}
	if outputDir == "" {
		outputDir = "./client"
	}

	// Prompt for package name
	packageName, err := ctx.Prompt("Package name [client]:")
	if err != nil {
		return err
	}
	if packageName == "" {
		packageName = "client"
	}

	// Prompt for base URL
	baseURL, err := ctx.Prompt("API base URL:")
	if err != nil {
		return err
	}

	// Create config file
	configContent := fmt.Sprintf(`# Forge Client Generation Configuration

clients:
  - language: %s
    output: %s
    package: %s
    base_url: %s
    features:
      reconnection: true
      heartbeat: true
      state_management: true
      typed_errors: true
`, language, outputDir, packageName, baseURL)

	if err := os.WriteFile(".forge-client.yaml", []byte(configContent), 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	ctx.Success("Configuration file created: .forge-client.yaml")
	ctx.Println("")
	ctx.Info("To generate the client, run:")
	ctx.Println("  forge client generate --from-spec <spec-file>")

	return nil
}

type endpointInfo struct {
	Type    string
	Method  string
	Path    string
	Auth    bool
	Summary string
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
