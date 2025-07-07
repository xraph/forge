package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/logger"
)

// CLICommands provides CLI commands for plugin management
type CLICommands struct {
	manager  Manager
	registry Registry
	loader   Loader
	logger   logger.Logger
}

// NewCLICommands creates new plugin CLI commands
func NewCLICommands(manager Manager, registry Registry, loader Loader) *CLICommands {
	return &CLICommands{
		manager:  manager,
		registry: registry,
		loader:   loader,
		logger:   logger.GetGlobalLogger().Named("plugin-cli"),
	}
}

// CreateCommands creates all plugin-related CLI commands
func (c *CLICommands) CreateCommands() *cobra.Command {
	pluginCmd := &cobra.Command{
		Use:   "plugin",
		Short: "Plugin management commands",
		Long:  "Commands for managing, installing, and configuring plugins",
	}

	// Add subcommands
	pluginCmd.AddCommand(c.createListCommand())
	pluginCmd.AddCommand(c.createInstallCommand())
	pluginCmd.AddCommand(c.createUninstallCommand())
	pluginCmd.AddCommand(c.createEnableCommand())
	pluginCmd.AddCommand(c.createDisableCommand())
	pluginCmd.AddCommand(c.createInfoCommand())
	pluginCmd.AddCommand(c.createSearchCommand())
	pluginCmd.AddCommand(c.createUpdateCommand())
	pluginCmd.AddCommand(c.createConfigCommand())
	pluginCmd.AddCommand(c.createStatusCommand())
	pluginCmd.AddCommand(c.createValidateCommand())
	pluginCmd.AddCommand(c.createDevCommand())

	return pluginCmd
}

// List command - lists installed plugins
func (c *CLICommands) createListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		Long:  "Display a list of all installed plugins with their status",
		RunE:  c.runListCommand,
	}

	cmd.Flags().Bool("enabled", false, "Show only enabled plugins")
	cmd.Flags().Bool("disabled", false, "Show only disabled plugins")
	cmd.Flags().String("format", "table", "Output format (table, json, yaml)")
	cmd.Flags().String("sort", "name", "Sort by (name, version, status, author)")

	return cmd
}

func (c *CLICommands) runListCommand(cmd *cobra.Command, args []string) error {
	enabledOnly, _ := cmd.Flags().GetBool("enabled")
	disabledOnly, _ := cmd.Flags().GetBool("disabled")
	format, _ := cmd.Flags().GetString("format")
	sortBy, _ := cmd.Flags().GetString("sort")

	var plugins []Plugin
	if enabledOnly {
		plugins = c.manager.ListEnabled()
	} else {
		plugins = c.manager.List()
	}

	// Filter by status if needed
	if disabledOnly {
		var filteredPlugins []Plugin
		enabledPlugins := c.manager.ListEnabled()
		enabledMap := make(map[string]bool)
		for _, p := range enabledPlugins {
			enabledMap[p.Name()] = true
		}

		for _, p := range plugins {
			if !enabledMap[p.Name()] {
				filteredPlugins = append(filteredPlugins, p)
			}
		}
		plugins = filteredPlugins
	}

	// Sort plugins
	c.sortPlugins(plugins, sortBy)

	// Output in requested format
	switch format {
	case "json":
		return c.outputPluginsJSON(plugins)
	case "yaml":
		return c.outputPluginsYAML(plugins)
	default:
		return c.outputPluginsTable(plugins)
	}
}

// Install command - installs a plugin
func (c *CLICommands) createInstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [plugin-name] [version]",
		Short: "Install a plugin",
		Long:  "Install a plugin from the registry or from a local file",
		Args:  cobra.MinimumNArgs(1),
		RunE:  c.runInstallCommand,
	}

	cmd.Flags().String("source", "", "Installation source (registry, file, url)")
	cmd.Flags().String("file", "", "Local plugin file path")
	cmd.Flags().String("url", "", "Plugin download URL")
	cmd.Flags().Bool("enable", true, "Enable plugin after installation")
	cmd.Flags().Bool("force", false, "Force installation over existing plugin")
	cmd.Flags().Bool("no-deps", false, "Skip dependency installation")

	return cmd
}

func (c *CLICommands) runInstallCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]
	version := ""
	if len(args) > 1 {
		version = args[1]
	}

	source, _ := cmd.Flags().GetString("source")
	filePath, _ := cmd.Flags().GetString("file")
	url, _ := cmd.Flags().GetString("url")
	enable, _ := cmd.Flags().GetBool("enable")
	force, _ := cmd.Flags().GetBool("force")
	noDeps, _ := cmd.Flags().GetBool("no-deps")

	fmt.Printf("Installing plugin: %s", pluginName)
	if version != "" {
		fmt.Printf(" (version: %s)", version)
	}
	fmt.Println(source)

	// Check if plugin already exists
	if !force {
		if _, err := c.manager.Get(pluginName); err == nil {
			return fmt.Errorf("plugin %s is already installed (use --force to reinstall)", pluginName)
		}
	}

	var plugin Plugin
	var err error

	switch {
	case filePath != "":
		// Install from local file
		plugin, err = c.installFromFile(filePath)
	case url != "":
		// Install from URL
		plugin, err = c.installFromURL(url)
	default:
		// Install from registry
		plugin, err = c.installFromRegistry(pluginName, version)
	}

	if err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	// Register plugin
	if err := c.manager.Register(plugin); err != nil {
		return fmt.Errorf("failed to register plugin: %w", err)
	}

	// Install dependencies if needed
	if !noDeps {
		if err := c.installDependencies(plugin); err != nil {
			c.logger.Warn("Failed to install some dependencies", logger.Error(err))
		}
	}

	// Enable plugin if requested
	if enable {
		if err := c.manager.Enable(pluginName); err != nil {
			c.logger.Warn("Failed to enable plugin", logger.Error(err))
		}
	}

	fmt.Printf("✓ Plugin %s installed successfully\n", pluginName)
	return nil
}

// Uninstall command - uninstalls a plugin
func (c *CLICommands) createUninstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall [plugin-name]",
		Short: "Uninstall a plugin",
		Long:  "Remove a plugin from the system",
		Args:  cobra.ExactArgs(1),
		RunE:  c.runUninstallCommand,
	}

	cmd.Flags().Bool("force", false, "Force uninstallation even if other plugins depend on it")
	cmd.Flags().Bool("keep-config", false, "Keep plugin configuration")

	return cmd
}

func (c *CLICommands) runUninstallCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]
	force, _ := cmd.Flags().GetBool("force")
	keepConfig, _ := cmd.Flags().GetBool("keep-config")

	fmt.Printf("Uninstalling plugin: %s\n", pluginName)

	// Check if plugin exists
	_, err := c.manager.Get(pluginName)
	if err != nil {
		return fmt.Errorf("plugin not found: %s", pluginName)
	}

	// Check dependencies if not forced
	if !force {
		if err := c.checkUninstallDependencies(pluginName); err != nil {
			return err
		}
	}

	// Disable and unregister plugin
	if err := c.manager.Disable(pluginName); err != nil {
		c.logger.Warn("Failed to disable plugin", logger.Error(err))
	}

	if err := c.manager.Unregister(pluginName); err != nil {
		return fmt.Errorf("failed to unregister plugin: %w", err)
	}

	// Remove plugin configuration if not keeping it
	if !keepConfig {
		// This would remove configuration files
		c.logger.Info("Removing plugin configuration", logger.String("plugin", pluginName))
	}

	fmt.Printf("✓ Plugin %s uninstalled successfully\n", pluginName)
	return nil
}

// Enable command - enables a plugin
func (c *CLICommands) createEnableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable [plugin-name]",
		Short: "Enable a plugin",
		Long:  "Enable a previously disabled plugin",
		Args:  cobra.ExactArgs(1),
		RunE:  c.runEnableCommand,
	}

	return cmd
}

func (c *CLICommands) runEnableCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]

	if err := c.manager.Enable(pluginName); err != nil {
		return fmt.Errorf("failed to enable plugin %s: %w", pluginName, err)
	}

	fmt.Printf("✓ Plugin %s enabled successfully\n", pluginName)
	return nil
}

// Disable command - disables a plugin
func (c *CLICommands) createDisableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable [plugin-name]",
		Short: "Disable a plugin",
		Long:  "Disable a plugin without uninstalling it",
		Args:  cobra.ExactArgs(1),
		RunE:  c.runDisableCommand,
	}

	return cmd
}

func (c *CLICommands) runDisableCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]

	if err := c.manager.Disable(pluginName); err != nil {
		return fmt.Errorf("failed to disable plugin %s: %w", pluginName, err)
	}

	fmt.Printf("✓ Plugin %s disabled successfully\n", pluginName)
	return nil
}

// Info command - shows plugin information
func (c *CLICommands) createInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info [plugin-name]",
		Short: "Show plugin information",
		Long:  "Display detailed information about a plugin",
		Args:  cobra.ExactArgs(1),
		RunE:  c.runInfoCommand,
	}

	cmd.Flags().String("format", "table", "Output format (table, json, yaml)")

	return cmd
}

func (c *CLICommands) runInfoCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]
	format, _ := cmd.Flags().GetString("format")

	plugin, err := c.manager.Get(pluginName)
	if err != nil {
		return fmt.Errorf("plugin not found: %s", pluginName)
	}

	status := c.manager.Status()[pluginName]

	switch format {
	case "json":
		return c.outputPluginInfoJSON(plugin, status)
	case "yaml":
		return c.outputPluginInfoYAML(plugin, status)
	default:
		return c.outputPluginInfoTable(plugin, status)
	}
}

// Search command - searches for plugins in registry
func (c *CLICommands) createSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search for plugins",
		Long:  "Search for plugins in the registry",
		Args:  cobra.MaximumNArgs(1),
		RunE:  c.runSearchCommand,
	}

	cmd.Flags().String("category", "", "Filter by category")
	cmd.Flags().String("author", "", "Filter by author")
	cmd.Flags().Bool("popular", false, "Show popular plugins")
	cmd.Flags().Int("limit", 20, "Maximum number of results")

	return cmd
}

func (c *CLICommands) runSearchCommand(cmd *cobra.Command, args []string) error {
	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	category, _ := cmd.Flags().GetString("category")
	author, _ := cmd.Flags().GetString("author")
	popular, _ := cmd.Flags().GetBool("popular")
	limit, _ := cmd.Flags().GetInt("limit")

	var results []PluginMetadata

	if popular {
		results = c.registry.GetPopular(limit)
	} else if category != "" {
		results = c.registry.GetByCategory(category)
	} else {
		results = c.registry.Search(query)
	}

	// Filter by author if specified
	if author != "" {
		var filtered []PluginMetadata
		for _, plugin := range results {
			if strings.Contains(strings.ToLower(plugin.Author), strings.ToLower(author)) {
				filtered = append(filtered, plugin)
			}
		}
		results = filtered
	}

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return c.outputSearchResults(results)
}

// Update command - updates plugins
func (c *CLICommands) createUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [plugin-name]",
		Short: "Update plugins",
		Long:  "Update one or all plugins to the latest version",
		Args:  cobra.MaximumNArgs(1),
		RunE:  c.runUpdateCommand,
	}

	cmd.Flags().Bool("all", false, "Update all plugins")
	cmd.Flags().Bool("check", false, "Only check for updates, don't install")

	return cmd
}

func (c *CLICommands) runUpdateCommand(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	checkOnly, _ := cmd.Flags().GetBool("check")

	if all {
		return c.updateAllPlugins(checkOnly)
	}

	if len(args) == 0 {
		return fmt.Errorf("plugin name required (or use --all)")
	}

	pluginName := args[0]
	return c.updatePlugin(pluginName, checkOnly)
}

// Config command - manages plugin configuration
func (c *CLICommands) createConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage plugin configuration",
		Long:  "View and modify plugin configuration",
	}

	cmd.AddCommand(c.createConfigGetCommand())
	cmd.AddCommand(c.createConfigSetCommand())
	cmd.AddCommand(c.createConfigListCommand())
	cmd.AddCommand(c.createConfigResetCommand())

	return cmd
}

func (c *CLICommands) createConfigGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get [plugin-name] [key]",
		Short: "Get configuration value",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  c.runConfigGetCommand,
	}
}

func (c *CLICommands) runConfigGetCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]

	config := c.manager.GetConfig(pluginName)
	if config == nil {
		return fmt.Errorf("no configuration found for plugin: %s", pluginName)
	}

	if len(args) == 2 {
		key := args[1]
		if value, exists := config[key]; exists {
			fmt.Printf("%v\n", value)
		} else {
			return fmt.Errorf("configuration key not found: %s", key)
		}
	} else {
		// Show all configuration
		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	}

	return nil
}

func (c *CLICommands) createConfigSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set [plugin-name] [key] [value]",
		Short: "Set configuration value",
		Args:  cobra.ExactArgs(3),
		RunE:  c.runConfigSetCommand,
	}
}

func (c *CLICommands) runConfigSetCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]
	key := args[1]
	value := args[2]

	// Get current config
	config := c.manager.GetConfig(pluginName)
	if config == nil {
		config = make(map[string]interface{})
	}

	// Set value (with basic type conversion)
	config[key] = c.convertStringValue(value)

	// Update configuration
	if err := c.manager.SetConfig(pluginName, config); err != nil {
		return fmt.Errorf("failed to set configuration: %w", err)
	}

	fmt.Printf("✓ Configuration updated: %s.%s = %s\n", pluginName, key, value)
	return nil
}

func (c *CLICommands) createConfigListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all plugin configurations",
		RunE:  c.runConfigListCommand,
	}
}

func (c *CLICommands) runConfigListCommand(cmd *cobra.Command, args []string) error {
	plugins := c.manager.List()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PLUGIN\tCONFIG KEYS\tLAST MODIFIED")

	for _, plugin := range plugins {
		config := c.manager.GetConfig(plugin.Name())
		keys := make([]string, 0, len(config))
		for k := range config {
			keys = append(keys, k)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n",
			plugin.Name(),
			strings.Join(keys, ", "),
			"N/A", // Would show actual modification time
		)
	}

	return w.Flush()
}

func (c *CLICommands) createConfigResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset [plugin-name]",
		Short: "Reset plugin configuration to defaults",
		Args:  cobra.ExactArgs(1),
		RunE:  c.runConfigResetCommand,
	}
}

func (c *CLICommands) runConfigResetCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]

	plugin, err := c.manager.Get(pluginName)
	if err != nil {
		return fmt.Errorf("plugin not found: %s", pluginName)
	}

	defaultConfig := plugin.DefaultConfig()
	if err := c.manager.SetConfig(pluginName, defaultConfig); err != nil {
		return fmt.Errorf("failed to reset configuration: %w", err)
	}

	fmt.Printf("✓ Configuration reset to defaults for plugin: %s\n", pluginName)
	return nil
}

// Status command - shows plugin system status
func (c *CLICommands) createStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show plugin system status",
		Long:  "Display overall plugin system status and health",
		RunE:  c.runStatusCommand,
	}

	cmd.Flags().Bool("health", false, "Include health checks")
	cmd.Flags().String("format", "table", "Output format (table, json)")

	return cmd
}

func (c *CLICommands) runStatusCommand(cmd *cobra.Command, args []string) error {
	includeHealth, _ := cmd.Flags().GetBool("health")
	format, _ := cmd.Flags().GetString("format")

	status := c.manager.Status()

	var health map[string]error
	if includeHealth {
		health = c.manager.Health(context.Background())
	}

	switch format {
	case "json":
		return c.outputStatusJSON(status, health)
	default:
		return c.outputStatusTable(status, health)
	}
}

// Validate command - validates plugins
func (c *CLICommands) createValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [plugin-name]",
		Short: "Validate plugins",
		Long:  "Validate plugin configuration and dependencies",
		Args:  cobra.MaximumNArgs(1),
		RunE:  c.runValidateCommand,
	}

	cmd.Flags().Bool("all", false, "Validate all plugins")
	cmd.Flags().Bool("config", true, "Validate configuration")
	cmd.Flags().Bool("deps", true, "Validate dependencies")

	return cmd
}

func (c *CLICommands) runValidateCommand(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	validateConfig, _ := cmd.Flags().GetBool("config")
	validateDeps, _ := cmd.Flags().GetBool("deps")

	var plugins []Plugin
	if all {
		plugins = c.manager.List()
	} else if len(args) > 0 {
		plugin, err := c.manager.Get(args[0])
		if err != nil {
			return err
		}
		plugins = []Plugin{plugin}
	} else {
		return fmt.Errorf("plugin name required (or use --all)")
	}

	hasErrors := false
	for _, plugin := range plugins {
		fmt.Printf("Validating plugin: %s\n", plugin.Name())

		if validateConfig {
			if err := c.validatePluginConfig(plugin); err != nil {
				fmt.Printf("  ❌ Configuration: %v\n", err)
				hasErrors = true
			} else {
				fmt.Printf("  ✓ Configuration: OK\n")
			}
		}

		if validateDeps {
			if err := c.validatePluginDependencies(plugin); err != nil {
				fmt.Printf("  ❌ Dependencies: %v\n", err)
				hasErrors = true
			} else {
				fmt.Printf("  ✓ Dependencies: OK\n")
			}
		}
	}

	if hasErrors {
		return fmt.Errorf("validation failed")
	}

	fmt.Println("✓ All validations passed")
	return nil
}

// Dev command - development utilities
func (c *CLICommands) createDevCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Development utilities",
		Long:  "Commands for plugin development and debugging",
	}

	cmd.AddCommand(c.createDevReloadCommand())
	cmd.AddCommand(c.createDevLogsCommand())
	cmd.AddCommand(c.createDevInspectCommand())

	return cmd
}

func (c *CLICommands) createDevReloadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reload [plugin-name]",
		Short: "Hot reload a plugin",
		Args:  cobra.ExactArgs(1),
		RunE:  c.runDevReloadCommand,
	}
}

func (c *CLICommands) runDevReloadCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]

	if err := c.manager.Reload(pluginName); err != nil {
		return fmt.Errorf("failed to reload plugin: %w", err)
	}

	fmt.Printf("✓ Plugin %s reloaded successfully\n", pluginName)
	return nil
}

func (c *CLICommands) createDevLogsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logs [plugin-name]",
		Short: "Show plugin logs",
		Args:  cobra.MaximumNArgs(1),
		RunE:  c.runDevLogsCommand,
	}
}

func (c *CLICommands) runDevLogsCommand(cmd *cobra.Command, args []string) error {
	// This would show plugin-specific logs
	fmt.Println("Plugin logs functionality not yet implemented")
	return nil
}

func (c *CLICommands) createDevInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect [plugin-name]",
		Short: "Inspect plugin internals",
		Args:  cobra.ExactArgs(1),
		RunE:  c.runDevInspectCommand,
	}
}

func (c *CLICommands) runDevInspectCommand(cmd *cobra.Command, args []string) error {
	pluginName := args[0]

	plugin, err := c.manager.Get(pluginName)
	if err != nil {
		return err
	}

	fmt.Printf("Plugin: %s\n", plugin.Name())
	fmt.Printf("Version: %s\n", plugin.Version())
	fmt.Printf("Author: %s\n", plugin.Author())
	fmt.Printf("Description: %s\n", plugin.Description())

	fmt.Printf("\nRoutes: %d\n", len(plugin.Routes()))
	for _, route := range plugin.Routes() {
		fmt.Printf("  %s %s\n", route.Method, route.Pattern)
	}

	fmt.Printf("\nJobs: %d\n", len(plugin.Jobs()))
	for _, job := range plugin.Jobs() {
		fmt.Printf("  %s (%s)\n", job.Name, job.Type)
	}

	fmt.Printf("\nMiddleware: %d\n", len(plugin.Middleware()))
	for _, mw := range plugin.Middleware() {
		fmt.Printf("  %s\n", mw.Name)
	}

	fmt.Printf("\nCommands: %d\n", len(plugin.Commands()))
	for _, cmd := range plugin.Commands() {
		fmt.Printf("  %s - %s\n", cmd.Name, cmd.Description)
	}

	return nil
}

// Helper methods for the CLI commands

func (c *CLICommands) sortPlugins(plugins []Plugin, sortBy string) {
	sort.Slice(plugins, func(i, j int) bool {
		switch sortBy {
		case "version":
			return plugins[i].Version() < plugins[j].Version()
		case "author":
			return plugins[i].Author() < plugins[j].Author()
		case "status":
			statusI := c.manager.Status()[plugins[i].Name()]
			statusJ := c.manager.Status()[plugins[j].Name()]
			return statusI.Enabled && !statusJ.Enabled
		default: // name
			return plugins[i].Name() < plugins[j].Name()
		}
	})
}

func (c *CLICommands) outputPluginsTable(plugins []Plugin) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tSTATUS\tAUTHOR\tDESCRIPTION")

	status := c.manager.Status()
	for _, plugin := range plugins {
		pluginStatus := status[plugin.Name()]
		statusStr := "Disabled"
		if pluginStatus.Enabled {
			statusStr = "Enabled"
		}
		if pluginStatus.Error != "" {
			statusStr = "Error"
		}

		description := plugin.Description()
		if len(description) > 50 {
			description = description[:47] + "..."
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			plugin.Name(),
			plugin.Version(),
			statusStr,
			plugin.Author(),
			description,
		)
	}

	return w.Flush()
}

func (c *CLICommands) outputPluginsJSON(plugins []Plugin) error {
	var output []map[string]interface{}
	status := c.manager.Status()

	for _, plugin := range plugins {
		pluginStatus := status[plugin.Name()]
		output = append(output, map[string]interface{}{
			"name":        plugin.Name(),
			"version":     plugin.Version(),
			"author":      plugin.Author(),
			"description": plugin.Description(),
			"enabled":     pluginStatus.Enabled,
			"status":      pluginStatus,
		})
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func (c *CLICommands) outputPluginsYAML(plugins []Plugin) error {
	// YAML output would be implemented with a YAML library
	fmt.Println("YAML output not implemented")
	return nil
}

func (c *CLICommands) outputPluginInfoTable(plugin Plugin, status PluginStatus) error {
	fmt.Printf("Name:        %s\n", plugin.Name())
	fmt.Printf("Version:     %s\n", plugin.Version())
	fmt.Printf("Author:      %s\n", plugin.Author())
	fmt.Printf("Description: %s\n", plugin.Description())
	fmt.Printf("Status:      %s\n", func() string {
		if status.Enabled {
			return "Enabled"
		}
		return "Disabled"
	}())
	fmt.Printf("Health:      %s\n", status.Health)

	if status.Error != "" {
		fmt.Printf("Error:       %s\n", status.Error)
	}

	fmt.Printf("\nCapabilities:\n")
	fmt.Printf("  Routes:      %d\n", len(plugin.Routes()))
	fmt.Printf("  Jobs:        %d\n", len(plugin.Jobs()))
	fmt.Printf("  Middleware:  %d\n", len(plugin.Middleware()))
	fmt.Printf("  Commands:    %d\n", len(plugin.Commands()))
	fmt.Printf("  Health Checks: %d\n", len(plugin.HealthChecks()))

	deps := plugin.Dependencies()
	if len(deps) > 0 {
		fmt.Printf("\nDependencies:\n")
		for _, dep := range deps {
			optional := ""
			if dep.Optional {
				optional = " (optional)"
			}
			fmt.Printf("  %s %s%s\n", dep.Name, dep.Version, optional)
		}
	}

	return nil
}

func (c *CLICommands) outputPluginInfoJSON(plugin Plugin, status PluginStatus) error {
	info := map[string]interface{}{
		"name":          plugin.Name(),
		"version":       plugin.Version(),
		"author":        plugin.Author(),
		"description":   plugin.Description(),
		"status":        status,
		"routes":        plugin.Routes(),
		"jobs":          plugin.Jobs(),
		"middleware":    plugin.Middleware(),
		"commands":      plugin.Commands(),
		"health_checks": plugin.HealthChecks(),
		"dependencies":  plugin.Dependencies(),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(info)
}

func (c *CLICommands) outputPluginInfoYAML(plugin Plugin, status PluginStatus) error {
	fmt.Println("YAML output not implemented")
	return nil
}

func (c *CLICommands) outputSearchResults(results []PluginMetadata) error {
	if len(results) == 0 {
		fmt.Println("No plugins found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tAUTHOR\tDESCRIPTION\tDOWNLOADS")

	for _, plugin := range results {
		description := plugin.Description
		if len(description) > 60 {
			description = description[:57] + "..."
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n",
			plugin.Name,
			plugin.Version,
			plugin.Author,
			description,
			plugin.Downloads,
		)
	}

	return w.Flush()
}

func (c *CLICommands) outputStatusTable(status map[string]PluginStatus, health map[string]error) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PLUGIN\tSTATUS\tHEALTH\tERROR")

	for name, s := range status {
		healthStatus := "N/A"
		errorMsg := ""

		if health != nil {
			if err, exists := health[name]; exists {
				if err != nil {
					healthStatus = "Unhealthy"
					errorMsg = err.Error()
				} else {
					healthStatus = "Healthy"
				}
			}
		}

		if s.Error != "" && errorMsg == "" {
			errorMsg = s.Error
		}

		if len(errorMsg) > 50 {
			errorMsg = errorMsg[:47] + "..."
		}

		statusStr := "Disabled"
		if s.Enabled {
			statusStr = "Enabled"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			name,
			statusStr,
			healthStatus,
			errorMsg,
		)
	}

	return w.Flush()
}

func (c *CLICommands) outputStatusJSON(status map[string]PluginStatus, health map[string]error) error {
	output := map[string]interface{}{
		"status": status,
		"health": health,
		"summary": map[string]interface{}{
			"total_plugins": len(status),
			"enabled_plugins": func() int {
				count := 0
				for _, s := range status {
					if s.Enabled {
						count++
					}
				}
				return count
			}(),
		},
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func (c *CLICommands) convertStringValue(value string) interface{} {
	// Try to convert to appropriate types
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	// Try duration
	if duration, err := time.ParseDuration(value); err == nil {
		return duration
	}

	// Try integer
	if i, err := fmt.Sscanf(value, "%d", new(int)); i == 1 && err == nil {
		var result int
		fmt.Sscanf(value, "%d", &result)
		return result
	}

	// Try float
	if i, err := fmt.Sscanf(value, "%f", new(float64)); i == 1 && err == nil {
		var result float64
		fmt.Sscanf(value, "%f", &result)
		return result
	}

	// Default to string
	return value
}

// Placeholder implementations for install/update functionality

func (c *CLICommands) installFromFile(filePath string) (Plugin, error) {
	return c.loader.LoadPlugin(filePath)
}

func (c *CLICommands) installFromURL(url string) (Plugin, error) {
	// This would download and install from URL
	return nil, fmt.Errorf("URL installation not implemented")
}

func (c *CLICommands) installFromRegistry(name, version string) (Plugin, error) {
	// This would download from registry
	return nil, fmt.Errorf("registry installation not implemented")
}

func (c *CLICommands) installDependencies(plugin Plugin) error {
	// This would recursively install dependencies
	return nil
}

func (c *CLICommands) checkUninstallDependencies(pluginName string) error {
	// This would check if other plugins depend on this one
	return nil
}

func (c *CLICommands) updateAllPlugins(checkOnly bool) error {
	// This would update all plugins
	return nil
}

func (c *CLICommands) updatePlugin(name string, checkOnly bool) error {
	// This would update a specific plugin
	return nil
}

func (c *CLICommands) validatePluginConfig(plugin Plugin) error {
	config := c.manager.GetConfig(plugin.Name())
	return plugin.ValidateConfig(config)
}

func (c *CLICommands) validatePluginDependencies(plugin Plugin) error {
	for _, dep := range plugin.Dependencies() {
		if dep.Optional {
			continue
		}

		if _, err := c.manager.Get(dep.Name); err != nil {
			return fmt.Errorf("missing dependency: %s", dep.Name)
		}
	}
	return nil
}
