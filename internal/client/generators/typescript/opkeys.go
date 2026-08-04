package typescript

import (
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// operationKeys returns one key per endpoint, in endpoint order, for the
// operation manifest (ops.ts) and the hook facades (hooks.ts).
//
// Both files must agree on these strings -- hooks.ts indexes the very table
// ops.ts declares -- so they are computed once, here, rather than derived
// twice from the endpoint.
//
// Why this exists at all: the emitters used to key off Endpoint.ID, which
// NEITHER intermediate-representation builder ever populates for a REST
// endpoint (introspector.operationToEndpoint and spec_parser.convertOperation
// both fill OperationID; only the WebSocket/SSE builders set ID). Generating
// from a real spec file therefore emitted `”: { ... }` for every endpoint --
// duplicate object keys in ops.ts and duplicate `export const use` bindings in
// hooks.ts, which is not merely wrong but unparseable. Every test that covered
// these emitters hand-built the IR with ID already set, so the fixtures agreed
// with each other and disagreed with production.
//
// The fallback chain is:
//
//	ID          -- explicit IR-level identifier: what the streaming builders
//	               set, and what a hand-assembled spec may set.
//	OperationID -- what both OpenAPI builders populate from the spec.
//	path-derived -- operationIDFromPath, the SAME rule rest.go already applies
//	               to endpoints with no operationId, so the manifest and the
//	               REST client name the same operation the same way.
//
// Uniqueness is enforced last: a spec is free to repeat an operationId, and
// two operations collapsing onto one key would silently drop an entry from a
// `const` object and emit a duplicate `export const`. Collisions get a numeric
// suffix, which is deterministic because endpoint order is (see the sorted
// path/method walk in both builders).
func operationKeys(endpoints []client.Endpoint) []string {
	keys := make([]string, len(endpoints))
	taken := make(map[string]bool, len(endpoints))

	for i := range endpoints {
		base := endpointKey(&endpoints[i])

		key := base
		for n := 2; taken[key]; n++ {
			key = base + strconv.Itoa(n)
		}

		taken[key] = true
		keys[i] = key
	}

	return keys
}

// endpointKey picks the best available name for one endpoint, before
// uniquification.
func endpointKey(ep *client.Endpoint) string {
	if ep.ID != "" {
		return ep.ID
	}

	if ep.OperationID != "" {
		return ep.OperationID
	}

	if derived := operationIDFromPath(*ep); derived != "" && derived != "." {
		return derived
	}

	return "operation"
}

// operationIDFromPath creates an operation ID from an endpoint's path and
// method. Package-level so the manifest, the hooks and the REST client all
// name an unnamed operation identically; RESTGenerator.generateOperationIDFromPath
// delegates here.
func operationIDFromPath(endpoint client.Endpoint) string {
	path := strings.TrimPrefix(endpoint.Path, "/")
	path = strings.ReplaceAll(path, "/", ".")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.ReplaceAll(path, "-", "")

	method := strings.ToLower(endpoint.Method)

	return method + "." + path
}

// hookNames renders one `useX` identifier per operation key, in the same
// order, keeping them unique.
//
// Unique keys do not guarantee unique hook names: toPascal collapses
// separators, so `list-orders` and `list_orders` are distinct object keys that
// would both become `useListOrders` -- two `export const` declarations of the
// same name, which TypeScript rejects outright.
func hookNames(keys []string) []string {
	names := make([]string, len(keys))
	taken := make(map[string]bool, len(keys))

	for i, key := range keys {
		base := hookName(key)

		name := base
		for n := 2; taken[name]; n++ {
			name = base + strconv.Itoa(n)
		}

		taken[name] = true
		names[i] = name
	}

	return names
}
