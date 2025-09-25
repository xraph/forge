package forge

import (
	"context"
	"fmt"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/plugins"
)

// AddPluginFromSource adds a plugin from any source
func (app *ForgeApplication) AddPluginFromSource(ctx context.Context, source PluginSource) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("add_plugin", fmt.Errorf("cannot add plugin after application has started"))
	}

	// Load plugin from source using plugin manager
	plugin, err := source.LoadPlugin(ctx, app.pluginsManager)
	if err != nil {
		return fmt.Errorf("failed to load plugin from source: %w", err)
	}

	pluginName := plugin.Name()

	// Initialize plugin if it hasn't been initialized
	if err := plugin.Initialize(ctx, app.container); err != nil {
		return fmt.Errorf("failed to initialize plugin %s: %w", pluginName, err)
	}

	// Register plugin with plugin manager registry
	if app.pluginsManager != nil && app.pluginsManager.GetPluginStore() != nil {
		if err := app.pluginsManager.GetPluginStore().Registry().Register(plugin); err != nil {
			if app.logger != nil {
				app.logger.Warn("failed to register plugin in registry",
					logger.String("plugin", pluginName),
					logger.Error(err),
				)
			}
		}
	}

	if app.logger != nil {
		app.logger.Info("plugin added",
			logger.String("plugin", pluginName),
			logger.String("version", plugin.Version()),
			logger.String("description", plugin.Description()),
			logger.String("dependencies", fmt.Sprintf("%v", plugin.Dependencies())),
			logger.String("source_type", fmt.Sprintf("%T", source)),
		)
	}

	return nil
}

// AddPluginFromFile loads and adds a plugin from a file
func (app *ForgeApplication) AddPluginFromFile(ctx context.Context, filePath string, config map[string]interface{}) error {
	return app.AddPluginFromSource(ctx, &FilePluginSource{
		Path:   filePath,
		Config: config,
	})
}

// AddPluginFromMarketplace loads and adds a plugin from the marketplace
func (app *ForgeApplication) AddPluginFromMarketplace(ctx context.Context, pluginID, version string, config map[string]interface{}) error {
	return app.AddPluginFromSource(ctx, &MarketplacePluginSource{
		PluginID: pluginID,
		Version:  version,
		Config:   config,
	})
}

// AddPluginFromURL loads and adds a plugin from a URL
func (app *ForgeApplication) AddPluginFromURL(ctx context.Context, url, version string, config map[string]interface{}) error {
	return app.AddPluginFromSource(ctx, &URLPluginSource{
		URL:     url,
		Version: version,
		Config:  config,
	})
}

// InstallPlugin installs a plugin from the marketplace and adds it to the application
func (app *ForgeApplication) InstallPlugin(ctx context.Context, pluginID, version string, config map[string]interface{}) error {
	if app.pluginsManager == nil {
		return fmt.Errorf("plugin manager not initialized")
	}

	// Install plugin via plugin manager
	source := plugins.PluginSource{
		Type:     plugins.PluginSourceTypeMarketplace,
		Location: pluginID,
		Version:  version,
		Config:   config,
	}

	if err := app.pluginsManager.InstallPlugin(ctx, source); err != nil {
		return fmt.Errorf("failed to install plugin %s: %w", pluginID, err)
	}

	// Add the installed plugin to the application
	return app.AddPluginFromMarketplace(ctx, pluginID, version, config)
}

// LoadPluginFromBinary loads a plugin from a binary file or package
func (app *ForgeApplication) LoadPluginFromBinary(ctx context.Context, binaryPath string, config map[string]interface{}) error {
	return app.AddPluginFromFile(ctx, binaryPath, config)
}

// REPLACE YOUR EXISTING RemovePlugin METHOD WITH THIS:

// // RemovePlugin removes a plugin and unloads it if it was loaded from external source
// func (app *ForgeApplication) RemovePlugin(pluginName string) error {
// 	app.mu.Lock()
// 	defer app.mu.Unlock()
//
// 	if app.status != common.ApplicationStatusNotStarted {
// 		return common.ErrLifecycleError("remove_plugin", fmt.Errorf("cannot remove plugin after application has started"))
// 	}
//
// 	plugin, exists := app.plugins[pluginName]
// 	if !exists {
// 		return common.ErrPluginNotFound(pluginName)
// 	}
//
// 	// Remove from router if available
// 	if app.router != nil {
// 		if err := app.router.RemovePlugin(pluginName); err != nil {
// 			return err
// 		}
// 	}
//
// 	// Unload plugin if it was loaded via plugin manager
// 	if app.pluginsManager != nil {
// 		if err := app.pluginsManager.UnloadPlugin(context.Background(), pluginName); err != nil {
// 			// Log warning but don't fail - plugin might not have been loaded via manager
// 			if app.logger != nil {
// 				app.logger.Warn("failed to unload plugin from manager",
// 					logger.String("plugin", pluginName),
// 					logger.Error(err),
// 				)
// 			}
// 		}
// 	}
//
// 	// Cleanup plugin
// 	if err := plugin.Cleanup(context.Background()); err != nil {
// 		if app.logger != nil {
// 			app.logger.Warn("failed to cleanup plugin",
// 				logger.String("plugin", pluginName),
// 				logger.Error(err),
// 			)
// 		}
// 	}
//
// 	delete(app.plugins, pluginName)
//
// 	if app.logger != nil {
// 		app.logger.Info("plugin removed", logger.String("plugin", pluginName))
// 	}
//
// 	return nil
// }

// PluginSource represents different ways to specify a plugin
type PluginSource interface {
	// LoadPlugin loads the plugin from this source
	LoadPlugin(ctx context.Context, manager plugins.PluginManager) (plugins.Plugin, error)
}

// DirectPluginSource wraps an already instantiated plugin
type DirectPluginSource struct {
	Plugin plugins.Plugin
}

func (d *DirectPluginSource) LoadPlugin(ctx context.Context, manager plugins.PluginManager) (plugins.Plugin, error) {
	return d.Plugin, nil
}

// FilePluginSource loads plugin from a file path
type FilePluginSource struct {
	Path   string
	Config map[string]interface{}
}

func (f *FilePluginSource) LoadPlugin(ctx context.Context, manager plugins.PluginManager) (plugins.Plugin, error) {
	source := plugins.PluginSource{
		Type:     plugins.PluginSourceTypeFile,
		Location: f.Path,
		Config:   f.Config,
	}
	return manager.LoadPlugin(ctx, source)
}

// MarketplacePluginSource loads plugin from marketplace
type MarketplacePluginSource struct {
	PluginID string
	Version  string
	Config   map[string]interface{}
}

func (m *MarketplacePluginSource) LoadPlugin(ctx context.Context, manager plugins.PluginManager) (plugins.Plugin, error) {
	source := plugins.PluginSource{
		Type:     plugins.PluginSourceTypeMarketplace,
		Location: m.PluginID,
		Version:  m.Version,
		Config:   m.Config,
	}
	return manager.LoadPlugin(ctx, source)
}

// URLPluginSource loads plugin from a URL
type URLPluginSource struct {
	URL     string
	Version string
	Config  map[string]interface{}
}

func (u *URLPluginSource) LoadPlugin(ctx context.Context, manager plugins.PluginManager) (plugins.Plugin, error) {
	source := plugins.PluginSource{
		Type:     plugins.PluginSourceTypeURL,
		Location: u.URL,
		Version:  u.Version,
		Config:   u.Config,
	}
	return manager.LoadPlugin(ctx, source)
}
