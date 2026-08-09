package typescript

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// CapabilityGenerator emits src/capabilities.ts: the scopes this API's routes
// declare, as a union type, plus the predicates an interface uses to decide
// what to render.
//
// The file is deliberately dependency-free -- it imports nothing, not even
// @forge-go/client-core. Capability gating is useful to a plain REST client
// with hooks turned off and to an AsyncAPI-only client with no REST endpoints
// at all, and generatePackageJSON only declares the runtime dependency when
// hooks are enabled. Holding the granted set in this module rather than in the
// runtime is what lets the feature exist in every generation mode without
// adding a dependency to clients that today have none.
//
// What it is NOT is stated at the top of the generated file rather than only in
// the documentation, because the file is what somebody reads at the call site:
// every answer here is computed from client-held state and is an affordance,
// never an authorization decision.
type CapabilityGenerator struct{}

func NewCapabilityGenerator() *CapabilityGenerator { return &CapabilityGenerator{} }

// capabilitiesNeeded reports whether the spec declares any scope at all.
//
// When it declares none, the Capability union would be `never` -- a type no
// call site can satisfy, on a module whose every function would be
// uncallable -- so the file is not emitted at all rather than emitted useless.
// Same shape as codecsNeeded: a run that gains nothing from a file does not
// get one, and the barrel export is gated on the identical condition.
func capabilitiesNeeded(spec *client.APISpec) bool {
	return len(client.NewAuthCodeGenerator().CollectCapabilities(spec)) > 0
}

// capabilityExports returns every name src/capabilities.ts exports for this
// spec -- which is exactly what a barrel `export *` would re-export, and
// therefore what can clash with a name some other generated module exports.
//
// Sorted, because the collision report built from it reaches the user-visible
// warnings slice, whose order is asserted to be stable.
func capabilityExports(spec *client.APISpec) []string {
	names := []string{"Capability", "can", "capabilitiesKnown", "setCapabilities"}

	if len(spec.Endpoints) > 0 {
		names = append(names,
			"OperationName", "canCall", "missingCapabilities", "requiredCapabilities")
	}

	sort.Strings(names)

	return names
}

// capabilityExportCollisions returns the capability exports a schema in this
// spec already claims.
//
// `Capability` and `OperationName` are ordinary words, and a Go API is
// perfectly entitled to a type of either name. When one has it, types.ts
// exports that name and so does capabilities.ts, and a barrel that re-exports
// both with `export *` does not merely shadow one -- TypeScript rejects the
// package outright (TS2308), so a client that has nothing to do with capability
// gating stops compiling because of a file it never imports.
//
// Schema keys are compared verbatim, matching checkSchemaNameCollisions, which
// reads them as the emitted type names because that is what generateTypes makes
// of them.
func capabilityExportCollisions(spec *client.APISpec) []string {
	if !capabilitiesNeeded(spec) {
		return nil
	}

	var collisions []string

	for _, name := range capabilityExports(spec) {
		if _, taken := spec.Schemas[name]; taken {
			collisions = append(collisions, name)
		}
	}

	return collisions
}

// collisionSubject renders a collision list as the subject of "... already
// named by a schema", so the warning reads as a sentence for one name and for
// several.
func collisionSubject(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = strconv.Quote(name)
	}

	if len(quoted) == 1 {
		return quoted[0] + " is"
	}

	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1] + " are"
}

// Generate produces capabilities.ts.
func (g *CapabilityGenerator) Generate(spec *client.APISpec, _ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(`/**
 * Capability constants and the can() helper.
 *
 * Generated from the scopes routes declare through WithRequiredAuth.
 *
 * ===========================================================================
 * A UX AFFORDANCE. NEVER A SECURITY BOUNDARY.
 *
 * Every value in this file is client-held and therefore attacker-controlled:
 * anyone can call setCapabilities() from a browser console and flip every
 * answer below to true. Authorization is enforced server-side, on every
 * request, unconditionally. Hiding a button is not access control.
 *
 * Nothing here blocks a request. Every predicate below answers a question; none
 * enforces the answer, and no generated request path consults one. A check that
 * passes here can still be refused by the server, and that refusal is the one
 * that counts.
 * ===========================================================================
 */

`)

	g.writeCapabilityUnion(&buf, spec)

	// Everything below is keyed by REST operation, so a spec whose scopes come
	// only from WebSocket, SSE or WebTransport routes gets the union and the
	// predicates over it and stops there. Emitting an OperationName union with
	// no members would make it `never`, and canCall would be a function no
	// caller could pass an argument to.
	if len(spec.Endpoints) > 0 {
		g.writeOperationUnion(&buf, spec)
		g.writeRequirements(&buf, spec)
	}

	g.writeState(&buf)

	if len(spec.Endpoints) > 0 {
		g.writeOperationPredicates(&buf)
	}

	return buf.String()
}

// writeCapabilityUnion emits the Capability type: every distinct scope in the
// spec, sorted.
func (g *CapabilityGenerator) writeCapabilityUnion(buf *strings.Builder, spec *client.APISpec) {
	buf.WriteString(`/**
 * Every scope any route in this API declares.
 *
 * A union rather than a bare string, so a misspelling is a compile error
 * instead of a silent false. That is the point of generating a type at all: a
 * typo that merely returns false hides an action forever, and is
 * indistinguishable -- at the call site and on screen -- from a permission the
 * user genuinely lacks.
 */
`)

	writeUnion(buf, "Capability", client.NewAuthCodeGenerator().CollectCapabilities(spec))
}

// writeOperationUnion emits OperationName: every endpoint, gated or not.
func (g *CapabilityGenerator) writeOperationUnion(buf *strings.Builder, spec *client.APISpec) {
	buf.WriteString(`/**
 * Every operation in this API, gated or not.
 *
 * Ungated operations are members too, so canCall() accepts any operation and
 * answers true for the ones that require nothing. Restricting the union to
 * gated operations would type-check the wrong thing -- it would make
 * canCall(anUngatedOperation) a compile error, forcing every caller to track
 * which endpoints happen to carry scopes, and to revisit that knowledge each
 * time a route's scopes change server-side.
 *
 * Names match the keys of the operation manifest (ops.ts) where one is
 * generated, because both come from the same helper.
 */
`)

	writeUnion(buf, "OperationName", operationKeys(spec.Endpoints))
}

// writeUnion emits a string-literal union, one member per line.
//
// The terminating semicolon rides on the last member rather than sitting on a
// line of its own, which is what Prettier produces and therefore what the
// generated lint setup (see linting.go) would otherwise rewrite on the first
// `npm run format` -- turning a freshly generated file into a diff.
//
// An empty member list degrades to `never` rather than emitting `export type X
// =;`, which does not parse. No caller reaches that today -- both unions are
// gated on being non-empty -- but a type nobody can satisfy is a far better
// failure than a module that cannot be loaded at all.
func writeUnion(buf *strings.Builder, name string, members []string) {
	if len(members) == 0 {
		buf.WriteString(fmt.Sprintf("export type %s = never;\n\n", name))

		return
	}

	buf.WriteString(fmt.Sprintf("export type %s =\n", name))

	for i, member := range members {
		terminator := "\n"
		if i == len(members)-1 {
			terminator = ";\n\n"
		}

		buf.WriteString(fmt.Sprintf("  | %s%s", tsString(member), terminator))
	}
}

// writeRequirements emits the per-operation scope table.
//
// Endpoint order, which both intermediate-representation builders make
// deterministic by walking paths in sorted order and methods in a fixed one --
// the same property ops.ts relies on.
func (g *CapabilityGenerator) writeRequirements(buf *strings.Builder, spec *client.APISpec) {
	buf.WriteString(`/**
 * What each gated operation requires, as ALTERNATIVES: holding every scope in
 * any ONE inner array permits the operation.
 *
 * The nesting is OpenAPI's own semantics rather than an invention -- security
 * requirements are ORed against each other, the scopes within one are ANDed.
 * WithRequiredAuth('jwt', 'write:users', 'admin') emits a single alternative
 * demanding both scopes; a route offering two providers emits one alternative
 * each, deduplicated where they demand the same thing.
 *
 * Operations requiring no scope are ABSENT rather than present with an empty
 * array, so a reader can tell "this route is not scope-gated" from "this route
 * is gated on nothing", which would otherwise look identical.
 */
export const requiredCapabilities = {
`)

	auth := client.NewAuthCodeGenerator()
	keys := operationKeys(spec.Endpoints)

	for i := range spec.Endpoints {
		alternatives := auth.EndpointCapabilities(spec.Endpoints[i])
		if len(alternatives) == 0 {
			continue
		}

		rendered := make([]string, 0, len(alternatives))
		for _, alternative := range alternatives {
			rendered = append(rendered, tsStringArray(alternative))
		}

		buf.WriteString(fmt.Sprintf("  %s: [%s],\n", tsKey(keys[i]), strings.Join(rendered, ", ")))
	}

	buf.WriteString(`} as const satisfies Partial<Record<OperationName, readonly (readonly Capability[])[]>>;

`)
}

// writeState emits the granted-scope store and the two predicates over it.
func (g *CapabilityGenerator) writeState(buf *strings.Builder) {
	buf.WriteString(`/**
 * The current principal's granted scopes, or undefined when nothing has told
 * this module anything yet.
 *
 * Undefined rather than an empty set on purpose: "we do not know yet" and "we
 * know, and it is none" are different answers that must not collapse into one.
 * capabilitiesKnown() is how a caller tells them apart.
 */
let granted: ReadonlySet<string> | undefined;

/**
 * Declare the current principal's granted scopes. Call with no argument to
 * forget them.
 *
 * Call this on sign-in, after a token refresh that changes scopes, and with no
 * argument on sign-out. This module holds no identity of its own and nothing
 * clears it for you: a client that switches principal without calling this
 * keeps answering for the previous one, and will offer actions the new user
 * does not hold -- which the server then refuses, because the server is what
 * decides.
 *
 * Takes arbitrary strings rather than Capability because granted scopes come
 * from a token, and a server is free to grant scopes this client's
 * specification never mentioned. Unrecognised ones are stored and simply never
 * asked about; dropping them would be a lie about what the principal holds.
 *
 * The state is module-scoped, which means per-process, which means this is for
 * a browser. Calling it while server-rendering shares one principal's scopes
 * with every request the process is handling concurrently, and the interface
 * one user receives is rendered against another's. Server-side authorization is
 * unaffected -- it never consults this -- so the damage is a wrong interface
 * rather than a breach, but it is wrong for everybody at once. Derive what to
 * render from the request's own session there instead.
 */
export function setCapabilities(scopes?: Iterable<string>): void {
  granted = scopes === undefined ? undefined : new Set(scopes);
}

/**
 * Whether setCapabilities() has been called with a scope set that has not
 * since been forgotten.
 *
 * The reason can() alone is not enough. Before anything is known can() answers
 * false, which is indistinguishable from a denial, so an interface that
 * renders straight off it hides every action during load and pops them all
 * into existence afterwards. A caller that would rather show a placeholder
 * than flicker checks this first.
 */
export function capabilitiesKnown(): boolean {
  return granted !== undefined;
}

/**
 * Whether the current principal holds a capability.
 *
 * False before any capabilities are known -- see capabilitiesKnown(). Never
 * a security decision; see this file's header.
 */
export function can(capability: Capability): boolean {
  return granted !== undefined && granted.has(capability);
}

`)
}

// writeOperationPredicates emits the per-operation half: what is missing, and
// the boolean over it.
func (g *CapabilityGenerator) writeOperationPredicates(buf *strings.Builder) {
	buf.WriteString(`/**
 * requiredCapabilities widened to its declared shape, so indexing it by an
 * operation that carries no scopes reads as absent rather than as an error
 * about a key the const object does not have.
 */
const required: Partial<Record<OperationName, readonly (readonly Capability[])[]>> =
  requiredCapabilities;

/**
 * The smallest set of capabilities the current principal is missing before the
 * given operation would be permitted, or an empty array when nothing is
 * missing.
 *
 * Smallest ACROSS alternatives, so where an operation can be reached more than
 * one way the answer names the cheapest route to it rather than an arbitrary
 * one -- which is what makes it usable in a message telling somebody what they
 * would need.
 *
 * Empty for an operation that requires no scope. Before any capabilities are
 * known every required scope reads as missing, consistent with can().
 */
export function missingCapabilities(operation: OperationName): readonly Capability[] {
  const alternatives = required[operation];

  if (alternatives === undefined || alternatives.length === 0) return [];

  let fewest: readonly Capability[] | undefined;

  for (const alternative of alternatives) {
    const missing = alternative.filter((capability) => !can(capability));

    if (missing.length === 0) return [];
    if (fewest === undefined || missing.length < fewest.length) fewest = missing;
  }

  return fewest ?? [];
}

/**
 * Whether the current principal could call the given operation without being
 * refused.
 *
 * Advisory, and false before any capabilities are known. Nothing in the
 * generated request path consults this: call it yourself, at a call site you
 * control, to skip a round trip you expect to fail or to explain why an action
 * is unavailable. A request issued regardless is still sent, and still
 * authorized by the server.
 */
export function canCall(operation: OperationName): boolean {
  return missingCapabilities(operation).length === 0;
}
`)
}
