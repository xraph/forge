package golang

import (
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// operationKeys returns one key per endpoint, in endpoint order, for the
// capability surface's per-operation requirement table (capabilities.go).
//
// This is the Go generator's version of the TypeScript generator's own
// opkeys.go, which capabilities.ts's OperationName union and
// requiredCapabilities/requiredAuthorization tables are keyed by. The two
// generators do not share code -- Go has no ops.ts/hooks.ts equivalent to
// keep in sync with -- but they must agree on the fallback chain, since a
// spec fed to both is expected to name the same operation the same way in
// both generated clients.
//
// The fallback chain is:
//
//	ID          -- explicit IR-level identifier: what the streaming builders
//	               set, and what a hand-assembled spec may set.
//	OperationID -- what both OpenAPI builders populate from the spec.
//	path-derived -- operationIDFromPath, the same rule RESTGenerator applies
//	               internally to endpoints with no operationId.
//
// Uniqueness is enforced last: a spec is free to repeat an operationId, and
// two operations collapsing onto one key would silently drop an entry from
// the operationRequirements map and emit a duplicate OperationName constant
// (a compile error). Collisions get a numeric suffix, which is deterministic
// because endpoint order is (both intermediate-representation builders walk
// paths in sorted order and methods in a fixed one).
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

// operationIDFromPath creates an operation key from an endpoint's path and
// method, for an endpoint with neither an ID nor an OperationID.
func operationIDFromPath(endpoint client.Endpoint) string {
	path := strings.TrimPrefix(endpoint.Path, "/")
	path = strings.ReplaceAll(path, "/", ".")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.ReplaceAll(path, "-", "")

	method := strings.ToLower(endpoint.Method)

	return method + "." + path
}
