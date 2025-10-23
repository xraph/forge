package cli

import (
	"bytes"
	"testing"
)

func TestNewCLI(t *testing.T) {
	app := New(Config{
		Name:        "testapp",
		Version:     "1.0.0",
		Description: "Test application",
	})

	if app.Name() != "testapp" {
		t.Errorf("expected name 'testapp', got '%s'", app.Name())
	}

	if app.Version() != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", app.Version())
	}

	if app.Description() != "Test application" {
		t.Errorf("expected description 'Test application', got '%s'", app.Description())
	}
}

func TestAddCommand(t *testing.T) {
	app := New(Config{
		Name:    "testapp",
		Version: "1.0.0",
	})

	cmd := NewCommand("test", "Test command", func(ctx CommandContext) error {
		return nil
	})

	err := app.AddCommand(cmd)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	commands := app.Commands()
	if len(commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(commands))
	}

	if commands[0].Name() != "test" {
		t.Errorf("expected command name 'test', got '%s'", commands[0].Name())
	}
}

func TestDuplicateCommand(t *testing.T) {
	app := New(Config{
		Name:    "testapp",
		Version: "1.0.0",
	})

	cmd1 := NewCommand("test", "Test command", func(ctx CommandContext) error {
		return nil
	})
	cmd2 := NewCommand("test", "Another test command", func(ctx CommandContext) error {
		return nil
	})

	err := app.AddCommand(cmd1)
	if err != nil {
		t.Errorf("expected no error for first command, got %v", err)
	}

	err = app.AddCommand(cmd2)
	if err == nil {
		t.Error("expected error for duplicate command, got nil")
	}
}

func TestRunCommandWithFlags(t *testing.T) {
	var capturedName string
	var capturedVerbose bool

	app := New(Config{
		Name:    "testapp",
		Version: "1.0.0",
	})

	cmd := NewCommand(
		"greet",
		"Greet someone",
		func(ctx CommandContext) error {
			capturedName = ctx.String("name")
			capturedVerbose = ctx.Bool("verbose")
			return nil
		},
		WithFlag(NewStringFlag("name", "n", "Name to greet", "World")),
		WithFlag(NewBoolFlag("verbose", "v", "Verbose output", false)),
	)

	app.AddCommand(cmd)

	// Test with flags
	args := []string{"testapp", "greet", "--name=John", "--verbose"}
	err := app.Run(args)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if capturedName != "John" {
		t.Errorf("expected name 'John', got '%s'", capturedName)
	}

	if !capturedVerbose {
		t.Error("expected verbose to be true")
	}
}

func TestRunCommandWithShortFlags(t *testing.T) {
	var capturedName string

	app := New(Config{
		Name:    "testapp",
		Version: "1.0.0",
	})

	cmd := NewCommand(
		"greet",
		"Greet someone",
		func(ctx CommandContext) error {
			capturedName = ctx.String("name")
			return nil
		},
		WithFlag(NewStringFlag("name", "n", "Name to greet", "World")),
	)

	app.AddCommand(cmd)

	// Test with short flag
	args := []string{"testapp", "greet", "-n", "Jane"}
	err := app.Run(args)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if capturedName != "Jane" {
		t.Errorf("expected name 'Jane', got '%s'", capturedName)
	}
}

func TestHelpOutput(t *testing.T) {
	output := &bytes.Buffer{}

	app := New(Config{
		Name:        "testapp",
		Version:     "1.0.0",
		Description: "Test application",
		Output:      output,
	})

	cmd := NewCommand("test", "Test command", func(ctx CommandContext) error {
		return nil
	})

	app.AddCommand(cmd)

	// Run with help flag
	args := []string{"testapp", "--help"}
	err := app.Run(args)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	helpOutput := output.String()
	if helpOutput == "" {
		t.Error("expected help output, got empty string")
	}

	// Check for expected content
	if !contains(helpOutput, "testapp") {
		t.Error("help output should contain app name")
	}

	if !contains(helpOutput, "COMMANDS") {
		t.Error("help output should contain COMMANDS section")
	}
}

func TestVersionOutput(t *testing.T) {
	output := &bytes.Buffer{}

	app := New(Config{
		Name:    "testapp",
		Version: "1.0.0",
		Output:  output,
	})

	// Run with version flag
	args := []string{"testapp", "--version"}
	err := app.Run(args)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	versionOutput := output.String()
	if !contains(versionOutput, "1.0.0") {
		t.Error("version output should contain version number")
	}
}

// Helper function
func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}
