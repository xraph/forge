// v2/cmd/forge/plugins/client.go
package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
	"github.com/xraph/forge/internal/client/generators/typescript"
)

// ClientPlugin handles client code generation.
type ClientPlugin struct {
	config *config.ForgeConfig
}

// NewClientPlugin creates a new client plugin.
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
		cli.WithFlag(cli.NewStringFlag("from-url", "u", "URL to fetch OpenAPI/AsyncAPI spec", "")),
		cli.WithFlag(cli.NewStringFlag("language", "l", "Target language (go, typescript)", "")),
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "")),
		cli.WithFlag(cli.NewStringFlag("package", "p", "Package/module name", "")),
		cli.WithFlag(cli.NewStringFlag("base-url", "b", "API base URL", "")),
		cli.WithFlag(cli.NewStringFlag("module", "m", "Go module path (for Go only)", "")),

		// Authentication and streaming (optional, defaults from config)
		cli.WithFlag(cli.NewBoolFlag("auth", "", "Include authentication", true)),
		cli.WithFlag(cli.NewBoolFlag("no-auth", "", "Disable authentication", false)),
		cli.WithFlag(cli.NewBoolFlag("streaming", "", "Include streaming (WebSocket/SSE)", true)),
		cli.WithFlag(cli.NewBoolFlag("no-streaming", "", "Disable streaming", false)),

		// Streaming features
		cli.WithFlag(cli.NewBoolFlag("reconnection", "", "Enable reconnection", true)),
		cli.WithFlag(cli.NewBoolFlag("heartbeat", "", "Enable heartbeat", true)),
		cli.WithFlag(cli.NewBoolFlag("state-management", "", "Enable state management", true)),

		// Enhanced features
		cli.WithFlag(cli.NewBoolFlag("use-fetch", "", "Use native fetch instead of axios (TypeScript)", true)),
		cli.WithFlag(cli.NewBoolFlag("dual-package", "", "Generate dual ESM+CJS package (TypeScript)", true)),
		cli.WithFlag(cli.NewBoolFlag("generate-tests", "", "Generate test setup", true)),
		cli.WithFlag(cli.NewBoolFlag("generate-linting", "", "Generate linting setup", true)),
		cli.WithFlag(cli.NewBoolFlag("generate-ci", "", "Generate CI configuration", true)),
		cli.WithFlag(cli.NewBoolFlag("error-taxonomy", "", "Generate typed error classes", true)),
		cli.WithFlag(cli.NewBoolFlag("interceptors", "", "Generate interceptor support", true)),
		cli.WithFlag(cli.NewBoolFlag("pagination", "", "Generate pagination helpers", true)),

		// Streaming extension features
		cli.WithFlag(cli.NewBoolFlag("rooms", "", "Enable room client generation", false)),
		cli.WithFlag(cli.NewBoolFlag("presence", "", "Enable presence client generation", false)),
		cli.WithFlag(cli.NewBoolFlag("typing", "", "Enable typing indicator client generation", false)),
		cli.WithFlag(cli.NewBoolFlag("channels", "", "Enable pub/sub channel client generation", false)),
		cli.WithFlag(cli.NewBoolFlag("history", "", "Enable message history support", false)),
		cli.WithFlag(cli.NewBoolFlag("all-streaming", "", "Enable all streaming features (rooms, presence, typing, channels)", false)),

		// Output control
		cli.WithFlag(cli.NewBoolFlag("client-only", "", "Generate only client source files (no package.json, tsconfig, etc.)", false)),
	))

	clientCmd.AddSubcommand(cli.NewCommand(
		"list",
		"List endpoints from specification",
		p.listEndpoints,
		cli.WithFlag(cli.NewStringFlag("from-spec", "s", "Path to OpenAPI/AsyncAPI spec file", "")),
		cli.WithFlag(cli.NewStringFlag("from-url", "u", "URL to fetch OpenAPI/AsyncAPI spec", "")),
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
	// Try to load .forge-client.yml config
	var (
		clientConfig *ClientConfig
		err          error
	)

	workDir, _ := os.Getwd()
	if p.config != nil {
		workDir = p.config.RootDir
	}

	clientConfig, err = LoadClientConfig(workDir)
	if err != nil {
		// Config not found, use defaults
		clientConfig = DefaultClientConfig()
	} else {
		ctx.Info("Using .forge-client.yml configuration")
	}

	// Get flags (command-line overrides config)
	fromSpec := ctx.String("from-spec")
	fromURL := ctx.String("from-url")
	language := ctx.String("language")
	outputDir := ctx.String("output")
	packageName := ctx.String("package")
	baseURL := ctx.String("base-url")
	module := ctx.String("module")

	// Use config defaults if flags not provided
	if language == "" {
		language = clientConfig.Defaults.Language
	}

	if outputDir == "" {
		outputDir = clientConfig.Defaults.Output
	}

	if packageName == "" {
		packageName = clientConfig.Defaults.Package
	}

	if baseURL == "" {
		baseURL = clientConfig.Defaults.BaseURL
	}

	if module == "" {
		module = clientConfig.Defaults.Module
	}

	// Authentication and streaming (handle both positive and negative flags)
	includeAuth := clientConfig.Defaults.Auth
	if ctx.Bool("no-auth") {
		includeAuth = false
	} else if ctx.Bool("auth") {
		includeAuth = true
	}

	includeStreaming := clientConfig.Defaults.Streaming
	if ctx.Bool("no-streaming") {
		includeStreaming = false
	} else if ctx.Bool("streaming") {
		includeStreaming = true
	}

	// Streaming features (use config defaults)
	reconnection := clientConfig.Defaults.Reconnection
	heartbeat := clientConfig.Defaults.Heartbeat
	stateManagement := clientConfig.Defaults.StateManagement

	// Enhanced features (use config defaults)
	useFetch := clientConfig.Defaults.UseFetch
	dualPackage := clientConfig.Defaults.DualPackage
	generateTests := clientConfig.Defaults.GenerateTests
	generateLinting := clientConfig.Defaults.GenerateLinting
	generateCI := clientConfig.Defaults.GenerateCI
	errorTaxonomy := clientConfig.Defaults.ErrorTaxonomy
	interceptors := clientConfig.Defaults.Interceptors
	pagination := clientConfig.Defaults.Pagination

	// Determine spec source
	var (
		specPath string
		specData []byte
	)

	switch {
	case fromSpec != "":
		// Use provided spec file
		specPath = fromSpec
		ctx.Info("Using spec file: " + specPath)

	case fromURL != "":
		// Fetch from URL
		ctx.Info("Fetching spec from: " + fromURL)
		spinner := ctx.Spinner("Downloading specification...")

		specData, err = fetchSpecFromURL(fromURL, 0)
		if err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return fmt.Errorf("fetch spec from URL: %w", err)
		}

		spinner.Stop(cli.Green("✓ Spec downloaded"))

		// Save to temp file
		tmpFile, err := os.CreateTemp("", "forge-client-spec-*.json")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(specData); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}

		tmpFile.Close()

		specPath = tmpFile.Name()

	case clientConfig.Source.Type == "url":
		// Use URL from config
		if clientConfig.Source.URL == "" {
			return cli.NewError("source.url is empty in .forge-client.yml", cli.ExitUsageError)
		}

		ctx.Info(fmt.Sprintf("Fetching spec from: %s (configured)", clientConfig.Source.URL))
		spinner := ctx.Spinner("Downloading specification...")

		specData, err = fetchSpecFromURL(clientConfig.Source.URL, 0)
		if err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			return fmt.Errorf("fetch spec from URL: %w", err)
		}

		spinner.Stop(cli.Green("✓ Spec downloaded"))

		// Save to temp file
		tmpFile, err := os.CreateTemp("", "forge-client-spec-*.json")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(specData); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}

		tmpFile.Close()

		specPath = tmpFile.Name()

	case clientConfig.Source.Type == "file":
		// Use file from config
		if clientConfig.Source.Path == "" {
			return cli.NewError("source.path is empty in .forge-client.yml", cli.ExitUsageError)
		}

		specPath = clientConfig.Source.Path
		if !filepath.IsAbs(specPath) {
			specPath = filepath.Join(workDir, specPath)
		}

		ctx.Info(fmt.Sprintf("Using spec file: %s (configured)", specPath))

	case clientConfig.Source.Type == "auto" || clientConfig.Source.Type == "":
		// Auto-discover spec file
		ctx.Info("Auto-discovering spec file...")

		specPath, err = autoDiscoverSpec(workDir, clientConfig.Source.AutoDiscoverPaths)
		if err != nil {
			ctx.Warning("No spec file found. Options:")
			ctx.Println("  1. Provide: --from-spec ./openapi.yaml")
			ctx.Println("  2. Fetch: --from-url http://localhost:8080/openapi.json")
			ctx.Println("  3. Configure: forge client init")
			ctx.Println("")
			ctx.Println("Auto-discover paths checked:")

			for _, path := range clientConfig.Source.AutoDiscoverPaths {
				ctx.Println("  - " + path)
			}

			return cli.NewError("no spec file found", cli.ExitUsageError)
		}

		ctx.Success("Found spec: " + specPath)

	default:
		return cli.NewError("unknown source type in config: "+clientConfig.Source.Type, cli.ExitUsageError)
	}

	// Validate spec path exists
	if specPath == "" {
		return cli.NewError("no spec source provided", cli.ExitUsageError)
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

	// Output control
	clientOnly := ctx.Bool("client-only") || clientConfig.Defaults.ClientOnly

	// Streaming extension features
	enableRooms := ctx.Bool("rooms") || ctx.Bool("all-streaming")
	enablePresence := ctx.Bool("presence") || ctx.Bool("all-streaming")
	enableTyping := ctx.Bool("typing") || ctx.Bool("all-streaming")
	enableChannels := ctx.Bool("channels") || ctx.Bool("all-streaming")
	enableHistory := ctx.Bool("history") || ctx.Bool("all-streaming")

	// Check if streaming config is in the config file
	if clientConfig.Streaming.Rooms {
		enableRooms = true
	}

	if clientConfig.Streaming.Presence {
		enablePresence = true
	}

	if clientConfig.Streaming.Typing {
		enableTyping = true
	}

	if clientConfig.Streaming.Channels {
		enableChannels = true
	}

	if clientConfig.Streaming.History {
		enableHistory = true
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
		Streaming: client.StreamingConfig{
			EnableRooms:            enableRooms,
			EnableChannels:         enableChannels,
			EnablePresence:         enablePresence,
			EnableTyping:           enableTyping,
			EnableHistory:          enableHistory,
			GenerateUnifiedClient:  enableRooms || enablePresence || enableTyping || enableChannels,
			GenerateModularClients: enableRooms || enablePresence || enableTyping || enableChannels,
			RoomConfig: client.RoomClientConfig{
				MaxRoomsPerUser:     50,
				IncludeMemberEvents: true,
				IncludeRoomMetadata: true,
			},
			PresenceConfig: client.PresenceClientConfig{
				Statuses:            []string{"online", "away", "busy", "offline"},
				HeartbeatIntervalMs: 30000,
				IdleTimeoutMs:       300000,
				IncludeCustomStatus: true,
			},
			TypingConfig: client.TypingClientConfig{
				TimeoutMs:  3000,
				DebounceMs: 300,
			},
			ChannelConfig: client.ChannelClientConfig{
				MaxChannelsPerUser: 100,
				SupportPatterns:    false,
			},
		},
		// Enhanced features
		UseFetch:        useFetch,
		DualPackage:     dualPackage,
		GenerateTests:   generateTests,
		GenerateLinting: generateLinting,
		GenerateCI:      generateCI,
		ErrorTaxonomy:   errorTaxonomy,
		Interceptors:    interceptors,
		Pagination:      pagination,

		// Output control
		ClientOnly: clientOnly,
	}

	// Validate config
	if err := genConfig.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	ctx.Info(fmt.Sprintf("Generating %s client...", language))
	spinner := ctx.Spinner("Parsing specification...")

	// Generate from file
	generatedClient, err := gen.GenerateFromFile(context.Background(), specPath, genConfig)
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

	spinner.Stop(cli.Green("✓ Client generated in " + outputDir))

	// Show summary
	ctx.Println("")
	ctx.Success("Client generation complete!")
	ctx.Println("")
	ctx.Println(cli.Bold("Generated files:"))

	for filename := range generatedClient.Files {
		ctx.Println("  - " + filename)
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
		ctx.Println("  cd " + outputDir)

		if module != "" {
			ctx.Println("  go mod tidy")
		}

		ctx.Println("  # Import and use the client in your code")

	case "typescript":
		ctx.Println("  cd " + outputDir)
		ctx.Println("  npm install")
		ctx.Println("  npm run build")
	}

	return nil
}

func (p *ClientPlugin) listEndpoints(ctx cli.CommandContext) error {
	fromSpec := ctx.String("from-spec")
	fromURL := ctx.String("from-url")
	filterType := ctx.String("type")

	// Determine spec source (similar to generateClient)
	var (
		specPath string
		err      error
	)

	workDir, _ := os.Getwd()
	if p.config != nil {
		workDir = p.config.RootDir
	}

	switch {
	case fromSpec != "":
		specPath = fromSpec

	case fromURL != "":
		// Fetch from URL
		ctx.Info("Fetching spec from: " + fromURL)

		specData, err := fetchSpecFromURL(fromURL, 0)
		if err != nil {
			return fmt.Errorf("fetch spec from URL: %w", err)
		}

		// Save to temp file
		tmpFile, err := os.CreateTemp("", "forge-client-spec-*.json")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(specData); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}

		tmpFile.Close()

		specPath = tmpFile.Name()

	default:
		// Try auto-discovery
		clientConfig, err := LoadClientConfig(workDir)
		if err != nil {
			clientConfig = DefaultClientConfig()
		}

		specPath, err = autoDiscoverSpec(workDir, clientConfig.Source.AutoDiscoverPaths)
		if err != nil {
			ctx.Warning("No spec file found. Provide one with:")
			ctx.Println("  --from-spec ./openapi.yaml")
			ctx.Println("  --from-url http://localhost:8080/openapi.json")

			return cli.NewError("no spec source provided", cli.ExitUsageError)
		}

		ctx.Info("Using spec: " + specPath)
	}

	// Parse spec
	parser := client.NewSpecParser()

	spec, err := parser.ParseFile(context.Background(), specPath)
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
	ctx.Println("")

	// Prompt for source type
	sourceType, err := ctx.Select("How do you want to provide the API specification?", []string{
		"auto - Auto-discover from common paths",
		"file - Specific file path",
		"url - Fetch from URL",
	})
	if err != nil {
		return err
	}

	// Extract just the type (before the dash)
	sourceType = sourceType[:4]

	config := DefaultClientConfig()
	config.Source.Type = sourceType

	switch sourceType {
	case "file":
		path, err := ctx.Prompt("Spec file path [./openapi.yaml]:")
		if err != nil {
			return err
		}

		if path == "" {
			path = "./openapi.yaml"
		}

		config.Source.Path = path
		config.Source.AutoDiscoverPaths = nil

	case "url ":
		url, err := ctx.Prompt("Spec URL [http://localhost:8080/openapi.json]:")
		if err != nil {
			return err
		}

		if url == "" {
			url = "http://localhost:8080/openapi.json"
		}

		config.Source.URL = url
		config.Source.AutoDiscoverPaths = nil

	case "auto":
		// Keep default auto-discover paths
		ctx.Info("Will auto-discover from common paths:")

		for _, path := range config.Source.AutoDiscoverPaths {
			ctx.Println("  - " + path)
		}
	}

	ctx.Println("")

	// Prompt for language
	language, err := ctx.Select("Select target language:", []string{"go", "typescript"})
	if err != nil {
		return err
	}

	config.Defaults.Language = language

	// Prompt for output directory
	outputDir, err := ctx.Prompt("Output directory [./client]:")
	if err != nil {
		return err
	}

	if outputDir == "" {
		outputDir = "./client"
	}

	config.Defaults.Output = outputDir

	// Prompt for package name
	packageName, err := ctx.Prompt("Package name [client]:")
	if err != nil {
		return err
	}

	if packageName == "" {
		packageName = "client"
	}

	config.Defaults.Package = packageName

	// Prompt for base URL
	baseURL, err := ctx.Prompt("API base URL (optional):")
	if err != nil {
		return err
	}

	config.Defaults.BaseURL = baseURL

	// For Go, ask for module
	if language == "go" {
		module, err := ctx.Prompt("Go module path (optional):")
		if err != nil {
			return err
		}

		config.Defaults.Module = module
	}

	// Save config
	configPath := ".forge-client.yml"
	if err := SaveClientConfig(config, configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	ctx.Println("")
	ctx.Success("Configuration file created: " + configPath)
	ctx.Println("")
	ctx.Info("Configuration:")
	ctx.Println("  Source: " + config.Source.Type)
	ctx.Println("  Language: " + config.Defaults.Language)
	ctx.Println("  Output: " + config.Defaults.Output)
	ctx.Println("")
	ctx.Info("To generate the client, run:")
	ctx.Println("  forge client generate")
	ctx.Println("")
	ctx.Info("Or override with flags:")
	ctx.Println("  forge client generate --from-spec ./custom.yaml")
	ctx.Println("  forge client generate --from-url http://localhost:8080/openapi.json")

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
