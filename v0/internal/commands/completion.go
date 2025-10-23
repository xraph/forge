package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/pkg/cli"
)

// CompletionCommand creates the 'forge completion' command
func CompletionCommand() *cli.Command {
	return &cli.Command{
		Use:   "completion [shell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for forge commands.

Supported shells:
  • bash
  • zsh  
  • fish
  • powershell

The completion script should be sourced or saved to the appropriate
location for your shell.`,
		Example: `  # Generate bash completion
  forge completion bash

  # Generate and install bash completion
  forge completion bash > /etc/bash_completion.d/forge

  # Generate zsh completion  
  forge completion zsh > "${fpath[1]}/_forge"

  # Generate fish completion
  forge completion fish > ~/.config/fish/completions/forge.fish

  # Generate PowerShell completion
  forge completion powershell > forge.ps1`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return []string{"bash", "zsh", "fish", "powershell"}, cobra.ShellCompDirectiveDefault
		},
		Run: func(ctx cli.CLIContext, args []string) error {
			shell := args[0]

			if err := ctx.App().GenerateCompletion(shell); err != nil {
				return fmt.Errorf("failed to generate %s completion: %w", shell, err)
			}

			return nil
		},
	}
}
