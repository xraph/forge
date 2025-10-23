package main

import (
	"fmt"
	"os"
	"time"

	"github.com/xraph/forge/v2/cli"
)

func main() {
	// Create CLI with interactive features
	app := cli.New(cli.Config{
		Name:        "interactive",
		Version:     "1.0.0",
		Description: "Interactive CLI example",
	})

	// Command demonstrating prompts
	setupCmd := cli.NewCommand(
		"setup",
		"Interactive setup wizard",
		func(ctx cli.CommandContext) error {
			ctx.Info("Welcome to the setup wizard!")
			ctx.Println("")

			// Simple prompt
			name, err := ctx.Prompt("What's your name?")
			if err != nil {
				return err
			}

			// Confirm prompt
			confirmed, err := ctx.Confirm(fmt.Sprintf("Is '%s' correct?", name))
			if err != nil {
				return err
			}

			if !confirmed {
				ctx.Warning("Setup cancelled")
				return nil
			}

			// Select prompt
			env, err := ctx.Select("Choose environment:", []string{
				"development",
				"staging",
				"production",
			})
			if err != nil {
				return err
			}

			// Multi-select prompt
			features, err := ctx.MultiSelect("Select features:", []string{
				"database",
				"cache",
				"events",
				"streaming",
			})
			if err != nil {
				return err
			}

			ctx.Println("")
			ctx.Success("Setup complete!")
			ctx.Printf("Name: %s\n", name)
			ctx.Printf("Environment: %s\n", env)
			ctx.Printf("Features: %v\n", features)

			return nil
		},
	)

	// Command demonstrating progress bar
	downloadCmd := cli.NewCommand(
		"download",
		"Download files with progress bar",
		func(ctx cli.CommandContext) error {
			total := 100
			progress := ctx.ProgressBar(total)

			for i := 0; i <= total; i++ {
				time.Sleep(30 * time.Millisecond)
				progress.Set(i)
			}

			progress.Finish("Download complete!")
			return nil
		},
	)

	// Command demonstrating spinner
	processCmd := cli.NewCommand(
		"process",
		"Process data with spinner",
		func(ctx cli.CommandContext) error {
			spinner := ctx.Spinner("Processing data...")

			// Simulate long-running task
			time.Sleep(3 * time.Second)

			spinner.Stop(cli.Green("✓ Processing complete!"))
			return nil
		},
	)

	// Command demonstrating table
	statusCmd := cli.NewCommand(
		"status",
		"Show service status",
		func(ctx cli.CommandContext) error {
			table := ctx.Table()
			table.SetHeader([]string{"Service", "Status", "Uptime", "Response Time"})

			table.AppendRow([]string{"API", cli.Green("✓ Healthy"), "99.9%", "12ms"})
			table.AppendRow([]string{"Database", cli.Yellow("⚠ Degraded"), "95.2%", "45ms"})
			table.AppendRow([]string{"Cache", cli.Red("✗ Down"), "0.0%", "-"})
			table.AppendRow([]string{"Queue", cli.Green("✓ Healthy"), "99.5%", "8ms"})

			table.Render()

			return nil
		},
	)

	app.AddCommand(setupCmd)
	app.AddCommand(downloadCmd)
	app.AddCommand(processCmd)
	app.AddCommand(statusCmd)

	// Run the CLI
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}
