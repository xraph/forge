package client

// ResolveEntityFieldsForTest exposes resolveEntityFields to the external test
// package. Test-only: this file is not compiled into the package binary.
func ResolveEntityFieldsForTest(spec *APISpec) { resolveEntityFields(spec) }
