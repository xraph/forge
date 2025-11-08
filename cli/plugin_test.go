package cli

import (
	"testing"
)

func TestNewBasePlugin(t *testing.T) {
	plugin := NewBasePlugin("test", "1.0.0", "Test plugin")

	if plugin.Name() != "test" {
		t.Errorf("expected name 'test', got '%s'", plugin.Name())
	}

	if plugin.Version() != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", plugin.Version())
	}

	if plugin.Description() != "Test plugin" {
		t.Errorf("expected description 'Test plugin', got '%s'", plugin.Description())
	}
}

func TestPluginAddCommand(t *testing.T) {
	plugin := NewBasePlugin("test", "1.0.0", "Test plugin")

	cmd := NewCommand("testcmd", "Test command", nil)
	plugin.AddCommand(cmd)

	commands := plugin.Commands()
	if len(commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(commands))
	}

	if commands[0].Name() != "testcmd" {
		t.Errorf("expected command name 'testcmd', got '%s'", commands[0].Name())
	}
}

func TestPluginRegistry(t *testing.T) {
	registry := NewPluginRegistry()

	plugin := NewBasePlugin("test", "1.0.0", "Test plugin")

	err := registry.Register(plugin)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !registry.Has("test") {
		t.Error("expected registry to have plugin")
	}

	if registry.Count() != 1 {
		t.Errorf("expected count 1, got %d", registry.Count())
	}
}

func TestPluginRegistryDuplicate(t *testing.T) {
	registry := NewPluginRegistry()

	plugin1 := NewBasePlugin("test", "1.0.0", "Test plugin")
	plugin2 := NewBasePlugin("test", "2.0.0", "Test plugin v2")

	err := registry.Register(plugin1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	err = registry.Register(plugin2)
	if !errors.Is(err, ErrPluginAlreadyRegistered) {
		t.Errorf("expected ErrPluginAlreadyRegistered, got %v", err)
	}
}

func TestPluginRegistryGet(t *testing.T) {
	registry := NewPluginRegistry()

	plugin := NewBasePlugin("test", "1.0.0", "Test plugin")
	registry.Register(plugin)

	retrieved, err := registry.Get("test")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if retrieved.Name() != "test" {
		t.Errorf("expected plugin name 'test', got '%s'", retrieved.Name())
	}
}

func TestPluginRegistryNotFound(t *testing.T) {
	registry := NewPluginRegistry()

	_, err := registry.Get("nonexistent")
	if !errors.Is(err, ErrPluginNotFound) {
		t.Errorf("expected ErrPluginNotFound, got %v", err)
	}
}

func TestRegisterPluginWithCLI(t *testing.T) {
	app := New(Config{
		Name:    "testapp",
		Version: "1.0.0",
	})

	plugin := NewBasePlugin("test", "1.0.0", "Test plugin")
	cmd := NewCommand("testcmd", "Test command", func(ctx CommandContext) error {
		return nil
	})
	plugin.AddCommand(cmd)

	err := app.RegisterPlugin(plugin)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Check that plugin commands were added to CLI
	commands := app.Commands()
	found := false

	for _, c := range commands {
		if c.Name() == "testcmd" {
			found = true

			break
		}
	}

	if !found {
		t.Error("expected plugin command to be added to CLI")
	}
}
