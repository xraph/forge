package client

import (
	"sort"

	"github.com/xraph/forge/internal/shared"
)

// Deterministic walks over the map-shaped parts of a parsed specification.
//
// Both intermediate-representation builders (Introspector.extractFromOpenAPI
// and SpecParser.parseOpenAPI) used to range straight over spec.Paths and over
// a locally built `methods` map. Go randomizes map iteration, so two parses of
// the same file produced two different Endpoint orders -- and every generator
// that walks spec.Endpoints in order (rest.ts, ops.ts, hooks.ts) then emitted a
// different byte sequence each run. A CI check that regenerates and diffs sees
// that as drift on every run, which trains everyone to ignore it.

// sortedPathKeys returns the paths of a spec in ascending order.
func sortedPathKeys(paths map[string]*shared.PathItem) []string {
	keys := make([]string, 0, len(paths))
	for path := range paths {
		keys = append(keys, path)
	}

	sort.Strings(keys)

	return keys
}

// pathOperation pairs an HTTP method with the operation declared under it.
type pathOperation struct {
	Method string
	Op     *shared.Operation
}

// orderedPathOps returns a path item's declared operations in a fixed method
// order, skipping the methods the path does not declare.
//
// The order is the conventional CRUD reading order rather than alphabetical:
// it is what a reader scanning generated output expects, and any fixed order
// satisfies determinism equally well.
func orderedPathOps(pathItem *shared.PathItem) []pathOperation {
	if pathItem == nil {
		return nil
	}

	candidates := [...]pathOperation{
		{"GET", pathItem.Get},
		{"POST", pathItem.Post},
		{"PUT", pathItem.Put},
		{"PATCH", pathItem.Patch},
		{"DELETE", pathItem.Delete},
		{"HEAD", pathItem.Head},
		{"OPTIONS", pathItem.Options},
	}

	ops := make([]pathOperation, 0, len(candidates))

	for _, c := range candidates {
		if c.Op != nil {
			ops = append(ops, c)
		}
	}

	return ops
}

// sortedStringKeys returns a map's keys in ascending order. Used for the
// AsyncAPI operation and channel maps, which decide both the order streaming
// endpoints land in the IR and -- where several operations share a channel --
// which one is converted first.
func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
