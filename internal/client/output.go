package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/internal/client/generators"
)

// OutputManager handles writing generated client files to disk.
type OutputManager struct{}

// NewOutputManager creates a new output manager.
func NewOutputManager() *OutputManager {
	return &OutputManager{}
}

// WriteClient writes the generated client to disk.
func (m *OutputManager) WriteClient(client *generators.GeneratedClient, outputDir string) error {
	// Create output directory with restrictive permissions
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Write each file
	for filename, content := range client.Files {
		filePath := filepath.Join(outputDir, filename)

		// Create subdirectories if needed
		dir := filepath.Dir(filePath)
		if dir != outputDir {
			if err := os.MkdirAll(dir, 0750); err != nil {
				return fmt.Errorf("create subdirectory %s: %w", dir, err)
			}
		}

		// Write file with restrictive permissions
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			return fmt.Errorf("write file %s: %w", filename, err)
		}
	}

	// Write README if instructions are provided
	if client.Instructions != "" {
		readmePath := filepath.Join(outputDir, "README.md")
		if err := os.WriteFile(readmePath, []byte(client.Instructions), 0600); err != nil {
			return fmt.Errorf("write README: %w", err)
		}
	}

	return nil
}

// GenerateREADME generates a README for the client.
func (m *OutputManager) GenerateREADME(config GeneratorConfig, spec *APISpec, authDocs string) string {
	var readme strings.Builder

	// Title
	readme.WriteString(fmt.Sprintf("# %s Client\n\n", spec.Info.Title))

	if spec.Info.Description != "" {
		readme.WriteString(spec.Info.Description + "\n\n")
	}

	readme.WriteString(fmt.Sprintf("Generated client for %s %s\n\n", spec.Info.Title, spec.Info.Version))

	// Installation
	readme.WriteString("## Installation\n\n")
	readme.WriteString(m.generateInstallationInstructions(config))
	readme.WriteString("\n")

	// Usage
	readme.WriteString("## Usage\n\n")
	readme.WriteString(m.generateUsageExample(config, spec))
	readme.WriteString("\n")

	// Authentication
	if authDocs != "" {
		readme.WriteString(authDocs)
		readme.WriteString("\n")
	}

	// API Overview
	readme.WriteString("## API Overview\n\n")
	readme.WriteString(m.generateAPIOverview(spec))
	readme.WriteString("\n")

	// Features
	if config.Features.Reconnection || config.Features.Heartbeat || config.Features.StateManagement {
		readme.WriteString("## Features\n\n")

		if config.Features.Reconnection {
			readme.WriteString("- ✓ Automatic reconnection with exponential backoff\n")
		}

		if config.Features.Heartbeat {
			readme.WriteString("- ✓ Heartbeat/ping for connection health\n")
		}

		if config.Features.StateManagement {
			readme.WriteString("- ✓ Connection state management\n")
		}

		if config.Features.TypedErrors {
			readme.WriteString("- ✓ Typed error responses\n")
		}

		if config.Features.RequestRetry {
			readme.WriteString("- ✓ Automatic request retry\n")
		}

		if config.Features.Timeout {
			readme.WriteString("- ✓ Request timeout configuration\n")
		}

		readme.WriteString("\n")
	}

	// License
	if spec.Info.License != nil {
		readme.WriteString("## License\n\n")
		readme.WriteString(fmt.Sprintf("[%s](%s)\n\n", spec.Info.License.Name, spec.Info.License.URL))
	}

	return readme.String()
}

// generateInstallationInstructions generates installation instructions for the language.
func (m *OutputManager) generateInstallationInstructions(config GeneratorConfig) string {
	switch config.Language {
	case "go":
		if config.Module != "" {
			return fmt.Sprintf("```bash\ngo get %s\n```\n", config.Module)
		}

		return "```bash\n# Copy the generated files to your project\n```\n"

	case "typescript":
		if config.PackageName != "" {
			return fmt.Sprintf("```bash\nnpm install %s\n# or\nyarn add %s\n```\n", config.PackageName, config.PackageName)
		}

		return "```bash\nnpm install\n```\n"

	default:
		return "See language-specific documentation.\n"
	}
}

// generateUsageExample generates a basic usage example.
func (m *OutputManager) generateUsageExample(config GeneratorConfig, spec *APISpec) string {
	switch config.Language {
	case "go":
		return m.generateGoUsageExample(config, spec)
	case "typescript":
		return m.generateTypeScriptUsageExample(config, spec)
	default:
		return "See generated files for usage examples.\n"
	}
}

// generateGoUsageExample generates a Go usage example.
func (m *OutputManager) generateGoUsageExample(config GeneratorConfig, spec *APISpec) string {
	var example strings.Builder

	example.WriteString("```go\n")
	example.WriteString("package main\n\n")
	example.WriteString("import (\n")
	example.WriteString(fmt.Sprintf("    \"%s\"\n", config.PackageName))
	example.WriteString("    \"context\"\n")
	example.WriteString("    \"log\"\n")
	example.WriteString(")\n\n")
	example.WriteString("func main() {\n")
	example.WriteString(fmt.Sprintf("    client := %s.NewClient(\n", config.PackageName))

	if config.BaseURL != "" {
		example.WriteString(fmt.Sprintf("        %s.WithBaseURL(\"%s\"),\n", config.PackageName, config.BaseURL))
	}

	if config.IncludeAuth && NeedsAuthConfig(spec) {
		example.WriteString(fmt.Sprintf("        %s.WithAuth(%s.AuthConfig{\n", config.PackageName, config.PackageName))
		example.WriteString("            BearerToken: \"your-token-here\",\n")
		example.WriteString("        }),\n")
	}

	example.WriteString("    )\n\n")
	example.WriteString("    ctx := context.Background()\n\n")
	example.WriteString("    // Use the client\n")
	example.WriteString("    // result, err := client.SomeMethod(ctx, ...)\n")
	example.WriteString("    // if err != nil {\n")
	example.WriteString("    //     log.Fatal(err)\n")
	example.WriteString("    // }\n")
	example.WriteString("}\n")
	example.WriteString("```\n")

	return example.String()
}

// generateTypeScriptUsageExample generates a TypeScript usage example.
func (m *OutputManager) generateTypeScriptUsageExample(config GeneratorConfig, spec *APISpec) string {
	var example strings.Builder

	example.WriteString("```typescript\n")
	example.WriteString(fmt.Sprintf("import { %s } from '%s';\n\n", config.APIName, config.PackageName))
	example.WriteString(fmt.Sprintf("const client = new %s({\n", config.APIName))

	if config.BaseURL != "" {
		example.WriteString(fmt.Sprintf("  baseURL: '%s',\n", config.BaseURL))
	}

	if config.IncludeAuth && NeedsAuthConfig(spec) {
		example.WriteString("  auth: {\n")
		example.WriteString("    bearerToken: 'your-token-here',\n")
		example.WriteString("  },\n")
	}

	example.WriteString("});\n\n")
	example.WriteString("// Use the client\n")
	example.WriteString("// const result = await client.someMethod(...);\n")
	example.WriteString("```\n")

	return example.String()
}

// generateAPIOverview generates an overview of the API.
func (m *OutputManager) generateAPIOverview(spec *APISpec) string {
	var overview strings.Builder

	stats := spec.GetStats()

	overview.WriteString(fmt.Sprintf("- **Total Endpoints**: %d\n", stats.TotalEndpoints))
	overview.WriteString(fmt.Sprintf("- **REST Endpoints**: %d\n", stats.RESTEndpoints))

	if stats.WebSocketCount > 0 {
		overview.WriteString(fmt.Sprintf("- **WebSocket Endpoints**: %d\n", stats.WebSocketCount))
	}

	if stats.SSECount > 0 {
		overview.WriteString(fmt.Sprintf("- **SSE Endpoints**: %d\n", stats.SSECount))
	}

	if stats.SecuredEndpoints > 0 {
		overview.WriteString(fmt.Sprintf("- **Secured Endpoints**: %d\n", stats.SecuredEndpoints))
	}

	if len(stats.Tags) > 0 {
		overview.WriteString(fmt.Sprintf("- **Tags**: %s\n", strings.Join(stats.Tags, ", ")))
	}

	if len(spec.Servers) > 0 {
		overview.WriteString("\n### Servers\n\n")

		for _, server := range spec.Servers {
			overview.WriteString(fmt.Sprintf("- **%s**", server.URL))

			if server.Description != "" {
				overview.WriteString(": " + server.Description)
			}

			overview.WriteString("\n")
		}
	}

	return overview.String()
}

// FormatCode formats generated code (basic formatting).
func FormatCode(code string, language string) string {
	// Basic formatting - in production, use language-specific formatters
	// For Go: use gofmt
	// For TypeScript: use prettier
	lines := strings.Split(code, "\n")

	var formatted []string

	indent := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Decrease indent for closing braces
		if strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, "]") || strings.HasPrefix(trimmed, ")") {
			indent--
		}

		// Apply indent
		if indent > 0 && trimmed != "" {
			formatted = append(formatted, strings.Repeat("    ", indent)+trimmed)
		} else {
			formatted = append(formatted, trimmed)
		}

		// Increase indent for opening braces
		if strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, "[") {
			indent++
		}
	}

	return strings.Join(formatted, "\n")
}

// EnsureDirectory ensures a directory exists.
func EnsureDirectory(path string) error {
	return os.MkdirAll(path, 0755)
}

// FileExists checks if a file exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
