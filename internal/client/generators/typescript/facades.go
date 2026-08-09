package typescript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// FacadeGenerator emits src/hooks.ts: one typed line per endpoint, delegating
// to the runtime.
//
// No per-endpoint logic is generated. Everything a hook does lives in
// @forge-go/client-core, so a defect there is fixed by publishing a package
// rather than by regenerating every repository that consumes this client.
type FacadeGenerator struct{}

func NewFacadeGenerator() *FacadeGenerator { return &FacadeGenerator{} }

// Generate produces hooks.ts.
func (g *FacadeGenerator) Generate(spec *client.APISpec, _ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(`/**
 * Typed hooks over the operation manifest.
 *
 * Generated. Each line is a binding, not an implementation.
 *
 * The bodies live in @forge-go/client-core, so a runtime defect is fixed by
 * upgrading that package rather than by regenerating this one.
 */

import { query, mutation } from '@forge-go/client-core';
import { ops } from './ops';

`)

	// Keys come from the same helper the manifest uses, so every hook indexes
	// an entry ops.ts actually declares. Access goes through tsMember rather
	// than `ops.` + tsKey: a key that is not a bare identifier is quoted by
	// tsKey, and `ops.'list-orders'` does not parse.
	keys := operationKeys(spec.Endpoints)
	names := hookNames(keys)

	var lines strings.Builder
	imports := map[string]bool{}

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]

		if isReadMethod(ep.Method) {
			lines.WriteString(fmt.Sprintf("export const %s = query(%s);\n",
				names[i], tsMember("ops", keys[i])))

			continue
		}

		lines.WriteString(fmt.Sprintf("export const %s = mutation%s(%s);\n",
			names[i], mutationTypeArgs(ep, imports), tsMember("ops", keys[i])))
	}

	if len(imports) > 0 {
		named := make([]string, 0, len(imports))
		for name := range imports {
			named = append(named, name)
		}
		// Sorted, because this file is byte-diffed by CI and a map's iteration
		// order is deliberately not stable in Go.
		sort.Strings(named)

		buf.WriteString(fmt.Sprintf("import type { %s } from './types';\n\n", strings.Join(named, ", ")))
	}

	buf.WriteString(lines.String())

	return buf.String()
}

// mutationTypeArgs renders the `<Response, Entity>` a mutation binding carries,
// and records the type names it referenced so the import line can be written.
//
// Both are needed and neither implies the other: RootType names the response
// DOCUMENT while Entity.Type names the record an optimistic patch is checked
// against, and an enveloped create makes them different types. A patch typed
// against the envelope would accept `items` and reject `total`, which is
// exactly backwards.
//
// An endpoint missing either name emits nothing rather than a partial argument
// list: `mutation<Order>` would silently leave the entity as `unknown`, and a
// patch checked against `unknown` accepts every misspelling -- the silent no-op
// this typing exists to prevent.
//
// RootType and Entity.Type are used RAW here, not run through toPascal.
// types.ts (generateTypes in generator.go) exports every schema under its
// literal spec.Schemas key -- `export interface %s`, with `name` taken
// straight from sortedKeys(spec.Schemas) -- and both RootType and Entity.Type
// are themselves derived from that same raw key (schemaName in
// introspector.go reads it off a $ref; the x-forge-entity extension path
// takes it verbatim from the spec author). So the two are already the same
// string; canonicalising one side would only matter if it diverged from
// types.ts, and it would diverge in the wrong direction -- a schema named
// `order_summary` would import `OrderSummary`, a name types.ts never
// exports. Every fixture in this repo happens to use already-PascalCase
// schema names, which is exactly why that bug would pass every existing
// test and only surface against a real spec with a snake_case component
// name.
func mutationTypeArgs(ep *client.Endpoint, imports map[string]bool) string {
	if ep.RootType == "" || ep.Entity == nil || ep.Entity.Type == "" {
		return ""
	}

	imports[ep.RootType] = true
	imports[ep.Entity.Type] = true

	return fmt.Sprintf("<%s, %s>", ep.RootType, ep.Entity.Type)
}

// isReadMethod reports whether an endpoint reads rather than writes. Caching
// a POST would serve a stale answer to a request whose entire purpose was to
// change something.
func isReadMethod(method string) bool {
	m := strings.ToUpper(method)

	return m == "GET" || m == "HEAD"
}

// hookName renders `orderList` as `useOrderList`.
//
// Delegates to toPascal (naming.go) rather than a bespoke
// ToUpper(id[:1])+id[1:]: toPascal already splits on underscores, hyphens
// and acronym runs the way the rest of this package does, so an id like
// `order_list` becomes `OrderList` rather than the mangled `Order_list` a
// first-letter uppercase would produce. For a plain camelCase id such as
// `orderList`, splitWords finds the same single lower-to-upper boundary a
// manual uppercase would target, so the two approaches agree exactly on the
// cases this generator actually sees -- reusing toPascal costs nothing and
// keeps hook naming consistent with every other identifier this package
// renders.
func hookName(id string) string {
	if id == "" {
		return "use"
	}

	return "use" + toPascal(id)
}
