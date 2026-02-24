package forge

// CLICommandProvider is an optional interface for extensions that want to
// contribute CLI commands when the app is wrapped in a CLI runner.
//
// The CLICommands method returns a slice of any values that must each
// implement cli.Command. The type is []any (rather than []cli.Command)
// to avoid a circular import — the forge package cannot import forge/cli
// since cli already imports forge.
//
// The CLI wrapper (cli.RunApp) performs type assertions at registration time
// and logs warnings for values that don't implement cli.Command.
//
// Example:
//
//	func (e *MyExtension) CLICommands() []any {
//	    return []any{
//	        cli.NewCommand("seed", "Seed the database", e.handleSeed),
//	        cli.NewCommand("dump", "Dump database schema", e.handleDump),
//	    }
//	}
type CLICommandProvider interface {
	Extension

	// CLICommands returns CLI commands contributed by this extension.
	// Each element in the returned slice must implement cli.Command.
	CLICommands() []any
}
