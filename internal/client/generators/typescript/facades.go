package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// FacadeGenerator emits src/hooks.ts: one typed line per endpoint, delegating
// to the runtime.
//
// No per-endpoint logic is generated. Everything a hook does lives in
// @forge/client-core, so a defect there is fixed by publishing a package
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
 * Requires @forge/client-core, which is not published yet -- it ships in a
 * later phase. "npm install" will fail on this package until it is; see the
 * generated README for details. The REST client in this same package does
 * not depend on it and works today.
 */

import { query, mutation } from '@forge/client-core';
import { ops } from './ops';

`)

	// Keys come from the same helper the manifest uses, so every hook indexes
	// an entry ops.ts actually declares. Access goes through tsMember rather
	// than `ops.` + tsKey: a key that is not a bare identifier is quoted by
	// tsKey, and `ops.'list-orders'` does not parse.
	keys := operationKeys(spec.Endpoints)
	names := hookNames(keys)

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]

		factory := "mutation"
		if isReadMethod(ep.Method) {
			factory = "query"
		}

		buf.WriteString(fmt.Sprintf("export const %s = %s(%s);\n",
			names[i], factory, tsMember("ops", keys[i])))
	}

	return buf.String()
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
