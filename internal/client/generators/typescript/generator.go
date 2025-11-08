package typescript

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
	"github.com/xraph/forge/internal/errors"
)

// Generator generates TypeScript clients.
type Generator struct{}

// NewGenerator creates a new TypeScript generator.
func NewGenerator() generators.LanguageGenerator {
	return &Generator{}
}

// Name returns the generator name.
func (g *Generator) Name() string {
	return "typescript"
}

// SupportedFeatures returns supported features.
func (g *Generator) SupportedFeatures() []string {
	return []string{
		generators.FeatureREST,
		generators.FeatureWebSocket,
		generators.FeatureSSE,
		generators.FeatureWebTransport,
		generators.FeatureAuth,
		generators.FeatureReconnection,
		generators.FeatureHeartbeat,
		generators.FeatureStateManagement,
		generators.FeatureTypedErrors,
	}
}

// Validate validates the spec for TypeScript generation.
func (g *Generator) Validate(specIface generators.APISpec) error {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return errors.New("spec is nil or invalid type")
	}

	if spec.Info.Title == "" {
		return errors.New("API title is required")
	}

	return nil
}

// Generate generates the TypeScript client.
func (g *Generator) Generate(ctx context.Context, specIface generators.APISpec, configIface generators.GeneratorConfig) (*generators.GeneratedClient, error) {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return nil, errors.New("spec is nil or invalid type")
	}

	config, ok := configIface.(client.GeneratorConfig)
	if !ok {
		return nil, errors.New("config is invalid type")
	}

	genClient := &generators.GeneratedClient{
		Files:        make(map[string]string),
		Language:     "typescript",
		Version:      config.Version,
		Dependencies: g.getDependencies(config),
	}

	// Generate package.json
	packageJSON := g.generatePackageJSON(spec, config)
	genClient.Files["package.json"] = packageJSON

	// Generate tsconfig.json
	tsconfigJSON := g.generateTSConfig()
	genClient.Files["tsconfig.json"] = tsconfigJSON

	// Generate types
	typesCode := g.generateTypes(spec, config)
	genClient.Files["src/types.ts"] = typesCode

	// Generate main client
	clientCode := g.generateClient(spec, config)
	genClient.Files["src/client.ts"] = clientCode

	// Generate REST methods
	if len(spec.Endpoints) > 0 {
		restGen := NewRESTGenerator()
		restCode := restGen.Generate(spec, config)
		genClient.Files["src/rest.ts"] = restCode
	}

	// Generate WebSocket clients
	if len(spec.WebSockets) > 0 && config.IncludeStreaming {
		wsGen := NewWebSocketGenerator()
		wsCode := wsGen.Generate(spec, config)
		genClient.Files["src/websocket.ts"] = wsCode
	}

	// Generate SSE clients
	if len(spec.SSEs) > 0 && config.IncludeStreaming {
		sseGen := NewSSEGenerator()
		sseCode := sseGen.Generate(spec, config)
		genClient.Files["src/sse.ts"] = sseCode
	}

	// Generate WebTransport clients
	if len(spec.WebTransports) > 0 && config.IncludeStreaming {
		wtGen := NewWebTransportGenerator()
		wtCode := wtGen.Generate(spec, config)
		genClient.Files["src/webtransport.ts"] = wtCode
	}

	// Generate index (barrel export)
	indexCode := g.generateIndex(spec, config)
	genClient.Files["src/index.ts"] = indexCode

	// Generate instructions
	genClient.Instructions = g.generateInstructions(spec, config)

	return genClient, nil
}

// generatePackageJSON generates package.json.
func (g *Generator) generatePackageJSON(spec *client.APISpec, config client.GeneratorConfig) string {
	packageName := config.PackageName
	if packageName == "" {
		packageName = strings.ToLower(strings.ReplaceAll(spec.Info.Title, " ", "-"))
	}

	deps := make(map[string]string)
	deps["axios"] = "^1.6.0"

	if config.IncludeStreaming {
		deps["ws"] = "^8.16.0"
		deps["eventsource"] = "^2.0.2"
	}

	depsJSON := "{\n"

	first := true

	var depsJSONSb143 strings.Builder

	for name, version := range deps {
		if !first {
			depsJSONSb143.WriteString(",\n")
		}

		depsJSONSb143.WriteString(fmt.Sprintf("    \"%s\": \"%s\"", name, version))

		first = false
	}

	depsJSON += depsJSONSb143.String()

	depsJSON += "\n  }"

	return fmt.Sprintf(`{
  "name": "%s",
  "version": "%s",
  "description": "%s",
  "main": "dist/index.js",
  "types": "dist/index.d.ts",
  "scripts": {
    "build": "tsc",
    "prepublish": "npm run build"
  },
  "dependencies": %s,
  "devDependencies": {
    "@types/node": "^20.0.0",
    "@types/ws": "^8.5.0",
    "typescript": "^5.3.0"
  }
}
`, packageName, config.Version, spec.Info.Description, depsJSON)
}

// generateTSConfig generates tsconfig.json.
func (g *Generator) generateTSConfig() string {
	return `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "lib": ["ES2020"],
    "declaration": true,
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "moduleResolution": "node"
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist"]
}
`
}

// generateTypes generates types.ts.
func (g *Generator) generateTypes(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Generated types\n\n")

	// Generate types from schemas
	for name, schema := range spec.Schemas {
		typeCode := g.schemaToTypeScript(name, schema, spec)
		buf.WriteString(typeCode)
		buf.WriteString("\n")
	}

	// Auth config interface
	if config.IncludeAuth && client.NeedsAuthConfig(spec) {
		buf.WriteString("export interface AuthConfig {\n")
		buf.WriteString("  bearerToken?: string;\n")
		buf.WriteString("  apiKey?: string;\n")
		buf.WriteString("  customHeaders?: Record<string, string>;\n")
		buf.WriteString("}\n\n")
	}

	// Client config interface
	buf.WriteString("export interface ClientConfig {\n")
	buf.WriteString("  baseURL: string;\n")

	if config.IncludeAuth {
		buf.WriteString("  auth?: AuthConfig;\n")
	}

	buf.WriteString("  timeout?: number;\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// schemaToTypeScript converts a schema to TypeScript.
func (g *Generator) schemaToTypeScript(name string, schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return ""
	}

	var buf strings.Builder

	switch schema.Type {
	case "object":
		buf.WriteString(fmt.Sprintf("export interface %s {\n", name))

		for propName, prop := range schema.Properties {
			required := contains(schema.Required, propName)

			optional := ""
			if !required {
				optional = "?"
			}

			tsType := g.schemaToTSType(prop, spec)
			buf.WriteString(fmt.Sprintf("  %s%s: %s;\n", propName, optional, tsType))
		}

		buf.WriteString("}\n")

	case "array":
		if schema.Items != nil {
			itemType := g.schemaToTSType(schema.Items, spec)
			buf.WriteString(fmt.Sprintf("export type %s = %s[];\n", name, itemType))
		}

	default:
		tsType := g.schemaToTSType(schema, spec)
		buf.WriteString(fmt.Sprintf("export type %s = %s;\n", name, tsType))
	}

	return buf.String()
}

// schemaToTSType converts a schema to a TypeScript type string.
func (g *Generator) schemaToTSType(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "any"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")

		return parts[len(parts)-1]
	}

	switch schema.Type {
	case "string":
		if len(schema.Enum) > 0 {
			var values []string
			for _, v := range schema.Enum {
				values = append(values, fmt.Sprintf("'%v'", v))
			}

			return strings.Join(values, " | ")
		}

		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if schema.Items != nil {
			return g.schemaToTSType(schema.Items, spec) + "[]"
		}

		return "any[]"
	case "object":
		return "Record<string, any>"
	}

	return "any"
}

// generateClient generates client.ts.
func (g *Generator) generateClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("import axios, { AxiosInstance, AxiosRequestConfig } from 'axios';\n")
	buf.WriteString("import { ClientConfig, AuthConfig } from './types';\n\n")

	buf.WriteString(fmt.Sprintf("export class %s {\n", config.APIName))
	buf.WriteString("  private client: AxiosInstance;\n")
	buf.WriteString("  private auth?: AuthConfig;\n\n")

	buf.WriteString("  constructor(config: ClientConfig) {\n")
	buf.WriteString("    this.auth = config.auth;\n")
	buf.WriteString("    this.client = axios.create({\n")
	buf.WriteString("      baseURL: config.baseURL,\n")
	buf.WriteString("      timeout: config.timeout || 30000,\n")
	buf.WriteString("      headers: {\n")
	buf.WriteString("        'Content-Type': 'application/json',\n")
	buf.WriteString("      },\n")
	buf.WriteString("    });\n\n")

	buf.WriteString("    // Add auth interceptor\n")
	buf.WriteString("    this.client.interceptors.request.use((config) => {\n")
	buf.WriteString("      if (this.auth?.bearerToken) {\n")
	buf.WriteString("        config.headers.Authorization = `Bearer ${this.auth.bearerToken}`;\n")
	buf.WriteString("      }\n")
	buf.WriteString("      if (this.auth?.apiKey) {\n")
	buf.WriteString("        config.headers['X-API-Key'] = this.auth.apiKey;\n")
	buf.WriteString("      }\n")
	buf.WriteString("      if (this.auth?.customHeaders) {\n")
	buf.WriteString("        Object.assign(config.headers, this.auth.customHeaders);\n")
	buf.WriteString("      }\n")
	buf.WriteString("      return config;\n")
	buf.WriteString("    });\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  protected async request<T>(config: AxiosRequestConfig): Promise<T> {\n")
	buf.WriteString("    const response = await this.client.request<T>(config);\n")
	buf.WriteString("    return response.data;\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateIndex generates index.ts.
func (g *Generator) generateIndex(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("export * from './types';\n")
	buf.WriteString("export * from './client';\n")

	if len(spec.Endpoints) > 0 {
		buf.WriteString("export * from './rest';\n")
	}

	if len(spec.WebSockets) > 0 && config.IncludeStreaming {
		buf.WriteString("export * from './websocket';\n")
	}

	if len(spec.SSEs) > 0 && config.IncludeStreaming {
		buf.WriteString("export * from './sse';\n")
	}

	return buf.String()
}

// getDependencies returns the list of dependencies.
func (g *Generator) getDependencies(config client.GeneratorConfig) []generators.Dependency {
	deps := []generators.Dependency{
		{Name: "axios", Version: "^1.6.0", Type: "direct"},
		{Name: "typescript", Version: "^5.3.0", Type: "dev"},
	}

	if config.IncludeStreaming {
		deps = append(deps,
			generators.Dependency{Name: "ws", Version: "^8.16.0", Type: "direct"},
			generators.Dependency{Name: "eventsource", Version: "^2.0.2", Type: "direct"},
		)
	}

	return deps
}

// generateInstructions generates setup instructions.
func (g *Generator) generateInstructions(spec *client.APISpec, config client.GeneratorConfig) string {
	outputMgr := client.NewOutputManager()
	authGen := client.NewAuthCodeGenerator()

	authDocs := ""

	if config.IncludeAuth {
		schemes := authGen.DetectAuthSchemes(spec)
		authDocs = authGen.GenerateAuthDocumentation(schemes)
	}

	return outputMgr.GenerateREADME(config, spec, authDocs)
}

// Helper function.
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
