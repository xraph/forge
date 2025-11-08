package cli

import (
	"slices"
	"strings"
)

// isFlag checks if an argument is a flag.
func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "-")
}

// parseFlag parses a flag argument and returns name, value, and whether value was included
// Supports:
// - Long flags: --name=value, --name value
// - Short flags: -n value, -n=value
// - Boolean flags: --verbose, -v
// - Combined short flags: -vvv (treated as separate -v -v -v).
func parseFlag(arg string) (name, value string, hasValue bool) {
	// Remove leading dashes
	arg = strings.TrimLeft(arg, "-")

	// Check for = separator
	if idx := strings.Index(arg, "="); idx != -1 {
		return arg[:idx], arg[idx+1:], true
	}

	// No value included in the argument
	return arg, "", false
}

// splitArgs splits arguments into flags and positional arguments.
func splitArgs(args []string) (flags []string, positional []string) {
	for i := range args {
		arg := args[i]

		// Check for -- separator (everything after is positional)
		if arg == "--" {
			positional = append(positional, args[i+1:]...)

			break
		}

		if isFlag(arg) {
			flags = append(flags, arg)

			// Check if next arg is a value (not a flag)
			if i+1 < len(args) && !isFlag(args[i+1]) && !strings.Contains(arg, "=") {
				// This might be a value for the flag
				// We'll handle this in parseFlags
			}
		} else {
			positional = append(positional, arg)
		}
	}

	return flags, positional
}

// expandShortFlags expands combined short flags like -abc into -a -b -c.
func expandShortFlags(args []string) []string {
	expanded := []string{}

	for _, arg := range args {
		// Only expand if it's a short flag (single dash) and has multiple characters
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(arg) > 2 && !strings.Contains(arg, "=") {
			// Remove leading dash
			flags := strings.TrimPrefix(arg, "-")

			// Expand each character
			for _, ch := range flags {
				expanded = append(expanded, "-"+string(ch))
			}
		} else {
			expanded = append(expanded, arg)
		}
	}

	return expanded
}

// findCommand finds a command by name or alias, traversing subcommands.
func findCommand(root Command, path []string) (Command, []string, error) {
	if len(path) == 0 {
		return root, path, nil
	}

	// Check if first element is a subcommand
	if sub, found := root.FindSubcommand(path[0]); found {
		// Recursively search in subcommand
		return findCommand(sub, path[1:])
	}

	// No subcommand found, return current command and remaining path
	return root, path, nil
}

// parseCommandPath parses the command path from arguments
// Returns the command name and remaining arguments.
func parseCommandPath(args []string) ([]string, []string) {
	commandPath := []string{}
	remainingArgs := []string{}

	for i, arg := range args {
		if isFlag(arg) {
			// Hit a flag, everything from here is arguments
			remainingArgs = args[i:]

			break
		}

		commandPath = append(commandPath, arg)
	}

	// If we didn't hit any flags, the last element might be an argument
	if len(remainingArgs) == 0 && len(commandPath) > 0 {
		// Check if we should keep parsing for subcommands
		// This is handled by findCommand
		remainingArgs = []string{}
	}

	return commandPath, remainingArgs
}

// isHelpFlag checks if an argument is a help flag.
func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

// hasHelpFlag checks if arguments contain a help flag.
func hasHelpFlag(args []string) bool {

	return slices.ContainsFunc(args, isHelpFlag)
}
