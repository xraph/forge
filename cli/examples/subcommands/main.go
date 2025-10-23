package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/cli"
)

func main() {
	// Create CLI with subcommands
	app := cli.New(cli.Config{
		Name:        "project",
		Version:     "1.0.0",
		Description: "Project management CLI",
	})

	// Create "project" command with subcommands
	projectCmd := cli.NewCommand(
		"project",
		"Project management commands",
		nil, // No direct handler
	)

	// Add "new" subcommand
	newCmd := cli.NewCommand(
		"new",
		"Create a new project",
		func(ctx cli.CommandContext) error {
			name := ctx.String("name")
			template := ctx.String("template")

			if name == "" {
				var err error
				name, err = ctx.Prompt("Project name:")
				if err != nil {
					return err
				}
			}

			spinner := ctx.Spinner(fmt.Sprintf("Creating project %s with template %s...", name, template))
			// Simulate work
			spinner.Update(fmt.Sprintf("Creating project %s...", name))
			spinner.Stop(cli.Green("✓ Project created successfully!"))

			ctx.Println("")
			ctx.Println("Next steps:")
			ctx.Println("  cd", name)
			ctx.Println("  make build")

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("name", "n", "Project name", "")),
		cli.WithFlag(cli.NewStringFlag("template", "t", "Project template", "basic",
			cli.ValidateEnum("basic", "api", "fullstack"))),
	)

	// Add "list" subcommand
	listCmd := cli.NewCommand(
		"list",
		"List all projects",
		func(ctx cli.CommandContext) error {
			ctx.Info("Projects:")

			table := ctx.Table()
			table.SetHeader([]string{"ID", "Name", "Status", "Created"})
			table.AppendRow([]string{"1", "project-a", cli.Green("Active"), "2025-10-01"})
			table.AppendRow([]string{"2", "project-b", cli.Yellow("Paused"), "2025-10-15"})
			table.AppendRow([]string{"3", "project-c", cli.Green("Active"), "2025-10-20"})
			table.Render()

			return nil
		},
	)

	// Add "delete" subcommand
	deleteCmd := cli.NewCommand(
		"delete",
		"Delete a project",
		func(ctx cli.CommandContext) error {
			name := ctx.String("name")
			if name == "" {
				return cli.NewError("project name is required", cli.ExitUsageError)
			}

			confirmed, err := ctx.Confirm(fmt.Sprintf("Are you sure you want to delete project '%s'?", name))
			if err != nil {
				return err
			}

			if !confirmed {
				ctx.Info("Cancelled")
				return nil
			}

			ctx.Success(fmt.Sprintf("Project '%s' deleted", name))
			return nil
		},
		cli.WithFlag(cli.NewStringFlag("name", "n", "Project name", "", cli.Required())),
	)

	// Add subcommands to project command
	projectCmd.AddSubcommand(newCmd)
	projectCmd.AddSubcommand(listCmd)
	projectCmd.AddSubcommand(deleteCmd)

	app.AddCommand(projectCmd)

	// Run the CLI
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}
