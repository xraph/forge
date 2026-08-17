package golang

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// CapabilitiesGenerator emits capabilities.go: the scopes, roles and
// permissions this API's routes declare, as string-typed unions, plus a
// client-held Principal and the predicates an interface uses to decide what
// to render.
//
// TypeScript has carried the scope half of this (capabilities.ts) since
// before roles and permissions existed in the IR at all. This is the Go
// generator's first version of the same idea, and it covers all three kinds
// from the start rather than leaving roles and permissions for later.
//
// What it is NOT is stated at the top of the generated file rather than only
// here, because the generated file is what somebody reads at the call site:
// every answer it gives is computed from client-held state and is never an
// authorization decision.
type CapabilitiesGenerator struct{}

// NewCapabilitiesGenerator creates a new capabilities generator.
func NewCapabilitiesGenerator() *CapabilitiesGenerator {
	return &CapabilitiesGenerator{}
}

// capabilitiesNeeded reports whether the spec declares anything the
// capability file could describe.
//
// Nothing declared means the const blocks below would all be empty and every
// method on Principal would be uncallable in any way that returns true, so
// no file is emitted at all -- the same choice the TypeScript generator makes
// for the scope-only case (see capabilitiesNeeded in the typescript package).
func capabilitiesNeeded(spec *client.APISpec) bool {
	authGen := client.NewAuthCodeGenerator()

	return len(authGen.CollectCapabilities(spec)) > 0 ||
		len(authGen.CollectRoles(spec)) > 0 ||
		len(authGen.CollectPermissions(spec)) > 0
}

// capabilityIdent turns a scope, role or permission string into an exported Go
// identifier fragment.
//
// Colon-delimited names are the norm here ("users:write", "read:users"), and
// goFieldName does not treat ':' as a separator, so calling it directly yields
// "Userswrite". It is deliberately not extended: its own doc comment notes
// that other callers' identifiers would change. Splitting on ':' first and
// handing each part to goFieldName keeps that guarantee and still produces
// "UsersWrite".
func capabilityIdent(value string) string {
	var out strings.Builder

	for _, part := range strings.Split(value, ":") {
		out.WriteString(goFieldName(part))
	}

	return out.String()
}

// capabilityConst is one collected scope/role/permission string paired with
// the Go identifier fragment capabilityIdent derived for it.
type capabilityConst struct {
	ident string
	value string
}

// resolveCapabilityConsts turns a collected, sorted, deduplicated list of
// strings into the constants the emitted file can actually declare, warning
// about anything it has to drop to get there.
//
// The input is already deduplicated by CollectCapabilities/CollectRoles/
// CollectPermissions, but two distinct STRINGS can still derive to the same Go
// identifier -- capabilityIdent drops everything but letters and digits, so
// "users:write" and "users-write" both become "UsersWrite" -- and a value that
// is entirely punctuation (":::") collapses to "". Emitting either as a const
// would either duplicate a name (a compile error) or leave a bare "Permission"
// with no suffix (legal Go, but not what the source string named, and silently
// wrong: the surviving constant's string value is whichever of the colliding
// inputs sorted first, not necessarily the one a reader expects). Skipping is
// therefore not enough on its own -- resolveAuthFields warns on both its
// analogous cases (an unusable field name, and two scheme keys colliding on
// one field) and generator.go forwards those into the result's Warnings, and
// this follows the same path so the same silent-wrong-answer defect does not
// reappear here under a different name.
//
// kind names what these values are ("capability", "role" or "permission") for
// the warning text; callers pass one of Generate's three fixed calls.
func resolveCapabilityConsts(kind string, values []string) ([]capabilityConst, []string) {
	var (
		out      []capabilityConst
		warnings []string
		taken    = map[string]string{} // identifier -> the value that claimed it
	)

	for _, value := range values {
		ident := capabilityIdent(value)

		if ident == "" {
			warnings = append(warnings, fmt.Sprintf(
				"%s %q has no usable Go identifier and was skipped", kind, value))

			continue
		}

		if owner, clash := taken[ident]; clash {
			// Two values, one constant: emitting both would duplicate the
			// identifier, and emitting only the second silently under the
			// first's name would give a reader a constant whose declared
			// string does not match the name they typed.
			warnings = append(warnings, fmt.Sprintf(
				"%s %q and %q both map to the identifier %q; %q was skipped",
				kind, owner, value, ident, value))

			continue
		}

		taken[ident] = value

		out = append(out, capabilityConst{ident: ident, value: value})
	}

	return out, warnings
}

// Generate produces capabilities.go, plus any warnings raised while resolving
// collected scopes, roles or permissions into Go identifiers (see
// resolveCapabilityConsts). Callers must check capabilitiesNeeded first; this
// does not gate itself and will happily emit three empty unions for a spec
// that declares nothing.
func (g *CapabilitiesGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) (string, []string) {
	authGen := client.NewAuthCodeGenerator()

	var (
		buf      strings.Builder
		warnings []string
	)

	hasOperations := len(spec.Endpoints) > 0

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))
	buf.WriteString(capabilitiesFileHeader(hasOperations))

	warnings = append(warnings, g.writeUnion(&buf, "Capability", "capability",
		"// Capability is every scope any route in this API declares through\n"+
			"// WithRequiredAuth.\n",
		authGen.CollectCapabilities(spec))...)

	warnings = append(warnings, g.writeUnion(&buf, "Role", "role",
		"// Role is every role any route in this API declares through\n"+
			"// WithRequiredRole or an equivalent authorization requirement.\n",
		authGen.CollectRoles(spec))...)

	warnings = append(warnings, g.writeUnion(&buf, "Permission", "permission",
		"// Permission is every permission any route in this API declares\n"+
			"// through WithRequiredPermission or an equivalent authorization\n"+
			"// requirement.\n",
		authGen.CollectPermissions(spec))...)

	buf.WriteString(g.generatePrincipal())

	// Everything below is keyed by REST operation, mirroring the TypeScript
	// generator's own gate on writeOperationUnion/writeRequirements: a spec
	// whose scopes, roles and permissions come only from WebSocket, SSE or
	// WebTransport routes gets the three unions above and the predicates over
	// them, and stops there. An OperationName union with no members would be
	// uninhabited, and CanCall/MissingCapabilities would be functions no
	// caller could pass an argument to.
	if hasOperations {
		opWarnings := g.generateOperationSurface(&buf, spec)
		warnings = append(warnings, opWarnings...)
	}

	return buf.String(), warnings
}

// generateOperationSurface emits OperationName, the per-operation
// requirement table, and CanCall/MissingCapabilities -- the parity gap this
// fix closes. TypeScript has had the equivalent (canCall,
// missingCapabilities, requiredCapabilities, requiredAuthorization) since
// roles and permissions were added to that generator; this is the Go
// generator's first version, structured the same way so the two cannot drift
// into different semantics.
func (g *CapabilitiesGenerator) generateOperationSurface(buf *strings.Builder, spec *client.APISpec) []string {
	keys := operationKeys(spec.Endpoints)

	// OperationName's identifiers go through writeUnion/resolveCapabilityConsts
	// exactly like Capability, Role and Permission's do, rather than a second,
	// silent skip path -- two operation keys colliding on their derived
	// identifier is the same defect resolveCapabilityConsts already exists to
	// catch and warn about for the other three unions.
	warnings := g.writeUnion(buf, "OperationName", "operation",
		"// OperationName is every operation in this API, gated or not.\n"+
			"//\n"+
			"// Ungated operations are members too, so CanCall accepts any operation\n"+
			"// and answers true for the ones that require nothing. The table below\n"+
			"// is keyed by the same string values as this union's constants, not by\n"+
			"// the constants themselves, so a value dropped from the union by a\n"+
			"// naming collision (see the warning above, if one was raised) still\n"+
			"// appears in the table -- CanCall stays correct for it even though no\n"+
			"// named constant exists to spell it with.\n",
		keys)

	buf.WriteString(g.generateRequirementTable(spec, keys))
	buf.WriteString(g.generateOperationPredicates())

	return warnings
}

// operationAlternatives normalises one endpoint's declared scopes and
// authorization into the shape generateRequirementTable renders, or nil when
// the endpoint is gated on nothing at all.
type operationAlternatives struct {
	capabilities [][]string
	roles        []string
	permissions  []string
}

func gatherOperationAlternatives(auth *client.AuthCodeGenerator, endpoint client.Endpoint) *operationAlternatives {
	alternatives := auth.EndpointCapabilities(endpoint)
	authz := auth.EndpointAuthorization(endpoint)

	var roles, permissions []string
	if authz != nil {
		roles = authz.Roles
		permissions = authz.Permissions
	}

	if len(alternatives) == 0 && len(roles) == 0 && len(permissions) == 0 {
		return nil
	}

	return &operationAlternatives{capabilities: alternatives, roles: roles, permissions: permissions}
}

// generateRequirementTable emits the operationRequirement type and the
// operationRequirements map: what each gated operation needs before CanCall
// would answer true.
//
// One table covering all three vocabularies rather than TypeScript's two
// (requiredCapabilities and requiredAuthorization) -- Go has no equivalent of
// `as const satisfies Partial<Record<...>>` forcing the split, and a single
// map keyed by OperationName with an optional field per vocabulary says the
// same thing in one lookup instead of two. The semantics are identical either
// way: Capabilities is nested ALTERNATIVES (holding every capability in any
// ONE inner slice permits the operation, OpenAPI's own OR-of-ANDs), Roles is
// ANY-of, Permissions is ALL-of.
//
// An operation declaring none of the three is ABSENT from the map entirely,
// not present with empty fields, so CanCall can tell "not gated" from "gated
// on nothing" -- the same convention capabilities.ts's requiredCapabilities
// and requiredAuthorization tables both use, for the same reason.
func (g *CapabilitiesGenerator) generateRequirementTable(spec *client.APISpec, keys []string) string {
	var buf strings.Builder

	auth := client.NewAuthCodeGenerator()

	buf.WriteString("// operationRequirement is what one operation requires across the three\n")
	buf.WriteString("// authorization vocabularies. A vocabulary an operation does not use is\n")
	buf.WriteString("// left nil.\n")
	buf.WriteString("type operationRequirement struct {\n")
	buf.WriteString("\t// Capabilities holds ALTERNATIVES: every capability in any ONE inner\n")
	buf.WriteString("\t// slice satisfies this half of the requirement.\n")
	buf.WriteString("\tCapabilities [][]Capability\n")
	buf.WriteString("\t// Roles is ANY-of: holding at least one of these satisfies this half.\n")
	buf.WriteString("\tRoles []Role\n")
	buf.WriteString("\t// Permissions is ALL-of: every one of these is required.\n")
	buf.WriteString("\tPermissions []Permission\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// operationRequirements is the per-operation requirement table CanCall\n")
	buf.WriteString("// and MissingCapabilities read. An operation absent from this map declares\n")
	buf.WriteString("// no requirement in any vocabulary and is callable unconditionally.\n")
	buf.WriteString("var operationRequirements = map[OperationName]operationRequirement{\n")

	for i := range spec.Endpoints {
		entry := gatherOperationAlternatives(auth, spec.Endpoints[i])
		if entry == nil {
			continue
		}

		buf.WriteString(fmt.Sprintf("\t%s: {\n", strconv.Quote(keys[i])))

		if len(entry.capabilities) > 0 {
			rendered := make([]string, 0, len(entry.capabilities))
			for _, alternative := range entry.capabilities {
				rendered = append(rendered, goTypedStringSlice("Capability", alternative))
			}

			buf.WriteString(fmt.Sprintf("\t\tCapabilities: [][]Capability{%s},\n", strings.Join(rendered, ", ")))
		}

		if len(entry.roles) > 0 {
			buf.WriteString(fmt.Sprintf("\t\tRoles: %s,\n", goTypedStringSlice("Role", entry.roles)))
		}

		if len(entry.permissions) > 0 {
			buf.WriteString(fmt.Sprintf("\t\tPermissions: %s,\n", goTypedStringSlice("Permission", entry.permissions)))
		}

		buf.WriteString("\t},\n")
	}

	buf.WriteString("}\n\n")

	return buf.String()
}

// goTypedStringSlice renders a Go slice literal of a named string type, e.g.
// []Capability{"users:read", "users:write"}.
func goTypedStringSlice(typeName string, values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}

	return fmt.Sprintf("[]%s{%s}", typeName, strings.Join(quoted, ", "))
}

// generateOperationPredicates emits MissingCapabilities and CanCall.
//
// CanCall's semantics are TypeScript's canCall exactly, not a
// reimplementation from first principles: scopes are checked via
// MissingCapabilities (itself the same alternatives-of-alternatives walk as
// TypeScript's missingCapabilities), roles are ANY-of, permissions are
// ALL-of, and an operation missing from operationRequirements is callable.
// Divergence between the two languages here would be a silent authorization
// affordance defect -- one client's "you can't" the other's "you can" for the
// identical principal and operation -- so this is deliberately mirrored
// rather than rewritten idiomatically from scratch.
func (g *CapabilitiesGenerator) generateOperationPredicates() string {
	var buf strings.Builder

	buf.WriteString("// MissingCapabilities returns the smallest set of capabilities the\n")
	buf.WriteString("// current principal is missing before op would be permitted, or nil when\n")
	buf.WriteString("// nothing is missing.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Smallest ACROSS alternatives, so where an operation can be reached more\n")
	buf.WriteString("// than one way the answer names the cheapest route to it rather than an\n")
	buf.WriteString("// arbitrary one. Empty for an operation that requires no capability,\n")
	buf.WriteString("// including one absent from operationRequirements entirely. Never a\n")
	buf.WriteString("// security decision; see this file's header.\n")
	buf.WriteString("func (c *Client) MissingCapabilities(op OperationName) []Capability {\n")
	buf.WriteString("\treq, ok := operationRequirements[op]\n")
	buf.WriteString("\tif !ok || len(req.Capabilities) == 0 {\n")
	buf.WriteString("\t\treturn nil\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\tvar fewest []Capability\n\n")
	buf.WriteString("\tfor _, alternative := range req.Capabilities {\n")
	buf.WriteString("\t\tvar missing []Capability\n\n")
	buf.WriteString("\t\tfor _, capability := range alternative {\n")
	buf.WriteString("\t\t\tif !c.Can(capability) {\n")
	buf.WriteString("\t\t\t\tmissing = append(missing, capability)\n")
	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t}\n\n")
	buf.WriteString("\t\tif len(missing) == 0 {\n")
	buf.WriteString("\t\t\treturn nil\n")
	buf.WriteString("\t\t}\n\n")
	buf.WriteString("\t\tif fewest == nil || len(missing) < len(fewest) {\n")
	buf.WriteString("\t\t\tfewest = missing\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\treturn fewest\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// CanCall reports whether the current principal could call op without\n")
	buf.WriteString("// being refused, by everything this client knows about it.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Checks every vocabulary an operation can be gated on: the capability\n")
	buf.WriteString("// alternatives (via MissingCapabilities), ANY ONE role, and EVERY\n")
	buf.WriteString("// permission the operationRequirements entry declares. An operation\n")
	buf.WriteString("// gated on more than one vocabulary must satisfy all of the ones it\n")
	buf.WriteString("// declares -- they stack server-side rather than substituting for each\n")
	buf.WriteString("// other, and this mirrors that rather than treating whichever check\n")
	buf.WriteString("// runs first as sufficient.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Advisory, and false before the relevant capabilities/roles/permissions\n")
	buf.WriteString("// are known. Nothing in the generated request path consults this: call\n")
	buf.WriteString("// it yourself, at a call site you control, to skip a round trip you\n")
	buf.WriteString("// expect to fail or to explain why an action is unavailable. A request\n")
	buf.WriteString("// issued regardless is still sent, and still authorized by the server.\n")
	buf.WriteString("func (c *Client) CanCall(op OperationName) bool {\n")
	buf.WriteString("\tif len(c.MissingCapabilities(op)) > 0 {\n")
	buf.WriteString("\t\treturn false\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\treq, ok := operationRequirements[op]\n")
	buf.WriteString("\tif !ok {\n")
	buf.WriteString("\t\treturn true\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\tif len(req.Roles) > 0 {\n")
	buf.WriteString("\t\tholdsAny := false\n\n")
	buf.WriteString("\t\tfor _, role := range req.Roles {\n")
	buf.WriteString("\t\t\tif c.HasRole(role) {\n")
	buf.WriteString("\t\t\t\tholdsAny = true\n")
	buf.WriteString("\t\t\t\tbreak\n")
	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t}\n\n")
	buf.WriteString("\t\tif !holdsAny {\n")
	buf.WriteString("\t\t\treturn false\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\tfor _, permission := range req.Permissions {\n")
	buf.WriteString("\t\tif !c.HasPermission(permission) {\n")
	buf.WriteString("\t\t\treturn false\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\treturn true\n")
	buf.WriteString("}\n")

	return buf.String()
}

// capabilitiesFileHeader is the warning every reader of this file needs
// before the first declaration, not buried in documentation somewhere else.
//
// It leans harder on roles than capabilities.ts's header does, because
// HasRole reads differently than Can does. Can("read:users") reads like a
// hint a caller checks before trying something risky. HasRole("admin") reads
// like a fact -- as if the client itself knows who the administrators are.
// That is exactly the call somebody will be tempted to trust for something
// that matters, so the header says plainly that it does not know: the server
// decides, on every request, and this is only good for deciding whether to
// show a button or hide it.
//
// hasOperations names CanCall and MissingCapabilities in the banner only when
// this file actually declares them -- both are gated on the spec having REST
// endpoints (see Generate), and a banner naming a function the file below it
// does not define would be its own kind of wrong answer.
func capabilitiesFileHeader(hasOperations bool) string {
	predicates := "Can, HasRole and HasPermission"
	compositionNote := ""

	if hasOperations {
		predicates = "Can, HasRole, HasPermission, CanCall and MissingCapabilities"
		compositionNote = " CanCall inherits the same risk by composition -- it is Can, HasRole\n" +
			"// and HasPermission chained together for one operation -- so trusting it\n" +
			"// is trusting all three at once."
	}

	return `// ============================================================================
// A UX AFFORDANCE. NEVER AN AUTHORIZATION DECISION.
//
// Everything below is computed from client-held state: whatever Principal
// was last passed to SetPrincipal. Nothing here verifies that state against
// anything, so it is only as trustworthy as whoever called SetPrincipal --
// in a browser or a CLI, that is the end user's own process, which the user
// (or anything running as them) can alter freely. ` + predicates + ` below each
// answer a question; none of them enforces the answer, and no generated
// request path consults one before sending.
//
// HasRole is the one to watch, more than Can. Can("read:users") reads like a
// hint the caller checks before trying something. HasRole("admin") reads like
// a fact, as though the client itself knows who the administrators are. It
// does not, and it never will: the server decides who holds a role, on every
// request, unconditionally.` + compositionNote + ` These methods are good for
// exactly one thing: deciding whether to show a button or hide it. Do not
// gate anything that matters on a call in this file, and treat every true it
// returns as provisional until the server's own response confirms it.
// ============================================================================

`
}

// writeUnion emits a string-typed named type plus a const block of every
// collected value, using capabilityIdent to derive each constant's suffix,
// and returns any warnings resolveCapabilityConsts raised along the way.
//
// An empty collection still gets its type declaration -- Principal's
// Capabilities/Roles/Permissions fields are typed against Capability, Role
// and Permission unconditionally, so all three types must exist whenever this
// file exists at all, even if (say) the spec declares roles and permissions
// but no bare scopes. It just gets no const block, since there is nothing to
// put in one.
func (g *CapabilitiesGenerator) writeUnion(buf *strings.Builder, typeName, kind, doc string, values []string) []string {
	buf.WriteString(doc)
	buf.WriteString(fmt.Sprintf("type %s string\n\n", typeName))

	consts, warnings := resolveCapabilityConsts(kind, values)
	if len(consts) == 0 {
		return warnings
	}

	buf.WriteString("const (\n")

	for _, c := range consts {
		buf.WriteString(fmt.Sprintf("\t%s%s %s = %q\n", typeName, c.ident, typeName, c.value))
	}

	buf.WriteString(")\n\n")

	return warnings
}

// generatePrincipal emits the Principal struct and the *Client methods that
// read it.
func (g *CapabilitiesGenerator) generatePrincipal() string {
	var buf strings.Builder

	buf.WriteString("// Principal is the client-held view of who is calling: whatever was last\n")
	buf.WriteString("// passed to SetPrincipal. See the warning at the top of this file before\n")
	buf.WriteString("// treating any of it as authoritative.\n")
	buf.WriteString("type Principal struct {\n")
	buf.WriteString("\tCapabilities []Capability\n")
	buf.WriteString("\tRoles        []Role\n")
	buf.WriteString("\tPermissions  []Permission\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// SetPrincipal declares the current principal's capabilities, roles and\n")
	buf.WriteString("// permissions.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Call this on sign-in, after a token refresh that changes any of the\n")
	buf.WriteString("// three, and with a zero Principal on sign-out. The client holds no\n")
	buf.WriteString("// identity of its own and nothing clears this for you: a client that\n")
	buf.WriteString("// switches principal without calling this keeps answering for the\n")
	buf.WriteString("// previous one.\n")
	buf.WriteString("func (c *Client) SetPrincipal(principal Principal) {\n")
	buf.WriteString("\tc.principal = principal\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// Can reports whether the current principal holds capability.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Never a security decision; see this file's header.\n")
	buf.WriteString("func (c *Client) Can(capability Capability) bool {\n")
	buf.WriteString("\tfor _, held := range c.principal.Capabilities {\n")
	buf.WriteString("\t\tif held == capability {\n")
	buf.WriteString("\t\t\treturn true\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\treturn false\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// HasRole reports whether the current principal holds role.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Never a security decision; see this file's header. In particular, do\n")
	buf.WriteString("// not use this to decide whether a request is safe to send -- send it\n")
	buf.WriteString("// and let the server decide.\n")
	buf.WriteString("func (c *Client) HasRole(role Role) bool {\n")
	buf.WriteString("\tfor _, held := range c.principal.Roles {\n")
	buf.WriteString("\t\tif held == role {\n")
	buf.WriteString("\t\t\treturn true\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\treturn false\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// HasPermission reports whether the current principal holds permission.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Never a security decision; see this file's header.\n")
	buf.WriteString("func (c *Client) HasPermission(permission Permission) bool {\n")
	buf.WriteString("\tfor _, held := range c.principal.Permissions {\n")
	buf.WriteString("\t\tif held == permission {\n")
	buf.WriteString("\t\t\treturn true\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\treturn false\n")
	buf.WriteString("}\n\n")

	return buf.String()
}
