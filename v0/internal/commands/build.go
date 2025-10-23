package commands

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/xraph/forge/internal/services"
	"github.com/xraph/forge/pkg/cli"
	"github.com/xraph/forge/pkg/cli/prompt"
)

// BuildCommand creates the 'forge build' command
func BuildCommand() *cli.Command {
	var (
		output     string
		target     string
		tags       []string
		ldflags    string
		gcflags    string
		race       bool
		msan       bool
		asan       bool
		trimpath   bool
		buildmode  string
		compiler   string
		gccgoflags string
		verbose    bool
		cmd        string
	)

	return &cli.Command{
		Use:   "build [package|command]",
		Short: "Build the application",
		Long: `Build the Forge application with optimizations.

The build command compiles your Forge application with production
optimizations and can cross-compile for different platforms.

You can build in several ways:
  • No argument: Interactively select from available commands in cmd/
  • Command name: Build a specific command (e.g., "api", "worker") 
  • Package path: Build a specific Go package path

Supported targets:
  • linux/amd64, linux/arm64
  • darwin/amd64, darwin/arm64  
  • windows/amd64, windows/arm64`,
		Example: `  # Interactively select command to build
  forge build

  # Build specific command by name
  forge build api

  # Build with custom output name
  forge build --output myapp

  # Cross-compile for Linux
  forge build --target linux/amd64

  # Build with race detection
  forge build --race

  # Build with custom build tags
  forge build --tags "prod,mysql"

  # Build with optimization flags
  forge build --ldflags "-s -w"

  # Build specific package path
  forge build ./cmd/api`,
		Run: func(ctx cli.CLIContext, args []string) error {
			var buildService services.BuildService
			if err := ctx.Resolve(&buildService); err != nil {
				return fmt.Errorf("build service not available: %w", err)
			}

			var packagePath string
			var commandName string

			// Determine what to build
			if len(args) > 0 {
				arg := args[0]

				// Check if argument looks like a package path (contains ./ or /)
				if strings.Contains(arg, "/") || strings.HasPrefix(arg, ".") {
					packagePath = arg
				} else {
					// Assume it's a command name
					commandName = arg
				}
			} else if cmd != "" {
				// Use the --cmd flag
				commandName = cmd
			}

			// Parse target platform
			targetOS := runtime.GOOS
			targetArch := runtime.GOARCH
			if target != "" {
				parts := strings.SplitN(target, "/", 2)
				if len(parts) == 2 {
					targetOS = parts[0]
					targetArch = parts[1]
				} else {
					return fmt.Errorf("invalid target format: %s (expected OS/ARCH)", target)
				}
			}

			// Build configuration from CLI flags
			config := services.BuildConfig{
				Package:    packagePath,
				Output:     output,
				TargetOS:   targetOS,
				TargetArch: targetArch,
				Tags:       tags,
				Ldflags:    ldflags,
				Gcflags:    gcflags,
				Race:       race,
				Msan:       msan,
				Asan:       asan,
				Trimpath:   trimpath,
				Buildmode:  buildmode,
				Compiler:   compiler,
				Gccgoflags: gccgoflags,
				Verbose:    verbose,
			}

			var result *services.BuildResult
			var err error

			// Get configuration manager from context
			configManager := ctx.Config()

			if commandName != "" {
				// Build specific command by name
				spinner := ctx.Spinner(fmt.Sprintf("Building command '%s' for %s/%s...", commandName, targetOS, targetArch))
				defer spinner.Stop()

				result, err = buildService.BuildCommand(context.Background(), commandName, config, configManager)
			} else if packagePath != "" {
				// Build specific package
				spinner := ctx.Spinner(fmt.Sprintf("Building package '%s' for %s/%s...", packagePath, targetOS, targetArch))
				defer spinner.Stop()

				result, err = buildService.BuildWithConfig(context.Background(), config, configManager)
			} else {
				// Interactive command selection
				commands, err := buildService.GetAvailableCommands()
				if err != nil {
					return err
				}

				if len(commands) == 0 {
					return fmt.Errorf("no valid commands found in cmd/ directory")
				}

				// Filter valid commands
				validCommands := make([]services.CommandInfo, 0)
				for _, c := range commands {
					if c.IsValid {
						validCommands = append(validCommands, c)
					}
				}

				if len(validCommands) == 0 {
					return fmt.Errorf("no valid commands found (commands must have main.go)")
				}

				// Interactive selection
				selectedCmd, err := selectCommandToBuild(ctx, validCommands)
				if err != nil {
					return err
				}

				spinner := ctx.Spinner(fmt.Sprintf("Building command '%s' for %s/%s...", selectedCmd.Name, targetOS, targetArch))
				defer spinner.Stop()

				result, err = buildService.BuildCommand(context.Background(), selectedCmd.Name, config, configManager)
			}

			if err != nil {
				ctx.Error("Build failed")
				return err
			}

			ctx.Success(fmt.Sprintf("Built successfully: %s", result.OutputPath))
			ctx.Info(fmt.Sprintf("Binary size: %s", result.Size))
			ctx.Info(fmt.Sprintf("Build time: %s", result.Duration))
			ctx.Info(fmt.Sprintf("Platform: %s", result.Platform))

			return nil
		},
		Flags: []*cli.FlagDefinition{
			cli.StringFlag("output", "o", "Output binary name", false),
			cli.StringFlag("target", "", "Target platform (OS/ARCH)", false),
			cli.StringSliceFlag("tags", "", []string{}, "Build tags", false),
			cli.StringFlag("ldflags", "", "Linker flags", false),
			cli.StringFlag("gcflags", "", "Compiler flags", false),
			cli.BoolFlag("race", "", "Enable race detector"),
			cli.BoolFlag("msan", "", "Enable memory sanitizer"),
			cli.BoolFlag("asan", "", "Enable address sanitizer"),
			cli.BoolFlag("trimpath", "", "Remove file system paths from binary").WithDefault(true),
			cli.StringFlag("buildmode", "", "Build mode", false),
			cli.StringFlag("compiler", "", "Compiler to use", false),
			cli.StringFlag("gccgoflags", "", "Flags for gccgo", false),
			cli.BoolFlag("verbose", "v", "Enable verbose build output"),
			cli.StringFlag("cmd", "", "Specific command to build (if not specified, will prompt for selection)", false),
		},
	}
}

// selectCommandToBuild provides interactive command selection for building
func selectCommandToBuild(ctx cli.CLIContext, commands []services.CommandInfo) (*services.CommandInfo, error) {
	// Convert commands to selection options
	options := make([]prompt.SelectionOption, len(commands))
	for i, cmd := range commands {
		description := cmd.Description
		if description == "" {
			description = "No description available"
		}

		options[i] = prompt.SelectionOption{
			Value:       cmd.Name,
			Description: description,
			Disabled:    !cmd.IsValid,
			Metadata: map[string]interface{}{
				"path":     cmd.Path,
				"has_main": cmd.HasMainGo,
				"is_valid": cmd.IsValid,
			},
		}
	}

	// Use CLIContext's interactive selection
	index, selectedOption, err := ctx.InteractiveSelect("Select command to build:", options)
	if err != nil {
		return nil, err
	}

	if selectedOption.Disabled {
		return nil, fmt.Errorf("selected command is not valid")
	}

	return &commands[index], nil
}
