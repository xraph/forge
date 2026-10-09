package spec

import _ "embed"

//go:embed deploy.schema.json
var schemaJSON []byte

// Schema returns the JSON Schema for the deploy section.
func Schema() ([]byte, error) {
	out := make([]byte, len(schemaJSON))
	copy(out, schemaJSON)

	return out, nil
}
