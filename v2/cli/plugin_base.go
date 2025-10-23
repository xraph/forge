package cli

// BasePlugin provides a base implementation for plugins
type BasePlugin struct {
	name         string
	version      string
	description  string
	commands     []Command
	dependencies []string
}

// NewBasePlugin creates a new base plugin
func NewBasePlugin(name, version, description string) *BasePlugin {
	return &BasePlugin{
		name:         name,
		version:      version,
		description:  description,
		commands:     []Command{},
		dependencies: []string{},
	}
}

// Name returns the plugin name
func (p *BasePlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *BasePlugin) Version() string {
	return p.version
}

// Description returns the plugin description
func (p *BasePlugin) Description() string {
	return p.description
}

// Commands returns the commands provided by this plugin
func (p *BasePlugin) Commands() []Command {
	return p.commands
}

// Dependencies returns the plugin dependencies
func (p *BasePlugin) Dependencies() []string {
	return p.dependencies
}

// Initialize is called when the plugin is registered
func (p *BasePlugin) Initialize() error {
	// Default implementation does nothing
	return nil
}

// AddCommand adds a command to the plugin
func (p *BasePlugin) AddCommand(cmd Command) {
	p.commands = append(p.commands, cmd)
}

// AddDependency adds a dependency to the plugin
func (p *BasePlugin) AddDependency(dep string) {
	p.dependencies = append(p.dependencies, dep)
}

// SetCommands sets the commands for the plugin
func (p *BasePlugin) SetCommands(commands []Command) {
	p.commands = commands
}

// SetDependencies sets the dependencies for the plugin
func (p *BasePlugin) SetDependencies(dependencies []string) {
	p.dependencies = dependencies
}
