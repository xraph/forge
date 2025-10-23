package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// CompletionManager handles shell completion functionality
type CompletionManager struct {
	app    CLIApp
	config CompletionConfig
}

// NewCompletionManager creates a new completion manager
func NewCompletionManager(app CLIApp, config CompletionConfig) *CompletionManager {
	return &CompletionManager{
		app:    app,
		config: config,
	}
}

// Generate generates completion scripts for the specified shell
func (cm *CompletionManager) Generate(shell string) ([]byte, error) {
	switch strings.ToLower(shell) {
	case "bash":
		return cm.generateBash()
	case "zsh":
		return cm.generateZsh()
	case "fish":
		return cm.generateFish()
	case "powershell":
		return cm.generatePowerShell()
	default:
		return nil, fmt.Errorf("unsupported shell: %s", shell)
	}
}

// Install installs completion scripts to the appropriate location
func (cm *CompletionManager) Install(shell string) error {
	script, err := cm.Generate(shell)
	if err != nil {
		return err
	}

	installPath, err := cm.getInstallPath(shell)
	if err != nil {
		return err
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(installPath), 0755); err != nil {
		return fmt.Errorf("failed to create completion directory: %w", err)
	}

	// Write completion script
	if err := os.WriteFile(installPath, script, 0644); err != nil {
		return fmt.Errorf("failed to write completion script: %w", err)
	}

	return nil
}

// Uninstall removes installed completion scripts
func (cm *CompletionManager) Uninstall(shell string) error {
	installPath, err := cm.getInstallPath(shell)
	if err != nil {
		return err
	}

	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return nil // Already uninstalled
	}

	return os.Remove(installPath)
}

// GetInstallInstructions returns installation instructions for the shell
func (cm *CompletionManager) GetInstallInstructions(shell string) string {
	appName := cm.app.GetConfig().Name

	switch strings.ToLower(shell) {
	case "bash":
		return fmt.Sprintf(`To enable completion for bash, run:
  %s completion bash > /etc/bash_completion.d/%s

Or add to your ~/.bashrc:
  source <(%s completion bash)

Then restart your shell or run:
  source ~/.bashrc`, appName, appName, appName)

	case "zsh":
		return fmt.Sprintf(`To enable completion for zsh, run:
  %s completion zsh > "${fpath[1]}/_%s"

Or add to your ~/.zshrc:
  source <(%s completion zsh)

Then restart your shell or run:
  source ~/.zshrc`, appName, appName, appName)

	case "fish":
		return fmt.Sprintf(`To enable completion for fish, run:
  %s completion fish > ~/.config/fish/completions/%s.fish

Then restart your shell.`, appName, appName)

	case "powershell":
		return fmt.Sprintf(`To enable completion for PowerShell, add to your PowerShell profile:
  %s completion powershell | Out-String | Invoke-Expression

To find your profile location, run:
  $PROFILE`, appName)

	default:
		return fmt.Sprintf("Completion not supported for shell: %s", shell)
	}
}

// generateBash generates bash completion script
func (cm *CompletionManager) generateBash() ([]byte, error) {
	var output strings.Builder

	// Get the root command from the app
	rootCmd := cm.getRootCommand()
	if rootCmd == nil {
		return nil, fmt.Errorf("failed to get root command")
	}

	if err := rootCmd.GenBashCompletion(&output); err != nil {
		return nil, fmt.Errorf("failed to generate bash completion: %w", err)
	}

	return []byte(output.String()), nil
}

// generateZsh generates zsh completion script
func (cm *CompletionManager) generateZsh() ([]byte, error) {
	var output strings.Builder

	rootCmd := cm.getRootCommand()
	if rootCmd == nil {
		return nil, fmt.Errorf("failed to get root command")
	}

	if err := rootCmd.GenZshCompletion(&output); err != nil {
		return nil, fmt.Errorf("failed to generate zsh completion: %w", err)
	}

	return []byte(output.String()), nil
}

// generateFish generates fish completion script
func (cm *CompletionManager) generateFish() ([]byte, error) {
	var output strings.Builder

	rootCmd := cm.getRootCommand()
	if rootCmd == nil {
		return nil, fmt.Errorf("failed to get root command")
	}

	if err := rootCmd.GenFishCompletion(&output, true); err != nil {
		return nil, fmt.Errorf("failed to generate fish completion: %w", err)
	}

	return []byte(output.String()), nil
}

// generatePowerShell generates PowerShell completion script
func (cm *CompletionManager) generatePowerShell() ([]byte, error) {
	var output strings.Builder

	rootCmd := cm.getRootCommand()
	if rootCmd == nil {
		return nil, fmt.Errorf("failed to get root command")
	}

	if err := rootCmd.GenPowerShellCompletionWithDesc(&output); err != nil {
		return nil, fmt.Errorf("failed to generate PowerShell completion: %w", err)
	}

	return []byte(output.String()), nil
}

// getRootCommand gets the root cobra command from the app
func (cm *CompletionManager) getRootCommand() *cobra.Command {
	// Try to extract the cobra command from the CLIApp
	if cliApp, ok := cm.app.(*cliApp); ok {
		return cliApp.Command
	}
	return nil
}

// getInstallPath gets the installation path for completion scripts
func (cm *CompletionManager) getInstallPath(shell string) (string, error) {
	if cm.config.InstallPath != "" {
		return cm.config.InstallPath, nil
	}

	appName := cm.app.GetConfig().Name
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	switch strings.ToLower(shell) {
	case "bash":
		// Try system-wide first, fallback to user
		systemPath := fmt.Sprintf("/etc/bash_completion.d/%s", appName)
		if _, err := os.Stat("/etc/bash_completion.d"); err == nil {
			return systemPath, nil
		}
		return filepath.Join(homeDir, ".local/share/bash-completion/completions", appName), nil

	case "zsh":
		// Check common zsh completion paths
		zshPaths := []string{
			"/usr/local/share/zsh/site-functions",
			"/usr/share/zsh/site-functions",
			filepath.Join(homeDir, ".local/share/zsh/site-functions"),
		}

		for _, path := range zshPaths {
			if _, err := os.Stat(path); err == nil {
				return filepath.Join(path, fmt.Sprintf("_%s", appName)), nil
			}
		}

		// Fallback to user directory
		userPath := filepath.Join(homeDir, ".local/share/zsh/site-functions")
		return filepath.Join(userPath, fmt.Sprintf("_%s", appName)), nil

	case "fish":
		return filepath.Join(homeDir, ".config/fish/completions", fmt.Sprintf("%s.fish", appName)), nil

	case "powershell":
		return filepath.Join(homeDir, "Documents/PowerShell/Microsoft.PowerShell_profile.ps1"), nil

	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

// DetectShell attempts to detect the current shell
func (cm *CompletionManager) DetectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}

	shellName := filepath.Base(shell)
	switch shellName {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	default:
		return ""
	}
}

// IsInstalled checks if completion is installed for the shell
func (cm *CompletionManager) IsInstalled(shell string) bool {
	installPath, err := cm.getInstallPath(shell)
	if err != nil {
		return false
	}

	_, err = os.Stat(installPath)
	return err == nil
}

// CompletionValidator validates completion functionality
type CompletionValidator struct {
	manager *CompletionManager
}

// NewCompletionValidator creates a new completion validator
func NewCompletionValidator(manager *CompletionManager) *CompletionValidator {
	return &CompletionValidator{
		manager: manager,
	}
}

// ValidateCompletion validates that completion works for a command
func (cv *CompletionValidator) ValidateCompletion(command string, args []string, expected []string) error {
	// This is a simplified validation - in a real implementation,
	// you would actually test the completion functionality

	rootCmd := cv.manager.getRootCommand()
	if rootCmd == nil {
		return fmt.Errorf("failed to get root command")
	}

	// Find the command
	cmd, _, err := rootCmd.Find(append([]string{command}, args...))
	if err != nil {
		return fmt.Errorf("command not found: %w", err)
	}

	// Check if command has completion
	if cmd.ValidArgsFunction == nil && len(cmd.ValidArgs) == 0 {
		return fmt.Errorf("command %s has no completion defined", command)
	}

	return nil
}

// CustomCompletionProvider allows custom completion logic
type CustomCompletionProvider struct {
	name         string
	description  string
	completeFunc func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
}

// NewCustomCompletionProvider creates a new custom completion provider
func NewCustomCompletionProvider(name, description string, completeFunc func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)) *CustomCompletionProvider {
	return &CustomCompletionProvider{
		name:         name,
		description:  description,
		completeFunc: completeFunc,
	}
}

// Name returns the provider name
func (ccp *CustomCompletionProvider) Name() string {
	return ccp.name
}

// Description returns the provider description
func (ccp *CustomCompletionProvider) Description() string {
	return ccp.description
}

// Complete provides completion suggestions
func (ccp *CustomCompletionProvider) Complete(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return ccp.completeFunc(cmd, args, toComplete)
}

// Common completion providers

// FileCompletionProvider provides file completion
func FileCompletionProvider(extensions []string) *CustomCompletionProvider {
	return NewCustomCompletionProvider(
		"file",
		"Complete file paths",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(extensions) > 0 {
				return []string{}, cobra.ShellCompDirectiveFilterFileExt
			}
			return []string{}, cobra.ShellCompDirectiveDefault
		},
	)
}

// ServiceCompletionProvider provides service name completion
func ServiceCompletionProvider(app CLIApp) *CustomCompletionProvider {
	return NewCustomCompletionProvider(
		"service",
		"Complete service names",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			var completions []string

			// Get services from the container
			if container := app.Container(); container != nil {
				services := container.Services()
				for _, service := range services {
					if service.Name != "" && strings.HasPrefix(service.Name, toComplete) {
						completions = append(completions, service.Name)
					}
				}
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
	)
}

// PluginCompletionProvider provides plugin name completion
func PluginCompletionProvider(plugins []string) *CustomCompletionProvider {
	return NewCustomCompletionProvider(
		"plugin",
		"Complete plugin names",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			var completions []string

			for _, plugin := range plugins {
				if strings.HasPrefix(plugin, toComplete) {
					completions = append(completions, plugin)
				}
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
	)
}
