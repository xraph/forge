// v2/cmd/forge/plugins/client.go
package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		append([]cli.CommandOption{cli.WithAliases("gen", "g")}, clientGenerationFlags()...)...,
	))

	// check shares generate's flag set verbatim -- not a copy of it. A check
	// that resolves its configuration even slightly differently from generate
	// reports drift that does not exist, and a gate that cries wolf gets
	// deleted within a week.
	clientCmd.AddSubcommand(cli.NewCommand(
		"check",
		"Verify the committed client matches what the current spec generates",
		p.checkClient,
		append([]cli.CommandOption{cli.WithUsage(checkUsage)}, clientGenerationFlags()...)...,
	))

	// watch is built from the same flag set for the same reason check is: it
	// runs generate's resolution on a loop, and a watch that regenerated into a
	// different directory, or with different options, than the generate a
	// developer runs by hand would be worse than no watch at all.
	clientCmd.AddSubcommand(cli.NewCommand(
		"watch",
		"Regenerate the client whenever the API specification changes",
		p.watchClient,
		append([]cli.CommandOption{
			cli.WithAliases("w"),
			cli.WithUsage(watchUsage),
			cli.WithFlag(cli.NewDurationFlag(
				"poll-interval",
				"",
				"How often to re-fetch a --from-url spec (a file spec is watched, not polled)",
				defaultWatchPollInterval,
			)),
		}, clientGenerationFlags()...)...,
	))

	clientCmd.AddSubcommand(cli.NewCommand(
		"diff",
		"Classify what changed between two API specifications",
		p.diffSpecs,
		cli.WithUsage(diffUsage),
		cli.WithFlag(cli.NewStringFlag("format", "f", "Output format: text or json", "text")),
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

// clientGenerationFlags is the single definition of the flags that select a
// spec, a language and an output shape. Both `generate` and `check` are built
// from it so that neither can drift from the other: `check` regenerating under
// a different set of defaults than `generate` wrote with would report a
// difference on every run, on a tree nobody had touched.
func clientGenerationFlags() []cli.CommandOption {
	return []cli.CommandOption{
		cli.WithFlag(cli.NewStringFlag("from-spec", "s", "Path to OpenAPI/AsyncAPI spec file", "")),
		cli.WithFlag(cli.NewStringFlag("from-url", "u", "URL to fetch OpenAPI/AsyncAPI spec", "")),
		cli.WithFlag(cli.NewStringFlag("language", "l", "Target language (go, typescript)", "")),
		cli.WithFlag(cli.NewStringFlag("output", "o", "Output directory", "")),
		cli.WithFlag(cli.NewStringFlag("package", "p", "Package/module name", "")),
		cli.WithFlag(cli.NewStringFlag("base-url", "b", "API base URL", "")),
		cli.WithFlag(cli.NewStringFlag("module", "m", "Go module path (for Go only)", "")),

		// Field naming (TypeScript). Empty ("") means "unset": the generator's
		// own per-language default applies (camel for typescript, preserve
		// otherwise) so omitting this flag changes nothing for existing users.
		cli.WithFlag(cli.NewStringFlag("field-naming", "", "Client-side field naming strategy: camel, pascal, snake, or preserve (default: camel for typescript, preserve otherwise)", "")),
		cli.WithFlag(cli.NewBoolFlag("hooks", "", "Generate the operation manifest (ops.ts) and typed hook facades (hooks.ts) over @forge-go/client-core", false)),

		// Retained so existing scripts keep working. Enables exactly what
		// --hooks does; see resolveHooks.
		cli.WithFlag(cli.NewBoolFlag("react-query", "", "DEPRECATED: use --hooks (the generated hooks are not TanStack Query)", false)),
		cli.WithFlag(cli.NewStringSliceFlag("include", "", "Only generate endpoints whose path matches a pattern (repeatable; prefix, glob or `/**`)", nil)),
		cli.WithFlag(cli.NewStringSliceFlag("exclude", "", "Skip endpoints whose path matches a pattern; applied after --include (repeatable)", nil)),
		cli.WithFlag(cli.NewStringFlag("field-overrides", "", "Comma-separated field name overrides, e.g. 'User.user_id=userIdentifier,api_key=apiKey' (schema-scoped keys use \"Schema.wire_name\"; a bare \"wire_name\" applies globally)", "")),

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
	}
}

// generationPlan is everything `forge client generate` decides before it writes
// a single file: which spec to read, under what configuration, and where the
// committed output lives.
//
// It exists so `check` can take the identical decisions rather than a
// reimplementation of them. cleanup releases anything the resolution allocated
// (a temp file holding a spec fetched over HTTP) and is safe to call once on
// every path, success or failure.
type generationPlan struct {
	specPath string
	// specURL is set when the spec came over HTTP; specPath is then a temp file
	// holding the fetched bytes. generate and check never look at it -- they
	// only need the file -- but `watch` cannot tell a downloaded spec from a
	// local one by its path, and polling a temp file that nothing ever writes to
	// again would be a watch that can never fire.
	specURL   string
	outputDir string
	config    client.GeneratorConfig
	cleanup   func()
}

func (p *ClientPlugin) generateClient(ctx cli.CommandContext) error {
	plan, err := p.resolveGenerationPlan(ctx)
	if err != nil {
		return err
	}

	defer plan.cleanup()

	gen, err := newClientGenerator()
	if err != nil {
		return err
	}

	language := plan.config.Language
	outputDir := plan.outputDir

	ctx.Info(fmt.Sprintf("Generating %s client...", language))
	spinner := ctx.Spinner("Parsing specification...")

	// Generate from file
	generatedClient, err := gen.GenerateFromFile(context.Background(), plan.specPath, plan.config)
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

	// Surface generation-time warnings (e.g. an undiscriminated union
	// resolved structurally rather than by a discriminator, or a conflicting
	// allOf composition) now that the spinner has stopped -- printing them
	// while the spinner is still active would have them overwritten by its
	// next repaint on a TTY before anyone could read them.
	for _, w := range generatedClient.Warnings {
		ctx.Warning(w)
	}

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

		if plan.config.Module != "" {
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

// newClientGenerator builds the generator with every language registered.
// Shared by generate and check so the two cannot end up with different
// generator sets.
func newClientGenerator() (*client.Generator, error) {
	gen := client.NewGenerator()

	if err := gen.Register(golang.NewGenerator()); err != nil {
		return nil, fmt.Errorf("register Go generator: %w", err)
	}

	if err := gen.Register(typescript.NewGenerator()); err != nil {
		return nil, fmt.Errorf("register TypeScript generator: %w", err)
	}

	return gen, nil
}

// resolveGenerationPlan resolves configuration file, flags, spec source and
// output directory exactly once, for whichever command asked.
//
// It is long because it is generate's resolution moved wholesale rather than
// rewritten. Splitting it would mean deciding again what each half does, which
// is precisely the drift `check` exists to make impossible.
//
//nolint:gocyclo,gocognit,funlen // see above
func (p *ClientPlugin) resolveGenerationPlan(ctx cli.CommandContext) (*generationPlan, error) {
	// Try to load .forge-client.yml config
	var (
		clientConfig *ClientConfig
		err          error
	)

	// Resolved from the working directory, not the project root.
	//
	// LoadClientConfig already walks upward, so starting here finds a config
	// beside the package being generated *and* one at the project root.
	// Starting at the root instead finds only the root's, which in a workspace
	// is the one place the file usually is not — a package that carries its own
	// .forge-client.yaml was silently generated with defaults.
	workDir, err := os.Getwd()
	if err != nil && p.config != nil {
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
	// --hooks, folding in the deprecated --react-query / react_query alias.
	// IsSet, not Bool: "--react-query=false" is still a use of the retired
	// name and earns the notice, even though it enables nothing.
	hooks, usedDeprecatedName := resolveHooks(
		ctx.Bool("hooks"),
		ctx.Bool("react-query"),
		ctx.Flag("react-query").IsSet(),
		clientConfig.Defaults.Hooks,
		clientConfig.Defaults.ReactQuery,
	)
	if usedDeprecatedName {
		ctx.Warning("--react-query / react_query is deprecated and will be removed; use --hooks / hooks instead")
		ctx.Warning("  it no longer generates TanStack Query hooks -- it emits src/ops.ts and src/hooks.ts over @forge-go/client-core")
	}
	includePaths := ctx.StringSlice("include")
	excludePaths := ctx.StringSlice("exclude")
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

	// Field naming: CLI flag wins over .forge-client.yml, which wins over
	// leaving it unset entirely. Unset ("") is passed straight through to
	// client.GeneratorConfig.FieldNaming and resolved by the library's own
	// effectiveFieldNaming (camel for typescript, preserve otherwise) --
	// nothing changes for a caller who never touches this. Unlike that
	// library-level resolution (which silently falls back to preserve for
	// an unrecognised NamingStrategy value -- see fieldname.go's
	// effectiveFieldNaming), a typo'd --field-naming (or a bad
	// field_naming in the config file) is rejected outright: a CLI user
	// typing "--field-naming cammel" almost certainly wants camel, not
	// preserve, and preserve is 100% silent about the rename it did not
	// do.
	fieldNamingFlag := ctx.String("field-naming")
	if fieldNamingFlag == "" {
		fieldNamingFlag = clientConfig.Defaults.FieldNaming
	}

	fieldNaming, err := parseFieldNaming(fieldNamingFlag)
	if err != nil {
		return nil, cli.NewError(err.Error(), cli.ExitUsageError)
	}

	fieldOverrides, err := parseFieldOverrides(ctx.String("field-overrides"))
	if err != nil {
		return nil, cli.NewError(fmt.Sprintf("invalid --field-overrides: %v", err), cli.ExitUsageError)
	}

	if len(fieldOverrides) == 0 {
		fieldOverrides = clientConfig.Defaults.FieldOverrides
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

	// Determine spec source.
	//
	// A spec fetched over HTTP lands in a temp file that has to outlive this
	// function -- generation runs after the plan is returned -- so the removal
	// cannot be deferred here the way it was when resolution and generation
	// lived in one function. It is registered on the plan's cleanup instead,
	// which every caller defers and which every failure path below runs before
	// returning.
	var (
		specPath string
		specURL  string
		specData []byte
		cleanups []func()
	)

	cleanup := func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	fail := func(err error) (*generationPlan, error) {
		cleanup()

		return nil, err
	}

	switch {
	case fromSpec != "":
		// Use provided spec file
		specPath = fromSpec
		ctx.Info("Using spec file: " + specPath)

	case fromURL != "":
		// Fetch from URL
		ctx.Info("Fetching spec from: " + fromURL)

		specURL = fromURL

		spinner := ctx.Spinner("Downloading specification...")

		specData, err = fetchSpecFromURL(ctx.Context(), fromURL, 0)
		if err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			// Exit 2, not the default 1: `check` documents 1 as "drift,
			// run generate and commit the result", and a CI job that
			// followed that advice in response to an unreachable spec URL
			// would commit a stale client on purpose.
			return fail(cli.WrapError(err, "fetch spec from URL", cli.ExitUsageError))
		}

		spinner.Stop(cli.Green("✓ Spec downloaded"))

		// Save to temp file
		tmpFile, err := os.CreateTemp("", "forge-client-spec-*.json")
		if err != nil {
			return fail(cli.WrapError(err, "create temp file", cli.ExitInternalError))
		}

		tmpName := tmpFile.Name()
		cleanups = append(cleanups, func() { _ = os.Remove(tmpName) })

		if _, err := tmpFile.Write(specData); err != nil {
			tmpFile.Close()

			return fail(cli.WrapError(err, "write temp file", cli.ExitInternalError))
		}

		tmpFile.Close()

		specPath = tmpName

	case clientConfig.Source.Type == "url":
		// Use URL from config
		if clientConfig.Source.URL == "" {
			return fail(cli.NewError("source.url is empty in .forge-client.yml", cli.ExitUsageError))
		}

		ctx.Info(fmt.Sprintf("Fetching spec from: %s (configured)", clientConfig.Source.URL))

		specURL = clientConfig.Source.URL

		spinner := ctx.Spinner("Downloading specification...")

		specData, err = fetchSpecFromURL(ctx.Context(), clientConfig.Source.URL, 0)
		if err != nil {
			spinner.Stop(cli.Red("✗ Failed"))

			// Exit 2, not the default 1: `check` documents 1 as "drift,
			// run generate and commit the result", and a CI job that
			// followed that advice in response to an unreachable spec URL
			// would commit a stale client on purpose.
			return fail(cli.WrapError(err, "fetch spec from URL", cli.ExitUsageError))
		}

		spinner.Stop(cli.Green("✓ Spec downloaded"))

		// Save to temp file
		tmpFile, err := os.CreateTemp("", "forge-client-spec-*.json")
		if err != nil {
			return fail(cli.WrapError(err, "create temp file", cli.ExitInternalError))
		}

		tmpName := tmpFile.Name()
		cleanups = append(cleanups, func() { _ = os.Remove(tmpName) })

		if _, err := tmpFile.Write(specData); err != nil {
			tmpFile.Close()

			return fail(cli.WrapError(err, "write temp file", cli.ExitInternalError))
		}

		tmpFile.Close()

		specPath = tmpName

	case clientConfig.Source.Type == "file":
		// Use file from config
		if clientConfig.Source.Path == "" {
			return fail(cli.NewError("source.path is empty in .forge-client.yml", cli.ExitUsageError))
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

			return fail(cli.NewError("no spec file found", cli.ExitUsageError))
		}

		ctx.Success("Found spec: " + specPath)

	default:
		return fail(cli.NewError("unknown source type in config: "+clientConfig.Source.Type, cli.ExitUsageError))
	}

	// Validate spec path exists
	if specPath == "" {
		return fail(cli.NewError("no spec source provided", cli.ExitUsageError))
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

	// Path filter: flags win, config fills in. Reported below rather than
	// applied silently — a client that is quietly missing half its endpoints
	// looks identical to one whose server never had them.
	pathFilter := client.PathFilter{
		Include: includePaths,
		Exclude: excludePaths,
	}

	if len(pathFilter.Include) == 0 {
		pathFilter.Include = clientConfig.Defaults.Include
	}

	if len(pathFilter.Exclude) == 0 {
		pathFilter.Exclude = clientConfig.Defaults.Exclude
	}

	if !pathFilter.Empty() {
		if len(pathFilter.Include) > 0 {
			ctx.Info("Including paths: " + strings.Join(pathFilter.Include, ", "))
		}

		if len(pathFilter.Exclude) > 0 {
			ctx.Info("Excluding paths: " + strings.Join(pathFilter.Exclude, ", "))
		}
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
		FieldNaming:      fieldNaming,
		FieldOverrides:   fieldOverrides,
		PathFilter:       pathFilter,
		Hooks:            hooks,
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

	// Validate config. Validate normalizes as well as checks (it lowercases
	// the language and folds the "ts" alias into "typescript"), so the plan
	// carries the normalized config -- not the raw one -- and generate and
	// check therefore generate under identical settings.
	if err := genConfig.Validate(); err != nil {
		// Exit 2: an unsupported --language or a missing package name is a
		// usage error, and must not share an exit code with drift.
		return fail(cli.WrapError(err, "invalid config", cli.ExitUsageError))
	}

	return &generationPlan{
		specPath:  specPath,
		specURL:   specURL,
		outputDir: outputDir,
		config:    genConfig,
		cleanup:   cleanup,
	}, nil
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

	// Resolved from the working directory, not the project root.
	//
	// LoadClientConfig already walks upward, so starting here finds a config
	// beside the package being generated *and* one at the project root.
	// Starting at the root instead finds only the root's, which in a workspace
	// is the one place the file usually is not — a package that carries its own
	// .forge-client.yaml was silently generated with defaults.
	workDir, err := os.Getwd()
	if err != nil && p.config != nil {
		workDir = p.config.RootDir
	}

	switch {
	case fromSpec != "":
		specPath = fromSpec

	case fromURL != "":
		// Fetch from URL
		ctx.Info("Fetching spec from: " + fromURL)

		specData, err := fetchSpecFromURL(ctx.Context(), fromURL, 0)
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

// parseFieldNaming validates a --field-naming (or .forge-client.yml
// field_naming) value and maps it to a client.NamingStrategy.
//
// An empty string means "unset" and passes straight through as
// client.NamingStrategy(""), letting the library's own
// effectiveFieldNaming resolve it (camel for typescript, preserve
// otherwise) exactly as if the flag had never been introduced.
//
// Any non-empty value that is not one of the four recognised strategies is
// rejected outright, unlike the library layer: effectiveFieldNaming
// silently treats an unrecognised client.GeneratorConfig.FieldNaming as
// preserve (see fieldname.go), which is the right call for a Go API caller
// who already has to read the source to construct a GeneratorConfig at
// all, but wrong for a CLI flag a human typed -- a typo like "cammel"
// silently becoming "preserve" would produce a client that compiles fine
// and simply never renames anything, with no signal that the flag was
// misspelled.
func parseFieldNaming(value string) (client.NamingStrategy, error) {
	switch value {
	case "":
		return "", nil
	case "camel":
		return client.NamingCamel, nil
	case "pascal":
		return client.NamingPascal, nil
	case "snake":
		return client.NamingSnake, nil
	case "preserve":
		return client.NamingPreserve, nil
	default:
		return "", fmt.Errorf("invalid --field-naming value %q: must be one of camel, pascal, snake, preserve", value)
	}
}

// resolveHooks folds the deprecated --react-query flag and react_query config
// key into --hooks / hooks, which gate the same emission: the operation
// manifest (ops.ts) and the typed hook facades (hooks.ts).
//
// enabled is the OR of all four sources. Any one of them turning the layer on
// is enough -- a project that has react_query in its .forge-client.yml and has
// not migrated it yet generates exactly what it generated before.
//
// deprecated reports whether an old name was involved at all, so the caller
// can warn. It is deliberately driven by reactQueryFlagSet rather than
// reactQueryFlag: passing "--react-query=false" enables nothing, but it is
// still a use of a name that is going away, and the one moment a user is
// looking at that flag is the moment worth telling them. A config file's
// react_query has no equivalent "was it written down" signal, so only a true
// value there counts -- yaml gives an absent key and an explicit "false" the
// same zero, and warning about a key that may not exist would be noise on
// every run.
func resolveHooks(hooksFlag, reactQueryFlag, reactQueryFlagSet, cfgHooks, cfgReactQuery bool) (enabled, deprecated bool) {
	enabled = hooksFlag || reactQueryFlag || cfgHooks || cfgReactQuery
	deprecated = reactQueryFlagSet || cfgReactQuery

	return enabled, deprecated
}

// parseFieldOverrides parses a --field-overrides value: a comma-separated
// list of "key=clientName" pairs, where key is either a bare wire name
// (applies globally) or "SchemaName.wire_name" (schema-scoped), exactly
// matching client.GeneratorConfig.FieldOverrides' own key format.
//
// Chosen over a repeated flag (--field-overrides a=b --field-overrides
// c=d) because this CLI's flag parser (cli/context.go's
// parseFlagsForCommand) overwrites a flag's value on each repeated
// occurrence rather than accumulating them -- a repeated-flag design would
// silently keep only the LAST override and drop the rest, which is worse
// than not offering the feature at all. A single comma-separated value has
// no such trap.
//
// An empty (or whitespace-only) value returns a nil map, not an error --
// omitting --field-overrides is the common case. Any other malformed entry
// (missing "=", or an empty key/value on either side of it) is rejected
// with the exact offending entry quoted, rather than silently skipped: a
// dropped override is a silent rename that never happens, exactly the
// failure mode --field-naming's strict validation above exists to avoid.
func parseFieldOverrides(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	overrides := make(map[string]string)

	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		key, value, hasEquals := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if !hasEquals || key == "" || value == "" {
			return nil, fmt.Errorf("malformed entry %q: expected \"wire_name=clientName\" or \"Schema.wire_name=clientName\"", pair)
		}

		overrides[key] = value
	}

	return overrides, nil
}
