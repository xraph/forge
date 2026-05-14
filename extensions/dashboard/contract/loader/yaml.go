// yaml.go
package loader

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// SupportedSchemaVersion is the schema integer this loader understands.
// Bumping it requires a coordinated platform release (see DESIGN.md).
const SupportedSchemaVersion = 1

// Load parses a contributor manifest YAML stream and validates its schemaVersion.
// Cross-reference validation (intent refs, slot accepts, warden names) runs separately
// in Validate.
func Load(r io.Reader, source string) (*contract.ContractManifest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", source, err)
	}
	var m contract.ContractManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", source, err)
	}
	if m.SchemaVersion != SupportedSchemaVersion {
		return nil, fmt.Errorf("%s: schemaVersion=%d unsupported, want %d", source, m.SchemaVersion, SupportedSchemaVersion)
	}
	return &m, nil
}
