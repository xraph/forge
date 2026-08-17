package golang

import (
	"fmt"
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
// strings into the constants the emitted file can actually declare.
//
// The input is already deduplicated by CollectCapabilities/CollectRoles/
// CollectPermissions, but two distinct STRINGS can still derive to the same Go
// identifier -- capabilityIdent drops everything but letters and digits, so
// "users:write" and "users-write" both become "UsersWrite" -- and a value that
// is entirely punctuation (":::") collapses to "". Emitting either as a const
// would either duplicate a name or leave a bare "Permission" with no suffix,
// neither of which is what the source string named. Skipping keeps the file
// legal Go; this is the same choice resolveAuthFields makes for security
// scheme fields, for the same reason.
func resolveCapabilityConsts(values []string) []capabilityConst {
	var out []capabilityConst

	seen := make(map[string]bool, len(values))

	for _, value := range values {
		ident := capabilityIdent(value)
		if ident == "" || seen[ident] {
			continue
		}

		seen[ident] = true

		out = append(out, capabilityConst{ident: ident, value: value})
	}

	return out
}

// Generate produces capabilities.go. Callers must check capabilitiesNeeded
// first; this does not gate itself and will happily emit three empty unions
// for a spec that declares nothing.
func (g *CapabilitiesGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	authGen := client.NewAuthCodeGenerator()

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))
	buf.WriteString(capabilitiesFileHeader())

	g.writeUnion(&buf, "Capability",
		"// Capability is every scope any route in this API declares through\n"+
			"// WithRequiredAuth.\n",
		authGen.CollectCapabilities(spec))

	g.writeUnion(&buf, "Role",
		"// Role is every role any route in this API declares through\n"+
			"// WithRequiredRole or an equivalent authorization requirement.\n",
		authGen.CollectRoles(spec))

	g.writeUnion(&buf, "Permission",
		"// Permission is every permission any route in this API declares\n"+
			"// through WithRequiredPermission or an equivalent authorization\n"+
			"// requirement.\n",
		authGen.CollectPermissions(spec))

	buf.WriteString(g.generatePrincipal())

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
func capabilitiesFileHeader() string {
	return `// ============================================================================
// A UX AFFORDANCE. NEVER AN AUTHORIZATION DECISION.
//
// Everything below is computed from client-held state: whatever Principal
// was last passed to SetPrincipal. Nothing here verifies that state against
// anything, so it is only as trustworthy as whoever called SetPrincipal --
// in a browser or a CLI, that is the end user's own process, which the user
// (or anything running as them) can alter freely. Can, HasRole and
// HasPermission below each answer a question; none of them enforces the
// answer, and no generated request path consults one before sending.
//
// HasRole is the one to watch, more than Can. Can("read:users") reads like a
// hint the caller checks before trying something. HasRole("admin") reads like
// a fact, as though the client itself knows who the administrators are. It
// does not, and it never will: the server decides who holds a role, on every
// request, unconditionally. These methods are good for exactly one thing:
// deciding whether to show a button or hide it. Do not gate anything that
// matters on a call in this file, and treat every true it returns as
// provisional until the server's own response confirms it.
// ============================================================================

`
}

// writeUnion emits a string-typed named type plus a const block of every
// collected value, using capabilityIdent to derive each constant's suffix.
//
// An empty collection still gets its type declaration -- Principal's
// Capabilities/Roles/Permissions fields are typed against Capability, Role
// and Permission unconditionally, so all three types must exist whenever this
// file exists at all, even if (say) the spec declares roles and permissions
// but no bare scopes. It just gets no const block, since there is nothing to
// put in one.
func (g *CapabilitiesGenerator) writeUnion(buf *strings.Builder, typeName, doc string, values []string) {
	buf.WriteString(doc)
	buf.WriteString(fmt.Sprintf("type %s string\n\n", typeName))

	consts := resolveCapabilityConsts(values)
	if len(consts) == 0 {
		return
	}

	buf.WriteString("const (\n")

	for _, c := range consts {
		buf.WriteString(fmt.Sprintf("\t%s%s %s = %q\n", typeName, c.ident, typeName, c.value))
	}

	buf.WriteString(")\n\n")
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
