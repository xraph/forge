package farp

import "fmt"

// Protocol version constants
const (
	// ProtocolVersion is the current FARP protocol version (semver)
	ProtocolVersion = "1.0.0"

	// ProtocolMajor is the major version
	ProtocolMajor = 1

	// ProtocolMinor is the minor version
	ProtocolMinor = 0

	// ProtocolPatch is the patch version
	ProtocolPatch = 0
)

// VersionInfo provides version information about the protocol
type VersionInfo struct {
	// Version is the full semver string
	Version string `json:"version"`

	// Major version number
	Major int `json:"major"`

	// Minor version number
	Minor int `json:"minor"`

	// Patch version number
	Patch int `json:"patch"`
}

// GetVersion returns the current protocol version information
func GetVersion() VersionInfo {
	return VersionInfo{
		Version: ProtocolVersion,
		Major:   ProtocolMajor,
		Minor:   ProtocolMinor,
		Patch:   ProtocolPatch,
	}
}

// IsCompatible checks if a manifest version is compatible with this protocol version
// Compatible means the major version matches and the manifest's minor version
// is less than or equal to the protocol's minor version
func IsCompatible(manifestVersion string) bool {
	// Parse manifest version (simple parsing for semver)
	var major, minor, patch int
	_, err := fmt.Sscanf(manifestVersion, "%d.%d.%d", &major, &minor, &patch)
	if err != nil {
		return false
	}

	// Major version must match
	if major != ProtocolMajor {
		return false
	}

	// Protocol must support manifest's minor version or higher
	return minor <= ProtocolMinor
}

