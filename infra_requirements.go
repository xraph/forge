package forge

// InfraRequirement describes a backend used by an extension. It carries config
// paths and instance names only. Keep credential values in runtime configuration.
type InfraRequirement struct {
	Kind      string `json:"kind"`
	Instance  string `json:"instance,omitempty"`
	ConfigKey string `json:"config_key"`
	Optional  bool   `json:"optional,omitempty"`
}

// InfraRequirer lets an extension report its deployment needs after Register.
// Implementations must not start services or resolve credentials for this report.
type InfraRequirer interface {
	Extension
	InfraRequirements() []InfraRequirement
}

const InfraSchemaVersion = "forge.infra/v1"

type ExtensionInfraRequirement struct {
	InfraRequirement

	Extension string `json:"extension"`
}

type InfraReport struct {
	Schema       string                      `json:"schema"`
	App          string                      `json:"app"`
	Requirements []ExtensionInfraRequirement `json:"requirements"`
}
