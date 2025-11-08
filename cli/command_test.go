package cli

import (
	"testing"
)

func TestNewCommand(t *testing.T) {
	called := false
	handler := func(ctx CommandContext) error {
		called = true

		return nil
	}

	cmd := NewCommand("test", "Test command", handler)

	if cmd.Name() != "test" {
		t.Errorf("expected name 'test', got '%s'", cmd.Name())
	}

	if cmd.Description() != "Test command" {
		t.Errorf("expected description 'Test command', got '%s'", cmd.Description())
	}

	// Create a mock context to test the handler
	ctx := &commandContext{
		args:  []string{},
		flags: make(map[string]*flagValue),
	}

	err := cmd.Run(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !called {
		t.Error("handler was not called")
	}
}

func TestCommandWithAliases(t *testing.T) {
	cmd := NewCommand(
		"test",
		"Test command",
		nil,
		WithAliases("t", "tst"),
	)

	aliases := cmd.Aliases()
	if len(aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(aliases))
	}

	if aliases[0] != "t" || aliases[1] != "tst" {
		t.Errorf("unexpected aliases: %v", aliases)
	}
}

func TestCommandWithFlags(t *testing.T) {
	flag1 := NewStringFlag("name", "n", "Name", "default")
	flag2 := NewBoolFlag("verbose", "v", "Verbose", false)

	cmd := NewCommand(
		"test",
		"Test command",
		nil,
		WithFlags(flag1, flag2),
	)

	flags := cmd.Flags()
	if len(flags) != 2 {
		t.Errorf("expected 2 flags, got %d", len(flags))
	}
}

func TestAddSubcommand(t *testing.T) {
	parent := NewCommand("parent", "Parent command", nil)
	child := NewCommand("child", "Child command", nil)

	err := parent.AddSubcommand(child)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	subcommands := parent.Subcommands()
	if len(subcommands) != 1 {
		t.Errorf("expected 1 subcommand, got %d", len(subcommands))
	}

	if subcommands[0].Name() != "child" {
		t.Errorf("expected subcommand name 'child', got '%s'", subcommands[0].Name())
	}

	// Check parent is set
	if child.Parent() != parent {
		t.Error("expected parent to be set on child command")
	}
}

func TestFindSubcommand(t *testing.T) {
	parent := NewCommand("parent", "Parent command", nil)
	child := NewCommand("child", "Child command", nil, WithAliases("c"))

	parent.AddSubcommand(child)

	// Find by name
	found, ok := parent.FindSubcommand("child")
	if !ok {
		t.Error("expected to find subcommand by name")
	}

	if found.Name() != "child" {
		t.Errorf("expected found command name 'child', got '%s'", found.Name())
	}

	// Find by alias
	found, ok = parent.FindSubcommand("c")
	if !ok {
		t.Error("expected to find subcommand by alias")
	}

	if found.Name() != "child" {
		t.Errorf("expected found command name 'child', got '%s'", found.Name())
	}

	// Not found
	_, ok = parent.FindSubcommand("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent subcommand")
	}
}

func TestCommandMiddleware(t *testing.T) {
	calls := []string{}

	beforeMiddleware := func(next CommandHandler) CommandHandler {
		return func(ctx CommandContext) error {
			calls = append(calls, "before")

			return next(ctx)
		}
	}

	afterMiddleware := func(next CommandHandler) CommandHandler {
		return func(ctx CommandContext) error {
			err := next(ctx)

			calls = append(calls, "after")

			return err
		}
	}

	handler := func(ctx CommandContext) error {
		calls = append(calls, "handler")

		return nil
	}

	cmd := NewCommand("test", "Test command", handler)
	cmd.Before(beforeMiddleware)
	cmd.After(afterMiddleware)

	ctx := &commandContext{
		args:  []string{},
		flags: make(map[string]*flagValue),
	}

	err := cmd.Run(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Check execution order
	expected := []string{"before", "handler", "after"}
	if len(calls) != len(expected) {
		t.Errorf("expected %d calls, got %d", len(expected), len(calls))
	}

	for i, call := range calls {
		if call != expected[i] {
			t.Errorf("expected call %d to be '%s', got '%s'", i, expected[i], call)
		}
	}
}
