package cli

// Plugin represents a CLI plugin that can provide commands
type Plugin interface {
	// Name returns the unique name of the plugin
	Name() string

	// Version returns the plugin version
	Version() string

	// Description returns a human-readable description
	Description() string

	// Commands returns the commands provided by this plugin
	Commands() []Command

	// Dependencies returns the names of plugins this plugin depends on
	Dependencies() []string

	// Initialize is called when the plugin is registered
	// This allows the plugin to perform setup before commands are added
	Initialize() error
}

// PluginMetadata contains information about a registered plugin
type PluginMetadata struct {
	Name         string
	Version      string
	Description  string
	Dependencies []string
	Commands     []string // Command names provided by this plugin
}
