package cli

import (
	"fmt"
	"strings"
)

// generateHelp generates help text for a CLI or command.
func generateHelp(cli CLI, cmd Command) string {
	var sb strings.Builder

	if cmd == nil {
		// Generate main CLI help
		fmt.Fprintf(&sb, "%s v%s\n", Bold(cli.Name()), cli.Version())

		if cli.Description() != "" {
			sb.WriteString(cli.Description() + "\n")
		}

		sb.WriteString("\n")

		// Usage
		sb.WriteString(Bold("USAGE:\n"))
		fmt.Fprintf(&sb, "  %s [command] [flags]\n\n", cli.Name())

		// Commands
		if len(cli.Commands()) > 0 {
			sb.WriteString(Bold("COMMANDS:\n"))

			for _, c := range cli.Commands() {
				fmt.Fprintf(&sb, "  %-15s %s\n", c.Name(), c.Description())
			}

			sb.WriteString("\n")
		}

		// Global flags (if any)
		sb.WriteString(Bold("FLAGS:\n"))
		sb.WriteString("  -h, --help      Show help\n")
		sb.WriteString("  -v, --version   Show version\n")

		sb.WriteString("\n")
		fmt.Fprintf(&sb, "Use \"%s [command] --help\" for more information about a command.\n", cli.Name())
	} else {
		// Generate command help
		sb.WriteString(Bold(cmd.Description()) + "\n")

		if cmd.Description() == "" {
			sb.WriteString(Bold(cmd.Name()) + "\n")
		}

		sb.WriteString("\n")

		// Usage
		sb.WriteString(Bold("USAGE:\n"))
		fmt.Fprintf(&sb, "  %s %s\n\n", cli.Name(), cmd.Usage())

		// Aliases
		if len(cmd.Aliases()) > 0 {
			sb.WriteString(Bold("ALIASES:\n"))
			fmt.Fprintf(&sb, "  %s\n\n", strings.Join(cmd.Aliases(), ", "))
		}

		// Subcommands
		if len(cmd.Subcommands()) > 0 {
			sb.WriteString(Bold("SUBCOMMANDS:\n"))

			for _, sub := range cmd.Subcommands() {
				fmt.Fprintf(&sb, "  %-15s %s\n", sub.Name(), sub.Description())
			}

			sb.WriteString("\n")
		}

		// Flags
		if len(cmd.Flags()) > 0 {
			sb.WriteString(Bold("FLAGS:\n"))

			for _, flag := range cmd.Flags() {
				flagLine := formatFlagHelp(flag)
				sb.WriteString(flagLine)
			}

			sb.WriteString("\n")
		}

		// Always show help flag
		sb.WriteString(Bold("GLOBAL FLAGS:\n"))
		sb.WriteString("  -h, --help      Show help\n")
	}

	return sb.String()
}

// formatFlagHelp formats help text for a flag.
func formatFlagHelp(flag Flag) string {
	var sb strings.Builder

	sb.WriteString("  ")

	// Short name
	if flag.ShortName() != "" {
		fmt.Fprintf(&sb, "-%s, ", flag.ShortName())
	} else {
		sb.WriteString("    ")
	}

	// Long name
	sb.WriteString("--" + flag.Name())

	// Type hint
	switch flag.Type() {
	case StringFlagType:
		sb.WriteString(" <string>")
	case IntFlagType:
		sb.WriteString(" <int>")
	case StringSliceFlagType:
		sb.WriteString(" <string>...")
	case DurationFlagType:
		sb.WriteString(" <duration>")
	}

	// Padding for description
	padding := max(
		// 2 spaces at start
		30-sb.Len()+2, 2)

	sb.WriteString(strings.Repeat(" ", padding))

	// Description
	sb.WriteString(flag.Description())

	// Default value
	if flag.DefaultValue() != nil && flag.DefaultValue() != "" && flag.DefaultValue() != 0 && flag.DefaultValue() != false {
		fmt.Fprintf(&sb, " (default: %v)", flag.DefaultValue())
	}

	// Required marker
	if flag.Required() {
		sb.WriteString(Red(" [required]"))
	}

	sb.WriteString("\n")

	return sb.String()
}

// generateUsageLine generates a usage line for a command.
func generateUsageLine(cliName string, cmd Command) string {
	parts := []string{cliName}

	// Add command path
	cmdPath := []string{}

	current := cmd
	for current != nil {
		cmdPath = append([]string{current.Name()}, cmdPath...)
		current = current.Parent()
	}

	parts = append(parts, cmdPath...)

	// Add flags indicator
	if len(cmd.Flags()) > 0 {
		parts = append(parts, "[flags]")
	}

	// Add subcommands indicator
	if len(cmd.Subcommands()) > 0 {
		parts = append(parts, "[subcommand]")
	}

	return strings.Join(parts, " ")
}
