package contract

import "gopkg.in/yaml.v3"

// UnmarshalManifestForTest is a test helper exposed for use by sibling packages.
// It is not part of the package's runtime API; production code should not call it.
func UnmarshalManifestForTest(b []byte, m *ContractManifest) error {
	return yaml.Unmarshal(b, m)
}
