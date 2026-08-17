package client

// authzExtension is the operation-level extension carrying what OpenAPI's
// security vocabulary cannot express.
const authzExtension = "x-forge-authz"

// resolveEndpointAuthz reads an endpoint's declared roles and permissions out
// of its raw x-forge-* extensions.
//
// This is the one place either intermediate-representation builder resolves
// authorization metadata: Introspector.extractFromOpenAPI for a live router,
// SpecParser for a file. It is one place rather than two for the same reason
// resolveEndpointCacheMeta is, and that comment says it best: a
// live-versus-file divergence in exactly this kind of metadata has been a
// recurring defect in this package.
//
// Returns nil rather than an empty value when nothing is declared, so a
// generator can test the pointer instead of counting slices, and so an
// unguarded endpoint never looks guarded.
func resolveEndpointAuthz(ext map[string]any) *Authorization {
	if len(ext) == 0 {
		return nil
	}

	raw, ok := ext[authzExtension].(map[string]any)
	if !ok {
		return nil
	}

	roles := sortedUniqueScopes(stringSlice(raw["roles"]))
	permissions := sortedUniqueScopes(stringSlice(raw["permissions"]))

	if len(roles) == 0 && len(permissions) == 0 {
		return nil
	}

	return &Authorization{Roles: roles, Permissions: permissions}
}
