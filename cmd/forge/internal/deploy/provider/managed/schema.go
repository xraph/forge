package managed

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas/*.json
var schemas embed.FS

// ValidateSchema checks a generated document against the pinned upstream schema.
func ValidateSchema(name string, doc map[string]any) error {
	raw, err := schemas.ReadFile("schemas/" + name + ".json")
	if err != nil {
		return fmt.Errorf("provider schema unavailable: %w", err)
	}

	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}

	compiler := jsonschema.NewCompiler()

	url := "https://forge.xraph.dev/schemas/" + name + ".json"
	if err := compiler.AddResource(url, schema); err != nil {
		return err
	}

	compiled, err := compiler.Compile(url)
	if err != nil {
		return err
	}
	// Normalize YAML integer/map representations to the JSON data model.
	encoded, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return err
	}

	return compiled.Validate(normalized)
}
