package dart

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// capabilitiesNeeded reports whether the document declares any scope, role
// or permission, which is the only case capabilities.dart is emitted.
func capabilitiesNeeded(spec *client.APISpec) bool {
	auth := client.NewAuthCodeGenerator()

	return len(auth.CollectCapabilities(spec)) > 0 ||
		len(auth.CollectRoles(spec)) > 0 ||
		len(auth.CollectPermissions(spec)) > 0
}

// capabilityTables collects the vocabularies and per-operation requirements,
// the same data the TypeScript generator's capabilityTables collects. It runs
// the same functions in the same order, so the two tables can only differ if
// a client function does.
func capabilityTables(spec *client.APISpec, keys []string) client.TableCapabilities {
	auth := client.NewAuthCodeGenerator()

	out := client.TableCapabilities{
		Scopes:                auth.CollectCapabilities(spec),
		Roles:                 auth.CollectRoles(spec),
		Permissions:           auth.CollectPermissions(spec),
		Operations:            keys,
		RequiredCapabilities:  map[string][][]string{},
		RequiredAuthorization: map[string]client.TableAuthorization{},
	}

	for i := range spec.Endpoints {
		if alternatives := auth.EndpointCapabilities(spec.Endpoints[i]); len(alternatives) > 0 {
			out.RequiredCapabilities[keys[i]] = alternatives
		}

		authz := auth.EndpointAuthorization(spec.Endpoints[i])
		if authz == nil {
			continue
		}

		roles := sortedUniqueStrings(authz.Roles)
		permissions := sortedUniqueStrings(authz.Permissions)

		if len(roles) == 0 && len(permissions) == 0 {
			continue
		}

		out.RequiredAuthorization[keys[i]] = client.TableAuthorization{Roles: roles, Permissions: permissions}
	}

	return out
}

// renderCapabilities renders lib/src/capabilities.dart.
func renderCapabilities(spec *client.APISpec, keys []string) string {
	tables := capabilityTables(spec, keys)

	var b strings.Builder

	b.WriteString(generatedHeader)
	b.WriteString(`//
// Scope, role and permission constants, and the predicates over them.
//
// A UX AFFORDANCE. NEVER A SECURITY BOUNDARY. Every value here is held by the
// client and can be changed by whoever runs it. The server authorizes every
// request on its own. Hiding a button is not access control.
//
// hasRole and hasPermission deserve the most suspicion: they read like facts
// about the user, and they are only whatever setPrincipal was last given.
// Nothing here blocks a request.
`)

	scopes := writeVocabulary(&b, "Scope", "A scope some route in this API declares.", tables.Scopes)
	roles := writeVocabulary(&b, "Role", "A role some route in this API declares.", tables.Roles)
	permissions := writeVocabulary(&b, "Permission", "A permission some route in this API declares.", tables.Permissions)

	// Everything keyed by operation needs a REST endpoint. A document whose
	// scopes come only from streaming routes gets the three vocabularies and
	// the predicates over them, and stops there.
	hasOps := len(spec.Endpoints) > 0

	if hasOps {
		writeVocabulary(&b, "OperationName", "An operation in this API, gated or not.", keys)

		b.WriteString("\n/// Scope alternatives per gated operation: every scope in any ONE inner list\n")
		b.WriteString("/// permits the operation.\n")
		b.WriteString("const Map<String, List<List<Scope>>> requiredCapabilities = {")

		if len(tables.RequiredCapabilities) > 0 {
			b.WriteString("\n")
		}

		for _, key := range sortedKeys(tables.RequiredCapabilities) {
			var alts []string

			for _, alt := range tables.RequiredCapabilities[key] {
				refs := make([]string, len(alt))
				for i, s := range alt {
					refs[i] = "Scope." + scopes[s]
				}

				alts = append(alts, "["+strings.Join(refs, ", ")+"]")
			}

			fmt.Fprintf(&b, "  %s: [%s],\n", dartString(key), strings.Join(alts, ", "))
		}

		b.WriteString("};\n")

		b.WriteString("\n/// Roles (any one suffices) and permissions (all required) per operation.\n")
		b.WriteString("const Map<String, ({List<Role> roles, List<Permission> permissions})> requiredAuthorization = {")

		if len(tables.RequiredAuthorization) > 0 {
			b.WriteString("\n")
		}

		for _, key := range sortedKeys(tables.RequiredAuthorization) {
			authz := tables.RequiredAuthorization[key]

			r := make([]string, len(authz.Roles))
			for i, s := range authz.Roles {
				r[i] = "Role." + roles[s]
			}

			p := make([]string, len(authz.Permissions))
			for i, s := range authz.Permissions {
				p[i] = "Permission." + permissions[s]
			}

			fmt.Fprintf(&b, "  %s: (roles: [%s], permissions: [%s]),\n",
				dartString(key), strings.Join(r, ", "), strings.Join(p, ", "))
		}

		b.WriteString("};\n")
	}

	b.WriteString(capabilityState)

	if hasOps {
		b.WriteString(capabilityPredicates)
	}

	return b.String()
}

// writeVocabulary writes an extension type with one const per value, so a
// misspelt scope is a compile error rather than a silent false, and returns
// value to member name.
func writeVocabulary(b *strings.Builder, typeName, doc string, values []string) map[string]string {
	reserved := copySet(enumReserved)
	reserved["value"] = true

	members := uniqueNames(values, func(s string) string { return memberIdent(identifierWords(s), reserved) }, copySet(reserved), false)
	out := make(map[string]string, len(values))

	fmt.Fprintf(b, "\n/// %s\n", doc)
	fmt.Fprintf(b, "extension type const %s._(String value) implements Object {\n", typeName)

	for i, v := range values {
		out[v] = members[i]
		fmt.Fprintf(b, "  /// `%s`\n", strings.ReplaceAll(v, "`", "'"))
		fmt.Fprintf(b, "  static const %s = %s._(%s);\n\n", members[i], typeName, dartString(v))
	}

	b.WriteString("  /// Every value.\n")
	fmt.Fprintf(b, "  static const List<%s> values = [%s];\n", typeName, strings.Join(members, ", "))
	b.WriteString("}\n")

	return out
}

// identifierWords turns every character that cannot be part of an identifier into a
// word break, so the scope read:users becomes readUsers rather than readusers.
// Scopes are conventionally written with a colon or a slash.
func identifierWords(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '$', r == '_', r == '-', r == '.':
			return r
		}

		return ' '
	}, s)
}

const capabilityState = `
Set<String>? _capabilities;
Set<String>? _roles;
Set<String>? _permissions;

/// Declares what the current principal holds. Omit a vocabulary to forget it;
/// call with no arguments on sign-out.
void setPrincipal({
  Iterable<String>? capabilities,
  Iterable<String>? roles,
  Iterable<String>? permissions,
}) {
  _capabilities = capabilities?.toSet();
  _roles = roles?.toSet();
  _permissions = permissions?.toSet();
}

/// Whether the principal's capabilities have been declared.
bool capabilitiesKnown() => _capabilities != null;

/// Whether the principal holds [scope]. False before anything is known.
bool can(Scope scope) => _capabilities?.contains(scope.value) ?? false;

/// Whether the principal holds [role]. Never a security decision.
bool hasRole(Role role) => _roles?.contains(role.value) ?? false;

/// Whether the principal holds [permission].
bool hasPermission(Permission permission) =>
    _permissions?.contains(permission.value) ?? false;
`

const capabilityPredicates = `
/// The fewest scopes the principal lacks before [operation] is permitted.
List<Scope> missingCapabilities(OperationName operation) {
  final alternatives = requiredCapabilities[operation.value];
  if (alternatives == null || alternatives.isEmpty) return const [];
  List<Scope>? fewest;
  for (final alternative in alternatives) {
    final missing = [for (final scope in alternative) if (!can(scope)) scope];
    if (missing.isEmpty) return const [];
    if (fewest == null || missing.length < fewest.length) fewest = missing;
  }
  return fewest ?? const [];
}

/// Whether the principal could call [operation] without being refused.
/// Advisory only: the server decides.
bool canCall(OperationName operation) {
  if (missingCapabilities(operation).isNotEmpty) return false;
  final authz = requiredAuthorization[operation.value];
  if (authz == null) return true;
  if (authz.roles.isNotEmpty && !authz.roles.any(hasRole)) return false;
  return authz.permissions.every(hasPermission);
}
`
