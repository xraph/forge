package client

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
//
// It deliberately does NOT print client.Warnings itself. That used to
// happen here via a raw os.Stderr write, but the one real caller
// (cmd/forge/plugins/client.go) starts a terminal spinner immediately
// around this call -- on a TTY the spinner's own repaint (every ~80ms)
// overwrites whatever this function had just written to stderr, so a human
// on the interactive path could go the whole run without ever seeing a
// warning (piped/CI output was fine; only the live-spinner case lost them).
// The caller now prints client.Warnings itself, through its own text-output
// mechanism, AFTER the spinner has stopped. Warnings are still on
// GeneratedClient for any caller that wants them; this function just isn't
// the one that decides how or when to surface them.
func (m *OutputManager) WriteClient(client *generators.GeneratedClient, outputDir string) error {
	if err := checkCaseCollisions(client); err != nil {
		return err
	}

	// Create output directory with restrictive permissions
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	casing := caseFixer{}

	// Sorted, so a run that fails partway fails at the same file every time.
	filenames := make([]string, 0, len(client.Files))
	for filename := range client.Files {
		filenames = append(filenames, filename)
	}

	slices.Sort(filenames)

	// Write each file
	for _, filename := range filenames {
		content := client.Files[filename]
		filePath := filepath.Join(outputDir, filename)

		// Create subdirectories if needed
		dir := filepath.Dir(filePath)
		if dir != outputDir {
			if err := os.MkdirAll(dir, 0750); err != nil {
				return fmt.Errorf("create subdirectory %s: %w", dir, err)
			}
		}

		if err := casing.clear(filePath); err != nil {
			return fmt.Errorf("write file %s: %w", filename, err)
		}

		// Write file with restrictive permissions
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			return fmt.Errorf("write file %s: %w", filename, err)
		}
	}

	// Write README if instructions are provided
	if client.Instructions != "" {
		readmePath := filepath.Join(outputDir, "README.md")
		if err := casing.clear(readmePath); err != nil {
			return fmt.Errorf("write README: %w", err)
		}

		if err := os.WriteFile(readmePath, []byte(client.Instructions), 0600); err != nil {
			return fmt.Errorf("write README: %w", err)
		}
	}

	if err := m.pruneExclusiveDirs(client, outputDir); err != nil {
		return err
	}

	return nil
}

// checkCaseCollisions refuses a client with two files whose paths differ only
// by case.
//
// macOS and Windows disks fold case, so those two files are one file there and
// whichever is written second wins. The generators already keep their own
// names apart (the TypeScript one writes `ShareResponse.ts` next to
// `shareResponse-2.ts`), so this firing means a generator lost that, and
// failing here beats a tree whose contents depend on which disk it landed on.
func checkCaseCollisions(client *generators.GeneratedClient) error {
	seen := make(map[string]string, len(client.Files)+1)

	names := make([]string, 0, len(client.Files)+1)
	for name := range client.Files {
		names = append(names, name)
	}

	if client.Instructions != "" {
		if _, ok := client.Files["README.md"]; !ok {
			names = append(names, "README.md")
		}
	}

	slices.Sort(names)

	for _, name := range names {
		key := strings.ToLower(filepath.ToSlash(name))
		if other, ok := seen[key]; ok {
			return fmt.Errorf("generated files %s and %s differ only by case and would overwrite "+
				"each other on a case-insensitive disk", other, name)
		}

		seen[key] = name
	}

	return nil
}

// caseFixer removes a file's stale spelling before the file is written.
//
// On a case-insensitive disk, writing `ShareResponse.ts` over an existing
// `shareResponse.ts` keeps the old name. The tree then carries a spelling no
// import uses, and pruneExclusiveDirs, which compares names exactly, deletes
// it as stale, taking the codec that was just written with it. The next run
// writes it back, so the output depended on what was on disk before.
//
// A variant is removed only when it is the same file as the target. On a
// case-sensitive disk the two are different files, and the other one belongs
// to somebody else.
type caseFixer map[string]map[string][]string

func (c caseFixer) clear(path string) error {
	target, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Nothing there under any spelling: the write creates it as named.
		return nil
	}

	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	dir, base := filepath.Dir(path), filepath.Base(path)

	byFold, ok := c[dir]
	if !ok {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read %s: %w", dir, err)
		}

		byFold = make(map[string][]string, len(entries))
		for _, entry := range entries {
			key := strings.ToLower(entry.Name())
			byFold[key] = append(byFold[key], entry.Name())
		}

		c[dir] = byFold
	}

	for _, name := range byFold[strings.ToLower(base)] {
		if name == base {
			continue
		}

		variant := filepath.Join(dir, name)

		info, err := os.Stat(variant)
		if err != nil || !os.SameFile(info, target) {
			continue
		}

		if err := os.Remove(variant); err != nil {
			return fmt.Errorf("remove %s, a stale spelling of %s: %w", name, base, err)
		}
	}

	return nil
}

// pruneExclusiveDirs deletes files left over in the directories the generator
// declares it owns outright.
//
// Rewriting a file is how a generator withdraws something from it; there is no
// equivalent for a directory of one file per operation, so an operation
// removed from the specification would leave its module on disk. That module
// compiles, exports a hook, and points at an endpoint the server has stopped
// serving -- a failure at runtime rather than at build time, which is the
// worse of the two.
//
// Scoped to ExclusiveDirs and nothing else. It deletes, so what it may reach
// is a declaration by the generator that produced the tree, never an inference
// from where files happened to land.
func (m *OutputManager) pruneExclusiveDirs(client *generators.GeneratedClient, outputDir string) error {
	for _, dir := range client.ExclusiveDirs {
		root := filepath.Join(outputDir, filepath.FromSlash(dir))

		entries, err := os.ReadDir(root)
		if err != nil {
			// A directory this run did not produce is not stale, it is
			// absent: a spec with no operations emits no src/ops at all.
			if os.IsNotExist(err) {
				continue
			}

			return fmt.Errorf("read %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := dir + "/" + entry.Name()
			if _, generated := client.Files[name]; generated {
				continue
			}

			if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
				return fmt.Errorf("remove stale file %s: %w", name, err)
			}
		}
	}

	return nil
}

// GenerateREADME generates a README for the client.
func (m *OutputManager) GenerateREADME(config GeneratorConfig, spec *APISpec, authDocs string) string {
	var readme strings.Builder

	// Title
	fmt.Fprintf(&readme, "# %s Client\n\n", spec.Info.Title)

	if spec.Info.Description != "" {
		readme.WriteString(spec.Info.Description + "\n\n")
	}

	fmt.Fprintf(&readme, "Generated client for %s %s\n\n", spec.Info.Title, spec.Info.Version)

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
		fmt.Fprintf(&readme, "[%s](%s)\n\n", spec.Info.License.Name, spec.Info.License.URL)
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
	fmt.Fprintf(&example, "    \"%s\"\n", config.PackageName)
	example.WriteString("    \"context\"\n")
	example.WriteString("    \"log\"\n")
	example.WriteString(")\n\n")
	example.WriteString("func main() {\n")
	fmt.Fprintf(&example, "    client := %s.NewClient(\n", config.PackageName)

	if config.BaseURL != "" {
		fmt.Fprintf(&example, "        %s.WithBaseURL(\"%s\"),\n", config.PackageName, config.BaseURL)
	}

	if config.IncludeAuth && NeedsAuthConfig(spec) {
		fmt.Fprintf(&example, "        %s.WithAuth(%s.AuthConfig{\n", config.PackageName, config.PackageName)
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
	fmt.Fprintf(&example, "import { %s } from '%s';\n\n", config.APIName, config.PackageName)
	fmt.Fprintf(&example, "const client = new %s({\n", config.APIName)

	if config.BaseURL != "" {
		fmt.Fprintf(&example, "  baseURL: '%s',\n", config.BaseURL)
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

	fmt.Fprintf(&overview, "- **Total Endpoints**: %d\n", stats.TotalEndpoints)
	fmt.Fprintf(&overview, "- **REST Endpoints**: %d\n", stats.RESTEndpoints)

	if stats.WebSocketCount > 0 {
		fmt.Fprintf(&overview, "- **WebSocket Endpoints**: %d\n", stats.WebSocketCount)
	}

	if stats.SSECount > 0 {
		fmt.Fprintf(&overview, "- **SSE Endpoints**: %d\n", stats.SSECount)
	}

	if stats.SecuredEndpoints > 0 {
		fmt.Fprintf(&overview, "- **Secured Endpoints**: %d\n", stats.SecuredEndpoints)
	}

	if len(stats.Tags) > 0 {
		fmt.Fprintf(&overview, "- **Tags**: %s\n", strings.Join(stats.Tags, ", "))
	}

	if len(spec.Servers) > 0 {
		overview.WriteString("\n### Servers\n\n")

		for _, server := range spec.Servers {
			fmt.Fprintf(&overview, "- **%s**", server.URL)

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
