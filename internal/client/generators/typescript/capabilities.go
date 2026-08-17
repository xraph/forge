package typescript

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// CapabilityGenerator emits src/capabilities.ts: the scopes, roles and
// permissions this API's routes declare, as union types, plus the
// predicates an interface uses to decide what to render.
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

// capabilitiesNeeded reports whether the spec declares anything the
// capability file could describe: a scope, a role, or a permission.
//
// When it declares none of the three, Capability, Role and Permission would
// all be `never` -- types no call site can satisfy, on a module whose every
// function would be uncallable -- so the file is not emitted at all rather
// than emitted useless. Same shape as codecsNeeded: a run that gains nothing
// from a file does not get one, and the barrel export is gated on the
// identical condition.
//
// Widened from scope-only (the original condition) because a spec can
// declare WithAnyRole or WithAllPermissions on a route that carries
// no WithRequiredAuth scope at all -- roles and permissions are declared
// through Authorization, a field independent of Security. Gating on scopes
// alone would silently withhold hasRole/hasPermission from exactly the specs
// that use only those two, which is the shape this widening exists for.
func capabilitiesNeeded(spec *client.APISpec) bool {
	auth := client.NewAuthCodeGenerator()

	return len(auth.CollectCapabilities(spec)) > 0 ||
		len(auth.CollectRoles(spec)) > 0 ||
		len(auth.CollectPermissions(spec)) > 0
}

// capabilityExports returns every name src/capabilities.ts exports for this
// spec -- which is exactly what a barrel `export *` would re-export, and
// therefore what can clash with a name some other generated module exports.
//
// Every export the file can emit must be listed here, not just the ones some
// particular spec happens to trigger: Role and Permission are, if anything,
// MORE ordinary words than Capability was when this list first existed for
// that reason alone, and a name missing from this list is a name
// capabilityExportCollisions cannot protect. setPrincipal deliberately takes
// an inline object type rather than a named `Principal` interface for the
// same reason: a named export not on this list would be exactly the same
// defect wearing a different name.
//
// Sorted, because the collision report built from it reaches the user-visible
// warnings slice, whose order is asserted to be stable.
func capabilityExports(spec *client.APISpec) []string {
	names := []string{
		"Capability", "Permission", "Role",
		"can", "capabilitiesKnown", "hasPermission", "hasRole",
		"setCapabilities", "setPrincipal",
	}

	if len(spec.Endpoints) > 0 {
		names = append(names,
			"OperationName", "canCall", "missingCapabilities",
			"requiredAuthorization", "requiredCapabilities")
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
 * Capability, role and permission constants, and the predicates over them.
 *
 * Generated from the scopes, roles and permissions routes declare through
 * WithRequiredAuth, WithAnyRole and WithAllPermissions.
 *
 * ===========================================================================
 * A UX AFFORDANCE. NEVER A SECURITY BOUNDARY.
 *
 * Every value in this file is client-held and therefore attacker-controlled:
 * anyone can call setPrincipal() (or the deprecated setCapabilities()) from a browser console and flip every
 * answer below to true. Authorization is enforced server-side, on every
 * request, unconditionally. Hiding a button is not access control.
 *
 * hasRole() is the one to watch most closely, more than can(). can('read:users')
 * reads like a hint a caller checks before trying something risky.
 * hasRole('admin') reads like a fact, as though the client itself knows who
 * the administrators are. It does not, and it never will:
 * the server decides who holds a role, on every request, unconditionally.
 * hasPermission() carries the identical risk for permissions.
 *
 * Nothing here blocks a request. Every predicate below answers a question; none
 * enforces the answer, and no generated request path consults one. A check that
 * passes here can still be refused by the server, and that refusal is the one
 * that counts.
 * ===========================================================================
 */

`)

	g.writeCapabilityUnion(&buf, spec)
	g.writeRoleUnion(&buf, spec)
	g.writePermissionUnion(&buf, spec)

	// Everything below is keyed by REST operation, so a spec whose scopes,
	// roles and permissions come only from WebSocket, SSE or WebTransport
	// routes gets the three unions and the predicates over them and stops
	// there. Emitting an OperationName union with no members would make it
	// `never`, and canCall would be a function no caller could pass an
	// argument to.
	if len(spec.Endpoints) > 0 {
		g.writeOperationUnion(&buf, spec)
		g.writeRequirements(&buf, spec)
		g.writeAuthorizationRequirements(&buf, spec)
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

// writeRoleUnion emits the Role type: every distinct role in the spec,
// sorted.
//
// Emitted unconditionally alongside Capability, in the same place and the
// same way -- even a spec that declares no role at all still gets
// `export type Role = never;`. This has to hold because hasRole() and
// setPrincipal()'s roles field are declared unconditionally too (see
// writeState): a spec whose only authorization vocabulary is permissions
// still needs Role to exist as a type for the rest of the file to compile,
// exactly as a spec with no capabilities at all still needs Capability to.
func (g *CapabilityGenerator) writeRoleUnion(buf *strings.Builder, spec *client.APISpec) {
	buf.WriteString(`/**
 * Every role any route in this API declares through WithAnyRole or an
 * equivalent authorization requirement.
 *
 * A union rather than a bare string for the same reason Capability is one:
 * see the comment above. The stakes are higher here than they are for
 * Capability -- hasRole('admin') reads at the call site as a fact about who
 * is an administrator, not a hint to be double-checked, so a misspelling
 * that silently returns false is a worse defect here than it is for can().
 */
`)

	writeUnion(buf, "Role", client.NewAuthCodeGenerator().CollectRoles(spec))
}

// writePermissionUnion emits the Permission type: every distinct permission
// in the spec, sorted. See writeRoleUnion for why this and Role are
// unconditional in the same way Capability is.
func (g *CapabilityGenerator) writePermissionUnion(buf *strings.Builder, spec *client.APISpec) {
	buf.WriteString(`/**
 * Every permission any route in this API declares through
 * WithAllPermissions or an equivalent authorization requirement.
 *
 * A union rather than a bare string for the same reason Capability and Role
 * are; see writeCapabilityUnion's comment above.
 */
`)

	writeUnion(buf, "Permission", client.NewAuthCodeGenerator().CollectPermissions(spec))
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

// sortedUniqueStrings returns values sorted, deduplicated, and with empty
// entries dropped.
//
// Roles and permissions arrive here already sorted, deduplicated and free of
// empty entries: the production paths that populate Endpoint.Authorization
// (resolveEndpointAuthz, routeToEndpoint) normalise before the Endpoint is
// built, and EndpointAuthorization normalises again on the way out, so even a
// hand-built Endpoint cannot reach this in a raw shape. The call below is
// therefore idempotent today and is kept anyway, for the same reason
// capabilityAlternatives re-sorts a SecurityRequirement's scopes rather than
// trusting them pre-sorted: the emitted table must not be able to become
// order-dependent, and CI's byte-diff has no way to tell "input arrived
// sorted" from "output happens to be sorted this run" apart.
//
// It is not the guard against cross-language divergence, though it once was
// the only one. Go's generator had no equivalent, so an unnormalised Endpoint
// produced two different tables; that is fixed at EndpointAuthorization now,
// where one normalisation serves every generator instead of each language
// remembering to do its own.
func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}

		seen[value] = true

		out = append(out, value)
	}

	sort.Strings(out)

	return out
}

// writeAuthorizationRequirements emits requiredAuthorization: what each
// gated operation requires beyond scope, in roles and permissions.
//
// Roles and permissions carry Authorization's own semantics (see
// Authorization in ir.go), not an invention here: holding ANY ONE declared
// role satisfies the role half, and EVERY declared permission is required
// for the permission half -- an AND of an OR and an AND, which is why this
// cannot reuse writeRequirements' alternatives-of-alternatives shape built
// for scopes. An operation declaring neither is ABSENT from the table
// entirely, the same convention requiredCapabilities uses for scopes, so a
// reader can tell "not gated on roles or permissions" from "gated on
// neither", which would otherwise look identical.
func (g *CapabilityGenerator) writeAuthorizationRequirements(buf *strings.Builder, spec *client.APISpec) {
	buf.WriteString(`/**
 * What each gated operation requires beyond scope: A ROLE (holding any ONE
 * of "roles" suffices) AND every permission listed in "permissions".
 *
 * Independent of requiredCapabilities: an operation can be gated on a scope,
 * a role, a permission, any combination of the three, or none. canCall()
 * checks all three; this table and requiredCapabilities together are its
 * complete input.
 *
 * Operations declaring neither a role nor a permission requirement are
 * ABSENT rather than present with empty arrays -- see requiredCapabilities'
 * comment above for why that distinction matters.
 */
export const requiredAuthorization = {
`)

	auth := client.NewAuthCodeGenerator()
	keys := operationKeys(spec.Endpoints)

	for i := range spec.Endpoints {
		authz := auth.EndpointAuthorization(spec.Endpoints[i])
		if authz == nil {
			continue
		}

		roles := sortedUniqueStrings(authz.Roles)
		permissions := sortedUniqueStrings(authz.Permissions)

		if len(roles) == 0 && len(permissions) == 0 {
			continue
		}

		var fields []string

		if len(roles) > 0 {
			fields = append(fields, fmt.Sprintf("roles: %s", tsStringArray(roles)))
		}

		if len(permissions) > 0 {
			fields = append(fields, fmt.Sprintf("permissions: %s", tsStringArray(permissions)))
		}

		buf.WriteString(fmt.Sprintf("  %s: { %s },\n", tsKey(keys[i]), strings.Join(fields, ", ")))
	}

	buf.WriteString(`} as const satisfies Partial<
  Record<OperationName, { readonly roles?: readonly Role[]; readonly permissions?: readonly Permission[] }>
>;

`)
}

// writeState emits the granted-principal store and the predicates over it.
func (g *CapabilityGenerator) writeState(buf *strings.Builder) {
	buf.WriteString(`/**
 * The current principal's granted capabilities, roles and permissions, each
 * independently undefined when nothing has told this module anything about
 * that particular vocabulary yet.
 *
 * Three separate sets rather than one, because a caller may know one
 * vocabulary and not another -- a token that carries scopes but not roles,
 * say -- and collapsing them would make "not granted" indistinguishable from
 * "not yet told". Undefined vs. an empty set carries the same meaning it
 * always has here: "we do not know yet" and "we know, and it is none" must
 * not collapse into one. capabilitiesKnown() is how a caller tells those
 * apart for the capabilities half; hasRole() and hasPermission() answer
 * false for both cases on the role and permission halves, the same way
 * can() always has.
 */
let grantedCapabilities: ReadonlySet<string> | undefined;
let grantedRoles: ReadonlySet<string> | undefined;
let grantedPermissions: ReadonlySet<string> | undefined;

/**
 * Declare the current principal's granted capabilities, roles and
 * permissions. Call with no argument, or with a field omitted, to forget
 * that field; call with no argument at all to forget everything.
 *
 * Call this on sign-in, after a token refresh that changes any of the three,
 * and with no argument on sign-out. This module holds no identity of its own
 * and nothing clears it for you: a client that switches principal without
 * calling this keeps answering for the previous one, and will offer actions
 * the new user does not hold -- which the server then refuses, because the
 * server is what decides.
 *
 * The parameter is an inline object type rather than a named interface on
 * purpose: a named export not listed in capabilityExports (generator-side)
 * would be exactly the collision risk this file's header warns about, under
 * a different name. See capabilityExports' comment in capabilities.go.
 *
 * Takes arbitrary strings rather than Capability/Role/Permission because
 * granted values come from a token, and a server is free to grant anything
 * this client's specification never mentioned. Unrecognised ones are stored
 * and simply never asked about; dropping them would be a lie about what the
 * principal holds.
 *
 * This replaces the three separate setters an earlier version of this file
 * might tempt a maintainer to add -- one setter, not three, because three
 * parallel setters are three chances to update two of them and ship a stale
 * third. setCapabilities() below is kept only as a backward-compatible
 * wrapper around this.
 *
 * The state is module-scoped, which means per-process, which means this is
 * for a browser. Calling it while server-rendering shares one principal's
 * state with every request the process is handling concurrently, and the
 * interface one user receives is rendered against another's. Server-side
 * authorization is unaffected -- it never consults this -- so the damage is
 * a wrong interface rather than a breach, but it is wrong for everybody at
 * once. Derive what to render from the request's own session there instead.
 */
export function setPrincipal(principal: {
  readonly capabilities?: Iterable<string>;
  readonly roles?: Iterable<string>;
  readonly permissions?: Iterable<string>;
} = {}): void {
  grantedCapabilities = principal.capabilities === undefined ? undefined : new Set(principal.capabilities);
  grantedRoles = principal.roles === undefined ? undefined : new Set(principal.roles);
  grantedPermissions = principal.permissions === undefined ? undefined : new Set(principal.permissions);
}

/**
 * @deprecated Use setPrincipal({ capabilities }) instead.
 *
 * Declare the current principal's granted scopes. Call with no argument to
 * forget them.
 *
 * Delegates to setPrincipal() rather than reimplementing it, and passes
 * through whatever roles and permissions are currently known so that
 * calling this -- the only setter callers written before roles and
 * permissions existed have ever known about -- cannot silently forget them.
 * A caller that never touches roles or permissions at all is unaffected:
 * both stay undefined throughout, exactly as before this function existed.
 */
export function setCapabilities(scopes?: Iterable<string>): void {
  setPrincipal({ capabilities: scopes, roles: grantedRoles, permissions: grantedPermissions });
}

/**
 * Whether the current principal's capabilities are known: setPrincipal() or
 * the deprecated setCapabilities() has been called with a capabilities value
 * that has not since been forgotten.
 *
 * The reason can() alone is not enough. Before anything is known can() answers
 * false, which is indistinguishable from a denial, so an interface that
 * renders straight off it hides every action during load and pops them all
 * into existence afterwards. A caller that would rather show a placeholder
 * than flicker checks this first.
 *
 * Answers specifically for capabilities, not roles or permissions -- a
 * principal can have one vocabulary known and another not, see writeState's
 * comment on the three granted sets above.
 */
export function capabilitiesKnown(): boolean {
  return grantedCapabilities !== undefined;
}

/**
 * Whether the current principal holds a capability.
 *
 * False before any capabilities are known -- see capabilitiesKnown(). Never
 * a security decision; see this file's header.
 */
export function can(capability: Capability): boolean {
  return grantedCapabilities !== undefined && grantedCapabilities.has(capability);
}

/**
 * Whether the current principal holds a role.
 *
 * False before any roles are known. Never a security decision; see this
 * file's header -- hasRole() above all. Do not use this to decide whether a
 * request is safe to send: send it, and let the server decide.
 */
export function hasRole(role: Role): boolean {
  return grantedRoles !== undefined && grantedRoles.has(role);
}

/**
 * Whether the current principal holds a permission.
 *
 * False before any permissions are known. Never a security decision; see
 * this file's header.
 */
export function hasPermission(permission: Permission): boolean {
  return grantedPermissions !== undefined && grantedPermissions.has(permission);
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
 * requiredAuthorization widened to its declared shape, so indexing it by an
 * operation that declares neither a role nor a permission requirement reads
 * as absent rather than as an error about a key the const object does not
 * have. Same reasoning as "required" above, for the role/permission half.
 */
const requiredAuthz: Partial<
  Record<OperationName, { readonly roles?: readonly Role[]; readonly permissions?: readonly Permission[] }>
> = requiredAuthorization;

/**
 * Whether the current principal could call the given operation without being
 * refused.
 *
 * Checks every vocabulary an operation can be gated on: the scope
 * alternatives in requiredCapabilities (via missingCapabilities -- ANDed
 * within an alternative, ORed across them; see writeRequirements), ANY ONE
 * role in requiredAuthorization, and EVERY permission there. An operation
 * gated on more than one vocabulary must satisfy all of the ones it
 * declares: WithRequiredAuth, WithAnyRole and WithAllPermissions
 * stack server-side rather than substituting for each other, and this
 * mirrors that rather than treating whichever check runs first as
 * sufficient.
 *
 * Advisory, and false before the relevant capabilities/roles/permissions are
 * known. Nothing in the generated request path consults this: call it
 * yourself, at a call site you control, to skip a round trip you expect to
 * fail or to explain why an action is unavailable. A request issued
 * regardless is still sent, and still authorized by the server.
 */
export function canCall(operation: OperationName): boolean {
  if (missingCapabilities(operation).length > 0) return false;

  const authz = requiredAuthz[operation];
  if (authz === undefined) return true;

  if (authz.roles !== undefined && authz.roles.length > 0 && !authz.roles.some((role) => hasRole(role))) {
    return false;
  }

  if (authz.permissions !== undefined && !authz.permissions.every((permission) => hasPermission(permission))) {
    return false;
  }

  return true;
}
`)
}
