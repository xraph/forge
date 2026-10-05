package dart

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// Dependency constraints, from the contracts' version table.
const (
	sdkConstraint         = "^3.13.0"
	httpConstraint        = "^1.6.0"
	forgeClientConstraint = "^1.0.0-dev"
)

// renderPubspec renders pubspec.yaml. A package generated with hooks depends
// on forge_client; one without depends on package:http alone.
func renderPubspec(spec *client.APISpec, config client.GeneratorConfig) string {
	var b strings.Builder

	version := config.Version
	if version == "" {
		version = "1.0.0"
	}

	description := packageSummary(spec.Info.Description)
	if description == "" {
		description = "Generated Forge client for " + spec.Info.Title + "."
	}

	fmt.Fprintf(&b, "name: %s\n", config.PackageName)
	fmt.Fprintf(&b, "description: %s\n", yamlString(description))
	fmt.Fprintf(&b, "version: %s\n", version)
	b.WriteString("publish_to: none\n\n")
	b.WriteString("environment:\n")
	fmt.Fprintf(&b, "  sdk: %s\n\n", sdkConstraint)
	b.WriteString("dependencies:\n")

	if config.HooksEnabled() {
		fmt.Fprintf(&b, "  forge_client: %s\n", forgeClientConstraint)
	}

	fmt.Fprintf(&b, "  http: %s\n", httpConstraint)

	return b.String()
}

// packageSummary reduces a description to its first paragraph on one line.
func packageSummary(description string) string {
	paragraph, _, _ := strings.Cut(strings.TrimSpace(description), "\n\n")

	return strings.Join(strings.Fields(paragraph), " ")
}

// yamlString renders s as a double-quoted YAML scalar.
func yamlString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)

	return `"` + r.Replace(s) + `"`
}

// renderBarrel renders lib/<package>.dart, which exports every public file.
func renderBarrel(spec *client.APISpec, files map[string]string) string {
	var exports []string

	for name := range files {
		if !strings.HasPrefix(name, "lib/src/") || !strings.HasSuffix(name, ".dart") {
			continue
		}

		rel := strings.TrimPrefix(name, "lib/")

		switch {
		case strings.HasPrefix(rel, "src/codecs/"), rel == "src/support.dart", rel == "src/sync.dart":
			continue
		}

		exports = append(exports, fmt.Sprintf("export '%s';", rel))
	}

	exports = append(exports, "export 'src/support.dart' show Assign, Int64, Json, Unchanged, Value;")
	sort.Strings(exports)

	var b strings.Builder

	b.WriteString(generatedHeader)
	b.WriteString("\n")
	b.WriteString(docComment(packageSummary(spec.Info.Description), "Generated Forge client for "+spec.Info.Title+".", ""))
	b.WriteString("library;\n\n")
	b.WriteString(strings.Join(exports, "\n"))
	b.WriteString("\n")

	return b.String()
}
